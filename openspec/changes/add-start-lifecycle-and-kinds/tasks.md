# Tasks

Prerequisites: `fix-stacktrace-frame-filter`, `rename-module-doger` and `upgrade-go-1-27` are archived. The module is `github.com/kartaladev/doger`, the package is `doger`, the Go floor is 1.27, and `make test` is green before starting.

## 0. Baseline

- [ ] 0.1 Record the benchmark baseline: `go test -run '^$' -bench . -benchmem -count 10 ./bench/ > <scratch>/bench-before.txt`. Verify: the file exists and `make test` passes.

## 1. Registration index

- [ ] 1.1 RED: add `TestRegistrationIndex_Order` (scenario "Registration index": a=0, b=1 in child scope, c=2, override keeps a=0) and `TestRegistrationIndex_ConcurrentUnique` (100 goroutines register distinct names; all indexes unique) in `options_test.go`. Verify: `go test -race -run 'TestRegistrationIndex' .` fails on the missing `getRegistrationIndex`.
- [ ] 1.2 GREEN: add `nextRegIndex atomic.Uint64` to `RootScope`; add `regIndex` and `getRegistrationIndex()` to the four wrappers and to `serviceWrapper`/`serviceWrapperAny`; assign the index in `provide`; reuse the existing index in `override` when the name already exists; copy it in `clone`. Verify: the tests above pass and `make test` is green.
- [ ] 1.3 REFACTOR: *Extract Function* `assignRegistrationIndex(scope, name)` shared by `provide`/`override`. Verify: `make test` and `make lint` pass.

## 2. ProvideOption and variadic registration signatures

- [ ] 2.1 RED: add `TestProvideOptions_Registration` (all 6 rows of "Registration with and without options"), `TestProvideOptions_SignatureHasVariadic` ("Function values of the old signature no longer compile", asserted via `reflect.TypeOf(...).IsVariadic()` for all 18 helpers) and the non-kind rows of `TestProvideOptions_Constructors` (`AsStarter`, `DependsOn[T]`, `DependsOnNamed`). Verify: `go test -race -run 'TestProvideOptions_(Registration|SignatureHasVariadic|Constructors)' .` fails on the missing symbols.
- [ ] 2.2 GREEN: add `options.go` with `serviceOptions`, `type ProvideOption func(*serviceOptions)`, `AsStarter`, `DependsOn[T]` and `DependsOnNamed`. Add `opts ...ProvideOption` to `Provide*`, `Override*`, `Lazy*`, `Eager*` and `Transient*`. Thread the options through `provide`/`override` into a new `opts` field on each wrapper (`getOptions()`, aliases return empty). Skip nil options. Verify: the tests above pass and `make test` is green, with existing tests unchanged.
- [ ] 2.3 REFACTOR: *Introduce Parameter Object*. Collapse `provide`/`override` option application into `buildServiceOptions(opts []ProvideOption) *serviceOptions`. Update godoc on every changed function, including an "Options" paragraph. Verify: `make test` and `make lint` pass, and `go doc doger.Provide` shows the variadic parameter.

## 3. Option validation, combination, clone, override and alias

- [ ] 3.1 RED: add `TestProvideOptions_Repeated` (non-kind rows: duplicate `DependsOnNamed` collapsed, `AsStarter` twice), `TestProvideOptions_Invalid` (rows: `cfg` concrete without Start + `AsStarter`, `srv` transient + `AsStarter`, `life` interface + `AsStarter` succeeds, `req` transient + `DependsOnNamed` succeeds, `srv` eager value + `AsStarter` succeeds) and `TestProvideOptions_CloneOverrideAlias` (all 5 rows). Verify: `go test -race -run 'TestProvideOptions_(Repeated|Invalid|CloneOverrideAlias)' .` fails on assertions or missing panics.
- [ ] 3.2 GREEN: implement `validateServiceOptions(name, reflectType, serviceType, opts)`, called from `provide`/`override`, with the exact spec panic messages. Collapse duplicate dependencies. Copy `opts` in each wrapper's `clone`. Verify: the tests above pass and `make test` is green.
- [ ] 3.3 REFACTOR: *Extract Function* for each validation rule (`validateStarterOpt`, `validateTransientOpts`) so the kind rule can be added in group 12 without branching. Verify: `make test` and `make lint` pass.

## 4. Static declared dependencies (DependsOn)

- [ ] 4.1 RED: in `dependency_test.go`, add:
  - `TestDependsOn_BuildOrder` (3 rows of "Declared dependency built before the dependent")
  - `TestDependsOn_EagerTarget`
  - `TestDependsOn_AncestorScope`
  - `TestDependsOn_ConcurrentSingleBuild`
  - `TestDependsOn_Errors` (5 rows of "Missing or circular declared dependency")
  - `TestEager_NoOptionsNeverErrors` (compatibility)

  Verify: `go test -race -run 'TestDependsOn|TestEager_NoOptionsNeverErrors' .` fails because dependencies are not built first and no edges are recorded.
- [ ] 4.2 GREEN: before calling the provider in `serviceLazy.build` and `serviceTransient.getInstance`, invoke each declared name through `invokeAnyByName` on the received virtual scope. In `serviceEager.getInstance`, resolve the names once with a `sync.Once` that stores the error. Verify: the tests above pass and `make test` is green.
- [ ] 4.3 REFACTOR: *Extract Function* `resolveDeclaredDependencies(i Injector, opts *serviceOptions) error`, shared by the three wrappers. Add `ExampleDependsOn`. Verify: `make test`, `make lint` and `go test -run ExampleDependsOn .` pass.

## 5. Dynamic declared dependencies and shutdown order

- [ ] 5.1 RED: add `TestDeclareDependency` (7 rows of "Declaring edges dynamically", plus `nil_injector_uses_default_root`), `TestDeclareDependency_Concurrent` and `TestDeclaredEdges_ShutdownOrder` (2 rows of "Scheduler is shut down before the jobs it drives"). Verify: `go test -race -run 'TestDeclareDependency|TestDeclaredEdges_ShutdownOrder' .` fails on the missing `DeclareDependency`.
- [ ] 5.2 GREEN: add `DAG.reachable(from, to ServiceDescription) bool` and a locked `addDependencyChecked` that rejects cycles and self-edges. Implement `DeclareDependency` in `dependency.go` with wrapped `ErrServiceNotFound`/`ErrCircularDependency`. Verify: the tests above pass and `make test` is green, including the existing `dag_test.go`.
- [ ] 5.3 REFACTOR: *Extract Function* `dag.transitiveDependencies(desc)` (DFS shared by `reachable` and group 7's ordering). Add `ExampleDeclareDependency`. Verify: `make test` and `make lint` pass.

## 6. Starter interfaces, detection and one-shot Start

- [ ] 6.1 RED: in `root_start_test.go`, add:
  - `TestStart_Variants` (4 rows)
  - `TestStart_Detection` (7 rows of "Which registrations are started")
  - `TestStart_DisableAutoDetection` (2 rows)
  - `TestStart_Empty`
  - `TestStart_OneShot` (4 rows of "Repeated and late Start calls")
  - `TestStart_LateRegistrationNotStarted`

  Verify: `go test -race -run 'TestStart_(Variants|Detection|DisableAutoDetection|Empty|OneShot|LateRegistrationNotStarted)' .` fails on the missing `Start`, `Starter*`, `ErrAlreadyStarted` and `ErrStartAfterShutdown`.
- [ ] 6.2 GREEN:
  - Add the four `Starter*` interfaces to `di_lifecycle.go`, plus the sentinels to `errors.go` and the two toggles to `InjectorOpts` (and `copy()`).
  - Add `lifecycleMu`/`state` to `RootScope`, and `Start`/`StartWithContext` in `root_start.go`.
  - Discovery: breadth-first scope snapshot, skipping transients and aliases, auto-detecting by registered type or `AsStarter`, sorted by `(depth, regIndex)`.
  - Build each candidate, then start each Starter with `startInstance` (type switch with `recover`). Order by `(depth, regIndex)` for now.
  - Root `ShutdownWithContext` does a check-and-set to `stateShutdown` under `lifecycleMu`. The lock is held only for the transition (D6).

  Verify: the tests above pass and `make test` is green.
- [ ] 6.3 REFACTOR: *Extract Function* `discoverCandidates`, `buildCandidates` and `startStarters`. *Replace Conditional with Polymorphism* is not needed: a type switch mirrors the existing `shutdown`. Add `ExampleRootScope_Start`. Verify: `make test`, `make lint` and `go test -run ExampleRootScope_Start .` pass.

## 7. Dependency-ordered start and deterministic ties

- [ ] 7.1 RED: add `TestStart_DependencyOrder` ("Dependency-ordered build and start"), `TestStart_ScopeTraversal` ("Child scopes are started after their ancestors"), `TestStart_TieBreak` and `TestStart_TieBreak_Stable` (100 repeats), all from "Ties broken by scope depth then registration order". Verify: `go test -race -run 'TestStart_(DependencyOrder|ScopeTraversal|TieBreak)' .` fails on the order assertions.
- [ ] 7.2 GREEN: implement the Kahn topological order over transitive Starter dependencies (`transitiveDependencies`), with `(depth, regIndex)` as tie-break and the defensive fallback. Compute the order after the `StageBeforeStart` point (still a no-op here). Verify: the tests above pass and `make test` is green.
- [ ] 7.3 REFACTOR: *Extract Function* `orderByDependencies(candidates []candidate) []candidate`, generic over candidates so group 12 reuses it for kind components. Verify: `make test` and `make lint` pass.

## 8. Start errors, panics and shutdown after failure

- [ ] 8.1 RED: add `TestStart_FirstFailure` (4 rows of "First failure stops the pipeline"), `TestStart_ShutdownAfterFailure` and `TestStart_AsStarterInstanceWithoutStart`. Verify: `go test -race -run 'TestStart_(FirstFailure|ShutdownAfterFailure|AsStarterInstanceWithoutStart)' .` fails on error wrapping and message assertions.
- [ ] 8.2 GREEN: wrap build and start failures with the D11 formats. Convert recovered panics to errors. Return the spec error for an `AsStarter` instance with no `Start` variant. Stop at the first failure. Verify: the tests above pass and `make test` is green.
- [ ] 8.3 REFACTOR: *Extract Function* `wrapStartError(scope, name, err)` / `recoverAsError(...)`. Verify: `make test` and `make lint` pass.

## 9. Context handling

- [ ] 9.1 RED: add `TestStart_Context` (3 rows of "Cancellation and deadline", inside `synctest.Test`) and `TestStart_NilContext`. Verify: `go test -race -run 'TestStart_(Context|NilContext)' .` fails, either because `b` is started after cancellation or because the nil context panics.
- [ ] 9.2 GREEN: check `ctx.Err()` before building and before each Starter, and wrap it. Pass `ctx` to the context-aware variants. Treat a nil context as `context.Background()`. Verify: the tests above pass and `make test` is green.
- [ ] 9.3 REFACTOR: none needed. The checks are single-line guards at the two loop heads. Verify: `make test` and `make lint` pass.

## 10. Concurrency and Start/Shutdown serialization

- [ ] 10.1 RED: add `TestStart_Concurrent` ("Concurrent Start calls"), `TestStart_ConcurrentInvoke` ("Concurrent Start and Invoke") and `TestStart_ShutdownWaits` ("Shutdown waits for a running Start", inside `synctest.Test`). Verify: `go test -race -count=20 -run 'TestStart_(Concurrent|ConcurrentInvoke|ShutdownWaits)' .` fails or reports a race before the fix, for example if Shutdown proceeds while Start is running.
- [ ] 10.2 GREEN: implement the D6 state machine: short check-and-set transitions under `lifecycleMu`, `startDone` closed when the pipeline ends, and root Shutdown waiting on `startDone` or its own `ctx.Done()`. Never hold the lock while Starters, handlers or hooks run. Verify: the tests above pass with `-race -count=20`, goleak is clean, and `make test` is green.
- [ ] 10.3 REFACTOR: confirm, with a `gopls` call-hierarchy review of the `lifecycleMu.Lock` sites, that no lock is held across pipeline code. Document in the `StartWithContext` godoc that calling `Shutdown` from inside Start deadlocks without a deadline, and that re-entrant `Start` returns `ErrAlreadyStarted`. Verify: `make test` and `make lint` pass.

## 11. Per-service start hooks

- [ ] 11.1 RED: add `TestStart_Hooks` ("Hooks around each Starter"). Verify: `go test -race -run TestStart_Hooks .` fails on the missing `HookBeforeStart`, `HookAfterStart` and `AddBeforeStartHook`.
- [ ] 11.2 GREEN: add the `InjectorOpts` fields (initialized in `NewWithOpts`, copied in `copy()`), the `onBeforeStart`/`onAfterStart` helpers and the `RootScope.Add*StartHook` methods. Call them around `startInstance`. Verify: the test passes and `make test` is green.
- [ ] 11.3 REFACTOR: none needed; this mirrors the shutdown hook helpers. Update the docs "Hooks" page with the start hooks. Verify: `make test` and `make lint` pass.

## 12. Component kinds

- [ ] 12.1 RED: in `kind_test.go`, add:
  - `TestKind_OptIn`, `TestKind_DistinctIdentity`
  - `TestKind_Stage` (2 rows), `TestKind_ReplaceHandler`, `TestKind_NilHandlerPanics`, `TestKind_NoHandler`
  - `TestKind_ComponentOrder`, `TestKind_HandlerFailure` (2 rows)
  - the kind rows of `TestProvideOptions_Constructors`, `TestProvideOptions_Repeated` (duplicate kind panics) and `TestProvideOptions_Invalid` (transient + kind, kind not assignable)

  Verify: `go test -race -run 'TestKind_|TestProvideOptions_' .` fails on the missing `NewKind`, `Kind`, `Component` and `Stage`.
- [ ] 12.2 GREEN: add `kind.go` with `kindID`, `Kind[T,M]` (`NewKind`, `Name`, `With`, `Handle`), `Component`, `Stage` and the erased handler entries on `RootScope` (`kindsMu`). Add the kind validation rules to `validateServiceOptions`. Make kind-bound services discovery candidates. Run the stage handlers at the D7 points, ordering components with `orderByDependencies`. Wrap errors and panics. Verify: the tests above pass and `make test` is green.
- [ ] 12.3 REFACTOR: *Extract Function* `runStage(ctx, stage)`. *Move Function*: move the shared ordering into `ordering.go`. Add `ExampleNewKind` (scheduled-job kind) and `ExampleKind_Handle`. Verify: `make test`, `make lint` and `go test -run 'Example(NewKind|Kind_Handle)' .` pass.

## 13. Handlers declaring dependencies

- [ ] 13.1 RED: add `TestKind_DeclareDependencyInHandler` ("Scheduler handler orders start and shutdown"). Verify: `go test -race -run TestKind_DeclareDependencyInHandler .` fails if the start order is computed before `StageBeforeStart` or if shutdown ignores the edge.
- [ ] 13.2 GREEN: make sure the Starter order is computed after `StageBeforeStart` handlers (D7/D8). Verify: the test passes and `make test` is green.
- [ ] 13.3 REFACTOR: none needed; this only confirms pipeline order. Verify: `make test` and `make lint` pass.

## 14. Built-in Runnable kind

- [ ] 14.1 RED: add `TestRunnable_RunsAfterStarters`, `TestRunnable_Failure`, `TestRunnable_AutoDetection` (2 rows plus `excludes_transient_and_alias`) and `TestRunnable_ReplaceHandler`, plus the `AsRunnable` row of `TestProvideOptions_Constructors`. Verify: `go test -race -run 'TestRunnable_' .` fails on the missing `Runner`, `Runnable` and `AsRunnable`.
- [ ] 14.2 GREEN: add `Runner`, `Runnable`, `AsRunnable` and the default `StageAfterStart` handler, registered in `NewWithOpts`. `EnableRunnerAutoDetection` adds implicit `Runnable` bindings during discovery, excluding transients and aliases. Verify: the tests above pass, goleak is clean, and `make test` is green.
- [ ] 14.3 REFACTOR: none needed beyond reusing `Kind`. Add `ExampleAsRunnable`. Write the docs pages "Starting the container", "Component kinds" and "Declared dependencies". Verify: `make test`, `make lint`, `go test -run ExampleAsRunnable .` and the docs build (`npm run build` in `docs/`) pass.

## 15. Integration checks

- [ ] 15.1 Compare benchmarks: rerun the group-0 command into `bench-after.txt`, then `benchstat bench-before.txt bench-after.txt`. Verify: the invoke and provide benchmarks show `~` (no significant change). Otherwise stop and investigate.
- [ ] 15.2 Run the full suite: `make test` (race + goleak), `make lint`, and `go vet ./... && go test -race ./...` in every workspace module. Verify: all pass.
- [ ] 15.3 Check traceability: every scenario in `specs/**/spec.md` has a passing test named as in design.md's Test Strategy. Verify: a `go test -race -run '<all names>' -v .` listing shows each one PASS.

## Workflow follow-up

- Archive this change after review, then start `add-container-lifecycle-hooks` and `add-collection-invocation`, which build on it.
- Revisit the design Open Questions (clone of replaced handlers, exported `StartError`, and how `ExplainService` should show declared edges) when a consumer needs them.
