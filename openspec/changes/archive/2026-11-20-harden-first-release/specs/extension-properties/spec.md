# Spec Delta

## MODIFIED Requirements

### Requirement: The property source is chosen by the application
At a container's first property read, its source SHALL be decided and kept: `InjectorOpts.PropertySource` when non-nil; otherwise the root's service named `NameOf[PropertySource]()`; otherwise `EnvPropertySource("DOGER_")`, registered in the root as a default under that name. Services of other names are never selected.

#### Scenario: Source selection
```gherkin
Given a root scope created with <opts>
And <registered>
And the environment variable "DOGER_REDIS_ADDR" is set to "from-env"
And an extension "redis" whose Extend records x.Properties().Lookup("addr")
When the consumer calls doger.Extend(root, redisExt)
Then the recorded value is "<value>"

Examples:
  | opts                                           | registered                                                         | value      |
  | no PropertySource                              | nothing                                                            | from-env   |
  | no PropertySource                              | a lazy doger.PropertySource (an *app.MapSource) with "redis.addr" = "from-service" | from-service |
  | PropertySource = map with "redis.addr" = "from-opts" | a lazy doger.PropertySource (an *app.MapSource) with "redis.addr" = "from-service" | from-opts |
  | no PropertySource                              | a lazy "*app.Cache" with a Lookup(string) (string, bool) method returning "from-cache" | from-env |
```

#### Scenario: A source registered after the first read is not used
```gherkin
Given a root scope with no InjectorOpts.PropertySource, "DOGER_REDIS_ADDR" set to "env-a" and "DOGER_REDIS___B_ADDR" set to "env-b"
And doger.Extend(root, redisExt) read the property "addr"
And a lazy "*app.MapSource" (registered under its own type name) with "redis/b.addr" = "late" is registered afterwards
When a second extension "redis" with instance "b" reads "addr" during its Extend
Then it reads "env-b"
```

#### Scenario: Registering the interface name after the first read panics
```gherkin
Given a root scope with no InjectorOpts.PropertySource whose first property read happened
When the consumer registers a lazy service under the name doger.NameOf[doger.PropertySource]()
Then the registration panics with "doger: service `github.com/kartaladev/doger.PropertySource` has already been declared"
```

<!-- cases: 1 default: the environment row -->
<!-- cases: 2 overrides: service and InjectorOpts rows -->
<!-- cases: 3 zero values: a nil InjectorOpts.PropertySource means unset -->
<!-- cases: 4 boundaries: before and after the first read -->
<!-- cases: 5 errors: the late same-name registration panics as a duplicate (default-registrations) -->
<!-- cases: 6 duplicate: late registration scenarios -->
<!-- cases: 7 lifecycle: the decision is kept for the container's life -->
<!-- cases: 8 concurrency: covered by "Concurrent lookups" -->
<!-- cases: 9 panics: third scenario -->
<!-- cases: N/A 10 compatibility: new API -->

### Requirement: A failed source decision is not kept
If deciding the source panics, the panic SHALL reach the reader and nothing SHALL be kept, so the next read decides again. If the selected source service fails to resolve, every property SHALL be unset for the container, and one line SHALL be logged with the error's type, never its message.

#### Scenario: A failed decision is retried
```gherkin
Given a root scope with no InjectorOpts.PropertySource, "DOGER_REDIS_ADDR" set to "from-env", and a HookAfterRegistration that panics with "hook boom" the first time a service named doger.NameOf[doger.PropertySource]() is registered
When an extension "redis" reads "addr" during its Extend, twice
Then the first doger.Extend returns an *ExtensionError containing "hook boom"
And the second reads ("from-env", true)
```

#### Scenario: A failing source provider leaves every property unset
```gherkin
Given a root scope with a Logger whose handler records every record at Debug level and a lazy doger.PropertySource whose provider returns errors.New("vault sealed: token s3cret")
And the environment variable "DOGER_REDIS_ADDR" is set to "from-env"
When an extension "redis" reads "addr" during its Extend
Then the read returns ("", false)
And one recorded record has message "property source unavailable"
And no recorded record, in its message or attributes, contains "s3cret" or "vault sealed"
```

<!-- cases: N/A 1 default, 2 overrides: failure handling is not configurable; InjectorOpts.PropertySource avoids the provider -->
<!-- cases: N/A 3 zero values, 4 boundaries: no input -->
<!-- cases: 5 errors: a failing provider leaves every property unset -->
<!-- cases: 6 duplicate: the retried decision registers the default once -->
<!-- cases: 7 lifecycle: a failed decision is not kept; a provider failure is kept like any decision -->
<!-- cases: N/A 8 concurrency: as in "Concurrent lookups" -->
<!-- cases: 9 panics: a panic while deciding reaches the reader and is retried -->
<!-- cases: N/A 10 compatibility: new API -->

### Requirement: Property values are never logged
The library SHALL NOT write property values to its logger or include them in errors it returns; when it logs a failure of a property source, it SHALL log the error's type only.

#### Scenario: Values stay out of logs and errors
```gherkin
Given a root scope with a Logger whose handler records every record at Debug level and InjectorOpts.PropertySource holding "redis.password" = "s3cret"
And an extension "redis" whose Extend reads "password" and then returns errors.New("auth failed")
When the consumer calls doger.Extend(root, redisExt)
Then no recorded record (message or attributes) and not the returned error contain "s3cret"
```

<!-- cases: N/A 2-7: secrecy has no input beyond the value -->
<!-- cases: N/A 8 concurrency, 9 panics: unchanged -->
<!-- cases: N/A 10 compatibility: new API -->

### Requirement: Providers read properties through their injector
`ExtensionProperties(i)` SHALL return the properties view of the extension whose private scope `i` belongs to, or a descendant of it, bound to `i`'s container, and `true`; for an injector outside any private extension scope, including the provider of a flattened extension, it SHALL return a view that always misses, and `false`.

#### Scenario: A provider reads properties in the original and the cloned container
```gherkin
Given a root scope created with InjectorOpts.PropertySource holding "redis.addr" = "orig"
And an extension "redis" whose provider of "*redis.Client" records doger.ExtensionProperties(i).Lookup("addr")
And doger.Extend(root, redisExt) returned nil
And clone := root.CloneWithOpts with InjectorOpts.PropertySource holding "redis.addr" = "cloned"
When the consumer invokes "*redis.Client" from <container>
Then the recorded value is "<value>"

Examples:
  | container | value  |
  | root      | orig   |
  | clone     | cloned |
```

#### Scenario: Injectors inside and outside extension scopes
```gherkin
Given a root scope with InjectorOpts.PropertySource holding "redis.addr" = "a:1"
And a lazy "*app.Svc" whose provider records doger.ExtensionProperties(i) and its Lookup("addr"), registered in <where>
When the consumer invokes "*app.Svc" from that scope
Then the recorded ok is <ok> and the Lookup returned <result>

Examples:
  | where                                                       | ok    | result        |
  | the root                                                    | false | ("", false)   |
  | a child scope "jobs" of the root                            | false | ("", false)   |
  | a child scope "pool" of the private scope of extension "redis" | true | ("a:1", true) |
  | the root, by extension "redis" installed with doger.ExtendIntoRoot() | false | ("", false)  |
```

<!-- cases: N/A 2 overrides: the view follows the container's source -->
<!-- cases: 3 zero values: an injector outside extensions gets a missing view -->
<!-- cases: 4 boundaries: original and cloned container; root, ordinary child, descendant of a private scope, flattened extension -->
<!-- cases: N/A 5 errors: misses, not errors -->
<!-- cases: N/A 6 duplicate: one view per extension identity -->
<!-- cases: 7 lifecycle: clone row -->
<!-- cases: N/A 8 concurrency: covered by "Concurrent lookups" -->
<!-- cases: N/A 9 panics: never panics -->
<!-- cases: N/A 10 compatibility: new API -->
