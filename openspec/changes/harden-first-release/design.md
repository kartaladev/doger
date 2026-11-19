# Design

## Context

See proposal.md (Why, Defect) for motivation and the reproduction of each defect. The constraints that shape the approach are below.

- **Lazy service lock.** A lazy service holds its own `sync.Mutex` for the whole provider call (`serviceLazy.instanceOrBuild`). Cycle detection only sees the invocation chain carried by `virtualScope` (`name`, `parent`, `base`). A provider that resolves through an injector it captured, or two chains on different goroutines, are invisible to the chain check. That is the root cause of the hang in a concurrent cycle (C1/F2) and in re-entrant resolution (C2). It is also why `ShutdownWithContext` waits on an in-flight build (C9).
- **Shutdown has no guards.** Each service shuts down on its own `sync.WaitGroup.Go` goroutine, with no `recover` and an unbounded `wg.Wait()` (`scope.go:741-760`). The service wrapper resets itself to "not built" instead of becoming terminal (`service_lazy.go:226-233`). `Invoke` looks up the wrapper and builds it later, with no check in between (`invoke.go:89-111`).
- **Children are never detached.** `Scope.ShutdownWithContext` never removes the child from its parent. Only extension rollback and clone call `detachChild` (`scope.go:162`).
- **Unsynchronized shared state.** These are written and read without synchronization: the hook lists (`root_scope.go:497-547`), `MaxInvocationFrames` (`service.go:14`), and `serviceLazy.getBuildTime` (`service_lazy.go:315`).
- **The web UI does no escaping.** The UI renders with `text/template` and builds pages from pre-rendered strings (`http/utils.go`). Its explain tree expands every path (`di_explain.go:337-375`).
- **CI covers the root module only.** `make test` and `make lint` run `./...` from the root, which in a `go.work` workspace matches the root module only.
- **Other in-flight changes.** `add-config`, `add-eventing` and `add-durable-events` are in flight. This change lands first, and they rebase on it (see proposal Impact).
- **Toolchain.** Go 1.27 stays the floor; the code uses 1.27-only constructs.

## Goals / Non-Goals

**Goals:**
- Every defect in proposal.md is fixed, and each fix starts from a regression test that fails first.
- The v1 public API is settled: every break is listed below with its migration, and nothing is left behind as a deprecated alias.
- Each module under `go.work` is gated in CI on build, test with `-race`, lint and govulncheck.
- Resolution never deadlocks inside the library. It may only block for as long as a user provider itself blocks.

**Non-Goals:**
- Detecting deadlocks that cross goroutines a provider spawns itself, for example a provider that starts a goroutine which resolves the provider's own service and then waits for it. These are documented, not detected.
- Making the container a security boundary. Extensions and imported packages remain trusted code (audit risk R1). Overriding a service stays allowed and observable through registration hooks.
- Built-in authentication for the web UI. Consumers mount it behind their own middleware.
- Renaming `RootScope` methods or removing the `AddXxxHook` methods. The audit flagged the duplication, but the fields are for configuration time and the methods are for plugins attached after construction. Both are kept, and the godoc of each points to the other.
- Lowering the Go floor below 1.27.

## Decisions

### D1. In-flight build markers replace the lock held across the provider call
Each lazy service, and each eager service with declared dependencies, keeps a build state machine instead:
- **Idle:** not built.
- **Building:** holds the builder's goroutine id and a `done` channel.
- **Built:** the instance is published atomically.
- **Terminal:** shut down.

A small per-container wait-for registry, keyed by goroutine id, records "goroutine G waits for the build owned by goroutine H".
- **Re-entrant build.** Before waiting on a build that is in flight, the caller checks whether that build's owner is itself. If so, the build is re-entrant, for example through a captured injector. The caller then returns an error wrapping `ErrCircularDependency` that names the chain it knows.
- **Cross-goroutine cycle.** Otherwise the caller adds the wait edge and walks the edges. If the walk reaches the caller again, the caller removes its edge and returns `ErrCircularDependency`. The build that caller was running then fails, which releases every other waiter with that error wrapped.
- **Getting the goroutine id.** The id is read from the first line of `runtime.Stack` output. That happens only when a build starts and when a wait has to happen, never on the path that returns an already-built singleton.

Alternatives considered:
- Keeping `sync.Mutex`. That leaves the hang.
- A re-entrant mutex. It needs the same goroutine id, and it hides cycles as recursion.
- `singleflight`. It has no way to detect a cycle.
- A context parameter on `Invoke`. That is a massive API break, and it still misses captured injectors.
- A timeout. That only replaces a hang with flaky timing.

The goroutine-id read is a known trade-off, made acceptable because it stays off the hot path. Transient services keep cycle detection by chain only (they share no lock). A transient that re-enters itself through a captured injector recurses until Go reports a stack overflow, and that case is documented.
- **Provider `runtime.Goexit`.** A provider that exits its goroutine releases every waiter with an error matching `ErrProviderPanic` whose message mentions `runtime.Goexit`; the build returns to Idle, so a later call retries.
- **Late build after a bounded shutdown.** A provider that finishes after a bounded shutdown gave up on it (D4) finds its wrapper Terminal: the instance is shut down exactly once, never cached, and the invoker gets `ErrShutdown`.

### D2. A scope is closed once its shutdown begins
- **Close flag.** `Scope` gets an atomic close flag. It is set under the scope's write lock at the start of shutdown, before its children are listed.
- **Registration and child scopes.** Registration (`Provide*`, `Override*`, aliases, `Export`) and `Scope()` check the flag under that same lock. On a closed scope, `Provide*`, `Override*` and `Scope()` panic with an error that matches `ErrShutdown`, the way a duplicate registration panics today, while `Alias*` and `Export*`, which return errors, return one.
- **Invoke.** `Invoke*` checks the flag before the lookup and returns `ErrShutdown`.
- **A wrapper looked up just before shutdown.** Shutting a service down moves its wrapper to Terminal. If a caller had already looked the wrapper up, its build sees Terminal and returns `ErrShutdown` instead of building (fixes F2/C7).
- **One sentinel.** `ErrShutdown` replaces `ErrStartAfterShutdown`, so Start, Refresh, Scope.Start, Invoke and registration all report one sentinel.

Alternative considered: re-listing children after shutting them down, to catch late ones. It still races, and it leaves the late scope's services running.

### D3. Detach on completed shutdown
The shutdown call that actually runs a child's shutdown calls `parent.detachChild(child)` once that shutdown has completed. A parent's own shutdown already removes its children, and detaching twice is a no-op. After detaching:
- `ChildByName` reports not found.
- `Children()` excludes the child.
- The name can be registered again.

### D4. Shutdown honors its context
- **Waiting for services.** Service shutdowns still run in parallel goroutines. `ShutdownWithContext` waits on a `select` over a completion channel and `ctx.Done()`.
- **When the context ends first.** Every service that has not finished is reported as `*ServiceError{Err: ctx.Err()}`, and the call returns. The goroutines for those services finish in the background and are documented as such.
- **A build in flight.** Waiting for an in-flight build (D1) uses the same `select`, so a stuck provider no longer blocks a bounded shutdown.
- **No context deadline.** The behavior is unchanged: the call waits as long as it takes.

### D5. Panic isolation on shutdown
`serviceShutdown` defers a `recover` around the before-shutdown hooks, `service.shutdown(ctx)` and the after-shutdown hooks.
- **Recorded error.** A recovered value becomes an error matching `ErrShutdownPanic`. An error value is wrapped with a second `%w`, so `errors.Is` and `errors.As` still reach it. That error goes in the report under the service.
- **First panic.** The first such panic of the operation is also passed to `HookContainerPanic` with the point `"service-shutdown"`, after the operation completes, following the existing observer-hook policy.
- **Later panics.** They are logged at Error level.
- **Each step separately.** The before-shutdown hooks, `service.shutdown(ctx)` and the after-shutdown hooks are recovered one by one, and their errors are joined. A panicking before-hook therefore never skips `Shutdown`, which would leak the resource.

### D6. Error taxonomy
```go
var (
    ErrServiceNotFound        // unchanged
    ErrServiceNotMatch        // unchanged: no visible service assignable to T (InvokeAs)
    ErrServiceTypeMismatch    // name found, registered type not T
    ErrServiceAlreadyDeclared // duplicate registration (panic value matches)
    ErrCircularDependency     // unchanged
    ErrProviderPanic          // provider panicked; message names the service
    ErrShutdown               // replaces ErrStartAfterShutdown
    ErrShutdownPanic          // Shutdown or shutdown hook panicked
    ErrNilInjector            // nil injector / root / scope argument
    ErrStructInjection        // invalid tag, unsupported or recursive field type
    // health and extension sentinels unchanged, re-prefixed
)
type ServiceError struct {
    Scope   string // scope name
    Service string // service name
    Err     error
}
func (e *ServiceError) Error() string // `doger: service "db" in scope "[root]": <cause>`
func (e *ServiceError) Unwrap() error
```
- **Provider failures.** A provider's error, and a failure in a dependency it resolves, are wrapped in `*ServiceError` once at each service boundary. A missing dependency of A therefore surfaces as `ServiceError{A, ServiceError{B, ErrServiceNotFound}}`. `errors.Is(err, ErrServiceNotFound)` still matches, and `errors.As` gives the outermost service, A.
- **Prefix.** Every message starts with `doger: ` exactly once. Wrapping layers inside the library never add the prefix again.
- **Not-found message.** It lists at most 10 names, sorted, followed by `… and N more`.
- **Alternative considered:** flat sentinels only. That loses which service failed and keeps message-text matching.
- **What `*ServiceError` wraps.** Only failures inside a build. A requested name that is not registered at all gives a plain not-found error, and `errors.As(err, &se)` fails. That is how "A is missing" is told apart from "A's dependency is missing" (`errors.As` succeeds, `se.Service == "A"`).
- **Provider panics (BREAKING, see D10).** An error panic value is no longer returned as is: it is wrapped so it matches both `ErrProviderPanic` and the original via `errors.Is`/`errors.As`.
- **`ErrScopeAlreadyDeclared`.** A duplicate child-scope name panics with a value matching it. A detached child's name (D3) can be declared again.
- **Chosen message texts.** `ErrShutdown`: `doger: container is shut down`. Stage: `doger: invalid stage N`, `doger: unknown stage`. DependencySource: `doger: invalid dependency source N`, `doger: unknown dependency source`. Kinds: `doger: kind not created with NewKind`, `doger: nil handler for kind "k"`.
- **Prefix scope.** Library-authored text carries `doger: ` once. The consumer-led rule is defensive: a message whose lead is the consumer's own error keeps that text first and unprefixed, and library text inside it does not repeat the prefix (`ledErrorf`). No public path produces such a message today, because every service failure of a run is a `*StartError`, which the library leads (``doger: failed to start service `a` in scope `[root]`: boom (start aborted: container is shut down)``); the rule is pinned through `abortOutcome` directly (`TestAbortOutcome`).
- **Typed nil.** A typed-nil `*RootScope` or `*Scope` behaves exactly like a nil injector (`ErrNilInjector`).

### D7. Doger-owned Clock
```go
type Clock interface {
    Now() time.Time
    Since(t time.Time) time.Duration
    AfterFunc(d time.Duration, f func()) Timer
}
type Timer interface{ Stop() bool }
func RealClock() Clock
```
These three methods are everything the root package uses today.
- **Adapter module.** A new module, `github.com/kartaladev/doger/clock/clockwork` (package `dogerclockwork`), provides `Wrap(clockwork.Clock) doger.Clock` and `Unwrap(doger.Clock) (clockwork.Clock, bool)`.
- **gocron.** `dogergocron` passes the unwrapped clock to `gocron.WithClock`. It maps `doger.RealClock()` to `clockwork.NewRealClock()`. Any other clock makes `ScheduleHealthRounds` return an error matching `dogergocron.ErrUnsupportedClock`, and the container then stays unmonitored, as for any scheduler error.
- **Root module.** The root module requires clockwork for its tests only, and the release strips that requirement.
- **Alternative considered:** structural compatibility with clockwork. Go has no covariant return types, so `AfterFunc` returning `clockwork.Timer` can never satisfy a doger interface.

### D8. Levelled logging with slog
`InjectorOpts.Logger *slog.Logger` replaces `Logf`. When it is nil, the container uses a discard logger and skips building log arguments.

| Level | Events |
|---|---|
| Debug | registration, invocation, build, `requested shutdown` (the first record of the winning root Shutdown), and the panic value of a recovered panic |
| Info | container start, ready, closing and closed |
| Warn | a fallback to an unmonitored container, a health state change to degraded |
| Error | a health scheduler failure (a stop failure included), a shutdown error, a recovered panic not passed to `HookContainerPanic` (service name and panic type only): the first panic of an operation goes to that hook when it is set, and is logged only when it is not; later panics are logged |

- **Attributes.** Every record carries the attributes `scope` and `service`, where they apply.
- **Excluded values.** Panic values and health-check error text are emitted only at Debug, to keep secrets out of default logs (audit R3).
- **Concrete messages.** The exact message, level and attributes of every event are specified in the `container-logging` spec (for example `service registered`, `service invoked`, `panic recovered` with `point` and `panic_type`, and `panic value` at Debug). A return to healthy is logged at Info, and a declined (disabled) extension install at Info.

### D9. Per-service hook signatures (BREAKING)
```go
HookBeforeStart    []func(ctx context.Context, scope *Scope, serviceName string)
HookAfterStart     []func(ctx context.Context, scope *Scope, serviceName string, elapsed time.Duration, err error)
HookBeforeShutdown []func(ctx context.Context, scope *Scope, serviceName string)
HookAfterShutdown  []func(ctx context.Context, scope *Scope, serviceName string, elapsed time.Duration, err error)
HookAfterInvocation []func(scope *Scope, serviceName string, elapsed time.Duration, err error)
```
- **Invocation hooks.** They get no context, because `Invoke` has none.
- **Registration.** Hook lists are stored copy-on-write behind an atomic pointer. `AddXxxHook` is safe concurrently with every operation, and a hook added during an operation applies from the next operation on.
- **Alternative considered:** one event struct for every hook. It is more extensible, but it breaks all 23 hooks, and the container hooks already use the `(ctx, scope, …)` shape.

### D10. Renames, retypes and removals (BREAKING)
| Before | After | Migration |
|---|---|---|
| `As`, `AsNamed`, `MustAs`, `MustAsNamed` | `Alias`, `AliasNamed`, `MustAlias`, `MustAliasNamed` | Rename the calls. `Bind`/`BindNamed` are unchanged |
| `ExportAs(name)` | `WithExportName(name)` | Rename the call |
| `Flatten()` | `ExtendIntoRoot()` | Rename the call |
| `HealthReportOption` (struct) | `type HealthReportOption func(*healthReportOptions)` | None for callers of `MatchHealthTags`/`IgnoreContainerPhase`. A zero value no longer exists |
| `Stage` and `DependencySource` start at 0 | Constants start at 1; 0 is invalid, its `String()` is `"unknown"`, and marshalling or using it fails | Use the named constants |
| `var Runnable` | `func Runnable() Kind[Runner, struct{}]` | `Runnable.Handle(...)` becomes `Runnable().Handle(...)` |
| `var MaxInvocationFrames` | `InjectorOpts.MaxInvocationFrames int` (0 means 100, negative turns recording off) | Set the option per container |
| `DAG` | unexported | No public use existed |
| `var DefaultRootScope`, and a nil injector falling back to it | Removed; nil gives `ErrNilInjector` (error-returning APIs) or a panic matching it (others) | Create a container with `doger.New()` and pass it explicitly |
| `DisableStarterAutoDetection` | `EnableStarterAutoDetection` (default false) | Add `AsStarter()` to services that must start, or set `EnableStarterAutoDetection: true` |
| `Logf` | `Logger *slog.Logger` | `Logger: slog.Default()`, or any handler |
| `Clock clockwork.Clock`, `HealthScheduler` taking a `clockwork.Clock` | `doger.Clock` | `Clock: dogerclockwork.Wrap(fake)`; scheduler implementers take a `doger.Clock` |
| `ErrStartAfterShutdown` | `ErrShutdown` | Rename the sentinel in `errors.Is` checks |
| `"DI: "` message prefix | `"doger: "` | Match with `errors.Is`/`errors.As`, not text |
| `ExplainServiceDependencyOutput` tree that expands every path | Each service expanded once; a repeat has `Ref: true` and no children | Follow `Ref` nodes through `ExplainNamedService` |
| Per-service hook signatures | See D9 | Add `ctx`/`elapsed` parameters |
| `dogerchi.Use(*chi.Mux, …)` | `dogerchi.Use(chi.Router, …)` | Source-compatible; the parameter is wider |
| `RootScope.HealthCheck()` unbounded | Bounded by 10s by default | `HealthCheckTimeout: -1` for unbounded |
| Starting or resolving on a container after shutdown | `ErrShutdown` | Build a new container |

Additional breaks and changes found while writing the specs:

| Before | After | Migration |
|---|---|---|
| An error panic value from a provider returned as is | Wrapped: matches `ErrProviderPanic` and the original | Keep using `errors.Is`/`errors.As`; don't compare with `==` |
| Web UI page functions and adapter `Use` | Gain a variadic `...dogerhttp.Option` | Call sites keep compiling; code storing the functions in typed func variables breaks (apidiff: incompatible) and must update the variable type |
| `ServiceDescription` JSON keys `ScopeID`, `ScopeName`, `Service` | `scopeId`, `scopeName`, `service` (also changes Explain JSON) | Update JSON consumers |
| `Stage` without a text form | `MarshalText` gives `before-start` / `after-start` | None |
| `HealthCheckTimeout` 0 meaning none on every forced path | 10s on `RootScope.HealthCheck`, `Scope.HealthCheck` and the per-service functions (`HealthCheckSingleTimeout` 0 inherits it); `HealthCheckGlobalTimeout` ≤ 0 means no global bound | Set a negative value for unbounded |
| Typed-nil injector, root or scope | Treated as nil: `ErrNilInjector` | Pass a real container |
| std web UI answered any method | GET and HEAD only; other methods get a 4xx | None for browsers |
| chi/std wrote `err.Error()` on a render failure | Generic 500 body on every adapter | None |

### D11. Bounded, plain-text explanation
- **Bounded tree.** `newExplainServiceDependencies` memoizes by `(scope, service)`. The first visit expands the service; later visits emit a node with `Ref: true`. Output grows with the number of edges, not the number of paths.
- **Plain text.** `String()` output is rendered with `text/template`, so names are not HTML-escaped.
- **Race fix.** Build time and built state are read under the service's read lock, or from the published atomic state, which fixes C3/F4.
- **Edges.** An edge is recorded only after a dependency built successfully, in `invokeByName` and `invokeAnyByNameAs`, as the by-type path already does (C10).
- **Report JSON conventions** (shared with D13/D14). Keys are lowerCamel. Durations are Go duration strings. Arrays are sorted by scope name, then service name. Zero times and nil errors are omitted. `services` is `[]`, never `null`.

### D12. Web UI
- **Templates.** One `html/template` set, parsed once at package init, with recursive `{{template}}` blocks for the trees. No pre-rendered strings are concatenated. Links use `url.Values`.
- **Signatures.** The exported page functions gain a variadic `...dogerhttp.Option`. Call sites keep compiling, but code storing the functions in typed func variables breaks.
- **Response headers.** Every adapter's response helper sets these four headers:
  - `Content-Type: text/html; charset=utf-8`
  - `X-Content-Type-Options: nosniff`
  - `Content-Security-Policy: default-src 'none'; style-src 'unsafe-inline'; frame-ancestors 'none'`
  - `Cache-Control: no-store`
- **Mount options.** Each adapter's `Use` takes variadic `...dogerhttp.Option`. There are two options:
  - `WithFullSourcePaths()`. Without it, source paths are shortened to `file.go:line`.
  - `WithErrorHandler(func(ctx context.Context, err error))`. It receives each render failure once, with the request's context. Fiber passes `c.UserContext()`; the other adapters pass the `*http.Request` context. The default discards the error, because the library writes nothing to stdout, stderr or `slog.Default()`. The last option given wins, and a nil handler means the default. A panicking handler propagates to the framework's recovery.
  - Alternatives considered: a `*slog.Logger` option, which ties the UI to slog and still can't route errors to an error tracker; and letting the handler write the response, which middleware can already do.
- **Example application.** It binds to `127.0.0.1` and wraps the UI in basic-auth middleware that reads its credentials from the environment.
- **Methods and errors.** The std adapter serves GET and HEAD only; other methods get a 4xx. Every adapter answers a render failure with a generic 500 body, never the error text. The defensive headers are also set on the 302 redirect.

### D13. Health changes
- **Default timeout.** `HealthCheckTimeout` 0 means 10s; a negative value means unbounded. This follows the `HealthReportCacheTTL` sign convention.
- **First monitored read.** A monitored read of a service that has never been checked, while the scheduled first check is in flight, joins that check under the read's own bound (fixes API-01). The godoc and spec now agree.
- **Goexit.** `finishCall` is deferred in `runCall`. A checker that exits through `runtime.Goexit` records an error that matches `ErrHealthCheckPanic` with the value `"runtime.Goexit"`.
- **JSON.** `HealthState`, `ContainerPhase`, `DependencySource` and `Stage` implement `MarshalText`/`UnmarshalText`. `HealthReport` and `ServiceHealth` marshal errors as their message string.
- **Goexit is not a hook panic.** A checker that exits through `runtime.Goexit` does not call `HookContainerPanic`; it is logged at Error.
- **Forced paths.** The 10s default applies to every forced path: `RootScope.HealthCheck`, `Scope.HealthCheck` and the per-service functions, through `HealthCheckSingleTimeout` 0 inheriting `HealthCheckTimeout`.
- **gocron clock errors.** `dogergocron.ErrUnsupportedClock` is also returned for a nil clock. It does not apply with `WithScheduler`, because a shared scheduler ignores the container clock.

### D14. Shutdown report and the remaining lifecycle fixes
- **Report accessors.** `ShutdownReport` gains `Err() error`, which is nil on success and an `errors.Join` of the service errors otherwise, and `Unwrap() []error`. `Error()` keeps returning `""` on success, and its doc says so. Callers are told to check `report.Err() != nil`.
- **Report JSON.** `MarshalJSON` emits an array `errors: [{scope, service, error}]` and `succeed`.
- **Eager values shared with clones.** An eager value carries a shared shutdown-once cell, and `Clone` copies the cell pointer. The first container to shut the value down calls its `Shutdown`; the others only remove their registration.
- **Re-entrant root Shutdown.** Root `ShutdownWithContext` derives the context it hands to services with a private marker key. A root Shutdown whose context carries its own marker returns at once with an empty report, so `Err()` is nil. Calls that drop the context, such as `Shutdown()` with no arguments, are documented as waiting on themselves.
- **Nil context.** `ShutdownOnSignalsWithContext(nil)` uses `context.Background()`. The check happens before `signal.Notify`.
- **Options copy.** `NewWithOpts` and `CloneWithOpts` shallow-copy `*opts` and clone every hook slice. The caller's struct is never written.

### D15. Struct injection
- **Nil interface.** A dependency whose value is a nil interface sets the field to its zero value through `memberValue`, as the collection path already does, once the service's declared type (`getReflectType()`) is checked assignable to the field: a nil interface has no dynamic type, so an unassignable declared type gives the "not assignable" error matching `ErrStructInjection`.
- **Named pointer targets.** `deepEmptyMakeValue` converts each allocated pointer to the target type, so `type P *S` is injected like `*S`.
- **Recursive types.** `deepEmptyMakeValue` tracks the types it has visited. A recursive type returns an error matching `ErrStructInjection` that names the type.
- **InvokeAs.** A nil instance gives the zero value and a nil error; any other instance was selected as assignable to `T` and is asserted with a forced assertion, so an impossible mismatch panics instead of becoming a silent zero value.
- **Aliases.** An alias whose target is a nil interface gives the zero value of the alias type.

### D16. Source tracking
Frame capture uses one `runtime.Callers` pass into a fixed-size array. It skips frames whose function belongs to package `github.com/kartaladev/doger` (not its sub-packages' tests), so value registrations record the user's call site.
- **Scope of the rule.** Skipping the root package's non-test frames applies to the provider frame of value and alias registrations only. Invocation frames keep today's rule, so the pinned "invocation frame of an alias service is recorded at the alias implementation" scenario is unchanged.

### D17. Performance, with behavior preserved
1. **Read-only fast path for invoking a built singleton:**
   - `onServiceInvoke` takes the read lock and upgrades to the write lock only when this is the first invocation of the service in the scope.
   - `instanceOrBuild` returns the atomically published instance without locking when the service is Built.
2. **Collection candidates.** `InvokeAll`/`InvokeAllMap` build their candidates from the existing per-scope type-match cache (`matchesFor`) instead of copying every visible service.
3. **Type names.** `NameOf[T]` and invocation type names are cached in a `sync.Map` keyed by `reflect.Type`.
4. **No-logger fast path.** When `Logger` is nil, formatting is skipped (D8). When the service is already built, the child virtual scope is not allocated.
5. **Shutdown planning.** It uses Kahn's algorithm with in-degree counters, O(V+E), instead of removing one layer per pass.

Each item gets a benchmark in `bench/` that runs before and after, with results compared by `benchstat` and recorded in the task. Each is a behavior-preserving refactor, guarded by the existing suite.

### D18. CI and release
- **Make targets.** `make modules` lists the workspace modules with `go list -m -f '{{.Dir}}'`. `make test-all`, `make lint-all` and `make vuln` loop over them, and `make test`/`make lint` keep their root-only meaning for fast local runs.
- **Workflows.**
  - `test.yml` runs `test-all` on ubuntu, macos and windows with Go 1.27.x and stable.
  - `lint.yml` runs `lint-all`.
  - `security.yml` re-enables govulncheck per module.
- **Pinning and permissions.** Every action is pinned to a full commit SHA with a `# vX.Y.Z` comment. Every workflow declares `permissions: contents: read` at the top and raises it per job only where needed.
- **Dependabot.** Auto-merge is limited to semver patch and minor updates, and is gated on `github.event.pull_request.user.login == 'dependabot[bot]'`. Dependabot also covers `/scheduler/**` and `/clock/**`.
- **Release.**
  - It requires `github.ref == 'refs/heads/main'`, or a tag that points at a commit on `main`, and the `release` environment.
  - After stripping tests, it drops from every module the test-only requirements (testify, goleak, clockwork) that its non-test packages do not use on any release platform, then runs `go mod tidy` while the local `replace` is still present (tidy alone keeps a requirement a dependency's graph mentions; a tidy that adds or raises a requirement fails the release), then pins the root version, builds, vets and runs govulncheck.
  - It creates the GitHub release with `generate_release_notes: true`.
- **Lint.** `.golangci.yml` enables `gosec`, `errorlint` and `noctx`, with each suppression justified inline.
- **Also released and checked.** The release also tags `clock/clockwork/<semver>`. Three scripts back the `release-integrity` scenarios: `scripts/check-workflows.sh` (pinning, permissions, matrix, provenance, dependabot, policy files; `--self-test`), `scripts/release-dry-run.sh <semver>` (mirrors release post-processing into a file proxy and builds a clean `GOWORK=off` consumer) and `scripts/test-make-targets.sh` (plants failures per module).
- **Accepted advisories.** SECURITY.md records echo's GO-2026-6875 and GO-2026-6872 as accepted (no upstream fix; the vulnerable symbols are not called).
- **Raised permissions.** Only these jobs raise permissions above `contents: read`: the `doc.yml` deploy job (`pages: write`, `id-token: write`), the `security.yml` `codeql` and `trivy-codeql` jobs (`security-events: write`, to upload to code scanning), the `release.yml` release job (`contents: write`) and the `dependabot-automerge` job (`contents: write`, `pull-requests: write`).

### D19. Starter convention in scenarios
A "Starter" in any scenario of any spec is registered with `AsStarter()`, unless the scenario explicitly enables `EnableStarterAutoDetection`. Existing test fixtures that rely on type-based detection are updated to opt in.

## Risks / Trade-offs

- **[Risk] The goroutine-id read (D1) relies on `runtime.Stack` format.** Mitigation: it is parsed defensively, with a fallback to "unknown". When the id is unknown, detection degrades to the chain check plus a fresh wait, never to a wrong error. A unit test pins the parse, and it runs on all 3 OSes in CI.
- **[Risk] A bounded shutdown leaves goroutines running for hung services (D4).** Mitigation: the report lists exactly which services were abandoned, and the godoc states it. Tests release blocked services before goleak runs.
- **[Risk] The default flip and the renames break every current consumer.** Mitigation: the D10 table goes into `docs/docs/migrating/` and CHANGELOG. This is the first release, so no tagged consumer has it yet.
- **[Risk] add-config conflicts (it keeps `DefaultRootScope` and changes the `NewWithOpts` signature).** Mitigation: land this change first, then update add-config's specs (`container-construction`) with `/opsx:update`.
- **[Trade-off] Closing a scope for good removes "reuse after shutdown",** which upstream allowed. Correctness wins: reuse made services that were never shut down possible.
- **[Trade-off] The read-lock fast path (D17.1) may show little gain on an overloaded host.** It is kept because it removes all library lock contention in profiles. A benchmark guards against regressions.

## Public API & Defaults

**Added:**
- `ErrServiceTypeMismatch`, `ErrServiceAlreadyDeclared`, `ErrProviderPanic`, `ErrShutdown`, `ErrShutdownPanic`, `ErrNilInjector`, `ErrStructInjection`
- `ServiceError`
- `Clock`, `Timer`, `RealClock()`
- `InjectorOpts.Logger`, `InjectorOpts.MaxInvocationFrames`, `InjectorOpts.EnableStarterAutoDetection`
- `ShutdownReport.Err()`, `ShutdownReport.Unwrap()`, `ShutdownReport.MarshalJSON()`, `HealthReport.MarshalJSON()`
- `MarshalText`/`UnmarshalText` on the enums
- `ExplainServiceDependencyOutput.Ref`
- `dogerhttp.Option`, `dogerhttp.WithFullSourcePaths()`, `dogerhttp.WithErrorHandler()`
- Module `clock/clockwork`, with `Wrap` and `Unwrap`
- `dogergocron.ErrUnsupportedClock`

**Changed or removed (BREAKING):** every row of the D10 table, plus the hook signatures in D9.

**Defaults and their overrides:** see the proposal's "Defaults & Control" table. In short:
- Safe behavior is on with zero configuration: panic isolation, closed scopes, bounded explanation, escaping, a 10s health-check bound, discarded logs, and Starter auto-detection turned off.
- Every knob sits on `InjectorOpts`, an `Option`, or a hook.

## Test Strategy

The table at the end maps every spec scenario to its Go test, script or CI job.

Test kinds used:
- **Table-driven unit tests:** testify, with named subtests.
- **Race tests:** every test runs under `-race`.
- **goleak:** the root `TestMain`, plus the `tests` package (added by this change).
- **Child-process tests:** the regression tests for the panic crash. A panic on a shutdown goroutine cannot be recovered in-process before the fix, so they run in a child process.
- **`synctest` / fake clock:** for deadlines.
- **`httptest`:** for all five adapters.
- **Fuzz:** explain text and the source-path shortener.
- **Benchmarks:** for D17.
- **`Example*` tests:** for every new or renamed public API.
- **Script checks:** the CI module loop and the release dry run (`release-integrity`).

Hot paths and the tests that cover them:

| Hot path | Covering tests |
|---|---|
| Invoke of a built singleton (fast path) | `TestInvoke_*`, `BenchmarkInvokeBuiltParallel`, concurrent-resolution race tests |
| First build under contention | `TestConcurrentResolution_*` |
| Scope create/shutdown per request | `TestContainerShutdown_ChildDetach*`, `BenchmarkPerRequestScope` |
| Shutdown ordering and panic/deadline handling | `TestContainerShutdown_*`, `TestShutdownContext_*`, mutant-killing chain-order test |
| Health execution | `TestHealth*` (existing), plus the Goexit and first-monitored-read tests |
| Web UI request handling | `http/*/web_ui_test.go` per adapter |

| Scenario | Test | Kind |
|----------|------|------|
| A panicking Shutdown method is recorded in the report | container_shutdown_test.go: TestContainerShutdown_ServiceShutdownPanic (/root_string, /root_error, /root_typed, /root_nil, /child, /child_via_root) | table, race |
| A panicking eager value is recorded in the report | container_shutdown_test.go: TestContainerShutdown_EagerShutdownPanic | unit, race |
| The crash is gone in a separate process | container_shutdown_test.go: TestContainerShutdown_ShutdownPanicDoesNotCrashProcess | child-process |
| A panicking shutdown hook is recorded for its service | container_shutdown_test.go: TestContainerShutdown_ShutdownHookPanic (/before, /after, /after_with_shutdown_error, /before_error_value) | table, race |
| The first shutdown panic goes to HookContainerPanic | container_shutdown_test.go: TestContainerShutdown_FirstPanicToHook (/one, /three, /nil_hook) | table, race |
| A HookContainerPanic that re-panics is the consumer's choice | container_shutdown_test.go: TestContainerShutdown_PanicHookRepanics | unit |
| An invocation that raced the shutdown is refused | container_shutdown_test.go: TestContainerShutdown_NoResurrection | race, handshake |
| Invoking a service shut down by name | container_shutdown_test.go: TestContainerShutdown_InvokeAfterShutdownNamed | unit |
| Concurrent invocations during shutdown leave nothing running | container_shutdown_test.go: TestContainerShutdown_ConcurrentInvokeDuringShutdown | race, goleak, stress ×50 |
| Calls on a closed scope are refused | container_shutdown_test.go: TestContainerShutdown_ClosedScopeRefuses (row per call) | table |
| A second shutdown of a closed scope succeeds with nothing to do | container_shutdown_test.go: TestContainerShutdown_SecondShutdownNoop (/child_twice, /root_then_child) | table |
| A scope created while its parent closes is refused | container_shutdown_test.go: TestContainerShutdown_ScopeDuringShutdownRefused (/closing_hook, /scope_closing_hook) | table |
| A shut-down child is detached and its name reusable | container_shutdown_test.go: TestContainerShutdown_ChildDetached (/shutdown, /with_deadline) | table |
| Per-request scopes do not accumulate | container_shutdown_test.go: TestContainerShutdown_ChildDetachNoGrowth (/1, /1000) | table |
| Concurrent per-request scopes detach | container_shutdown_test.go: TestContainerShutdown_ChildDetachConcurrent | race, goleak |
| A grandchild detaches from its own parent only | container_shutdown_test.go: TestContainerShutdown_GrandchildDetach | unit |
| A shared eager value is shut down once | container_shutdown_test.go: TestContainerShutdown_SharedEagerOnce (/source_first, /clone_first, /concurrent) | table, race |
| Lazy services stay per container | container_shutdown_test.go: TestContainerShutdown_CloneLazyPerContainer | unit |
| A re-entrant root shutdown returns at once | container_shutdown_test.go: TestContainerShutdown_ReentrantRootShutdown (/same_ctx, /derived_ctx, /unrelated_deadline) | table, synctest |
| Err and Unwrap reflect the report | container_shutdown_test.go: TestShutdownReport_ErrUnwrap (/success, /one, /two_scopes, /panic) | table |
| errors.As finds the failing service | container_shutdown_test.go: TestShutdownReport_ErrorsAsServiceError | unit |
| A report marshals to JSON | container_shutdown_test.go: TestShutdownReport_MarshalJSON (/success, /one_error) | table, synctest |
| A service reached after the context ended is skipped | root_scope_test.go: TestRootScope_ShutdownWithEndedContext (existing; alias row → AliasNamed) | table |
| An exported service reached after the context ended is skipped | root_scope_test.go: TestRootScope_ShutdownExportedAfterContextEnded (existing) | unit |
| A deadline that ends during the shutdown skips the services after it | root_scope_test.go: TestRootScope_ShutdownDeadlineDuringShutdown (assertions updated) | synctest |
| The signal helper shuts down when its context ends | container_hooks_test.go: TestContainerHooks_ShutdownOnSignalsWithContext (existing) | table |
| The signal helper shuts down after a signal even when its context ends with it | root_scope_unix_test.go: TestRootScope_ShutdownOnSignalsWithContext_Signal (existing) | unix, race ×50 |
| The signal helper accepts a nil context | root_scope_unix_test.go: TestRootScope_ShutdownOnSignalsWithContext_NilContext | unix, race |
| A hung Shutdown does not hold the call past its deadline | shutdown_context_test.go: TestShutdownContext_HungShutdownBounded (/root_Shutdown, /root_ShutdownErr, /root_ShutdownCtx, /root_ShutdownCtxErr, /child, /child_via_root) | table, synctest, goleak |
| A slow Shutdown is waited for when the context allows it | shutdown_context_test.go: TestShutdownContext_SlowShutdownWaited (/background, /longer_deadline, /instant) | table, synctest |
| A stuck provider does not hold a bounded shutdown | shutdown_context_test.go: TestShutdownContext_InFlightBuildBounded (/late_success, /late_failure) | table, synctest, goleak |
| An unbounded shutdown waits for the build | shutdown_context_test.go: TestShutdownContext_InFlightBuildWaited | synctest |
| Target is shut down once | service_alias_test.go: TestServiceAlias_ShutdownTargetOnce (existing; As → Alias) | table |
| Repeated runs are stable | service_alias_test.go: TestServiceAlias_ShutdownTargetOnce_Stable (existing) | race ×200 |
| Invoking the alias after its target is shut down | service_alias_test.go: TestServiceAlias_InvokeAfterTargetShutdown (existing) | unit |
| Exported target is shut down once | export_test.go: TestExport_TargetShutdownOnce (existing; WithExportName) | table |
| Alias functions register an alias | service_alias_test.go: TestAlias_Functions (row per call) | table |
| Bind keeps working | service_alias_test.go: TestAlias_BindUnchanged | unit |
| The As identifiers are gone | api_surface_test.go: TestAPISurface_AliasRename | unit |
| Export publishes under the source or a chosen name | export_test.go: TestExport_Publishes (existing, renamed option) | table |
| Exported services stay private otherwise | export_test.go: TestExport_OthersStayPrivate (existing) | unit |
| Export from a grandchild through two scopes | export_test.go: TestExport_Chained (existing) | unit |
| ExportAs is renamed WithExportName | api_surface_test.go: TestAPISurface_ExportNameRename | unit |
| Invalid exports are rejected | export_test.go: TestExport_Invalid (sentinel and prefix assertions) | table |
| A service visible only through an ancestor cannot be exported | export_test.go: TestExport_SourceMustBeOwn | unit |
| Must variants panic with the error | export_test.go: TestMustExport_Panics | table |
| Invocation scope does not change the target | export_test.go: TestExport_ResolvesFromSource (existing) | table |
| An export is listed as an alias | export_test.go: TestExport_ListedAsAlias (existing) | unit |
| A same-name grandchild does not capture the export | export_test.go: TestExport_DirectChildOnly (existing) | unit ×100 |
| An export replaces an unresolved default in the parent | export_test.go: TestExport_ReplacesUnresolvedDefault (existing) | unit |
| Exported Starter and Shutdowner run once | export_test.go: TestExport_Lifecycle (AsStarter added) | table |
| Root health check reports the exported service | export_test.go: TestExport_RootHealthCheck (existing) | unit |
| A health check from the source scope checks the exported service once | export_test.go: TestExport_SourceScopeHealthCheck (existing) | table |
| Collection membership of an export | export_test.go: TestExport_CollectionMembership (existing) | table |
| InvokeAs in the parent finds the export | export_test.go: TestExport_InvokeAsFindsExport | unit |
| A provider does not select its own export by type | export_test.go: TestExport_InvokeAsSkipsOwnExport (existing) | table |
| Concurrent resolution of a cycle returns ErrCircularDependency | concurrent_resolution_test.go: TestConcurrentResolution_CycleReturnsError | race, goleak, table |
| A cycle through a declared dependency is detected concurrently | concurrent_resolution_test.go: TestConcurrentResolution_DeclaredDependencyCycle | race |
| A failed cyclic build is retried, not cached | concurrent_resolution_test.go: TestConcurrentResolution_CycleRetried | race |
| Re-entrant resolution through a captured injector | concurrent_resolution_test.go: TestConcurrentResolution_ReentrantCapturedInjector | table, testWithTimeout |
| Resolving another service through a captured injector still works | concurrent_resolution_test.go: TestConcurrentResolution_CapturedInjectorNoCycle | unit |
| Single build under contention | concurrent_resolution_test.go: TestConcurrentResolution_SingleBuild | race, table |
| Concurrent builds of a shared dependency are not cycles | concurrent_resolution_test.go: TestConcurrentResolution_SharedDependencyNoFalseCycle | race, table |
| Waiters receive the build failure | concurrent_resolution_test.go: TestConcurrentResolution_WaitersShareFailure | race, goleak, table |
| Invoking a name under the wrong type | resolution_errors_test.go: TestResolutionErrors_TypeMismatch | table |
| MustInvokeNamed panics with the mismatch | resolution_errors_test.go: TestResolutionErrors_MustTypeMismatchPanics | unit |
| Errors.As finds the failing service | resolution_errors_test.go: TestResolutionErrors_ServiceErrorWrapping | table |
| ServiceError reports the scope of the failing service | resolution_errors_test.go: TestResolutionErrors_ServiceErrorScope | unit |
| Duplicate registrations | resolution_errors_test.go: TestResolutionErrors_DuplicateRegistration | table |
| Override replaces without an error | resolution_errors_test.go: TestResolutionErrors_OverrideNoError | unit |
| Error-returning entry points with a nil injector | resolution_errors_test.go: TestResolutionErrors_NilInjectorReturnsError | table |
| Entry points without an error result panic on a nil injector | resolution_errors_test.go: TestResolutionErrors_NilInjectorPanics | table |
| Not-found message size | resolution_errors_test.go: TestResolutionErrors_NotFoundCap (/0, /1, /10, /11, /2000) | table |
| The invoker chain follows the capped list | resolution_errors_test.go: TestResolutionErrors_NotFoundCapWithChain | unit |
| Injecting a nil-interface service | struct_injection_test.go: TestStructInjection_NilInterfaceDependency | table |
| MustInvokeStruct with a nil-interface service | struct_injection_test.go: TestStructInjection_MustNilInterfaceDependency | unit |
| Invalid targets | struct_injection_test.go: TestStructInjection_InvalidTargets (incl. the nil-interface rows) | table |
| A named pointer-to-struct target | struct_injection_test.go: TestStructInjection_NamedPointerTarget | unit |
| MustInvokeStruct with a recursive pointer type | struct_injection_test.go: TestStructInjection_MustRecursivePointerType | unit (child process before fix) |
| Dependency failures are not ErrStructInjection | struct_injection_test.go: TestStructInjection_DependencyFailureNotStructError | unit |
| Provider panic values become invocation errors | invoke_test.go: TestInvoke_ProviderPanic (existing, rows updated) | table |
| Consumers can restore the legacy nil-panic behavior | invoke_test.go: TestInvoke_ProviderPanicNil_GODEBUGOverride (existing) | unit |
| Panic in a nested provider | provider_panic_handling_test.go: TestProviderPanic_NestedProvider | unit |
| Selecting a service whose value is a nil interface | type_resolution_test.go: TestInvokeAs_NilInterfaceInstance | table |
| A non-nil selection is unchanged | type_resolution_test.go: TestInvokeAs_NonNilInstance | unit |
| The invoking service is not a candidate | invoke_test.go: TestInvokeAs_SkipsInvoker (existing, renamed calls) | table |
| MustInvokeAs from a provider | invoke_test.go: TestMustInvokeAs_FromProvider (existing) | unit |
| A stored provider injector still skips its service | invoke_test.go: TestInvokeAs_StoredInjectorSkipsHolder (existing) | unit |
| The alias skip compares names | invoke_test.go: TestInvokeAs_SkipsAliasOfShadowedName (existing, AliasNamed) | unit |
| Not-found errors for library types | invoke_test.go: TestInvoke_NotFoundNamesLibraryTypes (existing, rows updated) | table |
| Struct injection tag key keeps its default and override | module_identity_test.go: TestCompat_StructTagKey (existing) | table |
| Error sentinels keep their messages | module_identity_test.go: TestCompat_ErrorSentinelMessages (existing, rows updated) | table |
| Debug UI links stay relative to the consumer's base path | http/pages_test.go: TestIndexHTML_BasePathLinks (existing) | table |
| Messages that used to double the prefix | module_identity_test.go: TestModuleIdentity_PrefixOnce | table |
| No exported sentinel uses the DI prefix | module_identity_test.go: TestModuleIdentity_AllSentinelsPrefixed (one per module) | table |
| Explain concurrently with a lazy build | service_explanation_test.go: TestExplain_ConcurrentWithLazyBuild (/named, /injector) | race |
| Explain concurrently with shutdown | service_explanation_test.go: TestExplain_ConcurrentWithShutdown | race, goleak |
| Layered graph is bounded | service_explanation_test.go: TestExplain_LayeredGraphBounded (/1, /2, /3, /24) | table |
| Dependents tree is bounded the same way | service_explanation_test.go: TestExplain_DependentsBounded | unit |
| A reference renders as a single line | service_explanation_test.go: TestExplain_RefRendersSingleLine | unit |
| Ref is serialized only when true | service_explanation_test.go: TestExplain_RefJSON | unit |
| Names with HTML metacharacters render verbatim | service_explanation_test.go: TestExplain_PlainText (/named, /injector, /scope_name); FuzzExplainText | table, fuzz |
| A failed dependency records no edge | service_explanation_test.go: TestExplain_FailedDependencyNoEdge (/named, /named_any, /by_type, /struct) | table |
| A successful retry records the edge once | service_explanation_test.go: TestExplain_RetryRecordsEdgeOnce | unit |
| Caller frame from a stacktrace test helper | stacktrace/stacktrace_test.go: TestStacktrace (existing) | unit |
| Provider frame shown by ExplainService | source_tracking_test.go: TestSourceTracking_ProviderFrame (/eager, /alias) | table |
| Invocation frame of an alias service is recorded at the alias implementation | service_alias_test.go: existing alias invocation-frame test (unchanged) | unit |
| Source frame of each registration form | source_tracking_test.go: TestSourceTracking_RegistrationForms (/provide_value, /provide_named_value, /override_value, /override_named_value, /eager, /eager_named, /alias, /nil_value) | table |
| Registration through New records the New call site | source_tracking_test.go: TestSourceTracking_NewCallSite | unit |
| Default resolves when nothing replaces it | service_default_test.go: TestAsDefault_ResolvesWithoutReplacement (existing) | table |
| A nil injector registration is rejected | service_default_test.go: TestAsDefault_NilInjector (new; the former test of that name is TestAsDefault_ResolvesByType) | unit |
| Placement decides whether a default yields | service_default_test.go: TestAsDefault_YieldsByPlacement (existing) | table |
| A grandchild default yields to the root | service_default_test.go: TestAsDefault_GrandchildYieldsToRoot (existing) | unit |
| Registration order does not matter | service_default_test.go: TestAsDefault_OrderIndependent (existing) | table |
| Repeating AsDefault is the same as giving it once | service_default_test.go: TestAsDefault_Repeated (existing) | unit |
| A default does not yield to another default | service_default_test.go: TestAsDefault_DoesNotYieldToDefault (existing) | unit |
| An alias of a default resolves through the default's decision | service_default_test.go: TestAsDefault_AliasResolvesThroughDecision (existing, Alias) | unit |
| Explicit and default registrations of one name in one scope | service_default_test.go: existing same-scope table (doger: message) | table |
| Registration hooks around defaults | service_default_test.go: existing hooks test | table |
| A replaced default keeps its registration position | service_default_test.go: existing position test | unit |
| Concurrent first resolutions agree | service_default_test.go: TestAsDefault_ConcurrentFirstResolution (existing) | race |
| Replacement racing first resolution has one outcome | service_default_test.go: TestAsDefault_ReplaceRacesResolve, …Stress (existing, doger: message) | race |
| An explicit duplicate in one scope still panics | service_default_test.go: existing compatibility test (doger: message) | unit |
| A nearer explicit service still shadows an ancestor's | service_default_test.go: existing compatibility test | unit |
| Yielded default follows the ancestor's current registration | service_default_test.go: TestAsDefault_YieldedFollowsOverride (/none, /override, /override_value, /override_transient, /removed) | table |
| Yielded default through an alias follows an override | service_default_test.go: TestAsDefault_YieldedAliasFollowsOverride | unit |
| Concurrent reads during an override | service_default_test.go: TestAsDefault_YieldedConcurrentOverride | race |
| The caller's options are unchanged after construction | container_options_test.go: TestContainerOptions_CallerOptsUnchanged (/logger, /health_timeout, /frames, /hook_slice_cap, /clone_clock) | table |
| A nil options pointer | container_options_test.go: TestContainerOptions_NilOpts | unit |
| Containers built from one options value are independent | container_options_test.go: TestContainerOptions_SharedOptsIndependent (/new_new, /clone_with_opts, /add_to_b, /clone) | table |
| Adding hooks while the container works | container_options_test.go: TestContainerOptions_AddHookConcurrent (8 adder rows, /nil_hook, /same_hook_twice) | race, goleak |
| A hook added after Add returns applies to the next operation | container_options_test.go: TestContainerOptions_AddHookAppliesNext | unit |
| Frame cap per option value | container_options_test.go: TestContainerOptions_MaxInvocationFrames (/default_150, /default_99, /one, /five, /six, /disabled) | table |
| Two containers keep their own caps | container_options_test.go: TestContainerOptions_FramesPerContainer | race |
| A rejected nil call touches no container | container_options_test.go: TestContainerOptions_NilTouchesNoContainer (/provide_value, /provide_named_value, /invoke) | table |
| Containers are isolated | container_options_test.go: TestContainerOptions_Isolated | unit |
| A container without a logger is silent | container_logging_test.go: TestLogging_OffByDefault (/new, /empty_opts, /nil_opts) | table |
| Message, level and attributes per event | container_logging_test.go: TestLogging_Events (15 rows) | table |
| Records below the handler's level are dropped | container_logging_test.go: TestLogging_LevelFilter (/debug, /info, /error) | table |
| Child scope events carry the child's scope name | container_logging_test.go: TestLogging_ChildScopeAttr | unit |
| Concurrent logging | container_logging_test.go: TestLogging_Concurrent | race |
| A panic value is not logged above Debug | container_logging_test.go: TestLogging_PanicValueDebugOnly (/debug, /info) | table |
| Health-check error text is not logged above Debug | container_logging_test.go: TestLogging_HealthErrorDebugOnly (/debug, /info) | table |
| Each hook receives the documented arguments | service_lifecycle_hooks_test.go: TestServiceHooks_Arguments (/before_start, /after_start, /before_shutdown, /after_shutdown, /after_invocation) | table |
| Hooks added after construction use the same signatures | service_lifecycle_hooks_test.go: TestServiceHooks_AddMethods (5 rows) | table |
| The hook context carries the caller's values and deadline | service_lifecycle_hooks_test.go: TestServiceHooks_Context (5 rows) | table |
| A shutdown without a context passes a background-derived context | service_lifecycle_hooks_test.go: TestServiceHooks_ShutdownBackgroundContext | unit |
| A cancelled shutdown context is visible to the after-hook | service_lifecycle_hooks_test.go: TestServiceHooks_CancelledShutdownContext | synctest |
| Elapsed with a fake clock | service_lifecycle_hooks_test.go: TestServiceHooks_Elapsed (/build, /cached, /start, /shutdown, /zero, /real_clock) | table, fake clock |
| Elapsed is reported for failures too | service_lifecycle_hooks_test.go: TestServiceHooks_ElapsedOnFailure | unit |
| Before, operation, after | service_lifecycle_hooks_test.go: TestServiceHooks_Order (/start_ok, /start_err, /shutdown, /invocation) | table |
| Where a hook panic goes | service_lifecycle_hooks_test.go: TestServiceHooks_PanicPolicy (/before_shutdown, /after_shutdown, /after_invocation, /before_start) | table |
| Reads on a monitored container cost nothing | health_report_test.go: TestHealthReport_MonitoredNoChecks | race |
| A service never checked is checked once by a read | health_report_test.go: TestHealthReport_MonitoredFirstCheck (rows updated, /joins_in_flight, /reader_bound) | unit, synctest |
| A read right after Ready joins the first scheduled check | health_report_test.go: TestHealthReport_ReadAfterReadyJoinsFirstRound | regression, fake clock |
| Concurrent first reads of a monitored container share the round's check | health_report_test.go: TestHealthReport_ConcurrentFirstReadsShareRound | race |
| HealthState and ContainerPhase text round trip | health_report_test.go: TestHealthEnums_TextRoundTrip | table |
| Unknown text is rejected | health_report_test.go: TestHealthEnums_UnmarshalTextRejectsUnknown | table |
| Report JSON | health_report_test.go: TestHealthReport_MarshalJSON | table, fake clock |
| Empty and in-flight entries | health_report_test.go: TestHealthReport_MarshalJSONEdgeCases | table |
| Filtered reports | health_report_test.go: TestHealthReport_MatchTags (nil row is a nil func) | table |
| A filtered on-demand read checks only the covered services | health_report_test.go: TestHealthReport_MatchTagsOnDemand | unit |
| Report options are functions a consumer can compose | health_report_test.go: TestHealthReportOption_Compose; health_report_example_test.go: ExampleHealthReportOption | table, example |
| Each caller keeps its own bound when it joins a check | health_run_test.go: TestHealthCheck_JoinerOwnBound (rows updated, /default_10s) | table, fake clock |
| One caller leaving does not cancel the check for the others | health_run_test.go: TestHealthCheck_CallerLeaves | race |
| Panic results | health_run_test.go: TestHealthCheck_Panic (message rows) | table |
| A panic in a scheduled round does not stop later rounds | health_monitor_test.go: TestHealthRound_PanicKeepsRecording | unit |
| Goexit in a check is reported and does not block the next check | health_run_test.go: TestHealthCheck_GoexitDoesNotWedge | regression, table |
| Every waiter of a check that exits its goroutine gets the error | health_run_test.go: TestHealthCheck_GoexitAllWaiters | race, goleak |
| A scheduled round survives a check that exits its goroutine | health_monitor_test.go: TestHealthRound_GoexitKeepsRecording | unit |
| Health check on an ended context is a timeout | healthcheck_timeout_test.go: TestHealthCheck_EndedContextIsTimeout | table ×1000 |
| Health check that finishes before its context ends keeps its result | healthcheck_timeout_test.go: TestHealthCheck_FinishedInTimeKeepsResult | table |
| Timeout options decide the result on the clock | healthcheck_timeout_test.go: TestHealthCheck_TimeoutOptionsOnClock (default and -1ns rows) | table, fake clock |
| Sequential health checks share the global timeout | healthcheck_timeout_test.go: TestHealthCheck_SequentialChecksShareGlobalTimeout | fake clock |
| Health check does not time out before the clock reaches the timeout | healthcheck_timeout_test.go: TestHealthCheck_NotBeforeClockReachesTimeout (default row) | table, fake clock |
| RootScope.HealthCheck is bounded with zero configuration | healthcheck_timeout_test.go: TestHealthCheck_DefaultTimeoutZeroConfig | regression, synctest, goleak |
| A consumer's own deadline is not measured on the injector's clock | healthcheck_timeout_test.go: TestHealthCheck_ConsumerDeadlineOnContextOnly (-1ns row) | table |
| Per-service checks are bounded by HealthCheckTimeout | healthcheck_timeout_test.go: TestHealthCheck_PerServiceTimeout (rows updated) | table, fake clock |
| A per-service check returning an error in time keeps it | healthcheck_timeout_test.go: TestHealthCheck_PerServiceErrorInTime | unit |
| The scheduler is called only at Ready | health_monitor_test.go: TestHealthScheduler_CalledAtReady (wrapped clock) | table |
| The scheduler receives the real clock by default | health_monitor_test.go: TestHealthScheduler_ReceivesRealClockByDefault | table |
| A scheduler that fails leaves the container unmonitored | health_monitor_test.go: TestHealthScheduler_Failure (slog handler) | table |
| A stop returned with an error is called at once | health_monitor_test.go: TestHealthScheduler_StopReturnedWithError | unit |
| Callers of a check cancelled by Shutdown | health_monitor_test.go: TestHealthScheduler_ShutdownCancelCause | unit |
| A failing stop does not fail Shutdown | health_monitor_test.go: TestHealthScheduler_StopFailure | table |
| Hook panic is contained | health_monitor_test.go: TestHealthHooks_Panic | table |
| Zero configuration measures health-check timeouts on wall time | clock_test.go: TestClock_DefaultRealClock (RealClock and 10s rows) | table, synctest |
| Reported durations come from the clock | clock_test.go: TestClock_ReportedDurations | table, fake clock |
| Timeout errors read the same on any clock | clock_test.go: TestClock_TimeoutErrorSameOnAnyClock (manualClock row) | table |
| Health-check timeouts wait for the clock, not for wall time | clock_test.go: TestClock_TimeoutWaitsForClock | table |
| Derived injectors measure timeouts on the expected clock | clock_test.go: TestClock_DerivedInjectors | table |
| Report times come from the clock | clock_test.go: TestClock_HealthReportTimes | unit |
| Health caching waits for the clock, not for wall time | clock_test.go: TestClock_HealthCacheOnClock | unit |
| The scheduler receives the injector clock | clock_test.go: TestClock_HealthSchedulerClock | table |
| RealClock follows the time package | clock_test.go: TestRealClock | table, synctest |
| A consumer-written clock drives the container | clock_test.go: TestClock_ConsumerClock; clock_example_test.go: ExampleClock | table, goleak, example |
| Wrap and Unwrap round trip | clock/clockwork/clockwork_test.go: TestUnwrap | table |
| A wrapped fake clock drives timers and readings | clock/clockwork/clockwork_test.go: TestWrap_Delegates | table |
| Wrap of nil means the default clock | clock/clockwork/clockwork_test.go: TestWrap_NilIsDefault; clock/clockwork/example_test.go: ExampleWrap | synctest, example |
| Build list of a consumer | scripts/release-dry-run.sh (consumer `go list -m all`) | script |
| Default cadence on the container clock | scheduler/gocron/scheduler_test.go: TestScheduler_DefaultCadence (wrapped clock) | table |
| A slow round skips the next tick | scheduler/gocron/scheduler_test.go: TestScheduler_SingletonSkipsTick | unit |
| Clock mapping | scheduler/gocron/scheduler_test.go: TestScheduler_ClockMapping | table, goleak |
| An unsupported clock leaves the container unmonitored | scheduler/gocron/scheduler_test.go: TestScheduler_UnsupportedClockUnmonitored | unit |
| Each Starter variant is started once | root_start_test.go: TestStart_Variants (auto=false/AsStarter, auto=true) | table, race |
| Which registrations are started | root_start_test.go: TestStart_Detection | table, race |
| A Starter failure stops the remaining Starters | root_start_test.go: TestStart_StarterFailure | table |
| A build failure stops Start before any Starter | root_start_test.go: TestStart_BuildFailure | table |
| AsStarter on an interface whose instance cannot start | root_start_test.go: TestStart_AsStarterInstanceWithoutStart | unit |
| Shutdown after a failed Start reaches every built service | root_start_test.go: TestStart_ShutdownAfterFailure | unit |
| A panic escaping Start still releases Shutdown | root_start_test.go: TestStart_PanicReleasesShutdown | unit, testWithTimeout |
| Repeated and late Start calls | root_start_test.go: TestStart_OneShot (new row: failed Start then Shutdown) | table |
| Concurrent Start calls | root_start_test.go: TestStart_Concurrent | race, goleak |
| Concurrent Start and Invoke | root_start_test.go: TestStart_ConcurrentInvoke | race |
| Clone of a started container | root_start_test.go: TestStart_Clone | table |
| Shutdown cancels a running Start | root_start_test.go: TestStart_ShutdownCancels | synctest, race |
| The stored report is isolated from the first caller | root_scope_test.go: TestRootScope_ShutdownReportIsolated | unit |
| Later Shutdown with an ended context still returns the first report | root_scope_test.go: TestRootScope_ShutdownWithEndedContext | table |
| Nil Shutdown context | root_scope_test.go: TestRootScope_ShutdownNilContext | table |
| A panicking first caller still releases the others | root_scope_test.go: TestRootScope_ShutdownWinnerPanic (Logger handler panic) | table, testWithTimeout |
| Incremental start in the wrong phase | root_start_test.go: TestIncrementalStart_Phase | table |
| Incremental start while Start runs | root_refresh_test.go: TestIncrementalStart_DuringStart | table, race |
| A Refresh queued behind a Shutdown | root_refresh_test.go: TestRefresh_QueuedBehindShutdown | race, goleak |
| Concurrent incremental starts | root_start_test.go: TestRefresh_Concurrent | race |
| Shutdown cancels a running Refresh | root_start_test.go: TestRefresh_ShutdownCancels | synctest |
| Shutdown interrupts a scope Starting hook | root_refresh_test.go: TestScopeStartingHook_ShutdownCause | synctest |
| A failure followed by Shutdown under parallel start | root_start_test.go: TestStart_FailureThenShutdown | synctest, table |
| StartError fields per failure point | root_start_test.go: TestStartError_Fields | table |
| Incremental start failures are StartError values | root_refresh_test.go: TestStartError_Incremental | table |
| StartError values built by the consumer | errors_test.go: TestStartError_ConsumerBuilt | table |
| Errors that are not service failures are not StartError | root_start_test.go: TestStartError_NotForGuards | table |
| Caller cancellation, then Shutdown | root_start_test.go: TestStart_CallerCancelThenShutdown | synctest, table |
| The abort outcome rule | root_start_test.go: TestAbortOutcome | table |
| Auto-detection toggle | root_start_test.go: TestStart_AutoDetectionOptIn (replaces TestStart_DisableAutoDetection) | table |
| Familiar Start methods are not started by default | root_start_test.go: TestStart_FamiliarStartNotStarted | table, goleak |
| Opting a service into a custom kind | kind_test.go: TestKind_OptIn | unit |
| Same name, distinct kinds | kind_test.go: TestKind_DistinctIdentity | unit |
| Kind opt-in on an interface-typed registration | kind_test.go: TestKind_InterfaceRegistration | table |
| Invalid kind values panic | kind_test.go: TestKind_InvalidValues (new row Stage(0)) | table, panic |
| Clone keeps replaced handlers | kind_test.go: TestKind_CloneCopiesHandlers | table |
| Replacing a handler on the clone leaves the source unchanged | kind_test.go: TestKind_CloneReplaceIsolated | table |
| Handler stage relative to Starters | kind_test.go: TestKind_Stage | table |
| Replacing a handler | kind_test.go: TestKind_ReplaceHandler | unit |
| Nil root is rejected | kind_test.go: TestKind_NilRootRejected (new; the former TestKind_NilRootUsesDefault is TestKind_HandlerOnRoot) | table, panic |
| Kind without a handler | kind_test.go: TestKind_NoHandler | unit |
| Runners run after Starters | kind_test.go: TestRunnable_RunsAfterStarters | unit |
| Runner failure | kind_test.go: TestRunnable_Failure | unit |
| Runner auto-detection | kind_test.go: TestRunnable_AutoDetection | table |
| Replacing the Runnable handler | kind_test.go: TestRunnable_ReplaceHandler | unit, goleak |
| Runnable returns the built-in kind on every call | kind_test.go: TestRunnable_FuncReturnsBuiltIn (replaces TestRunnable_ReassignedVariable) | unit |
| Stage text conversions | kind_test.go: TestStage_Text | table |
| Decoding stage text | kind_test.go: TestStage_UnmarshalText | table |
| Declaring edges dynamically | dependency_test.go: TestDeclareDependency (nil/typed-nil rows) | table |
| Concurrent declarations are race-free | dependency_test.go: TestDeclareDependency_Concurrent | race |
| Start rejects a cycle closed by a dynamic edge | root_start_test.go: TestStart_RejectsDeclaredCycle | unit |
| Source per edge | dependency_test.go: TestExplain_EdgeSource | table |
| An edge both depended on and invoked | dependency_test.go: TestExplain_EdgeSourceStrongest | unit |
| Dependents report the same source | dependency_test.go: TestExplain_EdgeSourceDependents | unit |
| Output without declared edges is unchanged | dependency_test.go: TestExplain_EdgeSourceCompat | unit |
| Explained output round-trips through JSON | dependency_test.go: TestDependencySource_UnmarshalText (rows "unknown", "") | table |
| Encoding dependency sources | dependency_test.go: TestDependencySource_MarshalText | table |
| Placement by author and installer | extension_test.go: TestExtend_Placement | table |
| Repeated and colliding installations | extension_test.go: TestExtend_Repeat | table |
| A scope-name collision is resolved by the installer | extension_test.go: TestExtend_ScopeNameResolvesCollision | unit |
| Failure is reported and can be retried | extension_test.go: TestExtend_FailureRollback | table |
| A flattened extension keeps its partial registrations | extension_test.go: TestExtend_ExtendIntoRootNoRollback (renamed from TestExtend_FlattenNoRollback) | unit |
| A disabled extension installs nothing | extension_test.go: TestExtend_Disabled (recording slog handler) | table |
| A flattened declined install keeps its registrations | extension_test.go: TestExtend_DisabledFlattened | unit |
| A declined install whose shutdown fails is reported | extension_test.go: TestExtend_DisabledShutdownFails | unit |
| An explicit Extend after Require | extension_test.go: TestExtend_AfterRequire | table |
| A required install placed flat by its author is never replaced | extension_test.go: TestExtend_AfterRequireAuthorFlat | unit |
| A failing replacement restores the required install | extension_test.go: TestExtend_FailedReplacementRestores | table |
| Extend in each phase | extension_test.go: TestExtend_Phases | table |
| Lifecycle of extension services | extension_test.go: TestExtend_Lifecycle | unit |
| Only flattened extensions leave the root health check unchanged | extension_test.go: TestExtend_FlattenedHealthCheck | unit |
| Default private layout with an export | extension_test.go: TestExtend_PrivateLayout | table |
| The context describes the installation | extension_test.go: TestExtend_Context | table |
| A nil root is rejected | extension_test.go: TestExtend_NilRootRejected (new; the former TestExtend_NilRoot is TestExtend_UnderRoot) | table |
| Clone, override and alias | options_test.go: TestProvideOptions_CloneOverrideAlias | table |
| Hostile names are escaped on every page | http/{std,gin,echo,fiber,chi}/web_ui_test.go: TestWebUI_HostileNamesEscaped/<page> | httptest |
| Page functions escape names without an adapter | http/pages_test.go: TestPages_EscapeNames/<function> | unit |
| A base path cannot inject markup | http/pages_test.go: TestPages_BasePathEscaped | unit |
| A link opens the service it names | http/std/web_ui_test.go: TestWebUI_LinkOpensNamedService/<name> | httptest |
| Links resolve on every adapter | http/{adapter}/web_ui_test.go: TestWebUI_LinksResolve | httptest |
| Headers are set on every route | http/{adapter}/web_ui_test.go: TestWebUI_DefensiveHeaders/<path> | httptest |
| Consumer middleware can add headers | http/std/web_ui_test.go: TestWebUI_MiddlewareAddsHeaders | httptest |
| A layered graph renders each service once | http/{adapter}/web_ui_test.go: TestWebUI_LayeredGraphBounded | httptest, size/time bound |
| Tree size follows the graph shape | http/pages_test.go: TestPages_TreeSizeFollowsGraph/<graph> | table |
| A reference links to the referenced service | http/pages_test.go: TestPages_ReferenceLinks | unit |
| Source path display follows the option | http/{adapter}/web_ui_test.go: TestWebUI_SourcePathDisplay/<options> | httptest |
| Shortening handles unusual paths | http/pages_test.go: TestPages_ShortenSourcePath/<recorded>; FuzzShortenSourcePath | unit, fuzz |
| A service never invoked shows no location | http/pages_test.go: TestPages_NeverInvokedNoLocation | unit |
| Viewing every page builds nothing | http/{adapter}/web_ui_test.go: TestWebUI_ReadOnly | httptest, counters |
| Non-GET methods are rejected | http/{adapter}/web_ui_test.go: TestWebUI_NonGETRejected/<method> | httptest |
| Fallbacks for missing and unknown identifiers | http/std/web_ui_test.go: TestWebUI_Fallbacks/<path> | httptest |
| Query values are never reflected | http/{adapter}/web_ui_test.go: TestWebUI_NoReflection | httptest |
| A failing page returns a generic 500 | http/{adapter}/web_ui_test.go: TestWebUI_RenderFailureGeneric500 | unit |
| A consumer error handler receives the render failure | http/{adapter}/web_ui_test.go: TestWebUI_ErrorHandler/{record,none,nil,last_wins} | table (httptest) |
| Authentication middleware guards the UI | http/{adapter}/web_ui_test.go: TestWebUI_BehindAuthMiddleware/<credentials> | httptest |
| Existing chi mounts keep compiling | http/chi/web_ui_test.go: TestWebUI_ChiMuxStillAccepted | unit |
| Viewers race with container churn | http/{std,chi}/web_ui_test.go: TestWebUI_ConcurrentViewing | race, goleak |
| make modules lists every workspace module | scripts/check-workflows.sh (modules) / CI test.yml | script |
| A failure in any module fails make test-all | scripts/test-make-targets.sh | script |
| A clean tree passes make test-all | CI test.yml job test-all | ci |
| CI runs the module loop on three operating systems | scripts/check-workflows.sh (test.yml matrix) | script |
| An issue in any module fails make lint-all | scripts/test-make-targets.sh (planted lint issue) | script |
| The head of main is lint-clean | CI lint.yml job lint-all + scripts/check-workflows.sh (nolint reasons) | ci |
| A reachable vulnerability fails the gate | CI security.yml job govulncheck (make vuln) | ci |
| Released dependency floors | scripts/check-workflows.sh (go.mod floors) | script |
| An accepted vulnerability without a fix is recorded | scripts/check-workflows.sh (SECURITY.md advisories) | script |
| A clean consumer builds every released module | scripts/release-dry-run.sh v0.99.0 | script |
| Released adapters pin the root release | scripts/release-dry-run.sh v0.99.0 | script |
| The dry run rejects an invalid version | scripts/release-dry-run.sh <version> (CI release-dry-run job) | script |
| Test-only requirements are stripped | scripts/release-dry-run.sh v0.99.0 | script |
| Runtime requirements are kept | scripts/release-dry-run.sh v0.99.0 | script |
| The release workflow enforces provenance | scripts/check-workflows.sh (release.yml) | script |
| A dispatch from another branch is refused | scripts/check-workflows.sh (job if condition) | script |
| Workflow pinning and permissions are checked | scripts/check-workflows.sh (all workflows) | script |
| A tag-pinned action fails the check | scripts/check-workflows.sh --self-test | script |
| Auto-merge decision | scripts/check-workflows.sh (dependabot-automerge if expression) | script |
| Every module directory is covered | scripts/check-workflows.sh (dependabot vs make modules) | script |
| Policy files are present | scripts/check-workflows.sh (policy files) | script |
| No hooks for an AsStarter instance without a Start method | root_start_test.go: TestStart_AsStarterInstanceWithoutStart/no_hooks (existing) | unit |
| Start candidates with defaults | root_start_test.go: TestStart_CandidatesWithDefaults | table |
| A default yields even when it was never invoked before Start | service_default_test.go: TestAsDefault_YieldedLifecycle (existing) | unit |
| Scope.Start skips a default that yields | service_default_test.go: TestAsDefault_YieldedLifecycle/scope_start (existing) | unit |
| A joining caller gets the check's result after the starter's bound | health_run_test.go: TestHealthCheck_JoinerGetsCheckResult (existing) | fake clock |
| A caller whose context has already ended starts no check | health_run_test.go: TestHealthCheck_EndedContextStartsNoCheck (existing) | unit |
| A caller that gives up behind an abandoned check starts no fresh check | health_run_test.go: TestHealthCheck_EndedContextQueuesNoFreshCheck (existing) | unit |
| Deadline and values reach the check | health_run_test.go: TestHealthCheck_StarterContext (existing) | unit |
| The only caller leaving cancels the check | health_run_test.go: TestHealthCheck_LastCallerCancels (existing) | race |
| A check that ignores its context does not pile up | health_run_test.go: TestHealthCheck_NoPileUp (existing) | race |
| Concurrent forced calls share the in-flight checks | health_run_test.go: TestHealthCheck_ConcurrentShareInFlight (existing) | race |
| A call that joins a running check gets its result | health_run_test.go: TestHealthCheck_JoinRunning (existing) | unit |
| An abandoned check is never joined | health_run_test.go: TestHealthCheck_AbandonedNotJoined (existing) | unit |
| Sequential calls check again | health_run_test.go: TestHealthCheck_SequentialCheckAgain (existing) | unit |
| A caller's per-service bound does not count the wait for a slot | health_run_test.go: TestHealthCheck_BoundStartsAtSlot (existing) | fake clock |
| HealthCheckGlobalTimeout bounds the wait for a slot from the call | health_run_test.go: TestHealthCheck_GlobalTimeoutBoundsSlotWait (existing) | fake clock |
| A caller queued behind an abandoned check keeps its bound from its call | health_run_test.go: TestHealthCheck_QueuedBehindAbandonedKeepsBound (existing) | fake clock |
| Each underlying check runs once | health_run_test.go: TestHealthCheck_EachCheckOnce (existing) | unit |
| Round results | health_monitor_test.go: TestHealthRound_Results (existing) | table |
| A round checks each service once | health_monitor_test.go: TestHealthRound_EachCheckOnce (existing) | unit |
| A round after scheduling stopped does nothing | health_monitor_test.go: TestHealthRound_AfterStop (existing) | unit |
| Stop happens before the Closing hooks | health_monitor_test.go: TestHealthScheduler_StopBeforeClosing (existing) | unit |
| A context-aware check in a round is cancelled before services shut down | health_monitor_test.go: TestHealthScheduler_ShutdownCancelsRound (existing) | unit |
| Shutdown does not wait for a check that ignores its context | health_monitor_test.go: TestHealthScheduler_ShutdownSkipsHungCheck (existing) | synctest |
| Service hook calls follow state changes | health_monitor_test.go: TestHookServiceHealthChanged (existing) | table |
| A forced run records changes too | health_monitor_test.go: TestHookServiceHealthChanged_ForcedRun (existing) | unit |
| Hooks run one at a time, in order of addition | health_monitor_test.go: TestHookServiceHealthChanged_Serialized (existing) | race |
| A hook may call health APIs | health_monitor_test.go: TestHookServiceHealthChanged_Reentrant (existing) | unit |
| Nil hooks are skipped | health_monitor_test.go: TestHealthHooks_NilSkipped (existing; Logger handler) | unit |
| Single-service bound | healthcheck_timeout_test.go: TestHealthCheckSingleTimeout (existing, default rows) | table, fake clock |
| Invalid classification | options_test.go: TestHealthOptions_InvalidPanics (existing, doger: rows) | table, panic |
| Classification across clone, override and alias | health_report_test.go: TestHealthClassification_CloneOverrideDefaultAlias (existing, Alias) | table |
| Report membership | health_report_test.go: TestHealthReport_Membership (existing) | table |
| A reader that stops waiting | health_report_test.go: TestHealthReport_ReaderBound (existing) | synctest |
| A reader leaving does not affect other readers | health_report_test.go: TestHealthReport_ReaderLeaves (existing) | race |
| HealthCheckGlobalTimeout does not bound a read | health_report_test.go: TestHealthReport_GlobalTimeoutNotApplied (existing) | fake clock |
| A nil context reads like context.Background() | health_report_test.go: TestHealthReport_NilContext (existing) | unit |
| A check its starter's bound cancelled is recorded with its result | health_report_test.go: TestHealthReport_RecordedAfterStarterBound (existing) | fake clock |
| An abandoned check is not recorded | health_report_test.go: TestHealthReport_AbandonedNotRecorded (existing) | unit |
| Results after shutdown began are not recorded | health_report_test.go: TestHealthReport_NoRecordAfterShutdown (existing) | unit |
| A check in flight when its service leaves is not recorded | health_report_test.go: TestHealthReport_NotRecordedAfterLeaving (existing) | unit |
| A service that leaves before its check starts is not recorded | health_report_test.go: TestHealthReport_NotRecordedLeftBeforeStart (existing) | unit |
| A timeout of a check started after its service left is not recorded | health_report_test.go: TestHealthReport_TimeoutNotRecordedLeftBeforeStart (existing) | unit |
| A service that left before the read reached it is not checked | health_report_test.go: TestHealthReport_LeftBeforeReadNotChecked (existing) | unit |
| Repeated options | options_test.go: TestProvideOptions_Repeated (existing) | table |
| Invalid opt-in combinations | options_test.go: TestProvideOptions_Invalid (existing, doger: rows) | table, panic |
| Options on a default apply only when it uses its own provider | options_test.go: TestProvideOptions_AsDefaultCombined (existing) | table |
| AsDefault adds no registration panic of its own | options_test.go: TestProvideOptions_AsDefaultPanics (existing) | table |
| An invalid option panics even when the default would be skipped | options_test.go: TestProvideOptions_AsDefaultValidatedBeforeSkip (existing) | unit |
| Panicking Order() | invoke_all_order_test.go: existing Order()-panic table (message rows → `doger:`) | table |
| MustInvokeAll surfaces the Order() panic as its panic | invoke_all_order_test.go: existing MustInvokeAll panic test (message → `doger:`) | unit |
| Collection fields | invoke_all_struct_test.go: existing collection-field table (unchanged rows) | table |
| Custom struct tag key | invoke_all_struct_test.go: existing custom-tag test | unit |
| Invalid collection fields | invoke_all_struct_test.go: existing invalid-field table (messages → `doger:`) | table |
| A failing member fails the struct | invoke_all_struct_test.go: existing | unit |
| Struct collections inside a provider exclude the invoking service | invoke_all_struct_test.go: existing | unit |
| Failure point decides which hooks run | container_hooks_test.go: existing Failed-hook table (`ErrShutdown` rows) | table |
| One-shot guard errors do not fire Failed | container_hooks_test.go: existing (`ErrShutdown` row) | table |
| Starting or Closing hook calls Shutdown with a deadline | container_hooks_test.go: existing (`ErrShutdown` rows) | table, synctest |
| Shutdown races a running Start | container_hooks_test.go: existing (`ErrShutdown`) | table, synctest |
| Shutdown interrupts a Starting hook or a late pipeline step | container_hooks_test.go: existing (`ErrShutdown`) | synctest |
| A pipeline panic after Shutdown began | container_hooks_test.go: existing (`ErrShutdown`) | unit |
| A start finishing as Shutdown begins | container_hooks_test.go: existing (`ErrShutdown`) | race |
| Default re-panics after the operation completes | container_hooks_test.go: existing (`ErrShutdown` rows) | table |
| HookContainerPanic overrides the default | container_hooks_test.go: existing, rewritten to a recording slog handler (Error `panic recovered` with point + Debug `panic value` "boom") | table |
| Hooks around each Starter | root_start_test.go: existing per-service start hook test, migrated to `(ctx, scope, name)` / `(ctx, scope, name, elapsed, err)`; Starters opted in with `AsStarter()` | unit |
| Source selection | properties_test.go: existing (panic message → `doger:`) | table |
| A source registered after the first read is not used | properties_test.go: existing | unit |
| Registering the interface name after the first read panics | properties_test.go: existing (message → `doger:`) | unit |
| A failed decision is retried | properties_test.go: existing | unit |
| A failing source provider leaves every property unset | properties_test.go: existing, rewritten to a recording slog handler at Debug (message `property source unavailable`; no record contains "s3cret"/"vault sealed") | unit |
| Values stay out of logs and errors | properties_test.go: existing, rewritten to a recording slog handler at Debug | unit |
| A provider reads properties in the original and the cloned container | properties_test.go: existing | unit |
| Injectors inside and outside extension scopes | properties_test.go: existing (`ExtendIntoRoot()` row) | table |
| Later registrations are visible | collection_test.go: existing live-collection tests | unit |
| Creating a live collection builds nothing | collection_test.go: existing | unit |
| Reads after shutdown and failures | collection_test.go: existing | table |
| Concurrent reads are race-free | collection_test.go: existing | race |
| Starting hook allows or vetoes Scope.Start | scope_hooks_test.go: existing (veto-panic row → `doger: scope \`tenant\` start aborted: scope starting hook panicked: veto-boom`) | table |
| Closing and Closed around child scope shutdown | scope_hooks_test.go: existing, the Logf-panic row becomes a panicking slog handler; message → `doger:` | table |
| Overlapping shutdowns of a child scope join | scope_hooks_test.go / scope_test.go: existing, Logf-panic fixture becomes a panicking slog handler | table |
| Root scope fires no scope hook | scope_hooks_test.go: existing | unit |
| Closing and Closed around an exporting child | scope_hooks_test.go: existing | unit |
| An exporting child with no service left closes once | scope_hooks_test.go: existing | unit |
| Shutdowns of an exporting child join its parent's shutdown | scope_hooks_test.go: existing | race |
| Observer scope hook panics | scope_hooks_test.go: existing | table |
| Later scope hook panics are logged with their point | scope_hooks_test.go: existing, rewritten to a recording slog handler (`panic recovered` point=scope-closing; Debug `panic value` "second") | unit |
| Which invocations fire the build hook | build_hooks_test.go: existing (`Alias[*DB, DBIface]` row) | table |
| Build hook order relative to invocation hooks | build_hooks_test.go: existing (after-invocation hook migrated to `(scope, name, elapsed, err)`) | unit |
| Registration order and nil hooks | build_hooks_test.go: existing | unit |
| Build hook in a child scope and clones | build_hooks_test.go: existing | unit |
| A panicking build hook propagates | build_hooks_test.go: existing | unit |
| Messages that used to double the prefix (new rows: consumer-led abort, provider error) | module_identity_test.go: TestModuleIdentity_PrefixOnce/{consumer_led_abort,provider_error} (new rows) | table |
| Declaring a child scope name twice | resolution_errors_test.go: TestResolutionErrors_DuplicateScope/{same,after_shutdown,sibling,grandchild,other_depth} (new) | table |
| Concurrent declarations of one child scope name | resolution_errors_test.go: TestResolutionErrors_DuplicateScopeConcurrent (new) | race |
| Which calls record a started check | container_logging_test.go: TestLogging_HealthCheckStarted/{single,joined,cancelled,forced} (new) | table |
| The record is invisible above Debug | container_logging_test.go: TestLogging_HealthCheckStartedInfoLevel (new) | unit |
