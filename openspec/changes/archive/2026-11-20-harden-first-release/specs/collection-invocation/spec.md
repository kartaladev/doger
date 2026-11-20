# Spec Delta

## MODIFIED Requirements

### Requirement: Transient members and snapshot semantics
A transient member SHALL contribute a new instance on every call. The returned collection SHALL be a snapshot: services registered after the call are not added to it, and mutating the returned slice or map SHALL NOT affect later calls. Once the shutdown of the invoking scope has begun, a call SHALL return nil and an error matching `ErrShutdown` (container-shutdown, "A closed scope stays closed").

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
When the consumer calls <call>
Then the result is nil
And the error matches ErrShutdown

Examples:
  | call                                  |
  | InvokeAll for type "EventHandler"     |
  | InvokeAllMap for type "EventHandler"  |
```

<!-- cases: N/A 2 overrides: InvokeAll is a snapshot by definition; the live alternative is capability live-collections -->
<!-- cases: N/A 9 panics: no panicking path -->
<!-- cases: 10 compatibility: BEHAVIOR CHANGE (flagged) - a collection read after shutdown used to return an empty result and a nil error; it now matches ErrShutdown -->
