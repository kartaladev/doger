# Design

## Context

See proposal.md for the motivation and the pinned API contract, and `specs/` for the behavior. This change is implemented after `rename-module-doger` and `upgrade-go-1-27`, so code samples use package `doger` (module `github.com/kartaladev/doger`) and Go 1.27. The relevant current state of the code:

- **Registration** goes through `provide`/`override` in `di.go`. Both build a wrapper (`serviceLazy`, `serviceEager`, `serviceTransient`, `serviceAlias`) and call `Scope.serviceSet`.
- **Services are stored** in `Scope.services map[string]any`. There is no registration order: map iteration is random, which is why `invokeByGenericType` carries a nondeterminism TODO.
- **Dependency edges.** The `DAG` (`dag.go`) holds `dependencies`/`dependents` keyed by `ServiceDescription{ScopeID, ScopeName, Service}`. Edges are recorded only by `virtualScope.addDependency` while a provider invokes another service. Cycles are detected by `virtualScope.detectCircularDependency` on the invoker chain.
- **Shutdown** (`Scope.ShutdownWithContext`) shuts children first, in parallel. It then repeatedly shuts down the scope's services that have no dependents, in parallel. Shutdown calls every wrapper's `shutdown`; lazy wrappers skip it when not built, eager wrappers always call it.
- **Lifecycle interfaces** live in `di_lifecycle.go`: four `Shutdowner*` and two `Healthchecker*` variants, detected with type switches on the instance.
- **Hooks** are `InjectorOpts` slices plus `RootScope.Add*Hook` methods, copied in `InjectorOpts.copy()`.
- **Clone** (`RootScope.CloneWithOpts`) clones wrappers through `clone(Injector)`. Lazy clones reset the built instance.

## Goals / Non-Goals

**Goals:**
- A container-level `Start` that builds candidates, runs kind handlers, and starts Starters deterministically in DAG order.
- Registration metadata that travels with the wrapper, so override replaces it and clone copies it with no extra bookkeeping.
- A kind/handler extension point that the built-in `Runnable` uses unchanged.
- Zero impact on the invoke hot path for registrations without options.

**Non-Goals:**
- Container-level hooks (`add-container-lifecycle-hooks`).
- Public collection invocation (`add-collection-invocation`).
- Parallel start, scope-level `Start`, post-Start child scopes, an instance-aware post-build hook, and `Refresh`. All are phase 2.
- Changing shutdown's algorithm. Shutdown just sees more edges.

## Decisions

**D1. Metadata lives on the wrapper.**
- Each wrapper type gets an `opts *serviceOptions` field and a `regIndex uint64` field. The unexported `serviceWrapper[T]`/`serviceWrapperAny` interfaces gain `getOptions() *serviceOptions` and `getRegistrationIndex() uint64`.
- Aliases always return empty options. `Override*` builds a new wrapper, which naturally replaces the options, and copies the old wrapper's `regIndex` when the name already exists. `clone()` copies both fields.
- *Alternative:* side maps on `Scope` (`options map[string]*serviceOptions`). Rejected: these have to be kept in sync with `services` on override, shutdown removal and clone, which is three more places to get wrong.

**D2. `ProvideOption` is `func(*serviceOptions)`, validated in `provide`/`override`.**
- An option only records intent: `starter`, `dependsOn []string`, `kinds []kindBinding{id *kindID, meta any, accepts func(reflect.Type) bool}`.
- After building the wrapper, `provide` validates the collected options against `getReflectType()` and `getServiceType()`, then panics with the spec messages (transient plus starter/kind, a kind not assignable, a duplicate kind, a concrete type with no `Start*`).
- Nil options are skipped. The option function type is unexported (`type ProvideOption func(*serviceOptions)` with an unexported parameter type), so consumers cannot build their own.
- *Alternative:* an interface with an unexported method. This is equivalent; the function is simpler.

**D3. Registration index comes from an atomic counter on `RootScope`.** `nextRegIndex atomic.Uint64` lives on `RootScope` and is assigned in `provide`. Child scopes share it through `rootScope`, so indexes are unique across the container.

**D4. Declared dependencies are resolved inside the build path.**
- Before calling the provider, `serviceLazy.build` and `serviceTransient.getInstance` invoke each `dependsOn` name through `invokeAnyByName` on the virtual scope they already receive. This reuses circular-dependency detection, DAG edge recording and invocation hooks unchanged.
- `serviceEager.getInstance` resolves its declared dependencies once, on first invocation, guarded by a `sync.Once` that stores the error. Eager dependents therefore get edges too.
- `DependsOn[T]()` is `DependsOnNamed(NameOf[T]())`.
- *Alternative:* record declared edges at registration. Rejected: the target may not be registered yet, and registration cannot return an error.

**D5. `DeclareDependency` writes an edge after a reachability check.**
- It resolves both names with `serviceGetRec` and returns a wrapped `ErrServiceNotFound` if either is missing.
- Under the DAG write lock, a new `DAG.reachable(from, to)` (DFS over `dependencies`) checks whether `dependency` already reaches `dependent`. If so it returns a wrapped `ErrCircularDependency` naming the path. Otherwise `addDependency`, which is already idempotent because it is map-based.
- Self-edges are rejected as cycles.

**D6. Start state and serialization on `RootScope`.**
- `lifecycleMu sync.Mutex` plus `state` (`stateIdle`, `stateStarting`, `stateStarted`, `stateShutdown`) and `startDone chan struct{}`.
- **The mutex is held only for check-and-set transitions, never while Starters, handlers or hooks run.** This keeps re-entry safe: `add-container-lifecycle-hooks` relies on a hook being able to call `Start` without deadlocking.
- `StartWithContext` locks `lifecycleMu`.
  - If the state is not idle, it unlocks and returns `ErrAlreadyStarted` or `ErrStartAfterShutdown` **immediately**, without waiting. This covers concurrent and re-entrant callers.
  - Otherwise it sets `stateStarting`, creates `startDone` and unlocks. Then it runs the pipeline, and finally sets `stateStarted` and closes `startDone`, whether the pipeline succeeded or failed. A failed Start is therefore still "started".
- `RootScope.ShutdownWithContext` locks, reads the state and sets `stateShutdown`, then unlocks.
  - If a Start was in flight, it waits on `startDone` **or** its own `ctx.Done()`, whichever comes first, before shutting services down. Normally a running Start therefore finishes first (scenario "Shutdown waits for a running Start").
  - A stuck Starter cannot block shutdown beyond the caller's deadline. If the deadline wins, shutdown proceeds while Start is still running; this best-effort case is documented.
- `Scope.ShutdownWithContext` on a child scope is unaffected.
- A nil context becomes `context.Background()`.
- *Alternative:* a `sync.Once` for Start. Rejected: it cannot express "Shutdown waits for Start" or "Start after Shutdown".
- *Alternative:* hold `lifecycleMu` for the whole pipeline. Rejected: a hook or handler calling `Start` would deadlock, and a stuck Starter would block `Shutdown` with no escape.

**D7. The pipeline (`root_start.go`).**
1. `ctx.Err()` check.
2. **Discover.** Snapshot the scope tree breadth-first, root first, recording depth. For each non-transient, non-alias wrapper, mark it a candidate if any of these hold:
   - auto Starter: the registered type is assignable to one of the four `Starter*` interfaces (`typeCanCastToType`) and `!DisableStarterAutoDetection`;
   - `opts.starter`;
   - it has any kind binding;
   - `EnableRunnerAutoDetection` is set and the type is assignable to `Runner` (implicit `Runnable` binding).

   Sort candidates by `(depth, regIndex)`.
3. **Build.** For each candidate, `ctx.Err()`, then `invokeAnyByName(scope, name)`. Errors are wrapped as `DI: failed to build service \`%s\` in scope \`%s\`: %w`.
4. **`StageBeforeStart` handlers** (D9).
5. **Order Starters** (D8), then for each one:
   - check `ctx.Err()`;
   - run `HookBeforeStart`;
   - `startInstance(ctx, instance)`: a type switch over the four variants with `recover`. An `AsStarter` instance with no variant yields the spec error;
   - run `HookAfterStart(err)`.

   The first error stops the loop.
6. **`StageAfterStart` handlers.**

`add-container-lifecycle-hooks` inserts its Starting, Started and Ready points between steps 1, 5 and 6 at exactly those boundaries.

**D8. Start order is a Kahn topological sort over transitive Starter dependencies.**
- For each Starter, compute which other Starters it reaches through `DAG.dependencies`, using a DFS shared with D5.
- Repeatedly emit the ready Starter with the smallest `(depth, regIndex)`.
- Cycles cannot exist: build-time detection and D5 prevent them. If one somehow remained, the defensive fallback emits the rest in `(depth, regIndex)` order.
- Computed after `StageBeforeStart`, so handler-declared edges count.
- *Alternative:* start in build-completion order (`orderedInvocation`). Rejected: it ignores declared edges added by handlers and is per-scope only.

**D9. Kinds.**
- `Kind[T, M]` is `struct{ id *kindID }`, where `kindID{name string}`. `NewKind` allocates a new `kindID`, so equal names stay distinct.
- `With(meta)` adds a `kindBinding` whose `accepts` is `typeCanCastToType(t, reflect.TypeFor[T]())`.
- `Handle(root *RootScope, stage Stage, h func(context.Context, Component[T, M]) error)` stores, under `RootScope.kindsMu`, an erased entry `{id, stage, run func(ctx, *Scope, string, any, any) error}`. The `run` closure asserts `instance.(T)` and `meta.(M)`; both are safe because registration validated them.
- Entries are a slice. Replacement keeps the original slot, which gives "order of first `Handle`". A nil handler panics.
- Per stage, iterate entries in order. Each gets its candidates in D8 order generalized to all candidates (dependency, depth, regIndex). Every call is wrapped with `recover` and errors are formatted as `DI: kind \`%s\` failed for service \`%s\` in scope \`%s\`: %w`.
- The type `Stage int` has `StageBeforeStart` and `StageAfterStart`.
- `Component[T, M]` is the exported struct `{Scope *Scope; Name string; Instance T; Meta M}`.
- *Alternative:* a `Handler[T, M]` interface. Rejected: a func type is simpler, and a consumer that wants an interface can pass a method value.

**D10. `Runnable` is an ordinary kind.**
- `var Runnable = NewKind[Runner, struct{}]("runnable")` and `func AsRunnable() ProvideOption { return Runnable.With(struct{}{}) }`.
- `NewWithOpts` registers the default handler (`StageAfterStart`, calls `Run(ctx)`, fail-fast) on each new root, so replacing it affects only that container. `CloneWithOpts` goes through `NewWithOpts` and gets the default handler.
- Consumer-replaced handlers are not copied to clones (see Open Questions).

**D11. Errors.**
- New sentinels in `errors.go`: `ErrAlreadyStarted` ("DI: container already started") and `ErrStartAfterShutdown` ("DI: container is shut down").
- Failures are wrapped with `fmt.Errorf("...: %w", cause)`, so `errors.Is`/`errors.As` reach the cause.
- Recovered panics are formatted with `%v`, plus `%w` when the panic value is an error.
- No exported `StartError` type in this change. It can be added later without breaking callers.

**D12. Go 1.27 features.**
- `reflect.TypeFor` is used for target types.
- `testing/synctest` is used for deadline and cancellation tests.
- Generic methods are not needed here: `Kind.Handle`/`With` only use the receiver's type parameters. The package-level API stays function-based because `Injector` is an interface.

## Risks / Trade-offs

- **[Deadlock]** A `Start*` method, handler or container hook that calls `root.Shutdown()` (without a deadline) on the Start goroutine would wait on `startDone` forever. Calling `Start` re-entrantly is safe and returns `ErrAlreadyStarted`. → Document it in godoc ("return an error instead"), and add a test that a handler returning an error leads to a normal caller-driven `Shutdown`. Making Shutdown re-entrant is deferred.
- **[Invoke hot path]** D4 adds a `len(opts.dependsOn)` check per build, not per cached invoke. → `bench/do_bench_test.go` runs before and after, compared with `benchstat`, and the target is `~`.
- **[Eager invoke can now fail]** This happens only when it has `DependsOn` with a missing or cyclic target. Eager registrations without options keep returning `nil` error. → Pinned by a compatibility test.
- **[Interface-registered Starters are invisible to auto-detection]** This is by design, because detection uses the registered type. → `AsStarter()` is documented in the godoc and the docs page.
- **[Wider unexported wrapper interface]** It touches all four wrappers. → Compile-time assertions (`var _ serviceWrapper[int] = ...`) already exist and catch omissions.
- **[`DisableStarterAutoDetection` / `EnableRunnerAutoDetection` are fixed at container creation]** They are read from `opts` at Start, and `opts` is copied on clone. → Pinned by tests.

## Public API & Defaults

New identifiers (package `doger`):

```go
// di_lifecycle.go
type Starter interface{ Start() }
type StarterWithError interface{ Start() error }
type StarterWithContext interface{ Start(context.Context) }
type StarterWithContextAndError interface{ Start(context.Context) error }
type Runner interface{ Run(context.Context) error }

// root_scope.go
func (s *RootScope) Start() error
func (s *RootScope) StartWithContext(ctx context.Context) error
func (s *RootScope) AddBeforeStartHook(hook func(*Scope, string))
func (s *RootScope) AddAfterStartHook(hook func(*Scope, string, error))

// injector.go (InjectorOpts fields)
HookBeforeStart             []func(scope *Scope, serviceName string)
HookAfterStart              []func(scope *Scope, serviceName string, err error)
DisableStarterAutoDetection bool
EnableRunnerAutoDetection   bool

// options.go
type ProvideOption func(*serviceOptions)
func AsStarter() ProvideOption
func DependsOn[T any]() ProvideOption
func DependsOnNamed(name string) ProvideOption
func AsRunnable() ProvideOption

// dependency.go
func DeclareDependency(i Injector, dependent, dependency string) error

// kind.go
type Kind[T, M any] struct{ /* unexported */ }
type Component[T, M any] struct { Scope *Scope; Name string; Instance T; Meta M }
type Stage int
const ( StageBeforeStart Stage = iota; StageAfterStart )
func NewKind[T, M any](name string) Kind[T, M]
func (k Kind[T, M]) Name() string
func (k Kind[T, M]) With(meta M) ProvideOption
func (k Kind[T, M]) Handle(root *RootScope, stage Stage, h func(context.Context, Component[T, M]) error)
var Runnable = NewKind[Runner, struct{}]("runnable")

// errors.go
var ErrAlreadyStarted, ErrStartAfterShutdown error
```

**Changed signatures — BREAKING for function-value assignment only:**
- `Provide`, `ProvideNamed`, `ProvideValue`, `ProvideNamedValue`, `ProvideTransient`, `ProvideNamedTransient`
- `Override`, `OverrideNamed`, `OverrideValue`, `OverrideNamedValue`, `OverrideTransient`, `OverrideNamedTransient`
- `Lazy`, `LazyNamed`, `Eager`, `EagerNamed`, `Transient`, `TransientNamed`

All gain a trailing `opts ...ProvideOption`.

**Defaults:** see the proposal's Defaults & Control table. Additional defaults fixed here:
- nil option ignored
- duplicate `DependsOn` collapsed
- duplicate kind panics
- `Override` keeps the registration index
- nil Start context becomes `Background`
- a failed Start still counts as started
- Shutdown waits for a running Start
- clones get the default `Runnable` handler, not consumer replacements

**Example** (docs and `ExampleRootScope_Start`):

```go
root := doger.New()
doger.Provide(root, NewDB)                                  // Start(ctx) error -> auto-started
doger.Provide(root, NewHTTPServer)                          // invokes *DB -> started after DB
doger.Provide(root, NewMigrator, doger.AsRunnable())        // runs once after Starters
if err := root.StartWithContext(ctx); err != nil {
    _ = root.Shutdown()                                     // caller owns cleanup
    return err
}
```

## Test Strategy

Test files:
- `options_test.go`: registration-options
- `dependency_test.go`: dependency-declaration
- `root_start_test.go`: container-start
- `kind_test.go`: component-kinds
- Example tests: `root_start_example_test.go`, `kind_example_test.go`

All tests run under `go test -race`. goleak runs through `TestMain` (`main_test.go`). Tests that could hang use `testWithTimeout`. Deadline scenarios use `testing/synctest`.

| Scenario | Test | Kind |
|----------|------|------|
| Registration with and without options (6 rows) | `options_test.go: TestProvideOptions_Registration/<helper>` | table |
| Function values of the old signature no longer compile | documented in CHANGELOG/docs; compile-only check `options_test.go: TestProvideOptions_SignatureHasVariadic` (asserts `reflect.TypeOf(Provide[int]).IsVariadic()`) | unit |
| Each constructor yields an option recognized by registration (5 rows) | `TestProvideOptions_Constructors/<option>` | table |
| Repeated options (5 rows) | `TestProvideOptions_Repeated/<row>` (panic rows use `assert.PanicsWithError`) | table |
| Invalid opt-in combinations (7 rows) | `TestProvideOptions_Invalid/<row>` | table |
| Clone, override and alias (5 rows) | `TestProvideOptions_CloneOverrideAlias/<row>` | table |
| Registration index | `TestRegistrationIndex_Order`; concurrency: `TestRegistrationIndex_ConcurrentUnique` (100 goroutines) | unit / race |
| Declared dependency built before the dependent (3 rows) | `dependency_test.go: TestDependsOn_BuildOrder/<row>` | table |
| Declared dependency on an eager service | `TestDependsOn_EagerTarget` | unit |
| Declared dependency resolved through scope ancestry | `TestDependsOn_AncestorScope` | unit |
| Concurrent invocation builds declared dependencies once | `TestDependsOn_ConcurrentSingleBuild` | race |
| Missing or circular declared dependency (5 rows) | `TestDependsOn_Errors/<row>` | table |
| Declaring edges dynamically (7 rows) | `TestDeclareDependency/<row>`, plus a `nil_injector_uses_default_root` row | table |
| Concurrent declarations are race-free | `TestDeclareDependency_Concurrent` | race |
| Scheduler is shut down before the jobs it drives (2 rows) | `TestDeclaredEdges_ShutdownOrder/<row>` | table |
| Ties broken by scope depth then registration order | `root_start_test.go: TestStart_TieBreak`; repeated 100× in `TestStart_TieBreak_Stable` | unit |
| Each Starter variant is started once (4 rows) | `TestStart_Variants/<method>` | table |
| Which registrations are started (7 rows) | `TestStart_Detection/<row>` | table |
| AsStarter on an interface whose instance cannot start | `TestStart_AsStarterInstanceWithoutStart` | unit |
| Auto-detection toggle (2 rows) | `TestStart_DisableAutoDetection/<row>` | table |
| Dependency-ordered build and start | `TestStart_DependencyOrder` | unit |
| Child scopes are started after their ancestors | `TestStart_ScopeTraversal` | unit |
| Start on an empty container | `TestStart_Empty` | unit |
| First failure stops the pipeline (4 rows) | `TestStart_FirstFailure/<row>` | table |
| Shutdown after a failed Start reaches every built service | `TestStart_ShutdownAfterFailure` | unit |
| Cancellation and deadline (3 rows) | `TestStart_Context/<row>` inside `synctest.Test` | table / synctest |
| Nil context | `TestStart_NilContext` | unit |
| Repeated and late Start calls (4 rows) | `TestStart_OneShot/<row>` | table |
| Concurrent Start calls | `TestStart_Concurrent` | race |
| Concurrent Start and Invoke | `TestStart_ConcurrentInvoke` | race |
| Shutdown waits for a running Start | `TestStart_ShutdownWaits` inside `synctest.Test` | race / synctest |
| Late registration | `TestStart_LateRegistrationNotStarted` | unit |
| Hooks around each Starter | `TestStart_Hooks` | unit |
| Opting a service into a custom kind | `kind_test.go: TestKind_OptIn` | unit |
| Same name, distinct kinds | `TestKind_DistinctIdentity` | unit |
| Handler stage relative to Starters (2 rows) | `TestKind_Stage/<stage>` | table |
| Replacing a handler | `TestKind_ReplaceHandler`; nil handler: `TestKind_NilHandlerPanics` | unit |
| Kind without a handler | `TestKind_NoHandler` | unit |
| Components are passed in deterministic order | `TestKind_ComponentOrder` | unit |
| Handler failure stops Start (2 rows) | `TestKind_HandlerFailure/<row>` | table |
| Scheduler handler orders start and shutdown | `TestKind_DeclareDependencyInHandler` | unit |
| Runners run after Starters | `TestRunnable_RunsAfterStarters` | unit |
| Runner failure | `TestRunnable_Failure` | unit |
| Runner without opt-in is not run (2 rows) | `TestRunnable_AutoDetection/<row>`; transient/alias exclusion row `TestRunnable_AutoDetection/excludes_transient_and_alias` | table |
| Replacing the Runnable handler | `TestRunnable_ReplaceHandler` (handler goroutine joined in `t.Cleanup` so goleak passes) | unit |

**Documentation tests:**
- `ExampleRootScope_Start` (default auto-detection and the caller-cleanup idiom)
- `ExampleAsStarter` (interface-registered service)
- `ExampleDependsOn`
- `ExampleDeclareDependency`
- `ExampleNewKind` (a scheduled-job kind with a handler that declares edges)
- `ExampleAsRunnable`
- `ExampleKind_Handle` (replacing the Runnable handler)

**Hot paths and their coverage:**
- **Resolution/invocation:** `Invoke`/`MustInvoke` with and without options. The existing `di_test.go`/`invoke_test.go` stay green unchanged. `TestDependsOn_*` covers the new build branch. `bench/do_bench_test.go` is compared before and after with `benchstat`.
- **Lifecycle:** `TestStart_*` (start), `TestDeclaredEdges_ShutdownOrder` and `TestStart_ShutdownAfterFailure` (shutdown). The existing `di_lifecycle_test.go` and `scope_test.go` shutdown tests stay green.
- **Scope traversal:** `TestStart_ScopeTraversal`, `TestStart_TieBreak`, `TestDependsOn_AncestorScope`.
- **Concurrency:** `TestStart_Concurrent`, `TestStart_ConcurrentInvoke`, `TestStart_ShutdownWaits`, `TestDependsOn_ConcurrentSingleBuild`, `TestDeclareDependency_Concurrent`, `TestRegistrationIndex_ConcurrentUnique`, all under `-race` with goleak.
- **Compatibility:**
  - `TestProvideOptions_Registration/none` and `TestEager_NoOptionsNeverErrors`: eager invoke without options never errors.
  - The whole existing suite unchanged.

## Migration Plan

- The change is additive apart from the variadic signature break, which is listed in CHANGELOG with the fix: wrap the generic function in a closure if it was stored as a value.
- Docs gain a "Starting the container" page (Starters, ordering, errors, `AsStarter`), a "Component kinds" page (`NewKind`, `Handle`, `Runnable`, the scheduled-job example) and a "Declared dependencies" section.
- Rollback is a plain revert. No persisted state.

## Open Questions

- Should `CloneWithOpts` copy consumer-replaced kind handlers? Today a clone gets only the default `Runnable` handler. This affects only clones of containers that replaced handlers, and can be revisited without changing these specs, which do not cover clone plus handlers.
- Whether to export a structured `StartError` (scope, service, kind, cause) later. Callers already get `errors.Is`/`As` through wrapping, so adding it later is non-breaking.
- Whether `ExplainService` should mark declared edges differently from invoked ones. This is display only.
