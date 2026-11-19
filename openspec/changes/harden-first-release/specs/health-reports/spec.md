# Spec Delta

## MODIFIED Requirements

### Requirement: A monitored container's report runs only first checks
While a `HealthScheduler` is scheduling the container's rounds, a report SHALL have `Monitored` true. A read SHALL return the recorded results, and SHALL check on demand only covered services that have no recorded result. A covered service whose first check is in flight, started by a round or by another read, SHALL be joined: the read SHALL wait for it within its own context and report bound, and report an unfinished one as the report bound requirement says.

#### Scenario: Reads on a monitored container cost nothing
```gherkin
Given a started injector with a manual HealthScheduler and a Healthchecker "db" that counts its calls
And the test ran the round once
When 1000 goroutines call HealthReport() on the root scope concurrently
Then every report has Monitored true and "db" in HealthHealthy
And the health check of "db" was called 1 time in total
```

#### Scenario: A service never checked is checked once by a read
```gherkin
Given a started injector with a manual HealthScheduler, whose round the test has not run, and a fake clock set to 2026-01-01T00:00:00Z
And a Healthchecker "db" whose check <behavior>
When the consumer calls HealthReport() on the root scope
Then the report has Monitored true
And "db" is <entry>
And the health check of "db" was called <calls> time in total

Examples:
  | behavior                                                                                      | entry                                                                                      | calls |
  | returns nil                                                                                   | HealthHealthy                                                                              | 1     |
  | is already in flight from another reader, and is released 5ms after this read began          | HealthHealthy, the read having waited for that check                                       | 1     |
  | is already in flight from another reader, blocking until released, with the read's ctx at 20ms | HealthUnhealthy with an error matching ErrHealthCheckTimeout and context.DeadlineExceeded | 1     |
```

#### Scenario: A read right after Ready joins the first scheduled check
```gherkin
Given an injector with a fake clock, HealthCheckTimeout <timeout>, and a HealthScheduler whose ScheduleHealthRounds starts round(ctx) on its own goroutine at once
And a Healthchecker "db" whose check blocks until released, then returns <result>, counting its calls
And the consumer called Start, and the scheduled round's check of "db" is in flight
When the consumer calls HealthReport() on the root scope, and <event>
Then the report has Monitored true
And "db" is <entry>
And the health check of "db" was called 1 time in total

Examples:
  | timeout | result             | event                                          | entry                                                                    |
  | 0       | nil                | the check is released                          | HealthHealthy                                                            |
  | 0       | errors.New("down") | the check is released                          | HealthUnhealthy with the same "down" error                               |
  | 0       | nil                | the fake clock advances by 10s                 | HealthUnhealthy with an error matching ErrHealthCheckTimeout and context.DeadlineExceeded |
  | 50ms    | nil                | the fake clock advances by 50ms                | HealthUnhealthy with an error matching ErrHealthCheckTimeout and context.DeadlineExceeded |
```

#### Scenario: Concurrent first reads of a monitored container share the round's check
```gherkin
Given a started injector with a manual HealthScheduler and a Healthchecker "db" that blocks until released, counting its calls
And the test started the round on its own goroutine, so the first check of "db" is in flight
When 50 goroutines call HealthReport() on the root scope, and the check is released once all 50 wait
Then all 50 reports have Monitored true and "db" in HealthHealthy
And the health check of "db" was called 1 time in total
```

<!-- cases: N/A 1 default: monitoring needs a scheduler -->
<!-- cases: 2 overrides: HealthCheckTimeout 0 (10s report bound) and 50ms; the reader's own context -->
<!-- cases: 3 zero values: no result yet; HealthCheckTimeout 0 -->
<!-- cases: 4 boundaries: 1, 50 and 1000 readers; first check in flight; the bound reached exactly (advance equal to the bound) -->
<!-- cases: 5 errors: the check's own error is returned unchanged; an unfinished check matches ErrHealthCheckTimeout and the cause -->
<!-- cases: N/A 6 not found/duplicate: not applicable -->
<!-- cases: 7 lifecycle: Ready before and after a round; a read issued right after Start while the first round runs; a service appearing while monitored is checked by the next read -->
<!-- cases: 8 concurrency: 50 readers joining one in-flight check; 1000 concurrent readers of recorded results -->
<!-- cases: N/A 9 panics: a panicking check is specified in health-check-execution -->
<!-- cases: 10 backward compatibility: BEHAVIOR CHANGE (flagged) - a read that met a first check in flight returned HealthUnknown at once; it now waits for that check within its bound, as the HealthReportWithContext godoc already promised -->

### Requirement: Only health checkers appear in a report, once per underlying check
A report SHALL include a service only when it is a health checker at read time. It SHALL leave out values without a health check, transient services, and lazy services not yet built. An alias or an export SHALL NOT add an entry of its own: when its source is covered, the source's entry is listed once, at the source's scope.

#### Scenario: Report membership
```gherkin
Given an injector that is never started, with <registration>
When the consumer calls HealthReport() on <target>
Then the report lists <listed>

Examples:
  | registration                                                           | target              | listed                  |
  | a plain value root/"answer" = 42                                       | the RootScope       | no service              |
  | a lazy Healthchecker root/"db" that was never invoked                  | the RootScope       | no service              |
  | a lazy Healthchecker root/"db" that was invoked                        | the RootScope       | root/"db"               |
  | a transient Healthchecker root/"tmp" that was invoked                  | the RootScope       | no service              |
  | a Healthchecker value root/"db" and an alias "hc" to it (AliasNamed)      | the RootScope       | root/"db" only          |
  | an alias "hc" to a lazy Healthchecker root/"db" that was never invoked | the RootScope       | no service              |
  | a Healthchecker value "child"/"db" exported to the root                | the RootScope       | "child"/"db" only       |
  | a Healthchecker value "child"/"db" exported to the root                | the root's own *Scope | "child"/"db"          |
```

<!-- cases: N/A 1 default: membership has no option -->
<!-- cases: N/A 2 overrides: none -->
<!-- cases: 3 zero values: a container with no checker yields an empty Services slice -->
<!-- cases: N/A 4 boundaries: covered by the ordering requirement -->
<!-- cases: N/A 5 errors: no error path -->
<!-- cases: 6 not found/duplicate: an alias or export does not duplicate the entry -->
<!-- cases: 7 lifecycle: a lazy service before and after build -->
<!-- cases: N/A 8 concurrency: a lazy service being built is in health-check-execution -->
<!-- cases: N/A 9 panics: not applicable -->
<!-- cases: 10 backward compatibility: HealthCheck() keeps its own keys, including alias keys (health-check-execution) -->

### Requirement: A report read is bounded by its caller's context and a per-service bound
A read SHALL stop waiting for a service when ctx is done or when the report bound elapses: `HealthCheckTimeout` when positive, `DefaultHealthReportCheckTimeout` (10s) when 0, and no bound when negative. It SHALL report each unfinished check as `HealthUnhealthy` (`HealthDegraded` when optional) with an error matching `ErrHealthCheckTimeout` and the cause. `HealthCheckGlobalTimeout` SHALL NOT apply. `HealthReport()` and a nil ctx SHALL use `context.Background()`.

#### Scenario: A reader that stops waiting
```gherkin
Given an injector that is never started, with a fake clock and HealthCheckTimeout <timeout>
And a service "db" whose HealthCheck() blocks until released and ignores any context
When the consumer calls HealthReportWithContext with a context that is <context>, and the fake clock advances by <advance>
Then the read returns <returns>
And the entry for "db" is <entry>

Examples:
  | timeout | context                       | advance | returns                    | entry                                                                  |
  | 0       | never cancelled               | 10s     | after the advance          | HealthUnhealthy, "doger: health check timeout: context deadline exceeded" |
  | 0       | never cancelled               | 9.999s  | not yet                    | (still waiting)                                                        |
  | 20ms    | never cancelled               | 20ms    | after the advance          | HealthUnhealthy, "doger: health check timeout: context deadline exceeded" |
  | -1ns    | never cancelled               | 10m     | not yet                    | (still waiting)                                                        |
  | 0       | cancelled before the call     | 0s      | at once                    | HealthUnhealthy, "doger: health check timeout: context canceled"          |
```

#### Scenario: A reader leaving does not affect other readers
```gherkin
Given an injector that is never started, with a service "db" whose HealthCheck(ctx) blocks until released or ctx is done
And reader A calls HealthReportWithContext with a context cancelled after 10ms
And reader B calls HealthReport() at the same time
When A's context is cancelled, then the check is released
Then A's entry for "db" is HealthUnhealthy with an error matching ErrHealthCheckTimeout and context.Canceled
And B's entry for "db" is HealthHealthy
And a third read within the TTL returns HealthHealthy for "db" without a new check
```

#### Scenario: HealthCheckGlobalTimeout does not bound a read
```gherkin
Given an injector that is never started, with a fake clock, HealthCheckTimeout 0 and HealthCheckGlobalTimeout 30ms
And a service "db" whose HealthCheck() blocks until released and ignores any context
When the consumer calls HealthReport() on the root scope and the fake clock advances by <advance>
Then the read <returns>

Examples:
  | advance | returns                                                                  |
  | 30ms    | has not returned                                                         |
  | 10s     | has returned, with "db" HealthUnhealthy matching ErrHealthCheckTimeout   |
```

#### Scenario: A nil context reads like context.Background()
```gherkin
Given an injector that is never started, with a Healthchecker root/"db" returning nil and a Healthchecker "child"/"svc" returning nil
When the consumer calls HealthReportWithContext(nil) on <target>
Then the call does not panic, and the report is HealthHealthy with <entries> entries

Examples:
  | target              | entries |
  | the RootScope       | 2       |
  | child scope "child" | 2       |
```

<!-- cases: 1 default: 10s bound with HealthCheckTimeout 0 -->
<!-- cases: 2 overrides: a positive and a negative HealthCheckTimeout; the caller's ctx; HealthCheckGlobalTimeout does not apply (verification 3, W5) -->
<!-- cases: 3 zero values: HealthCheckTimeout 0; nil context (verification 3, W5) -->
<!-- cases: 4 boundaries: one millisecond before and exactly at the 10s default -->
<!-- cases: 5 errors: the timeout error matches ErrHealthCheckTimeout and the cause with errors.Is -->
<!-- cases: N/A 6 not found/duplicate: not applicable -->
<!-- cases: N/A 7 lifecycle: on-demand phases are in the previous requirement -->
<!-- cases: 8 concurrency: one reader leaving while another waits -->
<!-- cases: N/A 9 panics: specified in health-check-execution -->
<!-- cases: 10 backward compatibility: the timeout message matches health-check-timeouts -->

### Requirement: A check is recorded with what it returned
A completed check SHALL be recorded with what it returned, also when its starter's bound cancelled its context meanwhile, unless a report path already recorded its timeout. A check that returns its context's error after that context ended SHALL be recorded as a timeout matching `ErrHealthCheckTimeout`.

#### Scenario: A check its starter's bound cancelled is recorded with its result
```gherkin
Given an injector that is never started, with a fake clock and HealthCheckGlobalTimeout 30ms
And a service "db" whose check <check>
And a HealthCheck() call on the root scope started that check, and 10ms later the consumer called HealthCheckNamed(injector, "db")
When the fake clock advances by 50ms, so the HealthCheck() call returns its timeout for "db", the check is released, and the consumer reads HealthReport() on the root scope
Then the entry for "db" is <entry>

Examples:
  | check                                                                        | entry                                                                  |
  | blocks until released, ignoring its context, then returns nil                | HealthHealthy                                                          |
  | blocks until released, ignoring its context, then returns errors.New("down") | HealthUnhealthy, "down"                                                |
  | blocks until released or its context is done, then returns ctx.Err()         | HealthUnhealthy, "doger: health check timeout: context deadline exceeded" |
```

<!-- cases: N/A 1 default: no option -->
<!-- cases: 2 overrides: the starter's HealthCheckGlobalTimeout -->
<!-- cases: N/A 3 zero values: not applicable -->
<!-- cases: 4 boundaries: the starter's bound passed before the check returned -->
<!-- cases: 5 errors: the check's nil, its own error, and its ended context's error as a timeout -->
<!-- cases: N/A 6 not found/duplicate: not applicable -->
<!-- cases: 7 lifecycle: a forced starter leaves at its bound, which records no timeout (design H8), while a joiner waits (verification 3, W1) -->
<!-- cases: N/A 8 concurrency: specified in health-check-execution -->
<!-- cases: N/A 9 panics: not applicable -->
<!-- cases: N/A 10 backward compatibility: new API -->

### Requirement: Nothing is recorded for an abandoned check, a service that left, or after shutdown began
A check cancelled because every caller waiting for it stopped waiting SHALL NOT be recorded, and neither SHALL a check whose service left the container (its scope shut down, or its registration was replaced) while it was in flight or before it started. Once root `Shutdown` began, no result SHALL be recorded and no health hook SHALL be called.

#### Scenario: An abandoned check is not recorded
```gherkin
Given an injector that is never started, with a service "db" whose HealthCheck(ctx) returns ctx.Err() once ctx is done
And the consumer's only read used a context cancelled after 10ms
When the consumer calls HealthReportWithContext with a context that is already cancelled
Then "db" has no recorded result: its CheckedAt is zero
```

#### Scenario: Results after shutdown began are not recorded
```gherkin
Given a started injector with a Healthchecker "db" whose recorded result is nil
And a HookServiceHealthChanged hook that records calls
And a Shutdowner "svc" that, during Shutdown, makes "db" return errors.New("closed") and calls HealthCheck() on the root scope
When the consumer calls Shutdown
Then the hook recorded only (Unknown, Healthy, nil)
```

<!-- cases: N/A 1 default: no option -->
<!-- cases: N/A 2 overrides: none -->
<!-- cases: 3 zero values: a zero CheckedAt for a service never recorded -->
<!-- cases: N/A 4 boundaries: not applicable -->
<!-- cases: N/A 5 errors: not applicable -->
<!-- cases: N/A 6 not found/duplicate: not applicable -->
#### Scenario: A check in flight when its service leaves is not recorded
```gherkin
Given an injector that is never started, with a HookContainerHealthChanged hook that records calls
And a Healthchecker "db" in child scope "child" whose check blocks until released, then returns errors.New("down")
And a HealthCheck() call on scope "child" is running that check
When <leaving>, then the check is released and returns
Then the injector holds no health record for that registration
And the container hook recorded nothing after the service left

Examples:
  | leaving                                                     |
  | scope "child" is shut down                                  |
  | "db" is replaced in scope "child" with OverrideNamedValue    |
```

#### Scenario: A service that leaves before its check starts is not recorded
```gherkin
Given an injector with a Healthchecker root/"a" returning nil and a Healthchecker "b" in child scope "child" returning errors.New("down"), and <mode>
And a Logger whose handler shuts scope "child" down when it handles the "health check started" record for service=a, after "b" was listed
When <reader> runs
Then the injector holds no health record and no member for "b"
And the overall state the container hooks track is HealthHealthy

Examples:
  | mode                                        | reader                                  |
  | it is never started                         | the consumer's HealthReport() on the root scope |
  | a manual HealthScheduler, started           | the test's run of the stored round      |
```

#### Scenario: A timeout of a check started after its service left is not recorded
```gherkin
Given an injector with HealthCheckTimeout 20ms, a Healthchecker root/"a" returning nil and a Healthchecker "b" in child scope "child" that blocks until released, ignoring its context, and <mode>
And a Logger whose handler shuts scope "child" down when it handles the "health check started" record for service=a, after "b" was listed
When <reader> runs and reaches the 20ms bound of "b"
Then the injector holds no health record and no member for "b"
And the overall state the container hooks track is HealthHealthy

Examples:
  | mode                              | reader                                          |
  | it is never started               | the consumer's HealthReport() on the root scope |
  | a manual HealthScheduler, started | the test's run of the stored round              |
```

<!-- cases: 7 lifecycle: during shutdown, no alert is raised for services being shut down; a service leaving with its check in flight (review R1) or before it starts, its result or a report-path timeout (review R11, R12) -->
<!-- cases: 8 concurrency: abandonment by every waiter; a check returning after its service left -->
<!-- cases: N/A 9 panics: not applicable -->
<!-- cases: N/A 10 backward compatibility: new behavior -->

### Requirement: Report reads and rounds start no check for a service that left
A report read or round SHALL NOT start the check of a covered service that left the container (its scope shut down, or its registration was replaced) before the read or round reached it; its entry SHALL show no result, as for any service that left before its check started. A service that leaves after the read or round reached it is covered by the previous requirement. Forced paths and the single-service functions SHALL run the checks they listed, as today.

#### Scenario: A service that left before the read reached it is not checked
```gherkin
Given an injector with a Healthchecker root/"a" returning nil and a Healthchecker "b" in child scope "child" that counts its calls, and <mode>
And a Logger whose handler shuts scope "child" down when it handles the "health check started" record for service=a, after "b" was listed
When <reader> runs
Then the health check of "b" was never called

Examples:
  | mode                              | reader                                          |
  | it is never started               | the consumer's HealthReport() on the root scope |
  | a manual HealthScheduler, started | the test's run of the stored round              |
```

<!-- cases: N/A 1 default: no option -->
<!-- cases: N/A 2 overrides: none -->
<!-- cases: N/A 3 zero values: not applicable -->
<!-- cases: 4 boundaries: a service that left just before the read reached it; leaving after is the previous requirement -->
<!-- cases: N/A 5 errors: not applicable -->
<!-- cases: N/A 6 not found/duplicate: not applicable -->
<!-- cases: 7 lifecycle: a child scope shut down between listing and checking, on a read and on a round (code review R15) -->
<!-- cases: 8 concurrency: a service leaving between the read's registration check and the start of its check is not recorded (previous requirement) -->
<!-- cases: N/A 9 panics: not applicable -->
<!-- cases: 10 backward compatibility: forced paths and single-service functions keep running a listed service's check, as today -->

## ADDED Requirements

### Requirement: Health and phase enums have a text form
`HealthState` and `ContainerPhase` SHALL implement `encoding.TextMarshaler` and `encoding.TextUnmarshaler` with the lower-case names their `String()` returns. Marshalling a value outside the declared constants SHALL fail, and unmarshalling an unknown name SHALL fail without changing the receiver.

#### Scenario: HealthState and ContainerPhase text round trip
```gherkin
Given the value <value>
When the consumer calls MarshalText on it and UnmarshalText on the result into a new variable
Then MarshalText returns <text> and error <marshal error>
And the new variable equals <value> when MarshalText succeeded

Examples:
  | value              | text        | marshal error |
  | HealthUnknown      | "unknown"   | nil           |
  | HealthHealthy      | "healthy"   | nil           |
  | HealthDegraded     | "degraded"  | nil           |
  | HealthUnhealthy    | "unhealthy" | nil           |
  | HealthState(42)    | nil         | non-nil       |
  | PhaseIdle          | "idle"      | nil           |
  | PhaseStarting      | "starting"  | nil           |
  | PhaseStarted       | "started"   | nil           |
  | PhaseReady         | "ready"     | nil           |
  | PhaseFailed        | "failed"    | nil           |
  | PhaseClosing       | "closing"   | nil           |
  | PhaseClosed        | "closed"    | nil           |
  | ContainerPhase(-1) | nil         | non-nil       |
```

#### Scenario: Unknown text is rejected
```gherkin
Given a HealthState variable s holding HealthHealthy and a ContainerPhase variable p holding PhaseReady
When the consumer calls s.UnmarshalText(<text>) and p.UnmarshalText(<text>)
Then both calls return a non-nil error
And s is still HealthHealthy and p is still PhaseReady

Examples:
  | text        |
  | ""          |
  | "Healthy"   |
  | "bogus"     |
```

<!-- cases: 1 default: the zero values HealthUnknown and PhaseIdle marshal to "unknown" and "idle" -->
<!-- cases: N/A 2 overrides: no option -->
<!-- cases: 3 zero values: zero enum values and empty text -->
<!-- cases: 4 boundaries: first and last constants; one past the last (42, -1) -->
<!-- cases: 5 errors: out-of-range values and unknown names fail -->
<!-- cases: N/A 6 not found/duplicate: not applicable -->
<!-- cases: N/A 7 lifecycle: pure value methods -->
<!-- cases: N/A 8 concurrency: pure value methods -->
<!-- cases: N/A 9 panics: no input panics -->
<!-- cases: 10 backward compatibility: String() output is unchanged; text methods are additive -->

### Requirement: A health report marshals to JSON for monitoring
`json.Marshal` of a `HealthReport` the library returns SHALL succeed and SHALL emit lower-camel keys: `state` and `phase` as their text form, `monitored`, `checkedAt`, and `services`. Each service SHALL emit `service` (`scopeId`, `scopeName`, `service`), `tags`, `optional`, `state`, `error` (the error's message, also when it is empty; omitted only when the error is nil), `checkedAt` and `pendingSince` (RFC 3339, omitted when zero), and `duration` (a Go duration string).

#### Scenario: Report JSON
```gherkin
Given an injector that is never started, whose Clock is a fake clock set to 2026-01-01T00:00:00Z
And a Healthchecker "db" registered in the root scope with WithHealthTags(HealthTagReadiness), whose check advances the fake clock by 2s and returns <result>
And the consumer read report r := HealthReport()
When the consumer calls json.Marshal(r)
Then the error is nil
And the JSON equals <json>

Examples:
  | result             | json |
  | nil                | {"state":"healthy","phase":"idle","monitored":false,"checkedAt":"2026-01-01T00:00:02Z","services":[{"service":{"scopeId":"<root id>","scopeName":"[root]","service":"db"},"tags":["readiness"],"optional":false,"state":"healthy","checkedAt":"2026-01-01T00:00:02Z","duration":"2s"}]} |
  | errors.New("down") | {"state":"unhealthy","phase":"idle","monitored":false,"checkedAt":"2026-01-01T00:00:02Z","services":[{"service":{"scopeId":"<root id>","scopeName":"[root]","service":"db"},"tags":["readiness"],"optional":false,"state":"unhealthy","error":"down","checkedAt":"2026-01-01T00:00:02Z","duration":"2s"}]} |
```

#### Scenario: Empty and in-flight entries
```gherkin
Given <report>
When the consumer calls json.Marshal on it
Then the error is nil
And the JSON <expectation>

Examples:
  | report                                                                                                          | expectation                                                                                     |
  | the zero HealthReport{}                                                                                         | equals {"state":"unknown","phase":"idle","monitored":false,"services":[]}                       |
  | a report whose "db" entry has a check in flight since 2026-01-01T00:00:00Z and no recorded result               | has for "db" "state":"unknown", "pendingSince":"2026-01-01T00:00:00Z", "duration":"0s", and no "error" or "checkedAt" key |
  | a report whose "db" entry has Err set to a wrapped error fmt.Errorf("ping: %w", context.DeadlineExceeded)        | has for "db" "error":"ping: context deadline exceeded"                                          |
```

<!-- cases: 1 default: a zero-config report marshals with no option -->
<!-- cases: N/A 2 overrides: consumers needing another shape map the exported fields themselves -->
<!-- cases: 3 zero values: the zero HealthReport (services as [] not null; zero times omitted); a nil Err omitted -->
<!-- cases: 4 boundaries: zero, one service; zero and positive durations -->
<!-- cases: 5 errors: wrapped errors are emitted as their full message; an error with an empty message keeps the "error" key; json.Marshal never fails on a report the library returns, while a hand-built report with an out-of-range State or Phase fails, as the strict enum text methods do -->
<!-- cases: N/A 6 not found/duplicate: not applicable -->
<!-- cases: 7 lifecycle: an entry with a check in flight (pendingSince) -->
<!-- cases: N/A 8 concurrency: a report is the caller's own copy (existing requirement) -->
<!-- cases: N/A 9 panics: marshalling never panics, including for a nil Err -->
<!-- cases: 10 backward compatibility: previously json.Marshal emitted Go field names, numeric enums and "Err":{}; consumers relying on that shape must switch to the documented keys (BREAKING for that output, flagged) -->
