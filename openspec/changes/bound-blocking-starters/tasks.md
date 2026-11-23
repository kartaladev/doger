# Tasks

Test files: `root_start_timeout_test.go`, `root_start_slow_test.go` (new). Decisions D1–D14 are in design.md. Every timing test runs in `synctest.Test` (fake-clock tests in `testWithTimeout`), releases its blocking user code before it ends (the goleak check in TestMain), and runs with `-race`. A RED test whose Starter blocks until released fails before the feature exists as a synctest deadlock report ("all goroutines are durably blocked"), or as a `testWithTimeout` failure; both count as the expected RED.

## 1. Public API scaffolding (no behaviour)

- [x] 1.1 Add `InjectorOpts.SlowStartThreshold`, `InjectorOpts.StartTimeout`, `DefaultSlowStartThreshold` and `ErrStartTimeout` (in `libSentinels`), with godoc stating the defaults and the zero/negative semantics, all unused so far; verify `go build ./...`, `make test` green, and the `libSentinels` test in `errors_test.go` updated and passing

## 2. Default waits for every step (pin unchanged behaviour)

- [x] 2.1 RED: add `TestStartTimeout_DefaultWaits` (scenario "No start timeout waits for a slow step", rows zero/negative/ctx_deadline, ctx_deadline expecting `started: true` and the exact "doger: start aborted: context deadline exceeded" message); it should pass on arrival, since this is the pinned default, so prove the assertions can fail by temporarily removing `markStarted` in `startCandidate` (not committed); verify `go test -race -run TestStartTimeout_DefaultWaits .`
- [x] 2.2 GREEN: none needed, as this is existing behaviour; verify the test passes and `make test` is green
- [x] 2.3 REFACTOR: Extract Function `runStartStep` (D1) wrapping the inline `startInstance` call in `startCandidate` and the `callKindHandler` call in `runStage`; Introduce Parameter Object `startStep{candidate, op, kind}`; no behaviour change; verify `make test` and `make lint` pass

## 3. Start timeout abandons a stuck step (with its hook call and Goexit)

- [x] 3.1 RED: add `TestStartTimeout_Abandons` (scenario "A step that outlives the start timeout is abandoned"), `TestStartTimeout_CancelsStepContext`, `TestStartTimeout_Goexit`, `TestStartTimeout_FakeClock/{clockwork,manual}` (scenario "Start timeout waits for the container clock") and `TestStartTimeout_AfterStartHookOnce` (scenario "HookAfterStart fires once at abandonment"); verify `go test -race -run 'TestStartTimeout_(Abandons|CancelsStepContext|Goexit|FakeClock|AfterStartHookOnce)' .` fails: the 6s rows return nil after 6s, and the blocking tests report a synctest deadlock or time out
- [x] 3.2 GREEN: in `runStartStep`, when `StartTimeout > 0`, run the call in a goroutine with a buffered(1) outcome channel and a `withClockTimeout` step ctx, select on outcome or `stepCtx.Done()`, build the timeout `*StartError` (D4, timeout branch) and register the step as abandoned; the existing `onAfterStart` call then receives the timeout cause (D7); verify the tests pass and `make test` is green
- [x] 3.3 REFACTOR: Extract Function `timeoutCause(runCtx, d)`; Extract Function `awaitStep` for the select; verify `make test` and `make lint` pass

## 4. Ended run context and Shutdown abandon the step

- [x] 4.1 RED: add `TestStartTimeout_RunContextEnds/{deadline,cancel,shutdown}` (scenario "An ended run context abandons the step", asserting the exact messages with the cause once) and `TestStartTimeout_ShutdownDuringWait` (scenario "Shutdown during an abandoned step still shuts the service down"); verify `go test -race -run 'TestStartTimeout_(RunContextEnds|ShutdownDuringWait)' .` fails on the message (the timeout branch from group 3 prints "after 1h…" or doubles the cause)
- [x] 4.2 GREEN: in `timeoutCause`, return the bare `ErrStartTimeout` when `runCtx` ended and let `withAbortCause` append the cause once (D4, run-ctx branch); verify the tests pass, the existing "Shutdown cancels a running Start" and "An aborted run matches ErrShutdown…" tests stay green, and `make test` is green
- [x] 4.3 REFACTOR: Replace Nested Conditional with Guard Clauses in `timeoutCause` if it branched more than once; otherwise none needed; verify `make test` and `make lint` pass

## 5. Abandonment under parallel start

- [x] 5.1 RED: add `TestStartTimeout_Parallel` (scenario "Abandonment under parallel start"); verify `go test -race -run TestStartTimeout_Parallel .` fails for the expected reason, or passes on arrival because `startStartersParallel` reuses `startCandidate`; if it passes, record that and show it fails with the guard bypassed temporarily (not committed)
- [x] 5.2 GREEN: make any scheduler adjustment needed so that a timeout is an ordinary first error (D6); verify the test passes under `-race` with goleak clean, and `make test` is green
- [x] 5.3 REFACTOR: none needed, the scheduler stays unchanged by design (D6); verify `make test` and `make lint` pass

## 6. Abandoned step is not called again; atomic claim and guarded late mark

- [x] 6.1 RED: add `TestStartTimeout_AbandonedNotRestarted` (rows running/late_nil/late_error/late_panic/scope_start/runnable_running/runnable_late_nil/runnable_late_error), `TestStartTimeout_LateReturnRacesRun` and `TestStartTimeout_LateReturnAfterScopeShutdown`; verify `go test -race -run 'TestStartTimeout_(AbandonedNotRestarted|LateReturn)' .` fails (the running rows call Start or Run a second time; late_nil calls it again)
- [x] 6.2 GREEN: add the `abandoned` map under `startedMu`, the single claim at the top of `runStartStep` (skip if recorded, fail fast if abandoned, both before any hook), the late mark under `lifecycleMu` → `startedMu` with the `scopeHolds` and `!closingInLine` checks, and `abandoned` cleanup in `forget` (D5); verify the tests pass with `-race`, goleak clean, and `make test` green
- [x] 6.3 REFACTOR: Move Function for the claim and the late mark into `root_refresh.go` next to `markStarted`/`markHandled`/`forget`; Rename `stepKey` values for clarity if needed; verify `make test` and `make lint` pass
- [x] 6.4 Docs: godoc for `StartTimeout` (abandonment, no re-entry, the per-container guarantee and clones), `ErrStartTimeout`, `HookBeforeStart`/`HookAfterStart` (ctx wording, the abandonment call), `StartWithContext` (abandonment paragraph) and `InjectorOpts.Clock` (both new delays); add `ExampleInjectorOpts_startTimeout` in `root_start_example_test.go`; verify `go test -run ExampleInjectorOpts_startTimeout .` passes and `go doc doger.InjectorOpts` renders

## 7. Slow start step warning

- [x] 7.1 RED: add `TestSlowStart_Warning`, `TestSlowStart_ChildScope`, `TestSlowStart_ParallelNamesOnlySlow`, `TestSlowStart_PanicAfterWarning`, `TestSlowStart_StopsAtAbandonment`, `TestSlowStart_NoTimerWithoutLogger` (counting Clock), `TestSlowStart_FakeClock` and `TestSlowStart_NoTimerLeak`, plus rows `slow_start` in `TestLogging_Events`; verify `go test -race -run 'TestSlowStart_|TestLogging_Events' .` fails on the rows expecting 1 record and on the warn_level counting row
- [x] 7.2 GREEN: in `runStartStep`, resolve the threshold, arm `Clock.AfterFunc` only when `logEnabled(slog.LevelWarn)`, emit one WARN "start step is slow" with the scope/service/step/kind/threshold attributes, and stop the timer when `runStartStep` returns (D2); verify the tests pass with `-race`, goleak clean, and `make test` green
- [x] 7.3 REFACTOR: Extract Function `armSlowStartWarning(...) (stop func())`; Extract Function `stepAttrs`; verify `make test` and `make lint` pass
- [x] 7.4 Docs: godoc for `SlowStartThreshold` / `DefaultSlowStartThreshold`; add the "start step is slow" row and both fields to `docs/docs/container/options.md`; verify `go test -run 'TestReadme_SnippetsCompiled|TestDocs_' .` passes and the page lists the record

## 8. Late outcome logging and panic-safe logging on unowned goroutines

- [x] 8.1 RED: add `TestStartTimeout_LateOutcomeLogged` (rows nil/error_debug/error_info/panic/panic_child_scope), `TestSlowStart_PanickingHandler`, and rows `abandoned_returned`/`abandoned_error` in `TestLogging_Events`; verify `go test -race -run 'TestStartTimeout_LateOutcomeLogged|TestSlowStart_PanickingHandler|TestLogging_Events' .` fails on the missing records, and the handler test crashes the test binary with "handler boom"
- [x] 8.2 GREEN: emit WARN "abandoned start step returned", DEBUG "abandoned start step error" and `Scope.logRecoveredPanic("start", …)` on the candidate's scope from the late outcome, and wrap the timer callback and the late-outcome records in `logSafely` (D8); verify the tests pass and `make test` is green
- [x] 8.3 REFACTOR: reuse `stepAttrs` from 7.3 for the late-outcome records (Inline Function if a local duplicate appeared); verify `make test` and `make lint` pass
- [x] 8.4 Docs: add the two late-outcome records to the logging table in `docs/docs/container/options.md`; verify the page lists them and `go test -run 'TestDocs_' .` passes

## 9. Docs: a server's Start must return once listening

- [x] 9.1 Add `Example_starterHTTPServer` in `di_lifecycle_example_test.go` (listen on `127.0.0.1:0` in Start, `go Serve`, `Shutdown(ctx)` calls `srv.Shutdown`), showing the right shape; verify `go test -race -run Example_starterHTTPServer .` passes with goleak clean
- [x] 9.2 Update the godoc of the Starter variants (`di_lifecycle.go`) and `Runner` (`runnable.go`): name servers, state "return once listening", describe the wrong blocking shape, and point to the example, the warning and `StartTimeout`; verify `go doc doger.Starter`, `go doc doger.Runner` and `go vet ./...`
- [x] 9.3 RED: add `TestDocs_StarterSnippetsCompiled` to `docs_snippets_test.go` (reusing `docsRegions`) pinning every Go block of `docs/docs/service-lifecycle/starter.md` to a compiled snippet region; verify `go test -run TestDocs_StarterSnippetsCompiled .` fails on the not-yet-written "A Start that never returns" blocks (or on existing unpinned blocks, which then get regions too)
- [x] 9.4 GREEN: write the "A Start that never returns" section of `starter.md` (wrong/right snippets, and the diagnosis knobs `Logger`, `SlowStartThreshold`, `StartTimeout`), its compiled snippet regions, and the README Quickstart note; verify `go test -run 'TestDocs_|TestReadme_SnippetsCompiled' .` passes

## 10. Integration checks

- [x] 10.1 Run `make test`, `make lint`, and `go test -race ./...` in every `./http/*` and `./examples/*` module; verify all green
- [x] 10.2 Cross-check each `#### Scenario:` in all four delta specs against design.md's Test Strategy table, and run `openspec validate bound-blocking-starters` (without `--strict`); verify every scenario maps to a passing test and validation reports valid

## 11. Test seam scaffolding (no behaviour; review F2/F5, design D11)

- [x] 11.1 Add the unexported `startSeam` field on `RootScope` with nil-checked `beforeRecord(step)` and `afterStepDone(step)` calls at the two points of design D11, unused by production; verify `go build ./...`, `make test` and `make lint` green

## 12. A removal while a Start returns leaves no record (review F2; proposal Defect)

- [x] 12.1 RED: add `TestStart_RemovalBeforeRecord` (scenario "A removal between the registration check and the record", rows inline/awaited/abandoned) using the `beforeRecord` seam to run `ShutdownNamed`; verify `go test -race -run TestStart_RemovalBeforeRecord .` fails on every row (a stale started entry), the inline row being the regression proof of the pre-existing defect
- [x] 12.2 GREEN: route every start record (`startCandidate`, `runStage`, `finishAbandoned`) through `recordIfHeld(step)`: record under `startedMu`, re-check `scopeHolds`, undo if removed (design D5); verify the test passes under `-race` and `make test` is green
- [x] 12.3 REFACTOR: Inline Function for `markStarted`/`markHandled` call sites now covered by `recordIfHeld`, if unused; Remove Dead Code; verify `make test` and `make lint` pass

## 13. A step's context keeps its lifetime under StartTimeout (review F3)

- [x] 13.1 RED: add `TestStartTimeout_StepContextLifetime` (scenario "A step that returns in time keeps its context until the run ends", rows no_timeout/timeout); verify `go test -race -run TestStartTimeout_StepContextLifetime .` fails on the timeout row (Err() is context.Canceled, and a deadline is reported, while "b" runs)
- [x] 13.2 GREEN: build the step ctx as `context.WithCancelCause(runCtx)` plus a `Clock.AfterFunc` timer cancelling it with `errStepTimedOut`; stop only the timer on a normal return; cancel at abandonment (design D4); verify the test passes, every `TestStartTimeout_*` stays green, and `make test` is green
- [x] 13.3 REFACTOR: Extract Function `newStepContext(runCtx, clock, timeout) (ctx, stopTimer, abandon)`; verify `make test`, `make lint` and `go vet ./...` (lostcancel) pass
- [x] 13.4 Docs: fix the godoc of `StarterWithContextAndError`, `HookBeforeStart` and `StartTimeout` to state the context lifetime (ends at abandonment or with the run; no deadline); verify `go doc` renders and `make lint` passes

## 14. A timeout stays a timeout when the run's context ends right after it (review F5)

- [x] 14.1 RED: add `TestStartTimeout_TimeoutWinsOverLateCancel` (scenario "A timeout stays a timeout when the run's context ends right after it") cancelling the caller's ctx from the `afterStepDone` seam; verify `go test -race -run TestStartTimeout_TimeoutWinsOverLateCancel .` fails on the message (bare "start timeout (start aborted: …)") and on errors.Is(err, context.DeadlineExceeded)
- [x] 14.2 GREEN: decide the cause from `context.Cause(stepCtx) == errStepTimedOut` captured after the select, not from `runCtx.Err()` (design D4); verify the test passes, `TestStartTimeout_RunContextEnds` stays green, and `make test` is green
- [x] 14.3 REFACTOR: Change Function Declaration: `timeoutCause(stepCtx, timeout)` instead of `(runCtx, timeout)`; verify `make test` and `make lint` pass

## 15. The slow warning never races the abandonment (review F4)

- [x] 15.1 RED: add row `threshold_5s` (threshold equal to StartTimeout, 0 records) to `TestSlowStart_StopsAtAbandonment`; verify `go test -race -count=50 -run 'TestSlowStart_StopsAtAbandonment/threshold_5s' .` fails at least once (the outcome depends on which timer fires first)
- [x] 15.2 GREEN: arm the warning only when the threshold is below a set `StartTimeout`, and stop it inside `awaitStep` right after the select, before marking the step abandoned (design D2); verify `-count=50` passes and `make test` is green
- [x] 15.3 REFACTOR: Introduce Parameter Object if `awaitStep` now takes the stop function plus four other arguments; otherwise none needed; verify `make test` and `make lint` pass
- [x] 15.4 Docs: state the arming rule in the `SlowStartThreshold` godoc and `docs/docs/container/options.md`; verify `go test -run 'TestDocs_' .` passes

## 16. The example server's Shutdown never hangs (review F1)

- [x] 16.1 RED: add `TestExampleServer_ShutdownWithoutStart` in `di_lifecycle_example_test.go` (Shutdown with no prior Start, and after a failed bind) under `testWithTimeout`; verify `go test -race -run TestExampleServer_ShutdownWithoutStart .` fails by timing out
- [x] 16.2 GREEN: create `served` in the example's constructor and wait with a `select` on `ctx.Done()` (design D10); verify the test and `Example_starterHTTPServer` pass, and `make test` is green
- [x] 16.3 REFACTOR: none needed, the example stays one type; verify `make test` and `make lint` pass

## 17. The documented server snippet matches ListenAndServe for an empty Addr (review F6)

- [x] 17.1 Docs: default an empty `srv.Addr` to `":http"` in the "starter-listening-server" snippet of `docs/docs/service-lifecycle/starter.md` and its compiled region in `docs_starter_snippets_test.go` (design D10); no runtime test, since binding `:http` needs privileges; verify `go test -run TestDocs_StarterSnippetsCompiled .` and `make lint` pass

## 18. Start timeout and slow warning through dogerclockwork.Wrap (verification W1)

- [x] 18.1 RED: add `TestWrap_StartTimeout` and `TestWrap_SlowStartWarning` to `clock/clockwork/clockwork_test.go` (the clockwork rows of "Start timeout waits for the container clock" and "Slow-start warning waits for the container clock"); they should pass on arrival, so prove each can fail by temporarily passing `RealClock()` in place of the wrapped clock (not committed); verify `(cd clock/clockwork && go test -race -run 'TestWrap_(StartTimeout|SlowStartWarning)' .)`
- [x] 18.2 GREEN: none needed beyond the tests; verify `(cd clock/clockwork && go test -race ./...)` is green

## 19. Integration checks after the review fixes

- [x] 19.1 Run `make test`, `make lint`, and `go test -race ./...` in every `go.work` module; verify all green
- [x] 19.2 Cross-check each `#### Scenario:` in all four delta specs against design.md's Test Strategy table and run `openspec validate bound-blocking-starters` (without `--strict`); verify every scenario maps to a passing test and validation reports valid

## 20. The example server's Shutdown returns with any context (verification V1, folds review F1)

- [x] 20.1 RED: add rows `no_start_background` and `failed_bind_background` (Shutdown with context.Background()) to `TestExampleServer_ShutdownWithoutStart`, and `TestExampleServer_ContainerShutdownAfterFailedBind` (root.Shutdown() after root.Start() failed on the bind), all under `testWithTimeout`; verify `go test -race -run 'TestExampleServer_' .` fails by timing out on the three new cases
- [x] 20.2 GREEN: an atomic `serving` flag set by Start just before `go srv.Serve(ln)`; Shutdown returns `srv.Shutdown(ctx)` without waiting when it is unset (design D10); verify `TestExampleServer_*` and `Example_starterHTTPServer` pass, and `make test` is green
- [x] 20.3 REFACTOR: none expected, the example stays one type; verify `make test` and `make lint` pass

## 21. Test seam godoc matches its use (verification suggestion)

- [x] 21.1 Docs: the `startSeam` godoc in `root_start_step.go` says tests set the callbacks "before the run it observes", not "before the container starts"; verify `make lint` passes

## 22. Release checks, database tests included (verification suggestion)

- [x] 22.1 Run `make test`, `make lint`, and `go test -race ./...` in every `go.work` module; verify all green
- [x] 22.2 Run `make test-sql` (DOGER_SQL_REQUIRE=1: every events/sql database test runs against PostgreSQL 17 and 13 and MySQL 8.4 and 8.0) and `make test-watermill` (events/watermill/wmsql against PostgreSQL 17 and MySQL 8.4); verify both green and that `go test -v` in both modules reports no test skipped for a missing database or DSN; then `make test-sql-down`
- [x] 22.3 Run `openspec validate bound-blocking-starters` (without `--strict`) and cross-check the Test Strategy table; verify valid and every scenario maps to a passing test

## 23. An ended run context waits for the step up to its timeout (code review F1)

- [x] 23.1 RED: rewrite `TestStartTimeout_RunContextEnds/{deadline,cancel,shutdown}` to the scenario "An ended run context waits for a step that ignores it" (StartTimeout 5s, return at 5s, the exact "after 5s … (start aborted: <cause>)" messages, matching context.DeadlineExceeded too) and `TestStartTimeout_ShutdownDuringWait` to "Shutdown during a stuck step shuts the service down" (StartTimeout 5s, Shutdown bound 10s); add `TestStartTimeout_ShutdownAwaitsHonouringStart` ("A Start that honours its context is awaited when Shutdown cancels it") and `TestStartTimeout_ShutdownBoundedBelowTimeout` ("A Shutdown bounded below the start timeout"); verify `go test -race -run 'TestStartTimeout_(RunContextEnds|ShutdownDuringWait|ShutdownAwaitsHonouringStart|ShutdownBoundedBelowTimeout)' .` fails: the rows return at 100ms with the bare "start timeout (start aborted: …)" message, the honouring Start is abandoned at 100ms with its Shutdown method called before its cleanup finished, and the bounded Shutdown calls "srv"'s Shutdown method
- [x] 23.2 GREEN: wait on `returned` or a `timedOut` channel closed by the timer callback, never on `stepCtx.Done()` (design D4); verify the tests pass, every `TestStartTimeout_*` and `TestSlowStart_*` stays green (TimeoutWinsOverLateCancel included), and `make test` is green
- [x] 23.3 REFACTOR: Remove Dead Code: the bare-sentinel branch of `timeoutCause` once only the timer abandons (Inline Function if one branch remains); verify `make test` and `make lint` pass
- [x] 23.4 Docs: godoc of `StartTimeout`, `StartWithContext` and `ErrStartTimeout`: an ended context cancels the step's context at once and the run waits up to the timeout; a Shutdown bounded below StartTimeout stops waiting when its context ends and skips the services it then reaches, so a stuck step keeps running until its call returns, as without the option; update `docs/docs/container/options.md` and the `StartTimeout` lines of `docs/docs/service-lifecycle/starter.md`; verify `go doc` renders, `go test -run 'TestDocs_|TestReadme_SnippetsCompiled' .` and `make lint` pass

## 24. A step returning at the timeout is either started with a live context or abandoned (code review F4)

- [x] 24.1 RED: add `TestStartTimeout_ReturnAtTimeout/{root_start,root_refresh,tenant_start}` (scenario "A step that returns exactly at the timeout"; triage B reproduced it in all three); verify `go test -race -count=200 -run TestStartTimeout_ReturnAtTimeout .` fails at least once (Start returns nil while "b" saw "a"'s context cancelled)
- [x] 24.2 GREEN: order the timer callback and the call's end under the call's mutex: the callback does nothing once the call ended; otherwise it marks the call timed out, registers the step as abandoned, cancels the context and closes `timedOut`, and the call's end becomes its late outcome (design D4, D5); verify `-count=200` passes, every `TestStartTimeout_*` and `TestStart_RemovalBeforeRecord` stays green, and `make test` is green
- [x] 24.3 REFACTOR: Move Function for the abandoned mark into the timer callback's helper if it is still split across `awaitStep`; verify `make test` and `make lint` pass

## 25. A still-running step is skipped with its dependents; the rest starts (code review F2)

- [x] 25.1 RED: add `TestStartTimeout_RefreshSkipsStillRunning` with the seven rows of scenario "Refresh starts what does not depend on a still-running step" (root and tenant, sequential and parallel, `root.Refresh()` over the tenant, and two AsRunnable rows); verify `go test -race -run TestStartTimeout_RefreshSkipsStillRunning .` fails on the sequential rows ("worker" is not started; the parallel rows may pass on arrival, as triage B observed, which is the inconsistency this group removes)
- [x] 25.2 GREEN: the claim skips a still-running step and keeps its error as pending, the run skips candidates that depend on a skipped service (sequential and parallel scheduler, Starters and kind handlers), and returns its first failure or else the first pending error (design D5, D6); verify the test passes, `TestStartTimeout_AbandonedNotRestarted` and `TestStartTimeout_Parallel` stay green, and `make test` is green
- [x] 25.3 REFACTOR: Extract Function for the dependents check shared by the sequential loop, the parallel scheduler and `runStage`; verify `make test` and `make lint` pass
- [x] 25.4 Docs: `StartTimeout` godoc and `options.md`: a covering Refresh or Scope.Start skips a still-running step and its dependents and reports it; verify `make lint` passes

## 26. After root Start abandons a step, only Shutdown follows (code review F3)

- [x] 26.1 RED: add `TestStartTimeout_RootStartAbandonsThenOnlyShutdown` (scenario "After root Start abandons a step, only Shutdown follows"); it pins existing behaviour and should pass on arrival, so prove it can fail by temporarily setting `startStarted` instead of `startFailed` in `finishStart` (not committed, backup kept); verify `go test -race -run TestStartTimeout_RootStartAbandonsThenOnlyShutdown .`
- [x] 26.2 Docs: `StartTimeout` godoc: a late return is recorded and a late failure retried only by a later Refresh or Scope.Start, which exist only when the abandoning run was not root Start; after root Start abandoned a step, Shutdown is the way on; verify `go doc doger.InjectorOpts` renders and `make lint` passes

## 27. The default warning timer on a fake clock is documented (code review F6)

- [x] 27.1 Docs: `InjectorOpts.Clock` and `SlowStartThreshold` godoc and `docs/docs/container/options.md`: with a Warn-enabled Logger each start step arms one timer on the Clock, which counts toward a fake clock's `BlockUntil`; `SlowStartThreshold: -1` removes it; verify `go doc` renders, `go test -run 'TestDocs_' .` and `make lint` pass

## 28. The Goexit difference under StartTimeout is documented (code review F7)

- [x] 28.1 Docs: `StartTimeout` godoc: under the option a `runtime.Goexit` in a Start method or kind handler is reported as a step error; without it the call runs on the caller's goroutine, which Goexit ends, as before this change; verify `go doc` renders and `make lint` passes

## 29. TestWrap_SlowStartWarning never hangs (code review F8)

- [ ] 29.1 Bound the wait for Start in `TestWrap_SlowStartWarning` with a `select` on `time.After`, as `TestWrap_StartTimeout` does; verify `(cd clock/clockwork && go test -race ./...)` and its lint pass

## 30. A late return during root Shutdown is not recorded (triage E, E1)

- [ ] 30.1 RED: add `TestStartTimeout_LateReturnDuringShutdown/{root_starter,root_runnable,tenant_own,tenant_root}` (scenario "A late return during a shutdown is not recorded"); verify `go test -race -run TestStartTimeout_LateReturnDuringShutdown .` fails on the two root rows (recorded as started or handled) and passes on the tenant rows
- [ ] 30.2 GREEN: `finishAbandoned` also refuses the record while the root is closing (`closePhase != closeOpen`) (design D5); verify the test passes, `TestStartTimeout_LateReturnAfterScopeShutdown` stays green, and `make test` is green
- [ ] 30.3 REFACTOR: Extract Function `scopeShuttingDown(scope)` for `closingInLine || root closing` if it is used in more than one place (D12 also needs it); verify `make test` and `make lint` pass

## 31. A child scope Shutdown cancels the running start steps of its subtree (triage A/C, I1)

- [ ] 31.1 RED: add `TestChildShutdown_CancelsSubtreeSteps/{timeout_0,timeout_5s}/{root_start,root_refresh,tenant_start}` (scenario "Child shutdown cancels the running start steps of its subtree"; the timeout_5s/root_start row is not in the scenario and is omitted) and `TestChildShutdown_StuckStepWaitsTimeout` ("Child shutdown waits for a stuck step up to the start timeout"); verify `go test -race -run 'TestChildShutdown_' .` fails: with no timeout tenant.Shutdown() never returns (synctest deadlock), with 5s it returns at the timeout and the Shutdown method runs before the cleanup finished
- [ ] 31.2 GREEN: register every running step's context with its scope (D3 inline child ctx, D4 awaited ctx); `joinClose` cancels the steps of its subtree with a cause matching `ErrShutdown`; the run treats a step of a closing scope as skipped (not recorded, not the run's error) and checks for a closing scope before each later step (design D12); verify the tests pass, `TestScopeShutdown_WaitsForStart`, `TestScopeShutdown_ExportingChildWaitsForStart`, `TestPhase_ClosingWhileShutdownWaitsForStart` and every `TestStartTimeout_*` stay green, and `make test` is green
- [ ] 31.3 REFACTOR: Extract Function for the step-context registry (register, unregister, cancel subtree) out of `runStartStep`; verify `make test`, `make lint` and `go vet ./...` pass
- [ ] 31.4 Docs: godoc of `Scope.Shutdown`/`ShutdownWithContext` (it cancels its subtree's running start steps, then waits bounded by its context) and of the Starter variants (the context also ends when the service's scope shuts down); verify `go doc` renders and `make lint` passes

## 32. Dependents of a still-running step are skipped in any scope (triage B, I3)

- [ ] 32.1 RED: add `TestStartTimeout_AncestorStillRunningSkipsDependent/{depends_on,invoke,refresh}` (scenario "A dependent of a still-running step in an ancestor scope is skipped") plus two same-scope rows from the group 25 verification (requirement "An abandoned step is not started again while it runs": "every candidate that depends on its service, directly or through others"): `earlier_stage_starter` (StartTimeout 5s, "stuck" AsRunnable abandoned by a first `root.Refresh()`, Starter "dep" with `DependsOnNamed("stuck")` registered after it; the second Refresh does not call "dep"'s Start) and `earlier_stage_component` ("stuck" a Starter abandoned by a first Refresh, "dep" a component of a custom `StageBeforeStart` kind depending on "stuck"; the second Refresh does not call its handler); verify `go test -race -run TestStartTimeout_AncestorStillRunningSkipsDependent .` fails on the `tenant.Start()` rows and on both new rows ("dep" is started or handled)
- [ ] 32.2 GREEN: the dependents check consults the abandoned state of every dependency in the graph, in the candidate's own or an ancestor scope, not only the run's skipped set (design D5); verify the test passes, the group 25 tests stay green, and `make test` is green
- [ ] 32.3 REFACTOR: none expected beyond reusing group 25's dependents check; verify `make test` and `make lint` pass

## 33. The no-overlap guarantee holds per instance, across overrides and clones (triage D, I5)

- [ ] 33.1 RED: add `TestStartTimeout_InstanceNotCalledAgain/{clone,override}` (scenario "A still-running instance is not called again through an override or a clone"); verify `go test -race -run TestStartTimeout_InstanceNotCalledAgain .` fails ("inst"'s Start is called 2 times)
- [ ] 33.2 GREEN: an instance-keyed tracker of abandoned calls, shared by pointer by `Clone`/`CloneWithOpts`, filled with the abandoned mark and cleared by the late outcome; the claim skips a step whose comparable instance is tracked (design D14); verify the test passes, the clone tests (`TestRootScope_Clone*`, container-lifecycle-hooks clone tests) stay green, and `make test` is green
- [ ] 33.3 REFACTOR: Move Function for the tracker into its own file `root_start_instances.go` if it grew beyond a few lines; verify `make test` and `make lint` pass
- [ ] 33.4 Docs: `StartTimeout` godoc: the guarantee is per instance across overrides and clones; a non-comparable instance is tracked per registration only; verify `go doc` renders and `make lint` passes

## 34. Health reports check a service only once its start is complete (triage D, I4)

- [ ] 34.1 RED: add `TestHealthReport_ComponentCheckedOnceHandled/{runnable_returned,runnable_running,runnable_late_return}` (health-reports scenario "A component is checked only once its kind handlers handled it"); verify `go test -race -run TestHealthReport_ComponentCheckedOnceHandled .` fails on `runnable_running` (the entry is HealthHealthy and the check ran)
- [ ] 34.2 GREEN: `eligibleForReport` also requires every kind of the service with a registered handler to have handled it (design D13); verify the test passes, every `TestHealthReport_*` and `TestHealthScheduler_*` stays green, and `make test` is green
- [ ] 34.3 REFACTOR: none expected; verify `make test` and `make lint` pass
- [ ] 34.4 Docs: the godoc of `HealthReport` (and the health docs page under `docs/docs/`) states the rule for Starters and kind components alike, and that a Refresh which abandons a step leaves that service unknown until its call returns; verify `go test -run 'TestDocs_' .` and `make lint` pass

## 35. Late-outcome logging is consistent and the Logger godoc is correct (triage E)

- [ ] 35.1 RED: add the AsRunnable panic row to `TestStartTimeout_LateOutcomeLogged` and `step=start` to its panic rows (container-logging scenario "Late outcome of an abandoned step"); verify `go test -race -run TestStartTimeout_LateOutcomeLogged .` fails (the panic records have no `step`/`kind`)
- [ ] 35.2 GREEN: the late panic records carry `step` and `kind` (handle only) (design D8); verify the test passes, `TestLogging_Events` stays green, and `make test` is green
- [ ] 35.3 REFACTOR: reuse `startStep.attrs()` for the panic records instead of a local attribute list; verify `make test` and `make lint` pass
- [ ] 35.4 Docs: `InjectorOpts.Logger` godoc lists "start step is slow", "abandoned start step returned" and "abandoned start step error", states that a handler panic on the container's background goroutines (the warning timer, an abandoned call's late outcome) is dropped, and that these records can come after Start returned or the container closed; verify `go doc doger.InjectorOpts` renders and `make lint` passes

## 36. Undocumented production behaviour is documented (triages D, E)

- [ ] 36.1 Docs: `StartTimeout` godoc and `docs/docs/container/options.md`: a successful Shutdown report can come back while an abandoned Start still runs (after root Start abandoned it, nothing is in flight); `Invoke` of a service whose Start was abandoned returns its instance, which may be half started; a Starter that calls Refresh or Scope.Start synchronously deadlocks without the option and ends in an `ErrStartTimeout` pair with it (still never do it); verify `go doc` renders, `go test -run 'TestDocs_' .` and `make lint` pass
- [ ] 36.2 Docs: `HookScopeStarting` godoc: the hooks run on every Scope.Start attempt that passes its phase check, including one that then reports a still-running step; verify `go doc` renders and `make lint` passes

## 37. Release checks after the code review and consistency fixes

- [ ] 37.1 Run `make test`, `make lint`, and `go test -race ./...` in every `go.work` module; verify all green
- [ ] 37.2 Run `make test-sql` and `make test-watermill`; verify both green and that `go test -v` in `events/sql` and `events/watermill/wmsql` reports no test skipped for a missing database or DSN; then `make test-sql-down`
- [ ] 37.3 Run `openspec validate bound-blocking-starters` (without `--strict`) and cross-check the Test Strategy table; verify valid and every scenario maps to a passing test
- [ ] 37.4 Re-run the root/child and lifecycle-phase triage matrix (triages A–E, scratch tests kept under the session scratchpad) against the finished code; verify every row is CONSISTENT or documented, and report any remaining difference before archiving

## Workflow follow-up

- Propose separate changes for the sweep findings in proposal.md "Out of scope": (1) hardening, so that doger abandons a remote subscription stop that ignores ctx (not a defect: the transport contract requires stop to honour ctx); (2) bounding or warning on Closing/Closed hooks and sync ContainerClosing listeners; (3) logging abandoned service Shutdowns and naming the listener in the drain error.
- Propose a separate change for a `runtime.Goexit` in a Start method or kind handler without `StartTimeout`: it ends the goroutine that called Start, so Start never returns (reproduced on the base commit; code review F7).
- Propose a separate change for a child-scope start-failure observer matching `HookContainerFailed` (a failed `Scope.Start` is visible only through its return value; triage C).
- Propose a separate change to report one "waiting for start" error, not one per scope, when a root Shutdown bounded by its context covers a child scope a run is starting (triages A, E).
- Archive this change after review, once `make test`, `make lint`, `make test-sql` and `make test-watermill` pass.
