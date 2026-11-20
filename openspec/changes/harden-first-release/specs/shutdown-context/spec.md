# Spec Delta

## MODIFIED Requirements

### Requirement: Services reached after the shutdown context ended are skipped
When `ShutdownWithContext` (root or child scope) reaches a built Shutdowner after its context ended, it SHALL NOT call its `Shutdown` method, and the report SHALL hold an error for it matching `ctx.Err()`. Any other service reached SHALL get no error. Every service reached SHALL be removed from its scope, and the `HookBeforeShutdown` and `HookAfterShutdown` hooks SHALL still run once for it, the latter with its report error (nil when none).

#### Scenario: A service reached after the context ended is skipped
```gherkin
Given a root scope with child scope "tenant", with one HookBeforeShutdown and one HookAfterShutdown hook recording the service names and the AfterShutdown error
And <service> registered as "s" in "tenant", counting its Shutdown calls
When the consumer calls tenant.ShutdownWithContext(ctx), with ctx <ctx>
Then "s" had its Shutdown called <calls> times
And the report <report>
And the BeforeShutdown and AfterShutdown hooks each saw "s" once, the AfterShutdown hook with the report's error for "s" (nil when it has none)
And doger.ShutdownNamed(tenant, "s") then returns an error matching ErrServiceNotFound

Examples:
  | service                                                                  | ctx                                   | calls | report                                                                      |
  | a built lazy service implementing Shutdown() error                       | cancelled before the call             | 0     | fails, with an error for "s" in scope "tenant" matching context.Canceled    |
  | a built lazy service implementing Shutdown(context.Context) error        | cancelled before the call             | 0     | fails, with an error for "s" in scope "tenant" matching context.Canceled    |
  | a value service (ProvideNamedValue) implementing Shutdown() error        | cancelled before the call             | 0     | fails, with an error for "s" in scope "tenant" matching context.Canceled    |
  | a built lazy service implementing Shutdown() error                       | with a 1ns deadline that has expired  | 0     | fails, with an error for "s" in scope "tenant" matching context.DeadlineExceeded |
  | a lazy service implementing Shutdown() error, never invoked              | cancelled before the call             | 0     | succeeds                                                                    |
  | a built lazy service implementing no Shutdowner interface (calls: 0 by definition) | cancelled before the call   | 0     | succeeds                                                                    |
  | an alias (AliasNamed) to a built lazy Shutdowner registered in the root (calls: the target's) | cancelled before the call | 0 | succeeds                                                  |
  | a transient service implementing Shutdown() error, invoked once          | cancelled before the call             | 0     | succeeds                                                                    |
  | a built lazy service implementing Shutdown() error                       | context.Background()                  | 1     | succeeds                                                                    |
```

#### Scenario: An exported service reached after the context ended is skipped
```gherkin
Given a root scope with child scope "tenant"
And a built lazy "s" in "tenant" implementing Shutdown() error, counting its calls, exported to the root with doger.ExportNamed
And ctx cancelled before the call
When the consumer calls root.ShutdownWithContext(ctx)
Then "s" had its Shutdown called 0 times
And the report fails, with an error for "s" in scope "tenant" matching context.Canceled
And doger.ShutdownNamed(tenant, "s") then returns an error matching ErrServiceNotFound
```

#### Scenario: A deadline that ends during the shutdown skips the services after it
```gherkin
Given a root scope inside synctest.Test
And a built lazy "b" implementing Shutdown() error, counting its calls
And a built lazy "a" invoking "b", whose Shutdown() error sleeps 2s, then returns nil
When the consumer calls root.ShutdownWithContext(ctx), with ctx having a 1s timeout
Then the call returns after 1s
And "a" had its Shutdown called 1 time, and the report has an error for "a" matching context.DeadlineExceeded
And "b" had its Shutdown called 0 times, and the report has an error for "b" matching context.DeadlineExceeded
And after "a"'s Shutdown returned at 2s, "b" still had its Shutdown called 0 times
```

<!-- cases: 1 default: context.Background() never ends, so every service is shut down (last row) -->
<!-- cases: 2 overrides: the consumer controls the context; a still-live context with enough time shuts everything down -->
<!-- cases: 3 zero values: a service never built, implementing no Shutdowner interface, an alias or a transient service has nothing to shut down and reports no error (an alias never shuts its target down); a nil context is context.Background() (covered by container-start "Root Shutdown runs once") -->
<!-- cases: 4 boundaries: cancelled before the call, an expired deadline, a deadline ending between two services; a root that a child exports into, which shuts down through the export path -->
<!-- cases: 5 errors: the context error is reported under each skipped Shutdowner's name, and passed to the AfterShutdown hook -->
<!-- cases: lifecycle hooks: HookBeforeShutdown and HookAfterShutdown still run once for every service reached, skipped or not (inherited, documented) -->
<!-- cases: 6 duplicates: a skipped service is dropped, so a later shutdown of it finds nothing (ErrServiceNotFound) -->
<!-- cases: 7 lifecycle: a service whose Start still runs after a timed-out start wait is covered by container-start "A child shutdown wait is bounded by its context"; a service still shutting down when the context ends is covered by "Shutdown returns when its context ends" -->
<!-- cases: 8 concurrency: the mid-shutdown deadline runs in a synctest bubble under -race -->
<!-- cases: N/A 9 panics: a skipped service runs no user code -->
<!-- cases: 10 compatibility: BEHAVIOR CHANGE (flagged) - the third scenario used to wait for "a" to finish (2s) and report no error for it; the call now returns when the context ends, reporting "a" as abandoned; the alias row uses the renamed AliasNamed -->

### Requirement: The signal helper's context only ends the wait for a signal
The context passed to `RootScope.ShutdownOnSignalsWithContext` SHALL only end the wait for a signal, and a nil context SHALL behave as `context.Background()`. Whichever comes first, a signal or the context's end, the call SHALL shut down with a context that keeps that context's values without its cancellation or deadline, as `ShutdownOnSignals` does, and the Closing and Closed hooks SHALL receive it. It SHALL return the signal, or nil when the context ended first.

#### Scenario: The signal helper shuts down when its context ends
```gherkin
Given a root scope with a built lazy "s" implementing Shutdown(context.Context) error, recording ctx.Err() and the value of ctx under the test key
And one hook registered for each of Closing and Closed, recording ctx.Err() and the value of ctx under the test key
And a context carrying the test key = "r-1", <ending>
When the consumer calls root.ShutdownOnSignalsWithContext(ctx) with no signal argument
Then the returned signal is nil
And the report succeeds
And "s" had its Shutdown called 1 time, with a context whose Err() is nil and whose test key value is "r-1"
And the Closing and Closed hooks each ran once, with a context whose Err() is nil and whose test key value is "r-1"

Examples:
  | ending                                           |
  | cancelled before the call                        |
  | with a 10ms timeout that expires while it waits  |
```

#### Scenario: The signal helper shuts down after a signal even when its context ends with it
```gherkin
Given a unix test process with a guard signal.Notify registered for SIGUSR1
And a root scope with a built lazy "s" implementing Shutdown(context.Context) error, recording ctx.Err()
And one hook registered for each of Closing and Closed, recording ctx.Err()
And ctx from signal.NotifyContext(context.Background(), SIGUSR1)
When the consumer calls root.ShutdownOnSignalsWithContext(ctx, SIGUSR1) and SIGUSR1 is sent to the process until the call returns
Then the returned signal is SIGUSR1 or nil
And the report succeeds
And "s" had its Shutdown called 1 time, with a context whose Err() is nil
And the Closing and Closed hooks each ran once, with a context whose Err() is nil
And this holds in each of 50 sequential repeats
```

#### Scenario: The signal helper accepts a nil context
```gherkin
Given a unix test process with a guard signal.Notify registered for SIGUSR1
And a root scope with a built lazy "s" implementing Shutdown(context.Context) error, recording ctx.Err()
When the consumer calls root.ShutdownOnSignalsWithContext(nil, SIGUSR1) on goroutine G and SIGUSR1 is sent to the process until G returns
Then G does not panic
And the returned signal is SIGUSR1
And the report succeeds
And "s" had its Shutdown called 1 time, with a context whose Err() is nil
```

<!-- cases: 1 default: ShutdownOnSignals passes context.Background(), unchanged -->
<!-- cases: 2 overrides: to bound the shutdown, the consumer waits for the signal itself (signal.NotifyContext) and calls ShutdownWithContext with its own deadline; documented on the Shutdowner page -->
<!-- cases: 3 zero values: a nil context waits for a signal like context.Background() (new scenario; regression for the nil-pointer panic) -->
<!-- cases: 4 boundaries: a context ended before the call, one ending while the call waits, and one ending right after a signal (NotifyContext for the same signal) -->
<!-- cases: N/A 5 errors: the shutdown reports service errors as ShutdownWithContext does -->
<!-- cases: N/A 6 duplicates: root shutdown runs once (container-start) -->
<!-- cases: 7 lifecycle: Closing and Closed hooks still run once (container-lifecycle-hooks "Closing and Closed hooks bracket the root shutdown"), now with the context that has no cancellation ("Hook contexts" holds: the helper passes it to ShutdownWithContext) -->
<!-- cases: 8 concurrency: the signal and the context's end race the select (and NotifyContext cancels right after the signal); the signal scenarios repeat under -race, resending SIGUSR1 until the call returns -->
<!-- cases: 9 panics: a nil context used to panic with a nil-pointer dereference after registering the signal handler; it no longer panics -->
<!-- cases: 10 compatibility: BEHAVIOR CHANGE (flagged) - when the context ended first, the call used to shut nothing down, return a report of context errors, and give the Closing and Closed hooks the ended context; after a signal, the shutdown used to be bounded and cancelled by the context, skipping services once it ended. It is now unbounded in both cases. A nil context used to panic -->
<!-- cases: platform: the signal scenarios run on unix only (syscall.Kill, SIGUSR1); CI runs them on ubuntu and macos -->

## ADDED Requirements

### Requirement: Shutdown returns when its context ends
`ShutdownWithContext` on a root or child scope SHALL return once its context ends, even while a service's `Shutdown` still runs, reporting each such service with a `*ServiceError` whose `Err` matches `ctx.Err()`. That `Shutdown` SHALL keep running in the background, its `HookAfterShutdown` hooks running when it returns; a later panic SHALL be logged at Error level (container-shutdown). Each service SHALL appear once in the report and never be shut down again by a later layer. With a context that never ends, the call SHALL wait for every service.

#### Scenario: A hung Shutdown does not hold the call past its deadline
```gherkin
Given a root scope inside synctest.Test with child scope "tenant"
And a built lazy "stuck" in <scope> implementing <shutdowner> that blocks until the test closes channel release
And a built lazy "ok" in <scope> implementing Shutdown() error, counting its calls
And a HookAfterShutdown hook recording the service name and error
When the consumer calls <operation> with ctx having a 50ms timeout
Then the call returns after 50ms
And the report fails, with a *ServiceError for "stuck" in <scope> whose Err matches context.DeadlineExceeded
And "ok" had its Shutdown called 1 time and has no error in the report
And the AfterShutdown hook has not yet seen "stuck"
And after the test closes release, the AfterShutdown hook saw "stuck" once, with a nil error
And no goroutine is leaked

Examples:
  | scope    | shutdowner                       | operation                        |
  | the root | Shutdown()                       | root.ShutdownWithContext         |
  | the root | Shutdown() error                 | root.ShutdownWithContext         |
  | the root | Shutdown(context.Context)        | root.ShutdownWithContext         |
  | the root | Shutdown(context.Context) error  | root.ShutdownWithContext         |
  | "tenant" | Shutdown() error                 | tenant.ShutdownWithContext       |
  | "tenant" | Shutdown() error                 | root.ShutdownWithContext         |
```

#### Scenario: A slow Shutdown is waited for when the context allows it
```gherkin
Given a root scope inside synctest.Test with a built lazy "slow" whose Shutdown() error sleeps <sleep>, then returns nil
When the consumer calls root.ShutdownWithContext(<ctx>)
Then the call returns after <sleep>
And the report succeeds

Examples:
  | sleep | ctx                                    |
  | 2s    | context.Background()                   |
  | 2s    | a context with a 3s timeout            |
  | 0s    | a context with a 1ns timeout, not yet expired at the call |
```

<!-- cases: 1 default: context.Background() waits as long as it takes (row 1 of the second scenario) -->
<!-- cases: 2 overrides: the consumer's deadline is the override; a deadline longer than the shutdown changes nothing -->
<!-- cases: N/A 3 zero values: a nil context is context.Background() (container-start "Root Shutdown runs once") -->
<!-- cases: 4 boundaries: a deadline shorter and longer than the slow service; a shutdown that ends exactly with nothing left to wait for -->
<!-- cases: 5 errors: abandoned services are *ServiceError values matching ctx.Err(), so errors.Is(report.Err(), context.DeadlineExceeded) holds -->
<!-- cases: N/A 6 duplicates: an abandoned service is not shut down again -->
<!-- cases: 7 lifecycle: root and child scope, and a child reached through the root; the root phase still reaches PhaseClosed when the call returns -->
<!-- cases: 8 concurrency: synctest bubble under -race; goleak after release -->
<!-- cases: N/A 9 panics: a panic in the abandoned Shutdown after the call returned is recovered and logged (container-shutdown) -->
<!-- cases: 10 compatibility: BEHAVIOR CHANGE (flagged) - the call used to block until every Shutdown returned, past its deadline -->

### Requirement: Health hooks of a bounded shutdown run off the caller's goroutine
Removing a service from its scope during a shutdown SHALL run no user code on the caller's goroutine: a service's health bookkeeping (its health hooks) SHALL run on that service's shutdown goroutine, recovered like any shutdown step, so a blocking or panicking health hook neither holds the call past its context nor ends the process.

#### Scenario: Health hooks run per service during a bounded shutdown
```gherkin
Given a started root scope with a manual HealthScheduler and child scope "tenant" holding an unhealthy Healthchecker "h"
And a HookContainerHealthChanged hook that <behaves> on the change the removal of "h" causes
When the consumer calls tenant.ShutdownWithContext(ctx) with ctx <ctx>
Then <outcome>

Examples:
  | behaves                                                        | ctx                  | outcome                                                    |
  | blocks until the test releases it                              | having a 50ms timeout | the call returns within 50ms                               |
  | panics, with a HookContainerPanic that re-panics the value     | context.Background()  | the process does not crash; the panic reaches the caller   |
```

<!-- cases: 1 default: context.Background() with a panicking hook (row 2) -->
<!-- cases: 2 overrides: a consumer deadline (row 1) and a re-panicking HookContainerPanic (row 2) -->
<!-- cases: N/A 3 zero values: no input -->
<!-- cases: N/A 4 boundaries: one health-checked service is enough to place the hook -->
<!-- cases: N/A 5 errors: the hook returns nothing -->
<!-- cases: N/A 6 duplicates: unrelated -->
<!-- cases: 7 lifecycle: a child scope shut down while the root stays started -->
<!-- cases: 8 concurrency: a blocking hook does not hold the call past its deadline -->
<!-- cases: 9 panics: a panicking hook is recovered like any shutdown step and reaches the caller only through HookContainerPanic -->
<!-- cases: 10 compatibility: part of the BEHAVIOR CHANGE of "Shutdown returns when its context ends" -->

### Requirement: Shutdown does not wait past its context for a build in progress
When a scope or a single service (`ShutdownNamedWithContext`) shuts down while a provider of one of its services is still running, the call SHALL stop waiting for that build once its context ends and report the service with an error matching `ctx.Err()`. When the provider returns later, its instance is late: it SHALL NOT be handed out, and the invoker SHALL get an error matching `ErrShutdown` at once, never waiting for the instance's `Shutdown`.

#### Scenario: A stuck provider does not hold a bounded shutdown
```gherkin
Given a root scope inside synctest.Test
And a lazy "conn" implementing Shutdown() error, counting its calls, whose provider blocks until the test closes channel release, then returns <result>
And goroutine G invokes "conn" and is blocked in the provider
When the consumer calls root.ShutdownWithContext(ctx) with ctx having a 50ms timeout
Then the call returns after 50ms
And the report fails, with a *ServiceError for "conn" whose Err matches context.DeadlineExceeded
And after the test closes release, G receives a zero instance and an error matching <g error>
And the instance had its Shutdown called <calls> times
And no goroutine is leaked

Examples:
  | result                          | g error                    | calls |
  | a new instance and a nil error  | ErrShutdown                | 1     |
  | a nil instance and io.EOF       | io.EOF                     | 0     |
```

#### Scenario: An unbounded shutdown waits for the build
```gherkin
Given a root scope inside synctest.Test
And a lazy "conn" implementing Shutdown() error, counting its calls, whose provider sleeps 1s, then returns a new instance
And goroutine G invokes "conn" and is blocked in the provider
When the consumer calls root.ShutdownWithContext(context.Background())
Then the call returns after 1s
And the report succeeds
And G receives an error matching ErrShutdown
And the instance had its Shutdown called 1 time, before the call returned
```

#### Scenario: ShutdownNamedWithContext honors its bound with a build in flight
```gherkin
Given a root scope inside synctest.Test with a lazy "conn" implementing Shutdown() error, counting its calls
And <caller> calls ShutdownNamedWithContext(ctx, injector, "conn") with ctx having a 50ms timeout <when>
When the provider of "conn" returns a new instance
Then the call returned after <took> with <error>
And the invoker of "conn" receives an error matching ErrShutdown
And the instance had its Shutdown called 1 time

Examples:
  | caller                 | when                                             | took | error                                |
  | another goroutine      | while the provider blocks until released         | 50ms | an error matching DeadlineExceeded   |
  | the provider of "conn" | from inside the provider, before it returns      | 0s   | nil                                  |
```

#### Scenario: The invoker is never held by a late Shutdown
```gherkin
Given a root scope inside synctest.Test with a lazy "conn" whose instance's Shutdown(ctx) waits for ctx to end, or 1h
And goroutine G invokes "conn", whose provider blocks until released
When the consumer calls <shutdown> with ctx having a 50ms timeout <timing>, and then releases the provider
Then G returns an error matching ErrShutdown at once
And the instance had its Shutdown called 1 time, with the shutdown's ended context

Examples:
  | shutdown                                   | timing                       |
  | root.ShutdownWithContext(ctx)              | before the deadline          |
  | ShutdownNamedWithContext(ctx, root, "conn") | before the deadline          |
  | root.ShutdownWithContext(ctx)              | 60ms after it was created    |
```

<!-- cases: 1 default: context.Background() waits for the build (second scenario) -->
<!-- cases: 2 overrides: the consumer's deadline -->
<!-- cases: N/A 3 zero values: no input -->
<!-- cases: 4 boundaries: a provider that succeeds late and one that fails late -->
<!-- cases: 5 errors: the abandoned build is a *ServiceError matching ctx.Err(); the invoker gets ErrShutdown, or the provider's own error when it failed -->
<!-- cases: 6 duplicates: a late instance is shut down exactly once, and never cached -->
<!-- cases: 7 lifecycle: the build began before the shutdown; a build that would begin after it is refused (container-shutdown "A shut-down service is never built again") -->
<!-- cases: 8 concurrency: the invoker and the shutdown run on different goroutines in a synctest bubble under -race; goleak after release -->
<!-- cases: N/A 9 panics: a panicking late provider gives the invoker ErrProviderPanic (provider-panic-handling) and nothing to shut down -->
<!-- cases: 10 compatibility: BEHAVIOR CHANGE (flagged) - a bounded shutdown used to wait for the provider past its deadline, and the late instance was handed out and cached -->

### Requirement: A late instance is shut down exactly once by one owner
A late Shutdowner instance SHALL be shut down exactly once, on a goroutine of its own, with the original context of the shutdown that made the service Terminal, even when that context has ended. Exactly one party SHALL own it: the shutdown still waiting for the build, which reports the instance's error or panic (matching `ErrShutdownPanic`) under the service before the services it depends on shut down; otherwise the build, whose goroutine logs that error or panic once.

#### Scenario: The late instance is shut down by the shutdown, in dependency order
```gherkin
Given a root scope inside synctest.Test with lazy "db" implementing Shutdown() error that logs "db"
And a lazy "conn" whose provider invokes "db", then blocks until the test closes channel release, and whose instance's Shutdown() sleeps 100ms, logs "conn" and <outcome>
And goroutine G invokes "conn" and is blocked in the provider
When the consumer calls root.ShutdownWithContext(context.Background()) and the test then closes release
Then when the call returns, the log is "conn", "db"
And the report has an error for "conn" <matching>
And G receives an error matching ErrShutdown

Examples:
  | outcome                                  | matching                                              |
  | returns errors.New("conn close failed")  | containing "conn close failed"                        |
  | panics with "conn boom"                  | matching ErrShutdownPanic and containing "conn boom"  |
```

<!-- cases: 1 default: an unbounded shutdown owns the late instance (this scenario); a bounded one that gave up leaves it to the build ("A stuck provider does not hold a bounded shutdown", row 1) -->
<!-- cases: N/A 2 overrides: the owner is not configurable -->
<!-- cases: N/A 3 zero values: no input -->
<!-- cases: 4 boundaries: a late Shutdown that returns an error and one that panics -->
<!-- cases: 5 errors: the error or panic is reported under "conn"; a panic matches ErrShutdownPanic -->
<!-- cases: 6 duplicates: a late instance is shut down exactly once, and never cached -->
<!-- cases: 7 lifecycle: "conn" shuts down before "db", which it depends on; the original context is used even once ended ("The invoker is never held by a late Shutdown") -->
<!-- cases: 8 concurrency: the invoker and the shutdown run on different goroutines in a synctest bubble under -race -->
<!-- cases: 9 panics: a panicking late Shutdown is reported, matching ErrShutdownPanic -->
<!-- cases: 10 compatibility: BEHAVIOR CHANGE (flagged) - the late instance used to be handed out and cached -->

### Requirement: A provider that shuts its own service down does not wait for itself
A provider that calls `ShutdownNamed*` for its own service on its own goroutine SHALL NOT wait for its own build: the call returns nil at once, and the build owns the late instance. Non-goals, documented: a provider that shuts down a scope or root containing its own service (`Shutdown()`, `ShutdownWithContext` with a context that never ends), or that waits on another goroutine shutting its service down, waits for its own build and blocks, as it did before this change.

#### Scenario: A provider shutting its own service down does not wait
```gherkin
Given a root scope with a lazy "conn" whose provider calls ShutdownNamed(injector, "conn") before it returns an instance counting its Shutdown calls
When the consumer invokes "conn"
Then ShutdownNamed returned nil
And the invocation returns an error matching ErrShutdown
And the instance had its Shutdown called 1 time
```

<!-- cases: 1 default: ShutdownNamed without a context (this scenario) -->
<!-- cases: 2 overrides: ShutdownNamedWithContext from inside the provider ("ShutdownNamedWithContext honors its bound with a build in flight", row 2) -->
<!-- cases: N/A 3 zero values: no input -->
<!-- cases: N/A 4 boundaries: one re-entrant call -->
<!-- cases: 5 errors: the call returns nil; the invoker gets ErrShutdown -->
<!-- cases: 6 duplicates: the late instance is shut down exactly once -->
<!-- cases: 7 lifecycle: a re-entrant shutdown from inside the provider; the non-goals above keep blocking -->
<!-- cases: 8 concurrency: the call runs on the provider's goroutine, so waiting would deadlock -->
<!-- cases: N/A 9 panics: none -->
<!-- cases: 10 compatibility: the non-goals keep their previous blocking behavior -->
