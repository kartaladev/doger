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

	// declared dependencies, resolved on the first successful invocation;
	// depsMu is never held while they resolve (D1)
	depsMu       sync.Mutex
	depsResolved bool
	depsState    buildState

	built    atomic.Bool // the after-build hook ran
	terminal atomic.Bool // shut down: invocations get ErrShutdown

	// shutdownOnce is shared with the clones of this wrapper, which share
	// the value: its Shutdown runs once across them
	shutdownOnce *sharedShutdown
}

func newServiceEager[T any](name string, instance T) *serviceEager[T] {
	providerFrame := registrationFrame()

	return &serviceEager[T]{
		name:     name,
		typeName: inferServiceName[T](),
		instance: instance,

		providerFrame:    providerFrame,
		invocationFrames: newInvocationFrames(),
		shutdownOnce:     newSharedShutdown(),
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

// builtInstanceAny returns the value, which an eager service always holds.
func (s *serviceEager[T]) builtInstanceAny() (any, bool) {
	return s.instance, true
}

func (s *serviceEager[T]) getInstance(i Injector) (T, error) {
	// a closure here, not stacktrace.NewFrameFromCaller itself: the frame
	// recorded is then in this file, as before (invocation-frame rule, D16)
	s.recordInvocationFrame(invocationFrameCap(i), func() (stacktrace.Frame, bool) { return stacktrace.NewFrameFromCaller() }) //nolint:gocritic // see above

	if s.terminal.Load() {
		return empty[T](), shutDownServiceError(s.name)
	}
	if err := s.resolveDependsOn(i); err != nil {
		return empty[T](), err
	}
	// its scope may have shut it down while the dependencies resolved
	if s.terminal.Load() {
		return empty[T](), shutDownServiceError(s.name)
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

	for {
		s.depsMu.Lock()
		if s.depsResolved {
			s.depsMu.Unlock()
			return nil
		}
		run, inFlight := s.depsState.claimLocked(s.name, rootOf(i))
		s.depsMu.Unlock()

		if inFlight {
			if err := waitForBuild(s.name, run); err != nil {
				return err
			}
			continue
		}

		return s.runDependsOn(i, run)
	}
}

// runDependsOn resolves the declared dependencies as run, then records the
// outcome and releases run's waiters, also when a hook panics or a provider
// exits its goroutine with runtime.Goexit.
func (s *serviceEager[T]) runDependsOn(i Injector, run *buildRun) (err error) {
	finished := false
	defer func() {
		if finished {
			return
		}
		r := recover()
		cause := libErrorMatching(ErrProviderPanic, "provider called runtime.Goexit")
		if r != nil {
			cause = buildPanicError(r)
		}
		s.endDependsOn(run, wrapBuildError(i, s.name, cause))
		if r != nil {
			panic(r)
		}
	}()

	if err = resolveDeclaredDependencies(i, s.opts); err != nil {
		err = wrapBuildError(i, s.name, err)
	}
	finished = true
	s.endDependsOn(run, err)

	return err
}

// endDependsOn records the outcome of resolving the declared dependencies.
func (s *serviceEager[T]) endDependsOn(run *buildRun, err error) {
	s.depsMu.Lock()
	if err == nil {
		s.depsResolved = true
	}
	s.depsState.endLocked(run, err)
	s.depsMu.Unlock()

	run.release()
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
	s.terminal.Store(true)

	if !s.isShutdowner() {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err // the shared value stays for another container to shut down
	}

	// the value is shared with clones: the first container to shut it down
	// calls Shutdown, the others only drop their registration
	return s.shutdownOnce.do(ctx, func() error { return s.shutdownInstance(ctx) })
}

// sharedShutdown runs the Shutdown of a value shared by containers once.
type sharedShutdown struct {
	started atomic.Bool
	done    chan struct{} // closed once the Shutdown returned
}

func newSharedShutdown() *sharedShutdown {
	return &sharedShutdown{done: make(chan struct{})}
}

// do calls shutdown on the first call, and returns its error. A later call
// waits for that Shutdown to return, bounded by its own ctx (D4), and then
// returns nil: only the container that ran it reports its error.
func (o *sharedShutdown) do(ctx context.Context, shutdown func() error) error {
	if o.started.CompareAndSwap(false, true) {
		defer close(o.done)
		return shutdown()
	}

	select {
	case <-o.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// shutdownInstance calls the Shutdown method of the value.
func (s *serviceEager[T]) shutdownInstance(ctx context.Context) error {
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
		shutdownOnce:     s.shutdownOnce,
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
