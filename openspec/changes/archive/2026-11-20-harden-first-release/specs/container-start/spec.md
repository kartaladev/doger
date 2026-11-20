# Spec Delta

## RENAMED Requirements

- FROM: `### Requirement: A failure returned after Shutdown began matches ErrStartAfterShutdown`
- TO: `### Requirement: A failure returned after Shutdown began matches ErrShutdown`
- FROM: `### Requirement: An aborted run matches ErrStartAfterShutdown when the caller cancelled first`
- TO: `### Requirement: An aborted run matches ErrShutdown when the caller cancelled first`

## REMOVED Requirements

### Requirement: Starter auto-detection can be disabled
**Reason**: Auto-detection becomes opt-in, so `InjectorOpts.DisableStarterAutoDetection` is removed; common types with a `Start` method (`*exec.Cmd`, `*httptest.Server`, schedulers) were started by surprise.
**Migration**: See "Starter auto-detection is opt-in". Drop `DisableStarterAutoDetection: true` (now the default); containers that relied on auto-detection set `EnableStarterAutoDetection: true` or add `AsStarter()` to each service that must start.

## MODIFIED Requirements

### Requirement: Starter variants are detected by registered type
`Start` SHALL start every non-transient, non-alias service registered with `AsStarter()` and, only when `InjectorOpts.EnableStarterAutoDetection` is true, every non-transient, non-alias service whose registered type implements one of `Starter`, `StarterWithError`, `StarterWithContext` or `StarterWithContextAndError`. It SHALL call the variant's `Start` method exactly once. Unlike `Shutdowner*`, which is detected on the instance, detection uses the registered type.

#### Scenario: Each Starter variant is started once
```gherkin
Given a root scope created with EnableStarterAutoDetection = <auto>
And lazy "<service>" of a type whose method is <method>, registered with <options>
When the consumer calls StartWithContext(ctx)
Then the start method was called 1 time
And StartWithContext returns nil

Examples:
  | auto  | options           | service | method                          |
  | false | doger.AsStarter() | s1      | Start()                         |
  | false | doger.AsStarter() | s2      | Start() error                   |
  | false | doger.AsStarter() | s3      | Start(context.Context)          |
  | false | doger.AsStarter() | s4      | Start(context.Context) error    |
  | true  | no options        | s1      | Start()                         |
  | true  | no options        | s2      | Start() error                   |
  | true  | no options        | s3      | Start(context.Context)          |
  | true  | no options        | s4      | Start(context.Context) error    |
```

#### Scenario: Which registrations are started
```gherkin
Given a root scope created with EnableStarterAutoDetection = <auto>
And "<service>" registered as <registration>
And every instance also implements Shutdown() error
When the consumer calls Start() and then Shutdown()
Then the instance's Start was called <starts> times
And the instance's Shutdown was called <shutdowns> times

Examples:
  | auto  | service       | registration                                                                              | starts | shutdowns |
  | true  | *app.Server   | lazy, type has Start(ctx) error                                                           | 1      | 1         |
  | true  | *app.Server   | eager value, type has Start() error                                                       | 1      | 1         |
  | true  | app.Lifecycle | lazy registered as interface app.Lifecycle (no Start in interface), instance has Start()  | 0      | 0         |
  | true  | app.Lifecycle | same as above, plus AsStarter()                                                           | 1      | 1         |
  | true  | app.Lifecycle | same as above, plus doger.Invoke[app.Lifecycle] before Start()                            | 0      | 1         |
  | true  | *app.Server   | lazy, plus Alias[*app.Server, app.Startable] alias                                        | 1      | 1         |
  | true  | *app.Config   | lazy, type has no Start                                                                   | 0      | 0         |
  | true  | *app.Server   | transient, type has Start() error                                                         | 0      | 0         |
  | false | *app.Server   | lazy, type has Start(ctx) error                                                           | 0      | 0         |
  | false | *app.Server   | lazy, type has Start(ctx) error, plus AsStarter()                                         | 1      | 1         |
  | false | app.Lifecycle | lazy registered as interface app.Lifecycle, instance has Start(), plus AsStarter()        | 1      | 1         |
  | false | *app.Server   | lazy with AsStarter(), plus Alias[*app.Server, app.Startable] alias                       | 1      | 1         |
```

<!-- cases: 1 default: the auto = false rows are the zero-configuration behavior: only AsStarter() services start -->
<!-- cases: 2 overrides: EnableStarterAutoDetection is covered by "Starter auto-detection is opt-in" and the auto = true rows; AsStarter by the auto = false rows and the app.Lifecycle rows -->
<!-- cases: N/A 3 zero values: a container with no services is covered by "Start on an empty container" -->
<!-- cases: 6 duplicates: the alias row pins that a service is started once even when also reachable through an alias -->
<!-- cases: 9 panics: covered by "Start errors and panics" -->
<!-- cases: 10 compatibility: BREAKING (flagged) - a service whose type has a Start method is no longer started unless registered with AsStarter() or the container enables auto-detection; the "Invoke before Start" row pins that instance-based Shutdowner detection is unchanged while Starter detection is type-based -->

### Requirement: Start errors and panics
On the first failure, `Start` SHALL stop and return an error that names the service and scope and wraps the cause (`errors.Is`/`errors.As`). Failures are a build error, a `Start` error, a recovered panic in `Start`, or a kind-handler error. Starters not yet reached SHALL NOT be started, and nothing is rolled back. A panic that escapes `Start` SHALL still leave the container marked as started and SHALL NOT block a later `Shutdown`.

#### Scenario: A Starter failure stops the remaining Starters
```gherkin
Given a root scope with lazy Starters "a", "b", "c" registered in that order, with no dependencies
And "b" fails with <failure>
When the consumer calls Start()
Then the returned error message contains "service `b`"
And <cause-check>
And the started services are a
And "c" was built but not started

Examples:
  | failure                            | cause-check                            |
  | Start() returning errBoom          | errors.Is(err, errBoom) is true        |
  | Start(ctx) panicking with "kaboom" | the error message contains "kaboom"    |
  | Start(ctx) panicking with errBoom  | errors.Is(err, errBoom) is true        |
```

#### Scenario: A build failure stops Start before any Starter
```gherkin
Given a root scope with lazy Starters "a", "b", "c" registered in that order, with no dependencies
And "b" is registered with <registration>
When the consumer calls Start()
Then the returned error message contains "service `b`"
And errors.Is(err, <cause>) is true
And "a" was built and "c" was not built
And no Starter was started

Examples:
  | registration                                   | cause              |
  | a provider returning errBuild                  | errBuild           |
  | DependsOnNamed("missing")                      | ErrServiceNotFound |
```

#### Scenario: AsStarter on an interface whose instance cannot start
```gherkin
Given a root scope with lazy "app.Lifecycle" registered with AsStarter()
And the provider returns an instance with no Start method
When the consumer calls Start()
Then the error message contains "doger: service `app.Lifecycle` was opted in with AsStarter but does not implement any Starter interface"
```

#### Scenario: Shutdown after a failed Start reaches every built service
```gherkin
Given a root scope where Starter "a" starts, Starter "b" returns errBoom from Start, and Starter "c" is built but never started
And all three implement Shutdown() error
And the consumer called Start(), which returned an error
When the consumer calls Shutdown()
Then Shutdown() was called on a, b and c
```

#### Scenario: A panic escaping Start still releases Shutdown
```gherkin
Given a root scope with lazy Starter "a" implementing Shutdown() error
And a HookBeforeInvocation that panics with "hook-boom"
And the consumer called Start(), which panicked with "hook-boom"
When the consumer calls Shutdown() inside testWithTimeout(1s)
Then Shutdown returns a successful report
And a second Start() returns an error matching ErrShutdown
```

<!-- cases: N/A 2 overrides: no rollback by design; the caller decides (Shutdown, retry, exit) -->
<!-- cases: N/A 3 zero values: a nil error from Start() error is the success case -->
<!-- cases: 7 lifecycle: covered by "Shutdown after a failed Start" and "A panic escaping Start still releases Shutdown" -->
<!-- cases: N/A 8 concurrency: the pipeline is sequential -->
<!-- cases: 10 compatibility: Shutdown behavior for services that were never started is unchanged; ErrShutdown replaces ErrStartAfterShutdown and messages use the "doger: " prefix -->

### Requirement: Start is one-shot
A root container SHALL start at most once. Any `Start` after the first, whether that one succeeded or failed, SHALL return an error matching `ErrAlreadyStarted` and do nothing, as long as root `Shutdown` has not begun. Any `Start` after root `Shutdown` began SHALL return an error matching `ErrShutdown` and not `ErrAlreadyStarted`, whether or not a `Start` ran before. A clone SHALL start independently of its source.

#### Scenario: Repeated and late Start calls
```gherkin
Given a root scope with one lazy Starter "a"
And the consumer has already <previous>
When the consumer calls Start()
Then the result matches <result>
And errors.Is(err, ErrAlreadyStarted) is true only in the ErrAlreadyStarted rows
And "a" had Start called <calls> times in total

Examples:
  | previous                                                      | result                 | calls |
  | called nothing                                                | nil                    | 1     |
  | called Start() successfully                                   | ErrAlreadyStarted      | 1     |
  | called Start(), which failed because a's provider returned errBuild | ErrAlreadyStarted | 0     |
  | called Shutdown()                                             | ErrShutdown  | 0     |
  | called Start() successfully and then Shutdown()               | ErrShutdown  | 1     |
  | called Start(), which failed because a's provider returned errBuild, and then Shutdown() | ErrShutdown | 0 |
```

#### Scenario: Concurrent Start calls
```gherkin
Given a root scope with one lazy Starter "a"
When 10 goroutines call Start() concurrently
Then exactly 1 call returns nil
And 9 calls return an error matching ErrAlreadyStarted
And "a" was started 1 time
```

#### Scenario: Concurrent Start and Invoke
```gherkin
Given a root scope with lazy Starter "a" whose provider counts its calls
When Start() runs while 50 goroutines invoke "a"
Then the "a" provider was called exactly 1 time
And no data race is reported
```

#### Scenario: Clone of a started container
```gherkin
Given a root scope with <registration> Starter "a" counting Start calls per instance
And the consumer called Start() on the root scope
When the consumer calls Start() on root.Clone()
Then the clone's Start returns nil
And the Start calls are <calls>

Examples:
  | registration | calls                                     |
  | lazy         | 1 on the original instance, 1 on a new instance |
  | eager value  | 2 on the same shared instance             |
```

<!-- cases: N/A 2 overrides: one-shot is fixed; re-running is the deferred Refresh operation -->
<!-- cases: N/A 3 zero values: covered by the "called nothing" row -->
<!-- cases: 5 errors: ErrShutdown takes precedence over ErrAlreadyStarted once Shutdown began, after a successful or a failed Start -->
<!-- cases: 7 lifecycle: start after start, after a failed start, after shutdown (with and without an earlier Start), and clones are covered -->
<!-- cases: N/A 9 panics: covered by "A panic escaping Start still releases Shutdown" -->
<!-- cases: 10 compatibility: BREAKING (flagged) - ErrShutdown replaces ErrStartAfterShutdown; Clone keeps sharing eager instances, as today; the eager row documents the consequence (their shutdown runs once, see container-shutdown) -->

### Requirement: Shutdown cancels a running Start
A root `Shutdown` that begins while `Start` runs SHALL cancel Start's context with cause `ErrShutdown` and wait until `Start` returns or its own context is done; in the latter case its report SHALL fail with a root-scope error matching that context's error. `Start` SHALL then stop before the next Starter or handler call and return an error matching `ErrShutdown`. Nothing a late Starter brings up is rolled back.

#### Scenario: Shutdown cancels a running Start
```gherkin
Given a root scope inside synctest.Test with <role> "a" registered before <role> "z", both implementing Shutdown() error
And "a" <a-behavior>
And Start() is running in another goroutine and "a" has entered its <method> method
When the consumer calls ShutdownWithContext(<shutdown-ctx>)
Then Shutdown returns <when>
And the shutdown report <report>
And Start returns an error matching ErrShutdown
And "z" had its <method> method called 0 times

Examples:
  | role             | method | a-behavior                                                  | shutdown-ctx            | when                          | report                                                          |
  | lazy Starter     | Start  | returns ctx.Err() when ctx is done                          | context.Background()    | after Start returned          | succeeds and a's Shutdown was called 1 time                     |
  | lazy Runner      | Run    | returns ctx.Err() when ctx is done                          | context.Background()    | after Start returned          | succeeds and a's Shutdown was called 1 time                     |
  | lazy Starter     | Start  | ignores ctx and returns nil once released; a helper goroutine releases it after synctest.Wait() shows Shutdown blocked | context.Background() | after Start returned | succeeds and a's Shutdown was called 1 time |
  | lazy Starter     | Start  | ignores ctx and returns nil once released; the test releases it only after Shutdown returned | a 1s deadline | at the 1s deadline, before Start returned | fails, has an error for the root scope, every error matches context.DeadlineExceeded, and a's Shutdown was called 0 times |
```

<!-- cases: N/A 2 overrides: cancellation is always on; a Starter that must finish ignores ctx and Shutdown's deadline bounds the wait -->
<!-- cases: N/A 3 zero values: a Shutdown with no running Start is covered by "Root Shutdown runs once" -->
<!-- cases: 7 lifecycle: the last row documents that a Starter finishing after Shutdown stopped waiting is not rolled back -->
<!-- cases: 8 concurrency: every row runs under synctest with a channel handshake; goleak checks the Start goroutine exits -->
<!-- cases: N/A 9 panics: covered by "A panic escaping Start still releases Shutdown" -->
<!-- cases: N/A 10 compatibility: new API -->

### Requirement: Root Shutdown reports stay usable
A root Shutdown caller that finds the first shutdown completed SHALL get a copy of the first report, even with an ended context. The stored report SHALL be isolated from every caller's copy. A nil context SHALL mean `context.Background()`. If the first caller panics before producing a report, every other caller SHALL get a failed report whose only root-scope error (empty service name) contains `doger: root shutdown did not complete`.

#### Scenario: The stored report is isolated from the first caller
```gherkin
Given a root scope with built lazy "a" whose Shutdown() returns errClose
And the consumer called Shutdown(), which returned report r1
And the consumer then deleted every entry of r1's Errors map
When the consumer calls Shutdown() again
Then the returned report has the errClose entry for "a"
```

#### Scenario: Later Shutdown with an ended context still returns the first report
```gherkin
Given a root scope with built lazy "a" implementing Shutdown() error
And the consumer called Shutdown(), which returned report r1
When the consumer calls ShutdownWithContext(ctx) 100 times, where ctx <ctx>
Then every returned report equals r1
And "a" had Shutdown called 1 time

Examples:
  | ctx                                      |
  | was cancelled before the calls           |
  | has a deadline that passed before the calls |
```

#### Scenario: Nil Shutdown context
```gherkin
Given a root scope with built lazy "a" whose Shutdown(ctx) error records the context it receives
And the consumer has <previous>
When the consumer calls ShutdownWithContext(nil)
Then it returns a report with Succeed true
And "a" received a non-nil context exactly once

Examples:
  | previous            |
  | called nothing      |
  | called Shutdown()   |
```

#### Scenario: A panicking first caller still releases the others
```gherkin
Given a root scope created with a Logger whose handler panics with "log-boom" when a record's message contains "requested shutdown"
And the consumer called Shutdown(), which panicked with "log-boom"
When the consumer calls <call>
Then it returns without panicking
And the report has Succeed false
And its only error is keyed by the root scope with an empty service name and its message contains "doger: root shutdown did not complete"

Examples:
  | call                                          |
  | Shutdown()                                    |
  | ShutdownWithContext(ctx) with a 1s deadline   |
```

<!-- cases: N/A 2 overrides: these rules only make every caller's report usable; there is nothing to override -->
<!-- cases: 3 zero values: a nil context is covered by "Nil Shutdown context" -->
<!-- cases: 4 boundaries: a context that ended before the call, after the first shutdown completed, is covered by "Later Shutdown with an ended context still returns the first report" -->
<!-- cases: 5 errors: the "did not complete" report is covered by "A panicking first caller still releases the others" -->
<!-- cases: N/A 6 duplicates: repeated calls are the subject of "Root Shutdown runs once" -->
<!-- cases: 7 lifecycle: callers after completion are covered by the ended-context and isolation scenarios -->
<!-- cases: 8 concurrency: 100 calls per row pin completion over an ended context, which a random select would violate about half the time -->
<!-- cases: 9 panics: a panic on the first caller's goroutine propagates to that caller and the others get a failed report; panics in per-service shutdown hooks and Shutdown methods are recovered and reported (container-shutdown) -->
<!-- cases: 10 compatibility: a nil context no longer panics when a built service has a Shutdowner* method (fix); the rest is part of the run-once behavior change -->

### Requirement: Incremental start needs a started container
`Refresh` and `Scope.Start` SHALL return an error matching `ErrNotStarted` ("doger: container not started") while the container's first `Start` has not succeeded, including while that `Start` is still running, and an error matching `ErrShutdown` once root Shutdown has begun, which takes precedence. They SHALL decide this without waiting for a running `Start`, and SHALL start nothing in either case.

#### Scenario: Incremental start in the wrong phase
```gherkin
Given a root scope with lazy Starter "a" and child scope "tenant" with lazy Starter "w"
And the consumer has <previous>
When the consumer calls <operation>
Then the result matches <result>
And the operation started nothing ("w" is started by no Refresh or Scope.Start)

Examples:
  | previous                                                   | operation      | result                |
  | done nothing                                               | root.Refresh() | ErrNotStarted         |
  | done nothing                                               | tenant.Start() | ErrNotStarted         |
  | called Start(), which failed because a's provider returned errBuild | root.Refresh() | ErrNotStarted |
  | called Start() and then Shutdown()                         | root.Refresh() | ErrShutdown |
  | called Shutdown() without Start()                          | tenant.Start() | ErrShutdown |
```

#### Scenario: Incremental start while Start runs
```gherkin
Given a root scope with lazy Starter "a" and child scope "tenant" with lazy Starter "w"
And a's Start calls <operation> <how>, records its result, then returns nil
When the consumer calls Start()
Then Start returns nil
And the recorded result matches ErrNotStarted
And the recorded call returned before a's Start returned
And "w" was started 1 time, by Start

Examples:
  | operation      | how                                     |
  | root.Refresh() | synchronously                           |
  | tenant.Start() | synchronously                           |
  | root.Refresh() | in another goroutine, awaited by a's Start |
```

#### Scenario: A Refresh queued behind a Shutdown
```gherkin
Given a started root container with a HookContainerClosing hook that sleeps 100ms
And lazy Starter "late", registered after Start, whose Start(ctx) blocks until ctx is done and returns ctx.Err()
And a first root.Refresh() is running in another goroutine and "late" has entered Start
And a second root.Refresh() was called in another goroutine and is waiting for the pipeline held by the first
When the consumer calls root.Shutdown()
Then both Refresh calls return an error matching ErrShutdown
And "late"'s Start was called 1 time
```

<!-- cases: N/A 2 overrides: the phase rule has no option -->
<!-- cases: 3 zero values: an idle container -->
<!-- cases: N/A 4 boundaries: phases are discrete -->
<!-- cases: 5 errors: both sentinels and their precedence -->
<!-- cases: N/A 6 duplicates: covered by Refresh -->
<!-- cases: 7 lifecycle: idle, starting, failed, closed; a Refresh that was waiting for the pipeline when Shutdown began -->
<!-- cases: 8 concurrency: a call during a running Start fails fast instead of waiting for it (no deadlock from Start-time code); a queued Refresh re-checks the phase once it holds the pipeline -->
<!-- cases: N/A 9 panics: no method is called -->
<!-- cases: N/A 10 compatibility: new API -->

### Requirement: Concurrent Refresh calls are serialized
Concurrent `Refresh` and `Scope.Start` calls SHALL run one at a time, so each new service is started exactly once. A root Shutdown that begins during an incremental start SHALL cancel it with cause `ErrShutdown` and wait for it, as for `Start`.

#### Scenario: Concurrent incremental starts
```gherkin
Given a started root container and 10 lazy Starters "s0" to "s9" registered after Start
When 10 goroutines call root.Refresh() concurrently
Then every call returns nil
And each of s0 to s9 was started exactly 1 time
And no data race is reported
```

#### Scenario: Shutdown cancels a running Refresh
```gherkin
Given a started root container inside synctest.Test with lazy Starter "late" registered after Start, whose Start(ctx) blocks until ctx is done and returns ctx.Err()
And Refresh() is running in another goroutine and "late" has entered Start
When the consumer calls Shutdown()
Then Shutdown returns only after the Refresh run ended, its Starter having returned
And the report succeeds
And Refresh returns an error matching ErrShutdown
```

#### Scenario: Shutdown interrupts a scope Starting hook
```gherkin
Given a started root container inside synctest.Test and child scope "tenant", created after Start
And a HookScopeStarting hook that starts root Shutdown in a goroutine, waits for ctx.Done() and returns ctx.Err()
When the consumer calls tenant.Start()
Then it returns an error for which errors.Is(err, ErrShutdown) is true
```

<!-- cases: N/A 2 overrides: serialization is not configurable -->
<!-- cases: N/A 3 zero values, 4 boundaries: 10 callers -->
<!-- cases: 5 errors: the cancelled Refresh error -->
<!-- cases: N/A 6 duplicates: the exactly-once counts -->
<!-- cases: 7 lifecycle: Shutdown during Refresh -->
<!-- cases: 8 concurrency: the subject; runs under -race with goleak -->
<!-- cases: N/A 9 panics: as for Start -->
<!-- cases: N/A 10 compatibility: new API -->

### Requirement: A failure returned after Shutdown began matches ErrShutdown
When root Shutdown began before Start, Refresh or Scope.Start returns a service failure (for example while a parallel start waits for its running Starters after the first failure), that failure SHALL also match `ErrShutdown`, as every start failure after Shutdown began does, and keep matching its own cause.

#### Scenario: A failure followed by Shutdown under parallel start
```gherkin
Given a root container inside synctest.Test created with StartParallelism 2
And lazy Starters "a" (returns errBoom after 1s) and "b" (returns its context's error once cancelled, or nil after 10s), without dependencies, registered <when>
And root Shutdown() is called in another goroutine at 1.5s of bubble time
When the consumer calls <operation>
Then it returns an error matching both errBoom and ErrShutdown
And "b"'s context was cancelled

Examples:
  | when                         | operation |
  | before Start                 | Start()   |
  | after Start() returned nil   | Refresh() |
```

<!-- cases: N/A 1 default, 2 overrides: there is no option -->
<!-- cases: N/A 3 zero values, 4 boundaries: one failure and one Shutdown -->
<!-- cases: 5 errors: the failure matches both its cause and ErrShutdown -->
<!-- cases: N/A 6 duplicates: an error already carrying the abort cause is not wrapped again -->
<!-- cases: 7 lifecycle: Start and Refresh rows -->
<!-- cases: 8 concurrency: synctest bubble time; runs under -race with goleak -->
<!-- cases: N/A 9 panics: a panic is re-raised as before -->
<!-- cases: N/A 10 compatibility: completes the existing "Shutdown cancels a running Start" rule for parallel start -->

### Requirement: Start failures are StartError values
Every failure of a service or kind handler that Start, Refresh or Scope.Start returns SHALL be a `*StartError{Scope, Service, Kind, Op, Cause}` (`Op` is `build`, `start` or `handle`; `Kind` is empty unless `Op` is `handle`) whose `Error()` keeps the existing message, with the library prefix `doger: ` in place of the former one, and whose `Unwrap()` returns `Cause`. Because its fields are exported, `Error()` SHALL NOT panic on a value built by the consumer: a nil `Scope` reads as an empty scope name and a nil `Cause` as `<nil>`.

#### Scenario: StartError fields per failure point
```gherkin
Given a root scope where <failure>
When the consumer calls Start()
Then errors.As(err, &startErr) is true
And startErr has Service "<service>", Scope name "[root]", Kind "<kind>" and Op "<op>"
And errors.Is(err, <cause>) is true
And err.Error() starts with "doger: " and contains "doger: " exactly once

Examples:
  | failure                                               | service | kind      | op     | cause              |
  | lazy Starter "b"'s provider returns errBuild           | b       |           | build  | errBuild           |
  | lazy Starter "b"'s Start returns errBoom               | b       |           | start  | errBoom            |
  | kind "k"'s handler returns errBoom for lazy "j"        | j       | k         | handle | errBoom            |
  | lazy "b" registered with DependsOnNamed("missing")      | b       |           | build  | ErrServiceNotFound |
```

#### Scenario: Incremental start failures are StartError values
```gherkin
Given a started root container and child scope "tenant", created after Start, where <failure>
When the consumer calls <operation>
Then errors.As(err, &startErr) is true
And startErr has Service "<service>", Scope name "tenant", Kind "<kind>" and Op "<op>"
And errors.Is(err, <cause>) is true

Examples:
  | failure                                          | operation      | service | kind | op     | cause    |
  | lazy Starter "b"'s provider returns errBuild      | root.Refresh() | b       |      | build  | errBuild |
  | lazy Starter "b"'s Start returns errBoom          | tenant.Start() | b       |      | start  | errBoom  |
  | kind "k"'s handler returns errBoom for lazy "j"   | root.Refresh() | j       | k    | handle | errBoom  |
```

#### Scenario: StartError values built by the consumer
```gherkin
Given a *StartError built by the consumer with <fields>
When the consumer calls Error()
Then it does not panic
And it returns "<message>"

Examples:
  | fields                                                   | message                                                   |
  | Service "db", Op start, Cause errors.New("boom"), no Scope | doger: failed to start service `db` in scope ``: boom        |
  | Service "db", Op build, Scope the root's, no Cause         | doger: failed to build service `db` in scope `[root]`: <nil> |
  | Service "j", Kind "k", Op handle, no Scope, no Cause       | doger: kind `k` failed for service `j` in scope ``: <nil>    |
  | all fields zero                                          | doger: failed to build service `` in scope ``: <nil>         |
```

#### Scenario: Errors that are not service failures are not StartError
```gherkin
Given a root scope where <failure>
When the consumer calls Start()
Then errors.As(err, &startErr) is false
And errors.Is(err, <sentinel>) is true

Examples:
  | failure                                  | sentinel              |
  | Start() was already called successfully   | ErrAlreadyStarted     |
  | Shutdown() was already called             | ErrShutdown |
  | the context is already cancelled          | context.Canceled      |
```

<!-- cases: N/A 2 overrides: the error type is fixed -->
<!-- cases: 3 zero values: a nil error on success; a StartError with a nil Scope or Cause, or all fields zero ("StartError values built by the consumer") -->
<!-- cases: N/A 4 boundaries: one failure per Start -->
<!-- cases: 5 errors: every failure point and the non-service errors -->
<!-- cases: N/A 6 duplicates: covered by the one-shot requirement -->
<!-- cases: 7 lifecycle: Refresh and Scope.Start return the same type ("Incremental start failures are StartError values") -->
<!-- cases: N/A 8 concurrency: parallel start returns the first StartError -->
<!-- cases: 9 panics: a recovered Start panic is Op "start" with the panic as cause -->
<!-- cases: 10 compatibility: BREAKING (flagged) - the message prefix changes to "doger: " and ErrShutdown replaces ErrStartAfterShutdown; errors.Is and errors.As behavior is otherwise unchanged -->

### Requirement: An aborted run matches ErrShutdown when the caller cancelled first
When the caller's context was cancelled before root Shutdown began, a Start, Refresh or Scope.Start that returns after Shutdown began SHALL return an error matching `ErrShutdown` and the caller's cause, whether its pipeline failed or returned nil. A cause SHALL appear once in the message.

#### Scenario: Caller cancellation, then Shutdown
```gherkin
Given a root container inside synctest.Test created with StartParallelism <n>
And lazy Starter "a" whose Start blocks until released, then returns <outcome>, registered <when>
And with n = 4, lazy Starter "b", without dependencies, whose Start blocks until released, then returns nil
And <operation> is running with a cancellable context ctx and "a" (and "b" with n = 4) has entered Start
When the consumer cancels ctx, then calls Shutdown() in another goroutine, waits until it is blocked, and releases the Starters
Then <operation> returns an error matching ErrShutdown and context.Canceled
And <check>

Examples:
  | n | outcome | when                       | operation             | check                                        |
  | 1 | errBoom | before Start               | StartWithContext(ctx)   | it matches errBoom                           |
  | 1 | errBoom | after Start() returned nil | RefreshWithContext(ctx) | it matches errBoom                           |
  | 4 | errBoom | before Start               | StartWithContext(ctx)   | it matches errBoom                           |
  | 4 | errBoom | after Start() returned nil | RefreshWithContext(ctx) | it matches errBoom                           |
  | 1 | nil     | before Start               | StartWithContext(ctx)   | its message contains "context canceled" once |
  | 1 | nil     | after Start() returned nil | RefreshWithContext(ctx) | its message contains "context canceled" once |
```

#### Scenario: The abort outcome rule
```gherkin
Given a run whose context was cancelled with cause <cause>, after which Shutdown began
When the run's outcome is <result>
Then the returned error is <returned>

Examples:
  | cause                 | result                                    | returned                                                                  |
  | ErrShutdown | nil                                       | "doger: start aborted: container is shut down"                           |
  | context.Canceled      | nil                                       | "doger: start aborted: container is shut down: context canceled"          |
  | ErrShutdown | errBoom                                   | "boom (start aborted: container is shut down)"                        |
  | context.Canceled      | errBoom                                   | "boom (start aborted: container is shut down: context canceled)"       |
  | context.Canceled      | "boom (start aborted: context canceled)"  | "boom (start aborted: context canceled) (start aborted: container is shut down)" |
  | context.Canceled      | an error already matching ErrShutdown | that error unchanged                                                |
```

<!-- cases: N/A 1 default, 2 overrides: there is no option -->
<!-- cases: N/A 3 zero values: a nil outcome is a row -->
<!-- cases: N/A 4 boundaries: one cancellation and one Shutdown -->
<!-- cases: 5 errors: the subject; both causes match -->
<!-- cases: 6 duplicates: a cause already carried is not wrapped again -->
<!-- cases: 7 lifecycle: Start and Refresh rows; Scope.Start shares finishIncremental with Refresh -->
<!-- cases: 8 concurrency: sequential and parallel rows; synctest bubble; runs under -race with goleak -->
<!-- cases: N/A 9 panics: covered by container-lifecycle-hooks "Shutdown during an in-flight Start" -->
<!-- cases: 10 compatibility: BREAKING (flagged) - ErrShutdown replaces ErrStartAfterShutdown and the doubled library prefix becomes a single "doger: " prefix; a message led by the consumer's cause carries no library prefix -->

### Requirement: No start hooks without a Start method
No start hook SHALL be called for a service opted in with `AsStarter()` whose instance implements no Starter variant, because no `Start` method is called. `Start` SHALL return the error instead.

#### Scenario: No hooks for an AsStarter instance without a Start method
```gherkin
Given a root scope with HookBeforeStart and HookAfterStart recording calls
And lazy "app.Lifecycle" registered with AsStarter(), whose provider returns an instance with no Start method
When the consumer calls Start()
Then Start returns an error containing "doger: service `app.Lifecycle` was opted in with AsStarter but does not implement any Starter interface"
And no start hook was called
```

<!-- cases: N/A 2 overrides, 3 zero values, 4 boundaries: a single rule with one input shape -->
<!-- cases: 5 errors: the returned error is the one specified in "AsStarter on an interface whose instance cannot start" -->
<!-- cases: N/A 6 duplicates, 7 lifecycle, 8 concurrency: hooks run on the Start goroutine -->
<!-- cases: N/A 9 panics: no method is called, so nothing can panic -->
<!-- cases: N/A 10 compatibility: new hooks -->

### Requirement: A default that yields is not a start candidate
`Start`, `Refresh` and `Scope.Start` SHALL NOT start, or pass to a kind handler, a default that yields to a visible non-default service of the same name, even if its registered type is a Starter or it was given `AsStarter()` or a kind option. A default that does not yield SHALL be a candidate as any registration.

#### Scenario: Start candidates with defaults
```gherkin
Given a root scope with child scope "ext"
And a lazy "*app.Server" registered in scope "ext" with doger.AsDefault(), implementing Start(ctx) error and counting calls
And <explicit>
And <before>
When the consumer calls <operation> on the root
Then the default server's Start was called <default_starts> times
And the root server's Start was called <root_starts> times

Examples:
  | explicit                                                       | before                          | operation | default_starts | root_starts |
  | an explicit "*app.Server" in the root, counting Start calls    | nothing else happened           | Start     | 0              | 1           |
  | nothing                                                        | nothing else happened           | Start     | 1              | 0           |
  | an explicit "*app.Server" in the root, counting Start calls    | the root was started            | Refresh   | 0              | 1           |
```

#### Scenario: A default yields even when it was never invoked before Start
```gherkin
Given a root scope with child scope "ext"
And a lazy "*app.Server" registered in scope "ext" with doger.AsDefault() and doger.AsRunnable(), never invoked
And an explicit lazy "*app.Server" registered in the root with doger.AsRunnable(), implementing Run(ctx) error and counting calls
When the consumer calls Start on the root
Then the root server's Run was called 1 time
And the default's provider was called 0 times
```

#### Scenario: Scope.Start skips a default that yields
```gherkin
Given a started root scope
And a child scope "ext" created after Start, with a lazy "*app.Server" registered with doger.AsDefault(), implementing Start(ctx) error
And an explicit lazy "*app.Server" in the root that was started by the root Start
When the consumer calls Start on scope "ext"
Then the root server's Start was called 1 time in total
And the default server's provider was called 0 times
```

<!-- cases: N/A 2 overrides: EnableStarterAutoDetection applies to built defaults as to any service -->
<!-- cases: N/A 3 zero values, 4 boundaries, 5 invalid input: no input beyond registrations -->
<!-- cases: N/A 6 duplicate: same-scope cases are in default-registrations -->
<!-- cases: 7 lifecycle: Start, Refresh and Scope.Start are covered -->
<!-- cases: N/A 8 concurrency: Start discovery runs under the existing locks; concurrent first resolution is in default-registrations -->
<!-- cases: N/A 9 panics: a skipped default runs no code -->
<!-- cases: 10 compatibility: candidates without AsDefault are unchanged -->

### Requirement: Per-service start hooks
For each Starter, `Start` SHALL call every `HookBeforeStart(ctx, scope, name)` just before its `Start` method and every `HookAfterStart(ctx, scope, name, elapsed, err)` just after, where `ctx` is the context the Starter receives, `elapsed` is the time the method took on the container clock, and `err` is the error the method returned, unwrapped (nil on success, the recovered panic as an error). Hooks SHALL run in the order they were added. `AddBeforeStartHook`/`AddAfterStartHook` SHALL append to the same lists.

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
<!-- cases: 9 panics: a panicking start hook propagates (start hooks are not recovered; shutdown hooks are, see container-shutdown); container state is still released, see "A panic escaping Start still releases Shutdown" -->
<!-- cases: N/A 8 concurrency: hooks run on the Start goroutine -->
<!-- cases: N/A 10 compatibility: new hooks -->

### Requirement: Child scope shutdown waits for a start that is starting its services
A child scope's `Shutdown`/`ShutdownWithContext` that begins while a `Start`, `Refresh` or `Scope.Start` is running, and that run has services of this scope among its candidates, SHALL wait for that run to return, without cancelling it, before shutting anything down. A run that begins while the scope shuts down SHALL NOT start or handle services of that scope or of its descendant scopes. A descendant scope cannot be created under it then: `Scope()` panics with an error matching `ErrShutdown` (container-shutdown, "A closed scope stays closed"). Shutting such a scope down synchronously from code running in that run deadlocks, as for root Shutdown.

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

<!-- cases: 1 default: waiting is not configurable -->
<!-- cases: 2 overrides: the context bounds the wait ("A child shutdown wait is bounded by its context") -->
<!-- cases: N/A 3 zero values: a nil context means context.Background(), as for every Shutdown -->
<!-- cases: 4 boundaries: a run with no candidate in the scope (a scope created during the run) does not wait -->
<!-- cases: N/A 5 errors: the context error has its own requirement -->
<!-- cases: N/A 6 duplicates: a second Shutdown of the same scope finds no running run with candidates in it -->
<!-- cases: 7 lifecycle: Start, Refresh and Scope.Start rows; a run that begins during the shutdown -->
<!-- cases: 8 concurrency: the subject; runs under -race with goleak -->
<!-- cases: N/A 9 panics: hook panics during the wait follow scope-lifecycle-hooks -->
<!-- cases: 10 compatibility: BEHAVIOR CHANGE (flagged) - a child scope Shutdown no longer races a running start; Start-time code that shuts down a scope the run is starting deadlocks; a grandchild created under a closing scope is now refused with ErrShutdown instead of skipped -->

## ADDED Requirements

### Requirement: Starter auto-detection is opt-in
With zero configuration, `Start` SHALL start only services registered with `AsStarter()`, whatever methods their types have. When `InjectorOpts.EnableStarterAutoDetection` is true, `Start` SHALL also start every non-transient, non-alias service whose registered type implements a `Starter*` variant. A clone made with `Clone()` SHALL keep its source's setting.

#### Scenario: Auto-detection toggle
```gherkin
Given a root scope created with <opts>
And lazy "a" whose type has Start() error, registered without options
And lazy "b" whose type has Start() error, registered with AsStarter()
And lazy "c" whose type has Start(context.Context) error, registered without options
When the consumer calls Start() on <container>
Then the started services are <started>

Examples:
  | opts                                                          | container       | started  |
  | doger.New() (zero configuration)                              | the root        | b        |
  | NewWithOpts(&InjectorOpts{})                                  | the root        | b        |
  | NewWithOpts(nil)                                              | the root        | b        |
  | NewWithOpts(&InjectorOpts{EnableStarterAutoDetection: true})  | the root        | a, b, c  |
  | NewWithOpts(&InjectorOpts{EnableStarterAutoDetection: true})  | root.Clone()    | a, b, c  |
  | doger.New() (zero configuration)                              | root.Clone()    | b        |
```

#### Scenario: Familiar Start methods are not started by default
```gherkin
Given a root scope created with doger.New()
And lazy "<service>" of type <type> registered without options and invoked before Start
When the consumer calls Start() and then Shutdown()
Then Start returns nil
And the instance's Start method was called 0 times

Examples:
  | service | type                                               |
  | cmd     | *exec.Cmd (Start() error)                          |
  | srv     | *httptest.Server (Start())                         |
  | job     | *app.Cron whose Start() starts a goroutine         |
```

<!-- cases: 1 default: zero configuration starts only AsStarter() services ("Auto-detection toggle" doger.New() rows, "Familiar Start methods are not started by default") -->
<!-- cases: 2 overrides: EnableStarterAutoDetection: true, and AsStarter() per service -->
<!-- cases: 3 zero values: a nil *InjectorOpts and a zero InjectorOpts behave like doger.New() -->
<!-- cases: N/A 4 boundaries: a boolean option and a per-service opt-in -->
<!-- cases: N/A 5 errors: AsStarter() on a type with no Start method is covered by registration-options ("Invalid opt-ins panic at registration") -->
<!-- cases: 6 duplicates: auto-detection plus AsStarter() starts a service once ("Auto-detection toggle", the EnableStarterAutoDetection rows: "b" has both and starts once) -->
<!-- cases: 7 lifecycle: a clone keeps its source's setting (Clone rows) -->
<!-- cases: N/A 8 concurrency: options are fixed at container creation -->
<!-- cases: N/A 9 panics: no new panic path -->
<!-- cases: 10 compatibility: BREAKING (flagged) - DisableStarterAutoDetection is removed and auto-detection is off by default; migration: add AsStarter() to each service that must start, or set EnableStarterAutoDetection: true -->
<!-- note: in every scenario of the library's specs, a "Starter" is registered with AsStarter() unless the scenario sets EnableStarterAutoDetection -->
