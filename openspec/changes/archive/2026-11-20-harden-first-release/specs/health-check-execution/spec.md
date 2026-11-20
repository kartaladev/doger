# Spec Delta

## MODIFIED Requirements

### Requirement: Each caller's wait is bounded by its own context and its own timeout
Every caller SHALL stop waiting when its context ends or its own path's bound elapses, measured from its call. The bounds are `HealthCheckTimeout` per service (0 means 10s, negative none) and `HealthCheckGlobalTimeout` for `HealthCheck()`, and `HealthCheckSingleTimeout` (0 means `HealthCheckTimeout`) for the single-service functions. For each unfinished service the caller SHALL get an error matching `ErrHealthCheckTimeout` and the cause, whoever started the check.

#### Scenario: Each caller keeps its own bound when it joins a check
```gherkin
Given an injector that is never started, with a fake clock, HealthCheckTimeout <timeout>, HealthCheckSingleTimeout <single> and HealthCheckGlobalTimeout <global>
And a service "db" whose HealthCheck() blocks until released and ignores any context, whose check another caller started earlier and is still running
When the consumer calls <entry> and the fake clock advances by <advance>
Then the call returns <result> for "db"

Examples:
  | timeout | single | global | entry                                  | advance | result                                                          |
  | 50ms    | 0      | 0      | HealthCheckNamed(injector, "db")       | 50ms    | "doger: health check timeout: context deadline exceeded"        |
  | 50ms    | 0      | 0      | HealthCheckNamed(injector, "db")       | 49ms    | nothing yet (still waiting)                                     |
  | 50ms    | 20ms   | 0      | HealthCheckNamed(injector, "db")       | 20ms    | "doger: health check timeout: context deadline exceeded"        |
  | 50ms    | -1ns   | 0      | HealthCheckNamed(injector, "db")       | 10m     | nothing yet (still waiting)                                     |
  | -1ns    | 0      | 30ms   | HealthCheck() on the root scope        | 30ms    | "doger: health check timeout: context deadline exceeded"        |
  | -1ns    | 0      | 30ms   | HealthCheck() on the root scope        | 29ms    | nothing yet (still waiting): its own bound, not the starter's   |
  | -1ns    | 0      | 30ms   | HealthCheckNamed(injector, "db")       | 10m     | nothing yet (still waiting): the global timeout does not apply  |
  | 0       | 0      | 0      | HealthCheckNamed(injector, "db")       | 10s     | "doger: health check timeout: context deadline exceeded"        |
  | 0       | 0      | 0      | HealthCheck() on the root scope        | 9.999s  | nothing yet (still waiting)                                     |
```

#### Scenario: One caller leaving does not cancel the check for the others
```gherkin
Given an injector that is never started, with a service "db" whose HealthCheck(ctx) blocks until released or ctx is done, then returns ctx.Err() or nil
When caller A calls HealthCheckWithContext with a context cancelled after 10ms, caller B calls HealthCheck() at the same time, A's context is cancelled, then the check is released
Then A's result for "db" is "doger: health check timeout: context canceled"
And B's result for "db" is nil
```

<!-- cases: 1 default: zero configuration bounds forced paths at 10s per service (the 0/0/0 rows), with no global bound -->
<!-- cases: 2 overrides: HealthCheckTimeout (positive, negative), HealthCheckSingleTimeout, HealthCheckGlobalTimeout and their interplay -->
<!-- cases: 3 zero values: 0 inherits or means the 10s default; a nil or background context waits for the bound -->
<!-- cases: 4 boundaries: one millisecond before and exactly at the bound -->
<!-- cases: 5 errors: the timeout message and errors.Is matches of health-check-timeouts -->
<!-- cases: N/A 6 not found/duplicate: not applicable -->
<!-- cases: 7 lifecycle: joining a check another caller started -->
<!-- cases: 8 concurrency: two callers with different contexts -->
<!-- cases: N/A 9 panics: a later requirement -->
<!-- cases: 10 backward compatibility: BEHAVIOR CHANGE (flagged) - HealthCheckTimeout 0 now means 10s on forced paths (health-check-timeouts); rows that needed no per-service bound use -1ns. Message prefix "DI: " becomes "doger: " (module-identity) -->

### Requirement: A panicking health check returns an error
A panic in a health check SHALL be recovered on every path. The result SHALL be an error matching `ErrHealthCheckPanic`, with the message `doger: health check panic: ` followed by the value. An error value SHALL also match, and a nil value SHALL match `*runtime.PanicNilError` with `errors.As`. When set, `HookContainerPanic(root, "healthcheck", value)` SHALL be called, and a panic it raises SHALL propagate (next requirement).

#### Scenario: Panic results
```gherkin
Given an injector with HookContainerPanic <panic hook>
And a service "db" whose HealthCheck() panics with <value>
When the consumer calls <entry>
Then the result for "db" has the message "<message>" and matches ErrHealthCheckPanic with errors.Is
And it also matches <match>
And <observed>

Examples:
  | panic hook                     | value   | entry                            | message                                                                   | match                                 | observed                                            |
  | nil                            | "boom"  | HealthCheck() on the root scope  | doger: health check panic: boom                                           | nothing else                          | the process keeps running                           |
  | nil                            | io.EOF  | HealthCheckNamed(injector, "db") | doger: health check panic: EOF                                            | io.EOF with errors.Is                 | the process keeps running                           |
  | nil                            | nil     | HealthReport() on an idle root   | doger: health check panic: runtime error: panic called with nil argument | *runtime.PanicNilError with errors.As | the process keeps running                           |
  | set, recording (point, value)  | "boom"  | HealthCheck() on the root scope  | doger: health check panic: boom                                           | nothing else                          | HookContainerPanic recorded ("healthcheck", "boom") |
```

#### Scenario: A panic in a scheduled round does not stop later rounds
```gherkin
Given an injector with a manual HealthScheduler, started, and a service "db" whose HealthCheck() panics with "boom" on its first call and returns nil afterwards
And a HookServiceHealthChanged hook that records calls
When the test runs the round twice
Then the report entry for "db" is HealthHealthy
And the hook recorded (Unknown, Unhealthy, "doger: health check panic: boom") then (Unhealthy, Healthy, nil)
```

<!-- cases: N/A 1 default: panics are not a default path -->
<!-- cases: 2 overrides: with and without HookContainerPanic -->
<!-- cases: 3 zero values: a nil panic value -->
<!-- cases: N/A 4 boundaries: not applicable -->
<!-- cases: 5 errors: message, ErrHealthCheckPanic, the wrapped error value, PanicNilError -->
<!-- cases: N/A 6 not found/duplicate: not applicable -->
<!-- cases: 7 lifecycle: forced, single-service, report and round paths -->
<!-- cases: N/A 8 concurrency: recovery happens on the goroutine running the check -->
<!-- cases: 9 panics: this requirement (regression D7); a panic raised by HookContainerPanic propagates (next requirement); runtime.Goexit is the ADDED requirement below -->
<!-- cases: 10 backward compatibility: a crash becomes an error (flagged); a single-service panic used to reach the caller as a panic; BREAKING message text - "DI: " becomes "doger: " (module-identity) -->

### Requirement: A joining caller gets what the check returned
A caller that joined a check SHALL get what that check returned once it returns within the caller's own bound and context, also when the starter's bound cancelled the check's context meanwhile. A check that returns its context's error after that context ended counts as a timeout, matching `ErrHealthCheckTimeout` and that cause.

#### Scenario: A joining caller gets the check's result after the starter's bound
```gherkin
Given an injector that is never started, with a fake clock and HealthCheckGlobalTimeout 30ms
And a service "db" whose check <check>
And a HealthCheck() call on the root scope started that check, and 10ms later the consumer called HealthCheckNamed(injector, "db")
When the fake clock advances by 50ms, so the HealthCheck() call returns its timeout for "db", and the check is released
Then the consumer's call returns <result>
And the health check of "db" was called 1 time

Examples:
  | check                                                                        | result                                                  |
  | blocks until released, ignoring its context, then returns nil                | nil                                                     |
  | blocks until released, ignoring its context, then returns errors.New("down") | the same "down" error                                   |
  | blocks until released or its context is done, then returns ctx.Err()         | "doger: health check timeout: context deadline exceeded"   |
```

<!-- cases: N/A 1 default: no option -->
<!-- cases: 2 overrides: the starter's HealthCheckGlobalTimeout; the joiner's single-service path, bounded by the 10s default -->
<!-- cases: N/A 3 zero values: not applicable -->
<!-- cases: 4 boundaries: the starter's bound passed before the check returned -->
<!-- cases: 5 errors: the check's nil, its own error, and its ended context's error as a timeout -->
<!-- cases: N/A 6 not found/duplicate: not applicable -->
<!-- cases: 7 lifecycle: the starter leaves at its bound while the joiner waits (verification 3, W1) -->
<!-- cases: 8 concurrency: two callers with different bounds share one check -->
<!-- cases: N/A 9 panics: a later requirement -->
<!-- cases: N/A 10 backward compatibility: on 76404ef each caller ran its own check -->

### Requirement: A caller whose context has already ended starts no check
A caller whose context has already ended when it calls, and that finds no check in flight for the service it can join, SHALL start none and SHALL wait for no check started later: it SHALL get its timeout error at once, matching `ErrHealthCheckTimeout` and the context's cause, as on 76404ef. Likewise, a caller queued behind a check it could not join, whose context ends before that check returns, SHALL NOT have a fresh check started for it: it SHALL get its timeout error.

#### Scenario: A caller whose context has already ended starts no check
```gherkin
Given an injector that is never started, with a Logger whose handler records every record at Debug level, and a service "db" whose HealthCheck(ctx) blocks until released or ctx is done, with no check of "db" in flight
When the consumer calls <entry> with a context that is already cancelled
Then the call returns at once with <result>
And the handler recorded no "health check started" record with service=db

Examples:
  | entry                                        | result                                                                        |
  | HealthCheckWithContext on the root scope     | "doger: health check timeout: context canceled" for "db" in the map              |
  | HealthCheckNamedWithContext(ctx, injector, "db") | "doger: health check timeout: context canceled"                              |
  | HealthReportWithContext on the root scope    | the entry for "db" HealthUnhealthy, "doger: health check timeout: context canceled" |
```

#### Scenario: A caller that gives up behind an abandoned check starts no fresh check
```gherkin
Given an injector that is never started, with a service "db" whose HealthCheck() blocks until released, ignoring its context, whose first check a HealthCheckNamedWithContext call left running, abandoned
And a caller of "db" queued behind that check, as HealthCheckWithContext queues each service before it waits
When the caller's context is cancelled <when>, then the abandoned check returns and the caller waits
Then the caller gets "doger: health check timeout: context canceled"
And the health check of "db" was called 1 time

Examples:
  | when             |
  | before it queues |
  | after it queues  |
```

<!-- cases: N/A 1 default: a context that has not ended behaves as in the previous requirement -->
<!-- cases: N/A 2 overrides: no option changes this rule -->
<!-- cases: 3 zero values: a context cancelled before the call -->
<!-- cases: 4 boundaries: no check in flight; joining a check in flight is the previous requirement -->
<!-- cases: 5 errors: "doger: health check timeout: context canceled" on each entry point (verification W2) -->
<!-- cases: N/A 6 not found/duplicate: not applicable -->
<!-- cases: 7 lifecycle: a check abandoned just before returns, then a caller that already gave up; or that caller gives up after queueing behind it, before it returns (verification W2, code review R14) -->
<!-- cases: 8 concurrency: no goroutine is started for such a caller -->
<!-- cases: N/A 9 panics: not applicable -->
<!-- cases: 10 backward compatibility: as on 76404ef, which never ran a check for such a caller -->

### Requirement: A check sees its starter's context
A check SHALL receive a context carrying the values and the deadline of the caller that started it, further bounded by that caller's own bound. The context SHALL be cancelled when that bound elapses, when every caller waiting for the check has stopped waiting, or when root shutdown begins. A caller that joins a check SHALL NOT change the check's context.

#### Scenario: Deadline and values reach the check
```gherkin
Given an injector that is never started, with a HealthcheckerWithContext "c" that records whether ctx.Deadline() is set and the value of key "trace"
When the consumer calls <entry> with a context carrying "trace" = "trace-1" and a deadline 1 minute ahead
Then the check recorded a deadline and "trace-1"

Examples:
  | entry                                    |
  | HealthCheckWithContext on the root scope |
  | HealthCheckNamedWithContext for "c"      |
  | HealthReportWithContext on the root scope |
```

#### Scenario: The only caller leaving cancels the check
```gherkin
Given an injector that is never started, with a service "db" whose HealthCheck(ctx) blocks until ctx is done, then records and returns ctx.Err()
When the only caller calls HealthCheckWithContext with a context cancelled after 10ms
Then the call returns "doger: health check timeout: context canceled" for "db"
And the check recorded context.Canceled
```

<!-- cases: 1 default: zero configuration -->
<!-- cases: 2 overrides: the starter's timeout further bounds the context -->
<!-- cases: N/A 3 zero values: a background context carries no deadline, as today -->
<!-- cases: N/A 4 boundaries: covered by the previous requirement -->
<!-- cases: 5 errors: the cancellation cause reaches the check -->
<!-- cases: N/A 6 not found/duplicate: not applicable -->
<!-- cases: 7 lifecycle: shutdown cancellation is in health-check-scheduling -->
<!-- cases: 8 concurrency: cancellation when the last caller leaves -->
<!-- cases: N/A 9 panics: a later requirement -->
<!-- cases: 10 backward compatibility: the deadline and values a check sees today are kept for its starter; a joining caller's values do not reach the check (flagged) -->

### Requirement: A service never has two checks in flight
While a service's check is running, any path that would check it SHALL wait for that check instead of starting another. A check that reached its bound, or that every caller left, SHALL stay in flight until it returns. A caller arriving then SHALL NOT receive its result. It SHALL wait, within its own bound, for that check to return, then start a fresh one.

#### Scenario: A check that ignores its context does not pile up
```gherkin
Given an injector that is never started, with HealthCheckTimeout 5ms and a negative HealthReportCacheTTL, so no HealthReport read is answered from a recorded result
And a service "db" whose HealthCheck() blocks until released and ignores any context
When the consumer calls <entry> 100 times, one after another
Then every call returns or reports "doger: health check timeout: context deadline exceeded" for "db"
And the health check of "db" was called 1 time
And after the check is released and the calls end, goleak reports no leaked goroutine

Examples:
  | entry                                  |
  | HealthCheck() on the root scope        |
  | HealthCheckNamed(injector, "db")       |
  | HealthReport() on the root scope       |
```

#### Scenario: Concurrent forced calls share the in-flight checks
```gherkin
Given an injector that is never started, with 10 Healthchecker values "svc-0" to "svc-9" that count their calls and block until released
When 50 goroutines call HealthCheck() on the root scope, and the checks are released once all 50 wait
Then each service's health check was called 1 time
And every call returns a map with 10 entries, each nil
And deleting a key from one returned map leaves the others with 10 entries
```

#### Scenario: A call that joins a running check gets its result
```gherkin
Given an injector that is never started, with a service "db" whose check blocks until released, then returns errors.New("down")
And a HealthCheck() call on the root scope is running that check
When the consumer calls HealthCheckNamed(injector, "db"), then the check is released
Then the call returns the same "down" error
And the health check of "db" was called 1 time
```

#### Scenario: An abandoned check is never joined
```gherkin
Given an injector that is never started, with a service "db" whose HealthCheck(ctx) blocks until released, then returns ctx.Err()
And a HealthCheckWithContext call whose context was cancelled after 10ms left that check running
When the consumer calls HealthCheck() on the root scope, then the first check is released
Then the call does not return context.Canceled for "db"
And the health check of "db" was called 2 times, never two at once
```

#### Scenario: Sequential calls check again
```gherkin
Given an injector that is never started, with a Healthchecker "db" that counts its calls
When the consumer calls <entry> <n> times, one after another
Then the health check of "db" was called <n> times

Examples:
  | entry                            | n |
  | HealthCheck() on the root scope  | 1 |
  | HealthCheck() on the root scope  | 3 |
  | HealthCheckNamed(injector, "db") | 2 |
```

<!-- cases: 1 default: zero configuration -->
<!-- cases: 2 overrides: with and without HealthCheckTimeout; caching off (negative HealthReportCacheTTL), so every report read meets the check in flight (verification 2, W1) -->
<!-- cases: N/A 3 zero values: not applicable -->
<!-- cases: 4 boundaries: 1, 2, 3, 50 and 100 calls -->
<!-- cases: 5 errors: a joining caller gets the check's error; an abandoned check's cancellation never reaches a new caller -->
<!-- cases: 6 not found/duplicate: an unknown name still returns ErrServiceNotFound, as today -->
<!-- cases: 7 lifecycle: joining, abandonment, and a fresh check after the old one returns -->
<!-- cases: 8 concurrency: never two checks at once; 50 concurrent forced calls; isolated result maps; goleak -->
<!-- cases: N/A 9 panics: a later requirement -->
<!-- cases: 10 backward compatibility: sequential calls still check each time; concurrent forced calls return the same maps with fewer check calls (regression D1) -->

### Requirement: A caller's per-service bound starts when its check starts running
While a check waits for a `HealthCheckParallelism` slot, the per-service bound of every caller waiting for it (`HealthCheckTimeout` on forced paths, the report bound on report paths) SHALL NOT run: it SHALL start when the check starts running. Each caller's context and `HealthCheckGlobalTimeout` SHALL still bound its wait from its call. That wait is bounded too: each check ahead of it holds its slot at most until the report bound has passed (previous requirement).

#### Scenario: A caller's per-service bound does not count the wait for a slot
```gherkin
Given an injector that is never started, with a fake clock, HealthCheckParallelism 1 and HealthCheckTimeout <timeout>
And Healthcheckers whose checks block on the fake clock for <takes>, one each, then return nil
When the consumer calls <entry> on the root scope and the fake clock advances until every check returned
Then every service is <result>

Examples:
  | timeout | takes      | entry          | result                   |
  | 0       | 9s, 0s     | HealthReport() | HealthHealthy in the report |
  | 0       | 9s, 2s     | HealthReport() | HealthHealthy in the report |
  | 0       | 6s, 6s, 6s | HealthReport() | HealthHealthy in the report |
  | 10s     | 9s, 2s     | HealthCheck()  | nil in the map           |
```

#### Scenario: HealthCheckGlobalTimeout bounds the wait for a slot from the call
```gherkin
Given an injector that is never started, with a fake clock, HealthCheckParallelism 1, HealthCheckTimeout 50ms and HealthCheckGlobalTimeout 30ms
And a Healthchecker "hog" in child scope "child", whose check blocks until released and holds the only slot through a HealthReport() read on that scope
And a Healthchecker root/"db" returning nil
When the consumer calls HealthCheck() on the root scope, so the check of "db" waits for the slot, and the fake clock advances by <advance>
Then the call has <returned>

Examples:
  | advance | returned                                                             |
  | 29ms    | not returned                                                         |
  | 30ms    | returned "doger: health check timeout: context deadline exceeded" for "db" |
```

<!-- cases: N/A 1 default: no limit by default, so no check waits for a slot -->
<!-- cases: 2 overrides: HealthCheckParallelism 1 with the report bound (HealthCheckTimeout 0) and with a positive HealthCheckTimeout -->
<!-- cases: 3 zero values: HealthCheckTimeout 0 uses the 10s report bound -->
<!-- cases: 4 boundaries: checks that each fit the bound while their total exceeds it; a check that returns at once; one millisecond before and at a global timeout reached while waiting for a slot (verification 3, W5) -->
<!-- cases: 5 errors: no timeout error for a check that fits its own bound (review R9) -->
<!-- cases: N/A 6 not found/duplicate: not applicable -->
<!-- cases: 7 lifecycle: report-path and forced checks waiting for their slot -->
<!-- cases: 8 concurrency: checks serialized by a limit of 1 -->
<!-- cases: N/A 9 panics: not applicable -->
<!-- cases: 10 backward compatibility: as with the 76404ef pool, whose workers applied HealthCheckTimeout when they ran a job -->

### Requirement: A caller queued behind an abandoned check keeps its bound from its call
A caller that waits for a check it could not join (an abandoned check still running) SHALL keep its per-service bound running from its call, through that wait and through the slot wait of the check started for it afterwards, so a hung abandoned check never holds it past its bound.

#### Scenario: A caller queued behind an abandoned check keeps its bound from its call
```gherkin
Given an injector that is never started, with HealthCheckParallelism 1 and HealthCheckTimeout 20ms
And a service "leaf" whose first check blocks until released, ignoring its context, and was left running, abandoned, by HealthCheckNamedWithContext with a context that timed out after 10ms
And a service "hog" whose check blocks until released, ignoring its context
When <entry> is called on the root scope, 1ms later a HealthCheck() call gives "hog" the only slot, which it holds within its own 20ms bound, and 4ms after that the first check of "leaf" is released, so the check started for the caller waits for that slot
Then the call returns 20ms after it was made, with <result>
And "leaf" was checked once when the call returns

Examples:
  | entry          | result                                                                                  |
  | HealthCheck()  | "doger: health check timeout: context deadline exceeded" for "leaf" in the map             |
  | HealthReport() | the entry for "leaf" HealthUnhealthy, "doger: health check timeout: context deadline exceeded" |
```

<!-- cases: N/A 1 default: no limit by default, so no check waits for a slot -->
<!-- cases: 2 overrides: HealthCheckParallelism 1, HealthCheckTimeout 20ms -->
<!-- cases: N/A 3 zero values: with no per-service bound, the caller's context alone bounds its wait -->
<!-- cases: 4 boundaries: the bound elapsing while the check started for the caller still waits for its slot -->
<!-- cases: 5 errors: the timeout of a forced and of a report-path caller (verification W1) -->
<!-- cases: N/A 6 not found/duplicate: not applicable -->
<!-- cases: 7 lifecycle: queued behind an abandoned check, then behind a slot held by another check (review R2) -->
<!-- cases: 8 concurrency: a limit of 1 held by a hung check -->
<!-- cases: N/A 9 panics: not applicable -->
<!-- cases: N/A 10 backward compatibility: new behavior of this change -->

### Requirement: Each underlying check runs once per call
A scope-wide run SHALL check each underlying service once. It SHALL NOT check an ancestor's service shadowed by a nearer scope's service of the same name, nor a default that yielded to another service. An alias or export SHALL NOT check its source again in a run that also checks the source: its entry, when the map has one, SHALL get the source's result.

#### Scenario: Each underlying check runs once
```gherkin
Given an injector that is never started, with <registrations>, whose checks count their calls
When the consumer calls HealthCheck() on <target>
Then the map is <map>, where name(T) is the service name inferred for type T
And the counts are <counts>

Examples:
  | registrations                                                                 | target         | map                                        | counts   |
  | root/"svc" (A) and "child"/"svc" (B)                                          | scope "child"  | {"svc": nil}                               | A 0, B 1 |
  | root/"svc" (A) and "child"/"svc" (B)                                          | the root scope | {"svc": nil}                               | A 1, B 0 |
  | root *DB value (A) with Alias[*DB, Healthchecker]                                | the root scope | {name(*DB): nil, name(Healthchecker): nil} | A 1      |
  | root *DB value (A) returning errors.New("down") with Alias[*DB, Healthchecker]   | the root scope | both entries the same "down" error         | A 1      |
  | "child"/"svc" (A) exported to the root with ExportNamed                       | the root's own *Scope | {"svc": nil}                        | A 1      |
  | "child"/"svc" (A) exported to the root with ExportNamed                       | scope "child"  | {"svc": nil}                               | A 1      |
  | "child"/"svc" (A) registered AsDefault, and root/"svc" (B)                    | scope "child"  | {"svc": nil}                               | A 0, B 1 |
```

<!-- cases: N/A 1 default: no option -->
<!-- cases: N/A 2 overrides: none -->
<!-- cases: N/A 3 zero values: not applicable -->
<!-- cases: 4 boundaries: shadowing seen from the child and from the root -->
<!-- cases: 5 errors: the alias shares the target's error -->
<!-- cases: 6 not found/duplicate: shadowing, aliasing, exporting and yielded defaults are the duplicates (regressions D4, D5; export and default rows keep the behavior of scope-export and default-registrations, verified on 76404ef) -->
<!-- cases: 7 lifecycle: child scopes -->
<!-- cases: N/A 8 concurrency: not applicable -->
<!-- cases: N/A 9 panics: not applicable -->
<!-- cases: 10 backward compatibility: map keys and values are unchanged; only the number of calls drops -->

## ADDED Requirements

### Requirement: A health check that exits its goroutine does not block later checks
A health check that ends by calling `runtime.Goexit` (directly, or through `t.FailNow` and the like) SHALL finish its check: every caller waiting for it SHALL get an error matching `ErrHealthCheckPanic` whose message ends with `health check panic: runtime.Goexit`, the result SHALL be recorded like any other, and the next check of that service SHALL run the checker again. `HookContainerPanic` SHALL NOT be called for it.

#### Scenario: Goexit in a check is reported and does not block the next check
```gherkin
Given an injector that is never started, with HealthCheckTimeout 50ms, HealthCheckParallelism <parallelism> and a negative HealthReportCacheTTL
And a service "db" whose HealthCheck() calls runtime.Goexit() on its first call and returns nil afterwards, counting its calls
When the consumer calls <entry> twice in a row
Then the first call returns an error matching ErrHealthCheckPanic whose message ends with "health check panic: runtime.Goexit"
And the second call returns <second>
And the health check of "db" was called 2 times in total

Examples:
  | parallelism    | entry                                      | second                         |
  | 0 (unlimited)  | HealthCheckNamed(injector, "db")           | nil                            |
  | 0 (unlimited)  | HealthCheck() on the root scope            | a map with "db": nil           |
  | 1              | HealthCheck() on the root scope            | a map with "db": nil           |
  | 0 (unlimited)  | HealthReport() on the root scope           | a report with "db" HealthHealthy |
```

#### Scenario: Every waiter of a check that exits its goroutine gets the error
```gherkin
Given an injector that is never started, with HookContainerPanic recording its calls
And a service "db" whose HealthCheck() blocks until released, then calls runtime.Goexit()
When 10 goroutines call HealthCheckNamed(injector, "db"), and the check is released once all 10 wait
Then all 10 calls return an error matching ErrHealthCheckPanic whose message ends with "health check panic: runtime.Goexit"
And HookContainerPanic was not called
And no goroutine is leaked once the calls returned
```

#### Scenario: A scheduled round survives a check that exits its goroutine
```gherkin
Given a started injector with a manual HealthScheduler and a service "db" whose HealthCheck() calls runtime.Goexit() on its first call and returns nil afterwards
When the test runs the round twice
Then after the first round HealthReport() shows "db" HealthUnhealthy with an error matching ErrHealthCheckPanic
And after the second round HealthReport() shows "db" HealthHealthy
```

<!-- cases: N/A 1 default: Goexit is not a default path -->
<!-- cases: 2 overrides: with and without HealthCheckParallelism (slot released on Goexit); HookContainerPanic set and not called -->
<!-- cases: N/A 3 zero values: Goexit carries no value -->
<!-- cases: 4 boundaries: 1 and 10 waiters; first call only vs every call -->
<!-- cases: 5 errors: matches ErrHealthCheckPanic with errors.Is; message suffix pinned -->
<!-- cases: N/A 6 not found/duplicate: not applicable -->
<!-- cases: 7 lifecycle: forced paths, report reads and scheduled rounds -->
<!-- cases: 8 concurrency: 10 concurrent waiters; no goroutine leak (goleak) -->
<!-- cases: 9 panics: Goexit is not a panic, so the hook is not called; real panics keep the existing requirement -->
<!-- cases: 10 backward compatibility: previously the in-flight entry was never removed and later checks timed out without running (defect F6, reproduced by TestAudit_HealthCheckGoexitWedgesService) -->
