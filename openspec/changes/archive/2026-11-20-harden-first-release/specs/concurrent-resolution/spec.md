# Spec Delta

## Purpose

Defines how the container resolves a lazy service, or an eager service with declared dependencies, when several goroutines or a re-entrant provider ask for it before its first build has finished. Resolution never deadlocks inside the library: a cycle is reported as `ErrCircularDependency`, and every waiting caller gets the builder's result.

## ADDED Requirements

### Requirement: A cycle hit by concurrent first resolutions is an error
When goroutines resolve the services of a dependency cycle concurrently, and none of those services is built yet, every call SHALL return within 2s an error matching `ErrCircularDependency` with `errors.Is`. No call SHALL block forever and no goroutine SHALL leak. A later resolution SHALL try the build again and fail the same way.

#### Scenario: Concurrent resolution of a cycle returns ErrCircularDependency
```gherkin
Given a root scope with the lazy services <cycle>, each provider invoking the next service through the injector it receives
And each provider waits on a shared barrier until all <goroutines> providers have started
When <goroutines> goroutines each invoke a different service of the cycle at the same time
Then every call returns within 2s
And every error matches ErrCircularDependency
And goleak reports no leaked goroutine after the test

Examples:
  | cycle                     | goroutines |
  | "a" -> "b" -> "a"         | 2          |
  | "a" -> "b" -> "c" -> "a"  | 3          |
  | "a" -> "b" -> "c" -> "a"  | 2          |
```

#### Scenario: A cycle through a declared dependency is detected concurrently
```gherkin
Given a root scope with lazy "a" registered with DependsOnNamed("b")
And lazy "b" whose provider invokes "a"
And both providers wait on a shared barrier until both started
When one goroutine invokes "a" and another invokes "b" at the same time
Then both calls return within 2s with an error matching ErrCircularDependency
```

#### Scenario: A failed cyclic build is retried, not cached
```gherkin
Given the cycle "a" -> "b" -> "a" whose concurrent resolution returned ErrCircularDependency
When the consumer invokes "a" again from one goroutine
Then the error matches ErrCircularDependency
And each provider was called again
```

<!-- cases: 1 default: no option changes cycle handling; the first scenario is the default -->
<!-- cases: N/A 2 overrides: a deadlock is never wanted, so detection cannot be turned off (proposal Defaults & Control) -->
<!-- cases: N/A 3 zero values: covered by resolution-errors (nil injector); names are concrete -->
<!-- cases: 4 boundaries: 2 and 3 goroutines, and 2 goroutines on a 3-service cycle (the third service is reached by a goroutine already in the cycle) -->
<!-- cases: 5 errors: every row asserts errors.Is(err, ErrCircularDependency) -->
<!-- cases: N/A 6 not found/duplicate: unrelated to cycles -->
<!-- cases: 7 lifecycle: the retry scenario; a cycle with a shutdown in flight is covered by container-shutdown -->
<!-- cases: 8 concurrency: this requirement is the concurrency contract; tests run under -race with goleak -->
<!-- cases: N/A 9 panics: covered by "Waiting callers share the builder's outcome" -->
<!-- cases: 10 backward compatibility: sequential cycle detection keeps its existing errors; only the hang becomes an error -->

### Requirement: A re-entrant build through a captured injector is an error
When a provider, directly or through other providers, resolves its own service again on the same goroutine through an injector it did not receive (for example a captured root or scope) before its build finished, that resolution SHALL return an error matching `ErrCircularDependency` instead of blocking. The outer invocation SHALL then fail with an error matching it too.

#### Scenario: Re-entrant resolution through a captured injector
```gherkin
Given a root scope "root"
And <setup>
When the consumer invokes "a" from root
Then the call returns within 2s
And the error matches ErrCircularDependency

Examples:
  | setup                                                                                          |
  | lazy "a" whose provider calls InvokeNamed[int](root, "a")                                      |
  | lazy "a" whose provider calls InvokeNamed[int](root, "b"), and lazy "b" invoking "a" through its injector |
  | lazy "a" whose provider calls InvokeNamed[int](root, "b"), and lazy "b" calling InvokeNamed[int](root, "a") |
  | lazy "a" in child scope "c" whose provider calls InvokeNamed[int](c, "a")                       |
  | lazy "a" whose provider calls InvokeAs[fmt.Stringer](root), and "a" is the only fmt.Stringer    |
```

#### Scenario: Resolving another service through a captured injector still works
```gherkin
Given a root scope "root" with lazy "b" returning 2
And lazy "a" whose provider returns InvokeNamed[int](root, "b") + 1
When the consumer invokes "a"
Then the result is 3 and the error is nil
```

<!-- cases: 1 default: no option involved -->
<!-- cases: N/A 2 overrides: none -->
<!-- cases: N/A 3 zero values: resolution-errors covers nil injectors -->
<!-- cases: 4 boundaries: self re-entry (1 service) and 2-service re-entry -->
<!-- cases: 5 errors: errors.Is(err, ErrCircularDependency) on every row -->
<!-- cases: N/A 6 duplicates: unrelated -->
<!-- cases: 7 lifecycle: child-scope row; the non-cyclic captured-injector scenario guards against false positives -->
<!-- cases: 8 concurrency: the re-entry happens on the building goroutine; cross-goroutine waits are the previous requirement -->
<!-- cases: N/A 9 panics: no panic involved -->
<!-- cases: 10 backward compatibility: a captured injector resolving other services keeps working (second scenario) -->
<!-- cases: non-goal: a transient provider re-entering itself through a captured injector recurses until Go reports a stack overflow; deadlocks across goroutines a provider spawns and waits on are not detected (design Non-Goals) -->

### Requirement: Concurrent first resolution builds a service once
When several goroutines resolve a lazy service that is not built yet, the container SHALL call its provider exactly once, and every caller SHALL receive the same instance. Callers that resolve unrelated or shared non-cyclic dependencies concurrently SHALL NOT receive `ErrCircularDependency`.

#### Scenario: Single build under contention
```gherkin
Given a root scope with lazy "db" whose provider counts its calls and blocks until released
When <callers> goroutines invoke "db" and the provider is released once all of them are waiting
Then every caller receives the same *DB pointer and a nil error
And the provider was called exactly 1 time

Examples:
  | callers |
  | 1       |
  | 2       |
  | 64      |
```

#### Scenario: Concurrent builds of a shared dependency are not cycles
```gherkin
Given a root scope with lazy <graph>, each provider invoking its dependencies through its injector
And the provider of "d" blocks until every other provider started
When one goroutine per top-level service invokes it at the same time
Then every call returns a nil error
And the provider of "d" was called exactly 1 time

Examples:
  | graph                                              |
  | "a" -> "d", "b" -> "d"                             |
  | "a" -> "b" -> "d", "a" -> "c" -> "d" (diamond)     |
  | "a" -> "d", "b" -> "a" -> "d"                      |
```

<!-- cases: 1 default: no configuration -->
<!-- cases: N/A 2 overrides: none -->
<!-- cases: N/A 3 zero values: a provider returning a zero value is an ordinary instance -->
<!-- cases: 4 boundaries: 1, 2 and 64 callers -->
<!-- cases: N/A 5 errors: success paths; failures are the next requirement -->
<!-- cases: N/A 6 duplicates: unrelated -->
<!-- cases: N/A 7 lifecycle: shutdown racing a build is covered by container-shutdown -->
<!-- cases: 8 concurrency: the whole requirement; -race -->
<!-- cases: N/A 9 panics: next requirement -->
<!-- cases: 10 backward compatibility: the existing single-build guarantee is kept -->

### Requirement: Waiting callers share the builder's outcome
A caller that waits for a build in flight SHALL receive the builder's instance on success, or an error matching the builder's failure with `errors.Is` when the provider returns an error, panics (matching `ErrProviderPanic`) or exits through `runtime.Goexit` (matching `ErrProviderPanic`). After a failed build the service SHALL be buildable again by the next resolution.

#### Scenario: Waiters receive the build failure
```gherkin
Given a root scope with lazy "db" whose provider blocks until 8 other goroutines wait on "db", then <outcome>
When 9 goroutines invoke "db"
Then each of the 8 waiting calls returns <result>
And the provider was called exactly 1 time
And a later invoke of "db" calls the provider again

Examples:
  | outcome                          | result                                                            |
  | returns errors.New("refused")    | an error matching the "refused" error                             |
  | panics with "boom"               | an error matching ErrProviderPanic whose message contains "boom"  |
  | calls runtime.Goexit()           | an error matching ErrProviderPanic whose message contains "runtime.Goexit" |
```

<!-- cases: 1 default: no configuration -->
<!-- cases: N/A 2 overrides: none -->
<!-- cases: N/A 3 zero values: none -->
<!-- cases: 4 boundaries: 8 waiters plus the builder -->
<!-- cases: 5 errors: each failure kind and its matching sentinel -->
<!-- cases: N/A 6 duplicates: unrelated -->
<!-- cases: 7 lifecycle: retry after a failed build -->
<!-- cases: 8 concurrency: waiters are released on every outcome; goleak -->
<!-- cases: 9 panics: panic and Goexit rows -->
<!-- cases: 10 backward compatibility: a failed lazy build is still retried, as before -->
