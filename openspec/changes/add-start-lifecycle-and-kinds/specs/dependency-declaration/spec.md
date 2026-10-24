# Spec Delta

## Purpose

Lets a consumer or a component-kind handler declare a dependency between services that do not invoke each other, so construction, start and shutdown all follow one deterministic dependency order.

## ADDED Requirements

### Requirement: Static declared dependencies are built first
When a registration carries `DependsOn[T]()` or `DependsOnNamed(name)`, the container SHALL build each declared dependency, in declaration order, before calling the dependent's provider. It SHALL record a dependency edge for each, exactly as if the provider had invoked it. This SHALL hold for `Invoke` and for `Start`.

#### Scenario: Declared dependency built before the dependent
```gherkin
Given a root scope recording provider calls
And lazy "db" and lazy "cache"
And lazy "worker" registered with <options> whose provider invokes nothing
When the consumer invokes "worker"
Then the provider call order is <order>
And ExplainService("worker") lists dependencies <deps>

Examples:
  | options                                      | order                 | deps          |
  | DependsOnNamed("db")                         | db, worker            | db            |
  | DependsOnNamed("cache"), DependsOnNamed("db") | cache, db, worker    | cache, db     |
  | none                                         | worker                | none          |
```

#### Scenario: Declared dependency on an eager service
```gherkin
Given a root scope with eager value "cfg"
And lazy "worker" registered with DependsOnNamed("cfg")
When the consumer invokes "worker"
Then ExplainService("worker") lists dependency "cfg"
And ExplainService("cfg") lists dependent "worker"
```

#### Scenario: Declared dependency resolved through scope ancestry
```gherkin
Given a root scope with lazy "db"
And a child scope "request" with lazy "handler" registered with DependsOnNamed("db")
When the consumer invokes "handler" from scope "request"
Then "db" is built before "handler"
And the recorded edge goes from "request/handler" to "[root]/db"
```

#### Scenario: Concurrent invocation builds declared dependencies once
```gherkin
Given a root scope with lazy "db" whose provider counts its calls
And lazy "worker" registered with DependsOnNamed("db")
When 100 goroutines invoke "worker" concurrently
Then the "db" provider was called exactly 1 time
And the "worker" provider was called exactly 1 time
```

<!-- cases: N/A 2 overrides: declaring is itself the override of implicit (invocation-only) ordering -->
<!-- cases: 3 zero values: DependsOnNamed("") is covered by "Declared dependency errors" -->
<!-- cases: N/A 4 boundaries: 0, 1 and many dependencies are covered by the Examples rows -->
<!-- cases: N/A 9 panics: a declared dependency whose provider panics surfaces as the existing provider-panic error; no new path -->
<!-- cases: 10 compatibility: the "none" row pins unchanged behavior for registrations without DependsOn -->

### Requirement: Declared dependency errors
Building a service whose declared dependency is not registered in its scope or any ancestor SHALL fail with an error matching `ErrServiceNotFound`, naming both services. A declared dependency that closes a cycle SHALL fail with an error matching `ErrCircularDependency` that names the chain. Neither SHALL call the dependent's provider.

#### Scenario: Missing or circular declared dependency
```gherkin
Given a root scope with registrations <setup>
When the consumer invokes "<invoked>"
Then the error matches <error>
And the error message contains "<message>"
And the provider of "<invoked>" was not called

Examples:
  | setup                                                                 | invoked | error                 | message                         |
  | lazy "worker" DependsOnNamed("db")                                    | worker  | ErrServiceNotFound    | `db`                            |
  | lazy "worker" DependsOnNamed("")                                      | worker  | ErrServiceNotFound    | ``                              |
  | lazy "a" DependsOnNamed("b"); lazy "b" DependsOnNamed("a")            | a       | ErrCircularDependency | `a` -> `b` -> `a`               |
  | lazy "a" DependsOnNamed("a")                                          | a       | ErrCircularDependency | `a` -> `a`                      |
  | lazy "a" DependsOnNamed("b"); lazy "b" whose provider invokes "a"     | a       | ErrCircularDependency | `a` -> `b` -> `a`               |
```

<!-- cases: N/A 2 overrides: errors cannot be suppressed -->
<!-- cases: N/A 7 lifecycle: covered via Start in container-start ("Start fails on a declared-dependency error") -->
<!-- cases: N/A 8 concurrency: error paths share the build path covered above -->
<!-- cases: N/A 9 panics: errors are returned, not raised -->
<!-- cases: N/A 10 compatibility: new API -->

### Requirement: Dynamic dependency declaration
`DeclareDependency(i, dependent, dependency)` SHALL record an edge `dependent -> dependency` between two services visible from `i`, without building either. It SHALL return an error matching `ErrServiceNotFound` if either name is not visible, and `ErrCircularDependency` if `dependency` already depends, directly or transitively, on `dependent`. Declaring an existing edge again SHALL succeed and change nothing.

#### Scenario: Declaring edges dynamically
```gherkin
Given a root scope with lazy "scheduler", lazy "job-a" and lazy "job-b"
And the existing edge "job-b" -> "scheduler"
When the consumer calls DeclareDependency(root, "<dependent>", "<dependency>")
Then the result is <result>

Examples:
  | dependent | dependency | result                       |
  | scheduler | job-a      | nil, edge recorded           |
  | scheduler | job-a      | nil (second call), no change |
  | scheduler | missing    | ErrServiceNotFound           |
  | missing   | job-a      | ErrServiceNotFound           |
  | ""        | job-a      | ErrServiceNotFound           |
  | scheduler | job-b      | ErrCircularDependency        |
  | job-a     | job-a      | ErrCircularDependency        |
```

#### Scenario: Concurrent declarations are race-free
```gherkin
Given a root scope with lazy "scheduler" and 50 lazy services "job-0" to "job-49"
When 50 goroutines each call DeclareDependency(root, "scheduler", "job-<n>")
Then all 50 calls return nil
And ExplainService("scheduler") lists 50 dependencies
```

<!-- cases: N/A 2 overrides: no option -->
<!-- cases: 3 zero values: a nil injector uses the default root scope, as every package-level function does; covered by a table row in design.md test strategy -->
<!-- cases: N/A 9 panics: no panic paths; all failures are returned errors -->
<!-- cases: N/A 10 compatibility: new API -->

### Requirement: Declared edges order shutdown
Shutdown SHALL treat declared edges, static and dynamic, exactly like invoked dependencies: a dependent is shut down before the services it depends on.

#### Scenario: Scheduler is shut down before the jobs it drives
```gherkin
Given a root scope with built lazy "scheduler" and built lazy "job" recording shutdown order
And <edge>
When the consumer calls Shutdown()
Then the shutdown order is <order>

Examples:
  | edge                                              | order          |
  | DeclareDependency(root, "scheduler", "job")       | scheduler, job |
  | "scheduler" registered with DependsOnNamed("job") | scheduler, job |
```

<!-- cases: N/A 2 overrides: ordering is defined by edges only -->
<!-- cases: N/A 3 zero values, 4 boundaries: the no-edge case is the existing parallel-shutdown behavior -->
<!-- cases: N/A 5 errors: shutdown errors are reported as today -->
<!-- cases: N/A 9 panics: unchanged -->
<!-- cases: 10 compatibility: services without declared edges shut down exactly as before -->

### Requirement: Deterministic ordering ties
Wherever this change orders services that have no dependency path between them (discovery, start, kind handling), the container SHALL order them by scope depth (root first), then registration index.

#### Scenario: Ties broken by scope depth then registration order
```gherkin
Given a root scope and child scope "request"
And Starters registered in order: "z" in "request", "b" in root, "a" in root, with no dependencies between them
When the consumer calls Start()
Then the start order is b, a, z
```

<!-- cases: N/A 2 overrides: reorder registrations or declare edges to change ties -->
<!-- cases: N/A 3 zero values, 5 errors, 9 panics: ordering of an empty set is empty; no failure modes -->
<!-- cases: 8 concurrency: the order is identical across 100 repeated runs; covered by a repeat test in design.md -->
<!-- cases: N/A 10 compatibility: existing APIs did not promise an order -->
