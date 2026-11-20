# health-check-timeouts Specification

## Purpose

Defines what a health check returns when its context ends or a configured health-check timeout elapses, so that timeout results are deterministic and always matchable with `errors.Is`.

## Requirements

### Requirement: A health check whose context ended returns a timeout error
When the service is a health checker and its health check's context is done (cancelled or past its deadline) by the time its result is observed, the call SHALL return an error that matches `ErrHealthCheckTimeout` and the context's error with `errors.Is`, with the message `doger: health check timeout: <context error>`, whatever the service's check returned. This holds for the per-service functions and for scope-wide health checks, on every call.

#### Scenario: Health check on an ended context is a timeout
```gherkin
Given a root scope with a service "svc" whose health check <check>
And a context that is <context>
When the consumer calls <entry> for "svc" 1000 times, each with a fresh scope and context
Then every call returns an error with message "doger: health check timeout: <ctx message>"
And every error matches ErrHealthCheckTimeout and <ctx error> with errors.Is

Examples:
  | entry                                       | context                            | check                                                     | ctx message               | ctx error                |
  | HealthCheckNamedWithContext                 | cancelled before the call          | Healthchecker returning nil                               | context canceled          | context.Canceled         |
  | HealthCheckNamedWithContext                 | past its deadline before the call  | Healthchecker returning nil                               | context deadline exceeded | context.DeadlineExceeded |
  | HealthCheckNamedWithContext                 | cancelled before the call          | HealthcheckerWithContext returning ctx.Err()              | context canceled          | context.Canceled         |
  | HealthCheckNamedWithContext                 | cancelled during the check         | HealthcheckerWithContext returning nil once ctx is done   | context canceled          | context.Canceled         |
  | HealthCheckNamedWithContext                 | cancelled during the check         | HealthcheckerWithContext returning errors.New("db down") once ctx is done | context canceled | context.Canceled |
  | HealthCheckWithContext (injector)           | cancelled before the call          | Healthchecker returning nil                               | context canceled          | context.Canceled         |
  | HealthCheckWithContext (injector)           | past its deadline before the call  | HealthcheckerWithContext returning ctx.Err()              | context deadline exceeded | context.DeadlineExceeded |
  | HealthCheckWithContext (child scope)        | cancelled before the call          | Healthchecker returning nil, registered in the root scope | context canceled          | context.Canceled         |
```

#### Scenario: Health check that finishes before its context ends keeps its result
```gherkin
Given a root scope with a service "svc" whose health check returns <result>
And a context that is cancelled only after the call returns
When the consumer calls <entry> for "svc"
Then the call returns <expected>

Examples:
  | entry                              | result                  | expected                |
  | HealthCheckNamedWithContext        | nil                     | nil                     |
  | HealthCheckNamedWithContext        | errors.New("db down")   | the same "db down" error |
  | HealthCheckWithContext (injector)  | nil                     | nil for "svc"           |
  | HealthCheckWithContext (injector)  | errors.New("db down")   | the same "db down" error for "svc" |
  | HealthCheckNamed (no context)      | nil                     | nil                     |
```

<!-- cases: 1 default: the "keeps its result" scenario is the zero-configuration path (default timeout not reached, live context) -->
<!-- cases: N/A 2 overrides: the rule has no option by design; timeout options are the "Configured health-check timeouts" requirement -->
<!-- cases: 3 zero values: context.Background() (never done) is the "HealthCheckNamed (no context)" row; a nil context is unchanged behavior and not part of this requirement -->
<!-- cases: 4 boundaries: before the call vs during the check; a result observed exactly at the deadline is covered by the clock-driven boundary rows of the next requirement -->
<!-- cases: 5 errors: each row states the message and both errors.Is matches; a service error returned in time stays as is -->
<!-- cases: N/A 6 not found/duplicate: an unknown service name keeps returning ErrServiceNotFound, unchanged -->
<!-- cases: 7 lifecycle: the child-scope row checks a service inherited from the root scope -->
<!-- cases: 8 concurrency: the 1000-call repetition pins the formerly random outcome; cancellation during the check is a row -->
<!-- cases: N/A 9 panics: panic behavior of health checks is in health-check-execution -->
<!-- cases: 10 backward compatibility: BREAKING message text - the "DI: " prefix becomes "doger: " (module-identity); errors.Is(err, ErrHealthCheckTimeout) and the context-error match are unchanged -->

### Requirement: A service without a health check never times out
A service that is not a health checker when its health check begins (a value that implements neither `Healthchecker` nor `HealthcheckerWithContext`, a lazy service not yet built, a transient service, or an alias to one of these) SHALL report `nil`, whatever the state of the context, on the per-service functions and on scope-wide health checks. An unknown service name SHALL keep returning an error matching `ErrServiceNotFound`, whatever the state of the context.

#### Scenario: Service without a health check on an ended context
```gherkin
Given a root scope with a service "svc" that is <service>
And a context that is <context>
When the consumer calls <entry> for "svc" 1000 times, each with a fresh scope and context
Then every result for "svc" is <expected>

Examples:
  | entry                                | service                                         | context                           | expected                          |
  | HealthCheckNamedWithContext          | a plain value 42                                | cancelled before the call         | nil                               |
  | HealthCheckWithContext (injector)    | a plain value 42                                | cancelled before the call         | nil                               |
  | HealthCheckWithContext (injector)    | a plain value 42                                | past its deadline before the call | nil                               |
  | HealthCheckWithContext (injector)    | a lazy Healthchecker that was never invoked     | cancelled before the call         | nil                               |
  | HealthCheckWithContext (injector)    | a transient service                             | cancelled before the call         | nil                               |
  | HealthCheckWithContext (injector)    | an alias (As) to a lazy Healthchecker never invoked | cancelled before the call     | nil                               |
  | HealthCheckWithContext (child scope) | a plain value 42 registered in the root scope   | cancelled before the call         | nil                               |
  | HealthCheckNamedWithContext          | not registered (name "missing")                 | cancelled before the call         | an error matching ErrServiceNotFound |
```

<!-- cases: 1 default: every row runs with zero configuration (no timeout options) -->
<!-- cases: N/A 2 overrides: no option governs this rule by design -->
<!-- cases: 3 zero values: a plain value with no methods is the zero case of "has a health check" -->
<!-- cases: 4 boundaries: cancelled vs past-deadline contexts; a lazy service that is built is a health checker and follows the previous requirement -->
<!-- cases: 5 errors: no error for a service without a check; ErrServiceNotFound keeps precedence over the context state -->
<!-- cases: 6 not found/duplicate: the "missing" row -->
<!-- cases: 7 lifecycle: an unbuilt lazy service, a transient, an alias, and a service inherited from the root scope -->
<!-- cases: 8 concurrency: the 1000-call repetition pins the formerly random scope-wide result (about 998/1000 timeouts before) -->
<!-- cases: N/A 9 panics: unchanged and out of scope -->
<!-- cases: 10 backward compatibility: HealthCheckNamedWithContext on a service without a check already returned nil; the scope-wide result on a done context changes from a random timeout to nil, as the existing code comment intended -->

### Requirement: Configured health-check timeouts elapse on the injector's clock
`HealthCheckTimeout` (per service) and `HealthCheckGlobalTimeout` (per scope-wide check) SHALL be measured on the injector's clock. A check still running when its timeout elapses SHALL return the timeout error with `context deadline exceeded`; a check that returns before SHALL keep its result. A zero `HealthCheckTimeout` SHALL mean `DefaultHealthReportCheckTimeout` (10s) and a negative one no timeout. A zero or negative `HealthCheckGlobalTimeout` SHALL mean no global timeout.

#### Scenario: Timeout options decide the result on the clock
```gherkin
Given an injector with a fake clock and options <options>
And a service "svc" whose health check returns <result> after <duration> on that clock
When the consumer calls HealthCheckWithContext on the injector with context.Background()
And the fake clock advances by <advance>
Then the result for "svc" is <expected>
And the same result is returned on each of 1000 runs, each with a fresh injector and fake clock

Examples:
  | options                                                  | result                | duration | advance | expected                                                  |
  | HealthCheckTimeout: 10ms                                 | nil                   | 20ms     | 20ms    | "doger: health check timeout: context deadline exceeded"  |
  | HealthCheckTimeout: 10ms                                 | nil                   | 9ms      | 9ms     | nil                                                       |
  | HealthCheckTimeout: 10ms                                 | nil                   | 10ms     | 10ms    | "doger: health check timeout: context deadline exceeded"  |
  | HealthCheckTimeout: 10ms                                 | errors.New("db down") | 5ms      | 5ms     | the same "db down" error                                  |
  | HealthCheckGlobalTimeout: 10ms                           | nil                   | 20ms     | 20ms    | "doger: health check timeout: context deadline exceeded"  |
  | HealthCheckGlobalTimeout: 10ms                           | nil                   | 9ms      | 9ms     | nil                                                       |
  | HealthCheckTimeout: 50ms, HealthCheckGlobalTimeout: 10ms | nil                   | 20ms     | 20ms    | "doger: health check timeout: context deadline exceeded"  |
  | HealthCheckTimeout: 10ms, HealthCheckGlobalTimeout: 50ms | nil                   | 20ms     | 20ms    | "doger: health check timeout: context deadline exceeded"  |
  | (none: both 0)                                           | nil                   | 9.999s   | 9.999s  | nil                                                       |
  | (none: both 0)                                           | nil                   | 10s      | 10s     | "doger: health check timeout: context deadline exceeded"  |
  | (none: both 0)                                           | nil                   | 1h       | 1h      | "doger: health check timeout: context deadline exceeded"  |
  | HealthCheckTimeout: -1ns                                 | nil                   | 1h       | 1h      | nil                                                       |
  | HealthCheckTimeout: -1ns, HealthCheckGlobalTimeout: 10ms | nil                   | 20ms     | 20ms    | "doger: health check timeout: context deadline exceeded"  |
  | HealthCheckTimeout: -1ns, HealthCheckGlobalTimeout: -1ns | nil                   | 1h       | 1h      | nil                                                       |
```

#### Scenario: Sequential health checks share the global timeout
```gherkin
Given an injector with a fake clock, HealthCheckParallelism 1 and HealthCheckGlobalTimeout 50ms
And services "a", "b", "c" and "d" whose health checks each return nil after 20ms on that clock
When the consumer calls HealthCheckWithContext on the injector with context.Background()
And the fake clock advances in 10ms steps until the call returns
Then exactly 2 results are nil
And exactly 2 results are "doger: health check timeout: context deadline exceeded"
```

#### Scenario: Health check does not time out before the clock reaches the timeout
```gherkin
Given an injector with a fake clock and HealthCheckTimeout <timeout>
And a service "svc" whose health check blocks until released
When the consumer starts HealthCheckWithContext on the injector
And the fake clock advances by <before>
Then the call has not returned
And after the fake clock advances by another 1ms the result for "svc" is "doger: health check timeout: context deadline exceeded"

Examples:
  | timeout     | before  |
  | 10ms        | 9ms     |
  | 0 (default) | 9.999s  |
```

#### Scenario: RootScope.HealthCheck is bounded with zero configuration
```gherkin
Given an injector created with New(), inside synctest.Test
And a service "svc" whose health check blocks until released, ignoring its context
When the consumer calls HealthCheck() on the root scope
Then the call returns after 10s of fake time with a map whose "svc" entry matches ErrHealthCheckTimeout and context.DeadlineExceeded
And once the check is released no goroutine is leaked
```

<!-- cases: 1 default: the "(none: both 0)" rows and the New() scenario: a 10s per-service bound with zero configuration, no global bound -->
<!-- cases: 2 overrides: per-service and global timeout rows, both combined (the shorter one wins), and -1ns opting out of the per-service bound -->
<!-- cases: 3 zero values: both options 0 means the 10s default per service and no global timeout -->
<!-- cases: 4 boundaries: 9ms / 9.999s (one before), 10ms / 10s (exactly at the limit), 20ms / 1h (past it), each pinned over 1000 runs; the "does not time out before" scenario pins the boundary from the other side -->
<!-- cases: 5 errors: timeout message and errors.Is matching per the previous requirement; a service error returned in time is kept -->
<!-- cases: N/A 6 not found/duplicate: unrelated to timeouts -->
<!-- cases: 7 lifecycle: HealthCheckParallelism 1 runs checks through the health-check slots; Shutdown at the end of each test -->
<!-- cases: 8 concurrency: the sequential scenario fixes the outcome by count, not by map order; timed-out checks that are still running finish when the clock advances or are released, so no goroutine leaks -->
<!-- cases: N/A 9 panics: in health-check-execution -->
<!-- cases: 10 backward compatibility: BEHAVIOR CHANGE (flagged, BREAKING) - HealthCheckTimeout 0 used to mean no per-service timeout on forced paths; it now means 10s, as report reads and scheduled rounds already did. Migration: set HealthCheckTimeout to a negative value for unbounded checks -->

### Requirement: A container-set deadline counts once the injector's clock reaches it
A result observed when the injector's clock has reached the earliest deadline the container set for that check (from `HealthCheckTimeout`, its 10s default, or `HealthCheckGlobalTimeout`) SHALL be the timeout error with `context deadline exceeded`, even if the check returned at that same instant and the context is not yet marked done. A deadline carried only by the consumer's context SHALL count only once that context is done, never by the injector's clock.

#### Scenario: A consumer's own deadline is not measured on the injector's clock
```gherkin
Given an injector with a fake clock and options <options>
And a service "svc" whose health check returns nil after 2h on that fake clock
When the consumer calls HealthCheckWithContext on the injector with a context from context.WithTimeout(context.Background(), 1h) on the real clock
And the fake clock advances by 2h
Then the result for "svc" is nil

Examples:
  | options                                         |
  | HealthCheckTimeout: -1ns (no container deadline) |
  | HealthCheckTimeout: 3h                          |
```

<!-- cases: 1 default: with HealthCheckTimeout 0 the container deadline is 10s; the default exact-limit row is in "Configured health-check timeouts elapse on the injector's clock" -->
<!-- cases: 2 overrides: a container timeout (3h) later than the consumer's deadline; -1ns removes the container deadline so only the consumer's context counts -->
<!-- cases: 3 zero values: HealthCheckTimeout 0 is the 10s default (previous requirement); a negative value means no container deadline -->
<!-- cases: 4 boundaries: the 10ms / 10ms and 10s / 10s rows of the previous requirement are the exact-limit cases, pinned over 1000 runs; here the fake clock passes the consumer's real-clock deadline without it counting -->
<!-- cases: 5 errors: the timeout cause is context.DeadlineExceeded when the clock decided it -->
<!-- cases: N/A 6 not found/duplicate: unrelated to timeouts -->
<!-- cases: N/A 7 lifecycle: unchanged -->
<!-- cases: 8 concurrency: removes the race between the check's result and the context's own cancellation at the deadline instant -->
<!-- cases: N/A 9 panics: unchanged and out of scope -->
<!-- cases: 10 backward compatibility: the "no container deadline" row now needs a negative HealthCheckTimeout, because 0 means 10s (previous requirement) -->

### Requirement: Per-service health checks honor HealthCheckTimeout
`HealthCheck[T]`, `HealthCheckWithContext[T]`, `HealthCheckNamed` and `HealthCheckNamedWithContext` SHALL bound the check by `HealthCheckTimeout`, measured on the injector's clock: a positive value SHALL be the bound, 0 SHALL mean `DefaultHealthReportCheckTimeout` (10s), and a negative value SHALL mean no bound. `HealthCheckGlobalTimeout` SHALL NOT apply to them.

#### Scenario: Per-service checks are bounded by HealthCheckTimeout
```gherkin
Given an injector with a fake clock and HealthCheckTimeout <timeout> and HealthCheckGlobalTimeout <global>
And a service "svc" whose health check returns nil after <duration> on that clock
When the consumer calls <function> for "svc" with context.Background()
And the fake clock advances by <duration>
Then the result is <expected>

Examples:
  | function                     | timeout | global | duration | expected                                                 |
  | HealthCheckNamed             | 10ms    | 0      | 20ms     | "doger: health check timeout: context deadline exceeded" |
  | HealthCheckNamedWithContext  | 10ms    | 0      | 20ms     | "doger: health check timeout: context deadline exceeded" |
  | HealthCheck[*Svc]            | 10ms    | 0      | 9ms      | nil                                                      |
  | HealthCheckWithContext[*Svc] | 10ms    | 0      | 10ms     | "doger: health check timeout: context deadline exceeded" |
  | HealthCheckNamed             | 0       | 10ms   | 20ms     | nil                                                      |
  | HealthCheckNamed             | 0       | 0      | 9.999s   | nil                                                      |
  | HealthCheckNamed             | 0       | 0      | 1h       | "doger: health check timeout: context deadline exceeded" |
  | HealthCheckNamed             | -1ns    | 0      | 1h       | nil                                                      |
```

#### Scenario: A per-service check returning an error in time keeps it
```gherkin
Given an injector with HealthCheckTimeout 10ms
And a service "svc" whose health check returns errors.New("db down") immediately
When the consumer calls HealthCheckNamed for "svc"
Then the result is the same "db down" error
And errors.Is(result, ErrHealthCheckTimeout) is false
```

<!-- cases: 1 default: HealthCheckTimeout 0 bounds per-service checks at 10s (the 9.999s and 1h rows) -->
<!-- cases: 2 overrides: a positive HealthCheckTimeout bounds the check; a negative one removes the bound; HealthCheckGlobalTimeout alone does not apply (row 5) -->
<!-- cases: 3 zero values: both options 0 -->
<!-- cases: 4 boundaries: 9ms before, 10ms exactly at, 20ms past the timeout; 9.999s before the default -->
<!-- cases: 5 errors: timeout message per the first requirement; a service error in time is kept -->
<!-- cases: N/A 6 not found: an unknown name keeps returning ErrServiceNotFound, unchanged -->
<!-- cases: N/A 7 lifecycle: per-service checks have no scope traversal beyond name resolution, unchanged -->
<!-- cases: 8 concurrency: fake-clock rows run in synctest; timed-out checks finish when the clock advances, so no goroutine leaks -->
<!-- cases: N/A 9 panics: in health-check-execution -->
<!-- cases: 10 compatibility: BEHAVIOR CHANGE (flagged, BREAKING) - with HealthCheckTimeout 0 per-service checks were unbounded; they are now bounded at 10s. Migration: HealthCheckTimeout -1ns, or HealthCheckSingleTimeout -1ns for single-service calls only -->

### Requirement: HealthCheckSingleTimeout overrides the single-service bound
`InjectorOpts.HealthCheckSingleTimeout` SHALL bound `HealthCheck[T]`, `HealthCheckWithContext[T]`, `HealthCheckNamed` and `HealthCheckNamedWithContext` in place of `HealthCheckTimeout`: a positive value SHALL be the bound, a negative value SHALL mean no bound, and 0 SHALL keep `HealthCheckTimeout`. The checks run by `HealthCheck()` SHALL keep `HealthCheckTimeout`.

#### Scenario: Single-service bound
```gherkin
Given an injector with a fake clock, HealthCheckTimeout <timeout> and HealthCheckSingleTimeout <single>
And a service "svc" whose health check returns nil after <duration> on that clock
When the consumer calls <function> for "svc" with context.Background()
And the fake clock advances by <duration>
Then the result is <expected>

Examples:
  | function                     | timeout | single | duration | expected                                              |
  | HealthCheckNamed             | 10ms    | 0      | 20ms     | "doger: health check timeout: context deadline exceeded" |
  | HealthCheckNamed             | 10ms    | 30ms   | 20ms     | nil                                                   |
  | HealthCheckNamed             | 10ms    | 30ms   | 40ms     | "doger: health check timeout: context deadline exceeded" |
  | HealthCheckNamedWithContext  | 0       | 10ms   | 10ms     | "doger: health check timeout: context deadline exceeded" |
  | HealthCheck[*Svc]            | 10ms    | -1ns   | 1h       | nil                                                   |
  | HealthCheckWithContext[*Svc] | 0       | -1ns   | 1h       | nil                                                   |
  | injector.HealthCheck()       | 10ms    | -1ns   | 20ms     | a map whose "svc" entry is "doger: health check timeout: context deadline exceeded" |
  | injector.HealthCheck()       | 0       | 10ms   | 20ms     | a map whose "svc" entry is nil                        |
```

<!-- cases: 1 default: 0 inherits HealthCheckTimeout (first row), so existing configurations are unchanged -->
<!-- cases: 2 overrides: a positive value replaces HealthCheckTimeout for single-service checks, larger or smaller -->
<!-- cases: 3 zero values: 0 inherits; both options 0 is covered by the previous requirement -->
<!-- cases: 4 boundaries: exactly at the bound (10ms row), before and past it -->
<!-- cases: 5 errors: the timeout message of the first requirement -->
<!-- cases: N/A 6 duplicates: one option -->
<!-- cases: 7 lifecycle: HealthCheck() keeps HealthCheckTimeout in both directions -->
<!-- cases: 8 concurrency: fake-clock rows run in synctest -->
<!-- cases: N/A 9 panics: unchanged -->
<!-- cases: N/A 10 compatibility: new option; 0 keeps today's behavior -->
