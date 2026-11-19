# Spec Delta

## Purpose

Defines how the container logs through a consumer-supplied `*slog.Logger`: silent by default, one fixed level and message per event with structured `scope` and `service` attributes, and no panic values or health-check error text above Debug level.

## ADDED Requirements

### Requirement: Logging is off by default
With `InjectorOpts.Logger` nil, the container SHALL emit no log record anywhere: not to `slog.Default()`, the standard `log` package, stdout or stderr. `InjectorOpts` SHALL NOT have a `Logf` field.

#### Scenario: A container without a logger is silent
```gherkin
Given slog.SetDefault was given a handler that records every record
And log.SetOutput was given a buffer
And a root scope created with <constructor>
When the consumer registers, invokes, starts, health-checks and shuts down a lazy service "svc" implementing Start(ctx) error, HealthCheck() error returning errors.New("down") and Shutdown() error returning errors.New("stuck"), opted in with doger.AsStarter()
Then the default slog handler recorded 0 records
And the log buffer is empty

Examples:
  | constructor                                     |
  | doger.New()                                     |
  | doger.NewWithOpts(&doger.InjectorOpts{})        |
  | doger.NewWithOpts(nil)                          |
```

<!-- cases: 1 happy path: the subject -->
<!-- cases: 2 overrides: covered by "Events are logged at a fixed level" -->
<!-- cases: 3 zero values: nil Logger, empty opts, nil opts -->
<!-- cases: N/A 4 boundaries: no numeric input -->
<!-- cases: 5 errors: a failing health check and a failing shutdown still log nothing -->
<!-- cases: N/A 6 duplicate: not applicable -->
<!-- cases: 7 lifecycle: register, invoke, start, health check, shutdown -->
<!-- cases: N/A 8 concurrency: no logger, no shared state -->
<!-- cases: N/A 9 panics: covered by "Panic values and health error text stay at Debug level" -->
<!-- cases: 10 compatibility: BREAKING: Logf is replaced by Logger; migration in design D10 -->

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
  | a health scheduler stop function returning errors.New("busy") | health scheduler stop failed  | WARN  | error=busy                                  |
  | a PropertySource whose read fails with a *fs.PathError        | property source unavailable   | WARN  | source_type=*fs.PathError                   |
  | an extension declining with ErrExtensionDisabled              | extension disabled            | INFO  | extension=<its name>                        |
  | the container health changing from healthy to degraded        | container health changed      | WARN  | previous=healthy current=degraded           |
  | the container health changing from degraded to healthy        | container health changed      | INFO  | previous=degraded current=healthy           |
  | a health check of "svc" returning errors.New("x")              | health check failed           | DEBUG | scope=[root] service=svc error=x            |
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
<!-- cases: 5 errors: shutdown failure, scheduler failure, property source failure rows -->
<!-- cases: 6 override: the "service overridden" row -->
<!-- cases: 7 lifecycle: started, ready, closing, closed rows; child scopes -->
<!-- cases: 8 concurrency: "Concurrent logging" -->
<!-- cases: N/A 9 panics: covered by "Panic values and health error text stay at Debug level" -->
<!-- cases: 10 compatibility: BREAKING: messages change from "DI: ..." printf strings to fixed slog messages -->

### Requirement: Panic values and health error text stay at Debug level
A recovered panic SHALL be logged at Error level with message `panic recovered` and attributes `scope`, `service` (when it concerns one), `point` and `panic_type` (the `%T` of the value), and the value itself SHALL appear only in a separate Debug record `panic value`. Health-check error text SHALL appear only in Debug records.

#### Scenario: A panic value is not logged above Debug
```gherkin
Given a root scope with a recording Logger at level <level>
And a lazy service "svc" whose Shutdown() panics with "token=hunter2"
And "svc" was invoked
When the consumer calls root.Shutdown()
Then the handler recorded <errors> records with message "panic recovered", level ERROR, service=svc, point=service-shutdown and panic_type=string
And the text "hunter2" appears in <secret> recorded records

Examples:
  | level | errors | secret |
  | DEBUG | 1      | 1      |
  | INFO  | 1      | 0      |
```

#### Scenario: Health-check error text is not logged above Debug
```gherkin
Given a root scope with a recording Logger at level <level>
And a lazy service "db" whose HealthCheck() returns errors.New("dsn=postgres://u:pw@h")
And "db" was invoked
When the consumer calls root.HealthCheck()
Then the text "pw@h" appears in <secret> recorded records

Examples:
  | level | secret |
  | DEBUG | 1      |
  | INFO  | 0      |
```

<!-- cases: 1 happy path: INFO rows (production default level) -->
<!-- cases: 2 overrides: DEBUG rows show the value when the consumer opts in -->
<!-- cases: N/A 3 zero values: a nil panic value is recovered as *runtime.PanicNilError and logged by type like any value -->
<!-- cases: N/A 4 boundaries: no numeric input -->
<!-- cases: 5 errors: health check error text -->
<!-- cases: N/A 6 duplicate: panics after the first of one operation follow the same rule (WARN for later panics per design D8, value at Debug only) -->
<!-- cases: 7 lifecycle: shutdown panic; start and hook panics follow the same rule -->
<!-- cases: N/A 8 concurrency: covered by "Concurrent logging" -->
<!-- cases: 9 panics: the subject -->
<!-- cases: 10 compatibility: Logf previously received the panic value at the only level it had -->

### Requirement: A health check that starts is recorded at Debug level
Each health check the container starts, on any path (report read, scheduled round, `HealthCheck()` or a single-service function), SHALL emit one Debug record `health check started` with attributes `scope` and `service` before the check runs. A caller that joins a check in flight, or that starts no check, SHALL emit none.

#### Scenario: Which calls record a started check
```gherkin
Given a root scope with a Logger whose handler records every record at Debug level
And a Healthchecker "db" in the root that blocks until released
When <action>
Then the handler recorded <count> "health check started" records with scope=[root] and service=db

Examples:
  | action                                                                              | count |
  | the consumer calls HealthCheckNamed(root, "db") and the check is released           | 1     |
  | a second HealthCheckNamed(root, "db") joins the first before the check is released  | 1     |
  | the consumer calls HealthCheckNamedWithContext with an already-cancelled context    | 0     |
  | the consumer calls root.HealthCheck() and the check is released                     | 1     |
```

#### Scenario: The record is invisible above Debug
```gherkin
Given a root scope with a Logger whose handler records at Info level
And a Healthchecker "db" in the root returning nil
When the consumer calls HealthCheckNamed(root, "db")
Then the handler recorded 0 "health check started" records
```

<!-- cases: 1 default: no Logger means no record (requirement "Logging is off by default") -->
<!-- cases: 2 overrides: the handler's level decides -->
<!-- cases: N/A 3 zero values: covered by the default case -->
<!-- cases: 4 boundaries: one check, one joining caller, no check -->
<!-- cases: N/A 5 errors: the record does not carry the check's result -->
<!-- cases: N/A 6 duplicates: a joining caller adds no record -->
<!-- cases: 7 lifecycle: health-reports and health-check-execution scenarios use this record as the point where a check starts -->
<!-- cases: N/A 8 concurrency: covered by the joining row -->
<!-- cases: N/A 9 panics: unchanged -->
<!-- cases: 10 backward compatibility: replaces the Logf line "requested health check for service <name>" -->
