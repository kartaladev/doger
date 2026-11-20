# Proposal

## Why

An eight-way adversarial audit of `main` @ `8e217ef` (correctness, lifecycle, API, performance, tests, supply chain, web security, core-library abuse) found defects that would hurt production users: a shutdown panic crashes the process, concurrent first resolution of a cycle hangs, services are rebuilt after shutdown, per-request child scopes leak, and the debug web UI has stored XSS. Each one has a failing reproducer. Before the first release we also want to settle the API that v1 locks in. Fixing these after v1 would mean breaking changes or carrying deprecated aliases, so now is the cheapest time.

## What Changes

**Correctness, concurrency and lifecycle fixes**
- A panic in a service `Shutdown`, or in a per-service shutdown hook, is recovered and reported in the `ShutdownReport`. It no longer crashes the process.
- Once a scope begins shutting down it is closed for good. A lazy service is never rebuilt after it was shut down. `Invoke`, `Provide*` and `Scope()` on a closed scope fail with `ErrShutdown`. A child scope created while its parent shuts down is rejected instead of left behind.
- A child scope that shut down is detached from its parent, so its name can be reused and per-request scopes no longer leak memory.
- A dependency cycle hit by concurrent first resolutions, or by a provider that re-enters through an injector it captured, returns `ErrCircularDependency` instead of deadlocking.
- `ShutdownWithContext` returns when its context ends, even while a service's `Shutdown` or a provider still runs. The services still running are reported with the context error.
- These data races are removed: Explain reading build state during a build, hook registration concurrent with invocation, and the global frame cap.
- `InvokeAs` and `InvokeStruct` no longer panic when a service's value is a nil interface. `InvokeStruct` rejects recursive pointer types with an error instead of overflowing the stack.
- A default that yielded to an ancestor resolves that ancestor's current service on each call, including after an override.
- An eager value shared by a container and its clones is shut down exactly once.
- A dependency edge is recorded only for a successful build.
- A health check that calls `runtime.Goexit` no longer blocks that service's later checks.
- `ShutdownOnSignalsWithContext(nil)` behaves like `context.Background()`.
- A re-entrant root `Shutdown` that passes the shutdown context it received returns instead of waiting on itself.
- The first monitored health-report read waits for a first scheduled check that is still running instead of returning `unknown`.
- `NewWithOpts` no longer writes to the caller's `*InjectorOpts`.
- A value registration records the user's call site as its source.

**Error and report design**
- New sentinels and types:
  - `ErrServiceTypeMismatch`: a name is registered, but under another type.
  - `ErrProviderPanic`: a provider panicked; the service name is in the message.
  - `ErrServiceAlreadyDeclared`: matched by the duplicate-registration panic.
  - `ErrNilInjector`.
  - `ErrShutdown`.
  - `ErrShutdownPanic`: a service `Shutdown` or a shutdown hook panicked.
  - `*ServiceError{Scope, Service, Err}`: wraps provider and dependency failures.
- **BREAKING:** `ErrStartAfterShutdown` is replaced by `ErrShutdown`.
- **BREAKING:** error and panic messages carry the `doger: ` prefix once, replacing the uppercase `DI: ` prefix and its doubling.
- A "not found" error lists at most 10 visible service names.
- `ShutdownReport` gains `Err()` (nil on success) and `Unwrap() []error`, and its `Error()` doc is fixed.
- `HealthReport` and `ShutdownReport` marshal to JSON, with enums as text and errors as messages.
- `RootScope.HealthCheck()` gets a default timeout.

**Pre-v1 API cleanup (BREAKING, each with a migration note)**
- Starter auto-detection becomes opt-in: `DisableStarterAutoDetection` is replaced by `EnableStarterAutoDetection`.
- `InjectorOpts.Clock` and `HealthScheduler` use a doger-owned `Clock` interface instead of `jonboulle/clockwork`. A new adapter module, `github.com/kartaladev/doger/clock/clockwork`, keeps clockwork fakes usable.
- `InjectorOpts.Logf` is replaced by `InjectorOpts.Logger *slog.Logger`, with levels.
- Per-service start and shutdown hooks receive the operation's context. The `After*` per-service hooks receive the elapsed duration.
- Renames:
  - `As`, `AsNamed`, `MustAs`, `MustAsNamed` become `Alias`, `AliasNamed`, `MustAlias`, `MustAliasNamed`, so the `As*` prefix only means a registration option.
  - `ExportAs` becomes `WithExportName`.
  - `Flatten` becomes `ExtendIntoRoot`.
- `HealthReportOption` becomes a functional option type.
- The zero values of `Stage` and `DependencySource` become invalid instead of meaning the first constant.
- `Runnable` becomes a function. `MaxInvocationFrames` becomes the `InjectorOpts.MaxInvocationFrames` field.
- `DAG` is unexported.
- `DefaultRootScope` is removed. A nil injector, root or scope fails with `ErrNilInjector` and no longer falls back to a global container.
- `ExplainServiceOutput` expands each dependency once and marks repeats as references, which bounds its size. Explain text is plain text and no longer HTML-escaped.

**Debug web UI (`http/*`)**
- Pages are rendered with `html/template`, and links are built with URL escaping.
- Defensive headers are always set.
- The dependency tree is rendered bounded.
- Source paths are shortened by default.
- A render failure returns a generic 500 with no error text. The error goes only to an optional `WithErrorHandler(func(ctx, err))`.
- `dogerchi.Use` accepts `chi.Router`.
- The example web application shows an authenticated, loopback-only mount.

**Release integrity, CI and supply chain**
- CI builds, tests and lints every workspace module, on Linux, macOS and Windows.
- govulncheck gates CI and releases. The fiber adapter bumps brotli, and the echo and gin adapters bump x/net.
- Actions are pinned to commit SHAs with least-privilege `permissions`. Dependabot auto-merges only patch and minor updates.
- Releases are cut only from `main`, through a protected environment. Each module is tidied and verified after test stripping, and release notes are generated.
- Dependabot covers `scheduler/**`. gosec, errorlint and noctx linters are enabled.
- SECURITY.md, CHANGELOG.md and CODEOWNERS are added. The stale `vercel.json` is removed, Bearer runs at the default log level, and tool versions are pinned.

**Performance (behaviour-preserving)**
- A read-only fast path for invoking an already-built singleton.
- A cached candidate list for `InvokeAll`/`InvokeAllMap`.
- Type-name caching.
- Single-pass stack-frame capture.
- Fewer allocations when no logger is set.
- Linear-time shutdown of deep dependency chains.

**Tests and docs**
- Tests that close the audit's surviving mutants:
  - shutdown order of chains at least 3 deep;
  - lazy `ShutdownerWithContext`;
  - late result versus a recorded timeout;
  - untyped-path invocation tracking.
- The `tests` package gets goleak and the shared timeout helper, and its weak assertions are replaced.
- The gocron tests drop their sleep-based synchronisation.
- The `http/*` adapters get tests.
- Uncovered hot paths get tests: virtual-scope pass-throughs, `ShutdownOnSignals`, and the queued health paths.
- Docs:
  - Add a package doc and docs for the sentinels.
  - Correct the migration guide.
  - Remove or replace the upstream Play links.
  - Fix the README snippet and add a quickstart.
  - Correct `docs/docs/troubleshooting/web-ui.md` and `llms.txt`.
  - Add the missing `Example*` tests.
  - Document the Go 1.27 floor and why it exists.

## Capabilities

### New Capabilities
- `container-shutdown`: covers shutdown safety and its report.
  - Panic isolation.
  - The closed state after shutdown begins: no resurrection, `ErrShutdown`, no orphan child scopes.
  - Detaching a shut-down child and reusing its name.
  - Shared eager values shut down exactly once.
  - Re-entrant root shutdown.
  - `ShutdownReport` `Err`/`Unwrap`/JSON.
- `concurrent-resolution`: deadlock-free first resolution and re-entrant detection, both reporting `ErrCircularDependency`.
- `resolution-errors`: the error taxonomy and the not-found message cap.
  - Type mismatch, `*ServiceError` wrapping, the duplicate-registration sentinel and nil-injector rejection.
- `struct-injection`: plain struct-tag injection, covering nil-interface dependencies and recursive pointer types.
- `service-explanation`: Explain output.
  - Free of data races during builds and shutdown.
  - Bounded expansion with references.
  - Plain-text rendering.
  - Edges only from successful builds.
- `container-options`: `InjectorOpts` handling.
  - Constructors copy the caller's opts.
  - Hook registration is concurrency-safe.
  - A per-container `MaxInvocationFrames`.
  - No global default container.
- `container-logging`: the `slog` logger option: its default, levels per event, and keeping panic values and health errors out of info-level logs.
- `service-lifecycle-hooks`: per-service invocation, start and shutdown hook signatures (context, elapsed) and their panic policy.
- `web-ui`: the debug web UI contract across all adapters: escaping, headers, bounded rendering, source-path display, mounting and auth.
- `release-integrity`: what a consumer of a released tag can rely on.
  - Every module is resolvable without `go.work`, and built, tested and linted.
  - No reachable known vulnerabilities.
  - No test-only requirements in released `go.mod` files.
  - Provenance: released from `main`, with release notes.

### Modified Capabilities
- `shutdown-context`: shutdown returns once its context ends, even if a service's `Shutdown` or an in-flight provider hangs. The signal helper accepts a nil context.
- `provider-panic-handling`: a provider panic matches `ErrProviderPanic` and names the service.
- `type-resolution`: `InvokeAs` returns a nil-interface instance without panicking.
- `default-registrations`: a yielded default re-resolves its ancestor on each call.
- `service-aliases`: `As*` becomes `Alias*` (renamed API).
- `scope-export`: `ExportAs` becomes `WithExportName`.
- `extensions`: `Flatten` becomes `ExtendIntoRoot`. A nil root is rejected.
- `component-kinds`: `Runnable` becomes a function, the zero `Stage` is invalid, and a nil root is rejected.
- `dependency-declaration`: the zero `DependencySource` is invalid, and a nil injector is rejected.
- `container-start`:
  - Starter auto-detection becomes opt-in.
  - `ErrShutdown` replaces `ErrStartAfterShutdown`.
  - The precedence between `ErrShutdown` and `ErrAlreadyStarted` is defined.
- `container-clock`: `Clock` is a doger-owned interface, with a clockwork adapter module.
- `health-check-scheduling`: `HealthScheduler` takes a doger `Clock`.
- `gocron-health-scheduler`: accepts a doger `Clock` and unwraps the clockwork adapter.
- `health-reports`:
  - A first monitored read shares the first scheduled check that is still running.
  - JSON form.
  - `HealthReportOption` becomes a functional option.
- `health-classification`: `MatchHealthTags` and `IgnoreContainerPhase` become functional options.
- `health-check-execution`: a checker that calls `runtime.Goexit` does not block later checks.
- `health-check-timeouts`: `HealthCheck()` gets a default timeout.
- `service-source-tracking`: value registrations record the caller's frame.
- `module-identity`: error and panic messages use the `doger: ` prefix exactly once.
- `registration-options`: renamed alias calls, and Starter opt-in in the metadata scenarios.
- Text-only deltas: these capabilities' requirement text pinned old messages, `Logf`, `ErrStartAfterShutdown`, `Flatten`, the old hook signatures or `DefaultRootScope`, so it is updated to the new API without changing behavior:
  - `collection-ordering`
  - `collection-struct-injection`
  - `container-lifecycle-hooks`
  - `extension-properties`
  - `live-collections`
  - `scope-lifecycle-hooks`
  - `service-build-hooks`

## Impact

- **Code:** root package (most files), `stacktrace`, `http` and all five adapters, `scheduler/gocron`, and a new module `clock/clockwork`.
- **Examples, docs and CI:** all of `examples/*`, `docs/`, `README.md`, `.github/workflows/*`, `.github/dependabot.yml`, `Makefile`, `.golangci.yml`.
- **Breaking exported API:** see the BREAKING items above. design.md lists every break with its migration. The root module drops its `jonboulle/clockwork` requirement for consumers. Root tests may keep it as a test-only requirement, which the release strips.
- **In-flight changes:**
  - `add-config` must rebase on this change. It keeps `DefaultRootScope`, which this change removes, and it changes the `NewWithOpts` signature.
  - `add-config` also touches `health-check-execution`, `extension-properties`, `module-identity` and `toolchain-support`.
  - `add-eventing` and `add-durable-events` must use `Logger`, `Clock` and `ErrShutdown`.
  - This change lands first, as the first release.
- **Behaviour changes for existing users:**
  - Services with a `Start` method are no longer started unless opted in.
  - A container cannot be reused after shutdown.
  - Error message text changes.
  - `RootScope.HealthCheck()` is bounded by default.
  - A nil injector no longer means the global container.

## Defaults & Control

| Behaviour | Zero-config default | Override / opt-out |
|---|---|---|
| Shutdown panic | Recovered and reported as `ErrShutdownPanic` in the report; also sent to `HookContainerPanic` | Set `HookContainerPanic` to handle it, for example to re-panic. Recovery itself is not optional: one service must not kill the shutdown of all the others. |
| Closed scope | `Invoke`/`Provide*`/`Scope()` fail with `ErrShutdown` once shutdown has begun | None. A closed scope that worked would hand out services nobody shuts down. Use a new container instead. |
| Child-scope detach | Detached once its shutdown completes; its name can be reused | None needed. Keeping a dead child has no use, and `Children()` stays accurate. |
| Concurrent cycle | `ErrCircularDependency` | None. A deadlock is never wanted. |
| Shutdown deadline | Returns when ctx ends; running services are reported with `ctx.Err()` | Pass a ctx with no deadline to wait as long as needed. |
| Not-found message | Lists up to 10 names, then "and N more" | The full list is in `*ServiceError` and via `ExplainInjector`. |
| `HealthCheck()` timeout | 10s, the same as the report bound | `HealthCheckTimeout`: a positive value sets it, a negative value means unbounded. |
| Starter auto-detection | Off: only `AsStarter()` services are started | `EnableStarterAutoDetection: true` |
| Clock | Real clock | `InjectorOpts.Clock` (any `doger.Clock`; `dogerclockwork.Wrap(fake)` for clockwork fakes) |
| Logging | Discards everything (`Logger == nil`) | `InjectorOpts.Logger` (any `*slog.Logger`); levels decide what is emitted |
| Invocation frames | 100 per service | `InjectorOpts.MaxInvocationFrames`: positive sets the cap, negative turns recording off |
| Explain expansion | Each service expanded once; repeats are references | None needed. The full graph is still reachable through the references. |
| Web UI escaping and headers | Always on | None. Escaping must not be optional. Callers can add more headers with middleware. |
| Web UI dependency depth | Rendered bounded (each service once) | None needed. Pages link to each service's own page. |
| Web UI source paths | Shortened (base file name and line) | `WithFullSourcePaths()` mount option |
| Web UI render failures | Generic 500 body; the error is discarded (the library never writes to stdout, stderr or a global logger) | `WithErrorHandler(func(ctx, err))` mount option receives each failure with the request context |
| Web UI auth | None built in; docs and example show middleware on a loopback mount | Mount behind any middleware (std handler wrap, gin/echo/fiber group, chi `Router`) |
| Release provenance | Tags only from `main` via the protected environment | Maintainers configure the environment's reviewers |

## Defect

Each defect below has a reproducer that fails on `main` @ `8e217ef`. The files are in the audit scratchpad (`audit/{correctness,lifecycle,api,perf,security-core,security-http,tests}`). The tasks recreate them in the repository as RED regression tests.

| Observed | Expected | Minimal reproduction |
|---|---|---|
| A panicking `Shutdown()` kills the process (`panic: … (*Scope).serviceShutdown`) | Recovered; the report holds an error matching `ErrShutdownPanic` | Register a lazy service whose `Shutdown()` panics, invoke it, call `root.Shutdown()` (run in a child process) |
| `Invoke` succeeds after `Shutdown` returned, and the instance is never shut down (`built 2, shut down 1`) | `ErrShutdown`; no build | `HookBeforeInvocation` blocks `Invoke("svc")` until `root.Shutdown()` returns, then releases it |
| A cycle `a↔b` invoked by 2 goroutines at once hangs past 2s | Both return `ErrCircularDependency` | Two lazy providers that each invoke the other, started in parallel goroutines behind a barrier |
| A provider invoking itself through the captured outer `root` hangs | `ErrCircularDependency` | `ProvideNamed(root,"a", func(_ Injector){ InvokeNamed(root,"b") })`, with `b` invoking `a` |
| 1,000 created and shut-down child scopes stay in `root.Children()`, and `root.Scope("req")` panics with "already declared" | 0 children left; the name can be reused | Loop `root.Scope("req").Shutdown()` |
| `ShutdownWithContext` with a 50ms deadline is still blocked after 1s | Returns within the deadline (plus scheduling slack) with `context.DeadlineExceeded` for the hung service | A service whose `Shutdown()` blocks on a channel |
| `-race` reports a data race between `serviceLazy.build` and `getBuildTime` | No race | `ExplainInjector` concurrently with the first `Invoke` of a slow lazy service |
| `-race` reports a data race on `AddBeforeInvocationHook` | No race | Add a hook while another goroutine invokes |
| `InvokeAs[io.Reader]` panics: `interface conversion: interface is nil` | Returns `nil, nil` | `ProvideNamedValue[io.Reader](root, "r", nil)` then `InvokeAs[io.Reader]` |
| `InvokeStruct` panics: `reflect: call of reflect.Value.Type on zero Value` | The field is set to nil | The same nil service injected into a `do:""` field |
| `InvokeStruct` with `type R *R` ends in `fatal error: stack overflow` | An error naming the field | A struct with an `R` field tagged `do:""` |
| A child default still returns the old instance after `OverrideNamed(root, "x", …)` | Returns the new instance | Root has `x`; the child registers `x` with `AsDefault()`, which yields to the root |
| A clone and its source both shut down a shared eager value (2 calls) | Exactly 1 call | `ProvideValue` with a Shutdowner, `Clone()`, shut both down |
| A scope created during root shutdown stays attached and its services are never shut down | `Scope()` panics with `ErrShutdown` | `HookContainerClosing` creates `root.Scope("late")` |
| `ExplainNamedService` adds an edge to a dependency that failed to build | No edge | `a` invokes `b`; `b`'s provider fails |
| A checker that calls `runtime.Goexit` once makes later checks time out without running | Later checks run the checker | Call `runtime.Goexit()` on the first call only |
| `ShutdownOnSignalsWithContext(nil)` panics with a nil-pointer dereference | Behaves like `context.Background()` | Call it with `nil`, then send `SIGUSR1` |
| A re-entrant root `Shutdown` from a service's `ShutdownWithContext` (shutdown ctx passed through) fails with `waiting for root shutdown: context deadline exceeded` | Returns at once; the outer shutdown proceeds | A service whose `Shutdown(ctx)` calls `root.ShutdownWithContext(ctx)` |
| A monitored `HealthReport` read just after `Start` returns `unknown` | It waits for the first scheduled check in flight (bounded) | A gocron scheduler and a slow checker; read the report right after `Start` |
| A hook added to container A fires in container B when both were built from one opts value | Each container is independent | Pass one `*InjectorOpts` to two `NewWithOpts` calls; `a.AddBeforeInvocationHook` |
| The source of `ProvideNamedValue` is recorded as `service_eager.go:39` | The caller's file and line | `ExplainNamedService` after `ProvideNamedValue` |
| Web UI: a scope named `<script>alert(1)</script>` is served unescaped by all 5 adapters | HTML-escaped | An `httptest` GET of `/scope` |
| Web UI: a service page on a 24-node layered graph returns 26 MB in about 2s | Bounded output | 24 services, each depending on the previous two |
| CI: `make lint` reports 0 issues while the adapters have 8; the adapters are never built or tested | Every module gated | `golangci-lint run ./...` inside `http/chi` |
| govulncheck: GO-2026-6888 reachable via brotli v1.1.0 in `http/fiber` | No reachable vulnerability | `govulncheck ./...` in `http/fiber` |
