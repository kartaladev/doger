package doger

import (
	"context"
	"log/slog"
	"slices"
	"sync/atomic"
	"time"
)

// Injector is the main interface for dependency injection containers.
// It provides methods for service registration, resolution, lifecycle management,
// and scope hierarchy management.
//
// The Injector interface is implemented by both RootScope and Scope types,
// allowing for a consistent API across different levels of the scope hierarchy.
type Injector interface {
	// Public API methods for scope and service management

	// ID returns the unique identifier of the injector.
	ID() string

	// Name returns the human-readable name of the injector.
	Name() string

	// Scope creates a new child scope with the given name.
	// Optional package functions can be provided to execute during scope creation.
	Scope(string, ...func(Injector)) *Scope

	// RootScope returns the root scope of the injector hierarchy.
	RootScope() *RootScope

	// Ancestors returns the list of all parent scopes in order from immediate parent to root.
	Ancestors() []*Scope

	// Children returns the list of immediate child scopes.
	Children() []*Scope

	// ChildByID searches for a child scope by its unique ID across the entire scope hierarchy.
	ChildByID(string) (*Scope, bool)

	// ChildByName searches for a child scope by its name across the entire scope hierarchy.
	ChildByName(string) (*Scope, bool)

	// ListProvidedServices returns all services available in the current scope and all its ancestor scopes.
	ListProvidedServices() []ServiceDescription

	// ListInvokedServices returns only the services that have been actually invoked in the current scope and its ancestors.
	ListInvokedServices() []ServiceDescription

	// HealthCheck performs health checks on all services in the current scope and its ancestors.
	HealthCheck() map[string]error

	// HealthCheckWithContext performs health checks with context support for cancellation and timeouts.
	HealthCheckWithContext(context.Context) map[string]error

	// HealthReport returns the health of the services the injector covers,
	// reusing recorded results (see [Scope.HealthReport]).
	HealthReport(opts ...HealthReportOption) HealthReport

	// HealthReportWithContext is HealthReport bounded by ctx.
	HealthReportWithContext(ctx context.Context, opts ...HealthReportOption) HealthReport

	// Shutdown gracefully shuts down the injector and all its descendant scopes.
	Shutdown() *ShutdownReport

	// ShutdownWithContext gracefully shuts down the injector and all its descendant scopes with context support.
	ShutdownWithContext(context.Context) *ShutdownReport

	// clone creates a deep copy of the injector with all its services and child scopes.
	clone(*RootScope, *Scope) *Scope

	// Internal service lifecycle methods

	// serviceExist checks if a service with the given name exists in the current scope.
	serviceExist(string) bool
	// serviceExistRec checks if a service with the given name exists in the current scope or any of its ancestor scopes.
	serviceExistRec(string) bool
	// serviceGet retrieves a service from the current scope by name.
	serviceGet(string) (any, bool)
	// serviceGetRec retrieves a service by name from the current scope or any of its ancestor scopes.
	serviceGetRec(string) (any, *Scope, bool)
	// serviceSet registers a service in the current scope with the given name.
	// Note: This method is not protected against double registration.
	serviceSet(string, any)
	// serviceForEach iterates over all services in the current scope.
	serviceForEach(func(string, *Scope, any) bool)
	// serviceForEachRec iterates over all services in the current scope and all ancestor scopes.
	serviceForEachRec(func(string, *Scope, any) bool)
	// serviceHealthCheck performs a health check on a specific service in the current scope.
	serviceHealthCheck(context.Context, string) error
	// shutdownService gracefully shuts down a specific service in the current scope.
	shutdownService(context.Context, string) error
	// onServiceInvoke is called whenever a service is invoked in this scope.
	onServiceInvoke(string)
}

// getInjector returns i, or ErrNilInjector when i is nil or a typed-nil
// *RootScope or *Scope.
func getInjector(i Injector) (Injector, error) {
	switch injector := i.(type) {
	case nil:
		return nil, ErrNilInjector
	case *RootScope:
		if injector == nil {
			return nil, ErrNilInjector
		}
	case *Scope:
		if injector == nil {
			return nil, ErrNilInjector
		}
	}

	return i, nil
}

// mustInjector returns i, or panics with ErrNilInjector when i is nil or a
// typed-nil *RootScope or *Scope, for the entry points without an error
// result.
func mustInjector(i Injector) Injector {
	injector, err := getInjector(i)
	if err != nil {
		panic(err)
	}

	return injector
}

// InjectorOpts contains all configuration options for the dependency injection container.
// These options control logging, hooks, health checks, and other behavioral aspects
// of the DI container.
//
// A container keeps its own copy of the options it was created with. Hook
// fields are for configuration time; a plugin that adds hooks to a running
// container uses the matching RootScope.AddXxxHook method, which is safe to
// call at any time and applies from the next operation on.
type InjectorOpts struct {
	// HookBeforeRegistration is called before a service is registered in a scope.
	// This hook can be used for validation, logging, or other pre-registration tasks.
	//
	// The container copies this list when it is created: set it at
	// configuration time. [RootScope.AddBeforeRegistrationHook] adds a hook later, safely
	// at any time, from the next operation on.
	HookBeforeRegistration []func(scope *Scope, serviceName string)

	// HookAfterRegistration is called after a service is successfully registered in a scope.
	// This hook can be used for logging, metrics collection, or other post-registration tasks.
	//
	// The container copies this list when it is created: set it at
	// configuration time. [RootScope.AddAfterRegistrationHook] adds a hook later, safely
	// at any time, from the next operation on.
	HookAfterRegistration []func(scope *Scope, serviceName string)

	// HookBeforeInvocation is called before a service is invoked (instantiated).
	// This hook can be used for logging, metrics, or other pre-invocation tasks.
	//
	// The container copies this list when it is created: set it at
	// configuration time. [RootScope.AddBeforeInvocationHook] adds a hook later, safely
	// at any time, from the next operation on.
	HookBeforeInvocation []func(scope *Scope, serviceName string)

	// HookAfterInvocation is called after a service is invoked, with the result error.
	// This hook can be used for logging, metrics collection, or error handling.
	//
	// The container copies this list when it is created: set it at
	// configuration time. [RootScope.AddAfterInvocationHook] adds a hook later, safely
	// at any time, from the next operation on.
	HookAfterInvocation []func(scope *Scope, serviceName string, err error)

	// HookAfterBuild is called with each instance the container builds:
	// after every successful provider call (each transient instance
	// included) and after an eager value's first successful invocation. It
	// is not called for a failed build, a cached lazy instance or an alias,
	// and runs between the before- and after-invocation hooks. A panic
	// propagates to the caller, as for the other per-service hooks. It runs
	// after the service released its lock, so it may invoke, health-check or
	// explain that service; another goroutine may obtain the instance before
	// the hook returned.
	//
	// The container copies this list when it is created: set it at
	// configuration time. [RootScope.AddAfterBuildHook] adds a hook later, safely
	// at any time, from the next operation on.
	HookAfterBuild []func(scope *Scope, serviceName string, instance any)

	// HookBeforeShutdown is called before a service is shut down.
	// This hook can be used for cleanup preparation or logging.
	//
	// The container copies this list when it is created: set it at
	// configuration time. [RootScope.AddBeforeShutdownHook] adds a hook later, safely
	// at any time, from the next operation on.
	HookBeforeShutdown []func(scope *Scope, serviceName string)

	// HookAfterShutdown is called after a service is shut down, with the result error.
	// This hook can be used for logging, metrics collection, or error handling.
	//
	// The container copies this list when it is created: set it at
	// configuration time. [RootScope.AddAfterShutdownHook] adds a hook later, safely
	// at any time, from the next operation on.
	HookAfterShutdown []func(scope *Scope, serviceName string, err error)

	// HookBeforeStart is called by Start, Refresh and Scope.Start just before
	// a service's Start method. With StartParallelism > 1, calls for
	// different services may run concurrently.
	//
	// The container copies this list when it is created: set it at
	// configuration time. [RootScope.AddBeforeStartHook] adds a hook later, safely
	// at any time, from the next operation on.
	HookBeforeStart []func(scope *Scope, serviceName string)

	// HookAfterStart is called by Start, Refresh and Scope.Start just after a
	// service's Start method, with the error the method returned, unwrapped
	// (nil on success; a recovered panic as an error). With StartParallelism
	// > 1, calls for different services may run concurrently.
	//
	// The container copies this list when it is created: set it at
	// configuration time. [RootScope.AddAfterStartHook] adds a hook later, safely
	// at any time, from the next operation on.
	HookAfterStart []func(scope *Scope, serviceName string, err error)

	// HookContainerStarting is called by the root container's first Start,
	// after its one-shot guard and before its context check, discovery or any
	// instantiation, with the Start-time context. A non-nil error vetoes
	// Start: the remaining Starting hooks do not run, nothing is built or
	// started, and Start returns an error wrapping it. A panic is a veto too:
	// Start returns an error containing the panic value (wrapping it when it
	// is an error), and HookContainerPanic is not called.
	//
	// A Starting hook must not call root Shutdown synchronously without a
	// deadline: Shutdown waits for the running Start, which waits for the hook.
	// With a deadline, Shutdown returns a failed report when it expires, and
	// Start then fails with an error matching ErrShutdown.
	//
	// The container copies this list when it is created: set it at
	// configuration time. [RootScope.AddContainerStartingHook] adds a hook later, safely
	// at any time, from the next operation on.
	HookContainerStarting []func(ctx context.Context, scope *RootScope) error

	// HookContainerStarted is called by the first Start once its pipeline
	// succeeded: after every Starter and every StageAfterStart handler,
	// including the Runnable handler. It receives the context passed to
	// Start. Use it for last wiring, such as route registration. It runs
	// after the running Start was released, so it may call root Shutdown.
	//
	// The container copies this list when it is created: set it at
	// configuration time. [RootScope.AddContainerStartedHook] adds a hook later, safely
	// at any time, from the next operation on.
	HookContainerStarted []func(ctx context.Context, scope *RootScope)

	// HookContainerReady is called by the first Start after every Started
	// hook has returned, with the context passed to Start. Use it to signal
	// readiness to an orchestrator. It may call root Shutdown.
	//
	// The container copies this list when it is created: set it at
	// configuration time. [RootScope.AddContainerReadyHook] adds a hook later, safely
	// at any time, from the next operation on.
	HookContainerReady []func(ctx context.Context, scope *RootScope)

	// HookContainerFailed is called by the first Start when it fails after
	// its one-shot guard, with the context passed to Start and the exact
	// error Start returns. It may call root Shutdown, for example to clean up
	// what did start. When a per-service hook panics during Start (such as
	// HookBeforeStart or HookAfterBuild), the Failed hooks run with an error
	// wrapping the panic value before Start re-raises it; a Failed hook panic
	// then goes to HookContainerPanic when set, and is logged otherwise.
	//
	// The container copies this list when it is created: set it at
	// configuration time. [RootScope.AddContainerFailedHook] adds a hook later, safely
	// at any time, from the next operation on.
	HookContainerFailed []func(ctx context.Context, scope *RootScope, err error)

	// HookContainerClosing is called by the root Shutdown that begins the
	// close phase (the first one), before any service is shut down, with the
	// Shutdown context. It fires whether or not Start ran or succeeded.
	//
	// A Closing hook must not call root Shutdown synchronously without a
	// deadline: that call waits for the running Shutdown, which waits for the
	// hook. With a deadline, it returns a failed report when it expires.
	//
	// Ordering with Start: a Shutdown that begins while Start runs cancels
	// it and fires Closing only after Start's pipeline returned, or after the
	// Shutdown context ended. Once the pipeline has returned, Closing and
	// Closed may run before or while the Started, Ready or Failed hooks run on
	// the Start goroutine: hooks sharing state across these points must
	// synchronize it.
	//
	// The container copies this list when it is created: set it at
	// configuration time. [RootScope.AddContainerClosingHook] adds a hook later, safely
	// at any time, from the next operation on.
	HookContainerClosing []func(ctx context.Context, scope *RootScope)

	// HookContainerClosed is called by the first root Shutdown once every
	// service is shut down and the close phase is closed, with the Shutdown
	// context and the report Shutdown returns. Mutating the report is
	// unsupported. Concurrent Shutdown callers may return before the Closed
	// hooks have run. A Closed hook may call root Shutdown, which returns a
	// copy of the first report.
	//
	// The container copies this list when it is created: set it at
	// configuration time. [RootScope.AddContainerClosedHook] adds a hook later, safely
	// at any time, from the next operation on.
	HookContainerClosed []func(ctx context.Context, scope *RootScope, report *ShutdownReport)

	// HookScopeStarting is called by Scope.Start on a child scope, after its
	// phase check and before anything starts, with the scope. A non-nil error
	// or a panic vetoes Scope.Start, which returns an error wrapping it. It is
	// not called when the scope, or one of its ancestors, is shutting down:
	// Scope.Start then starts nothing and returns nil. The root's own scope
	// never fires scope hooks.
	//
	// The container copies this list when it is created: set it at
	// configuration time. [RootScope.AddScopeStartingHook] adds a hook later, safely
	// at any time, from the next operation on.
	HookScopeStarting []func(ctx context.Context, scope *Scope) error

	// HookScopeStarted is called by Scope.Start on a child scope once its
	// services started successfully. It is not called when the scope, or one
	// of its ancestors, was shutting down, so that Scope.Start started
	// nothing. A shutdown of the scope that begins after Scope.Start collected
	// its services waits for those services to start, then runs its Closing
	// hooks, so they may run during or before the Started hooks. A panic is
	// recovered and dispatched like a container observer hook panic, with the
	// point "scope-started" (see HookContainerPanic).
	//
	// The container copies this list when it is created: set it at
	// configuration time. [RootScope.AddScopeStartedHook] adds a hook later, safely
	// at any time, from the next operation on.
	HookScopeStarted []func(ctx context.Context, scope *Scope)

	// HookScopeClosing is called when a child scope shuts down, directly or
	// as part of its parent's shutdown, after its children shut down and
	// before its own services. Sibling scopes shut down in parallel, so their
	// hooks may run concurrently. A panic is dispatched with the point
	// "scope-closing" (see HookContainerPanic).
	//
	// A child that exports services into its parent (see Export) is the
	// exception: its services shut down in dependency order with the
	// parent's, so its Closing and Closed hooks run inside its parent's
	// Closing/Closed pair, Closing just before its first service shuts down.
	//
	// The container copies this list when it is created: set it at
	// configuration time. [RootScope.AddScopeClosingHook] adds a hook later, safely
	// at any time, from the next operation on.
	HookScopeClosing []func(ctx context.Context, scope *Scope)

	// HookScopeClosed is called when a child scope shut down, after its
	// services. A panic is dispatched with the point "scope-closed" (see
	// HookContainerPanic).
	//
	// The container copies this list when it is created: set it at
	// configuration time. [RootScope.AddScopeClosedHook] adds a hook later, safely
	// at any time, from the next operation on.
	HookScopeClosed []func(ctx context.Context, scope *Scope)

	// HookContainerPanic receives the first panic recovered from a Started,
	// Ready, Failed, Closing or Closed hook, once the operation is complete:
	// every hook of the list has run, and so has the rest of Start or
	// Shutdown. hook names the point ("started", "ready", "failed",
	// "closing" or "closed"); later panics of the same operation are logged
	// to Logger (see Logger). Start or Shutdown then returns its normal result. Scope
	// observer hooks follow the same policy for Scope.Start and a child
	// scope shutdown, with the points "scope-started", "scope-closing" and
	// "scope-closed". Default (nil): re-panic with the recovered value.
	// Panics in Starting hooks are vetoes and never reach it.
	//
	// A panic of a service Shutdown method or of a HookBeforeShutdown or
	// HookAfterShutdown hook is recorded in the shutdown report (see
	// ErrShutdownPanic) and never raised by the container: the first one of
	// a root or child shutdown reaches HookContainerPanic with the point
	// "service-shutdown" once that shutdown is complete, Closed hooks
	// included, and later ones are logged. With no HookContainerPanic, each
	// is only recorded and logged. Other points: "healthscheduler" and
	// "healthchanged" (see HealthScheduler), and "healthcheck".
	HookContainerPanic func(scope *RootScope, hook string, recovered any)

	// Logger receives the container's log records, each with a fixed
	// message and level and the attributes "scope" and "service" where they
	// apply:
	//   - Debug: registrations, overrides, invocations, exports, a health
	//     check requested or failed, the property source selected, and the
	//     value of a recovered panic ("panic value");
	//   - Info: container started, ready, closing and closed, an extension
	//     disabled, and the container health back to healthy;
	//   - Warn: an unavailable property source, properties read after
	//     Extend, and the container health changing to another state;
	//   - Error: a failed service shutdown, a health scheduler failure or a
	//     failure of its stop function, and a recovered panic that is not
	//     passed to HookContainerPanic ("panic recovered", with its point and
	//     type only): the first panic of an operation goes to
	//     HookContainerPanic when it is set, and is logged only when it is
	//     not; later panics of the operation are logged.
	// A recovered panic's value, and a health check's error text, appear
	// only in Debug records, since they could hold secrets.
	//
	// The Logger's handler must not panic. A panic it raises on the
	// caller's goroutine propagates to the caller; one raised on a
	// goroutine the container runs in the background, such as a service
	// shutdown or a health check, cannot be recovered by the caller and
	// crashes the process.
	// Default (nil): nothing is logged, and no record is built.
	Logger *slog.Logger

	// HealthCheckParallelism caps the health checks running at once among
	// those started by HealthCheck(), HealthReport reads and scheduled
	// rounds: each takes a slot while it runs. Waiting for a slot holds no
	// lock and counts against each caller's context and
	// HealthCheckGlobalTimeout, while a caller's per-service bound starts
	// once the check has its slot; only a caller that waited behind an
	// abandoned check, still running, keeps the bound it started at its
	// call. A check holds its slot until it returns, its context ends, or
	// the report bound (see DefaultHealthReportCheckTimeout) has passed since
	// it took the slot; a check that keeps running after that, ignoring its
	// context, no longer counts against the limit. The
	// single-service functions (HealthCheck[T], HealthCheckWithContext[T],
	// HealthCheckNamed and HealthCheckNamedWithContext) take no slot: they
	// run at once, as does a check waiting for a slot that one of them needs,
	// so composite checks complete under any limit.
	// Default: 0, all health checks run in parallel (unlimited).
	HealthCheckParallelism uint

	// HealthCheckGlobalTimeout sets a global timeout for all health check operations.
	// This timeout applies to the entire health check process across all
	// services of HealthCheck(); the single-service functions ignore it.
	// Default: 0, and any value of 0 or less means no global timeout.
	HealthCheckGlobalTimeout time.Duration

	// HealthCheckTimeout sets a timeout for individual service health checks,
	// measured on Clock. It applies to each service's health check method in
	// HealthCheck(), HealthReport reads and scheduled rounds and, unless
	// HealthCheckSingleTimeout is set, in the single-service HealthCheck[T],
	// HealthCheckWithContext[T], HealthCheckNamed and
	// HealthCheckNamedWithContext functions.
	// Default: 0, which means DefaultHealthReportCheckTimeout (10s). Set a
	// negative value for unbounded checks.
	HealthCheckTimeout time.Duration

	// HealthCheckSingleTimeout bounds the single-service HealthCheck[T],
	// HealthCheckWithContext[T], HealthCheckNamed and
	// HealthCheckNamedWithContext functions in place of HealthCheckTimeout:
	// a positive value is their bound and a negative value means no bound.
	// Checks run by HealthCheck() keep HealthCheckTimeout.
	// Default: 0, which keeps HealthCheckTimeout for them too, so they are
	// bounded at 10s unless HealthCheckTimeout says otherwise.
	HealthCheckSingleTimeout time.Duration

	// HealthReportCacheTTL is how long a recorded health check result is
	// reused by a HealthReport read before the read checks the service again.
	// A positive value is the TTL, a negative value turns caching off (every
	// read checks again), and 0 means DefaultHealthReportCacheTTL.
	// Concurrent reads share each check in flight either way.
	HealthReportCacheTTL time.Duration

	// HealthScheduler keeps HealthReport results fresh with scheduled
	// rounds. Nil (default): no round is ever scheduled and the container
	// starts no goroutine for health; reads check stale services on demand.
	// The container calls it once, when it reaches PhaseReady, and stops it
	// when root Shutdown begins (see HealthScheduler).
	HealthScheduler HealthScheduler

	// HookServiceHealthChanged is called when a service's recorded health
	// state changes, including from HealthUnknown to its first result,
	// whichever path recorded it (a report read, a round, HealthCheck() or a
	// single-service function); a new error with the same state calls
	// nothing. HookContainerHealthChanged is called when the overall state
	// of the container's unfiltered report, without the phase gate, changes,
	// after the service hooks of the same result, including when a service
	// appears or leaves. A service leaving (its scope shut down, or its
	// registration replaced) changes it at once. A service appearing changes
	// it when the next round or root HealthReport lists it, counting as
	// unknown until its first result, or when its first result is recorded;
	// registering or building a service never does.
	//
	// Delivery: hooks run one at a time, in recording order and in the
	// order they were added, with no lock held, on the goroutine that
	// recorded the result or that is already delivering, so they get no ctx:
	// no caller's request is there to pass one. A result reaches its callers
	// without waiting for hooks. Hooks may call the health API, including
	// HealthCheck and HealthReport, but should be fast: they delay the next
	// deliveries. A nil hook is skipped. A panic is recovered and passed to
	// HookContainerPanic with the point "healthchanged", or logged; the other
	// hooks still run. Nothing is called once root Shutdown began, nor once
	// the root's own *Scope (the Injector a provider receives in the root
	// scope) was shut down: that ends health recording and these hooks for
	// good, although the container phase stays unchanged.
	HookServiceHealthChanged   []func(scope *Scope, serviceName string, previous, current ServiceHealth)
	HookContainerHealthChanged []func(root *RootScope, previous, current HealthState)

	// MaxInvocationFrames caps the distinct call sites (invocation frames)
	// each service of the container records, for debugging: a service
	// records the call site of each of its first MaxInvocationFrames
	// invocations. A positive value is the cap, a negative value turns
	// recording off, and 0 means 100. Child scopes use the root's value.
	MaxInvocationFrames int

	// StructTagKey specifies the tag key used for struct field injection.
	// Default: "do" (see DefaultStructTagKey constant).
	// This allows customization of the struct tag format for injection.
	StructTagKey string

	// Clock is the time source of the container. It measures
	// HealthCheckTimeout, HealthCheckSingleTimeout and HealthCheckGlobalTimeout,
	// HealthReportCacheTTL and the report bound, the CheckedAt, Duration and
	// PendingSince of HealthReport entries, lazy service build times, and
	// shutdown durations, and it is passed to HealthScheduler. Child scopes
	// and Clone use the root scope's clock.
	// Default: nil, which means RealClock().
	// Set a controllable Clock to drive time in tests, such as
	// dogerclockwork.Wrap(clockwork.NewFakeClock()) from the
	// github.com/kartaladev/doger/clock/clockwork module, or any type with
	// the Clock methods.
	Clock Clock

	// DisableStarterAutoDetection makes Start start only the services
	// registered with AsStarter. By default, every non-transient service whose
	// registered type implements a Starter variant is started too.
	DisableStarterAutoDetection bool

	// StartParallelism is the number of Starters that Start, Refresh and
	// Scope.Start may run concurrently, each only after every Starter it
	// depends on has started. The first failure stops scheduling new
	// Starters; running ones are awaited, then the failure is returned.
	// Default: 0 or 1, sequential. With n > 1, HookBeforeStart and
	// HookAfterStart may run concurrently and must synchronize their state.
	// Kind handlers always run sequentially.
	StartParallelism uint

	// EnableRunnerAutoDetection makes Start run, as a Runnable, every
	// non-transient service whose registered type implements Runner. By
	// default, runners must opt in with AsRunnable, because Run is too
	// common a method name to auto-detect. Custom kinds always need opt-in.
	EnableRunnerAutoDetection bool

	// PropertySource is where extensions read their configuration (see
	// PropertySource). Nil means unset: the container then uses a
	// PropertySource service registered in the root under
	// NameOf[PropertySource](), or else EnvPropertySource("DOGER_"), decided
	// at its first property read. If deciding panics (a registration hook,
	// for example), the panic reaches the reader and the next read decides
	// again. If that service fails to resolve, every property is unset and
	// one line naming the error's type, never its message, is logged.
	// Setting this field avoids any ordering constraint with registrations,
	// and any provider failure. A clone uses the source of its own options.
	PropertySource PropertySource
}

// clone returns a copy of o that shares no hook slice with it, so neither
// side's appends reach the other.
func (o *InjectorOpts) clone() *InjectorOpts {
	c := *o
	c.HookBeforeRegistration = cloneHooks(o.HookBeforeRegistration)
	c.HookAfterRegistration = cloneHooks(o.HookAfterRegistration)
	c.HookBeforeInvocation = cloneHooks(o.HookBeforeInvocation)
	c.HookAfterInvocation = cloneHooks(o.HookAfterInvocation)
	c.HookAfterBuild = cloneHooks(o.HookAfterBuild)
	c.HookBeforeShutdown = cloneHooks(o.HookBeforeShutdown)
	c.HookAfterShutdown = cloneHooks(o.HookAfterShutdown)
	c.HookBeforeStart = cloneHooks(o.HookBeforeStart)
	c.HookAfterStart = cloneHooks(o.HookAfterStart)
	c.HookContainerStarting = cloneHooks(o.HookContainerStarting)
	c.HookContainerStarted = cloneHooks(o.HookContainerStarted)
	c.HookContainerReady = cloneHooks(o.HookContainerReady)
	c.HookContainerFailed = cloneHooks(o.HookContainerFailed)
	c.HookContainerClosing = cloneHooks(o.HookContainerClosing)
	c.HookContainerClosed = cloneHooks(o.HookContainerClosed)
	c.HookScopeStarting = cloneHooks(o.HookScopeStarting)
	c.HookScopeStarted = cloneHooks(o.HookScopeStarted)
	c.HookScopeClosing = cloneHooks(o.HookScopeClosing)
	c.HookScopeClosed = cloneHooks(o.HookScopeClosed)
	c.HookServiceHealthChanged = cloneHooks(o.HookServiceHealthChanged)
	c.HookContainerHealthChanged = cloneHooks(o.HookContainerHealthChanged)

	return &c
}

// cloneHooks returns a new slice holding the hooks of s, empty but not nil
// when s is.
func cloneHooks[T any](s []T) []T {
	return append([]T{}, s...)
}

// initDefaults replaces unset options with their defaults: a discard
// logger and the real clock.
func (o *InjectorOpts) initDefaults() {
	if o.Logger == nil {
		o.Logger = discardLogger
	}

	if o.Clock == nil {
		o.Clock = RealClock()
	}
}

func (h *containerHooks) onBeforeRegistration(scope *Scope, serviceName string) {
	for _, fn := range h.beforeRegistration.load() {
		fn(scope, serviceName)
	}
}

func (h *containerHooks) onAfterRegistration(scope *Scope, serviceName string) {
	for _, fn := range h.afterRegistration.load() {
		fn(scope, serviceName)
	}
}

func (h *containerHooks) onBeforeInvocation(scope *Scope, serviceName string) {
	for _, fn := range h.beforeInvocation.load() {
		fn(scope, serviceName)
	}
}

func (h *containerHooks) onAfterInvocation(scope *Scope, serviceName string, err error) {
	for _, fn := range h.afterInvocation.load() {
		fn(scope, serviceName, err)
	}
}

func (h *containerHooks) onAfterBuild(scope *Scope, serviceName string, instance any) {
	for _, fn := range h.afterBuild.load() {
		if fn != nil {
			fn(scope, serviceName, instance)
		}
	}
}

func (h *containerHooks) onBeforeShutdown(scope *Scope, serviceName string) {
	for _, fn := range h.beforeShutdown.load() {
		fn(scope, serviceName)
	}
}

func (h *containerHooks) onAfterShutdown(scope *Scope, serviceName string, err error) {
	for _, fn := range h.afterShutdown.load() {
		fn(scope, serviceName, err)
	}
}

func (h *containerHooks) onBeforeStart(scope *Scope, serviceName string) {
	for _, fn := range h.beforeStart.load() {
		fn(scope, serviceName)
	}
}

func (h *containerHooks) onAfterStart(scope *Scope, serviceName string, err error) {
	for _, fn := range h.afterStart.load() {
		fn(scope, serviceName, err)
	}
}

// containerHooks holds the hook lists of a container. Each list is
// copy-on-write: an adder stores a new slice, so a reader that loaded a list
// once per operation runs it unchanged while hooks are added.
type containerHooks struct {
	beforeRegistration hookList[func(*Scope, string)]
	afterRegistration  hookList[func(*Scope, string)]
	beforeInvocation   hookList[func(*Scope, string)]
	afterInvocation    hookList[func(*Scope, string, error)]
	afterBuild         hookList[func(*Scope, string, any)]
	beforeShutdown     hookList[func(*Scope, string)]
	afterShutdown      hookList[func(*Scope, string, error)]
	beforeStart        hookList[func(*Scope, string)]
	afterStart         hookList[func(*Scope, string, error)]
	containerStarting  hookList[func(context.Context, *RootScope) error]
	containerStarted   hookList[func(context.Context, *RootScope)]
	containerReady     hookList[func(context.Context, *RootScope)]
	containerFailed    hookList[func(context.Context, *RootScope, error)]
	containerClosing   hookList[func(context.Context, *RootScope)]
	containerClosed    hookList[func(context.Context, *RootScope, *ShutdownReport)]
	scopeStarting      hookList[func(context.Context, *Scope) error]
	scopeStarted       hookList[func(context.Context, *Scope)]
	scopeClosing       hookList[func(context.Context, *Scope)]
	scopeClosed        hookList[func(context.Context, *Scope)]

	serviceHealthChanged   hookList[func(*Scope, string, ServiceHealth, ServiceHealth)]
	containerHealthChanged hookList[func(*RootScope, HealthState, HealthState)]
}

// take moves the hook lists of opts, the container's own copy, into h.
func (h *containerHooks) take(opts *InjectorOpts) {
	h.beforeRegistration.set(opts.HookBeforeRegistration)
	opts.HookBeforeRegistration = nil
	h.afterRegistration.set(opts.HookAfterRegistration)
	opts.HookAfterRegistration = nil
	h.beforeInvocation.set(opts.HookBeforeInvocation)
	opts.HookBeforeInvocation = nil
	h.afterInvocation.set(opts.HookAfterInvocation)
	opts.HookAfterInvocation = nil
	h.afterBuild.set(opts.HookAfterBuild)
	opts.HookAfterBuild = nil
	h.beforeShutdown.set(opts.HookBeforeShutdown)
	opts.HookBeforeShutdown = nil
	h.afterShutdown.set(opts.HookAfterShutdown)
	opts.HookAfterShutdown = nil
	h.beforeStart.set(opts.HookBeforeStart)
	opts.HookBeforeStart = nil
	h.afterStart.set(opts.HookAfterStart)
	opts.HookAfterStart = nil
	h.containerStarting.set(opts.HookContainerStarting)
	opts.HookContainerStarting = nil
	h.containerStarted.set(opts.HookContainerStarted)
	opts.HookContainerStarted = nil
	h.containerReady.set(opts.HookContainerReady)
	opts.HookContainerReady = nil
	h.containerFailed.set(opts.HookContainerFailed)
	opts.HookContainerFailed = nil
	h.containerClosing.set(opts.HookContainerClosing)
	opts.HookContainerClosing = nil
	h.containerClosed.set(opts.HookContainerClosed)
	opts.HookContainerClosed = nil
	h.scopeStarting.set(opts.HookScopeStarting)
	opts.HookScopeStarting = nil
	h.scopeStarted.set(opts.HookScopeStarted)
	opts.HookScopeStarted = nil
	h.scopeClosing.set(opts.HookScopeClosing)
	opts.HookScopeClosing = nil
	h.scopeClosed.set(opts.HookScopeClosed)
	opts.HookScopeClosed = nil
	h.serviceHealthChanged.set(opts.HookServiceHealthChanged)
	opts.HookServiceHealthChanged = nil
	h.containerHealthChanged.set(opts.HookContainerHealthChanged)
	opts.HookContainerHealthChanged = nil
}

// optsWith returns a copy of opts holding the current hook lists of h.
func (h *containerHooks) optsWith(opts *InjectorOpts) *InjectorOpts {
	c := opts.clone()
	c.HookBeforeRegistration = slices.Clone(h.beforeRegistration.load())
	c.HookAfterRegistration = slices.Clone(h.afterRegistration.load())
	c.HookBeforeInvocation = slices.Clone(h.beforeInvocation.load())
	c.HookAfterInvocation = slices.Clone(h.afterInvocation.load())
	c.HookAfterBuild = slices.Clone(h.afterBuild.load())
	c.HookBeforeShutdown = slices.Clone(h.beforeShutdown.load())
	c.HookAfterShutdown = slices.Clone(h.afterShutdown.load())
	c.HookBeforeStart = slices.Clone(h.beforeStart.load())
	c.HookAfterStart = slices.Clone(h.afterStart.load())
	c.HookContainerStarting = slices.Clone(h.containerStarting.load())
	c.HookContainerStarted = slices.Clone(h.containerStarted.load())
	c.HookContainerReady = slices.Clone(h.containerReady.load())
	c.HookContainerFailed = slices.Clone(h.containerFailed.load())
	c.HookContainerClosing = slices.Clone(h.containerClosing.load())
	c.HookContainerClosed = slices.Clone(h.containerClosed.load())
	c.HookScopeStarting = slices.Clone(h.scopeStarting.load())
	c.HookScopeStarted = slices.Clone(h.scopeStarted.load())
	c.HookScopeClosing = slices.Clone(h.scopeClosing.load())
	c.HookScopeClosed = slices.Clone(h.scopeClosed.load())
	c.HookServiceHealthChanged = slices.Clone(h.serviceHealthChanged.load())
	c.HookContainerHealthChanged = slices.Clone(h.containerHealthChanged.load())

	return c
}

// hookList is a copy-on-write list of hooks, safe for concurrent use. The
// zero value is an empty list. A slice returned by load is never written.
type hookList[F any] struct {
	p atomic.Pointer[[]F]
}

// load returns the hooks of the list. A reader loads a list once per
// operation and runs that slice unchanged while hooks are added.
func (l *hookList[F]) load() []F {
	if hooks := l.p.Load(); hooks != nil {
		return *hooks
	}

	return nil
}

// set replaces the hooks of the list with a copy of hooks.
func (l *hookList[F]) set(hooks []F) {
	hooks = slices.Clone(hooks)
	l.p.Store(&hooks)
}

// add appends hook to the list.
func (l *hookList[F]) add(hook F) {
	for {
		old := l.p.Load()
		var next []F
		if old != nil {
			next = slices.Clone(*old)
		}
		next = append(next, hook)
		if l.p.CompareAndSwap(old, &next) {
			return
		}
	}
}
