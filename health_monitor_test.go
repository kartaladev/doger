package doger

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"
)

// manualScheduler is a HealthScheduler test double: ScheduleHealthRounds
// records its calls and the clock, stores round and returns a stop function
// recording its calls. The test runs the stored round itself.
type manualScheduler struct {
	mu       sync.Mutex
	calls    int
	clocks   []Clock
	rounds   []func(context.Context)
	stops    []int
	stopCtxs []context.Context
	err      error // returned by ScheduleHealthRounds
	// stopWithErr makes ScheduleHealthRounds return its stop function
	// together with err
	stopWithErr bool
	panicWith   any   // raised by ScheduleHealthRounds when set
	stopErr     error // returned by the stop functions
	stopPanic   any   // raised by the stop functions when set
}

func (m *manualScheduler) ScheduleHealthRounds(clock Clock, round func(ctx context.Context)) (func(ctx context.Context) error, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.calls++
	m.clocks = append(m.clocks, clock)
	if m.panicWith != nil {
		panic(m.panicWith)
	}
	if m.err != nil && !m.stopWithErr {
		return nil, m.err
	}

	n := len(m.rounds)
	m.rounds = append(m.rounds, round)
	m.stops = append(m.stops, 0)

	return func(ctx context.Context) error {
		m.mu.Lock()
		defer m.mu.Unlock()
		m.stops[n]++
		m.stopCtxs = append(m.stopCtxs, ctx)
		if m.stopPanic != nil {
			panic(m.stopPanic)
		}
		return m.stopErr
	}, m.err
}

// callCount returns the number of ScheduleHealthRounds calls.
func (m *manualScheduler) callCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.calls
}

// round returns the round of the n-th schedule.
func (m *manualScheduler) round(n int) func(context.Context) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.rounds[n]
}

// stopCount returns how many times the stop of the n-th schedule was called.
func (m *manualScheduler) stopCount(n int) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.stops[n]
}

// runRound runs the round of the first schedule with context.Background().
func (m *manualScheduler) runRound() { m.round(0)(context.Background()) }

// healthMonitorTestLog records the records of a Logger, rendered as lines.
type healthMonitorTestLog struct {
	mu    sync.Mutex
	lines []string
}

func (l *healthMonitorTestLog) logger() *slog.Logger {
	return lineLogger(func(line string) {
		l.mu.Lock()
		defer l.mu.Unlock()
		l.lines = append(l.lines, line)
	})
}

// containing returns the lines containing every one of parts.
func (l *healthMonitorTestLog) containing(parts ...string) []string {
	l.mu.Lock()
	defer l.mu.Unlock()

	var found []string
	for _, line := range l.lines {
		ok := true
		for _, part := range parts {
			ok = ok && strings.Contains(line, part)
		}
		if ok {
			found = append(found, line)
		}
	}
	return found
}

// healthMonitorTestRecords returns the records of rec at level whose
// message contains part.
func healthMonitorTestRecords(rec *logRecorder, level slog.Level, part string) []logRecord {
	var found []logRecord
	for _, r := range rec.all() {
		if r.level == level && strings.Contains(r.message, part) {
			found = append(found, r)
		}
	}
	return found
}

// healthMonitorTestLevelsWith returns the levels of the records of rec
// whose rendering contains text.
func healthMonitorTestLevelsWith(rec *logRecorder, text string) []slog.Level {
	var levels []slog.Level
	for _, r := range rec.all() {
		if strings.Contains(r.String(), text) {
			levels = append(levels, r.level)
		}
	}
	return levels
}

// assertHealthSchedulerLogged asserts that rec holds one record at level
// whose message contains "health scheduler", with the error attribute
// errText when it is not empty, and that value, when not empty, appears
// only in Debug records.
func assertHealthSchedulerLogged(t *testing.T, rec *logRecorder, level slog.Level, errText, value string) {
	t.Helper()

	found := healthMonitorTestRecords(rec, level, "health scheduler")
	if assert.Len(t, found, 1, "%s records about the health scheduler: %v", level, rec.all()) && errText != "" {
		assert.Equal(t, errText, found[0].attrs["error"])
	}
	if value != "" {
		assert.Equal(t, []slog.Level{slog.LevelDebug}, healthMonitorTestLevelsWith(rec, value), "levels of the records with the panic value")
	}
}

func TestHealthScheduler_NilNoRounds(t *testing.T) {
	t.Parallel()
	testWithTimeout(t, 5*time.Second)

	synctest.Test(t, func(t *testing.T) {
		is := assert.New(t)

		i := New()
		db := &healthReportTestSwitch{}
		ProvideNamedValue(i, "db", db)
		require.NoError(t, i.Start())
		defer i.Shutdown()

		time.Sleep(50 * time.Millisecond)
		is.Equal(int32(0), db.calls.Load(), "calls of the check")
		is.False(i.HealthReport(IgnoreContainerPhase()).Monitored)
	})
}

// Not parallel: goleak checks the goroutines this test leaves.
func TestHealthScheduler_NilNoGoroutine(t *testing.T) { //nolint:paralleltest // goleak checks the goroutines this test leaves
	ignore := goleak.IgnoreCurrent()

	i := New()
	ProvideNamedValue(i, "db", &healthReportTestSwitch{})
	require.NoError(t, i.Start())
	i.HealthReport()

	assert.NoError(t, goleak.Find(ignore))
	i.Shutdown()
}

func TestHealthScheduler_ReceivesRealClockByDefault(t *testing.T) {
	t.Parallel()

	fake := newFakeClock()
	tests := []struct {
		name  string
		clock Clock
		want  Clock
	}{
		{name: "nil_default", clock: nil, want: RealClock()},
		{name: "wrapped_fake_clock", clock: fake, want: fake},
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
			assert.Equal(t, tt.want, scheduler.clocks[0])
		})
	}
}

func TestHealthScheduler_CalledAtReady(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		action     func(t *testing.T, i *RootScope)
		calls      int
		readyCalls int // -1: the Ready hook did not run
		monitored  bool
	}{
		{name: "new_only", action: func(*testing.T, *RootScope) {}, calls: 0, readyCalls: -1},
		{name: "start", action: func(t *testing.T, i *RootScope) { t.Helper(); require.NoError(t, i.Start()) }, calls: 1, readyCalls: 1, monitored: true},
		{
			name: "start_fails",
			action: func(t *testing.T, i *RootScope) {
				t.Helper()
				ProvideNamedValue(i, "starter", &healthReportTestStarter{release: closedChan(), err: errors.New("boom")})
				require.Error(t, i.Start())
			},
			calls: 0, readyCalls: -1,
		},
		{
			name: "shutdown_during_start",
			action: func(t *testing.T, i *RootScope) {
				t.Helper()
				release := make(chan struct{})
				ProvideNamedValue(i, "starter", &healthReportTestStarter{release: release})
				started := make(chan error, 1)
				go func() { started <- i.Start() }()
				require.Eventually(t, func() bool { return i.Phase() == PhaseStarting }, time.Second, time.Millisecond)
				shut := make(chan struct{})
				go func() { defer close(shut); i.Shutdown() }()
				// Shutdown began; it may complete before the Starter ran
				require.Eventually(t, func() bool { p := i.Phase(); return p == PhaseClosing || p == PhaseClosed }, time.Second, time.Millisecond)
				close(release)
				<-started
				<-shut
			},
			calls: 0, readyCalls: -1,
		},
		{
			name: "start_then_refresh_twice",
			action: func(t *testing.T, i *RootScope) {
				t.Helper()
				require.NoError(t, i.Start())
				require.NoError(t, i.Refresh())
				require.NoError(t, i.Refresh())
			},
			calls: 1, readyCalls: 1, monitored: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			testWithTimeout(t, 5*time.Second)

			synctest.Test(t, func(t *testing.T) {
				is := assert.New(t)

				fc := newFakeClock()
				scheduler := &manualScheduler{}
				i := NewWithOpts(&InjectorOpts{Clock: fc, HealthScheduler: scheduler})
				readyCalls := -1
				i.AddContainerReadyHook(func(context.Context, *RootScope) { readyCalls = scheduler.callCount() })

				tt.action(t, i)
				defer i.Shutdown()

				is.Equal(tt.calls, scheduler.callCount())
				for _, clock := range scheduler.clocks {
					is.Same(fc, clock)
				}
				is.Equal(tt.readyCalls, readyCalls, "calls seen by the Ready hook")
				is.Equal(tt.monitored, i.HealthReport(IgnoreContainerPhase()).Monitored)
			})
		})
	}
}

// closedChan returns a closed channel.
func closedChan() chan struct{} {
	c := make(chan struct{})
	close(c)
	return c
}

func TestHealthScheduler_Failure(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		scheduler *manualScheduler
		withHook  bool
		errText   string // the error attribute of the Error record
		value     string // the panic value, only in a Debug record
	}{
		{name: "error", scheduler: &manualScheduler{err: errors.New("bad cron")}, errText: "bad cron"},
		{name: "panic", scheduler: &manualScheduler{panicWith: "boom"}, value: "boom"},
		{name: "panic_hook", scheduler: &manualScheduler{panicWith: "boom"}, withHook: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			testWithTimeout(t, 5*time.Second)
			is := assert.New(t)

			log := newLogRecorder(slog.LevelDebug)
			recorder := &healthRunTestPanicRecorder{}
			opts := &InjectorOpts{HealthScheduler: tt.scheduler, Logger: log.logger()}
			if tt.withHook {
				opts.HookContainerPanic = recorder.hook
			}
			i := NewWithOpts(opts)
			defer i.Shutdown()

			is.NoError(i.Start())
			is.Equal(PhaseReady, i.Phase())
			is.False(i.HealthReport(IgnoreContainerPhase()).Monitored)
			if tt.withHook {
				is.Equal([][2]any{{"healthscheduler", "boom"}}, recorder.recorded())
			} else {
				assertHealthSchedulerLogged(t, log, slog.LevelError, tt.errText, tt.value)
			}
		})
	}
}

func TestHealthScheduler_StopReturnedWithError(t *testing.T) {
	t.Parallel()
	testWithTimeout(t, 5*time.Second)
	is := assert.New(t)

	log := newLogRecorder(slog.LevelDebug)
	scheduler := &manualScheduler{err: errors.New("partial"), stopWithErr: true}
	i := NewWithOpts(&InjectorOpts{HealthScheduler: scheduler, Logger: log.logger()})

	is.NoError(i.Start())
	is.Equal(1, scheduler.stopCount(0), "calls of stop before Start returned")
	is.False(i.HealthReport(IgnoreContainerPhase()).Monitored)
	assertHealthSchedulerLogged(t, log, slog.LevelError, "partial", "")

	i.Shutdown()
	is.Equal(1, scheduler.stopCount(0), "calls of stop after Shutdown")
}

func TestHealthScheduler_FailurePanicHook(t *testing.T) {
	t.Parallel()
	testWithTimeout(t, 5*time.Second)

	synctest.Test(t, func(t *testing.T) {
		is := assert.New(t)

		i := NewWithOpts(&InjectorOpts{
			HealthScheduler:    &manualScheduler{panicWith: "boom"},
			HookContainerPanic: func(_ *RootScope, _ string, value any) { panic(value) },
		})

		recovered := func() (recovered any) {
			defer func() { recovered = recover() }()
			_ = i.Start()
			return nil
		}()
		is.Equal("boom", recovered)
		is.False(i.HealthReport(IgnoreContainerPhase()).Monitored, "Monitored after the scheduler panicked")

		shutdown := make(chan *ShutdownReport, 1)
		go func() { shutdown <- i.Shutdown() }()
		select {
		case <-shutdown:
		case <-time.After(time.Second):
			is.Fail("Shutdown did not return within 1s")
		}
	})
}

func TestHealthRound_Results(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		timeout time.Duration
		advance time.Duration
		returns bool
		cState  HealthState
		pending bool // "c" is still in flight at the read
	}{
		{name: "default_10s", advance: DefaultHealthReportCheckTimeout, returns: true, cState: HealthUnhealthy},
		// the check of "c" is still in flight: a read joins it, so this one
		// stops waiting at once and shows it pending
		{name: "default_9999ms", advance: DefaultHealthReportCheckTimeout - time.Millisecond, cState: HealthUnhealthy, pending: true},
		{name: "50ms", timeout: 50 * time.Millisecond, advance: 50 * time.Millisecond, returns: true, cState: HealthUnhealthy},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			synctest.Test(t, func(t *testing.T) {
				is := assert.New(t)

				fc := newFakeClock()
				scheduler := &manualScheduler{}
				i := NewWithOpts(&InjectorOpts{Clock: fc, HealthScheduler: scheduler, HealthCheckTimeout: tt.timeout})
				ProvideNamedValue(i, "a", &healthReportTestSwitch{})
				down := &healthReportTestSwitch{}
				down.set(errors.New("down"))
				ProvideNamedValue(i.Scope("child"), "b", down)
				c := newHealthRunTestBlocking(nil)
				ProvideNamedValue(i, "c", c)
				require.NoError(t, i.Start())

				returned := make(chan struct{})
				go func() { defer close(returned); scheduler.runRound() }()
				synctest.Wait()
				fc.Advance(tt.advance)
				synctest.Wait()

				select {
				case <-returned:
					is.True(tt.returns, "the round returned before the bound")
				default:
					is.False(tt.returns, "the round did not return at the bound")
				}

				ctx, cancel := context.WithCancel(context.Background())
				if tt.pending {
					cancel()
				}
				report := i.HealthReportWithContext(ctx)
				cancel()
				is.Equal(HealthHealthy, healthReportTestEntry(t, report, "a").State)
				is.Equal(HealthUnhealthy, healthReportTestEntry(t, report, "b").State)
				entryC := healthReportTestEntry(t, report, "c")
				is.Equal(tt.cState, entryC.State)
				is.ErrorIs(entryC.Err, ErrHealthCheckTimeout)
				if tt.pending {
					is.ErrorIs(entryC.Err, context.Canceled)
					is.NotZero(entryC.PendingSince)
				}

				close(c.release)
				<-returned
				i.Shutdown()
			})
		})
	}
}

func TestHealthRound_EachCheckOnce(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		is := assert.New(t)

		scheduler := &manualScheduler{}
		i := NewWithOpts(&InjectorOpts{HealthScheduler: scheduler})
		db := newHealthRunTestBlocking(nil)
		ProvideNamedValue(i, "db", db)
		must0(AliasNamed[*healthRunTestBlocking, Healthchecker](i, "db", "hc"))
		child := i.Scope("child")
		svc := &healthReportTestSwitch{}
		ProvideNamedValue(child, "svc", svc)
		must0(ExportNamed[*healthReportTestSwitch](child, "svc"))
		require.NoError(t, i.Start())

		forced := make(chan map[string]error, 1)
		go func() { forced <- i.HealthCheck() }()
		synctest.Wait()
		require.Equal(t, int32(1), svc.calls.Load(), "the forced run checks the export of svc")
		dbBefore, svcBefore := db.calls.Load(), svc.calls.Load()

		round := make(chan struct{})
		go func() { defer close(round); scheduler.runRound() }()
		synctest.Wait()
		close(db.release)
		<-round
		<-forced

		is.Equal(int32(0), db.calls.Load()-dbBefore, "calls of db by the round, which joined the running check")
		is.Equal(int32(1), svc.calls.Load()-svcBefore, "calls of svc by the round")
		is.Equal(int32(1), db.calls.Load(), "calls of db in total")
		i.Shutdown()
	})
}

// Not parallel: AllocsPerRun counts the allocations of the whole process.
func TestHealthRound_AllocsLinear(t *testing.T) { //nolint:paralleltest // AllocsPerRun counts the allocations of the whole process
	testWithTimeout(t, 60*time.Second)

	// roundAllocs returns the allocations of a round re-recording the results
	// of n services.
	roundAllocs := func(n int) float64 {
		scheduler := &manualScheduler{}
		i := NewWithOpts(&InjectorOpts{HealthScheduler: scheduler})
		for k := range n {
			ProvideNamedValue(i, fmt.Sprintf("svc-%d", k), &healthReportTestSwitch{})
		}
		require.NoError(t, i.Start())
		defer i.Shutdown()
		scheduler.runRound() // the first results

		return testing.AllocsPerRun(2, scheduler.runRound)
	}

	at500, at1000 := roundAllocs(500), roundAllocs(1000)

	assert.Less(t, at1000, 2.5*at500, "allocations of a round: %.0f at 500 services, %.0f at 1,000 (×%.2f)", at500, at1000, at1000/at500)
}

func TestHealthRound_PanicKeepsRecording(t *testing.T) {
	t.Parallel()
	testWithTimeout(t, 5*time.Second)

	synctest.Test(t, func(t *testing.T) {
		is := assert.New(t)

		scheduler := &manualScheduler{}
		recorder := &healthHookRecorder{}
		i := NewWithOpts(&InjectorOpts{HealthScheduler: scheduler})
		i.AddServiceHealthChangedHook(recorder.serviceHook)
		var calls atomic.Int32
		ProvideNamedValue(i, "db", healthReportTestFunc{check: func(context.Context) error {
			if calls.Add(1) == 1 {
				panic("boom")
			}
			return nil
		}})
		require.NoError(t, i.Start())
		defer i.Shutdown()

		scheduler.runRound()
		entry := healthReportTestEntry(t, i.HealthReport(), "db")
		is.Equal(HealthUnhealthy, entry.State)
		is.EqualError(entry.Err, "doger: health check panic: boom")

		scheduler.runRound()
		is.Equal(HealthHealthy, healthReportTestEntry(t, i.HealthReport(), "db").State)
		is.Eventually(func() bool { return len(recorder.serviceCalls()) == 2 }, time.Second, time.Millisecond)
		is.Equal([]healthHookCall{{HealthUnknown, HealthUnhealthy, "doger: health check panic: boom"}, {HealthUnhealthy, HealthHealthy, ""}}, recorder.serviceCalls())
	})
}

func TestHealthScheduler_StopBeforeClosing(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		shutdown func(i *RootScope, provided Injector)
		closing  bool // the Closing hooks run
	}{
		{name: "RootScope_Shutdown", shutdown: func(i *RootScope, _ Injector) { i.Shutdown() }, closing: true},
		{name: "ShutdownWithContext_cancelled", shutdown: func(i *RootScope, _ Injector) {
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			i.ShutdownWithContext(ctx)
		}, closing: true},
		{name: "provider_injector", shutdown: func(_ *RootScope, provided Injector) { provided.Shutdown() }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			testWithTimeout(t, 5*time.Second)
			is := assert.New(t)

			scheduler := &manualScheduler{}
			i := NewWithOpts(&InjectorOpts{HealthScheduler: scheduler})
			var provided Injector
			ProvideNamed(i, "db", func(inj Injector) (*healthReportTestSwitch, error) {
				provided = inj
				return &healthReportTestSwitch{}, nil
			})
			must1(InvokeNamed[*healthReportTestSwitch](i, "db"))
			closingRan, stoppedBeforeClosing := false, false
			i.AddContainerClosingHook(func(context.Context, *RootScope) {
				closingRan, stoppedBeforeClosing = true, scheduler.stopCount(0) == 1
			})
			require.NoError(t, i.Start())

			tt.shutdown(i, provided)
			defer i.Shutdown()

			is.Equal(1, scheduler.stopCount(0), "calls of stop")
			is.Equal(tt.closing, closingRan, "the Closing hooks ran")
			if closingRan {
				is.True(stoppedBeforeClosing, "stop was not called before the Closing hooks")
			}
			if is.Len(scheduler.stopCtxs, 1) {
				is.NoError(contextDone(scheduler.stopCtxs[0]), "the context stop received was done")
			}
		})
	}
}

func TestHealthScheduler_RootScopeShutdownEndsRecording(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		calls func(i *RootScope) HealthReport
	}{
		{name: "HealthReport", calls: func(i *RootScope) HealthReport { return i.HealthReport() }},
		{name: "HealthCheck_then_HealthReport", calls: func(i *RootScope) HealthReport {
			_ = i.HealthCheck()
			return i.HealthReport()
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			testWithTimeout(t, 5*time.Second)

			synctest.Test(t, func(t *testing.T) {
				is := assert.New(t)

				recorder := &healthHookRecorder{}
				i := NewWithOpts(&InjectorOpts{HookServiceHealthChanged: []func(*Scope, string, ServiceHealth, ServiceHealth){recorder.serviceHook}})
				var received Injector
				ProvideNamed(i, "provider", func(injector Injector) (int, error) {
					received = injector
					return 1, nil
				})
				must1(InvokeNamed[int](i, "provider"))
				received.Shutdown()

				registered := recoverError(func() { ProvideNamedValue(i, "db", &healthReportTestSwitch{}) })
				report := tt.calls(i)

				is.ErrorIs(registered, ErrShutdown)
				is.Empty(report.Services)
				is.Equal(PhaseIdle, report.Phase)
				time.Sleep(10 * time.Millisecond) // let a stray hook call land
				is.Empty(recorder.serviceCalls())
			})
		})
	}
}

func TestHealthScheduler_StopFailure(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		scheduler *manualScheduler
		withHook  bool
		level     slog.Level // of the record about the health scheduler
		errText   string     // its error attribute
		value     string     // the panic value, only in a Debug record
	}{
		{name: "error", scheduler: &manualScheduler{stopErr: errors.New("busy")}, level: slog.LevelError, errText: "busy"},
		{name: "panic", scheduler: &manualScheduler{stopPanic: "stop boom"}, level: slog.LevelError, value: "stop boom"},
		{name: "panic_hook", scheduler: &manualScheduler{stopPanic: "stop boom"}, withHook: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			testWithTimeout(t, 5*time.Second)
			is := assert.New(t)

			log := newLogRecorder(slog.LevelDebug)
			recorder := &healthRunTestPanicRecorder{}
			opts := &InjectorOpts{HealthScheduler: tt.scheduler, Logger: log.logger()}
			if tt.withHook {
				opts.HookContainerPanic = recorder.hook
			}
			i := NewWithOpts(opts)
			require.NoError(t, i.Start())

			is.True(i.Shutdown().Succeed, "the shutdown report")
			is.Equal(1, tt.scheduler.stopCount(0), "calls of stop")
			if tt.withHook {
				is.Equal([][2]any{{"healthscheduler", "stop boom"}}, recorder.recorded())
			} else {
				assertHealthSchedulerLogged(t, log, tt.level, tt.errText, tt.value)
			}
		})
	}
}

// TestHealthScheduler_StopLogPanicPropagates checks that a panic raised by
// the consumer's slog handler while logging a stop error is not taken for a
// panic of the scheduler: it propagates from Shutdown, as any Logger panic.
func TestHealthScheduler_StopLogPanicPropagates(t *testing.T) {
	t.Parallel()
	testWithTimeout(t, 5*time.Second)
	is := assert.New(t)

	recorder := &healthRunTestPanicRecorder{}
	i := NewWithOpts(&InjectorOpts{
		HealthScheduler:    &manualScheduler{stopErr: errors.New("busy")},
		HookContainerPanic: recorder.hook,
		Logger:             panickingLogger(func(rec logRecord) bool { return rec.message == "health scheduler stop failed" }, "log-boom"),
	})
	require.NoError(t, i.Start())

	is.PanicsWithValue("log-boom", func() { i.Shutdown() })
	is.Empty(recorder.recorded(), "HookContainerPanic calls")
}

// healthMonitorTestCtxCheck is a HealthcheckerWithContext that blocks until
// its context is done, keeping it, then returns ctx.Err().
type healthMonitorTestCtxCheck struct {
	ctx     atomic.Pointer[context.Context]
	entered chan struct{}
}

func (c *healthMonitorTestCtxCheck) HealthCheck(ctx context.Context) error {
	c.ctx.Store(&ctx)
	close(c.entered)
	<-ctx.Done()
	return ctx.Err()
}

// healthMonitorTestShutdowner records, at its Shutdown, whether the check's
// context was already done.
type healthMonitorTestShutdowner struct {
	check    *healthMonitorTestCtxCheck
	ctxDone  atomic.Bool
	shutDown atomic.Bool
}

func (s *healthMonitorTestShutdowner) Shutdown() {
	s.shutDown.Store(true)
	if ctx := s.check.ctx.Load(); ctx != nil {
		s.ctxDone.Store(contextDone(*ctx) != nil)
	}
}

func TestHealthScheduler_ShutdownCancelsRound(t *testing.T) {
	t.Parallel()
	testWithTimeout(t, 5*time.Second)
	is := assert.New(t)

	scheduler := &manualScheduler{}
	i := NewWithOpts(&InjectorOpts{HealthScheduler: scheduler, HealthCheckTimeout: -time.Nanosecond})
	db := &healthMonitorTestCtxCheck{entered: make(chan struct{})}
	ProvideNamedValue(i, "db", db)
	svc := &healthMonitorTestShutdowner{check: db}
	ProvideNamedValue(i, "svc", svc)
	require.NoError(t, i.Start())

	round := make(chan struct{})
	go func() { defer close(round); scheduler.runRound() }()
	<-db.entered

	i.Shutdown()
	is.True(svc.shutDown.Load())
	is.True(svc.ctxDone.Load(), "the check's context was not done when services shut down")
	<-round
}

func TestHealthScheduler_ShutdownSkipsHungCheck(t *testing.T) {
	t.Parallel()
	testWithTimeout(t, 5*time.Second)

	synctest.Test(t, func(t *testing.T) {
		is := assert.New(t)

		scheduler := &manualScheduler{}
		i := NewWithOpts(&InjectorOpts{HealthScheduler: scheduler, HealthCheckTimeout: -time.Nanosecond})
		db := newHealthRunTestBlocking(nil)
		ProvideNamedValue(i, "db", db)
		require.NoError(t, i.Start())

		round := make(chan struct{})
		go func() { defer close(round); scheduler.runRound() }()
		require.Eventually(t, func() bool { return db.calls.Load() == 1 }, time.Second, time.Millisecond)

		shut := make(chan struct{})
		go func() { defer close(shut); i.Shutdown() }()
		select {
		case <-shut:
		case <-time.After(time.Second):
			is.Fail("Shutdown did not return within 1s while a round's check ignored its context")
		}

		close(db.release)
		<-shut
		<-round
	})
}

// healthMonitorTestCauseCheck waits for its context to end, records its
// cause, and returns the context's error.
type healthMonitorTestCauseCheck struct {
	started chan struct{}
	cause   chan error
}

func (c *healthMonitorTestCauseCheck) HealthCheck(ctx context.Context) error {
	close(c.started)
	<-ctx.Done()
	c.cause <- context.Cause(ctx)
	return ctx.Err()
}

func TestHealthScheduler_ShutdownCancelCause(t *testing.T) {
	t.Parallel()
	testWithTimeout(t, 5*time.Second)

	synctest.Test(t, func(t *testing.T) {
		is := assert.New(t)

		i := New()
		check := &healthMonitorTestCauseCheck{started: make(chan struct{}), cause: make(chan error, 1)}
		ProvideNamedValue(i, "db", check)

		result := make(chan map[string]error, 1)
		go func() { result <- i.HealthCheck() }()
		<-check.started
		i.Shutdown()

		is.EqualError((<-result)["db"], "doger: health check timeout: context canceled")
		is.ErrorIs(<-check.cause, ErrShutdown)
	})
}

// healthMonitorTestBlockingScheduler blocks in ScheduleHealthRounds until
// released, then returns a stop function counting its calls.
type healthMonitorTestBlockingScheduler struct {
	release chan struct{}
	stops   atomic.Int32
}

func (s *healthMonitorTestBlockingScheduler) ScheduleHealthRounds(Clock, func(context.Context)) (func(context.Context) error, error) {
	<-s.release
	return func(context.Context) error {
		s.stops.Add(1)
		return nil
	}, nil
}

func TestHealthScheduler_ShutdownWaitsForSchedulerCall(t *testing.T) {
	t.Parallel()
	testWithTimeout(t, 5*time.Second)

	synctest.Test(t, func(t *testing.T) {
		is := assert.New(t)

		scheduler := &healthMonitorTestBlockingScheduler{release: make(chan struct{})}
		i := NewWithOpts(&InjectorOpts{HealthScheduler: scheduler})
		started := make(chan error, 1)
		go func() { started <- i.Start() }()
		synctest.Wait()
		require.Equal(t, PhaseReady, i.Phase(), "Start is blocked in ScheduleHealthRounds")

		ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
		defer cancel()
		shut := make(chan *ShutdownReport, 1)
		go func() { shut <- i.ShutdownWithContext(ctx) }()
		time.Sleep(time.Minute)
		synctest.Wait()
		is.Empty(shut, "ShutdownWithContext returned while ScheduleHealthRounds was in progress")

		close(scheduler.release)
		<-shut
		<-started
		is.Equal(int32(1), scheduler.stops.Load(), "calls of stop")
	})
}

func TestHealthScheduler_Clone(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		clone     func(i *RootScope) *RootScope
		start     bool
		calls     int
		monitored bool
	}{
		{name: "Clone_not_started", clone: func(i *RootScope) *RootScope { return i.Clone() }, calls: 1},
		{name: "Clone_started", clone: func(i *RootScope) *RootScope { return i.Clone() }, start: true, calls: 2, monitored: true},
		{name: "CloneWithOpts_started", clone: func(i *RootScope) *RootScope { return i.CloneWithOpts(&InjectorOpts{}) }, start: true, calls: 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			testWithTimeout(t, 5*time.Second)
			is := assert.New(t)

			scheduler := &manualScheduler{}
			source := NewWithOpts(&InjectorOpts{HealthScheduler: scheduler})
			ProvideNamedValue(source, "db", &healthReportTestSwitch{})
			require.NoError(t, source.Start())
			defer source.Shutdown()

			clone := tt.clone(source)
			defer clone.Shutdown()
			if tt.start {
				require.NoError(t, clone.Start())
			}

			is.Equal(tt.calls, scheduler.callCount())
			is.Equal(tt.monitored, clone.HealthReport(IgnoreContainerPhase()).Monitored)
		})
	}
}

func TestHealthScheduler_CloneShutdownKeepsSource(t *testing.T) {
	t.Parallel()
	testWithTimeout(t, 5*time.Second)
	is := assert.New(t)

	scheduler := &manualScheduler{}
	source := NewWithOpts(&InjectorOpts{HealthScheduler: scheduler})
	ProvideNamedValue(source, "db", &healthReportTestSwitch{})
	require.NoError(t, source.Start())
	defer source.Shutdown()
	clone := source.Clone()
	require.NoError(t, clone.Start())
	require.Equal(t, 2, scheduler.callCount())

	clone.Shutdown()

	is.Equal(1, scheduler.stopCount(1), "stops of the clone's schedule")
	is.Equal(0, scheduler.stopCount(0), "stops of the source's schedule")
	is.True(source.HealthReport().Monitored)
}

func TestHealthHooks_CloneKeepsHooks(t *testing.T) {
	t.Parallel()
	testWithTimeout(t, 5*time.Second)

	synctest.Test(t, func(t *testing.T) {
		is := assert.New(t)

		var mu sync.Mutex
		var scopes []*Scope
		var roots []*RootScope
		i := NewWithOpts(&InjectorOpts{
			HookServiceHealthChanged: []func(*Scope, string, ServiceHealth, ServiceHealth){func(scope *Scope, _ string, _, _ ServiceHealth) {
				mu.Lock()
				defer mu.Unlock()
				scopes = append(scopes, scope)
			}},
			HookContainerHealthChanged: []func(*RootScope, HealthState, HealthState){func(root *RootScope, _, _ HealthState) {
				mu.Lock()
				defer mu.Unlock()
				roots = append(roots, root)
			}},
		})
		ProvideNamedValue(i, "db", &healthReportTestSwitch{})
		clone := i.Clone()

		_ = clone.HealthCheck()
		synctest.Wait() // the hooks run on the recording goroutine

		mu.Lock()
		if is.Len(scopes, 1, "service hook calls") {
			is.Same(clone.self, scopes[0], "the scope the service hook received")
		}
		if is.Len(roots, 1, "container hook calls") {
			is.Same(clone, roots[0], "the root the container hook received")
		}
		mu.Unlock()
		clone.Shutdown()
		i.Shutdown()
	})
}

func TestHealthRound_AfterStop(t *testing.T) {
	t.Parallel()
	testWithTimeout(t, 5*time.Second)

	scheduler := &manualScheduler{}
	i := NewWithOpts(&InjectorOpts{HealthScheduler: scheduler})
	db := &healthReportTestSwitch{}
	ProvideNamedValue(i, "db", db)
	require.NoError(t, i.Start())
	i.Shutdown()

	calls := db.calls.Load()
	scheduler.runRound()
	assert.Equal(t, calls, db.calls.Load(), "calls by the round")
}

// healthHookCall is a service hook call: previous and current states, and
// the current error's message ("" for nil).
type healthHookCall struct {
	previous, current HealthState
	err               string
}

// healthHookRecorder records service and container hook calls.
type healthHookRecorder struct {
	mu        sync.Mutex
	service   []healthHookCall
	container [][2]HealthState
}

func (r *healthHookRecorder) serviceHook(_ *Scope, _ string, previous, current ServiceHealth) {
	r.mu.Lock()
	defer r.mu.Unlock()
	message := ""
	if current.Err != nil {
		message = current.Err.Error()
	}
	r.service = append(r.service, healthHookCall{previous: previous.State, current: current.State, err: message})
}

func (r *healthHookRecorder) containerHook(_ *RootScope, previous, current HealthState) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.container = append(r.container, [2]HealthState{previous, current})
}

func (r *healthHookRecorder) serviceCalls() []healthHookCall {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]healthHookCall{}, r.service...)
}

func (r *healthHookRecorder) containerCalls() [][2]HealthState {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([][2]HealthState{}, r.container...)
}

// healthMonitorTestSequence is a Healthchecker returning results in turn,
// then the last one.
type healthMonitorTestSequence struct {
	mu      sync.Mutex
	results []error
	calls   int
}

func (s *healthMonitorTestSequence) HealthCheck() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := min(s.calls, len(s.results)-1)
	s.calls++
	return s.results[n]
}

func TestHookServiceHealthChanged(t *testing.T) {
	t.Parallel()

	errDown, errA, errB := errors.New("down"), errors.New("a"), errors.New("b")

	tests := []struct {
		name    string
		results []error
		rounds  int
		calls   []healthHookCall
	}{
		{name: "healthy_throughout", results: []error{nil, nil, nil}, rounds: 3, calls: []healthHookCall{{HealthUnknown, HealthHealthy, ""}}},
		{name: "down_then_up", results: []error{nil, errDown, nil}, rounds: 3, calls: []healthHookCall{
			{HealthUnknown, HealthHealthy, ""}, {HealthHealthy, HealthUnhealthy, "down"}, {HealthUnhealthy, HealthHealthy, ""},
		}},
		{name: "new_error_same_state", results: []error{errA, errB}, rounds: 2, calls: []healthHookCall{{HealthUnknown, HealthUnhealthy, "a"}}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			testWithTimeout(t, 5*time.Second)

			synctest.Test(t, func(t *testing.T) {
				scheduler := &manualScheduler{}
				recorder := &healthHookRecorder{}
				i := NewWithOpts(&InjectorOpts{HealthScheduler: scheduler, HookServiceHealthChanged: []func(*Scope, string, ServiceHealth, ServiceHealth){recorder.serviceHook}})
				ProvideNamedValue(i, "db", &healthMonitorTestSequence{results: tt.results})
				require.NoError(t, i.Start())
				defer i.Shutdown()

				for range tt.rounds {
					scheduler.runRound()
				}

				assert.Eventually(t, func() bool { return len(recorder.serviceCalls()) == len(tt.calls) }, time.Second, time.Millisecond)
				assert.Equal(t, tt.calls, recorder.serviceCalls())
			})
		})
	}
}

func TestHookServiceHealthChanged_ForcedRun(t *testing.T) {
	t.Parallel()
	testWithTimeout(t, 5*time.Second)

	synctest.Test(t, func(t *testing.T) {
		i := New()
		recorder := &healthHookRecorder{}
		i.AddServiceHealthChangedHook(recorder.serviceHook)
		db := &healthReportTestSwitch{}
		ProvideNamedValue(i, "db", db)
		i.HealthReport() // records nil

		db.set(errors.New("down"))
		i.HealthCheck()

		assert.Eventually(t, func() bool { return len(recorder.serviceCalls()) == 2 }, time.Second, time.Millisecond)
		assert.Equal(t, []healthHookCall{{HealthUnknown, HealthHealthy, ""}, {HealthHealthy, HealthUnhealthy, "down"}}, recorder.serviceCalls())
	})
}

func TestHookServiceHealthChanged_Serialized(t *testing.T) {
	t.Parallel()
	testWithTimeout(t, 5*time.Second)

	synctest.Test(t, func(t *testing.T) {
		is := assert.New(t)

		scheduler := &manualScheduler{}
		i := NewWithOpts(&InjectorOpts{HealthScheduler: scheduler})
		var running, overlaps atomic.Int32
		var mu sync.Mutex
		order := map[string][]string{}
		hook := func(label string) func(*Scope, string, ServiceHealth, ServiceHealth) {
			return func(_ *Scope, name string, _, _ ServiceHealth) {
				if running.Add(1) > 1 {
					overlaps.Add(1)
				}
				time.Sleep(time.Millisecond)
				mu.Lock()
				order[name] = append(order[name], label)
				mu.Unlock()
				running.Add(-1)
			}
		}
		i.AddServiceHealthChangedHook(hook("A"))
		i.AddServiceHealthChangedHook(hook("B"))
		for n := range 10 {
			ProvideNamedValue(i, fmt.Sprintf("svc-%d", n), &healthReportTestSwitch{})
		}
		require.NoError(t, i.Start())
		defer i.Shutdown()

		scheduler.runRound()

		is.Eventually(func() bool {
			mu.Lock()
			defer mu.Unlock()
			total := 0
			for _, labels := range order {
				total += len(labels)
			}
			return total == 20
		}, 2*time.Second, time.Millisecond)
		is.Zero(overlaps.Load(), "hook calls that overlapped")
		mu.Lock()
		defer mu.Unlock()
		for name, labels := range order {
			is.Equal([]string{"A", "B"}, labels, name)
		}
	})
}

func TestHookServiceHealthChanged_Reentrant(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		call func(i *RootScope)
	}{
		{name: "HealthCheckNamed", call: func(i *RootScope) { _ = HealthCheckNamed(i, "db") }},
		{name: "HealthCheck", call: func(i *RootScope) { i.HealthCheck() }},
		{name: "HealthReport", call: func(i *RootScope) { i.HealthReport() }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			testWithTimeout(t, 5*time.Second)

			synctest.Test(t, func(t *testing.T) {
				is := assert.New(t)

				i := New()
				ProvideNamedValue(i, "db", &healthMonitorTestSequence{results: []error{nil, errors.New("down")}})
				var hookCalls atomic.Int32
				hookReturned := make(chan struct{}, 10)
				i.AddServiceHealthChangedHook(func(*Scope, string, ServiceHealth, ServiceHealth) {
					hookCalls.Add(1)
					tt.call(i)
					hookReturned <- struct{}{}
				})

				for range 2 {
					done := make(chan struct{})
					go func() { defer close(done); i.HealthCheck() }()
					select {
					case <-done:
					case <-time.After(time.Second):
						is.Fail("HealthCheck did not return within 1s")
					}
				}
				select {
				case <-hookReturned:
				case <-time.After(time.Second):
					is.Fail("the hook's own call did not return within 1s")
				}
				is.Positive(hookCalls.Load())
				is.Eventually(func() bool { return len(hookReturned) == int(hookCalls.Load())-1 }, time.Second, time.Millisecond)
			})
		})
	}
}

func TestHookContainerHealthChanged(t *testing.T) {
	t.Parallel()

	errDown, errX, errY := errors.New("down"), errors.New("x"), errors.New("y")

	tests := []struct {
		name      string
		a, b      []error
		bOptional bool
		calls     [][2]HealthState
	}{
		{name: "healthy_twice", a: []error{nil, nil}, b: []error{nil, nil}, calls: [][2]HealthState{{HealthUnknown, HealthHealthy}}},
		{name: "b_down", a: []error{nil, nil}, b: []error{nil, errDown}, calls: [][2]HealthState{{HealthUnknown, HealthHealthy}, {HealthHealthy, HealthUnhealthy}}},
		{name: "a_new_error", a: []error{errX, errY}, b: []error{nil, nil}, calls: [][2]HealthState{{HealthUnknown, HealthUnhealthy}}},
		{name: "b_optional_down", a: []error{nil, nil}, b: []error{nil, errDown}, bOptional: true, calls: [][2]HealthState{{HealthUnknown, HealthHealthy}, {HealthHealthy, HealthDegraded}}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			testWithTimeout(t, 5*time.Second)

			synctest.Test(t, func(t *testing.T) {
				scheduler := &manualScheduler{}
				recorder := &healthHookRecorder{}
				i := NewWithOpts(&InjectorOpts{HealthScheduler: scheduler, HookContainerHealthChanged: []func(*RootScope, HealthState, HealthState){recorder.containerHook}})
				ProvideNamedValue(i, "a", &healthMonitorTestSequence{results: tt.a})
				var opts []ProvideOption
				if tt.bOptional {
					opts = append(opts, AsOptionalHealth())
				}
				ProvideNamedValue(i, "b", &healthMonitorTestSequence{results: tt.b}, opts...)
				require.NoError(t, i.Start())
				defer i.Shutdown()

				scheduler.runRound()
				scheduler.runRound()

				assert.Eventually(t, func() bool { return len(recorder.containerCalls()) == len(tt.calls) }, time.Second, time.Millisecond)
				assert.Equal(t, tt.calls, recorder.containerCalls())
			})
		})
	}
}

func TestHookContainerHealthChanged_Membership(t *testing.T) {
	t.Parallel()
	testWithTimeout(t, 5*time.Second)

	synctest.Test(t, func(t *testing.T) {
		scheduler := &manualScheduler{}
		i := NewWithOpts(&InjectorOpts{HealthScheduler: scheduler})
		ProvideNamedValue(i, "db", &healthReportTestSwitch{})
		child := i.Scope("child")
		bad := &healthReportTestSwitch{}
		bad.set(errors.New("down"))
		ProvideNamedValue(child, "bad", bad)
		recorder := &healthHookRecorder{}
		i.AddContainerHealthChangedHook(recorder.containerHook)
		require.NoError(t, i.Start())
		defer i.Shutdown()
		scheduler.runRound()
		require.Eventually(t, func() bool { return len(recorder.containerCalls()) == 1 }, time.Second, time.Millisecond)

		child.Shutdown()

		assert.Eventually(t, func() bool {
			calls := recorder.containerCalls()
			return len(calls) > 0 && calls[len(calls)-1] == [2]HealthState{HealthUnhealthy, HealthHealthy}
		}, time.Second, time.Millisecond, "container calls: %v", recorder.containerCalls())
	})
}

func TestHealthHooks_ContainerAfterService(t *testing.T) {
	t.Parallel()
	testWithTimeout(t, 5*time.Second)

	synctest.Test(t, func(t *testing.T) {
		var mu sync.Mutex
		var order []string
		i := NewWithOpts(&InjectorOpts{
			HookServiceHealthChanged: []func(*Scope, string, ServiceHealth, ServiceHealth){func(_ *Scope, name string, _, _ ServiceHealth) {
				mu.Lock()
				defer mu.Unlock()
				order = append(order, "service:"+name)
			}},
			HookContainerHealthChanged: []func(*RootScope, HealthState, HealthState){func(_ *RootScope, _, current HealthState) {
				mu.Lock()
				defer mu.Unlock()
				order = append(order, "container:"+current.String())
			}},
		})
		ProvideNamedValue(i, "db", &healthReportTestSwitch{})

		_ = i.HealthCheck()
		synctest.Wait() // the hooks run on the recording goroutine

		mu.Lock()
		got := append([]string(nil), order...)
		mu.Unlock()
		assert.Equal(t, []string{"service:db", "container:healthy"}, got)
		i.Shutdown()
	})
}

// TestHealthHooks_AddRacesClone is a race guard: adding health hooks while
// the root is cloned must not race, and a clone taken afterwards has every
// hook.
func TestHealthHooks_AddRacesClone(t *testing.T) {
	t.Parallel()
	testWithTimeout(t, 10*time.Second)

	root := New()
	var wg sync.WaitGroup
	wg.Go(func() {
		for range 200 {
			root.AddServiceHealthChangedHook(func(*Scope, string, ServiceHealth, ServiceHealth) {})
			root.AddContainerHealthChangedHook(func(*RootScope, HealthState, HealthState) {})
		}
	})
	wg.Go(func() {
		for range 200 {
			root.Clone()
		}
	})
	wg.Wait()

	clone := root.Clone()
	assert.Len(t, clone.hooks.serviceHealthChanged.load(), 200)
	assert.Len(t, clone.hooks.containerHealthChanged.load(), 200)
}

func TestHealthHooks_AddNilIgnored(t *testing.T) {
	t.Parallel()
	is := assert.New(t)

	root := New()
	root.AddServiceHealthChangedHook(nil)
	root.AddContainerHealthChangedHook(nil)

	is.Empty(root.hooks.serviceHealthChanged.load())
	is.Empty(root.hooks.containerHealthChanged.load())
}

func TestHealthHooks_NilSkipped(t *testing.T) {
	t.Parallel()
	testWithTimeout(t, 5*time.Second)

	synctest.Test(t, func(t *testing.T) {
		is := assert.New(t)

		log := newLogRecorder(slog.LevelDebug)
		recorder := &healthRunTestPanicRecorder{}
		var calls atomic.Int32
		i := NewWithOpts(&InjectorOpts{
			Logger:             log.logger(),
			HookContainerPanic: recorder.hook,
			HookServiceHealthChanged: []func(*Scope, string, ServiceHealth, ServiceHealth){
				nil,
				func(*Scope, string, ServiceHealth, ServiceHealth) { calls.Add(1) },
			},
			HookContainerHealthChanged: []func(*RootScope, HealthState, HealthState){nil},
		})
		i.AddServiceHealthChangedHook(nil)
		i.AddContainerHealthChangedHook(nil)
		ProvideNamedValue(i, "db", &healthReportTestSwitch{})

		_ = i.HealthCheck()
		synctest.Wait() // the hooks run on the recording goroutine

		is.Equal(int32(1), calls.Load(), "calls of the hook that is not nil")
		is.Empty(recorder.recorded(), "HookContainerPanic calls")
		is.Empty(log.withMessage("panic recovered"), "panic recovered records")
		is.Empty(log.withMessage("panic value"), "panic value records")
		i.Shutdown()
	})
}

func TestHookContainerHealthChanged_Appearing(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		result error
		calls  [][2]HealthState // since "b" appeared
	}{
		{name: "healthy", calls: [][2]HealthState{{HealthHealthy, HealthUnknown}, {HealthUnknown, HealthHealthy}}},
		{name: "down", result: errors.New("down"), calls: [][2]HealthState{{HealthHealthy, HealthUnknown}, {HealthUnknown, HealthUnhealthy}}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			testWithTimeout(t, 5*time.Second)

			synctest.Test(t, func(t *testing.T) {
				is := assert.New(t)

				scheduler := &manualScheduler{}
				i := NewWithOpts(&InjectorOpts{HealthScheduler: scheduler})
				ProvideNamedValue(i, "a", &healthReportTestSwitch{})
				recorder := &healthHookRecorder{}
				i.AddContainerHealthChangedHook(recorder.containerHook)
				require.NoError(t, i.Start())
				defer i.Shutdown()
				scheduler.runRound()
				require.Eventually(t, func() bool { return len(recorder.containerCalls()) == 1 }, time.Second, time.Millisecond)
				require.Equal(t, [][2]HealthState{{HealthUnknown, HealthHealthy}}, recorder.containerCalls())

				b := &healthReportTestSwitch{}
				b.set(tt.result)
				ProvideNamedValue(i, "b", b)

				is.Never(func() bool { return len(recorder.containerCalls()) != 1 }, 50*time.Millisecond, time.Millisecond,
					"container calls before the next round: %v", recorder.containerCalls())
				scheduler.runRound()
				is.Eventually(func() bool { return len(recorder.containerCalls()) == 1+len(tt.calls) }, time.Second, time.Millisecond)
				is.Equal(tt.calls, recorder.containerCalls()[1:])
			})
		})
	}
}

func TestHealthHooks_Panic(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		withHook bool
	}{
		{name: "panic_hook", withHook: true},
		{name: "logged"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			testWithTimeout(t, 5*time.Second)

			synctest.Test(t, func(t *testing.T) {
				is := assert.New(t)

				scheduler := &manualScheduler{}
				log := newLogRecorder(slog.LevelDebug)
				panics := &healthRunTestPanicRecorder{}
				opts := &InjectorOpts{HealthScheduler: scheduler, Logger: log.logger()}
				if tt.withHook {
					opts.HookContainerPanic = panics.hook
				}
				i := NewWithOpts(opts)
				var bCalls atomic.Int32
				i.AddServiceHealthChangedHook(func(*Scope, string, ServiceHealth, ServiceHealth) { panic("boom") })
				i.AddServiceHealthChangedHook(func(*Scope, string, ServiceHealth, ServiceHealth) { bCalls.Add(1) })
				ProvideNamedValue(i, "db", &healthMonitorTestSequence{results: []error{nil, errors.New("down")}})
				require.NoError(t, i.Start())
				defer i.Shutdown()

				scheduler.runRound()
				scheduler.runRound()

				is.Eventually(func() bool { return bCalls.Load() == 2 }, time.Second, time.Millisecond)
				if tt.withHook {
					is.Equal([][2]any{{"healthchanged", "boom"}, {"healthchanged", "boom"}}, panics.recorded())
				} else {
					recovered := log.withMessage("panic recovered")
					if is.Len(recovered, 2) {
						for _, rec := range recovered {
							is.Equal(slog.LevelError, rec.level)
							is.Equal("healthchanged", rec.attrs["point"])
							is.Equal("string", rec.attrs["panic_type"])
						}
					}
					is.Equal([]slog.Level{slog.LevelDebug, slog.LevelDebug}, healthMonitorTestLevelsWith(log, "boom"), "levels of the records with the panic value")
				}
			})
		})
	}
}

func TestHealthHooks_Degraded(t *testing.T) {
	t.Parallel()
	testWithTimeout(t, 5*time.Second)

	synctest.Test(t, func(t *testing.T) {
		scheduler := &manualScheduler{}
		recorder := &healthHookRecorder{}
		i := NewWithOpts(&InjectorOpts{HealthScheduler: scheduler})
		i.AddServiceHealthChangedHook(recorder.serviceHook)
		i.AddContainerHealthChangedHook(recorder.containerHook)
		ProvideNamedValue(i, "cache", &healthMonitorTestSequence{results: []error{nil, errors.New("slow")}}, AsOptionalHealth())
		require.NoError(t, i.Start())
		defer i.Shutdown()

		scheduler.runRound()
		scheduler.runRound()

		assert.Eventually(t, func() bool { return len(recorder.containerCalls()) == 2 }, time.Second, time.Millisecond)
		assert.Equal(t, []healthHookCall{{HealthUnknown, HealthHealthy, ""}, {HealthHealthy, HealthDegraded, "slow"}}, recorder.serviceCalls())
		assert.Equal(t, [][2]HealthState{{HealthUnknown, HealthHealthy}, {HealthHealthy, HealthDegraded}}, recorder.containerCalls())
	})
}

func BenchmarkHealthRound(b *testing.B) {
	scheduler := &manualScheduler{}
	i := NewWithOpts(&InjectorOpts{HealthScheduler: scheduler})
	for n := range 1000 {
		ProvideNamedValue(i, fmt.Sprintf("svc-%d", n), &healthReportTestSwitch{})
	}
	require.NoError(b, i.Start())
	defer i.Shutdown()
	scheduler.runRound() // the first results

	b.ReportAllocs()
	for b.Loop() {
		scheduler.runRound()
	}
}

func TestHealthRound_GoexitKeepsRecording(t *testing.T) {
	t.Parallel()
	testWithTimeout(t, 5*time.Second)

	synctest.Test(t, func(t *testing.T) {
		is := assert.New(t)

		scheduler := &manualScheduler{}
		i := NewWithOpts(&InjectorOpts{HealthScheduler: scheduler})
		var calls atomic.Int32
		ProvideNamedValue(i, "db", healthRunTestGoexitOnce(&calls))
		require.NoError(t, i.Start())
		defer i.Shutdown()

		scheduler.runRound()
		entry := healthReportTestEntry(t, i.HealthReport(), "db")
		is.Equal(HealthUnhealthy, entry.State)
		is.ErrorIs(entry.Err, ErrHealthCheckPanic)

		scheduler.runRound()
		is.Equal(HealthHealthy, healthReportTestEntry(t, i.HealthReport(), "db").State)
	})
}

func TestHealthMonitor_Guards(t *testing.T) {
	t.Parallel()

	t.Run("keep_only_pruning", func(t *testing.T) {
		t.Parallel()
		is := assert.New(t)

		i := New()
		var mu sync.Mutex
		var states []HealthState // current state of each container transition
		i.AddContainerHealthChangedHook(func(_ *RootScope, _, current HealthState) {
			mu.Lock()
			defer mu.Unlock()
			states = append(states, current)
		})
		db := &healthReportTestSwitch{}
		db.set(errors.New("down"))
		ProvideNamedValue(i, "db", db)
		is.Equal(HealthUnhealthy, healthReportTestEntry(t, i.HealthReport(), "db").State)

		// a listing without "db" drops its record and membership, and the
		// container's state no longer counts it
		i.health.keepOnly(map[any]healthTarget{})
		i.deliverTransitions()

		mu.Lock()
		is.Equal([]HealthState{HealthUnhealthy, HealthHealthy}, states, "container states")
		mu.Unlock()

		i.health.mu.Lock()
		_, recorded := i.health.records[i.self.services["db"]]
		_, member := i.health.members[i.self.services["db"]]
		i.health.mu.Unlock()
		is.False(recorded, "record of db")
		is.False(member, "membership of db")
	})

	t.Run("deliver_transitions_repanic", func(t *testing.T) {
		t.Parallel()
		testWithTimeout(t, 5*time.Second)

		synctest.Test(t, func(t *testing.T) {
			is := assert.New(t)

			fc := newFakeClock()
			i := NewWithOpts(&InjectorOpts{
				Clock:              fc,
				HealthCheckTimeout: 50 * time.Millisecond,
				HookContainerPanic: func(_ *RootScope, point string, value any) {
					panic(fmt.Sprintf("%s: %v", point, value))
				},
			})
			var delivered atomic.Int32
			i.AddServiceHealthChangedHook(func(*Scope, string, ServiceHealth, ServiceHealth) {
				if delivered.Add(1) == 1 {
					panic("boom")
				}
			})
			db := newHealthRunTestBlocking(nil)
			ProvideNamedValue(i, "db", db)

			// the read records its timeout and delivers the transition
			// itself: HookContainerPanic re-panics, out of the read
			recovered := make(chan any, 1)
			go func() {
				defer func() { recovered <- recover() }()
				i.HealthReport()
			}()
			synctest.Wait()
			fc.Advance(50 * time.Millisecond)
			is.Equal("healthchanged: boom", <-recovered)

			// delivering was reset: the next transition is delivered
			close(db.release)
			synctest.Wait()
			is.NoError(HealthCheckNamed(i, "db"))
			synctest.Wait()
			is.GreaterOrEqual(delivered.Load(), int32(2), "transitions delivered after the re-panic")
			i.Shutdown()
		})
	})

	t.Run("scheduling_stopped_before_ready", func(t *testing.T) {
		t.Parallel()
		testWithTimeout(t, 5*time.Second)
		is := assert.New(t)

		// the root's own scope is shut down by a Started hook, which stops
		// health scheduling for good: reaching Ready does not start it
		scheduler := &manualScheduler{}
		i := NewWithOpts(&InjectorOpts{
			HealthScheduler: scheduler,
			HookContainerStarted: []func(context.Context, *RootScope){
				func(_ context.Context, root *RootScope) { root.self.Shutdown() },
			},
		})
		require.NoError(t, i.Start())

		is.Equal(PhaseReady, i.Phase())
		is.Zero(scheduler.callCount(), "ScheduleHealthRounds calls")
		is.False(i.HealthReport().Monitored)
		i.Shutdown()
	})
}
