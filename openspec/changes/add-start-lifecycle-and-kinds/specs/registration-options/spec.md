# Spec Delta

## Purpose

Lets a consumer attach opt-ins and metadata (starter opt-in, declared dependencies, component kinds) to a service at registration time, with predictable behavior under clone, override and alias.

## ADDED Requirements

### Requirement: Registration functions accept options without breaking existing calls
Every `Provide*`, `Override*` and `Package` helper (`Lazy*`, `Eager*`, `Transient*`) SHALL accept a trailing variadic `...ProvideOption`. A call without options SHALL behave exactly as before this change. A nil option in the list SHALL be ignored.

#### Scenario: Registration with and without options
```gherkin
Given a root scope
When the consumer registers "<service>" with <helper> and options <options>
Then invoking "<service>" returns the provided value "ok"
And the registration carries the opt-ins <optins>

Examples:
  | helper                    | service      | options                   | optins             |
  | doger.Provide             | *app.Server  | none                      | none               |
  | doger.ProvideNamed        | srv          | doger.AsStarter()         | starter            |
  | doger.ProvideValue        | *app.Config  | nil                       | none               |
  | doger.ProvideNamedValue   | cfg          | nil, doger.AsStarter()    | starter            |
  | doger.Lazy (via Package)  | *app.Server  | doger.AsRunnable()        | runnable           |
  | doger.EagerNamed          | srv          | doger.DependsOnNamed("db")| depends-on db      |
```

#### Scenario: Function values of the old signature no longer compile
```gherkin
Given consumer code "var f func(doger.Injector, doger.Provider[int]) = doger.Provide[int]"
When the consumer compiles against this version
Then compilation fails because the signature now ends with "...doger.ProvideOption"
```

<!-- cases: N/A 4 boundaries: options are an unbounded list with no limits; 0 options and 1 option are covered by the Examples rows -->
<!-- cases: N/A 7 lifecycle: options only exist at registration time; lifecycle effects are specified in container-start, component-kinds and dependency-declaration -->
<!-- cases: N/A 8 concurrency: registration concurrency is unchanged from the existing scope locking -->
<!-- cases: 9 panics: invalid option combinations are covered by "Invalid opt-ins panic at registration" -->
<!-- cases: 10 compatibility: covered by both scenarios above (call sites unchanged, function-value assignment is the documented break) -->

### Requirement: Options are opaque and constructed only by the library
`ProvideOption` SHALL be an opaque type whose values come only from library constructors: `AsStarter()`, `DependsOn[T]()`, `DependsOnNamed(name)`, `AsRunnable()` and `Kind[T, M].With(meta)`. Consumers SHALL NOT be able to create options that bypass the validation those constructors perform.

#### Scenario: Each constructor yields an option recognized by registration
```gherkin
Given a root scope
When the consumer registers a lazy "*app.Server" (which implements Start(ctx) error and Run(ctx) error) with option <option>
Then the registration carries opt-in <optin>

Examples:
  | option                                   | optin                         |
  | doger.AsStarter()                        | starter                       |
  | doger.DependsOn[*app.DB]()               | depends-on "*app.DB"          |
  | doger.DependsOnNamed("primary-db")       | depends-on "primary-db"       |
  | doger.AsRunnable()                       | kind "runnable" with meta {}  |
  | scheduled.With(Spec{Every: time.Minute}) | kind "scheduled" with meta {Every: 1m} |
```

<!-- cases: N/A 2 overrides: there is nothing to override; this requirement constrains the option type itself -->
<!-- cases: N/A 3 zero values: a zero ProvideOption is nil and is covered by the previous requirement (nil ignored) -->
<!-- cases: N/A 4 boundaries, 6 duplicates: covered by "Invalid opt-ins panic at registration" and "Repeated options" -->
<!-- cases: N/A 7 lifecycle, 8 concurrency, 9 panics: constructing an option is a pure value creation -->
<!-- cases: N/A 10 compatibility: new API -->

### Requirement: Repeated options combine predictably
Repeating `DependsOn`/`DependsOnNamed` SHALL accumulate dependencies in the given order, with duplicates collapsed. Repeating `AsStarter()` SHALL be equivalent to giving it once. Applying the same `Kind` twice to one registration SHALL panic at registration, naming the kind and the service.

#### Scenario: Repeated options
```gherkin
Given a root scope
When the consumer registers lazy "worker" with options <options>
Then the outcome is <outcome>

Examples:
  | options                                                        | outcome                                                       |
  | DependsOnNamed("a"), DependsOnNamed("b")                       | depends-on ["a", "b"]                                         |
  | DependsOnNamed("a"), DependsOnNamed("a")                       | depends-on ["a"]                                              |
  | AsStarter(), AsStarter()                                       | starter                                                       |
  | AsRunnable(), AsRunnable()                                     | panic "DI: kind `runnable` applied twice to service `worker`" |
  | scheduled.With(Spec{Every: 1m}), scheduled.With(Spec{Every: 2m}) | panic "DI: kind `scheduled` applied twice to service `worker`" |
```

<!-- cases: N/A 2 overrides: no option changes this combination rule -->
<!-- cases: N/A 7 lifecycle, 8 concurrency: registration-time only -->
<!-- cases: N/A 10 compatibility: new API -->

### Requirement: Invalid opt-ins panic at registration
Registration SHALL panic immediately, naming the service, when:
- `AsStarter()` is applied to a concrete (non-interface) registered type that implements no `Starter*` variant. Interface types are checked on the instance at Start instead;
- `AsStarter()` or a kind option is applied to a transient registration;
- a kind option is applied to a type not assignable to the kind's target type `T`.

`DependsOn*` on a transient registration SHALL be allowed.

#### Scenario: Invalid opt-in combinations
```gherkin
Given a root scope
When the consumer registers "<service>" of type <type> with <helper> and option <option>
Then registration <result>

Examples:
  | service | type                         | helper                  | option                   | result                                                              |
  | cfg     | *app.Config (no Start)       | ProvideNamed            | AsStarter()              | panics "DI: service `cfg` does not implement any Starter interface" |
  | srv     | *app.Server (Start() error)  | ProvideNamedTransient   | AsStarter()              | panics "DI: transient service `srv` cannot be started"              |
  | job     | *app.Job (Run(ctx) error)    | ProvideNamedTransient   | AsRunnable()             | panics "DI: transient service `job` cannot be handled by kind `runnable`" |
  | cfg     | *app.Config (no Run)         | ProvideNamed            | AsRunnable()             | panics "DI: service `cfg` is not assignable to `doger.Runner` required by kind `runnable`" |
  | life    | app.Lifecycle (interface, no Start) | ProvideNamed     | AsStarter()              | succeeds (checked on the instance at Start)                         |
  | req     | *app.Request                 | ProvideNamedTransient   | DependsOnNamed("db")     | succeeds                                                            |
  | srv     | *app.Server (Start() error)  | ProvideNamedValue       | AsStarter()              | succeeds                                                            |
```

<!-- cases: N/A 2 overrides: validation cannot be disabled; opt-ins with an unusable target would silently do nothing -->
<!-- cases: N/A 3 zero values: an empty service name already panics or not exactly as before; a nil option is ignored (first requirement) -->
<!-- cases: N/A 7 lifecycle, 8 concurrency: registration-time only -->
<!-- cases: N/A 10 compatibility: new API; registrations without options never panic for these reasons -->

### Requirement: Metadata follows clone and override, and is not inherited by aliases
`RootScope.Clone`/`CloneWithOpts` SHALL copy each registration's options. `Override*` SHALL replace the previous registration's options with the new call's options; no options means none. An alias (`As`, `AsNamed`, `Bind`, `BindNamed`) SHALL carry no opt-ins of its own, and SHALL NOT inherit the target's opt-ins.

#### Scenario: Clone, override and alias
```gherkin
Given a root scope with lazy "*app.Server" registered with AsStarter() and DependsOnNamed("db")
When the consumer performs <operation>
Then the registration seen at "<name>" in <container> carries <optins>

Examples:
  | operation                                              | container | name        | optins                      |
  | Clone()                                                | clone     | *app.Server | starter, depends-on "db"    |
  | Override[*app.Server](i, newServer)                    | original  | *app.Server | none                        |
  | Override[*app.Server](i, newServer, DependsOnNamed("cache")) | original | *app.Server | depends-on "cache"    |
  | As[*app.Server, app.Lifecycle](i)                      | original  | app.Lifecycle | none                      |
  | As[*app.Server, app.Lifecycle](i)                      | original  | *app.Server | starter, depends-on "db"    |
```

<!-- cases: N/A 2 overrides: this requirement is itself the override behavior -->
<!-- cases: N/A 3 zero values: Override without options is the "none" row -->
<!-- cases: N/A 4 boundaries: no numeric limits -->
<!-- cases: N/A 8 concurrency: Clone and Override keep the existing scope locking -->
<!-- cases: N/A 9 panics: no new panic paths -->
<!-- cases: 10 compatibility: Clone/Override/As behave as before for registrations without options -->

### Requirement: Registration order is recorded per container
Each registration SHALL receive a monotonically increasing registration index, unique within its root container and its descendant scopes. `Override*` of an existing name SHALL keep that name's original index, so re-registering does not move it in deterministic orderings.

#### Scenario: Registration index
```gherkin
Given a root scope and a child scope "request"
When the consumer registers in order: lazy "a" in root, lazy "b" in "request", lazy "c" in root, then overrides "a"
Then the registration indexes are a=0, b=1, c=2
```

<!-- cases: N/A 2 overrides: Override is the covered row -->
<!-- cases: N/A 3 zero values: the first registration's index is 0 (covered) -->
<!-- cases: N/A 5 errors: indexing cannot fail -->
<!-- cases: N/A 9 panics: none -->
<!-- cases: 8 concurrency: concurrent registrations from many goroutines receive distinct indexes; covered by a race test listed in design.md -->
<!-- cases: N/A 10 compatibility: internal observable only through ordering guarantees -->
