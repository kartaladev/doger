# Spec Delta

## MODIFIED Requirements

### Requirement: A report can be filtered by tag
`HealthReport(opts...)` and `HealthReportWithContext(ctx, opts...)` SHALL accept `HealthReportOption` values, a functional option type: a nil option SHALL be ignored. `MatchHealthTags(tags...)` SHALL limit the report, its overall state and any on-demand checks to covered services carrying at least one of the tags; repeated options accumulate. With no tag given, or no option, the report SHALL cover every health checker.

#### Scenario: Filtered reports
```gherkin
Given an injector that is never started, with Healthchecker values "db" (no options, returns errors.New("down")), "proc" (WithHealthTags(HealthTagLiveness), returns nil) and "cache" (WithHealthTags("cache", HealthTagReadiness), returns nil)
When the consumer calls HealthReport(<options>) on the root scope
Then the report lists <listed>
And its State is <state>

Examples:
  | options                                                         | listed                   | state           |
  | none                                                            | "cache", "db", "proc"    | HealthUnhealthy |
  | MatchHealthTags(HealthTagLiveness)                              | "proc"                   | HealthHealthy   |
  | MatchHealthTags(HealthTagReadiness)                             | "cache", "db"            | HealthUnhealthy |
  | MatchHealthTags("cache")                                        | "cache"                  | HealthHealthy   |
  | MatchHealthTags("cache"), MatchHealthTags(HealthTagLiveness)    | "cache", "proc"          | HealthHealthy   |
  | MatchHealthTags()                                               | "cache", "db", "proc"    | HealthUnhealthy |
  | MatchHealthTags("unknown-tag")                                  | no service               | HealthHealthy   |
  | nil                                                             | "cache", "db", "proc"    | HealthUnhealthy |
```

#### Scenario: A filtered on-demand read checks only the covered services
```gherkin
Given an injector that is never started, with Healthchecker values "db" (no options) and "proc" (WithHealthTags(HealthTagLiveness)), both counting their calls
When the consumer calls HealthReport(MatchHealthTags(HealthTagLiveness)) on the root scope
Then the health check of "proc" was called 1 time
And the health check of "db" was not called
```

#### Scenario: Report options are functions a consumer can compose
```gherkin
Given an injector that is never started, with Healthchecker values "db" (no options) and "proc" (WithHealthTags(HealthTagLiveness))
And a consumer helper `func liveness() HealthReportOption { return MatchHealthTags(HealthTagLiveness) }`
And a slice opts of type []HealthReportOption holding <opts>
When the consumer calls HealthReport(opts...) on the root scope
Then the report lists <listed>

Examples:
  | opts                                              | listed        |
  | liveness()                                        | "proc"        |
  | liveness(), IgnoreContainerPhase()                | "proc"        |
  | nil, liveness()                                   | "proc"        |
  | (empty slice)                                     | "db", "proc"  |
  | HealthReportOption(nil)                           | "db", "proc"  |
```

<!-- cases: 1 default: no option covers every checker -->
<!-- cases: 2 overrides: single, repeated and custom tags; options combined with IgnoreContainerPhase -->
<!-- cases: 3 zero values: MatchHealthTags(), a nil option, a typed-nil HealthReportOption and an empty slice; the former zero-value struct option no longer exists (BREAKING, compile-time only for code that declared HealthReportOption{} literals) -->
<!-- cases: 4 boundaries: a filter matching 0, 1 and several services -->
<!-- cases: N/A 5 errors: an unknown tag matches nothing, which is not an error -->
<!-- cases: N/A 6 not found/duplicate: a service matching two tags is listed once (the repeated-option row) -->
<!-- cases: 7 lifecycle: on-demand reads of an idle container -->
<!-- cases: N/A 8 concurrency: filtering is a pure selection over the shared checks of health-reports -->
<!-- cases: N/A 9 panics: a nil option is ignored, never called -->
<!-- cases: 10 backward compatibility: calls of HealthReport with MatchHealthTags/IgnoreContainerPhase compile and behave unchanged; only code naming the struct literal HealthReportOption{} breaks -->

### Requirement: Invalid health classification panics at registration
Registration SHALL panic before the service is stored, naming the service, when `WithHealthTags` is given an empty tag, or when `WithHealthTags` or `AsOptionalHealth` is applied to a transient registration. Classification options on a service that turns out not to be a health checker SHALL have no effect.

#### Scenario: Invalid classification
```gherkin
Given an injector
When the consumer calls <registration> for "<name>" with <option>
Then the call <outcome>
And the service is registered: <registered>

Examples:
  | name  | registration          | option                       | outcome                                                     | registered |
  | db    | ProvideNamedValue     | WithHealthTags("")           | panics "doger: service `db` has an empty health tag"           | false      |
  | db    | ProvideNamedValue     | WithHealthTags("ok", "")     | panics "doger: service `db` has an empty health tag"           | false      |
  | tmp   | ProvideNamedTransient | WithHealthTags("readiness")  | panics "doger: transient service `tmp` cannot be health-checked" | false    |
  | tmp   | ProvideNamedTransient | AsOptionalHealth()           | panics "doger: transient service `tmp` cannot be health-checked" | false    |
  | cfg   | ProvideNamedValue (no health check) | AsOptionalHealth() | returns; HealthReport() does not list "cfg"              | true       |
```

<!-- cases: N/A 1 default: no option, no panic -->
<!-- cases: 2 overrides: each option -->
<!-- cases: 3 zero values: the empty tag -->
<!-- cases: N/A 4 boundaries: not applicable -->
<!-- cases: 5 errors: each panic message names the service -->
<!-- cases: N/A 6 not found/duplicate: duplicates collapse (previous requirement) -->
<!-- cases: 7 lifecycle: the panic happens before the service is stored -->
<!-- cases: N/A 8 concurrency: not applicable -->
<!-- cases: 9 panics: this requirement -->
<!-- cases: N/A 10 backward compatibility: new options -->

### Requirement: Classification follows clones, overrides, defaults and aliases
`Clone` and `CloneWithOpts` SHALL copy each registration's classification. `Override*` SHALL replace it with the new call's options, and no options SHALL mean the default. A default that yields SHALL take no part: the entry shows the classification of the service it yielded to. An alias or an export carries no classification and no entry of its own.

#### Scenario: Classification across clone, override and alias
```gherkin
Given an injector with a Healthchecker value "db" registered with WithHealthTags(HealthTagLiveness) and AsOptionalHealth()
When the consumer <action> and calls HealthReport() on <target>
Then the entry for "<entry>" has Tags <tags> and Optional <optional>

Examples:
  | action                                                          | target         | entry | tags          | optional |
  | clones the injector                                             | the clone      | db    | ["liveness"]  | true     |
  | overrides "db" with OverrideNamedValue and no options           | the injector   | db    | ["readiness"] | false    |
  | overrides "db" with OverrideNamedValue and WithHealthTags("x")  | the injector   | db    | ["x"]         | false    |
  | adds an alias "hc" to "db" with AliasNamed                         | the injector   | db    | ["liveness"]  | true     |
  | registers, in child scope "child", a Healthchecker value "db" with AsDefault() and WithHealthTags("x") | scope "child" | db | ["liveness"] | true |
```

<!-- cases: N/A 1 default: covered by the first requirement -->
<!-- cases: 2 overrides: Override with and without options -->
<!-- cases: N/A 3 zero values: not applicable -->
<!-- cases: N/A 4 boundaries: not applicable -->
<!-- cases: N/A 5 errors: not applicable -->
<!-- cases: 6 not found/duplicate: Override, alias (no entry of its own, health-reports) and a yielded default -->
<!-- cases: 7 lifecycle: clone -->
<!-- cases: N/A 8 concurrency: not applicable -->
<!-- cases: N/A 9 panics: not applicable -->
<!-- cases: 10 backward compatibility: follows the existing option rules of registration-options -->
