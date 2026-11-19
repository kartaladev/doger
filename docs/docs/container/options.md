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
    EnableStarterAutoDetection: false, // true: Start also starts every type with a Start method
    StartParallelism: 0, // 0 or 1: sequential; n > 1: up to n Starters at a time
})
```

### Logging {#logging}

`InjectorOpts.Logger` takes any `*slog.Logger`. When it is nil (the default) the container logs nothing, not even to `slog.Default()`, and builds no record. Each event has a fixed message and level, with the attributes `scope` (the scope name) and `service` where they apply:

| Level | Messages |
| --- | --- |
| Debug | `service registered`, `service overridden`, `service invoked`, `service exported`, `requested shutdown`, `health check failed` (with `error`), `property source selected`, `panic value` (with `value`) |
| Info | `container started`, `container ready`, `container closing`, `container closed`, `extension disabled`, `container health changed` back to healthy |
| Warn | `property source unavailable` (with `source_type`), `properties read after extend`, `health scheduler stop failed`, `container health changed` to another state |
| Error | `service shutdown failed` (with `error`), `health scheduler failed`, `panic recovered` (with `point` and `panic_type`) |

A recovered panic's value and a health check's error text appear only at Debug level, so they stay out of production logs that could expose secrets.

```go
logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
injector := doger.NewWithOpts(&doger.InjectorOpts{Logger: logger})
```

### Clock {#clock}

`InjectorOpts.Clock` is the container's time source, a [`clockwork.Clock`](https://pkg.go.dev/github.com/jonboulle/clockwork#Clock). The container reads time only from it: the `HealthCheckTimeout`, `HealthCheckSingleTimeout` and `HealthCheckGlobalTimeout` deadlines, lazy service build times (`ServiceBuildTime` in explain output), and `ShutdownReport.ShutdownTime`/`ServiceShutdownTime`.

- **Default:** `nil`, which means the real clock (`clockwork.NewRealClock()`).
- **Override:** pass a fake clock to control time in tests, for example to reach a health-check timeout without waiting for it.
- Child scopes and `Clone()` use the root scope's clock. `CloneWithOpts(opts)` uses `opts.Clock`.

```go
import "github.com/jonboulle/clockwork"

clock := clockwork.NewFakeClock()
injector := doger.NewWithOpts(&doger.InjectorOpts{
    Clock:              clock,
    HealthCheckTimeout: 5 * time.Second,
})

// ... start injector.HealthCheck() in a goroutine, then:
clock.Advance(5 * time.Second) // a check still running now times out
```

:::caution
With a fake clock, a context the container creates for a health check blocks on `ctx.Err()` until it is done (clockwork behavior). In a `HealthCheck(ctx)` method, wait on `<-ctx.Done()` instead of polling `ctx.Err()`.
:::

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
