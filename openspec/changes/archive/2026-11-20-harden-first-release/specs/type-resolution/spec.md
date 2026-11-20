# Spec Delta

## ADDED Requirements

### Requirement: InvokeAs returns a nil-interface instance without panicking
When the service selected by `InvokeAs[T]` or `MustInvokeAs[T]` resolves to a nil interface value, the call SHALL return the zero value of `T` and a nil error instead of panicking.

#### Scenario: Selecting a service whose value is a nil interface
```gherkin
Given a root scope with ProvideNamedValue[io.Reader](root, "reader", nil)
When the consumer calls <call>
Then the result is a nil io.Reader
And <outcome>

Examples:
  | call                        | outcome                 |
  | InvokeAs[io.Reader](root)   | the error is nil        |
  | MustInvokeAs[io.Reader](root) | the call does not panic |
```

#### Scenario: A non-nil selection is unchanged
```gherkin
Given a root scope with ProvideNamedValue[io.Reader](root, "reader", strings.NewReader("x"))
When the consumer calls InvokeAs[io.Reader](root)
Then the result reads "x"
And the error is nil
```

<!-- cases: 1 default: no configuration -->
<!-- cases: N/A 2 overrides: none -->
<!-- cases: 3 zero values: the nil interface is the subject -->
<!-- cases: N/A 4 boundaries: one candidate -->
<!-- cases: N/A 5 errors: success path; ErrServiceNotMatch paths are unchanged -->
<!-- cases: N/A 6 duplicates: selection order is the existing requirement -->
<!-- cases: N/A 7 lifecycle: unrelated -->
<!-- cases: N/A 8 concurrency: no shared state written -->
<!-- cases: 9 panics: previously an interface-conversion panic -->
<!-- cases: 10 backward compatibility: non-nil selections are unchanged (second scenario) -->

## MODIFIED Requirements

### Requirement: InvokeAs skips the invoking service
When `InvokeAs[T]` or `MustInvokeAs[T]` is called from a provider, the service that provider builds SHALL NOT be a candidate, as for collections, and neither SHALL an alias whose target chain resolves to it, so a decorator can resolve the service it wraps. When it is the only match, the result SHALL be an error matching `ErrServiceNotMatch`.

#### Scenario: The invoking service is not a candidate
```gherkin
Given a root scope with lazy "decor" implementing "Logger", whose provider calls InvokeAs[Logger] and wraps the result
And <others>
When the consumer calls InvokeNamed for "decor"
Then the result is <result>

Examples:
  | others                                            | result                                               |
  | lazy "base" implementing Logger, registered after "decor" | "decor" wrapping "base", nil error                  |
  | lazy "base" implementing Logger, registered before "decor" | "decor" wrapping "base", nil error                 |
  | no other service implementing Logger              | an error matching ErrServiceNotMatch, not ErrCircularDependency |
  | alias "decor-alias" of "decor" as Logger, then lazy "base" implementing Logger | "decor" wrapping "base", nil error |
  | alias "a1" of "decor", alias "a2" of "a1", both as Logger, then lazy "base" implementing Logger | "decor" wrapping "base", nil error |
  | alias "decor-alias" of "decor" as Logger, and no other service | an error matching ErrServiceNotMatch, not ErrCircularDependency |
```

#### Scenario: MustInvokeAs from a provider
```gherkin
Given a root scope with lazy "decor" implementing "Greeter", whose provider wraps MustInvokeAs[Greeter](i)
And lazy "base" implementing "Greeter", registered after "decor"
When the consumer calls InvokeNamed for "decor"
Then "decor" wraps "base"
And ExplainNamedService(root, "decor") lists exactly the dependency base
```

#### Scenario: A stored provider injector still skips its service
```gherkin
Given a root scope with lazy "decor" implementing "Greeter", whose provider stores its injector and returns
And lazy "base" implementing "Greeter", registered after "decor"
And the consumer invoked "decor"
When the consumer calls InvokeAs[Greeter] with the stored injector
Then the result is "base"
```

#### Scenario: The alias skip compares names
```gherkin
Given a root scope with lazy "svc" implementing "Greeter" and alias "g" of "svc" registered with AliasNamed
And child scope "child" with lazy "svc" implementing "Greeter", whose provider calls InvokeAs[Greeter](i)
When the consumer invokes "svc" from "child"
Then the error matches ErrServiceNotMatch, not ErrCircularDependency, because "g" targets the name "svc" of the invoking service
```

<!-- cases: 1 default: the first row -->
<!-- cases: N/A 2 overrides: InvokeNamed selects a specific service, including the provider's own -->
<!-- cases: 3 zero values: no other match gives ErrServiceNotMatch -->
<!-- cases: 4 boundaries: zero and one other match -->
<!-- cases: 5 errors: ErrServiceNotMatch instead of ErrCircularDependency -->
<!-- cases: 6 duplicates: registration order no longer matters for self-selection; aliases of the invoking service, direct or chained, are skipped too, by target name, even when the target name resolves to a shadowed ancestor service -->
<!-- cases: 7 lifecycle: a provider's injector stored and used after the provider returned still skips its service -->
<!-- cases: N/A 8 concurrency, 9 panics: unchanged -->
<!-- cases: 10 compatibility: AliasNamed replaces AsNamed (BREAKING rename, see service-aliases). BEHAVIOR CHANGE (flagged) - a decorator calling InvokeAs for its own interface no longer fails with ErrCircularDependency; a lone self-match fails with ErrServiceNotMatch. BEHAVIOR CHANGE (flagged) - an alias of the invoking service is skipped too -->


## REMOVED Requirements

### Requirement: An alias registered with a nil injector uses DefaultRootScope
**Reason**: `DefaultRootScope` is removed and a nil injector no longer falls back to a global container (design D10).
**Migration**: Create a container with `doger.New()` and pass it to `Alias`/`AliasNamed`; a nil injector now returns an error matching `ErrNilInjector` (see resolution-errors).
