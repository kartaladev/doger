# Proposal

## Why

The container has build, health-check and shutdown, but no notion of *starting*. Today:
- Background components such as servers, consumers and schedulers must be started by hand after wiring.
- One-shot startup work (runners) has no home.
- There is no way to introduce a new component type (e.g. `Scheduled`) whose instances a dedicated handler discovers and wires into another service (e.g. a scheduler).

These are the first features needed to match common DI container practice, built in a Go-idiomatic way: interfaces, functional options, generics.

## What Changes

**Start lifecycle**
- Add `Starter` interface variants that mirror the existing `Shutdowner` family: `Starter` (`Start()`), `StarterWithError` (`Start() error`), `StarterWithContext` (`Start(context.Context)`), `StarterWithContextAndError` (`Start(context.Context) error`). There is no Stop counterpart. A started service is stopped by its existing `Shutdowner*` method during `Shutdown`.
- Add `Start() error` / `StartWithContext(ctx) error` on `RootScope`, mirroring `Shutdown` / `ShutdownWithContext`.
  - Starting is a container-level operation. It covers the root scope and every descendant scope that exists when `Start` is called, parent scopes before children.
  - `Scope` and the `Injector` interface do not gain `Start` in this change. Scope-level start, and scopes created after `Start`, are phase 2.
- Start runs as a pipeline:
  1. Discover candidates by type or opt-in and instantiate them in dependency order. Every service's dependencies, invoked or declared, finish construction before it. Independent services are taken in registration order.
  2. Run component-kind handlers.
  3. Start `Starter*` services one at a time in dependency (DAG) order, with parent scopes before child scopes and registration order for ties. This is the mirror image of shutdown order. Parallel start of independent services is a possible later option.
  4. Run post-start handlers, such as runnables.
- The dependency DAG is the single source of ordering for construction, start and shutdown. There is no `Phase`/`Order` interface. Explicit ordering is expressed as a declared dependency edge. Registration order deterministically breaks ties between services that have no dependency between them; today that order follows Go map iteration and is nondeterministic.
- Construction remains initialization, following the Go constructor idiom: the provider returns a ready value. No separate post-construction initializer callback is added.
- On the first error, `Start` stops the pipeline and returns a wrapped error that identifies the failing service. It does **not** roll back. Cleanup is the caller's job (`Shutdown()`), and `Shutdown` must be safe to call on a service whose `Start` never ran.
- `Start` is one-shot: a second call returns `ErrAlreadyStarted`, and a call after shutdown returns `ErrStartAfterShutdown`.
- A panic inside a `Start*` method or a kind handler is recovered and returned as an error naming the service.
- `StartWithContext` checks the context before each service. A cancelled or expired context stops the pipeline with an error that wraps `ctx.Err()`.
- Transient services are never started or handled. A factory has no single instance to start. Opting a transient registration into a kind, or into `AsStarter()`, panics at registration.
- A `Starter*` that implements no `Shutdowner*` variant is allowed. Its lifecycle simply ends with the process.
- Detection uses the service's **registered** type. A service registered under an interface type that lacks `Start` is not auto-detected even if its concrete instance has `Start`. Such a service opts in with `AsStarter()`.
- Add per-service start hooks `HookBeforeStart []func(*Scope, string)` / `HookAfterStart []func(*Scope, string, error)` to `InjectorOpts`, plus `RootScope.AddBeforeStartHook` / `AddAfterStartHook`, mirroring the per-service shutdown hooks.

**Registration options and metadata**
- **BREAKING (signature)**: `Provide*`, `Override*` and the `Package` helpers (`Lazy*`, `Eager*`, `Transient*`) gain a trailing variadic `...ProvideOption`. Every existing call compiles unchanged. Only code that assigns these functions to function-typed variables breaks.
- `ProvideOption` is an opaque functional-option type. Consumers obtain options only from library constructors: `AsStarter()`, `DependsOn[T]()`, `DependsOnNamed(name)`, `kind.With(meta)`, `AsRunnable()`.
- Each registration carries metadata. `clone()` copies it, and `Override*` replaces it. Aliases (`As`/`Bind`) do not inherit opt-ins, so one service is never started or handled twice.

**Component kinds**
- Add a typed component kind, `Kind[T, M]`, created with `NewKind[T, M](name)`: a target interface `T` plus per-registration metadata `M`.
  - Its option (`kind.With(meta)`) opts a registration in.
  - Registration panics immediately if the provided type is not assignable to `T`.
- Consumers register one handler per kind with `kind.Handle(root, stage, handler)`.
  - `stage` is `StageBeforeStart` (after discovery, before Starters start) or `StageAfterStart` (after all Starters are up).
  - The handler is `func(ctx context.Context, c Component[T, M]) error`, where `Component` carries the scope, the service name, the instance typed `T`, and the metadata `M`.
  - Registering a handler for a kind that already has one replaces it.
  - Opted-in services of a kind that has no handler are still instantiated at Start, but nothing else happens to them.
- Ship the built-in `Runnable` kind, `Kind[Runner, struct{}]`, where `Runner` is `Run(context.Context) error`. `AsRunnable()` is shorthand for `Runnable.With(struct{}{})`.
  - Its default `StageAfterStart` handler runs every opted-in runner once, in dependency then registration order, and stops at the first error.
  - The library's own feature is built on the public extension mechanism.

**Explicit dependency edges**
- Declare a dependency between two registered services without one invoking the other. There are two forms, and both feed the same DAG:
  - **Static**: a `DependsOn` registration option. Covers hidden dependencies, e.g. a provider that keeps the injector and invokes a service later at runtime.
  - **Dynamic**: `DeclareDependency(i Injector, dependent, dependency string) error`, typically called by a kind handler during Start. Example: a scheduler handler records `Scheduler -> Job`, so shutdown stops the scheduler before the jobs it drives. Without such an edge the two would shut down in parallel.
- A declared edge orders construction, start and shutdown in the same way an invoked dependency does.
- A declared edge that points to an unregistered service makes `Start` fail with a not-found error.
- A declared edge that closes a cycle makes `Start` fail with the existing circular-dependency error, naming the chain.

**Discovery**
- Internal, deterministic enumeration of registered services by assignable type or by kind, without invoking unrelated services. Order is ancestor scopes first, then registration order. This requires recording a registration index for each service.
- A public collection API is out of scope for this change.

## Capabilities

### New Capabilities

- `container-start`: starting a container — `Starter*` detection, ordering, scope traversal, errors, one-shot semantics, per-service start hooks, and the shutdown-without-start contract.
- `registration-options`: options and metadata attached to a service registration, and their behavior under clone, override and alias.
- `component-kinds`: typed component kinds, opt-in, handler registration and staging, and the built-in `Runnable` kind.
- `dependency-declaration`: explicitly declared dependency edges, static and dynamic, and their effect on construction, start and shutdown order. Also covers deterministic tie-breaking by registration order.

### Modified Capabilities

None (no existing specs).

## Impact

- Core package: `service.go` and all four service wrappers (metadata, start), `scope.go` / `root_scope.go` (Start, scan, ordering), `dag.go` (forward walk, explicit edges), `di.go` (option parameters), `injector.go` (hooks), `di_lifecycle.go` (`Starter*`).
- Docs: new lifecycle pages and `Example*` tests for each default and override.
- Phase 2, out of scope here: an instance-aware post-build hook, scope-level `Start`, handling of child scopes created after `Start`, parallel start, and a `Refresh` operation. Public `InvokeAll[T]` is in `add-collection-invocation`.
- Depends on `upgrade-go-1-27` (and transitively on `fix-stacktrace-frame-filter` and `rename-module-doger`). `add-container-lifecycle-hooks` and `add-collection-invocation` build on this change.

## Defaults & Control

| Behavior | Zero-config default | Override / opt-out |
|---|---|---|
| `Starter*` discovery | Auto-detected by registered type, like `Shutdowner*` | `InjectorOpts.DisableStarterAutoDetection` switches to explicit opt-in via `AsStarter()` |
| `Runner` and custom kinds | Explicit opt-in only (`AsRunnable()`, `kind.With(...)`), because `Run` is too common a method name to auto-detect | `InjectorOpts.EnableRunnerAutoDetection` auto-detects `Runner` by registered type (custom kinds always require opt-in) |
| Construction and start order | Dependency DAG forward, parent scope before children, registration order for ties | Declare edges with the `DependsOn` option or from a kind handler; reorder registrations to change ties |
| `Runnable` handling | Built-in handler: run once after Starters, fail-fast | Replace the `Runnable` handler with a consumer handler |
| Start failure | Return wrapped error, no rollback | Caller decides: `Shutdown()`, retry, or exit |
| Per-service start hooks | None registered | `HookBeforeStart` / `HookAfterStart` |
| Calling `Start` again / after shutdown | `ErrAlreadyStarted` / `ErrStartAfterShutdown` | None needed: a container is started once. Re-running belongs to the deferred `Refresh` operation. |
| Start order among independent services | Sequential, registration order | Declare edges; parallel start is a possible later option |
| Transient services | Never started or handled; opting one in panics at registration | Register as lazy or eager to make it startable |
