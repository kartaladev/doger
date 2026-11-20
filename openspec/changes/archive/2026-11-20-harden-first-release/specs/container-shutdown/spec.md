# Spec Delta

## Purpose

Defines how shutting down a scope stays safe and observable. It covers what happens when a service's `Shutdown` or a shutdown hook panics, the closed state a scope enters once its shutdown begins, child scopes detaching from their parent, shared eager values across clones, re-entrant root shutdown, and the `ShutdownReport` API.

## ADDED Requirements

### Requirement: A panicking Shutdown is reported, not raised
When a service's `Shutdown` method panics during a root or child shutdown, the shutdown SHALL recover the panic and SHALL NOT crash the process. It SHALL shut down every other service, run the remaining hooks, and record an error for the service that matches `ErrShutdownPanic`. When the panic value is an error, the recorded error SHALL also match that value with `errors.Is` and `errors.As`.

#### Scenario: A panicking Shutdown method is recorded in the report
```gherkin
Given a root scope with child scope "tenant"
And a built lazy "bad" in <scope> implementing <shutdowner> that panics with <value>
And a built lazy "good" in <scope> implementing Shutdown() error, counting its calls
When the consumer calls <operation>
Then the call returns without the process crashing
And the report fails, with an error for "bad" in <scope> matching ErrShutdownPanic and <also>
And the error message contains <text>
And "good" had its Shutdown called 1 time and has no error in the report

Examples:
  | scope    | shutdowner                               | value                       | operation               | also                       | text                     |
  | the root | Shutdown()                               | "boom"                      | root.Shutdown()         | nothing else               | "boom"                   |
  | the root | Shutdown() error                         | io.ErrClosedPipe            | root.Shutdown()         | io.ErrClosedPipe           | "io: read/write on closed pipe" |
  | the root | Shutdown(context.Context)                | &app.PoolError{Code: 7}     | root.Shutdown()         | errors.As *app.PoolError   | "pool error 7"           |
  | the root | Shutdown(context.Context) error          | nil (panic(nil))            | root.Shutdown()         | *runtime.PanicNilError     | "panic called with nil"  |
  | "tenant" | Shutdown()                               | "boom"                      | tenant.Shutdown()       | nothing else               | "boom"                   |
  | "tenant" | Shutdown()                               | "boom"                      | root.Shutdown()         | nothing else               | "boom"                   |
```

#### Scenario: A panicking eager value is recorded in the report
```gherkin
Given a root scope
And a value "cfg" registered with doger.ProvideNamedValue whose Shutdown() panics with "eager boom"
When the consumer calls root.Shutdown()
Then the report fails, with an error for "cfg" matching ErrShutdownPanic and containing "eager boom"
And root.Phase() is PhaseClosed
```

#### Scenario: The crash is gone in a separate process
```gherkin
Given a test binary that re-runs itself as a child process with DOGER_SHUTDOWN_PANIC_CHILD=1
And the child registers a built lazy "bad" whose Shutdown() panics with "boom", then calls root.Shutdown() and prints report.Err()
When the parent runs the child process
Then the child exits with status 0
And its output contains "doger: " and "boom"
```

<!-- cases: 1 default: the zero-configuration root and child shutdown rows -->
<!-- cases: 2 overrides: HookContainerPanic receives the first panic (next requirement); recovery itself cannot be turned off (proposal Defaults & Control) -->
<!-- cases: 3 zero values: panic(nil) row (Go 1.21+ PanicNilError) -->
<!-- cases: 4 boundaries: one panicking service next to a healthy one; root and child scope; all four Shutdowner forms; eager and lazy -->
<!-- cases: 5 errors: matches ErrShutdownPanic; an error-valued panic also matches its value, and errors.As reaches a typed panic value -->
<!-- cases: N/A 6 duplicates: one panic per service; a second shutdown finds nothing (see "A closed scope stays closed") -->
<!-- cases: 7 lifecycle: the phase still reaches PhaseClosed, and the Closed hooks run (next requirement) -->
<!-- cases: 8 concurrency: services shut down in parallel goroutines; the panic is recovered on its own goroutine; runs under -race -->
<!-- cases: 9 panics: the subject; the child-process scenario proves the process no longer crashes (regression for the audit defect) -->
<!-- cases: 10 compatibility: BEHAVIOR CHANGE (flagged) - this used to crash the process; no consumer could rely on that -->

### Requirement: A panicking shutdown hook is reported, not raised
When a `HookBeforeShutdown` or `HookAfterShutdown` hook panics for a service, the shutdown SHALL recover it and record an error for that service matching `ErrShutdownPanic`. The service's `Shutdown` method SHALL still be called exactly once, and the other hooks of the list SHALL still run. If both the hook and `Shutdown` fail, the recorded error SHALL match both errors.

#### Scenario: A panicking shutdown hook is recorded for its service
```gherkin
Given a root scope whose InjectorOpts has <hooks>
And a built lazy "db" implementing Shutdown() error that counts its calls and returns <shutdown error>
When the consumer calls root.Shutdown()
Then the call returns without the process crashing
And "db" had its Shutdown called 1 time
And the report has an error for "db" matching ErrShutdownPanic and <also>
And the second hook of each list ran once for "db"

Examples:
  | hooks                                                                         | shutdown error   | also                 |
  | HookBeforeShutdown: [panics with "before boom", records]                      | nil              | containing "before boom" |
  | HookAfterShutdown: [panics with "after boom", records]                        | nil              | containing "after boom"  |
  | HookAfterShutdown: [panics with "after boom", records]                        | io.ErrClosedPipe | io.ErrClosedPipe     |
  | HookBeforeShutdown: [panics with io.EOF, records]                             | nil              | io.EOF               |
```

<!-- cases: 1 default: no hooks means no hook panic (covered by the previous requirement) -->
<!-- cases: 2 overrides: hooks are the consumer's override; each list position is covered -->
<!-- cases: N/A 3 zero values: a nil hook in the list is the consumer's bug and panics like any hook; recorded as row 1 would be -->
<!-- cases: 4 boundaries: before vs after hook; the panicking hook first in its list, with a later one still running -->
<!-- cases: 5 errors: a hook panic alone, and a hook panic together with a Shutdown error (both matched) -->
<!-- cases: N/A 6 duplicates: one call per service -->
<!-- cases: 7 lifecycle: Shutdown still called once even when the before hook panicked -->
<!-- cases: 8 concurrency: hooks of different services run concurrently on their own goroutines; runs under -race -->
<!-- cases: 9 panics: the subject -->
<!-- cases: 10 compatibility: BEHAVIOR CHANGE (flagged) - a hook panic used to crash the process -->

### Requirement: The first shutdown panic reaches HookContainerPanic
Once a root or child shutdown has completed, Closed hooks included, the first panic recovered from a service `Shutdown` or a per-service shutdown hook SHALL be passed to `HookContainerPanic` with the point `"service-shutdown"` and the recovered value. Later panics of that shutdown, and a panic of an abandoned `Shutdown` (shutdown-context) arriving after its dispatch ran, SHALL only be logged at Error level. With no `HookContainerPanic`, the panic SHALL only be reported and logged.

#### Scenario: The first shutdown panic goes to HookContainerPanic
```gherkin
Given a root scope with HookContainerPanic <panic hook> and a HookContainerClosed hook that records when it ran
And built lazy services <services>, each panicking in Shutdown() with its own name
When the consumer calls root.Shutdown()
Then HookContainerPanic was called <calls> times, with the point "service-shutdown" and one of the panic values
And it was called after the Closed hook ran
And the report has an error matching ErrShutdownPanic for each of <services>

Examples:
  | panic hook                | services       | calls |
  | recording its arguments   | "a"            | 1     |
  | recording its arguments   | "a", "b", "c"  | 1     |
  | nil                       | "a"            | 0     |
```

#### Scenario: A HookContainerPanic that re-panics is the consumer's choice
```gherkin
Given a root scope whose HookContainerPanic re-panics with the recovered value
And a built lazy "a" whose Shutdown() panics with "boom"
When the consumer calls root.Shutdown() inside a function that recovers
Then the recovered value is "boom"
And root.Phase() is PhaseClosed
```

<!-- cases: 1 default: no HookContainerPanic means report and log only (row 3) -->
<!-- cases: 2 overrides: a recording hook, and a re-panicking hook -->
<!-- cases: 3 zero values: nil HookContainerPanic -->
<!-- cases: 4 boundaries: one panic vs three panics (only the first is passed) -->
<!-- cases: N/A 5 errors: the report requirement above covers matching -->
<!-- cases: N/A 6 duplicates: at most one call per shutdown -->
<!-- cases: 7 lifecycle: called after the operation completed, Closed hooks included, matching the existing observer-hook policy -->
<!-- cases: 8 concurrency: the three services panic on parallel goroutines; exactly one value reaches the hook; runs under -race -->
<!-- cases: 9 panics: a re-panicking HookContainerPanic propagates to the Shutdown caller after the container is closed -->
<!-- cases: 10 compatibility: HookContainerPanic gains the point "service-shutdown"; existing points are unchanged -->

### Requirement: A shut-down service is never built again
After shutting a service down, a scope SHALL NOT build a new instance for it, even for a caller that looked the service up before the shutdown removed it. Such a caller SHALL get an error matching `ErrShutdown` and no instance. Every instance the container built SHALL be shut down exactly once.

#### Scenario: An invocation that raced the shutdown is refused
```gherkin
Given a root scope with a lazy "svc" implementing Shutdown() error, counting its builds and its Shutdown calls
And "svc" was invoked once
And a HookBeforeInvocation that, for the second invocation of "svc" only, signals "looked-up" and then waits until root.Shutdown() returned
When the consumer invokes "svc" on goroutine G, waits for "looked-up", then calls root.Shutdown()
Then G receives a zero instance and an error matching ErrShutdown
And "svc" was built 1 time and had its Shutdown called 1 time
```

#### Scenario: Invoking a service shut down by name
```gherkin
Given a root scope with a lazy "svc" invoked once
And the consumer called doger.ShutdownNamed(root, "svc")
When the consumer invokes "svc"
Then the error matches ErrServiceNotFound
And "svc" was built 1 time
```

#### Scenario: Concurrent invocations during shutdown leave nothing running
```gherkin
Given a root scope with child scopes "s0" to "s9", each holding lazy "svc" implementing Shutdown() error, counting builds and Shutdown calls across all scopes
When 32 goroutines invoke "svc" from random scopes in a loop while the consumer calls root.Shutdown(), and every goroutine stops after its first error
Then every invocation either returned an instance before the shutdown reached its scope or an error matching ErrShutdown or ErrServiceNotFound
And the total number of builds equals the total number of Shutdown calls
And no goroutine is leaked
And this holds in each of 50 repeats
```

<!-- cases: 1 default: zero configuration -->
<!-- cases: N/A 2 overrides: refusing to resurrect is not configurable (proposal Defaults & Control) -->
<!-- cases: N/A 3 zero values: no input -->
<!-- cases: 4 boundaries: a caller that looked the service up just before removal (pinned with the hook handshake), and one after removal by name -->
<!-- cases: 5 errors: ErrShutdown for the race; ErrServiceNotFound after ShutdownNamed removed only the service (the scope stays open) -->
<!-- cases: 6 duplicates: builds equal shutdowns, so no instance is shut down twice or never -->
<!-- cases: 7 lifecycle: the subject -->
<!-- cases: 8 concurrency: 32 goroutines across 10 scopes, 50 repeats, -race, goleak -->
<!-- cases: N/A 9 panics: covered by the panic requirements -->
<!-- cases: 10 compatibility: BEHAVIOR CHANGE (flagged) - the racing invocation used to build and return an instance nobody shut down -->

### Requirement: A closed scope stays closed
Once a scope's shutdown has begun, the scope SHALL stay closed. `Invoke*` on it SHALL return an error matching `ErrShutdown`. `Provide*`, `Override*`, `Must*` registrations and `Scope()` on it SHALL panic with an error matching `ErrShutdown`, and `Alias*`/`Export*` SHALL return such an error. A second shutdown SHALL return a successful report with no services.

#### Scenario: Calls on a closed scope are refused
```gherkin
Given a root scope with child scope "tenant", each holding a lazy "svc" of type *app.Svc, invoked once
And the consumer called <shutdown>
When the consumer calls <call> on <target>
Then <outcome>

Examples:
  | shutdown           | target   | call                                                   | outcome                                              |
  | root.Shutdown()    | the root | doger.Invoke[*app.Svc]                                 | it returns an error matching ErrShutdown             |
  | root.Shutdown()    | the root | doger.InvokeNamed[*app.Svc](…, "svc")                  | it returns an error matching ErrShutdown             |
  | root.Shutdown()    | the root | doger.InvokeAll[*app.Svc]                              | it returns an error matching ErrShutdown             |
  | root.Shutdown()    | the root | doger.ProvideNamedValue(…, "late", 1)                  | it panics with an error matching ErrShutdown         |
  | root.Shutdown()    | the root | doger.OverrideNamedValue(…, "svc", &app.Svc{})         | it panics with an error matching ErrShutdown         |
  | root.Shutdown()    | the root | .Scope("late")                                         | it panics with an error matching ErrShutdown         |
  | root.Shutdown()    | the root | doger.AliasNamed[*app.Svc, app.Iface](…, "svc", "i")   | it returns an error matching ErrShutdown             |
  | tenant.Shutdown()  | "tenant" | doger.InvokeNamed[*app.Svc](…, "svc")                  | it returns an error matching ErrShutdown             |
  | tenant.Shutdown()  | "tenant" | doger.ProvideNamedValue(…, "late", 1)                  | it panics with an error matching ErrShutdown         |
  | tenant.Shutdown()  | "tenant" | .Scope("late")                                         | it panics with an error matching ErrShutdown         |
  | tenant.Shutdown()  | the root | doger.InvokeNamed[*app.Svc](…, "svc")                  | it returns the root's instance and a nil error       |
```

#### Scenario: A second shutdown of a closed scope succeeds with nothing to do
```gherkin
Given a root scope with child scope "tenant" holding a built lazy "svc" implementing Shutdown() error, counting its calls
And the consumer called <first>
When the consumer calls <second>
Then the second report succeeds and lists no services
And "svc" had its Shutdown called 1 time

Examples:
  | first              | second             |
  | tenant.Shutdown()  | tenant.Shutdown()  |
  | root.Shutdown()    | tenant.Shutdown()  |
```

#### Scenario: A scope created while its parent closes is refused
```gherkin
Given a root scope with child scope "a"
And <hook> that calls <create> and records whether it panicked and with what
When the consumer calls root.Shutdown()
Then the hook recorded a panic with an error matching ErrShutdown
And after Shutdown returned, <parent>.Children() is empty

Examples:
  | hook                                         | create            | parent  |
  | a HookContainerClosing hook                  | root.Scope("late") | the root |
  | a HookScopeClosing hook for scope "a"        | a.Scope("late")    | "a"      |
```

<!-- cases: 1 default: zero configuration -->
<!-- cases: N/A 2 overrides: a closed scope has no reopen option; build a new container (proposal Defaults & Control) -->
<!-- cases: N/A 3 zero values: a nil scope is ErrNilInjector (resolution-errors) -->
<!-- cases: 4 boundaries: during shutdown (hooks) and after it; a closed child next to an open root -->
<!-- cases: 5 errors: Invoke and Alias/Export return the error; registration and Scope() panic, as a duplicate registration does today; both match ErrShutdown -->
<!-- cases: 6 duplicates: a second shutdown is a successful no-op -->
<!-- cases: 7 lifecycle: the subject; Start after shutdown is container-start's -->
<!-- cases: N/A 8 concurrency: covered by "A shut-down service is never built again" -->
<!-- cases: 9 panics: registration and Scope() panic values match ErrShutdown -->
<!-- cases: 10 compatibility: BEHAVIOR CHANGE (flagged) - a container used to accept registrations and invocations after Shutdown; Scope() during shutdown used to leave an orphan scope whose services were never shut down -->

### Requirement: A shut-down child scope detaches from its parent
Once a child scope's shutdown has completed, whether called on the child or reached through an ancestor, the child SHALL no longer appear in its parent's `Children()` or `ChildByName`, and the parent SHALL accept a new child scope with the same name. Creating and shutting down child scopes repeatedly SHALL NOT grow the parent.

#### Scenario: A shut-down child is detached and its name reusable
```gherkin
Given a root scope with child scope "req" holding a built lazy "svc"
When the consumer calls <shutdown>
Then root.ChildByName("req") returns false
And root.Children() has <remaining> children
And root.Scope("req") returns a new, open scope without panicking

Examples:
  | shutdown                                   | remaining |
  | req.Shutdown()                             | 0         |
  | req.ShutdownWithContext(ctx with deadline) | 0         |
```

#### Scenario: Per-request scopes do not accumulate
```gherkin
Given a root scope
When the consumer runs <cycles> times: create root.Scope("req"), register a value, invoke it, call Shutdown on the scope
Then root.Children() is empty
And root.HealthReport() lists no service of a "req" scope

Examples:
  | cycles |
  | 1      |
  | 1000   |
```

#### Scenario: Concurrent per-request scopes detach
```gherkin
Given a root scope
When 16 goroutines each create and shut down 200 uniquely named child scopes
Then root.Children() is empty
And no goroutine is leaked
```

#### Scenario: A grandchild detaches from its own parent only
```gherkin
Given a root scope with child scope "a" and grandchild scope "a/b"
When the consumer calls b.Shutdown()
Then a.Children() is empty
And root.Children() still holds "a"
```

<!-- cases: 1 default: zero configuration -->
<!-- cases: N/A 2 overrides: detaching is not configurable (proposal Defaults & Control) -->
<!-- cases: N/A 3 zero values: no input -->
<!-- cases: 4 boundaries: 1 vs 1000 cycles; child and grandchild -->
<!-- cases: N/A 5 errors: a shutdown error still detaches the child (row with a deadline that may expire) -->
<!-- cases: 6 duplicates: the name of a detached child is reusable -->
<!-- cases: 7 lifecycle: detaching happens once shutdown completed; a parent shutdown removes children as before -->
<!-- cases: 8 concurrency: 16 goroutines x 200 scopes under -race, goleak -->
<!-- cases: N/A 9 panics: Scope("req") no longer panics with "already declared" after the first one shut down -->
<!-- cases: 10 compatibility: BEHAVIOR CHANGE (flagged) - ChildByName used to find a shut-down child, and reusing its name panicked -->

### Requirement: A shared eager value is shut down once across clones
An eager value that a root and its clones share SHALL have its `Shutdown` called exactly once. The first container to shut it down SHALL call it, and the others SHALL only remove their registration. Lazy services SHALL keep one instance per container, each shut down by its own container.

#### Scenario: A shared eager value is shut down once
```gherkin
Given a root scope with a value "pool" registered with doger.ProvideNamedValue, implementing Shutdown() error that counts its calls
And clone := root.Clone()
When the consumer calls <shutdowns>
Then "pool" had its Shutdown called 1 time
And both reports succeed

Examples:
  | shutdowns                                          |
  | root.Shutdown(), then clone.Shutdown()             |
  | clone.Shutdown(), then root.Shutdown()             |
  | root.Shutdown() and clone.Shutdown() concurrently  |
```

#### Scenario: Lazy services stay per container
```gherkin
Given a root scope with a lazy "svc" implementing Shutdown() error, counting its builds and its calls
And clone := root.Clone()
And "svc" was invoked from the root and from the clone
When the consumer calls root.Shutdown() and clone.Shutdown()
Then "svc" was built 2 times and had its Shutdown called 2 times
```

<!-- cases: 1 default: zero configuration -->
<!-- cases: N/A 2 overrides: no option -->
<!-- cases: N/A 3 zero values: an eager value that is no Shutdowner has nothing to shut down -->
<!-- cases: 4 boundaries: source first, clone first, both at once -->
<!-- cases: N/A 5 errors: the call that runs Shutdown reports its error; the others report none -->
<!-- cases: 6 duplicates: the double shutdown is the subject -->
<!-- cases: 7 lifecycle: clone and shutdown order -->
<!-- cases: 8 concurrency: concurrent shutdowns under -race -->
<!-- cases: N/A 9 panics: a panicking shared Shutdown is reported by the container that ran it (first requirement) -->
<!-- cases: 10 compatibility: BEHAVIOR CHANGE (flagged) - a shared eager value used to be shut down once per container -->

### Requirement: A re-entrant root shutdown with the shutdown context returns at once
When code running inside a root shutdown, such as a service's `Shutdown(ctx)`, calls `ShutdownWithContext` on the same root with the context it received or one derived from it, the call SHALL return at once a successful, empty report, and the outer shutdown SHALL complete normally. Any other call made during the shutdown SHALL wait for it, bounded by its own context.

#### Scenario: A re-entrant root shutdown returns at once
```gherkin
Given a root scope with a built lazy "supervisor" implementing Shutdown(ctx context.Context) error that calls <inner> and records the report and when it returned
And a built lazy "db" implementing Shutdown() error, counting its calls
When the consumer calls root.Shutdown()
Then the inner call <inner outcome>
And the outer report <outer outcome>
And "db" had its Shutdown called 1 time
And the HookContainerClosed hooks ran 1 time

Examples:
  | inner                                                                  | inner outcome                                                                     | outer outcome                                                     |
  | root.ShutdownWithContext(ctx)                                          | returned at once a report whose Err() is nil and which lists no services          | succeeds                                                          |
  | root.ShutdownWithContext(context.WithValue(ctx, k, "v"))               | returned at once a report whose Err() is nil and which lists no services          | succeeds                                                          |
  | root.ShutdownWithContext(context.WithTimeout(context.Background(), 50ms)) | returned after 50ms a failed report whose error matches context.DeadlineExceeded | fails only with the inner error, recorded for "supervisor"        |
```

<!-- cases: 1 default: the outer root.Shutdown() -->
<!-- cases: 2 overrides: the inner context given through, derived from, or unrelated to the shutdown context -->
<!-- cases: N/A 3 zero values: a nil context is context.Background() (unrelated: waits) -->
<!-- cases: 4 boundaries: the context received vs a derived one vs an unrelated one with a 50ms deadline (synctest) -->
<!-- cases: 5 errors: the unrelated bounded call fails with context.DeadlineExceeded, as before -->
<!-- cases: N/A 6 duplicates: root shutdown runs once (container-start) -->
<!-- cases: 7 lifecycle: re-entrancy is the subject; Shutdown() with no arguments drops the context and still waits on itself (documented in its godoc) -->
<!-- cases: 8 concurrency: the inner call runs on a shutdown goroutine; runs under -race -->
<!-- cases: N/A 9 panics: no panic path -->
<!-- cases: 10 compatibility: BEHAVIOR CHANGE (flagged) - the inner call with the shutdown context used to wait on itself until its context ended -->

### Requirement: ShutdownReport exposes its errors as Go errors
`ShutdownReport.Err()` SHALL return nil when the report has no error. Otherwise it SHALL return an error that matches every service error with `errors.Is` and `errors.As`. `ShutdownReport.Unwrap()` SHALL return the same errors as `[]error`: one `*ServiceError` per failed service, ordered by scope name, then service name. `Error()` SHALL return "" when there is no error.

#### Scenario: Err and Unwrap reflect the report
```gherkin
Given a root scope with child scope "tenant"
And built lazy services <services>
When the consumer calls root.Shutdown()
Then report.Err() is <err>
And report.Unwrap() is <unwrap>
And report.Error() is <text>

Examples:
  | services                                                                                      | err                                                     | unwrap                                                                                         | text                                                    |
  | "a" whose Shutdown() error returns nil                                                         | nil                                                     | empty                                                                                          | ""                                                      |
  | "a" in the root returning io.EOF                                                               | an error matching io.EOF                                | [*ServiceError{Scope: "[root]", Service: "a", Err: io.EOF}]                                    | starting "doger: shutdown errors:" and containing "a"  |
  | "b" in "tenant" returning io.EOF, and "a" in the root returning io.ErrClosedPipe               | an error matching io.EOF and io.ErrClosedPipe           | [*ServiceError for "[root]"/"a", *ServiceError for "tenant"/"b"]                               | containing both service names, "a" before "b"          |
  | "a" in the root whose Shutdown() panics with "boom"                                            | an error matching ErrShutdownPanic                      | [*ServiceError{Scope: "[root]", Service: "a"} matching ErrShutdownPanic]                       | containing "boom"                                       |
```

#### Scenario: errors.As finds the failing service
```gherkin
Given a root scope with a built lazy "db" whose Shutdown() error returns io.EOF
When the consumer calls errors.As(root.Shutdown().Err(), &serviceErr) with serviceErr of type *doger.ServiceError
Then it returns true
And serviceErr.Service is "db" and serviceErr.Scope is "[root]"
```

<!-- cases: 1 default: the successful report row -->
<!-- cases: N/A 2 overrides: no option -->
<!-- cases: 3 zero values: a report with no errors gives a nil Err(), an empty Unwrap() and "" -->
<!-- cases: 4 boundaries: 0, 1 and 2 errors across two scopes, with a deterministic order -->
<!-- cases: 5 errors: errors.Is per cause, errors.As to *ServiceError, a panic error -->
<!-- cases: N/A 6 duplicates: one error per service -->
<!-- cases: N/A 7 lifecycle: the report is produced by any shutdown -->
<!-- cases: N/A 8 concurrency: the report is read after Shutdown returned -->
<!-- cases: N/A 9 panics: no panic path; a panic error is one of the rows -->
<!-- cases: 10 compatibility: Error() is unchanged except the "doger: " prefix; Err and Unwrap are additions; callers comparing the report with nil must use report.Err() (documented) -->

### Requirement: ShutdownReport marshals to JSON
`json.Marshal` of a `ShutdownReport` SHALL succeed. It SHALL produce an object with `succeed` (bool), `shutdownTime` (the duration's `String()`), `services` (an array of `{"scope", "service"}`) and `errors` (an array of `{"scope", "service", "error"}`, with the error's message). Arrays SHALL be ordered by scope name, then service name.

#### Scenario: A report marshals to JSON
```gherkin
Given a root scope inside synctest.Test with built lazy services <services>
When the consumer calls json.Marshal(root.Shutdown())
Then the error is nil
And the JSON has "succeed": <succeed>
And the JSON "errors" array is <errors>
And the JSON "services" array lists <listed>

Examples:
  | services                                                       | succeed | errors                                                                      | listed                                             |
  | "a" whose Shutdown() returns nil                               | true    | []                                                                          | [{"scope":"[root]","service":"a"}]                 |
  | "b" returning nil, and "a" returning errors.New("closing failed") | false   | [{"scope":"[root]","service":"a","error":"closing failed"}]                 | [{"scope":"[root]","service":"a"},{"scope":"[root]","service":"b"}] |
```

<!-- cases: 1 default: the successful row -->
<!-- cases: N/A 2 overrides: no option; consumers needing another shape use Unwrap() -->
<!-- cases: 3 zero values: an empty errors array is [] (never null) -->
<!-- cases: 4 boundaries: 0 and 1 error; ordering of 2 services -->
<!-- cases: 5 errors: json.Marshal used to fail on the struct-keyed Errors map; the regression is row 2 -->
<!-- cases: N/A 6 duplicates, 7 lifecycle: no lifecycle input -->
<!-- cases: N/A 8 concurrency: marshalled after Shutdown returned -->
<!-- cases: N/A 9 panics: no panic path -->
<!-- cases: 10 compatibility: the Go struct fields are unchanged; only the JSON form is new (it used to fail) -->
