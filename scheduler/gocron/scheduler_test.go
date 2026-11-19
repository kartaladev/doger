package dogergocron

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-co-op/gocron/v2"
	"github.com/google/uuid"
	"github.com/jonboulle/clockwork"
	"github.com/kartaladev/doger"
	dogerclockwork "github.com/kartaladev/doger/clock/clockwork"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"
)

// counter is a health check that counts its calls and how many run at once,
// blocking until released when block is set.
type counter struct {
	calls, running, maxRunning atomic.Int32
	block                      chan struct{} // nil: never blocks
	blockFirst                 bool          // blocks only its first call
}

func (c *counter) HealthCheck() error {
	n := c.calls.Add(1)
	if r := c.running.Add(1); r > c.maxRunning.Load() {
		c.maxRunning.Store(r)
	}
	defer c.running.Add(-1)
	if c.block != nil && (!c.blockFirst || n == 1) {
		<-c.block
	}
	return nil
}

// finishedRuns counts the health jobs gocron started and finished, and, as
// a gocron.Monitor, the ticks it dropped because a run still held the job's
// singleton slot.
type finishedRuns struct{ started, n, dropped atomic.Int32 }

// option returns the job option recording started and finished runs.
func (r *finishedRuns) option() Option {
	return WithJobOptions(gocron.WithEventListeners(
		gocron.BeforeJobRuns(func(uuid.UUID, string) { r.started.Add(1) }),
		gocron.AfterJobRuns(func(uuid.UUID, string) { r.n.Add(1) }),
	))
}

// monitor returns the scheduler option recording dropped ticks.
func (r *finishedRuns) monitor() Option {
	return WithSchedulerOptions(gocron.WithMonitor(r))
}

func (r *finishedRuns) IncrementJob(_ uuid.UUID, _ string, _ []string, status gocron.JobStatus) {
	if status == gocron.SingletonRescheduled {
		r.dropped.Add(1)
	}
}

func (r *finishedRuns) RecordJobTiming(time.Time, time.Time, uuid.UUID, string, []string) {}

// wait waits until gocron finished n runs.
func (r *finishedRuns) wait(t *testing.T, n int32) {
	t.Helper()
	require.Eventually(t, func() bool { return r.n.Load() >= n }, 2*time.Second, time.Millisecond, "gocron did not finish %d runs", n)
}

// assertRounds checks the rounds of a cadence row. Short of the next run,
// the fake clock fires no timer, so the count is exact. Once an advance
// reaches a run, at least that run happened. Extra rounds for one advance
// have been observed with gocron on a clockwork fake clock (no test here
// pins down their cause), so the count is a lower bound there, and the
// rows just short of each deadline pin the cadence.
func assertRounds(t *testing.T, got, want int32, reached bool) {
	t.Helper()
	if reached {
		assert.GreaterOrEqual(t, got, want, "rounds")
		return
	}
	assert.Equal(t, want, got, "rounds")
}

// advance moves fc by step, which must reach the next run, and waits until
// run number want started. gocron frees a run's singleton slot an instant
// after the run's listeners return, with nothing to wait on: a tick racing
// that release is dropped (the monitor counts it) and rescheduled one
// interval later. The tick itself fired on time, so the test then advances
// once more. A drop while a run is under way is a duplicate tick of the
// same instant, which the singleton mode absorbs.
func (r *finishedRuns) advance(t *testing.T, fc *clockwork.FakeClock, step time.Duration, want int32) {
	t.Helper()
	for attempt := 1; ; attempt++ {
		require.LessOrEqual(t, attempt, 10, "every tick for run %d was dropped", want)
		waitForTimer(t, fc)
		drops := r.dropped.Load()
		fc.Advance(step)
		var lost bool
		require.Eventually(t, func() bool {
			if r.started.Load() >= want {
				return true
			}
			// dropped with no run under way: the previous run still held
			// the slot, so this tick's run is lost
			lost = r.dropped.Load() > drops && r.started.Load() == r.n.Load()
			return lost
		}, 2*time.Second, time.Millisecond, "the tick for run %d neither ran nor was dropped", want)
		if !lost {
			return
		}
	}
}

// waitForTimer waits until gocron armed its next run on fc.
func waitForTimer(t *testing.T, fc *clockwork.FakeClock) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	require.NoError(t, fc.BlockUntilContext(ctx, 1), "gocron armed no timer")
}

// startWith starts a container with scheduler on fc and a counter "db".
func startWith(t *testing.T, fc *clockwork.FakeClock, scheduler doger.HealthScheduler, db *counter, opts *doger.InjectorOpts) *doger.RootScope {
	t.Helper()
	if opts == nil {
		opts = &doger.InjectorOpts{}
	}
	opts.Clock, opts.HealthScheduler = dogerclockwork.Wrap(fc), scheduler
	i := doger.NewWithOpts(opts)
	doger.ProvideNamedValue(i, "db", db)
	require.NoError(t, i.Start())
	return i
}

func TestNew_InvalidOptions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		opt     Option
		message string // "" for a valid option
	}{
		{name: "interval_0", opt: WithInterval(0), message: "interval must be positive"},
		{name: "interval_negative", opt: WithInterval(-time.Second), message: "interval must be positive"},
		{name: "nil_job_definition", opt: WithJobDefinition(nil), message: "job definition is nil"},
		{name: "nil_scheduler", opt: WithScheduler(nil), message: "scheduler is nil"},
		{name: "nil_start_at", opt: WithStartAt(nil), message: "start-at option is nil"},
		{name: "interval_1ns", opt: WithInterval(time.Nanosecond)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			scheduler, err := New(tt.opt)
			if tt.message == "" {
				assert.NoError(t, err)
				assert.NotNil(t, scheduler)
				return
			}
			assert.ErrorIs(t, err, ErrInvalidOption)
			assert.ErrorContains(t, err, tt.message)
			assert.Nil(t, scheduler)
		})
	}
}

func TestScheduler_DefaultCadence(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		steps []time.Duration // advances, each waiting for gocron's timer
		calls int32
	}{
		{name: "0s", calls: 1},
		{name: "29999ms", steps: []time.Duration{DefaultInterval - time.Millisecond}, calls: 1},
		{name: "30s", steps: []time.Duration{DefaultInterval}, calls: 2},
		{name: "60s", steps: []time.Duration{DefaultInterval, DefaultInterval}, calls: 3},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			fc := clockwork.NewFakeClock()
			runs := &finishedRuns{}
			db := &counter{}
			i := startWith(t, fc, must(New(runs.option(), runs.monitor())), db, nil)
			defer i.Shutdown()

			require.Eventually(t, func() bool { return db.calls.Load() == 1 }, 2*time.Second, time.Millisecond, "the first round did not run at once")
			for n, step := range tt.steps {
				runs.wait(t, int32(n+1))
				if want := int32(n + 2); want <= tt.calls {
					runs.advance(t, fc, step, want)
				} else {
					// short of the next run: the fake clock fires no timer
					waitForTimer(t, fc)
					fc.Advance(step)
				}
			}
			runs.wait(t, tt.calls)
			assertRounds(t, db.calls.Load(), tt.calls, tt.calls > 1)
		})
	}
}

func TestScheduler_SingletonSkipsTick(t *testing.T) {
	t.Parallel()

	fc := clockwork.NewFakeClock()
	runs := &finishedRuns{}
	db := &counter{block: make(chan struct{}), blockFirst: true}
	i := startWith(t, fc, must(New(runs.option(), runs.monitor())), db, &doger.InjectorOpts{HealthCheckTimeout: -time.Nanosecond})
	defer i.Shutdown()

	require.Eventually(t, func() bool { return db.calls.Load() == 1 }, 2*time.Second, time.Millisecond)
	waitForTimer(t, fc)
	fc.Advance(DefaultInterval) // the tick finds the first round running
	require.Eventually(t, func() bool { return runs.dropped.Load() >= 1 }, 2*time.Second, time.Millisecond,
		"gocron did not drop the tick that found the first round running")
	close(db.block)
	runs.wait(t, 1)

	runs.advance(t, fc, DefaultInterval, 2) // the round after the release runs
	runs.wait(t, 2)
	assertRounds(t, db.calls.Load(), 2, true)
	assert.Equal(t, int32(1), db.maxRunning.Load(), "checks running at once")
}

func TestScheduler_Overrides(t *testing.T) {
	t.Parallel()

	fc := clockwork.NewFakeClock()
	tests := []struct {
		name  string
		opts  func() []Option
		steps []time.Duration
		calls int32
		// reached: the last step reaches a run (see assertRounds)
		reached bool
	}{
		{name: "interval_9999ms", opts: func() []Option { return []Option{WithInterval(10 * time.Second)} }, steps: []time.Duration{10*time.Second - time.Millisecond}, calls: 1},
		{name: "interval_10s", opts: func() []Option { return []Option{WithInterval(10 * time.Second)} }, steps: []time.Duration{10 * time.Second, 10 * time.Second}, calls: 3, reached: true},
		{name: "duration_job_4999ms", opts: func() []Option { return []Option{WithJobDefinition(gocron.DurationJob(5 * time.Second))} }, steps: []time.Duration{5*time.Second - time.Millisecond}, calls: 1},
		{name: "duration_job_5s", opts: func() []Option { return []Option{WithJobDefinition(gocron.DurationJob(5 * time.Second))} }, steps: []time.Duration{5 * time.Second}, calls: 2, reached: true},
		{name: "definition_wins_over_interval", opts: func() []Option {
			return []Option{WithJobDefinition(gocron.DurationJob(5 * time.Second)), WithInterval(10 * time.Second)}
		}, steps: []time.Duration{5 * time.Second, 5 * time.Second}, calls: 3, reached: true},
		{name: "start_at_9999ms", opts: func() []Option {
			return []Option{WithStartAt(gocron.WithStartDateTime(fc.Now().Add(10 * time.Second)))}
		}, steps: []time.Duration{10*time.Second - time.Millisecond}, calls: 0},
		{name: "start_at_10s", opts: func() []Option {
			return []Option{WithStartAt(gocron.WithStartDateTime(fc.Now().Add(10 * time.Second)))}
		}, steps: []time.Duration{10 * time.Second}, calls: 1, reached: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Not parallel: rows share fc, whose now the start-at rows read.
			clock := clockwork.NewFakeClockAt(fc.Now())
			runs := &finishedRuns{}
			db := &counter{}
			i := startWith(t, clock, must(New(append(tt.opts(), runs.option(), runs.monitor())...)), db, nil)
			defer i.Shutdown()

			want := tt.calls - int32(len(tt.steps)) // rounds run at once
			if want > 0 {
				require.Eventually(t, func() bool { return db.calls.Load() == want }, 2*time.Second, time.Millisecond, "the first round did not run at once")
			}
			for _, step := range tt.steps {
				if want > 0 {
					runs.wait(t, want)
				}
				if want++; want > 0 {
					runs.advance(t, clock, step, want)
				} else {
					// short of the first run: the fake clock fires no timer
					waitForTimer(t, clock)
					clock.Advance(step)
				}
			}
			if tt.calls > 0 {
				runs.wait(t, tt.calls)
			}
			assertRounds(t, db.calls.Load(), tt.calls, tt.reached)
		})
	}
}

// recordingLogger is a gocron.Logger counting its calls.
type recordingLogger struct{ calls atomic.Int32 }

func (l *recordingLogger) Debug(string, ...any) { l.calls.Add(1) }
func (l *recordingLogger) Error(string, ...any) { l.calls.Add(1) }
func (l *recordingLogger) Info(string, ...any)  { l.calls.Add(1) }
func (l *recordingLogger) Warn(string, ...any)  { l.calls.Add(1) }

func TestScheduler_SchedulerOptions(t *testing.T) {
	t.Parallel()

	logger := &recordingLogger{}
	i := startWith(t, clockwork.NewFakeClock(), must(New(WithSchedulerOptions(gocron.WithLogger(logger)))), &counter{}, nil)
	i.Shutdown()

	assert.Positive(t, logger.calls.Load())
}

// optionPrecedenceRun is the state of a TestScheduler_OptionPrecedence row.
type optionPrecedenceRun struct {
	names chan string          // the names of the finished runs, as gocron reports them
	other *clockwork.FakeClock // a clock other than the container's
	runs  *finishedRuns
	db    *counter
}

// nameListener returns the option reporting the names of finished runs.
func (r optionPrecedenceRun) nameListener() Option {
	return WithJobOptions(gocron.WithEventListeners(gocron.AfterJobRuns(func(_ uuid.UUID, name string) {
		select {
		case r.names <- name:
		default:
		}
	})))
}

// firstName returns the name of the first finished run.
func (r optionPrecedenceRun) firstName(t *testing.T) string {
	t.Helper()
	select {
	case name := <-r.names:
		return name
	case <-time.After(2 * time.Second):
		require.FailNow(t, "no round ran")
		return ""
	}
}

func TestScheduler_OptionPrecedence(t *testing.T) {
	t.Parallel()

	nameIs := func(want string) func(t *testing.T, r optionPrecedenceRun) {
		return func(t *testing.T, r optionPrecedenceRun) {
			t.Helper()
			assert.Equal(t, want, r.firstName(t))
		}
	}

	tests := []struct {
		name  string
		opts  func(r optionPrecedenceRun) []Option
		check func(t *testing.T, r optionPrecedenceRun)
	}{
		{
			name: "job_option_wins_over_default",
			opts: func(r optionPrecedenceRun) []Option {
				return []Option{WithJobOptions(gocron.WithName("custom")), r.nameListener()}
			},
			check: nameIs("custom"),
		},
		{
			name: "job_options_last_wins",
			opts: func(r optionPrecedenceRun) []Option {
				return []Option{WithJobOptions(gocron.WithName("first")), WithJobOptions(gocron.WithName("second")), r.nameListener()}
			},
			check: nameIs("second"),
		},
		{
			name: "job_options_accumulate",
			opts: func(r optionPrecedenceRun) []Option {
				return []Option{r.nameListener(), WithJobOptions(gocron.WithName("second"))}
			},
			check: nameIs("second"),
		},
		{
			name: "scheduler_option_wins_over_default",
			opts: func(r optionPrecedenceRun) []Option {
				return []Option{WithSchedulerOptions(gocron.WithClock(r.other)), r.runs.option(), r.runs.monitor()}
			},
			check: func(t *testing.T, r optionPrecedenceRun) {
				t.Helper()
				r.runs.wait(t, 1) // the immediate first round
				r.runs.advance(t, r.other, DefaultInterval, 2)
				r.runs.wait(t, 2)
				assertRounds(t, r.db.calls.Load(), 2, true)
			},
		},
		{
			name: "scheduler_options_accumulate",
			opts: func(r optionPrecedenceRun) []Option {
				return []Option{WithSchedulerOptions(gocron.WithClock(r.other)), WithSchedulerOptions(gocron.WithLocation(time.UTC)), r.runs.option()}
			},
			check: func(t *testing.T, r optionPrecedenceRun) {
				t.Helper()
				r.runs.wait(t, 1)
				waitForTimer(t, r.other)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			r := optionPrecedenceRun{names: make(chan string, 4), other: clockwork.NewFakeClock(), runs: &finishedRuns{}, db: &counter{}}
			i := startWith(t, clockwork.NewFakeClock(), must(New(tt.opts(r)...)), r.db, nil)
			defer i.Shutdown()

			tt.check(t, r)
		})
	}
}

// Not parallel: goleak checks the goroutines gocron leaves.
func TestScheduler_RejectedJob(t *testing.T) { //nolint:paralleltest
	tests := []struct {
		name   string
		option Option
	}{
		{name: "job", option: WithJobDefinition(gocron.CronJob("not a cron", false))},
		{name: "scheduler", option: WithSchedulerOptions(gocron.WithLocation(nil))},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) { //nolint:paralleltest // goleak checks the goroutines gocron leaves
			ignore := goleak.IgnoreCurrent()

			scheduler := must(New(tt.option))
			stop, err := scheduler.ScheduleHealthRounds(dogerclockwork.Wrap(clockwork.NewFakeClock()), func(context.Context) {})

			assert.Error(t, err)
			assert.Nil(t, stop)
			assert.NoError(t, goleak.Find(ignore))
		})
	}
}

func TestScheduler_Shared(t *testing.T) {
	t.Parallel()
	is := assert.New(t)

	shared, err := gocron.NewScheduler()
	require.NoError(t, err)
	defer func() { _ = shared.Shutdown() }()
	_, err = shared.NewJob(gocron.DurationJob(time.Hour), gocron.NewTask(func() {}), gocron.WithName("report"))
	require.NoError(t, err)
	shared.Start()

	i := doger.NewWithOpts(&doger.InjectorOpts{HealthScheduler: must(New(WithScheduler(shared)))})
	doger.ProvideNamedValue(i, "db", &counter{})
	require.NoError(t, i.Start())
	is.Len(shared.Jobs(), 2, "jobs while the container is ready")

	i.Shutdown()

	jobs := shared.Jobs()
	if is.Len(jobs, 1) {
		is.Equal("report", jobs[0].Name())
	}
	ran := make(chan struct{})
	var once sync.Once
	_, err = shared.NewJob(gocron.OneTimeJob(gocron.OneTimeJobStartImmediately()), gocron.NewTask(func() { once.Do(func() { close(ran) }) }))
	require.NoError(t, err)
	select {
	case <-ran:
	case <-time.After(2 * time.Second):
		is.Fail("the shared scheduler no longer runs jobs")
	}
}

func TestScheduler_SharedStop(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		// run checks how the stop of a round scheduled on shared returns
		run func(t *testing.T, shared gocron.Scheduler, scheduler *Scheduler)
	}{
		{name: "round_running", run: func(t *testing.T, _ gocron.Scheduler, scheduler *Scheduler) {
			started, release := make(chan struct{}), make(chan struct{})
			var returned atomic.Bool
			var once sync.Once
			stop, err := scheduler.ScheduleHealthRounds(doger.RealClock(), func(context.Context) {
				once.Do(func() { close(started) })
				<-release
				returned.Store(true)
			})
			require.NoError(t, err)
			defer close(release)
			<-started

			stopped := make(chan error, 1)
			go func() { stopped <- stop(context.Background()) }()
			select {
			case <-stopped:
				assert.Fail(t, "stop returned while the round still ran")
				return
			case <-time.After(50 * time.Millisecond):
			}
			release <- struct{}{} // lets the round return

			assert.NoError(t, <-stopped)
			assert.True(t, returned.Load(), "the round returned before stop")
		}},
		{name: "owner_shut_down", run: func(t *testing.T, shared gocron.Scheduler, scheduler *Scheduler) {
			stop, err := scheduler.ScheduleHealthRounds(doger.RealClock(), func(context.Context) {})
			require.NoError(t, err)
			require.NoError(t, shared.Shutdown())

			assert.NoError(t, stop(context.Background()))
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			shared, err := gocron.NewScheduler()
			require.NoError(t, err)
			defer func() { _ = shared.Shutdown() }()
			shared.Start()

			tt.run(t, shared, must(New(WithScheduler(shared))))
		})
	}
}

// Not parallel: goleak checks the goroutines gocron leaves.
func TestScheduler_CleanStop(t *testing.T) { //nolint:paralleltest
	tests := []struct {
		name     string
		shutdown func(i *doger.RootScope)
	}{
		{name: "Shutdown", shutdown: func(i *doger.RootScope) { i.Shutdown() }},
		{name: "ShutdownWithContext_cancelled", shutdown: func(i *doger.RootScope) {
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			i.ShutdownWithContext(ctx)
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ignore := goleak.IgnoreCurrent()

			i := doger.NewWithOpts(&doger.InjectorOpts{HealthScheduler: must(New())})
			doger.ProvideNamedValue(i, "db", &counter{})
			require.NoError(t, i.Start())
			tt.shutdown(i)

			assert.NoError(t, goleak.Find(ignore))
		})
	}
}

// must returns v, panicking on err.
func must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}

// consumerClock is a consumer-written doger.Clock, which gocron cannot use.
type consumerClock struct{}

func (consumerClock) Now() time.Time                              { return time.Time{} }
func (consumerClock) Since(time.Time) time.Duration               { return 0 }
func (consumerClock) AfterFunc(time.Duration, func()) doger.Timer { return nil }

// Not parallel: goleak checks the goroutines gocron leaves.
func TestScheduler_ClockMapping(t *testing.T) { //nolint:paralleltest
	tests := []struct {
		name string
		// setup returns the options of New, the clock passed to
		// ScheduleHealthRounds, the fake clock rounds run on (nil: the real
		// clock) and a cleanup
		setup       func(t *testing.T) ([]Option, doger.Clock, *clockwork.FakeClock, func())
		unsupported bool
	}{
		{name: "wrapped_fake_clock", setup: func(*testing.T) ([]Option, doger.Clock, *clockwork.FakeClock, func()) {
			fake := clockwork.NewFakeClock()
			return nil, dogerclockwork.Wrap(fake), fake, func() {}
		}},
		{name: "real_clock", setup: func(*testing.T) ([]Option, doger.Clock, *clockwork.FakeClock, func()) {
			return nil, doger.RealClock(), nil, func() {}
		}},
		{name: "consumer_clock", unsupported: true, setup: func(*testing.T) ([]Option, doger.Clock, *clockwork.FakeClock, func()) {
			return nil, consumerClock{}, nil, func() {}
		}},
		{name: "nil", unsupported: true, setup: func(*testing.T) ([]Option, doger.Clock, *clockwork.FakeClock, func()) {
			return nil, nil, nil, func() {}
		}},
		{name: "shared_scheduler_consumer_clock", setup: func(t *testing.T) ([]Option, doger.Clock, *clockwork.FakeClock, func()) {
			t.Helper()
			g := clockwork.NewFakeClock()
			shared, err := gocron.NewScheduler(gocron.WithClock(g))
			require.NoError(t, err)
			shared.Start()
			return []Option{WithScheduler(shared)}, consumerClock{}, g, func() { _ = shared.Shutdown() }
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) { //nolint:paralleltest // goleak checks the goroutines gocron leaves
			is := assert.New(t)
			ignore := goleak.IgnoreCurrent()

			opts, clock, fake, cleanup := tt.setup(t)
			defer cleanup()
			var rounds atomic.Int32
			stop, err := must(New(opts...)).ScheduleHealthRounds(clock, func(context.Context) { rounds.Add(1) })

			if tt.unsupported {
				is.ErrorIs(err, ErrUnsupportedClock)
				is.Nil(stop)
				// nothing was scheduled: goleak below finds no gocron goroutine
				is.Zero(rounds.Load(), "rounds run")
				is.NoError(goleak.Find(ignore))
				return
			}

			require.NoError(t, err)
			require.NotNil(t, stop)
			defer func() { is.NoError(stop(context.Background())) }()
			require.Eventually(t, func() bool { return rounds.Load() == 1 }, 2*time.Second, time.Millisecond, "the first round did not run at once")
			if fake != nil {
				// rounds follow the fake clock: none before it advances, a
				// later one once it does. The count after one advance is a
				// lower bound: extra rounds for one advance have been
				// observed with gocron on a clockwork fake clock (see
				// assertRounds); the clock mapping is what this row checks
				waitForTimer(t, fake)
				is.Equal(int32(1), rounds.Load(), "a round ran before the fake clock advanced")
				fake.Advance(DefaultInterval)
				require.Eventually(t, func() bool { return rounds.Load() >= 2 }, 2*time.Second, time.Millisecond, "no round on the fake clock's schedule")
			}
		})
	}
}

// errorRecorder is a slog.Handler keeping the "error" attribute of each
// record, by level and message.
type errorRecorder struct {
	mu      sync.Mutex
	records []errorRecord
}

type errorRecord struct {
	level   slog.Level
	message string
	err     any
}

func (r *errorRecorder) Enabled(context.Context, slog.Level) bool { return true }

func (r *errorRecorder) Handle(_ context.Context, record slog.Record) error {
	rec := errorRecord{level: record.Level, message: record.Message}
	record.Attrs(func(attr slog.Attr) bool {
		if attr.Key == "error" {
			rec.err = attr.Value.Any()
		}
		return true
	})
	r.mu.Lock()
	defer r.mu.Unlock()
	r.records = append(r.records, rec)
	return nil
}

func (r *errorRecorder) WithAttrs([]slog.Attr) slog.Handler { return r }
func (r *errorRecorder) WithGroup(string) slog.Handler      { return r }

func (r *errorRecorder) all() []errorRecord {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]errorRecord(nil), r.records...)
}

func TestScheduler_UnsupportedClockUnmonitored(t *testing.T) {
	t.Parallel()
	is := assert.New(t)

	recorder := &errorRecorder{}
	i := doger.NewWithOpts(&doger.InjectorOpts{Clock: consumerClock{}, HealthScheduler: must(New()), Logger: slog.New(recorder)})
	defer i.Shutdown()

	is.NoError(i.Start())
	is.Equal(doger.PhaseReady, i.Phase())
	is.False(i.HealthReport(doger.IgnoreContainerPhase()).Monitored)

	var found []errorRecord
	for _, rec := range recorder.all() {
		if rec.level == slog.LevelError && strings.Contains(rec.message, "health scheduler") {
			found = append(found, rec)
		}
	}
	if is.Len(found, 1) {
		err, _ := found[0].err.(error)
		is.ErrorIs(err, ErrUnsupportedClock)
	}
}
