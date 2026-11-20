# Spec Delta

## REMOVED Requirements

### Requirement: Extend installs an extension into a private scope by default
**Reason**: Its scenario "A nil root means DefaultRootScope" depends on `DefaultRootScope`, which this change removes; a nil root is now rejected with `ErrNilInjector`.
**Migration**: See "Extend installs an extension into a private scope of an explicit root by default". Replace `doger.Extend(nil, ext)` with `doger.Extend(root, ext)` on the container created with `doger.New()`.

## MODIFIED Requirements

### Requirement: The author and the installer choose the placement
An extension implementing `ExtensionPlacement` with `PlacementFlat` SHALL register into the root's own scope; `PlacementPrivate`, or no such method, SHALL use the private scope. The installer's `ExtendIntoRoot()` SHALL force the root, and `ExtendScopeName(name)` SHALL rename the private scope. When flattened, `ExtensionExport` SHALL do nothing and return nil.

#### Scenario: Placement by author and installer
```gherkin
Given a root scope
And an extension "redis" declaring placement <author> whose Extend registers lazy "*redis.Pool" and calls doger.ExtensionExport[*redis.Pool](x)
When the consumer calls doger.Extend(root, redisExt, <options>)
Then "*redis.Pool" is registered in <where>
And invoking "*redis.Pool" from the root succeeds

Examples:
  | author            | options                          | where                        |
  | (none)            | (none)                           | child scope "ext:redis"      |
  | PlacementPrivate  | (none)                           | child scope "ext:redis"      |
  | PlacementFlat     | (none)                           | the root's own scope         |
  | (none)            | doger.ExtendIntoRoot()                  | the root's own scope         |
  | PlacementFlat     | doger.ExtendScopeName("cache")   | the root's own scope         |
  | (none)            | doger.ExtendScopeName("cache")   | child scope "cache"          |
```

<!-- cases: 1 default: first row -->
<!-- cases: 2 overrides: author, installer and both rows -->
<!-- cases: 3 zero values: ExtendScopeName("") is an error, covered by "Extend validates its inputs" -->
<!-- cases: N/A 4 boundaries: placement has two values -->
<!-- cases: N/A 5 errors: placement cannot fail -->
<!-- cases: 6 duplicate: a scope name collision is covered by "Identity and repeated installation" -->
<!-- cases: N/A 7 lifecycle, 8 concurrency, 9 panics: placement is decided before Extend runs -->
<!-- cases: N/A 10 compatibility: new API -->

### Requirement: Identity and repeated installation
An extension's identity SHALL be its Go type plus its instance key, the value of its optional `Instance() string` method (`ExtensionInstance`), empty without it. Repeating `Extend` for an installed identity SHALL do nothing when the two extension values are deeply equal and the `ExtendOption`s have the same effect, and SHALL fail with `ErrExtensionConflict` otherwise. A private scope name already used by another scope SHALL fail with `ErrExtensionConflict` instead of panicking.

#### Scenario: Repeated and colliding installations
```gherkin
Given a root scope where doger.Extend(root, <first>) returned nil
When the consumer calls doger.Extend(root, <second>)
Then the result is <result>
And Extend of <first> was called <calls> times in total

Examples:
  | first                                   | second                                   | result                                         | calls |
  | redis.Extension(redis.Addr("a:1"))      | redis.Extension(redis.Addr("a:1"))       | nil                                            | 1     |
  | redis.Extension(redis.Addr("a:1"))      | redis.Extension(redis.Addr("b:2"))       | an error matching ErrExtensionConflict         | 1     |
  | redis.Extension(redis.Instance("cache"))| redis.Extension(redis.Instance("queue")) | nil, with child scope "ext:redis/queue"        | 1     |
  | redis.Extension()                       | other.Extension() also named "redis"     | an error matching ErrExtensionConflict naming scope "ext:redis" | 1 |
  | redis.Extension()                       | redis.Extension() with doger.ExtendIntoRoot()   | an error matching ErrExtensionConflict         | 1     |
  | &redisExt{} (pointer)                   | redisExt{} (value of the same type)      | an error matching ErrExtensionConflict naming scope "ext:redis" | 1 |
  | a metrics extension that declined       | the same metrics value                   | nil                                            | 1     |
  | a metrics extension that declined       | another metrics value                    | an error matching ErrExtensionConflict         | 1     |
```

#### Scenario: A scope-name collision is resolved by the installer
```gherkin
Given a root scope where doger.Extend(root, redis.Extension()) returned nil
When the consumer calls doger.Extend(root, other.Extension(), doger.ExtendScopeName("other-redis")) for an extension also named "redis"
Then it returns nil
And the root has child scopes "ext:redis" and "other-redis"
```

<!-- cases: 2 overrides: ExtendScopeName resolves a collision -->
<!-- cases: N/A 3 zero values: covered by "Extend validates its inputs" -->
<!-- cases: 4 boundaries: same identity, other instance, other type with the same name, other options, pointer versus value (two identities: the guide asks authors to always return a pointer), installed or disabled identity -->
<!-- cases: 5 errors: conflict rows; ErrExtensionConflict is matchable with errors.Is -->
<!-- cases: 6 duplicate: the subject -->
<!-- cases: N/A 7 lifecycle: phases are covered by "Extend follows the container phase" -->
<!-- cases: 8 concurrency: covered by "Concurrent Extend calls install each identity once" -->
<!-- cases: N/A 9 panics: a collision never panics -->
<!-- cases: N/A 10 compatibility: new API -->

### Requirement: A failing Extend leaves nothing behind
When `ext.Extend` returns an error or panics, `Extend` SHALL return an `*ExtensionError` naming the extension and instance and wrapping the cause (a recovered panic as an error), SHALL remove the private scope and export nothing, and SHALL NOT record the identity, so a later `Extend` can retry. Extensions it required stay installed. With `ExtendIntoRoot()` registrations are not rolled back.

#### Scenario: Failure is reported and can be retried
```gherkin
Given a root scope
And an extension "redis" with instance "cache" whose first Extend registers lazy "*redis.Pool", calls doger.ExtensionExport[*redis.Pool](x) and then <fails>, and whose second Extend succeeds
When the consumer calls doger.Extend(root, redisExt) twice
Then the first call returns an error that errors.As finds as *ExtensionError with Name "redis" and Instance "cache"
And the first error <matches>
And after the first call the root has no child scope "ext:redis/cache" and no service "*redis.Pool"
And the second call returns nil and "*redis.Pool" is invokable from the root

Examples:
  | fails                              | matches                                        |
  | returns errors.New("dial failed")  | contains "dial failed"                          |
  | panics with errors.New("boom")     | wraps the panic error "boom" (errors.Is)        |
  | panics with the string "boom"      | contains "boom"                                 |
```

#### Scenario: A flattened extension keeps its partial registrations
```gherkin
Given a root scope
And an extension "redis" whose Extend registers lazy "*redis.Pool" and then returns errors.New("dial failed")
When the consumer calls doger.Extend(root, redisExt, doger.ExtendIntoRoot())
Then the error contains "dial failed"
And "*redis.Pool" is still registered in the root
```

<!-- cases: 2 overrides: ExtendIntoRoot disables rollback (second scenario) -->
<!-- cases: N/A 3 zero values: a nil error is success -->
<!-- cases: N/A 4 boundaries: one failing call followed by one retry -->
<!-- cases: 5 errors: error and panic rows; ExtensionError supports errors.As and Unwrap -->
<!-- cases: 6 duplicate: the retry is not rejected as a repeat -->
<!-- cases: 7 lifecycle: rolling back shuts the private scope down, so its shutdown and scope Closing/Closed hooks fire, as its registration hooks did (design D4); it shuts down with a context that never ends, so no service the attempt built is skipped (shutdown-context) -->
<!-- cases: N/A 8 concurrency: covered by "Concurrent Extend calls install each identity once" -->
<!-- cases: 9 panics: the panic rows -->
<!-- cases: N/A 10 compatibility: new API -->

### Requirement: An extension can decline to install
When `ext.Extend` returns an error matching `ErrExtensionDisabled`, `Extend` SHALL return nil, log one `slog.LevelInfo` record naming the extension and its instance key through `InjectorOpts.Logger`, install nothing, and record the identity as disabled, except where "A declined install that cannot be rolled back cleanly" and "A declined replacement keeps the required install" specify otherwise.

#### Scenario: A disabled extension installs nothing
```gherkin
Given a root scope created with a Logger whose handler records every record
And an extension "metrics" with instance key "<instance>" whose Extend registers lazy "*metrics.Exporter" and returns fmt.Errorf("off by config: %w", doger.ErrExtensionDisabled)
When the consumer calls doger.Extend(root, metricsExt)
Then it returns nil
And exactly one recorded record has level INFO, message "extension disabled" and attribute extension=<extension>
And the root has no child scopes

Examples:
  | instance | extension  |
  |          | metrics    |
  | eu       | metrics/eu |
```

<!-- cases: N/A 2 overrides: whether to disable is the extension author's choice; ExtendIntoRoot is covered by "A declined install that cannot be rolled back cleanly" -->
<!-- cases: 3 zero values: with a nil Logger the decline is discarded, as every record is (container-logging) -->
<!-- cases: 4 boundaries: with and without an instance key -->
<!-- cases: N/A 5 errors: a decline is not an error; the exceptions are their own requirements -->
<!-- cases: 6 duplicate: a repeated Extend of a disabled identity follows "Identity and repeated installation" (disabled rows there) -->
<!-- cases: 7 lifecycle: the declined attempt is shut down and removed -->
<!-- cases: N/A 8 concurrency, 9 panics: as for any Extend -->
<!-- cases: N/A 10 compatibility: new API -->

### Requirement: A declined install that cannot be rolled back cleanly
A flattened extension that declines SHALL keep the registrations it made, as a flattened failure does, and its identity SHALL be recorded as disabled. When shutting a declined attempt down fails, `Extend` SHALL return an `*ExtensionError` wrapping the failed report, which does not match `ErrExtensionDisabled`, and the identity SHALL still be recorded as disabled.

#### Scenario: A flattened declined install keeps its registrations
```gherkin
Given a root scope
And an extension "metrics" whose Extend registers lazy "*metrics.Exporter" and returns doger.ErrExtensionDisabled
When the consumer calls doger.Extend(root, metricsExt, doger.ExtendIntoRoot())
Then it returns nil
And "*metrics.Exporter" is still registered in the root
And a second doger.Extend(root, metricsExt, doger.ExtendIntoRoot()) returns nil
```

#### Scenario: A declined install whose shutdown fails is reported
```gherkin
Given a root scope
And an extension "metrics" whose Extend registers a value "*metrics.Exporter" whose Shutdown returns errors.New("close failed"), then returns doger.ErrExtensionDisabled
When the consumer calls doger.Extend(root, metricsExt)
Then the error is an *ExtensionError named "metrics" containing "close failed"
And the error does not match ErrExtensionDisabled
And the root has no child scopes
And a second doger.Extend(root, metricsExt) returns nil
```

<!-- cases: 2 overrides: ExtendIntoRoot keeps the registrations -->
<!-- cases: N/A 3 zero values, 4 boundaries: no input -->
<!-- cases: 5 errors: the failed shutdown is returned, not matching ErrExtensionDisabled -->
<!-- cases: 6 duplicate: a repeat of the disabled identity is a no-op in both scenarios -->
<!-- cases: 7 lifecycle: nothing is rolled back when flattened; the failed shutdown is reported -->
<!-- cases: N/A 8 concurrency, 9 panics: as for any Extend -->
<!-- cases: N/A 10 compatibility: new API -->

### Requirement: An explicit Extend replaces an unused required install
An explicit `Extend` with another value of an identity installed privately and only through `Require` SHALL replace it while no service of its scope or of a descendant scope was resolved, and fail with `ErrExtensionConflict` otherwise; the replacement MAY rename the scope or flatten. A required install placed flat by its author SHALL never be replaced. If the replacing `Extend` fails, the required install SHALL be installed again from its value.

#### Scenario: An explicit Extend after Require
```gherkin
Given a root scope
And extension "cache" whose Extend calls x.Require(redis.Extension(redis.Addr("default:6379")))
And doger.Extend(root, cacheExt) returned nil
And <step>
When the consumer calls doger.Extend(root, redis.Extension(redis.Addr("prod:6379")))
Then the result is <result>
And the redis client uses address "<addr>"

Examples:
  | step                                          | options                              | result                                  | addr          |
  | nothing was invoked                           | (none)                               | nil                                     | prod:6379     |
  | an unrelated root service "*app.Config" was invoked | (none)                         | nil                                     | prod:6379     |
  | "*redis.Client" was invoked from the root     | (none)                               | an error matching ErrExtensionConflict  | default:6379  |
  | nothing was invoked                           | doger.ExtendScopeName("redis-prod")  | nil, child "redis-prod" and no child "ext:redis" | prod:6379 |
  | nothing was invoked                           | doger.ExtendIntoRoot()                      | nil, no child "ext:redis"               | prod:6379     |
  | a service of a child scope of "ext:redis" was invoked | (none)                       | an error matching ErrExtensionConflict  | default:6379  |
```

#### Scenario: A required install placed flat by its author is never replaced
```gherkin
Given a root scope
And extension "cache" whose Extend calls x.Require(flatRedis.Extension(flatRedis.Addr("default:6379"))) for an extension declaring PlacementFlat
And doger.Extend(root, cacheExt) returned nil
When the consumer calls doger.Extend(root, flatRedis.Extension(flatRedis.Addr("prod:6379")))
Then the error matches ErrExtensionConflict
```

#### Scenario: A failing replacement restores the required install
```gherkin
Given a root scope
And extension "cache" whose Extend calls x.Require(redis.Extension(redis.Addr("default:6379")))
And doger.Extend(root, cacheExt) returned nil
When the consumer calls doger.Extend(root, redis.Extension(redis.Addr("bad:6379"))) whose Extend returns errors.New("dial failed")
Then the error contains "dial failed"
And <then>

Examples:
  | then                                                                                                   |
  | invoking "*redis.Client" from the root returns a client using "default:6379"                           |
  | a later doger.Extend(root, redis.Extension(redis.Addr("prod:6379"))) returns nil, the restored install being unresolved |
```

<!-- cases: 2 overrides: the explicit Extend is the override; another scope name or ExtendIntoRoot -->
<!-- cases: N/A 3 zero values: no input -->
<!-- cases: 4 boundaries: nothing resolved, an unrelated service resolved, a service of the scope or of a descendant resolved; placed flat by the author -->
<!-- cases: 5 errors: conflict rows; a failing replacement -->
<!-- cases: 6 duplicate: an equal explicit Extend is a repeat ("Identity and repeated installation") -->
<!-- cases: 7 lifecycle: a failed replacement restores the required install -->
<!-- cases: 8 concurrency: resolving the required extension's services while an Extend replaces it is unsupported and documented (not reproducible, see proposal Defect) -->
<!-- cases: N/A 9 panics: a panic in the replacing Extend is a failing replacement -->
<!-- cases: N/A 10 compatibility: new API -->

### Requirement: Extend follows the container phase
`Extend` SHALL be allowed while the container is idle, starting, started, ready or failed. Services an extension adds after a successful Start SHALL be started by the next `Refresh`; after a failed Start, `Refresh` fails with `ErrNotStarted` as today. In the closing or closed phase, `Extend` SHALL fail with an error matching `ErrShutdown` and install nothing.

#### Scenario: Extend in each phase
```gherkin
Given a root scope in phase <phase>
And an extension "redis" whose Extend registers lazy "*redis.Pool" implementing Start(ctx) error with doger.AsStarter(), and exports it
When the consumer calls doger.Extend(root, redisExt)
Then the result is <result>

Examples:
  | phase                                     | result                                        |
  | idle                                      | nil                                           |
  | starting (called from a Starting hook)    | nil, and the pool is started by that Start    |
  | started (called from a Started hook)      | nil, and the pool is started by root.Refresh()|
  | ready                                     | nil, and the pool is started by root.Refresh()|
  | failed                                    | nil, the pool is not started, and root.Refresh() fails with ErrNotStarted |
  | closing (called from a Closing hook)      | an error matching ErrShutdown       |
  | closed                                    | an error matching ErrShutdown       |
```

<!-- cases: N/A 2 overrides: phases are not configurable -->
<!-- cases: N/A 3 zero values, 5 invalid input: phase is not an input -->
<!-- cases: 4 boundaries: every phase -->
<!-- cases: N/A 6 duplicate: covered by "Identity and repeated installation" -->
<!-- cases: 7 lifecycle: the subject -->
<!-- cases: 8 concurrency: Extend concurrent with Start is unsupported and documented, as for the hook adders -->
<!-- cases: N/A 9 panics: as for any Extend -->
<!-- cases: 10 compatibility: BREAKING (flagged) - the closing and closed rows match ErrShutdown, which replaces ErrStartAfterShutdown -->

### Requirement: Extension services follow the container lifecycle
Services of an extension's private scope SHALL be started by `Start`, shut down by root `Shutdown` after the root services that depend on their exports, and checked by root `HealthCheck`, keyed `<scope name>/<service name>`; yielded defaults and exports from extension scopes SHALL be skipped there, so each service is checked once. Other child scopes SHALL keep today's root health check.

#### Scenario: Lifecycle of extension services
```gherkin
Given a root scope
And an extension "redis" whose private "*redis.Pool" implements Start(ctx) error, Shutdown() error and HealthCheck() error returning errors.New("pool down"), each counting calls, registered with doger.AsStarter() and exported
And a lazy "*app.App" in the root invoking "*redis.Pool" and recording whether the pool was closed at its own shutdown
And an ordinary child scope "jobs" with a lazy "*app.Job" implementing HealthCheck() error, invoked
When the consumer calls Start, HealthCheck and Shutdown on the root
Then the pool's Start, HealthCheck and Shutdown were each called 1 time
And the health map has key "ext:redis/*redis.Pool" with an error containing "pool down" and no key "*redis.Pool"
And the health map has no key for "*app.Job"
And "*app.App" did not see the pool closed
```

#### Scenario: Only flattened extensions leave the root health check unchanged
```gherkin
Given a root scope
And an extension "redis" installed with doger.ExtendIntoRoot() whose lazy "*redis.Pool" implements HealthCheck() error and was invoked
And an ordinary child scope "jobs" with a lazy "*app.Job" implementing HealthCheck() error, invoked
When the consumer calls HealthCheck on the root
Then the health map has key "*redis.Pool" and no key starting with "ext:"
And the health map has no key for "*app.Job"
```

<!-- cases: N/A 2 overrides: lifecycle is not configurable; ExtendIntoRoot puts the services in the root, where they follow the ordinary rules (flattened scenario) -->
<!-- cases: N/A 3 zero values, 4 boundaries, 5 invalid input: no input -->
<!-- cases: 6 duplicate: each service checked once -->
<!-- cases: 7 lifecycle: the subject; a root Shutdown whose context ended skips extension-scope services exactly as shutdown-context specifies, with no extension-specific rule -->
<!-- cases: N/A 8 concurrency: existing parallel start, health check and shutdown apply -->
<!-- cases: N/A 9 panics: unchanged -->
<!-- cases: 10 compatibility: BEHAVIOR CHANGE (flagged) - root HealthCheck output gains extension-scope keys only in containers that use extensions; ordinary child scopes are unchanged ("jobs" row) -->

## ADDED Requirements

### Requirement: Extend installs an extension into a private scope of an explicit root by default
`Extend(root, ext, opts...)` SHALL call `ext.Extend(x)` once with an `ExtensionContext` whose `Scope()` is a new child scope of the root named `ext:<Name()>`, or `ext:<Name()>/<instance>` when the extension has an instance key. Services registered there SHALL be invisible from the root except through exports made with `ExtensionExport`/`ExtensionExportNamed`. A nil root, including a typed nil, SHALL give `ErrNilInjector`.

#### Scenario: Default private layout with an export
```gherkin
Given a root scope
And an extension "redis" whose Extend registers lazy "*redis.Pool" and lazy "*redis.Client" in x.Scope() and calls doger.ExtensionExport[*redis.Client](x)
When the consumer calls doger.Extend(root, redisExt)
Then it returns nil
And the root has a child scope named "<scope>"
And invoking "*redis.Client" from the root succeeds
And invoking "*redis.Pool" from the root fails with an error matching ErrServiceNotFound

Examples:
  | instance | scope              |
  | (none)   | ext:redis          |
  | cache    | ext:redis/cache    |
```

#### Scenario: The context describes the installation
```gherkin
Given a root scope
And an extension "redis" with instance key "<instance>" whose Extend records x.Root(), x.Instance() and x.Scope().Name()
When the consumer calls doger.Extend(root, redisExt)
Then the recorded root is the root scope, the instance is "<instance>" and the scope name is "<scope>"

Examples:
  | instance | scope           |
  |          | ext:redis       |
  | cache    | ext:redis/cache |
```

#### Scenario: A nil root is rejected
```gherkin
Given an extension "redis" whose Extend counts its calls
When the consumer calls doger.Extend(<root>, redisExt)
Then the error matches ErrNilInjector
And redisExt's Extend was called 0 times

Examples:
  | root                    |
  | nil                     |
  | (*doger.RootScope)(nil) |
```

<!-- cases: 2 overrides: covered by "The author and the installer choose the placement" -->
<!-- cases: 3 zero values: nil and typed-nil root rows of "A nil root is rejected"; nil extension and empty name are covered by "Extend validates its inputs" -->
<!-- cases: 4 boundaries: with and without an instance key -->
<!-- cases: 5 errors: covered by "Extend validates its inputs" and "A failing Extend leaves nothing behind" -->
<!-- cases: 6 duplicate: covered by "Identity and repeated installation" -->
<!-- cases: 7 lifecycle: covered by "Extend follows the container phase" and "Extension services follow the container lifecycle" -->
<!-- cases: 8 concurrency: covered by "Concurrent Extend calls install each identity once" -->
<!-- cases: 9 panics: covered by "A failing Extend leaves nothing behind" -->
<!-- cases: 10 compatibility: BREAKING (flagged) - a nil root used to mean DefaultRootScope, which this change removes; migration: pass the container created with doger.New() -->
