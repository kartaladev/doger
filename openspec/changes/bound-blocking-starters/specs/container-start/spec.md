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

<!-- cases: 2 overrides: with StartTimeout unset, a Starter that ignores an ended context is still awaited; InjectorOpts.StartTimeout makes the run stop waiting for it, see "A start timeout abandons a stuck step" and "With a start timeout, an ended run context abandons a stuck step" -->
<!-- cases: 3 zero values: nil context is covered by "Nil context" -->
<!-- cases: 8 concurrency: deadline scenarios run under testing/synctest, so time is virtual and no goroutine leaks -->
<!-- cases: N/A 9 panics: covered by "Start errors and panics" -->
<!-- cases: N/A 10 compatibility: new API -->

### Requirement: Per-service start hooks
For each Starter, `Start` SHALL call every `HookBeforeStart(ctx, scope, name)` just before its `Start` method and every `HookAfterStart(ctx, scope, name, elapsed, err)` just after, where `ctx` is the context the run passes to its steps, `elapsed` is the time the method took on the container clock, and `err` is the error the method returned, unwrapped (nil on success, the recovered panic as an error). With `InjectorOpts.StartTimeout` set, the Starter receives a context derived from that `ctx` that is also cancelled when the step is abandoned and reports no deadline, and an abandoned Starter's `HookAfterStart` call follows "Start hooks for an abandoned step". Hooks SHALL run in the order they were added. `AddBeforeStartHook`/`AddAfterStartHook` SHALL append to the same lists.

#### Scenario: Hooks around each Starter
```gherkin
Given a root scope with HookBeforeStart and HookAfterStart recording calls
And a hook added later with AddAfterStartHook recording "late-after"
And Starters "a" (succeeds) then "b" (returns errBoom), both opted in with AsStarter()
When the consumer calls Start()
Then the recorded calls are: before a, after a nil, late-after a, before b, after b errBoom, late-after b
And the err passed for "b" is errBoom itself, not a wrapping error
```

<!-- cases: N/A 3 zero values: no hooks registered means no calls (default; covered by every other scenario) -->
<!-- cases: N/A 4 boundaries: 0, 1 and many hooks follow the same slice iteration as the existing shutdown hooks -->
<!-- cases: 7 lifecycle: an abandoned Starter is covered by "Start hooks for an abandoned step"; a Starter skipped by "An abandoned step is not started again while it runs", because it is recorded or its abandoned call still runs, gets no hook call -->
<!-- cases: 9 panics: a panicking start hook propagates (start hooks are not recovered; shutdown hooks are, see container-shutdown); container state is still released, see "A panic escaping Start still releases Shutdown" -->
<!-- cases: N/A 8 concurrency: hooks run on the Start goroutine, also with StartTimeout set -->
<!-- cases: N/A 10 compatibility: new hooks; with StartTimeout unset the Starter receives the hook ctx itself, as before -->

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

<!-- cases: N/A 1 happy path: the requirement is an error path -->
<!-- cases: 2 overrides: deadline, cancellation and Shutdown triggers; a Start that honours its context -->
<!-- cases: N/A 3 zero values: a nil ctx is context.Background(), which never ends -->
<!-- cases: 4 boundaries: the run returns at the step's StartTimeout at the latest, however early the context ended; a step that returns earlier ends the wait -->
<!-- cases: 5 errors: each cause row, with the exact message; the context's cause appears once, after the timeout, as "An aborted run matches ErrShutdown when the caller cancelled first" requires -->
<!-- cases: N/A 6 duplicates: not applicable -->
<!-- cases: 7 lifecycle: root Shutdown waits for the run, bounded by its own context, before it shuts services down, so a Start that honours its context never overlaps its service's Shutdowner. A Shutdown bounded below StartTimeout stops waiting and can run the Shutdowner while the Start still runs; StartTimeout's godoc states it. The late nil return after removal is not recorded -->
<!-- cases: 8 concurrency: Shutdown from another goroutine; synctest -->
<!-- cases: N/A 9 panics: as in "A start timeout abandons a stuck step" -->
<!-- cases: 10 compatibility: without StartTimeout an ended context still waits without bound, see "Start waits for every step by default" -->

### Requirement: An abandoned step is not started again while it runs
While an abandoned Starter `Start` call or kind-handler call runs, its service or component SHALL NOT be recorded as started or handled, and a covering Refresh or Scope.Start SHALL NOT call it again: it SHALL skip that step, without hook calls, and skip the services that depend on its service, directly or through others, start everything else, and then fail with a `*StartError` naming the step that matches `ErrStartTimeout`. When another step of the run fails, that failure SHALL be the run's error instead. Once the call returns nil it SHALL be recorded, unless its scope is shutting down or no longer holds it; once it errs or panics, the next run SHALL try it again. When root Start is the run that abandoned the step, Start has failed, so no later run follows: Refresh and Scope.Start return `ErrNotStarted` and Start returns `ErrAlreadyStarted`; the late outcome is still recorded or logged, and Shutdown is the way on.

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
```

#### Scenario: Refresh starts what does not depend on a still-running step
```gherkin
Given a started root container created with InjectorOpts{StartTimeout: 5s, StartParallelism: <parallelism>}, inside synctest.Test
And lazy Starter "stuck", registered after Start, whose first Start call blocks until the test releases it
And root.Refresh() returned an error naming "stuck" matching ErrStartTimeout
And then lazy Starters "worker" (depending on nothing) and "dep" (registered with DependsOnNamed("stuck")) were registered
When the consumer calls root.Refresh() before the test releases "stuck"
Then Refresh returns an error naming "stuck" that matches ErrStartTimeout
And "worker" was started and "dep" was not
And "stuck"'s Start was called 1 time in total

Examples:
  | parallelism |
  | 0           |
  | 4           |
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
<!-- cases: 10 compatibility: a failed (not abandoned) Starter or handler is still retried as in "Refresh starts what is not started yet" and component-kinds "Handlers run for new components on incremental start". The guarantee is per container: a clone shares eager instances and starts them independently, so a clone's Start can overlap an abandoned call of its source -->

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
