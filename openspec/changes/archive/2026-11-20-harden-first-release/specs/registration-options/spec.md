# Spec Delta

## MODIFIED Requirements

### Requirement: Metadata follows clone and override, and is not inherited by aliases
`RootScope.Clone`/`CloneWithOpts` SHALL copy each registration's options. `Override*` SHALL replace the previous registration's options with the new call's options; no options means none. An alias (`Alias`, `AliasNamed`, `Bind`, `BindNamed`) SHALL carry no opt-ins of its own, and SHALL NOT inherit the target's opt-ins.

#### Scenario: Clone, override and alias
```gherkin
Given a root scope with lazy "*app.Server" (Start() error) registered with AsStarter() and DependsOnNamed("db")
When the consumer performs <operation>
Then the registration seen at "<name>" in <container> carries <optins>

Examples:
  | operation                                                    | container | name          | optins                   |
  | Clone()                                                      | clone     | *app.Server   | starter, depends-on "db" |
  | Override[*app.Server](i, newServer)                          | original  | *app.Server   | none                     |
  | Override[*app.Server](i, newServer, DependsOnNamed("cache")) | original  | *app.Server   | depends-on "cache"       |
  | Alias[*app.Server, app.Lifecycle](i)                         | original  | app.Lifecycle | none                     |
  | Alias[*app.Server, app.Lifecycle](i)                         | original  | *app.Server   | starter, depends-on "db" |
```

<!-- cases: N/A 2 overrides: this requirement is itself the override behavior -->
<!-- cases: N/A 3 zero values: Override without options is the "none" row -->
<!-- cases: N/A 4 boundaries: no numeric limits -->
<!-- cases: N/A 8 concurrency: Clone and Override keep the existing scope locking -->
<!-- cases: N/A 9 panics: no new panic paths -->
<!-- cases: 10 compatibility: Clone/Override/Alias behave as before for registrations without options; BREAKING (flagged): As/AsNamed are renamed Alias/AliasNamed (see service-aliases) -->

### Requirement: Repeated options combine predictably
Repeating `DependsOn`/`DependsOnNamed` SHALL accumulate dependencies in the given order, with duplicates collapsed. Repeating `AsStarter()` SHALL be equivalent to giving it once. Applying the same `Kind` twice to one registration SHALL panic at registration, naming the kind and the service.

#### Scenario: Repeated options
```gherkin
Given a root scope
And var scheduled = doger.NewKind[app.Job, Spec]("scheduled")
When the consumer registers lazy "worker" of type *app.Worker (Start() error and Run(ctx) error) with options <options>
Then the outcome is <outcome>

Examples:
  | options                                                          | outcome                                                       |
  | DependsOnNamed("a"), DependsOnNamed("b")                         | depends-on ["a", "b"]                                         |
  | DependsOnNamed("a"), DependsOnNamed("a")                         | depends-on ["a"]                                              |
  | AsStarter(), AsStarter()                                         | starter                                                       |
  | AsRunnable(), AsRunnable()                                       | panic "doger: kind `runnable` applied twice to service `worker`" |
  | scheduled.With(Spec{Every: 1m}), scheduled.With(Spec{Every: 2m}) | panic "doger: kind `scheduled` applied twice to service `worker`" |
```

<!-- cases: N/A 2 overrides: no option changes this combination rule -->
<!-- cases: N/A 7 lifecycle, 8 concurrency: registration-time only -->
<!-- cases: N/A 10 compatibility: new API -->

### Requirement: Invalid opt-ins panic at registration
Registration SHALL panic before the service is stored, naming the service, when `AsStarter()` is applied to a concrete type with no `Starter*` variant, when `AsStarter()` or a kind option is applied to a transient registration, or when a kind option is applied to a concrete type not assignable to the kind's `T`. Interface-typed registrations are checked on the instance at Start instead. `DependsOn*` on a transient SHALL be allowed.

#### Scenario: Invalid opt-in combinations
```gherkin
Given a root scope
And var scheduled = doger.NewKind[doger.Job, Spec]("scheduled"), where doger.Job is a test interface Run(ctx) error
When the consumer registers "<service>" of type <type> with <helper> and option <option>
Then registration <result>
And "<service>" is registered afterwards: <registered>

Examples:
  | service | type                                | helper                | option                  | result                                                                                         | registered |
  | cfg     | *app.Config (no Start)              | ProvideNamed          | AsStarter()             | panics "doger: service `cfg` does not implement any Starter interface"                            | false      |
  | srv     | *app.Server (Start() error)         | ProvideNamedTransient | AsStarter()             | panics "doger: transient service `srv` cannot be started"                                         | false      |
  | tjob    | *app.Job (Run(ctx) error)           | ProvideNamedTransient | scheduled.With(Spec{})  | panics "doger: transient service `tjob` cannot be handled by kind `scheduled`"                    | false      |
  | plain   | *app.Config (no Run)                | ProvideNamed          | scheduled.With(Spec{})  | panics "doger: service `plain` is not assignable to `doger.Job` required by kind `scheduled`"     | false      |
  | job     | *app.Job (Run(ctx) error)           | ProvideNamedTransient | AsRunnable()            | panics "doger: transient service `job` cannot be handled by kind `runnable`"                      | false      |
  | cfg     | *app.Config (no Run)                | ProvideNamed          | AsRunnable()            | panics "doger: service `cfg` is not assignable to `doger.Runner` required by kind `runnable`"     | false      |
  | life    | app.Lifecycle (interface, no Start) | ProvideNamed          | AsStarter()             | succeeds (checked on the instance at Start)                                                    | true       |
  | life    | app.Lifecycle (interface, no Run)   | ProvideNamed          | scheduled.With(Spec{})  | succeeds (checked on the instance at Start)                                                    | true       |
  | req     | *app.Request                        | ProvideNamedTransient | DependsOnNamed("db")    | succeeds                                                                                       | true       |
  | srv     | *app.Server (Start() error)         | ProvideNamedValue     | AsStarter()             | succeeds                                                                                       | true       |
```

<!-- cases: N/A 2 overrides: validation cannot be disabled; opt-ins with an unusable target would silently do nothing -->
<!-- cases: N/A 3 zero values: an empty service name behaves exactly as before; a nil option is ignored (first requirement) -->
<!-- cases: 5 errors: the type in the "not assignable" message is rendered with reflect.Type.String(), so it reads `doger.Runner` -->
<!-- cases: N/A 7 lifecycle, 8 concurrency: registration-time only -->
<!-- cases: N/A 10 compatibility: new API; registrations without options never panic for these reasons -->

### Requirement: AsDefault combines with every other option
`AsDefault()` SHALL be accepted on every registration form (lazy, eager, transient and the `Package` helpers) and with every other option. The other options SHALL apply to the default when it uses its own provider and SHALL have no effect when it yields. Options SHALL be validated before a default is skipped, so an invalid combination panics whether or not the default takes effect.

#### Scenario: Options on a default apply only when it uses its own provider
```gherkin
Given a root scope with child scope "ext"
And a lazy "cfg" in scope "ext" whose provider counts its calls
And a lazy "*app.Pool" registered in scope "ext" with doger.AsDefault() and <options>, implementing Start(ctx) error and Run(ctx) error, never invoked
And <explicit>
When the consumer calls Start on the root
Then the default pool's Start was called <starts> times, its Run <runs> times, and "cfg" was built <cfg_builds> times

Examples:
  | options                                          | explicit                                                   | starts | runs | cfg_builds |
  | doger.AsStarter(), doger.DependsOnNamed("cfg")   | nothing is registered in the root                          | 1      | 0    | 1          |
  | doger.AsStarter(), doger.DependsOnNamed("cfg")   | an explicit "*app.Pool" is in the root                     | 0      | 0    | 0          |
  | doger.AsRunnable()                               | nothing is registered in the root                          | 1      | 1    | 0          |
  | doger.AsRunnable()                               | an explicit "*app.Pool" is in the root                     | 0      | 0    | 0          |
```

#### Scenario: AsDefault adds no registration panic of its own
```gherkin
Given a root scope
When the consumer registers "*app.Dialer" as <form> with <options>
Then the registration <outcome>

Examples:
  | form                           | options                               | outcome                                                                  |
  | doger.Provide                  | doger.AsDefault()                     | does not panic                                                           |
  | doger.ProvideValue             | doger.AsDefault()                     | does not panic                                                           |
  | doger.ProvideTransient         | doger.AsDefault()                     | does not panic                                                           |
  | doger.ProvideNamed "dialer"    | doger.AsDefault()                     | does not panic                                                           |
  | doger.Package(doger.Lazy(...)) | doger.AsDefault()                     | does not panic                                                           |
  | doger.ProvideTransient         | doger.AsDefault(), doger.AsStarter()  | panics with "doger: transient service `*app.Dialer` cannot be started"      |
```

#### Scenario: An invalid option panics even when the default would be skipped
```gherkin
Given a root scope with an explicit lazy "*app.Dialer" in the root
When the consumer registers a transient "*app.Dialer" in the root with doger.AsDefault() and doger.AsStarter()
Then the registration panics with "doger: transient service `*app.Dialer` cannot be started"
```

<!-- cases: N/A 3 zero values: a nil option next to AsDefault is ignored as today -->
<!-- cases: 4 boundaries: one and several combined options are the rows -->
<!-- cases: 5 invalid input: the AsStarter-on-transient rows; the message is the existing one -->
<!-- cases: 6 duplicate: a second default of one name panics as specified in default-registrations -->
<!-- cases: 7 lifecycle: Start rows of the first scenario -->
<!-- cases: N/A 8 concurrency: registration-time only -->
<!-- cases: 9 panics: second and third scenarios -->
<!-- cases: 10 compatibility: other options behave as before when AsDefault is absent -->
