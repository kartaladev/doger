# Spec Delta

## Purpose

Lets a consumer introduce a new component type (for example "scheduled job") whose opted-in services a dedicated handler receives at container start, and ships the built-in runnable kind on the same mechanism.

## ADDED Requirements

### Requirement: Kinds are typed and opted into at registration
`NewKind[T, M](name)` SHALL create a kind with target type `T`, metadata type `M` and a name used in errors. `kind.With(meta)` SHALL return a `ProvideOption` that opts the registration into the kind with that metadata. Two kinds created with the same name SHALL still be distinct kinds.

#### Scenario: Opting a service into a custom kind
```gherkin
Given var scheduled = doger.NewKind[Job, Spec]("scheduled")
And a root scope with lazy "cleanup" of type *CleanupJob (implements Job) registered with scheduled.With(Spec{Every: time.Minute})
And a StageBeforeStart handler for scheduled that records what it receives
When the consumer calls Start()
Then the handler received exactly 1 component
And the component has Name "cleanup", Scope "[root]", Meta Spec{Every: 1m}
And the component Instance is the same *CleanupJob instance Invoke returns
```

#### Scenario: Same name, distinct kinds
```gherkin
Given k1 := doger.NewKind[Job, Spec]("scheduled") and k2 := doger.NewKind[Job, Spec]("scheduled")
And lazy "a" registered with k1.With(Spec{}) and lazy "b" registered with k2.With(Spec{})
And handlers for k1 and k2 recording names
When the consumer calls Start()
Then the k1 handler received "a" only
And the k2 handler received "b" only
```

<!-- cases: 3 zero values: NewKind with name "" is allowed and errors show "``"; zero metadata Spec{} is a valid value (covered by the second scenario) -->
<!-- cases: 6 duplicates: applying one kind twice to one registration is covered in registration-options ("Repeated options") -->
<!-- cases: 9 panics: assignability and transient panics are covered in registration-options ("Invalid opt-ins panic at registration") -->
<!-- cases: N/A 7 lifecycle, 8 concurrency: kind creation is a pure value -->
<!-- cases: N/A 10 compatibility: new API -->

### Requirement: One handler per kind, staged around Starters
`kind.Handle(root, stage, handler)` SHALL register the kind's handler on the root container. A later call for the same kind SHALL replace both handler and stage. `StageBeforeStart` handlers SHALL run after all candidates are built and before any Starter starts. `StageAfterStart` handlers SHALL run after all Starters have started.

#### Scenario: Handler stage relative to Starters
```gherkin
Given a root scope recording events
And Starter "server"
And lazy "job" opted into kind k
And k.Handle(root, <stage>, handler recording "handle job")
When the consumer calls Start()
Then the recorded events are <events>

Examples:
  | stage            | events                                     |
  | StageBeforeStart | build server, build job, handle job, start server |
  | StageAfterStart  | build server, build job, start server, handle job |
```

#### Scenario: Replacing a handler
```gherkin
Given lazy "job" opted into kind k
And k.Handle(root, StageBeforeStart, handler recording "first")
And then k.Handle(root, StageAfterStart, handler recording "second")
When the consumer calls Start()
Then only "second" was recorded, after all Starters started
```

#### Scenario: Kind without a handler
```gherkin
Given lazy "job" opted into kind k and no handler registered for k
When the consumer calls Start()
Then Start returns nil
And "job" was built
```

<!-- cases: 3 zero values: a nil handler passed to Handle panics immediately with "DI: nil handler for kind `<name>`" (row in design test strategy) -->
<!-- cases: N/A 4 boundaries: 0, 1 and many components per kind follow the same loop; covered by ordering scenario -->
<!-- cases: N/A 8 concurrency: handlers run sequentially on the Start goroutine -->
<!-- cases: N/A 10 compatibility: new API -->

### Requirement: Handler invocation order and failure
Handlers in one stage SHALL run kind by kind, in the order kinds were first given a handler. Within a kind, components SHALL be passed in dependency order, then scope depth, then registration index. The first handler error, or a recovered handler panic, SHALL stop `Start` with an error naming the kind and service and wrapping the cause.

#### Scenario: Components are passed in deterministic order
```gherkin
Given kind k with a handler recording names
And lazy "c" opted into k, registered first, with DependsOnNamed("a")
And lazy "a" opted into k, registered second
And child scope "s" with lazy "b" opted into k
When the consumer calls Start()
Then the handler received a, c, b
```

#### Scenario: Handler failure stops Start
```gherkin
Given kind k with components "a" then "b"
And the k handler <behavior> for "a"
When the consumer calls Start()
Then the error message contains "kind `k`" and "service `a`"
And errors.Is(err, <cause>) is <is>
And the handler was not called for "b"
And no Starter was started when the stage is StageBeforeStart

Examples:
  | behavior               | cause    | is    |
  | returns errBoom        | errBoom  | true  |
  | panics with "kaboom"   | n/a      | false |
```

<!-- cases: N/A 2 overrides: order is controlled by declaring edges or registration order -->
<!-- cases: 8 concurrency: the handler context is the Start context; cancellation is covered by container-start ("Start honours its context") -->
<!-- cases: N/A 10 compatibility: new API -->

### Requirement: Handlers can declare dependencies
A handler SHALL be able to call `DeclareDependency` during Start. Edges declared in `StageBeforeStart` SHALL affect the order in which Starters start in the same `Start` call, and both stages SHALL affect shutdown order.

#### Scenario: Scheduler handler orders start and shutdown
```gherkin
Given Starter "scheduler" with Shutdown() error
And lazy "job" opted into kind scheduled, implementing Shutdown() error
And a StageBeforeStart handler for scheduled that calls DeclareDependency(c.Scope, "scheduler", c.Name)
When the consumer calls Start() and then Shutdown()
Then "job" was built before "scheduler" started
And the shutdown order is scheduler, job
```

<!-- cases: 5 errors: DeclareDependency errors returned from the handler are covered by "Handler failure stops Start" (errBoom row generalizes) -->
<!-- cases: N/A 3, 4, 9: covered in dependency-declaration -->
<!-- cases: N/A 10 compatibility: new API -->

### Requirement: Built-in Runnable kind
The library SHALL provide `Runner` (`Run(context.Context) error`), the kind `Runnable` (`Kind[Runner, struct{}]`) and `AsRunnable()`. By default, `Runnable` SHALL have a `StageAfterStart` handler that calls `Run` once per opted-in service, in handler order, with the Start context, and stops at the first error.

#### Scenario: Runners run after Starters
```gherkin
Given a root scope recording events
And Starter "server"
And lazy "migrate" registered with AsRunnable() and lazy "warmup" registered with AsRunnable(), in that order
When the consumer calls Start()
Then the recorded events are start server, run migrate, run warmup
```

#### Scenario: Runner failure
```gherkin
Given runnables "a" (returns errMigrate) and "b"
When the consumer calls Start()
Then errors.Is(err, errMigrate) is true
And the error message contains "kind `runnable`" and "service `a`"
And "b" did not run
```

#### Scenario: Runner without opt-in is not run
```gherkin
Given a root scope created with EnableRunnerAutoDetection = <enabled>
And lazy "job" whose type has Run(context.Context) error, registered without options
When the consumer calls Start()
Then "job" ran <runs> times

Examples:
  | enabled | runs |
  | false   | 0    |
  | true    | 1    |
```

#### Scenario: Replacing the Runnable handler
```gherkin
Given runnable "a"
And doger.Runnable.Handle(root, StageAfterStart, handler that runs "a" in a goroutine and returns nil)
When the consumer calls Start()
Then Start returns before "a" finishes
And the default handler did not run
```

<!-- cases: 3 zero values: EnableRunnerAutoDetection zero value false is the default row -->
<!-- cases: 7 lifecycle: auto-detection excludes transients and aliases, as for Starters; covered by a row in design test strategy -->
<!-- cases: 8 concurrency: a consumer handler that spawns goroutines owns their lifetime; the default handler runs inline, so no goroutine leaks (goleak) -->
<!-- cases: N/A 10 compatibility: new API -->
