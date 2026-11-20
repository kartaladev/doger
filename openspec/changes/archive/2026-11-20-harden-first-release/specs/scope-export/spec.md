# Spec Delta

## MODIFIED Requirements

### Requirement: Export publishes a child-scope service in the parent
`Export[T](from, opts...)` SHALL register in `from`'s parent a service that resolves the service named `NameOf[T]()` from `from`; `ExportNamed[T](from, name, opts...)` resolves `name`. The published name SHALL be the source name unless `WithExportName(name)` is given. Invoking it SHALL return the instance owned by `from`.

#### Scenario: Export publishes under the source or a chosen name
```gherkin
Given a root scope with child scope "redis"
And a lazy "*app.Client" registered in scope "redis", producing "client-1"
And the consumer called <export>
When the consumer invokes <published> from the root
Then the result is "client-1"
And it is the same instance as invoking "*app.Client" from scope "redis"
And the provider was called 1 time

Examples:
  | export                                                                          | published       |
  | doger.Export[*app.Client](redis)                                                | "*app.Client"   |
  | doger.Export[*app.Client](redis, doger.WithExportName("cache-client"))          | "cache-client"  |
  | doger.ExportNamed[*app.Client](redis, "*app.Client", doger.WithExportName("c")) | "c"             |
```

#### Scenario: Exported services stay private otherwise
```gherkin
Given a root scope with child scope "redis"
And lazy "*app.Client" and lazy "*app.Pool" registered in scope "redis"
And the consumer called doger.Export[*app.Client](redis)
When the consumer invokes "*app.Pool" from the root
Then the error matches ErrServiceNotFound
```

#### Scenario: Export from a grandchild through two scopes
```gherkin
Given a root scope with child scope "a" and grandchild scope "a/b"
And a lazy "*app.Client" registered in scope "a/b", producing "client-1"
And the consumer called doger.Export[*app.Client](b), then doger.Export[*app.Client](a)
When the consumer invokes "*app.Client" from the root
Then the result is "client-1"
```

#### Scenario: ExportAs is renamed WithExportName
```gherkin
Given the exported API of package doger, listed with go doc -short
When the test reads the listing
Then it contains "func WithExportName(name string) ExportOption"
And it contains no "func ExportAs("
```

<!-- cases: 2 overrides: WithExportName is the only option; rows 2 and 3 -->
<!-- cases: 3 zero values: covered by "Export validates its inputs" (nil scope, empty name) -->
<!-- cases: 4 boundaries: one export, a chained export across two scopes -->
<!-- cases: 5 errors: covered by "Export validates its inputs" -->
<!-- cases: 6 duplicate: covered by "Export validates its inputs" -->
<!-- cases: 7 lifecycle: covered by the lifecycle, shutdown and clone requirements -->
<!-- cases: 8 concurrency: covered by "Concurrent first invocations of an export" -->
<!-- cases: N/A 9 panics: covered by "Export validates its inputs" (Must variants) -->
<!-- cases: 10 compatibility: BREAKING (flagged) - ExportAs(name) is renamed WithExportName(name); migration: rename the call -->

### Requirement: Export validates its inputs
`Export` and `ExportNamed` SHALL return an error and register nothing when `from` is nil or has no parent, the source is not registered in `from` itself or its type is not assignable to `T`, or the published name is empty or taken in the parent. A name held by an unresolved default is not taken: the export replaces it. `MustExport` and `MustExportNamed` SHALL panic with the error.

#### Scenario: Invalid exports are rejected
```gherkin
Given a root scope with child scope "redis"
And a lazy "*app.Client" registered in scope "redis"
And a lazy "*app.Other" registered in the root
When the consumer calls <call>
Then it returns an error <matching> and containing "<message>"
And the root has no service named <name>

Examples:
  | call                                                                    | matching                            | message                                  | name           |
  | doger.Export[*app.Client](nil)                                          | matching ErrNilInjector             | doger: cannot export from a nil scope    | "*app.Client"  |
  | doger.Export[*app.Client](<the root's own *Scope>)                      | (no sentinel)                       | has no parent scope to export to         | "*app.Client"  |
  | doger.Export[*app.Missing](redis)                                       | matching ErrServiceNotFound         | *app.Missing                             | "*app.Missing" |
  | doger.ExportNamed[*app.Other](redis, "*app.Client")                     | matching ErrServiceTypeMismatch     | *app.Client                              | "*app.Client"  |
  | doger.Export[*app.Client](redis, doger.WithExportName(""))              | (no sentinel)                       | doger: export name cannot be empty       | ""             |
  | doger.Export[*app.Client](redis, doger.WithExportName("*app.Other"))    | matching ErrServiceAlreadyDeclared  | *app.Other                               | (unchanged)    |
```

#### Scenario: A service visible only through an ancestor cannot be exported
```gherkin
Given a root scope with child scope "a" and grandchild scope "a/b"
And a lazy "*app.Client" registered in scope "a"
When the consumer calls doger.Export[*app.Client](b)
Then it returns an error matching ErrServiceNotFound and containing "*app.Client"
```

#### Scenario: Must variants panic with the error
```gherkin
Given a root scope with child scope "redis" and no service registered
When the consumer calls <call>
Then it panics with an error matching ErrServiceNotFound and containing "*app.Client"

Examples:
  | call                                                   |
  | doger.MustExport[*app.Client](redis)                   |
  | doger.MustExportNamed[*app.Client](redis, "*app.Client") |
```

<!-- cases: N/A 1 default: covered by "Export publishes a child-scope service in the parent" -->
<!-- cases: N/A 2 overrides: Override* on the published name replaces the export as for any service -->
<!-- cases: 3 zero values: nil scope, empty name rows -->
<!-- cases: 4 boundaries: source in the scope itself vs only in an ancestor -->
<!-- cases: 5 errors: every row; the not-found, type-mismatch, duplicate and nil-scope errors match the resolution-errors sentinels; the no-parent and empty-name errors are plain errors -->
<!-- cases: 6 duplicate: last row; exporting the same service twice under one name hits the same row -->
<!-- cases: N/A 7 lifecycle: registration-time validation only; exporting from or into a closed scope is container-shutdown's "A closed scope stays closed" -->
<!-- cases: N/A 8 concurrency: registration-time validation only -->
<!-- cases: 9 panics: Must variants -->
<!-- cases: 10 compatibility: BEHAVIOR CHANGE (flagged) - messages use the "doger: " prefix and four errors now match sentinels; ExportAs is renamed WithExportName -->

### Requirement: An export resolves from its source scope only
An export SHALL always resolve its source name from the scope it was exported from, whatever scope it is invoked from, and SHALL be listed with service type `ServiceTypeAlias`. A same-name service registered in the parent or elsewhere SHALL NOT change what the export resolves to.

#### Scenario: Invocation scope does not change the target
```gherkin
Given a root scope with child scopes "redis" and "app"
And a lazy "*app.Client" registered in scope "redis", producing "redis-client"
And a lazy "*app.Client" registered in scope "app", producing "app-client"
And the consumer called doger.Export[*app.Client](redis, doger.WithExportName("shared"))
When the consumer invokes "shared" from scope "<from>"
Then the result is "redis-client"

Examples:
  | from  |
  | root  |
  | app   |
  | redis |
```

#### Scenario: An export is listed as an alias
```gherkin
Given a root scope with child scope "redis" exporting "*app.Client"
And "*app.Client" was invoked once from the root
When the consumer calls ListProvidedServices on the root
Then the root lists "*app.Client"
And doger.ExplainNamedService(root, "*app.Client") reports ServiceTypeAlias and a dependency on "*app.Client" in scope "redis"
```

#### Scenario: A same-name grandchild does not capture the export
```gherkin
Given a root scope with child scopes "redis" and "app", and grandchild scope "app/redis"
And a lazy "*app.Client" in scope "redis" producing "direct", and a lazy "*app.Client" in scope "app/redis" producing "nested"
And the consumer called doger.Export[*app.Client](redis)
When the consumer invokes "*app.Client" from the root, in 100 new containers built the same way
Then every result is "direct"
```

#### Scenario: An export replaces an unresolved default in the parent
```gherkin
Given a root scope with child scope "redis"
And a lazy "*app.Client" registered in the root with doger.AsDefault(), producing "root-default", never invoked
And a lazy "*app.Client" registered in scope "redis" without options, producing "redis-client"
When the consumer calls doger.Export[*app.Client](redis)
Then it returns nil
And invoking "*app.Client" from the root returns "redis-client"
```

<!-- cases: N/A 2 overrides: no option changes resolution -->
<!-- cases: N/A 3 zero values, 5 invalid input: covered by "Export validates its inputs" -->
<!-- cases: 4 boundaries: invoked from parent, sibling and the source scope itself -->
<!-- cases: N/A 6 duplicate: covered by "Export validates its inputs" -->
<!-- cases: 7 lifecycle: covered by "An export has no lifecycle of its own" -->
<!-- cases: N/A 8 concurrency: covered by "Concurrent first invocations of an export" -->
<!-- cases: N/A 9 panics: resolution errors are returned, as for aliases -->
<!-- cases: 10 compatibility: unchanged apart from the WithExportName rename -->

### Requirement: An export has no lifecycle of its own
An export SHALL NOT be started, run, handled by a component kind or health-checked on its own. The exported service SHALL be started, handled and shut down exactly once, by the scope that owns it. A health check of the export SHALL run the exported service's health check, except from the source scope or its descendants, where the export is skipped.

#### Scenario: Exported Starter and Shutdowner run once
```gherkin
Given a root scope with child scope "redis"
And a lazy "*app.Pool" registered in scope "redis" with doger.AsStarter(), implementing Start(ctx) error, Shutdown() error and HealthCheck() error, each counting calls
And the consumer called doger.Export[*app.Pool](redis)
And a lazy "*app.App" in the root that invokes "*app.Pool"
And "*app.App" was invoked from the root
And <before>
When the consumer calls <operation>
Then the pool's <method> was called <count> times

Examples:
  | before                      | operation          | method      | count |
  | nothing else happened       | root.Start()       | Start       | 1     |
  | root.Start() was called     | root.Shutdown()    | Shutdown    | 1     |
  | nothing else happened       | root.HealthCheck() | HealthCheck | 1     |
```

#### Scenario: Root health check reports the exported service
```gherkin
Given a root scope with child scope "redis"
And a lazy "*app.Pool" registered in scope "redis" whose HealthCheck() returns errors.New("pool down"), invoked once
And the consumer called doger.Export[*app.Pool](redis)
When the consumer calls HealthCheck on the root
Then the result has key "*app.Pool" with an error containing "pool down"
```

#### Scenario: A health check from the source scope checks the exported service once
```gherkin
Given a root scope with child scope "redis"
And a lazy "*app.Pool" registered in scope "redis" <source registered>, whose HealthCheck() counts calls and returns errors.New("pool down"), invoked once
And the consumer called doger.Export[*app.Pool](redis, doger.WithExportName("pool"))
When the consumer calls HealthCheck on scope "redis"
Then the result has key "*app.Pool" with an error containing "pool down"
And the result has no key "pool"
And the pool's HealthCheck was called 1 time

Examples:
  | source registered        |
  | without options          |
  | with doger.AsDefault()   |
```

<!-- cases: N/A 2 overrides: lifecycle ownership is not configurable; to make the parent own a service, register it in the parent -->
<!-- cases: N/A 3 zero values, 5 invalid input: no input -->
<!-- cases: 4 boundaries: health check from the parent and from the source scope -->
<!-- cases: 6 duplicate: the source-scope scenario pins one call and one key -->
<!-- cases: 7 lifecycle: start, shutdown and health check rows; shutdown order is in dependency-declaration -->
<!-- cases: N/A 8 concurrency: existing parallel start and shutdown apply unchanged -->
<!-- cases: N/A 9 panics: a panicking Start follows container-start; a panicking Shutdown is reported (container-shutdown) -->
<!-- cases: 10 compatibility: the pool opts in with AsStarter() because Starter auto-detection is opt-in (container-start); WithExportName replaces ExportAs -->

### Requirement: Collections and type selection see exports in the parent
In the parent and its other descendants, an export SHALL be a member of collections and a candidate of by-type selection, resolving to the exported instance. Read from the exporting scope or its descendants, the export SHALL NOT be a collection member, so the exported service is not counted twice. A provider's own export SHALL NOT be a by-type candidate for that provider, as for an `Alias` of it.

#### Scenario: Collection membership of an export
```gherkin
Given a root scope with child scopes "redis" and "app"
And a lazy "*app.Pool" in scope "redis" registered <source registered>, implementing HealthCheck() error
And the consumer called doger.Export[*app.Pool](redis, doger.WithExportName("pool"))
When the consumer calls doger.InvokeAllMap[doger.Healthchecker] from scope "<from>"
Then the keys are <keys>

Examples:
  | source registered                 | from  | keys                |
  | without options                   | root  | "pool"              |
  | without options                   | app   | "pool"              |
  | without options                   | redis | "*app.Pool"         |
  | with doger.AsDefault()            | root  | "pool"              |
  | with doger.AsDefault()            | redis | "*app.Pool"         |
```

#### Scenario: InvokeAs in the parent finds the export
```gherkin
Given a root scope with child scope "redis"
And a lazy "*app.Pool" in scope "redis" implementing HealthCheck() error
And the consumer called doger.Export[*app.Pool](redis)
When the consumer calls doger.InvokeAs[doger.Healthchecker] from the root
Then the result is the pool instance owned by scope "redis"
```

#### Scenario: A provider does not select its own export by type
```gherkin
Given a root scope with child scope "a" and grandchild scope "c" of "a"
And a lazy "dec" of type "*app.Decorator" in scope <scope> implementing "API", whose provider calls doger.InvokeAs[API] and wraps the result as "dec(<inner>)"
And the consumer called <exports>
And a lazy "base" of type "*app.Base" registered in the root afterwards, implementing "API" as "base"
When the consumer invokes "dec" from scope <scope>
Then the result is "dec(base)"

Examples:
  | scope | exports                                                                                                  |
  | "a"   | doger.ExportNamed[API](a, "dec", doger.WithExportName("api"))                                            |
  | "c"   | doger.ExportNamed[API](c, "dec"), then doger.ExportNamed[API](a, "dec", doger.WithExportName("api"))     |
```

<!-- cases: N/A 2 overrides: membership has no option; the export's position is its own registration in the parent, so WithOrder on the source does not move it -->
<!-- cases: 3 zero values: no export means no member (existing "No match yields an empty collection") -->
<!-- cases: 4 boundaries: parent, sibling, exporting scope rows; a provider and its own renamed export, directly and through a chain of exports -->
<!-- cases: N/A 5 invalid input: unchanged collection errors -->
<!-- cases: N/A 6 duplicate: the exporting-scope row pins no double count -->
<!-- cases: N/A 7 lifecycle, 8 concurrency, 9 panics: unchanged collection behavior -->
<!-- cases: 10 compatibility: aliases created with Alias/Bind stay excluded from collections; WithExportName replaces ExportAs -->
