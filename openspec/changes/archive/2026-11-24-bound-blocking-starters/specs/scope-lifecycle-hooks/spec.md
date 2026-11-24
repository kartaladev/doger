# Spec Delta

## MODIFIED Requirements

### Requirement: Scope Started hooks skip a scope that is shutting down
`HookScopeStarted` hooks SHALL NOT run when the scope, or one of its ancestors, was shutting down when `Scope.Start` collected its services, so that nothing of it was started. Nor SHALL they run when a shutdown of the scope, or of one of its ancestors, began after `Scope.Start` collected its services: that shutdown cancels their running start steps (container-start, "Child scope shutdown waits for a start that is starting its services"), `Scope.Start` skips them, and the shutdown waits for `Scope.Start` to return, bounded by its context, before its `HookScopeClosing` hooks run; this holds also when a bounded shutdown gave up waiting and completed before `Scope.Start` returned. `Scope.Start` SHALL still return nil then. A shutdown that began before `Scope.Start` collected the services, and completed by then, is one `Scope.Start` runs after: the hooks SHALL run. A shutdown of a descendant scope only skips that scope's services: the hooks SHALL run for the scope `Scope.Start` runs on.

#### Scenario: Scope.Start during the scope's shutdown fires no Started hook
```gherkin
Given a started root container and child scope "tenant" with lazy Starter "w", registered after Start
And a counting HookScopeStarted hook
And <shutting-down>.Shutdown() is running in another goroutine, blocked in a HookScopeClosing hook, having begun <began>
When the consumer calls tenant.Start()
Then it returns nil
And "w" was started 0 times
And the Started hook ran 0 times

Examples:
  | shutting-down                                  | began                                                                                      |
  | tenant                                         | before tenant.Start() was called                                                           |
  | "parent", the scope "tenant" was created under | before tenant.Start() was called                                                           |
  | tenant                                         | from a HookScopeStarting hook, which waits until the shutdown is blocked, then returns nil |
```

<!-- cases: N/A 1 default, 2 overrides: there is no option -->
<!-- cases: N/A 3 zero values: covered by the veto requirement -->
<!-- cases: 4 boundaries: the scope itself and an ancestor; a shutdown before Scope.Start and one beginning after its Starting hooks ran (only the run's claim sees it); one beginning after the services were collected, unbounded and bounded, and a descendant's ("Scope.Start fires no Started hook when a shutdown cancels its steps") -->
<!-- cases: N/A 5 errors: Scope.Start returns nil -->
<!-- cases: N/A 6 duplicates: a later Scope.Start after the shutdown finds nothing to start -->
<!-- cases: 7 lifecycle: the subject -->
<!-- cases: 8 concurrency: the shutdown runs in another goroutine; runs under -race with goleak -->
<!-- cases: N/A 9 panics: none -->
<!-- cases: 10 compatibility: BEHAVIOR CHANGE (flagged) - a shutdown that began after Scope.Start collected the scope's services used to wait for them to start, and the Started hooks ran, possibly after the Closing hooks; the shutdown now cancels their start steps and the Started hooks do not run. The rows of "Scope.Start during the scope's shutdown fires no Started hook" are unchanged -->

#### Scenario: A shutdown that completed before Scope.Start claimed the scope
```gherkin
Given a started root container and child scope "tenant" with lazy Starter "w", registered after Start
And a counting HookScopeStarted hook
And tenant.Start() is running in another goroutine and has collected "w", but has not claimed its candidates
When tenant.Shutdown() runs to completion
And tenant.Start() continues
Then it returns nil
And "w" was started 0 times
And the Started hook ran 1 time, as for a tenant.Start() called after tenant.Shutdown() returned
```

#### Scenario: Scope.Start fires no Started hook when a shutdown cancels its steps
```gherkin
Given a started root container created with InjectorOpts{StartTimeout: <timeout>}, inside synctest.Test, and child scope "c" created after Start
And a HookScopeStarted hook that counts its calls by scope
And lazy Starter "x" in <x-scope>, registered after Start, whose Start(ctx) error <behaviour>
And c.Start() is running in another goroutine and "x" has entered Start
When the consumer calls <shutdown>
And <then>
Then c.Start() returns nil
And the HookScopeStarted hook ran <started> times for "c" and 0 times for any other scope

Examples:
  | timeout | x-scope                  | behaviour                                          | shutdown                                  | then                                                                                    | started |
  | 0       | "c"                      | waits for ctx.Done(), then returns ctx.Err()       | c.Shutdown()                              | c.Shutdown() returns a successful report                                                | 0       |
  | 5s      | "c"                      | waits for ctx.Done(), then returns ctx.Err()       | c.Shutdown()                              | c.Shutdown() returns a successful report                                                | 0       |
  | 0       | "c"                      | ignores ctx and blocks until released by the test  | c.ShutdownWithContext with a 10ms timeout | c.ShutdownWithContext returns a failed report after 10ms, and only then "x" is released | 0       |
  | 0       | grandchild "deep" of "c" | waits for ctx.Done(), then returns ctx.Err()       | deep.Shutdown()                           | deep.Shutdown() returns a successful report                                             | 1       |
```
