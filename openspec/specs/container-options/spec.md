# container-options Specification

## Purpose

Defines how a container treats the `InjectorOpts` it is given and the hooks added to it later: constructors never write the caller's options, containers stay independent of each other and of any global, hook registration is safe at any time, and per-container limits replace package-level variables.

## Requirements

### Requirement: Constructors never write the caller's options
`NewWithOpts` and `CloneWithOpts` SHALL take a copy of the `*InjectorOpts` they receive, including a copy of every hook slice, and SHALL NOT write any field of the caller's value. A nil `opts` SHALL behave like `&InjectorOpts{}`.

#### Scenario: The caller's options are unchanged after construction
```gherkin
Given an InjectorOpts value with <field> set to <value>
When the consumer calls <constructor>
Then every field of the caller's InjectorOpts value equals its value before the call
And the container behaves with <field> as <effective>

Examples:
  | constructor                               | field                  | value                           | effective                        |
  | doger.NewWithOpts(opts)                   | Logger                 | nil                             | a discard logger                 |
  | doger.NewWithOpts(opts)                   | HealthCheckTimeout     | 0                               | 10s                              |
  | doger.NewWithOpts(opts)                   | MaxInvocationFrames    | 0                               | 100                              |
  | doger.NewWithOpts(opts)                   | HookBeforeInvocation   | a slice of 1 hook with cap 4    | 1 hook                           |
  | source.CloneWithOpts(opts)                | Clock                  | nil                             | doger.RealClock()                |
```

#### Scenario: A nil options pointer
```gherkin
Given no InjectorOpts value
When the consumer calls doger.NewWithOpts(nil)
Then the returned root is non-nil
And it behaves exactly like doger.New()
```

#### Scenario: Containers built from one options value are independent
```gherkin
Given one InjectorOpts value "opts" holding 1 HookBeforeInvocation hook "shared"
And root "a" created with doger.NewWithOpts(opts) and root "b" created with <second>
And a lazy "svc" registered in both
When the consumer calls <add> and then invokes "svc" from "b"
Then the hooks that ran for "b" are <ran>
And opts.HookBeforeInvocation still holds exactly 1 hook

Examples:
  | second                    | add                                                  | ran                   |
  | doger.NewWithOpts(opts)   | a.AddBeforeInvocationHook(hook "only-a")             | shared                |
  | a.CloneWithOpts(opts)     | a.AddBeforeInvocationHook(hook "only-a")             | shared                |
  | doger.NewWithOpts(opts)   | b.AddBeforeInvocationHook(hook "only-b")             | shared, only-b        |
  | a.Clone()                 | a.AddBeforeInvocationHook(hook "only-a")             | shared                |
```

<!-- cases: 1 happy path: nil-pointer scenario and the defaults rows -->
<!-- cases: 2 overrides: each field row is an explicit value that must survive unchanged -->
<!-- cases: 3 zero values: nil opts, nil Logger, zero durations, zero frame cap -->
<!-- cases: 4 boundaries: a hook slice with spare capacity (cap 4, len 1) is the aliasing boundary -->
<!-- cases: N/A 5 invalid input: no option value makes construction fail in this change -->
<!-- cases: 6 duplicate: the same opts value used twice -->
<!-- cases: 7 lifecycle: clones (Clone and CloneWithOpts) -->
<!-- cases: N/A 8 concurrency: covered by "Hook registration is safe at any time" -->
<!-- cases: N/A 9 panics: construction does not panic on any opts value -->
<!-- cases: 10 compatibility: a caller that never reused opts sees no difference -->

### Requirement: Hook registration is safe at any time
Every `RootScope.AddXxxHook` method SHALL be safe to call concurrently with registration, invocation, start, health checks, shutdown and other `AddXxxHook` calls. A hook added while an operation runs SHALL apply to operations that begin after the `AddXxxHook` call returned; whether it applies to the running one is unspecified.

#### Scenario: Adding hooks while the container works
```gherkin
Given a root scope with 10 lazy services "svc-0" to "svc-9", each opted in with doger.AsStarter() and implementing Start(ctx) error and Shutdown() error
When 4 goroutines run <operation> in a loop while 4 goroutines call <adder> 100 times each with a hook that increments a counter
Then the race detector reports no data race
And no call panics

Examples:
  | operation                                   | adder                          |
  | doger.InvokeNamed[*Svc](root, "svc-N")       | root.AddBeforeInvocationHook   |
  | doger.InvokeNamed[*Svc](root, "svc-N")       | root.AddAfterInvocationHook    |
  | doger.ProvideNamedValue(root, "v-N", N)      | root.AddBeforeRegistrationHook |
  | doger.InvokeNamed[*Svc](root, "svc-N")       | root.AddAfterBuildHook         |
  | doger.ShutdownNamed(root, "svc-N")           | root.AddAfterShutdownHook      |
  | doger.ShutdownNamed(root, "svc-N")           | root.AddBeforeShutdownHook     |
  | root.Start(ctx)                             | root.AddBeforeStartHook        |
  | root.Shutdown()                             | root.AddContainerClosingHook   |
```

#### Scenario: A hook added after Add returns applies to the next operation
```gherkin
Given a root scope with a lazy "svc"
And the consumer called root.AddBeforeInvocationHook with a hook that records the service name
When the consumer invokes "svc" from the root
Then the hook recorded "svc" exactly 1 time
```

<!-- cases: 1 happy path: second scenario -->
<!-- cases: 2 overrides: every adder category in the Examples -->
<!-- cases: 3 zero values: a nil hook passed to AddXxxHook is ignored, as an extra row in the test table -->
<!-- cases: 4 boundaries: zero hooks before adding, and 400 hooks added concurrently -->
<!-- cases: N/A 5 invalid input: adders return nothing and accept any non-nil func -->
<!-- cases: 6 duplicate: the same hook func added twice runs twice; covered as an extra row -->
<!-- cases: 7 lifecycle: adders during Start and during Shutdown rows -->
<!-- cases: 8 concurrency: the subject; goleak verifies no goroutine outlives the test -->
<!-- cases: N/A 9 panics: a hook's own panic follows its existing policy (service-lifecycle-hooks, container-lifecycle-hooks) -->
<!-- cases: 10 compatibility: adding hooks before any operation behaves as before -->

### Requirement: Invocation frames are capped per container
`InjectorOpts.MaxInvocationFrames` SHALL cap the invocation frames each service records in that container. 0 SHALL mean 100, a positive value SHALL be the cap, and a negative value SHALL turn recording off. The package SHALL NOT expose a package-level frame cap.

#### Scenario: Frame cap per option value
```gherkin
Given a root scope created with doger.NewWithOpts(&doger.InjectorOpts{MaxInvocationFrames: <option>})
And a lazy "svc"
When "svc" is invoked from <sites> distinct call sites
Then "svc" has recorded <recorded> invocation frames

Examples:
  | option | sites | recorded |
  | 0      | 150   | 100      |
  | 0      | 99    | 99       |
  | 1      | 3     | 1        |
  | 5      | 5     | 5        |
  | 5      | 6     | 5        |
  | -1     | 10    | 0        |
```

#### Scenario: Two containers keep their own caps
```gherkin
Given root "a" created with MaxInvocationFrames 2 and root "b" created with MaxInvocationFrames 0
And a lazy "svc" in each
When "svc" is invoked from 5 distinct call sites in each container, concurrently
Then "svc" in "a" has recorded 2 invocation frames
And "svc" in "b" has recorded 5 invocation frames
And the race detector reports no data race
```

Invocation frames are recorded internally and are not exposed by an exported API; these scenarios are verified by in-package tests, as in service-source-tracking.

<!-- cases: 1 happy path: the 0 rows (default 100) -->
<!-- cases: 2 overrides: positive and negative rows -->
<!-- cases: 3 zero values: option 0 -->
<!-- cases: 4 boundaries: 99/100/150 against the default; 5/6 at a custom cap; 1 -->
<!-- cases: N/A 5 invalid input: every int is valid -->
<!-- cases: N/A 6 duplicate: the same call site twice records one frame, unchanged -->
<!-- cases: N/A 7 lifecycle: the cap applies for the container's life -->
<!-- cases: 8 concurrency: second scenario -->
<!-- cases: N/A 9 panics: recording does not panic -->
<!-- cases: 10 compatibility: BREAKING: the package variable MaxInvocationFrames is removed; migration in design D10 -->

### Requirement: No package-level container exists
The package SHALL NOT expose a package-level container. Every operation SHALL act only on the container passed to it, and a nil injector, root or scope SHALL be rejected as specified in resolution-errors, never redirected to another container.

#### Scenario: A rejected nil call touches no container
```gherkin
Given root "a" created with doger.New()
When the consumer calls <call> and recovers or receives the error
Then the error or panic value matches doger.ErrNilInjector
And root "a" has no service named "int"

Examples:
  | call                                         |
  | doger.ProvideValue[int](nil, 1)              |
  | doger.ProvideNamedValue[int](nil, "int", 1)  |
  | doger.Invoke[int](nil)                       |
```

#### Scenario: Containers are isolated
```gherkin
Given roots "a" and "b" created with doger.New()
And a value "int" registered in "a"
When the consumer invokes "int" from "b"
Then the error matches doger.ErrServiceNotFound
```

<!-- cases: 1 happy path: containers are isolated -->
<!-- cases: N/A 2 overrides: there is no global container to configure -->
<!-- cases: 3 zero values: nil injector rows; the full nil matrix (every entry point, typed-nil) is owned by resolution-errors -->
<!-- cases: N/A 4 boundaries: no numeric input -->
<!-- cases: 5 errors: ErrNilInjector rows -->
<!-- cases: N/A 6 duplicate: not applicable -->
<!-- cases: N/A 7 lifecycle: covered by container-shutdown for closed containers -->
<!-- cases: N/A 8 concurrency: no shared global state remains -->
<!-- cases: 9 panics: registration rows panic with an ErrNilInjector error -->
<!-- cases: 10 compatibility: BREAKING: DefaultRootScope and the nil fallback are removed; migration in design D10 -->
