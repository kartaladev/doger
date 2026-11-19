# Spec Delta

## MODIFIED Requirements

### Requirement: New returns a scheduler with safe defaults
`dogergocron.New(opts...)` SHALL return a `*Scheduler` implementing `doger.HealthScheduler`, whose `ScheduleHealthRounds` takes a `doger.Clock`. With no option, each `ScheduleHealthRounds` call SHALL:
- create and start its own gocron scheduler on the gocron clock matching the given clock;
- run the first round at once, then a round every `DefaultInterval` (30s);
- never run two rounds of one schedule at once: a tick that finds a round still running SHALL be skipped.

#### Scenario: Default cadence on the container clock
```gherkin
Given an injector whose Clock is dogerclockwork.Wrap(F) for a clockwork fake clock F, HealthScheduler is dogergocron.New(), and a Healthchecker "db" that counts its calls
When the consumer calls Start, which succeeds, and F advances by <advance>, waiting for each due round
Then the health check of "db" was called <calls> times

Examples:
  | advance | calls |
  | 0s      | 1     |
  | 29.999s | 1     |
  | 30s     | 2     |
  | 60s     | 3     |
```

#### Scenario: A slow round skips the next tick
```gherkin
Given an injector whose Clock is dogerclockwork.Wrap(F) for a clockwork fake clock F, HealthCheckTimeout is -1ns, HealthScheduler is dogergocron.New()
And a Healthchecker "db" whose first check blocks until released and counts its calls
When the consumer calls Start, F advances by 30s, the check is released, and F advances by 30s more
Then the health check of "db" was called 2 times, never two at once
```

<!-- cases: 1 default: this requirement -->
<!-- cases: 2 overrides: next requirement -->
<!-- cases: N/A 3 zero values: no option is the default -->
<!-- cases: 4 boundaries: one millisecond before and at the interval -->
<!-- cases: N/A 5 errors: a later requirement -->
<!-- cases: N/A 6 not found/duplicate: not applicable -->
<!-- cases: 7 lifecycle: the first round at Ready -->
<!-- cases: 8 concurrency: singleton rounds -->
<!-- cases: N/A 9 panics: checks recover their panics in the root module; gocron also recovers job panics -->
<!-- cases: 10 backward compatibility: BREAKING signature - ScheduleHealthRounds takes a doger.Clock instead of a clockwork.Clock (health-check-scheduling); containers using the adapter need no change -->

## ADDED Requirements

### Requirement: The adapter maps the container clock to a gocron clock
When it creates its own gocron scheduler, `ScheduleHealthRounds` SHALL pass gocron the clockwork clock `dogerclockwork.Unwrap` returns for the given clock, `clockwork.NewRealClock()` for a clock equal to `doger.RealClock()`, and SHALL otherwise return an error matching `dogergocron.ErrUnsupportedClock` and a nil stop, starting no goroutine. With `WithScheduler`, every clock SHALL be accepted and ignored.

#### Scenario: Clock mapping
```gherkin
Given dogergocron.New(<options>)
When ScheduleHealthRounds is called with <clock> and a round that counts its calls
Then it returns <result>
And <observed>

Examples:
  | options                 | clock                                     | result                                                  | observed                                                                    |
  | none                    | dogerclockwork.Wrap(F), F a fake clock    | a non-nil stop and a nil error                          | the round ran 1 time, and advancing F by 30s runs it a 2nd time            |
  | none                    | doger.RealClock()                         | a non-nil stop and a nil error                          | the round ran 1 time at once                                                |
  | none                    | a consumer-written doger.Clock            | a nil stop and an error matching ErrUnsupportedClock    | the round never ran and goleak reports no goroutine started by gocron      |
  | none                    | nil                                       | a nil stop and an error matching ErrUnsupportedClock    | the round never ran and goleak reports no goroutine started by gocron      |
  | WithScheduler(S), S started by the application on fake clock G | a consumer-written doger.Clock | a non-nil stop and a nil error | the round runs on G's schedule                                     |
```

#### Scenario: An unsupported clock leaves the container unmonitored
```gherkin
Given an injector whose Clock is a consumer-written doger.Clock, HealthScheduler is dogergocron.New(), and a Logger whose handler records every record
When the consumer calls Start
Then Start returns nil and Phase() is PhaseReady
And HealthReport(IgnoreContainerPhase()) on the root scope has Monitored false
And the handler recorded an Error record whose message contains "health scheduler" and whose error matches dogergocron.ErrUnsupportedClock
```

<!-- cases: 1 default: the real clock (nil InjectorOpts.Clock) maps to clockwork's real clock -->
<!-- cases: 2 overrides: a wrapped fake clock; a shared scheduler ignoring the clock; a WithSchedulerOptions(gocron.WithClock(D)) still wins after the mapped clock, as in "Every default can be overridden" -->
<!-- cases: 3 zero values: a nil clock (never passed by the container, which defaults to RealClock()) is rejected -->
<!-- cases: N/A 4 boundaries: not applicable -->
<!-- cases: 5 errors: ErrUnsupportedClock, matchable with errors.Is; the container logs it and stays unmonitored (health-check-scheduling) -->
<!-- cases: N/A 6 not found/duplicate: not applicable -->
<!-- cases: 7 lifecycle: a rejected clock at Ready leaves the container unmonitored; Start still succeeds -->
<!-- cases: 8 concurrency: goleak after a rejected clock -->
<!-- cases: N/A 9 panics: no input panics -->
<!-- cases: 10 backward compatibility: consumers that set a clockwork fake clock wrap it with dogerclockwork.Wrap; the real-clock default is unchanged -->
