# Design

## Context

See proposal.md (Why, Defect) and `specs/service-source-tracking/spec.md` for the required behavior.

Current classification in `stacktrace/stacktrace.go` (`NewFrameFromCaller`): a frame is returned when

```
!isGoPkg && (!isDoPkg || !isDoStacktracePkg || isExamplePkg || isTestPkg)
```

All three `isDo*`/`isExample*` checks are `strings.Contains` on the file path against `"do"`, `"do/stacktrace/"` and `"do/examples/"`.
- Because `isDoPkg` is almost always true, the expression reduces to: skip a frame only when it is in GOROOT, or its file path contains `do/stacktrace/` and is not a test or example file.
- That is the contract the existing tests pin: only the `stacktrace` package's own frames are skipped. It breaks whenever the path does not contain the literal `do/stacktrace/`.

`runtime.FuncForPC(pc).Name()` already gives the fully qualified function name. Examples:
- `github.com/samber/do/v2/stacktrace.NewFrameFromCaller`
- `....NewFrameFromCaller.func1` (closures)
- `github.com/samber/do/v2.(*serviceLazy[...]).getInstance` (methods, generics)

That name carries the package import path, independent of where the file sits on disk.

`NewFrameFromPC` does no classification. `TestNewFrameFromPtr` fails only because its assertion hard-codes `do/stacktrace/`.

## Goals / Non-Goals

**Goals:**
- Classify frames by function import path, with the `stacktrace` package path derived at runtime, not hard-coded.
- Make the classifier and the frame search directly table-testable without relying on the checkout location.
- Keep every currently pinned frame result: the provider frames `newServiceEager`/`newServiceAlias`, `example1`/line 12, `Frame.String` format, and GOPATH-relative paths.

**Non-Goals:**
- Skipping all library frames to report user code (a behavior change, out of scope).
- Changing `removeGoPath`, `shortFuncName` or `NewFrameFromPC` logic.
- Changing the search depth (10 frames).
- Revisiting GOROOT detection (see Open Questions).

## Decisions

**D1. Derive the package path from a type in the package.**
Store `selfPkgPath = reflect.TypeOf(pkgMarker{}).PkgPath()` once at package init, using an unexported empty struct type. The commented-out `packageName = reflect.TypeOf(fake{}).PkgPath()` in the current file shows this was the original intent.
- *Alternative:* `runtime.FuncForPC` on a function in the package, then trimming the name. This works but needs string surgery.
- *Alternative:* `debug.ReadBuildInfo().Main.Path`. Rejected: it gives the *main* module, which is the consumer's module, not this library's.

**D2. Classify by name prefix `selfPkgPath + "."`.**
A frame belongs to the package iff `strings.HasPrefix(funcName, selfPkgPath+".")`. The trailing `.` excludes sibling paths such as `.../stacktracex.Helper`. Matching on the function name rather than the file excludes lookalike packages such as `github.com/acme/do/stacktrace` and `github.com/palantir/stacktrace`, and is immune to checkout and module-cache layout.
- *Alternative:* compare `filepath.Dir(file)` with the package directory from `runtime.Caller(0)` at init. Rejected: still file-based, and fragile under `-trimpath` and symlinked checkouts.

**D3. Extract a pure classifier and an injectable frame search.** These are behavior-preserving extractions (Fowler: *Extract Function*, *Introduce Parameter*):
- `skipFrame(selfPkg, goroot, function, file string) bool`: skip iff the file is in GOROOT, **or** the function has the `selfPkg+"."` prefix and the file is not a `_test.go` file. The `isExamplePkg` clause is dropped. No file of the `stacktrace` package can live under an examples directory, so the clause can never affect a stacktrace-package frame.
- `callerFrame(caller func(skip int) (pc uintptr, file string, line int, ok bool), funcName func(pc uintptr) (string, bool), depth int) (Frame, bool)`: the loop body of today's `NewFrameFromCaller`, parameterized so tests can feed synthetic stacks.
- `NewFrameFromCaller()` becomes `callerFrame(runtime.Caller, realFuncName, 10)` with `skipFrame(selfPkgPath, goroot, ...)`. Its exported signature is unchanged.

**D4. The fake caller must replicate the real offset.**
`runtime.Caller(i)` inside `callerFrame` sees `callerFrame` itself, plus `NewFrameFromCaller`, as `stacktrace` frames. Both are skipped by D2 because they share the package prefix, so the real-caller result is unchanged. The scenario "Caller frame from a stacktrace test helper" pins this end to end.

**D5. Test-file rule unchanged:** a frame whose file ends in `_test.go` is always reported. This preserves `example1` resolving to itself.

## Risks / Trade-offs

- [The Go linker escapes `.` in the **last** import-path element of symbol names (e.g. `gopkg.in/yaml%2ev2.F`)] → The last element of our path is `stacktrace`, which has no dot, so the prefix matches. A guard test asserts that `runtime.FuncForPC` of a known package function has the prefix `selfPkgPath + "."`. If a future rename puts a dot in that element, the test catches it.
- [The extraction subtly changes the frame offset] → The end-to-end scenarios (`TestStacktrace`, the provider and invocation frame tests) run against real stacks and pin `example1`/line 12 and `newServiceEager`.
- [Hidden callers depend on the exact frame for transient or alias services] → Covered by the existing `service_*_test.go` suites, which must pass unchanged except for the newly added alias-file assertion.
- [The checkout-name Examples rows (`do`, `my-fork`) cannot be exercised by relocating the test binary] → They are covered by classifier rows with those paths (D2 makes file location irrelevant), plus the real end-to-end run in this `doger` checkout.

## Public API & Defaults

- No exported identifier added, removed or changed. `NewFrameFromCaller`, `NewFrameFromPC`, `Frame` and `Frame.String` keep their signatures.
- Behavior: in layouts where the old substring matched (a checkout named `do`), results are identical. In every other layout, results now match that same contract instead of returning the `stacktrace` package's own frame.
- No new defaults or knobs. Override is not applicable, as justified in the proposal (Defaults & Control).
- Not a breaking change.

## Test Strategy

| Scenario | Test | Kind |
|----------|------|------|
| Caller frame from a stacktrace test helper (`doger` row, real stack) | `stacktrace/stacktrace_test.go: TestStacktrace` (assert suffix `stacktrace/stacktrace_test.go`) | unit |
| Caller frame from a stacktrace test helper (`do`, `my-fork` rows) | `stacktrace/stacktrace_internal_test.go: TestSkipFrame/checkout_do`, `/checkout_my_fork` | table |
| Provider frame of library-created services (eager, alias) | `service_test.go: TestInferServiceProviderStacktrace` (existing, unchanged) | unit |
| Invocation frame of an alias service is recorded at the alias implementation | `service_alias_test.go: TestServiceAlias_getInstanceAny` (add file assertion) | unit |
| Classify frames across layouts and lookalike packages (12 rows) | `stacktrace/stacktrace_internal_test.go: TestSkipFrame/<row>` | table |
| Frames in GOROOT are skipped (2 rows) | `TestSkipFrame/goroot_runtime_goexit`, `/goroot_testing_trunner` | table |
| Search depth boundary (9/10 and 10/11) | `stacktrace/stacktrace_internal_test.go: TestCallerFrame_SearchDepth/<row>` with a fake `caller` | table |
| Unresolvable program counter | `stacktrace/stacktrace_test.go: TestNewFrameFromPC_ZeroPC` | unit |
| Frame string format | `stacktrace/stacktrace_test.go: TestFrame_String` (assert suffix `stacktrace/stacktrace_test.go:example1:12`) | unit |
| Guard: symbol prefix matches the derived package path | `stacktrace/stacktrace_internal_test.go: TestSelfPkgPath_MatchesSymbolPrefix` | unit |
| Provider frame from a PC (path-agnostic suffix) | `stacktrace/stacktrace_test.go: TestNewFrameFromPtr` (assert suffix `stacktrace/stacktrace_test.go`) | unit |

**Hot paths and their coverage:**
- Service invocation tracking. `getInstance` in lazy, eager, alias and transient services calls `NewFrameFromCaller` on every invocation. Covered by the existing `service_*_test.go` suites and their concurrent invocation tests under `-race`, with goleak via `TestMain`.
- Explain output (`di_explain.go`). Covered by the existing `di_explain_test.go`, which must stay green.
- Classifier: `TestSkipFrame` covers every branch (GOROOT, own package, own-package test file, other package, lookalikes).

**RED expectation:** before the fix, the following fail in this checkout:
- `TestStacktrace` and the three root-package tests: assertion failures, wrong frame.
- `TestSkipFrame`, `TestCallerFrame_SearchDepth` and `TestSelfPkgPath_MatchesSymbolPrefix`: missing symbols.

The module-cache rows of `TestSkipFrame` are the regression proof for the unverified consumer-layout claim in the proposal.

## Migration Plan

None needed. The change is internal to the `stacktrace` package and its tests. Rollback is a plain revert. It lands under the current module path, before `rename-module-doger`, which then carries it over mechanically and needs no special handling, because `selfPkgPath` is derived at runtime.

## Open Questions

- `goroot` comes from the deprecated `runtime.GOROOT()`. Under some build modes (e.g. `-trimpath`) it may be empty, and `strings.Contains(file, "")` is always true. This is unverified and out of scope here. If it matters, it needs its own failing test and change. It does not affect this fix's specs or tasks.
