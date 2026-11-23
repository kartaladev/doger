# Changelog

All notable changes to the doger modules are recorded here. The root module
(`github.com/kartaladev/doger`) and every adapter module (`http/*`,
`scheduler/*`, `clock/*`, `events/*`) are released together under the same
version. doger is pre-1.0: a minor release may break the API, and every
break is listed under **BREAKING** with its migration.

## Unreleased

### Changed

- A child scope `Shutdown`/`ShutdownWithContext` cancels the context of the
  running start steps (Starter `Start` methods and kind-handler calls) of
  its subtree, with a cause matching `ErrShutdown`, instead of only waiting
  for the run. Such a step is skipped, not a failure of the run: it is not
  recorded as started, and the run skips the rest of that subtree and
  goes on. A service that depends on a step skipped this way is skipped
  too; a dependent outside the shutting-down scope (such as a root service
  depending on its export) then fails the run with a `*StartError` naming
  it that matches `ErrShutdown`, or, while the skipped step's abandoned
  call still runs, with that step's error matching `ErrStartTimeout`. A
  start step now always receives its own child of the run's context. With
  a `Logger`, a cancelled step's error is logged at Debug as `cancelled
  start step error`, and its panic as `panic recovered` (Error) and `panic
  value` (Debug) with `step` and `kind`. See the upgrading guide, "A child scope shutdown
  cancels its subtree's running start steps".
- `eventtransporttest`: the conformance kit's waits are no-progress windows
  of one minute instead of 10s totals, so a slow but live transport (a
  loaded CI machine) passes, while a stalled one fails about a minute
  after its last progress instead of after 10s.

### Fixed

- A scope shutdown called from an async listener now waits for another
  async delivery to that scope whose publisher's `Submit` already returned
  (the default `BoundedEventExecutor` starts the delivery inside `Submit`);
  it could return while that delivery still ran. A custom executor,
  including one wrapping `NewBoundedEventExecutor`, keeps the previous
  rule; see the events docs, "Shutdown".

## v0.3.0 - 2026-11-22

Events: in-process publishing, transport-neutral delivery between
processes, and SQL and watermill transports. No exported API of v0.2.0
changed: everything below is additive.

### Added

- In-process events (OpenSpec `add-eventing`): any Go value is an event.
  `Publish` delivers it to every reachable listener whose declared type it
  is assignable to (`PublishLocal` keeps it in the publishing scope).
  Listeners opt in at registration with `AsListener[E]()` or `ListenMethod`,
  or register a function with `Listen`; `ListenerOrder`, `ListenerFilter`,
  `ListenerAsync` and `ListenerID` tune them. Errors stop dispatch as a
  `*ListenerError` (`EventErrorMode`, `EventContinue` joins them instead),
  panics are recovered, and errors without a caller go to
  `InjectorOpts.HookEventError`. Async delivery runs on a bounded,
  replaceable executor (`EventAsyncParallelism`, `EventExecutor`,
  `NewBoundedEventExecutor`) drained at shutdown. The root publishes
  `ContainerStarting`, `ContainerStarted`, `ContainerReady`,
  `ContainerFailed`, `ContainerClosing` and `ContainerClosed`.
- Delivery between processes (OpenSpec `add-event-transport`):
  `RegisterEventType` gives an event type a wire name (`EventVersion`,
  `EventDestination`, `EventAlias`); externalized events travel as an
  `Envelope` through an `EventTransport`, with a replaceable codec (JSON),
  ID generator (UUIDv7) and propagator. Externalization is opt-in per type
  (`EventExternalized`, `EventExternalize`) with the strategies
  `ExternalizeAuto` (default), `ExternalizeImmediate`,
  `ExternalizeAfterCommit` and `ExternalizeOutbox`; an outbox publish
  without a transaction fails with `ErrNoTransaction` unless
  `EventOutboxFallback` allows it. Remote listeners use
  `ListenerDelivery(DeliverOnce)` or `ListenerDelivery(DeliverAll)`, and
  `PermanentError` stops redelivery. Transaction-bound listeners use
  `Tx`, `TxSource`, `ListenerTxPhase` and `ListenerTxFallback`.
  `NewMemoryEventTransport` is an in-memory transport, `PublisherFor` an
  application publishing port, and package `eventtransporttest` the
  conformance suite every transport runs.
- New module `events/sqltx` (package `sqltx`, OpenSpec
  `add-sql-event-transport`): binds a `database/sql` transaction to a
  context with commit and rollback callbacks (`BeginTx`, `InTx`, `BindTx`,
  `From`, `FromDB`, `Source`); a transaction on another `*sql.DB` fails with
  `ErrForeignTransaction`.
- New module `events/sql` (package `eventsql`): a `doger.EventTransport`
  and `TxEventSender` storing events in PostgreSQL 13+ or MySQL 8.0+
  (dialects in `events/sql/postgres` and `events/sql/mysql`, no driver
  imported). Consumers claim rows with a lease and `FOR UPDATE SKIP
  LOCKED`; retries back off exponentially with jitter, then dead-letter
  (`ListDeadLetters`, `Resubmit`, `DropGroup`). Ephemeral groups are removed
  on stop and reaped after a crash. The caller owns the schema (`DDL`,
  optional `EnsureSchema`, `CheckSchema`). `make test-sql` runs the database
  tests in Docker.
- New module `events/watermill` (package `dogerwatermill`, OpenSpec
  `add-watermill-event-transport`): a `doger.EventTransport` over any
  watermill publisher and a `SubscriberFactory` building one subscriber
  per subscription. Defaults: retry after 1s, give up after 10 attempts per
  process, `doger_` metadata prefix, acked and logged when given up on;
  `WithRetryDelay`, `WithMaxAttempts`, `WithMaxTrackedMessages`,
  `WithPoisonTopic`, `WithMetadataPrefix`, `WithLogger` and `WithClock`
  override them.
- New module `events/watermill/wmsql` (package `dogerwmsql`): watermill-sql
  subscriber factories for PostgreSQL and MySQL whose new groups start at
  the latest message and whose ephemeral groups are deleted on stop
  (`WithKeepEphemeralGroups`, `WithCleanupTimeout`, `WithSubscriberConfig`,
  `WithLogger`);
  `PostgresSchema` / `MySQLSchema` schema adapters with binary payload
  columns; and `Outbox`, a PostgreSQL transactional outbox on `events/sqltx`
  transactions relayed by watermill's forwarder, its tables shaped by
  `OutboxConfig.SchemaAdapter` and `OffsetsAdapter`, created when missing
  unless `OutboxConfig.SkipSchemaInitialization` is set.
- `make test-watermill` runs the `wmsql` database tests against PostgreSQL
  17 and MySQL 8.4 in Docker; Linux CI runs them in `make test-all`.

### Changed

- `scheduler/gocron` requires `github.com/go-co-op/gocron/v2` v2.23.2
  (was v2.23.0).

## v0.2.0 - 2026-11-20

First-release hardening (OpenSpec `harden-first-release`).

### BREAKING

Each entry gives the change and its migration.

| Before | After | Migration |
|---|---|---|
| `As`, `AsNamed`, `MustAs`, `MustAsNamed` | `Alias`, `AliasNamed`, `MustAlias`, `MustAliasNamed` | Rename the calls. `Bind`/`BindNamed` are unchanged. |
| `ExportAs(name)` | `WithExportName(name)` | Rename the call. |
| `Flatten()` | `ExtendIntoRoot()` | Rename the call. `Extend` with a nil root, typed-nil included, returns `ErrNilInjector`. |
| `HealthReportOption` (struct) | A function type (`func` over an unexported query); a nil option is ignored | None for callers of `MatchHealthTags`/`IgnoreContainerPhase`; consumers can compose options in their own helpers. The struct literal `HealthReportOption{}` no longer compiles: use `nil` or no option. |
| `Stage` and `DependencySource` start at 0 | Constants start at 1; 0 is invalid, its `String()` is `"unknown"`, and marshalling or using it fails | Use the named constants. |
| `var Runnable` | `func Runnable() Kind[Runner, struct{}]` | `Runnable.Handle(...)` becomes `Runnable().Handle(...)`, and `Runnable.With(m)` becomes `Runnable().With(m)`. |
| `var MaxInvocationFrames` | `InjectorOpts.MaxInvocationFrames int` (0 means 100, negative turns recording off) | Set the option per container. |
| `DAG` | Unexported | No public use existed. |
| `var DefaultRootScope`, and a nil injector falling back to it | Removed; nil gives `ErrNilInjector` (error-returning APIs) or a panic matching it (others) | Create a container with `doger.New()` and pass it explicitly. |
| `DisableStarterAutoDetection` | `EnableStarterAutoDetection` (default false) | Add `AsStarter()` to services that must start, or set `EnableStarterAutoDetection: true`. |
| `Logf` | `Logger *slog.Logger` | `Logger: slog.Default()`, or any handler. |
| `Clock clockwork.Clock`, `HealthScheduler` taking a `clockwork.Clock` | `doger.Clock` | `Clock: dogerclockwork.Wrap(fake)` (new module `github.com/kartaladev/doger/clock/clockwork`); scheduler implementers take a `doger.Clock`. |
| `dogergocron` ran on any `clockwork.Clock` the container held | It maps `doger.RealClock()` and clocks made by `dogerclockwork.Wrap`; a consumer-written `doger.Clock` makes `ScheduleHealthRounds` fail with `dogergocron.ErrUnsupportedClock`, and the container then stays unmonitored | Use `doger.RealClock()` or `dogerclockwork.Wrap(...)`, or pass your own gocron scheduler with `dogergocron.WithScheduler`, which brings its own clock. |
| `ErrStartAfterShutdown` | `ErrShutdown` | Rename the sentinel in `errors.Is` checks. |
| `"DI: "` message prefix | `"doger: "`, carried once | Match with `errors.Is`/`errors.As`, not text. |
| `ExplainServiceDependencyOutput` tree that expands every path | Each service expanded once; a repeat has `Ref: true` and no children | Follow `Ref` nodes through `ExplainNamedService`. |
| Per-service hook signatures `HookBeforeStart`, `HookAfterStart`, `HookBeforeShutdown`, `HookAfterShutdown`, `HookAfterInvocation` | Start and shutdown hooks take `(ctx, scope, serviceName)`, and their "after" hooks also `elapsed time.Duration, err error`; `HookAfterInvocation` takes `(scope, serviceName, elapsed, err)` without a context | Add the `ctx`/`elapsed` parameters to your hook functions. |
| `dogerchi.Use(*chi.Mux, …)` | `dogerchi.Use(chi.Router, …)` | Source-compatible; the parameter is wider. |
| `RootScope.HealthCheck()` unbounded | Bounded by 10s by default | `HealthCheckTimeout: -1` for unbounded. |
| Starting, resolving, registering or creating a scope on a container or scope after its shutdown began | `ErrShutdown`: `Invoke*`, `InvokeAll*`, collection reads, `Alias*` and `Export*` return it; `Provide*`, `Override*` and `Scope()` panic with it, also from Closing hooks. A container cannot be reused after Shutdown | Build a new container. |
| `ShutdownWithContext` waited for every service, whatever its context | Returns once its context ends: services still shutting down are reported with the context's error and keep running in the background, and a service reached after the context ended is not shut down | Give the context the time services need, or `context.Background()` to wait for them all. |
| A panicking service `Shutdown` method or shutdown hook crashed the process | Recovered and reported with an error matching `ErrShutdownPanic`; the other services still shut down, and the first panic reaches `HookContainerPanic` | None; read the report. |
| `ShutdownReport` marshalled to JSON field by field, with a map of errors | `MarshalJSON` gives `succeed`, `shutdownTime`, `services` and `errors` arrays ordered by scope, then service | Update JSON consumers of the report. |
| An error panic value from a provider returned as is | Wrapped: matches `ErrProviderPanic` and the original | Keep using `errors.Is`/`errors.As`; don't compare with `==`. |
| Web UI page functions and adapter `Use` | Gain a variadic `...dogerhttp.Option`, such as `dogerhttp.WithFullSourcePaths()` and `dogerhttp.WithErrorHandler(handler)` | Calls compile unchanged; code storing these functions in typed func variables breaks (apidiff: incompatible) and must update the variable's type. |
| `ServiceDescription` JSON keys `ScopeID`, `ScopeName`, `Service` | `scopeId`, `scopeName`, `service`, wherever a `ServiceDescription` is marshalled (`ListProvidedServices`/`ListInvokedServices` results, `HealthReport` entries); the Explain output types have their own keys and are unchanged | Update JSON consumers of those values. |
| `Stage` without a text form | `MarshalText` gives `before-start` / `after-start` | None. |
| `HealthCheckTimeout` 0 meaning none on every forced path | 10s on `RootScope.HealthCheck`, `Scope.HealthCheck` and the per-service functions (`HealthCheckSingleTimeout` 0 inherits it); `HealthCheckGlobalTimeout` ≤ 0 means no global bound | Set a negative value for unbounded. |
| Typed-nil injector, root or scope | Treated as nil: `ErrNilInjector` | Pass a real container. |
| std web UI answered any method | GET and HEAD only; other methods get a 4xx | None for browsers. |
| chi/std wrote `err.Error()` on a render failure | Generic 500 body on every adapter | None. |

### Added

- `ShutdownReport.Err()` returns nil on success and otherwise an error
  matching every service error with `errors.Is`/`errors.As`;
  `ShutdownReport.Unwrap()` returns one `*ServiceError` per failed service.
  `Error()` still returns `""` on success: check `Err()`.
- New `InjectorOpts` fields: `Logger`, `MaxInvocationFrames`, `Clock`,
  `HookAfterBuild`, `HookBeforeStart`/`HookAfterStart`, the container and
  scope lifecycle hooks, `HookContainerPanic`, the health-change hooks,
  `HealthCheckSingleTimeout`, `HealthReportCacheTTL`, `HealthScheduler`,
  `EnableStarterAutoDetection`, `StartParallelism`,
  `EnableRunnerAutoDetection` and `PropertySource`; each works unset. See
  the migration guide for their defaults.
- `docs/docs/migrating/migrating-from-samber-do.md` covers every change
  from `samber/do` v2, with code that compiles (`docs_snippets_test.go`).

### Changed

- New sentinel `ErrScopeAlreadyDeclared`: `Scope(name)` on a scope that
  already has an attached child named `name` panics with an error
  matching it. A child that was shut down no longer counts.
- New sentinel `ErrStructInjection`: the errors of `InvokeStruct` and
  `MustInvokeStruct` for an invalid target (not a struct, a recursive
  pointer type, a service not assignable to its field, `*` on a field that
  is not a collection) match it. Their messages are unchanged apart from
  the prefix. A recursive pointer type used to overflow the stack, and an
  interface target type used to panic; both now return this error.
- A service whose value is a nil interface no longer panics in `InvokeAs`
  or `InvokeStruct`, and an alias of it no longer fails with a type
  mismatch: it gives the zero value, or the "not assignable" error when
  its declared type does not fit the field.
- Not-found errors list at most 10 services, sorted, followed by
  `and N more`; `ExplainInjector` still lists all of them.

### Release and supply chain

- Released `go.mod` files carry no test-only requirements: the root module
  no longer requires `testify`, `goleak` or `clockwork`, and no adapter
  requires `testify` or `goleak`.
- Released adapters require the root module at the same version and have
  no `replace` directive.
- New module `github.com/kartaladev/doger/clock/clockwork`, tagged
  `clock/clockwork/<version>`.
- Dependency floors: `github.com/valyala/fasthttp` v1.79.0 in `http/fiber`,
  which no longer depends on `github.com/andybalholm/brotli` (GO-2026-6888),
  and `golang.org/x/net` v0.62.0 in `http/echo` and `http/gin`. Echo's
  GO-2026-6875 and GO-2026-6872 are accepted in [SECURITY.md](SECURITY.md).
- `http/echo` requires `github.com/labstack/echo/v4` v4.16.5.
- Releases are cut only from `main` through the `release` environment, after
  build, vet and `govulncheck` of the post-processed tree.
