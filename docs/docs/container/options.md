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
    HookAfterInvocation:    []func(scope *doger.Scope, serviceName string, err error){},
    HookAfterBuild:         []func(scope *doger.Scope, serviceName string, instance any){},
    HookBeforeShutdown:     []func(scope *doger.Scope, serviceName string){},
    HookAfterShutdown:      []func(scope *doger.Scope, serviceName string, err error){},
    HookBeforeStart:        []func(scope *doger.Scope, serviceName string){},
    HookAfterStart:         []func(scope *doger.Scope, serviceName string, err error){},

    Logger: slog.Default(), // nil: nothing is logged

    HealthCheckParallelism:   100,
    HealthCheckGlobalTimeout: 1 * time.Second,
    HealthCheckTimeout:       100 * time.Millisecond, // also bounds HealthCheck[T] and HealthCheckNamed
    HealthCheckSingleTimeout: 0,                      // 0: single-service checks keep HealthCheckTimeout

    Clock: nil, // nil: the real clock
    DisableStarterAutoDetection: false, // true: Start only starts AsStarter() services
    StartParallelism: 0, // 0 or 1: sequential; n > 1: up to n Starters at a time
})
```

### Logging {#logging}

`InjectorOpts.Logger` takes any `*slog.Logger`. When it is nil (the default) the container logs nothing, not even to `slog.Default()`, and builds no record. Each event has a fixed message and level, with the attributes `scope` (the scope name) and `service` where they apply:

| Level | Messages |
| --- | --- |
| Debug | `service registered`, `service overridden`, `service invoked`, `service exported`, `health check requested`, `health check failed` (with `error`), `property source selected`, `panic value` (with `value`) |
| Info | `container started`, `container ready`, `container closing`, `container closed`, `extension disabled`, `container health changed` back to healthy |
| Warn | `property source unavailable` (with `source_type`), `properties read after extend`, `container health changed` to another state |
| Error | `service shutdown failed` (with `error`), `health scheduler failed` (with `error`), `health scheduler stop failed` (with `error`), `panic recovered` (with `point` and `panic_type`) for a recovered panic not passed to `HookContainerPanic`: the first panic of an operation goes to that hook when it is set (logged only when it is not), and later ones are logged |

A recovered panic's value and a health check's error text appear only at Debug level, so they stay out of production logs that could expose secrets.

The handler must not panic: a panic it raises on a goroutine the container runs in the background, such as a service shutdown or a health check, cannot be recovered by the caller and crashes the process.

```go
logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
injector := doger.NewWithOpts(&doger.InjectorOpts{Logger: logger})
```

### Clock {#clock}

`InjectorOpts.Clock` is the container's time source, a `doger.Clock`: any type with `Now() time.Time`, `Since(time.Time) time.Duration` and `AfterFunc(time.Duration, func()) doger.Timer`. The container reads time only from it: the `HealthCheckTimeout`, `HealthCheckSingleTimeout` and `HealthCheckGlobalTimeout` deadlines, the health report cache and bounds, lazy service build times (`ServiceBuildTime` in explain output), and `ShutdownReport.ShutdownTime`/`ServiceShutdownTime`. It is also passed to the `HealthScheduler`.

- **Default:** `nil`, which means `doger.RealClock()`, backed by the `time` package.
- **Override:** pass a controllable clock to drive time in tests, for example to reach a health-check timeout without waiting for it. The module `github.com/kartaladev/doger/clock/clockwork` wraps a [clockwork](https://github.com/jonboulle/clockwork) clock; `dogerclockwork.Wrap(nil)` is a nil clock, so the default.
- Child scopes and `Clone()` use the root scope's clock. `CloneWithOpts(opts)` uses `opts.Clock`.

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
injector.AddAfterInvocationHook(func(scope *doger.Scope, serviceName string, err error) {
    // ...
})

// Start hooks
injector.AddBeforeStartHook(func(scope *doger.Scope, serviceName string) {
    // ...
})
injector.AddAfterStartHook(func(scope *doger.Scope, serviceName string, err error) {
    // ...
})

// Shutdown hooks
injector.AddBeforeShutdownHook(func(scope *doger.Scope, serviceName string) {
    // ...
})
injector.AddAfterShutdownHook(func(scope *doger.Scope, serviceName string, err error) {
    // ...
})
```
