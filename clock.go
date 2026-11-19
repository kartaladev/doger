package doger

import (
	"context"
	"time"
)

// Clock is the time source of a container: see InjectorOpts.Clock. It is
// everything the container reads from time, so any type with these methods
// can drive it, such as a fake clock in tests. The
// github.com/kartaladev/doger/clock/clockwork module adapts
// github.com/jonboulle/clockwork clocks to it.
//
// A Clock must be safe for concurrent use.
type Clock interface {
	// Now returns the current time.
	Now() time.Time
	// Since returns the time elapsed since t.
	Since(t time.Time) time.Duration
	// AfterFunc calls f in its own goroutine once d has elapsed, and returns
	// a Timer that can cancel the call. A d of zero or less calls f at once.
	AfterFunc(d time.Duration, f func()) Timer
}

// Timer is a call scheduled by Clock.AfterFunc.
type Timer interface {
	// Stop prevents the call from happening. It returns true when it stopped
	// the call, and false when the call already happened or was stopped.
	Stop() bool
}

// RealClock returns the Clock backed by the time package, the default of
// InjectorOpts.Clock. Every call returns an equal value, so a clock can be
// compared with RealClock() to tell whether it is the real clock.
func RealClock() Clock {
	return realClock{}
}

// realClock is the Clock of the time package.
type realClock struct{}

func (realClock) Now() time.Time                  { return time.Now() }
func (realClock) Since(t time.Time) time.Duration { return time.Since(t) }

func (realClock) AfterFunc(d time.Duration, f func()) Timer {
	return time.AfterFunc(d, f)
}

// withClockTimeout returns a copy of parent that is done once d elapsed on
// clock, with Err reporting context.DeadlineExceeded then, and its cancel
// function, which also stops the clock's timer. On the real clock it is
// context.WithTimeout.
func withClockTimeout(parent context.Context, clock Clock, d time.Duration) (context.Context, context.CancelFunc) {
	if clock == RealClock() {
		return context.WithTimeout(parent, d)
	}

	deadline := clock.Now().Add(d)
	ctx, cancel := context.WithCancelCause(parent)
	timer := clock.AfterFunc(d, func() { cancel(context.DeadlineExceeded) })

	return clockContext{Context: ctx, deadline: deadline}, func() {
		timer.Stop()
		cancel(context.Canceled)
	}
}

// clockContext is a context bounded on a Clock other than the real clock: it
// reports the deadline it was given on that clock.
type clockContext struct {
	boundContext
	deadline time.Time
}

func (c clockContext) Deadline() (time.Time, bool) { return c.deadline, true }
