# Design

## Context

See proposal.md for the motivation. State after `fix-stacktrace-frame-filter` and `rename-module-doger`:

**Go version declarations**
- All 25 `go.mod` files and `go.work` declare `go 1.18`.
- CI (`.github/workflows/test.yml`) tests a matrix from `1.18` to `1.26` plus `stable`.
- `release.yml` reads `go-version-file: 'go.work'`.
- `.golangci.yml` (version 2) pins no Go version.
- `README.md` (badge and tagline) and `docs/docs/getting-started.md` say "Go 1.18+".

**Helpers in `utils.go` and their stdlib equivalents.** Equivalence must be exact, including nil versus empty results:

| Helper | Call sites (non-test) | Stdlib candidate | Exactly equivalent? |
|---|---|---|---|
| `contains` | `virtual_scope.go:115` | `slices.Contains` | Yes |
| `reverseSlice` | `di_explain.go:480` | `slices.Reverse` | Yes |
| `coalesce` | `invoke.go:234` | `cmp.Or` | Yes: both return the first non-zero argument |
| `keys` | `dag.go:189,193`, `scope.go:440` | `slices.Collect(maps.Keys(m))` | **No**: `Collect` returns `nil` for an empty map, `keys` returns a non-nil empty slice. `dag.go` results flow into explain output, where JSON shows `null` versus `[]`. |
| `flatten` | `di_explain.go:126` | `slices.Concat(c...)` | **No**: `Concat` returns `nil` when the total length is 0 |
| `filter` | `di_explain.go:530` | none (`slices.DeleteFunc` mutates in place) | No equivalent |
| `mAp` | `invoke.go`, `di_explain.go` (×12) | none in stdlib | No equivalent |
| `orderedUniq` | `scope.go:251,281` | none (`slices.Compact` needs sorted input) | No equivalent |

**Goroutine capture workarounds**
- `scope.go:405-407`: `wg.Add(1)` + `go func(c *Scope, i int)`.
- `scope.go:488-492`: `wg.Add(len(...))` + `go func(n string)`.
- `utils.go:204,226` and `root_scope.go:224,230` start goroutines without loop-variable capture. They are left alone.

**Other.** 18 non-test occurrences of `interface{}`.

**Directive-sensitive semantics.** Raising the `go` directive changes the language semantics of *this module's* code, and the GODEBUG defaults of *this repository's own* test binaries, where this module is the main module.
- **Loop variables become per-iteration (1.22).** This is a language change that applies to every consumer, because it is decided by the declaring module's version.
- **`panicnil` (1.21).** In test binaries, `panic(nil)` inside a provider now reaches `handleProviderPanic` as `*runtime.PanicNilError` (`err != nil`), where before `recover()` returned `nil`. For consumers this follows *their* main module's directive, not ours. No current test panics with `nil`.
- **`asynctimerchan` (1.23) timer semantics** apply to `raceWithTimeout` (`utils.go:219`) in test binaries.

## Goals / Non-Goals

**Goals:**
- `go 1.27` in every `go.mod` and in `go.work`. CI tests `1.27` and `stable`.
- Replace only the **exactly equivalent** helpers: `contains`, `reverseSlice`, `coalesce`.
- Remove the goroutine capture workarounds using `sync.WaitGroup.Go`, and apply `interface{}` → `any`.
- Make `testing/synctest` and generic methods available to the following changes.

**Non-Goals:**
- Replacing `keys`, `flatten`, `filter`, `mAp` or `orderedUniq`, which have no exact equivalent (see table).
- Adopting new Go features in the public API. That belongs to the feature changes.
- Changing `make test` to cover the sub-modules (separate tooling follow-up).
- Changing GODEBUG settings. Consumers' builds follow their own main module.

## Decisions

**D1. Bump directives with `go mod edit -go=1.27` in every module, and `go work edit -go=1.27`.** No `toolchain` line is added: the local toolchain is 1.27.x, so none is needed.
- *Alternative:* `go 1.27.0` with a `toolchain` pin. Rejected: it adds churn without benefit for a library.

**D2. Helper replacement uses *Inline Function*, then *Remove Dead Code*.**
1. Replace each call site of `contains`, `reverseSlice` and `coalesce` with the stdlib call. Tests stay untouched.
2. Once a helper has no callers, delete it together with its dedicated unit test in `utils_test.go`.

That second step deletes tests, so it is not a "tests unchanged" refactor. The deleted tests only cover unexported code that no longer exists, and the behavior moves to stdlib functions with identical semantics. Every caller stays covered by its own tests. This is an explicit exception, recorded here for review.
- *Alternative:* keep the helpers as one-line wrappers around stdlib, so no test changes. Rejected: wrappers that add nothing are dead weight. Choose this alternative instead if the strict "no test changes" reading is preferred.

**D3. `sync.WaitGroup.Go` for the two `scope.go` fan-outs.** Replace `wg.Add(n)` + `go func(x T){...; wg.Done()}(v)` with `wg.Go(func(){...})`, capturing the per-iteration loop variable. This is behavior-preserving: same concurrency, and `Wait` semantics are unchanged.

**D4. Mechanical modernization through tools, one analyzer per step.** Use the gopls `modernize` analyzers (`gopls codeaction -kind=source.fixAll` or the `modernize` command) and `gofmt -r 'interface{} -> any'`. Review each diff, and accept only fixes inside this design's scope (`any`, `WaitGroup.Go`, `slices.Contains`, `slices.Reverse`, `cmp.Or`). Reject others, e.g. `min`/`max` or `range-over-int` rewrites outside the touched code, to keep the diff reviewable.

**D5. CI and lint.**
- The `test.yml` matrix becomes `["1.27", "stable"]`.
- `.golangci.yml` gets `run.go: "1.27"`, so linters judge against the new floor.
- `release.yml` already follows `go.work`.

**D6. Docs:** update the README badge and tagline, and the "Compatible with Go …" line in `docs/docs/getting-started.md`, to 1.27.

## Risks / Trade-offs

- [Per-iteration loop variables change the semantics of an existing closure that relied on the shared variable] → Run the full race suite after the directive bump, *before* any other edit (tasks group 1). `go vet`'s `loopclosure` check is no longer meaningful, so also inspect closures inside loops in the non-test code that group 1 touches.
- [`panicnil` changes test-binary behavior for `panic(nil)` in providers] → No test exercises it today. Add a characterization test showing that a `panic(nil)` provider now yields an error. It documents the new behavior in this repo's own tests and does not change consumer behavior (their GODEBUG follows their directive).
- [`asynctimerchan` changes timer behavior in `raceWithTimeout`] → Covered by the existing health-check timeout tests under `-race`. Any flake or failure after the bump means stop and investigate before continuing.
- [Dropping Go 1.26 users] → Accepted and flagged as **BREAKING** in the proposal.
- [A modernize fix outside scope slips in] → One analyzer per step, with a diff review.

## Public API & Defaults

- **BREAKING:** the minimum Go version is 1.27.
- No exported identifier, signature, default or option changes.
- Override: none needed. The version floor is a build requirement.

## Test Strategy

There are no spec scenarios (`skip_specs: true`). The contract is "no behavior change for consumers", so the existing suite is the safety net and every hot path stays covered by its current tests:
- public API: `di_test.go`, Examples
- resolution: `invoke_test.go`, `service_*_test.go`
- lifecycle: `di_lifecycle_test.go`, health-check timeout tests
- scope traversal: `scope_test.go`
- concurrency: the race suite with goleak in `main_test.go`; the `scope.go` shutdown fan-outs touched by D3 are covered by the shutdown tests in `scope_test.go` / `root_scope_test.go`

| Check | How | Kind |
|---|---|---|
| Suite green right after the directive bump, before code edits | `make test` | unit / race / goleak |
| Test inventory unchanged, except the deleted helper tests from D2 | Before/after `go test -list '.*' ./...` per module, attributed by directory | regression |
| `panic(nil)` provider behavior documented | New characterization test `TestHandleProviderPanic_PanicNil` (`invoke_test.go`): asserts an error wrapping `*runtime.PanicNilError` | unit |
| Shutdown fan-out still parallel and complete after `WaitGroup.Go` | Existing shutdown tests in `scope_test.go` / `root_scope_test.go` under `-race`; `make test` | race |
| Health-check timeouts unchanged under the new timer semantics | Existing health-check timeout tests, run with `-count=5` | race |
| Every workspace module builds and passes | Per module: `go vet ./... && go test -race ./...` | unit / race |
| Lint against the new floor | `make lint` with `run.go: "1.27"` | lint |
| Modules tidy | `go mod tidy` leaves no diff | tooling |

## Migration Plan

1. Bump the directives (D1), then run the full suite with no other change.
2. Apply the helper inlining (D2), one helper per step, with `make test` after each.
3. Apply D3, then the remaining D4 steps, one at a time.
4. Update CI, lint and docs (D5, D6).

Rollback: revert the commit series. No published artifact depends on it.
