# Design

## Context

See proposal.md (Why, What Changes) and the specs `collection-invocation` and `collection-ordering`.

Current mechanics this change builds on:
- **`invokeByGenericType`** (`invoke.go`) walks `serviceForEachRec` and stops at the first service for which `serviceCanCastToGeneric[T]` is true, a reflect-type check that does not build the service.
  - It detects cycles with `virtualScope.detectCircularDependency`.
  - It builds via `getInstanceAny(newVirtualScope(serviceScope, chain+realName))`.
  - It records the edge with `vScope.addDependency`.
  - It fires `onBeforeInvocation`/`onAfterInvocation` and calls `serviceScope.onServiceInvoke`.
  - `InvokeAll` is the same per-member pipeline without the early stop.
- **`serviceForEachRec`** (`scope.go`) visits the **current scope first, then ancestors**, over Go map snapshots (unordered). Collections need ancestors first plus registration order, so ordering cannot come from this iteration.
- **From `add-start-lifecycle-and-kinds`:**
  - a per-service registration index, monotonic per scope and kept by `Override*`;
  - service metadata carried by the opaque `ProvideOption`;
  - `ServiceTypeAlias` from `getServiceType()`.
- **Error style** (`errors.go`, `invoke.go`): sentinel errors with a `DI:` prefix, wrapped with `fmt.Errorf("...%w...")`. `handleProviderPanic` turns a panic value into an error (`error` values kept as-is, others become `DI: %v`). `MustInvoke` uses `must1`, which panics with the error itself.

## Goals / Non-Goals

**Goals:**
- One shared collection pipeline behind `InvokeAll`, `InvokeAllNamed` and their `Must*` variants.
- Every per-member semantic (cycle detection, edges, hooks, invocation tracking) is identical to `Invoke`.
- Deterministic order with no dependence on map iteration.
- Linear cost in the number of visible services, plus the sort.

**Non-Goals:**
- Caching collections or invalidating them on registration (snapshot semantics only).
- Struct-tag `[]T` injection, live or lazy collections, deterministic `InvokeAs` (proposal Out of scope).
- Parallel building of members. They are built sequentially in default order, which keeps construction deterministic and matches how `Start` builds.

## Decisions

**D1. Candidate selection = nearest-name view, then type filter.**
Walk the invoking scope and its ancestors and keep the **first occurrence of each name** (nearest wins) *before* filtering by type. A nearest service that is not assignable to `T` therefore hides an ancestor that is (spec: shadowing row 4). This matches what `InvokeNamed` would resolve for that name.
- Then drop aliases (`getServiceType() == ServiceTypeAlias`), then keep only services where `serviceCanCastToGeneric[T]` is true.
- *Alternative:* filter by type first, then deduplicate names. Rejected: it would include an ancestor service that named resolution can never reach from this scope.

**D2. Default order = (scope depth ascending, registration index ascending).**
Collect candidates with their scope depth (root = 0) and registration index, then sort by that key. Iteration order of `serviceForEachRec` is irrelevant.

**D3. Build members sequentially in default order through a shared per-member helper.**
Extract the body of `invokeByGenericType` after selection into `invokeServiceInstance(injector, vScope, name, scope, service)`. It does cycle detection, `getInstanceAny`, `addDependency`, the hooks and `onServiceInvoke`. Both `invokeByGenericType` and the collection pipeline call it.
- On the first error, return `fmt.Errorf("DI: could not invoke collection member `%s`: %w", name, err)`. The call stops, and members already built stay built (lazy caches are kept).
- *Alternative:* build every member and join the errors with `errors.Join`. Rejected: building after a failure wastes work and can trigger further side effects, while the spec only requires the first failing member.

**D4. Ordering is a separate stable pass, for `InvokeAll` only.**
After all members are built, compute each member's order value:
1. `WithOrder` metadata, if present. `Order()` is then never called.
2. Otherwise, if `any(instance).(Ordered)` succeeds, `Order()` is called inside a `recover`.
3. Otherwise `0`.

Then run `slices.SortStableFunc` by order value over the default-ordered slice.
- An `Order()` panic becomes `fmt.Errorf("DI: Order() of collection member `%s` panicked: %w", name, cause)`, where `cause` is the panic error or `fmt.Errorf("%v", r)`.
- `InvokeAllNamed` skips this pass entirely.
- *Alternative:* sort by `(order, depth, index)` in a single pass. Equivalent, but it would need the order value before building, and `Order()` is an instance method.

**D5. `WithOrder` storage.**
`WithOrder(n)` sets a `*int` (so "set" is distinguishable from "set to 0") in the service metadata from `add-start-lifecycle-and-kinds`. Applying it again overwrites the value, so the last one wins. `clone()` copies it, and `Override*` replaces the metadata wholesale, as specified there. Nothing other than the ordering pass reads it.

**D6. Empty results are non-nil.**
Return `[]T{}` / `map[string]T{}` when nothing matches. On error, return `nil, err`.

**D7. `Must*` variants use `must1`**, so they panic with the exact error value, like `MustInvoke`.

**D8. Concurrency.**
- Candidate selection reads scope maps through the existing snapshotting iterators (`serviceForEachRec` under `RLock`).
- Members are built through the service wrappers, which already serialize their own builds (`serviceLazy.mu`). Concurrent callers therefore build each lazy member exactly once.
- No new shared state is introduced.

## Risks / Trade-offs

- [O(N) scan of every visible service on every call] → `serviceCanCastToGeneric` is a reflect check with no build. Measured by `BenchmarkInvokeAll/{10,100,1000}`. Callers in hot loops should resolve once and keep the snapshot, as documented.
- [`InvokeAll[any]` builds every non-alias service] → Documented as a sharp edge, and pinned by a spec scenario. Not prevented, because it is a legitimate (if rare) use.
- [Shadowing by a non-matching nearest service surprises users] → Documented with an example. It is consistent with named resolution, and the alternative would expose unreachable services.
- [The registration index depends on `add-start-lifecycle-and-kinds`] → This change is ordered after it. Tasks start by asserting the index exists via a failing test, not by assumption.
- [Hooks fire once per member, so N hook calls per `InvokeAll`] → Same as calling `Invoke` N times. Documented.

## Public API & Defaults

New exported identifiers, all purely additive (no breaking change):

```go
func InvokeAll[T any](i Injector) ([]T, error)
func MustInvokeAll[T any](i Injector) []T
func InvokeAllNamed[T any](i Injector) (map[string]T, error)
func MustInvokeAllNamed[T any](i Injector) map[string]T

type Ordered interface{ Order() int }

func WithOrder(n int) ProvideOption
```

Example (after `rename-module-doger`):

```go
doger.Provide(i, NewAuthMiddleware, doger.WithOrder(-10))
doger.Provide(i, NewLoggingMiddleware)            // order 0 (or its Order())
chain := doger.MustInvokeAll[Middleware](i)       // auth, then logging
```

| Behavior | Default | Override |
|---|---|---|
| Membership | Visible, non-alias, registered type assignable to `T` | Register under another type or scope |
| Shadowing | Nearest name wins, even if not assignable | Use distinct names |
| Order | Scope depth, then registration index | `Ordered` on the instance; `WithOrder(n)` (wins) |
| Missing order | `0` | `WithOrder(n)` |
| Errors | First failing member, wrapped with its name; no partial result | Resolve members individually |
| `nil` injector | `DefaultRootScope` (as `Invoke`) | Pass an explicit injector |

## Test Strategy

| Scenario | Test | Kind |
|----------|------|------|
| Membership by registered type (4 rows) | `invoke_all_test.go: TestInvokeAll_Membership/<T>` | table |
| Eager values are members without a provider call | `invoke_all_test.go: TestInvokeAll_EagerMembers` | unit |
| Interface type any matches every non-alias service | `invoke_all_test.go: TestInvokeAll_AnyType` | unit |
| Nil injector uses the default root scope | `invoke_all_test.go: TestInvokeAll_NilInjector` (restores `DefaultRootScope`) | unit |
| Typed-nil member is returned as is | `invoke_all_test.go: TestInvokeAll_TypedNilMember` | unit |
| Scope visibility and shadowing (5 rows) | `invoke_all_test.go: TestInvokeAll_Visibility/<row>` | table |
| Alias does not duplicate its target | `invoke_all_test.go: TestInvokeAll_AliasExcluded` | unit |
| Alias whose target is not assignable is still excluded | `invoke_all_test.go: TestInvokeAll_AliasOnlyMatch` | unit |
| Empty results (4 rows) | `invoke_all_test.go: TestInvokeAll_Empty/<function>_<services>` | table |
| Member failure modes (3 rows) | `invoke_all_test.go: TestInvokeAll_MemberFailure/<behavior>` | table |
| Collection invoked from inside a member's own provider | `invoke_all_test.go: TestInvokeAll_SelfCycle` | unit |
| Must variants panic with the same error (2 rows) | `invoke_all_test.go: TestMustInvokeAll_Panics/<function>` | table |
| Must variants return the collection on success (2 rows) | `invoke_all_test.go: TestMustInvokeAll_Success/<function>` | table |
| Named collection keys (2 rows) | `invoke_all_test.go: TestInvokeAllNamed_Keys/<from>` | table |
| Dependency edges order shutdown | `invoke_all_test.go: TestInvokeAll_EdgesOrderShutdown` | unit |
| Invocation hooks per member | `invoke_all_test.go: TestInvokeAll_HooksPerMember` | unit |
| No edges when invoked outside a provider | `invoke_all_test.go: TestInvokeAll_NoEdgesOutsideProvider` | unit |
| Transient and singleton members across calls | `invoke_all_test.go: TestInvokeAll_TransientFresh` | unit |
| Later registration is not visible in an earlier snapshot | `invoke_all_test.go: TestInvokeAll_Snapshot` | unit |
| Caller mutation does not leak | `invoke_all_test.go: TestInvokeAll_CallerMutation` | unit |
| Collections after shutdown | `invoke_all_test.go: TestInvokeAll_AfterShutdown` | unit |
| Concurrent callers build each member once | `invoke_all_test.go: TestInvokeAll_ConcurrentSingleBuild` (`testWithTimeout`, `-race`) | race |
| Concurrent registration and collection | `invoke_all_test.go: TestInvokeAll_ConcurrentRegistration` (`testWithTimeout`, `-race`) | race |
| Default order across scopes (4 rows) | `invoke_all_order_test.go: TestInvokeAll_DefaultOrder/<row>` | table |
| Order is stable across repeated calls | `invoke_all_order_test.go: TestInvokeAll_DefaultOrderStable` | unit |
| Override replaces a registration in place | `invoke_all_order_test.go: TestInvokeAll_OverrideKeepsPosition` | unit |
| Ordered values (6 rows) | `invoke_all_order_test.go: TestInvokeAll_Ordered/<values>` | table |
| Order is read from each transient instance (2 rows) | `invoke_all_order_test.go: TestInvokeAll_OrderedTransient/call_<n>` | table |
| Precedence (5 rows) | `invoke_all_order_test.go: TestInvokeAll_WithOrderPrecedence/<row>` | table |
| WithOrder on each registration style (3 rows) | `invoke_all_order_test.go: TestInvokeAll_WithOrderStyles/<style>` | table |
| Order() is not called when WithOrder is set | `invoke_all_order_test.go: TestInvokeAll_WithOrderSkipsOrderMethod` | unit |
| WithOrder outside collections has no effect | `invoke_all_order_test.go: TestWithOrder_NoEffectOnInvoke` | unit |
| Panicking Order() (3 rows) | `invoke_all_order_test.go: TestInvokeAll_OrderPanic/<panic>` | table |
| MustInvokeAll surfaces the Order() panic as its panic | `invoke_all_order_test.go: TestMustInvokeAll_OrderPanic` | unit |
| Shutdown ignores collection order | `invoke_all_order_test.go: TestInvokeAll_OrderDoesNotAffectShutdown` | unit |
| Start ignores collection order | `invoke_all_order_test.go: TestInvokeAll_OrderDoesNotAffectStart` | unit |
| Order() not called for named collections | `invoke_all_order_test.go: TestInvokeAllNamed_IgnoresOrdered` | unit |
| Public API defaults and overrides | `di_example_test.go: ExampleInvokeAll`, `ExampleInvokeAllNamed`, `ExampleWithOrder`, `ExampleOrdered` | example |

**Hot paths and their coverage:**
- **Resolution/invocation** (shared per-member helper from D3): `TestInvokeAll_*`, plus the unchanged `invoke_test.go` `InvokeAs` tests. These prove the extraction did not change `invokeByGenericType`.
- **Scope traversal** (D1, D2): `TestInvokeAll_Visibility`, `TestInvokeAll_DefaultOrder`, `TestInvokeAllNamed_Keys`.
- **Concurrency:** `TestInvokeAll_ConcurrentSingleBuild` and `TestInvokeAll_ConcurrentRegistration` under `-race`, with goleak via `TestMain`.
- **Lifecycle interaction:** `TestInvokeAll_EdgesOrderShutdown`, `TestInvokeAll_OrderDoesNotAffect{Shutdown,Start}`, `TestInvokeAll_AfterShutdown`.
- **Performance:** `bench/do_bench_test.go: BenchmarkInvokeAll/{10,100,1000}` (visible services, half matching) and `BenchmarkInvokeAllParallel`, recorded with `-benchmem`. Not a gate, but the baseline for future changes.

## Migration Plan

Additive only. No migration. Rollback is a revert.
