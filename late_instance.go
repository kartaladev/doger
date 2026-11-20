package doger

import (
	"context"
	"log/slog"
	"sync"
)

// lateClaim is a shutdown's claim on the late instance of a build it found
// in flight (D4): the build hands the instance's shutdown outcome to it.
// Exactly one party owns a late instance, decided under the service's lock:
// the shutdown holding the claim, or the build itself when no claim is
// held. Either way the instance is shut down on a dedicated goroutine with
// the context of the shutdown that made the service Terminal, so the
// invoker never runs or waits for it.
type lateClaim struct {
	ctx context.Context // the shutdown's original context

	mu        sync.Mutex
	done      chan struct{} // closed once the outcome is delivered
	err       error         // the Shutdown error
	recovered any           // the Shutdown panic value, when panicked
	panicked  bool
	abandoned bool // the claiming shutdown stopped waiting: the goroutine logs the outcome
}

func newLateClaim(ctx context.Context) *lateClaim {
	return &lateClaim{ctx: ctx, done: make(chan struct{})}
}

// deliver records the outcome for the claiming shutdown, and reports false
// when that shutdown stopped waiting: the caller then logs it.
func (c *lateClaim) deliver(recovered any, panicked bool, err error) bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.abandoned {
		return false
	}
	c.recovered, c.panicked, c.err = recovered, panicked, err
	close(c.done)

	return true
}

// outcome returns the delivered outcome, re-raising a Shutdown panic on
// the claiming shutdown's goroutine, so it is recovered and reported as
// the panic of any service Shutdown (D5).
func (c *lateClaim) outcome() error {
	if c.panicked {
		panic(c.recovered)
	}

	return c.err
}

// await waits for the outcome, bounded by ctx. When ctx ends first, the
// claim is abandoned: the outcome, when it arrives, is logged instead.
func (c *lateClaim) await(ctx context.Context) error {
	select {
	case <-c.done:
		return c.outcome()
	case <-ctx.Done():
	}

	c.mu.Lock()
	select {
	case <-c.done: // delivered meanwhile
		c.mu.Unlock()
		return c.outcome()
	default:
		c.abandoned = true
		c.mu.Unlock()
		return ctx.Err()
	}
}

// shutdownLateInstance shuts the late instance of the service name down
// with ctx, the original context of the shutdown that made it Terminal,
// even when ctx has ended (a Shutdowner given an ended context returns
// promptly), recovering its panic as D5 says. claim, when not nil, receives
// the outcome; when it is nil, or abandoned, the outcome is logged on scope:
// an error as "service shutdown failed", a panic as "panic recovered" with
// its value at Debug level.
func shutdownLateInstance(scope *Scope, name string, ctx context.Context, instance any, claim *lateClaim) { //nolint:revive // the scope logs; ctx is the shutdown's original context
	if ctx == nil {
		ctx = context.Background()
	}
	var err error
	recovered, panicked := callHookSafely(func() { err = callShutdown(ctx, instance) })

	if claim != nil && claim.deliver(recovered, panicked, err) {
		return
	}
	if scope == nil {
		return
	}
	switch {
	case panicked:
		scope.logRecoveredPanic("service-shutdown", name, recovered)
	case err != nil:
		scope.log(slog.LevelError, "service shutdown failed", slog.String("service", name), slog.Any("error", err))
	}
}

// callShutdown calls the Shutdown method of value, in whichever Shutdowner
// form it implements, whatever the state of ctx.
func callShutdown(ctx context.Context, value any) error {
	switch instance := value.(type) {
	case ShutdownerWithContextAndError:
		return instance.Shutdown(ctx)
	case ShutdownerWithError:
		return instance.Shutdown()
	case ShutdownerWithContext:
		instance.Shutdown(ctx)
	case Shutdowner:
		instance.Shutdown()
	}

	return nil
}
