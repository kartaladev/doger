package dogerclockwork

import (
	"context"
	"log/slog"
	"sync"
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

	tests := []struct {
		name  string
		clock clockwork.Clock
	}{
		{name: "nil", clock: nil},
		// a typed nil would otherwise give a non-nil Clock that panics on
		// first use
		{name: "typed_nil_fake_clock", clock: (*clockwork.FakeClock)(nil)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			synctest.Test(t, func(t *testing.T) {
				require.Nil(t, Wrap(tt.clock))

				injector := doger.NewWithOpts(&doger.InjectorOpts{Clock: Wrap(tt.clock), HealthCheckTimeout: 10 * time.Millisecond})
				doger.ProvideNamedValue(injector, "svc", clockworkTestSlowCheck{})

				err := doger.HealthCheckNamed(injector, "svc")
				assert.EqualError(t, err, "doger: health check timeout: context deadline exceeded")

				time.Sleep(20 * time.Millisecond) // let the abandoned check return
			})
		})
	}
}

// clockworkTestBlockingStarter is a Starter whose Start blocks until release
// is closed, then returns nil.
type clockworkTestBlockingStarter struct{ release chan struct{} }

func (s *clockworkTestBlockingStarter) Start(context.Context) error {
	<-s.release
	return nil
}

// clockworkTestStartBlocked registers a clockworkTestBlockingStarter as
// "srv" in injector and starts the container in another goroutine. It
// returns the func that releases "srv", safe to call more than once, and
// the channel of Start's outcome.
func clockworkTestStartBlocked(injector *doger.RootScope) (release func(), done <-chan error) {
	srv := &clockworkTestBlockingStarter{release: make(chan struct{})}
	doger.ProvideNamedValue(injector, "srv", srv, doger.AsStarter())

	out := make(chan error, 1)
	go func() { out <- injector.Start() }()

	return sync.OnceFunc(func() { close(srv.release) }), out
}

// clockworkTestWaitArmed waits until fake has one armed timer.
func clockworkTestWaitArmed(t *testing.T, fake *clockwork.FakeClock) {
	t.Helper()

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	require.NoError(t, fake.BlockUntilContext(ctx, 1))
}

// TestWrap_StartTimeout pins the clockwork row of container-start "Start
// timeout waits for the container clock" through Wrap.
func TestWrap_StartTimeout(t *testing.T) {
	t.Parallel()

	fake := clockwork.NewFakeClockAt(clockworkTestEpoch)
	injector := doger.NewWithOpts(&doger.InjectorOpts{StartTimeout: 10 * time.Millisecond, Clock: Wrap(fake)})
	release, done := clockworkTestStartBlocked(injector)
	defer injector.Shutdown()
	defer release()

	clockworkTestWaitArmed(t, fake)
	time.Sleep(50 * time.Millisecond) // wall time: the fake clock does not move
	select {
	case err := <-done:
		t.Fatalf("Start returned before the clock advanced: %v", err)
	default:
	}

	fake.Advance(10 * time.Millisecond)
	select {
	case err := <-done:
		require.ErrorIs(t, err, doger.ErrStartTimeout)
		assert.ErrorIs(t, err, context.DeadlineExceeded)
	case <-time.After(5 * time.Second):
		t.Fatal("Start did not return once the clock advanced past StartTimeout")
	}
}

// clockworkTestRecorder is a slog.Handler that keeps the messages of the
// records it handles.
type clockworkTestRecorder struct {
	mu       sync.Mutex
	messages []string
}

func (r *clockworkTestRecorder) Enabled(context.Context, slog.Level) bool { return true }

func (r *clockworkTestRecorder) Handle(_ context.Context, record slog.Record) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.messages = append(r.messages, record.Message)
	return nil
}

func (r *clockworkTestRecorder) WithAttrs([]slog.Attr) slog.Handler { return r }
func (r *clockworkTestRecorder) WithGroup(string) slog.Handler      { return r }

// count returns how many records had message.
func (r *clockworkTestRecorder) count(message string) int {
	r.mu.Lock()
	defer r.mu.Unlock()

	n := 0
	for _, m := range r.messages {
		if m == message {
			n++
		}
	}

	return n
}

// TestWrap_SlowStartWarning pins container-logging "Slow-start warning
// waits for the container clock" through Wrap.
func TestWrap_SlowStartWarning(t *testing.T) {
	t.Parallel()

	const slowStart = "start step is slow"
	fake := clockwork.NewFakeClockAt(clockworkTestEpoch)
	recorder := &clockworkTestRecorder{}
	injector := doger.NewWithOpts(&doger.InjectorOpts{Logger: slog.New(recorder), Clock: Wrap(fake)})
	release, done := clockworkTestStartBlocked(injector)
	defer injector.Shutdown()
	defer release()

	clockworkTestWaitArmed(t, fake)
	time.Sleep(50 * time.Millisecond) // wall time: the fake clock does not move
	assert.Zero(t, recorder.count(slowStart), "no warning before the clock reaches the threshold")

	fake.Advance(doger.DefaultSlowStartThreshold)
	assert.Eventually(t, func() bool { return recorder.count(slowStart) == 1 }, 5*time.Second, time.Millisecond,
		"one warning once the clock reached the threshold, before srv is released")

	release()
	require.NoError(t, <-done)
	assert.Equal(t, 1, recorder.count(slowStart), "exactly one warning")
}
