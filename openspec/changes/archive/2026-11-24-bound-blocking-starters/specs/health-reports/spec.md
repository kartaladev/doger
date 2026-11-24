# Spec Delta

## MODIFIED Requirements

### Requirement: Services not started yet are not checked by reports
Once the container left `PhaseIdle`, a covered service whose start is not complete SHALL be listed as `HealthUnknown` and SHALL NOT be checked by a report read or a scheduled round. Its start is not complete while it is a Starter that `Start`, `Refresh` or `Scope.Start` would start and that has not been started successfully, or a component of a kind with a handler that has not handled it successfully, such as a Runnable whose `Run` has not returned, including a call abandoned under `InjectorOpts.StartTimeout` that still runs, or a component registered after Ready that no `Refresh` or `Scope.Start` handled yet, as a Starter registered after Ready waits for one. A kind without a handler SHALL NOT hold a component back. Nor SHALL a kind that `Start`, `Refresh` or `Scope.Start` passed over for the component, having no handler when that run collected it, while no later run attempted it (collected the component while the kind had a handler, or called its handler for it): a handler given to it after that run does not demote the started, healthy component, which stays checked until a later `Refresh` or `Scope.Start` attempts the kind. A handler call that failed leaves the kind attempted, so it holds the component back. `HealthCheck()` and the single-service functions SHALL still check it.

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

#### Scenario: A component registered after Ready waits for Refresh
```gherkin
Given a started injector in PhaseReady whose kind "k" has a StageAfterStart handler calling Run
And value "job" registered afterwards <binding>, implementing Run(ctx) error, returning nil, and HealthCheck() error counting its calls and returning an error until Run was called
When the consumer calls HealthReport() on the root scope
Then the entry for "job" is HealthUnknown and its check was called 0 times
When the consumer calls Refresh(), which returns nil, then HealthReport()
Then the entry for "job" is HealthHealthy

Examples:
  | name                     | binding               |
  | runnable                 | with AsRunnable()     |
  | custom_kind_with_handler | with kind "k"'s With option |
```

#### Scenario: Only the kinds with a handler that the start pipeline did not pass over hold a component back
```gherkin
Given a started injector in PhaseReady created with <opts>, inside synctest.Test
And lazy "job" registered afterwards <binding>, implementing Run(ctx) error, which <run>, and HealthCheck() error counting its calls and returning nil
And the consumer called Refresh(), which returned <refresh>
And then <after>
When the consumer calls HealthReport() on the root scope
Then the entry for "job" is <state>, its check was called <calls> times by that read, and its CheckedAt is <checked-at>

Examples:
  | name                                   | opts                                                            | binding                                                                                      | run                               | refresh                           | after                                                                                                    | state         | calls | checked-at |
  | runnable_autodetected                  | InjectorOpts{EnableRunnerAutoDetection: true}                   | with no option                                                                               | returns nil at once               | nil                               | nothing else                                                                                             | HealthHealthy | 1     | set        |
  | runnable_autodetected_running          | InjectorOpts{EnableRunnerAutoDetection: true, StartTimeout: 5s} | with no option                                                                               | blocks until the test releases it | an error matching ErrStartTimeout | nothing else                                                                                             | HealthUnknown | 0     | zero       |
  | kind_without_handler                   | InjectorOpts{}                                                  | with kind "audit"'s With option, "audit" having no handler                                   | returns nil at once               | nil                               | nothing else                                                                                             | HealthHealthy | 1     | set        |
  | several_kinds_one_failed               | InjectorOpts{}                                                  | with kinds "a" and "b", whose StageAfterStart handlers return nil and errors.New("b failed") | returns nil at once               | an error wrapping "b failed"      | nothing else                                                                                             | HealthUnknown | 0     | zero       |
  | first_handler_after_ready              | InjectorOpts{}                                                  | with kind "audit"'s With option, "audit" having no handler                                   | returns nil at once               | nil                               | the consumer gives "audit" a StageAfterStart handler                                                     | HealthHealthy | 1     | set        |
  | first_handler_after_checked            | InjectorOpts{}                                                  | with kind "zz"'s With option, "zz" having no handler                                         | returns nil at once               | nil                               | a HealthReport() read shows "job" HealthHealthy, then the consumer gives "zz" a StageAfterStart handler | HealthHealthy | 0     | set        |
  | first_handler_after_ready_then_refresh | InjectorOpts{}                                                  | with kind "audit"'s With option, "audit" having no handler                                   | returns nil at once               | nil                               | the consumer gives "audit" a StageAfterStart handler returning nil, then calls Refresh(), which returns nil | HealthHealthy | 1     | set        |
  | first_handler_then_run_fails_before_stage | InjectorOpts{}                                               | with kind "audit"'s With option, "audit" having no handler                                   | returns nil at once               | nil                               | the consumer gives "audit" a StageAfterStart handler returning nil and registers lazy Starter "bad" whose Start returns errors.New("bad failed"), then calls Refresh(), which fails with it before job's StageAfterStart step | HealthUnknown | 0     | zero       |
  | first_handler_during_run_fails         | InjectorOpts{}                                                  | with kind "audit"'s With option, "audit" having no handler                                   | returns nil at once               | nil                               | the consumer registers lazy Starter "w" whose Start gives "audit" a StageAfterStart handler returning errors.New("audit failed"), then calls Refresh(), which fails with it | HealthUnknown | 0     | zero       |
```

<!-- cases: 1 default: no option -->
<!-- cases: 2 overrides: EnableRunnerAutoDetection binds a Runner implicitly (runnable_autodetected*); a handler given after Ready does not demote a started component, which stays checked until the next Refresh handles it (first_handler_after_ready, first_handler_after_checked, first_handler_after_ready_then_refresh; BEHAVIOR CHANGE of the earlier approved first_handler_after_ready row, decided by the user at code review finding 4); a run that collects the component once the kind has a handler attempts it even when it fails before the kind's stage (first_handler_then_run_fails_before_stage), and so does a run that calls a handler given during it (first_handler_during_run_fails); a component registered after Ready whose kind has a handler waits for Refresh like a Starter ("A component registered after Ready waits for Refresh", code review 40.5) -->
<!-- cases: 3 zero values: an idle container keeps today's semantics; a kind with no handler holds nothing back (kind_without_handler) -->
<!-- cases: 4 boundaries: before and after Refresh; a Runnable before and after its abandoned Run returned -->
<!-- cases: 5 errors: a service bound to several kinds stays unknown while one of their handlers failed (several_kinds_one_failed); the service's own error is not reported before its start is complete -->
<!-- cases: N/A 6 not found/duplicate: not applicable -->
<!-- cases: 7 lifecycle: registration and Extend after Ready, incremental start, a kind handler call abandoned under StartTimeout -->
<!-- cases: 8 concurrency: the eligibility check and the run both read the kind handler list under kindsMu, released before startedMu is taken for the handled and passed-over records, so the two locks never nest; TestHealthReport_ComponentCheckedOnceHandled/runnable_running reads while the abandoned Run still runs on its own goroutine, runnable_late_return reads after that goroutine recorded the late return, and TestHealthReport_OnlyHandledKindsHoldBack/first_handler_after_ready reads after a Handle given after Ready; all run under -race. No row races a read against the handled record being written. -->
<!-- cases: N/A 9 panics: not applicable -->
<!-- cases: 10 backward compatibility: forced paths still check such a service; rounds skip it like reads (verification 3, W5). BEHAVIOR CHANGE (flagged): a component whose kind handler has not handled it yet, such as a Runnable whose Run is still running, is no longer checked by reads and rounds, so a stuck Runnable no longer reports healthy; Starters and Runnables now follow one rule -->
