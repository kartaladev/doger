# Tasks

Bug-fix change: group 1 opens with the regression tests that reproduce the defect on the **current** code. No production change comes before they are seen failing. All commands run from the repository root, under the current module path (`github.com/samber/do/v2`). This change lands before `rename-module-doger`.

## 1. Caller frame skips only the stacktrace package's own frames, by import path

- [ ] 1.1 RED (regression): Make the path assertions checkout-independent. In `stacktrace/stacktrace_test.go`, change the three hard-coded suffixes to `stacktrace/stacktrace_test.go` (`TestStacktrace:31`, `TestNewFrameFromPtr:44`) and `stacktrace/stacktrace_test.go:example1:12` (`TestFrame_String:57`). These cover scenarios "Caller frame from a stacktrace test helper" (doger row) and "Frame string format". Verify: `go test -race -count=1 -run 'TestStacktrace|TestFrame_String|TestNewFrameFromPtr' ./stacktrace/`.
  - `TestStacktrace` and `TestFrame_String` FAIL with function `NewFrameFromCaller` / file `.../stacktrace/stacktrace.go` (wrong frame).
  - `TestNewFrameFromPtr` PASSES, because only its assertion was directory-dependent.
- [ ] 1.2 RED (regression): Add the file assertion from scenario "Invocation frame of an alias service is recorded at the alias implementation" to `service_alias_test.go: TestServiceAlias_getInstanceAny`: the single invocation frame's file contains `service_alias.go`. Verify: `go test -race -count=1 -run 'TestServiceAlias_getInstanceAny|TestServiceAlias_getInstance$|TestInferServiceProviderStacktrace' .` FAILS with frame file `.../stacktrace/stacktrace.go`, the same failure seen in the baseline.
- [ ] 1.3 RED: Add the table test `stacktrace/stacktrace_internal_test.go: TestSkipFrame`, with one named subtest per row of scenarios "Classify frames across layouts and lookalike packages" (12 rows) and "Frames in GOROOT are skipped" (2 rows). Call `skipFrame(selfPkg, goroot, function, file)` with the row's values. Verify: `go test -race -count=1 -run TestSkipFrame ./stacktrace/` FAILS on the missing symbol `skipFrame`, and on nothing else.
- [ ] 1.4 GREEN: In `stacktrace/stacktrace.go`:
  - add the unexported marker type and `selfPkgPath = reflect.TypeOf(pkgMarker{}).PkgPath()` (design D1);
  - add `skipFrame`, which skips a frame iff its file contains the non-empty `goroot`, or (`strings.HasPrefix(function, selfPkg+".")` and the file does not end with `_test.go`) (design D2/D5);
  - make `NewFrameFromCaller` classify each frame with `skipFrame(selfPkgPath, goroot, f.Name(), file)`, using the **unshortened** function name.
  
  Verify: the commands from 1.1–1.3 all pass, and `make test` is fully green (0 failures; previously 6).
- [ ] 1.5 REFACTOR: *Remove Dead Code*: delete `packageName`, `packageNameStacktrace` and `packageNameExamples` and the commented-out `fake` type. *Rename Variable* if needed for clarity. Update the `NewFrameFromCaller` doc comment so it lists the real filtering rules: GOROOT, and the stacktrace package's own non-test frames. Verify: `make test` and `make lint` pass.

## 2. Search depth boundary

- [ ] 2.1 RED: Add `stacktrace/stacktrace_internal_test.go: TestCallerFrame_SearchDepth`, with rows `9_skippable_then_reportable` (found=true) and `10_skippable_then_reportable` (found=false) from scenario "Search depth boundary". Use a fake `caller` and a fake `funcName` resolver. Verify: `go test -race -count=1 -run TestCallerFrame_SearchDepth ./stacktrace/` FAILS on the missing symbol `callerFrame`.
- [ ] 2.2 GREEN: *Extract Function* + *Introduce Parameter*: move the loop out of `NewFrameFromCaller` into `callerFrame(caller, funcName, depth)` (design D3/D4). `NewFrameFromCaller` calls it with `runtime.Caller`, the real resolver and `10`. Verify: `TestCallerFrame_SearchDepth` passes, `TestStacktrace` still reports `example1` at line 12, and `make test` is green.
- [ ] 2.3 REFACTOR: none needed beyond 2.2, which is itself the extraction. Confirm there is no duplicated GOPATH or short-name handling between `callerFrame` and `NewFrameFromPC`; if there is, *Extract Function* for the shared frame construction. Verify: `make test` and `make lint` pass.

## 3. Characterization: unresolvable PC and symbol-prefix guard

These tests pin behavior that is already correct, or that the fix newly relies on. They are characterization tests, so they are expected to **pass** when added. They are not RED steps.

- [ ] 3.1 Add `stacktrace/stacktrace_test.go: TestNewFrameFromPC_ZeroPC` (scenario "Unresolvable program counter": `NewFrameFromPC(0)` returns `Frame{}` and `false` without panicking). Verify: `go test -race -count=1 -run TestNewFrameFromPC_ZeroPC ./stacktrace/` passes. If it fails, stop and report: that would be a second, separate defect.
- [ ] 3.2 Add `stacktrace/stacktrace_internal_test.go: TestSelfPkgPath_MatchesSymbolPrefix`. It checks that the `runtime.FuncForPC` name of a known package function starts with `selfPkgPath + "."` (design risk: symbol escaping of dots in the last path element). Verify: it passes, and it fails if `selfPkgPath` is temporarily replaced with a wrong value. Revert that probe.
- [ ] 3.3 Confirm the provider-frame scenario rows (eager → `newServiceEager` in `service_eager.go`, alias → `newServiceAlias` in `service_alias.go`) are still asserted by the unmodified `TestInferServiceProviderStacktrace`. Verify: `go test -race -count=1 -run TestInferServiceProviderStacktrace .` passes.

## 4. Integration checks

- [ ] 4.1 Full suite under race and goleak: `make test`. Verify: all packages `ok`, including `.`, `./stacktrace`, `./tests` and `./http`, with no goleak reports.
- [ ] 4.2 Explain output is still correct end to end: `go test -race -count=1 -run 'TestExplain' .` passes, and `di_explain_test.go` is unmodified.
- [ ] 4.3 Lint: `make lint` reports 0 issues.
- [ ] 4.4 Checkout independence: copy the repository into a scratch directory named `do` and run `go test -race -count=1 . ./stacktrace/` there. Verify: it passes there as well as in this `doger` checkout. Delete the scratch copy afterwards.

## Workflow follow-up

- Archive this change after review, then resume `rename-module-doger` at task 1.1, re-recording the baseline, which must now be green.
- The `runtime.GOROOT()`-may-be-empty question (design.md Open Questions) needs its own failing test before any change is proposed.
