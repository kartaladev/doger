---
title: Container options
description: Configure a doger injector — default options, package initialization, custom hooks, and health check timeouts.
sidebar_position: 2
---

# Container options

The DI container in `doger` is called an injector (`doger.Injector`, implemented by `*doger.RootScope`). It can be created with sensible defaults via `doger.New()`, or tuned with `doger.NewWithOpts()` for hooks, logging, and health check behavior.

## Default options {#default-options}

The simplest way to start is to use the default parameters:

```go
import "github.com/kartaladev/doger"

injector := doger.New()
```

## No global container {#no-global-container}

doger has no package-level container: every call acts only on the container you pass. A nil injector, root or scope, including a typed-nil `*doger.RootScope` or `*doger.Scope`, is rejected. Functions with an error result return an error matching `doger.ErrNilInjector`, and the others panic with a value matching it.

```go
injector := doger.New()

doger.Provide(injector, ...)
doger.Invoke[*MyService](injector)
```

## Register services on container initialization {#register-services-on-container-initialization}

The services can be assembled into a package, and then, imported all at once into a new container.

```go
// pkg/stores/package.go

var Package = doger.Package(
    doger.Lazy(NewPostgreSQLConnectionService),
    doger.Lazy(NewUserRepository),
    doger.Lazy(NewArticleRepository),
    doger.EagerNamed("repository.logger", slog.New(slog.NewTextHandler(os.Stdout, nil))),
)
```

```go
// cmd/main.go

import "example/pkg/stores"

injector := doger.New(stores.Package)
```

## Custom options {#custom-options}

```go
import "github.com/kartaladev/doger"

injector := doger.NewWithOpts(&doger.InjectorOpts{
    HookBeforeRegistration: []func(scope *doger.Scope, serviceName string){},
    HookAfterRegistration:  []func(scope *doger.Scope, serviceName string){},
    HookBeforeInvocation:   []func(scope *doger.Scope, serviceName string){},
    HookAfterInvocation:    []func(scope *doger.Scope, serviceName string, elapsed time.Duration, err error){},
    HookAfterBuild:         []func(scope *doger.Scope, serviceName string, instance any){},
    HookBeforeShutdown:     []func(ctx context.Context, scope *doger.Scope, serviceName string){},
    HookAfterShutdown:      []func(ctx context.Context, scope *doger.Scope, serviceName string, elapsed time.Duration, err error){},
    HookBeforeStart:        []func(ctx context.Context, scope *doger.Scope, serviceName string){},
    HookAfterStart:         []func(ctx context.Context, scope *doger.Scope, serviceName string, elapsed time.Duration, err error){},

    Logger: slog.Default(), // nil: nothing is logged

    HealthCheckParallelism:   100,
    HealthCheckGlobalTimeout: 1 * time.Second,
    HealthCheckTimeout:       100 * time.Millisecond, // also bounds HealthCheck[T] and HealthCheckNamed
    HealthCheckSingleTimeout: 0,                      // 0: single-service checks keep HealthCheckTimeout

    Clock: nil, // nil: the real clock
    EnableStarterAutoDetection: false, // true: Start also starts every type with a Start method
    StartParallelism: 0, // 0 or 1: sequential; n > 1: up to n Starters at a time
    SlowStartThreshold: 0, // 0: 10s; negative: no warning. Logs a start step still running after it
    StartTimeout: 0,       // 0: wait for every step; > 0: abandon a step still running after it
})
```

### Logging {#logging}

`InjectorOpts.Logger` takes any `*slog.Logger`. When it is nil (the default) the container logs nothing, not even to `slog.Default()`, and builds no record. Each event has a fixed message and level, with the attributes `scope` (the scope name) and `service` where they apply:

| Level | Messages                                                                                                                                                                                                                                                                                                                                                                |
| ----- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Debug | `service registered`, `service overridden`, `service invoked`, `service exported`, `requested shutdown`, `health check started`, `health check failed` (with `error`), `property source selected`, `panic value` (with `value`), `abandoned start step error` (with `step`, `kind` for a handler call, `elapsed` and `error`) for an abandoned start step that later returned an error, `cancelled start step error` (with `step`, `kind` for a handler call, and `error`) for a start step that a child scope shutdown cancelled and that returned an error (one that panicked logs `panic recovered` and `panic value` with `step` and `kind`) |
| Info  | `container started`, `container ready`, `container closing`, `container closed`, `extension disabled`, `container health changed` back to healthy                                                                                                                                                                                                                       |
| Warn  | `property source unavailable` (with `source_type`), `properties read after extend`, `container health changed` to another state, `start step is slow` (with `step`, `kind` for a handler call, and `threshold`) for a Starter's `Start` method or a kind-handler call still running after `SlowStartThreshold`, `abandoned start step returned` (with `step`, `kind` for a handler call, and `elapsed`) when a step abandoned under `StartTimeout` later returns (one that panics logs `panic recovered` and `panic value` with `step` and `kind`) |
| Error | `service shutdown failed` (with `error`), `health scheduler failed` (with `error`), `health scheduler stop failed` (with `error`), `panic recovered` (with `point` and `panic_type`) for a recovered panic not passed to `HookContainerPanic`: the first panic of an operation goes to that hook when it is set (logged only when it is not), and later ones are logged |

A recovered panic's value and the error text of a health check or of a start step appear only at Debug level, so they stay out of production logs that could expose secrets.

`start step is slow` and the records of an abandoned step that ended are logged on the container's background goroutines (the warning timer, the abandoned call's own goroutine), so they can come after `Start` returned, after `Shutdown`, and even after the container closed: the call is still running user code then. They are logged to the same `Logger`.

The handler must not panic: a panic it raises on a goroutine the container runs in the background, such as a service shutdown or a health check, cannot be recovered by the caller and crashes the process. The start step records (`start step is slow`, and the records of an abandoned step that ended, including its `panic recovered` record with point `start`) are the exception: a handler panic while emitting them is dropped.

```go
logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
injector := doger.NewWithOpts(&doger.InjectorOpts{Logger: logger})
```

### Slow and stuck start steps {#start-timeout}

`Start`, `Refresh` and `Scope.Start` call each Starter's `Start` method and each kind handler (such as a `Runner`'s `Run`) and wait for it. A method that never returns, such as an HTTP server calling `ListenAndServe` in `Start`, makes the whole call block. Two options make such a step visible:

- **`SlowStartThreshold`** (default `0`, meaning 10s): a step still running after it is logged once at Warn level as `start step is slow`, naming its `scope`, `service` and `step`. It needs a `Logger` that keeps Warn records; without one, no timer is armed. A negative value disables it. With `StartTimeout` set, the warning is armed only when the threshold is below it: a step that reaches the timeout is reported by its abandonment instead, and a step abandoned before the threshold never warns. When armed, the warning is one timer on `Clock` per running step, which a fake clock's `BlockUntil` counts.
- **`StartTimeout`** (default `0`, off): a step still running after it is abandoned. The call returns a `*doger.StartError` naming the step that matches `doger.ErrStartTimeout` and `context.DeadlineExceeded`. When the context of `StartWithContext` ends first, root `Shutdown` begins, or a `Shutdown` of the step's child scope (or of an ancestor child scope) begins, the step's context is cancelled at once and the run waits for it up to the timeout, so a step that honours its context returns and is not abandoned. A step cancelled by its child scope's own shutdown, while root `Shutdown` has not begun, is skipped, not a failure, even once abandoned, unless a service outside that scope depends on it (see [Starter](../service-lifecycle/starter.md)). A `Shutdown` bounded below `StartTimeout` stops waiting at its deadline and skips the services it then reaches, as without the option. Under root `Shutdown`, a step of a child scope is reported like a root step: a `*doger.StartError` naming it that matches `doger.ErrShutdown`, and `doger.ErrStartTimeout` once abandoned. Go cannot stop the method: it keeps running in its own goroutine and is never called a second time while it runs, and `Shutdown` still calls the service's `Shutdowner`. So a `runtime.Goexit` in a `Start` method or kind handler ends only that goroutine and, before the timeout, is reported as a `*doger.StartError` for the step (after it, it is logged as the late outcome); without the option the call runs on the caller's goroutine, which `Goexit` ends. Meanwhile `Invoke` of the service returns its instance, which may be only half started. A `Refresh` or `Scope.Start` that covers it meanwhile skips it, with no hooks, and skips every service of that run that depends on it, directly or through others; it starts everything else, then returns its first failure, or else a `*doger.StartError` naming the still-running step that matches `doger.ErrStartTimeout`. A run that does not cover the step does the same for its dependents: `Scope.Start`, or any run, skips a dependent of a still-running step that lives in an ancestor scope, or whose own step runs in an earlier stage, and fails with that step's `ErrStartTimeout` error. Once the call returns nil it is recorded as started (or handled), unless its scope is shutting down or no longer holds it, and once it fails a later `Refresh` or `Scope.Start` tries it again. After root `Start` abandoned a step, no such run follows: `Start` has failed, so `Refresh` and `Scope.Start` return `doger.ErrNotStarted` and `Start` returns `doger.ErrAlreadyStarted`; the late return is still recorded or logged, and `Shutdown` is the way on. Nothing is in flight then, so `Shutdown` does not wait for the abandoned call: it can return a successful report while that call still runs. The guarantee holds per instance for non-nil pointers to non-zero-size values, non-nil channels and non-nil unsafe pointers: a run skips the same way, with its dependents in any scope, a step whose instance has an abandoned call of that step still running, also after an override registers that instance again, and in a clone, which shares eager instances and so shares with its source the record of the abandoned calls that still run. That record is made at abandonment only: two containers that share an instance can still call it at the same time while neither call has been abandoned. Any other instance (a struct or other value, a map, a nil pointer, a pointer to a zero-size type) is tracked per registration only: a clone or an override calls it again, and two registrations never block each other through it. A default registration that yielded holds no instance of its own: its dependents wait for the target's. A `Start` method that calls `Refresh` or `Scope.Start` synchronously, from the run that covers it, waits for that run, which waits for it: without the option that is a deadlock; with it, the run abandons the step and fails with `ErrStartTimeout`, and the nested call then fails with `ErrStartTimeout` too, since the step it reaches is its own still-running caller. Never do it either way.

Both are measured on `Clock`. The fix for a blocking `Start` is to return once the service is ready; see [A Start that never returns](../service-lifecycle/starter.md#start-never-returns).

```go
injector := doger.NewWithOpts(&doger.InjectorOpts{
    Logger:       logger,
    StartTimeout: 30 * time.Second,
})
err := injector.Start() // names the stuck service instead of blocking forever
```

### Clock {#clock}

`InjectorOpts.Clock` is the container's time source, a `doger.Clock`: any type with `Now() time.Time`, `Since(time.Time) time.Duration` and `AfterFunc(time.Duration, func()) doger.Timer`. The container reads time only from it: the `HealthCheckTimeout`, `HealthCheckSingleTimeout` and `HealthCheckGlobalTimeout` deadlines, the health report cache and bounds, lazy service build times (`ServiceBuildTime` in explain output), `ShutdownReport.ShutdownTime`/`ServiceShutdownTime`, and `SlowStartThreshold` and `StartTimeout`. It is also passed to the `HealthScheduler`.

- **Default:** `nil`, which means `doger.RealClock()`, backed by the `time` package.
- **Override:** pass a controllable clock to drive time in tests, for example to reach a health-check timeout without waiting for it. The module `github.com/kartaladev/doger/clock/clockwork` wraps a [clockwork](https://github.com/jonboulle/clockwork) clock; `dogerclockwork.Wrap(nil)` is a nil clock, so the default.
- Child scopes and `Clone()` use the root scope's clock. `CloneWithOpts(opts)` uses `opts.Clock`.
- While a start step runs, a positive `StartTimeout` arms one timer on the clock for it, and with a `Logger` that keeps Warn records the `SlowStartThreshold` warning may arm one more. A fake clock's `BlockUntil` counts them, so a test that waits for a number of timers counts these too, or sets `SlowStartThreshold: -1` to remove the warning timer.

```go
import (
    "github.com/jonboulle/clockwork"
    dogerclockwork "github.com/kartaladev/doger/clock/clockwork"
)

fake := clockwork.NewFakeClock()
injector := doger.NewWithOpts(&doger.InjectorOpts{
    Clock:              dogerclockwork.Wrap(fake),
    HealthCheckTimeout: 5 * time.Second,
})

// ... start injector.HealthCheck() in a goroutine, then:
fake.Advance(5 * time.Second) // a check still running now times out
```

:::note Migration
`InjectorOpts.Clock` was a `clockwork.Clock`. Replace `Clock: c` with `Clock: dogerclockwork.Wrap(c)` and require `github.com/kartaladev/doger/clock/clockwork`. The root module no longer depends on clockwork.
:::

### Health scheduler {#health-scheduler}

`InjectorOpts.HealthScheduler` keeps `HealthReport()` results fresh in the background. The container calls its `ScheduleHealthRounds(clock doger.Clock, round func(ctx context.Context))` once, when it reaches `PhaseReady`, with the injector clock (`doger.RealClock()` when `Clock` is nil). The `scheduler/gocron` module provides one.

A scheduler that needs a clockwork clock recovers it with `dogerclockwork.Unwrap(clock)`, which reports `false` for a clock not made by `dogerclockwork.Wrap`:

```go
func (s *myScheduler) ScheduleHealthRounds(clock doger.Clock, round func(context.Context)) (func(context.Context) error, error) {
    cw, ok := dogerclockwork.Unwrap(clock)
    if !ok {
        cw = clockwork.NewRealClock()
    }
    // ... schedule round on cw
}
```

An error or a panic of `ScheduleHealthRounds` leaves the container unmonitored and is logged at Error level on `InjectorOpts.Logger`; a panic value is logged at Debug level only. Migration: implementers change the parameter type from `clockwork.Clock` to `doger.Clock`.

### Start hooks {#start-hooks}

`HookBeforeStart` and `HookAfterStart` run around each service's `Start` method when the container [starts](../service-lifecycle/starter.md), in the order they were added. `HookAfterStart` receives the error the method returned, **unwrapped**: `nil` on success, or the recovered panic as an error. No start hook runs for an `AsStarter()` service whose instance has no `Start` method, since no method is called. A panicking hook propagates out of `Start`, as a panicking shutdown hook does.

### Build hook {#build-hook}

`HookAfterBuild` receives each instance the container builds, with its scope and service name: after every successful provider call (each transient instance included) and after an eager value's first successful invocation. It runs between the before- and after-invocation hooks. It does not run for a failed build, a cached lazy instance or an alias (the alias's target reports its own build). A panicking hook propagates to the caller. The hook runs after the service released its lock, so it may invoke, health-check or explain the service it receives; another goroutine may already be using the instance while the hook runs.

```go
injector.AddAfterBuildHook(func(scope *doger.Scope, serviceName string, instance any) {
    metrics.ServicesBuilt.WithLabelValues(serviceName).Inc()
})
```

### Add hooks at runtime {#add-hooks-at-runtime}

Hooks can also be registered after the injector is created using helper methods on the root scope. These append to the corresponding hook lists in `doger.InjectorOpts` and apply to subsequent registrations/invocations/starts/shutdowns.

```go
import "github.com/kartaladev/doger"

injector := doger.New()

// Registration hooks
injector.AddBeforeRegistrationHook(func(scope *doger.Scope, serviceName string) {
    // ...
})
injector.AddAfterRegistrationHook(func(scope *doger.Scope, serviceName string) {
    // ...
})

// Invocation hooks
injector.AddBeforeInvocationHook(func(scope *doger.Scope, serviceName string) {
    // ...
})
injector.AddAfterInvocationHook(func(scope *doger.Scope, serviceName string, elapsed time.Duration, err error) {
    // elapsed: the invocation, a build included, on the container clock
})

// Start hooks: ctx is the context Start, Refresh or Scope.Start runs with
injector.AddBeforeStartHook(func(ctx context.Context, scope *doger.Scope, serviceName string) {
    // ...
})
injector.AddAfterStartHook(func(ctx context.Context, scope *doger.Scope, serviceName string, elapsed time.Duration, err error) {
    // elapsed: the Start method call
})

// Shutdown hooks: ctx derives from the shutdown's context
injector.AddBeforeShutdownHook(func(ctx context.Context, scope *doger.Scope, serviceName string) {
    // ...
})
injector.AddAfterShutdownHook(func(ctx context.Context, scope *doger.Scope, serviceName string, elapsed time.Duration, err error) {
    // elapsed: the Shutdown call
})
```
