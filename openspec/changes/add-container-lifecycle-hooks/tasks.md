# Tasks

Prerequisite: `add-start-lifecycle-and-kinds` is merged, providing `RootScope.Start`/`StartWithContext`, `ErrAlreadyStarted`, `ErrStartAfterShutdown`, kind stages and `Runnable`. Tests live in the root package: `container_hooks_test.go`, `container_hooks_example_test.go`. The package is `doger`.

## 1. Hook registration surface (fields, Add methods, copy, nil handling)

- [ ] 1.1 RED: add `TestContainerHooks_RegistrationOrder` (scenario "Registration order"), `TestContainerHooks_NilIgnored` (scenario "Nil hooks are ignored", 5 rows) and `TestContainerHooks_CloneCopiesHooksNotState` (scenario "Clone copies hooks but not state"). Verify: `go test -race -run 'TestContainerHooks_(RegistrationOrder|NilIgnored|CloneCopiesHooksNotState)' .` fails on the missing `HookContainer*` fields and `AddContainer*Hook` methods.
- [ ] 1.2 GREEN: add the six `InjectorOpts.HookContainer*` fields (design D3), initialize nil slices in `NewWithOpts`, extend `InjectorOpts.copy()`, and add the six `RootScope.AddContainer<State>Hook` methods, with `Add*(nil)` as a no-op. Add the unexported `containerState` and `lifecycleMu` on `RootScope` (D1). Fire Started hooks only as far as needed to make these three tests pass. Verify: the command from 1.1 passes and `make test` is green.
- [ ] 1.3 REFACTOR: *Extract Function* for the six near-identical `Add*` bodies only if lint flags duplication; otherwise "none needed — mirrors existing Add*Hook style". Add doc comments on every new field and method stating firing point, ordering, at-most-once and panic behavior. Verify: `make test` and `make lint` pass.

## 2. Starting hooks and veto

- [ ] 2.1 RED: add `TestContainerStartingHook_Veto` (scenario "Starting hook allows or vetoes Start", 4 rows), `TestContainerStartingHook_ReceivesContext` and `TestContainerStartingHook_CancelledContext`. Verify: `go test -race -run 'TestContainerStartingHook_' .` fails on assertions (veto not honored, provider still built).
- [ ] 2.2 GREEN: implement the guard transition `configured → starting` and `runStartingHooks` (D1, D4), called before discovery. Wrap a veto as `DI: container start aborted: %w`. Verify: the 2.1 command passes and `make test` is green.
- [ ] 2.3 REFACTOR: *Extract Function* `transitionTo(from, to)` for the guarded state change if `Start` grows past one screen; otherwise "none needed". Verify: `make test` and `make lint` pass.
- [ ] 2.4 Add `ExampleRootScope_AddContainerStartingHook`, showing fail-fast config validation that vetoes `Start`. Verify: `go test -run ExampleRootScope_AddContainerStartingHook .` passes.

## 3. Started and Ready hooks

- [ ] 3.1 RED: add `TestContainerHooks_StartOrder` (scenario "Hook order relative to the start pipeline") and `TestContainerHooks_EmptyContainer` (scenario "Started and Ready fire for an empty container"). Verify: `go test -race -run 'TestContainerHooks_(StartOrder|EmptyContainer)' .` fails on the recorded order.
- [ ] 3.2 GREEN: fire Started hooks after Starters (state `started`), and Ready hooks after the `StageAfterStart` handlers (state `ready`) (D5, D7). Verify: the 3.1 command passes and `make test` is green.
- [ ] 3.3 REFACTOR: *Extract Function* `runObserverHooks` shared by Started and Ready (D4). Verify: `make test` and `make lint` pass.
- [ ] 3.4 Add `ExampleRootScope_AddContainerReadyHook`, which signals readiness after runners complete. Verify: `go test -run ExampleRootScope_AddContainerReadyHook .` passes.

## 4. Failed hooks and guard errors

- [ ] 4.1 RED: add `TestContainerFailedHook_FailurePoint` (scenario "Failure point decides which hooks run", 6 rows) and `TestContainerFailedHook_GuardErrors` (scenario "One-shot guard errors do not fire Failed", 3 rows). Verify: `go test -race -run 'TestContainerFailedHook_' .` fails (Failed hook not called, or Ready called after failure).
- [ ] 4.2 GREEN: on any error after the guard, set state `failed`, run Failed hooks with the exact returned error, then return it. Guard errors (`ErrAlreadyStarted`, `ErrStartAfterShutdown`) return before any hook runs. Verify: the 4.1 command passes and `make test` is green.
- [ ] 4.3 REFACTOR: *Consolidate Duplicate Conditional Fragments*: route every failure exit of `Start` through one deferred `fail(err)` path. Verify: `make test` and `make lint` pass.

## 5. Closing and Closed hooks on root shutdown

- [ ] 5.1 RED: add `TestContainerClosingClosedHooks_Order` (scenario "Closing and Closed around service shutdown from every state", 3 rows), `TestContainerClosedHook_ReportErrors` and `TestContainerHooks_ShutdownOnSignalsWithContext`. Verify: `go test -race -run 'TestContainer(ClosingClosedHooks_Order|ClosedHook_ReportErrors|Hooks_ShutdownOnSignalsWithContext)' .` fails on the recorded order and counts.
- [ ] 5.2 GREEN: in `RootScope.ShutdownWithContext`, add the guard and transition to `closing`, Closing hooks before `s.self.ShutdownWithContext`, and Closed hooks with the same report pointer after the health-check pool stops (D6). Verify: the 5.1 command passes and `make test` is green. The existing `root_scope_test.go` shutdown tests must pass unchanged.
- [ ] 5.3 REFACTOR: none needed if `ShutdownWithContext` stays under ~25 lines; otherwise *Extract Function* `closeContainer(ctx)`. Verify: `make test` and `make lint` pass.

## 6. Panic recovery

- [ ] 6.1 RED: add `TestContainerHooks_PanicRecovered` (scenario "Panicking hook by lifecycle point", 6 rows), with a `Logf` recorder via `NewWithOpts`. Verify: `go test -race -run TestContainerHooks_PanicRecovered .` fails (the panic escapes the test).
- [ ] 6.2 GREEN: add `defer recover()` per hook call. A Starting-hook panic converts to an abort error. An observer-hook panic is logged via `Logf` and the loop continues (D4). Verify: the 6.1 command passes and `make test` is green.
- [ ] 6.3 REFACTOR: *Extract Function* `callHookSafely(point string, fn func()) (panicErr error)` shared by both runners. Verify: `make test` and `make lint` pass.

## 7. At most once, concurrency and scope boundaries

- [ ] 7.1 RED: add `TestContainerHooks_ConcurrentStart` (scenario "Concurrent Start calls", 50 goroutines), `TestContainerHooks_ConcurrentShutdown` (scenario "Repeated and concurrent Shutdown calls", 2 rows), `TestContainerHooks_ReentrantStart` (scenario "Hook calling Start re-entrantly"), `TestContainerHooks_ChildScopeShutdown` (scenario "Child scope shutdown fires nothing") and `TestContainerHooks_NoHooksUnchanged` (scenario "No hooks registered leaves behavior unchanged", 3 rows). Use `testWithTimeout` on the concurrent and re-entrant tests. Verify: `go test -race -count=5 -run 'TestContainerHooks_(ConcurrentStart|ConcurrentShutdown|ReentrantStart|ChildScopeShutdown|NoHooksUnchanged)' .` fails on hook counts or the race detector.
- [ ] 7.2 GREEN: make every transition check-and-set under `lifecycleMu`, released before running hooks (D1). A second root Shutdown skips the container hooks but still runs the service shutdown. Verify: the 7.1 command passes with `-race -count=5`, goleak reports no leaks, and `make test` is green.
- [ ] 7.3 REFACTOR: none expected. Confirm no lock is held while hooks run, using a `gopls` call-hierarchy review of `lifecycleMu.Lock` sites. Verify: `make test` and `make lint` pass.

## 8. Documentation

- [ ] 8.1 Add `ExampleInjectorOpts_containerHooks`, showing all six hooks configured through `InjectorOpts` and the printed order for a start and shutdown. Verify: `go test -run ExampleInjectorOpts_containerHooks .` passes.
- [ ] 8.2 Add a "Container lifecycle hooks" page under `docs/docs/service-lifecycle/` covering the state diagram, firing points, veto, panics, at-most-once, and root-only behavior. Describe "Started" as the "refreshed" point in common DI practice. Verify: `npm run build` in `docs/` succeeds.

## 9. Integration checks

- [ ] 9.1 Full run: `make test` and `make lint` pass, and `go test -race -count=3 -run 'TestContainer' .` is stable. Verify: all green, goleak clean.
- [ ] 9.2 Traceability: every scenario and Examples row in `specs/container-lifecycle-hooks/spec.md` maps to a passing test listed in design.md's Test Strategy. Verify: `go test -v -run 'TestContainer|ExampleRootScope_AddContainer|ExampleInjectorOpts_containerHooks' .` lists them all as PASS.

## Workflow follow-up

- Archive the change after review once `make test` passes and the scenario-to-test mapping is confirmed.
- Phase 2 candidates: per-scope container hooks, a public container state accessor, and a test that hooks honor shutdown deadlines using `testing/synctest`.
