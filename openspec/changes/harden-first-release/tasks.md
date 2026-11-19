# Tasks

Every behavior group is RED → GREEN → REFACTOR. A RED task must be seen failing for the expected reason before its GREEN task starts. Run `make test` and `make lint` after every REFACTOR step. Groups are ordered by dependency: a later group may rely on symbols an earlier group introduced. A GREEN step that changes observable text or renames an identifier also updates the existing tests that mechanically follow from it (message prefix, renamed calls), so `make test` stays green; group 36 then aligns the tests pinned by the swept specs.

## 1. Error prefix and sentinels (foundational)

- [x] 1.1 RED: update the rows of `TestCompat_ErrorSentinelMessages` and `TestInvoke_NotFoundNamesLibraryTypes` to `doger: ` and add `TestModuleIdentity_PrefixOnce` and `TestModuleIdentity_AllSentinelsPrefixed` (scenarios "Error sentinels keep their messages", "Not-found errors for library types", "Messages that used to double the prefix", "No exported sentinel uses the DI prefix"); also update the health message rows of `TestHealthCheck_EndedContextIsTimeout`, `TestHealthCheck_Panic`, `TestHealthCheck_CallerLeaves` and `TestHealthRound_PanicKeepsRecording`; verify `go test -race -run 'TestCompat_ErrorSentinelMessages|TestInvoke_NotFoundNamesLibraryTypes|TestModuleIdentity_|TestHealthCheck_(EndedContextIsTimeout|Panic|CallerLeaves)|TestHealthRound_PanicKeepsRecording' .` fails on the `DI: ` text
- [x] 1.2 GREEN: replace every `DI: ` prefix with `doger: ` in root, http, scheduler/gocron and stacktrace messages (including `ErrHealthCheckTimeout`/`ErrHealthCheckPanic`), and wrap library errors without re-prefixing so the "cannot start: … shut down" doubling disappears; verify the 1.1 tests pass and `make test` is green
- [x] 1.3 REFACTOR: Extract Function `libErrorf` (adds the prefix once) and Replace Inline Literal with it at every call site; verify `make test` and `make lint` pass and `grep -rn '"DI: ' --include=*.go .` returns nothing
- [x] 1.4 Docs: godoc for every sentinel in `errors.go` saying when it matches, and remove the `//nolint:revive`; verify `go doc -all . | grep -A2 '^var Err'` shows a doc for each and `make lint` passes

## 2. Resolution error taxonomy

- [x] 2.1 RED: add `TestResolutionErrors_TypeMismatch`, `TestResolutionErrors_MustTypeMismatchPanics`, `TestResolutionErrors_ServiceErrorWrapping`, `TestResolutionErrors_ServiceErrorScope`, `TestResolutionErrors_DuplicateRegistration` and `TestResolutionErrors_OverrideNoError` (resolution-errors scenarios); verify `go test -race -run TestResolutionErrors_ .` fails to compile naming only `ErrServiceTypeMismatch`, `ErrServiceAlreadyDeclared` and `ServiceError`
- [x] 2.2 GREEN: add `ErrServiceTypeMismatch`, `ErrServiceAlreadyDeclared` and `*ServiceError` (`Error`/`Unwrap`); wrap in-build provider and dependency failures once per service boundary (a missing top-level name stays a plain not-found error); make duplicate-registration and type-mismatch errors wrap their sentinels; verify the 2.1 tests pass and `make test` is green
- [x] 2.3 REFACTOR: Extract Function `wrapBuildError(scope, name, err)` shared by the lazy, transient and eager-dependency paths; verify `make test` and `make lint` pass
- [x] 2.4 Docs: add `ExampleServiceError` (errors.As for the failing service, errors.Is for the cause); verify `go test -run ExampleServiceError .` passes

## 3. Nil injector rejected and no package-level container

- [x] 3.1 RED: add `TestResolutionErrors_NilInjectorReturnsError`, `TestResolutionErrors_NilInjectorPanics`, `TestContainerOptions_NilTouchesNoContainer` and `TestContainerOptions_Isolated` (scenarios "Error-returning entry points with a nil injector", "Entry points without an error result panic on a nil injector", "A rejected nil call touches no container", "Containers are isolated"), using today's names for APIs renamed later; verify `go test -race -run 'TestResolutionErrors_NilInjector|TestContainerOptions_(NilTouchesNoContainer|Isolated)' .` fails to compile naming only `ErrNilInjector`
- [x] 3.2 GREEN: add `ErrNilInjector`; replace `getInjectorOrDefault` with `getInjector(i) (Injector, error)` that detects nil and typed-nil `*RootScope`/`*Scope`; error-returning APIs return it, the others panic with it; delete `DefaultRootScope` and migrate tests that used it to explicit roots; verify the 3.1 tests pass and `make test` is green
- [x] 3.3 REFACTOR: Inline Function at callers of the removed helper and Extract Function `mustInjector(i)` for panicking entry points; verify `make test`, `make lint` and `go build ./...` in every `go.work` module pass
- [x] 3.4 Docs: remove "nil means DefaultRootScope" from godoc (collection.go, dependency.go, di.go, extension.go, runnable.go) and add the migration row; verify `grep -rnw DefaultRootScope --include=*.go --include=*.md --exclude-dir=.claude . | grep -v -e CHANGELOG -e migrating -e openspec` prints nothing

## 4. Renames: Alias and WithExportName

- [x] 4.1 RED: add `TestAlias_Functions`, `TestAlias_BindUnchanged`, `TestAPISurface_AliasRename` and `TestAPISurface_ExportNameRename`, update `TestExport_Invalid` to sentinel rows, and switch `TestInvokeAs_SkipsAliasOfShadowedName`, `TestInvokeAs_SkipsInvoker` and `TestProvideOptions_CloneOverrideAlias` to `Alias`/`AliasNamed` (scenarios "Alias functions register an alias", "Bind keeps working", "The As identifiers are gone", "ExportAs is renamed WithExportName", "Invalid exports are rejected", "The invoking service is not a candidate", "Clone, override and alias"); verify `go test -race -run 'TestAlias_|TestAPISurface_|TestExport_Invalid|TestInvokeAs_Skips|TestProvideOptions_CloneOverrideAlias' .` fails to compile naming only `Alias*`/`MustAlias*`/`WithExportName`
- [x] 4.2 GREEN: gopls Rename `As`→`Alias`, `AsNamed`→`AliasNamed`, `MustAs`→`MustAlias`, `MustAsNamed`→`MustAliasNamed`, `ExportAs`→`WithExportName` across all workspace modules and examples; duplicate alias/export returns `ErrServiceAlreadyDeclared`, export errors match `ErrNilInjector`/`ErrServiceNotFound`/`ErrServiceTypeMismatch`; verify the 4.1 tests pass, `make test` is green and `go build ./...` passes in every workspace module
- [x] 4.3 REFACTOR: mechanical rename updates in existing tests and examples only (`TestAs*`→`TestAlias*`); verify `make test` and `make lint` pass
- [x] 4.4 Docs: godoc for the alias functions and `WithExportName`, update `ExampleAlias`/`ExampleExport`, export docs page, README/docs references and migration rows; verify `go test -run 'ExampleAlias|ExampleExport' .` passes and `grep -rn 'doger\.As\[\|ExportAs(' docs examples README.md` prints nothing

## 5. Constructors never write the caller's options

- [x] 5.1 RED: add `TestContainerOptions_CallerOptsUnchanged`, `TestContainerOptions_NilOpts` and `TestContainerOptions_SharedOptsIndependent` (scenarios "The caller's options are unchanged after construction", "A nil options pointer", "Containers built from one options value are independent"); verify `go test -race -run 'TestContainerOptions_(CallerOptsUnchanged|NilOpts|SharedOptsIndependent)' .` fails with hook `only-a` running for `b`
- [x] 5.2 GREEN: in `NewWithOpts`/`CloneWithOpts` shallow-copy `*opts`, clone every hook slice and apply defaults to the copy; verify the 5.1 tests pass and `make test` is green
- [x] 5.3 REFACTOR: Extract Function `(*InjectorOpts).clone()` replacing the existing `copy()`; verify `make test` and `make lint` pass

## 6. Hook registration is safe at any time

- [x] 6.1 RED: add `TestContainerOptions_AddHookConcurrent` (8 adder rows, nil hook, same hook twice) and `TestContainerOptions_AddHookAppliesNext` (scenarios "Adding hooks while the container works", "A hook added after Add returns applies to the next operation"); verify `go test -race -run 'TestContainerOptions_AddHook' .` reports `DATA RACE` at the `root_scope.go` adders
- [x] 6.2 GREEN: store every hook list copy-on-write behind an `atomic.Pointer` (adders load-copy-append-CAS, readers load once per operation); verify the 6.1 tests pass under `-race` and `make test` is green
- [x] 6.3 REFACTOR: Extract a generic `hookList[F]` type with `add`/`load` replacing the 19 hand-written adders; verify `make test` and `make lint` pass
- [x] 6.4 Docs: godoc of every `AddXxxHook` and `InjectorOpts` hook field (safe at any time, applies from the next operation; fields for configuration time, adders for plugins); verify `go doc RootScope.AddBeforeInvocationHook` shows the text

## 7. Invocation frames are capped per container

- [x] 7.1 RED: add `TestContainerOptions_MaxInvocationFrames` (6 rows) and `TestContainerOptions_FramesPerContainer` (scenarios "Frame cap per option value", "Two containers keep their own caps"); verify `go test -race -run 'TestContainerOptions_(MaxInvocationFrames|FramesPerContainer)' .` fails to compile naming only `InjectorOpts.MaxInvocationFrames`
- [x] 7.2 GREEN: add `InjectorOpts.MaxInvocationFrames int` (0→100, negative off), read it per container in lazy/eager/alias/export wrappers and remove the package variable; verify the 7.1 tests pass under `-race` and `make test` is green
- [x] 7.3 REFACTOR: Extract Function `recordInvocationFrame(cap)` shared by the four wrappers; verify `make test` and `make lint` pass

## 8. Levelled logging with slog

- [x] 8.1 RED: add `TestLogging_OffByDefault`, `TestLogging_Events`, `TestLogging_LevelFilter`, `TestLogging_ChildScopeAttr`, `TestLogging_Concurrent`, `TestLogging_PanicValueDebugOnly` and `TestLogging_HealthErrorDebugOnly` with a recording `slog.Handler` helper (all container-logging scenarios, except: `TestLogging_PanicValueDebugOnly` triggers a panic already recovered today, a second Closing hook panic of one Shutdown with point `closing`, and its service-shutdown row moves to 13.4; the `health check started` scenarios are group 36's); verify `go test -race -run TestLogging_ .` fails to compile naming only `InjectorOpts.Logger`
- [x] 8.2 GREEN: replace `Logf` with `Logger *slog.Logger` (nil → discard with an `Enabled` check before building attrs), convert every `Logf`/`logf` call site to the container-logging message/level table with `scope`/`service` attrs, split recovered panics into an Error `panic recovered` and a Debug `panic value`, move health result text to Debug; verify the 8.1 tests pass under `-race` and `make test` is green
- [x] 8.3 REFACTOR: Extract Function `(*Scope).log(level, msg, attrs...)` and remove `logf`; verify `make test` and `make lint` pass
- [x] 8.4 Docs: add `ExampleInjectorOpts_logger` (slog text handler at Info) and update README/docs logging sections and the migration row; verify `go test -run ExampleInjectorOpts_logger .` passes

## 9. Scaffold the clockwork adapter module

- [x] 9.1 Create module `clock/clockwork` (`go.mod` `github.com/kartaladev/doger/clock/clockwork`, package `dogerclockwork`, requiring root and `jonboulle/clockwork` with a local `replace`), add `./clock/clockwork` to `go.work` and a `doc.go` package comment; verify `go build ./...` inside `clock/clockwork` and `go work sync` succeed

## 10. Doger-owned Clock

- [ ] 10.1 RED: add `TestRealClock` and `TestClock_ConsumerClock` (scenarios "RealClock follows the time package", "A consumer-written clock drives the container"); verify `go test -race -run 'TestRealClock|TestClock_ConsumerClock' .` fails to compile naming only `RealClock`/`Clock`/`Timer`
- [ ] 10.2 GREEN: add `Clock`, `Timer` and `RealClock()`, and switch `InjectorOpts.Clock` and every `Now`/`Since`/`AfterFunc` use to `doger.Clock`; verify the 10.1 tests pass and `make test` is green
- [ ] 10.3 RED: add `TestUnwrap`, `TestWrap_Delegates` and `TestWrap_NilIsDefault` in `clock/clockwork/clockwork_test.go` (Wrap/Unwrap scenarios); verify `go test -race ./...` in `clock/clockwork` fails on undefined `Wrap`/`Unwrap`
- [ ] 10.4 GREEN: implement `Wrap`/`Unwrap` (`Wrap(nil)` returns a nil Clock); verify `go test -race ./...` in `clock/clockwork` passes
- [ ] 10.5 GREEN: migrate root tests from `clockwork.NewFakeClock()` to a test-only wrapper and update the `TestClock_*` rows (RealClock row, 10s default row, manualClock row) for the MODIFIED container-clock scenarios; verify `go test -race -run TestClock_ .` passes
- [ ] 10.6 REFACTOR: Inline Function `injectorClock` defaults into `RealClock()` and Rename internal clock variables; verify `make test` and `make lint` pass
- [ ] 10.7 Docs: add `ExampleClock` and `dogerclockwork.ExampleWrap`, godoc for `Clock`/`Timer`/`RealClock` and a migration row; verify `go test -run Example . ./clock/clockwork/...` passes
- [ ] 10.8 Verify scenario "Build list of a consumer": `go list -m all` in a scratch consumer of the root module omits `github.com/jonboulle/clockwork` (via `scripts/release-dry-run.sh` once group 50 exists, or a manual GOWORK=off consumer now)

## 11. HealthScheduler takes a doger.Clock

- [ ] 11.1 RED: add `TestHealthScheduler_ReceivesRealClockByDefault` and switch `TestHealthScheduler_CalledAtReady` to a wrapped clock; verify `go test -race -run TestHealthScheduler_ .` fails to compile only on the `ScheduleHealthRounds` signature
- [ ] 11.2 GREEN: change `HealthScheduler.ScheduleHealthRounds(clock Clock, …)` and pass `opts.Clock` (`RealClock()` when nil); verify the 11.1 tests pass and `make test` is green
- [ ] 11.3 RED: update `TestHealthScheduler_Failure`, `TestHealthScheduler_StopReturnedWithError`, `TestHealthScheduler_StopFailure`, `TestHealthHooks_Panic` and `TestHealthHooks_NilSkipped` to assert slog records (Error without the value, Debug with it); verify they fail on the old logging path
- [ ] 11.4 GREEN: route scheduler, stop and health-hook failures through `Logger` at Error with panic values only at Debug; verify the 11.3 tests pass and `make test` is green
- [ ] 11.5 REFACTOR: Extract Function `logSchedulerFailure` shared by the schedule, stop and hook paths; verify `make test` and `make lint` pass
- [ ] 11.6 Docs: update the `HealthScheduler` godoc and the health-scheduling docs page to the doger.Clock signature; verify `go doc HealthScheduler` shows `Clock`

## 12. gocron adapter maps the container clock

- [ ] 12.1 RED: add `TestScheduler_ClockMapping` and `TestScheduler_UnsupportedClockUnmonitored`, and switch `TestScheduler_DefaultCadence`/`TestScheduler_SingletonSkipsTick` to `dogerclockwork.Wrap(fake)`; verify `go test -race ./...` in `scheduler/gocron` fails on the signature and the missing `ErrUnsupportedClock`
- [ ] 12.2 GREEN: `ScheduleHealthRounds(clock doger.Clock, …)` maps an `Unwrap` result, maps `RealClock()` to `clockwork.NewRealClock()`, else returns `ErrUnsupportedClock` (also for nil; ignored with `WithScheduler`); require `clock/clockwork`; verify `go test -race ./...` in `scheduler/gocron` passes with goleak
- [ ] 12.3 REFACTOR: Extract Function `gocronClock(doger.Clock) (clockwork.Clock, error)`; verify `go test -race ./...` and `golangci-lint run ./...` in `scheduler/gocron` pass
- [ ] 12.4 Docs: update the gocron README/godoc and `ExampleNew` to show `Clock: dogerclockwork.Wrap(fake)`; verify `go test -run Example ./...` in `scheduler/gocron` passes

## 13. Shutdown panic isolation

- [ ] 13.1 RED: regression `TestContainerShutdown_ShutdownPanicDoesNotCrashProcess` (child process) plus `TestContainerShutdown_ServiceShutdownPanic`, `TestContainerShutdown_EagerShutdownPanic` and `TestContainerShutdown_ShutdownHookPanic` (scenarios "A panicking Shutdown method is recorded in the report", "A panicking eager value is recorded in the report", "The crash is gone in a separate process", "A panicking shutdown hook is recorded for its service"); verify `go test -race -run 'TestContainerShutdown_(ShutdownPanicDoesNotCrashProcess|ServiceShutdownPanic|EagerShutdownPanic|ShutdownHookPanic)' .` fails: the child exits 2 and the rest fail to compile only on `ErrShutdownPanic`
- [ ] 13.2 GREEN: add `ErrShutdownPanic` and recover separately around each before hook, `service.shutdown(ctx)` and each after hook in `serviceShutdown`, joining the errors (D5); verify the 13.1 tests pass and `make test` is green
- [ ] 13.3 RED: add `TestContainerShutdown_FirstPanicToHook` and `TestContainerShutdown_PanicHookRepanics` (scenarios "The first shutdown panic goes to HookContainerPanic", "A HookContainerPanic that re-panics is the consumer's choice"); verify `go test -race -run 'TestContainerShutdown_(FirstPanicToHook|PanicHookRepanics)' .` fails on hook call count 0
- [ ] 13.4 GREEN: keep the first shutdown panic of the operation with point `"service-shutdown"`, dispatch it after the Closed hooks and log later ones at Error; add the service-shutdown row to `TestLogging_PanicValueDebugOnly` (a lazy "svc" whose Shutdown() panics with "token=hunter2": one Error `panic recovered` with service=svc, point=service-shutdown, panic_type=string, and "hunter2" only at Debug, moved from 8.1); verify the 13.3 tests and that row pass and `make test` is green
- [ ] 13.5 REFACTOR: Extract Function `recoverShutdownStep` shared by hook and Shutdown calls and Rename `serviceShutdown`→`shutdownService`; verify `make test` and `make lint` pass
- [ ] 13.6 Docs: godoc for `ErrShutdownPanic`, the `HookContainerPanic` point list and the Shutdowner docs page ("panics are reported"); verify `go doc doger.ErrShutdownPanic` shows the doc

## 14. Closed scopes and no resurrection

- [ ] 14.1 RED: regression `TestContainerShutdown_NoResurrection` plus `TestContainerShutdown_InvokeAfterShutdownNamed`, `TestContainerShutdown_ConcurrentInvokeDuringShutdown`, `TestContainerShutdown_ClosedScopeRefuses`, `TestContainerShutdown_SecondShutdownNoop` and `TestContainerShutdown_ScopeDuringShutdownRefused` (container-shutdown closed-scope scenarios); verify `go test -race -run 'TestContainerShutdown_(NoResurrection|InvokeAfterShutdownNamed|ConcurrentInvokeDuringShutdown|ClosedScopeRefuses|SecondShutdownNoop|ScopeDuringShutdownRefused)' .` fails (built 2 vs shut down 1; compile error only on `ErrShutdown`)
- [ ] 14.2 GREEN: add `ErrShutdown` (replacing `ErrStartAfterShutdown`), a scope close flag set under the write lock before children are listed and checked by Invoke*, registration, Alias*/Export* and `Scope()`, and a Terminal wrapper state so a build after shutdown returns `ErrShutdown` (D2); verify the 14.1 tests pass and `make test` is green
- [ ] 14.3 REFACTOR: Extract Function `(*Scope).checkOpen` shared by registration, invocation and `Scope()`; verify `make test` and `make lint` pass
- [ ] 14.4 Docs: godoc for `ErrShutdown`, `Scope()`, `Provide*` and `Invoke*` (closed-scope behavior), migration note "a container cannot be reused after Shutdown" and `ExampleRootScope_Shutdown_closed`; verify `go test -run ExampleRootScope_Shutdown_closed .` passes

## 15. Start and health use ErrShutdown, with defined precedence

- [ ] 15.1 RED: rename `ErrStartAfterShutdown`→`ErrShutdown` in `TestStart_OneShot`, `TestIncrementalStart_Phase`, `TestRefresh_ShutdownCancels`, `TestRefresh_QueuedBehindShutdown`, `TestScopeStartingHook_ShutdownCause`, `TestStart_FailureThenShutdown`, `TestStart_CallerCancelThenShutdown`, `TestStartError_NotForGuards`, `TestStart_PanicReleasesShutdown`, `TestExtend_Phases` and `TestHealthScheduler_ShutdownCancelCause`, and add the `TestStart_OneShot` row "failed Start then Shutdown → ErrShutdown, not ErrAlreadyStarted" (scenarios "Repeated and late Start calls", "Incremental start in the wrong phase", "Extend in each phase", "Callers of a check cancelled by Shutdown"); verify `go test -race -run 'TestStart_OneShot|TestIncrementalStart_Phase|TestExtend_Phases|TestHealthScheduler_ShutdownCancelCause' .` fails on the new row
- [ ] 15.2 GREEN: return and cancel with `ErrShutdown` in root_start.go, root_refresh.go, extension.go and the health cancel cause, and check the shutdown phase before the already-started guard; verify the 15.1 tests pass and `make test` is green
- [ ] 15.3 REFACTOR: gopls Rename helpers named after the old sentinel and Inline Variable for any leftover alias; verify `make test` and `make lint` pass
- [ ] 15.4 Docs: godoc of `ErrAlreadyStarted` and `ErrShutdown` states the precedence, plus the migration row; verify `go doc doger.ErrAlreadyStarted` shows it

## 16. Detach shut-down child scopes

- [ ] 16.1 RED: regression `TestContainerShutdown_ChildDetachNoGrowth` plus `TestContainerShutdown_ChildDetached`, `TestContainerShutdown_ChildDetachConcurrent` and `TestContainerShutdown_GrandchildDetach` (scenarios "A shut-down child is detached and its name reusable", "Per-request scopes do not accumulate", "Concurrent per-request scopes detach", "A grandchild detaches from its own parent only"); verify `go test -race -run 'TestContainerShutdown_(ChildDetach|GrandchildDetach)' .` fails (1000 children left; "already declared" panic)
- [ ] 16.2 GREEN: the winning child shutdown calls `parent.detachChild(name)` after completion (D3); verify the 16.1 tests pass with goleak clean and `make test` is green
- [ ] 16.3 REFACTOR: Inline the delete in `shutdownChildrenInParallel` into `detachChild`; verify `make test` and `make lint` pass
- [ ] 16.4 Docs: godoc for `Scope.ShutdownWithContext`, `Children` and `ChildByName` (detach, name reuse), a per-request scope docs section and `ExampleScope_perRequest`; verify `go test -run ExampleScope_perRequest .` passes

## 17. Shared eager values and re-entrant root shutdown

- [ ] 17.1 RED: regression `TestContainerShutdown_SharedEagerOnce` plus `TestContainerShutdown_CloneLazyPerContainer` and `TestContainerShutdown_ReentrantRootShutdown` (scenarios "A shared eager value is shut down once", "Lazy services stay per container", "A re-entrant root shutdown returns at once"); verify `go test -race -run 'TestContainerShutdown_(SharedEagerOnce|CloneLazyPerContainer|ReentrantRootShutdown)' .` fails (2 calls; inner deadline error)
- [ ] 17.2 GREEN: a shared shutdown-once cell on `serviceEager` copied by `clone`, and a private marker key on the context root shutdown hands to services so a marked re-entrant call returns an empty report (D14); verify the 17.1 tests pass and `make test` is green
- [ ] 17.3 REFACTOR: none needed if the cell is a single `*sync.Once` field, otherwise Introduce Parameter Object for it; verify `make test` and `make lint` pass
- [ ] 17.4 Docs: godoc for `Clone` (shared eager values) and `RootScope.ShutdownWithContext` (re-entrancy; `Shutdown()` drops the context); verify `go doc doger.RootScope.ShutdownWithContext` shows the text

## 18. ShutdownReport API

- [ ] 18.1 RED: add `TestShutdownReport_ErrUnwrap`, `TestShutdownReport_ErrorsAsServiceError` and regression `TestShutdownReport_MarshalJSON` (scenarios "Err and Unwrap reflect the report", "errors.As finds the failing service", "A report marshals to JSON"); verify `go test -race -run TestShutdownReport_ .` fails to compile only on `Err`/`Unwrap`, then the JSON test fails with "object member name must be a string"
- [ ] 18.2 GREEN: add `Err()`, `Unwrap() []error` (sorted `*ServiceError`), `MarshalJSON` (lowerCamel keys, sorted arrays, duration string) and the `doger: ` prefix in `Error()` (D11/D14); verify the 18.1 tests pass and `make test` is green
- [ ] 18.3 REFACTOR: Extract Function `sortedServiceErrors` shared by `Error`, `Err`, `Unwrap` and `MarshalJSON`; verify `make test` and `make lint` pass
- [ ] 18.4 Docs: fix the `ShutdownReport` doc (`Error()` is "" on success; check `Err()`) and add `ExampleShutdownReport_Err` and `ExampleShutdownReport_MarshalJSON`; verify `go test -run 'ExampleShutdownReport_' .` passes

## 19. Concurrent resolution without deadlocks

- [ ] 19.1 RED: regression tests `TestConcurrentResolution_CycleReturnsError`, `TestConcurrentResolution_DeclaredDependencyCycle`, `TestConcurrentResolution_CycleRetried`, `TestConcurrentResolution_ReentrantCapturedInjector` and `TestConcurrentResolution_WaitersShareFailure` (each under `testWithTimeout` 2s); verify `go test -race -run TestConcurrentResolution_ .` fails with the hang timeouts
- [ ] 19.2 RED: add guards `TestConcurrentResolution_SingleBuild`, `TestConcurrentResolution_SharedDependencyNoFalseCycle` and `TestConcurrentResolution_CapturedInjectorNoCycle`; they pass on arrival, so record that and show each assertion can fail by temporarily calling the provider twice (not committed)
- [ ] 19.3 GREEN: implement D1 (build states Idle/Building/Built/Terminal with a `done` channel, a per-root wait-for registry keyed by goroutine id read only at build start or before a wait, self-owned build → `ErrCircularDependency`, a closing wait cycle fails the closing waiter, waiters share the outcome, provider `runtime.Goexit` → `ErrProviderPanic`); verify every 19.1/19.2 test passes with `-race -count=20` and `make test` is green
- [ ] 19.4 REFACTOR: Extract Class `buildState` from `serviceLazy`, Move Function so eager services with `DependsOn` use it, and Extract Function `goroutineID()` with a unit test pinning the parse; verify `make test`, `make lint` and `go test -race -count=50 -run TestConcurrentResolution_ .` pass
- [ ] 19.5 Docs: godoc on `Invoke` and `Provide` documenting cycle errors under concurrency and the transient / spawned-goroutine non-goals; verify `make lint` passes

## 20. Shutdown honors its context

- [ ] 20.1 RED: regression `TestShutdownContext_HungShutdownBounded` and `TestShutdownContext_InFlightBuildBounded` plus `TestShutdownContext_SlowShutdownWaited` and `TestShutdownContext_InFlightBuildWaited`, and update `TestRootScope_ShutdownDeadlineDuringShutdown` (shutdown-context scenarios); verify `go test -race -run 'TestShutdownContext_|TestRootScope_ShutdownDeadlineDuringShutdown' .` fails (the call returns only after release)
- [ ] 20.2 GREEN: wait on completion or `ctx.Done()` per layer, report unfinished services as `*ServiceError{Err: ctx.Err()}`, stop scheduling further layers, run AfterShutdown hooks when an abandoned Shutdown returns, wait for in-flight builds with `select`, and shut a late instance down once on its Terminal wrapper (D4, D1); verify the 20.1 tests pass with goleak clean and `make test` is green
- [ ] 20.3 RED: regression `TestRootScope_ShutdownOnSignalsWithContext_NilContext` (scenario "The signal helper accepts a nil context"); verify `go test -race -run TestRootScope_ShutdownOnSignalsWithContext_NilContext .` fails with a nil-pointer panic
- [ ] 20.4 GREEN: `if ctx == nil { ctx = context.Background() }` before `signal.Notify` in `ShutdownOnSignalsWithContext`; verify the 20.3 test passes and `make test` is green
- [ ] 20.5 REFACTOR: Extract Function `awaitLayer(ctx, done)` shared with `awaitDone`; verify `make test` and `make lint` pass
- [ ] 20.6 Docs: godoc for root and Scope `ShutdownWithContext` (returns at the deadline, abandoned services keep running) and `ShutdownOnSignalsWithContext` (nil context), plus the Shutdowner docs page; verify `go doc doger.Scope.ShutdownWithContext` shows the text

## 21. Provider panic errors

- [x] 21.1 RED: update the `TestInvoke_ProviderPanic` rows and add `TestProviderPanic_NestedProvider` (scenarios "Provider panic values become invocation errors", "Panic in a nested provider"); verify `go test -race -run 'TestInvoke_ProviderPanic$|TestProviderPanic_' .` fails to compile naming only `ErrProviderPanic`, then on the message rows
- [x] 21.2 GREEN: add `ErrProviderPanic`; `handleProviderPanic` wraps it with an error panic value (`%w` twice) or formats other values with `%v`, and `ServiceError` adds the service name; keep `TestInvoke_ProviderPanicNil_GODEBUGOverride` green; verify the 21.1 tests pass and `make test` is green
- [x] 21.3 REFACTOR: none needed — the change is local to `handleProviderPanic`; verify `make test` and `make lint` pass

## 22. InvokeAs and struct injection with nil interfaces and invalid targets

- [x] 22.1 RED: regression tests `TestInvokeAs_NilInterfaceInstance`, `TestInvokeAs_NonNilInstance`, `TestStructInjection_NilInterfaceDependency`, `TestStructInjection_MustNilInterfaceDependency`, `TestStructInjection_InvalidTargets`, `TestStructInjection_MustRecursivePointerType` (child process for the stack overflow) and `TestStructInjection_DependencyFailureNotStructError`; verify `go test -race -run 'TestInvokeAs_N|TestStructInjection_' .` fails with the interface-conversion/reflect panics and the missing `ErrStructInjection`
- [x] 22.2 GREEN: `InvokeAs` uses `typed, _ := instance.(T)`, struct injection uses `memberValue(dependency, fieldValue.Type())`, add `ErrStructInjection` wrapping the invalid-target errors, and `deepEmptyMakeValue` tracks visited types returning an error naming the type; verify the 22.1 tests pass and `make test` is green
- [x] 22.3 REFACTOR: Change Function Declaration so `deepEmpty[T]` returns `(T, error)` propagated through `InvokeStruct`, and Extract Function `structInjectionError`; verify `make test` and `make lint` pass

## 23. Not-found message cap

- [x] 23.1 RED: add `TestResolutionErrors_NotFoundCap` (rows 0/1/10/11/2000) and `TestResolutionErrors_NotFoundCapWithChain`; verify `go test -race -run TestResolutionErrors_NotFoundCap .` fails on the 11 and 2000 rows
- [x] 23.2 GREEN: keep the first 10 sorted names and append ` and N more` before `, path:` in `serviceNotFound`; verify the 23.1 tests pass and `make test` is green
- [x] 23.3 REFACTOR: Extract Function `formatServiceList(names, limit)`; verify `make test` and `make lint` pass

## 24. Explanation is free of data races

- [ ] 24.1 RED: add `TestExplain_ConcurrentWithLazyBuild` and `TestExplain_ConcurrentWithShutdown` (scenarios "Explain concurrently with a lazy build", "Explain concurrently with shutdown"); verify `go test -race -run TestExplain_Concurrent .` fails with `WARNING: DATA RACE` at `getBuildTime`
- [ ] 24.2 GREEN: read build time and built state under the read lock or from the published atomic state in lazy/default/eager `getBuildTime`; verify the 24.1 tests pass under `-race` and `make test` is green
- [ ] 24.3 REFACTOR: Extract Function for the shared build-state snapshot used by explain and the web UI's `inferServiceInfo`; verify `make test` and `make lint` pass

## 25. Only successful builds record dependency edges

- [ ] 25.1 RED: add `TestExplain_FailedDependencyNoEdge` (4 path rows) and `TestExplain_RetryRecordsEdgeOnce` (scenarios "A failed dependency records no edge", "A successful retry records the edge once"); verify the `named` and `named_any` rows fail with a recorded dependency
- [ ] 25.2 GREEN: record the edge only after a successful build in `invokeByName` and `invokeAnyByNameAs`; verify the 25.1 tests pass and `make test` is green
- [ ] 25.3 REFACTOR: Extract Function `recordDependencyOnSuccess` shared by the by-name and by-type paths; verify `make test` and `make lint` pass

## 26. Bounded, plain-text explanation

- [ ] 26.1 RED: add `TestExplain_LayeredGraphBounded`, `TestExplain_DependentsBounded`, `TestExplain_RefRendersSingleLine`, `TestExplain_RefJSON` and `TestExplain_PlainText` (service-explanation bounded/plain-text scenarios); verify they fail (121391 vs 45 nodes; `Ref` undefined; `&lt;` in output)
- [ ] 26.2 GREEN: memoize `newExplainServiceDependencies` per call by (scope, service), emit `Ref: true` nodes (`json:"ref,omitempty"`) with no children rendered as `" (ref)"`, add JSON tags to `ServiceDescription`, and switch `di_explain.go` to `text/template`; verify the 26.1 tests pass and `make test` is green
- [ ] 26.3 REFACTOR: Introduce Parameter Object for the visited set passed through the recursive builders; verify `make test` and `make lint` pass
- [ ] 26.4 Add `FuzzExplainText` and run it 60s, and update the godoc of `ExplainServiceDependencyOutput`/`ExplainNamedService` and `ExampleExplainNamedService`; verify `go test -run=^$ -fuzz=FuzzExplainText -fuzztime=60s .` and `go test -run ExampleExplainNamedService .` pass

## 27. Registration source is the consumer's call site

- [ ] 27.1 RED: add `TestSourceTracking_RegistrationForms` and `TestSourceTracking_NewCallSite`, and move `TestSourceTracking_ProviderFrame` from `di_explain_frame_test.go` expecting the test file and line (service-source-tracking scenarios); verify they fail with `service_eager.go:39` / `service_alias.go`
- [ ] 27.2 GREEN: the provider frame of value and alias registrations skips non-test frames of the root `doger` package (invocation frames unchanged); verify the 27.1 tests pass, the alias invocation-frame test stays green and `make test` is green
- [ ] 27.3 REFACTOR: Replace Loop with one `runtime.Callers` pass into a fixed array in `stacktrace` (D16/D17) keeping the classifier; verify `make test`, `make lint` and `go test ./stacktrace` pass

## 28. A yielded default follows its target by name

- [x] 28.1 RED: add `TestAsDefault_YieldedFollowsOverride` (5 rows), `TestAsDefault_YieldedAliasFollowsOverride` and `TestAsDefault_YieldedConcurrentOverride`, and add `TestAsDefault_NilInjector` (the former test of that name is now `TestAsDefault_ResolvesByType`) plus the `doger:`/`Alias` texts of existing default tests (default-registrations scenarios); verify the override rows fail (`expected app-dialer-2, actual app-dialer-1`)
- [x] 28.2 GREEN: each yielded resolution looks the name up in `targetScope` instead of the stored wrapper, returning `ErrServiceNotFound` when gone; verify the 28.1 tests pass under `-race` and `go test -race -run TestAsDefault .` and `make test` are green
- [x] 28.3 REFACTOR: Inline Variable to remove the stored `target` field, keeping `targetScope` for Explain; verify `make test` and `make lint` pass

## 29. Per-service hook signatures with context and elapsed

- [ ] 29.1 RED: add `TestServiceHooks_Arguments`, `TestServiceHooks_AddMethods`, `TestServiceHooks_Context`, `TestServiceHooks_ShutdownBackgroundContext`, `TestServiceHooks_CancelledShutdownContext`, `TestServiceHooks_Elapsed`, `TestServiceHooks_ElapsedOnFailure`, `TestServiceHooks_Order` and `TestServiceHooks_PanicPolicy` (all service-lifecycle-hooks scenarios); verify `go test -race -run TestServiceHooks_ .` fails to compile only on the new hook signatures
- [ ] 29.2 GREEN: change `HookBefore/AfterStart`, `HookBefore/AfterShutdown` and `HookAfterInvocation` fields and adders to the D9 signatures, pass the start/shutdown ctx, measure elapsed on the container `Clock`, and migrate existing hook tests and examples mechanically; verify the 29.1 tests pass under `-race` and `make test` is green
- [ ] 29.3 REFACTOR: Introduce Parameter Object for the internal hook-dispatch arguments `(ctx, scope, name, start)`; verify `make test` and `make lint` pass
- [ ] 29.4 Docs: add `ExampleInjectorOpts_afterInvocationHook` using elapsed and the hook-signature table to the migration guide; verify `go test -run Example .` passes

## 30. Start messages carry one doger prefix

- [ ] 30.1 RED: update the expected messages in `TestAbortOutcome`, `TestStartError_ConsumerBuilt`, `TestStart_AsStarterInstanceWithoutStart`, `TestKind_InvalidValues` and `TestKind_InterfaceRegistration` to a single `doger: ` prefix and add a "contains doger: once" assertion to `TestStartError_Fields` (scenarios "The abort outcome rule", "StartError values built by the consumer"); verify `go test -race -run 'TestAbortOutcome|TestStartError_' .` fails on the messages
- [ ] 30.2 GREEN: switch StartError, abort and kind messages to the shared prefix helper so wrapped sentinel text loses its own prefix (consumer-led abort text stays unprefixed); verify the 30.1 tests pass and `make test` is green
- [ ] 30.3 REFACTOR: Extract Function for abort-message composition if still duplicated between Start and incremental start; verify `make test` and `make lint` pass

## 31. Root shutdown winner test uses the Logger

- [ ] 31.1 RED: rewrite `TestRootScope_ShutdownWinnerPanic` with a Logger whose handler panics on "requested shutdown" (scenario "A panicking first caller still releases the others"); verify `go test -race -run TestRootScope_ShutdownWinnerPanic .` fails because no record is emitted
- [ ] 31.2 GREEN: emit the "requested shutdown" record through the Logger; verify the 31.1 test passes and `make test` is green
- [ ] 31.3 REFACTOR: none needed — only the logging call moves; verify `make test` and `make lint` pass

## 32. Starter auto-detection is opt-in

- [ ] 32.1 RED: add `TestStart_AutoDetectionOptIn` (replacing `TestStart_DisableAutoDetection`), `TestStart_FamiliarStartNotStarted` and `TestStart_CandidatesWithDefaults`, and add the auto column to `TestStart_Variants` and `TestStart_Detection` (scenarios "Auto-detection toggle", "Familiar Start methods are not started by default", "Each Starter variant is started once", "Which registrations are started", "Start candidates with defaults"); verify `go test -race -run 'TestStart_(AutoDetectionOptIn|FamiliarStartNotStarted|Variants|Detection|CandidatesWithDefaults)' .` fails to compile naming only `EnableStarterAutoDetection`
- [ ] 32.2 GREEN: replace `DisableStarterAutoDetection` with `EnableStarterAutoDetection` (default false, copied by Clone), gate type-based detection on it, and add `AsStarter()` to fixtures relying on detection (container-start, container/scope lifecycle hooks, extensions, `tests/`, examples) per D19; verify the 32.1 tests pass and `make test` is green
- [ ] 32.3 REFACTOR: Extract Function `isStartCandidate(opts, svc)` shared by Start, Refresh and Scope.Start discovery; verify `make test` and `make lint` pass
- [ ] 32.4 Docs: rewrite the `Starter`/`EnableStarterAutoDetection` godoc, add `ExampleAsStarter` and `ExampleInjectorOpts_enableStarterAutoDetection`, update `docs/docs/service-lifecycle/starter.md`, the migration guide and `examples/*`; verify `go test -run 'Example(AsStarter|InjectorOpts_enableStarterAutoDetection)' .` passes and the example modules build

## 33. Kinds need an explicit root; Runnable is a function; Stage zero is invalid

- [x] 33.1 RED: add `TestKind_NilRootRejected` (the former `TestKind_NilRootUsesDefault` is now `TestKind_HandlerOnRoot`), a Stage(0) row in `TestKind_InvalidValues`, `TestRunnable_FuncReturnsBuiltIn` (replacing `TestRunnable_ReassignedVariable`), `TestStage_Text` and `TestStage_UnmarshalText`, and call `Runnable().Handle` in `TestRunnable_ReplaceHandler`/`TestKind_CloneCopiesHandlers` (component-kinds scenarios); verify `go test -race -run 'TestKind_|TestRunnable_|TestStage_' .` fails to compile on `Runnable()`/`MarshalText` or on the Stage(0) row
- [x] 33.2 GREEN: `Handle` panics with an `ErrNilInjector`-wrapping error on a nil/typed-nil root, Stage constants start at `iota + 1` with `String`/`MarshalText`/`UnmarshalText` (`before-start`/`after-start`), and `var Runnable` becomes `func Runnable() Kind[Runner, struct{}]`; verify the 33.1 tests pass and `make test` is green
- [x] 33.3 REFACTOR: Inline Variable (`runnableKind` used by `Runnable()`) and Extract Function `validStage(stage)` shared by `Handle` and `MarshalText`; verify `make test` and `make lint` pass
- [x] 33.4 Docs: godoc of `Runnable()`, `Stage` and `Handle`, `ExampleKind_Handle` and `ExampleRunnable`, the component-kinds docs and the migration row (`Runnable.Handle` → `Runnable().Handle`); verify `go test -run 'ExampleKind_Handle|ExampleRunnable' .` passes

## 34. DependencySource zero is invalid; DeclareDependency rejects nil

- [x] 34.1 RED: add nil and typed-nil rows to `TestDeclareDependency`, "unknown"/"" rows to `TestDependencySource_UnmarshalText` and `TestDependencySource_MarshalText` (scenarios "Declaring edges dynamically", "Explained output round-trips through JSON", "Encoding dependency sources"); verify `go test -race -run 'TestDeclareDependency|TestDependencySource_' .` fails on the new rows
- [x] 34.2 GREEN: DependencySource constants start at `iota + 1` (zero: `String()` "unknown", `MarshalText` error `doger: invalid dependency source N`) and `DeclareDependency` returns `ErrNilInjector` for nil/typed-nil; verify the 34.1 tests pass, `TestExplain_EdgeSourceCompat` stays green and `make test` is green
- [x] 34.3 REFACTOR: Replace Loop with a lookup table for the source texts; verify `make test` and `make lint` pass
- [x] 34.4 Docs: godoc of `DependencySource` and `DeclareDependency`; verify `go doc doger.DependencySource` shows the invalid zero value

## 35. Extensions: ExtendIntoRoot, explicit root, Logger decline record

- [ ] 35.1 RED: add `TestExtend_NilRootRejected` (the former `TestExtend_NilRoot` is now `TestExtend_UnderRoot`), replace `Flatten()` with `ExtendIntoRoot()` in the placement/repeat/no-rollback/disabled/after-require/health tests, assert one Info record in `TestExtend_Disabled`, and add `AsStarter()` to Starter fixtures of `TestExtend_Phases`/`TestExtend_Lifecycle` (extensions scenarios); verify `go test -race -run 'TestExtend_|TestRequire_' .` fails to compile naming only `ExtendIntoRoot`
- [ ] 35.2 GREEN: gopls Rename `Flatten`→`ExtendIntoRoot`, `Extend` returns `ErrNilInjector` for nil/typed-nil root, and the decline is logged as one Info record; verify the 35.1 tests pass and `make test` is green
- [ ] 35.3 REFACTOR: Rename Variable for internals named "flatten" visible in errors or logs; verify `make test` and `make lint` pass
- [ ] 35.4 Docs: update the extension guide, `ExtendIntoRoot` godoc and example and the migration row (`Flatten` → `ExtendIntoRoot`; nil root); verify `go test -run ExampleExtend .` passes

## 36. Stale spec text follows the API changes

This group depends on: the error-prefix group (`doger:`), the closed-scope group (`ErrShutdown`), the Alias rename group, the ExtendIntoRoot group, the levelled-logging group (`Logger`), the per-service hook signature group, and the Starter opt-in group. Run it after all of them.

- [ ] 36.1 RED: change the expected text in the existing tests named in the sweep rows of the Test Strategy, covering the messages in invoke_all_order_test.go, invoke_all_struct_test.go, options_test.go, scope_hooks_test.go, root_start_test.go, properties_test.go, health_run_test.go, health_report_test.go and healthcheck_timeout_test.go to `doger: …` (scenarios "Panicking Order()", "Invalid collection fields", "Invalid classification", "Repeated options", "Invalid opt-in combinations", "Starting hook allows or vetoes Scope.Start", "Single-service bound"); verify `go test -race -run 'Order|Collection|Classification|Options|ScopeHooks|PropertySource|HealthCheck|HealthReport' .` fails only on the message assertions before the prefix group lands, and passes after it
- [ ] 36.2 RED: rewrite the Logf-based fixtures as recording or panicking `slog.Handler`s: container_hooks_test.go "HookContainerPanic overrides the default"; scope_hooks_test.go/scope_test.go "Closing and Closed around child scope shutdown", "Overlapping shutdowns…", "Later scope hook panics are logged with their point"; properties_test.go "A failing source provider…", "Values stay out of logs and errors"; health_run_test.go "A caller whose context has already ended starts no check"; health_monitor_test.go "Nil hooks are skipped"; and the timing hooks of health_report_test.go "Nothing is recorded…" and "Report reads and rounds start no check…". Verify they fail to compile only on `InjectorOpts.Logger`, or fail on a missing `health check started`/`panic recovered` record
- [ ] 36.3 GREEN: emit the Debug `health check started` record (scope, service) where a check starts in `health_run.go`, and route later hook panics as Error `panic recovered` (point) plus Debug `panic value` (container-logging); verify the 36.2 tests, `TestLogging_HealthCheckStarted` and `TestLogging_HealthCheckStartedInfoLevel` pass and `make test` is green
- [ ] 36.4 RED/GREEN (mechanical): `ErrStartAfterShutdown` → `ErrShutdown` in container_hooks_test.go; `As`/`AsNamed` → `Alias`/`AliasNamed` in health_run_test.go, health_monitor_test.go, health_report_test.go, options_test.go and build_hooks_test.go; `Flatten()` → `ExtendIntoRoot()` in properties_test.go; `AsStarter()` on the Starter fixtures of root_start_test.go "Hooks around each Starter" with the new start-hook signatures (scenarios "Failure point decides which hooks run", "Each underlying check runs once", "Report membership", "Which invocations fire the build hook", "Injectors inside and outside extension scopes", "Hooks around each Starter"); verify `go test -race ./...` fails to compile on the old identifiers before the rename groups and passes after them, and `make test` is green
- [ ] 36.5 REFACTOR: Extract Function `newRecordingLogger(t, level)` returning a `*slog.Logger` plus its records, shared by every test above (test helper only); verify `make test` and `make lint` pass
- [ ] 36.6 Check that no main-spec text is stale after archive: run `grep -rnE 'DI: |DefaultRootScope|Logf|ErrStartAfterShutdown|DisableStarterAutoDetection|\bFlatten\(|\bAs\[|\bAsNamed\b|ExportAs' openspec/changes/harden-first-release/specs` and confirm every hit is inside a REMOVED/RENAMED/Migration line or a scenario that pins the old name being gone

## 37. Duplicate scope names match ErrScopeAlreadyDeclared

This group depends on: the error-prefix group and the "Detach shut-down child scopes" group (for the after-shutdown row).

- [ ] 37.1 RED: add `TestResolutionErrors_DuplicateScope` (rows same, after_shutdown, sibling, grandchild, other_depth) and `TestResolutionErrors_DuplicateScopeConcurrent` (scenarios "Declaring a child scope name twice", "Concurrent declarations of one child scope name"); verify `go test -race -run TestResolutionErrors_DuplicateScope .` fails to compile, naming only `ErrScopeAlreadyDeclared`
- [ ] 37.2 GREEN: add `ErrScopeAlreadyDeclared`; make the duplicate-child panic in `Scope()` wrap it, with message ``doger: scope `<name>` has already been declared``, decided under the parent's write lock; verify the 37.1 tests pass under `-race` and `make test` is green
- [ ] 37.3 REFACTOR: Extract Function `duplicateScopeError(name)` next to the duplicate-service error helper; verify `make test` and `make lint` pass
- [ ] 37.4 Add `ErrScopeAlreadyDeclared` godoc and a row to the migration guide; verify `go doc doger.ErrScopeAlreadyDeclared` shows the doc

## 38. HealthCheckTimeout defaults to 10s

- [ ] 38.1 RED: add `TestHealthCheck_DefaultTimeoutZeroConfig` and update the default rows of `TestHealthCheck_TimeoutOptionsOnClock`, `TestHealthCheck_NotBeforeClockReachesTimeout`, `TestHealthCheck_PerServiceTimeout`, `TestHealthCheck_ConsumerDeadlineOnContextOnly`, `TestHealthCheckSingleTimeout` and `TestHealthCheck_JoinerOwnBound` (health-check-timeouts scenarios); verify `go test -race -run 'TestHealthCheck' .` fails on the default rows
- [ ] 38.2 GREEN: forced-path and single-service bounds use the report bound (0 → 10s, negative → none) and a global ≤ 0 means no global bound; verify the 38.1 tests pass and `make test` is green
- [ ] 38.3 REFACTOR: Consolidate Duplicate Conditional Fragments into one `serviceBound(opts, path)` helper; verify `make test` and `make lint` pass
- [ ] 38.4 Docs: godoc of `HealthCheckTimeout`/`HealthCheckSingleTimeout` and the CHANGELOG/migration row ("set a negative value for unbounded"); verify `go doc InjectorOpts.HealthCheckTimeout` states the default

## 39. A Goexit in a check does not block later checks

- [ ] 39.1 RED: regression tests `TestHealthCheck_GoexitDoesNotWedge`, `TestHealthCheck_GoexitAllWaiters` and `TestHealthRound_GoexitKeepsRecording`; verify `go test -race -run Goexit .` fails (second call times out, HealthCheck invoked 1 time)
- [ ] 39.2 GREEN: defer `finishCall` in `runCall` with a "did not return" flag producing `fmt.Errorf("%w: runtime.Goexit", ErrHealthCheckPanic)`, logged at Error without calling HookContainerPanic; verify the 39.1 tests pass with goleak
- [ ] 39.3 REFACTOR: Extract Function `runCheckGuarded` wrapping the check, recover and Goexit detection; verify `make test` and `make lint` pass

## 40. A monitored read joins the first check in flight

- [ ] 40.1 RED: add `TestHealthReport_ReadAfterReadyJoinsFirstRound` and `TestHealthReport_ConcurrentFirstReadsShareRound` and update `TestHealthReport_MonitoredFirstCheck` rows; verify `go test -race -run 'TestHealthReport_(ReadAfterReady|ConcurrentFirstReads|MonitoredFirstCheck)' .` fails (got HealthUnknown)
- [ ] 40.2 GREEN: a monitored read of a never-checked service joins its in-flight first check under the read's bound; verify the 40.1 tests pass and `make test` is green
- [ ] 40.3 REFACTOR: Inline the monitored and unmonitored branches into one `needsWait` predicate; verify `make test` and `make lint` pass
- [ ] 40.4 Docs: align the godoc of `HealthReportWithContext`, `HealthReport.Monitored` and `HealthScheduler` with the joined read; verify `go doc HealthReport.Monitored` shows it

## 41. HealthReportOption is a functional option

- [ ] 41.1 RED: add `TestHealthReportOption_Compose` (rows include a nil option) and update the nil row of `TestHealthReport_MatchTags` (scenarios "Report options are functions a consumer can compose", "Filtered reports"); verify it fails to compile on `HealthReportOption(nil)` against the struct type
- [ ] 41.2 GREEN: `type HealthReportOption func(*reportQuery)` and skip nil options; verify `TestHealthReport_MatchTags*` and the new test pass and `make test` is green
- [ ] 41.3 REFACTOR: none needed — a mechanical type change; verify `make test` and `make lint` pass
- [ ] 41.4 Docs: add `ExampleHealthReportOption` (a consumer composes `liveness()`); verify `go test -run ExampleHealthReportOption .` passes

## 42. Health enums and reports serialize for monitoring

- [ ] 42.1 RED: add `TestHealthEnums_TextRoundTrip`, `TestHealthEnums_UnmarshalTextRejectsUnknown`, `TestHealthReport_MarshalJSON` and `TestHealthReport_MarshalJSONEdgeCases`; verify `go test -race -run 'TestHealthEnums|TestHealthReport_MarshalJSON' .` fails (no text methods; `Err` marshals as `{}`)
- [ ] 42.2 GREEN: `MarshalText`/`UnmarshalText` on `HealthState` and `ContainerPhase`, and `MarshalJSON` on `HealthReport`/`ServiceHealth` (error string, omitted zero times, duration string, `[]` services); verify the 42.1 tests pass and `make test` is green
- [ ] 42.3 REFACTOR: Extract Function `enumText` shared by both enums' `String` and `MarshalText`; verify `make test` and `make lint` pass
- [ ] 42.4 Docs: add `ExampleHealthReport_json` with `// Output:` and document the JSON shape on the health docs page; verify `go test -run ExampleHealthReport_json .` passes

## 43. Web UI escapes names and links

- [x] 43.1 RED: add `TestPages_EscapeNames` and `TestPages_BasePathEscaped` in `http/pages_test.go` and `TestWebUI_HostileNamesEscaped`, `TestWebUI_LinkOpensNamedService`, `TestWebUI_LinksResolve` and `TestWebUI_NoReflection` in `http/{std,gin,echo,fiber,chi}/web_ui_test.go` (web-ui escaping scenarios); verify `go test -race ./...` fails on the escape assertions in `http` and each adapter module
- [x] 43.2 GREEN: replace `text/template` with one `html/template` set parsed at init using recursive `{{template}}` blocks and `url.Values` links (D12); verify the 43.1 tests pass in every module and `make test-all` (or a per-module loop until group 46) is green
- [x] 43.3 REFACTOR: Inline Function `fromTemplate`, Extract Function `scopeLink`/`serviceLink` and Move Function for the page data builders; verify the per-module tests and lint pass

## 44. Web UI headers, read-only routes and error bodies

- [x] 44.1 RED: add `TestWebUI_DefensiveHeaders`, `TestWebUI_MiddlewareAddsHeaders`, `TestWebUI_NonGETRejected`, `TestWebUI_RenderFailureGeneric500`, `TestWebUI_ReadOnly` and `TestWebUI_Fallbacks` per adapter (web-ui header/read-only/fallback scenarios); verify each adapter fails on missing headers, std POST returning 200 and chi/std leaking error text
- [x] 44.2 GREEN: a shared `dogerhttp.WriteHeaders` helper called on every response and redirect, GET/HEAD-only patterns in std and a generic 500 body; verify the 44.1 tests pass in every module
- [x] 44.3 REFACTOR: Extract Function for the per-adapter `response` into the shared helper where the framework allows, and Rename Variable `url`→`target`; verify the per-module tests and lint pass

## 45. Web UI renders bounded dependency trees

- [ ] 45.1 RED: add `TestPages_TreeSizeFollowsGraph`, `TestPages_ReferenceLinks` and `TestWebUI_LayeredGraphBounded` per adapter (scenarios "Tree size follows the graph shape", "A reference links to the referenced service", "A layered graph renders each service once"); verify they fail (about 26 MB and 121391 entries for svc-23)
- [ ] 45.2 GREEN: render `Ref` nodes as links with no children via the recursive template block; verify the 45.1 tests pass with the body under 64 KiB within 1s
- [ ] 45.3 REFACTOR: Extract Function `dependencyTree` template block shared by Dependencies and Dependents; verify the per-module tests and lint pass

## 46. Web UI source paths and mount options

- [x] 46.1 RED: add `TestWebUI_SourcePathDisplay` per adapter and `TestPages_ShortenSourcePath`, `FuzzShortenSourcePath` and `TestPages_NeverInvokedNoLocation`, plus `TestWebUI_ErrorHandler` per adapter (scenarios "Source path display follows the option", "Shortening handles unusual paths", "A service never invoked shows no location", "A consumer error handler receives the render failure"); verify they fail to compile only on `dogerhttp.Option`/`WithFullSourcePaths`/`WithErrorHandler` and on full paths shown
- [x] 46.2 GREEN: add `type Option`, `WithFullSourcePaths()`, `WithErrorHandler(func(ctx, err))` (last wins, nil means discard; called once per render failure before the generic 500, fiber passing `c.UserContext()`) and variadic `...Option` on each adapter's `Use` and the page functions, plus a shortener for `/` and `\` separators; verify the 46.1 tests pass and `go test -fuzz=FuzzShortenSourcePath -fuzztime=30s ./http` finds no crasher
- [x] 46.3 REFACTOR: Introduce Parameter Object `pageOptions` threaded through the page builders; verify the per-module tests and lint pass

## 47. Web UI mounts behind middleware and under concurrency

- [ ] 47.1 RED: add `TestWebUI_BehindAuthMiddleware` per adapter, `TestWebUI_ChiMuxStillAccepted` and `TestWebUI_ConcurrentViewing` (std, chi) (scenarios "Authentication middleware guards the UI", "Existing chi mounts keep compiling", "Viewers race with container churn"); verify the chi `r.With(auth)` rows fail to compile on `*chi.Mux`
- [ ] 47.2 GREEN: change `dogerchi.Use` to take `chi.Router`; verify the 47.1 tests pass under `-race` with goleak
- [ ] 47.3 REFACTOR: none needed — only the parameter type changes; verify the per-module tests and lint pass
- [ ] 47.4 Docs: fix `docs/docs/troubleshooting/web-ui.md` (drop the "health-check status" claim; add headers, bounded trees, `WithFullSourcePaths` and per-adapter middleware examples) and remove `doger.Options{EnableWebUI, WebUIPort}` from `docs/static/llms.txt`; verify `grep -c EnableWebUI docs/static/llms.txt` prints 0 and `cd docs && npm run build` passes
- [ ] 47.5 Docs: `examples/web-application` mounts the UI with `r.With(basicAuth(os.Getenv("DEBUG_USER"), os.Getenv("DEBUG_PASSWORD")))` on a `127.0.0.1` listener, and each adapter gets an `Example` with a middleware mount; verify `go build ./...` in `examples/web-application` and `go test -run Example ./...` in each adapter module pass

## 48. Make targets for every workspace module

- [x] 48.1 Add `make modules`, `test-all`, `lint-all` and `vuln` looping over `go list -m -f '{{.Dir}}'`, continuing past failures and naming the failing module; verify `make modules` lists every `go.work` entry and `make test-all` exits 0
- [x] 48.2 Add `scripts/test-make-targets.sh` that plants a failing test and a lint issue in a temp copy of each module and expects a non-zero exit naming it (scenarios "A failure in any module fails make test-all", "An issue in any module fails make lint-all"); verify the script passes
- [x] 48.3 Pin tool versions in `make tools` (golangci-lint, govulncheck, benchstat; no `@latest`) and drop `go get -t -u`; verify `grep -c '@latest' Makefile` prints 0

## 49. Lint configuration and adapter lint fixes

- [ ] 49.1 Enable `gosec`, `errorlint` and `noctx` in `.golangci.yml` with an inline justification for every suppression; verify `make lint-all` reports the expected new issues (baseline for 47.2)
- [ ] 49.2 Fix the 8 adapter issues (errcheck in chi/echo/fiber, ST1013 in std/chi) and every gosec/errorlint/noctx finding across modules (scenario "The head of main is lint-clean"); verify `make lint-all` exits 0

## 50. Vulnerability gate and dependency floors

- [x] 50.1 Re-enable the govulncheck job in `security.yml` running `make vuln` and confirm it fails on fiber's brotli v1.1.0 (scenario "A reachable vulnerability fails the gate"); verify the job log or local `make vuln` names GO-2026-6888
- [x] 50.2 Bump `github.com/andybalholm/brotli` ≥ v1.2.7 in `http/fiber` and its example, and `golang.org/x/net` ≥ v0.62.0 in `http/echo` and `http/gin` (scenario "Released dependency floors"); verify `make vuln` exits 0 and `make test-all` is green
- [x] 50.3 Add SECURITY.md with a private reporting channel and "Accepted advisories" listing GO-2026-6875/GO-2026-6872 (scenario "An accepted vulnerability without a fix is recorded"); verify the policy section of `scripts/check-workflows.sh` passes

## 51. Workflow supply chain

- [x] 51.1 Add `scripts/check-workflows.sh` (SHA-pinned `uses`, top-level `permissions: contents: read` and allowed raises, test.yml matrix, release.yml provenance, dependabot condition and directories, go.mod floors, policy files, `--self-test`); verify it fails on the current tree (RED baseline)
- [x] 51.2 Pin every action in `.github/workflows/*` to a 40-char SHA with a `# vX.Y.Z` comment and add top-level `permissions: contents: read` with per-job raises only in the `doc.yml` deploy job, the `security.yml` `codeql` and `trivy-codeql` jobs, the `release.yml` release job and dependabot-automerge (scenarios "Workflow pinning and permissions are checked", "A tag-pinned action fails the check"); verify the pinning and permissions sections of the script pass
- [ ] 51.3 `test.yml` runs `make test-all` on ubuntu, macos and windows with Go 1.27.x and stable, and `lint.yml` runs `make lint-all` (scenarios "CI runs the module loop on three operating systems", "A clean tree passes make test-all", "make modules lists every workspace module"); verify the matrix check passes and CI is green on all 3 OSes
- [x] 51.4 Restrict dependabot auto-merge to `dependabot[bot]` authors and semver patch/minor updates, and add `/scheduler/**` and `/clock/**` gomod entries (scenarios "Auto-merge decision", "Every module directory is covered"); verify the dependabot checks of the script pass
- [ ] 51.5 Set Bearer's `log-level` in `bearer.yml` to its default and delete `vercel.json`; verify the policy check passes and the security workflow runs

## 52. Release provenance and dry run

- [x] 52.1 Add `scripts/release-dry-run.sh <semver>` mirroring release.yml post-processing into a file proxy and checking that a clean `GOWORK=off` consumer gets and builds every released module with the root pinned, no `replace` and no test-only requirements; verify it fails on the current tree because `scheduler/gocron` still requires testify and goleak (RED)
- [ ] 52.2 Change release.yml: job `if: github.ref == 'refs/heads/main'` and `environment: release`, `go mod tidy` in every released module before pinning, `go build`/`go vet` after pinning, `make vuln` before tagging, tag `clock/clockwork`, and `generate_release_notes: true` (scenarios "The release workflow enforces provenance", "A dispatch from another branch is refused", "Test-only requirements are stripped", "Runtime requirements are kept", "Released adapters pin the root release"); verify `scripts/release-dry-run.sh v0.99.0` and the release section of `scripts/check-workflows.sh` pass
- [ ] 52.3 Add a CI job `release-dry-run` in `test.yml` running `scripts/release-dry-run.sh v0.99.0`, its invalid-version rows and `scripts/check-workflows.sh` (scenarios "A clean consumer builds every released module", "The dry run rejects an invalid version"); verify the job is green
- [x] 52.4 Add CHANGELOG.md with an "Unreleased" section listing every BREAKING row of design.md D10/D9 with its migration, and `.github/CODEOWNERS` (scenario "Policy files are present"); verify the policy section of `scripts/check-workflows.sh` passes

## 53. Close the audit's test-suite gaps (existing behavior, tests only)

- [ ] 53.1 Add `TestShutdownOrder_ThreeLevelChain` (synctest; a→b→c in one scope, c must not stop before b finishes) and `TestDAG_removeDependent`; they pass on arrival, so prove they can fail by temporarily deleting the `removeService` dependents loop (mutant M07, not committed); verify `go test -race -run 'TestShutdownOrder_ThreeLevelChain|TestDAG_removeDependent' .` fails under the mutant and passes without it
- [ ] 53.2 Add `TestLazyShutdown_ShutdownerWithContext_IsCalled` and ended-context rows for lazy and eager services; prove they can fail by temporarily replacing `instance.Shutdown(ctx)` with `_ = instance` (mutant M29); verify the same red/green check
- [ ] 53.3 Add `TestHealthReport_TimeoutNotOverwrittenByLateResult` (synctest: an unbounded `HealthCheckNamed` holds the check while `HealthReport` times out); prove it can fail by temporarily dropping `&& !call.timedOut` in `finishCall` (mutant M18); verify the same red/green check
- [ ] 53.4 Add `TestInvokeNamedAny_ListedAsInvoked`; prove it can fail by temporarily removing `onServiceInvoke` from `invokeAnyByNameAs` (mutant M22); verify the same red/green check
- [ ] 53.5 Add `TestHealthCheck_QueuedDeadlineBoundary` (`HealthCheckParallelism: 1`; a queued check returning exactly at its deferred bound reports a timeout) for mutant M15, recording in a comment if the mutant proves equivalent; verify `go test -race -run TestHealthCheck_QueuedDeadlineBoundary .`
- [ ] 53.6 Cover the uncovered health branches (service left while callers queued, take-over by a fresh check, queue ordering, `keepOnly` pruning, `deliverTransitions` re-panic, scheduler double-start guard) as named subtests in `health_run_test.go`/`health_monitor_test.go`; verify `go test -race -coverprofile=c.out . && go tool cover -func=c.out` shows those blocks covered
- [ ] 53.7 Cover the virtual-scope pass-through methods called from inside a provider in `virtual_scope_test.go` and the broken alias/export chains of `aliasResolvesTo`; verify the coverage report shows `virtual_scope.go` pass-throughs and `aliasResolvesTo` covered
- [ ] 53.8 Add `TestShutdownOnSignals_Default` (SIGUSR1 through a test-only signal set) and give `ExampleRootScope_ShutdownOnSignals` a runnable `// Output:`; verify `go test -race -run ShutdownOnSignals .` passes
- [x] 53.9 Harden the `tests` package: goleak `TestMain`, a `testWithTimeout` with a 5s floor reporting through `t` instead of `os.Exit`, and real report/error assertions in `TestParallelShutdown`, `TestConcurrentHealthCheckDuringShutdown`, `TestConcurrentBlockingOperations` and `TestRaceConditionShutdownInvocation`; verify `go test -race -count=20 -shuffle=on ./tests` passes
- [ ] 53.10 Replace the `time.Sleep` synchronisation in `scheduler/gocron/scheduler_test.go` with channel handshakes or a fake clock; verify `go test -race -count=50 -shuffle=on ./...` in `scheduler/gocron` passes
- [ ] 53.11 Tighten bare `Error` assertions on hot paths (invoke, shutdown, start, health) to `ErrorIs`/`ErrorAs` with the expected sentinel; verify `make test` passes

## 54. Performance (behavior-preserving, D17)

- [ ] 54.1 Add benchmarks to `bench/` (`BenchmarkInvokeBuilt` depth 0/5, `BenchmarkInvokeBuiltParallel`, `BenchmarkInvokeTransient`, `BenchmarkInvokeAll` 10 members + 0/1k/10k unrelated, `BenchmarkPerRequestScope`, `BenchmarkProvideValue100`, `BenchmarkHealthCheck50`, `BenchmarkShutdownChain` 1k/4k) and record a baseline; verify `go test -run '^$' -bench . -benchmem -count=10 ./bench > bench/baseline.txt` succeeds
- [ ] 54.2 REFACTOR (Split Phase): read lock in `onServiceInvoke` with the write lock only on a first invocation, and the published-instance fast path in `instanceOrBuild`; verify `make test` green before and after and `benchstat` plus a mutex profile show lower contention on `BenchmarkInvokeBuiltParallel`
- [ ] 54.3 REFACTOR (Replace Algorithm): build `InvokeAll`/`InvokeAllMap` candidates from the per-scope type-match cache; verify `make test` and `BenchmarkInvokeAll` stays flat across 0/1k/10k unrelated
- [ ] 54.4 REFACTOR (Introduce cache): cache type names by `reflect.Type` and skip the virtual-scope allocation for an already-built service; verify `make test` and `BenchmarkInvokeBuilt` at or below 3 allocs/op
- [ ] 54.5 REFACTOR (Replace Algorithm): plan shutdown with in-degree counters (Kahn, O(V+E)); verify the shutdown-order tests including 51.1 pass and `BenchmarkShutdownChain` scales linearly from 1k to 4k
- [ ] 54.6 REFACTOR: skip log argument building when no logger is set and cut health-check allocations; verify `make test` and `BenchmarkHealthCheck50` allocs/op below baseline
- [ ] 54.7 Commit `bench/RESULTS.md` comparing `bench/baseline.txt` with `bench/after.txt`; verify `benchstat bench/baseline.txt bench/after.txt` shows no regression above 5%

## 55. Documentation for the first release

- [ ] 55.1 Add `doc.go` with a package overview (quickstart, scopes, lifecycle, health, extensions) and remove the `package-comments` suppression; verify `make lint` passes and `go doc . | head -40` shows the overview
- [ ] 55.2 Remove the `Play:` links that run samber/do code (or replace them with doger playground links); verify `grep -rn 'go.dev/play' --include=*.go . | wc -l` prints 0 or each remaining link imports `github.com/kartaladev/doger`
- [ ] 55.3 Rewrite `docs/docs/migrating/migrating-from-samber-do.md` from the D9/D10 tables (every break with before/after code, the variadic `...ProvideOption` changes, all new `InjectorOpts` fields, the Go 1.27 floor and its reason); verify the before/after snippets compile in a `docs_snippets_test.go`
- [ ] 55.4 README: add a 20-line quickstart, fix the gocron snippet's unused `err`, remove upstream templates and "See also" links, and update the feature list (start lifecycle, extensions, properties, kinds, collections, health reports); verify the snippets compile in `readme_snippets_test.go`
- [ ] 55.5 Add the missing `Example*` tests with `// Output:` (`Eager`, `Lazy`, `Transient`, `Package`, `Bind`, `ExportNamed`, `MustExport`, `ExtendIntoRoot`, `ExtendScopeName`, `WithHealthTags`, `AsOptionalHealth`, `IgnoreContainerPhase`, `ExtensionProperties`, `HealthScheduler`, `EnableRunnerAutoDetection`, `PropertySource`, `StructTagKey`); verify `go test -run '^Example' .` passes
- [ ] 55.6 Document the Go 1.27 floor and why it exists in README and `docs/docs/getting-started`; verify `grep -n '1.27' README.md` shows it

## 56. Integration checks

- [ ] 56.1 Run `make test-all lint-all vuln`; verify every module is green on the local OS
- [ ] 56.2 Run every audit reproducer from the audit scratchpad against the final tree in a temporary package (not committed); verify each now passes or is superseded by its scenario test
- [ ] 56.3 Run `openspec validate harden-first-release --strict` and cross-check every `#### Scenario:` header against a passing test in the design.md Test Strategy table; verify no scenario is unmapped
- [ ] 56.4 Run `scripts/release-dry-run.sh v0.99.0` against a local file proxy; verify a clean consumer gets and builds the root and every submodule without `go.work`


## Workflow follow-up

- Archive the change once review is done and every scenario maps to a passing test (`openspec validate harden-first-release --strict`, `make test-all`, `make lint-all`).
- After archive, edit the Purpose of `openspec/specs/service-aliases/spec.md` to name `Alias`/`AliasNamed`.
- Rebase `add-config`, `add-eventing` and `add-durable-events` onto this change with `/opsx:update` (DefaultRootScope removal, `Logger`, `Clock`, `ErrShutdown`, `NewWithOpts` copy semantics).
- Cut the first release tag from `main` through the protected `release` environment.
