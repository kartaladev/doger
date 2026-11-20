# container-clock Specification

## Purpose

Defines the container's time source: the `InjectorOpts.Clock` option, its real-clock default, and the timeouts and durations the container measures on it, so consumers can control time in their own tests.

## Requirements

### Requirement: The injector clock defaults to the real clock
`InjectorOpts.Clock` (type `doger.Clock`) SHALL set the injector's time source. A nil `Clock`, including `New()` and `NewWithOpts(nil)`, SHALL use `doger.RealClock()`, so timeouts and durations follow wall time as before.

#### Scenario: Zero configuration measures health-check timeouts on wall time
```gherkin
Given an injector created by <constructor> with HealthCheckTimeout 10ms where the constructor allows options, inside synctest.Test
And a service "svc" whose health check returns nil after time.Sleep(<sleep>)
When the consumer calls HealthCheckWithContext on the injector with context.Background()
Then the result for "svc" is <expected>

Examples:
  | constructor                                                      | sleep | expected                                                 |
  | NewWithOpts(&InjectorOpts{HealthCheckTimeout: 10ms})             | 20ms  | "doger: health check timeout: context deadline exceeded" |
  | NewWithOpts(&InjectorOpts{HealthCheckTimeout: 10ms})             | 5ms   | nil                                                      |
  | NewWithOpts(&InjectorOpts{HealthCheckTimeout: 10ms, Clock: nil}) | 20ms  | "doger: health check timeout: context deadline exceeded" |
  | NewWithOpts(&InjectorOpts{HealthCheckTimeout: 10ms, Clock: RealClock()}) | 20ms | "doger: health check timeout: context deadline exceeded" |
  | New() (10s default timeout)                                      | 20ms  | nil                                                      |
  | NewWithOpts(nil) (10s default timeout)                           | 20ms  | nil                                                      |
  | New() (10s default timeout)                                      | 10s   | "doger: health check timeout: context deadline exceeded" |
```

<!-- cases: 1 default: every row uses the default real clock; synctest makes wall time deterministic -->
<!-- cases: 2 overrides: an explicit RealClock(); a fake or custom clock is the next requirements -->
<!-- cases: 3 zero values: Clock nil, NewWithOpts(nil) and New() rows -->
<!-- cases: 4 boundaries: 5ms (before) and 20ms (after) the 10ms timeout; the 10s default reached; the exact-limit boundary is specified in health-check-timeouts on a fake clock -->
<!-- cases: 5 errors: the timeout message; matching rules are in health-check-timeouts -->
<!-- cases: N/A 6 not found/duplicate: no lookup involved -->
<!-- cases: N/A 7 lifecycle: scopes and clones are a later requirement -->
<!-- cases: N/A 8 concurrency: covered by the fake-clock requirements and the existing race suite -->
<!-- cases: N/A 9 panics: a nil Clock is a valid default, never a panic -->
<!-- cases: 10 backward compatibility: BREAKING type - InjectorOpts.Clock is doger.Clock instead of clockwork.Clock (migration: Clock: dogerclockwork.Wrap(c)); a consumer that never sets Clock gets the previous wall-clock behavior; New() now has the 10s default health-check bound (health-check-timeouts) -->

### Requirement: The container measures time only on its clock
With `InjectorOpts.Clock` set, the container SHALL read every time it measures from that clock: the `HealthCheckTimeout` and `HealthCheckGlobalTimeout` deadlines, a lazy service's build time (`ServiceBuildTime` in `ExplainService` and `ExplainNamedService` output), and `ShutdownReport.ShutdownTime` and `ShutdownReport.ServiceShutdownTime`.

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
<!-- cases: 7 lifecycle: build (first invocation) and shutdown are the measured phases -->
<!-- cases: 8 concurrency: the wall-time scenario shows the deadline is independent of goroutine timing; the blocked check is released before the test ends, so no goroutine leaks -->
<!-- cases: N/A 9 panics: unchanged -->
<!-- cases: 10 backward compatibility: field names, JSON keys and the omission of a zero build time are unchanged; message prefix "DI: " becomes "doger: " (module-identity) -->

### Requirement: Scopes and clones use the root scope's clock
Child scopes SHALL use their root scope's clock. `Clone()` SHALL keep the original clock. `CloneWithOpts(opts)` SHALL use `opts.Clock`, defaulting to the real clock when nil.

#### Scenario: Derived injectors measure timeouts on the expected clock
```gherkin
Given a root injector whose Clock is dogerclockwork.Wrap(F) for a clockwork fake clock F, and HealthCheckTimeout is 10ms
And a service "svc" whose health check blocks until released, registered in <where>
When the consumer calls HealthCheckWithContext on <target>
And F advances by 10ms
Then the result for "svc" is <expected>

Examples:
  | where             | target                                                              | expected                                                 |
  | a child scope "c" | child scope "c"                                                     | "doger: health check timeout: context deadline exceeded" |
  | the root scope    | child scope "c" (inherited service)                                 | "doger: health check timeout: context deadline exceeded" |
  | the root scope    | Clone() of the root injector                                        | "doger: health check timeout: context deadline exceeded" |
  | the root scope    | CloneWithOpts with Clock dogerclockwork.Wrap(G) for a new fake clock G, timeout 10ms | still running: G has not advanced |
```

<!-- cases: 1 default: CloneWithOpts with Clock nil falls back to the real clock (covered by the first requirement's Clock: nil row) -->
<!-- cases: 2 overrides: CloneWithOpts with its own clock -->
<!-- cases: N/A 3 zero values: covered by the first requirement -->
<!-- cases: N/A 4 boundaries: covered in health-check-timeouts -->
<!-- cases: N/A 5 errors: timeout message only -->
<!-- cases: N/A 6 not found/duplicate: no lookup involved -->
<!-- cases: 7 lifecycle: child scope, inherited service, Clone and CloneWithOpts rows -->
<!-- cases: 8 concurrency: blocked checks are released before each test ends, so no goroutine leaks -->
<!-- cases: N/A 9 panics: unchanged -->
<!-- cases: 10 backward compatibility: Clone and CloneWithOpts keep copying options as before; Clock is copied like the other fields -->

### Requirement: The container measures health reports and scheduling on its clock
With `InjectorOpts.Clock` set, the container SHALL pass that clock to `HealthScheduler.ScheduleHealthRounds`. It SHALL also read from it the `HealthReportCacheTTL`, the report-path check bound, and the `CheckedAt`, `Duration` and `PendingSince` of health report entries.

#### Scenario: Report times come from the clock
```gherkin
Given an injector that is never started, whose Clock is dogerclockwork.Wrap(F) for a clockwork fake clock F set to 2026-01-01T00:00:00Z
And an invoked lazy service "svc" whose health check advances F by 4s and returns nil
When the consumer reads HealthReport() on the root scope
Then the entry for "svc" has Duration 4s and CheckedAt 2026-01-01T00:00:04Z
```

#### Scenario: Health caching waits for the clock, not for wall time
```gherkin
Given an injector that is never started, whose Clock is dogerclockwork.Wrap(F) for a clockwork fake clock F, and HealthReportCacheTTL is 10s
And a Healthchecker "svc" that counts its calls
And the consumer read HealthReport() once
When 50ms of wall time pass without advancing F, and the consumer reads HealthReport() again
Then the health check of "svc" was called 1 time in total
And after F advances by 10s and the consumer reads HealthReport() again, it was called 2 times in total
```

#### Scenario: The scheduler receives the injector clock
```gherkin
Given an injector whose Clock is <clock> and whose HealthScheduler is a manual HealthScheduler
When the consumer calls Start, which succeeds
Then ScheduleHealthRounds received <received>

Examples:
  | clock                                  | received                     |
  | C := dogerclockwork.Wrap(F)            | C                            |
  | nil (default)                          | a Clock equal to RealClock() |
```

<!-- cases: 1 default: the real clock row; the real-clock default is the "The injector clock defaults to the real clock" requirement -->
<!-- cases: 2 overrides: a wrapped fake clock drives the TTL, the report bound, the report times and the scheduler -->
<!-- cases: 3 zero values: a nil Clock; a zero Duration is in health-reports -->
<!-- cases: 4 boundaries: the TTL and bound boundaries are in health-reports -->
<!-- cases: N/A 5 errors: measurement only -->
<!-- cases: N/A 6 not found/duplicate: no lookup involved -->
<!-- cases: 7 lifecycle: never-started and started containers -->
<!-- cases: 8 concurrency: the wall-time scenario shows caching and bounds are independent of goroutine timing -->
<!-- cases: N/A 9 panics: not applicable -->
<!-- cases: 10 backward compatibility: every time the container measured before is unchanged; the scheduler receives a doger.Clock (health-check-scheduling, BREAKING type) -->

### Requirement: Clock is a minimal doger-owned interface
`doger.Clock` SHALL declare exactly `Now() time.Time`, `Since(time.Time) time.Duration` and `AfterFunc(time.Duration, func()) Timer`, and `doger.Timer` exactly `Stop() bool`. `RealClock()` SHALL return a `Clock` backed by the `time` package, and every call SHALL return an equal value. Any consumer type with these methods SHALL be usable as `InjectorOpts.Clock`.

#### Scenario: RealClock follows the time package
```gherkin
Given c := RealClock(), inside synctest.Test
When the consumer calls <call>
Then <observed>

Examples:
  | call                                                         | observed                                                     |
  | c.Now()                                                      | it equals time.Now()                                         |
  | c.Since(t) with t := time.Now(), after time.Sleep(3s)         | it returns 3s                                                |
  | c.AfterFunc(5ms, f), then time.Sleep(5ms) and synctest.Wait() | f has run 1 time                                             |
  | c.AfterFunc(5ms, f).Stop() at once, then time.Sleep(10ms)     | Stop returns true and f has run 0 times                      |
  | c.AfterFunc(0, f), then synctest.Wait()                       | f has run 1 time                                             |
  | RealClock() == RealClock()                                   | true                                                         |
```

#### Scenario: A consumer-written clock drives the container
```gherkin
Given a consumer type manualClock that implements Now, Since and AfterFunc by recording the scheduled functions and running them when its Advance(d) method is called, returning a Timer whose Stop removes the function
And an injector with Clock set to a manualClock m and HealthCheckTimeout 10ms
And a service "svc" whose health check blocks until released, ignoring its context
When the consumer starts HealthCheckNamed(injector, "svc") and calls m.Advance(<advance>)
Then the call <result>
And once the check is released no goroutine is leaked and every Timer the container armed was stopped or fired

Examples:
  | advance | result                                                                  |
  | 9ms     | has not returned (until m advances by another 1ms)                      |
  | 10ms    | returns an error matching ErrHealthCheckTimeout and context.DeadlineExceeded |
```

<!-- cases: 1 default: RealClock() is what a nil Clock means -->
<!-- cases: 2 overrides: a consumer-written Clock; the clockwork adapter is the next requirement -->
<!-- cases: 3 zero values: AfterFunc with a zero duration runs f at once -->
<!-- cases: 4 boundaries: 9ms before and 10ms exactly at the bound on a consumer clock -->
<!-- cases: N/A 5 errors: the interface has no error path -->
<!-- cases: N/A 6 not found/duplicate: not applicable -->
<!-- cases: 7 lifecycle: timers the container arms are stopped or fired by the end of the check -->
<!-- cases: 8 concurrency: no goroutine leak (goleak); RealClock is safe for concurrent use -->
<!-- cases: N/A 9 panics: no input panics -->
<!-- cases: 10 backward compatibility: BREAKING - replaces clockwork.Clock in the public API; the root module no longer requires github.com/jonboulle/clockwork for consumers (next-but-one requirement) -->

### Requirement: The clockwork adapter module wraps and unwraps clockwork clocks
Module `github.com/kartaladev/doger/clock/clockwork` (package `dogerclockwork`) SHALL provide `Wrap(clockwork.Clock) doger.Clock`, which delegates every method to the clockwork clock, and `Unwrap(doger.Clock) (clockwork.Clock, bool)`, which returns the wrapped clock and true for a value made by `Wrap`. `Wrap(nil)` SHALL return a nil `doger.Clock`.

#### Scenario: Wrap and Unwrap round trip
```gherkin
Given a clockwork fake clock F
When the consumer calls dogerclockwork.Unwrap(<input>)
Then it returns <clock> and <ok>

Examples:
  | input                           | clock | ok    |
  | dogerclockwork.Wrap(F)          | F     | true  |
  | doger.RealClock()               | nil   | false |
  | a consumer-written doger.Clock  | nil   | false |
  | nil                             | nil   | false |
```

#### Scenario: A wrapped fake clock drives timers and readings
```gherkin
Given a clockwork fake clock F set to 2026-01-01T00:00:00Z and c := dogerclockwork.Wrap(F)
When the consumer calls <call>
Then <observed>

Examples:
  | call                                                     | observed                                         |
  | c.Now()                                                  | it returns 2026-01-01T00:00:00Z                  |
  | F.Advance(2s), then c.Since(2026-01-01T00:00:00Z)         | it returns 2s                                    |
  | c.AfterFunc(5ms, f), then F.Advance(4ms)                  | f has run 0 times                                |
  | c.AfterFunc(5ms, f), then F.Advance(5ms)                  | f has run 1 time                                 |
  | c.AfterFunc(5ms, f).Stop(), then F.Advance(5ms)           | Stop returns true and f has run 0 times          |
```

#### Scenario: Wrap of nil means the default clock
```gherkin
Given an injector created with NewWithOpts(&InjectorOpts{Clock: dogerclockwork.Wrap(nil), HealthCheckTimeout: 10ms}), inside synctest.Test
And a service "svc" whose health check returns nil after time.Sleep(20ms)
When the consumer calls HealthCheckNamed(injector, "svc")
Then the result is "doger: health check timeout: context deadline exceeded"
```

<!-- cases: 1 default: Wrap(nil) yields a nil Clock, so the injector uses RealClock() -->
<!-- cases: 2 overrides: a wrapped fake clock -->
<!-- cases: 3 zero values: Wrap(nil), Unwrap(nil) -->
<!-- cases: 4 boundaries: 4ms before and 5ms exactly at a timer -->
<!-- cases: N/A 5 errors: no error path; Unwrap reports failure with ok false -->
<!-- cases: 6 not found/duplicate: Unwrap of a clock not made by Wrap reports false -->
<!-- cases: N/A 7 lifecycle: pure adapter -->
<!-- cases: N/A 8 concurrency: delegates to clockwork, which is safe for concurrent use -->
<!-- cases: N/A 9 panics: Wrap(nil) does not panic -->
<!-- cases: 10 backward compatibility: migration path for consumers that set Clock to a clockwork clock: Clock: dogerclockwork.Wrap(c) -->

### Requirement: The root module does not require clockwork
A consumer module that requires only `github.com/kartaladev/doger` at a released version SHALL NOT have `github.com/jonboulle/clockwork` in its build list. Only the `clock/clockwork` and `scheduler/gocron` modules SHALL require it.

#### Scenario: Build list of a consumer
```gherkin
Given a fresh consumer module that imports only <import> at the released version, resolved without go.work
When the consumer runs go list -m all
Then github.com/jonboulle/clockwork is <listed>

Examples:
  | import                                          | listed     |
  | github.com/kartaladev/doger                      | not listed |
  | github.com/kartaladev/doger/clock/clockwork      | listed     |
  | github.com/kartaladev/doger/scheduler/gocron     | listed     |
```

<!-- cases: 1 default: the root module alone -->
<!-- cases: 2 overrides: the opt-in adapter modules -->
<!-- cases: N/A 3 zero values: not applicable -->
<!-- cases: N/A 4 boundaries: not applicable -->
<!-- cases: N/A 5 errors: not applicable -->
<!-- cases: N/A 6 not found/duplicate: not applicable -->
<!-- cases: 7 lifecycle: checked on the release dry run (release-integrity) -->
<!-- cases: N/A 8 concurrency: not applicable -->
<!-- cases: N/A 9 panics: not applicable -->
<!-- cases: 10 backward compatibility: consumers that used clockwork through doger must require the adapter module -->
