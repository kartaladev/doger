# Spec Delta

## Purpose

Lets consumers run container-wide logic once when the root container starts, becomes ready, fails to start, begins closing, and finishes closing.

## ADDED Requirements

### Requirement: Starting hooks run before discovery and can abort Start
Each registered `HookContainerStarting` hook SHALL run before any service is discovered or instantiated by `Start`. If one returns a non-nil error, the rest SHALL NOT run, no service SHALL be instantiated or started, and `Start` SHALL return an error that wraps the hook's error.

#### Scenario: Starting hook allows or vetoes Start
```gherkin
Given a root container with a lazy Starter service "server" whose provider records that it was built
And HookContainerStarting hooks returning <results> in registration order
When the consumer calls Start
Then Start returns <start_error>
And the provider of "server" was called <builds> times
And the number of Starting hooks that ran is <ran>

Examples:
  | results                        | start_error                          | builds | ran |
  | nil                            | nil                                  | 1      | 1   |
  | nil, nil                       | nil                                  | 1      | 2   |
  | errors.New("missing DB_URL")   | error wrapping "missing DB_URL"      | 0      | 1   |
  | nil, errors.New("no license"), nil | error wrapping "no license"      | 0      | 2   |
```

#### Scenario: Starting hook receives the Start context
```gherkin
Given a root container with a HookContainerStarting hook that records its context and root
And a context carrying the value "trace-id" = "abc"
When the consumer calls StartWithContext with that context
Then the hook received a context whose "trace-id" value is "abc"
And the hook received the same root container
```

#### Scenario: Starting hook can stop on a cancelled context
```gherkin
Given a root container with a HookContainerStarting hook that returns ctx.Err()
And a context that is already cancelled
When the consumer calls StartWithContext with that context
Then Start returns an error for which errors.Is(err, context.Canceled) is true
And no service was instantiated
```

<!-- cases: N/A 6 duplicates: a hook registered twice simply runs twice; covered by the ordering requirement -->

### Requirement: Started and Ready hooks mark a successful start
After `StageBeforeStart` handlers have run and every Starter has started, `Start` SHALL run each `HookContainerStarted` hook. After `StageAfterStart` handlers (including the built-in `Runnable` handler) complete, it SHALL run each `HookContainerReady` hook, then return nil.

#### Scenario: Hook order relative to the start pipeline
```gherkin
Given a root container whose recorder logs every lifecycle step
And a Starter service "server" and a Runnable service "migrate"
And one hook registered for each of Starting, Started and Ready
When the consumer calls Start
Then Start returns nil
And the recorded order is "starting-hook, server.Start, started-hook, migrate.Run, ready-hook"
```

#### Scenario: Started and Ready fire for an empty container
```gherkin
Given a root container with no services
And one hook registered for each of Starting, Started, Ready and Failed
When the consumer calls Start
Then Start returns nil
And the recorded order is "starting-hook, started-hook, ready-hook"
And the Failed hook did not run
```

<!-- cases: N/A 5 errors: these hooks have no error return; failures are covered by the Failed requirement -->
<!-- cases: N/A 6 duplicates: covered by the ordering requirement -->

### Requirement: Failed hooks observe a failed start
When `Start` fails after its one-shot guard has passed, it SHALL run each `HookContainerFailed` hook with the same error it is about to return, and then return that error. Failures include a Starting veto, a provider, Starter, handler or Runner error, and context cancellation. `Ready` SHALL NOT run, and `Started` SHALL run only if the failure happened after it.

#### Scenario: Failure point decides which hooks run
```gherkin
Given a root container with one hook registered for each of Starting, Started, Ready and Failed
And the start pipeline fails at "<failure_point>" with error "boom"
When the consumer calls Start
Then Start returns an error for which errors.Is(err, boom) is true
And the Failed hook received an error for which errors.Is(err, boom) is true
And the hooks that ran, in order, are "<hooks_ran>"

Examples:
  | failure_point                      | hooks_ran                         |
  | Starting hook veto                 | starting, failed                  |
  | provider of a Starter service      | starting, failed                  |
  | StageBeforeStart kind handler      | starting, failed                  |
  | Start method of a Starter          | starting, failed                  |
  | Run method of a Runnable           | starting, started, failed         |
  | StageAfterStart kind handler       | starting, started, failed         |
```

#### Scenario: One-shot guard errors do not fire Failed
```gherkin
Given a root container with one hook registered for each of Starting and Failed
And the container is in state "<state>"
When the consumer calls Start
Then Start returns <error>
And neither the Starting nor the Failed hook ran during this call

Examples:
  | state                         | error                 |
  | started                       | ErrAlreadyStarted     |
  | failed (after a Starting veto) | ErrAlreadyStarted    |
  | shut down                     | ErrStartAfterShutdown |
```

<!-- cases: N/A 3 zero values: an error is always non-nil when Failed runs -->
<!-- cases: N/A 6 duplicates: covered by the ordering requirement -->

### Requirement: Closing and Closed hooks bracket the root shutdown
The first call to `RootScope.Shutdown` or `ShutdownWithContext` (including through `ShutdownOnSignals*`) SHALL run each `HookContainerClosing` hook before any service is shut down. After all services are shut down, it SHALL run each `HookContainerClosed` hook with the same `*ShutdownReport` it returns. This happens whether or not `Start` was called or succeeded. The hooks cannot change the report.

#### Scenario: Closing and Closed around service shutdown from every state
```gherkin
Given a root container in state "<state>" with a built Shutdowner service "db"
And one hook registered for each of Closing and Closed
When the consumer calls Shutdown
Then the recorded order is "closing-hook, db.Shutdown, closed-hook"
And the Closed hook received the same report pointer that Shutdown returned

Examples:
  | state                       |
  | configured (never started)  |
  | ready                       |
  | failed                      |
```

#### Scenario: Closed hook sees shutdown errors in the report
```gherkin
Given a root container with a built Shutdowner service "db" whose Shutdown returns errors.New("flush failed")
And one Closed hook that records the report
When the consumer calls Shutdown
Then the recorded report has Succeed false
And the recorded report has one error for service "db"
```

#### Scenario: Signal-driven shutdown fires the hooks
```gherkin
Given a root container with one hook registered for each of Closing and Closed
And a context that is cancelled before any signal arrives
When the consumer calls ShutdownOnSignalsWithContext with that context
Then the Closing hook ran once
And the Closed hook ran once
```

<!-- cases: N/A 5 errors: Closing and Closed have no error return and cannot veto -->

### Requirement: Container hooks run synchronously in registration order
Each container hook list SHALL run on the goroutine that called `Start` or `Shutdown`: first the hooks from `InjectorOpts`, in slice order, then those added with `RootScope.AddContainer<State>Hook`, in call order. A nil hook SHALL be skipped. `RootScope.Clone` and `CloneWithOpts` SHALL copy registered container hooks into the clone, but not the container state.

#### Scenario: Registration order
```gherkin
Given InjectorOpts.HookContainerStarted contains hooks "opt-1" and "opt-2"
And RootScope.AddContainerStartedHook was called with "add-1" then "add-2"
When the consumer calls Start
Then the Started hooks ran in the order "opt-1, opt-2, add-1, add-2"
And every hook ran before Start returned
```

#### Scenario: Nil hooks are ignored
```gherkin
Given InjectorOpts.<field> contains nil followed by a recording hook
And RootScope.<adder> was called with nil
When the consumer runs <operation>
Then <operation> completes without panicking
And the recording hook ran once

Examples:
  | field                   | adder                       | operation |
  | HookContainerStarting   | AddContainerStartingHook    | Start     |
  | HookContainerStarted    | AddContainerStartedHook     | Start     |
  | HookContainerReady      | AddContainerReadyHook       | Start     |
  | HookContainerClosing    | AddContainerClosingHook     | Shutdown  |
  | HookContainerClosed     | AddContainerClosedHook      | Shutdown  |
```

#### Scenario: Clone copies hooks but not state
```gherkin
Given a root container with a Started hook that counts calls
And the container has been started
When the consumer clones the container and calls Start on the clone
Then Start on the clone returns nil
And the Started hook count is 2
```

<!-- cases: N/A 5 errors: ordering has no error path; errors are covered by the Starting and Failed requirements -->
<!-- cases: N/A 9 panics: covered by the "Hook panics are recovered" requirement -->

### Requirement: Hook panics are recovered
A panic in a container hook SHALL be recovered. In a `HookContainerStarting` hook it SHALL be treated as a veto: `Start` returns an error containing the panic value, Failed hooks run, and no service is instantiated. In any other container hook it SHALL be logged through `InjectorOpts.Logf`, the remaining hooks of that list SHALL still run, and the result of `Start` or `Shutdown` SHALL be unchanged.

#### Scenario: Panicking hook by lifecycle point
```gherkin
Given a root container with a Starter service "server"
And a <hook> hook that panics with "kaboom", followed by a recording hook in the same list
When the consumer calls <operation>
Then <operation> returns <result>
And the following recording hook <followed>
And Logf received a message containing "kaboom" <logged>

Examples:
  | hook     | operation | result                                   | followed | logged |
  | Starting | Start     | an error containing "kaboom"             | did not run | no  |
  | Started  | Start     | nil                                      | ran      | yes    |
  | Ready    | Start     | nil                                      | ran      | yes    |
  | Failed   | Start     | the original start error (not "kaboom")  | ran      | yes    |
  | Closing  | Shutdown  | a report with Succeed true               | ran      | yes    |
  | Closed   | Shutdown  | a report with Succeed true               | ran      | yes    |
```

<!-- cases: N/A 3 zero values: panic(nil) is reported by Go 1.21+ as *runtime.PanicNilError and handled the same as any panic value -->

### Requirement: Container hooks fire at most once per container
Each container transition SHALL happen at most once for a root container. Concurrent or repeated `Start` calls SHALL run the Starting, Started, Ready and Failed hooks at most once in total. Concurrent or repeated root `Shutdown` calls SHALL run Closing and Closed exactly once in total. Later calls fire no container hooks.

#### Scenario: Concurrent Start calls
```gherkin
Given a root container with a Starter service "server" and one counting hook for each of Starting, Started and Ready
When 50 goroutines call Start concurrently
Then exactly 1 call returns nil
And 49 calls return ErrAlreadyStarted
And each counting hook ran exactly 1 time
And the test passes under -race with no leaked goroutines
```

#### Scenario: Repeated and concurrent Shutdown calls
```gherkin
Given a started root container with one counting hook for each of Closing and Closed
When <callers> goroutines call Shutdown <times> times each
Then the Closing hook ran exactly 1 time
And the Closed hook ran exactly 1 time

Examples:
  | callers | times |
  | 1       | 2     |
  | 20      | 1     |
```

#### Scenario: Hook calling Start re-entrantly
```gherkin
Given a root container whose Started hook calls Start on the same root and records the result
When the consumer calls Start
Then the outer Start returns nil
And the recorded inner result is ErrAlreadyStarted
And the Started hook ran exactly 1 time
```

<!-- cases: N/A 2 overrides: the at-most-once rule has no option -->

### Requirement: Only the root container fires container hooks
Container hooks SHALL fire only for the root container's `Start` and `Shutdown`. Shutting down a child scope, including through a `Scope` or a provider's injector, SHALL NOT fire any container hook. With no container hooks registered, `Start` and `Shutdown` SHALL behave exactly as they do without this change.

#### Scenario: Child scope shutdown fires nothing
```gherkin
Given a started root container with one counting hook for each of Closing and Closed
And a child scope "request" with a built Shutdowner service "session"
When the consumer calls Shutdown on scope "request"
Then "session" was shut down
And the Closing hook ran 0 times
And the Closed hook ran 0 times
```

#### Scenario: No hooks registered leaves behavior unchanged
```gherkin
Given a root container created with <constructor> and no container hooks
And a Starter service "server" and a Shutdowner service "db"
When the consumer calls Start and then Shutdown
Then Start returns nil
And the shutdown report has Succeed true
And no log line mentions a container hook

Examples:
  | constructor                     |
  | New()                           |
  | NewWithOpts(&InjectorOpts{})    |
  | NewWithOpts(nil)                |
```

<!-- cases: N/A 4 boundaries: there is exactly one root per container -->
<!-- cases: N/A 9 panics: covered by the "Hook panics are recovered" requirement -->
