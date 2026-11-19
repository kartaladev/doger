package doger

import (
	"context"
	"reflect"
	"sync"
	"sync/atomic"
	"time"

	"github.com/kartaladev/doger/stacktrace"
)

var (
	_ serviceWrapper[int]       = (*serviceLazy[int])(nil)
	_ serviceWrapperHealthcheck = (*serviceLazy[int])(nil)
	_ serviceWrapperShutdown    = (*serviceLazy[int])(nil)
	_ serviceWrapperClone       = (*serviceLazy[int])(nil)
)

type serviceLazy[T any] struct {
	opts     *serviceOptions // nil when registered without options
	regIndex uint64          // registration order, unique per root container
	mu       sync.RWMutex
	name     string
	typeName string
	instance T

	// lazy loading
	built bool
	// terminal is set once the service was shut down: it is never built
	// again, so a caller that looked it up before gets ErrShutdown
	terminal bool
	// state holds the build in flight (D1): the provider runs without s.mu,
	// and other callers wait for the build instead of a lock, so a cycle is
	// reported instead of deadlocking
	state buildState
	// published holds the instance once built, nil otherwise; health
	// checks read it without s.mu, which a build holds for the whole
	// provider call.
	published atomic.Pointer[T]
	// buildTime holds the time.Duration of the last successful build, in
	// nanoseconds; explain reads it without s.mu.
	buildTime atomic.Int64
	provider  Provider[T]

	providerFrame stacktrace.Frame
	invocationFrames
}

func newServiceLazy[T any](name string, provider Provider[T]) *serviceLazy[T] {
	providerFrame, _ := stacktrace.NewFrameFromPC(reflect.ValueOf(provider).Pointer())

	return &serviceLazy[T]{
		mu:       sync.RWMutex{},
		name:     name,
		typeName: inferServiceName[T](),

		built:    false,
		provider: provider,

		providerFrame:    providerFrame,
		invocationFrames: newInvocationFrames(),
	}
}

func (s *serviceLazy[T]) getName() string {
	return s.name
}

func (s *serviceLazy[T]) getTypeName() string {
	return s.typeName
}

func (s *serviceLazy[T]) getServiceType() ServiceType {
	return ServiceTypeLazy
}

func (s *serviceLazy[T]) getReflectType() reflect.Type {
	return reflect.TypeFor[T]() // if T is a pointer or interface, it will return a typed nil
}

func (s *serviceLazy[T]) getInstanceAny(i Injector) (any, error) {
	return s.getInstance(i)
}

func (s *serviceLazy[T]) getInstance(i Injector) (T, error) {
	// a closure here, not stacktrace.NewFrameFromCaller itself: the frame
	// recorded is then in this file, as before (invocation-frame rule, D16)
	s.recordInvocationFrame(invocationFrameCap(i), func() (stacktrace.Frame, bool) { return stacktrace.NewFrameFromCaller() }) //nolint:gocritic // see above

	instance, built, err := s.instanceOrBuild(i)
	if err != nil {
		return empty[T](), err
	}

	// outside s.mu, so the hook may use this service
	if built {
		onAfterBuild(i, s.name, instance)
	}

	return instance, nil
}

// instanceOrBuild returns the instance, building it first if needed, and
// whether this call built it.
func (s *serviceLazy[T]) instanceOrBuild(i Injector) (instance T, built bool, err error) {
	for {
		s.mu.Lock()
		switch {
		case s.terminal:
			s.mu.Unlock()
			return empty[T](), false, shutDownServiceError(s.name)
		case s.built:
			instance := s.instance
			s.mu.Unlock()
			return instance, false, nil
		}

		run, inFlight := s.state.claimLocked()
		s.mu.Unlock()
		if inFlight {
			// a waiter gets the builder's failure; on success it reads the
			// built instance on the next turn
			if err := waitForBuild(i, s.name, run); err != nil {
				return empty[T](), false, err
			}
			continue
		}

		instance, err := s.runBuild(i, run)
		return instance, err == nil, err
	}
}

// runBuild builds the service as run, without s.mu, and publishes the
// outcome to run's waiters, also when the provider exits its goroutine with
// runtime.Goexit or a hook panics.
func (s *serviceLazy[T]) runBuild(i Injector, run *buildRun) (instance T, err error) {
	finished := false
	defer func() {
		if finished {
			return
		}
		if r := recover(); r != nil {
			s.finishBuild(run, empty[T](), 0, libErrorf("%w: %v", ErrProviderPanic, r))
			panic(r)
		}
		// runtime.Goexit: the waiters are released, the caller never returns
		s.finishBuild(run, empty[T](), 0, wrapBuildError(i, s.name, libErrorMatching(ErrProviderPanic, "provider called runtime.Goexit")))
	}()

	instance, elapsed, err := s.build(i)
	finished = true
	s.finishBuild(run, instance, elapsed, err)

	return instance, err
}

// finishBuild records the outcome of run: a successful build publishes the
// instance, a failed one leaves the service buildable again. Waiters are
// then released with err.
func (s *serviceLazy[T]) finishBuild(run *buildRun, instance T, elapsed time.Duration, err error) {
	s.mu.Lock()
	if err == nil {
		s.instance = instance
		s.built = true
		s.buildTime.Store(int64(elapsed)) // written before the instance is published
		s.published.Store(&instance)
	}
	s.state.endLocked(run, err)
	s.mu.Unlock()

	run.release()
}

// build calls the provider after the declared dependencies, and returns the
// instance and the time the build took on the container clock.
func (s *serviceLazy[T]) build(i Injector) (instance T, elapsed time.Duration, err error) {
	// the clock of the injector's root scope, or the real clock when there
	// is no injector
	clock := RealClock()
	if i != nil {
		clock = i.RootScope().opts.Clock
	}
	start := clock.Now()

	if err := resolveDeclaredDependencies(i, s.opts); err != nil {
		return empty[T](), 0, wrapBuildError(i, s.name, err)
	}

	instance, err = handleProviderPanic(s.provider, i)
	if err != nil {
		return empty[T](), 0, wrapBuildError(i, s.name, err)
	}

	return instance, clock.Since(start), nil
}

// onAfterBuild runs the after-build hooks for an instance a wrapper built,
// with the scope behind the injector the wrapper received.
func onAfterBuild(i Injector, name string, instance any) {
	if scope := injectorScope(i); scope != nil {
		scope.RootScope().hooks.onAfterBuild(scope, name, instance)
	}
}

// shutDownServiceError is the error of an invocation of the service name
// once it was shut down.
func shutDownServiceError(name string) error {
	return libErrorMatching(ErrShutdown, "service `%s` is shut down", name)
}

// injectorScope returns the *Scope behind an injector a wrapper receives
// (usually a provider's virtual scope), or nil.
func injectorScope(i Injector) *Scope {
	switch injector := i.(type) {
	case *virtualScope:
		return injectorScope(injector.self)
	case *Scope:
		return injector
	case *RootScope:
		return injector.self
	case nil:
		return nil
	default:
		// a type embedding an Injector
		return injector.ownScope()
	}
}

// wrapBuildError returns the error of a build of service name that failed
// with err, through the injector scope the wrapper received.
func wrapBuildError(scope Injector, name string, err error) error {
	return &ServiceError{Scope: injectorScopeName(scope), Service: name, Err: err}
}

// injectorScopeName returns the name of the *Scope behind an injector a
// wrapper receives, or "" when there is none.
func injectorScopeName(i Injector) string {
	if scope := injectorScope(i); scope != nil {
		return scope.Name()
	}

	return ""
}

// builtInstance returns the instance and whether it is built, without
// taking s.mu: a service being built counts as not built.
func (s *serviceLazy[T]) builtInstance() (instance T, built bool) {
	published := s.published.Load()
	if published == nil {
		return empty[T](), false
	}

	return *published, true
}

func (s *serviceLazy[T]) isHealthchecker() bool {
	instance, built := s.builtInstance()
	if !built {
		return false
	}

	_, ok1 := any(instance).(HealthcheckerWithContext)
	_, ok2 := any(instance).(Healthchecker)
	return ok1 || ok2
}

// healthcheck runs the check of the built instance without holding s.mu, so
// a check in flight never delays the service's shutdown or invocation.
func (s *serviceLazy[T]) healthcheck(ctx context.Context) error {
	instance, built := s.builtInstance()
	if !built {
		return nil
	}

	switch instance := any(instance).(type) {
	case HealthcheckerWithContext:
		if err := contextDone(ctx); err != nil {
			return err
		}

		return instance.HealthCheck(ctx)
	case Healthchecker:
		if err := contextDone(ctx); err != nil {
			return err
		}

		return instance.HealthCheck()
	}

	return nil
}

// isShutdowner reads the published instance without s.mu, so explaining a
// service never waits for its build.
func (s *serviceLazy[T]) isShutdowner() bool {
	instance, built := s.builtInstance()
	if !built {
		return false
	}

	_, ok1 := any(instance).(ShutdownerWithContextAndError)
	_, ok2 := any(instance).(ShutdownerWithError)
	_, ok3 := any(instance).(ShutdownerWithContext)
	_, ok4 := any(instance).(Shutdowner)
	return ok1 || ok2 || ok3 || ok4
}

func (s *serviceLazy[T]) shutdown(ctx context.Context) error {
	s.mu.Lock()
	// a build in flight completes first, as when the build held s.mu
	for run := s.state.inFlight(); run != nil; run = s.state.inFlight() {
		s.mu.Unlock()
		<-run.done
		s.mu.Lock()
	}

	defer func() {
		// whatever the outcome, reset `build` flag and instance, for good
		s.terminal = true
		s.built = false
		s.instance = empty[T]()
		s.published.Store(nil)
		s.mu.Unlock()
	}()

	if !s.built {
		return nil
	}

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

func (s *serviceLazy[T]) clone(newScope Injector) any {
	// reset `build` flag and instance
	return &serviceLazy[T]{
		mu:       sync.RWMutex{},
		name:     s.name,
		regIndex: s.regIndex,
		opts:     s.opts,
		typeName: s.typeName,

		built:    false,
		provider: s.provider,

		providerFrame:    s.providerFrame,
		invocationFrames: newInvocationFrames(),
	}
}

func (s *serviceLazy[T]) source() (stacktrace.Frame, []stacktrace.Frame) {
	return s.providerFrame, s.invocationFrameList()
}

// getBuildTime reads the build time and the published instance without
// s.mu, so explaining a service never waits for, nor races with, its build or
// shutdown.
//
// The instance is loaded first: build stores the build time before it
// publishes the instance, so a published instance seen here always comes
// with its build time, never (0, true).
func (s *serviceLazy[T]) getBuildTime() (time.Duration, bool) {
	built := s.published.Load() != nil
	return time.Duration(s.buildTime.Load()), built
}

func (s *serviceLazy[T]) getRegistrationIndex() uint64 {
	return s.regIndex
}

func (s *serviceLazy[T]) setRegistrationIndex(index uint64) {
	s.regIndex = index
}

func (s *serviceLazy[T]) getOptions() *serviceOptions {
	return s.opts
}

func (s *serviceLazy[T]) setOptions(opts *serviceOptions) {
	s.opts = opts
}
