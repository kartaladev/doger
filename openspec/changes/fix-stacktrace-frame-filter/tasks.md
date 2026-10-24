# Tasks

Bug-fix change: group 1 opens with regression tests that reproduce the defect on the **current** code. No production change comes before they are seen failing. All commands run from the repository root, under the current module path (`github.com/samber/do/v2`). This change lands before `rename-module-doger`. Per the schema's apply rule for bug-fix changes, the baseline failures listed in proposal.md (Defect) may stay red until their GREEN step: the 6 tests of Defect 1 until 1.5, and `TestStacktrace` under `-trimpath` (Defect 3) until 2.3. Every other pre-existing test must stay green.

## 1. Own-package frames are classified by import path

- [x] 1.1 RED (regression): in `stacktrace/stacktrace_test.go` change the hard-coded suffixes to `stacktrace/stacktrace_test.go` (`TestStacktrace:31`, `TestNewFrameFromPtr:44`) and `stacktrace/stacktrace_test.go:example1:12` (`TestFrame_String:57`) for scenarios "Caller frame from a stacktrace test helper" and "Frame string format"; verify `go test -race -count=1 -run 'TestStacktrace|TestFrame_String|TestNewFrameFromPtr' ./stacktrace/` fails on `TestStacktrace` and `TestFrame_String` with function `NewFrameFromCaller` (wrong frame), while `TestNewFrameFromPtr` passes on arrival (record this, then temporarily set its suffix to `stacktrace/x_test.go`, see it fail, and revert)
- [x] 1.2 RED (regression): add `di_explain_frame_test.go` (package `do_test`, exported API only) with `TestExplainService_ProviderFrame`, rows `eager` and `alias` from scenario "Provider frame shown by ExplainService"; verify `go test -race -count=1 -run TestExplainService_ProviderFrame .` fails on both rows with `Invoked.File` ending in `stacktrace/stacktrace.go`
- [x] 1.3 RED (regression): add the scenario "Invocation frame of an alias service is recorded at the alias implementation" assertion to `service_alias_test.go: TestServiceAlias_getInstanceAny` (the single invocation frame's `File` contains `service_alias.go`); verify `go test -race -count=1 -run 'TestServiceAlias_getInstanceAny' .` fails on that assertion with file `.../stacktrace/stacktrace.go`
- [x] 1.4 RED: add `stacktrace/stacktrace_internal_test.go: TestSkipFrame_Layouts` with one subtest per row of scenario "Classify frames across layouts and lookalike packages" (16 rows, subtest name = `name` column, `selfPkg` = `<lib>/stacktrace`, `goroot` = `/usr/local/go`) calling `skipFrame(selfPkg, goroot, function, file)`; verify `go test -race -count=1 -run TestSkipFrame_Layouts ./stacktrace/` fails to compile naming only the undefined `skipFrame`
- [x] 1.5 GREEN: in `stacktrace/stacktrace.go` add `pkgMarker` + `selfPkgPath` (design D1) and `skipFrame` with the own-package rule (D2, D6) but the GOROOT rule still `strings.Contains(file, goroot)`, and make `NewFrameFromCaller` call `skipFrame(selfPkgPath, goroot, f.Name(), file)` with the unshortened name; verify the 1.1–1.4 commands pass and `make test` reports 0 failures (was 6)
- [x] 1.6 REFACTOR: *Remove Dead Code* (`packageName`, `packageNameStacktrace`, `packageNameExamples`, the commented-out `fake` type) and update the `NewFrameFromCaller` doc comment to list the real rules (GOROOT frames, the stacktrace package's own non-test frames); verify `make test` and `make lint` pass

## 2. GOROOT frames are identified by path prefix

- [ ] 2.1 RED (regression): run scenario "Caller frame in a -trimpath build" against the group 1 code; verify `env -u GOROOT go test -race -trimpath -count=1 -run 'TestStacktrace$' ./stacktrace/` fails with found `false`, function `""` and line `0` (Defect 3)
- [ ] 2.2 RED: add `TestSkipFrame_GOROOTPrefix` (3 rows) and `TestSkipFrame_EmptyGOROOT` (2 rows) to `stacktrace/stacktrace_internal_test.go` from scenarios "Frames in GOROOT are skipped by path prefix" and "Empty GOROOT does not skip every frame" (subtest name = `name` column); verify `go test -race -count=1 -run 'TestSkipFrame_GOROOTPrefix|TestSkipFrame_EmptyGOROOT' ./stacktrace/` fails on assertions in exactly `goroot_sibling_gopath` and `goroot_empty_consumer` (Defect 4 and 3 under the substring rule), with the other 3 rows passing on arrival (record this; they pin unchanged behavior)
- [ ] 2.3 GREEN: change the GOROOT rule in `skipFrame` to `goroot != "" && strings.HasPrefix(file, goroot+"/")`, and pass `NewFrameFromCaller`'s raw `runtime.Caller` file (before `removeGoPath`) to `skipFrame` (design D3); verify the 2.1 and 2.2 commands pass and `make test` is green
- [ ] 2.4 REFACTOR: none needed, because the GOROOT rule is one expression inside `skipFrame` and the reported `File` still goes through `removeGoPath` unchanged; verify `make test` and `make lint` pass

## 3. Search depth boundary

- [ ] 3.1 RED: add `stacktrace/stacktrace_internal_test.go: TestCallerFrame_SearchDepth` with rows `9_skippable_then_reportable` (found=true) and `10_skippable_then_reportable` (found=false) from scenario "Search depth boundary", using a fake `caller` and `funcName` as described in design D5; verify `go test -race -count=1 -run TestCallerFrame_SearchDepth ./stacktrace/` fails to compile naming only the undefined `callerFrame`
- [ ] 3.2 GREEN: *Extract Function* + *Introduce Parameter*: move the loop of `NewFrameFromCaller` into `callerFrame(caller, funcName, depth)` and call it as `callerFrame(runtime.Caller, funcNameForPC, 10)` (design D4, D5); verify `TestCallerFrame_SearchDepth` passes, `go test -race -count=1 -run 'TestStacktrace$' ./stacktrace/` still reports `example1` at line 12, and `make test` is green
- [ ] 3.3 REFACTOR: none needed beyond 3.2, which is itself the extraction; confirm no GOPATH or short-name handling is duplicated between `callerFrame` and `NewFrameFromPC`, and apply *Extract Function* for shared frame construction only if it is; verify `make test` and `make lint` pass

## 4. Characterization: unresolvable PC, symbol-prefix guard, provider frames

- [ ] 4.1 Characterization: add `stacktrace/stacktrace_test.go: TestNewFrameFromPC_ZeroPC` for scenario "Unresolvable program counter" (`NewFrameFromPC(0)` returns the zero `Frame` and `false`, wrapped in `assert.NotPanics`); verify `go test -race -count=1 -run TestNewFrameFromPC_ZeroPC ./stacktrace/` passes on arrival (record this, then temporarily assert `true`, see it fail, and revert; a real failure means a separate defect, so stop and report)
- [ ] 4.2 Characterization: add `stacktrace/stacktrace_internal_test.go: TestSelfPkgPath_MatchesSymbolPrefix` asserting the `runtime.FuncForPC` name of a known package function starts with `selfPkgPath + "."` (design Risks: symbol escaping); verify it passes, fails when `selfPkgPath` is temporarily replaced with `github.com/samber/do/v2/stacktracex`, and revert the probe
- [ ] 4.3 Characterization: confirm the unmodified `TestInferServiceProviderStacktrace` still asserts the eager (`newServiceEager`, `service_eager.go`) and alias (`newServiceAlias`, `service_alias.go`) provider frames; verify `go test -race -count=1 -run TestInferServiceProviderStacktrace .` passes

## 5. Integration checks

- [ ] 5.1 Full suite under race and goleak: verify `make test` reports all packages `ok`, including `.`, `./stacktrace`, `./tests` and `./http`, with no goleak reports
- [ ] 5.2 Explain output end to end: verify `go test -race -count=1 -run 'TestExplain' .` passes (including `TestExplainService_ProviderFrame`) and `git diff --exit-code di_explain_test.go` shows `di_explain_test.go` unmodified
- [ ] 5.3 Lint: verify `make lint` reports 0 issues
- [ ] 5.4 Checkout independence (spec rows `own_checkout_do`, `own_checkout_my_fork`): copy the repository into scratch directories named `do` and `my-fork` and verify `go test -race -count=1 . ./stacktrace/` passes in each, then delete the copies
- [ ] 5.5 Vendored consumer (spec row `own_vendored`): in a scratch module with `require github.com/samber/do/v2 v2.0.0`, `replace` pointing at this checkout and `GOWORK=off`, run `go mod vendor`, then `go run -mod=vendor .` printing `do.ExplainService[*DB](i)` after `do.ProvideValue(i, &DB{})`; verify the `Invoked:` line contains `vendor/github.com/samber/do/v2/service_eager.go:newServiceEager` (it named `stacktrace.go:NewFrameFromCaller` before the fix), then delete the scratch module

## Workflow follow-up

- Archive this change after review, then resume `rename-module-doger` at task 1.1, re-recording the baseline, which must now be green.
