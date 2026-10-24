# Proposal

## Why

All existing hooks fire per service (registration, invocation, shutdown, and per-service start from `add-start-lifecycle-and-kinds`). Nothing fires once for the whole container when it changes state. Consumers therefore have no single place for:
- container-wide work: fail-fast config validation before start, "ready" signals to orchestrators, metrics and logging of boot and shutdown
- reacting to a failed start

Common DI practice covers this with container lifecycle events such as "refreshed" and "ready". Here, "refresh" is a **lifecycle point**, not an operation: the container starts once, and there is no runtime rebuild.

## What Changes

- Define the container states that `Start` and `Shutdown` move through: configured → starting → started → ready → closing → closed, plus a start-failed branch that ends in closing once the caller runs `Shutdown`.
- Add container-level hooks to `InjectorOpts`, as named fields with typed payloads, mirroring the existing `Hook*` style. The `Add*Hook` methods live on `RootScope`.
  - `HookContainerStarting`: `func(ctx, *RootScope) error`. Runs before discovery. A non-nil error **aborts `Start`** and is returned wrapped.
  - `HookContainerStarted`: `func(ctx, *RootScope)`. Runs after kinds are handled and Starters are up. This is the "refreshed" point in common DI practice.
  - `HookContainerReady`: `func(ctx, *RootScope)`. Runs after post-start handlers such as runnables complete.
  - `HookContainerFailed`: `func(ctx, *RootScope, error)`. Runs when `Start` fails, before the error is returned to the caller. It only observes; cleanup stays with the caller.
  - `HookContainerClosing`: `func(ctx, *RootScope)`. Runs when root `Shutdown` begins. It cannot veto.
  - `HookContainerClosed`: `func(ctx, *RootScope, *ShutdownReport)`. Runs after root `Shutdown` finishes.
- Hooks fire once per root container, not per child scope. They run synchronously, in registration order, on the goroutine that called `Start` or `Shutdown`.
- A panic inside a hook is recovered and turned into an error. Only a "Starting" hook's error aborts `Start`; an error from any other hook is reported but does not abort.
- Container hooks run on the existing `ShutdownOnSignals` path as well, because it ends in root `Shutdown`.
- No exported identifiers are removed or changed. This change is purely additive.

## Capabilities

### New Capabilities

- `container-lifecycle-hooks`: container-level state transitions and the global hooks fired on them, including veto semantics, ordering, panic handling, and the one-shot start contract as seen by hooks.

### Modified Capabilities

None. `container-start` is introduced by `add-start-lifecycle-and-kinds`; this change adds requirements in its own capability rather than modifying that one.

## Impact

- `injector.go` (new `InjectorOpts` fields and `copy()`), `root_scope.go` (`Add*Hook` methods; firing points in `Start` and `Shutdown`), docs lifecycle page, and `Example*` tests.
- Depends on `add-start-lifecycle-and-kinds`.
- Phase 2, out of scope: per-scope container hooks for child scopes, and a re-runnable `Refresh` operation.

## Defaults & Control

| Behavior | Zero-config default | Override / opt-out |
|---|---|---|
| Container hooks | None registered, so no behavior change | Register via `InjectorOpts` fields or `RootScope.Add*Hook` |
| Veto on start | Only `HookContainerStarting` can abort `Start` | Return `nil` to allow; other hooks only observe, so shutdown can never be blocked |
| Hook panics | Recovered and converted to an error | Hooks that want to crash the process can do so explicitly (e.g. `os.Exit`); none needed beyond that |
| Scope granularity | Once per root container | Per-scope hooks deferred to phase 2 |
