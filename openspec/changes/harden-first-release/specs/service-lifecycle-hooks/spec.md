# Spec Delta

## Purpose

Defines the per-service invocation, start and shutdown hooks: their signatures, the context and elapsed time they receive, when they run relative to the operation, and what happens when one panics, so consumers can attach metrics and tracing to every service operation.

## ADDED Requirements

### Requirement: Per-service hook signatures
`InjectorOpts` and the matching `RootScope.AddXxxHook` methods SHALL use these signatures: `HookBeforeStart` and `HookBeforeShutdown` take `(ctx context.Context, scope *Scope, serviceName string)`; `HookAfterStart` and `HookAfterShutdown` take `(ctx, scope, serviceName, elapsed time.Duration, err error)`; `HookAfterInvocation` takes `(scope, serviceName, elapsed, err)`.

#### Scenario: Each hook receives the documented arguments
```gherkin
Given a root scope whose <hook> records its arguments
And a lazy service "svc" in the root implementing Start(ctx) error and Shutdown(ctx) error, opted in with doger.AsStarter()
When the consumer runs <operation>
Then the hook ran exactly 1 time for "svc"
And it received scope "[root]", service name "svc" and <extra>

Examples:
  | hook                | operation                     | extra                                     |
  | HookBeforeStart     | root.Start(ctx)               | a context                                 |
  | HookAfterStart      | root.Start(ctx)               | a context, an elapsed duration, a nil err |
  | HookBeforeShutdown  | root.Shutdown()               | a context                                 |
  | HookAfterShutdown   | root.Shutdown()               | a context, an elapsed duration, a nil err |
  | HookAfterInvocation | doger.InvokeNamed(root,"svc") | an elapsed duration, a nil err            |
```

#### Scenario: Hooks added after construction use the same signatures
```gherkin
Given a root scope created with doger.New()
And the consumer called <adder> with a hook that records its arguments
When the consumer runs <operation> for a lazy service "svc" opted in with doger.AsStarter()
Then the hook received the same arguments as the InjectorOpts field <hook>

Examples:
  | adder                          | hook                | operation        |
  | root.AddBeforeStartHook        | HookBeforeStart     | root.Start(ctx)  |
  | root.AddAfterStartHook         | HookAfterStart      | root.Start(ctx)  |
  | root.AddBeforeShutdownHook     | HookBeforeShutdown  | root.Shutdown()  |
  | root.AddAfterShutdownHook      | HookAfterShutdown   | root.Shutdown()  |
  | root.AddAfterInvocationHook    | HookAfterInvocation | doger.InvokeNamed |
```

<!-- cases: 1 happy path: the subject -->
<!-- cases: 2 overrides: InjectorOpts fields and AddXxxHook methods -->
<!-- cases: N/A 3 zero values: a nil hook in a slice is skipped, as today -->
<!-- cases: N/A 4 boundaries: no numeric input; elapsed values are covered by "Elapsed time is measured on the container clock" -->
<!-- cases: N/A 5 invalid input: hooks are typed funcs -->
<!-- cases: N/A 6 duplicate: several hooks run in slice order, as today -->
<!-- cases: 7 lifecycle: start, shutdown, invocation -->
<!-- cases: N/A 8 concurrency: covered by container-options "Hook registration is safe at any time" -->
<!-- cases: N/A 9 panics: covered by "Hook panics follow the operation's policy" -->
<!-- cases: 10 compatibility: BREAKING: these five hook types gain ctx and/or elapsed; HookBeforeInvocation, registration and build hooks are unchanged; migration in design D10 -->

### Requirement: Start and shutdown hooks receive the operation's context
`HookBeforeStart` and `HookAfterStart` SHALL receive the context Start, Refresh or `Scope.Start` runs with. `HookBeforeShutdown` and `HookAfterShutdown` SHALL receive a context derived from the shutdown's context, carrying its values, deadline and cancellation.

#### Scenario: The hook context carries the caller's values and deadline
```gherkin
Given a context "ctx" holding value "trace-id"="t-1" with a deadline 5s from now on the container clock
And a root scope whose <hook> records ctx.Value("trace-id") and whether ctx has a deadline
And a lazy service "svc" opted in with doger.AsStarter(), implementing Start(ctx) error and Shutdown(ctx) error, that was invoked
When the consumer calls <operation> with "ctx"
Then the hook recorded "t-1" and a deadline equal to ctx's deadline

Examples:
  | hook               | operation                     |
  | HookBeforeStart    | root.Start                    |
  | HookAfterStart     | root.Start                    |
  | HookBeforeShutdown | root.ShutdownWithContext      |
  | HookAfterShutdown  | root.ShutdownWithContext      |
  | HookBeforeShutdown | doger.ShutdownNamedWithContext(ctx, root, "svc") |
```

#### Scenario: A shutdown without a context passes a background-derived context
```gherkin
Given a root scope whose HookBeforeShutdown records ctx and ctx.Err()
And an invoked lazy service "svc" implementing Shutdown() error
When the consumer calls root.Shutdown()
Then the hook recorded a non-nil context
And ctx.Err() was nil
```

#### Scenario: A cancelled shutdown context is visible to the after-hook
```gherkin
Given a context "ctx" that is cancelled while "svc"'s Shutdown(ctx) runs
And "svc"'s Shutdown(ctx) returns ctx.Err()
And a root scope whose HookAfterShutdown records its ctx.Err() and err
When the consumer calls root.ShutdownWithContext(ctx)
Then the hook recorded ctx.Err() matching context.Canceled
And err matching context.Canceled
```

<!-- cases: 1 happy path: root.Shutdown() with a background-derived context -->
<!-- cases: 2 overrides: caller-supplied contexts in every row -->
<!-- cases: 3 zero values: Shutdown() without a context; a nil ctx passed to the signal helper is owned by shutdown-context -->
<!-- cases: N/A 4 boundaries: no numeric input -->
<!-- cases: 5 errors: cancelled context -->
<!-- cases: N/A 6 duplicate: not applicable -->
<!-- cases: 7 lifecycle: Start, ShutdownWithContext, ShutdownNamedWithContext, Refresh and Scope.Start share the start path -->
<!-- cases: 8 concurrency: cancellation while Shutdown runs -->
<!-- cases: N/A 9 panics: covered by "Hook panics follow the operation's policy" -->
<!-- cases: 10 compatibility: hooks previously received no context -->

### Requirement: Elapsed time is measured on the container clock
The `elapsed` argument SHALL be the duration of the hooked operation for that service, measured on the container's `Clock` from just before the operation to just after it: the Start method call, the Shutdown call, or the whole invocation including a build. It SHALL be 0 or more.

#### Scenario: Elapsed with a fake clock
```gherkin
Given a root scope created with Clock set to dogerclockwork.Wrap(fake), a clockwork fake clock
And a lazy service "svc" opted in with doger.AsStarter() whose <method> advances the fake clock by <advance>
And a root scope whose <hook> records elapsed
When the consumer runs <operation>
Then the hook recorded elapsed <elapsed>

Examples:
  | method       | advance | hook                | operation                          | elapsed |
  | provider     | 3s      | HookAfterInvocation | doger.InvokeNamed(root, "svc")     | 3s      |
  | provider     | 3s      | HookAfterInvocation | a second doger.InvokeNamed (cached) | 0s     |
  | Start(ctx)   | 250ms   | HookAfterStart      | root.Start(ctx)                    | 250ms   |
  | Shutdown()   | 1s      | HookAfterShutdown   | root.Shutdown()                    | 1s      |
  | Shutdown()   | 0s      | HookAfterShutdown   | root.Shutdown()                    | 0s      |
```

#### Scenario: Elapsed is reported for failures too
```gherkin
Given a root scope with a fake clock
And a lazy service "svc" whose provider advances the clock by 2s and returns errors.New("boom")
And a HookAfterInvocation that records elapsed and err
When the consumer invokes "svc"
Then the hook recorded elapsed 2s and an err wrapping "boom"
```

<!-- cases: 1 happy path: real clock is the default; with it elapsed is >= 0, checked as an extra row -->
<!-- cases: 2 overrides: the fake clock via InjectorOpts.Clock -->
<!-- cases: 3 zero values: 0s rows -->
<!-- cases: 4 boundaries: 0, 250ms, 1s, 3s -->
<!-- cases: 5 errors: "Elapsed is reported for failures too" -->
<!-- cases: N/A 6 duplicate: the cached-invocation row covers a repeated invocation -->
<!-- cases: 7 lifecycle: invocation, start, shutdown -->
<!-- cases: N/A 8 concurrency: elapsed is per call; covered by the existing -race suite -->
<!-- cases: N/A 9 panics: a panicking provider is a failure with its elapsed time, as the error row -->
<!-- cases: N/A 10 compatibility: new argument -->

### Requirement: Hook order around the operation
For one service operation, every before-hook SHALL run before the operation and every after-hook after it, in slice order, with the error the operation produced (nil on success, a recovered panic as an error).

#### Scenario: Before, operation, after
```gherkin
Given a root scope with 2 hooks for each of <before> and <after>, each appending its label to a shared log
And a lazy service "svc" whose <method> appends "op" to the log and returns <err>
When the consumer runs <operation>
Then the log is "before-1, before-2, op, after-1, after-2"
And each after-hook received err <received>

Examples:
  | before             | after               | method     | err                   | operation               | received               |
  | HookBeforeStart    | HookAfterStart      | Start(ctx) | nil                   | root.Start(ctx)         | nil                    |
  | HookBeforeStart    | HookAfterStart      | Start(ctx) | errors.New("no port") | root.Start(ctx)         | an error "no port"     |
  | HookBeforeShutdown | HookAfterShutdown   | Shutdown() | nil                   | root.Shutdown()         | nil                    |
  | HookBeforeInvocation | HookAfterInvocation | provider | nil                   | doger.InvokeNamed       | nil                    |
```

<!-- cases: 1 happy path: nil error rows -->
<!-- cases: N/A 2 overrides: order is fixed -->
<!-- cases: N/A 3 zero values: zero hooks means nothing runs, as today -->
<!-- cases: 4 boundaries: 2 hooks per slot pins slice order -->
<!-- cases: 5 errors: the Start error row -->
<!-- cases: N/A 6 duplicate: not applicable -->
<!-- cases: 7 lifecycle: start, shutdown, invocation -->
<!-- cases: N/A 8 concurrency: with StartParallelism > 1, hooks of different services may interleave, as documented today -->
<!-- cases: N/A 9 panics: covered by the next requirement -->
<!-- cases: 10 compatibility: order is unchanged from today -->

### Requirement: Hook panics follow the operation's policy
A panic in a before- or after-shutdown hook SHALL be recovered and reported as specified in container-shutdown, so it never ends the process. A panic in an invocation hook SHALL propagate to the caller of `Invoke`, and a panic in a start hook SHALL be handled by Start as before.

#### Scenario: Where a hook panic goes
```gherkin
Given a root scope whose <hook> panics with "hook-boom"
And a lazy service "svc" opted in with doger.AsStarter(), implementing Start(ctx) error and Shutdown() error, that was invoked when the operation is a shutdown
When the consumer runs <operation>
Then <outcome>

Examples:
  | hook                 | operation              | outcome                                                                                     |
  | HookBeforeShutdown   | root.Shutdown()        | the report's Err() holds an error for "svc" matching doger.ErrShutdownPanic and containing "hook-boom" |
  | HookAfterShutdown    | root.Shutdown()        | the report's Err() holds an error for "svc" matching doger.ErrShutdownPanic and containing "hook-boom" |
  | HookAfterInvocation  | doger.InvokeNamed      | the call panics with "hook-boom"                                                            |
  | HookBeforeStart      | root.Start(ctx)        | the Failed hooks run with an error containing "hook-boom" and Start re-panics with it        |
```

<!-- cases: N/A 1 happy path: covered by the other requirements -->
<!-- cases: N/A 2 overrides: HookContainerPanic receives shutdown hook panics per container-shutdown -->
<!-- cases: 3 zero values: a panic with nil follows container-shutdown (recovered as *runtime.PanicNilError) -->
<!-- cases: N/A 4 boundaries: no numeric input -->
<!-- cases: 5 errors: ErrShutdownPanic rows -->
<!-- cases: N/A 6 duplicate: several panicking hooks: the first panic stops the remaining hooks of that slot, as today -->
<!-- cases: 7 lifecycle: shutdown, invocation, start -->
<!-- cases: N/A 8 concurrency: covered by container-shutdown -->
<!-- cases: 9 panics: the subject -->
<!-- cases: 10 compatibility: invocation and start hook panic behavior is unchanged; shutdown hook panics no longer crash the process -->
