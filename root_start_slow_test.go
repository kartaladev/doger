package doger

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// slowStartMessage is the message of the slow start step record.
const slowStartMessage = "start step is slow"

// countingClock is a Clock that counts its AfterFunc calls.
type countingClock struct {
	Clock
	afterFuncs atomic.Int32
}

func (c *countingClock) AfterFunc(d time.Duration, f func()) Timer {
	c.afterFuncs.Add(1)
	return c.Clock.AfterFunc(d, f)
}

// TestSlowStart_Warning pins container-logging "Slow step warning per
// threshold".
func TestSlowStart_Warning(t *testing.T) {
	t.Parallel()

	startAttrs := func(threshold string) map[string]string {
		return map[string]string{"scope": "[root]", "service": "srv", "step": "start", "threshold": threshold}
	}
	tests := []struct {
		name      string
		threshold time.Duration
		runnable  bool
		runs      time.Duration
		count     int
		attrs     map[string]string
	}{
		{name: "default_below", runs: 9 * time.Second},
		{name: "default_above", runs: 11 * time.Second, count: 1, attrs: startAttrs("10s")},
		{name: "default_long", runs: time.Hour, count: 1, attrs: startAttrs("10s")},
		{name: "custom_above", threshold: 2 * time.Second, runs: 3 * time.Second, count: 1, attrs: startAttrs("2s")},
		{name: "custom_below", threshold: 2 * time.Second, runs: time.Second},
		{name: "disabled", threshold: -time.Nanosecond, runs: time.Hour},
		{
			name: "runnable", runnable: true, runs: 11 * time.Second, count: 1,
			attrs: map[string]string{"scope": "[root]", "service": "srv", "step": "handle", "kind": "runnable", "threshold": "10s"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			synctest.Test(t, func(t *testing.T) {
				logger, recorder := newRecordingLogger(t, slog.LevelDebug)
				root := NewWithOpts(&InjectorOpts{Logger: logger, SlowStartThreshold: tt.threshold})
				opt := AsStarter()
				if tt.runnable {
					opt = AsRunnable()
				}
				provideCounted(root, "srv", newStepProbe(tt.runs), opt)

				require.NoError(t, root.Start())

				records := recorder.withMessage(slowStartMessage)
				require.Len(t, records, tt.count, "records: %v", recorder.all())
				for _, rec := range records {
					assert.Equal(t, slog.LevelWarn, rec.level)
					assert.Equal(t, tt.attrs, rec.attrs)
				}

				root.Shutdown()
			})
		})
	}
}

// TestSlowStart_ChildScope pins container-logging "The warning names the
// scope of a child scope's step".
func TestSlowStart_ChildScope(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		operation func(root *RootScope, tenant *Scope) error
	}{
		{name: "scope_start", operation: func(_ *RootScope, tenant *Scope) error { return tenant.Start() }},
		{name: "refresh", operation: func(root *RootScope, _ *Scope) error { return root.Refresh() }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			synctest.Test(t, func(t *testing.T) {
				logger, recorder := newRecordingLogger(t, slog.LevelDebug)
				root := NewWithOpts(&InjectorOpts{Logger: logger})
				require.NoError(t, root.Start())
				tenant := root.Scope("tenant")
				provideCounted(tenant, "worker", newStepProbe(11*time.Second), AsStarter())

				require.NoError(t, tt.operation(root, tenant))

				records := recorder.withMessage(slowStartMessage)
				require.Len(t, records, 1, "records: %v", recorder.all())
				assert.Equal(t, "tenant", records[0].attrs["scope"])
				assert.Equal(t, "worker", records[0].attrs["service"])
				assert.Equal(t, "start", records[0].attrs["step"])

				root.Shutdown()
			})
		})
	}
}

// TestSlowStart_ParallelNamesOnlySlow pins container-logging "Only the slow
// Starter is named under parallel start".
func TestSlowStart_ParallelNamesOnlySlow(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		logger, recorder := newRecordingLogger(t, slog.LevelDebug)
		root := NewWithOpts(&InjectorOpts{Logger: logger, StartParallelism: 4})
		for _, name := range []string{"fast-a", "fast-b", "fast-c"} {
			provideCounted(root, name, &startWithCtxErr{}, AsStarter())
		}
		provideCounted(root, "slow", newStepProbe(11*time.Second), AsStarter())

		require.NoError(t, root.Start())

		records := recorder.withMessage(slowStartMessage)
		require.Len(t, records, 1, "records: %v", recorder.all())
		assert.Equal(t, "slow", records[0].attrs["service"])

		root.Shutdown()
	})
}

// TestSlowStart_PanicAfterWarning pins container-logging "A step that
// panics after the warning".
func TestSlowStart_PanicAfterWarning(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		logger, recorder := newRecordingLogger(t, slog.LevelDebug)
		root := NewWithOpts(&InjectorOpts{Logger: logger})
		probe := newStepProbe(11 * time.Second)
		probe.outcome = func(int32) error { panic("boom") }
		provideCounted(root, "srv", probe, AsStarter())

		err := root.Start()

		var startErr *StartError
		require.ErrorAs(t, err, &startErr)
		assert.Equal(t, "srv", startErr.Service)
		assert.ErrorContains(t, err, "recovered panic: boom")
		assert.Len(t, recorder.withMessage(slowStartMessage), 1, "records: %v", recorder.all())
		assert.Empty(t, recorder.withMessage("panic recovered"), "a Start panic is returned, not logged")

		root.Shutdown()
	})
}

// TestSlowStart_StopsAtAbandonment pins container-logging "The warning
// stops when the step is abandoned".
func TestSlowStart_StopsAtAbandonment(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		threshold time.Duration
		count     int
	}{
		{name: "threshold_2s", threshold: 2 * time.Second, count: 1},
		{name: "threshold_5s", threshold: 5 * time.Second, count: 0}, // equal to StartTimeout: the abandonment reports the step
		{name: "threshold_10s", threshold: 10 * time.Second, count: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			synctest.Test(t, func(t *testing.T) {
				logger, recorder := newRecordingLogger(t, slog.LevelDebug)
				root := NewWithOpts(&InjectorOpts{Logger: logger, StartTimeout: 5 * time.Second, SlowStartThreshold: tt.threshold})
				probe := provideCounted(root, "srv", newStepProbe(0), AsStarter())

				require.ErrorIs(t, root.Start(), ErrStartTimeout)
				time.Sleep(time.Minute - 5*time.Second) // released at 1m
				probe.unblock()
				<-probe.ended
				synctest.Wait()

				assert.Len(t, recorder.withMessage(slowStartMessage), tt.count, "records: %v", recorder.all())

				root.Shutdown()
			})
		})
	}
}

// TestSlowStart_NoTimerWithoutLogger pins container-logging "No warning
// timer without a Warn-enabled logger". Not parallel: it replaces the
// default slog logger.
func TestSlowStart_NoTimerWithoutLogger(t *testing.T) { //nolint:paralleltest // replaces the default slog logger
	tests := []struct {
		name   string
		logger func() *slog.Logger // nil: no Logger
		calls  int32
	}{
		{name: "no_logger"},
		{name: "error_level", logger: func() *slog.Logger { return newLogRecorder(slog.LevelError).logger() }},
		{name: "warn_level", logger: func() *slog.Logger { return newLogRecorder(slog.LevelWarn).logger() }, calls: 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) { //nolint:paralleltest // replaces the default slog logger
			defaults, _ := captureDefaultLogs(t)

			synctest.Test(t, func(t *testing.T) {
				clock := &countingClock{Clock: RealClock()}
				opts := &InjectorOpts{Clock: clock}
				if tt.logger != nil {
					opts.Logger = tt.logger()
				}
				root := NewWithOpts(opts)
				provideCounted(root, "srv", newStepProbe(time.Hour), AsStarter())

				require.NoError(t, root.Start())

				assert.Equal(t, tt.calls, clock.afterFuncs.Load(), "AfterFunc calls")
				assert.Empty(t, defaults.all(), "nothing reaches slog.Default()")

				root.Shutdown()
			})
		})
	}
}

// TestSlowStart_FakeClock pins container-logging "Slow-start warning waits
// for the container clock".
func TestSlowStart_FakeClock(t *testing.T) {
	t.Parallel()
	testWithTimeout(t, 10*time.Second)

	fake := newFakeClock()
	logger, recorder := newRecordingLogger(t, slog.LevelDebug)
	root := NewWithOpts(&InjectorOpts{Logger: logger, Clock: fake})
	probe := provideCounted(root, "srv", newStepProbe(0), AsStarter())

	done := make(chan error, 1)
	go func() { done <- root.Start() }()

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	require.NoError(t, fake.BlockUntilContext(ctx, 1), "the warning timer is armed")
	time.Sleep(50 * time.Millisecond) // wall time, the clock does not move
	assert.Empty(t, recorder.withMessage(slowStartMessage))

	fake.Advance(10 * time.Second)
	assert.Eventually(t, func() bool { return len(recorder.withMessage(slowStartMessage)) == 1 }, 5*time.Second, time.Millisecond)

	probe.unblock()
	require.NoError(t, <-done)
	assert.Len(t, recorder.withMessage(slowStartMessage), 1)
	root.Shutdown()
}

// TestSlowStart_NoTimerLeak pins container-logging "The warning timer
// leaves no goroutine": every armed timer is stopped, so none fires later
// (goleak in TestMain checks goroutines).
func TestSlowStart_NoTimerLeak(t *testing.T) {
	t.Parallel()
	testWithTimeout(t, 10*time.Second)

	clock := newManualClock()
	logger, recorder := newRecordingLogger(t, slog.LevelDebug)
	root := NewWithOpts(&InjectorOpts{Logger: logger, Clock: clock})
	for i := range 100 {
		provideCounted(root, fmt.Sprintf("s%03d", i), &startWithCtxErr{}, AsStarter())
	}

	require.NoError(t, root.Start())
	root.Shutdown()

	armed, pending := clock.counts()
	assert.Equal(t, 100, armed, "one warning timer per Starter")
	assert.Zero(t, pending, "every warning timer was stopped")
	assert.Empty(t, recorder.withMessage(slowStartMessage))
}

// lateOutcomeMessages are the messages of the records about an abandoned
// step that ended.
var lateOutcomeMessages = map[string]bool{
	"abandoned start step returned": true,
	"abandoned start step error":    true,
	"panic recovered":               true,
	"panic value":                   true,
}

// TestStartTimeout_LateOutcomeLogged pins container-logging "Late outcome
// of an abandoned step".
func TestStartTimeout_LateOutcomeLogged(t *testing.T) {
	t.Parallel()

	inUse := func(int32) error { return errors.New("bind: in use") }
	panics := func(int32) error { panic("boom") }
	tests := []struct {
		name    string
		level   slog.Level
		child   bool
		outcome func(int32) error
		records []string
	}{
		{
			name: "nil", level: slog.LevelDebug,
			records: []string{"WARN abandoned start step returned scope=[root] service=srv step=start elapsed=1m0s"},
		},
		{
			name: "error_debug", level: slog.LevelDebug, outcome: inUse,
			records: []string{
				"WARN abandoned start step returned scope=[root] service=srv step=start elapsed=1m0s",
				"DEBUG abandoned start step error scope=[root] service=srv step=start elapsed=1m0s error=bind: in use",
			},
		},
		{
			name: "error_info", level: slog.LevelInfo, outcome: inUse,
			records: []string{"WARN abandoned start step returned scope=[root] service=srv step=start elapsed=1m0s"},
		},
		{
			name: "panic", level: slog.LevelDebug, outcome: panics,
			records: []string{
				"ERROR panic recovered scope=[root] service=srv point=start panic_type=string",
				"DEBUG panic value scope=[root] service=srv point=start value=boom",
			},
		},
		{
			name: "panic_child_scope", level: slog.LevelDebug, child: true, outcome: panics,
			records: []string{
				"ERROR panic recovered scope=tenant service=srv point=start panic_type=string",
				"DEBUG panic value scope=tenant service=srv point=start value=boom",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			synctest.Test(t, func(t *testing.T) {
				logger, recorder := newRecordingLogger(t, tt.level)
				root := NewWithOpts(&InjectorOpts{StartTimeout: 5 * time.Second, Logger: logger})
				scope, operation := root.self, root.Start
				if tt.child {
					require.NoError(t, root.Start())
					scope = root.Scope("tenant")
					operation = scope.Start
				}
				probe := newStepProbe(0)
				probe.outcome = tt.outcome
				ProvideNamed(scope, "srv", func(Injector) (startOnly, error) { return startOnly{probe}, nil }, AsStarter())

				begin := time.Now()
				require.ErrorIs(t, operation(), ErrStartTimeout)
				time.Sleep(time.Minute - time.Since(begin)) // released at 1m
				probe.unblock()
				<-probe.ended
				synctest.Wait()

				var got []string
				for _, rec := range recorder.all() {
					if lateOutcomeMessages[rec.message] {
						got = append(got, rec.String())
					}
				}
				assert.Equal(t, tt.records, got)

				root.Shutdown()
			})
		})
	}
}

// TestSlowStart_PanickingHandler pins container-logging "A panicking log
// handler does not crash the process": the handler panics on the slow
// start warning (timer goroutine) and on the late outcome record (the
// abandoned call's goroutine).
func TestSlowStart_PanickingHandler(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		recorder := newLogRecorder(slog.LevelDebug)
		recorder.onRecord = func(rec logRecord) {
			if rec.level == slog.LevelWarn {
				panic("handler boom")
			}
		}
		root := NewWithOpts(&InjectorOpts{Logger: recorder.logger(), StartTimeout: 5 * time.Second, SlowStartThreshold: 2 * time.Second})
		probe := newStepProbe(0)
		ProvideNamed(root, "srv", func(Injector) (startOnly, error) { return startOnly{probe}, nil }, AsStarter())
		wrapper, ok := root.self.serviceGet("srv")
		require.True(t, ok)

		require.ErrorIs(t, root.Start(), ErrStartTimeout)
		time.Sleep(time.Minute)
		probe.unblock()
		<-probe.ended
		synctest.Wait()

		assert.True(t, root.isStarted(wrapper), "the late nil return is recorded although logging it panicked")

		root.Shutdown()
	})
}
