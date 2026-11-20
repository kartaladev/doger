# component-kinds Specification

## Purpose

Lets a consumer introduce a new component type (for example "scheduled job") whose opted-in services a dedicated handler receives at container start, and ships the built-in runnable kind on the same mechanism.

## Requirements

### Requirement: Kinds are typed and opted into at registration
`NewKind[T, M](name)` SHALL create a kind with target type `T`, metadata type `M` and a name used in errors. `kind.With(meta)` SHALL return a `ProvideOption` that opts the registration into the kind with that metadata. Two kinds created with the same name SHALL still be distinct kinds. A registration under an interface type not assignable to `T` SHALL be checked on its instance at Start.

#### Scenario: Opting a service into a custom kind
```gherkin
Given var scheduled = doger.NewKind[Job, Spec]("scheduled")
And a root scope with lazy "cleanup" of type *CleanupJob (implements Job) registered with scheduled.With(Spec{Every: time.Minute})
And a StageBeforeStart handler for scheduled that records what it receives
When the consumer calls Start()
Then the handler received exactly 1 component
And the component has Name "cleanup", Scope named "[root]", Meta Spec{Every: 1m}
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

#### Scenario: Kind opt-in on an interface-typed registration
```gherkin
Given var scheduled = doger.NewKind[Job, Spec]("scheduled")
And lazy "life" registered under interface type app.Lifecycle (not assignable to Job) with scheduled.With(Spec{})
And its provider returns <instance>
And a StageBeforeStart handler for scheduled recording names
When the consumer calls Start()
Then Start returns <result>
And the handler received <received>

Examples:
  | instance                       | result                                                                                      | received |
  | a *CleanupJob (implements Job) | nil                                                                                         | "life"   |
  | an *app.Plain (no Run method)  | the error "doger: failed to build service `life` in scope `[root]`: service `life` does not implement `doger.Job` required by kind `scheduled`" | nothing  |
```

#### Scenario: Invalid kind values panic
```gherkin
Given a root scope and var zero doger.Kind[Job, Spec] (not created with NewKind)
And k := doger.NewKind[Job, Spec]("k")
When the consumer calls <call>
Then it panics with "<message>"

Examples:
  | call                                                   | message                                   |
  | zero.With(Spec{})                                      | doger: kind not created with NewKind      |
  | zero.Handle(root, doger.StageBeforeStart, h)           | doger: kind not created with NewKind      |
  | k.Handle(root, doger.Stage(0), h)                      | doger: invalid stage 0 for kind `k`       |
  | k.Handle(root, doger.Stage(7), h)                      | doger: invalid stage 7 for kind `k`       |
  | k.Handle(root, doger.StageBeforeStart, nil)            | doger: nil handler for kind `k`           |
```

<!-- cases: 3 zero values: NewKind with name "" is allowed and errors show "``"; zero metadata Spec{} is a valid value (second scenario); a zero Kind, the zero Stage and a nil handler panic ("Invalid kind values panic"); zero.Name() returns "" -->
<!-- cases: 4 boundaries: Stage(0) just below StageBeforeStart and Stage(7) past StageAfterStart -->
<!-- cases: 5 errors: an interface-typed registration whose instance is not a T fails Start ("Kind opt-in on an interface-typed registration") -->
<!-- cases: 6 duplicates: applying one kind twice to one registration is covered in registration-options ("Repeated options") -->
<!-- cases: 9 panics: assignability and transient panics are covered in registration-options ("Invalid opt-ins panic at registration") -->
<!-- cases: N/A 7 lifecycle, 8 concurrency: kind creation is a pure value -->
<!-- cases: 10 compatibility: BREAKING (flagged) - Stage(0) was StageBeforeStart and now panics; messages use the "doger: " prefix -->

### Requirement: Handler invocation order and failure
Handlers in one stage SHALL run kind by kind, in the order kinds were first given a handler. Within a kind, components SHALL be passed in dependency order, then scope depth, then registration order. The first handler error, or a recovered handler panic, SHALL stop `Start` with an error naming the kind and service and wrapping the cause.

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
Given lazy Starter "server"
And kind k with components "a" then "b" and its handler registered at <stage>
And the k handler <behavior> for "a"
When the consumer calls Start()
Then the error message contains "kind `k`" and "service `a`"
And <cause-check>
And the handler was not called for "b"
And "server" was started <starts> times

Examples:
  | stage            | behavior             | cause-check                         | starts |
  | StageBeforeStart | returns errBoom      | errors.Is(err, errBoom) is true     | 0      |
  | StageBeforeStart | panics with "kaboom" | the error message contains "kaboom" | 0      |
  | StageAfterStart  | returns errBoom      | errors.Is(err, errBoom) is true     | 1      |
```

<!-- cases: N/A 2 overrides: order is controlled by declaring edges or registration order -->
<!-- cases: 8 concurrency: the handler context is the Start context; cancellation is covered by container-start ("Start honours its context", "Shutdown cancels a running Start") -->
<!-- cases: N/A 10 compatibility: new API -->

### Requirement: Handlers can declare dependencies
A handler SHALL be able to call `DeclareDependency` during Start. Edges declared in `StageBeforeStart` SHALL affect the order in which Starters start in the same `Start` call, and edges from both stages SHALL affect shutdown order.

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
<!-- cases: 7 lifecycle: cross-scope edges are restricted by dependency-declaration ("Declaring edges dynamically") -->
<!-- cases: N/A 10 compatibility: new API -->

### Requirement: Clones copy kind handlers
`RootScope.Clone()` and `CloneWithOpts(opts)` SHALL copy the source's kind handler registrations, including consumer replacements of the default handler of the built-in `Runnable()` kind, with their stages and order. Registering a handler on the clone afterwards SHALL replace it on the clone only.

#### Scenario: Clone keeps replaced handlers
```gherkin
Given a root scope with lazy "job" opted into kind k and runnable "migrate"
And k.Handle(root, StageBeforeStart, handler recording "k <name>")
And Runnable().Handle(root, StageAfterStart, handler recording "custom <name>")
When the consumer calls Start() on <container>
Then the recorded events are <events>

Examples:
  | container                                                                    | events                    |
  | root.Clone()                                                                 | k job, custom migrate     |
  | root.CloneWithOpts(&InjectorOpts{})                                          | k job, custom migrate     |
```

#### Scenario: Replacing a handler on the clone leaves the source unchanged
```gherkin
Given a root scope with lazy "job" opted into kinds k and j
And k.Handle(root, StageBeforeStart, handler recording "k source"), then j.Handle(root, StageBeforeStart, handler recording "j")
And clone := root.Clone() with k.Handle(clone, StageBeforeStart, handler recording "k clone")
When the consumer calls Start() on <container>
Then the recorded events are <events>

Examples:
  | container | events      |
  | root      | k source, j |
  | clone     | k clone, j  |
```

<!-- cases: 1 default: a clone of a container without replaced handlers keeps the default Runnable behavior (first row's runnable part) -->
<!-- cases: 2 overrides: re-registering on the clone -->
<!-- cases: N/A 3 zero values: a container with no handlers clones to none -->
<!-- cases: N/A 4 boundaries: one and two kinds -->
<!-- cases: N/A 5 errors: no error path -->
<!-- cases: 6 duplicates: replacement on the clone keeps the slot order (the clone row: k still runs before j) -->
<!-- cases: 7 lifecycle: clone of a started container starts independently (container-start) -->
<!-- cases: N/A 8 concurrency: handlers are configuration-time -->
<!-- cases: N/A 9 panics: no new path -->
<!-- cases: 10 compatibility: BEHAVIOR CHANGE (flagged) - before, a clone got only the default Runnable handler; the design listed this as an open question. Runnable is now called as a function (Runnable()) -->

### Requirement: Handlers run for new components on incremental start
`Refresh` and `Scope.Start` SHALL call each stage's handler for the components that have not been handled yet, in the same order rules as `Start`. A component SHALL be handled successfully at most once per kind per container: a handler call that failed is not recorded, so the next Refresh or Scope.Start that covers the component SHALL call that handler for it again.

#### Scenario: Refresh handles only new components
```gherkin
Given a root scope with lazy "a" opted into kind k and k.Handle(root, StageBeforeStart, handler recording names)
And the consumer called Start()
And then the consumer registers lazy "b" opted into k
When the consumer calls Refresh() twice
Then the handler recorded a, b
```

#### Scenario: Runners run once
```gherkin
Given a root scope with runnable "migrate" and the consumer called Start()
And then the consumer registers runnable "seed"
When the consumer calls Refresh()
Then "migrate" ran 1 time in total
And "seed" ran 1 time
```

#### Scenario: Scope.Start handles only its subtree
```gherkin
Given a started root container with k.Handle(root, StageBeforeStart, handler recording names)
And then the consumer registers lazy "r" opted into k in the root, and creates child scope "tenant" with lazy "t" opted into k
When the consumer calls tenant.Start()
Then the handler recorded t
When the consumer calls Refresh()
Then the handler recorded t, r
```

#### Scenario: A failed handler is called again
```gherkin
Given a started root container with k.Handle(root, StageBeforeStart, handler) where the handler returns errBoom on its first call and nil afterwards
And then the consumer registers lazy "b" opted into k
When the consumer calls Refresh() three times
Then the calls return an error matching errBoom, nil, nil
And the handler was called 2 times for "b"
```

<!-- cases: 1 default: the default Runnable handler on Refresh -->
<!-- cases: N/A 2 overrides: replaced handlers follow the same rule -->
<!-- cases: N/A 3 zero values: no new component calls no handler -->
<!-- cases: 4 boundaries: zero, one new component; repeated Refresh -->
<!-- cases: 5 errors: a handler error fails Refresh as it fails Start (StartError Op "handle"); the handler is called again on the next run ("A failed handler is called again") -->
<!-- cases: 6 duplicates: at most once per kind per container; only a successful call counts -->
<!-- cases: 7 lifecycle: Scope.Start restricts components to its subtree, as for Starters ("Scope.Start handles only its subtree") -->
<!-- cases: N/A 8 concurrency: covered by container-start "Concurrent Refresh calls are serialized" -->
<!-- cases: N/A 9 panics: recovered as for Start -->
<!-- cases: N/A 10 compatibility: new API -->

### Requirement: One handler per kind on an explicit root, staged around Starters
`kind.Handle(root, stage, handler)` SHALL register the kind's handler on the root container. A nil root, including a typed-nil `*RootScope`, SHALL make `Handle` panic with an error matching `ErrNilInjector`; there is no default root scope. A later call for the same kind SHALL replace both handler and stage. `StageBeforeStart` handlers SHALL run after all candidates are built and before any Starter starts. `StageAfterStart` handlers SHALL run after all Starters have started.

#### Scenario: Handler stage relative to Starters
```gherkin
Given a root scope recording events
And Starter "server" registered with AsStarter() before lazy "job" opted into kind k
And k.Handle(root, <stage>, handler recording "handle job")
When the consumer calls Start()
Then the recorded events are <events>

Examples:
  | stage            | events                                            |
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

#### Scenario: Nil root is rejected
```gherkin
Given k := doger.NewKind[Job, Spec]("k")
When the consumer calls k.Handle(<root>, StageBeforeStart, handler recording names)
Then it panics with an error for which errors.Is(err, doger.ErrNilInjector) is true
And no handler is registered on any container

Examples:
  | root                    |
  | nil                     |
  | (*doger.RootScope)(nil) |
```

#### Scenario: Kind without a handler
```gherkin
Given lazy "job" opted into kind k and no handler registered for k
When the consumer calls Start()
Then Start returns nil
And "job" was built
```

<!-- cases: 3 zero values: a nil handler and an invalid stage are rows of "Invalid kind values panic"; a nil and a typed-nil root are rows of "Nil root is rejected" -->
<!-- cases: N/A 4 boundaries: 0, 1 and many components per kind follow the same loop; covered by the ordering scenario -->
<!-- cases: 5 errors: the nil-root panic value matches ErrNilInjector with errors.Is -->
<!-- cases: N/A 8 concurrency: handlers run sequentially on the Start goroutine -->
<!-- cases: 10 compatibility: BREAKING (flagged) - a nil root used to mean DefaultRootScope, which no longer exists; migration: pass the container created with doger.New() -->

### Requirement: Built-in Runnable kind returned by a function
The library SHALL provide `Runner` (`Run(context.Context) error`), the function `Runnable()` returning the built-in kind (`Kind[Runner, struct{}]`, the same comparable value on every call) and `AsRunnable()`. A runner is one-shot: `Run` MUST return. By default, the built-in kind SHALL have a `StageAfterStart` handler that calls `Run` once per opted-in service, in handler order, with the Start context, and stops at the first error.

#### Scenario: Runners run after Starters
```gherkin
Given a root scope recording events
And Starter "server" registered with AsStarter()
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

#### Scenario: Runner auto-detection
```gherkin
Given a root scope created with EnableRunnerAutoDetection = <enabled>
And "job" whose type has Run(context.Context) error, registered as <registration>
When the consumer calls Start()
Then "job" ran <runs> times

Examples:
  | enabled | registration                                       | runs |
  | false   | lazy, no options                                   | 0    |
  | true    | lazy, no options                                   | 1    |
  | true    | lazy, with AsRunnable()                            | 1    |
  | true    | transient, no options                              | 0    |
  | true    | lazy, plus Alias[*app.Job, doger.Runner] alias     | 1    |
```

#### Scenario: Replacing the Runnable handler
```gherkin
Given runnable "a"
And doger.Runnable().Handle(root, StageAfterStart, handler that runs "a" in a goroutine and returns nil)
When the consumer calls Start()
Then Start returns before "a" finishes
And the default handler did not run
```

#### Scenario: Runnable returns the built-in kind on every call
```gherkin
Given k1 := doger.Runnable() and k2 := doger.Runnable()
When the consumer compares them and reads their names
Then k1 == k2 is true
And k1.Name() is "runnable"
And a handler registered with k1.Handle(root, StageAfterStart, h) receives lazy "job" registered with AsRunnable()
```

<!-- cases: 3 zero values: EnableRunnerAutoDetection zero value false is the default row -->
<!-- cases: 6 duplicates: auto-detection plus AsRunnable runs once (third row); an alias does not run its target twice (last row) -->
<!-- cases: 7 lifecycle: auto-detection excludes transients and aliases, as for Starters (rows); a runner cancelled by Shutdown is covered by container-start ("Shutdown cancels a running Start", Runner row) -->
<!-- cases: 8 concurrency: a consumer handler that spawns goroutines owns their lifetime; the default handler runs inline, so no goroutine leaks (goleak) -->
<!-- cases: N/A 9 panics: Runnable() cannot panic -->
<!-- cases: 10 compatibility: BREAKING (flagged) - the exported variable Runnable becomes the function Runnable(); migration: Runnable.Handle(...) -> Runnable().Handle(...), Runnable.With(m) -> Runnable().With(m). The variable could be reassigned, which the function rules out, so the "Reassigning the Runnable variable" scenario is gone -->

### Requirement: Stage values are explicit and round-trip as text
`Stage` constants SHALL start at 1, so the zero `Stage` is invalid. `String()` SHALL return `before-start`, `after-start`, or `unknown` for any other value. `MarshalText` SHALL encode the two valid stages by those texts and fail for any other value; `UnmarshalText` SHALL decode only those two texts.

#### Scenario: Stage text conversions
```gherkin
Given the Stage value <stage>
When the consumer calls String(), MarshalText() and then UnmarshalText on the marshalled text
Then String() returns "<string>"
And MarshalText returns <marshal>
And the round trip returns <roundtrip>

Examples:
  | stage                  | string       | marshal                                        | roundtrip              |
  | doger.StageBeforeStart | before-start | "before-start", nil error                      | StageBeforeStart       |
  | doger.StageAfterStart  | after-start  | "after-start", nil error                       | StageAfterStart        |
  | doger.Stage(0)         | unknown      | an error containing "doger: invalid stage 0"   | not attempted          |
  | doger.Stage(7)         | unknown      | an error containing "doger: invalid stage 7"   | not attempted          |
```

#### Scenario: Decoding stage text
```gherkin
Given the text <text>
When the consumer calls UnmarshalText on a Stage
Then the result is <result>

Examples:
  | text            | result                                           |
  | "before-start"  | StageBeforeStart, nil error                      |
  | "after-start"   | StageAfterStart, nil error                       |
  | "unknown"       | an error containing "doger: unknown stage"       |
  | ""              | an error containing "doger: unknown stage"       |
  | "Before-Start"  | an error containing "doger: unknown stage"       |
```

<!-- cases: 1 default: the two valid stages -->
<!-- cases: N/A 2 overrides: the texts are fixed -->
<!-- cases: 3 zero values: Stage(0) and the empty text -->
<!-- cases: 4 boundaries: 0 below the first constant, 7 past the last, a case-changed text -->
<!-- cases: 5 errors: invalid values and texts fail with messages naming them -->
<!-- cases: N/A 6 duplicates, 7 lifecycle, 8 concurrency: a pure value conversion -->
<!-- cases: N/A 9 panics: conversions never panic -->
<!-- cases: 10 compatibility: BREAKING (flagged) - the numeric values of StageBeforeStart and StageAfterStart change from 0/1 to 1/2; code using the named constants is unaffected -->
