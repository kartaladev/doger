# Spec Delta

## Purpose

Defines what `ExplainService`, `ExplainNamedService` and `ExplainInjector` report about the dependency graph, so introspection is safe to call at any time, stays bounded in size, renders names verbatim and reflects only dependencies that were actually built.

## ADDED Requirements

### Requirement: Explaining is free of data races
`ExplainService`, `ExplainNamedService` and `ExplainInjector` SHALL be safe to call concurrently with the first build of any service, with invocations, and with the shutdown of any service or scope. A build time SHALL be reported as 0 until the build completed and as the measured build time after it.

#### Scenario: Explain concurrently with a lazy build
```gherkin
Given a root scope
And a lazy service "slow" whose provider blocks until released, then returns "slow-instance"
And a goroutine invoking "slow" from the root
When 8 goroutines call <explain> 100 times each while "slow" is building, and "slow" is released after the first 50 calls
Then the race detector reports no data race
And every ServiceBuildTime observed for "slow" before the release is 0
And the ServiceBuildTime observed after the invocation returned is greater than 0

Examples:
  | explain                                    |
  | doger.ExplainNamedService(root, "slow")    |
  | doger.ExplainInjector(root)                |
```

#### Scenario: Explain concurrently with shutdown
```gherkin
Given a root scope with child scope "req"
And 20 lazy services "svc-0" to "svc-19" registered in "req", each invoked once
When one goroutine calls req.Shutdown() while 4 goroutines call doger.ExplainInjector(root) 100 times each
Then the race detector reports no data race
And no call panics
```

<!-- cases: 1 happy path: both scenarios use zero configuration -->
<!-- cases: N/A 2 overrides: explain has no options -->
<!-- cases: N/A 3 zero values: an unknown service returns ok=false as today; a nil injector is owned by resolution-errors (ErrNilInjector) -->
<!-- cases: 4 boundaries: build time before (0) and after (> 0) the build completes -->
<!-- cases: N/A 5 invalid input: explain returns no error -->
<!-- cases: N/A 6 duplicate: not applicable to a read -->
<!-- cases: 7 lifecycle: during build and during shutdown -->
<!-- cases: 8 concurrency: the subject; goleak verifies no goroutine outlives the test -->
<!-- cases: 9 panics: "no call panics" during shutdown -->
<!-- cases: 10 compatibility: field names and JSON tags are unchanged -->

### Requirement: Each service is expanded once per explanation
Within one `ExplainService` or `ExplainNamedService` result, the dependency tree and the dependents tree SHALL each expand a service's own dependencies only at its first occurrence. Every later occurrence SHALL be a node with `Ref` set to true and no `Recursive` children. The node count SHALL equal the number of reachable edges.

#### Scenario: Layered graph is bounded
```gherkin
Given a root scope
And <count> lazy services "svc-0" to "svc-<last>", where "svc-1" invokes "svc-0" and every "svc-k" with k >= 2 invokes "svc-<k-1>" then "svc-<k-2>"
And "svc-<last>" was invoked
When the consumer calls doger.ExplainNamedService(root, "svc-<last>")
Then the dependency tree holds <nodes> nodes in total
And <expanded> nodes have Ref false
And <refs> nodes have Ref true and no Recursive children

Examples:
  | count | last | nodes | expanded | refs |
  | 1     | 0    | 0     | 0        | 0    |
  | 2     | 1    | 1     | 1        | 0    |
  | 3     | 2    | 3     | 2        | 1    |
  | 24    | 23   | 45    | 23       | 22   |
```

#### Scenario: Dependents tree is bounded the same way
```gherkin
Given the 24-service layered graph of "Layered graph is bounded", with "svc-23" invoked
When the consumer calls doger.ExplainNamedService(root, "svc-0")
Then the dependents tree holds 45 nodes in total
And 23 nodes have Ref false
```

#### Scenario: A reference renders as a single line
```gherkin
Given a root scope where "a" invokes "b" then "c", and "b" invokes "c"
And "a" was invoked
When the consumer calls String() on doger.ExplainNamedService(root, "a")
Then the dependencies section lists "c" twice
And exactly one of those lines ends with " (ref)"
And no line is nested under the line ending with " (ref)"
```

#### Scenario: Ref is serialized only when true
```gherkin
Given the graph of "A reference renders as a single line"
When the consumer marshals doger.ExplainNamedService(root, "a") to JSON
Then exactly one dependency object has "ref": true
And no other dependency object has a "ref" key
```

<!-- cases: 1 happy path: layered graph with defaults -->
<!-- cases: N/A 2 overrides: expansion is not configurable; a reference is followed by explaining that service by name -->
<!-- cases: 3 zero values: the single-service row (no dependencies) yields an empty, non-nil slice as today -->
<!-- cases: 4 boundaries: 1, 2, 3 and 24 services -->
<!-- cases: N/A 5 invalid input: explain takes a name only; unknown names return ok=false as today -->
<!-- cases: 6 duplicate: repeated occurrences are the subject -->
<!-- cases: 7 lifecycle: cycles cannot reach the tree because circular invocations fail and record no edge (see "Only successful builds record dependency edges") -->
<!-- cases: N/A 8 concurrency: covered by "Explaining is free of data races" -->
<!-- cases: N/A 9 panics: explain does not panic -->
<!-- cases: 10 compatibility: BREAKING: a repeated service is no longer expanded in full; Ref is added; migration in design D10 -->

### Requirement: Explanation text is plain text
The `String()` output of every explain type SHALL contain service and scope names verbatim, with no HTML escaping. Escaping for HTML is the caller's responsibility.

#### Scenario: Names with HTML metacharacters render verbatim
```gherkin
Given a root scope with child scope "<scope>"
And a lazy service named "<service>" registered in that scope and invoked
When the consumer calls String() on <explain>
Then the output contains "<service>" exactly as registered
And the output does not contain "&lt;", "&gt;", "&amp;", "&#39;" or "&#34;"

Examples:
  | scope   | service      | explain                                         |
  | ext     | a<b>&'c"     | doger.ExplainNamedService(scope, "a<b>&'c\"")   |
  | ext     | a<b>&'c"     | doger.ExplainInjector(root)                     |
  | t<1>    | db           | doger.ExplainInjector(root)                     |
```

<!-- cases: 1 happy path: names without metacharacters are covered by the existing explain tests -->
<!-- cases: N/A 2 overrides: no option -->
<!-- cases: 3 zero values: the empty service name renders as an empty name, as today -->
<!-- cases: N/A 4 boundaries: no numeric input -->
<!-- cases: N/A 5 invalid input: any string is a valid name -->
<!-- cases: N/A 6 duplicate, 7 lifecycle: unrelated to rendering -->
<!-- cases: N/A 8 concurrency: rendering works on a value already returned -->
<!-- cases: N/A 9 panics: rendering does not panic on any name; the fuzz target in the design Test Strategy covers arbitrary names -->
<!-- cases: 10 compatibility: BREAKING for consumers that relied on the escaped text; names without metacharacters render identically -->

### Requirement: Only successful builds record dependency edges
A dependency edge from a service to a dependency it resolved SHALL be recorded only when that resolution succeeded, on every resolution path. A failed resolution SHALL NOT appear in `Dependencies` or `Dependents`; a later successful retry SHALL record the edge once.

#### Scenario: A failed dependency records no edge
```gherkin
Given a root scope
And a lazy service "b" whose provider returns errors.New("b down")
And a lazy service "a" whose provider resolves "b" with <path> and returns the error
When the consumer invokes "a" and receives an error wrapping "b down"
Then doger.ExplainNamedService(root, "a") lists no dependencies
And doger.ExplainNamedService(root, "b") lists no dependents

Examples:
  | path                                     |
  | doger.InvokeNamed[*B](i, "b")            |
  | doger.InvokeNamed[any](i, "b")           |
  | doger.Invoke[*B](i)                      |
  | doger.InvokeStruct with a field tagged do:"b" |
```

#### Scenario: A successful retry records the edge once
```gherkin
Given the setup of "A failed dependency records no edge" with path doger.InvokeNamed[*B](i, "b")
And "b"'s provider succeeds from its second call on
And "a" was invoked once and failed
When the consumer invokes "a" twice more
Then doger.ExplainNamedService(root, "a") lists exactly 1 dependency, "b"
```

<!-- cases: 1 happy path: successful edges are covered by the existing explain tests -->
<!-- cases: N/A 2 overrides: no option -->
<!-- cases: N/A 3 zero values: no input -->
<!-- cases: 4 boundaries: failure then success; repeated success records one edge -->
<!-- cases: 5 errors: each resolution path row -->
<!-- cases: 6 duplicate: repeated successful invocations record one edge -->
<!-- cases: 7 lifecycle: shutdown ordering follows the recorded edges, so a failed dependency no longer orders shutdown -->
<!-- cases: N/A 8 concurrency: edge recording keeps its existing lock; covered by the existing -race suite -->
<!-- cases: N/A 9 panics: a panicking dependency is a failed resolution, same as the error rows -->
<!-- cases: 10 compatibility: the by-type path already behaved this way; the by-name paths now match it -->
