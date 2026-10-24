# Spec Delta

## Purpose

Lets a consumer resolve every visible service assignable to a type in one call, as a slice or a name-keyed map, with predictable visibility, error and dependency semantics.

## ADDED Requirements

### Requirement: Collect every visible service assignable to a type
`InvokeAll[T]` SHALL return one element per visible, non-alias service whose registered type is assignable to `T`, building lazy members as needed. It SHALL NOT build services whose registered type is not assignable to `T`.

#### Scenario: Membership by registered type
```gherkin
Given a root scope
And a lazy service "console" registered as type "*ConsoleHandler" which implements "EventHandler"
And a lazy service "audit" registered as type "*AuditHandler" which implements "EventHandler"
And a lazy service "db" registered as type "*sql.DB" whose provider counts its calls
When the consumer calls InvokeAll for type "<T>"
Then the result contains the services <members>
And the error is nil
And the provider of "db" was called <dbCalls> times

Examples:
  | T             | members              | dbCalls |
  | EventHandler  | "console", "audit"   | 0       |
  | *AuditHandler | "audit"              | 0       |
  | *sql.DB       | "db"                 | 1       |
  | io.Closer     | none                 | 0       |
```

#### Scenario: Eager values are members without a provider call
```gherkin
Given a root scope
And an eager value "static" of type "*StaticHandler" which implements "EventHandler"
And a lazy service "console" registered as type "*ConsoleHandler" which implements "EventHandler"
When the consumer calls InvokeAll for type "EventHandler"
Then the result contains "static" and "console"
And the error is nil
```

#### Scenario: Interface type any matches every non-alias service
```gherkin
Given a root scope with lazy services "a" of type "int" and "b" of type "string"
And an alias of "a" registered as type "any"
When the consumer calls InvokeAll for type "any"
Then the result contains exactly "a" and "b"
And both providers were called exactly 1 time
```

#### Scenario: Nil injector uses the default root scope
```gherkin
Given the default root scope has a lazy service "console" of type "*ConsoleHandler" implementing "EventHandler"
When the consumer calls InvokeAll for type "EventHandler" with a nil injector
Then the result contains "console"
And the error is nil
```

#### Scenario: Typed-nil member is returned as is
```gherkin
Given a root scope
And a lazy service "noop" of type "*NoopHandler" whose provider returns a nil "*NoopHandler" and no error
When the consumer calls InvokeAll for type "EventHandler"
Then the result has 1 element
And that element is a nil "*NoopHandler" wrapped in "EventHandler"
And the error is nil
```

<!-- cases: N/A 2 overrides: membership has no option; ordering options are covered by collection-ordering -->
<!-- cases: N/A 9 panics: provider panics are covered by "A failing member fails the whole call" -->

### Requirement: Visibility follows scope ancestry with nearest-name shadowing
A collection invoked from a scope SHALL include services of that scope and its ancestors only. When the same service name exists in several visible scopes, only the nearest one SHALL be considered, even if the nearest one is not assignable to `T`.

#### Scenario: Scope visibility and shadowing
```gherkin
Given a root scope with a lazy service "<rootName>" of type "<rootType>"
And a child scope "child" of root with a lazy service "<childName>" of type "<childType>"
And a sibling scope "sibling" of root with a lazy service "s" of type "*SiblingHandler"
When the consumer calls InvokeAll for type "EventHandler" from scope "<from>"
Then the result contains the services <members>

Examples:
  | rootName | rootType        | childName | childType       | from    | members                  |
  | a        | *AHandler       | b         | *BHandler       | child   | root "a", child "b"      |
  | a        | *AHandler       | b         | *BHandler       | root    | root "a"                 |
  | x        | *AHandler       | x         | *BHandler       | child   | child "x" only           |
  | x        | *AHandler       | x         | *sql.DB         | child   | none                     |
  | x        | *AHandler       | x         | *BHandler       | sibling | root "x", sibling "s"    |
```

<!-- cases: N/A 2 overrides: visibility is not configurable -->
<!-- cases: N/A 5 errors: visibility cannot fail -->
<!-- cases: N/A 9 panics: no panicking path -->

### Requirement: Aliases are excluded from collections
Services registered through `As`/`Bind` aliases SHALL NOT appear in a collection. The aliased original SHALL represent itself when its registered type is assignable to `T`.

#### Scenario: Alias does not duplicate its target
```gherkin
Given a root scope
And a lazy service "*AuditHandler" of type "*AuditHandler" implementing "EventHandler"
And an alias of "*AuditHandler" registered as type "EventHandler"
When the consumer calls InvokeAll for type "EventHandler"
Then the result has exactly 1 element
And that element is the instance of "*AuditHandler"
```

#### Scenario: Alias whose target is not assignable is still excluded
```gherkin
Given a root scope
And a lazy service "impl" of type "*Impl" that does not implement "EventHandler"
And an alias of "impl" registered as type "EventHandler" whose instance is the same "*Impl"
When the consumer calls InvokeAll for type "EventHandler"
Then the result is empty
And the error is nil
```

<!-- cases: N/A 2 overrides: alias exclusion is not configurable; register the instance directly to include it -->
<!-- cases: N/A 4 boundaries: covered by the 1-element scenario above -->
<!-- cases: N/A 9 panics: no panicking path -->

### Requirement: No match yields an empty collection
When no visible service matches, `InvokeAll[T]` SHALL return an empty, non-nil slice and `InvokeAllNamed[T]` SHALL return an empty, non-nil map. Both SHALL return a nil error, never `ErrServiceNotFound` or `ErrServiceNotMatch`.

#### Scenario: Empty results
```gherkin
Given a root scope containing <services>
When the consumer calls "<function>" for type "EventHandler"
Then the result is non-nil with length 0
And the error is nil

Examples:
  | services                     | function       |
  | no services                  | InvokeAll      |
  | no services                  | InvokeAllNamed |
  | only "db" of type "*sql.DB"  | InvokeAll      |
  | only "db" of type "*sql.DB"  | InvokeAllNamed |
```

<!-- cases: N/A 2 overrides: no option -->
<!-- cases: N/A 7 lifecycle: covered by "Collections after shutdown" -->
<!-- cases: N/A 8 concurrency: covered by "Concurrent collection callers" -->
<!-- cases: N/A 9 panics: no panicking path -->

### Requirement: A failing member fails the whole call
When any member fails to build, the call SHALL return no collection and an error that names the failing member and wraps its cause, so `errors.Is` matches the cause. Members already built stay built.

#### Scenario: Member failure modes
```gherkin
Given a root scope
And a lazy service "console" of type "*ConsoleHandler" implementing "EventHandler"
And a lazy service "audit" of type "*AuditHandler" implementing "EventHandler" whose provider <behavior>
When the consumer calls InvokeAll for type "EventHandler"
Then the result is nil
And the error message contains "audit"
And errors.Is(err, <cause>) is true

Examples:
  | behavior                                   | cause                 |
  | returns error ErrAuditDown                 | ErrAuditDown          |
  | panics with error ErrAuditPanic            | ErrAuditPanic         |
  | invokes "missing" which is not registered  | ErrServiceNotFound    |
```

#### Scenario: Collection invoked from inside a member's own provider
```gherkin
Given a root scope
And a lazy service "bus" of type "*Bus" implementing "EventHandler"
And the provider of "bus" calls InvokeAll for type "EventHandler"
When the consumer invokes "bus"
Then the error matches ErrCircularDependency
And the error message contains "`bus` -> `bus`"
```

#### Scenario: Must variants panic with the same error
```gherkin
Given a root scope
And a lazy service "audit" of type "*AuditHandler" implementing "EventHandler" whose provider returns error ErrAuditDown
When the consumer calls "<function>" for type "EventHandler"
Then the call panics with an error for which errors.Is(err, ErrAuditDown) is true

Examples:
  | function           |
  | MustInvokeAll      |
  | MustInvokeAllNamed |
```

#### Scenario: Must variants return the collection on success
```gherkin
Given a root scope with lazy services "console" and "audit" implementing "EventHandler"
When the consumer calls "<function>" for type "EventHandler"
Then it does not panic
And the result has 2 elements

Examples:
  | function           |
  | MustInvokeAll      |
  | MustInvokeAllNamed |
```

<!-- cases: N/A 2 overrides: failure semantics are not configurable; the caller can resolve members individually -->
<!-- cases: N/A 3 zero values: covered by "No match yields an empty collection" -->

### Requirement: Name-keyed collection
`InvokeAllNamed[T]` SHALL return the same members as `InvokeAll[T]`, keyed by service name, under the same visibility, alias, shadowing and failure rules.

#### Scenario: Named collection keys
```gherkin
Given a root scope with lazy services "console" of type "*ConsoleHandler" and "audit" of type "*AuditHandler", both implementing "EventHandler"
And a child scope "child" with a lazy service "console" of type "*ChildConsoleHandler" implementing "EventHandler"
When the consumer calls InvokeAllNamed for type "EventHandler" from scope "<from>"
Then the map has keys <keys>
And the value for "console" is the instance from scope "<consoleScope>"

Examples:
  | from  | keys                 | consoleScope |
  | root  | "console", "audit"   | root         |
  | child | "console", "audit"   | child        |
```

<!-- cases: N/A 2 overrides: no option -->
<!-- cases: N/A 4 boundaries: empty covered by "No match yields an empty collection" -->
<!-- cases: N/A 5 errors, 9 panics: covered by "A failing member fails the whole call" Must rows -->

### Requirement: The invoker depends on every member
When a collection is invoked from inside a provider, the invoking service SHALL record a dependency on every member, as `Invoke` does for a single dependency. Invocation hooks SHALL fire once per member, with the member's name.

#### Scenario: Dependency edges order shutdown
```gherkin
Given a root scope
And lazy services "console" and "audit" implementing "EventHandler" and "Shutdowner"
And a lazy service "bus" implementing "Shutdowner" whose provider calls InvokeAll for type "EventHandler"
When the consumer invokes "bus" and then shuts down the root scope
Then the explain output of "bus" lists dependencies "console" and "audit"
And "bus" is shut down before "console" and before "audit"
```

#### Scenario: Invocation hooks per member
```gherkin
Given a root scope with an after-invocation hook that records service names
And lazy services "console" and "audit" implementing "EventHandler"
When the consumer calls InvokeAll for type "EventHandler"
Then the hook recorded "console" exactly 1 time and "audit" exactly 1 time
```

#### Scenario: No edges when invoked outside a provider
```gherkin
Given a root scope with lazy services "console" and "audit" implementing "EventHandler"
When the consumer calls InvokeAll for type "EventHandler" directly on the root scope
Then "console" and "audit" have no dependents
```

<!-- cases: N/A 2 overrides: edge recording is not configurable; it is what makes lifecycle ordering correct -->
<!-- cases: N/A 3 zero values: an empty collection records no edges, covered by the empty-result requirement -->
<!-- cases: N/A 9 panics: no panicking path -->

### Requirement: Transient members and snapshot semantics
A transient member SHALL contribute a new instance on every call. The returned collection SHALL be a snapshot: services registered after the call are not added to it, and mutating the returned slice or map SHALL NOT affect later calls.

#### Scenario: Transient and singleton members across calls
```gherkin
Given a root scope
And a transient service "req" of type "*ReqHandler" implementing "EventHandler"
And a lazy service "console" of type "*ConsoleHandler" implementing "EventHandler"
When the consumer calls InvokeAll for type "EventHandler" twice
Then the "req" elements of the two results are different instances
And the "console" elements of the two results are the same instance
```

#### Scenario: Later registration is not visible in an earlier snapshot
```gherkin
Given a root scope with a lazy service "console" implementing "EventHandler"
And the consumer calls InvokeAll for type "EventHandler" and keeps result R1
When a lazy service "audit" implementing "EventHandler" is registered and InvokeAll is called again as R2
Then R1 has 1 element
And R2 has 2 elements
```

#### Scenario: Caller mutation does not leak
```gherkin
Given a root scope with lazy services "console" and "audit" implementing "EventHandler"
And the consumer calls InvokeAll for type "EventHandler" and sets every element of the result to nil
When the consumer calls InvokeAll for type "EventHandler" again
Then no element of the new result is nil
```

#### Scenario: Collections after shutdown
```gherkin
Given a root scope with a lazy service "console" implementing "EventHandler" that has been invoked
And the root scope has been shut down
When the consumer calls InvokeAll for type "EventHandler"
Then the result is empty
And the error is nil
```

<!-- cases: N/A 2 overrides: snapshot semantics are fixed; live collections are phase 2 -->
<!-- cases: N/A 9 panics: no panicking path -->

### Requirement: Concurrent collection callers
Concurrent calls to `InvokeAll`/`InvokeAllNamed`, including calls concurrent with registrations in the same scope, SHALL be free of data races. Each lazy member SHALL be built exactly once.

#### Scenario: Concurrent callers build each member once
```gherkin
Given a root scope with lazy services "console" and "audit" implementing "EventHandler" whose providers count their calls
When 100 goroutines call InvokeAll for type "EventHandler" concurrently
Then every goroutine receives 2 elements
And each provider was called exactly 1 time
And the race detector reports no race
And goleak reports no leaked goroutine
```

#### Scenario: Concurrent registration and collection
```gherkin
Given a root scope with a lazy service "console" implementing "EventHandler"
When 50 goroutines register distinct services "h-<n>" implementing "EventHandler" while 50 goroutines call InvokeAll for type "EventHandler"
Then every result contains "console"
And every result has between 1 and 51 elements
And the race detector reports no race
```

<!-- cases: N/A 2 overrides, 3 zero values, 4 boundaries: covered by earlier requirements; this requirement is about interleaving only -->
<!-- cases: N/A 9 panics: no panicking path -->
<!-- cases: 10 compatibility: Invoke, InvokeNamed and InvokeAs behavior is unchanged; covered by the existing invoke_test.go and di_test.go suites -->
