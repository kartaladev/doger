# Spec Delta

## MODIFIED Requirements

### Requirement: A live collection re-resolves on every read
`InvokeCollection[T](i) Collection[T]` SHALL return a value whose `All(r Injector) ([]T, error)` and `Map(r Injector) (map[string]T, error)` resolve the members on each call, from the scope of `i`, with the membership, order, error and hook rules of `InvokeAll[T]`/`InvokeAllMap[T]`. `InvokeCollection` SHALL build nothing itself. Once the shutdown of that scope has begun, `All` and `Map` SHALL return nil and an error matching `ErrShutdown`, as `InvokeAll` does.

#### Scenario: Later registrations are visible
```gherkin
Given a root scope with lazy "console" implementing "EventHandler"
And c := InvokeCollection[EventHandler](root)
And the consumer then registers lazy "audit" implementing "EventHandler"
When the consumer calls <read>
Then the result is <result>

Examples:
  | read         | result                                |
  | c.All(nil)   | [console, audit], nil error           |
  | c.Map(nil)   | {"console", "audit"} keys, nil error  |
```

#### Scenario: Creating a live collection builds nothing
```gherkin
Given a root scope with lazy "console" implementing "EventHandler" whose provider counts its calls
When the consumer calls InvokeCollection[EventHandler](root)
Then the provider was called 0 times
And after c.All(nil) it was called 1 time
```

#### Scenario: Reads after shutdown and failures
```gherkin
Given a root scope with <setup> and c := InvokeCollection[EventHandler](root)
When the consumer calls c.All(nil)
Then the result is <result>

Examples:
  | setup                                                        | result                                                    |
  | lazy "console", and the root scope has been shut down        | nil and an error matching ErrShutdown                     |
  | lazy "console" whose provider returns errBuild               | nil and an error matching errBuild naming "console"       |
  | no service implementing EventHandler                         | an empty, non-nil slice and a nil error                   |
```

#### Scenario: A zero Collection is bound to no container
```gherkin
Given var c doger.Collection[EventHandler], never returned by InvokeCollection
When the consumer calls <read>
Then the result is nil and an error matching doger.ErrNilInjector

Examples:
  | read         |
  | c.All(nil)   |
  | c.Map(nil)   |
```

#### Scenario: Concurrent reads are race-free
```gherkin
Given a root scope with lazy "console" implementing "EventHandler" whose provider counts its calls, and c := InvokeCollection[EventHandler](root)
When 50 goroutines call c.All(nil) while 10 goroutines register lazy "h-<n>" implementing "EventHandler"
Then no data race is reported
And the "console" provider was called exactly 1 time
```

<!-- cases: 1 default: re-resolution on read -->
<!-- cases: 2 overrides: ordering overrides apply as for InvokeAll; a snapshot is InvokeAll -->
<!-- cases: 3 zero values: no member yields an empty non-nil slice; a nil i panics with a value matching ErrNilInjector, as InvokeCollection does (resolution-errors); a zero Collection's All and Map return an error matching ErrNilInjector -->
<!-- cases: N/A 4 boundaries: covered by collection-invocation -->
<!-- cases: 5 errors: member failure row -->
<!-- cases: 6 duplicates: alias exclusion as for InvokeAll; the holder has its own requirement -->
<!-- cases: 7 lifecycle: after shutdown, late registration -->
<!-- cases: 8 concurrency: concurrent reads and registrations -->
<!-- cases: N/A 9 panics: an Order() panic is returned by All, as in collection-ordering -->
<!-- cases: N/A 10 compatibility: new API; the read signature changed before any release, so no consumer breaks -->
