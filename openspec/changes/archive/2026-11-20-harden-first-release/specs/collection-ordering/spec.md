# Spec Delta

## MODIFIED Requirements

### Requirement: Order() panic is reported
If `Order()` panics, `InvokeAll[T]` SHALL return no collection and an error reading ``doger: Order() of collection member `<name>` panicked: <value>``. When the panic value is an error it SHALL be wrapped, so `errors.Is` matches it.

#### Scenario: Panicking Order()
```gherkin
Given a root scope with lazy services "ok" and "bad" implementing "EventHandler" and "Ordered"
And Order() of "bad" panics with <panic>
When the consumer calls InvokeAll for type "EventHandler"
Then the result is nil
And the error message contains "bad"
And <match>

Examples:
  | panic                       | match                                     |
  | error ErrOrderBroken        | errors.Is(err, ErrOrderBroken) is true    |
  | string "boom"               | the error message is "doger: Order() of collection member `bad` panicked: boom" |
  | Order() on a nil *Handler   | the error message contains "nil pointer"  |
```

#### Scenario: MustInvokeAll surfaces the Order() panic as its panic
```gherkin
Given a root scope with a lazy service "bad" whose Order() panics with error ErrOrderBroken
When the consumer calls MustInvokeAll for type "EventHandler"
Then the call panics with an error for which errors.Is(err, ErrOrderBroken) is true
```

<!-- cases: N/A 2 overrides: setting WithOrder avoids calling Order(), covered above -->
<!-- cases: N/A 3 zero values, 4 boundaries: a single panicking member is sufficient -->
<!-- cases: N/A 8 concurrency: Order() is called on the calling goroutine -->
