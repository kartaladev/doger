package dogerclockwork

import (
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/jonboulle/clockwork"
	"github.com/kartaladev/doger"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// clockworkTestEpoch is the time the fake clocks of these tests start at.
var clockworkTestEpoch = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

// clockworkTestConsumerClock is a consumer-written doger.Clock.
type clockworkTestConsumerClock struct{}

func (clockworkTestConsumerClock) Now() time.Time                              { return clockworkTestEpoch }
func (clockworkTestConsumerClock) Since(time.Time) time.Duration               { return 0 }
func (clockworkTestConsumerClock) AfterFunc(time.Duration, func()) doger.Timer { return nil }

func TestUnwrap(t *testing.T) {
	t.Parallel()

	fake := clockwork.NewFakeClock()
	tests := []struct {
		name   string
		input  doger.Clock
		want   clockwork.Clock
		wantOK bool
	}{
		{name: "wrapped_fake_clock", input: Wrap(fake), want: fake, wantOK: true},
		{name: "real_clock", input: doger.RealClock()},
		{name: "consumer_clock", input: clockworkTestConsumerClock{}},
		{name: "nil", input: nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			is := assert.New(t)

			got, ok := Unwrap(tt.input)
			is.Equal(tt.wantOK, ok)
			is.Equal(tt.want, got)
		})
	}
}

func TestWrap_Delegates(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		run  func(t *testing.T, fake *clockwork.FakeClock, c doger.Clock)
	}{
		{name: "now", run: func(t *testing.T, _ *clockwork.FakeClock, c doger.Clock) {
			t.Helper()
			assert.Equal(t, clockworkTestEpoch, c.Now())
		}},
		{name: "since_after_2s", run: func(t *testing.T, fake *clockwork.FakeClock, c doger.Clock) {
			t.Helper()
			fake.Advance(2 * time.Second)
			assert.Equal(t, 2*time.Second, c.Since(clockworkTestEpoch))
		}},
		{name: "after_func_5ms_not_run_at_4ms", run: func(t *testing.T, fake *clockwork.FakeClock, c doger.Clock) {
			t.Helper()
			var runs atomic.Int32
			c.AfterFunc(5*time.Millisecond, func() { runs.Add(1) })
			fake.Advance(4 * time.Millisecond)
			synctest.Wait()
			assert.Equal(t, int32(0), runs.Load())
		}},
		{name: "after_func_5ms_run_at_5ms", run: func(t *testing.T, fake *clockwork.FakeClock, c doger.Clock) {
			t.Helper()
			var runs atomic.Int32
			c.AfterFunc(5*time.Millisecond, func() { runs.Add(1) })
			fake.Advance(5 * time.Millisecond)
			synctest.Wait()
			assert.Equal(t, int32(1), runs.Load())
		}},
		{name: "after_func_stopped", run: func(t *testing.T, fake *clockwork.FakeClock, c doger.Clock) {
			t.Helper()
			var runs atomic.Int32
			stopped := c.AfterFunc(5*time.Millisecond, func() { runs.Add(1) }).Stop()
			fake.Advance(5 * time.Millisecond)
			synctest.Wait()
			assert.True(t, stopped)
			assert.Equal(t, int32(0), runs.Load())
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			synctest.Test(t, func(t *testing.T) {
				fake := clockwork.NewFakeClockAt(clockworkTestEpoch)
				tt.run(t, fake, Wrap(fake))
			})
		})
	}
}

// clockworkTestSlowCheck is a health check returning nil after 20ms of
// wall time.
type clockworkTestSlowCheck struct{}

func (clockworkTestSlowCheck) HealthCheck() error {
	time.Sleep(20 * time.Millisecond)
	return nil
}

func TestWrap_NilIsDefault(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		require.Nil(t, Wrap(nil))

		injector := doger.NewWithOpts(&doger.InjectorOpts{Clock: Wrap(nil), HealthCheckTimeout: 10 * time.Millisecond})
		doger.ProvideNamedValue(injector, "svc", clockworkTestSlowCheck{})

		err := doger.HealthCheckNamed(injector, "svc")
		assert.EqualError(t, err, "doger: health check timeout: context deadline exceeded")

		time.Sleep(20 * time.Millisecond) // let the abandoned check return
	})
}
