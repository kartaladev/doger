# Spec Delta

## Purpose

Defines the errors resolution and registration report, so a consumer can tell apart a missing service, a type mismatch, a duplicate registration, a nil injector and a failure inside a service's build with `errors.Is` and `errors.As` instead of matching message text.

## ADDED Requirements

### Requirement: A type mismatch matches ErrServiceTypeMismatch
When a service is found by name but its registered type is not the requested type, the invocation SHALL return the zero value and an error matching `ErrServiceTypeMismatch` that names both types. It SHALL NOT match `ErrServiceNotFound` or `ErrServiceNotMatch`.

#### Scenario: Invoking a name under the wrong type
```gherkin
Given a root scope with <registration>
When the consumer calls <call>
Then the result is the zero value
And the error matches ErrServiceTypeMismatch
And the error does not match ErrServiceNotFound
And the error message is "<message>"

Examples:
  | registration                     | call                          | message                                                                     |
  | ProvideNamedValue(root, "x", 42) | InvokeNamed[string](root, "x") | doger: service found, but type mismatch: invoking `string` but registered `int` |
  | ProvideNamedValue(root, "x", 42) | InvokeNamed[int64](root, "x")  | doger: service found, but type mismatch: invoking `int64` but registered `int`  |
```

#### Scenario: MustInvokeNamed panics with the mismatch
```gherkin
Given a root scope with ProvideNamedValue(root, "x", 42)
When the consumer calls MustInvokeNamed[string](root, "x")
Then it panics with an error value matching ErrServiceTypeMismatch
```

<!-- cases: 1 default: no configuration -->
<!-- cases: N/A 2 overrides: none -->
<!-- cases: N/A 3 zero values: an empty name is "not found", covered below -->
<!-- cases: N/A 4 boundaries: no numeric limits -->
<!-- cases: 5 errors: matches ErrServiceTypeMismatch, not ErrServiceNotFound -->
<!-- cases: N/A 6 duplicates: covered by the duplicate requirement -->
<!-- cases: N/A 7 lifecycle: lookup error, independent of lifecycle -->
<!-- cases: N/A 8 concurrency: no shared state written -->
<!-- cases: 9 panics: the Must variant -->
<!-- cases: 10 backward compatibility: BREAKING (flagged) only in the message prefix; the sentence is unchanged -->

### Requirement: A failure inside a build is a ServiceError
When a service's build fails (its provider returns an error or panics, or a dependency it resolves fails), the invocation SHALL return a `*ServiceError` naming that service and its scope, wrapping the cause so `errors.Is` matches the cause's sentinels. Each service whose build failed SHALL add one `*ServiceError` layer. A lookup failure of the requested name itself SHALL NOT be a `*ServiceError`.

#### Scenario: Errors.As finds the failing service
```gherkin
Given a root scope with <graph>
When the consumer invokes "a"
Then errors.As(err, *ServiceError) is <is_service_error>
And the outermost ServiceError has Service "<service>" and Scope "[root]"
And errors.Is(err, <sentinel>) is true
And the error message is "<message>"

Examples:
  | graph                                                         | is_service_error | service | sentinel                         | message                                                                                                             |
  | nothing registered                                            | false            | (none)  | ErrServiceNotFound               | doger: could not find service `a`, no service available                                                             |
  | lazy "a" whose provider returns errors.New("refused")         | true             | a       | the "refused" error              | doger: service "a" in scope "[root]": refused                                                                       |
  | lazy "a" invoking missing "b"                                 | true             | a       | ErrServiceNotFound               | doger: service "a" in scope "[root]": could not find service `b`, available services: `a`, path: `a` -> `b`         |
  | lazy "a" invoking lazy "b", whose provider returns errors.New("refused") | true  | a       | the "refused" error              | doger: service "a" in scope "[root]": service "b" in scope "[root]": refused                                        |
  | transient "a" whose provider returns errors.New("refused")    | true             | a       | the "refused" error              | doger: service "a" in scope "[root]": refused                                                                       |
```

#### Scenario: ServiceError reports the scope of the failing service
```gherkin
Given a root scope with child scope "tenant-1"
And lazy "repo" in "tenant-1" whose provider returns errors.New("refused")
When the consumer invokes "repo" from "tenant-1"
Then the outermost ServiceError has Service "repo" and Scope "tenant-1"
```

<!-- cases: 1 default: no configuration -->
<!-- cases: N/A 2 overrides: none -->
<!-- cases: 3 zero values: nothing registered row (no ServiceError) -->
<!-- cases: 4 boundaries: zero, one and two ServiceError layers -->
<!-- cases: 5 errors: errors.Is to the cause and errors.As to the outermost service on every row -->
<!-- cases: 6 not found: a missing requested name versus a missing dependency -->
<!-- cases: 7 lifecycle: child-scope row; failures during shutdown are covered by container-shutdown -->
<!-- cases: N/A 8 concurrency: waiters sharing a failure are covered by concurrent-resolution -->
<!-- cases: N/A 9 panics: provider-panic-handling covers panics, which are wrapped the same way -->
<!-- cases: 10 backward compatibility: BREAKING (flagged) message text gains the service and scope; errors.Is on the cause keeps working -->

### Requirement: A duplicate registration matches ErrServiceAlreadyDeclared
Registering a name already registered in the same scope SHALL fail with an error matching `ErrServiceAlreadyDeclared`: the registration functions without an error result SHALL panic with that error as the panic value, and error-returning ones SHALL return it. The existing service SHALL stay registered.

#### Scenario: Duplicate registrations
```gherkin
Given a root scope with lazy "db" returning 1
When the consumer calls <call>
Then <outcome> an error matching ErrServiceAlreadyDeclared with message "doger: service `db` has already been declared"
And InvokeNamed[int](root, "db") still returns 1

Examples:
  | call                                                       | outcome                  |
  | ProvideNamed(root, "db", provider returning 2)             | it panics with           |
  | ProvideNamedValue(root, "db", 2)                           | it panics with           |
  | ProvideNamedTransient(root, "db", provider returning 2)    | it panics with           |
  | AliasNamed[int, int](root, "other", "db") after ProvideNamedValue(root, "other", 3) | it returns |
  | MustAliasNamed[int, int](root, "other", "db") after ProvideNamedValue(root, "other", 3) | it panics with |
```

#### Scenario: Override replaces without an error
```gherkin
Given a root scope with lazy "db" returning 1
When the consumer calls OverrideNamedValue(root, "db", 2)
Then nothing panics
And InvokeNamed[int](root, "db") returns 2
```

<!-- cases: 1 default: registration without options -->
<!-- cases: 2 overrides: Override* is the explicit replacement path -->
<!-- cases: N/A 3 zero values: an empty name is a valid, distinct name -->
<!-- cases: N/A 4 boundaries: one existing registration -->
<!-- cases: 5 errors: the panic value and the returned error both match the sentinel -->
<!-- cases: 6 duplicate/override: the subject of this requirement -->
<!-- cases: N/A 7 lifecycle: registration on a closed scope is covered by container-shutdown (ErrShutdown) -->
<!-- cases: N/A 8 concurrency: concurrent duplicate registrations are decided under the scope lock (existing behavior, existing test) -->
<!-- cases: 9 panics: panic values are error values matching the sentinel -->
<!-- cases: 10 backward compatibility: the panic still happens; its value becomes matchable; message prefix BREAKING (flagged) -->

### Requirement: A nil injector is rejected with ErrNilInjector
Every exported function, method or option constructor that takes an injector, root or scope SHALL reject a nil or typed-nil value: functions with an error result SHALL return an error matching `ErrNilInjector`, and the others SHALL panic with a value matching it. No call SHALL fall back to a global container.

#### Scenario: Error-returning entry points with a nil injector
```gherkin
Given <injector>
When the consumer calls <call>
Then the error matches ErrNilInjector
And the error message is "doger: nil injector"

Examples:
  | injector                    | call                                   |
  | a nil Injector              | Invoke[int](nil)                       |
  | a nil Injector              | InvokeNamed[int](nil, "db")            |
  | a nil Injector              | InvokeAs[fmt.Stringer](nil)            |
  | a nil Injector              | InvokeAll[int](nil)                    |
  | a nil Injector              | InvokeStruct[App](nil)                 |
  | a nil Injector              | HealthCheckNamed(nil, "db")            |
  | a nil Injector              | ShutdownNamed(nil, "db")               |
  | a nil Injector              | AliasNamed[int, int](nil, "a", "b")    |
  | a nil Injector              | DeclareDependency(nil, "a", "b")       |
  | a typed-nil *RootScope      | Invoke[int](root)                      |
  | a typed-nil *Scope          | InvokeNamed[int](scope, "db")          |
  | a nil *RootScope            | Extend(nil, ext)                       |
```

#### Scenario: Entry points without an error result panic on a nil injector
```gherkin
Given a nil Injector
When the consumer calls <call>
Then it panics with an error value matching ErrNilInjector

Examples:
  | call                                         |
  | Provide[int](nil, provider)                  |
  | ProvideNamedValue(nil, "db", 1)              |
  | ProvideTransient[int](nil, provider)         |
  | OverrideNamedValue(nil, "db", 1)             |
  | MustInvoke[int](nil)                         |
  | MustAlias[int, int](nil)                     |
  | InvokeCollection[int](nil)                   |
  | ExplainInjector(nil)                         |
  | Package(Lazy[int](provider))(nil)            |
  | Runnable().Handle(nil, StageAfterStart, h)   |
```

<!-- cases: 1 default: there is no default container any more -->
<!-- cases: N/A 2 overrides: pass a container explicitly -->
<!-- cases: 3 zero values: nil interface and typed-nil *RootScope and *Scope rows -->
<!-- cases: N/A 4 boundaries: none -->
<!-- cases: 5 errors: returned error or panic value, both matching ErrNilInjector -->
<!-- cases: N/A 6 duplicates: unrelated -->
<!-- cases: N/A 7 lifecycle: the check happens before any lifecycle state is read -->
<!-- cases: N/A 8 concurrency: no shared state read -->
<!-- cases: 9 panics: second scenario -->
<!-- cases: 10 backward compatibility: BREAKING (flagged) - DefaultRootScope is removed and nil no longer means it; migration: create a container with doger.New() and pass it -->

### Requirement: Not-found errors list at most 10 services
A not-found error SHALL list the visible services sorted by name, at most 10 of them, followed by `and N more` when more exist, and SHALL keep the invoker chain after the list. It SHALL still match `ErrServiceNotFound` (or `ErrServiceNotMatch` for `InvokeAs`).

#### Scenario: Not-found message size
```gherkin
Given a root scope with <count> int values named "s00" to "s<last>"
When the consumer calls InvokeNamed[int](root, "missing")
Then the error matches ErrServiceNotFound
And the error message is "<message>"

Examples:
  | count | last | message                                                                                                  |
  | 0     | -    | doger: could not find service `missing`, no service available                                            |
  | 1     | 00   | doger: could not find service `missing`, available services: `s00`                                       |
  | 10    | 09   | doger: could not find service `missing`, available services: `s00`, `s01`, `s02`, `s03`, `s04`, `s05`, `s06`, `s07`, `s08`, `s09` |
  | 11    | 10   | doger: could not find service `missing`, available services: `s00`, `s01`, `s02`, `s03`, `s04`, `s05`, `s06`, `s07`, `s08`, `s09` and 1 more |
  | 2000  | 1999 | doger: could not find service `missing`, available services: `s00`, `s01`, `s02`, `s03`, `s04`, `s05`, `s06`, `s07`, `s08`, `s09` and 1990 more |
```

#### Scenario: The invoker chain follows the capped list
```gherkin
Given a root scope with 12 int values named "s00" to "s11" and lazy "a" invoking "missing"
When the consumer invokes "a"
Then the error message contains "`s08` and 3 more, path: `a` -> `missing`", because `a` sorts before `s00` and is listed too
```

<!-- cases: 1 default: the cap of 10 has no option; ExplainInjector lists everything (proposal Defaults & Control) -->
<!-- cases: N/A 2 overrides: none -->
<!-- cases: 3 zero values: 0 services row -->
<!-- cases: 4 boundaries: 0, 1, 10 (at the limit), 11 (one past), 2000 -->
<!-- cases: 5 errors: every row matches ErrServiceNotFound -->
<!-- cases: 6 not found: the subject -->
<!-- cases: N/A 7 lifecycle: unrelated -->
<!-- cases: N/A 8 concurrency: read-only -->
<!-- cases: N/A 9 panics: none -->
<!-- cases: 10 backward compatibility: up to 10 services the list is unchanged apart from the prefix; longer lists are truncated (flagged) -->

### Requirement: A duplicate child scope name matches ErrScopeAlreadyDeclared
`Scope(name)` on a scope that already has an attached child named `name` SHALL panic with an error matching the new sentinel `ErrScopeAlreadyDeclared`, whose message is ``doger: scope `<name>` has already been declared``. A child that was shut down is detached (container-shutdown), so its name SHALL be accepted again.

#### Scenario: Declaring a child scope name twice
```gherkin
Given a root scope with <setup>
When the consumer calls <call>
Then <outcome>

Examples:
  | setup                                                        | call                         | outcome                                                                                                   |
  | child scope "tenant"                                         | root.Scope("tenant")         | it panics with an error matching ErrScopeAlreadyDeclared and message "doger: scope `tenant` has already been declared" |
  | child scope "tenant" that was shut down with tenant.Shutdown() | root.Scope("tenant")       | it returns a new child scope "tenant", and root.Children() has 1 element                                  |
  | child scope "tenant"                                         | root.Scope("billing")        | it returns a new child scope "billing", and root.Children() has 2 elements                                |
  | child scope "tenant" with grandchild "jobs"                  | tenant.Scope("jobs")         | it panics with an error matching ErrScopeAlreadyDeclared and message "doger: scope `jobs` has already been declared" |
  | child scope "tenant" with grandchild "jobs"                  | root.Scope("jobs")           | it returns a new child scope "jobs" of the root                                                           |
```

#### Scenario: Concurrent declarations of one child scope name
```gherkin
Given a root scope
When 16 goroutines call root.Scope("tenant") at the same time and recover any panic
Then exactly 1 goroutine got a scope
And 15 goroutines recovered an error matching ErrScopeAlreadyDeclared
And root.Children() has 1 element
```

<!-- cases: N/A 1 default, 2 overrides: there is no option for scope names -->
<!-- cases: N/A 3 zero values: an empty scope name keeps its existing behavior, which this requirement does not change -->
<!-- cases: 4 boundaries: one existing child, one sibling name, a same-name scope at another depth -->
<!-- cases: 5 errors: the panic value matches ErrScopeAlreadyDeclared with errors.Is -->
<!-- cases: 6 duplicate: the subject of this requirement; a shut-down child no longer counts -->
<!-- cases: 7 lifecycle: name reuse after the child's shutdown (container-shutdown); Scope() on a closed parent panics with ErrShutdown instead (container-shutdown) -->
<!-- cases: 8 concurrency: 16 concurrent declarations, one winner -->
<!-- cases: 9 panics: the duplicate panics, as it did before -->
<!-- cases: 10 backward compatibility: still a panic; BREAKING only in its message prefix (doger: instead of DI:) and in matching a sentinel -->
