# Spec Delta

## MODIFIED Requirements

### Requirement: The container measures time only on its clock
With `InjectorOpts.Clock` set, the container SHALL read every time it measures from that clock: the `HealthCheckTimeout` and `HealthCheckGlobalTimeout` deadlines, a lazy service's build time (`ServiceBuildTime` in `ExplainService` and `ExplainNamedService` output), `ShutdownReport.ShutdownTime` and `ShutdownReport.ServiceShutdownTime`, and the `SlowStartThreshold` warning delay, the `StartTimeout` bound and the `elapsed` of start hooks and start-step log records.

#### Scenario: Reported durations come from the clock
```gherkin
Given an injector whose Clock is dogerclockwork.Wrap(F) for a clockwork fake clock F
And a lazy service "svc" whose <phase> advances F by <advance> and returns no error
When the consumer <action>
Then <measurement> is exactly <advance>

Examples:
  | phase    | advance | action                                      | measurement                                    |
  | provider | 3s      | invokes "svc" and calls ExplainNamedService | ServiceBuildTime of "svc"                      |
  | provider | 0s      | invokes "svc" and calls ExplainNamedService | ServiceBuildTime of "svc" (omitted when zero)  |
  | Shutdown | 2s      | invokes "svc" and calls Shutdown            | ShutdownReport.ServiceShutdownTime for "svc"   |
  | Shutdown | 2s      | invokes "svc" and calls Shutdown            | ShutdownReport.ShutdownTime                    |
```

#### Scenario: Timeout errors read the same on any clock
```gherkin
Given an injector whose Clock is <clock> and HealthCheckTimeout is 10ms
And a service "svc" whose health check blocks until released
When the 10ms timeout elapses on that clock
Then the result for "svc" has the message "doger: health check timeout: context deadline exceeded"
And it matches ErrHealthCheckTimeout and context.DeadlineExceeded with errors.Is

Examples:
  | clock                                          |
  | nil (real clock, inside synctest.Test)         |
  | dogerclockwork.Wrap(clockwork.NewFakeClock())  |
  | a consumer-written doger.Clock (manualClock)   |
```

#### Scenario: Health-check timeouts wait for the clock, not for wall time
```gherkin
Given an injector whose Clock is dogerclockwork.Wrap(F) for a clockwork fake clock F, and <option> is 10ms
And a service "svc" whose health check blocks until released
When the consumer starts HealthCheckWithContext on the injector
And 50ms of wall time pass without advancing F
Then the call has not returned
And after F advances by 10ms the result for "svc" is "doger: health check timeout: context deadline exceeded"

Examples:
  | option                   |
  | HealthCheckTimeout       |
  | HealthCheckGlobalTimeout |
```

<!-- cases: 1 default: previous requirement -->
<!-- cases: 2 overrides: a wrapped clockwork fake clock and a consumer-written Clock; each time read the container makes is a row -->
<!-- cases: 3 zero values: a 0s advance yields a zero build time, which explain output omits as today -->
<!-- cases: 4 boundaries: the exact-limit and one-before boundaries are in health-check-timeouts -->
<!-- cases: N/A 5 errors: measurement only; timeout errors are specified in health-check-timeouts -->
<!-- cases: N/A 6 not found/duplicate: no lookup involved -->
<!-- cases: 7 lifecycle: build (first invocation) and shutdown are the measured phases here; the start-step delays are pinned on a fake clock by container-start "Start timeout waits for the container clock" and container-logging "Slow-start warning waits for the container clock" -->
<!-- cases: 8 concurrency: the wall-time scenario shows the deadline is independent of goroutine timing; the blocked check is released before the test ends, so no goroutine leaks -->
<!-- cases: N/A 9 panics: unchanged -->
<!-- cases: 10 backward compatibility: field names, JSON keys and the omission of a zero build time are unchanged; message prefix "DI: " becomes "doger: " (module-identity) -->
