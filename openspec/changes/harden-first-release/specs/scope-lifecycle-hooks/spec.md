# Spec Delta

## MODIFIED Requirements

### Requirement: Scope Starting hooks can veto Scope.Start
`Scope.Start` on a child scope SHALL run the `HookScopeStarting` hooks with that scope before starting anything; a non-nil error SHALL abort it with an error wrapping the hook's error, and a panic SHALL abort it with an error reading `scope starting hook panicked: <value>` that wraps the value when it is an error, as for container Starting hooks. `HookScopeStarted` hooks SHALL run after the scope's services started successfully.

#### Scenario: Starting hook allows or vetoes Scope.Start
```gherkin
Given a started root container and a child scope "tenant" with lazy Starter "worker"
And a HookScopeStarting hook that <behavior>
And a HookScopeStarted hook recording its scope name
When the consumer calls tenant.Start()
Then the result is <result>
And "worker" was started <starts> times
And the Started hook recorded <recorded>

Examples:
  | behavior                     | result                                                   | starts | recorded |
  | returns nil                  | nil                                                      | 1      | tenant   |
  | returns errVeto              | an error matching errVeto, containing "scope `tenant`"   | 0      | nothing  |
  | panics with "veto-boom"      | an error containing "doger: scope `tenant` start aborted: scope starting hook panicked: veto-boom" | 0 | nothing |
  | panics with errVeto          | an error matching errVeto, containing "scope starting hook panicked" | 0 | nothing  |
```

<!-- cases: 1 default: the nil row -->
<!-- cases: N/A 2 overrides: veto is the override -->
<!-- cases: N/A 3 zero values, 4 boundaries: one hook per row -->
<!-- cases: 5 errors: veto error and panic rows; a panic message mirrors the container's -->
<!-- cases: N/A 6 duplicates: a second Scope.Start starts nothing new and is covered in container-start -->
<!-- cases: 7 lifecycle: Scope.Start before the root started is covered in container-start (ErrNotStarted, no hooks) -->
<!-- cases: N/A 8 concurrency: covered in container-start -->
<!-- cases: 9 panics: the panic row -->
<!-- cases: N/A 10 compatibility: new API -->

### Requirement: Scope Closing and Closed hooks bracket a child scope shutdown
When a child scope shuts down, directly or as part of its parent's shutdown, `HookScopeClosing` SHALL run with that scope before its services are shut down and `HookScopeClosed` after. They SHALL run once per shutdown of each child scope, children before their parent, except a child exporting into its parent (scope-export). A shutdown of a child scope that finds one of it running SHALL wait for it, bounded by its context, and return a copy of its report instead of walking the scope again.

#### Scenario: Closing and Closed around child scope shutdown
```gherkin
Given a root scope with child "tenant" and grandchild "jobs", each with a built lazy service implementing Shutdown() error that records "shutdown <scope>"
And HookScopeClosing and HookScopeClosed hooks recording "closing <scope>" and "closed <scope>"
When the consumer calls <operation>
Then the recorded events are <events>

Examples:
  | operation       | events                                                                                                   |
  | tenant.Shutdown() | closing jobs, shutdown jobs, closed jobs, closing tenant, shutdown tenant, closed tenant               |
  | root.Shutdown()   | closing jobs, shutdown jobs, closed jobs, closing tenant, shutdown tenant, closed tenant                |
  | jobs.Shutdown()   | closing jobs, shutdown jobs, closed jobs                                                               |
  | tenant.Shutdown() then root.Shutdown() | closing jobs, shutdown jobs, closed jobs, closing tenant, shutdown tenant, closed tenant, closing tenant, closed tenant |
```

#### Scenario: Overlapping shutdowns of a child scope join
```gherkin
Given a root scope, inside synctest.Test, with child "tenant" holding value services "a", "b", "c" and "d" that count their Shutdown calls
And a HookScopeClosing hook that blocks its first call until released, and a counting HookScopeClosed hook
And tenant.Shutdown() is running in another goroutine, blocked in that hook
When the consumer calls <second> in another goroutine, waits until every goroutine is blocked, then releases the hook
Then <outcome>

Examples:
  | second                                                                    | outcome                                                                                                 |
  | tenant.Shutdown()                                                         | both reports succeed; each hook ran once; each service shut down once                                    |
  | root.Shutdown()                                                           | both reports succeed; each hook ran once; each service shut down once                                    |
  | tenant.ShutdownWithContext(ctx), ctx with a 1s timeout, before the release | it returns a failed report keyed by "tenant" with an empty service name, matching context.DeadlineExceeded; the first report then succeeds |
  | tenant.Shutdown(), where a custom Logger handler panics on the first record it handles after its services shut down | the first call panics with that value; the second gets a failed report containing "doger: scope shutdown did not complete" |
```

#### Scenario: Root scope fires no scope hook
```gherkin
Given a root scope with counting HookScopeStarting, HookScopeStarted, HookScopeClosing and HookScopeClosed hooks and no child scope
When the consumer calls Start() and then Shutdown()
Then every counting hook ran 0 times
```

#### Scenario: Closing and Closed around an exporting child
```gherkin
Given a root scope with child "tenant" and grandchild "jobs"
And a built lazy "*app.Pool" in "jobs" exported into "tenant" with doger.Export[*app.Pool](jobs), recording "shutdown jobs"
And a built lazy "*app.Worker" in "tenant" invoking "*app.Pool", recording "shutdown tenant"
And <more>
And HookScopeClosing and HookScopeClosed hooks recording "closing <scope>" and "closed <scope>"
When the consumer calls tenant.Shutdown()
Then the recorded events are <events>

Examples:
  | more                                                                        | events                                                                                                  |
  | nothing more                                                                | closing tenant, shutdown tenant, closing jobs, shutdown jobs, closed jobs, closed tenant                 |
  | a built lazy "x" in "jobs" invoking "*app.Worker", recording "shutdown x"   | closing tenant, closing jobs, shutdown x, shutdown tenant, shutdown jobs, closed jobs, closed tenant     |
```

#### Scenario: An exporting child with no service left closes once
```gherkin
Given a root scope with child "tenant" and grandchild "jobs"
And a built lazy "*app.Pool" in "jobs" exported into "tenant" with doger.Export[*app.Pool](jobs)
And a built lazy "*app.Worker" in "tenant" invoking "*app.Pool", recording "shutdown tenant"
And HookScopeClosing and HookScopeClosed hooks recording "closing <scope>" and "closed <scope>"
And the consumer called doger.ShutdownNamed(jobs, "*app.Pool")
When the consumer calls tenant.Shutdown()
Then "closing jobs" and "closed jobs" are each recorded once, "closed jobs" right after "closing jobs"
And both are recorded after "closing tenant" and before "closed tenant"
```

#### Scenario: Shutdowns of an exporting child join its parent's shutdown
```gherkin
Given a root scope, inside synctest.Test, with counting HookScopeClosing and HookScopeClosed hooks
And <containers>
And <running> is running in another goroutine, blocked as described
When the consumer calls <second> in another goroutine, waits until every goroutine is blocked, then releases the block
Then <outcome>

Examples:
  | containers | running | second | outcome |
  | child "redis" with a value "*app.Pool" exported with doger.Export[*app.Pool](redis), and a root service "app" invoking "*app.Pool" whose first Shutdown blocks | root.Shutdown(), blocked in the Shutdown of "app" | redis.Shutdown() | both reports succeed; each hook ran once; "app" and the pool were shut down once, the pool after "app" |
  | child "redis" with a value "*app.Pool" exported with doger.Export[*app.Pool](redis), and a HookScopeClosing that blocks its first call | redis.Shutdown(), blocked in that hook | root.Shutdown() | both reports succeed; each hook ran once; the pool was shut down once |
  | child "tenant" with grandchild "jobs", a value "*app.Pool" in "jobs" exported into "tenant", a "worker" in "tenant" invoking "*app.Pool" whose first Shutdown blocks, every HookScopeClosing panicking, and a custom Logger handler that panics when it handles a "panic recovered" record | tenant.Shutdown(), blocked in the Shutdown of "worker" | jobs.Shutdown() | the first call panics with the handler's value; jobs.Shutdown() returns a failed report containing "doger: scope shutdown did not complete" |
```

<!-- cases: 1 default: no hooks registered keeps shutdown unchanged (existing scope tests) -->
<!-- cases: N/A 2 overrides: Closing/Closed only observe -->
<!-- cases: N/A 3 zero values: covered by the registration requirement -->
<!-- cases: 4 boundaries: no child (root only), one child, nested grandchild, exporting grandchild, an exporting grandchild's service using a parent dependent, an exporting grandchild with no service left -->
<!-- cases: N/A 5 errors: shutdown errors stay in the report -->
<!-- cases: 6 duplicates: a child shut down directly stays attached to its parent, so a later parent shutdown fires its hooks again (last row) -->
<!-- cases: 7 lifecycle: direct child shutdown, as part of root shutdown, and an exporting child (closes inside its parent's Closing/Closed pair, after the parent's services that depend on its exports) -->
<!-- cases: 8 concurrency: overlapping shutdowns of one scope join (direct, through root Shutdown, bounded by a context, after a winner panic), and so do shutdowns of an exporting child and of its parent, in either order and after a panic in the parent's pass; sibling scopes shut down in parallel, so hooks for siblings may run concurrently; documented, not ordered -->
<!-- cases: N/A 9 panics: covered by the next requirement -->
<!-- cases: 10 compatibility: BEHAVIOR CHANGE (flagged) - the exception applies only to scopes that export into their parent; every other scope keeps children-first order -->

### Requirement: Scope hook panics follow the container panic policy
A panic in a `HookScopeStarted`, `HookScopeClosing` or `HookScopeClosed` hook SHALL be recovered so the operation completes, and then passed to `HookContainerPanic` with the point name `scope-started`, `scope-closing` or `scope-closed`; when `HookContainerPanic` is nil it SHALL re-panic with the original value after the operation completed. Later panics of the same operation SHALL be logged through `InjectorOpts.Logger` as an Error record `panic recovered` whose `point` attribute is their point name, such as `scope-closing`, without calling them container hooks; the value appears only in a Debug record `panic value`.

#### Scenario: Observer scope hook panics
```gherkin
Given a started root container with a child scope "tenant" holding built lazy "svc" implementing Shutdown() error
And a <hook> hook that panics with "scope-boom"
And HookContainerPanic is <policy>
When the consumer calls <operation>
Then <outcome>
And "svc" <svc-state>

Examples:
  | hook             | policy             | operation         | outcome                                                     | svc-state                    |
  | HookScopeClosing | nil                | tenant.Shutdown() | the call panics with "scope-boom"                           | had Shutdown called 1 time   |
  | HookScopeClosed  | a recording func   | tenant.Shutdown() | the call returns and the recorder saw "scope-closed", "scope-boom" | had Shutdown called 1 time |
  | HookScopeStarted | a recording func   | tenant.Start()    | the call returns nil and the recorder saw "scope-started", "scope-boom" | had Shutdown called 0 times |
  | HookScopeClosing | a recording func   | tenant.Shutdown() | the call returns and the recorder saw "scope-closing", "scope-boom" | had Shutdown called 1 time |
  | HookScopeStarted | nil                | tenant.Start()    | a second Started hook ran, then the call panics with "scope-boom" | had Shutdown called 0 times |
```

#### Scenario: Later scope hook panics are logged with their point
```gherkin
Given a started root container with a child scope "tenant", a Logger whose handler records every record at Debug level, and a recording HookContainerPanic
And two HookScopeClosing hooks that panic with "first" and "second"
When the consumer calls tenant.Shutdown()
Then the recorder saw "scope-closing", "first"
And the handler recorded one "panic recovered" record with point "scope-closing" and one "panic value" record containing "second"
And no recorded point attribute is "container scope-closing"
```

<!-- cases: N/A 2 overrides: HookContainerPanic is the override -->
<!-- cases: N/A 3 zero values, 4 boundaries: one panicking hook per row -->
<!-- cases: N/A 5 errors: a Starting hook panic is a veto (previous requirement) -->
<!-- cases: 6 duplicates: the first panic is dispatched, later ones are logged with their point -->
<!-- cases: 7 lifecycle: the operation completes before the panic is dispatched -->
<!-- cases: N/A 8 concurrency: dispatch happens on the caller's goroutine -->
<!-- cases: 9 panics: the subject of this requirement -->
<!-- cases: N/A 10 compatibility: new API -->
