# Design

## Context

See proposal.md (Why, Defect) and `specs/service-source-tracking/spec.md` for the required behavior.

Current classification in `stacktrace/stacktrace.go` (`NewFrameFromCaller`): a frame is returned when

```
!isGoPkg && (!isDoPkg || !isDoStacktracePkg || isExamplePkg || isTestPkg)
```

- `isGoPkg` is `strings.Contains(file, goroot)`, where `goroot = runtime.GOROOT()` is read once at package init. It is true for every file when `goroot` is `""`, and for sibling directories such as `/usr/local/gopath` when `goroot` is `/usr/local/go`.
- The `isDo*`/`isExample*` checks are `strings.Contains` on the file path (after `removeGoPath`) against `"do"`, `"do/stacktrace/"` and `"do/examples/"`.
- Because `isDoPkg` is almost always true, the expression reduces to: skip a frame only when it is in GOROOT, or its file path contains `do/stacktrace/` and is not a test or example file.
- That is the contract the existing tests pin: only the `stacktrace` package's own frames are skipped. It breaks whenever the path does not contain the literal `do/stacktrace/`: any checkout not ending in `do`, the module cache (`do/v2@<version>/stacktrace/`) and vendor directories (`do/v2/stacktrace/`).

Callers (gopls references): `newServiceEager` and `serviceEager.getInstance` (`service_eager.go:31,71`), `newServiceAlias` and `serviceAlias.getInstance` (`service_alias.go:36,76`), `serviceLazy.getInstance` (`service_lazy.go:79`), and `bench/stacktrace_bench_test.go:14`. Transient services record no frames (`service_transient.go:87-90`). The provider frame is what `ExplainService` / `ExplainNamedService` show as `Invoked:` (`di_explain.go:222-239`). Invocation frames are kept internally only.

`runtime.FuncForPC(pc).Name()` already gives the fully qualified function name. Examples:
- `github.com/samber/do/v2/stacktrace.NewFrameFromCaller`
- `....NewFrameFromCaller.func1` (closures)
- `github.com/samber/do/v2.(*serviceLazy[...]).getInstance` (methods, generics)

That name carries the package import path, independent of where the file sits on disk. With inlining, `runtime.Caller(i)` yields the logical frame's PC, and `FuncForPC` of that PC names the same logical function, so the name and file stay consistent.

`NewFrameFromPC` does no classification. `TestNewFrameFromPtr` fails only because its assertion hard-codes `do/stacktrace/`.

## Goals / Non-Goals

**Goals:**
- Classify own-package frames by function import path, with the `stacktrace` package path derived at runtime, not hard-coded.
- Identify GOROOT frames by a path-prefix match that tolerates an empty GOROOT.
- Make the classifier and the frame search directly table-testable without relying on the checkout location.
- Keep every currently pinned frame result: the provider frames `newServiceEager`/`newServiceAlias`, `example1`/line 12, `Frame.String` format, and GOPATH-relative paths.

**Non-Goals:**
- Skipping all library frames to report user code (a behavior change, out of scope).
- Changing `removeGoPath`, `shortFuncName` or `NewFrameFromPC` logic.
- Changing the search depth constant (10 frames). D5 notes the one-frame shift that the extraction causes.
- Exposing invocation frames through a new exported API.
- Normalizing GOROOT separators on Windows (see Risks).

## Decisions

**D1. Derive the package path from a type in the package.**
Store `selfPkgPath = reflect.TypeOf(pkgMarker{}).PkgPath()` once at package init, using an unexported empty struct type. The commented-out `packageName = reflect.TypeOf(fake{}).PkgPath()` in the current file shows this was the original intent.
- *Alternative:* `runtime.FuncForPC` on a function in the package, then trimming the name. This works but needs string surgery.
- *Alternative:* `debug.ReadBuildInfo().Main.Path`. Rejected: it gives the *main* module, which is the consumer's module, not this library's.

**D2. Classify own-package frames by name prefix `selfPkgPath + "."`.**
A frame belongs to the package iff `strings.HasPrefix(funcName, selfPkgPath+".")`, using the **unshortened** name from `runtime.FuncForPC`. The trailing `.` excludes sibling paths such as `.../stacktracex.Helper` and the external test package `.../stacktrace_test.TestCaller`. Matching on the function name rather than the file excludes lookalike packages such as `github.com/acme/do/stacktrace` and `github.com/palantir/stacktrace`, and is immune to checkout, module-cache and vendor layout and to `-trimpath`.
- *Alternative:* compare `filepath.Dir(file)` with the package directory from `runtime.Caller(0)` at init. Rejected: still file-based, and fragile under `-trimpath` and symlinked checkouts.

**D3. Identify GOROOT frames by path prefix, on the raw file path.**
A frame is a GOROOT frame iff `goroot != "" && strings.HasPrefix(file, goroot+"/")`. `file` is the raw path from `runtime.Caller`, before `removeGoPath`. `removeGoPath` still shapes the reported `Frame.File`.
- Prefix plus `/` stops `/usr/local/gopath/...` from matching `/usr/local/go`.
- The non-empty guard stops an empty GOROOT (a `-trimpath` build without `GOROOT` in the environment) from matching every frame. Under `-trimpath`, GOROOT files have relative paths such as `runtime/proc.go`, so with an empty GOROOT they are not recognized as GOROOT frames. That is harmless here: the first frame after the `stacktrace` package is always the calling library code, which is reached before any GOROOT frame.
- *Alternative:* keep `strings.Contains`. Rejected: it causes Defect 3 and Defect 4.
- *Alternative:* classify GOROOT frames by function name (standard-library import paths have no dot in their first element). Rejected: `main.main` and GOPATH-mode packages also lack a dot, so the rule would misfire.

**D4. Extract a pure classifier and an injectable frame search.** These are seams (Fowler: *Extract Function*, *Introduce Parameter*):
- `skipFrame(selfPkg, goroot, function, file string) bool`: skip iff the frame is a GOROOT frame (D3), **or** the function has the `selfPkg+"."` prefix (D2) and the file does not end in `_test.go` (D6). The `isExamplePkg` clause is dropped. No file of the `stacktrace` package can live under an examples directory, so the clause can never affect a stacktrace-package frame.
- `callerFrame(caller func(skip int) (pc uintptr, file string, line int, ok bool), funcName func(pc uintptr) (string, bool), depth int) (Frame, bool)`: the loop body of today's `NewFrameFromCaller`, parameterized so tests can feed synthetic stacks. It classifies with `skipFrame(selfPkgPath, goroot, name, rawFile)` and builds the reported frame with `removeGoPath(rawFile)` and `shortFuncName(name)`.
- `NewFrameFromCaller()` becomes `callerFrame(runtime.Caller, funcNameForPC, 10)`, where `funcNameForPC` wraps `runtime.FuncForPC(pc)` and returns `false` for a nil `*Func`. Its exported signature is unchanged.
- *Alternative:* `runtime.Callers` + `runtime.CallersFrames` and `Frame.Function`, which the runtime docs recommend. Not taken: it changes the allocation profile of the invocation hot path and the resolution code, which this fix doesn't need. `Caller` + `FuncForPC` stay consistent for inlined frames (see Context).

**D5. Frame offset and depth after extraction.**
`runtime.Caller` passed as a function value is called from `callerFrame`, so index 0 is `callerFrame` and index 1 is `NewFrameFromCaller`. Both are skipped by D2, so the real-caller result is unchanged. The 10-frame window now counts from `callerFrame`, so it covers 8 frames above `NewFrameFromCaller` instead of 9. This is unobservable, because the first frame above `NewFrameFromCaller` is always reportable library code. In `TestCallerFrame_SearchDepth`, the fake caller's index 0 stands for `callerFrame`, and the rows count skippable frames from that index. Skippable fake frames use function names with the prefix `selfPkgPath + "."` and non-test file names. The reportable fake frame uses `main.main` in `/home/u/app/main.go`.

**D6. Test-file rule unchanged:** a frame whose file ends in `_test.go` is never skipped as an own-package frame. This preserves `example1` resolving to itself.

## Risks / Trade-offs

- [The Go linker escapes `.` in the **last** import-path element of symbol names (verified: package `example.com/esc/lib.v2` has `PkgPath()` `example.com/esc/lib.v2` but symbol `example.com/esc/lib%2ev2.Names`)] → The last element of our path is `stacktrace`, which has no dot, so the prefix matches. `TestSelfPkgPath_MatchesSymbolPrefix` asserts that `runtime.FuncForPC` of a known package function has the prefix `selfPkgPath + "."`. If a future rename puts a dot in that element, the test catches it.
- [The extraction subtly changes the frame offset] → See D5. The end-to-end tests (`TestStacktrace`, `TestExplainService_ProviderFrame`, the alias invocation test) run against real stacks and pin `example1`/line 12, `newServiceEager` and `newServiceAlias`.
- [Hidden callers depend on the exact frame for alias services] → Covered by the existing `service_*_test.go` suites, which must pass unchanged except for the newly added alias-file assertion.
- [On Windows, `runtime.GOROOT()` may use `\` while frame paths use `/`] → Unverified and not claimed as a defect. Today's `strings.Contains` has the same exposure, so this change does not make it worse. It needs its own failing test before any change.
- [A `GOROOT` environment variable with a trailing `/` would not match `goroot + "/"`] → Unverified and not claimed. Same handling as above.

## Public API & Defaults

- No exported identifier added, removed or changed. `NewFrameFromCaller`, `NewFrameFromPC`, `Frame`, `Frame.String`, `ExplainService`, `ExplainNamedService` and `ExplainServiceOutput` keep their signatures.
- Behavior:
  - In layouts where the old substring matched (a checkout ending in `do`, normal build), results are identical.
  - In other checkouts, the module cache, vendor directories and `-trimpath` builds, results now match that same contract instead of returning the `stacktrace` package's own frame or nothing.
  - A frame under a directory that only shares GOROOT's prefix (e.g. `/usr/local/gopath`) is no longer treated as a GOROOT frame.
- No new defaults or knobs. Override is not applicable, as justified in the proposal (Defaults & Control).
- Not a breaking change.

## Test Strategy

| Scenario | Test | Kind |
|----------|------|------|
| Caller frame from a stacktrace test helper | `stacktrace/stacktrace_test.go: TestStacktrace` (assert suffix `stacktrace/stacktrace_test.go`) | unit, real stack |
| Provider frame shown by ExplainService (eager, alias) | `di_explain_frame_test.go` (package `do_test`): `TestExplainService_ProviderFrame/eager`, `/alias` | table, exported API, real stack |
| Invocation frame of an alias service is recorded at the alias implementation | `service_alias_test.go: TestServiceAlias_getInstanceAny` (add file assertion) | unit |
| Classify frames across layouts and lookalike packages (16 rows) | `stacktrace/stacktrace_internal_test.go: TestSkipFrame_Layouts/<name>` (subtest names = the `name` column) | table |
| Frames in GOROOT are skipped by path prefix (3 rows) | `stacktrace/stacktrace_internal_test.go: TestSkipFrame_GOROOTPrefix/<name>` | table |
| Empty GOROOT does not skip every frame (2 rows) | `stacktrace/stacktrace_internal_test.go: TestSkipFrame_EmptyGOROOT/<name>` | table |
| Caller frame in a -trimpath build | `stacktrace/stacktrace_test.go: TestStacktrace` run as `env -u GOROOT go test -race -trimpath -count=1 -run 'TestStacktrace$' ./stacktrace/` | unit, real stack, build mode |
| Search depth boundary (2 rows) | `stacktrace/stacktrace_internal_test.go: TestCallerFrame_SearchDepth/<name>` with a fake `caller` (D5) | table |
| Unresolvable program counter | `stacktrace/stacktrace_test.go: TestNewFrameFromPC_ZeroPC` | unit |
| Frame string format | `stacktrace/stacktrace_test.go: TestFrame_String` (assert suffix `stacktrace/stacktrace_test.go:example1:12`) | unit |

Additional checks, not tied to a single scenario:

| Check | Test / command | Kind |
|-------|----------------|------|
| Guard: symbol prefix matches the derived package path | `stacktrace/stacktrace_internal_test.go: TestSelfPkgPath_MatchesSymbolPrefix` | unit |
| Provider frame from a PC (path-agnostic suffix) | `stacktrace/stacktrace_test.go: TestNewFrameFromPtr` (assert suffix `stacktrace/stacktrace_test.go`) | unit |
| Provider frames via unexported constructors (unchanged) | `service_test.go: TestInferServiceProviderStacktrace` | unit |
| Checkout independence, real stacks (spec rows `own_checkout_do`, `own_checkout_my_fork`) | tasks 5.4: `go test -race -count=1 . ./stacktrace/` in scratch copies named `do` and `my-fork` | integration |
| Vendored consumer layout, real stacks (spec row `own_vendored`) | tasks 5.5: scratch consumer with `replace` + `go mod vendor`; the `Invoked:` line names `service_eager.go:newServiceEager` | integration |

**Hot paths and their coverage:**
- Service invocation tracking. `getInstance` in lazy, eager and alias services calls `NewFrameFromCaller` on every invocation, up to `MaxInvocationFrames`. Covered by the existing `service_*_test.go` suites and their concurrent invocation tests under `-race`, with goleak via `TestMain`, plus the alias invocation-frame assertion.
- Provider tracking. `newServiceEager` and `newServiceAlias` call `NewFrameFromCaller`, and `newServiceLazy` calls `NewFrameFromPC`. Covered by `TestExplainService_ProviderFrame` through the exported API and by `TestInferServiceProviderStacktrace`.
- Explain output (`di_explain.go`). Covered by the existing `di_explain_test.go`, which must stay green and unmodified, and by `TestExplainService_ProviderFrame`.
- Classifier: the three `TestSkipFrame_*` tables cover every branch: GOROOT prefix, empty GOROOT, sibling prefix, own package, own-package test file, delimiter siblings, other packages and lookalikes.

**RED expectation** (in this `doger` checkout):
- Group 1:
  - `TestStacktrace`, `TestFrame_String`, `TestExplainService_ProviderFrame` and `TestServiceAlias_getInstanceAny` fail on assertions: wrong frame.
  - `TestSkipFrame_Layouts` fails to compile on the missing `skipFrame` only.
- Group 2 (after group 1's GREEN, which keeps the old GOROOT rule):
  - `TestStacktrace` under `-trimpath` fails with found `false`.
  - `TestSkipFrame_GOROOTPrefix/goroot_sibling_gopath` and `TestSkipFrame_EmptyGOROOT/goroot_empty_consumer` fail on assertions under the substring rule. The other three rows pass on arrival, because they pin unchanged behavior.
- Group 3: `TestCallerFrame_SearchDepth` fails to compile on the missing `callerFrame` only.

## Migration Plan

None needed. The change is internal to the `stacktrace` package and its tests. Rollback is a plain revert. It lands under the current module path, before `rename-module-doger`, which then carries it over mechanically and needs no special handling, because `selfPkgPath` is derived at runtime.
