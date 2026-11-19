package doger

import (
	"fmt"
	"sync"
	"time"
)

// exampleClock is a consumer-written Clock that moves only when Advance is
// called; dogerclockwork.Wrap(clockwork.NewFakeClock()) serves the same
// purpose.
type exampleClock struct {
	mu     sync.Mutex
	armed  *sync.Cond
	now    time.Time
	timers map[*exampleTimer]struct{}
}

type exampleTimer struct {
	clock *exampleClock
	at    time.Time
	f     func()
}

func newExampleClock() *exampleClock {
	c := &exampleClock{now: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), timers: map[*exampleTimer]struct{}{}}
	c.armed = sync.NewCond(&c.mu)
	return c
}

func (c *exampleClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *exampleClock) Since(t time.Time) time.Duration { return c.Now().Sub(t) }

func (c *exampleClock) AfterFunc(d time.Duration, f func()) Timer {
	c.mu.Lock()
	timer := &exampleTimer{clock: c, at: c.now.Add(d), f: f}
	c.timers[timer] = struct{}{}
	c.armed.Broadcast()
	c.mu.Unlock()
	if d <= 0 {
		c.Advance(0)
	}
	return timer
}

// Advance moves the clock by d and calls the functions whose time came.
func (c *exampleClock) Advance(d time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(d)
	var due []func()
	for timer := range c.timers {
		if !timer.at.After(c.now) {
			delete(c.timers, timer)
			due = append(due, timer.f)
		}
	}
	c.mu.Unlock()
	for _, f := range due {
		go f()
	}
}

// BlockUntil waits until n timers are armed.
func (c *exampleClock) BlockUntil(n int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for len(c.timers) < n {
		c.armed.Wait()
	}
}

func (t *exampleTimer) Stop() bool {
	t.clock.mu.Lock()
	defer t.clock.mu.Unlock()
	_, ok := t.clock.timers[t]
	delete(t.clock.timers, t)
	return ok
}

// clockExampleDatabase is a health check that hangs until released.
type clockExampleDatabase struct{ release chan struct{} }

func (db *clockExampleDatabase) HealthCheck() error {
	<-db.release
	return nil
}

func ExampleClock() {
	// RealClock is the default: InjectorOpts.Clock nil means RealClock().
	fmt.Println(RealClock() == RealClock())

	// Any type with Now, Since and AfterFunc is a Clock, such as
	// exampleClock, which moves only when told.
	var clock Clock = newExampleClock()
	start := clock.Now()
	fired := make(chan struct{})
	clock.AfterFunc(time.Minute, func() { close(fired) })
	clock.(*exampleClock).Advance(time.Minute)
	<-fired
	fmt.Println(clock.Since(start))
	// Output:
	// true
	// 1m0s
}

func ExampleInjectorOpts_clock() {
	// By default (Clock: nil) the container measures time on the real clock.
	// A controllable clock, such as dogerclockwork.Wrap(clockwork.NewFakeClock())
	// or exampleClock, lets a test reach a timeout without waiting for it.
	clock := newExampleClock()
	injector := NewWithOpts(&InjectorOpts{
		Clock:              clock,
		HealthCheckTimeout: 5 * time.Second,
	})

	db := &clockExampleDatabase{release: make(chan struct{})}
	defer close(db.release)
	ProvideNamedValue(injector, "db", db)

	result := make(chan map[string]error, 1)
	go func() { result <- injector.HealthCheck() }()

	clock.BlockUntil(1) // the 5s timeout is armed
	clock.Advance(5 * time.Second)

	fmt.Println((<-result)["db"])
	// Output:
	// doger: health check timeout: context deadline exceeded
}

func ExampleInjectorOpts_healthCheckSingleTimeout() {
	// HealthCheckTimeout bounds each check of HealthCheck(); single-service
	// checks such as HealthCheckNamed get their own, longer bound here.
	clock := newExampleClock()
	injector := NewWithOpts(&InjectorOpts{
		Clock:                    clock,
		HealthCheckTimeout:       time.Second,
		HealthCheckSingleTimeout: 10 * time.Second,
	})

	db := &clockExampleDatabase{release: make(chan struct{})}
	defer close(db.release)
	ProvideNamedValue(injector, "db", db)

	result := make(chan error, 1)
	go func() { result <- HealthCheckNamed(injector, "db") }()

	clock.BlockUntil(1)        // the 10s bound is armed
	clock.Advance(time.Second) // HealthCheckTimeout does not apply
	select {
	case err := <-result:
		fmt.Println("returned early:", err)
	case <-time.After(10 * time.Millisecond):
		fmt.Println("still running after 1s")
	}

	clock.Advance(9 * time.Second)
	fmt.Println(<-result)
	// Output:
	// still running after 1s
	// doger: health check timeout: context deadline exceeded
}
