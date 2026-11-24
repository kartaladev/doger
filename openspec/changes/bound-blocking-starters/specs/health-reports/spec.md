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

#### Scenario: Only the kinds with a registered handler hold a component back
```gherkin
Given a started injector in PhaseReady created with <opts>, inside synctest.Test
And lazy "job" registered afterwards <binding>, implementing Run(ctx) error, which <run>, and HealthCheck() error counting its calls and returning nil
And the consumer called Refresh(), which returned <refresh>
And then <after>
When the consumer calls HealthReport() on the root scope
Then the entry for "job" is <state> and its check was called <calls> times by that read

Examples:
  | name                          | opts                                                            | binding                                                                                      | run                               | refresh                           | after                                                | state         | calls |
  | runnable_autodetected         | InjectorOpts{EnableRunnerAutoDetection: true}                   | with no option                                                                               | returns nil at once               | nil                               | nothing else                                         | HealthHealthy | 1     |
  | runnable_autodetected_running | InjectorOpts{EnableRunnerAutoDetection: true, StartTimeout: 5s} | with no option                                                                               | blocks until the test releases it | an error matching ErrStartTimeout | nothing else                                         | HealthUnknown | 0     |
  | kind_without_handler          | InjectorOpts{}                                                  | with kind "audit"'s With option, "audit" having no handler                                   | returns nil at once               | nil                               | nothing else                                         | HealthHealthy | 1     |
  | several_kinds_one_failed      | InjectorOpts{}                                                  | with kinds "a" and "b", whose StageAfterStart handlers return nil and errors.New("b failed") | returns nil at once               | an error wrapping "b failed"      | nothing else                                         | HealthUnknown | 0     |
  | first_handler_after_ready     | InjectorOpts{}                                                  | with kind "audit"'s With option, "audit" having no handler                                   | returns nil at once               | nil                               | the consumer gives "audit" a StageAfterStart handler | HealthUnknown | 0     |
```

<!-- cases: 1 default: no option -->
<!-- cases: 2 overrides: EnableRunnerAutoDetection binds a Runner implicitly (runnable_autodetected*); a handler given after Ready holds its kind's components back until the next Refresh (first_handler_after_ready) -->
<!-- cases: 3 zero values: an idle container keeps today's semantics; a kind with no handler holds nothing back (kind_without_handler) -->
<!-- cases: 4 boundaries: before and after Refresh; a Runnable before and after its abandoned Run returned -->
<!-- cases: 5 errors: a service bound to several kinds stays unknown while one of their handlers failed (several_kinds_one_failed); the service's own error is not reported before its start is complete -->
<!-- cases: N/A 6 not found/duplicate: not applicable -->
<!-- cases: 7 lifecycle: registration and Extend after Ready, incremental start, a kind handler call abandoned under StartTimeout -->
<!-- cases: 8 concurrency: the eligibility check reads the kind handler list (kindsMu) and the handled records (startedMu) without nesting the two locks; TestHealthReport_ComponentCheckedOnceHandled/runnable_running reads while the abandoned Run still runs on its own goroutine, runnable_late_return reads after that goroutine recorded the late return, and TestHealthReport_OnlyHandledKindsHoldBack/first_handler_after_ready reads after a Handle given after Ready; all run under -race. No row races a read against the handled record being written. -->
<!-- cases: N/A 9 panics: not applicable -->
<!-- cases: 10 backward compatibility: forced paths still check such a service; rounds skip it like reads (verification 3, W5). BEHAVIOR CHANGE (flagged): a component whose kind handler has not handled it yet, such as a Runnable whose Run is still running, is no longer checked by reads and rounds, so a stuck Runnable no longer reports healthy; Starters and Runnables now follow one rule -->
