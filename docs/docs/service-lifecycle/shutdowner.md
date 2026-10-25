---
title: Services shutdown
description: Graceful service shutdown
sidebar_position: 2
---

# Graceful service shutdown

If your service maintains state such as a connection pool, a buffer, etc., you might want to close or flush it gracefully when the application exits.

When the `doger.Shutdown[type]()` or the `injector.Shutdown()` function is called, the framework triggers the `Shutdown` method of each service implementing the `doger.Shutdowner` interface, in reverse invocation order.

🛑 Services can be shut down properly, in reverse initialization order. Requesting a shutdown on a scope also shuts down its children recursively.

## Trigger shutdown {#trigger-shutdown}

A shutdown can be triggered on a root scope:

```go
// on demand
injector.Shutdown() *doger.ShutdownReport
injector.ShutdownWithContext(context.Context) *doger.ShutdownReport

// on signal (helper methods on the root scope)
injector.ShutdownOnSignals(...os.Signal) (os.Signal, *doger.ShutdownReport)
injector.ShutdownOnSignalsWithContext(context.Context, ...os.Signal) (os.Signal, *doger.ShutdownReport)
```

...on a single service:

```go
// returns error on failure
doger.Shutdown[T any](doger.Injector) error
doger.ShutdownWithContext[T any](context.Context, doger.Injector) error
doger.ShutdownNamed(doger.Injector, string) error
doger.ShutdownNamedWithContext(context.Context, doger.Injector, string) error

// panics on failure
doger.MustShutdown[T any](doger.Injector)
doger.MustShutdownWithContext[T any](context.Context, doger.Injector)
doger.MustShutdownNamed(doger.Injector, string)
doger.MustShutdownNamedWithContext(context.Context, doger.Injector, string)
```

:::info

If no signal is passed to `injector.ShutdownOnSignals(...)`, both `syscall.SIGTERM` and `os.Interrupt` are handled by default.

:::

## Shutdowner interfaces {#shutdowner-interfaces}

Your service can implement one of the following signatures:

```go
type Shutdowner interface {
	Shutdown()
}

type ShutdownerWithError interface {
	Shutdown() error
}

type ShutdownerWithContext interface {
	Shutdown(context.Context)
}

type ShutdownerWithContextAndError interface {
	Shutdown(context.Context) error
}
```

### Example {#example}

```go
// Ensure at compile-time MyService implements doger.ShutdownerWithContextAndError
var _ doger.ShutdownerWithContextAndError = (*MyService)(nil)

type MyService struct {}

func (*MyService) Shutdown(context.Context) error {
    // ...
    return nil
}

i := doger.New()

Provide(i, ...)
Invoke(i, ...)

ctx := context.WithTimeout(10 * time.Second)
errors := i.ShutdownWithContext(ctx)
if err != nil {
	log.Println("shutdown error:", err)
}
```

**Play: https://go.dev/play/p/nkiBBYow2d5**
