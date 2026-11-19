# Spec Delta

## Purpose

Defines struct-tag injection with `InvokeStruct` and `MustInvokeStruct` for single-service fields: how a dependency whose value is a nil interface is assigned, and which errors an invalid target type reports, so struct injection never crashes the process.

## ADDED Requirements

### Requirement: A nil-interface dependency sets the field to its zero value
When a tagged field's dependency resolves to a nil interface value and the service's declared (registered) type is assignable to the field's type, `InvokeStruct` SHALL set the field to its zero value and continue with the next field, returning a nil error, as the collection path already does. A nil interface has no dynamic type, so the declared type decides: when it is not assignable to the field, `InvokeStruct` SHALL return the zero value and the "not assignable" error matching `ErrStructInjection` (see "Invalid injection targets match ErrStructInjection"), even when a non-nil value of that service would fit the field.

#### Scenario: Injecting a nil-interface service
```gherkin
Given a root scope with ProvideNamedValue[io.Reader](root, "reader", nil)
And the int value 7 registered as type int
When the consumer calls InvokeStruct for a struct with field <field> tagged <tag> and an int field tagged do:""
Then the error is nil
And the reader field is nil
And the int field is 7

Examples:
  | field                       | tag               |
  | exported Reader io.Reader   | do:"reader"       |
  | unexported reader io.Reader | do:"reader"       |
```

#### Scenario: MustInvokeStruct with a nil-interface service
```gherkin
Given a root scope with ProvideNamedValue[io.Reader](root, "reader", nil)
When the consumer calls MustInvokeStruct for a struct with field Reader io.Reader tagged do:"reader"
Then it does not panic
And the field is nil
```

<!-- cases: 1 default: default tag key "do" -->
<!-- cases: N/A 2 overrides: the struct tag key option is covered by module-identity -->
<!-- cases: 3 zero values: the nil interface is the subject -->
<!-- cases: 4 boundaries: exported and unexported field -->
<!-- cases: 5 errors: a declared type not assignable to the field is a row of "Invalid targets" -->
<!-- cases: N/A 6 duplicates: unrelated -->
<!-- cases: N/A 7 lifecycle: unrelated -->
<!-- cases: N/A 8 concurrency: no shared state written -->
<!-- cases: 9 panics: previously a reflect panic; the Must variant no longer panics -->
<!-- cases: 10 backward compatibility: non-nil dependencies are assigned exactly as before -->

### Requirement: Invalid injection targets match ErrStructInjection
`InvokeStruct` SHALL return the zero value and an error matching `ErrStructInjection`, naming the type or field, when the target is not a struct or a pointer chain to one, when a resolved service is not assignable to its field (for a nil-interface value, when its declared type is not), when a `*` tag is on a field that is not a slice or `map[string]`, or when the target type is a recursive pointer type. It SHALL never recurse without bound. A named pointer type to a struct, such as `type P *S`, SHALL be a valid target, injected like `*S`.

#### Scenario: Invalid targets
```gherkin
Given a root scope with the string value "x" registered under the name "name"
And ProvideNamedValue[io.Reader](root, "reader", nil)
When the consumer calls <call>
Then the result is the zero value
And the error matches ErrStructInjection
And the error message is "<message>"

Examples:
  | call                                                                   | message                                                                         |
  | InvokeStruct[int]                                                      | doger: must be a struct or a pointer to a struct, but got `int`                 |
  | InvokeStruct[R] where type R *R                                        | doger: recursive pointer type `R` cannot be injected                            |
  | InvokeStruct[App] where App has field Port int tagged do:"name"        | doger: `name` is not assignable to field `App.Port`                             |
  | InvokeStruct[App] where App has field Port int tagged do:"*"           | doger: tag `*` requires a slice or map[string] field, got `App.Port`            |
  | InvokeStruct[App] where App has field Port int tagged do:"reader"      | doger: `reader` is not assignable to field `App.Port`                           |
  | InvokeStruct[App] where App has field Port string tagged do:"reader"   | doger: `reader` is not assignable to field `App.Port`                           |
  | InvokeStruct[App] where App has field Port io.Writer tagged do:"reader" | doger: `reader` is not assignable to field `App.Port`                          |
```

#### Scenario: A named pointer-to-struct target
```gherkin
Given a root scope with the string value "x" registered under the name "name"
And type S struct { Name string `do:"name"` } and type P *S
When the consumer calls InvokeStruct[P]
Then the error is nil
And the result is a non-nil P whose Name is "x"
```

#### Scenario: MustInvokeStruct with a recursive pointer type
```gherkin
Given a root scope
And type R *R
When the consumer calls MustInvokeStruct[R]
Then it panics with an error value matching ErrStructInjection
And the process does not crash with a stack overflow
```

#### Scenario: Dependency failures are not ErrStructInjection
```gherkin
Given a root scope with nothing registered
When the consumer calls InvokeStruct for a struct with field DB *DB tagged do:""
Then the error matches ErrServiceNotFound
And the error does not match ErrStructInjection
```

<!-- cases: 1 default: default tag key -->
<!-- cases: N/A 2 overrides: none -->
<!-- cases: 3 zero values: the non-struct int target; a nil-interface service whose declared type does not fit the field -->
<!-- cases: 4 boundaries: a recursive pointer type is the unbounded case -->
<!-- cases: 5 errors: every invalid-target path matches ErrStructInjection; a missing service matches only ErrServiceNotFound -->
<!-- cases: 6 not found: third scenario -->
<!-- cases: N/A 7 lifecycle: unrelated -->
<!-- cases: N/A 8 concurrency: no shared state written -->
<!-- cases: 9 panics: the Must variant panics with a matchable error instead of a fatal stack overflow -->
<!-- cases: 10 backward compatibility: the same calls still fail; their errors become matchable and use the doger prefix (flagged) -->
