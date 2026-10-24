# Design

## Context

See proposal.md (Why, What Changes) and `specs/container-lifecycle-hooks/spec.md` for the required behavior. This change builds on the Start contract of `add-start-lifecycle-and-kinds`:
- `RootScope.Start` / `StartWithContext` only
- `ErrAlreadyStarted`, `ErrStartAfterShutdown`
- `StageBeforeStart` / `StageAfterStart` kind handlers and the built-in `Runnable` handler
- per-service `HookBeforeStart` / `HookAfterStart`

Facts in the current code that shape the approach:
- **Hook storage.** `InjectorOpts` (`injector.go`) holds every hook list as an exported slice. `NewWithOpts` (`root_scope.go`) replaces nil slices with empty ones. `InjectorOpts.copy()` duplicates each slice, and `RootScope.Clone` / `CloneWithOpts` use it. The `RootScope.Add*Hook` methods `append` to `s.opts`. Today's hook lists are not mutex-protected; they are assumed to be configured before use.
- **Shutdown entry points.** `RootScope.Shutdown()` delegates to `ShutdownWithContext(context.Background())`, which shuts down the root's `*Scope` (`s.self`) and then stops the health-check pool. `ShutdownOnSignals` and `ShutdownOnSignalsWithContext` end in `RootScope.ShutdownWithContext` (`root_scope.go:342,348`).
- **Calls that bypass root shutdown.** `Scope.ShutdownWithContext` (`scope.go:369`) shuts down children and then services, and has no notion of "root". `virtualScope.Shutdown` delegates to its `*Scope`. So a shutdown reached through a provider's injector or a child `*Scope` never passes through `RootScope.ShutdownWithContext`.
- **Repeat shutdowns.** A second root `Shutdown` today iterates an empty service map and returns an empty, successful report.
- **Logging.** `InjectorOpts.Logf` is always non-nil after `NewWithOpts` (`noOpLogf` default).

## Goals / Non-Goals

**Goals:**
- Six container hook lists with typed payloads, registered via `InjectorOpts` or `RootScope.AddContainer<State>Hook`.
- A small internal state machine on `RootScope` guaranteeing each transition, and therefore each hook list, fires at most once, safely under concurrent callers.
- Panic isolation: a misbehaving hook never crashes `Start` or `Shutdown`, and never blocks shutdown.

**Non-Goals:**
- Exposing the container state publicly (no `State()` accessor in this change).
- Per-scope container hooks, or firing hooks from child `Scope` or `virtualScope` shutdown (phase 2).
- A `Refresh` operation, or restarting a failed container.
- Changing per-service hooks or the Start pipeline itself (owned by `add-start-lifecycle-and-kinds`).

## Decisions

**D1. Unexported container state, guarded by a mutex held only for transitions.**
`RootScope` gains:
- `lifecycleMu sync.Mutex`
- `state`, extending the enum from `add-start-lifecycle-and-kinds` D6. Its `stateIdle` plays the role of "configured" here, and its `stateShutdown` is split into `stateClosing`/`stateClosed`. This change adds `stateReady` and `stateFailed`. Full set: `stateIdle`, `stateStarting`, `stateStarted`, `stateReady`, `stateFailed`, `stateClosing`, `stateClosed`.

`Start` locks, checks and moves the state, then **unlocks before running any hook or service code**. This avoids deadlock when a hook re-enters `Start` or `Shutdown`.
- `Start` from `configured` moves to `starting`.
- `Start` from `starting`/`started`/`ready`/`failed` returns `ErrAlreadyStarted`.
- `Start` from `closing`/`closed` returns `ErrStartAfterShutdown`.
- `Shutdown` from any non-closing state moves to `closing`. From `closing`/`closed` it fires no container hooks and still performs the existing, idempotent service shutdown.
- *Alternative:* `sync.Once` per hook list. Rejected: it cannot express the cross-dependency between Start and Shutdown (`ErrStartAfterShutdown`), or "Started only if reached".
- *Alternative:* hold the mutex for the whole of `Start`. Rejected: a hook calling `Start` would deadlock.

The guard errors `ErrAlreadyStarted` and `ErrStartAfterShutdown` come from `add-start-lifecycle-and-kinds`. This change only uses that guard as the single gate for container hooks.

**D2. Shutdown during an in-flight Start follows `add-start-lifecycle-and-kinds` D6.**
Root `Shutdown` waits for the in-flight Start (its `startDone`) or for its own context to end, whichever is first, and only then fires Closing and shuts services down. A Start that finishes first fires its Started/Ready or Failed hooks before Closing. This design adds only these guarantees:
- each hook list fires at most once;
- no data race;
- no deadlock.

The wait is bounded by the Shutdown context, so a stuck Starter cannot block shutdown forever ("shutdown can never be blocked").

**D3. Hook storage and registration follow the existing style.**
- New `InjectorOpts` fields:
  - `HookContainerStarting []func(context.Context, *RootScope) error`
  - `HookContainerStarted`, `HookContainerReady`, `HookContainerClosing` `[]func(context.Context, *RootScope)`
  - `HookContainerFailed []func(context.Context, *RootScope, error)`
  - `HookContainerClosed []func(context.Context, *RootScope, *ShutdownReport)`
- `NewWithOpts` initializes nil slices to empty, as it does for the existing hooks. `copy()` duplicates them, so clones get the hooks (spec: "Clone copies hooks but not state").
- `RootScope.AddContainerStartingHook`, `AddContainerStartedHook`, `AddContainerReadyHook`, `AddContainerFailedHook`, `AddContainerClosingHook` and `AddContainerClosedHook` append, matching today's `Add*Hook`. `Add*(nil)` is a no-op, and nil entries in the slices are skipped at run time.
- *Alternative:* a single `HookContainerLifecycle` listener with an event enum. Rejected during exploration: payloads would become optional fields.

**D4. Runner helpers, one per signature, with panic recovery.**
- `runStartingHooks(ctx) error` stops at the first error or panic. A panic is converted with `fmt.Errorf("DI: container starting hook panicked: %v", r)`.
- `runObserverHooks(...)` runs every hook in turn. Each call is wrapped in `defer recover()`, and a recovered value is logged via `s.opts.Logf("DI: container %s hook panicked: %v", point, r)`.
- Hooks run on the caller's goroutine.
- *Alternative:* return a joined error from `Start` when an observer hook panics. Rejected: observers must not change the outcome (spec "Hook panics are recovered").

**D5. Firing points inside `Start` (`root_scope.go`):**

```
guard (D1) -> Starting hooks (veto?) -> discovery + StageBeforeStart handlers
          -> Starters -> state=started, Started hooks
          -> StageAfterStart handlers (incl. Runnable) -> state=ready, Ready hooks -> return nil
any error after guard -> state=failed, Failed hooks(err) -> return err
```

- A Starting veto returns `fmt.Errorf("DI: container start aborted: %w", err)`, which `errors.Is` can match against the hook's error. Failed hooks receive exactly the error that `Start` returns.
- No new sentinel error is added. Consumers match their own error with `errors.Is`.

**D6. Firing points inside `RootScope.ShutdownWithContext`:**
1. Guard (D1).
2. If this call is the first, run the Closing hooks.
3. Run the existing `s.self.ShutdownWithContext(ctx)`.
4. Stop the health-check pool (existing deferred step).
5. If this call is the first, set `state=closed` and run the Closed hooks with the report pointer that will be returned.

The Closed hooks run before the function returns. The health-check pool is stopped before the Closed hooks run, so Closed observes a fully shut-down container.

**D7. Ready means after the `StageAfterStart` handlers.** The built-in `Runnable` handler is one of them. If it or any other after-start handler fails, Started has already fired, Failed fires, and Ready never does.

## Risks / Trade-offs

- [A hook registers more hooks during a run] → Each list is copied into a local slice before iterating, so additions made during a run take effect on the next transition only. Since transitions happen once, those additions never fire. This is documented on the `Add*` methods.
- [`Add*Hook` racing with `Start`/`Shutdown` on another goroutine] → Same as today's `Add*Hook`: registration is configuration-time only. Documented, and not made concurrent-safe in this change, to stay consistent with the existing hooks.
- [A slow or blocking Closing/Closed hook delays process exit] → Accepted. Hooks are synchronous by design and receive the shutdown context so they can honor deadlines.
- [The Shutdown deadline expires while Start is still running (D2)] → Shutdown proceeds best-effort, as specified in `add-start-lifecycle-and-kinds` D6. Here only at-most-once firing and race-freedom are guaranteed and tested.
- [Root shutdown through `virtualScope` or the root's `*Scope` skips the hooks] → Documented. Only `RootScope` methods are container-level.

## Public API & Defaults

Added, non-breaking:
- `InjectorOpts` fields: `HookContainerStarting`, `HookContainerStarted`, `HookContainerReady`, `HookContainerFailed`, `HookContainerClosing`, `HookContainerClosed` (signatures in D3).
- `RootScope` methods: `AddContainerStartingHook`, `AddContainerStartedHook`, `AddContainerReadyHook`, `AddContainerFailedHook`, `AddContainerClosingHook`, `AddContainerClosedHook`.

Defaults:
- No hooks are registered, so `Start` and `Shutdown` behave exactly as without this change.
- Only Starting can veto. Observer panics are logged via `Logf` (default no-op).
- Container hooks fire at most once per container.

Override:
- Consumers register hooks.
- To fail hard on any hook problem, a consumer's own hook can `os.Exit`, or record the error and act after `Start` returns.

Existing behavior change: a second root `Shutdown` still performs the idempotent service shutdown, but now fires no container hooks. There were no container hooks before this change, so this is not observable to existing consumers. No breaking change.

## Test Strategy

| Scenario | Test | Kind |
|----------|------|------|
| Starting hook allows or vetoes Start (4 rows) | `container_hooks_test.go: TestContainerStartingHook_Veto/<row>` | table |
| Starting hook receives the Start context | `TestContainerStartingHook_ReceivesContext` | unit |
| Starting hook can stop on a cancelled context | `TestContainerStartingHook_CancelledContext` | unit |
| Hook order relative to the start pipeline | `TestContainerHooks_StartOrder` | unit |
| Started and Ready fire for an empty container | `TestContainerHooks_EmptyContainer` | unit |
| Failure point decides which hooks run (6 rows) | `TestContainerFailedHook_FailurePoint/<row>` | table |
| One-shot guard errors do not fire Failed (3 rows) | `TestContainerFailedHook_GuardErrors/<row>` | table |
| Closing and Closed around service shutdown from every state (3 rows) | `TestContainerClosingClosedHooks_Order/<row>` | table |
| Closed hook sees shutdown errors in the report | `TestContainerClosedHook_ReportErrors` | unit |
| Signal-driven shutdown fires the hooks | `TestContainerHooks_ShutdownOnSignalsWithContext` | unit |
| Registration order | `TestContainerHooks_RegistrationOrder` | unit |
| Nil hooks are ignored (5 rows) | `TestContainerHooks_NilIgnored/<row>` | table |
| Clone copies hooks but not state | `TestContainerHooks_CloneCopiesHooksNotState` | unit |
| Panicking hook by lifecycle point (6 rows) | `TestContainerHooks_PanicRecovered/<row>` | table |
| Concurrent Start calls | `TestContainerHooks_ConcurrentStart` (50 goroutines, `testWithTimeout`) | race |
| Repeated and concurrent Shutdown calls (2 rows) | `TestContainerHooks_ConcurrentShutdown/<row>` | race |
| Hook calling Start re-entrantly | `TestContainerHooks_ReentrantStart` (`testWithTimeout`) | unit |
| Child scope shutdown fires nothing | `TestContainerHooks_ChildScopeShutdown` | unit |
| No hooks registered leaves behavior unchanged (3 rows) | `TestContainerHooks_NoHooksUnchanged/<row>` | table |
| Docs: hook registration and veto | `container_hooks_example_test.go: ExampleRootScope_AddContainerStartingHook`, `ExampleRootScope_AddContainerReadyHook`, `ExampleInjectorOpts_containerHooks` | example |

**Hot paths and their coverage:**
- **Lifecycle (Start and Shutdown entry points).** Every transition and its guard is covered by the Veto, FailurePoint, GuardErrors, Order and Closing/Closed tests. The existing `di_lifecycle_test.go` and `root_scope_test.go` shutdown tests must stay green unchanged.
- **Concurrency.**
  - `ConcurrentStart` and `ConcurrentShutdown` run under `-race`, with goleak via `TestMain` (`main_test.go`).
  - `ReentrantStart` uses `testWithTimeout` to catch deadlocks.
  - Deterministic timing is not needed: there is no timer or deadline logic in this change. `testing/synctest` is therefore not required. It is reserved for a later test that hooks honor shutdown deadlines.
- **Panic isolation.** `PanicRecovered` covers all six lists.

**RED expectation:** before GREEN, the tests fail to compile on the missing `HookContainer*` fields and `AddContainer*Hook` methods (missing symbols). After the fields are added but before the firing points exist, they fail on assertions: hooks not called, counts of 0.

## Migration Plan

Additive only. No migration needed. Rollback is a revert.

## Open Questions

None. Choices that could have changed the specs are recorded in Decisions D1–D7.
