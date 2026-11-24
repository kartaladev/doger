---
title: Start lifecycle and lifecycle hooks releases
description: Changes to check when upgrading to the doger releases that add the Start lifecycle, lifecycle hooks and collections
sidebar_position: 2
---

# Upgrading to the Start lifecycle and lifecycle hooks releases

The Start lifecycle release adds container start, registration options, component kinds and declared dependencies. The lifecycle hooks release adds container and scope hooks, incremental start and collections. The changes below can affect existing code.

## Registration functions take options {#registration-options}

`Provide*`, `Override*` and the `Package` helpers (`Lazy*`, `Eager*`, `Transient*`) gain a trailing variadic parameter, `opts ...doger.ProvideOption`.

Starter auto-detection is opt-in: by default `Start` starts only the services registered with `doger.AsStarter()`, so add the option to each service that must start, or set `InjectorOpts.EnableStarterAutoDetection: true` to also start every non-transient, non-alias service whose registered type implements a Starter variant.

Every existing **call** compiles and behaves unchanged:

```go
doger.Provide(injector, NewDatabase)                       // unchanged
doger.Provide(injector, NewServer, doger.AsStarter())      // new: with an option (Start only starts AsStarter() services by default)
```

Code that stores one of these generic functions **as a value** of its old function type no longer compiles, because the function type now ends with `...doger.ProvideOption`:

```go
// no longer compiles
var register func(doger.Injector, doger.Provider[*Database]) = doger.Provide[*Database]
```

Wrap the call in a closure instead:

```go
register := func(i doger.Injector, p doger.Provider[*Database]) {
	doger.Provide(i, p)
}
```

## Root shutdown runs once {#root-shutdown-runs-once}

`Shutdown()` and `ShutdownWithContext()` on the root scope now shut the container down exactly once. This fixes concurrent calls, which used to race and report spurious `ErrServiceNotFound` errors.

Behavior changes:

- A second root `Shutdown()` returns a **copy of the first report** instead of an empty successful one.
- It no longer shuts down services registered on the root after the first shutdown. Create a new container instead of reusing a shut-down one.
- A `nil` context is treated as `context.Background()`; it used to panic once a built service had a `Shutdown` method.

Concurrent `Shutdown()` calls on one child scope join each other too: see [Concurrent child scope shutdowns join](#child-shutdown-joins). See [Root shutdown runs once](../service-lifecycle/shutdowner.md#root-shutdown-runs-once).

## An alias never shuts its target down {#alias-shutdown}

An alias registered with `As`, `AsNamed`, `Bind` or `BindNamed` no longer shuts its target down. The target is shut down only through its own registration, exactly once. This fixes an eager target (`ProvideValue`) being shut down twice once its alias had been invoked.

Behavior changes:

- Shutting down a child scope no longer shuts down a parent-scope target through an alias registered in the child. The target keeps running until its own scope shuts down.
- `ShutdownNamed` on an alias name removes the alias and no longer shuts its target down.

Migration: shut the target down by its own name, or through its own scope.

## Clones copy kind handlers {#clone-kind-handlers}

`Clone()` and `CloneWithOpts()` now copy the source's kind handlers, including a replaced `Runnable` handler, with their stages and order. A clone used to get only the default `Runnable` handler, so custom kinds had no handler on it.

Migration: none if you re-registered the same handlers on the clone. To use a different handler on a clone, call `Kind.Handle` on the clone; it replaces the handler on the clone only.

## InvokeAs selects deterministically {#invoke-as-deterministic}

`InvokeAs[T]`, `MustInvokeAs[T]` and the implicit-aliasing fallback of `InvokeStruct` (an empty `do:""` tag with no service of that name) used to return an arbitrary match when several services were assignable to `T`, depending on map iteration. They now select deterministically: the service in the nearest scope, then the earliest registration, then the smallest service name.

Since the previous pick was random, no code could rely on a specific one, but a call that used to succeed by luck may now consistently return another match. Migration: use `InvokeNamed` to pin a specific service.

**Shadowing.** A service hidden by a service of the same name in a nearer scope is no longer a candidate, as for `InvokeAll`. For example, with `db` (a `*PG` implementing `Store`) in the root and a `db` string in a child scope, `InvokeAs[Store](child)` used to return the hidden root `*PG`; it now returns an error matching `ErrServiceNotMatch`, and the struct fallback returns `ErrServiceNotFound` instead of failing with "not assignable". Migration: rename one of the services, or invoke the ancestor's service by name from its own scope.

**The invoking service is skipped.** Called from a provider, `InvokeAs` and the struct fallback skip the service that provider builds, as `InvokeAll` does. A decorator that resolves its own interface used to fail with `ErrCircularDependency` whenever it picked itself; it now resolves the service it wraps. When the provider's own service is the only match, the error now matches `ErrServiceNotMatch` instead of `ErrCircularDependency`. An alias whose target chain resolves to that service (registered with `As`, `AsNamed` or a chain of them) is skipped too: it used to be selected when registered before the wrapped service, and failed with `ErrCircularDependency`. Aliases of other services stay candidates.

Migration: none for decorators; code that checked for `ErrCircularDependency` in that case checks for `ErrServiceNotMatch`.

**Cycle detection compares services, not names.** A service invoked while another service with the same name, registered in a different scope, is being built (for example, a child scope's `x` invokes a root aggregator that resolves the root's `x`, by name or as an `InvokeAll` member) used to fail with `ErrCircularDependency`. It is now built. A service that really depends on itself still fails as before. Migration: none, unless code relied on that error.

**The struct fallback decides by name (BREAKING, edge cases).** An empty `do:""` tag falls back to resolution by type only when no service is registered under the field type, or when the only one is the service whose provider calls `InvokeStruct`.

- A service registered under the field type that fails with an error matching `ErrServiceNotFound` (for example, its own provider could not find a dependency) used to make the field silently fall back to another assignable service. That error is now returned.
- A decorator registered under the field type (`doger.Provide[Logger]` whose provider calls `InvokeStruct` with a `Logger` field) used to fail with `ErrCircularDependency`; it now receives the service it wraps. With no other match, it still fails with `ErrCircularDependency`.

Migration: fix the failing provider, or register the service you want under the field type's name.

Performance: to pick deterministically, `InvokeAs` considers every match of the nearest scope that has one, instead of stopping at the first one found. Each scope caches, per requested type, its services assignable to it, and resets that cache whenever a service is registered, overridden or removed, so a call no longer scans and reflects over every service. In a scope with 20 services (10 runs alternating with the previous release), a call takes about 0.3µs instead of 1.3µs, with 4 allocations instead of 11. The first call for a type in a scope, and the first after each registration in it, builds that scope's entry.

## The `do:"*"` struct tag injects a collection {#star-struct-tag}

**BREAKING (edge case).** In `InvokeStruct`, the tag value `*` now means "every service assignable to the element type": a `[]E` field receives `InvokeAll[E]`, and a `map[string]E` field receives `InvokeAllMap[E]`. A service literally named `*` can no longer be injected through a struct tag, and `*` on any other field kind is an error.

Migration: rename a service called `*`, or inject it by type.

## Single-service health checks honor HealthCheckTimeout {#per-service-health-check-timeout}

`HealthCheck[T]`, `HealthCheckWithContext[T]`, `HealthCheckNamed` and `HealthCheckNamedWithContext` are now bounded by `InjectorOpts.HealthCheckTimeout` when it is greater than zero, like each check of `injector.HealthCheck()`. They used to run unbounded. `HealthCheckGlobalTimeout` still does not apply to them.

Migration: none if `HealthCheckTimeout` is `0`. Otherwise, a single-service check that runs longer than the timeout now returns an error matching `ErrHealthCheckTimeout`. To keep single-service checks unbounded while `HealthCheckTimeout` bounds `injector.HealthCheck()`, set `HealthCheckSingleTimeout: -1`. A positive `HealthCheckSingleTimeout` gives single-service checks their own bound.

## Failed hooks run when a per-service hook panics during Start {#failed-on-hook-panic}

When a per-service hook (`HookBeforeStart`, `HookAfterStart`, `HookAfterBuild`) panics during the first `Start`, the panic is still re-raised on the calling goroutine, but `Start` now runs the `HookContainerFailed` hooks first, with an error wrapping the panic value. They used to be skipped, although the container was marked failed. A Failed hook that panics meanwhile goes to `HookContainerPanic` when set, and is logged otherwise, so the original panic is the one re-raised.

Migration: none, unless a Failed hook assumed that `Start` had returned. Recover the panic in the per-service hook to turn it into a normal failure.

## A start that Shutdown interrupts always reports it {#abort-cause}

When root `Shutdown` begins during `Start`, `Refresh` or `Scope.Start`, the error they return matches `ErrShutdown`. When the caller had already cancelled the context passed to `StartWithContext`, `RefreshWithContext` or `Scope.StartWithContext`, the error used to match only `context.Canceled`; it now matches both, and its message adds the Shutdown cause once, for example `doger: start aborted: context canceled (start aborted: doger: container is shut down)`.

The `HookContainerFailed` hooks receive an error that matches `ErrShutdown` in these cases too, including when a per-service hook panicked after `Shutdown` began; that error used to wrap only the panic value.

Migration: none for code that checks `errors.Is(err, doger.ErrShutdown)` (named `ErrStartAfterShutdown` before the first release) or `errors.Is(err, context.Canceled)`. Code that compares the message must accept the added text.

## A child scope shutdown waits for a running start {#child-shutdown-waits}

`Scope.Shutdown()` and `Scope.ShutdownWithContext(ctx)` on a child scope now wait for a `Start`, `Refresh` or `Scope.Start` that is starting services of that scope before shutting it down, bounded by `ctx`. They used to run concurrently with it, so a service could start after its scope had been shut down and keep running. A run that begins while the scope shuts down now skips it.

Migration: none for shutdowns that happen after start. Code running during a start (a starter, kind handler, provider or hook) must not synchronously shut down a scope that this start is starting: it would wait for itself. Shut the scope down after the start returned, or bound the wait with `ShutdownWithContext`.

### A child scope shutdown cancels its subtree's running start steps {#child-shutdown-cancels}

A child scope `Shutdown()` or `ShutdownWithContext(ctx)` now also cancels the context of every running start step (a Starter `Start` method or a kind-handler call) of that scope and its descendants, with a cause matching `ErrShutdown`, as root `Shutdown()` cancels a whole run; it used to wait without cancelling, so it could wait forever on a `Start` that stops only when its context ends. The run itself is not cancelled. Such a step is skipped, not a failure: it is not recorded as started, its error does not become the run's error, and the run skips the rest of that scope's subtree and goes on with the other scopes. With `StartTimeout` set, a step that ignores the cancellation is abandoned at the timeout, as before, and is still skipped. This applies to the child scope's own shutdown only: root `Shutdown()` cancels the whole run, and a step of a child scope that it reaches is reported like a root step, a `*StartError` naming it that matches `ErrShutdown` (and `ErrStartTimeout` once abandoned), even when a bounded root `ShutdownWithContext` gave up waiting and went on to shut that scope down. A start step's context is now its own child of the run's context, also without `StartTimeout`; it carries the same values and ends with the run, as before. With a `Logger` set, a cancelled step that returned an error is logged at Debug as `cancelled start step error`, and one that panicked as `panic recovered`.

A service whose step was skipped this way does not count as started for its dependents: the run skips every service that depends on it, directly or through others. A dependent inside the shutting-down scope is skipped silently. A dependent outside it (for example a root service depending on an export of that scope) is skipped too, and the run fails once everything else started, unless another step failed first: with a `*StartError` naming the dependent that matches `ErrShutdown`, or, while the skipped step's abandoned call still runs, with that step's `*StartError` matching `ErrStartTimeout`. When the dependent depends on several skipped services, a still-running one decides the error, then the first by scope name, service name and scope ID (the last tie-break only separates same-named scopes, and is stable within a container only, since scope IDs are random).

A bounded `ShutdownWithContext(ctx)` whose `ctx` ends before the run returns gives up waiting and removes the scope's services, with their dependency edges, while the run still goes on; the scope is detached from its parent. The run then skips the later steps of that scope, and of its descendants, without calling them. A dependent of an export of that scope can no longer reach the source it depended on, so it is skipped once the run skipped any step of that scope or of its descendants, even one it did not depend on, and the run fails with a `*StartError` naming the dependent that matches `ErrShutdown` and names the removed export, for example ``doger: failed to start service `P` in scope `[root]`: dependency `S` (export of scope `c`) was removed by the scope's shutdown: container is shut down: scope `c` is shutting down``; while a skipped step of that subtree has an abandoned call still running, the error is that step's, matching `ErrStartTimeout`. The source scope is told apart by identity: a new scope that reuses the removed scope's name does not make the export's source live again. A bounded shutdown of a descendant of the source scope only does not remove the export: its dependent starts when its own dependencies did. An unbounded `Shutdown()` waits for the run, so it removes nothing before the run returns, and the same dependent starts when its own dependencies did.

Migration: none for Starters that do not depend on their context ending only with the run. A `Refresh`, `Start` or `Scope.Start` that runs while a child scope shuts down can now return an error matching `ErrShutdown` when a service outside that scope depends on one of its services; retry the run once the scope is gone, or stop depending on the scope's export.

## Health reports wait for a component's kind handlers {#health-waits-for-kind-handlers}

`HealthReport()`, `HealthReportWithContext()` and the rounds of a `HealthScheduler` now skip a component until every kind of it that has a handler handled it successfully, as they already skipped a Starter not started yet: the component is listed as `HealthUnknown` and its `HealthCheck` is not called, and a component registered after `Ready` waits for `Refresh` as a Starter does. A failed handler call keeps it unknown. A Runnable whose `Run` has not returned, a stuck one or one abandoned under `StartTimeout` that still runs, used to report healthy; it now reports `HealthUnknown` until its `Run` returned nil. A kind without a handler holds nothing back: its components are checked as before. A kind given its first handler with `Handle` after the run that started a component does not demote that component: it stays checked until the next `Refresh` or `Scope.Start` collects it, and is unknown from then on until that handler handled it. The forced paths, `injector.HealthCheck()`, `HealthCheckWithContext()` and the single-service functions, still check every service, and a container that was never started checks every service too. See [Services whose start is not complete](../service-lifecycle/healthchecker.md#services-whose-start-is-not-complete).

Migration: none for Runnables whose `Run` returns once running, as `Runner` requires. A dashboard or probe that read a stuck or still-starting Runnable as healthy now sees `HealthUnknown`: make `Run` return once the component runs, or use `HealthCheck()` to check every service whatever its start.

## With StartTimeout, a shared instance is never called twice {#start-timeout-shared-instance}

With `InjectorOpts.StartTimeout` set, a clone or a same-instance override never calls a shared instance whose abandoned call still runs. `Clone` and `CloneWithOpts` used to share no start state with their source; they now share the record of abandoned calls by instance, so a clone's `Start`, or a `Refresh` after `OverrideNamedValue` registers the same instance, skips that step instead of calling it a second time. Its dependents are skipped too, in any scope or stage, and the run fails with the step's `*StartError` matching `ErrStartTimeout`. This holds for non-nil pointers to non-zero-size values, non-nil channels and non-nil unsafe pointers; any other instance (a struct or other value, a map, a nil pointer, a pointer to a zero-size type) is tracked per registration only, as before. An instance is recorded only once a call on it is abandoned, so two containers can still call a shared instance at the same time while neither call has been abandoned. See [Slow and stuck start steps](../container/options.md#start-timeout).

Migration: none without `StartTimeout`. With it, a clone's `Start`, or a run after an override, that reaches an instance whose `Start` (or kind-handler call) was abandoned fails with `ErrStartTimeout` until that call returns: make the call return, or register a new instance.

## Concurrent child scope shutdowns join {#child-shutdown-joins}

A `Shutdown()` or `ShutdownWithContext(ctx)` of a child scope that finds a shutdown of the same scope running (another direct call, or root `Shutdown()` reaching it) now waits for it, bounded by `ctx`, and returns a **copy of its report**. Both calls used to walk the scope: `HookScopeClosing` and `HookScopeClosed` ran twice, and each call reported the services the other had already removed as `ErrServiceNotFound`, so `Succeed` was false although every service shut down once. A shutdown of the scope that begins after the running one finished runs again, as before.

Migration: none, unless code shuts a scope down synchronously from its own `HookScopeClosing` or `HookScopeClosed` hook, or from one of its services' `Shutdown` method: that call used to run a nested shutdown and now waits for itself. Shut the scope down from elsewhere, or bound the wait with `ShutdownWithContext`.

## Scope.Start skips a scope that is shutting down {#scope-start-while-closing}

`scope.Start()` on a child scope that is shutting down, or whose ancestor is, now runs no `HookScopeStarting` hook; it starts nothing and returns nil, as before. The Starting hooks used to run, although nothing of the scope was started and `HookScopeStarted` did not run.

Migration: none, unless a Starting hook counted on running for every `scope.Start()` call; use a `HookScopeClosing` hook to observe the shutdown.

### Scope.Start skips the Started hooks when a shutdown cancels its steps {#scope-started-skipped-on-cancel}

`scope.Start()` no longer runs the `HookScopeStarted` hooks when a shutdown of that scope, or of one of its ancestors, begins after `scope.Start()` collected the scope's services. That shutdown [cancels their running start steps](#child-shutdown-cancels); `scope.Start()` skips them and returns nil, and the shutdown waits for it to return, bounded by its context, before its `HookScopeClosing` hooks run. The Started hooks used to run in that case, and the Closing hooks could run during or before them. This also holds when a bounded `ShutdownWithContext` gives up waiting and completes before `scope.Start()` returns. The Started hooks still run when the shutdown is of a descendant scope, which only skips that scope's services, and when a shutdown completed before `scope.Start()` collected the services, as for a `scope.Start()` called after it.

Migration: none, unless a Started hook was used to pair with every Closing hook of a scope shut down while `scope.Start()` ran. A Closing hook now runs without a Started hook in that case: make it tolerate a scope whose Started hooks did not run.

## New exported struct fields {#new-struct-fields}

**BREAKING (unkeyed literals only).** These exported structs gain fields:

- `InjectorOpts`: `StartParallelism`, `HookAfterBuild`, the `HookContainer*` and `HookScope*` hook lists, `HookContainerPanic` and `HealthCheckSingleTimeout`;
- `ExplainServiceDependencyOutput`: `Source`.

Keyed literals (`doger.InjectorOpts{Logger: ...}`) and zero values compile unchanged. An **unkeyed** composite literal of one of these structs no longer compiles. Migration: name the fields.

## Explained dependencies report their source {#explain-source}

Each dependency edge in `ExplainServiceDependencyOutput` now says how it was recorded: `invoked` by a provider, from a `DependsOn` registration option, or `declared` with `DeclareDependency`.

- `String()` prints invoked edges exactly as before, and adds the source to the others: `* cache (depends-on) from scope [root]`, `* job (declared) from scope [root]`.
- The JSON encoding gains a `"source"` field (`"invoked"`, `"depends-on"` or `"declared"`), which decodes back.

Migration: code that parses the text output must accept the ` (depends-on)` and ` (declared)` suffixes; JSON consumers that reject unknown fields must accept `source`.
