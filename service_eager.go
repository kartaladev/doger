package doger

import (
	"context"
	"reflect"
	"sync"
	"sync/atomic"

	"github.com/kartaladev/doger/stacktrace"
)

var (
	_ serviceWrapper[int]       = (*serviceEager[int])(nil)
	_ serviceWrapperHealthcheck = (*serviceEager[int])(nil)
	_ serviceWrapperShutdown    = (*serviceEager[int])(nil)
	_ serviceWrapperClone       = (*serviceEager[int])(nil)
)

type serviceEager[T any] struct {
	opts     *serviceOptions // nil when registered without options
	regIndex uint64          // registration order, unique per root container
	name     string
	typeName string
	instance T

	providerFrame stacktrace.Frame
	invocationFrames

	// declared dependencies, resolved on the first successful invocation
	depsMu       sync.Mutex
	depsResolved bool

	built atomic.Bool // the after-build hook ran
}

func newServiceEager[T any](name string, instance T) *serviceEager[T] {
	providerFrame, _ := stacktrace.NewFrameFromCaller()

	return &serviceEager[T]{
		name:     name,
		typeName: inferServiceName[T](),
		instance: instance,

		providerFrame:    providerFrame,
		invocationFrames: newInvocationFrames(),
	}
}

func (s *serviceEager[T]) getName() string {
	return s.name
}

func (s *serviceEager[T]) getTypeName() string {
	return s.typeName
}

func (s *serviceEager[T]) getServiceType() ServiceType {
	return ServiceTypeEager
}

func (s *serviceEager[T]) getReflectType() reflect.Type {
	return reflect.TypeFor[T]() // if T is a pointer or interface, it will return a typed nil
}

func (s *serviceEager[T]) getInstanceAny(i Injector) (any, error) {
	return s.getInstance(i)
}

func (s *serviceEager[T]) getInstance(i Injector) (T, error) {
	// a closure here, not stacktrace.NewFrameFromCaller itself: the frame
	// recorded is then in this file, as before (invocation-frame rule, D16)
	s.recordInvocationFrame(invocationFrameCap(i), func() (stacktrace.Frame, bool) { return stacktrace.NewFrameFromCaller() }) //nolint:gocritic // see above

	if err := s.resolveDependsOn(i); err != nil {
		return empty[T](), err
	}

	// the first successful invocation counts as the build of an eager value;
	// the hook runs outside depsMu, so it may use this service
	if s.built.CompareAndSwap(false, true) {
		onAfterBuild(i, s.name, s.instance)
	}

	return s.instance, nil
}

// resolveDependsOn builds the declared dependencies once. The flag is set
// only on success, so a failed resolution is retried on the next invocation.
func (s *serviceEager[T]) resolveDependsOn(i Injector) error {
	if s.opts == nil || len(s.opts.dependsOn) == 0 {
		return nil
	}

	s.depsMu.Lock()
	defer s.depsMu.Unlock()

	if !s.depsResolved {
		if err := resolveDeclaredDependencies(i, s.opts); err != nil {
			return wrapBuildError(i, s.name, err)
		}
		s.depsResolved = true
	}

	return nil
}

func (s *serviceEager[T]) isHealthchecker() bool {
	_, ok1 := any(s.instance).(HealthcheckerWithContext)
	_, ok2 := any(s.instance).(Healthchecker)
	return ok1 || ok2
}

func (s *serviceEager[T]) healthcheck(ctx context.Context) error {
	if instance, ok := any(s.instance).(HealthcheckerWithContext); ok {
		if err := contextDone(ctx); err != nil {
			return err
		}

		return instance.HealthCheck(ctx)
	} else if instance, ok := any(s.instance).(Healthchecker); ok {
		if err := contextDone(ctx); err != nil {
			return err
		}

		return instance.HealthCheck()
	}

	return nil
}

func (s *serviceEager[T]) isShutdowner() bool {
	_, ok1 := any(s.instance).(ShutdownerWithContextAndError)
	_, ok2 := any(s.instance).(ShutdownerWithError)
	_, ok3 := any(s.instance).(ShutdownerWithContext)
	_, ok4 := any(s.instance).(Shutdowner)
	return ok1 || ok2 || ok3 || ok4
}

func (s *serviceEager[T]) shutdown(ctx context.Context) error {
	switch instance := any(s.instance).(type) {
	case ShutdownerWithContextAndError:
		if ctx.Err() != nil {
			return ctx.Err()
		}

		return instance.Shutdown(ctx)
	case ShutdownerWithError:
		if ctx.Err() != nil {
			return ctx.Err()
		}

		return instance.Shutdown()
	case ShutdownerWithContext:
		if ctx.Err() != nil {
			return ctx.Err()
		}

		instance.Shutdown(ctx)
	case Shutdowner:
		if ctx.Err() != nil {
			return ctx.Err()
		}

		instance.Shutdown()
	}

	return nil
}

func (s *serviceEager[T]) clone(newScope Injector) any {
	return &serviceEager[T]{
		name:     s.name,
		regIndex: s.regIndex,
		opts:     s.opts,
		typeName: s.typeName,
		instance: s.instance,

		providerFrame:    s.providerFrame,
		invocationFrames: newInvocationFrames(),
	}
}

func (s *serviceEager[T]) source() (stacktrace.Frame, []stacktrace.Frame) {
	return s.providerFrame, s.invocationFrameList()
}

func (s *serviceEager[T]) getRegistrationIndex() uint64 {
	return s.regIndex
}

func (s *serviceEager[T]) setRegistrationIndex(index uint64) {
	s.regIndex = index
}

func (s *serviceEager[T]) getOptions() *serviceOptions {
	return s.opts
}

func (s *serviceEager[T]) setOptions(opts *serviceOptions) {
	s.opts = opts
}
