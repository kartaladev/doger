package doger

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/jonboulle/clockwork"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClock_DefaultRealClock(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		build func() *RootScope
		sleep time.Duration
		want  string // "" for nil
	}{
		{name: "timeout_10ms_check_20ms", build: func() *RootScope { return NewWithOpts(&InjectorOpts{HealthCheckTimeout: 10 * time.Millisecond}) }, sleep: 20 * time.Millisecond, want: healthCheckTestDeadlineMessage},
		{name: "timeout_10ms_check_5ms", build: func() *RootScope { return NewWithOpts(&InjectorOpts{HealthCheckTimeout: 10 * time.Millisecond}) }, sleep: 5 * time.Millisecond},
		{name: "timeout_10ms_nil_clock_check_20ms", build: func() *RootScope {
			return NewWithOpts(&InjectorOpts{HealthCheckTimeout: 10 * time.Millisecond, Clock: nil})
		}, sleep: 20 * time.Millisecond, want: healthCheckTestDeadlineMessage},
		{name: "timeout_10ms_real_clock_check_20ms", build: func() *RootScope {
			return NewWithOpts(&InjectorOpts{HealthCheckTimeout: 10 * time.Millisecond, Clock: RealClock()})
		}, sleep: 20 * time.Millisecond, want: healthCheckTestDeadlineMessage},
		{name: "new_default_timeout_check_20ms", build: func() *RootScope { return New() }, sleep: 20 * time.Millisecond},
		{name: "nil_opts_default_timeout_check_20ms", build: func() *RootScope { return NewWithOpts(nil) }, sleep: 20 * time.Millisecond},
		{name: "new_default_timeout_check_10s", build: func() *RootScope { return New() }, sleep: 10 * time.Second, want: healthCheckTestDeadlineMessage},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			synctest.Test(t, func(t *testing.T) {
				i := tt.build()
				ProvideNamedValue(i, "svc", &healthCheckTestOnClock{clock: clockwork.NewRealClock(), duration: tt.sleep})

				err := i.HealthCheckWithContext(context.Background())["svc"]
				if tt.want == "" {
					assert.NoError(t, err)
				} else {
					assert.EqualError(t, err, tt.want)
				}

				time.Sleep(tt.sleep) // let an abandoned check return
			})
		})
	}
}

// clockTestAdvancer advances a fake clock when it is built and when it is
// shut down.
type clockTestAdvancer struct {
	clock    *fakeClock
	shutdown time.Duration
}

func (c *clockTestAdvancer) Shutdown() { c.clock.Advance(c.shutdown) }

func TestClock_ReportedDurations(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		buildAdvance  time.Duration
		shutdownAdvnc time.Duration
		measure       func(t *testing.T, i *RootScope) time.Duration
		want          time.Duration
	}{
		{
			name: "build_time_3s", buildAdvance: 3 * time.Second, want: 3 * time.Second,
			measure: func(t *testing.T, i *RootScope) time.Duration {
				t.Helper()
				out, ok := ExplainNamedService(i, "svc")
				require.True(t, ok)
				return out.ServiceBuildTime
			},
		},
		{
			name: "build_time_0s_omitted", want: 0,
			measure: func(t *testing.T, i *RootScope) time.Duration {
				t.Helper()
				out, ok := ExplainNamedService(i, "svc")
				require.True(t, ok)
				assert.NotContains(t, out.String(), "Service build time")
				return out.ServiceBuildTime
			},
		},
		{
			name: "service_shutdown_time_2s", shutdownAdvnc: 2 * time.Second, want: 2 * time.Second,
			measure: func(t *testing.T, i *RootScope) time.Duration {
				t.Helper()
				report := i.Shutdown()
				return report.ServiceShutdownTime[newServiceDescription(i.ID(), i.Name(), "svc")]
			},
		},
		{
			name: "shutdown_time_2s", shutdownAdvnc: 2 * time.Second, want: 2 * time.Second,
			measure: func(t *testing.T, i *RootScope) time.Duration {
				t.Helper()
				return i.Shutdown().ShutdownTime
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			fc := newFakeClock()
			i := NewWithOpts(&InjectorOpts{Clock: fc})
			ProvideNamed(i, "svc", func(Injector) (*clockTestAdvancer, error) {
				fc.Advance(tt.buildAdvance)
				return &clockTestAdvancer{clock: fc, shutdown: tt.shutdownAdvnc}, nil
			})
			_, err := InvokeNamed[*clockTestAdvancer](i, "svc")
			require.NoError(t, err)

			assert.Equal(t, tt.want, tt.measure(t, i))
		})
	}
}

func TestClock_TimeoutErrorSameOnAnyClock(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		// clock returns the injector clock and how to advance it; a nil
		// advance lets wall time pass inside the synctest bubble.
		clock func() (Clock, func(time.Duration))
	}{
		{name: "nil_real_clock", clock: func() (Clock, func(time.Duration)) { return nil, nil }},
		{name: "fake_clock", clock: func() (Clock, func(time.Duration)) { fc := newFakeClock(); return fc, fc.Advance }},
		{name: "consumer_clock", clock: func() (Clock, func(time.Duration)) { m := newManualClock(); return m, m.Advance }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			synctest.Test(t, func(t *testing.T) {
				is := assert.New(t)

				clock, advance := tt.clock()
				i := NewWithOpts(&InjectorOpts{Clock: clock, HealthCheckTimeout: 10 * time.Millisecond})
				check := newHealthCheckTestBlocking()
				ProvideNamedValue(i, "svc", check)

				result := healthCheckTestAsync(context.Background(), i)
				synctest.Wait()
				if advance != nil {
					advance(10 * time.Millisecond)
				}
				err := (<-result)["svc"]

				is.EqualError(err, healthCheckTestDeadlineMessage)
				is.ErrorIs(err, ErrHealthCheckTimeout)
				is.ErrorIs(err, context.DeadlineExceeded)

				close(check.release)
				synctest.Wait()
			})
		})
	}
}

func TestClock_TimeoutWaitsForClock(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		opts func(Clock) *InjectorOpts
	}{
		{name: "health_check_timeout", opts: func(c Clock) *InjectorOpts {
			return &InjectorOpts{Clock: c, HealthCheckTimeout: 10 * time.Millisecond}
		}},
		{name: "health_check_global_timeout", opts: func(c Clock) *InjectorOpts {
			return &InjectorOpts{Clock: c, HealthCheckGlobalTimeout: 10 * time.Millisecond}
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			testWithTimeout(t, 5*time.Second)

			synctest.Test(t, func(t *testing.T) {
				is := assert.New(t)
				must := require.New(t)

				fc := newFakeClock()
				i := NewWithOpts(tt.opts(fc))
				check := newHealthCheckTestBlocking()
				defer close(check.release)
				ProvideNamedValue(i, "svc", check)

				result := healthCheckTestAsync(context.Background(), i)
				waitCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
				defer cancel()
				must.NoError(fc.BlockUntilContext(waitCtx, 1)) // the timeout's timer

				time.Sleep(50 * time.Millisecond) // wall time, fake clock not advanced
				is.False(healthCheckTestReturned(result), "returned on wall time")

				fc.Advance(10 * time.Millisecond)
				is.EqualError((<-result)["svc"], healthCheckTestDeadlineMessage)
			})
		})
	}
}

func TestClock_DerivedInjectors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		// target registers "svc" (blocking until released) and returns the
		// injector to check, plus a clock to advance to finish the check
		// when F does not.
		target       func(root *RootScope, check Healthchecker) (Injector, *fakeClock)
		stillRunning bool
	}{
		{
			name: "service_in_child_scope",
			target: func(root *RootScope, check Healthchecker) (Injector, *fakeClock) {
				child := root.Scope("c")
				ProvideNamedValue(child, "svc", check)
				return child, nil
			},
		},
		{
			name: "inherited_service_from_child_scope",
			target: func(root *RootScope, check Healthchecker) (Injector, *fakeClock) {
				ProvideNamedValue(root, "svc", check)
				return root.Scope("c"), nil
			},
		},
		{
			name: "clone",
			target: func(root *RootScope, check Healthchecker) (Injector, *fakeClock) {
				ProvideNamedValue(root, "svc", check)
				return root.Clone(), nil
			},
		},
		{
			name: "clone_with_opts_own_fake_clock",
			target: func(root *RootScope, check Healthchecker) (Injector, *fakeClock) {
				ProvideNamedValue(root, "svc", check)
				g := newFakeClock()
				return root.CloneWithOpts(&InjectorOpts{Clock: g, HealthCheckTimeout: 10 * time.Millisecond}), g
			},
			stillRunning: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			synctest.Test(t, func(t *testing.T) {
				is := assert.New(t)

				f := newFakeClock()
				root := NewWithOpts(&InjectorOpts{Clock: f, HealthCheckTimeout: 10 * time.Millisecond})
				check := newHealthCheckTestBlocking()
				target, g := tt.target(root, check)

				result := healthCheckTestAsync(context.Background(), target)
				synctest.Wait()
				f.Advance(10 * time.Millisecond)
				synctest.Wait()

				if tt.stillRunning {
					is.False(healthCheckTestReturned(result), "timed out on the root's clock")
					g.Advance(10 * time.Millisecond)
				}
				err := (<-result)["svc"]
				is.EqualError(err, healthCheckTestDeadlineMessage)
				is.ErrorIs(err, ErrHealthCheckTimeout)

				close(check.release)
				synctest.Wait()
			})
		})
	}
}

func TestClock_HealthReportTimes(t *testing.T) {
	t.Parallel()
	testWithTimeout(t, 5*time.Second)
	is := assert.New(t)

	fc := newFakeClockAt(healthReportTestEpoch)
	i := NewWithOpts(&InjectorOpts{Clock: fc})
	ProvideNamed(i, "svc", func(Injector) (*healthReportTestOnClock, error) {
		return &healthReportTestOnClock{clock: fc, takes: 4 * time.Second}, nil
	})
	must1(InvokeNamed[*healthReportTestOnClock](i, "svc"))

	entry := healthReportTestEntry(t, i.HealthReport(), "svc")
	is.Equal(4*time.Second, entry.Duration)
	is.Equal(healthReportTestEpoch.Add(4*time.Second), entry.CheckedAt)
}

func TestClock_HealthCacheOnClock(t *testing.T) {
	t.Parallel()
	testWithTimeout(t, 5*time.Second)

	synctest.Test(t, func(t *testing.T) {
		is := assert.New(t)

		fc := newFakeClock()
		i := NewWithOpts(&InjectorOpts{Clock: fc, HealthReportCacheTTL: 10 * time.Second})
		svc := &healthReportTestSwitch{}
		ProvideNamedValue(i, "svc", svc)

		i.HealthReport()
		time.Sleep(50 * time.Millisecond)
		i.HealthReport()
		is.Equal(int32(1), svc.calls.Load(), "calls once wall time passed")

		fc.Advance(10 * time.Second)
		i.HealthReport()
		is.Equal(int32(2), svc.calls.Load(), "calls once the fake clock reached the TTL")
	})
}

func TestClock_HealthSchedulerClock(t *testing.T) {
	t.Parallel()

	fake := newFakeClock()
	tests := []struct {
		name  string
		clock Clock
		check func(t *testing.T, got Clock)
	}{
		{name: "fake", clock: fake, check: func(t *testing.T, got Clock) { t.Helper(); assert.Same(t, fake, got) }},
		{name: "default_real", check: func(t *testing.T, got Clock) {
			t.Helper()
			assert.Equal(t, RealClock(), got)
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			testWithTimeout(t, 5*time.Second)

			scheduler := &manualScheduler{}
			i := NewWithOpts(&InjectorOpts{Clock: tt.clock, HealthScheduler: scheduler})
			require.NoError(t, i.Start())
			defer i.Shutdown()

			require.Len(t, scheduler.clocks, 1)
			tt.check(t, scheduler.clocks[0])
		})
	}
}

func TestRealClock(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		check func(t *testing.T, c Clock)
	}{
		{name: "now_follows_time_now", check: func(t *testing.T, c Clock) {
			t.Helper()
			assert.Equal(t, time.Now(), c.Now())
		}},
		{name: "since_after_3s", check: func(t *testing.T, c Clock) {
			t.Helper()
			start := time.Now()
			time.Sleep(3 * time.Second)
			assert.Equal(t, 3*time.Second, c.Since(start))
		}},
		{name: "after_func_5ms_runs_once", check: func(t *testing.T, c Clock) {
			t.Helper()
			var runs atomic.Int32
			c.AfterFunc(5*time.Millisecond, func() { runs.Add(1) })
			time.Sleep(5 * time.Millisecond)
			synctest.Wait()
			assert.Equal(t, int32(1), runs.Load())
		}},
		{name: "after_func_stopped_at_once_never_runs", check: func(t *testing.T, c Clock) {
			t.Helper()
			var runs atomic.Int32
			stopped := c.AfterFunc(5*time.Millisecond, func() { runs.Add(1) }).Stop()
			time.Sleep(10 * time.Millisecond)
			synctest.Wait()
			assert.True(t, stopped)
			assert.Equal(t, int32(0), runs.Load())
		}},
		{name: "after_func_zero_runs_at_once", check: func(t *testing.T, c Clock) {
			t.Helper()
			var runs atomic.Int32
			c.AfterFunc(0, func() { runs.Add(1) })
			synctest.Wait()
			assert.Equal(t, int32(1), runs.Load())
		}},
		{name: "every_call_returns_an_equal_value", check: func(t *testing.T, c Clock) {
			t.Helper()
			assert.Equal(t, RealClock(), c)
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			synctest.Test(t, func(t *testing.T) {
				tt.check(t, RealClock())
			})
		})
	}
}

// manualClock is a consumer-written Clock: AfterFunc records f, and Advance
// moves the time and runs the functions it reached.
type manualClock struct {
	mu     sync.Mutex
	now    time.Time
	timers map[*manualTimer]struct{}
	armed  int
}

type manualTimer struct {
	clock *manualClock
	at    time.Time
	f     func()
}

func newManualClock() *manualClock {
	return &manualClock{now: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), timers: map[*manualTimer]struct{}{}}
}

func (m *manualClock) Now() time.Time {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.now
}

func (m *manualClock) Since(t time.Time) time.Duration { return m.Now().Sub(t) }

func (m *manualClock) AfterFunc(d time.Duration, f func()) Timer {
	m.mu.Lock()
	timer := &manualTimer{clock: m, at: m.now.Add(d), f: f}
	m.timers[timer] = struct{}{}
	m.armed++
	m.mu.Unlock()

	if d <= 0 {
		m.Advance(0)
	}
	return timer
}

// Advance moves the clock by d and runs, outside its lock, every function
// whose time it reached.
func (m *manualClock) Advance(d time.Duration) {
	m.mu.Lock()
	m.now = m.now.Add(d)
	var due []func()
	for timer := range m.timers {
		if !timer.at.After(m.now) {
			delete(m.timers, timer)
			due = append(due, timer.f)
		}
	}
	m.mu.Unlock()

	for _, f := range due {
		f()
	}
}

// counts returns how many timers were armed, and how many of them were
// neither stopped nor fired.
func (m *manualClock) counts() (armed, pending int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.armed, len(m.timers)
}

func (t *manualTimer) Stop() bool {
	t.clock.mu.Lock()
	defer t.clock.mu.Unlock()
	_, ok := t.clock.timers[t]
	delete(t.clock.timers, t)
	return ok
}

func TestClock_ConsumerClock(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		advance  time.Duration
		returned bool
	}{
		{name: "9ms_before_the_bound", advance: 9 * time.Millisecond},
		{name: "10ms_at_the_bound", advance: 10 * time.Millisecond, returned: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			testWithTimeout(t, 5*time.Second)

			synctest.Test(t, func(t *testing.T) {
				is := assert.New(t)

				m := newManualClock()
				i := NewWithOpts(&InjectorOpts{Clock: m, HealthCheckTimeout: 10 * time.Millisecond})
				check := newHealthCheckTestBlocking()
				ProvideNamedValue(i, "svc", check)

				result := make(chan error, 1)
				go func() { result <- HealthCheckNamed(i, "svc") }()
				synctest.Wait()
				m.Advance(tt.advance)
				synctest.Wait()

				select {
				case err := <-result:
					is.True(tt.returned, "returned before the bound")
					is.ErrorIs(err, ErrHealthCheckTimeout)
					is.ErrorIs(err, context.DeadlineExceeded)
				default:
					is.False(tt.returned, "has not returned at the bound")
					m.Advance(time.Millisecond)
					err := <-result
					is.ErrorIs(err, ErrHealthCheckTimeout)
					is.ErrorIs(err, context.DeadlineExceeded)
				}

				close(check.release)
				synctest.Wait()
				armed, pending := m.counts()
				is.Positive(armed)
				is.Zero(pending, "every armed timer was stopped or fired")
			})
		})
	}
}

// fakeClock is a test-only Clock over a clockwork fake clock. The root
// tests cannot use dogerclockwork.Wrap: that package imports this one.
type fakeClock struct{ *clockwork.FakeClock }

func newFakeClock() *fakeClock { return &fakeClock{clockwork.NewFakeClock()} }

func newFakeClockAt(t time.Time) *fakeClock { return &fakeClock{clockwork.NewFakeClockAt(t)} }

func (c *fakeClock) AfterFunc(d time.Duration, f func()) Timer { return c.FakeClock.AfterFunc(d, f) }
