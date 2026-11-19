# Spec Delta

## MODIFIED Requirements

### Requirement: A default yields to a visible non-default service of the same name
On its first resolution, a default SHALL check whether a non-default service with the same name is registered in an ancestor of the default's scope. If one is, the default SHALL resolve to that service's instance and its own provider SHALL NOT be called. Defaults, and services in descendant or sibling scopes, SHALL NOT count.

#### Scenario: Placement decides whether a default yields
```gherkin
Given a root scope with child scope "ext", grandchild scope "ext/sub" and child scope "other"
And a lazy "*app.Dialer" registered in scope "ext" with doger.AsDefault(), producing "default-dialer"
And an explicit lazy "*app.Dialer" registered in scope "<explicit_in>", producing "app-dialer"
When the consumer invokes "*app.Dialer" from scope "ext"
Then the result is "<result>"
And the default provider was called <calls> times

Examples:
  | explicit_in | result         | calls |
  | root        | app-dialer     | 0     |
  | other       | default-dialer | 1     |
  | ext/sub     | default-dialer | 1     |
  | (none)      | default-dialer | 1     |
```

#### Scenario: A grandchild default yields to the root
```gherkin
Given a root scope with child scope "a" and grandchild scope "a/b"
And a lazy "*app.Dialer" registered in scope "a/b" with doger.AsDefault()
And an explicit lazy "*app.Dialer" registered in the root, producing "app-dialer"
When the consumer invokes "*app.Dialer" from scope "a/b"
Then the result is "app-dialer"
```

#### Scenario: Registration order does not matter
```gherkin
Given a root scope with child scope "ext"
And a default lazy "*app.Dialer" in scope "ext" and an explicit lazy "*app.Dialer" in the root producing "app-dialer", registered in order <order>
When the consumer invokes "*app.Dialer" from scope "ext"
Then the result is "app-dialer"

Examples:
  | order                     |
  | default first, explicit second |
  | explicit first, default second |
```

#### Scenario: Repeating AsDefault is the same as giving it once
```gherkin
Given a root scope with child scope "ext"
And a lazy "*app.Dialer" registered in scope "ext" with doger.AsDefault(), doger.AsDefault()
And an explicit lazy "*app.Dialer" registered in the root, producing "app-dialer"
When the consumer invokes "*app.Dialer" from scope "ext"
Then the result is "app-dialer"
```

#### Scenario: A default does not yield to another default
```gherkin
Given a root scope with child scope "ext"
And a lazy "*app.Dialer" registered in the root with doger.AsDefault(), producing "root-default"
And a lazy "*app.Dialer" registered in scope "ext" with doger.AsDefault(), producing "ext-default"
When the consumer invokes "*app.Dialer" from scope "ext"
Then the result is "ext-default"
```

#### Scenario: An alias of a default resolves through the default's decision
```gherkin
Given a root scope with child scope "ext"
And a lazy "*app.Dialer" registered in scope "ext" with doger.AsDefault()
And doger.Alias[*app.Dialer, app.DialerIface] registered in scope "ext"
And an explicit lazy "*app.Dialer" registered in the root, producing "app-dialer"
When the consumer invokes "app.DialerIface" from scope "ext"
Then the result is "app-dialer"
```

<!-- cases: 2 overrides: the explicit registration is the override; Override* is covered in "Override ignores AsDefault" -->
<!-- cases: N/A 3 zero values: the empty name is an ordinary name; nil injector is covered in the first requirement -->
<!-- cases: 4 boundaries: depth 1 (parent) and 2 (grandparent), sibling and descendant rows -->
<!-- cases: N/A 5 invalid input: no input beyond the registrations -->
<!-- cases: 6 duplicate: same-scope cases are covered in "Same-scope registrations of a name with a default"; default-vs-default is the fifth scenario -->
<!-- cases: 7 lifecycle: the frozen decision is covered in "The decision is made once, at first resolution" -->
<!-- cases: 8 concurrency: covered in "Concurrent first resolution and replacement" -->
<!-- cases: N/A 9 panics: yielding calls no provider of the default; the target's provider panic follows provider-panic-handling -->
<!-- cases: N/A 10 compatibility: new behavior, only for registrations given AsDefault() -->

### Requirement: Same-scope registrations of a name with a default
In one scope, an explicit registration SHALL replace an unresolved default of the same name. A default registered where an explicit service of that name exists SHALL be skipped. An explicit registration over a resolved default, or a second default, SHALL panic with "doger: service `<name>` has already been declared".

#### Scenario: Explicit and default registrations of one name in one scope
```gherkin
Given a root scope
And <first> "*app.Dialer" registered in the root
And <step>
When the consumer registers <second> "*app.Dialer" in the root
Then the registration <outcome>
And invoking "*app.Dialer" returns "<result>"

Examples:
  | first                       | step                              | second              | outcome                                                          | result         |
  | a lazy default-dialer default | nothing else happened           | explicit app-dialer | succeeds                                                         | app-dialer     |
  | explicit app-dialer         | nothing else happened             | a default           | succeeds without effect                                          | app-dialer     |
  | a lazy default-dialer default | "*app.Dialer" was invoked       | explicit app-dialer | panics with "doger: service `*app.Dialer` has already been declared" | default-dialer |
  | a lazy default-dialer default | nothing else happened           | a second default    | panics with "doger: service `*app.Dialer` has already been declared" | default-dialer |
  | an eager default value      | nothing else happened             | explicit app-dialer | succeeds                                                         | app-dialer     |
  | a transient default         | nothing else happened             | explicit app-dialer | succeeds                                                         | app-dialer     |
```

#### Scenario: Registration hooks around defaults
```gherkin
Given a root scope with before- and after-registration hooks that record the service name
And <first> "*app.Dialer" registered in the root
When the consumer registers <second> "*app.Dialer" in the root
Then the before-registration hook saw "*app.Dialer" <before> times in total
And the after-registration hook saw "*app.Dialer" <after> times in total

Examples:
  | first                  | second              | before | after |
  | a lazy default         | an explicit lazy    | 2      | 2     |
  | an explicit lazy       | a lazy default      | 2      | 1     |
```

#### Scenario: A replaced default keeps its registration position
```gherkin
Given a root scope
And a lazy "first" registered, then a lazy "*app.Dialer" registered with doger.AsDefault(), then a lazy "last" registered
When the consumer registers an explicit "*app.Dialer" in the root
Then the registration order of the root is "first", "*app.Dialer", "last"
```

<!-- cases: 2 overrides: Override* is covered in "Override ignores AsDefault" -->
<!-- cases: N/A 3 zero values: the empty name behaves as any name -->
<!-- cases: 4 boundaries: zero, one and two prior registrations of the name are covered by the rows -->
<!-- cases: 5 errors: both panic rows; the message is the existing duplicate message -->
<!-- cases: 6 duplicate: the subject -->
<!-- cases: 7 lifecycle: "resolved" is pinned by the invoked row; a yielded default counts as resolved -->
<!-- cases: 8 concurrency: covered in "Concurrent first resolution and replacement" -->
<!-- cases: 9 panics: rows 3 and 4 -->
<!-- cases: 10 compatibility: an explicit duplicate without defaults still panics, see "Registrations without AsDefault are unchanged" -->

### Requirement: Concurrent first resolution and replacement
Concurrent first resolutions of a default SHALL agree on one decision and call its provider at most once. An explicit registration racing a first resolution SHALL either replace the default before it is resolved, or panic as a duplicate; an instance a default's provider built SHALL never be dropped from the container.

#### Scenario: Concurrent first resolutions agree
```gherkin
Given a root scope with child scope "ext"
And a lazy "*app.Dialer" registered in scope "ext" with doger.AsDefault(), whose provider counts its calls
When 100 goroutines invoke "*app.Dialer" from scope "ext" concurrently
Then every goroutine receives the same instance
And the provider was called exactly 1 time
```

#### Scenario: Replacement racing first resolution has one outcome
```gherkin
Given a root scope
And a lazy "*app.Dialer" registered with doger.AsDefault() that implements Shutdown() and counts calls, whose provider blocks until released
And a goroutine that will invoke "*app.Dialer", and another that will register an explicit "*app.Dialer", ordered by a channel handshake as <order>
When both goroutines finish
Then the registration <registration>
And every invoke returns <instance>
And after root Shutdown the default's Shutdown was called <shutdowns> times

Examples:
  | order                                             | registration                                                        | instance               | shutdowns |
  | registration completes before the invoke starts   | succeeded                                                           | the explicit instance  | 0         |
  | the invoke's decision completes before registering | panicked with "doger: service `*app.Dialer` has already been declared" | the default instance   | 1         |
```

<!-- cases: N/A 2 overrides, 3 zero values, 4 boundaries, 5 invalid input: no input -->
<!-- cases: 6 duplicate: the panic row is the duplicate path -->
<!-- cases: 7 lifecycle: the shutdown column pins that no built instance leaks -->
<!-- cases: 8 concurrency: the subject; a stress variant of both rows runs 1,000 times under -race -->
<!-- cases: 9 panics: the duplicate panic is the only panic -->
<!-- cases: N/A 10 compatibility: new behavior -->

### Requirement: Registrations without AsDefault are unchanged
A registration given no `AsDefault()` SHALL behave exactly as before this change, including the panic on a duplicate name in one scope and shadowing of an ancestor service by a same-name service in a nearer scope.

#### Scenario: An explicit duplicate in one scope still panics
```gherkin
Given a root scope with an explicit lazy "*app.Dialer" registered in the root
When the consumer registers an explicit lazy "*app.Dialer" in the root
Then the registration panics with "doger: service `*app.Dialer` has already been declared"
```

#### Scenario: A nearer explicit service still shadows an ancestor's
```gherkin
Given a root scope with child scope "ext"
And an explicit lazy "*app.Dialer" producing "first" in the root and one producing "second" in scope "ext"
When the consumer invokes "*app.Dialer" from scope "ext"
Then the result is "second"
```

<!-- cases: N/A 2-9: this requirement only pins that existing behavior is unchanged; each behavior is specified where it is owned -->
<!-- cases: 10 compatibility: the subject -->

## ADDED Requirements

### Requirement: A default without a replacement resolves like any registration
A registration given `AsDefault()` SHALL be resolved like a registration without it when no non-default service of the same name is visible from the default's scope: its provider is called on first resolution and the instance is returned, cached for lazy and eager services, as usual.

#### Scenario: Default resolves when nothing replaces it
```gherkin
Given a root scope
And <form> "*app.Dialer" registered in the root with doger.AsDefault(), producing "default-dialer"
When the consumer invokes "*app.Dialer" from the root <times> times
Then every result is "default-dialer"
And the provider was called <calls> times

Examples:
  | form                  | times | calls                |
  | a lazy service        | 1     | 1                    |
  | a lazy service        | 3     | 1                    |
  | an eager value        | 3     | - (no provider)      |
  | a transient service   | 3     | 3                    |
```

#### Scenario: A nil injector registration is rejected
```gherkin
Given no container is passed
When the consumer calls doger.Provide[*app.Dialer](nil, newDialer, doger.AsDefault())
Then the call panics with an error matching doger.ErrNilInjector
And no service is registered in any container
```

<!-- cases: 2 overrides: the explicit registration is the override; covered by "A default yields to a visible non-default service of the same name" and "Same-scope registrations of a name with a default" -->
<!-- cases: 3 zero values: the nil injector scenario (no global container exists any more; see resolution-errors); a typed-nil eager value given AsDefault() is a value like any other and resolves as is -->
<!-- cases: 4 boundaries: the times/calls rows (1 and many) -->
<!-- cases: N/A 5 invalid input: AsDefault takes no argument and is valid on every registration form -->
<!-- cases: 6 duplicate: covered by "Same-scope registrations of a name with a default" -->
<!-- cases: 7 lifecycle: covered by the lifecycle and eager-shutdown requirements -->
<!-- cases: 8 concurrency: covered by "Concurrent first resolution and replacement" -->
<!-- cases: N/A 9 panics: a provider panic follows provider-panic-handling unchanged -->
<!-- cases: 10 compatibility: covered by "Registrations without AsDefault are unchanged" -->

### Requirement: A yielded default resolves its target by name on every call
A default that yielded SHALL resolve, on every resolution, the non-default service currently registered under its name in the scope it yielded to, as an alias resolves by name. A later override of that service SHALL be seen; a service no longer registered there SHALL give an error matching `ErrServiceNotFound`, never the default's own provider.

#### Scenario: Yielded default follows the ancestor's current registration
```gherkin
Given a root scope with child scope "ext"
And an explicit lazy "*app.Dialer" registered in the root, producing "app-dialer-1"
And a lazy "*app.Dialer" registered in scope "ext" with doger.AsDefault(), producing "default-dialer"
And "*app.Dialer" was invoked from scope "ext" and returned "app-dialer-1"
And <change>
When the consumer invokes "*app.Dialer" from scope "ext"
Then the result is <result>
And the error is <error>
And the default's provider was called 0 times

Examples:
  | change                                                                                   | result          | error                                  |
  | nothing else happened                                                                    | "app-dialer-1"  | nil                                    |
  | doger.OverrideNamed(root, "*app.Dialer", provider producing "app-dialer-2")              | "app-dialer-2"  | nil                                    |
  | doger.OverrideNamedValue(root, "*app.Dialer", "app-dialer-3")                            | "app-dialer-3"  | nil                                    |
  | doger.OverrideNamedTransient(root, "*app.Dialer", provider producing a new value per call) | a new value    | nil                                    |
  | doger.ShutdownNamed(root, "*app.Dialer") removed the root service                        | the zero value  | an error matching doger.ErrServiceNotFound |
```

#### Scenario: Yielded default through an alias follows an override
```gherkin
Given a root scope with child scope "ext"
And an explicit lazy "*app.Dialer" in the root producing "app-dialer-1"
And a lazy "*app.Dialer" in scope "ext" with doger.AsDefault()
And doger.Alias[*app.Dialer, app.DialerIface] registered in scope "ext"
And "app.DialerIface" was invoked from scope "ext"
And doger.OverrideNamed(root, "*app.Dialer", provider producing "app-dialer-2") was called
When the consumer invokes "app.DialerIface" from scope "ext"
Then the result is "app-dialer-2"
```

#### Scenario: Concurrent reads during an override
```gherkin
Given the setup of "Yielded default follows the ancestor's current registration" with nothing else happened
When 8 goroutines invoke "*app.Dialer" from scope "ext" 100 times each while one goroutine calls doger.OverrideNamed(root, "*app.Dialer", provider producing "app-dialer-2")
Then the race detector reports no data race
And every result is "app-dialer-1" or "app-dialer-2"
And every result after the override returned is "app-dialer-2"
```

<!-- cases: 1 happy path: the "nothing else happened" row -->
<!-- cases: 2 overrides: OverrideNamed, OverrideNamedValue and OverrideNamedTransient rows -->
<!-- cases: N/A 3 zero values: the name is fixed at registration; a nil injector is covered by "A default without a replacement is built and used" -->
<!-- cases: 4 boundaries: zero overrides and one override; the transient row checks per-call resolution -->
<!-- cases: 5 errors: the removed-target row -->
<!-- cases: 6 override of an existing entry: the subject -->
<!-- cases: 7 lifecycle: removal by ShutdownNamed; the decision itself stays fixed per "The decision is made once, at first resolution" -->
<!-- cases: 8 concurrency: "Concurrent reads during an override" -->
<!-- cases: N/A 9 panics: no provider of the default runs; the target's provider panic follows provider-panic-handling -->
<!-- cases: 10 compatibility: without an override the result is identical to before -->

## REMOVED Requirements

### Requirement: A default without a replacement is built and used
**Reason**: Its scenario "A nil injector registers the default on DefaultRootScope" pins the package-level container, which this change removes (see container-options "No package-level container exists").
**Migration**: The behavior is kept, without the global container, in the ADDED requirement "A default without a replacement resolves like any registration"; register defaults on a container created with doger.New() and pass it explicitly.
