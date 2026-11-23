# Proposal

## Why

A Starter whose `Start` blocks until shutdown (the usual mistake with an HTTP server that calls `ListenAndServe` in `Start`) makes `RootScope.Start` hang forever, and nothing names the service. A `StartWithContext` deadline does not help either: a Starter that ignores its context runs to its own end (measured: a 100ms deadline returned after 1.5s with `start aborted: context deadline exceeded`). This is documented behaviour, not a defect, but it is the most common integration mistake and today it can only be diagnosed with a goroutine dump. It came up while making `github.com/kartaladev/srvr` pluggable into doger (srvr v0.4.0's `*Server.Start` blocks).

## What Changes

- **Slow start step warning (on by default).** When a service's `Start` method, or a kind handler call for a component (including the built-in `Runnable` kind), is still running after a threshold, the container logs one `WARN` record that names the service, its scope and the step. This happens at most once per step call. The new field `InjectorOpts.SlowStartThreshold` defaults to 10s, and a negative value disables the warning. It applies to `Start`, `Refresh` and `Scope.Start`, because they share one pipeline.
- **Optional abandoning start timeout (off by default).** The new field `InjectorOpts.StartTimeout`: when it is positive, the run stops waiting for a step that is still running after the timeout. When the run's context ends first (caller deadline or cancellation, or Shutdown), the step's context is cancelled at once and the run waits for it up to the timeout, so a step that honours its context returns and is never abandoned. An abandoned step fails the run with a `*StartError` that names the stuck service and wraps the new sentinel `ErrStartTimeout`. The abandoned call keeps running in its goroutine. While it runs, it is not recorded as started (or handled) and is never called again: a covering Refresh or Scope.Start skips it and the services that depend on it, starts the rest, and reports it. After root Start abandoned a step, Start has failed, so only Shutdown follows. This applies to Starters and to kind handlers, including the built-in `Runnable`. If the call later returns nil, it is recorded then, unless its scope is shutting down or gone. If it errs or panics, the next run retries it, as with any failure. The late outcome is logged. Shutdown still runs every Shutdowner as it does today. The context a step receives keeps today's lifetime: it ends when the run ends, and also when the step is abandoned. It reports no deadline, because a step that returns in time keeps it past the timeout.
- **Docs.** The godoc of the `Starter` variants and of `Runner`, plus the README and the docs pages (`service-lifecycle/starter.md`, `container/options.md`), state that a server's `Start` must return once it is listening. They show the wrong (blocking) shape and the right one (listen in `Start`, serve in a goroutine, stop in `Shutdown`), with a runnable `Example`.
- **Root and child scopes behave the same (consistency fixes from the triage).** A child scope Shutdown cancels the running start steps of its subtree, as root Shutdown cancels a whole run, then waits for the run bounded by its context; those steps are skipped, not run failures. A still-running abandoned step's dependents are skipped in any scope. Health reports check a service only once its start is complete, Starters and Runnables alike. The no-overlap guarantee holds per instance, across overrides and clones.
- No breaking API change. Two `InjectorOpts` fields, one sentinel and its entry in the library sentinel list are added. With zero configuration, the new behaviours are the warning (only when a `Logger` is set) and three flagged behaviour changes of existing rules: child scope Shutdown cancels its subtree's running start steps; health reads and rounds skip a component its kind handlers have not handled yet; a clone or same-instance override never calls an instance whose abandoned call still runs (the last only matters with `StartTimeout` set).

## Capabilities

### New Capabilities
<!-- none -->

### Modified Capabilities
- `container-start`: adds the abandoning start timeout and `ErrStartTimeout`, the rule that an abandoned step is not called again while it runs (per instance, across overrides and clones, with its dependents skipped in any scope), and its hook and Shutdown interplay. It modifies "Start honours its context" (which today records "there is no Start timeout option"), "Per-service start hooks" (the Starter's ctx under a timeout, and the abandonment call), "Refresh starts what is not started yet" (the abandoned exception to retry) and "Child scope shutdown waits for a start that is starting its services" (a child scope Shutdown now cancels the running start steps of its subtree, as root Shutdown does).
- `health-reports`: "Services not started yet are not checked by reports" now covers a component whose kind handler has not handled it, so a stuck or abandoned Runnable is unknown like a not-yet-started Starter.
- `container-logging`: adds the `WARN` record for a slow start step and the records for an abandoned step that later ends, and adds their rows to "Events are logged at a fixed level".
- `component-kinds`: "Handlers run for new components on incremental start" gets the abandoned-handler exception, so the built-in Runnable never runs twice at once.
- `container-clock`: "The container measures time only on its clock" also lists the two new start delays.

## Impact

- Code: `root_start.go` (the per-step call shared by Start, Refresh and Scope.Start, the parallel scheduler, the kind handler call), `injector.go` (`InjectorOpts` fields, Clock doc), `errors.go` (`ErrStartTimeout`, `libSentinels`), `di_lifecycle.go` and `runnable.go` (godoc), README, `docs/docs/**`. README Go blocks are pinned by `TestReadme_SnippetsCompiled` (`readme_snippets_test.go`). `docs_snippets_test.go` today pins only the migration guide, so this change extends it to `service-lifecycle/starter.md`, so that the new snippets compile.
- Code, consistency fixes: `scope.go` (a child scope Shutdown cancels its subtree's running start steps), `health_report.go` (report eligibility requires every kind handler to have handled the service), `root_scope.go` (`Clone`/`CloneWithOpts` share the per-instance tracker of abandoned calls).
- API: additive only, and the API surface tests are updated.
- Behaviour changes (flagged, no exported API change): a child scope Shutdown cancels the running start steps of its subtree, as root Shutdown cancels a whole run, instead of waiting without cancelling; health reads and rounds no longer check a component its kind handlers have not handled yet (a stuck Runnable is `HealthUnknown`, not healthy); a clone or a same-instance override no longer calls Start on an instance whose abandoned call still runs.
- Dependencies: none. Timing uses `InjectorOpts.Clock`, so tests stay deterministic under `testing/synctest` or a fake clock.
- Downstream: srvr can keep a blocking `Start` and still be diagnosable, but the docs steer it to the non-blocking shape.

## Defaults & Control

| Behaviour | Zero-config default | Override / opt-out |
|---|---|---|
| Slow start step warning | On, 10s (`DefaultSlowStartThreshold`). Emitted only when `InjectorOpts.Logger` is set, because logging is off by default | `SlowStartThreshold`: a positive value sets the threshold, a negative value disables it. The log destination and level filter come from `Logger`'s handler |
| Abandoning start timeout | Off (`StartTimeout` 0): Start waits for every step, exactly as today | `StartTimeout > 0` sets a per-step bound; when the run's context ends first, the step's context is cancelled at once and the run waits for it up to that bound. The step's context still lives until the run ends unless the step is abandoned. A negative value is off, the same as 0 |
| Slow warning with a timeout | Armed only below the timeout | A `SlowStartThreshold` at or above `StartTimeout` arms no warning: the abandonment reports the step instead |
| Abandoned call cleanup | It keeps running until it returns. The container never calls it again while it runs. This holds per instance (when comparable), across same-instance overrides and clones; dependents are skipped in any scope | The service's Shutdowner, which Shutdown still calls, is the stop path. Honouring ctx avoids abandonment altogether |

Deviation from the brief: the brief asked for "0 disables" on the warning. `InjectorOpts` is a plain struct, so 0 is what a zero-config consumer gets. To be on by default, 0 must mean "default", and disabling takes a negative value. This matches the existing `HealthCheckTimeout` convention.

## Defect

Found in review, in code that predates this change: a Starter whose `Start` returns nil can stay recorded as started after its service was removed. `startCandidate` checks that the scope still holds the service, then records it, in two steps. Every remover (`ShutdownNamed` and root Shutdown through `shutdownTaken`, `removeService`, an override through `registerLocked`) removes the service under the scope lock and only afterwards calls `forget`, without `lifecycleMu`. If a removal and its `forget` both run between the check and the record, the record survives. The removed instance then stays referenced from the started set, and the abandoned-call path (`finishAbandoned`) has the same window.

- Observed: after `ShutdownNamed` removes "srv" between the check and the record of its Start, the started set still holds the removed wrapper.
- Expected: no start record for a removed service.
- Reproduction: no hook sits inside that window, so the regression test (tasks group 12) pins it with an unexported test seam on `RootScope` (design D11) that runs `ShutdownNamed` there. It fails on the current code.

Found while fixing flaky tests (tasks 37.2), also predating this change: a scope shutdown called from an async listener could return while another async delivery to that scope still ran. The shutdown does not wait for a publisher still inside the executor's `Submit`, so that an inline executor cannot deadlock. The default `BoundedEventExecutor` starts the delivery goroutine inside `Submit`, so under load the new delivery reached the shutdown before the publisher left `Submit`, and the shutdown skipped it.

- Observed: `TestScopeShutdown_PublishingDelivery/elsewhere` failed 3 of 6000 loaded runs with the order `[shutdown:returned, publisher:done]`.
- Expected: the scope shutdown waits for the publisher's delivery.
- Reproduction: `TestScopeShutdown_PublishingDeliveryBeforeSubmitReturns` wraps the executor so a nested `Submit` returns only once the drain waits; it failed 20 of 20 runs before the fix. A wrapper whose `Submit` waits for its task keeps the old rule (`TestScopeShutdown_WaitingWrapperExecutor`), so it cannot deadlock.

## Out of scope: findings of the lifecycle sweep

A sweep of the other lifecycle paths used ctx-ignoring callees with a 100ms deadline. Each case is reproduced by a scratch test (`scratchpad/sweep/sweep_test.go`, not committed). Shutdown of services, the per-service shutdown hooks, health checks, builds caught by Shutdown and the async listener drain are already bounded by ctx. These need their own changes:

1. **Remote subscription stop is not abandoned by doger** (`event_remote.go` `closeSubscriptions`). A `stop` that ignores ctx held Shutdown for 800ms, and the report said `Succeed=true`. This is not a defect: `EventTransport.Subscribe` requires the transport's stop function to return "once its context ended" (`event_transport.go:49-50`), so the "bounded by ctx" comment holds for conforming transports. It is a hardening gap: doger could abandon a non-conforming stop the way it abandons a service Shutdown. A follow-up change would claim a defect only if a test shows a conforming transport being held.
2. **Container and scope Closing/Closed hooks and sync `ContainerClosing` listeners run unbounded on the Shutdown caller.** Each held Shutdown for 800ms past a 100ms deadline. `RootScope.ShutdownWithContext` says the call "returns once ctx ends", but only for services and builds. Fixing this is a gap fix: bound or warn, plus docs.
3. **Abandoned service Shutdowns are not logged.** Only the report names them, and the async drain's error carries no listener ID. This could reuse the warning mechanism introduced here.
4. The HealthScheduler stop and `ShutdownOnSignalsWithContext` are unbounded by design and documented as such. No change is proposed for them.
