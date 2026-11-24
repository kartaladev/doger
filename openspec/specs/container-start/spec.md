# container-start Specification

## Purpose

Lets a consumer start a fully wired container in one call, so background components are built and started in dependency order, and then stopped by the existing shutdown, which runs exactly once per root container.

## Requirements

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

### Requirement: Start builds candidates and starts them in dependency order
`Start` SHALL build every candidate (Starters and services opted into any kind) before starting any Starter. A service's dependencies, invoked or declared with `DependsOn`, are built before it. It SHALL then start Starters one at a time, unless `InjectorOpts.StartParallelism` is greater than 1: a Starter starts only after every Starter it depends on, directly or transitively, has started. Ties are broken by scope depth, then registration order.

#### Scenario: Dependency-ordered build and start
```gherkin
Given a root scope recording provider calls and start calls
And lazy Starter "http" whose provider invokes "db"
And lazy Starter "db"
And lazy non-starter "repo" whose provider invokes "db"
And lazy Starter "consumer" whose provider invokes "repo"
When the consumer calls Start()
Then every provider call happens before the first start call
And the start order is db, http, consumer
And "repo" is built but not started
```

#### Scenario: Child scopes are started after their ancestors
```gherkin
Given a grandchild scope "jobs" under child scope "tenant-a" with lazy Starter "cron", registered first
And child scope "tenant-a" with lazy Starter "worker" invoking "db", registered second
And the root scope with lazy Starter "db", registered third
When the consumer calls Start() on the root scope
Then the start order is db, worker, cron
```

#### Scenario: Start on an empty container
```gherkin
Given a root scope with no registrations
When the consumer calls Start()
Then Start returns nil
```

<!-- cases: 2 overrides: ordering is overridden by declaring edges (dependency-declaration); concurrency by StartParallelism ("Parallel start keeps dependency order") -->
<!-- cases: N/A 4 boundaries: 0, 1 and many Starters are covered across scenarios -->
<!-- cases: 6 duplicates: a service reachable from several Starters is built once (lazy singleton semantics) -->
<!-- cases: 7 lifecycle: child scopes are covered by "Child scopes are started after their ancestors"; cycles by dependency-declaration ("Start rejects a cycle closed by a dynamic edge") -->
<!-- cases: 8 concurrency: Starters start sequentially by default; concurrent Invoke during Start is covered by "Concurrent Start and Invoke"; parallel start by its own requirement -->
<!-- cases: N/A 9 panics: covered by "Start errors and panics" -->
<!-- cases: N/A 10 compatibility: new API -->

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

### Requirement: Root Shutdown runs once
Root `Shutdown`/`ShutdownWithContext` SHALL shut services down exactly once per root container. A concurrent or later caller SHALL wait for the first one, or for its own context, and SHALL return a copy of the first caller's report. A waiting caller whose context ends first SHALL return a failed report with one error, keyed by the root scope with an empty service name, wrapping `ctx.Err()`. This fixes spurious `ErrServiceNotFound` errors and a data race.

#### Scenario: Concurrent root Shutdown runs once
```gherkin
Given a root scope created with <opts>
And built lazy services "a", "b" and "c", each counting Shutdown() error calls
When 20 goroutines call Shutdown() on the root scope concurrently
Then every returned report has Succeed true and no errors
And every returned report equals the first caller's report
And each of a, b and c had Shutdown called 1 time
And no data race is reported

Examples:
  | opts                                  |
  | &InjectorOpts{}                       |
  | &InjectorOpts{HealthCheckParallelism: 2} |
```

#### Scenario: Later root Shutdown returns the first report
```gherkin
Given a root scope with built lazy "a" whose Shutdown() returns errClose
And the consumer called Shutdown(), which returned report r1
When the consumer calls Shutdown() again
Then the returned report equals r1, including the errClose entry for "a"
And it is a distinct copy: mutating its Errors map leaves r1 unchanged
And "a" had Shutdown called 1 time
```

#### Scenario: Waiting Shutdown caller honours its context
```gherkin
Given a root scope inside synctest.Test with built lazy "a" whose Shutdown() blocks until released
And a first Shutdown() is running in another goroutine and "a" has entered Shutdown()
When the consumer calls ShutdownWithContext(ctx) where ctx has a 1s deadline
Then it returns at the 1s deadline with Succeed false
And its only error is keyed by the root scope with an empty service name and matches context.DeadlineExceeded
And "a" had Shutdown called 1 time after it is released
```

<!-- cases: N/A 2 overrides: running shutdown more than once is never correct; there is nothing to override -->
<!-- cases: 3 zero values: an empty container is covered by existing shutdown tests; a nil context is covered by "Root Shutdown reports stay usable" -->
<!-- cases: 5 errors: per-service errors are carried in the copied report ("Later root Shutdown returns the first report") -->
<!-- cases: 7 lifecycle: double shutdown is "Later root Shutdown returns the first report"; Start after Shutdown is covered by "Start is one-shot" -->
<!-- cases: 8 concurrency: first scenario under -race with 20 goroutines; deadline in "Waiting Shutdown caller honours its context" -->
<!-- cases: N/A 9 panics: covered by "Root Shutdown reports stay usable" -->
<!-- cases: 10 compatibility: BEHAVIOR CHANGE - a later root Shutdown returns the first report instead of an empty one, and no longer shuts down services registered on the root after the first Shutdown; Scope.Shutdown on child scopes is unchanged -->

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

### Requirement: Registrations after Start are not started
Services registered after `Start` returned, and child scopes created after `Start` took its discovery snapshot, SHALL NOT be started or handled automatically. They remain invokable as usual, and `RootScope.Refresh` or `Scope.Start` starts them.

#### Scenario: Late registration
```gherkin
Given a root scope that has been started
When the consumer registers lazy Starter "late" and invokes it
Then "late" is built
And "late" was started 0 times
```

<!-- cases: 2 overrides: Refresh and Scope.Start start late registrations (their own requirements) -->
<!-- cases: N/A 3, 4, 5, 9: no inputs or failure modes beyond the scenario -->
<!-- cases: N/A 7 lifecycle: registrations made during Start (for example by a provider) are unspecified and are not covered by any guarantee -->
<!-- cases: N/A 8 concurrency: covered by "Concurrent Start and Invoke" -->
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

### Requirement: Scope.Start starts a scope subtree incrementally
`Scope.Start()` / `Scope.StartWithContext(ctx)` on a child scope SHALL do what `Refresh` does, restricted to that scope and its descendants. Ancestor services the subtree depends on SHALL be built as needed but SHALL NOT be started by it.

#### Scenario: Scope.Start starts only its subtree
```gherkin
Given a started root container
And child scopes "tenant-a" with lazy Starter "wa" and "tenant-b" with lazy Starter "wb", created after Start
And grandchild "jobs" under "tenant-a" with lazy Starter "cron"
And lazy Starter "late" registered in the root after Start
When the consumer calls tenant-a.Start()
Then it returns nil
And the started services are wa, cron
And "wb" and "late" were started 0 times
```

#### Scenario: Scope.Start builds ancestors but does not start them
```gherkin
Given a started root container and lazy Starter "db" registered in the root after Start
And child scope "tenant", created after Start, with lazy Starter "worker" whose provider invokes "db"
When the consumer calls tenant.Start()
Then it returns nil
And "worker" was started 1 time
And "db" is built and was started 0 times
```

#### Scenario: Scope.Start on the root's own scope
```gherkin
Given a started root container with lazy Starter "late" registered after Start
And s is the root's own *Scope, as a kind handler receives it in Component.Scope for a root service (a provider receives an Injector, not a *Scope)
When the consumer calls s.Start()
Then it returns nil
And "late" was started 1 time
```

<!-- cases: 1 default: subtree start -->
<!-- cases: N/A 2 overrides: as for Refresh -->
<!-- cases: N/A 3 zero values: an empty subtree returns nil (as "Refresh with nothing new") -->
<!-- cases: 4 boundaries: child with grandchild; sibling untouched; an ancestor dependency is built, not started -->
<!-- cases: N/A 5 errors: as for Refresh -->
<!-- cases: 6 duplicates: the root's own scope behaves as Refresh -->
<!-- cases: 7 lifecycle: before Start is covered by "Incremental start needs a started container" -->
<!-- cases: N/A 8 concurrency: Scope.Start and Refresh share the serialization of "Concurrent Refresh calls are serialized" -->
<!-- cases: N/A 9 panics: as for Start -->
<!-- cases: N/A 10 compatibility: new API -->

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

### Requirement: Parallel start keeps dependency order
With `InjectorOpts.StartParallelism` n > 1, Start, Refresh and Scope.Start SHALL run up to n Starters concurrently, each only after every Starter it depends on has started. The first failure SHALL stop new Starters from being scheduled; Start SHALL wait for running ones and return that failure. 0 and 1 SHALL mean sequential, and a value larger than the number of Starters SHALL behave as that number.

#### Scenario: Independent Starters start concurrently
```gherkin
Given a root scope inside synctest.Test created with StartParallelism <n>
And lazy Starters registered in order "a", "b", "c", "d", "e", each blocking 1s in Start, where only "d"'s provider invokes "a"
When the consumer calls Start()
Then Start returns nil after <elapsed> of bubble time
And "d" started after "a" returned

Examples:
  | n | elapsed |
  | 0 | 5s      |
  | 1 | 5s      |
  | 2 | 3s      |
  | 3 | 2s      |
  | 8 | 2s      |
  | 18446744073709551615 (max uint) | 2s |
```

#### Scenario: A failure stops scheduling
```gherkin
Given a root scope inside synctest.Test created with StartParallelism 2
And lazy Starters "a" (returns errBoom after 1s), "b" (blocks 2s, then returns nil) and "c", registered in that order, without dependencies
When the consumer calls Start()
Then Start returns at 2s with an error matching errBoom and naming "a"
And "c" was started 0 times
And "b" was started 1 time
```

#### Scenario: Start hooks under parallel start
```gherkin
Given a root scope created with StartParallelism 4 and HookBeforeStart/HookAfterStart hooks recording calls under a mutex
And 8 lazy Starters without dependencies
When the consumer calls Start()
Then each Starter has exactly one before and one after record
And each Starter's before record precedes its own after record
And no data race is reported
```

#### Scenario: Parallel incremental start
```gherkin
Given a started root container inside synctest.Test created with StartParallelism 3
And lazy Starters "a", "b" and "c" registered after Start in <scope>, each blocking 1s in Start, without dependencies
When the consumer calls <operation>
Then it returns nil after 1s of bubble time
And each of a, b and c was started 1 time

Examples:
  | scope                                   | operation      |
  | the root                                | root.Refresh() |
  | child scope "tenant", created after Start | tenant.Start() |
```

#### Scenario: Parallel incremental start keeps dependency order
```gherkin
Given a started root container inside synctest.Test created with StartParallelism 3
And lazy Starters "a" (blocking 2s), "b", "c" and "d" (each blocking 1s) registered after Start in <scope>, where only "d"'s provider invokes "a"
When the consumer calls <operation>
Then it returns nil after 3s of bubble time
And "d" started after "a" returned

Examples:
  | scope                                   | operation      |
  | the root                                | root.Refresh() |
  | child scope "tenant", created after Start | tenant.Start() |
```

<!-- cases: 1 default: 0 and 1 rows are the sequential default -->
<!-- cases: 2 overrides: n > 1 rows -->
<!-- cases: 3 zero values: StartParallelism 0 -->
<!-- cases: 4 boundaries: n = 1, 2, number of independent Starters, more than that, the maximum uint -->
<!-- cases: 5 errors: the first failure is returned; running Starters are awaited; a Shutdown during that wait has its own requirement -->
<!-- cases: N/A 6 duplicates: each Starter starts once -->
<!-- cases: 7 lifecycle: dependency order is kept ("d" after "a") -->
<!-- cases: 8 concurrency: per-service hooks may run concurrently and must synchronize their own state; runs under -race with goleak -->
<!-- cases: 9 panics: a recovered Start panic is a failure as in "Start errors and panics"; hook panics have their own requirement -->
<!-- cases: 10 compatibility: the default stays sequential -->

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

### Requirement: Start hook panics under parallel start reach the caller
With `StartParallelism` n > 1, a panic in a per-service start hook (`HookBeforeStart`, `HookAfterStart`) SHALL be re-raised on the goroutine that called Start, Refresh or Scope.Start once the running Starters have returned, exactly as in sequential mode.

#### Scenario: A panicking start hook under parallel start
```gherkin
Given a root scope created with StartParallelism <n> and a <hook> hook that panics with "hook-boom" for "b"
And lazy Starters "a", "b" and "c" without dependencies, registered <when>, where "a" and "c" take 1s to start and "b" returns at once
When the consumer calls <operation>
Then it panics on the calling goroutine with "hook-boom"
And no Starter's Start is still running when the panic is raised
And Phase() is <phase> afterwards

Examples:
  | n | hook            | when         | operation | phase       |
  | 1 | HookBeforeStart | before Start | Start()   | PhaseFailed |
  | 4 | HookBeforeStart | before Start | Start()   | PhaseFailed |
  | 4 | HookAfterStart  | before Start | Start()   | PhaseFailed |
  | 4 | HookBeforeStart | after Start  | Refresh() | PhaseReady  |
  | 4 | HookAfterStart  | after Start  | Refresh() | PhaseReady  |
```

<!-- cases: 1 default: the n = 1 row is the sequential reference -->
<!-- cases: N/A 2 overrides: there is no panic option for per-service hooks -->
<!-- cases: N/A 3 zero values, 4 boundaries: one panicking hook among three Starters -->
<!-- cases: N/A 5 errors: a returned error is covered by "A failure stops scheduling" -->
<!-- cases: N/A 6 duplicates: the first panic wins, as for container hooks -->
<!-- cases: 7 lifecycle: the phase is failed after Start, and stays ready after Refresh -->
<!-- cases: 8 concurrency: no Starter is left running; runs under -race with goleak -->
<!-- cases: 9 panics: the subject -->
<!-- cases: N/A 10 compatibility: new API -->

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

### Requirement: Child scope shutdown waits for a start that is starting its services
A child scope's `Shutdown`/`ShutdownWithContext` that begins while a `Start`, `Refresh` or `Scope.Start` is running with services of this scope or its descendants among its candidates SHALL cancel the contexts of their running start steps with a cause matching `ErrShutdown`, as root Shutdown does for a whole run, then wait for that run to return, bounded by its context, before shutting anything down. Those steps SHALL NOT fail the run or be recorded, and the run SHALL skip the rest of that subtree and continue, also once a bounded shutdown of that scope that gave up waiting for the run completed: a later step of the closed, detached scope SHALL NOT be called on its removed service. A `Scope.Start` on that scope, or on a descendant of it, whose candidates were claimed before the shutdown began SHALL return nil without running the `HookScopeStarted` hooks, also when a bounded shutdown gave up waiting and completed before the run returned, as for a scope that was already shutting down when `Scope.Start` collected its services (scope-lifecycle-hooks); a shutdown of a descendant scope only skips that scope's steps, and the Started hooks of the scope `Scope.Start` runs on still run. This skip rule SHALL apply only to the child scope's own Shutdown while root Shutdown has not begun: root Shutdown cancels the whole run, and a step of a child scope it reaches SHALL be cancelled and reported like a root step ("A Shutdown bounded below the start timeout"). A step of that subtree that the run skips, a cancelled one included, SHALL NOT count as started for its dependents: the run SHALL skip every candidate that depends on its service, directly or through others (an export included), without hook calls. A dependent inside the closing subtree is skipped silently. A dependency through an export of the closing scope SHALL count whenever its edge was declared, an edge declared during the run included, and also once a bounded shutdown that gave up waiting for the run removed the skipped service, its edges and the export's source scope before the step ended: the run SHALL NOT start or silently drop the dependent then. Such a removal takes the edges inside the removed subtree with it, so a dependent of an export of that subtree SHALL then be skipped once the run skipped any step of the subtree, even one the dependent did not depend on, and SHALL be reported, unless a step of the run failed, with the `ErrStartTimeout` error of a step of the subtree whose abandoned call still runs, otherwise with a `*StartError` naming the dependent that matches `ErrShutdown` and names the removed export, never an unrelated skipped step. The source scope SHALL be told apart by identity: a new scope that reuses its name does not make the export's source live again. A bounded shutdown of a descendant of the source scope removes only that descendant: the export's source stays live, and its dependent SHALL NOT be skipped for the descendant's skipped steps. A shutdown that waits for the run removes nothing before the run returns, so the same dependent starts when its own dependencies did. A parallel start SHALL NOT return nil while a Starter outside a shutting-down scope was neither started nor reported. When the run skips a dependent outside it, the run SHALL start everything else and then fail: with the step's own `*StartError` matching `ErrStartTimeout` when the dependency's call was abandoned at `StartTimeout` and still runs, and otherwise with a `*StartError` naming the dependent that matches `ErrShutdown` (its dependency's scope is shutting down). When the dependent reaches several such skipped services, one whose abandoned call still runs SHALL decide the error over one skipped only because its scope is shutting down, and among the same reason the first by scope name, then service name, then scope ID (the last tie-break only separates same-named scopes, and is stable within a container only, since scope IDs are random); the error SHALL NOT depend on the order the dependency graph is walked in. The first step of the run that failed SHALL be the run's error instead. A step abandoned at `StartTimeout` whose dependents all lie inside the closing subtree SHALL NOT fail the run. With a `Logger` set, a cancelled step that returned an error or panicked is logged (container-logging, "A start step cancelled by its scope's shutdown that fails is logged"). A run that begins while the scope shuts down SHALL NOT start or handle services of that scope or of its descendant scopes. A descendant scope cannot be created under it then: `Scope()` panics with an error matching `ErrShutdown` (container-shutdown, "A closed scope stays closed"). Shutting such a scope down synchronously from code running in that run deadlocks, as for root Shutdown.

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

#### Scenario: Scope.Start skips the Started hooks of a scope whose shutdown cancelled its steps
```gherkin
Given a started root container created with InjectorOpts{StartTimeout: <timeout>}, inside synctest.Test, and child scope "c" created after Start
And a HookScopeStarted hook that counts its calls by scope
And lazy Starter "x" in <x-scope>, registered after Start, whose Start(ctx) error <behaviour>
And c.Start() is running in another goroutine and "x" has entered Start
When the consumer calls <shutdown>
And <then>
Then c.Start() returns nil
And the HookScopeStarted hook ran <started> times for "c" and 0 times for any other scope

Examples:
  | timeout | x-scope                  | behaviour                                          | shutdown                                       | then                                                                                     | started |
  | 0       | "c"                      | waits for ctx.Done(), then returns ctx.Err()       | c.Shutdown()                                   | c.Shutdown() returns a successful report                                                 | 0       |
  | 5s      | "c"                      | waits for ctx.Done(), then returns ctx.Err()       | c.Shutdown()                                   | c.Shutdown() returns a successful report                                                 | 0       |
  | 0       | "c"                      | ignores ctx and blocks until released by the test  | c.ShutdownWithContext with a 10ms timeout      | c.ShutdownWithContext returns a failed report after 10ms, and only then "x" is released  | 0       |
  | 0       | grandchild "deep" of "c" | waits for ctx.Done(), then returns ctx.Err()       | deep.Shutdown()                                | deep.Shutdown() returns a successful report                                              | 1       |
```

#### Scenario: A child shutdown that begins in the step's HookBeforeStart
```gherkin
Given a started root container created with InjectorOpts{StartTimeout: <timeout>}, inside synctest.Test, and child scope "tenant" created after Start, with a HookScopeStarted hook counting its calls
And lazy Starter "w" in "tenant", registered after Start, whose Start(ctx) error records the cause of ctx <returns>
And a HookBeforeStart hook that, for "w", <shutdown>
When the consumer calls tenant.Start()
Then it returns nil
And "w"'s Start was called 1 time, with a context already cancelled with a cause matching ErrShutdown
And "w" is not recorded as started and HookScopeStarted did not run

Examples:
  | timeout | returns                                 | shutdown                                                                                                          |
  | 0       | then waits for ctx.Done()               | calls tenant.Shutdown() in another goroutine, which waits for the run                                             |
  | 0       | and returns nil at once                 | calls tenant.ShutdownWithContext with a 1s timeout, which gives up waiting for the run, removes "w" and completes |
  | 5s      | and returns nil at once                 | calls tenant.ShutdownWithContext with a 1s timeout, which gives up waiting for the run, removes "w" and completes |
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

#### Scenario: A dependent of a skipped step whose bounded scope shutdown already removed it
```gherkin
Given a started root container created with InjectorOpts{StartParallelism: <parallelism>}, inside synctest.Test, and child scope "c" created after Start
And lazy Starter "S" in "c", registered after Start, whose Start(ctx) error ignores ctx and blocks until released by the test
And "S" is exported to the root with ExportNamed, and <dependent>
And root.Refresh() is running in another goroutine and "S" has entered Start
When the consumer calls c.ShutdownWithContext with a 10ms timeout
Then it returns a failed report after 10ms, "S" is no longer registered in "c" and "c" is detached from the root
When "S" is released
Then root.Refresh() returns an error naming "P" that matches ErrShutdown
And "P"'s Start was called 0 times and "P" is not recorded as started or handled

Examples:
  | parallelism | dependent                                                                                                  |
  | 0           | lazy Starter "P" in the root, registered after Start, uses DependsOnNamed("S")                             |
  | 0           | lazy Starter "P" in the root, registered after Start, invokes "S" in its provider                          |
  | 2           | lazy Starter "P" in the root, registered after Start, uses DependsOnNamed("S")                             |
  | 2           | lazy Starter "P" in the root, registered after Start, invokes "S" in its provider                          |
  | 0           | lazy "P" in the root, registered after Start, a component of a StageAfterStart kind, uses DependsOnNamed("S") |
```

#### Scenario: An edge declared during the run to the export of a step a bounded shutdown removes
```gherkin
Given a started root container created with InjectorOpts{StartParallelism: <parallelism>}, inside synctest.Test, whose kind "wire" has a StageBeforeStart handler that calls DeclareDependency(root, "P", "S")
And child scope "c" created after Start, with lazy Starter "S", registered after Start, whose Start(ctx) error ignores ctx and blocks until released by the test, exported to the root with ExportNamed
And lazy Starter "P" in the root, registered after Start, depending on nothing at registration, and lazy "W" in the root, a component of "wire"
And root.Refresh() is running in another goroutine, the "wire" handler has declared the edge, and "S" has entered Start
When the consumer calls c.ShutdownWithContext with a 10ms timeout
Then it returns a failed report after 10ms and "c" is detached from the root
When "S" is released
Then root.Refresh() returns an error naming "P" that matches ErrShutdown
And "P"'s Start was called 0 times and "P" is not recorded as started

Examples:
  | parallelism |
  | 0           |
  | 2           |
```

#### Scenario: A dependent of an export whose scope a bounded shutdown removed
```gherkin
Given a started root container created with InjectorOpts{StartTimeout: <timeout>}, inside synctest.Test, whose kind "early" has a StageBeforeStart handler calling the component's Run, which ignores ctx and blocks until released by the test, and whose kind "late" has a StageAfterStart handler calling the component's Start
And child scope "c" created after Start, with lazy Starter "S", exported to the root with ExportNamed, which a first root.Refresh() started
And lazy "U" in "c", a component of "early", registered after that Refresh
And lazy <dependent> "P" in the root, registered after that Refresh, using DependsOnNamed("S") and depending on nothing else
And root.Refresh() is running in another goroutine and "U"'s handler has been called
When the consumer calls <shutdown>
And <then>
Then root.Refresh() returns <result>
And "P" was started <starts> times

Examples:
  | timeout | dependent                 | shutdown                                  | then                                                     | result                                                                                                                                                                              | starts |
  | 0       | Starter                   | c.ShutdownWithContext with a 10ms timeout | "U" is released once it returned a failed report         | an error matching ErrShutdown whose text is "doger: failed to start service `P` in scope `[root]`: dependency `S` (export of scope `c`) was removed by the scope's shutdown: container is shut down: scope `c` is shutting down" | 0      |
  | 0       | component of kind "late"  | c.ShutdownWithContext with a 10ms timeout | "U" is released once it returned a failed report         | an error matching ErrShutdown whose text is "doger: kind `late` failed for service `P` in scope `[root]`: dependency `S` (export of scope `c`) was removed by the scope's shutdown: container is shut down: scope `c` is shutting down" | 0      |
  | 5s      | Starter                   | c.ShutdownWithContext with a 10ms timeout | "U" is not released: its call is abandoned at 5s         | a *StartError naming "U" that matches ErrStartTimeout                                                                                                                               | 0      |
  | 0       | Starter                   | c.Shutdown() in another goroutine         | "U" is released while the shutdown waits for the run     | nil                                                                                                                                                                                  | 1      |
```

#### Scenario: A dependent reaching a skipped step through the source of a removed export
```gherkin
Given a started root container created with InjectorOpts{StartParallelism: <parallelism>}, inside synctest.Test, whose kind "late" has a StageAfterStart handler calling the component's Start, and child scope "c" created after Start
And lazy Starter "T" in <t-scope>, registered after Start, whose Start(ctx) error ignores ctx and blocks until released by the test
And lazy <source> "S" in "c", registered after Start, exported to the root with ExportNamed
And lazy <dependent> "P" in the root, registered after Start, using DependsOnNamed("S")
And root.Refresh() is running in another goroutine and "T" has entered Start
When the consumer calls <t-scope>.ShutdownWithContext with a 10ms timeout
Then it returns a failed report after 10ms and <t-scope> is detached from its parent
When "T" is released
Then root.Refresh() returns <result>
And "P"'s Start was called <starts> times

Examples:
  | parallelism | t-scope                  | source                                                                                                | dependent                | result                                  | starts |
  | 0           | "c"                      | plain service whose provider invokes "T"                                                              | Starter                  | an error naming "P" that matches ErrShutdown | 0 |
  | 2           | "c"                      | plain service whose provider invokes "T"                                                              | Starter                  | an error naming "P" that matches ErrShutdown | 0 |
  | 0           | "c"                      | Starter whose provider invokes "T"                                                                    | Starter                  | an error naming "P" that matches ErrShutdown | 0 |
  | 2           | "c"                      | Starter whose provider invokes "T"                                                                    | Starter                  | an error naming "P" that matches ErrShutdown | 0 |
  | 0           | "c"                      | plain service whose provider invokes "T", and a new scope "c" is created once the shutdown detached the first | Starter          | an error naming "P" that matches ErrShutdown | 0 |
  | 2           | "c"                      | plain service whose provider invokes "T", and a new scope "c" is created once the shutdown detached the first | Starter          | an error naming "P" that matches ErrShutdown | 0 |
  | 0           | grandchild "d" of "c"    | plain service that does not invoke "T" ("c", the export's source, stays live)                         | component of kind "late" | nil                                     | 1      |
```

#### Scenario: The later steps of a scope whose bounded shutdown completed are skipped
```gherkin
Given a started root container created with InjectorOpts{StartParallelism: <parallelism>}, inside synctest.Test, whose kind "late" has a StageAfterStart handler calling the component's Start, and whose kind "bare" has no handler
And child scope "c" created after Start, with lazy Starter "S", registered after Start, whose Start(ctx) error ignores ctx and blocks until released by the test
And <later> in "c", registered after "S"
And root.Refresh() is running in another goroutine and "S" has entered Start
When the consumer calls c.ShutdownWithContext with a 10ms timeout
Then it returns a failed report after 10ms and "c" is detached from the root
When "S" is released
Then root.Refresh() returns nil
And "T"'s Start was called 0 times, "T" is not recorded as started or handled, and the container keeps no passed-over kind record for the removed "T"

Examples:
  | parallelism | later                                                   |
  | 0           | lazy Starter "T"                                        |
  | 0           | lazy "T", a component of kind "late"                    |
  | 0           | lazy "T", a component of kind "bare", which the run passed over |
  | 2           | lazy Starter "T" depending on "S"                       |
```

#### Scenario: A root Shutdown that begins after a bounded scope shutdown completed reports the scope's pending steps
```gherkin
Given a started root container created with InjectorOpts{StartParallelism: <parallelism>}, inside synctest.Test
And child scope "c" created after Start, with lazy Starter "S", registered after Start, whose Start(ctx) error ignores ctx and blocks until released by the test
And lazy Starter "T" in "c", registered after "S", using DependsOnNamed("S")
And root.Refresh() is running in another goroutine and "S" has entered Start
When the consumer calls c.ShutdownWithContext with a 10ms timeout
Then it returns a failed report after 10ms and "c" is detached from the root
When root.Shutdown() is running in another goroutine, waiting for the run
And "S" is released
Then root.Refresh() returns <result>
And "T"'s Start was called 0 times

Examples:
  | parallelism | result                                                                                   |
  | 0           | an error matching ErrShutdown that is not a *StartError ("doger: start aborted: container is shut down") |
  | 2           | a *StartError naming "T" that matches ErrShutdown ("T" is still pending when scheduling ends) |
```

#### Scenario: A shutdown that has only closed the scope has not begun for the run
```gherkin
Given a started root container and child scope "c" created after Start, with a HookScopeStarted hook counting its calls for "c"
And lazy Starter "A" in "c", registered after Start, whose Start closes "c" as the first step of its shutdown does, and stops there
And lazy Starter "B" in "c", registered after Start, using DependsOnNamed("A")
When the consumer calls c.Start()
Then it returns nil
And "A" and "B" were each started 1 time
And the HookScopeStarted hook ran 1 time for "c"
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
<!-- cases: 7 lifecycle: Start, Refresh and Scope.Start rows; a run that begins during the shutdown; the run continues with scopes outside the closing subtree; dependents inside the subtree are skipped silently, outside it (through an export, by DependsOnNamed or by invocation) they are skipped and reported; a candidate the run dropped at discovery because its scope was already closing is not a skipped step: a dependent of it fails or succeeds by its own build, as before; Scope.Start skips the Started hooks of a scope whose own or ancestor shutdown began after the claim, an unbounded and a bounded shutdown, and keeps them for a descendant's shutdown ("Scope.Start skips the Started hooks of a scope whose shutdown cancelled its steps"); once root Shutdown began, a completed bounded scope shutdown no longer makes the run drop a pending Starter of that scope silently ("A root Shutdown that begins after a bounded scope shutdown completed reports the scope's pending steps") -->
<!-- cases: 8 concurrency: the subject; runs under -race with goleak; StartParallelism 4 rows; the registry of running step contexts is empty after every run (normal, panic, abandoned and cancelled steps) -->
<!-- cases: N/A 9 panics: hook panics during the wait follow scope-lifecycle-hooks -->
<!-- cases: 10 compatibility: BEHAVIOR CHANGE (flagged) - a child scope Shutdown now cancels the running start steps of its subtree, as root Shutdown cancels a whole run, so it no longer waits forever on a Start that stops only when its context ends; a step so cancelled no longer fails the run. BEHAVIOR CHANGE (flagged) - Scope.Start no longer runs the HookScopeStarted hooks when a shutdown of its scope, or of an ancestor, that began after it collected its services cancels them; they used to run, and the Closing hooks could run during or before them (scope-lifecycle-hooks, "Scope Started hooks skip a scope that is shutting down"). Earlier: a child scope Shutdown no longer races a running start; Start-time code that shuts down a scope the run is starting deadlocks; a grandchild created under a closing scope is refused with ErrShutdown -->

### Requirement: A run skips services shut down before it claims them
A Start, Refresh or Scope.Start SHALL NOT build, handle or start a service that its scope no longer holds when the run claims its candidates, such as one removed by a child scope shutdown that ran entirely after the run collected it. Such a service SHALL NOT fail the run, and a same-name service of an ancestor scope SHALL NOT be started again through it.

#### Scenario: A child shutdown completes between collection and claim
```gherkin
Given a started root container with <ancestor>
And child scope "tenant" with lazy Starter "w", registered after Start
And <run> is running in another goroutine and has collected "w", but has not claimed its candidates
When tenant.Shutdown() runs to completion
And <run> continues
Then <run> returns nil
And tenant's "w" was started 0 times
And the root's "w" was started <root_starts> times

Examples:
  | ancestor                     | run            | root_starts |
  | no service "w"               | root.Refresh() | 0           |
  | lazy Starter "w" in the root | root.Refresh() | 1           |
  | no service "w"               | tenant.Start() | 0           |
  | lazy Starter "w" in the root | tenant.Start() | 1           |
```

<!-- cases: N/A 1 default, 2 overrides: there is no option -->
<!-- cases: N/A 3 zero values: an empty scope has nothing to claim -->
<!-- cases: 4 boundaries: the shutdown completes after collection and before the claim; a shutdown still running at the claim is covered by "Child scope shutdown waits for a start that is starting its services" -->
<!-- cases: 5 errors: the run returns nil instead of a build error matching ErrServiceNotFound -->
<!-- cases: 6 duplicates: a same-name ancestor Starter is not started twice -->
<!-- cases: 7 lifecycle: Refresh and Scope.Start rows; Start cannot race a child scope created before it in this way without a test seam, and shares the claim -->
<!-- cases: 8 concurrency: the subject; the test pauses collection deterministically; runs under -race with goleak -->
<!-- cases: N/A 9 panics: none -->
<!-- cases: N/A 10 compatibility: fixes a race; no behavior that worked before changes -->

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

### Requirement: Incremental start uses the caller's context
`RefreshWithContext(ctx)` and `Scope.StartWithContext(ctx)` SHALL derive the run's context from ctx: its values SHALL reach Starters, kind handlers and scope Starting hooks, its cancellation SHALL abort the run with an error matching `context.Canceled`, and a nil ctx SHALL be treated as `context.Background()`.

#### Scenario: The caller's context reaches the run
```gherkin
Given a started root container and lazy Starter "a", registered after Start in <scope>, whose Start(ctx) records ctx
And component "j" of kind "k", registered with "a", whose StageBeforeStart handler records its context
And a HookScopeStarting hook recording its context
When the consumer calls <operation> with <ctx>
Then <result>

Examples:
  | scope                         | operation               | ctx                                   | result                                                                                          |
  | the root                      | RefreshWithContext      | a context holding value "v" for key k | it returns nil and "a" and the handler recorded a context holding "v" for k                      |
  | child scope "tenant"          | tenant.StartWithContext | a context holding value "v" for key k | it returns nil and "a", the handler and the Starting hook recorded a context holding "v" for k   |
  | the root                      | RefreshWithContext      | an already cancelled context          | it returns an error matching context.Canceled; "a" was started 0 times and the handler not called |
  | child scope "tenant"          | tenant.StartWithContext | an already cancelled context          | it returns an error matching context.Canceled; "a" was started 0 times and the handler not called |
  | the root                      | RefreshWithContext      | nil                                   | it returns nil and "a" and the handler recorded a non-nil context                                |
  | child scope "tenant"          | tenant.StartWithContext | nil                                   | it returns nil and "a" and the handler recorded a non-nil context                                |
```

<!-- cases: 1 default: Refresh() and Scope.Start() use context.Background() -->
<!-- cases: 2 overrides: the caller's context is the subject -->
<!-- cases: 3 zero values: the nil rows -->
<!-- cases: N/A 4 boundaries: one Starter per row -->
<!-- cases: 5 errors: the cancelled rows -->
<!-- cases: N/A 6 duplicates: none -->
<!-- cases: 7 lifecycle: Refresh and Scope.Start rows -->
<!-- cases: N/A 8 concurrency: covered by "Concurrent Refresh calls are serialized" -->
<!-- cases: N/A 9 panics: none -->
<!-- cases: N/A 10 compatibility: new API -->

### Requirement: A child shutdown wait is bounded by its context
If the context of a child scope's `ShutdownWithContext` ends while it waits for a running start, the report SHALL fail with an error keyed by the scope with an empty service name wrapping `ctx.Err()`, and the shutdown SHALL proceed with that ended context: the services it then reaches SHALL be skipped and reported as `shutdown-context` specifies ("Services reached after the shutdown context ended are skipped").

#### Scenario: A child shutdown bounded by its context
```gherkin
Given a started root container inside synctest.Test and child scope "tenant" with lazy Starter "w", registered after Start, whose Start blocks until released and whose Shutdown() error records "shutdown"
And root.Refresh() is running in another goroutine and "w" has entered Start
When the consumer calls tenant.ShutdownWithContext(ctx) with a 1s timeout
Then it returns after 1s of bubble time with a failed report
And the report has an error keyed by scope "tenant" with an empty service name, matching context.DeadlineExceeded
And the report has an error for "w" in scope "tenant", matching context.DeadlineExceeded
And once "w" is released and Refresh returned, "shutdown" was never recorded and the start bookkeeping holds no entry for "w"
```

<!-- cases: 1 default: Shutdown() uses context.Background() and waits until the run returns -->
<!-- cases: 2 overrides: the context deadline -->
<!-- cases: N/A 3 zero values: a nil context means context.Background() -->
<!-- cases: N/A 4 boundaries: one deadline -->
<!-- cases: 5 errors: the context error in the report, for the scope and for the service the ended shutdown skipped -->
<!-- cases: N/A 6 duplicates, 9 panics: as for the wait -->
<!-- cases: 7 lifecycle: the shutdown proceeds with the ended context while the run is still running, so it skips the service still starting -->
<!-- cases: 8 concurrency: synctest bubble time; runs under -race with goleak -->
<!-- cases: 10 compatibility: the behavior is unchanged; the requirement now states that the services reached are skipped -->

### Requirement: A run checks its context between scheduling steps
`Start`, `Refresh` and `Scope.Start` SHALL check the run's context before each kind-handler call and, under parallel start, before scheduling each Starter, as `StartWithContext` does before each candidate build. Once the caller's context is cancelled, no further handler call or Starter SHALL begin, even when every call so far succeeded, and the run SHALL fail with an error matching the context's error.

#### Scenario: A cancel between two steps
```gherkin
Given a root scope, with <setup>, registered <when>
And the consumer passes ctx to <operation>
When <canceller> cancels ctx and returns nil
Then the operation returns an error matching context.Canceled
And <skipped> did not begin

Examples:
  | setup                                                                      | when         | operation                   | canceller                     | skipped                        |
  | kind "k" with a StageBeforeStart handler and components "j1" then "j2"     | before Start | StartWithContext(ctx)       | the handler, for "j1"         | the handler call for "j2"      |
  | kind "k" with a StageBeforeStart handler and components "j1" then "j2"     | after Start  | RefreshWithContext(ctx)     | the handler, for "j1"         | the handler call for "j2"      |
  | StartParallelism 2 and Starters "a", "b" (returns once "a" returned), "c"  | before Start | StartWithContext(ctx)       | "a"'s Start                   | "c"'s Start                    |
  | StartParallelism 2 and Starters "a", "b" (returns once "a" returned), "c"  | after Start  | RefreshWithContext(ctx)     | "a"'s Start                   | "c"'s Start                    |
```

<!-- cases: 1 default: the checks always run -->
<!-- cases: N/A 2 overrides: the caller's context is the control -->
<!-- cases: N/A 3 zero values: a nil context is context.Background() and never cancels -->
<!-- cases: 4 boundaries: the cancel lands between two components of one kind, and between two scheduling steps -->
<!-- cases: 5 errors: context.Canceled -->
<!-- cases: N/A 6 duplicates: none -->
<!-- cases: 7 lifecycle: Start and Refresh rows -->
<!-- cases: 8 concurrency: the parallel scheduler rows; runs under -race -->
<!-- cases: N/A 9 panics: none -->
<!-- cases: N/A 10 compatibility: the checks existed; this pins them -->

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

#### Scenario: A dependent skipped for a still-running step stays skipped once that step ends
```gherkin
Given a started root container created with InjectorOpts{StartTimeout: 1s, StartParallelism: <parallelism>}, inside synctest.Test
And lazy Starter "S" in the root, registered after Start, whose first call blocks until released, and root.Refresh() returned an error naming "S" that matches ErrStartTimeout
And child scope "c" with, registered in this order, <kind> "D" using DependsOnNamed("S"), <kind> "X" whose call releases "S" and waits until its late return is recorded, and <kind> "E" using DependsOnNamed("D")
When the consumer calls c.Start()
Then "X" was called 1 time, and "D" and "E" 0 times
And c.Start() returns an error naming "S" that matches ErrStartTimeout

Examples:
  | parallelism | kind                                    |
  | 0           | lazy Starter                            |
  | 2           | lazy Starter                            |
  | 0           | lazy component of a StageAfterStart kind |
```

#### Scenario: A dependent of a closing scope stays silent when the scope's shutdown completes during the run
```gherkin
Given a started root container, inside synctest.Test, and child scope "c" created after Start, with a HookScopeStarted hook counting its calls
And lazy Starter "W" in "c", registered after Start, whose Start(ctx) waits for ctx.Done(), and lazy Starter "D" in "c" using DependsOnNamed("W")
And c.Start() is running in another goroutine and "W" has entered Start
When the consumer calls c.ShutdownWithContext with a 1s timeout in another goroutine
And that shutdown gives up waiting for the run and completes <when>
Then c.Start() returns nil
And "D" was started 0 times and the HookScopeStarted hook ran 0 times

Examples:
  | when                                                                                     |
  | once the run found that "D" reaches the skipped "W", before it decided whether c is closing |
  | once the run returned                                                                    |
```

#### Scenario: A dependent of a closing scope that reaches a still-running step is skipped silently
```gherkin
Given a started root container created with InjectorOpts{StartTimeout: 1s}, inside synctest.Test
And lazy Starter "S" in the root, registered after Start, whose first call blocks until released, and root.Refresh() returned an error naming "S" that matches ErrStartTimeout
And child scope "c" with lazy Starter "W", whose Start(ctx) waits for ctx.Done(), and lazy Starter "D" using DependsOnNamed("S")
And c.Start() is running in another goroutine and "W" has entered Start
When the consumer calls c.Shutdown() in another goroutine
Then c.Start() returns nil and "D" was started 0 times
And c.Shutdown() returns a successful report
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
