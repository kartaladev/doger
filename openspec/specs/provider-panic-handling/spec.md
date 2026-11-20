# provider-panic-handling Specification

## Purpose
Defines what a consumer gets when a service provider panics during invocation: an error instead of a crash, with the panic value preserved so it can be matched, including a panic with a nil value.

## Requirements

### Requirement: A panicking provider yields an invocation error
When a lazy or transient provider panics during invocation, the invocation SHALL return the zero value and a `*ServiceError` naming the service instead of propagating the panic. Its cause SHALL match `ErrProviderPanic`. An error panic value SHALL also match with `errors.Is`, and a nil panic value with `errors.As` to `*runtime.PanicNilError`. Any other value SHALL appear in the message. A lazy provider that exits its goroutine with `runtime.Goexit` SHALL release every caller waiting for its build with a `*ServiceError` naming the service whose cause matches `ErrProviderPanic` and mentions `runtime.Goexit` (D1); the goroutine that called it does not return, and a later invocation builds again.

#### Scenario: Provider panic values become invocation errors
```gherkin
Given a root scope
And a <kind> provider for type int that panics with <value>
When the consumer invokes that service
Then the returned value is 0
And the error message is "<message>"
And the error matches ErrProviderPanic with errors.Is
And the error matches <match>

Examples:
  | kind      | value               | message                                                                                          | match                                      |
  | lazy      | nil                 | doger: service "int" in scope "[root]": provider panicked: runtime error: panic called with nil argument | errors.As *runtime.PanicNilError |
  | transient | nil                 | doger: service "int" in scope "[root]": provider panicked: runtime error: panic called with nil argument | errors.As *runtime.PanicNilError |
  | lazy      | errors.New("boom")  | doger: service "int" in scope "[root]": provider panicked: boom                                  | errors.Is the panicked error               |
  | transient | errors.New("boom")  | doger: service "int" in scope "[root]": provider panicked: boom                                  | errors.Is the panicked error               |
  | lazy      | "s"                 | doger: service "int" in scope "[root]": provider panicked: s                                     | errors.As *ServiceError with Service "int" |
  | lazy      | 42                  | doger: service "int" in scope "[root]": provider panicked: 42                                    | errors.As *ServiceError with Service "int" |
```

#### Scenario: A provider exiting with runtime.Goexit releases its waiters
```gherkin
Given a root scope with lazy "db" whose first provider call waits until 8 other goroutines wait for its build, then calls runtime.Goexit()
When 9 goroutines invoke "db"
Then each of the 8 waiting calls returns a *ServiceError with Service "db"
And each error matches ErrProviderPanic and its message contains "runtime.Goexit"
And a later invoke of "db" calls the provider again and returns its instance
```

#### Scenario: Consumers can restore the legacy nil-panic behavior
```gherkin
Given the process runs with GODEBUG "panicnil=1"
And a root scope with a lazy provider for type int that panics with nil
When the consumer invokes that service
Then the returned value is 0
And the error is nil
```

<!-- cases: 1 default: the first scenario is the zero-configuration behavior -->
<!-- cases: 2 overrides: GODEBUG panicnil=1 (or a //go:debug panicnil=1 directive in the consumer's main package) is the Go-provided override; second scenario -->
<!-- cases: 3 zero values: the nil panic value rows -->
<!-- cases: N/A 4 boundaries: no numeric limits -->
<!-- cases: 5 errors: each row states how the error is matched; every row also matches ErrProviderPanic -->
<!-- cases: N/A 6 not found/duplicate: unrelated to provider panics -->
<!-- cases: N/A 7 lifecycle: recovery happens per invocation; the retry behavior of a lazy service after a failed build is unchanged and covered by existing tests -->
<!-- cases: 8 concurrency: waiters sharing a panicked build are covered by concurrent-resolution; the Goexit scenario releases 8 waiters -->
<!-- cases: 9 panics: this requirement is the panic-recovery contract -->
<!-- cases: 10 backward compatibility: BREAKING (flagged) - an error panic value is no longer returned as is but wrapped (errors.Is still matches it), and messages gain the doger prefix, the service and the scope -->

### Requirement: A provider panic during a dependency's build names both services
When a provider panics while building a dependency of another service, the invocation SHALL return a `*ServiceError` for the requested service wrapping a `*ServiceError` for the dependency whose provider panicked, so `errors.Is(err, ErrProviderPanic)` matches and `errors.As` yields the requested service first.

#### Scenario: Panic in a nested provider
```gherkin
Given a root scope with lazy "api" invoking lazy "db"
And the provider of "db" panics with "connection pool exhausted"
When the consumer invokes "api"
Then the error matches ErrProviderPanic
And the outermost *ServiceError has Service "api"
And the error message is "doger: service \"api\" in scope \"[root]\": service \"db\" in scope \"[root]\": provider panicked: connection pool exhausted"
And invoking "api" again calls the provider of "db" again
```

<!-- cases: 1 default: no configuration -->
<!-- cases: N/A 2 overrides: none -->
<!-- cases: N/A 3 zero values: covered by the modified requirement -->
<!-- cases: 4 boundaries: one level of nesting; deeper nesting adds one layer per service (resolution-errors) -->
<!-- cases: 5 errors: errors.Is and errors.As -->
<!-- cases: N/A 6 duplicates: unrelated -->
<!-- cases: 7 lifecycle: the failed build is retried -->
<!-- cases: N/A 8 concurrency: covered by concurrent-resolution -->
<!-- cases: 9 panics: the subject -->
<!-- cases: 10 backward compatibility: the panic was already an error; its message and wrapping change (flagged) -->
