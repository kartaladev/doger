# Spec Delta

## MODIFIED Requirements

### Requirement: Services not started yet are not checked by reports
Once the container left `PhaseIdle`, a covered service whose start is not complete SHALL be listed as `HealthUnknown` and SHALL NOT be checked by a report read or a scheduled round. Its start is not complete while it is a Starter that `Start`, `Refresh` or `Scope.Start` would start and that has not been started successfully, or a component of a kind with a registered handler that has not handled it successfully, such as a Runnable whose `Run` has not returned, including a call abandoned under `InjectorOpts.StartTimeout` that still runs. `HealthCheck()` and the single-service functions SHALL still check it.

#### Scenario: A service registered after Ready waits for Refresh
```gherkin
Given a started injector in PhaseReady
And an eager value "late" registered afterwards, implementing Start(ctx) error and HealthCheck() error counting its calls and returning errors.New("not started") until started
When the consumer <action>, then calls HealthReport() on the root scope
Then the entry for "late" is <state> and its check was called <calls> times by that read

Examples:
  | action                         | state           | calls |
  | does nothing else              | HealthUnknown   | 0     |
  | calls Refresh, which succeeds  | HealthHealthy   | 1     |
```

#### Scenario: Idle containers check every service
```gherkin
Given an injector that is never started, with an eager value "svc" implementing Start(ctx) error and HealthCheck() error returning nil
When the consumer calls HealthReport() on the root scope
Then the entry for "svc" is HealthHealthy
```

#### Scenario: Rounds skip a service not started yet, forced paths check it
```gherkin
Given a started injector in PhaseReady, <scheduler>
And an eager value "late" registered afterwards, implementing Start(ctx) error and HealthCheck() error counting its calls and returning nil
When <caller> runs
Then the health check of "late" was called <calls> times
And HealthReport() on the root scope shows "late" as HealthUnknown

Examples:
  | scheduler                     | caller                                 | calls |
  | with a manual HealthScheduler | the test's run of the stored round     | 0     |
  | without a HealthScheduler     | HealthCheck() on the root scope        | 1     |
  | without a HealthScheduler     | HealthCheckNamed(injector, "late")     | 1     |
```

#### Scenario: A component is checked only once its kind handlers handled it
```gherkin
Given a started injector in PhaseReady created with InjectorOpts{StartTimeout: <timeout>}, inside synctest.Test
And lazy "job" registered afterwards with AsRunnable(), implementing HealthCheck() error counting its calls and returning nil, whose Run(ctx) error <run>
And the consumer called Refresh(), which returned <refresh>
When the consumer calls HealthReport() on the root scope
Then the entry for "job" is <state> and its check was called <calls> times by that read

Examples:
  | timeout | run                                                       | refresh                         | state         | calls |
  | 0       | returns nil at once                                       | nil                             | HealthHealthy | 1     |
  | 5s      | blocks until the test releases it                         | an error matching ErrStartTimeout | HealthUnknown | 0     |
  | 5s      | blocked until the test released it, and has since returned nil | an error matching ErrStartTimeout | HealthHealthy | 1     |
```

<!-- cases: 1 default: no option -->
<!-- cases: N/A 2 overrides: Refresh or Scope.Start is the way to make it checked -->
<!-- cases: N/A 3 zero values: an idle container keeps today's semantics -->
<!-- cases: 4 boundaries: before and after Refresh; a Runnable before and after its abandoned Run returned -->
<!-- cases: N/A 5 errors: the service's own error is not reported before its start is complete -->
<!-- cases: N/A 6 not found/duplicate: not applicable -->
<!-- cases: 7 lifecycle: registration and Extend after Ready, incremental start, a kind handler call abandoned under StartTimeout -->
<!-- cases: N/A 8 concurrency: not applicable -->
<!-- cases: N/A 9 panics: not applicable -->
<!-- cases: 10 backward compatibility: forced paths still check such a service; rounds skip it like reads (verification 3, W5). BEHAVIOR CHANGE (flagged): a component whose kind handler has not handled it yet, such as a Runnable whose Run is still running, is no longer checked by reads and rounds, so a stuck Runnable no longer reports healthy; Starters and Runnables now follow one rule -->
