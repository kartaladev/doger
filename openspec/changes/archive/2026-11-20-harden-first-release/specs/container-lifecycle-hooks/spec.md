# Spec Delta

## MODIFIED Requirements

### Requirement: Failed hooks observe a failed start
When `Start` fails after its one-shot guard, it SHALL release the running Start, run each `HookContainerFailed` hook with the exact error it is about to return, and then return that error. Failures include a Starting veto, a cancelled context, a provider, Starter, kind-handler or Runner error, and a `Shutdown` that began before the pipeline finished. Started and Ready SHALL NOT run. Guard errors SHALL NOT run any container hook.

#### Scenario: Failure point decides which hooks run
```gherkin
Given a root container with one hook registered for each of Starting, Started, Ready and Failed
And the start pipeline fails at "<failure_point>" with cause <cause>
When the consumer calls Start
Then Start returns an error for which errors.Is(err, <cause>) is true
And the Failed hook received the same error value that Start returned
And the hooks that ran, in order, are "<hooks_ran>"

Examples:
  | failure_point                       | cause            | hooks_ran        |
  | Starting hook veto                  | boom             | starting, failed |
  | context already cancelled           | context.Canceled | starting, failed |
  | provider of a Starter service       | boom             | starting, failed |
  | StageBeforeStart kind handler       | boom             | starting, failed |
  | Start method of a Starter           | boom             | starting, failed |
  | Run method of a Runnable            | boom             | starting, failed |
  | StageAfterStart kind handler        | boom             | starting, failed |
```

#### Scenario: One-shot guard errors do not fire Failed
```gherkin
Given a root container with one hook registered for each of Starting and Failed
And the container is in state "<state>"
When the consumer calls Start
Then Start returns an error for which errors.Is(err, <error>) is true
And neither the Starting nor the Failed hook ran during this call

Examples:
  | state                          | error                 |
  | started                        | ErrAlreadyStarted     |
  | failed (after a Starting veto) | ErrAlreadyStarted     |
  | shut down                      | ErrShutdown |
```

<!-- cases: N/A 2 overrides: firing points are fixed -->
<!-- cases: N/A 3 zero values: an error is always non-nil when Failed runs -->
<!-- cases: N/A 6 duplicates: covered by the ordering requirement -->
<!-- cases: N/A 7 lifecycle: Shutdown from a Failed hook is covered by "Post-start and Closed hooks may shut the container down" -->
<!-- cases: N/A 8 concurrency: covered by "Shutdown during an in-flight Start" -->
<!-- cases: N/A 9 panics: a pipeline panic has its own requirement; Failed hook panics are covered by "Observer hook panics are recovered and re-raised" -->
<!-- cases: N/A 10 compatibility: new API -->

### Requirement: Starting and Closing hooks must not call Shutdown synchronously
A `HookContainerStarting` or `HookContainerClosing` hook MUST NOT call root `Shutdown` without a deadline: that call waits for the running Start or Shutdown (start-lifecycle D6). `ShutdownWithContext` called from such a hook with a deadline SHALL return when the deadline expires with a failed report. A Start whose Starting hook did so SHALL fail with an error wrapping `ErrShutdown`.

#### Scenario: Starting or Closing hook calls Shutdown with a deadline
```gherkin
Given a root container with counting Closing, Closed and Failed hooks
And a <hook> hook that calls root.ShutdownWithContext with a 1s deadline, records the report and returns <returns>
When the consumer calls <operation> inside synctest.Test
Then the recorded report has Succeed false and every error in it matches context.DeadlineExceeded
And <outcome>
And the counting Closing hook ran exactly 1 time
And the counting Closed hook ran exactly 1 time

Examples:
  | hook     | returns | operation | outcome                                                                                              |
  | Starting | nil     | Start     | Start returns an error matching ErrShutdown and the Failed hook ran 1 time                 |
  | Starting | errVeto | Start     | Start returns an error matching both ErrShutdown and errVeto and the Failed hook ran 1 time |
  | Closing  | nil     | Shutdown  | Shutdown returns a report with Succeed true and the Failed hook ran 0 times                          |
```

<!-- cases: N/A 2 overrides: the deadline is the consumer's escape hatch -->
<!-- cases: N/A 3 zero values: Shutdown without a deadline is the forbidden call and is documented, not tested -->
<!-- cases: N/A 4 boundaries, 6 duplicates: a single calling hook per row is sufficient -->
<!-- cases: N/A 9 panics: covered by "Starting hook panics are vetoes" -->
<!-- cases: N/A 10 compatibility: new API -->

### Requirement: Shutdown during an in-flight Start
If root `Shutdown` begins while the start pipeline runs, it SHALL cancel the running Start with cause `ErrShutdown` and fire Closing only after the pipeline returned or its own context ended (start-lifecycle D6). `Start` SHALL then skip Started and Ready and run Failed with an error wrapping `ErrShutdown`, even if the pipeline returned nil. Once the pipeline has returned, Closing and Closed MAY run before or while Started, Ready or Failed hooks run.

#### Scenario: Shutdown races a running Start
```gherkin
Given a root container with a built Shutdowner service "db" and one hook registered for each of Started, Ready, Failed, Closing and Closed
And <setup>
When the consumer calls Start in one goroutine and Shutdown in another at "<shutdown_point>", inside synctest.Test with channel handshakes
Then Start returns <start_result>
And Started ran <started> times, Ready <ready>, Failed <failed>, Closing 1 and Closed 1
And <ordering>

Examples:
  | shutdown_point            | setup                                                                                                   | start_result                                                     | started | ready | failed | ordering                                                                    |
  | Start finishes first      | a Started hook that blocks until Shutdown has returned                                                  | nil                                                              | 1       | 1     | 0      | the recorded order is "started-hook, closing-hook, closed-hook, ready-hook" |
  | Shutdown cancels Start    | a Starter "a" whose Start(ctx) blocks until ctx.Done() and returns ctx.Err()                            | an error for which errors.Is(err, ErrShutdown) is true | 0       | 0     | 1      | the Closing hook ran after "a".Start returned                               |
  | Shutdown in the last step | a StageAfterStart kind handler that starts Shutdown in a goroutine, waits until synctest.Wait shows it blocked, and returns nil | an error for which errors.Is(err, ErrShutdown) is true | 0       | 0     | 1      | the Closing hook ran after the handler returned                             |
```

#### Scenario: Shutdown interrupts a Starting hook or a late pipeline step
```gherkin
Given a root container inside synctest.Test with a Failed hook recording its error
And <setup>
When the consumer calls Start
Then Start returns an error for which errors.Is(err, ErrShutdown) is true
And the Failed hook received that same error

Examples:
  | setup                                                                                                                                         |
  | a Starting hook that starts Shutdown in a goroutine, waits for ctx.Done() and returns ctx.Err()                                                |
  | lazy "x" registered as any with kind "k" for Job, whose provider starts Shutdown in a goroutine, waits until it is blocked and returns a non-Job value |
  | lazy Starter "scheduler", lazy Starter "job" whose provider invokes "scheduler", DeclareDependency(root, "scheduler", "job") before Start, and a StageBeforeStart handler of kind "k" for lazy "j" that starts Shutdown in a goroutine, waits until it is blocked and returns nil, so the cycle check fails |
```

#### Scenario: A pipeline panic after Shutdown began
```gherkin
Given a root container inside synctest.Test created with StartParallelism <n>, with a Failed hook recording its error
And a HookAfterStart hook that panics with errBoom for "a"
And lazy Starter "a" whose Start(ctx) blocks until ctx.Done() and returns nil, <others>
When the consumer calls Start, and Shutdown() is called in another goroutine once "a" has entered Start
Then Start panics with errBoom
And the Failed hook received an error matching both errBoom and ErrShutdown

Examples:
  | n | others                                  |
  | 1 | and no other service                    |
  | 4 | and lazy Starter "b" that returns nil   |
```

#### Scenario: A start finishing as Shutdown begins
```gherkin
Given 20000 fresh root containers, each with a StageAfterStart handler of kind "k" for lazy "j" that signals another goroutine to call Shutdown() and then returns nil
When the consumer calls <operation> on each and waits for its Shutdown
Then every call returns nil or an error for which errors.Is(err, ErrShutdown) is true

Examples:
  | operation                                                   |
  | Start()                                                     |
  | Refresh(), after Start() and registering "j" with the handler |
```

<!-- cases: N/A 2 overrides: the Shutdown context deadline is the only control and is covered by "Starting and Closing hooks must not call Shutdown synchronously" -->
<!-- cases: N/A 3 zero values, 4 boundaries: one Start and one Shutdown are sufficient; many callers are covered by the at-most-once requirement -->
<!-- cases: 5 errors: the cancellation error is asserted in each row, and holds when the pipeline returns at the moment Shutdown begins -->
<!-- cases: N/A 9 panics: covered by the panic requirements -->
<!-- cases: N/A 10 compatibility: new API -->

### Requirement: Observer hook panics are recovered and re-raised
A panic in a Started, Ready, Failed, Closing or Closed hook SHALL be recovered. The remaining hooks of that list and the rest of `Start` or `Shutdown`, including its phase transitions, SHALL still run. Once the operation is complete, the first recovered panic SHALL be passed on the calling goroutine to `InjectorOpts.HookContainerPanic` with the hook point name; later ones SHALL be logged through `InjectorOpts.Logger` as an Error record `panic recovered` with attribute `point`, the value itself only in a Debug record `panic value`. When `HookContainerPanic` is nil, the default SHALL re-panic with the recovered value.

#### Scenario: Default re-panics after the operation completes
```gherkin
Given a root container with a built Shutdowner service "db" and no HookContainerPanic
And <setup>
And a <hook> hook that panics with "kaboom", followed by a recording hook in the same list
When the consumer calls <operation> and recovers its panic
Then the recovered value is "kaboom"
And <completed> before the panic reached the caller
And a later call to Start returns an error for which errors.Is(err, <later_error>) is true

Examples:
  | hook    | setup                                               | operation | completed                                              | later_error           |
  | Started | a Ready hook that records its call                  | Start     | the following Started hook and the Ready hook ran      | ErrAlreadyStarted     |
  | Ready   | no other hooks                                      | Start     | the following Ready hook ran                           | ErrAlreadyStarted     |
  | Failed  | a Starter service "server" whose Start returns boom | Start     | the following Failed hook ran                          | ErrAlreadyStarted     |
  | Closing | a Closed hook that records its call                 | Shutdown  | "db" was shut down and the following Closing and the Closed hook ran | ErrShutdown |
  | Closed  | no other hooks                                      | Shutdown  | the following Closed hook ran                          | ErrShutdown |
```

#### Scenario: HookContainerPanic overrides the default
```gherkin
Given a root container whose HookContainerPanic records the hook point and the recovered value
And a <hook> hook that panics with "kaboom", followed by a <hook> hook that panics with "boom"
And a Logger whose handler records every record at Debug level
When the consumer calls <operation>
Then <operation> returns <result> without panicking
And HookContainerPanic was called exactly 1 time with hook "<point>" and value "kaboom"
And the handler recorded one "panic recovered" record at Error level with point "<point>" and one "panic value" record at Debug level containing "boom"

Examples:
  | hook    | operation | result                     | point   |
  | Started | Start     | nil                        | started |
  | Closing | Shutdown  | a report with Succeed true | closing |
```

<!-- cases: N/A 3 zero values: panic(nil) surfaces as *runtime.PanicNilError (Go 1.21+) and is handled like any panic value -->
<!-- cases: N/A 4 boundaries: one and two panicking hooks are covered; more add nothing -->
<!-- cases: N/A 5 errors: observer hooks have no error return -->
<!-- cases: N/A 6 duplicates: covered by the two-panic override rows -->
<!-- cases: N/A 8 concurrency: panics are handled on the calling goroutine only -->
<!-- cases: 10 compatibility: the default propagates the panic, like per-service hooks do -->
