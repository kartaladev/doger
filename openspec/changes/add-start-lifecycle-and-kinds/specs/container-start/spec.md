# Spec Delta

## Purpose

Lets a consumer start a fully wired container in one call, so background components are built and started in dependency order and then stopped by the existing shutdown.

## ADDED Requirements

### Requirement: Starter variants are detected by registered type
`Start` SHALL start every non-transient, non-alias service whose registered type implements one of `Starter`, `StarterWithError`, `StarterWithContext` or `StarterWithContextAndError`, plus every service registered with `AsStarter()`. It SHALL call the variant's `Start` method exactly once.

#### Scenario: Each Starter variant is started once
```gherkin
Given a root scope with lazy "<service>" of a type whose method is <method>
When the consumer calls StartWithContext(ctx)
Then the start method was called 1 time
And StartWithContext returns nil

Examples:
  | service | method                          |
  | s1      | Start()                         |
  | s2      | Start() error                   |
  | s3      | Start(context.Context)          |
  | s4      | Start(context.Context) error    |
```

#### Scenario: Which registrations are started
```gherkin
Given a root scope with "<service>" registered as <registration>
When the consumer calls Start()
Then the instance's Start was called <calls> times

Examples:
  | service       | registration                                                         | calls |
  | *app.Server   | lazy, type has Start(ctx) error                                      | 1     |
  | *app.Server   | eager value, type has Start() error                                  | 1     |
  | app.Lifecycle | lazy registered as interface app.Lifecycle (no Start in interface), instance has Start() | 0 |
  | app.Lifecycle | same as above, plus AsStarter()                                      | 1     |
  | *app.Server   | lazy, plus As[*app.Server, app.Startable] alias                      | 1     |
  | *app.Config   | lazy, type has no Start                                              | 0     |
  | *app.Server   | transient, type has Start() error                                    | 0     |
```

<!-- cases: 2 overrides: DisableStarterAutoDetection is covered by "Starter auto-detection can be disabled" -->
<!-- cases: N/A 3 zero values: a container with no services is covered by "Start on an empty container" -->
<!-- cases: N/A 6 duplicates: an alias row pins that a service is started once even when also reachable through an alias -->
<!-- cases: N/A 9 panics: covered by "Start errors and panics" -->

### Requirement: Starter auto-detection can be disabled
When `InjectorOpts.DisableStarterAutoDetection` is true, `Start` SHALL start only services registered with `AsStarter()`.

#### Scenario: Auto-detection toggle
```gherkin
Given a root scope created with DisableStarterAutoDetection = <disabled>
And lazy "a" whose type has Start() error, registered without options
And lazy "b" whose type has Start() error, registered with AsStarter()
When the consumer calls Start()
Then the started services are <started>

Examples:
  | disabled | started |
  | false    | a, b    |
  | true     | b       |
```

<!-- cases: N/A 3 zero values: the zero value of the field is false, which is the default row -->
<!-- cases: N/A 4 boundaries, 5 errors, 9 panics: a boolean toggle -->
<!-- cases: N/A 7 lifecycle, 8 concurrency: options are fixed at container creation -->
<!-- cases: N/A 10 compatibility: new option -->

### Requirement: Start builds candidates and starts them in dependency order
`Start` SHALL build every candidate (Starters and services opted into any kind) before starting any Starter. A service's dependencies, invoked or declared, are built before it. It SHALL then start Starters one at a time in dependency order: a Starter starts only after every Starter it depends on, directly or transitively, has started. Ties are broken by scope depth, then registration index.

#### Scenario: Dependency-ordered build and start
```gherkin
Given a root scope recording provider calls and start calls
And lazy Starter "http" whose provider invokes "db"
And lazy Starter "db"
And lazy non-starter "repo" whose provider invokes "db"
And lazy Starter "consumer" whose provider invokes "repo"
When the consumer calls Start()
Then every provider call happens before the first start call
And the start order is db, http, consumer
And "repo" is built but not started
```

#### Scenario: Child scopes are started after their ancestors
```gherkin
Given a root scope with lazy Starter "db"
And a child scope "tenant-a" with lazy Starter "worker" invoking "db"
And a grandchild scope "tenant-a/jobs" with lazy Starter "cron"
When the consumer calls Start() on the root scope
Then the start order is db, worker, cron
```

#### Scenario: Start on an empty container
```gherkin
Given a root scope with no registrations
When the consumer calls Start()
Then Start returns nil
```

<!-- cases: N/A 2 overrides: ordering is overridden by declaring edges (dependency-declaration) -->
<!-- cases: N/A 4 boundaries: 0, 1 and many Starters are covered across scenarios -->
<!-- cases: 6 duplicates: a service reachable from several Starters is built once (lazy singleton semantics) -->
<!-- cases: 8 concurrency: Starters start sequentially; concurrent Invoke during Start is covered by "Concurrent Start and Invoke" -->
<!-- cases: N/A 9 panics: covered by "Start errors and panics" -->
<!-- cases: N/A 10 compatibility: new API -->

### Requirement: Start errors and panics
On the first failure, `Start` SHALL stop and return an error that names the service and scope and wraps the cause (matchable with `errors.Is`/`errors.As`). Failures are a build error, a `Start` error, a panic in `Start` (recovered), or a kind-handler error. Starters not yet reached SHALL NOT be started, and no rollback happens.

#### Scenario: First failure stops the pipeline
```gherkin
Given a root scope with Starters "a", "b", "c" registered in that order, with no dependencies
And "b" fails with <failure>
When the consumer calls Start()
Then the returned error message contains "service `b`"
And errors.Is(err, <cause>) is <is>
And the started services are a
And "c" was built but not started

Examples:
  | failure                                  | cause                 | is    |
  | Start() returning errBoom                | errBoom               | true  |
  | Start(ctx) panicking with "kaboom"       | n/a (message "kaboom")| false |
  | provider returning errBuild              | errBuild              | true  |
  | DependsOnNamed("missing")                | ErrServiceNotFound    | true  |
```

#### Scenario: AsStarter on an interface whose instance cannot start
```gherkin
Given a root scope with lazy "app.Lifecycle" registered with AsStarter()
And the provider returns an instance with no Start method
When the consumer calls Start()
Then the error message contains "DI: service `app.Lifecycle` was opted in with AsStarter but does not implement any Starter interface"
```

#### Scenario: Shutdown after a failed Start reaches every built service
```gherkin
Given a root scope where Starter "a" starts, Starter "b" fails to start, and Starter "c" is built but never started
And all three implement Shutdown() error
When the consumer calls Shutdown() after the failed Start
Then Shutdown() was called on a, b and c
```

<!-- cases: N/A 2 overrides: no rollback by design; the caller decides (Shutdown, retry, exit) -->
<!-- cases: N/A 3 zero values: a nil error from Start() error is the success case -->
<!-- cases: N/A 8 concurrency: the pipeline is sequential -->
<!-- cases: 10 compatibility: Shutdown behavior for services that were never started is unchanged -->

### Requirement: Start honours its context
`StartWithContext` SHALL check the context before building candidates and before each Starter. A cancelled or expired context SHALL stop the pipeline with an error wrapping `ctx.Err()`. Context-aware `Start(ctx)` variants SHALL receive the same context. `Start()` SHALL use `context.Background()`. A nil context SHALL be treated as `context.Background()`.

#### Scenario: Cancellation and deadline
```gherkin
Given a root scope with Starters "a" then "b"
And "a" is a Start(ctx) error that <a-behavior>
When the consumer calls StartWithContext(ctx) where ctx <ctx>
Then errors.Is(err, <cause>) is true
And "b" was not started

Examples:
  | ctx                                   | a-behavior                                  | cause                    |
  | is already cancelled                  | returns nil                                 | context.Canceled         |
  | has a 5s deadline                     | blocks until ctx.Done() and returns ctx.Err() | context.DeadlineExceeded |
  | is cancelled while "a" runs           | returns nil after the cancellation          | context.Canceled         |
```

#### Scenario: Nil context
```gherkin
Given a root scope with one Starter "a"
When the consumer calls StartWithContext(nil)
Then Start returns nil
And "a" received a non-nil context
```

<!-- cases: N/A 2 overrides: there is no Start timeout option; callers use context deadlines -->
<!-- cases: 8 concurrency: deadline scenarios run under testing/synctest so time is virtual and no goroutine leaks -->
<!-- cases: N/A 9 panics: covered by "Start errors and panics" -->
<!-- cases: N/A 10 compatibility: new API -->

### Requirement: Start is one-shot and serialized with Shutdown
A root container SHALL start at most once. Any call after the first, whether it succeeded or failed, SHALL return `ErrAlreadyStarted` without doing anything. A call after `Shutdown` began SHALL return `ErrStartAfterShutdown`. A `Shutdown` that begins while `Start` is running SHALL wait for `Start` to return.

#### Scenario: Repeated and late Start calls
```gherkin
Given a root scope with one Starter "a"
And the consumer has already <previous>
When the consumer calls Start()
Then the result is <result>
And "a" was started <calls> times in total

Examples:
  | previous                        | result                 | calls |
  | called nothing                  | nil                    | 1     |
  | called Start() successfully     | ErrAlreadyStarted      | 1     |
  | called Start() which failed     | ErrAlreadyStarted      | 0     |
  | called Shutdown()               | ErrStartAfterShutdown  | 0     |
```

#### Scenario: Concurrent Start calls
```gherkin
Given a root scope with one Starter "a"
When 10 goroutines call Start() concurrently
Then exactly 1 call returns nil
And 9 calls return ErrAlreadyStarted
And "a" was started 1 time
```

#### Scenario: Concurrent Start and Invoke
```gherkin
Given a root scope with lazy Starter "a" whose provider counts its calls
When Start() runs while 50 goroutines invoke "a"
Then the "a" provider was called exactly 1 time
And no data race is reported
```

#### Scenario: Shutdown waits for a running Start
```gherkin
Given a root scope with Starter "a" whose Start(ctx) blocks until released
When the consumer calls Start() in one goroutine and Shutdown() in another after "a" began starting
And "a" is released
Then Shutdown returns only after Start has returned
And "a" was shut down once
```

<!-- cases: N/A 2 overrides: one-shot is fixed; re-running is the deferred Refresh operation -->
<!-- cases: N/A 3 zero values: covered by the "called nothing" row -->
<!-- cases: 7 lifecycle: covered by every row in this requirement -->
<!-- cases: N/A 9 panics: covered by "Start errors and panics" -->
<!-- cases: N/A 10 compatibility: new API -->

### Requirement: Registrations after Start are not started
Services registered after `Start` returned, and child scopes created after `Start` began, SHALL NOT be started or handled automatically in this change. They remain invokable as usual.

#### Scenario: Late registration
```gherkin
Given a root scope that has been started
When the consumer registers lazy Starter "late" and invokes it
Then "late" is built
And "late" was started 0 times
```

<!-- cases: N/A 2 overrides: scope-level Start is phase 2 -->
<!-- cases: N/A 3, 4, 5, 9: no inputs or failure modes beyond the scenario -->
<!-- cases: N/A 8 concurrency: covered by "Concurrent Start and Invoke" -->
<!-- cases: N/A 10 compatibility: new API -->

### Requirement: Per-service start hooks
For each Starter, `Start` SHALL call every `HookBeforeStart(scope, name)` just before calling its `Start` method, and every `HookAfterStart(scope, name, err)` just after, with that method's error (nil on success, the recovered error on panic). Hooks SHALL run in registration order. `AddBeforeStartHook`/`AddAfterStartHook` SHALL append to the same lists.

#### Scenario: Hooks around each Starter
```gherkin
Given a root scope with HookBeforeStart and HookAfterStart recording calls
And a hook added later with AddAfterStartHook recording "late-after"
And Starters "a" (succeeds) then "b" (returns errBoom)
When the consumer calls Start()
Then the recorded calls are: before a, after a nil, late-after a, before b, after b errBoom, late-after b
```

<!-- cases: N/A 3 zero values: no hooks registered means no calls (default; covered by every other scenario) -->
<!-- cases: N/A 4 boundaries: 0, 1 and many hooks follow the same slice iteration as the existing shutdown hooks -->
<!-- cases: 9 panics: a panicking hook propagates exactly as a panicking shutdown hook does today (no new recovery) -->
<!-- cases: N/A 8 concurrency: hooks run on the Start goroutine -->
<!-- cases: N/A 10 compatibility: new hooks -->
