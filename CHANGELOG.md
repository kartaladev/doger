# Changelog

All notable changes to the doger modules are recorded here. The root module
(`github.com/kartaladev/doger`) and every adapter module (`http/*`,
`scheduler/*`, `clock/*`) are released together under the same version.
doger is pre-1.0: a minor release may break the API, and every break is
listed under **BREAKING** with its migration.

## Unreleased

First-release hardening (OpenSpec `harden-first-release`).

### BREAKING

Each entry gives the change and its migration.

| Before | After | Migration |
|---|---|---|
| `As`, `AsNamed`, `MustAs`, `MustAsNamed` | `Alias`, `AliasNamed`, `MustAlias`, `MustAliasNamed` | Rename the calls. `Bind`/`BindNamed` are unchanged. |
| `ExportAs(name)` | `WithExportName(name)` | Rename the call. |
| `Flatten()` | `ExtendIntoRoot()` | Rename the call. |
| `HealthReportOption` (struct) | `type HealthReportOption func(*healthReportOptions)` | None for callers of `MatchHealthTags`/`IgnoreContainerPhase`. A zero value no longer exists. |
| `Stage` and `DependencySource` start at 0 | Constants start at 1; 0 is invalid, its `String()` is `"unknown"`, and marshalling or using it fails | Use the named constants. |
| `var Runnable` | `func Runnable() Kind[Runner, struct{}]` | `Runnable.Handle(...)` becomes `Runnable().Handle(...)`. |
| `var MaxInvocationFrames` | `InjectorOpts.MaxInvocationFrames int` (0 means 100, negative turns recording off) | Set the option per container. |
| `DAG` | Unexported | No public use existed. |
| `var DefaultRootScope`, and a nil injector falling back to it | Removed; nil gives `ErrNilInjector` (error-returning APIs) or a panic matching it (others) | Create a container with `doger.New()` and pass it explicitly. |
| `DisableStarterAutoDetection` | `EnableStarterAutoDetection` (default false) | Add `AsStarter()` to services that must start, or set `EnableStarterAutoDetection: true`. |
| `Logf` | `Logger *slog.Logger` | `Logger: slog.Default()`, or any handler. |
| `Clock clockwork.Clock`, `HealthScheduler` taking a `clockwork.Clock` | `doger.Clock` | `Clock: dogerclockwork.Wrap(fake)` (new module `github.com/kartaladev/doger/clock/clockwork`); scheduler implementers take a `doger.Clock`. |
| `ErrStartAfterShutdown` | `ErrShutdown` | Rename the sentinel in `errors.Is` checks. |
| `"DI: "` message prefix | `"doger: "`, carried once | Match with `errors.Is`/`errors.As`, not text. |
| `ExplainServiceDependencyOutput` tree that expands every path | Each service expanded once; a repeat has `Ref: true` and no children | Follow `Ref` nodes through `ExplainNamedService`. |
| Per-service hook signatures `HookBeforeStart`, `HookAfterStart`, `HookBeforeShutdown`, `HookAfterShutdown`, `HookAfterInvocation` | Start and shutdown hooks take `(ctx, scope, serviceName)`, and their "after" hooks also `elapsed time.Duration, err error`; `HookAfterInvocation` takes `(scope, serviceName, elapsed, err)` without a context | Add the `ctx`/`elapsed` parameters to your hook functions. |
| `dogerchi.Use(*chi.Mux, …)` | `dogerchi.Use(chi.Router, …)` | Source-compatible; the parameter is wider. |
| `RootScope.HealthCheck()` unbounded | Bounded by 10s by default | `HealthCheckTimeout: -1` for unbounded. |
| Starting or resolving on a container after shutdown | `ErrShutdown` | Build a new container. |
| An error panic value from a provider returned as is | Wrapped: matches `ErrProviderPanic` and the original | Keep using `errors.Is`/`errors.As`; don't compare with `==`. |
| Web UI page functions and adapter `Use` | Gain a variadic `...dogerhttp.Option` | Source-compatible; no change needed. |
| `ServiceDescription` JSON keys `ScopeID`, `ScopeName`, `Service` | `scopeId`, `scopeName`, `service` (also changes Explain JSON) | Update JSON consumers. |
| `Stage` without a text form | `MarshalText` gives `before-start` / `after-start` | None. |
| `HealthCheckTimeout` 0 meaning none on every forced path | 10s on `RootScope.HealthCheck`, `Scope.HealthCheck` and the per-service functions (`HealthCheckSingleTimeout` 0 inherits it); `HealthCheckGlobalTimeout` ≤ 0 means no global bound | Set a negative value for unbounded. |
| Typed-nil injector, root or scope | Treated as nil: `ErrNilInjector` | Pass a real container. |
| std web UI answered any method | GET and HEAD only; other methods get a 4xx | None for browsers. |
| chi/std wrote `err.Error()` on a render failure | Generic 500 body on every adapter | None. |

### Release and supply chain

- Released `go.mod` files carry no test-only requirements: the root module
  no longer requires `testify`, `goleak` or `clockwork`, and no adapter
  requires `testify` or `goleak`.
- Released adapters require the root module at the same version and have
  no `replace` directive.
- New module `github.com/kartaladev/doger/clock/clockwork`, tagged
  `clock/clockwork/<version>`.
- Dependency floors: `github.com/andybalholm/brotli` v1.2.7 in `http/fiber`
  (GO-2026-6888) and `golang.org/x/net` v0.62.0 in `http/echo` and
  `http/gin`. Echo's GO-2026-6875 and GO-2026-6872 are accepted in
  [SECURITY.md](SECURITY.md).
- Releases are cut only from `main` through the `release` environment, after
  build, vet and `govulncheck` of the post-processed tree.
