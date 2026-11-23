---
title: Starting the container
description: Build and start background components in dependency order
sidebar_position: 3
---

# Starting the container

Background components such as servers, consumers and schedulers need to be started once the container is wired. `Start()` builds every component that needs starting, then starts them in dependency order.

<!-- snippet: starter-quickstart -->
```go
root := doger.New()
doger.Provide(root, NewDB, doger.AsStarter())         // Start(ctx) error, opted in
doger.Provide(root, NewHTTPServer, doger.AsStarter()) // invokes *DB -> started after DB

if err := root.StartWithContext(ctx); err != nil {
	_ = root.Shutdown() // the caller owns cleanup
	return err
}
```

`Start()` and `StartWithContext(ctx)` exist on the root scope only.

## Starter interfaces {#starter-interfaces}

A service registered with `doger.AsStarter()` is started through one of:

<!-- snippet: starter-interfaces -->
```go
type (
	Starter                    interface{ Start() }
	StarterWithError           interface{ Start() error }
	StarterWithContext         interface{ Start(context.Context) }
	StarterWithContextAndError interface{ Start(context.Context) error }
)
```

There is no `Stop` counterpart: a started service is stopped by its [`Shutdown` method](./shutdowner.md) when the container shuts down. A starter without a `Shutdown` method is allowed.

## Which services are started {#detection}

With zero configuration, `Start` starts **only the services registered with `doger.AsStarter()`**, whatever methods their types have. A type with a familiar `Start` method, such as `*exec.Cmd` or `*httptest.Server`, is never started by surprise.

| Registration                                                | Started                                           |
| ----------------------------------------------------------- | ------------------------------------------------- |
| Lazy or eager, with `AsStarter()`                           | yes                                               |
| Lazy or eager, without `AsStarter()`                        | no, unless auto-detection is on (below)           |
| Under an interface type without `Start`, with `AsStarter()` | yes; the instance is checked when `Start` runs    |
| Transient                                                   | never (a factory has no single instance to start) |
| Alias (`Alias`, `Bind`)                                     | no; the target is started once                    |
| Default (`AsDefault()`) that yields                         | no; the service it yields to is started once      |
| Default (`AsDefault()`) that does not yield                 | as the same registration without `AsDefault()`    |

<!-- snippet: starter-interface-opt-in -->
```go
// The interface has no Start method: the instance is checked at Start.
doger.Provide(root, NewLifecycle, doger.AsStarter())
```

:::note Defaults are decided at Start

`Start`, `Refresh` and `Scope.Start` decide every [default registration](../service-registration/default-registrations.md) they would start or hand to a kind: a default yields when an ancestor scope holds an explicit service of its name, and uses its own provider otherwise. The decision is final, so an explicit service of that name registered later in the same scope panics as a duplicate. Root shutdown decides an eager default that was never resolved the same way, and shuts its value down only when the default uses it. A value that was replaced or that yielded belongs to the code that created it.

:::

`Start` covers the root scope and every child scope that exists when it runs. Services registered after `Start`, and scopes created after it, are not started; they stay invokable as usual, and [`Refresh`](#refresh) starts them.

### Enabling auto-detection {#enable-auto-detection}

<!-- snippet: starter-auto-detection -->
```go
root := doger.NewWithOpts(&doger.InjectorOpts{
	EnableStarterAutoDetection: true, // also start every type with a Start method
})
```

Detection then uses the **registered type**, unlike `Shutdowner`, which is detected on the built instance, so a lazy service is discovered without being built; a service registered under an interface type without `Start` still needs `AsStarter()`. A clone keeps the setting.

:::caution Surprising auto-detection

Any registered type with a matching `Start` method is then started, including standard-library types such as `*exec.Cmd`. A `*doger.RootScope` registered as a service of another container is started, but never shut down automatically. Code that already calls `Start()` by hand would start such services twice once it calls `root.Start()`. Keep auto-detection off and opt services in with `AsStarter()`.

:::

## Order {#order}

`Start` runs in two passes:

1. **Build.** Every candidate is built first, so every provider call happens before the first `Start` method. A service's dependencies, whether invoked by its provider or declared with [`DependsOn`](../service-registration/declared-dependencies.md), are built before it.
2. **Start.** Starters start one at a time. A starter starts only after every starter it depends on, directly or through services that are not starters, has started.

The dependency graph is the only source of ordering; there is no `Order` or `Phase` interface. To force an order between services that do not invoke each other, declare a dependency edge with `DependsOn` or `DeclareDependency`.

### Ties {#ties}

Starters with no dependency path between them start in a deterministic order:

1. by scope depth: the root scope first, then its children, then grandchildren;
2. then by registration order. An `Override` in the same scope keeps the position of the first registration; a clone keeps its source's positions, and services registered later in the clone come after them.

<!-- snippet: starter-ties -->
```go
request := root.Scope("request")
doger.ProvideNamed(request, "z", NewZ) // registered first, but in a child scope
doger.ProvideNamed(root, "b", NewB)
doger.ProvideNamed(root, "a", NewA)
_ = root.Start() // starts b, a, z
```

Starters are started sequentially.

### Cycles {#cycles}

A cycle that only exists once services are built, for example a [declared edge](../service-registration/declared-dependencies.md#declare-dependency) later closed by a provider's invocation, makes `Start` fail with an error matching `doger.ErrCircularDependency`, naming the chain, before any starter is started.

## Context {#context}

`StartWithContext(ctx)` derives its own cancellable context from `ctx`; `Start()` uses `context.Background()`, and a `nil` context is treated the same way.

- `Start(ctx)` methods, kind handlers and runners receive the derived context.
- The derived context is **cancelled when `Start` returns**. Do not keep it for long-running work: start your own goroutine with its own context in `Start`, and stop it in `Shutdown`.
- `Start` checks the context before each build, each handler call and each starter. A cancelled or expired context stops `Start` with an error wrapping `ctx.Err()`, so `errors.Is(err, context.DeadlineExceeded)` works.

<!-- snippet: starter-context -->
```go
ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
defer cancel()

if err := root.StartWithContext(ctx); err != nil {
	_ = root.Shutdown()
	return err
}
```

## A Start that never returns {#start-never-returns}

`Start` waits for every `Start` method, and runs kind handlers such as a `Runner`'s `Run` inline. A method that runs until shutdown therefore makes `Start` block forever, and nothing names the culprit. A server is the usual case: `ListenAndServe` returns only once the server stops.

<!-- snippet: starter-blocking-server -->
```go
// Wrong: Start blocks until Shutdown, so root.Start never returns.
func (s *BlockingServer) Start(ctx context.Context) error {
	return s.srv.ListenAndServe()
}
```

Return once the service is ready: bind the listener in `Start`, so a bind error fails `Start`, serve in a goroutine, and stop serving in `Shutdown`.

<!-- snippet: starter-listening-server -->
```go
// Right: Start returns once the server is listening.
func (s *ListeningServer) Start(ctx context.Context) error {
	addr := s.srv.Addr
	if addr == "" {
		addr = ":http" // as ListenAndServe does
	}
	ln, err := new(net.ListenConfig).Listen(ctx, "tcp", addr)
	if err != nil {
		return err // fails root.Start, naming this service
	}
	go func() { _ = s.srv.Serve(ln) }() // http.ErrServerClosed after Shutdown
	return nil
}

func (s *ListeningServer) Shutdown(ctx context.Context) error {
	return s.srv.Shutdown(ctx)
}
```

A `Start` method that ignores its context is awaited even when the context of `StartWithContext` has expired. Two [options](../container/options.md#start-timeout) help you find one:

- **`SlowStartThreshold`** (default 10s, a negative value disables it): a step still running after it is logged once at Warn level as `start step is slow`, naming its scope and service. It needs `InjectorOpts.Logger`.
- **`StartTimeout`** (default off): a step still running after it is abandoned. When the context ends first, the step's context is cancelled and `Start` waits for it up to the timeout. `Start` returns a `*doger.StartError` naming it that matches `doger.ErrStartTimeout`. The method keeps running in its own goroutine and is not called again while it runs; `Shutdown` still calls the service's `Shutdowner`.

## Errors {#errors}

On the first failure, `Start` stops and returns an error that names the service and its scope and wraps the cause (`errors.Is` / `errors.As` work). A failure is:

- a build error, including a missing or circular [declared dependency](../service-registration/declared-dependencies.md);
- an error returned by a `Start` method;
- a panic inside a `Start` method, recovered and returned as an error;
- an `AsStarter()` service whose instance has no `Start` method.

Starters not reached yet are not started, and **nothing is rolled back**. Cleanup is the caller's job:

<!-- snippet: starter-errors -->
```go
if err := root.Start(); err != nil {
	_ = root.Shutdown() // shuts down every built service, started or not
	return err
}
```

Services must therefore tolerate `Shutdown` without a prior `Start`. A panic that escapes `Start` (for example from a hook) still ends the one-shot `Start`: `Phase()` reports `failed`, a later `Start` returns `ErrAlreadyStarted`, and a later `Shutdown()` never blocks on it.

### StartError {#start-error}

A failure of a service or of a kind handler is a `*doger.StartError`. Its `Error()` message is the one shown above, and `errors.Is` reaches the cause through `Unwrap`. Use `errors.AsType` (or `errors.As`) to inspect it:

| Field     | Content                                                             |
| --------- | ------------------------------------------------------------------- |
| `Scope`   | the scope the service is registered in                              |
| `Service` | the service name                                                    |
| `Kind`    | the kind whose handler failed; empty unless `Op` is `StartOpHandle` |
| `Op`      | `StartOpBuild`, `StartOpStart` or `StartOpHandle`                   |
| `Cause`   | the underlying error                                                |

<!-- snippet: starter-start-error -->
```go
if startErr, ok := errors.AsType[*doger.StartError](err); ok {
	log.Printf("%s failed at %s in scope %s: %v", startErr.Service, startErr.Op, startErr.Scope.Name(), startErr.Cause)
}
```

Errors that are not tied to a service are not `StartError` values: `ErrAlreadyStarted`, `ErrShutdown`, a cancelled context and a cycle among the dependency edges checked before the build (for example from `DeclareDependency`). A cycle met while building a service, such as a circular `DependsOn`, is that service's build failure, so it is a `StartError`.

## Shutdown during Start {#shutdown-during-start}

A root `Shutdown()` that begins while `Start` runs:

1. cancels the Start-time context with cause `doger.ErrShutdown`;
2. waits for `Start` to return, bounded by its own context;
3. shuts services down.

`Start` stops before its next starter or handler and returns an error matching `doger.ErrShutdown`.

If Shutdown's own context ends first (for example a 10-second deadline), its report fails with an error for the root scope wrapping `ctx.Err()`, and shutdown proceeds while `Start` is still running. A starter that ignores its context and finishes after that is **not rolled back**: honour `ctx` in `Start(ctx)`.

:::danger Do not call Shutdown from Start-time code

A starter, kind handler, runner, provider or hook that runs during `Start` must not call `root.Shutdown()` synchronously: Shutdown waits for `Start`, which waits for that code. Return an error instead; `Start` stops and the caller shuts down. Calling `Start` from such code returns `ErrAlreadyStarted` immediately, and calling `Refresh` or `scope.Start` returns `ErrNotStarted` immediately.

:::

Only the root scope cancels a whole running Start. A shutdown reached through the root's `*Scope` (the injector a provider receives) bypasses this coordination.

A child scope's `Shutdown()` cancels the context of every running start step (a `Start` method or kind handler call) of that scope and its descendants, with a cause matching `ErrShutdown`, but not the run itself: such a step is neither recorded nor a failure of the run, which skips the rest of that scope's subtree and goes on with the other scopes. The shutdown then waits for a running `Start`, `Refresh` or `scope.Start` that is starting services of that scope, and shuts the scope down, so no service starts after its scope was shut down. A step that ignores its context is awaited, or abandoned at `StartTimeout` when set. A service whose step was skipped this way does not count as started: the run skips every service that depends on it. A dependent inside the shutting-down scope is skipped silently; one outside it, such as a root service that depends on an export of that scope, makes the run fail once everything else started (unless another step failed first), with an error naming the dependent that matches `ErrShutdown`, or, while the skipped step's abandoned call still runs, with that step's error matching `ErrStartTimeout`. When the dependent depends on several skipped services, a still-running one decides the error, then the first by scope name and service name. A run that begins while the scope shuts down skips it, including child scopes created under it meanwhile. `ShutdownWithContext(ctx)` bounds the wait: if `ctx` ends first, the report fails with an error for that scope wrapping `ctx.Err()`, and the shutdown proceeds. A scope that the running start has nothing to start in, such as one a provider creates and shuts down during `Start`, never waits. Shutting down, from Start-time code, a scope that the running start is starting would wait for itself.

## Start is one-shot {#one-shot}

A container starts at most once:

- any later `Start` returns an error matching `doger.ErrAlreadyStarted`, even if the first one failed;
- `Start` after root `Shutdown()` began returns an error matching `doger.ErrShutdown`.

A clone starts independently of its source. Clones share eager instances, so starting a clone starts an eager starter a second time.

## Refresh {#refresh}

`Start` takes a snapshot of the scope tree: services registered after it, and scopes created after it, are not started automatically. `root.Refresh()` (or `RefreshWithContext(ctx)`) starts them: it runs the same pipeline over everything not started or handled yet, with the same order, context, error and per-service hook rules as `Start`.

<!-- snippet: starter-refresh -->
```go
_ = root.Start()

doger.Provide(root, NewReportsWorker) // registered after Start
tenant := root.Scope("tenant")        // created after Start
doger.Provide(tenant, NewTenantWorker)

err := root.Refresh() // starts both workers
```

- Each service starts **at most once** per container: a second `Refresh` starts nothing again, and services started by `Start` are never restarted. An `Override` registered after `Start` is a new service, so `Refresh` starts it.
- Only a success counts. A Starter whose build or `Start` failed during a `Refresh` or `scope.Start`, or a component whose kind handler failed, is tried again by the next `Refresh` or `scope.Start` that covers it. Fix or remove the service before calling it again if a retry is not wanted.
- `Refresh` fires no [container hook](./container-hooks.md).
- Before the first successful `Start`, it returns an error matching `doger.ErrNotStarted`, at once, even while that `Start` is still running. Once root `Shutdown` began, it returns an error matching `doger.ErrShutdown`, which takes precedence. It starts nothing in either case.
- Concurrent `Refresh` calls run one at a time. Never call `Refresh` synchronously from code that runs during a `Refresh` (a Starter, a kind handler, a provider or a hook): it would wait for itself.

### Starting one scope {#scope-start}

`scope.Start()` (or `StartWithContext(ctx)`) does what `Refresh` does, restricted to that scope and its descendants. It is the natural way to start a tenant or plugin scope created after `Start`:

<!-- snippet: starter-scope-start -->
```go
tenant := root.Scope("tenant-a")
doger.Provide(tenant, NewTenantSync)

err := tenant.Start() // starts tenant-a and its child scopes only
```

Services of ancestor scopes that the subtree depends on are built as needed, but `scope.Start` does not start them: `Refresh` does. On the root's own `*Scope` (for example `Component.Scope` of a root service in a kind handler), `Start` behaves as `Refresh`. `scope.Start` follows the same rules as `Refresh`: `ErrNotStarted`, `ErrShutdown`, at most once per service, and calls serialized with `Refresh`.

## Parallel start {#parallel-start}

By default, Starters start one at a time. With `InjectorOpts.StartParallelism` set to `n > 1`, `Start`, `Refresh` and `Scope.Start` run up to `n` Starters concurrently:

<!-- snippet: starter-parallel -->
```go
root := doger.NewWithOpts(&doger.InjectorOpts{StartParallelism: 4})
```

- Dependency order is kept: a Starter starts only after every Starter it depends on, directly or transitively, has started. Among ready Starters, the one with the smallest scope depth, then registration order, goes first.
- The first failure stops scheduling new Starters. `Start` waits for the running ones, then returns that failure.
- `HookBeforeStart` and `HookAfterStart` may run concurrently: hooks that share state must synchronize it. Each Starter still gets its before hook before its after hook.
- Kind handlers, including the `Runnable` handler, always run sequentially.

`0` and `1` mean sequential.
