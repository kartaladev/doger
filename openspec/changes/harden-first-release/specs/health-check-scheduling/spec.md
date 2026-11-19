# Spec Delta

## MODIFIED Requirements

### Requirement: The scheduler is called once when the container becomes ready
A root container with a `HealthScheduler` SHALL call `ScheduleHealthRounds(clock, round)` exactly once, with the injector clock as a `doger.Clock`, when it reaches `PhaseReady`, before the Ready hooks run. A container that never reaches `PhaseReady` SHALL NOT call it. While the scheduler is scheduling, reports SHALL have `Monitored` true.

#### Scenario: The scheduler is called only at Ready
```gherkin
Given an injector with a manual HealthScheduler, a Clock C := dogerclockwork.Wrap(clockwork.NewFakeClock()), and a Ready hook that records the scheduler's call count
When the consumer <action>
Then ScheduleHealthRounds was called <calls> times, with a doger.Clock equal to C when called
And the Ready hook saw <ready calls> calls
And HealthReport(IgnoreContainerPhase()) on the root scope has Monitored <monitored>

Examples:
  | action                                                                 | calls | ready calls | monitored |
  | calls New only                                                         | 0     | (not run)   | false     |
  | calls Start, which succeeds                                            | 1     | 1           | true      |
  | calls Start, which fails because a Starter returns errors.New("boom")  | 0     | (not run)   | false     |
  | calls Shutdown while Start is blocked in a Starter, then releases it   | 0     | (not run)   | false     |
  | calls Start, which succeeds, then Refresh twice                        | 1     | 1           | true      |
```

#### Scenario: The scheduler receives the real clock by default
```gherkin
Given an injector with a manual HealthScheduler and InjectorOpts.Clock <clock>
When the consumer calls Start, which succeeds
Then ScheduleHealthRounds received a doger.Clock that <received>

Examples:
  | clock                                                 | received                                                    |
  | nil (default)                                         | equals doger.RealClock()                                    |
  | dogerclockwork.Wrap(clockwork.NewFakeClock())         | dogerclockwork.Unwrap returns that fake clock with ok true |
```

#### Scenario: A scheduler that fails leaves the container unmonitored
```gherkin
Given an injector whose HealthScheduler's ScheduleHealthRounds <fails>, with HookContainerPanic <panic hook> and a Logger whose handler records every record
When the consumer calls Start
Then Start returns nil and Phase() is PhaseReady
And HealthReport(IgnoreContainerPhase()) on the root scope has Monitored false
And <observed>

Examples:
  | fails                                | panic hook                    | observed                                                                                                  |
  | returns errors.New("bad cron")       | nil                           | the handler recorded an Error record whose message contains "health scheduler" with an error attribute "bad cron" |
  | panics with "boom"                   | nil                           | the handler recorded an Error record whose message contains "health scheduler", and the value "boom" only in a Debug record |
  | panics with "boom"                   | set, recording (point, value) | HookContainerPanic recorded ("healthscheduler", "boom")                                                   |
```

<!-- cases: 1 default: no scheduler (previous requirement); the real clock when Clock is nil -->
<!-- cases: 2 overrides: a scheduler set; a wrapped clockwork fake clock -->
<!-- cases: N/A 3 zero values: covered by the previous requirement -->
<!-- cases: N/A 4 boundaries: not applicable -->
<!-- cases: 5 errors: an error or a panic from the scheduler does not fail Start -->
<!-- cases: 6 not found/duplicate: called once per container, even after Refresh; a second Start returns ErrAlreadyStarted as in container-start -->
<!-- cases: 7 lifecycle: idle, failed, shutdown during start, ready, refresh -->
<!-- cases: N/A 8 concurrency: the call happens on Start's goroutine, outside the lifecycle lock -->
<!-- cases: 9 panics: a scheduler panic is recovered; a panic raised by HookContainerPanic propagates from Start (next requirement); its value is logged at Debug only (container-logging) -->
<!-- cases: 10 backward compatibility: BREAKING - ScheduleHealthRounds takes a doger.Clock instead of a clockwork.Clock; implementers change the parameter type and use dogerclockwork.Unwrap when they need a clockwork clock. Logf is replaced by Logger (container-logging) -->

### Requirement: A stop returned with a scheduler error is called
When `ScheduleHealthRounds` returns an error together with a stop function, the container SHALL stay unmonitored and SHALL call that stop once, before Start returns, with `context.Background()`. Its error SHALL be logged at Error level, and its panic handled as for any stop. Shutdown SHALL NOT call it again.

#### Scenario: A stop returned with an error is called at once
```gherkin
Given an injector whose HealthScheduler's ScheduleHealthRounds returns a stop function that counts its calls, together with errors.New("partial"), and a Logger whose handler records every record
When the consumer calls Start, then Shutdown
Then Start returns nil and HealthReport(IgnoreContainerPhase()) on the root scope has Monitored false
And the handler recorded an Error record whose message contains "health scheduler" with an error attribute "partial"
And the stop was called 1 time before Start returned, and 1 time in total after Shutdown
```

<!-- cases: N/A 1 default: a scheduler returns a nil stop with its error -->
<!-- cases: 2 overrides: a third-party HealthScheduler returning both -->
<!-- cases: N/A 3 zero values: a nil stop with an error is the previous requirement -->
<!-- cases: N/A 4 boundaries: not applicable -->
<!-- cases: 5 errors: the scheduler's error is logged; the stop's error is logged too -->
<!-- cases: 6 not found/duplicate: the stop is called once, not again by Shutdown -->
<!-- cases: 7 lifecycle: at Start, before it returns (verification 3, S2) -->
<!-- cases: N/A 8 concurrency: on Start's goroutine -->
<!-- cases: 9 panics: a panic of that stop goes to HookContainerPanic when set, logged otherwise -->
<!-- cases: 10 backward compatibility: Logf is replaced by Logger (container-logging); the logged content is unchanged -->

### Requirement: Shutdown cancels the checks in flight with a container-shut-down cause
When root Shutdown begins, the container SHALL cancel the context of every check in flight with the cause `ErrShutdown`, without waiting for the checks. A caller still waiting for such a check SHALL get an error matching `ErrHealthCheckTimeout` and `context.Canceled`.

#### Scenario: Callers of a check cancelled by Shutdown
```gherkin
Given an injector that is never started, with a service "db" whose HealthCheck(ctx) blocks until ctx is done, then records context.Cause(ctx) and returns ctx.Err()
And a HealthCheck() call on the root scope is waiting for that check
When the consumer calls Shutdown
Then the HealthCheck() call returns "doger: health check timeout: context canceled" for "db"
And the check recorded a cause matching ErrShutdown
```

<!-- cases: N/A 1 default: no option -->
<!-- cases: N/A 2 overrides: none -->
<!-- cases: N/A 3 zero values: not applicable -->
<!-- cases: N/A 4 boundaries: not applicable -->
<!-- cases: 5 errors: the caller's timeout error and the check's cause (verification 3, S1) -->
<!-- cases: N/A 6 not found/duplicate: not applicable -->
<!-- cases: 7 lifecycle: an injector never started, shut down with a forced check in flight -->
<!-- cases: 8 concurrency: the check is cancelled, not waited for -->
<!-- cases: N/A 9 panics: not applicable -->
<!-- cases: 10 backward compatibility: BREAKING - the cause is ErrShutdown, which replaces ErrStartAfterShutdown (container-shutdown); its message is "doger: container is shut down" -->

### Requirement: A failing stop does not fail Shutdown
An error returned by the stop function SHALL be logged at Error level on `InjectorOpts.Logger`. A panic in it SHALL be recovered and passed to `HookContainerPanic(root, "healthscheduler", value)` when that option is set, and logged otherwise. Neither SHALL fail Shutdown.

#### Scenario: A failing stop does not fail Shutdown
```gherkin
Given a started injector whose HealthScheduler's stop function <fails>, with HookContainerPanic <panic hook> and a Logger whose handler records every record
When the consumer calls Shutdown
Then the report succeeds
And <observed>

Examples:
  | fails                          | panic hook                    | observed                                                                                                         |
  | returns errors.New("busy")     | nil                           | the handler recorded an Error record whose message contains "health scheduler" with an error attribute "busy"     |
  | panics with "stop boom"        | nil                           | the handler recorded an Error record whose message contains "health scheduler", and the value "stop boom" only in a Debug record |
  | panics with "stop boom"        | set, recording (point, value) | HookContainerPanic recorded ("healthscheduler", "stop boom")                                                     |
```

<!-- cases: N/A 1 default: a stop that returns nil -->
<!-- cases: 2 overrides: with and without HookContainerPanic -->
<!-- cases: N/A 3 zero values: a nil error is not logged -->
<!-- cases: N/A 4 boundaries: not applicable -->
<!-- cases: 5 errors: an error is logged at Error level -->
<!-- cases: N/A 6 not found/duplicate: not applicable -->
<!-- cases: 7 lifecycle: root Shutdown -->
<!-- cases: N/A 8 concurrency: not applicable -->
<!-- cases: 9 panics: this requirement (review R8); panic values reach logs only at Debug (container-logging) -->
<!-- cases: 10 backward compatibility: Logf is replaced by Logger (container-logging) -->

### Requirement: A panicking health hook does not stop health recording
A panic in a `HookServiceHealthChanged` or `HookContainerHealthChanged` hook SHALL be recovered. It SHALL be passed to `HookContainerPanic(root, "healthchanged", value)` when that option is set, and logged on `InjectorOpts.Logger` otherwise (Error level without the value, Debug level with it); a panic raised by `HookContainerPanic` SHALL propagate. The remaining hooks for the same change SHALL still be called, and later results SHALL still be recorded.

#### Scenario: Hook panic is contained
```gherkin
Given a started injector with a manual HealthScheduler, HookContainerPanic <panic hook>, and a Logger whose handler records every record at Debug level and above
And service hooks A, which panics "boom", and B, added in that order
And a Healthchecker "db" that returns nil then errors.New("down")
When the test runs the round twice
Then B was called 2 times
And <observed>

Examples:
  | panic hook                    | observed                                                                                   |
  | set, recording (point, value) | HookContainerPanic recorded ("healthchanged", "boom") 2 times                               |
  | nil                           | the handler recorded 2 Error records about a health hook panic and 2 Debug records carrying "boom" |
```

<!-- cases: N/A 1 default: panics are not a default path -->
<!-- cases: 2 overrides: with and without HookContainerPanic -->
<!-- cases: N/A 3 zero values: a nil panic value is reported as runtime.PanicNilError, as for checks -->
<!-- cases: N/A 4 boundaries: not applicable -->
<!-- cases: N/A 5 errors: hooks return nothing -->
<!-- cases: N/A 6 not found/duplicate: not applicable -->
<!-- cases: 7 lifecycle: the next round still records -->
<!-- cases: N/A 8 concurrency: hooks are serialized -->
<!-- cases: 9 panics: this requirement; a nil HookContainerPanic logs instead of re-raising, because no caller is there to receive the panic (flagged) -->
<!-- cases: 10 backward compatibility: Logf is replaced by Logger; the panic value moves to Debug level (container-logging, audit R3) -->

### Requirement: A round checks every eligible service once and returns
Each call of `round(ctx)` SHALL check each underlying health checker of the container once, joining checks already in flight, and SHALL return once every check is recorded or its bound elapsed. Services not started yet are excluded (health-reports). Each check is bounded by `HealthCheckTimeout` when positive, `DefaultHealthReportCheckTimeout` when 0, and nothing when negative, and by ctx. A round SHALL return at once after scheduling stopped.

#### Scenario: Round results
```gherkin
Given a started injector with a manual HealthScheduler, a fake clock, and HealthCheckTimeout <timeout>
And Healthcheckers root/"a" returning nil, "child"/"b" returning errors.New("down"), and root/"c" whose HealthCheck() blocks until released and ignores any context
When the test runs the round and the fake clock advances by <advance>
Then the round returns <returns>
And the report lists "a" HealthHealthy, "b" HealthUnhealthy, and "c" <c>

Examples:
  | timeout | advance | returns           | c                                              |
  | 0       | 10s     | after the advance | HealthUnhealthy matching ErrHealthCheckTimeout |
  | 0       | 9.999s  | not yet           | HealthUnknown with PendingSince set            |
  | 50ms    | 50ms    | after the advance | HealthUnhealthy matching ErrHealthCheckTimeout |
```

#### Scenario: A round checks each service once
```gherkin
Given a started injector with a manual HealthScheduler and a Healthchecker root/"db" with an alias "hc" (AliasNamed), and "child"/"svc" exported to the root, all counting their calls
And a HealthCheck() call on the root scope is running the check of "db", blocking until released, and has already checked "svc" (exported to the root scope) once
When the test runs the round and the check of "db" is released
Then the round called the health check of "db" 0 times, as it joined the running check, and that of "svc" 1 time
And the health check of "db" was called 1 time in total
```

#### Scenario: A round after scheduling stopped does nothing
```gherkin
Given a started injector with a manual HealthScheduler and a Healthchecker "db" that counts its calls
And the container was shut down
When the test runs the stored round
Then it returns at once and "db" was not checked by it
```

<!-- cases: 1 default: the 10s report bound -->
<!-- cases: 2 overrides: a positive HealthCheckTimeout -->
<!-- cases: 3 zero values: HealthCheckTimeout 0 -->
<!-- cases: 4 boundaries: one millisecond before and at the bound -->
<!-- cases: 5 errors: a failing check, a timed-out check -->
<!-- cases: 6 not found/duplicate: aliases, exports and joined checks -->
<!-- cases: 7 lifecycle: a round after Shutdown -->
<!-- cases: 8 concurrency: joining a forced check in flight -->
<!-- cases: N/A 9 panics: specified in health-check-execution -->
<!-- cases: N/A 10 backward compatibility: new behavior -->

### Requirement: Scheduling stops when root shutdown begins
When root `Shutdown` begins, through the `RootScope` or the root's own `*Scope`, the container SHALL stop scheduling before the Closing hooks run and before any service shuts down. It SHALL cancel the context of every round in progress and record nothing more. It SHALL then call the stop function once, with a context that Shutdown's context cannot cancel, and wait for it to return. Shutdown SHALL NOT wait for checks.

#### Scenario: Stop happens before the Closing hooks
```gherkin
Given a started injector with a manual HealthScheduler and a Closing hook that records whether stop was already called
When the consumer calls <shutdown>
Then stop was called 1 time
And <closing>
And the context stop received was not done

Examples:
  | shutdown                                                  | closing                          |
  | Shutdown() on the RootScope                               | the Closing hook ran after stop  |
  | ShutdownWithContext with an already-cancelled context     | the Closing hook ran after stop  |
  | Shutdown() on the injector a provider received for "db"   | no Closing hook ran, as on this path for any container |
```

#### Scenario: A context-aware check in a round is cancelled before services shut down
```gherkin
Given a started injector with a manual HealthScheduler and HealthCheckTimeout -1ns
And a service "db" whose HealthCheck(ctx) blocks until ctx is done, then returns ctx.Err()
And a Shutdowner "svc" whose Shutdown records whether the check's context was already done
And the test is running the round, so the check of "db" is in flight
When the consumer calls Shutdown
Then "svc" recorded that the check's context was done
And the round returned
```

#### Scenario: Shutdown does not wait for a check that ignores its context
```gherkin
Given a started injector with a manual HealthScheduler and HealthCheckTimeout -1ns
And a service "db" whose HealthCheck() blocks until released and ignores any context
And the test is running the round, so the check of "db" is in flight
When the consumer calls Shutdown
Then Shutdown returns within 1s
And the check of "db" is released before the test ends
```

<!-- cases: N/A 1 default: no scheduler means nothing to stop -->
<!-- cases: N/A 2 overrides: none -->
<!-- cases: N/A 3 zero values: a container that never reached Ready has no stop to call -->
<!-- cases: N/A 4 boundaries: not applicable -->
<!-- cases: 5 errors: an error returned by stop is logged through the Logger and does not fail Shutdown -->
<!-- cases: 6 not found/duplicate: a second Shutdown returns the first report as in container-start; stop is called once -->
<!-- cases: 7 lifecycle: order against Closing hooks and service shutdown; the root's own *Scope path, which runs no Closing hook; an ended Shutdown context (shutdown-context) -->
<!-- cases: 8 concurrency: cancellation of a round in progress -->
<!-- cases: N/A 9 panics: a panic in stop is the next requirement -->
<!-- cases: N/A 10 backward compatibility: Shutdown without a scheduler is unchanged -->

### Requirement: Service health changes call the service hooks in order
Each `HookServiceHealthChanged(scope, serviceName, previous, current ServiceHealth)` SHALL be called once per change of a service's recorded `State`, including from `HealthUnknown` to its first result, whichever path recorded it. Calls SHALL be serialized, in recording order. A new error with an unchanged state SHALL NOT call it. `AddServiceHealthChangedHook` SHALL append to the same list.

#### Scenario: Service hook calls follow state changes
```gherkin
Given a started injector with a manual HealthScheduler
And a Healthchecker "db" whose results, round after round, are <results>
And a HookServiceHealthChanged hook that records (previous state, current state, current error)
When the test runs the round <rounds> times
Then the hook recorded <calls>

Examples:
  | results                          | rounds | calls                                                                             |
  | nil, nil, nil                    | 3      | (Unknown, Healthy, nil)                                                           |
  | nil, errors.New("down"), nil     | 3      | (Unknown, Healthy, nil), (Healthy, Unhealthy, "down"), (Unhealthy, Healthy, nil)  |
  | errors.New("a"), errors.New("b") | 2      | (Unknown, Unhealthy, "a")                                                         |
```

#### Scenario: A forced run records changes too
```gherkin
Given an injector that is never started, with a Healthchecker "db" whose recorded result is nil
And a HookServiceHealthChanged hook that records calls
And "db" now returns errors.New("down")
When the consumer calls HealthCheck() on the root scope
Then within 1s the hook recorded (Healthy, Unhealthy, "down")
```

#### Scenario: Hooks run one at a time, in order of addition
```gherkin
Given a started injector with a manual HealthScheduler and hooks A and B added in that order with AddServiceHealthChangedHook
And 10 Healthchecker services whose first results arrive concurrently
When the test runs the round and the hooks have run
Then the hooks were called 20 times in total, never two at once
And for each service, A was called before B
```

#### Scenario: A hook may call health APIs
```gherkin
Given an injector that is never started, with a Healthchecker "db" that returns nil, then errors.New("down")
And a HookServiceHealthChanged hook that calls <call> and records its result
When the consumer calls HealthCheck() on the root scope twice
Then each call returns within 1s
And the hook's own call returned within 1s

Examples:
  | call                                         |
  | HealthCheckNamed(injector, "db")             |
  | HealthCheck() on the root scope              |
  | HealthReport() on the root scope             |
```

#### Scenario: Nil hooks are skipped
```gherkin
Given an injector that is never started, with a Healthchecker "db" returning nil, a HookContainerPanic that records its calls and a Logger whose handler records every record at Debug level
And HookServiceHealthChanged holding nil and a hook R that counts its calls, HookContainerHealthChanged holding nil
And AddServiceHealthChangedHook(nil) and AddContainerHealthChangedHook(nil) were called
When the consumer calls HealthCheck() on the root scope and the hooks have run
Then R was called 1 time
And HookContainerPanic was not called and the handler recorded no "panic recovered" record
```

<!-- cases: 1 default: no hook registered is the default; nothing is called -->
<!-- cases: 2 overrides: the InjectorOpts field and the Add method share one list -->
<!-- cases: 3 zero values: a nil hook in the list is skipped, in the field and through the Add methods (verification 3, W5) -->
<!-- cases: 4 boundaries: the first result counts as a change; an unchanged state does not -->
<!-- cases: N/A 5 errors: hooks return nothing -->
<!-- cases: 6 not found/duplicate: a removed service calls no service hook -->
<!-- cases: 7 lifecycle: round and forced paths; re-entrant calls from a hook -->
<!-- cases: 8 concurrency: serialized calls under concurrent results; a result reaches its callers without waiting for hooks -->
<!-- cases: 9 panics: the hook-panic requirement below -->
<!-- cases: N/A 10 backward compatibility: new hook; it takes no ctx, like HookContainerPanic, because it is delivered off any caller's request -->
