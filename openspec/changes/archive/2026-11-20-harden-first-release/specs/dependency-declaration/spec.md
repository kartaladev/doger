# Spec Delta

## MODIFIED Requirements

### Requirement: Dynamic dependency declaration
`DeclareDependency(i, dependent, dependency)` SHALL record `dependent -> dependency` without building either, resolving `dependent` from `i` and `dependency` from the dependent's scope or its ancestors. An unresolved name SHALL give `ErrServiceNotFound`. If `dependency` already reaches `dependent` through recorded or static `DependsOn` edges, it SHALL give `ErrCircularDependency`. Repeating an edge changes nothing. Such edges order start and shutdown, not construction. A nil `i`, including a typed nil, SHALL give `ErrNilInjector` and record nothing.

#### Scenario: Declaring edges dynamically
```gherkin
Given a root scope with lazy "scheduler", lazy "job-a", lazy "job-b" and lazy "job-s" registered with DependsOnNamed("scheduler"), none built
And the edge "job-b" -> "scheduler" recorded by invoking "job-b", whose provider invokes "scheduler"
And a child scope "tenant" with lazy "tenant-job"
And <given>
When the consumer calls DeclareDependency(<injector>, "<dependent>", "<dependency>")
Then the result is <result>

Examples:
  | given                                       | injector             | dependent  | dependency | result                                                 |
  | nothing else                                | root                 | scheduler  | job-a      | nil, ExplainNamedService lists job-a under scheduler   |
  | the edge scheduler -> job-a already declared | root                | scheduler  | job-a      | nil, scheduler still has exactly 1 dependency job-a    |
  | nothing else                                | root                 | scheduler  | missing    | ErrServiceNotFound                                     |
  | nothing else                                | root                 | missing    | job-a      | ErrServiceNotFound                                     |
  | nothing else                                | root                 | ""         | job-a      | ErrServiceNotFound                                     |
  | nothing else                                | root                 | scheduler  | job-b      | ErrCircularDependency                                  |
  | nothing else                                | root                 | job-a      | job-a      | ErrCircularDependency                                  |
  | nothing else                                | root                 | scheduler  | job-s      | ErrCircularDependency (static DependsOn, not built)    |
  | nothing else                                | tenant               | scheduler  | tenant-job | ErrServiceNotFound (not visible from scheduler's scope) |
  | nothing else                                | tenant               | tenant-job | scheduler  | nil, edge "tenant/tenant-job" -> "[root]/scheduler"    |
  | nothing else                                | nil                  | scheduler  | job-a      | ErrNilInjector, and the root records no edge           |
  | nothing else                                | (*doger.Scope)(nil)  | scheduler  | job-a      | ErrNilInjector, and the root records no edge           |
```

#### Scenario: Concurrent declarations are race-free
```gherkin
Given a root scope with lazy "scheduler" and 50 lazy services "job-0" to "job-49"
When 50 goroutines each call DeclareDependency(root, "scheduler", "job-<n>")
Then all 50 calls return nil
And ExplainNamedService(root, "scheduler") lists 50 dependencies
```

#### Scenario: Start rejects a cycle closed by a dynamic edge
```gherkin
Given a root scope with lazy Starter "scheduler" registered first with AsStarter()
And lazy Starter "job" registered second with AsStarter(), whose provider invokes "scheduler"
And DeclareDependency(root, "scheduler", "job") returned nil before Start, because nothing was built yet
When the consumer calls Start()
Then errors.Is(err, ErrCircularDependency) is true
And the error message contains "`scheduler`" and "`job`"
And no Starter was started
```

<!-- cases: N/A 2 overrides: no option -->
<!-- cases: 3 zero values: an empty name, a nil injector and a typed-nil injector are rows of "Declaring edges dynamically"; both injector rows give ErrNilInjector -->
<!-- cases: 5 errors: ErrServiceNotFound, ErrCircularDependency and ErrNilInjector rows, each matchable with errors.Is -->
<!-- cases: 7 lifecycle: cross-scope visibility rows; cycles closed only after build are rejected by Start ("Start rejects a cycle closed by a dynamic edge") -->
<!-- cases: N/A 9 panics: no panic paths; all failures are returned errors -->
<!-- cases: 10 compatibility: BREAKING (flagged) - a nil injector used to mean DefaultRootScope, which this change removes; migration: pass the container created with doger.New() -->

### Requirement: Explained edges report their source
Each `ExplainServiceDependencyOutput` SHALL carry `Source`: `DependencySourceInvoked` (a provider invoked the service), `DependencySourceDependsOn` (a `DependsOn` declaration) or `DependencySourceDeclared` (`DeclareDependency`). An edge recorded several ways SHALL report the first of invoked, depends-on, declared. `String()` SHALL append ` (depends-on)` or ` (declared)` to non-invoked edges only.

#### Scenario: Source per edge
```gherkin
Given a root scope with lazy "db", lazy "cache", lazy "job"
And lazy "worker" whose provider invokes "db", registered with DependsOnNamed("cache")
And "worker" has been invoked
And DeclareDependency(root, "worker", "job") returned nil
And <extra>
When the consumer calls ExplainNamedService(root, "worker")
Then the dependency sources are <sources>
And its String() output contains <marks>

Examples:
  | extra                                             | sources                                      | marks                                      |
  | nothing else                                      | cache: depends-on, db: invoked, job: declared | "cache (depends-on)", "job (declared)" and "db" without a suffix |
  | DeclareDependency(root, "worker", "db") returned nil | cache: depends-on, db: invoked, job: declared | "db" without a suffix                    |
```

#### Scenario: An edge both depended on and invoked
```gherkin
Given a root scope with lazy "queue"
And lazy "worker" registered with DependsOnNamed("queue"), whose provider invokes "queue"
And "worker" has been invoked
When the consumer calls ExplainNamedService(root, "worker")
Then the dependency on "queue" has Source DependencySourceInvoked
```

#### Scenario: Dependents report the same source
```gherkin
Given the setup of "Source per edge", first row
When the consumer calls ExplainNamedService(root, "job")
Then "job" lists dependent "worker" with Source DependencySourceDeclared
```

#### Scenario: Output without declared edges is unchanged
```gherkin
Given a root scope where lazy "worker"'s provider invokes lazy "db"
And "worker" has been invoked
When the consumer calls ExplainNamedService(root, "worker").String()
Then the output equals the output produced before this change
And the JSON encoding has field "source" with value "invoked" for "db"
```

<!-- cases: 1 default: invoked edges print as before -->
<!-- cases: N/A 2 overrides: the source is observation only -->
<!-- cases: 3 zero values: the zero DependencySource is invalid ("Dependency sources round-trip as text"); every recorded edge carries one of the three named sources -->
<!-- cases: N/A 4 boundaries: one edge per source -->
<!-- cases: N/A 5 errors: no error path -->
<!-- cases: 6 duplicates: an edge recorded as invoked and declared reports invoked -->
<!-- cases: 7 lifecycle: clones re-record DependsOn edges as depends-on and do not copy declared edges (existing requirement) -->
<!-- cases: N/A 8 concurrency: covered by "Concurrent declarations are race-free" -->
<!-- cases: N/A 9 panics: none -->
<!-- cases: 10 compatibility: String() of invoked-only graphs is unchanged; BREAKING (flagged): a struct literal relying on the zero Source meaning invoked must now set DependencySourceInvoked -->

### Requirement: Dependency sources round-trip as text
`DependencySource` SHALL encode as and decode from the texts `invoked`, `depends-on` and `declared`, so explained output round-trips through JSON. Any other text SHALL be a decoding error. Its constants SHALL start at 1: the zero value is invalid, its `String()` SHALL be `unknown`, and `MarshalText` SHALL fail for it and for any other value outside the three constants.

#### Scenario: Explained output round-trips through JSON
```gherkin
Given the JSON object {"service": "db", "source": <text>}
When the consumer decodes it into an ExplainServiceDependencyOutput
Then the result is <result>

Examples:
  | text         | result                                                           |
  | "invoked"    | Source DependencySourceInvoked, nil error                        |
  | "depends-on" | Source DependencySourceDependsOn, nil error                      |
  | "declared"   | Source DependencySourceDeclared, nil error                       |
  | "other"      | an error naming "other"                                          |
  | "unknown"    | an error naming "unknown"                                        |
  | ""           | an error containing "doger: unknown dependency source"           |
```

#### Scenario: Encoding dependency sources
```gherkin
Given the DependencySource value <source>
When the consumer calls String() and MarshalText()
Then String() returns "<string>"
And MarshalText returns <marshal>

Examples:
  | source                          | string     | marshal                                                   |
  | doger.DependencySourceInvoked   | invoked    | "invoked", nil error                                      |
  | doger.DependencySourceDependsOn | depends-on | "depends-on", nil error                                   |
  | doger.DependencySourceDeclared  | declared   | "declared", nil error                                     |
  | doger.DependencySource(0)       | unknown    | an error containing "doger: invalid dependency source 0"  |
  | doger.DependencySource(9)       | unknown    | an error containing "doger: invalid dependency source 9"  |
```

<!-- cases: 1 default: the three texts -->
<!-- cases: N/A 2 overrides: the texts are fixed -->
<!-- cases: 3 zero values: DependencySource(0) and the empty text are rejected -->
<!-- cases: 4 boundaries: 0 below the first constant and 9 past the last -->
<!-- cases: 5 errors: unknown texts and invalid values -->
<!-- cases: N/A 6 duplicates, 7 lifecycle, 8 concurrency, 9 panics: a pure value conversion -->
<!-- cases: 10 compatibility: BREAKING (flagged) - the numeric values change from 0/1/2 to 1/2/3 and the zero value no longer encodes as "invoked"; JSON produced by the library is unchanged -->
