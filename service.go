package doger

import (
	"context"
	"reflect"
	"sync"
	"sync/atomic"
	"time"

	"github.com/kartaladev/doger/internal/frames"
	"github.com/kartaladev/doger/stacktrace"
	typetostring "github.com/samber/go-type-to-string"
)

// defaultMaxInvocationFrames is the frame cap of InjectorOpts.MaxInvocationFrames 0.
const defaultMaxInvocationFrames = 100

// invocationFrames records the call sites a service was invoked from, each
// once, for debugging and observability. A wrapper embeds it.
type invocationFrames struct {
	invokationFramesMu      sync.RWMutex
	invokationFrames        map[stacktrace.Frame]struct{} // map garanties uniqueness
	invokationFramesCounter uint32                        // invocations counted against the cap
}

// newInvocationFrames returns an empty record.
func newInvocationFrames() invocationFrames {
	return invocationFrames{invokationFrames: map[stacktrace.Frame]struct{}{}}
}

// recordInvocationFrame records the frame returned by frame for each of the
// first limit invocations counted; a limit of 0 or less records nothing. frame is called only when the frame is recorded: capturing one
// is costly.
func (f *invocationFrames) recordInvocationFrame(limit int, frame func() (stacktrace.Frame, bool)) {
	if limit <= 0 {
		return
	}
	if uint64(atomic.AddUint32(&f.invokationFramesCounter, 1)) <= uint64(limit) {
		if fr, ok := frame(); ok {
			f.invokationFramesMu.Lock()
			f.invokationFrames[fr] = struct{}{}
			f.invokationFramesMu.Unlock()
		}
	}
}

// invocationFrameList returns the recorded frames, in no particular order.
func (f *invocationFrames) invocationFrameList() []stacktrace.Frame {
	f.invokationFramesMu.RLock()
	defer f.invokationFramesMu.RUnlock()

	frames := make([]stacktrace.Frame, 0, len(f.invokationFrames))
	for frame := range f.invokationFrames {
		frames = append(frames, frame)
	}

	return frames
}

// invocationFrameCap returns the frame cap of the container of i (see
// InjectorOpts.MaxInvocationFrames), or the default one when i is nil, as
// for a wrapper invoked directly.
func invocationFrameCap(i Injector) int {
	if i == nil {
		return defaultMaxInvocationFrames
	}

	switch n := i.RootScope().opts.MaxInvocationFrames; {
	case n == 0:
		return defaultMaxInvocationFrames
	case n < 0:
		return 0
	default:
		return n
	}
}

// ServiceType represents the different types of services that can be registered
// in the dependency injection container. Each type has different lifecycle
// and instantiation behavior.
type ServiceType string

const (
	// ServiceTypeLazy represents a service that is instantiated only when first requested.
	// The service instance is cached and reused for subsequent requests.
	ServiceTypeLazy ServiceType = "lazy"

	// ServiceTypeEager represents a service that is instantiated immediately when registered.
	// The service is always available and ready to use.
	ServiceTypeEager ServiceType = "eager"

	// ServiceTypeTransient represents a service that is recreated each time it is requested.
	// No singleton caching is performed, ensuring a fresh instance every time. It is basically a factory.
	ServiceTypeTransient ServiceType = "transient"

	// ServiceTypeAlias represents a service that is an alias to another service.
	// It provides a different interface or name for accessing an existing service.
	ServiceTypeAlias ServiceType = "alias"
)

// serviceTypeToIcon maps each service type to a visual icon for debugging
// and observability purposes in logs and UI displays.
var serviceTypeToIcon = map[ServiceType]string{
	ServiceTypeLazy:      "😴",
	ServiceTypeEager:     "🔁",
	ServiceTypeTransient: "🏭",
	ServiceTypeAlias:     "🔗",
}

// serviceWrapper[T] is the main interface that all services in the DI container must implement.
// It provides methods for serviceWrapper lifecycle management, health checking, and shutdown.
// The generic type T represents the type of the serviceWrapper instance.
type serviceWrapper[T any] interface {
	getName() string
	getTypeName() string
	getServiceType() ServiceType
	getReflectType() reflect.Type
	getInstanceAny(Injector) (any, error)
	getInstance(Injector) (T, error)
	isHealthchecker() bool
	healthcheck(context.Context) error
	isShutdowner() bool
	shutdown(context.Context) error
	clone(Injector) any
	source() (stacktrace.Frame, []stacktrace.Frame)
	getRegistrationIndex() uint64
	setRegistrationIndex(uint64)
	getOptions() *serviceOptions
	setOptions(*serviceOptions)
}

// serviceWrapperAny is a non-generic version of serviceWrapper[T] that provides access to
// service functionality without requiring type information. This is useful
// for internal operations where the specific type is not known.
type serviceWrapperAny interface {
	getName() string
	getTypeName() string
	getServiceType() ServiceType
	getReflectType() reflect.Type
	getInstanceAny(Injector) (any, error)
	// getInstance(Injector) (T, error) - Not available in non-generic interface
	isHealthchecker() bool
	healthcheck(context.Context) error
	isShutdowner() bool
	shutdown(context.Context) error
	clone(Injector) any
	source() (stacktrace.Frame, []stacktrace.Frame)
	getRegistrationIndex() uint64
	setRegistrationIndex(uint64)
	getOptions() *serviceOptions
	setOptions(*serviceOptions)
}

// Interface definitions for specific service capabilities.
// These interfaces allow for type-safe access to specific service methods
// without requiring the full serviceWrapper[T] interface.

type (
	serviceWrapperGetName         interface{ getName() string }
	serviceWrapperGetTypeName     interface{ getTypeName() string }
	serviceWrapperGetServiceType  interface{ getServiceType() ServiceType }
	serviceWrapperGetReflectType  interface{ getReflectType() reflect.Type }
	serviceWrapperGetInstanceAny  interface{ getInstanceAny(Injector) (any, error) }
	serviceWrapperIsHealthchecker interface{ isHealthchecker() bool }
	serviceWrapperHealthcheck     interface{ healthcheck(context.Context) error }
	serviceWrapperIsShutdowner    interface{ isShutdowner() bool }
	serviceWrapperShutdown        interface{ shutdown(context.Context) error }
	serviceWrapperClone           interface{ clone(Injector) any }
	serviceWrapperSource          interface {
		source() (stacktrace.Frame, []stacktrace.Frame)
	}
)

// serviceWrapperAliasTarget is implemented by aliases.
type serviceWrapperAliasTarget interface {
	aliasTarget() (Injector, string)
}

// rootPkgPath is the import path of this package: the provider frame of a
// value or alias registration skips its non-test frames, so it names the
// consumer's call site.
var rootPkgPath = reflect.TypeFor[Scope]().PkgPath()

// registrationFrame returns the consumer's call site of a value, alias or
// export registration, for the wrapper constructor calling it: the first
// frame outside this package, whose _test.go files count as outside. When
// that frame is deeper than frames.MaxDepth, it returns the wrapper
// constructor's own frame instead, never a zero frame.
func registrationFrame() stacktrace.Frame {
	// skip registrationFrame itself
	frame, ok := frames.Caller(1, frames.MaxDepth, rootPkgPath)
	if !ok {
		frame, _ = frames.Caller(1, frames.MaxDepth)
	}

	return stacktrace.Frame(frame)
}

type serviceWrapperBuildTime interface {
	getBuildTime() (time.Duration, bool)
}

// buildStateOf returns the build time of a service and whether it is built,
// read without waiting for a build in flight; a service with no build time
// reports 0 and false.
func buildStateOf(service any) (buildTime time.Duration, built bool) {
	if timed, ok := service.(serviceWrapperBuildTime); ok {
		return timed.getBuildTime()
	}

	return 0, false
}

// Interface compliance checks to ensure serviceWrapper[T] implements all required interfaces.
// These compile-time checks help catch interface implementation errors early.
var (
	_ serviceWrapperGetName         = serviceWrapper[int](nil)
	_ serviceWrapperGetTypeName     = serviceWrapper[int](nil)
	_ serviceWrapperGetServiceType  = serviceWrapper[int](nil)
	_ serviceWrapperGetReflectType  = serviceWrapper[int](nil)
	_ serviceWrapperGetInstanceAny  = serviceWrapper[int](nil)
	_ serviceWrapperIsHealthchecker = serviceWrapper[int](nil)
	_ serviceWrapperHealthcheck     = serviceWrapper[int](nil)
	_ serviceWrapperIsShutdowner    = serviceWrapper[int](nil)
	_ serviceWrapperShutdown        = serviceWrapper[int](nil)
	_ serviceWrapperClone           = serviceWrapper[int](nil)
	_ serviceWrapperSource          = serviceWrapper[int](nil)
)

// inferServiceName uses type inference to determine the service name
// based on the generic type parameter T. This is used internally
// to automatically generate service names from types.
func inferServiceName[T any]() string {
	return typetostring.GetType[T]()
}

// inferServiceProviderStacktrace extracts stacktrace information from a service
// for debugging and observability purposes. Transient services don't have
// provider stacktraces since they are recreated on each request.
func inferServiceProviderStacktrace(service serviceWrapperAny) (stacktrace.Frame, bool) {
	if service.getServiceType() == ServiceTypeTransient {
		return stacktrace.Frame{}, false
	}
	providerFrame, _ := service.source()
	return providerFrame, true
}

type serviceInfo struct {
	name             string
	serviceType      ServiceType
	serviceBuildTime time.Duration
	healthchecker    bool
	shutdowner       bool
}

func inferServiceInfo(injector Injector, name string) (serviceInfo, bool) {
	if serviceAny, ok := injector.serviceGet(name); ok {
		buildTime, _ := buildStateOf(serviceAny)

		return serviceInfo{
			name:             name,
			serviceType:      serviceAny.(serviceWrapperGetServiceType).getServiceType(), //nolint:errcheck,forcetypeassert // every value a scope stores is a service wrapper, which implements serviceWrapperAny
			serviceBuildTime: buildTime,
			healthchecker:    serviceAny.(serviceWrapperIsHealthchecker).isHealthchecker(), //nolint:errcheck,forcetypeassert // every value a scope stores is a service wrapper, which implements serviceWrapperAny
			shutdowner:       serviceAny.(serviceWrapperIsShutdowner).isShutdowner(),       //nolint:errcheck,forcetypeassert // every value a scope stores is a service wrapper, which implements serviceWrapperAny
		}, true
	}

	return serviceInfo{}, false
}

func serviceCanCastToGeneric[T any](service any) bool {
	if svc, ok := service.(serviceWrapperGetReflectType); ok {
		// we need type reflection here, because we don't want to invoke the service when not needed
		return typeCanCastToGeneric[T](svc.getReflectType())
	}

	return false
}

func serviceCanCastToType(service any, toType reflect.Type) bool {
	if svc, ok := service.(serviceWrapperGetReflectType); ok {
		// we need type reflection here, because we don't want to invoke the service when not needed
		return typeCanCastToType(svc.getReflectType(), toType)
	}

	return false
}
