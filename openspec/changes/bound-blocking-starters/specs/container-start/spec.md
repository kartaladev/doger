# Spec Delta

## MODIFIED Requirements

### Requirement: Start honours its context
`StartWithContext` SHALL check its context before each candidate build, each kind-handler call and each Starter. A cancelled or expired context SHALL stop Start with an error wrapping `ctx.Err()`. Context-aware `Start(ctx)` variants, handlers and runners SHALL receive a context derived from it, which is cancelled when `Start` returns. `Start()` SHALL use `context.Background()`, and a nil context SHALL be treated as `context.Background()`.

#### Scenario: Cancellation and deadline
```gherkin
Given a root scope with lazy Starters "a" then "b", inside synctest.Test
And "a" is a Start(ctx) error that <a-behavior>
When the consumer calls StartWithContext(ctx) where ctx <ctx>
Then errors.Is(err, <cause>) is true
And "b" was not started

Examples:
  | ctx                         | a-behavior                                    | cause                    |
  | is already cancelled        | returns nil                                   | context.Canceled         |
  | has a 5s deadline           | blocks until ctx.Done() and returns ctx.Err() | context.DeadlineExceeded |
  | is cancelled while "a" runs | returns nil after the cancellation            | context.Canceled         |
```

#### Scenario: Nil context
```gherkin
Given a root scope with one lazy Starter "a" of type Start(ctx) error
When the consumer calls StartWithContext(nil)
Then Start returns nil
And "a" received a non-nil context
```

#### Scenario: Start-time context ends when Start returns
```gherkin
Given a root scope with one lazy Starter "a" of type Start(ctx) error that keeps the received context
When the consumer calls Start()
Then Start returns nil
And the kept context's Err() is context.Canceled
```

<!-- cases: 2 overrides: with StartTimeout unset, a Starter that ignores an ended context is still awaited; InjectorOpts.StartTimeout makes the run stop waiting for it, see "A start timeout abandons a stuck step" and "With a start timeout, an ended run context waits for the step up to the timeout" -->
<!-- cases: 3 zero values: nil context is covered by "Nil context" -->
<!-- cases: 8 concurrency: deadline scenarios run under testing/synctest, so time is virtual and no goroutine leaks -->
<!-- cases: N/A 9 panics: covered by "Start errors and panics" -->
<!-- cases: N/A 10 compatibility: new API -->

### Requirement: Per-service start hooks
For each Starter, `Start` SHALL call every `HookBeforeStart(ctx, scope, name)` just before its `Start` method and every `HookAfterStart(ctx, scope, name, elapsed, err)` just after, where `ctx` is the context the run passes to its steps, `elapsed` is the time the method took on the container clock, and `err` is the error the method returned, unwrapped (nil on success, the recovered panic as an error). The Starter receives its own child of that `ctx`, which reports no deadline, is not cancelled when the Starter returns, and is also cancelled, with a cause matching `ErrShutdown`, when a shutdown of the Starter's child scope or of an ancestor child scope begins while it runs (see "Child scope shutdown waits for a start that is starting its services"); with `InjectorOpts.StartTimeout` set, it is also cancelled when the step is abandoned, and an abandoned Starter's `HookAfterStart` call follows "Start hooks for an abandoned step". `HookAfterStart` is called for a step that such a scope shutdown cancelled too, with the error the Starter returned. Hooks SHALL run in the order they were added. `AddBeforeStartHook`/`AddAfterStartHook` SHALL append to the same lists.

#### Scenario: Hooks around each Starter
```gherkin
Given a root scope with HookBeforeStart and HookAfterStart recording calls
And a hook added later with AddAfterStartHook recording "late-after"
And Starters "a" (succeeds) then "b" (returns errBoom), both opted in with AsStarter()
When the consumer calls Start()
Then the recorded calls are: before a, after a nil, late-after a, before b, after b errBoom, late-after b
And the err passed for "b" is errBoom itself, not a wrapping error
```

#### Scenario: HookAfterStart for a step a child scope shutdown cancelled
```gherkin
Given a started root container created with InjectorOpts{StartTimeout: <timeout>} and a HookAfterStart recording its calls for "w", inside synctest.Test
And child scope "tenant", created after Start, with lazy Starter "w" whose Start(ctx) waits for ctx to end and returns ctx.Err()
And the consumer called tenant.Start() in another goroutine
When 100ms of virtual time passes and the consumer calls tenant.Shutdown()
Then tenant.Start() returns nil
And HookAfterStart was called <calls> times for "w", with elapsed <elapsed> and err <err>

Examples:
  | timeout | calls | elapsed | err                                   |
  | 0       | 1     | 100ms   | context.Canceled itself, not wrapped |
  | 5s      | 1     | 100ms   | context.Canceled itself, not wrapped |
```

<!-- cases: N/A 3 zero values: no hooks registered means no calls (default; covered by every other scenario) -->
<!-- cases: N/A 4 boundaries: 0, 1 and many hooks follow the same slice iteration as the existing shutdown hooks -->
<!-- cases: 7 lifecycle: an abandoned Starter is covered by "Start hooks for an abandoned step"; a Starter skipped by "An abandoned step is not started again while it runs", because it is recorded or its abandoned call still runs, gets no hook call; a Starter a child scope shutdown cancelled gets its HookAfterStart call, with the error it returned ("HookAfterStart for a step a child scope shutdown cancelled") -->
<!-- cases: 9 panics: a panicking start hook propagates (start hooks are not recovered; shutdown hooks are, see container-shutdown); container state is still released, see "A panic escaping Start still releases Shutdown" -->
<!-- cases: N/A 8 concurrency: hooks run on the Start goroutine, also with StartTimeout set -->
<!-- cases: 10 compatibility: new hooks; the Starter now receives a child of the hook ctx, also with StartTimeout unset: it carries the same values and ends with it, and ends earlier only when its scope shuts down (D3, D12) -->

### Requirement: Refresh starts what is not started yet
`RootScope.Refresh()` / `RefreshWithContext(ctx)` SHALL run the start pipeline over the services and scopes not yet started or handled: build them, run kind handlers for their new components, and start their Starters, with the same order, context, error and per-service hook rules as `Start`. A service SHALL be started successfully at most once per container: a Starter whose build or Start failed is not recorded as started, so the next Refresh or Scope.Start that covers it SHALL try it again. A Starter whose abandoned Start call is still running is the exception, see "An abandoned step is not started again while it runs".

#### Scenario: Refresh starts late registrations and new scopes once
```gherkin
Given a root scope with lazy Starter "a" and the consumer called Start()
And then the consumer registers lazy Starter "late" in the root and creates child scope "tenant" with lazy Starter "worker"
When the consumer calls Refresh() <times>
Then every call returns nil
And the start counts are a: 1, late: 1, worker: 1
And the start order of the first Refresh is late, worker

Examples:
  | times |
  | once  |
  | twice |
```

#### Scenario: Refresh with nothing new
```gherkin
Given a root scope with lazy Starter "a" and the consumer called Start()
When the consumer calls Refresh()
Then Refresh returns nil
And "a" was started 1 time
```

#### Scenario: A Starter that failed is tried again
```gherkin
Given a started root container and lazy Starter "late", registered after Start in <scope>, whose <failing> returns errBoom on its first call and nil afterwards
When the consumer calls <operation> twice
Then the first call returns an error matching errBoom and the second returns nil
And "late"'s provider was called <builds> times and its Start <starts> times
When the consumer calls <operation> again
Then it returns nil
And "late"'s provider was called <builds> times and its Start <starts> times

Examples:
  | scope                                     | operation      | failing  | builds | starts |
  | the root                                  | root.Refresh() | Start    | 1      | 2      |
  | child scope "tenant", created after Start | tenant.Start() | Start    | 1      | 2      |
  | the root                                  | root.Refresh() | provider | 2      | 1      |
  | child scope "tenant", created after Start | tenant.Start() | provider | 2      | 1      |
```

<!-- cases: 1 default: Refresh after Start -->
<!-- cases: N/A 2 overrides: StartParallelism, StartTimeout and context apply as for Start -->
<!-- cases: 3 zero values: nothing new; a nil context is treated as context.Background(), as for Start -->
<!-- cases: 4 boundaries: zero, one and several new services; repeated Refresh -->
<!-- cases: 5 errors: failures are wrapped as for Start ("Incremental start failures are StartError values"); a Starter whose Start or build failed is tried again ("A Starter that failed is tried again"); an abandoned one that still runs is skipped with its dependents, and the run reports it after starting the rest ("An abandoned step is not started again while it runs") -->
<!-- cases: 6 duplicates: a second Refresh starts nothing again; only a successful Start counts -->
<!-- cases: 7 lifecycle: before Start and after Shutdown are covered by "Incremental start needs a started container" -->
<!-- cases: 8 concurrency: covered by "Concurrent Refresh calls are serialized" -->
<!-- cases: 9 panics: a panicking Start method is recovered as for Start -->
<!-- cases: N/A 10 compatibility: new API -->

### Requirement: Child scope shutdown waits for a start that is starting its services
A child scope's `Shutdown`/`ShutdownWithContext` that begins while a `Start`, `Refresh` or `Scope.Start` is running with services of this scope or its descendants among its candidates SHALL cancel the contexts of their running start steps with a cause matching `ErrShutdown`, as root Shutdown does for a whole run, then wait for that run to return, bounded by its context, before shutting anything down. Those steps SHALL NOT fail the run or be recorded, and the run SHALL skip the rest of that subtree and continue. This skip rule SHALL apply only to the child scope's own Shutdown while root Shutdown has not begun: root Shutdown cancels the whole run, and a step of a child scope it reaches SHALL be cancelled and reported like a root step ("A Shutdown bounded below the start timeout"). A step of that subtree that the run skips, a cancelled one included, SHALL NOT count as started for its dependents: the run SHALL skip every candidate that depends on its service, directly or through others (an export included), without hook calls. A dependent inside the closing subtree is skipped silently. When the run skips a dependent outside it, the run SHALL start everything else and then fail: with the step's own `*StartError` matching `ErrStartTimeout` when the dependency's call was abandoned at `StartTimeout` and still runs, and otherwise with a `*StartError` naming the dependent that matches `ErrShutdown` (its dependency's scope is shutting down). When the dependent reaches several such skipped services, one whose abandoned call still runs SHALL decide the error over one skipped only because its scope is shutting down, and among the same reason the first by scope name, then service name, then scope ID (the last tie-break only separates same-named scopes, and is stable within a container only, since scope IDs are random); the error SHALL NOT depend on the order the dependency graph is walked in. The first step of the run that failed SHALL be the run's error instead. A step abandoned at `StartTimeout` whose dependents all lie inside the closing subtree SHALL NOT fail the run. With a `Logger` set, a cancelled step that returned an error or panicked is logged (container-logging, "A start step cancelled by its scope's shutdown that fails is logged"). A run that begins while the scope shuts down SHALL NOT start or handle services of that scope or of its descendant scopes. A descendant scope cannot be created under it then: `Scope()` panics with an error matching `ErrShutdown` (container-shutdown, "A closed scope stays closed"). Shutting such a scope down synchronously from code running in that run deadlocks, as for root Shutdown.

#### Scenario: Child shutdown waits for a running start
```gherkin
Given a root container and child scope "tenant" with lazy Starter "w" whose Start blocks until released
And the consumer has <before>
And <run> is running in another goroutine and "w" has entered Start
When the consumer calls tenant.Shutdown() in another goroutine
Then tenant.Shutdown() has not returned while "w" is blocked
When "w" is released
Then <run> returns nil
And tenant.Shutdown() returns a successful report
And "w" was shut down 1 time, after its Start returned
And the start bookkeeping holds no entry for "w"

Examples:
  | before                                          | run            |
  | done nothing                                    | root.Start()   |
  | called Start(), then registered "w" in "tenant" | root.Refresh() |
  | called Start(), then registered "w" in "tenant" | tenant.Start() |
```

#### Scenario: Child shutdown cancels the running start steps of its subtree
```gherkin
Given a root container created with InjectorOpts{StartTimeout: <timeout>, StartParallelism: <parallelism>}, inside synctest.Test, and child scope "tenant"
And lazy Starter "w" in "tenant" whose Start(ctx) error waits for ctx.Done(), then cleans up for 100ms of virtual time and returns ctx.Err(), and whose Shutdown() error records whether that cleanup had finished
And the consumer has <before>
And <run> is running in another goroutine and "w" has entered Start
When the consumer calls tenant.Shutdown() 100ms later
Then "w"'s Start context ends with a cause matching ErrShutdown
And tenant.Shutdown() returns a successful report after 100ms of virtual time
And <run> returns nil
And "w"'s Shutdown method was called exactly once, after its Start cleanup finished
And the start bookkeeping holds no entry for "w"

Examples:
  | timeout | parallelism | before                                                                   | run            |
  | 0       | 0           | done nothing                                                             | root.Start()   |
  | 0       | 0           | called Start(), then registered "w" in "tenant"                          | root.Refresh() |
  | 0       | 0           | called Start(), then registered "w" in "tenant"                          | tenant.Start() |
  | 5s      | 0           | called Start(), then registered "w" in "tenant"                          | root.Refresh() |
  | 5s      | 0           | called Start(), then registered "w" in "tenant"                          | tenant.Start() |
  | 0       | 4           | called Start(), then registered "w" and lazy Starter "x", whose Start returns nil at once, in "tenant" | root.Refresh() |
```

#### Scenario: Child shutdown waits for a stuck step up to the start timeout
```gherkin
Given a started root container created with InjectorOpts{StartTimeout: 5s}, inside synctest.Test, and child scope "tenant"
And lazy Starter "w" in "tenant", registered after Start, whose Start(ctx) error ignores ctx and blocks until its Shutdown() error method is called
And tenant.Start() is running in another goroutine and "w" has entered Start
When the consumer calls tenant.Shutdown() 100ms later
Then tenant.Shutdown() returns a successful report after 4.9s of virtual time
And tenant.Start() returns nil
And "w"'s Shutdown method was called exactly once
And the start bookkeeping holds no entry for "w" once its Start call returned
```

#### Scenario: Dependents of a step the child shutdown skipped are skipped
```gherkin
Given a started root container created with InjectorOpts{StartTimeout: <timeout>, StartParallelism: <parallelism>}, inside synctest.Test, and child scope "redis" created after Start
And lazy Starter "cache" in "redis", registered after Start, whose Start(ctx) error <behaviour>
And <dependent>
And lazy Starter "worker" in the root, registered after Start, depending on nothing
And <run> is running in another goroutine and "cache" has entered Start
When the consumer calls redis.Shutdown() 100ms later
Then <run> returns <result>
And "dep"'s Start was called 0 times and "worker"'s Start 1 time
And neither "cache" nor "dep" is recorded as started
And redis.Shutdown() returns a successful report once "cache"'s Start returned

Examples:
  | timeout | parallelism | behaviour                                                         | dependent                                                                                            | run            | result                                                      |
  | 0       | 0           | waits for ctx.Done(), then returns ctx.Err()                      | "cache" is exported to the root with ExportNamed, and lazy Starter "dep" in the root, registered after Start, uses DependsOnNamed("cache") | root.Refresh() | an error naming "dep" that matches ErrShutdown              |
  | 0       | 4           | waits for ctx.Done(), then returns ctx.Err()                      | "cache" is exported to the root with ExportNamed, and lazy Starter "dep" in the root, registered after Start, uses DependsOnNamed("cache") | root.Refresh() | an error naming "dep" that matches ErrShutdown              |
  | 5s      | 0           | waits for ctx.Done(), then returns ctx.Err()                      | "cache" is exported to the root with ExportNamed, and lazy Starter "dep" in the root, registered after Start, invokes "cache" in its provider | root.Refresh() | an error naming "dep" that matches ErrShutdown              |
  | 5s      | 0           | ignores ctx and blocks until its Shutdown() error method is called | "cache" is exported to the root with ExportNamed, and lazy Starter "dep" in the root, registered after Start, uses DependsOnNamed("cache") | root.Refresh() | an error naming "cache" that matches ErrStartTimeout        |
  | 0       | 0           | waits for ctx.Done(), then returns ctx.Err()                      | lazy Starter "dep" in "redis", registered after Start, uses DependsOnNamed("cache")                 | root.Refresh() | nil                                                         |
  | 5s      | 0           | ignores ctx and blocks until its Shutdown() error method is called | lazy Starter "dep" in "redis", registered after Start, uses DependsOnNamed("cache")                 | root.Refresh() | nil                                                         |
```

#### Scenario: A failure of the run wins over a skipped dependent
```gherkin
Given a started root container, inside synctest.Test, and child scope "redis" created after Start
And lazy Starter "cache" in "redis", registered after Start, whose Start(ctx) error waits for ctx.Done(), then returns ctx.Err()
And "cache" is exported to the root with ExportNamed, and lazy Starter "dep" in the root, registered after Start, uses DependsOnNamed("cache")
And child scope "other", created after "redis", with lazy Starter "bad", registered after "cache", depending on nothing, whose Start returns errBoom
And root.Refresh() is running in another goroutine and "cache" has entered Start
When the consumer calls redis.Shutdown() 100ms later
Then root.Refresh() returns an error naming "bad" that matches errBoom and not ErrShutdown
And "dep"'s Start was called 0 times
```

#### Scenario: A start that begins during a child shutdown skips the scope
```gherkin
Given a started root container and child scope "tenant" with lazy Starter "w" registered after Start
And tenant.Shutdown() is running in another goroutine, blocked in a HookScopeClosing hook
When the consumer calls root.Refresh()
Then Refresh returns nil
And "w" was started 0 times
```

#### Scenario: A scope created under a shutting-down scope is skipped
```gherkin
Given a started root container and child scope "tenant"
And tenant.Shutdown() is running in another goroutine, blocked in a HookScopeClosing hook
When the consumer creates grandchild "jobs" under "tenant"
Then the call panics with an error matching ErrShutdown
And root.Refresh() returns nil
And once tenant.Shutdown() returned a successful report, tenant.Children() is empty
```

#### Scenario: A scope created during a running start does not wait
```gherkin
Given a root container with lazy Starter "a" whose provider creates child scope "tmp", registers and invokes lazy "x" in it, and calls tmp.Shutdown()
When the consumer calls Start()
Then Start returns nil
And tmp.Shutdown() returned a successful report while Start was running
```

<!-- cases: 1 default: waiting and cancelling are not configurable -->
<!-- cases: 2 overrides: the context bounds the wait ("A child shutdown wait is bounded by its context"); StartTimeout bounds a step that ignores its cancelled context -->
<!-- cases: N/A 3 zero values: a nil context means context.Background(), as for every Shutdown -->
<!-- cases: 4 boundaries: a run with no candidate in the scope (a scope created during the run) does not wait; a step that honours its context returns before the wait ends; one that ignores it is abandoned at its StartTimeout -->
<!-- cases: 5 errors: a cancelled step is not a run failure; a dependent outside the closing subtree fails the run with ErrShutdown, or with the dependency's ErrStartTimeout while its abandoned call runs; a real failure of the run wins ("A failure of the run wins over a skipped dependent"); the context error has its own requirement -->
<!-- cases: N/A 6 duplicates: a second Shutdown of the same scope finds no running run with candidates in it -->
<!-- cases: 7 lifecycle: Start, Refresh and Scope.Start rows; a run that begins during the shutdown; the run continues with scopes outside the closing subtree; dependents inside the subtree are skipped silently, outside it (through an export, by DependsOnNamed or by invocation) they are skipped and reported; a candidate the run dropped at discovery because its scope was already closing is not a skipped step: a dependent of it fails or succeeds by its own build, as before -->
<!-- cases: 8 concurrency: the subject; runs under -race with goleak; StartParallelism 4 rows; the registry of running step contexts is empty after every run (normal, panic, abandoned and cancelled steps) -->
<!-- cases: N/A 9 panics: hook panics during the wait follow scope-lifecycle-hooks -->
<!-- cases: 10 compatibility: BEHAVIOR CHANGE (flagged) - a child scope Shutdown now cancels the running start steps of its subtree, as root Shutdown cancels a whole run, so it no longer waits forever on a Start that stops only when its context ends; a step so cancelled no longer fails the run. Earlier: a child scope Shutdown no longer races a running start; Start-time code that shuts down a scope the run is starting deadlocks; a grandchild created under a closing scope is refused with ErrShutdown -->

## ADDED Requirements

### Requirement: Start waits for every step by default
With `InjectorOpts.StartTimeout` zero or negative, Start, Refresh and Scope.Start SHALL wait for every Starter `Start` method and every kind-handler call to return, however long it runs and whether or not it honours its context, exactly as before this option existed.

#### Scenario: No start timeout waits for a slow step
```gherkin
Given a root scope created with InjectorOpts{StartTimeout: <timeout>}, inside synctest.Test
And lazy Starter "srv", opted in with AsStarter(), whose Start(ctx) error ignores ctx, sleeps 1h and returns nil
When the consumer calls StartWithContext(ctx) where ctx <ctx>
Then Start returns after 1h of virtual time with <result>
And "srv" is recorded as started: <started>

Examples:
  | timeout | ctx                     | result                                                                                           | started |
  | 0       | is context.Background() | nil                                                                                              | true    |
  | -1s     | is context.Background() | nil                                                                                              | true    |
  | 0       | has a 100ms deadline    | "doger: start aborted: context deadline exceeded", matching context.DeadlineExceeded, not ErrStartTimeout | true    |
```

<!-- cases: 1 happy path: the zero value row -->
<!-- cases: 2 overrides: a positive StartTimeout is the subject of "A start timeout abandons a stuck step" -->
<!-- cases: 3 zero values: 0 and a negative value both mean no bound -->
<!-- cases: N/A 4 boundaries: no bound exists to be at -->
<!-- cases: 5 errors: the expired-deadline row keeps today's "start aborted" error, which does not match ErrStartTimeout; today a Starter whose Start returned nil is recorded as started even when the run then fails its context check, and this change keeps that -->
<!-- cases: N/A 6 duplicates: not applicable -->
<!-- cases: N/A 7 lifecycle: Shutdown during a run is covered by "Shutdown cancels a running Start" -->
<!-- cases: 8 concurrency: synctest makes the 1h wait virtual -->
<!-- cases: N/A 9 panics: covered by "Start errors and panics" -->
<!-- cases: 10 compatibility: this requirement pins the unchanged default -->

### Requirement: A start timeout abandons a stuck step
With `InjectorOpts.StartTimeout` positive, a Starter `Start` method or kind-handler call still running after that duration on the container clock SHALL be abandoned: the run SHALL stop waiting for it, start nothing further, and fail with a `*StartError` naming the step whose cause matches `ErrStartTimeout` and `context.DeadlineExceeded`. The step's context SHALL be cancelled. The timeout SHALL be decided once: a step that returned before its timer fired SHALL NOT be abandoned and its context SHALL NOT be cancelled by the timer; a step whose timer fired first SHALL be abandoned, even if it returns before the run notices.

#### Scenario: A step that outlives the start timeout is abandoned
```gherkin
Given a root scope created with InjectorOpts{StartTimeout: 5s}, inside synctest.Test
And <step> that ignores its context, sleeps <runs> of virtual time and then returns nil
And lazy Starter "later", opted in with AsStarter(), registered after it and depending on nothing
When the consumer calls Start()
Then Start returns after <returns> of virtual time with <result>
And errors.As(err, &startErr) gives Service "srv", Scope name "[root]", Op "<op>" and Kind "<kind>"
And "later" was started: <later>
And the test waits for the step to return before it ends

Examples:
  | step                                                       | runs | returns | result                                                                                                                                                                  | op     | kind     | later |
  | lazy Starter "srv" whose Start(ctx) error                  | 4s   | 4s      | nil                                                                                                                                                                     | -      | -        | true  |
  | lazy Starter "srv" whose Start(ctx) error                  | 6s   | 5s      | an error matching ErrStartTimeout and context.DeadlineExceeded, with message "doger: failed to start service `srv` in scope `[root]`: start timeout after 5s: context deadline exceeded" | start  |          | false |
  | lazy Starter "srv" whose Start() (no context, no error)    | 6s   | 5s      | an error matching ErrStartTimeout                                                                                                                                       | start  |          | false |
  | lazy "srv" opted in with AsRunnable() whose Run(ctx) error | 6s   | 5s      | an error matching ErrStartTimeout, with message "doger: kind `runnable` failed for service `srv` in scope `[root]`: start timeout after 5s: context deadline exceeded"  | handle | runnable | true  |
```

#### Scenario: The abandoned step's context is cancelled
```gherkin
Given a root scope created with InjectorOpts{StartTimeout: 5s}, inside synctest.Test
And lazy Starter "srv" whose Start(ctx) error keeps ctx and blocks on a channel closed by the test
When the consumer calls Start()
Then Start returns an error matching ErrStartTimeout
And the kept ctx's Err() is not nil
```

#### Scenario: A step that returns in time keeps its context until the run ends
```gherkin
Given a root scope created with InjectorOpts{StartTimeout: <timeout>}, inside synctest.Test
And lazy Starter "a" whose Start(ctx) error keeps ctx and returns nil at once
And lazy Starter "b", registered after "a", whose Start records the Err() of "a"'s kept ctx and whether it has a deadline
When the consumer calls Start()
Then Start returns nil
And "b" recorded Err() nil and no deadline
And once Start returned, the Err() of "a"'s kept ctx is context.Canceled

Examples:
  | timeout |
  | 0       |
  | 5s      |
```

#### Scenario: A step that returns exactly at the timeout
```gherkin
Given a root scope created with InjectorOpts{StartTimeout: 5s}, inside synctest.Test
And lazy Starter "a" whose Start(ctx) error keeps ctx, sleeps exactly 5s of virtual time and returns nil
And lazy Starter "b", registered after "a", whose Start records the Err() of "a"'s kept ctx
When the consumer calls Start(), in each of 200 runs
Then each Start either returns nil, with "b" having recorded Err() nil
Or returns an error matching ErrStartTimeout, with "a" recorded as started once its late return was handled
And no Start returns nil while "a"'s kept ctx was cancelled by the timeout
```

#### Scenario: A timeout stays a timeout when the run's context ends right after it
```gherkin
Given a root scope created with InjectorOpts{StartTimeout: 5s}, inside synctest.Test
And lazy Starter "srv" whose Start(ctx) error ignores ctx and blocks until the test releases it
And the consumer's ctx is cancelled after the 5s timeout fired and before the run reports the abandoned step (pinned by the container's test seam)
When the consumer calls StartWithContext(ctx)
Then err.Error() is "doger: failed to start service `srv` in scope `[root]`: start timeout after 5s: context deadline exceeded (start aborted: context canceled)"
And errors.Is(err, ErrStartTimeout), errors.Is(err, context.DeadlineExceeded) and errors.Is(err, context.Canceled) are true
```

#### Scenario: A step that ends with runtime.Goexit before the timeout
```gherkin
Given a root scope created with InjectorOpts{StartTimeout: 5s}, inside synctest.Test
And lazy Starter "srv" whose Start(ctx) error calls runtime.Goexit()
When the consumer calls Start()
Then Start returns at once with a *StartError for "srv" with Op "start", which does not match ErrStartTimeout
And no goroutine of the run is left blocked
```

#### Scenario: Start timeout waits for the container clock
```gherkin
Given a root scope created with InjectorOpts{StartTimeout: 10ms, Clock: <clock>}
And lazy Starter "srv" whose Start(ctx) error ignores ctx and blocks until the test releases it
When the consumer calls Start() in another goroutine and 50ms of wall time pass without advancing the clock
Then Start has not returned
And after the clock advances by 10ms Start returns an error matching ErrStartTimeout and context.DeadlineExceeded

Examples:
  | clock                                          |
  | dogerclockwork.Wrap(clockwork.NewFakeClock())  |
  | a consumer-written doger.Clock (manualClock)   |
```

#### Scenario: Abandonment under parallel start
```gherkin
Given a root scope created with InjectorOpts{StartTimeout: 5s, StartParallelism: 4}, inside synctest.Test
And independent lazy Starters "stuck" (Start blocks until released), "b" and "c" (Start returns nil at once)
And lazy Starter "d" registered with DependsOnNamed("stuck")
When the consumer calls Start()
Then Start returns after 5s of virtual time with an error naming service "stuck" and matching ErrStartTimeout
And "b" and "c" were started, "d" was not
And no goroutine of the run is still blocked once the test releases "stuck"
```

<!-- cases: 1 happy path: the 4s row, a step that finishes inside the bound -->
<!-- cases: 2 overrides: StartParallelism combined in "Abandonment under parallel start"; a ctx deadline shorter than StartTimeout is "With a start timeout, an ended run context waits for the step up to the timeout"; Clock in "Start timeout waits for the container clock" -->
<!-- cases: N/A 3 zero values: 0 and negative are "Start waits for every step by default" -->
<!-- cases: 4 boundaries: 4s (inside) and 6s (past). A step returning exactly at 5s races the timer: either it is started with a live context or it is abandoned, never both, pinned by "A step that returns exactly at the timeout". Under parallel start the run returns at most StartTimeout after its first failure, since running Starters are awaited, each within its own bound -->
<!-- cases: 5 errors: StartError fields, ErrStartTimeout, context.DeadlineExceeded, both message shapes; Goexit is an ordinary step failure -->
<!-- cases: N/A 6 duplicates: re-runs are "An abandoned step is not started again while it runs" -->
<!-- cases: 7 lifecycle: Refresh and Scope.Start share the pipeline; "later" not started; dependents not started -->
<!-- cases: 8 concurrency: parallel scenario; goleak in TestMain checks the released goroutines; the context lifetime of a step that returns in time; a run context ending right after the timeout fired keeps the timeout shape -->
<!-- cases: 10b compatibility: with StartTimeout set, a Starter's context lives exactly as long as without it unless the step is abandoned ("A step that returns in time keeps its context until the run ends") -->
<!-- cases: 9 panics: a panic before the timeout is recovered as today; after it, see container-logging "An abandoned start step that ends is logged"; Goexit scenario -->
<!-- cases: 10 compatibility: opt-in only -->

### Requirement: With a start timeout, an ended run context waits for the step up to the timeout
With `InjectorOpts.StartTimeout` positive, when the run's context ends while a Starter `Start` method or kind-handler call is running (caller cancellation or deadline, or root Shutdown), the step's context SHALL be cancelled at once, and the run SHALL keep waiting until the step returns or its `StartTimeout` elapses. A step that returns SHALL be handled as without the option, and the run then stops with the context's cause. A step still running when its `StartTimeout` elapses SHALL be abandoned as in "A start timeout abandons a stuck step", and its error SHALL carry the context's cause once, after the timeout.

#### Scenario: An ended run context waits for a step that ignores it
```gherkin
Given a root scope created with InjectorOpts{StartTimeout: 5s}, inside synctest.Test
And lazy Starter "srv", with a Shutdown() error method, whose Start(ctx) error ignores ctx and blocks until its Shutdown is called
When <trigger> while "srv" is starting
Then StartWithContext returns after 5s of virtual time
And errors.As(err, &startErr) gives Service "srv" and Op "start"
And errors.Is(err, ErrStartTimeout), errors.Is(err, context.DeadlineExceeded) and errors.Is(err, <cause>) are true
And err.Error() is "<message>"

Examples:
  | trigger                                          | cause                    | message                                                                                                                                              |
  | the consumer's ctx has a 100ms deadline          | context.DeadlineExceeded | doger: failed to start service `srv` in scope `[root]`: start timeout after 5s: context deadline exceeded (start aborted: context deadline exceeded) |
  | the consumer cancels ctx at 100ms                | context.Canceled         | doger: failed to start service `srv` in scope `[root]`: start timeout after 5s: context deadline exceeded (start aborted: context canceled)          |
  | another goroutine calls root.Shutdown() at 100ms | ErrShutdown              | doger: failed to start service `srv` in scope `[root]`: start timeout after 5s: context deadline exceeded (start aborted: container is shut down)    |
```

#### Scenario: A Start that honours its context is awaited when Shutdown cancels it
```gherkin
Given a root scope created with InjectorOpts{StartTimeout: 5s}, inside synctest.Test
And lazy Starter "srv" whose Start(ctx) error waits for ctx.Done(), then cleans up for 100ms of virtual time and returns ctx.Err()
And whose Shutdown() error method records whether that cleanup had finished
When another goroutine calls root.Shutdown() at 100ms of virtual time
Then StartWithContext returns after 200ms of virtual time with an error matching ErrShutdown and context.Canceled, and not ErrStartTimeout
And "srv"'s Shutdown method was called exactly once, after its Start cleanup finished
```

#### Scenario: Shutdown during a stuck step shuts the service down
```gherkin
Given a root scope created with InjectorOpts{StartTimeout: 5s}, inside synctest.Test
And lazy Starter "srv" whose Start(ctx) error ignores ctx and blocks until its Shutdown() error method is called
When another goroutine calls root.ShutdownWithContext(ctx with a 10s deadline) at 100ms of virtual time
Then Start returns after 5s of virtual time with an error matching ErrShutdown and ErrStartTimeout
And Shutdown called "srv"'s Shutdown method exactly once
And Shutdown returns a report with no error before its deadline
And "srv" is not recorded as started once its Start call returned
```

#### Scenario: A Shutdown bounded below the start timeout
```gherkin
Given a root scope created with InjectorOpts{StartTimeout: 5s}, inside synctest.Test
And the consumer has <before>
And lazy Starter "srv" in <scope> whose Start(ctx) error ignores ctx and blocks until its Shutdown() error method is called
And <run> is running in another goroutine
When another goroutine calls root.ShutdownWithContext(ctx with a 1s deadline) at 100ms of virtual time
Then Shutdown returns after 1s with a failed report
And the report has an error keyed by the root scope with an empty service name, matching context.DeadlineExceeded
And the report has an error for "srv", matching context.DeadlineExceeded, and "srv"'s Shutdown method was not called
And <run> returns after 5s of virtual time with a *StartError naming "srv" in <scope>, matching ErrStartTimeout and ErrShutdown, whose message is <message>
And "srv" is not recorded as started once the test released its Start call

Examples:
  | before                                     | scope            | run            | message                                                                                                                                 |
  | done nothing                               | the root         | root.Start()   | doger: failed to start service `srv` in scope `[root]`: start timeout after 5s: context deadline exceeded (start aborted: container is shut down) |
  | called Start(), then created scope "tenant" | "tenant"         | tenant.Start() | doger: failed to start service `srv` in scope `tenant`: start timeout after 5s: context deadline exceeded (start aborted: container is shut down) |
  | called Start(), then created scope "tenant" | "tenant"         | root.Refresh() | doger: failed to start service `srv` in scope `tenant`: start timeout after 5s: context deadline exceeded (start aborted: container is shut down) |
```

#### Scenario: A bounded root Shutdown that reaches the scope before the step begins
```gherkin
Given a root scope created with InjectorOpts{StartTimeout: 5s}, inside synctest.Test, with a HookBeforeStart that blocks until <gate>
And the consumer has <before>
And lazy Starter "srv" in <scope> whose Start(ctx) error ignores ctx and blocks until its Shutdown() error method is called
And <run> is running in another goroutine, blocked in that HookBeforeStart for "srv"
When another goroutine calls root.ShutdownWithContext(ctx with a 1s deadline) at 100ms of virtual time
Then "srv"'s Start is called once, with a context already cancelled with a cause matching ErrShutdown
And <run> returns a *StartError naming "srv" in <scope>, matching ErrStartTimeout and ErrShutdown, whose message is <message>

Examples:
  | before                                     | scope    | run            | gate                                                                         | message                                                                                                                                 |
  | done nothing                               | the root | root.Start()   | root.ShutdownWithContext returned                                            | doger: failed to start service `srv` in scope `[root]`: start timeout after 5s: context deadline exceeded (start aborted: container is shut down) |
  | called Start(), then created scope "tenant" | "tenant" | tenant.Start() | a HookScopeClosing for "tenant" ran, which then keeps "tenant" closing for 1s | doger: failed to start service `srv` in scope `tenant`: start timeout after 5s: context deadline exceeded (start aborted: container is shut down) |
  | called Start(), then created scope "tenant" | "tenant" | root.Refresh() | a HookScopeClosing for "tenant" ran, which then keeps "tenant" closing for 1s | doger: failed to start service `srv` in scope `tenant`: start timeout after 5s: context deadline exceeded (start aborted: container is shut down) |
```

<!-- cases: N/A 1 happy path: the requirement is an error path -->
<!-- cases: 2 overrides: deadline, cancellation and Shutdown triggers; a Start that honours its context -->
<!-- cases: N/A 3 zero values: a nil ctx is context.Background(), which never ends -->
<!-- cases: 4 boundaries: the run returns at the step's StartTimeout at the latest, however early the context ended; a step that returns earlier ends the wait -->
<!-- cases: 5 errors: each cause row, with the exact message; the context's cause appears once, after the timeout, as "An aborted run matches ErrShutdown when the caller cancelled first" requires -->
<!-- cases: N/A 6 duplicates: not applicable -->
<!-- cases: 7 lifecycle: root Shutdown waits for the run, bounded by its own context, before it shuts services down, so a Start that honours its context never overlaps its service's Shutdowner. A Shutdown bounded below StartTimeout stops waiting when its context ends and proceeds with that ended context, skipping the services it reaches as shutdown-context specifies, exactly as without the option ("A Shutdown bounded below the start timeout"); the stuck step keeps running until its call returns, and StartTimeout's godoc states it. The late nil return after removal is not recorded. A root Shutdown also marks the child scopes it shuts down as closing, but it cancelled the whole run first: a step of a child scope it reaches, running or about to begin, is cancelled with the run and reported like a root step, a *StartError naming it with the same message apart from the scope name, never skipped as a child scope's own Shutdown skips it (tenant.Start() and root.Refresh() rows of "A Shutdown bounded below the start timeout" and "A bounded root Shutdown that reaches the scope before the step begins"; release triage A and E, decisions I1 and I3) -->
<!-- cases: 8 concurrency: Shutdown from another goroutine; synctest -->
<!-- cases: N/A 9 panics: as in "A start timeout abandons a stuck step" -->
<!-- cases: 10 compatibility: without StartTimeout an ended context still waits without bound, see "Start waits for every step by default" -->

### Requirement: An abandoned step is not started again while it runs
While an abandoned Starter `Start` call or kind-handler call runs, its service or component SHALL NOT be recorded as started or handled, and no run SHALL call it again: a run SHALL skip that step, without hook calls, and skip every candidate that depends on its service, directly or through others, whether that service lives in the run's scopes or in an ancestor scope, start everything else, and then fail with a `*StartError` naming the step that matches `ErrStartTimeout`. When another step of the run fails, that failure SHALL be the run's error instead. The guarantee SHALL hold per instance: a step whose instance (when it is a non-nil pointer to a non-zero-size value, a non-nil channel or a non-nil unsafe pointer) has an abandoned call still running SHALL be skipped the same way, with its dependents in any scope or stage, in this container and in its clones, also after an override registers the same instance. Any other instance (a struct or other value, a map, a nil pointer, a pointer to a zero-size type) is tracked per registration only, so two registrations never block each other through it. A default registration that yielded holds no instance of its own: its dependents wait for its target's. Once the call returns nil it SHALL be recorded, unless its scope is shutting down (by its own, an ancestor's or root Shutdown) or no longer holds it; once it errs or panics, the next run SHALL try it again. When root Start is the run that abandoned the step, Start has failed, so no later run follows: Refresh and Scope.Start return `ErrNotStarted` and Start returns `ErrAlreadyStarted`; the late outcome is still recorded or logged, and Shutdown is the way on.

#### Scenario: Re-running while the abandoned call runs, and after it returns
```gherkin
Given a started root container created with InjectorOpts{StartTimeout: 5s}, inside synctest.Test
And <step> "late", registered after Start, whose first call blocks until the test releases it and then <late-outcome>, and whose later calls return nil
And the consumer called <operation>, which returned an error matching ErrStartTimeout
When the consumer calls <operation> <when>
Then that call returns <result>
And "late"'s <method> was called <calls> times in total

Examples:
  | step                          | method | operation      | late-outcome    | when                                                 | result                                          | calls |
  | lazy Starter                  | Start  | root.Refresh() | returns nil     | before the test releases "late"                      | an error naming "late" matching ErrStartTimeout | 1     |
  | lazy Starter                  | Start  | root.Refresh() | returns nil     | after the test released "late" and its call returned | nil                                             | 1     |
  | lazy Starter                  | Start  | root.Refresh() | returns errBoom | after the test released "late" and its call returned | nil                                             | 2     |
  | lazy Starter                  | Start  | root.Refresh() | panics          | after the test released "late" and its call returned | nil                                             | 2     |
  | lazy Starter in child "tenant", created after Start | Start | tenant.Start() | returns nil | before the test releases "late"           | an error naming "late" matching ErrStartTimeout | 1     |
  | lazy "late" opted in with AsRunnable() | Run | root.Refresh() | returns nil     | before the test releases "late"                      | an error naming "late" matching ErrStartTimeout | 1     |
  | lazy "late" opted in with AsRunnable() | Run | root.Refresh() | returns nil     | after the test released "late" and its call returned | nil                                             | 1     |
  | lazy "late" opted in with AsRunnable() | Run | root.Refresh() | returns errBoom | after the test released "late" and its call returned | nil                                             | 2     |
  | lazy Starter "late" that is also a component of a StageBeforeStart kind whose handler calls its Run; the handler call blocks | Start | root.Refresh() | returns nil | before the test releases "late" | an error naming "late" matching ErrStartTimeout | 0 (its handler: 1) |
```

<!-- The last row pins D5's rule that a service with a skipped step has its later steps in the run skipped: its abandoned StageBeforeStart handler call still runs, so the second run skips that call and then the Starter's Start too, with no hook call. -->

#### Scenario: Refresh starts what does not depend on a still-running step
```gherkin
Given a started root container created with InjectorOpts{StartTimeout: 5s, StartParallelism: <parallelism>}, inside synctest.Test
And <kind> "stuck" in <scope>, registered after Start, whose first call blocks until the test releases it
And <operation> returned an error naming "stuck" matching ErrStartTimeout
And then, in the same scope, <kind> "worker" (depending on nothing) and <kind> "dep" (registered with DependsOnNamed("stuck")) were registered
When the consumer calls <operation> before the test releases "stuck"
Then it returns an error naming "stuck" that matches ErrStartTimeout
And "worker" was started and "dep" was not
And "stuck" was called 1 time in total

Examples:
  | parallelism | kind                         | scope                                     | operation      |
  | 0           | lazy Starter                 | the root                                  | root.Refresh() |
  | 4           | lazy Starter                 | the root                                  | root.Refresh() |
  | 0           | lazy Starter                 | child scope "tenant", created after Start | tenant.Start() |
  | 4           | lazy Starter                 | child scope "tenant", created after Start | tenant.Start() |
  | 0           | lazy Starter                 | child scope "tenant", created after Start | root.Refresh() |
  | 0           | lazy service with AsRunnable() | the root                                | root.Refresh() |
  | 0           | lazy service with AsRunnable() | child scope "tenant", created after Start | tenant.Start() |
```

#### Scenario: A dependent of a still-running step in an ancestor scope is skipped
```gherkin
Given a started root container created with InjectorOpts{StartTimeout: 5s}, inside synctest.Test
And lazy Starter "stuck" in the root, registered after Start, whose first Start call blocks until the test releases it
And root.Refresh() returned an error naming "stuck" matching ErrStartTimeout
And then child scope "tenant" with lazy Starters "worker" (depending on nothing) and "dep", whose provider depends on "stuck" by <declaration>
When the consumer calls <operation> before the test releases "stuck"
Then it returns an error naming "stuck" that matches ErrStartTimeout
And "worker" was started and "dep" was not
And once the test released "stuck" and its call returned, tenant.Start() returns nil and "dep" was started

Examples:
  | declaration                        | operation      |
  | DependsOnNamed("stuck")            | tenant.Start() |
  | invoking it in its provider        | tenant.Start() |
  | DependsOnNamed("stuck")            | root.Refresh() |
```

#### Scenario: A still-running instance is not called again through an override or a clone
```gherkin
Given a root container created with InjectorOpts{StartTimeout: 5s}, inside synctest.Test
And a Starter instance "inst" (a pointer) whose first Start call blocks until the test releases it
And "inst" is registered as eager value "srv" and <first> returned an error matching ErrStartTimeout
When <second> before the test releases "inst"
Then <second-op> returns an error naming "srv" that matches ErrStartTimeout
And "inst"'s Start was called 1 time in total

Examples:
  | first          | second                                                                              | second-op     |
  | root.Start()   | the consumer calls clone := root.Clone() and clone.Start()                          | clone.Start() |
  | Start(), then root.Refresh() for "srv" registered after Start | the consumer overrides "srv" with OverrideNamedValue(root, "srv", inst) and calls root.Refresh() | root.Refresh() |
```

#### Scenario: An instance two containers abandoned stays tracked until both calls return
```gherkin
Given a root container created with InjectorOpts{StartTimeout: 5s}, inside synctest.Test
And a Starter instance "inst" (a pointer) registered as eager value "srv", whose every Start call blocks until the test releases it
And clones of root taken before any Start
And root.Start() and one clone's Start(), called at the same time, each called "inst"'s Start and abandoned it
When the test releases <released> and another clone, not started yet, calls Start()
Then that Start() returns <result>
And "inst"'s Start was called <calls> times in total

Examples:
  | released                 | result                                          | calls |
  | one of the two calls     | an error naming "srv" matching ErrStartTimeout  | 2     |
  | both calls               | nil                                             | 3     |
```

#### Scenario: The dependents of a still-running instance are skipped in any scope or stage
```gherkin
Given a root container created with InjectorOpts{StartTimeout: 5s}, inside synctest.Test
And a Starter instance "inst" (a pointer) whose first Start call blocks until the test releases it
And "dep" is <dependent>, which depends on "srv" with DependsOnNamed("srv") and returns at once
And "inst" is registered as eager value "srv" and <first> returned an error matching ErrStartTimeout
When <second> before the test releases "inst"
Then <second-op> returns an error naming "srv" that matches ErrStartTimeout
And "inst"'s Start was called 1 time in total
And "dep" was called <dep-calls> times in total

Examples:
  | dependent                                                                    | first                                                         | second                                                                                                                   | second-op      | dep-calls |
  | lazy Starter "w" in child scope "tenant", registered by <second>             | Start(), then root.Refresh() for "srv" registered after Start | the consumer overrides "srv" with OverrideNamedValue(root, "srv", inst), registers "w" and calls tenant.Start()        | tenant.Start() | 0         |
  | a lazy component of a StageBeforeStart kind in the root, handled by root.Start() before it abandoned "srv" | root.Start()                                                  | the consumer calls clone := root.Clone() and clone.Start(), which reaches "dep"'s stage before it claims "srv"       | clone.Start()  | 1         |
```

#### Scenario: An instance that is not one shared object is tracked per registration only
```gherkin
Given a started root container created with InjectorOpts{StartTimeout: 5s}, inside synctest.Test
And a shared stepProbe whose first call blocks until the test releases it and whose later calls return nil
And eager Starter value "a" holding <instance-a>, registered after Start, whose Start calls the probe
And root.Refresh() returned an error matching ErrStartTimeout, abandoning "a"
When <second> before the test releases the probe
Then <second-op> returns <result>
And the probe was called <calls> times in total
And no call panics

Examples:
  | instance-a                                              | second                                                                       | second-op      | result                                       | calls |
  | a struct value startOnly{probe}                         | the consumer registers "b" holding an equal startOnly{probe} and calls root.Refresh() | root.Refresh() | an error naming "a" matching ErrStartTimeout | 2     |
  | &zeroStarter{}, a pointer to a zero-size type          | the consumer registers "b" holding another &zeroStarter{}, whatever its address, and calls root.Refresh() | root.Refresh() | an error naming "a" matching ErrStartTimeout | 2     |
  | (*nilStarter)(nil), a typed nil pointer                 | the consumer registers "b" holding (*nilStarter)(nil) and calls root.Refresh() | root.Refresh() | an error naming "a" matching ErrStartTimeout | 2     |
  | a struct value holding a slice in an interface field    | the consumer overrides "a" with the same value and calls root.Refresh()      | root.Refresh() | nil                                          | 2     |
  | a map-kind Starter                                      | the consumer overrides "a" with the same map and calls root.Refresh()        | root.Refresh() | nil                                          | 2     |
```

#### Scenario: A yielded default holds no instance of its own
```gherkin
Given a root container created with InjectorOpts{StartTimeout: 5s}, inside synctest.Test, and clone := root.Clone() taken while empty
And root "srv" is eager value x, a *stepProbe that is not a Starter in the root
And child scope "tenant" registers "srv" with ProvideNamedValue(tenant, "srv", p, AsDefault()), which yields to root "srv"
And tenant lazy Starter "w" depends on "srv" with DependsOnNamed("srv")
And the clone registers <abandoned> as Starter "q", and clone.Start() abandoned it
When the consumer calls root.Start() before the test releases <abandoned>
Then root.Start() returns <result>
And "w" was called <w-calls> times

Examples:
  | abandoned | result                                                              | w-calls |
  | p         | nil                                                                 | 1       |
  | x         | an error naming "srv" in the root scope that matches ErrStartTimeout | 0       |
```

#### Scenario: A late return during a shutdown is not recorded
```gherkin
Given a started root container created with InjectorOpts{StartTimeout: 5s}, inside synctest.Test
And <kind> "srv" in <scope>, registered after Start and abandoned by <operation>, whose first call blocks until the test releases it, then returns nil
And service "gate" in the same scope depends on "srv" and its Shutdown() error blocks until the test releases it
When the consumer calls <shutdown> in another goroutine, and the test releases "srv" while that shutdown is blocked in "gate"'s Shutdown
Then "srv" is not recorded as started or handled
And once "gate" is released, <shutdown> returns

Examples:
  | kind                           | scope                                     | operation      | shutdown          |
  | lazy Starter                   | the root                                  | root.Refresh() | root.Shutdown()   |
  | lazy service with AsRunnable() | the root                                  | root.Refresh() | root.Shutdown()   |
  | lazy Starter                   | child scope "tenant", created after Start | tenant.Start() | tenant.Shutdown() |
  | lazy Starter                   | child scope "tenant", created after Start | tenant.Start() | root.Shutdown()   |
```

#### Scenario: After root Start abandons a step, only Shutdown follows
```gherkin
Given a root scope created with InjectorOpts{StartTimeout: 10ms}, inside synctest.Test
And lazy Starter "srv" whose Start sleeps 50ms of virtual time and returns nil
When the consumer calls Start(), which returns an error matching ErrStartTimeout, and 1s of virtual time passes
Then "srv" is recorded as started
And root.Refresh() returns an error matching ErrNotStarted
And root.Start() returns an error matching ErrAlreadyStarted
And root.Shutdown() returns a report with no error
```

#### Scenario: A late return races a covering run
```gherkin
Given a started root container created with InjectorOpts{StartTimeout: 5s}, inside synctest.Test
And lazy Starter "late", registered after Start, whose abandoned first Start call is released by the test while a second root.Refresh() is between discovering "late" and starting it (pinned with a HookBeforeStart of another service that waits on a channel)
When the second Refresh continues
Then "late"'s Start was called exactly 1 time
And the second Refresh returns nil
```

#### Scenario: A late return after the scope began shutting down is not recorded
```gherkin
Given a started root container created with InjectorOpts{StartTimeout: 5s}, inside synctest.Test
And child scope "tenant", created after Start, with lazy Starter "late" whose Start blocks until released, then returns nil
And tenant.Start() returned an error matching ErrStartTimeout
When the consumer calls tenant.Shutdown(), and only then the test releases "late" and waits for its call to return
Then tenant.Shutdown() returns without waiting for "late"'s Start call
And the start bookkeeping holds no entry for "late"
```

<!-- cases: N/A 1 happy path: requirement concerns a failure follow-up -->
<!-- cases: N/A 2 overrides: StartTimeout must be set for abandonment to exist -->
<!-- cases: N/A 3 zero values: not applicable -->
<!-- cases: 4 boundaries: before and after the late return -->
<!-- cases: 5 errors: the still-running StartError, returned after the rest of the run; late error and late panic rows; after root Start abandoned a step, ErrNotStarted and ErrAlreadyStarted -->
<!-- cases: 6 duplicates: a second call never overlaps the first, for Starters and for the built-in Runnable; the race scenario pins the discovery-to-start window; the skipped step's dependents are skipped, independent services start, sequentially and in parallel -->
<!-- cases: 7 lifecycle: Refresh and Scope.Start rows; a child scope shutting down before the late return. A child scope Shutdown waits for a running run ("Child scope shutdown waits for a start that is starting its services"), and an abandoned run has returned, so it does not wait for the abandoned call -->
<!-- cases: 8 concurrency: the late return races the next run; the rows pin it with release channels; runs under -race with goleak -->
<!-- cases: 9 panics: the late panic row -->
<!-- cases: 10 compatibility: a failed (not abandoned) Starter or handler is still retried as in "Refresh starts what is not started yet" and component-kinds "Handlers run for new components on incremental start". The guarantee is per instance: a clone shares eager instances and otherwise starts them independently, but never calls an instance whose abandoned call, made by its source or by another clone, still runs; nor does an override that registers the same instance, and the dependents of such an instance are skipped in any scope or stage ("The dependents of a still-running instance are skipped in any scope or stage"). Only a non-nil pointer to a non-zero-size value, a non-nil channel or a non-nil unsafe pointer is tracked by instance; a value, a map, a nil pointer and a pointer to a zero-size type are tracked per registration only, and a yielded default holds no instance ("An instance that is not one shared object is tracked per registration only", "A yielded default holds no instance of its own") -->
<!-- cases: 11 cross-scope: a dependent of a still-running step in an ancestor scope is skipped by Scope.Start as by Refresh; an ancestor that merely failed or never started keeps the rule that Scope.Start does not start ancestors -->
<!-- cases: 12 shutdown: a late nil return during root Shutdown is not recorded, as during a child scope shutdown ("A late return during a shutdown is not recorded") -->

### Requirement: Start hooks for an abandoned step
For an abandoned Starter, `HookAfterStart` SHALL be called exactly once, on the run's goroutine when the step is abandoned, with `elapsed` equal to the time waited on the container clock and an `err` matching `ErrStartTimeout`. It SHALL NOT be called again when the abandoned call returns.

#### Scenario: HookAfterStart fires once at abandonment
```gherkin
Given a root scope created with InjectorOpts{StartTimeout: 5s} and HookBeforeStart and HookAfterStart recording calls, inside synctest.Test
And lazy Starter "srv" whose Start blocks until the test releases it
When the consumer calls Start() and then the test releases "srv" and waits for its call to return
Then the recorded calls are exactly: before srv, after srv elapsed=5s err matching ErrStartTimeout
```

<!-- cases: N/A 2 overrides, 3 zero values, 4 boundaries: one hook point, one shape -->
<!-- cases: 5 errors: the err matches ErrStartTimeout -->
<!-- cases: 7 lifecycle: no second after-call on the late return -->
<!-- cases: N/A 8 concurrency: covered by "Abandonment under parallel start" -->
<!-- cases: N/A 9 panics: hook panics behave as in "Per-service start hooks" -->
<!-- cases: 10 compatibility: before/after calls still pair one to one -->

### Requirement: A service removed while its Start returns leaves no start record
When a Starter's `Start` returns nil, in time or after being abandoned, while its service is being removed from its scope (`ShutdownNamed`, a scope or root Shutdown, or an override), the container SHALL NOT keep a start record for the removed service, whichever of the removal and the record comes first.

#### Scenario: A removal between the registration check and the record
```gherkin
Given a started root container created with InjectorOpts{StartTimeout: <timeout>}, inside synctest.Test
And lazy Starter "srv", registered after Start, whose Start returns nil <when>
And the container's test seam runs doger.ShutdownNamed(root, "srv") after the run checked that the root still holds "srv" and before it records the start
When the consumer calls root.Refresh()
Then ShutdownNamed returns nil
And the start bookkeeping holds no entry for the removed "srv"

Examples:
  | timeout | when                                                      |
  | 0       | at once                                                   |
  | 5s      | at once                                                   |
  | 5s      | after the test releases it, once Refresh returned ErrStartTimeout |
```

<!-- cases: N/A 1 happy path: the requirement covers an interleaving -->
<!-- cases: N/A 2 overrides: StartTimeout rows only select the path (inline, awaited, abandoned) -->
<!-- cases: N/A 3 zero values: not applicable -->
<!-- cases: 4 boundaries: the removal lands exactly inside the window, pinned by the seam -->
<!-- cases: N/A 5 errors: no error is returned; the defect is a stale record -->
<!-- cases: 6 duplicates: no record survives for a removed wrapper -->
<!-- cases: 7 lifecycle: the inline, awaited and abandoned paths -->
<!-- cases: 8 concurrency: the subject; the seam makes the interleaving deterministic; runs under -race -->
<!-- cases: N/A 9 panics: not applicable -->
<!-- cases: 10 compatibility: BEHAVIOR FIX (flagged in the proposal's Defect section) - the inline path had the same window before this change -->
