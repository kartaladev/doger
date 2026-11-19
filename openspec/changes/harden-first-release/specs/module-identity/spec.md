# Spec Delta

## MODIFIED Requirements

### Requirement: Service-not-found errors name library types with the doger import path
When a service of a library type cannot be found, the returned error SHALL match `ErrServiceNotFound` with `errors.Is`, SHALL start with the `doger: ` message prefix, and SHALL name the type with the import path `github.com/kartaladev/doger`.

#### Scenario: Not-found errors for library types
```gherkin
Given a root scope where "<registered>" is registered
When the consumer invokes the type "<requested>"
Then the error matches ErrServiceNotFound
And the error message is "<message>"

Examples:
  | registered     | requested           | message                                                                                                   |
  | nothing        | *doger.RootScope    | doger: could not find service `*github.com/kartaladev/doger.RootScope`, no service available               |
  | the int value 42 | doger.Healthchecker | doger: could not find service `github.com/kartaladev/doger.Healthchecker`, available services: `int`     |
```

<!-- cases: N/A 2 overrides: error text has no option -->
<!-- cases: 3 zero values: the "nothing registered" row is the empty scope -->
<!-- cases: 4 boundaries: zero and one available service are the two rows -->
<!-- cases: 5 errors: both rows assert errors.Is(err, ErrServiceNotFound) -->
<!-- cases: N/A 6 duplicate/override: unrelated to naming -->
<!-- cases: N/A 7 lifecycle: unrelated to naming -->
<!-- cases: N/A 8 concurrency: unrelated to naming -->
<!-- cases: N/A 9 panics: invoking a missing service returns an error, it does not panic -->
<!-- cases: 10 backward compatibility: BREAKING (flagged) the `DI: ` prefix becomes `doger: `; the sentence shape is unchanged -->

### Requirement: Compatibility identifiers stay unchanged across the rename
The rename SHALL NOT change the default struct-injection tag key `do` or the debug UI routes, which stay relative to the base path the consumer passes. The exported error sentinels SHALL use the `doger: ` prefix.

#### Scenario: Struct injection tag key keeps its default and override
```gherkin
Given a root scope created with tag key option "<option>"
And the value 7 registered as type int
When the consumer invokes a struct whose int field is tagged "<tag>"
Then the field value is <value>

Examples:
  | option  | tag         | value |
  | (unset) | do:""       | 7     |
  | inject  | inject:""   | 7     |
  | inject  | do:""       | 0     |
```

#### Scenario: Error sentinels keep their messages
```gherkin
Given the exported error sentinel "<sentinel>"
When its message is read
Then the message is "<message>"

Examples:
  | sentinel              | message                                             |
  | ErrServiceNotFound    | doger: could not find service                       |
  | ErrServiceNotMatch    | doger: could not find service satisfying interface  |
  | ErrCircularDependency | doger: circular dependency detected                 |
  | ErrHealthCheckTimeout | doger: health check timeout                         |
```

#### Scenario: Debug UI links stay relative to the consumer's base path
```gherkin
Given the debug UI base path "<base>"
When the index page is rendered
Then it links to "<base>/scope" and "<base>/service"

Examples:
  | base      |
  | /debug/do |
  | /admin/di |
```

<!-- cases: 1 default: the "(unset)" row is the zero-configuration default -->
<!-- cases: 2 overrides: the "inject" rows; the base path rows -->
<!-- cases: N/A 3 zero values: an empty tag key option means the default, already the "(unset)" row -->
<!-- cases: N/A 4 boundaries: no numeric limits -->
<!-- cases: 5 errors: the sentinel rows pin the exact messages consumers may match on -->
<!-- cases: N/A 6 not found: a missing injected service is an existing InvokeStruct error path, unchanged by the rename -->
<!-- cases: N/A 7 lifecycle: unrelated -->
<!-- cases: N/A 8 concurrency: unrelated -->
<!-- cases: N/A 9 panics: unrelated -->
<!-- cases: 10 backward compatibility: the tag key and routes are unchanged; the sentinel prefix changes from `DI: ` to `doger: ` (BREAKING, flagged; migration: match with errors.Is) -->

## ADDED Requirements

### Requirement: Every library message carries the doger prefix exactly once
Every error message, panic value message and error sentinel the library originates SHALL start with `doger: ` exactly once and SHALL NOT contain `DI: `, including messages that wrap another library error. A message that leads with a consumer's error, such as an abort outcome, SHALL keep that text first and unprefixed, and library text inside it SHALL NOT repeat the prefix.

#### Scenario: Messages that used to double the prefix
```gherkin
Given <setup>
When the consumer calls <call>
Then the error message is "<message>"

Examples:
  | setup                                                         | call                         | message                                                     |
  | a root scope that was shut down                               | root.Start(ctx)              | doger: cannot start: container is shut down                 |
  | a root scope with lazy "a" invoking "b" and "b" invoking "a"  | InvokeNamed[int](root, "a")  | doger: service "a" in scope "[root]": service "b" in scope "[root]": circular dependency detected: `a` -> `b` -> `a` |
  | a root scope with "db" registered and ProvideNamedValue(root, "db", 2) | recover() of the panic | doger: service `db` has already been declared       |
  | a root scope with child scope "tenant" and Scope("tenant") again | recover() of the panic    | doger: scope `tenant` has already been declared             |
  | a root scope whose Starter "a" returns errors.New("boom") while root.Shutdown(ctx) cancels root.Start(ctx) | root.Start(ctx) | boom (start aborted: container is shut down) |
  | a root scope with lazy "a" whose provider returns errors.New("boom") | InvokeNamed[int](root, "a") | doger: service "a" in scope "[root]": boom |
```

#### Scenario: No exported sentinel uses the DI prefix
```gherkin
Given every exported error variable of the packages doger, dogerhttp, dogergocron and dogerclockwork
When the test reads each message
Then each message starts with "doger: "
And no message contains "DI: "
And no message contains "doger: " twice
```

<!-- cases: 1 default: the prefix has no option -->
<!-- cases: N/A 2 overrides: none -->
<!-- cases: N/A 3 zero values: none -->
<!-- cases: 4 boundaries: one wrapping layer and two (nested ServiceError); a consumer-led abort outcome keeps the consumer text first -->
<!-- cases: 5 errors: sentinel and wrapped messages -->
<!-- cases: 6 duplicate: duplicate service and duplicate scope panics -->
<!-- cases: 7 lifecycle: Start after shutdown row (previously "DI: cannot start: DI: container is shut down") -->
<!-- cases: N/A 8 concurrency: unrelated -->
<!-- cases: 9 panics: panic value rows -->
<!-- cases: 10 backward compatibility: BREAKING (flagged) for consumers matching message text; migration: errors.Is / errors.As -->
