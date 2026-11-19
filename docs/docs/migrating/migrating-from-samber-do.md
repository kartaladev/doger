---
title: From samber/do
description: Migrate a Go application from samber/do v2 to doger, its fork
sidebar_position: 1
---

# Migrating from samber/do

doger is a fork of [samber/do](https://github.com/samber/do) by Samuel Berthe. It keeps the shape of the `samber/do` v2 API (`New`, `Provide*`, `Invoke*`, scopes, health checks, shutdown) and hardens it for production use. This page lists every change a `samber/do` v2 application meets, each with its migration; code marked **Before** is `samber/do` v2, code marked **After** is doger and compiles as shown (the repository's `docs_snippets_test.go` checks it).

A last section covers the APIs that changed between doger pre-releases, for code written against an early version of doger.

## Go 1.27 or later {#go-1-27}

doger requires Go 1.27 or later: every module declares `go 1.27`. The code sets the promoted fields of embedded structs directly in struct literals, which the compiler accepts only from Go 1.27, and it uses `sync.WaitGroup.Go` and, in its tests, `testing/synctest`. With the go command's default `GOTOOLCHAIN=auto`, building a module that requires doger downloads Go 1.27 automatically; with `GOTOOLCHAIN=local`, an older toolchain stops with "go.mod requires go >= 1.27".

Requiring doger raises your module's `go` line to 1.27, and GODEBUG defaults follow that line. If it was below 1.21, this turns on `panicnil=0`: a provider that panics with `nil` used to give the zero value and a `nil` error, and now gives an error matching `*runtime.PanicNilError`, as every provider panic also matches `doger.ErrProviderPanic`:

<!-- snippet: panic-nil -->
```go
injector := doger.New()
doger.Provide(injector, func(i doger.Injector) (*Postgres, error) {
	panic(nil)
})

_, err := doger.Invoke[*Postgres](injector)

var panicNilErr *runtime.PanicNilError
errors.As(err, &panicNilErr)           // true
errors.Is(err, doger.ErrProviderPanic) // true
```

To restore the old `(zero, nil)` result, run the program with `GODEBUG=panicnil=1`, or add the directive to your main package:

<!-- text -->
```go
//go:debug panicnil=1

package main
```

## Module path and package name {#module-path-and-package-name}

The module `github.com/samber/do/v2` (package `do`) becomes `github.com/kartaladev/doger` (package `doger`). The `/v2` suffix is dropped: doger starts at v0.

```bash
go get github.com/kartaladev/doger@latest
```

**Before**

<!-- before -->
```go
import "github.com/samber/do/v2"

injector := do.New()
do.Provide(injector, NewDatabase)
```

**After**

<!-- text -->
```go
import "github.com/kartaladev/doger"
```

<!-- snippet: basic -->
```go
injector := doger.New()
doger.Provide(injector, NewDatabase)
```

To keep your `do.` call sites, alias the import: `import do "github.com/kartaladev/doger"`.

### Debug UI adapters {#debug-ui-adapters}

The HTTP adapters move under the root path and get a `doger` prefix, so they no longer clash with the framework they adapt:

| Old import path                      | New import path                          | New package    |
| ------------------------------------ | ---------------------------------------- | -------------- |
| `github.com/samber/do/v2/http`       | `github.com/kartaladev/doger/http`       | `dogerhttp`    |
| `github.com/samber/do/http/chi/v2`   | `github.com/kartaladev/doger/http/chi`   | `dogerchi`     |
| `github.com/samber/do/http/echo/v2`  | `github.com/kartaladev/doger/http/echo`  | `dogerecho`    |
| `github.com/samber/do/http/fiber/v2` | `github.com/kartaladev/doger/http/fiber` | `dogerfiber`   |
| `github.com/samber/do/http/gin/v2`   | `github.com/kartaladev/doger/http/gin`   | `dogergin`     |
| `github.com/samber/do/http/std/v2`   | `github.com/kartaladev/doger/http/std`   | `dogerhttpstd` |

The routes stay relative to the base path you pass. `dogerchi.Use` takes a `chi.Router` instead of a `*chi.Mux` (a wider parameter, so calls compile unchanged). The page functions and each adapter's `Use` gain a trailing `...dogerhttp.Option`: calls compile unchanged, but a variable of the old function type must be updated. The std adapter answers GET and HEAD only, and every adapter answers a render failure with a generic 500 body instead of the error text.

### Inferred service names of library types {#inferred-service-names}

Service names inferred from types embed the import path. For types declared in the library, that path changes (`*github.com/samber/do/v2.RootScope` becomes `*github.com/kartaladev/doger.RootScope`, and so on). Never hard-code such names: compute them with `doger.NameOf[T]()`, or register the services under explicit names. Names of your own types do not change.

## Error messages and sentinels {#errors}

Every library message starts with `doger: ` once, instead of `DI: `. Match errors with `errors.Is` and `errors.As`, never by text. Sentinels and error types that are new or changed:

| Error | Matched by |
| --- | --- |
| `ErrServiceTypeMismatch` | an invocation that finds the name under another type |
| `ErrServiceAlreadyDeclared` | a duplicate registration: the panic value of `Provide*`, the error of `Alias`/`AliasNamed` |
| `ErrScopeAlreadyDeclared` | the panic value of `Scope(name)` for a name its parent already has |
| `ErrNilInjector` | a nil injector, root or scope, including a typed nil |
| `ErrProviderPanic` | an invocation whose provider panicked; an error panic value is wrapped, so it still matches too |
| `ErrStructInjection` | an invalid `InvokeStruct` target |
| `ErrShutdown` | a container or scope used after its shutdown began |
| `ErrShutdownPanic` | a service whose `Shutdown` method or shutdown hook panicked: the panic is recovered and reported |
| `*ServiceError` (`errors.As`) | a failure inside a service's build; it names the service and its scope |
| `*StartError` (`errors.As`) | a service or kind handler failing during `Start`, `Refresh` or `Scope.Start` |

A provider that panics no longer returns its error panic value as is, so do not compare it with `==`:

<!-- snippet: provider-panic -->
```go
_, err := doger.Invoke[*Postgres](injector) // its provider panicked with errBroken
if errors.Is(err, errBroken) {              // still true: the panic value is wrapped
	fmt.Println("broken:", err)
}
if serviceErr, ok := errors.AsType[*doger.ServiceError](err); ok {
	fmt.Println("failed service:", serviceErr.Service)
}
```

Not-found errors list at most 10 services, followed by `and N more`.

## Renamed and removed APIs {#renamed-and-removed-apis}

### As becomes Alias {#alias}

`As`, `AsNamed`, `MustAs` and `MustAsNamed` become `Alias`, `AliasNamed`, `MustAlias` and `MustAliasNamed`. `Alias` and `AliasNamed` return a duplicate name as an error matching `ErrServiceAlreadyDeclared` instead of panicking. `Bind` and `BindNamed` are unchanged.

**Before**

<!-- before -->
```go
do.Provide(injector, NewDatabase)
do.As[*Postgres, Database](injector)
```

**After**

<!-- snippet: alias -->
```go
doger.Provide(injector, NewDatabase)
if err := doger.Alias[*Postgres, Database](injector); err != nil {
	return err // matches doger.ErrServiceAlreadyDeclared for a taken name
}
```

### No package-level container {#no-default-root-scope}

`DefaultRootScope` is removed, and a nil injector no longer falls back to it. A nil injector, root or scope, including a typed-nil `*RootScope` or `*Scope`, gives an error matching `ErrNilInjector` from the functions with an error result, and panics with a value matching it elsewhere.

**Before**

<!-- before -->
```go
do.Provide(nil, NewDatabase) // registered in do.DefaultRootScope
db := do.MustInvoke[*Postgres](nil)
```

**After**

<!-- snippet: nil-injector -->
```go
injector := doger.New() // pass it explicitly; there is no package-level container
doger.Provide(injector, NewDatabase)

_, err := doger.Invoke[*Postgres](nil)
errors.Is(err, doger.ErrNilInjector) // true
```

### Logf becomes Logger {#logger}

`InjectorOpts.Logf` becomes `InjectorOpts.Logger`, a `*slog.Logger`. Each event has a fixed message and level, with `scope` and `service` attributes; panic values and health-check error text appear only at Debug level. A nil logger, the default, logs nothing.

**Before**

<!-- before -->
```go
injector := do.NewWithOpts(&do.InjectorOpts{
	Logf: func(format string, args ...any) { log.Printf(format, args...) },
})
```

**After**

<!-- snippet: logger -->
```go
injector := doger.NewWithOpts(&doger.InjectorOpts{
	Logger: slog.Default(), // nil (the default) logs nothing
})
```

### MaxInvocationFrames is per container {#max-invocation-frames}

The package-wide `MaxInvocationFrames` variable becomes the `InjectorOpts.MaxInvocationFrames` field.

**Before**

<!-- before -->
```go
do.MaxInvocationFrames = 10
```

**After**

<!-- snippet: max-frames -->
```go
injector := doger.NewWithOpts(&doger.InjectorOpts{
	MaxInvocationFrames: 10, // 0: 100 frames; negative: none recorded
})
```

### Per-service hooks get a context and the elapsed time {#hooks}

The shutdown hooks receive the operation's context, and the "after" hooks the time the step took on the container clock. `HookAfterInvocation` receives the elapsed time but no context, because `Invoke` has none. The new start hooks follow the shutdown shape.

| Hook | Signature |
| --- | --- |
| `HookBeforeShutdown`, `HookBeforeStart` | `func(ctx context.Context, scope *Scope, serviceName string)` |
| `HookAfterShutdown`, `HookAfterStart` | `func(ctx context.Context, scope *Scope, serviceName string, elapsed time.Duration, err error)` |
| `HookAfterInvocation` | `func(scope *Scope, serviceName string, elapsed time.Duration, err error)` |

**Before**

<!-- before -->
```go
injector := do.NewWithOpts(&do.InjectorOpts{
	HookAfterInvocation: []func(scope *do.Scope, serviceName string, err error){
		func(scope *do.Scope, serviceName string, err error) { /* ... */ },
	},
	HookBeforeShutdown: []func(scope *do.Scope, serviceName string){
		func(scope *do.Scope, serviceName string) { /* ... */ },
	},
	HookAfterShutdown: []func(scope *do.Scope, serviceName string, err error){
		func(scope *do.Scope, serviceName string, err error) { /* ... */ },
	},
})
```

**After**

<!-- snippet: hooks -->
```go
injector := doger.NewWithOpts(&doger.InjectorOpts{
	HookAfterInvocation: []func(scope *doger.Scope, serviceName string, elapsed time.Duration, err error){
		func(scope *doger.Scope, serviceName string, elapsed time.Duration, err error) {
			fmt.Println("invoked", serviceName, "in", elapsed)
		},
	},
	HookBeforeShutdown: []func(ctx context.Context, scope *doger.Scope, serviceName string){
		func(ctx context.Context, scope *doger.Scope, serviceName string) {
			fmt.Println("shutting down", serviceName)
		},
	},
	HookAfterShutdown: []func(ctx context.Context, scope *doger.Scope, serviceName string, elapsed time.Duration, err error){
		func(ctx context.Context, scope *doger.Scope, serviceName string, elapsed time.Duration, err error) {
			fmt.Println("shut down", serviceName, "in", elapsed, "err:", err)
		},
	},
})
```

The `Add…Hook` methods take the same function types, and adding a hook is safe at any time: it applies from the next operation on.

### Registration functions take options {#provide-options}

`Provide*`, `Override*` and the `Package` helpers (`Lazy*`, `Eager*`, `Transient*`) gain a trailing `...doger.ProvideOption` parameter, for `AsStarter()`, `AsDefault()`, `DependsOn`, health tags and kinds. Every existing call compiles unchanged; a variable holding one of these functions must take the new function type.

<!-- snippet: provide-options -->
```go
doger.Provide(injector, NewServer, doger.AsStarter()) // a trailing option

// a registration function stored as a value has the options parameter:
// func(doger.Injector, doger.Provider[*Postgres], ...doger.ProvideOption)
register := doger.Provide[*Postgres]
register(injector, NewDatabase)
```

### Other removals {#other-removals}

- `DAG` is unexported; it had no public use.
- `ServiceDescription` marshals to JSON with the keys `scopeId`, `scopeName` and `service` instead of `ScopeID`, `ScopeName` and `Service`.
- `ExplainServiceDependencyOutput` expands each service once: a repeat is a node with `Ref: true` and no children. Follow it with `ExplainNamedService`.

## Changed behavior {#changed-behavior}

### Health checks are bounded {#health-checks-bounded}

`HealthCheckTimeout` 0 now means 10 seconds, on `RootScope.HealthCheck`, `Scope.HealthCheck` and the per-service functions (`HealthCheckSingleTimeout` 0 inherits it). `HealthCheckGlobalTimeout` 0 or less means no global bound.

<!-- snippet: health-timeout -->
```go
injector := doger.NewWithOpts(&doger.InjectorOpts{
	HealthCheckTimeout: -1, // no bound; 0 (the default) means 10s
})
```

A health check whose context has ended returns the timeout error every time, matching `ErrHealthCheckTimeout` and the context's error, instead of a random result:

<!-- snippet: health-ended -->
```go
err := doger.HealthCheckNamedWithContext(ctx, injector, "db")
if errors.Is(err, context.Canceled) {
	fmt.Println("the check's context was cancelled")
}
```

### Shutdown {#shutdown}

- **Run once.** Root `Shutdown` runs once per container; a later call returns a copy of the first report.
- **The report's error.** `ShutdownReport.Err()` returns nil on success and otherwise an error matching every service's error; check it instead of the report itself. `Error()` returns `""` on success.

<!-- snippet: shutdown-report -->
```go
if err := injector.Shutdown().Err(); err != nil {
	return err // matches each service's error with errors.Is and errors.As
}
```

- **Bounded by its context.** `ShutdownWithContext` returns once its context ends, even while a service's `Shutdown` still runs: such services are reported with the context's error and keep running in the background. A service reached after the context ended is not shut down at all.

<!-- snippet: bounded-shutdown -->
```go
ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
defer cancel()

report := injector.ShutdownWithContext(ctx)
if err := report.Err(); errors.Is(err, context.DeadlineExceeded) {
	// services still shutting down at the deadline keep running in the
	// background; the report names them
	fmt.Println(err)
}
```

- **Panics are recovered.** A panicking `Shutdown` method or shutdown hook no longer crashes the process: it is reported with an error matching `ErrShutdownPanic`, and the other services still shut down.
- **No reuse after shutdown.** Invoking, registering, aliasing, exporting or creating a scope on a container whose shutdown began fails with `ErrShutdown` (returned, or as a panic value for the functions without an error result). Build a new container instead.

<!-- snippet: after-shutdown -->
```go
injector.Shutdown()

_, err := doger.Invoke[*Postgres](injector)
errors.Is(err, doger.ErrShutdown) // true: build a new container instead
```

- **Shut-down child scopes are detached**, so a child scope's name can be declared again after its shutdown. Declaring an attached child's name twice panics with `ErrScopeAlreadyDeclared`:

<!-- snippet: duplicate-scope -->
```go
defer func() {
	if err, ok := recover().(error); ok && errors.Is(err, doger.ErrScopeAlreadyDeclared) {
		fmt.Println(err) // doger: scope `tenant` has already been declared
	}
}()
injector.Scope("tenant")
injector.Scope("tenant")
```

## New options {#new-options}

Every `InjectorOpts` field `samber/do` v2 had is kept, apart from `Logf`. These are new; each has a default that works with zero configuration:

| Field | Default | Purpose |
| --- | --- | --- |
| `Logger` | nil: no logging | levelled `log/slog` records (replaces `Logf`) |
| `MaxInvocationFrames` | 0: 100 frames | invocation frames kept per service for Explain; negative turns recording off |
| `Clock` | nil: the real clock | the container clock for timeouts, health timing and hook elapsed times |
| `HookAfterBuild` | none | called with each newly built instance |
| `HookBeforeStart`, `HookAfterStart` | none | around each Starter's `Start` |
| `HookContainerStarting` … `HookContainerClosed` | none | container lifecycle: Starting (may veto), Started, Ready, Failed, Closing, Closed |
| `HookScopeStarting` … `HookScopeClosed` | none | scope lifecycle for `Scope.Start` and scope shutdown |
| `HookContainerPanic` | nil: an observer hook's panic is raised again once the operation completed | receives the first recovered panic of an observer hook, a service shutdown, a health check or the health scheduler |
| `HookServiceHealthChanged`, `HookContainerHealthChanged` | none | health state transitions |
| `HealthCheckSingleTimeout` | 0: `HealthCheckTimeout` | bound of the single-service health functions |
| `HealthReportCacheTTL` | 1s | how long `HealthReport` reuses a recorded result |
| `HealthScheduler` | nil: checks run on demand | runs scheduled health rounds, such as `scheduler/gocron` |
| `EnableStarterAutoDetection` | false | also start every type with a `Start` method, without `AsStarter()` |
| `StartParallelism` | 0: sequential | Starters started at a time |
| `EnableRunnerAutoDetection` | false | also run every type with `Run(ctx) error` at Start, without `AsRunnable()` |
| `PropertySource` | nil: environment variables prefixed `DOGER_` | where extensions read their configuration |

`HealthCheckParallelism`, `HealthCheckGlobalTimeout`, `HealthCheckTimeout` (now 10s when 0), `StructTagKey` and the registration and invocation hooks keep their meaning.

### Starting the container {#start}

doger adds `Start`: it builds and starts, in dependency order, the services registered with `AsStarter()`. A `samber/do` application never called it, so nothing starts by surprise; to start every type with a `Start` method instead, turn auto-detection on:

<!-- snippet: starter -->
```go
injector := doger.New()
doger.Provide(injector, NewServer, doger.AsStarter()) // started by injector.Start()

// or start every registered type with a Start method, as before:
auto := doger.NewWithOpts(&doger.InjectorOpts{EnableStarterAutoDetection: true})
```

See [Upgrading to the Start lifecycle release](../upgrading/start-lifecycle.md) for the lifecycle, and [the options page](../container/options.md) for every option.

## What stays the same {#what-stays-the-same}

- The struct injection tag key is still `do` (`do:""`), and `InjectorOpts.StructTagKey` still overrides it.
- `ErrServiceNotFound`, `ErrServiceNotMatch`, `ErrCircularDependency` and `ErrHealthCheckTimeout` still match with `errors.Is`.
- Lazy, eager and transient registrations, scopes, `Override*`, `InvokeStruct`, health checks and `ShutdownOnSignals` work as before, apart from the changes above.

## From a doger pre-release {#from-a-doger-pre-release}

Code written against an early doger version also meets these renames:

| Before | After | Migration |
| --- | --- | --- |
| `Flatten()` | `ExtendIntoRoot()` | Rename the call; `Extend` with a nil root returns `ErrNilInjector` |
| `ExportAs(name)` | `WithExportName(name)` | Rename the call |
| `var Runnable` | `func Runnable() Kind[Runner, struct{}]` | `Runnable.Handle(...)` becomes `Runnable().Handle(...)` |
| `HealthReportOption` struct | a function type | None for `MatchHealthTags`/`IgnoreContainerPhase`; a `HealthReportOption{}` literal no longer compiles |
| `Stage`, `DependencySource` starting at 0 | constants start at 1; 0 is invalid | Use the named constants |
| `DisableStarterAutoDetection` | `EnableStarterAutoDetection`, false by default | Add `AsStarter()` to the services that must start |
| `Clock clockwork.Clock`, `HealthScheduler` taking a `clockwork.Clock` | `doger.Clock` | Wrap a clockwork clock with `dogerclockwork.Wrap` (module `github.com/kartaladev/doger/clock/clockwork`) |
| `ErrStartAfterShutdown` | `ErrShutdown` | Rename the sentinel |

<!-- snippet: prerelease -->
```go
_ = doger.Extend(root, ext, doger.ExtendIntoRoot()) // was Flatten()

doger.Runnable().Handle(root, doger.StageAfterStart, func(ctx context.Context, c doger.Component[doger.Runner, struct{}]) error {
	return c.Instance.Run(ctx) // was Runnable.Handle
})

_ = root.HealthReport(doger.MatchHealthTags(doger.HealthTagReadiness), doger.IgnoreContainerPhase())

errors.Is(root.Start(), doger.ErrShutdown) // was ErrStartAfterShutdown
```

<!-- snippet: clock -->
```go
fake := clockwork.NewFakeClock()
injector := doger.NewWithOpts(&doger.InjectorOpts{
	Clock: dogerclockwork.Wrap(fake), // was Clock: fake
})
```
