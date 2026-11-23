# Spec Delta

## MODIFIED Requirements

### Requirement: Events are logged at a fixed level
With a `Logger` set, each container event SHALL emit exactly one record with the message and level of this requirement's table, carrying `scope` (scope name) and `service` (service name) attributes where the event concerns them. Records below the handler's level SHALL NOT be emitted.

#### Scenario: Message, level and attributes per event
```gherkin
Given a root scope with Logger set to a slog logger whose handler records every record at level Debug
And a lazy service "svc" in the root implementing Start(ctx) error and Shutdown() error, opted in with doger.AsStarter()
When the consumer triggers <event>
Then exactly one record with message "<message>" is recorded
And its level is <level>
And its attributes include <attrs>

Examples:
  | event                                                         | message                       | level | attrs                                       |
  | doger.Provide registering "svc"                               | service registered            | DEBUG | scope=[root] service=svc                    |
  | doger.OverrideNamed of "svc"                                  | service overridden            | DEBUG | scope=[root] service=svc                    |
  | doger.InvokeNamed of "svc"                                    | service invoked               | DEBUG | scope=[root] service=svc                    |
  | doger.ExportNamed of "svc" from child "ext" to the root       | service exported              | DEBUG | scope=ext service=svc                       |
  | root.Start(ctx) succeeding                                    | container started             | INFO  | scope=[root]                                |
  | root.Start(ctx) reaching PhaseReady                           | container ready               | INFO  | scope=[root]                                |
  | root.Shutdown() requested, first record of the winning call    | requested shutdown            | DEBUG | scope=[root]                                |
  | root.Shutdown() beginning                                     | container closing             | INFO  | scope=[root]                                |
  | root.Shutdown() completing                                    | container closed              | INFO  | scope=[root]                                |
  | root.Shutdown() where "svc" Shutdown returns errors.New("x")  | service shutdown failed       | ERROR | scope=[root] service=svc error=x            |
  | a HealthScheduler whose ScheduleHealthRounds returns errors.New("no cron") | health scheduler failed | ERROR | error=no cron                     |
  | a health scheduler stop function returning errors.New("busy") | health scheduler stop failed  | ERROR | error=busy                                  |
  | a PropertySource whose read fails with a *fs.PathError        | property source unavailable   | WARN  | source_type=*fs.PathError                   |
  | an extension declining with ErrExtensionDisabled              | extension disabled            | INFO  | extension=<its name>                        |
  | the container health changing from healthy to degraded        | container health changed      | WARN  | previous=healthy current=degraded           |
  | the container health changing from degraded to healthy        | container health changed      | INFO  | previous=degraded current=healthy           |
  | a health check of "svc" returning errors.New("x")              | health check failed           | DEBUG | scope=[root] service=svc error=x            |
  | root.Start(ctx) while "svc"'s Start is still running once SlowStartThreshold elapsed | start step is slow   | WARN  | scope=[root] service=svc step=start threshold=<SlowStartThreshold> |
  | with StartTimeout 5s, "svc"'s abandoned Start returning nil at 1m | abandoned start step returned | WARN  | scope=[root] service=svc step=start elapsed=1m |
  | with StartTimeout 5s, "svc"'s abandoned Start returning errors.New("x") | abandoned start step error | DEBUG | scope=[root] service=svc step=start error=x |
```

#### Scenario: Records below the handler's level are dropped
```gherkin
Given a root scope with Logger set to a slog logger whose handler level is <handler_level>
When the consumer invokes a lazy "svc", calls root.Start(ctx) and root.Shutdown()
Then the handler recorded <debug> "service invoked" records
And it recorded <info> "container started" records

Examples:
  | handler_level | debug | info |
  | DEBUG         | 1     | 1    |
  | INFO          | 0     | 1    |
  | ERROR         | 0     | 0    |
```

#### Scenario: Child scope events carry the child's scope name
```gherkin
Given a root scope with a recording Logger at level Debug
And child scope "tenant-a" of the root
When the consumer registers and invokes a lazy "svc" in "tenant-a"
Then the "service registered" and "service invoked" records have scope=tenant-a and service=svc
```

#### Scenario: Concurrent logging
```gherkin
Given a root scope with a recording Logger at level Debug
When 8 goroutines invoke 10 distinct lazy services 100 times each
Then the race detector reports no data race
And the handler recorded exactly 8000 "service invoked" records
```

<!-- cases: 1 happy path: the Examples table at Debug -->
<!-- cases: 2 overrides: the handler level rows; any slog.Handler is accepted -->
<!-- cases: N/A 3 zero values: a nil Logger is covered by "Logging is off by default" -->
<!-- cases: 4 boundaries: DEBUG/INFO/ERROR handler levels -->
<!-- cases: 5 errors: shutdown failure, scheduler failure, property source failure rows; the slow and abandoned start step rows are detailed in "A slow start step is logged at Warn level" and "An abandoned start step that ends is logged" -->
<!-- cases: 6 override: the "service overridden" row -->
<!-- cases: 7 lifecycle: started, ready, closing, closed rows; child scopes -->
<!-- cases: 8 concurrency: "Concurrent logging" -->
<!-- cases: N/A 9 panics: covered by "Panic values stay at Debug level" -->
<!-- cases: 10 compatibility: BREAKING: messages change from "DI: ..." printf strings to fixed slog messages -->


## ADDED Requirements

### Requirement: A slow start step is logged at Warn level
With a `Logger` set, a Starter `Start` method or kind-handler call still running once `InjectorOpts.SlowStartThreshold` elapsed on the container clock SHALL emit exactly one `WARN` record, "start step is slow", with `scope`, `service`, `step` (`start` or `handle`), `kind` (handle only) and `threshold`. Zero means `DefaultSlowStartThreshold` (10s); a negative value, or one at or above a set `StartTimeout`, arms nothing. The record SHALL be emitted while the step still runs, also when the container finished shutting down meanwhile (a Shutdown bounded by its context stops waiting for a running start).

#### Scenario: Slow step warning per threshold
```gherkin
Given a root scope created with InjectorOpts{Logger: a recording slog logger at level Debug, SlowStartThreshold: <threshold>}, inside synctest.Test
And <step> that sleeps <runs> and returns nil
When the consumer calls Start()
Then Start returns nil
And the handler recorded <count> "start step is slow" records
And each such record has level WARN and attributes <attrs>

Examples:
  | threshold | step                                                       | runs | count | attrs                                                            |
  | 0         | lazy Starter "srv" whose Start(ctx) error                  | 9s   | 0     | -                                                                |
  | 0         | lazy Starter "srv" whose Start(ctx) error                  | 11s  | 1     | scope=[root] service=srv step=start threshold=10s                |
  | 0         | lazy Starter "srv" whose Start(ctx) error                  | 1h   | 1     | scope=[root] service=srv step=start threshold=10s                |
  | 2s        | lazy Starter "srv" whose Start(ctx) error                  | 3s   | 1     | scope=[root] service=srv step=start threshold=2s                 |
  | 2s        | lazy Starter "srv" whose Start(ctx) error                  | 1s   | 0     | -                                                                |
  | -1ns      | lazy Starter "srv" whose Start(ctx) error                  | 1h   | 0     | -                                                                |
  | 0         | lazy "srv" opted in with AsRunnable() whose Run(ctx) error | 11s  | 1     | scope=[root] service=srv step=handle kind=runnable threshold=10s |
```

#### Scenario: The warning names the scope of a child scope's step
```gherkin
Given a started root container with a recording Logger at level Debug, inside synctest.Test
And child scope "tenant", created after Start, with lazy Starter "worker" whose Start sleeps 11s
When the consumer calls <operation>
Then exactly one "start step is slow" record is recorded, with scope=tenant service=worker step=start

Examples:
  | operation      |
  | tenant.Start() |
  | root.Refresh() |
```

#### Scenario: Only the slow Starter is named under parallel start
```gherkin
Given a root scope created with InjectorOpts{Logger: a recording logger at level Debug, StartParallelism: 4}, inside synctest.Test
And independent lazy Starters "fast-a", "fast-b" and "fast-c" returning at once and "slow" sleeping 11s
When the consumer calls Start()
Then exactly one "start step is slow" record is recorded, with service=slow
```

#### Scenario: A step that panics after the warning
```gherkin
Given a root scope with a recording Logger at level Debug, inside synctest.Test
And lazy Starter "srv" whose Start sleeps 11s and then panics with "boom"
When the consumer calls Start()
Then Start returns a *StartError for "srv" matching the recovered panic, as today
And the handler recorded exactly one "start step is slow" record
And it recorded no "panic recovered" record, since a Start panic is returned, not logged
```

#### Scenario: The warning stops when the step is abandoned
```gherkin
Given a root scope created with InjectorOpts{Logger: a recording logger at level Debug, StartTimeout: 5s, SlowStartThreshold: <threshold>}, inside synctest.Test
And lazy Starter "srv" whose Start blocks until the test releases it at 1m
When the consumer calls Start() and the test releases "srv" and waits for its call to return
Then the handler recorded <count> "start step is slow" records

Examples:
  | threshold | count |
  | 2s        | 1     |
  | 5s        | 0     |
  | 10s       | 0     |
```

#### Scenario: No warning timer without a Warn-enabled logger
```gherkin
Given a root scope created with <opts> and Clock set to a counting doger.Clock that wraps RealClock() and counts AfterFunc calls, inside synctest.Test
And lazy Starter "srv" whose Start sleeps 1h
When the consumer calls Start()
Then Start returns nil
And the counting clock saw <calls> AfterFunc calls
And the default slog handler, set with slog.SetDefault to a recording handler, recorded 0 records

Examples:
  | opts                                                    | calls |
  | InjectorOpts{}                                          | 0     |
  | InjectorOpts{Logger: a recording logger at level Error} | 0     |
  | InjectorOpts{Logger: a recording logger at level Warn}  | 1     |
```

#### Scenario: Slow-start warning waits for the container clock
```gherkin
Given a root scope created with InjectorOpts{Logger: a recording logger at level Debug, Clock: dogerclockwork.Wrap(F)} for a clockwork fake clock F
And lazy Starter "srv" whose Start blocks until the test releases it
When the consumer calls Start() in another goroutine and 50ms of wall time pass without advancing F
Then no "start step is slow" record is recorded
And after F advances by 10s exactly one is recorded, before the test releases "srv"
```

#### Scenario: The warning timer leaves no goroutine
```gherkin
Given a root scope with a recording Logger at level Debug, inside synctest.Test
And 100 lazy Starters whose Start returns nil at once
When the consumer calls Start() and then Shutdown()
Then no "start step is slow" record is recorded
And goleak finds no goroutine left by the container
```

#### Scenario: A panicking log handler does not crash the process
```gherkin
Given a root scope created with InjectorOpts{Logger: a slog logger whose handler panics with "handler boom" on every WARN record, StartTimeout: 5s}, inside synctest.Test
And lazy Starter "srv" whose Start blocks until the test releases it at 1m, then returns nil
When the consumer calls Start() and the test releases "srv" and waits for its call to return
Then the process does not crash
And Start returned an error matching ErrStartTimeout
And "srv" is recorded as started
```

<!-- cases: 1 happy path: zero-config threshold rows (9s, 11s) -->
<!-- cases: 2 overrides: 2s threshold rows; Logger handler level rows; Clock rows; StartTimeout combined in "The warning stops when the step is abandoned" -->
<!-- cases: 3 zero values: threshold 0 means the default; nil Logger and empty opts arm no timer and emit nothing -->
<!-- cases: 4 boundaries: below/above 10s and 2s; -1ns smallest disabling value; a threshold above StartTimeout never fires. A step returning exactly at the threshold races the timer: at most one record, no scenario pins the race -->
<!-- cases: N/A 5 errors: the warning is not an error; the step's own outcome is unchanged -->
<!-- cases: 6 duplicates: a 1h step still logs exactly once -->
<!-- cases: 7 lifecycle: Start, Refresh, Scope.Start; child scope attribute; the warning timer stops before the step is abandoned, and a threshold equal to StartTimeout (the 5s row) never warns, so the outcome does not depend on which timer fires first -->
<!-- cases: 8 concurrency: parallel start names only the slow Starter; goleak for timers; the timer callback runs on its own goroutine -->
<!-- cases: 9 panics: a step panic after the warning; a panicking handler on the timer goroutine is recovered and dropped, since no caller owns that goroutine -->
<!-- cases: 10 compatibility: no record and no timer unless a Logger at Warn or below is set, so "Logging is off by default" holds -->

### Requirement: An abandoned start step that ends is logged
With a `Logger` set, when a step abandoned under `InjectorOpts.StartTimeout` later returns, the container SHALL emit one `WARN` record "abandoned start step returned" with `scope`, `service`, `step`, `kind` (handle only) and `elapsed`, and, if it returned an error, a `DEBUG` record "abandoned start step error" with the same attributes plus `error`. When it panics, it SHALL emit the existing "panic recovered" records with the step's own scope, plus `step` and `kind` (handle only), as the other late records carry them. These records SHALL be emitted whenever the call ends, also after Start returned and after the container or the step's scope finished shutting down.

#### Scenario: Late outcome of an abandoned step
```gherkin
Given a root scope created with InjectorOpts{StartTimeout: 5s, Logger: a recording logger at level <level>}, inside synctest.Test
And lazy Starter "srv" in <scope> whose Start blocks until the test releases it at 1m and then <outcome>
When the consumer calls <operation> and the test releases "srv" and waits for its call to end
Then the records about "srv" after the abandonment are exactly <records>

Examples:
  | level | scope                                     | operation      | outcome                            | records                                                                                                                                       |
  | DEBUG | the root                                  | Start()        | returns nil                        | WARN "abandoned start step returned" scope=[root] service=srv step=start elapsed=1m                                                           |
  | DEBUG | the root                                  | Start()        | returns errors.New("bind: in use") | WARN "abandoned start step returned" scope=[root] service=srv step=start elapsed=1m; DEBUG "abandoned start step error" scope=[root] service=srv step=start elapsed=1m error=bind: in use |
  | INFO  | the root                                  | Start()        | returns errors.New("bind: in use") | WARN "abandoned start step returned" scope=[root] service=srv step=start elapsed=1m                                                           |
  | DEBUG | the root                                  | Start()        | panics with "boom"                 | ERROR "panic recovered" scope=[root] service=srv point=start step=start panic_type=string; DEBUG "panic value" scope=[root] service=srv point=start step=start value=boom |
  | DEBUG | child scope "tenant", created after Start | tenant.Start() | panics with "boom"                 | ERROR "panic recovered" scope=tenant service=srv point=start step=start panic_type=string; DEBUG "panic value" scope=tenant service=srv point=start step=start value=boom |
  | DEBUG | the root, "srv" opted in with AsRunnable() instead | Start() | panics with "boom" in Run   | ERROR "panic recovered" scope=[root] service=srv point=start step=handle kind=runnable panic_type=string; DEBUG "panic value" scope=[root] service=srv point=start step=handle kind=runnable value=boom |
```

<!-- cases: 1 happy path: late nil return -->
<!-- cases: 2 overrides: handler level rows -->
<!-- cases: N/A 3 zero values: a nil Logger emits nothing, per "Logging is off by default" -->
<!-- cases: N/A 4 boundaries: one outcome per abandoned call -->
<!-- cases: 5 errors: error text stays at Debug, as health-check error text does -->
<!-- cases: N/A 6 duplicates: an abandoned call ends once -->
<!-- cases: 7 lifecycle: the late return may come after Start returned or after Shutdown, even after the container closed; logging still uses the container Logger, and the records are emitted then too (stated in the requirement and the Logger godoc); the child scope row names the step's own scope -->
<!-- cases: 11 consistency: the panic records carry step and kind like the other late records (triage E) -->
<!-- cases: 8 concurrency: the late outcome is logged on the abandoned goroutine; the test synchronizes on its end -->
<!-- cases: 9 panics: the panic rows; the panic is never re-raised, since nothing waits for it; a panicking handler here is covered by "A panicking log handler does not crash the process" -->
<!-- cases: 10 compatibility: only reachable with StartTimeout set -->
