# Tasks

Prerequisite: `add-start-lifecycle-and-kinds` has landed (registration index, `ProvideOption` metadata, `ServiceTypeAlias` detection, `Start`). Commands assume the root module `github.com/kartaladev/doger`. Tests live in `invoke_all_test.go` and `invoke_all_order_test.go` (package `doger`). Load `golang-how-to`, then `golang-testing`, `golang-stretchr-testify`, `golang-concurrency` and `golang-refactoring` before starting.

## 1. Collect every visible service assignable to a type

- [ ] 1.1 RED: add `TestInvokeAll_Membership` (4 rows), `TestInvokeAll_EagerMembers`, `TestInvokeAll_AnyType`, `TestInvokeAll_NilInjector` and `TestInvokeAll_TypedNilMember`, covering the scenarios "Membership by registered type", "Eager values are members without a provider call", "Interface type any matches every non-alias service", "Nil injector uses the default root scope" and "Typed-nil member is returned as is". Verify: `go test -race -run 'TestInvokeAll_(Membership|EagerMembers|AnyType|NilInjector|TypedNilMember)' .` fails on the missing symbol `InvokeAll`.
- [ ] 1.2 GREEN: add `InvokeAll[T]` in `di.go`, backed by a collection pipeline in `invoke.go`: nearest-name candidate view (D1), alias filter, `serviceCanCastToGeneric[T]` filter, then sequential build. Copy the per-member logic from `invokeByGenericType` for now. Verify: the command above passes and `make test` is green.
- [ ] 1.3 REFACTOR: *Extract Function* `invokeServiceInstance` (D3) from `invokeByGenericType` and use it from both call sites. Run `go test -race -run 'TestInvokeAs|TestInvokeAll' .` after the extraction. Verify: `make test` and `make lint` pass, and the existing `InvokeAs` tests are unchanged.

## 2. Visibility, shadowing and alias exclusion

- [ ] 2.1 RED: add `TestInvokeAll_Visibility` (5 rows), `TestInvokeAll_AliasExcluded` and `TestInvokeAll_AliasOnlyMatch` for the scenarios "Scope visibility and shadowing", "Alias does not duplicate its target" and "Alias whose target is not assignable is still excluded". Verify: `go test -race -run 'TestInvokeAll_(Visibility|Alias)' .` fails on the shadowing row `x/*sql.DB` (expected none) and/or the alias rows, for the reason stated in the assertion.
- [ ] 2.2 GREEN: dedupe names before type filtering (nearest wins) and exclude `ServiceTypeAlias`. Verify: the command above passes and `make test` is green.
- [ ] 2.3 REFACTOR: *Extract Function* `collectionCandidates[T](injector) []candidate` (name, scope, depth, registration index, service), for reuse by the named variant. Verify: `make test` and `make lint` pass.

## 3. Empty results, failure semantics and Must variants

- [ ] 3.1 RED: add `TestInvokeAll_Empty` (4 rows), `TestInvokeAll_MemberFailure` (3 rows), `TestInvokeAll_SelfCycle`, `TestMustInvokeAll_Panics` (2 rows) and `TestMustInvokeAll_Success` (2 rows) for the scenarios "Empty results", "Member failure modes", "Collection invoked from inside a member's own provider", "Must variants panic with the same error" and "Must variants return the collection on success". Verify: `go test -race -run 'TestInvokeAll_(Empty|MemberFailure|SelfCycle)|TestMustInvokeAll_' .` fails: nil-vs-empty assertion, unwrapped member name, missing `MustInvokeAll`/`InvokeAllNamed`.
- [ ] 3.2 GREEN: return `[]T{}` on no match, wrap the first member error as ``DI: could not invoke collection member `%s`: %w``, and add `MustInvokeAll` via `must1`. Add a minimal `InvokeAllNamed`/`MustInvokeAllNamed` so the Must rows compile and pass. Verify: the command above passes and `make test` is green.
- [ ] 3.3 REFACTOR: *Extract Function* `wrapMemberError(name string, err error) error`. Verify: `make test` and `make lint` pass.

## 4. Name-keyed collection

- [ ] 4.1 RED: add `TestInvokeAllNamed_Keys` (2 rows) for the scenario "Named collection keys", and extend `TestInvokeAll_Empty` for the map rows if not already covered. Verify: `go test -race -run 'TestInvokeAllNamed_Keys|TestInvokeAll_Empty' .` fails on the child row (wrong `console` instance) or the map shape.
- [ ] 4.2 GREEN: build `InvokeAllNamed` on `collectionCandidates` with the same build loop. Return `map[string]T{}` on no match. Verify: the command above passes and `make test` is green.
- [ ] 4.3 REFACTOR: *Extract Function* `buildMembers` shared by the slice and map variants, removing the duplicated loop. Verify: `make test` and `make lint` pass.

## 5. Dependency edges, hooks and snapshot semantics

- [ ] 5.1 RED: add `TestInvokeAll_EdgesOrderShutdown`, `TestInvokeAll_HooksPerMember`, `TestInvokeAll_NoEdgesOutsideProvider`, `TestInvokeAll_TransientFresh`, `TestInvokeAll_Snapshot`, `TestInvokeAll_CallerMutation` and `TestInvokeAll_AfterShutdown` for the scenarios "Dependency edges order shutdown", "Invocation hooks per member", "No edges when invoked outside a provider", "Transient and singleton members across calls", "Later registration is not visible in an earlier snapshot", "Caller mutation does not leak" and "Collections after shutdown". Verify: `go test -race -run 'TestInvokeAll_(Edges|Hooks|NoEdges|TransientFresh|Snapshot|CallerMutation|AfterShutdown)' .` runs. Any test that already passes because of the reused helper is recorded as passing-on-arrival with a one-line note; at least the edge/hook assertions must have been seen failing before 1.3, otherwise add an assertion that distinguishes them.
- [ ] 5.2 GREEN: ensure `invokeServiceInstance` records `addDependency` for every member when called from a virtual scope, and fires the hooks with the member name. Verify: the command above passes and `make test` is green.
- [ ] 5.3 REFACTOR: none needed if 5.2 required no new code. Otherwise *Inline Function* any helper made redundant. Verify: `make test` and `make lint` pass.

## 6. Concurrency

- [ ] 6.1 RED: add `TestInvokeAll_ConcurrentSingleBuild` (100 goroutines, provider call counters) and `TestInvokeAll_ConcurrentRegistration` (50 registering + 50 collecting goroutines), both wrapped in `testWithTimeout`, for the scenarios "Concurrent callers build each member once" and "Concurrent registration and collection". Verify: `go test -race -count=5 -run 'TestInvokeAll_Concurrent' .`. If it passes on arrival (the wrappers already serialize builds), record that, and confirm the assertions would fail with a deliberately unsynchronized fake provider counter in a throwaway local experiment (not committed).
- [ ] 6.2 GREEN: fix any race or double build reported. Expected: no production change. Verify: the command above passes with `-race`, and goleak (via `TestMain`) reports no leak.
- [ ] 6.3 REFACTOR: none needed: no new shared state (D8). Verify: `make test` and `make lint` pass.

## 7. Default collection order

- [ ] 7.1 RED: add `TestInvokeAll_DefaultOrder` (4 rows), `TestInvokeAll_DefaultOrderStable` (50 services × 20 calls) and `TestInvokeAll_OverrideKeepsPosition` for the scenarios "Default order across scopes", "Order is stable across repeated calls" and "Override replaces a registration in place". Verify: `go test -race -run 'TestInvokeAll_(DefaultOrder|OverrideKeepsPosition)' .` fails on order assertions (map iteration order).
- [ ] 7.2 GREEN: sort candidates by (scope depth ascending, registration index ascending) before building (D2). Verify: the command above passes with `-count=10` and `make test` is green.
- [ ] 7.3 REFACTOR: *Introduce Parameter Object*: make `candidate` carry `depth` and `index` explicitly if they are still loose values. Verify: `make test` and `make lint` pass.

## 8. Ordered and WithOrder

- [ ] 8.1 RED: add `TestInvokeAll_Ordered` (6 rows), `TestInvokeAll_OrderedTransient` (2 rows), `TestInvokeAll_WithOrderPrecedence` (5 rows), `TestInvokeAll_WithOrderStyles` (3 rows), `TestInvokeAll_WithOrderSkipsOrderMethod` and `TestWithOrder_NoEffectOnInvoke` for the scenarios "Ordered values", "Order is read from each transient instance", "Precedence", "WithOrder on each registration style", "Order() is not called when WithOrder is set" and "WithOrder outside collections has no effect". Verify: `go test -race -run 'TestInvokeAll_(Ordered|WithOrder)|TestWithOrder_' .` fails on the missing symbols `Ordered` and `WithOrder`.
- [ ] 8.2 GREEN:
  - add `Ordered` (`di_lifecycle.go` or a new `ordered.go`) and `WithOrder(n int) ProvideOption`, which stores a `*int`, last one wins (D5);
  - add the stable ordering pass for `InvokeAll` only, with precedence WithOrder > Ordered > 0 (D4).

  Verify: the command above passes and `make test` is green.
- [ ] 8.3 REFACTOR: *Extract Function* `orderValue(name, instance, meta) (int, error)`, and *Replace Loop with Pipeline* using `slices.SortStableFunc`. Verify: `make test` and `make lint` pass.

## 9. Order() panic and named-collection independence

- [ ] 9.1 RED: add `TestInvokeAll_OrderPanic` (3 rows), `TestMustInvokeAll_OrderPanic` and `TestInvokeAllNamed_IgnoresOrdered` for the scenarios "Panicking Order()", "MustInvokeAll surfaces the Order() panic as its panic" and "Order() not called for named collections". Verify: `go test -race -run 'TestInvokeAll_OrderPanic|TestMustInvokeAll_OrderPanic|TestInvokeAllNamed_IgnoresOrdered' .` fails: the test binary panics or the error is not wrapped.
- [ ] 9.2 GREEN: recover inside `orderValue`. Wrap error panics with `%w` and stringify others, both naming the member. Verify: the command above passes and `make test` is green.
- [ ] 9.3 REFACTOR: *Extract Function* `recoverAsError(r any) error`, shared with `handleProviderPanic` only if that keeps `handleProviderPanic` behavior byte-identical (its tests unchanged). Otherwise none needed. Verify: `make test` and `make lint` pass.

## 10. Ordering never affects lifecycle order

- [ ] 10.1 RED: add `TestInvokeAll_OrderDoesNotAffectShutdown` and `TestInvokeAll_OrderDoesNotAffectStart` for the scenarios "Shutdown ignores collection order" and "Start ignores collection order". Verify: `go test -race -run 'TestInvokeAll_OrderDoesNotAffect' .`. These guard against regressions: run each once against a throwaway local change that feeds `WithOrder` into start ordering, to see it fail (not committed), then record the result.
- [ ] 10.2 GREEN: no production change expected; `WithOrder` is read only by `orderValue`. Verify: the command above passes and `make test` is green.
- [ ] 10.3 REFACTOR: none needed. Verify: `make test` and `make lint` pass.

## 11. Documentation and examples

- [ ] 11.1 Add `ExampleInvokeAll`, `ExampleInvokeAllNamed`, `ExampleWithOrder` and `ExampleOrdered` in `di_example_test.go`. Each documents the default (empty non-nil result, registration order, order 0) and the overrides (`Ordered`, `WithOrder`). Verify: `go test -run 'Example(InvokeAll|InvokeAllNamed|WithOrder|Ordered)' .` passes with matching `// Output:`.
- [ ] 11.2 Add godoc for the new identifiers covering snapshot semantics, the `InvokeAll[any]` sharp edge, shadowing by a non-matching nearest service, and per-member hooks. Add a docs page `docs/docs/service-invocation/collection-invocation.md`. Verify: `go doc` shows the comments, and the docs site build (`npm run build` in `docs/`) succeeds.

## 12. Benchmark and integration checks

- [ ] 12.1 Add `BenchmarkInvokeAll/{10,100,1000}` and `BenchmarkInvokeAllParallel` to `bench/do_bench_test.go`. Verify: `go test -run '^$' -bench 'BenchmarkInvokeAll' -benchmem ./bench/` runs, and the results are recorded in the change notes.
- [ ] 12.2 Full run: `make test` (race and goleak) and `make lint`. Verify: both pass, and every spec scenario row maps to a passing test per the design.md Test Strategy table.

## Workflow follow-up

- Propose `InvokeAs[T]` determinism (first match by registration order) as a separate, explicitly flagged behavior change.
- Consider struct-tag `[]T` injection and live collections in phase 2.
- Archive this change after review, once `make test` passes and the scenario-to-test mapping is confirmed.
