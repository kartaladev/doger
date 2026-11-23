package doger

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// sleepStarter is a Starter whose Start ignores its context, sleeps d and
// returns nil.
type sleepStarter struct{ d time.Duration }

func (s *sleepStarter) Start(context.Context) error {
	time.Sleep(s.d)
	return nil
}

// startedNamed reports whether the service name registered in scope is
// recorded as started by root.
func startedNamed(t *testing.T, root *RootScope, scope *Scope, name string) bool {
	t.Helper()

	wrapper, ok := scope.serviceGet(name)
	require.True(t, ok, "service %q is not registered", name)

	return root.isStarted(wrapper)
}

// TestStartTimeout_DefaultWaits pins container-start "No start timeout
// waits for a slow step": without a positive StartTimeout, Start waits for
// a Starter that ignores its context, exactly as before the option existed.
func TestStartTimeout_DefaultWaits(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		timeout     time.Duration
		ctxDeadline time.Duration // 0: context.Background()
		wantErr     string        // "": Start returns nil
	}{
		{name: "zero", timeout: 0},
		{name: "negative", timeout: -time.Second},
		{name: "ctx_deadline", timeout: 0, ctxDeadline: 100 * time.Millisecond, wantErr: "doger: start aborted: context deadline exceeded"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			synctest.Test(t, func(t *testing.T) {
				root := NewWithOpts(&InjectorOpts{StartTimeout: tt.timeout})
				ProvideNamed(root, "srv", func(Injector) (*sleepStarter, error) {
					return &sleepStarter{d: time.Hour}, nil
				}, AsStarter())

				ctx := context.Background()
				if tt.ctxDeadline > 0 {
					var cancel context.CancelFunc
					ctx, cancel = context.WithTimeout(ctx, tt.ctxDeadline)
					defer cancel()
				}

				begin := time.Now()
				err := root.StartWithContext(ctx)

				assert.Equal(t, time.Hour, time.Since(begin), "Start must wait for the whole Start method")
				if tt.wantErr == "" {
					require.NoError(t, err)
				} else {
					require.EqualError(t, err, tt.wantErr)
					assert.ErrorIs(t, err, context.DeadlineExceeded)
					assert.NotErrorIs(t, err, ErrStartTimeout)
				}
				assert.True(t, startedNamed(t, root, root.self, "srv"), "a Start that returned nil is recorded as started")

				root.Shutdown()
			})
		})
	}
}

// stepProbe is a Starter (Start(ctx) error) and a Runner whose call either
// sleeps run, or blocks until release is closed when run is 0. It counts
// its calls, keeps the context of the last one, closes ended when its first
// call ends, and returns outcome(call) (nil when outcome is nil).
type stepProbe struct {
	run     time.Duration
	release chan struct{}
	outcome func(call int32) error

	calls     atomic.Int32
	shutdowns atomic.Int32
	ctx       atomic.Pointer[context.Context]
	endOnce   sync.Once
	ended     chan struct{}
}

func newStepProbe(run time.Duration) *stepProbe {
	return &stepProbe{run: run, release: make(chan struct{}), ended: make(chan struct{})}
}

func (p *stepProbe) call(ctx context.Context) error {
	call := p.calls.Add(1)
	p.ctx.Store(&ctx)
	defer p.endOnce.Do(func() { close(p.ended) })

	if p.run > 0 {
		time.Sleep(p.run)
	} else if call == 1 {
		<-p.release
	}
	if p.outcome == nil {
		return nil
	}

	return p.outcome(call)
}

func (p *stepProbe) Start(ctx context.Context) error { return p.call(ctx) }
func (p *stepProbe) Run(ctx context.Context) error   { return p.call(ctx) }

// Shutdown releases a blocked first call, as a server's Shutdown stops
// its Serve loop.
func (p *stepProbe) Shutdown() error {
	p.shutdowns.Add(1)
	p.unblock()
	return nil
}

// unblock releases a blocked first call.
func (p *stepProbe) unblock() {
	select {
	case <-p.release:
	default:
		close(p.release)
	}
}

// plainSleeper is a Starter with Start() (no context, no error) that
// sleeps d.
type plainSleeper struct{ d time.Duration }

func (s *plainSleeper) Start() { time.Sleep(s.d) }

// TestStartTimeout_Abandons pins container-start "A step that outlives the
// start timeout is abandoned".
func TestStartTimeout_Abandons(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		provide func(root *RootScope) // registers "srv"
		returns time.Duration
		wantErr string // "" with wantOp "": Start returns nil
		wantOp  StartOp
		kind    string
		later   bool
	}{
		{
			name:    "inside_bound",
			provide: func(root *RootScope) { provideCounted(root, "srv", newStepProbe(4*time.Second), AsStarter()) },
			returns: 4 * time.Second,
			later:   true,
		},
		{
			name:    "start_ctx_error",
			provide: func(root *RootScope) { provideCounted(root, "srv", newStepProbe(6*time.Second), AsStarter()) },
			returns: 5 * time.Second,
			wantErr: "doger: failed to start service `srv` in scope `[root]`: start timeout after 5s: context deadline exceeded",
			wantOp:  StartOpStart,
		},
		{
			name:    "start_plain",
			provide: func(root *RootScope) { provideCounted(root, "srv", &plainSleeper{d: 6 * time.Second}, AsStarter()) },
			returns: 5 * time.Second,
			wantOp:  StartOpStart,
		},
		{
			name:    "runnable",
			provide: func(root *RootScope) { provideCounted(root, "srv", newStepProbe(6*time.Second), AsRunnable()) },
			returns: 5 * time.Second,
			wantErr: "doger: kind `runnable` failed for service `srv` in scope `[root]`: start timeout after 5s: context deadline exceeded",
			wantOp:  StartOpHandle,
			kind:    "runnable",
			later:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			synctest.Test(t, func(t *testing.T) {
				root := NewWithOpts(&InjectorOpts{StartTimeout: 5 * time.Second})
				tt.provide(root)
				later := provideCounted(root, "later", &startWithCtxErr{}, AsStarter())

				begin := time.Now()
				err := root.Start()

				assert.Equal(t, tt.returns, time.Since(begin))
				assert.Equal(t, tt.later, later.starts.Load() == 1, "later started")
				if tt.wantOp == "" {
					require.NoError(t, err)
				} else {
					require.Error(t, err)
					assert.ErrorIs(t, err, ErrStartTimeout)
					assert.ErrorIs(t, err, context.DeadlineExceeded)
					if tt.wantErr != "" {
						assert.EqualError(t, err, tt.wantErr)
					}
					var startErr *StartError
					require.ErrorAs(t, err, &startErr)
					assert.Equal(t, "srv", startErr.Service)
					assert.Equal(t, "[root]", startErr.Scope.Name())
					assert.Equal(t, tt.wantOp, startErr.Op)
					assert.Equal(t, tt.kind, startErr.Kind)
				}

				time.Sleep(time.Hour) // let an abandoned call end inside the bubble
				root.Shutdown()
			})
		})
	}
}

// TestStartTimeout_CancelsStepContext pins container-start "The abandoned
// step's context is cancelled".
func TestStartTimeout_CancelsStepContext(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		root := NewWithOpts(&InjectorOpts{StartTimeout: 5 * time.Second})
		probe := provideCounted(root, "srv", newStepProbe(0), AsStarter())

		err := root.Start()

		require.ErrorIs(t, err, ErrStartTimeout)
		kept := probe.ctx.Load()
		require.NotNil(t, kept)
		assert.Error(t, (*kept).Err(), "the abandoned step's context is cancelled")

		probe.unblock()
		<-probe.ended
		root.Shutdown()
	})
}

// goexitStarter is a Starter whose Start ends its goroutine with
// runtime.Goexit.
type goexitStarter struct{}

func (goexitStarter) Start(context.Context) error {
	runtime.Goexit()
	return nil
}

// TestStartTimeout_Goexit pins container-start "A step that ends with
// runtime.Goexit before the timeout".
func TestStartTimeout_Goexit(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		root := NewWithOpts(&InjectorOpts{StartTimeout: 5 * time.Second})
		provideCounted(root, "srv", goexitStarter{}, AsStarter())

		begin := time.Now()
		err := root.Start()

		assert.Zero(t, time.Since(begin), "Start returns at once")
		var startErr *StartError
		require.ErrorAs(t, err, &startErr)
		assert.Equal(t, "srv", startErr.Service)
		assert.Equal(t, StartOpStart, startErr.Op)
		assert.NotErrorIs(t, err, ErrStartTimeout)

		root.Shutdown()
	})
}

// TestStartTimeout_FakeClock pins container-start "Start timeout waits for
// the container clock". The clockwork row uses fakeClock, the root tests'
// clockwork-backed Clock (they cannot import dogerclockwork).
func TestStartTimeout_FakeClock(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		clock func() (Clock, func(t *testing.T), func(d time.Duration))
	}{
		{
			name: "clockwork",
			clock: func() (Clock, func(t *testing.T), func(d time.Duration)) {
				fake := newFakeClock()
				armed := func(t *testing.T) {
					t.Helper()
					ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
					defer cancel()
					require.NoError(t, fake.BlockUntilContext(ctx, 1))
				}
				return fake, armed, fake.Advance
			},
		},
		{
			name: "manual",
			clock: func() (Clock, func(t *testing.T), func(d time.Duration)) {
				manual := newManualClock()
				armed := func(t *testing.T) {
					t.Helper()
					require.Eventually(t, func() bool { _, pending := manual.counts(); return pending == 1 }, 5*time.Second, time.Millisecond)
				}
				return manual, armed, manual.Advance
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			testWithTimeout(t, 10*time.Second)

			clock, waitArmed, advance := tt.clock()
			root := NewWithOpts(&InjectorOpts{StartTimeout: 10 * time.Millisecond, Clock: clock})
			probe := provideCounted(root, "srv", newStepProbe(0), AsStarter())

			done := make(chan error, 1)
			go func() { done <- root.Start() }()

			waitArmed(t)
			time.Sleep(50 * time.Millisecond) // wall time, the clock does not move
			select {
			case err := <-done:
				t.Fatalf("Start returned before the clock advanced: %v", err)
			default:
			}

			advance(10 * time.Millisecond)
			err := <-done
			assert.ErrorIs(t, err, ErrStartTimeout)
			assert.ErrorIs(t, err, context.DeadlineExceeded)

			probe.unblock()
			<-probe.ended
			root.Shutdown()
		})
	}
}

// TestStartTimeout_AfterStartHookOnce pins container-start "HookAfterStart
// fires once at abandonment".
func TestStartTimeout_AfterStartHookOnce(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		var mu sync.Mutex
		var calls []string
		var afterErr error
		root := NewWithOpts(&InjectorOpts{
			StartTimeout: 5 * time.Second,
			HookBeforeStart: []func(context.Context, *Scope, string){func(_ context.Context, _ *Scope, name string) {
				mu.Lock()
				defer mu.Unlock()
				calls = append(calls, "before "+name)
			}},
			HookAfterStart: []func(context.Context, *Scope, string, time.Duration, error){func(_ context.Context, _ *Scope, name string, elapsed time.Duration, err error) {
				mu.Lock()
				defer mu.Unlock()
				calls = append(calls, fmt.Sprintf("after %s elapsed=%s", name, elapsed))
				afterErr = err
			}},
		})
		probe := provideCounted(root, "srv", newStepProbe(0), AsStarter())

		require.ErrorIs(t, root.Start(), ErrStartTimeout)
		probe.unblock()
		<-probe.ended
		synctest.Wait()

		mu.Lock()
		defer mu.Unlock()
		assert.Equal(t, []string{"before srv", "after srv elapsed=5s"}, calls)
		assert.ErrorIs(t, afterErr, ErrStartTimeout)

		root.Shutdown()
	})
}

// TestStartTimeout_RunContextEnds pins container-start "An ended run
// context abandons the step".
func TestStartTimeout_RunContextEnds(t *testing.T) {
	t.Parallel()

	const prefix = "doger: failed to start service `srv` in scope `[root]`: start timeout after 5s: context deadline exceeded"
	tests := []struct {
		name    string
		trigger func(root *RootScope, cancel context.CancelFunc) // runs at 100ms
		ctx     func() (context.Context, context.CancelFunc)
		cause   error
		message string
	}{
		{
			name: "deadline",
			ctx: func() (context.Context, context.CancelFunc) {
				return context.WithTimeout(context.Background(), 100*time.Millisecond)
			},
			cause:   context.DeadlineExceeded,
			message: prefix + " (start aborted: context deadline exceeded)",
		},
		{
			name:    "cancel",
			ctx:     func() (context.Context, context.CancelFunc) { return context.WithCancel(context.Background()) },
			trigger: func(_ *RootScope, cancel context.CancelFunc) { cancel() },
			cause:   context.Canceled,
			message: prefix + " (start aborted: context canceled)",
		},
		{
			name:    "shutdown",
			ctx:     func() (context.Context, context.CancelFunc) { return context.WithCancel(context.Background()) },
			trigger: func(root *RootScope, _ context.CancelFunc) { root.Shutdown() },
			cause:   ErrShutdown,
			message: prefix + " (start aborted: container is shut down)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			synctest.Test(t, func(t *testing.T) {
				root := NewWithOpts(&InjectorOpts{StartTimeout: 5 * time.Second})
				probe := provideCounted(root, "srv", newStepProbe(0), AsStarter())

				ctx, cancel := tt.ctx()
				defer cancel()
				var triggered sync.WaitGroup
				if tt.trigger != nil {
					triggered.Go(func() {
						time.Sleep(100 * time.Millisecond)
						tt.trigger(root, cancel)
					})
				}

				begin := time.Now()
				err := root.StartWithContext(ctx)

				assert.Equal(t, 5*time.Second, time.Since(begin), "the run waits for the step up to StartTimeout")
				var startErr *StartError
				require.ErrorAs(t, err, &startErr)
				assert.Equal(t, "srv", startErr.Service)
				assert.Equal(t, StartOpStart, startErr.Op)
				assert.ErrorIs(t, err, ErrStartTimeout)
				assert.ErrorIs(t, err, context.DeadlineExceeded)
				assert.ErrorIs(t, err, tt.cause)
				assert.EqualError(t, err, tt.message)

				triggered.Wait()
				root.Shutdown() // releases "srv" through its Shutdown
				<-probe.ended
			})
		})
	}
}

// TestStartTimeout_ShutdownDuringWait pins container-start "Shutdown
// during a stuck step shuts the service down".
func TestStartTimeout_ShutdownDuringWait(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		root := NewWithOpts(&InjectorOpts{StartTimeout: 5 * time.Second})
		probe := provideCounted(root, "srv", newStepProbe(0), AsStarter())
		wrapper, ok := root.self.serviceGet("srv")
		require.True(t, ok)

		var report *ShutdownReport
		var shutdownTook time.Duration
		var shut sync.WaitGroup
		shut.Go(func() {
			time.Sleep(100 * time.Millisecond)
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			begin := time.Now()
			report = root.ShutdownWithContext(ctx)
			shutdownTook = time.Since(begin)
		})

		begin := time.Now()
		err := root.Start()
		startTook := time.Since(begin)
		shut.Wait()
		<-probe.ended
		synctest.Wait()

		assert.Equal(t, 5*time.Second, startTook, "the run waits for the stuck step up to StartTimeout")
		assert.ErrorIs(t, err, ErrShutdown)
		assert.ErrorIs(t, err, ErrStartTimeout)
		assert.Equal(t, int32(1), probe.shutdowns.Load(), "Shutdown called srv's Shutdown exactly once")
		require.NotNil(t, report)
		assert.NoError(t, report.Err())
		assert.Less(t, shutdownTook, 10*time.Second, "Shutdown returned before its deadline")
		assert.False(t, root.isStarted(wrapper), "a late return after Shutdown is not recorded")
	})
}

// honouringStarter is a Starter whose Start waits for its context to end,
// keeps the context's cause, then cleans up for cleanup and returns
// ctx.Err(). Its Shutdown records whether that cleanup had finished.
type honouringStarter struct {
	cleanup time.Duration

	cause               atomic.Pointer[error]
	cleanupDone         atomic.Bool
	shutdowns           atomic.Int32
	shutdownBeforeClean atomic.Bool
	entered, returned   chan struct{}
	enterOnce           sync.Once
}

func newHonouringStarter(cleanup time.Duration) *honouringStarter {
	return &honouringStarter{cleanup: cleanup, entered: make(chan struct{}), returned: make(chan struct{})}
}

func (s *honouringStarter) Start(ctx context.Context) error {
	defer close(s.returned)
	s.enterOnce.Do(func() { close(s.entered) })
	<-ctx.Done()
	cause := context.Cause(ctx)
	s.cause.Store(&cause)
	time.Sleep(s.cleanup)
	s.cleanupDone.Store(true)

	return ctx.Err()
}

// startCause returns the cause of the end of Start's context, or nil while
// it has not ended.
func (s *honouringStarter) startCause() error {
	if cause := s.cause.Load(); cause != nil {
		return *cause
	}

	return nil
}

func (s *honouringStarter) Shutdown() error {
	s.shutdowns.Add(1)
	if !s.cleanupDone.Load() {
		s.shutdownBeforeClean.Store(true)
	}

	return nil
}

// TestStartTimeout_ShutdownAwaitsHonouringStart pins container-start "A
// Start that honours its context is awaited when Shutdown cancels it".
func TestStartTimeout_ShutdownAwaitsHonouringStart(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		root := NewWithOpts(&InjectorOpts{StartTimeout: 5 * time.Second})
		srv := provideCounted(root, "srv", newHonouringStarter(100*time.Millisecond), AsStarter())

		var shut sync.WaitGroup
		shut.Go(func() {
			<-srv.entered
			time.Sleep(100 * time.Millisecond)
			root.Shutdown()
		})

		begin := time.Now()
		err := root.Start()
		startTook := time.Since(begin)
		shut.Wait()
		<-srv.returned

		assert.Equal(t, 200*time.Millisecond, startTook, "the run waits for the Start that honours its context")
		assert.ErrorIs(t, err, ErrShutdown)
		assert.ErrorIs(t, err, context.Canceled)
		assert.NotErrorIs(t, err, ErrStartTimeout)
		assert.Equal(t, int32(1), srv.shutdowns.Load(), "srv's Shutdown method was called exactly once")
		assert.False(t, srv.shutdownBeforeClean.Load(), "srv's Shutdown method was called after its Start cleanup finished")
	})
}

// TestStartTimeout_ShutdownBoundedBelowTimeout pins container-start "A
// Shutdown bounded below the start timeout": the Shutdown stops waiting at
// its deadline and skips the service, as without the option.
func TestStartTimeout_ShutdownBoundedBelowTimeout(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		root := NewWithOpts(&InjectorOpts{StartTimeout: 5 * time.Second})
		probe := provideCounted(root, "srv", newStepProbe(0), AsStarter())
		wrapper, ok := root.self.serviceGet("srv")
		require.True(t, ok)

		var report *ShutdownReport
		var shutdownTook time.Duration
		var shut sync.WaitGroup
		shut.Go(func() {
			time.Sleep(100 * time.Millisecond)
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			begin := time.Now()
			report = root.ShutdownWithContext(ctx)
			shutdownTook = time.Since(begin)
		})

		begin := time.Now()
		err := root.Start()
		startTook := time.Since(begin)
		shut.Wait()

		assert.Equal(t, time.Second, shutdownTook, "Shutdown stops waiting at its deadline")
		require.NotNil(t, report)
		assert.False(t, report.Succeed)
		var scopeErr, srvErr error
		for desc, e := range report.Errors {
			switch desc.Service {
			case "":
				scopeErr = e
			case "srv":
				srvErr = e
			}
		}
		assert.ErrorIs(t, scopeErr, context.DeadlineExceeded, "the report has an error for the root scope's wait")
		assert.ErrorIs(t, srvErr, context.DeadlineExceeded, "the ended Shutdown skipped srv")
		assert.Zero(t, probe.shutdowns.Load(), "srv's Shutdown method was not called")
		assert.Equal(t, 5*time.Second, startTook, "Start returns at StartTimeout")
		assert.ErrorIs(t, err, ErrStartTimeout)
		assert.ErrorIs(t, err, ErrShutdown)

		probe.unblock()
		<-probe.ended
		synctest.Wait()
		assert.False(t, root.isStarted(wrapper), "a late return after Shutdown is not recorded")
	})
}

// TestStartTimeout_Parallel pins container-start "Abandonment under
// parallel start".
func TestStartTimeout_Parallel(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		root := NewWithOpts(&InjectorOpts{StartTimeout: 5 * time.Second, StartParallelism: 4})
		stuck := provideCounted(root, "stuck", newStepProbe(0), AsStarter())
		b := provideCounted(root, "b", &startWithCtxErr{}, AsStarter())
		c := provideCounted(root, "c", &startWithCtxErr{}, AsStarter())
		d := provideCounted(root, "d", &startWithCtxErr{}, AsStarter(), DependsOnNamed("stuck"))

		begin := time.Now()
		err := root.Start()

		assert.Equal(t, 5*time.Second, time.Since(begin))
		var startErr *StartError
		require.ErrorAs(t, err, &startErr)
		assert.Equal(t, "stuck", startErr.Service)
		assert.ErrorIs(t, err, ErrStartTimeout)
		assert.Equal(t, int32(1), b.starts.Load(), "b started")
		assert.Equal(t, int32(1), c.starts.Load(), "c started")
		assert.Zero(t, d.starts.Load(), "d depends on stuck and is not started")

		stuck.unblock()
		<-stuck.ended
		root.Shutdown()
	})
}

// TestStartTimeout_AbandonedNotRestarted pins container-start "Re-running
// while the abandoned call runs, and after it returns".
func TestStartTimeout_AbandonedNotRestarted(t *testing.T) {
	t.Parallel()

	errBoom := errors.New("boom")
	nilOutcome := func(int32) error { return nil }
	errOnFirst := func(call int32) error {
		if call == 1 {
			return errBoom
		}
		return nil
	}
	panicOnFirst := func(call int32) error {
		if call == 1 {
			panic("boom")
		}
		return nil
	}

	tests := []struct {
		name     string
		runnable bool
		child    bool // "late" in child scope "tenant", run with tenant.Start()
		outcome  func(call int32) error
		released bool // the second run comes after the first call returned
		wantErr  bool
		calls    int32
	}{
		{name: "running", outcome: nilOutcome, wantErr: true, calls: 1},
		{name: "late_nil", outcome: nilOutcome, released: true, calls: 1},
		{name: "late_error", outcome: errOnFirst, released: true, calls: 2},
		{name: "late_panic", outcome: panicOnFirst, released: true, calls: 2},
		{name: "scope_start", child: true, outcome: nilOutcome, wantErr: true, calls: 1},
		{name: "runnable_running", runnable: true, outcome: nilOutcome, wantErr: true, calls: 1},
		{name: "runnable_late_nil", runnable: true, outcome: nilOutcome, released: true, calls: 1},
		{name: "runnable_late_error", runnable: true, outcome: errOnFirst, released: true, calls: 2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			synctest.Test(t, func(t *testing.T) {
				var befores atomic.Int32
				root := NewWithOpts(&InjectorOpts{
					StartTimeout: 5 * time.Second,
					HookBeforeStart: []func(context.Context, *Scope, string){func(_ context.Context, _ *Scope, name string) {
						if name == "late" {
							befores.Add(1)
						}
					}},
				})
				require.NoError(t, root.Start())

				scope := root.self
				operation := root.Refresh
				if tt.child {
					scope = root.Scope("tenant")
					operation = scope.Start
				}
				probe := newStepProbe(0)
				probe.outcome = tt.outcome
				opt := AsStarter()
				if tt.runnable {
					opt = AsRunnable()
				}
				ProvideNamed(scope, "late", func(Injector) (startOnly, error) { return startOnly{probe}, nil }, opt)
				defer func() {
					probe.unblock()
					<-probe.ended
					root.Shutdown()
				}()

				require.ErrorIs(t, operation(), ErrStartTimeout)
				if tt.released {
					probe.unblock()
					<-probe.ended
					synctest.Wait() // the abandoned call's late outcome is handled
				}

				err := operation()

				if tt.wantErr {
					var startErr *StartError
					require.ErrorAs(t, err, &startErr)
					assert.Equal(t, "late", startErr.Service)
					assert.ErrorIs(t, err, ErrStartTimeout)
				} else {
					require.NoError(t, err)
				}
				assert.Equal(t, tt.calls, probe.calls.Load(), "calls of late")
				if !tt.runnable {
					assert.Equal(t, tt.calls, befores.Load(), "HookBeforeStart calls for late: none for a skipped or failed-fast step")
				}
			})
		})
	}
}

// startOnly exposes only the Start and Run methods of a stepProbe, so that
// Shutdown does not release it.
type startOnly struct{ p *stepProbe }

func (s startOnly) Start(ctx context.Context) error { return s.p.call(ctx) }
func (s startOnly) Run(ctx context.Context) error   { return s.p.call(ctx) }

// TestStartTimeout_RefreshSkipsStillRunning pins container-start "Refresh
// starts what does not depend on a still-running step": a covering run
// skips the still-running step and its dependents, starts the rest and
// reports the still-running step.
func TestStartTimeout_RefreshSkipsStillRunning(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		parallelism uint
		runnable    bool // the services are opted in with AsRunnable, not AsStarter
		child       bool // the services live in child scope "tenant"
		viaRoot     bool // with child: the operation is root.Refresh(), not tenant.Start()
	}{
		{name: "sequential/root/refresh"},
		{name: "parallel/root/refresh", parallelism: 4},
		{name: "sequential/tenant/scope_start", child: true},
		{name: "parallel/tenant/scope_start", parallelism: 4, child: true},
		{name: "sequential/tenant/refresh", child: true, viaRoot: true},
		{name: "runnable/root/refresh", runnable: true},
		{name: "runnable/tenant/scope_start", runnable: true, child: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			synctest.Test(t, func(t *testing.T) {
				var depBefores, depAfters atomic.Int32
				root := NewWithOpts(&InjectorOpts{
					StartTimeout:     5 * time.Second,
					StartParallelism: tt.parallelism,
					HookBeforeStart: []func(context.Context, *Scope, string){func(_ context.Context, _ *Scope, name string) {
						if name == "dep" {
							depBefores.Add(1)
						}
					}},
					HookAfterStart: []func(context.Context, *Scope, string, time.Duration, error){func(_ context.Context, _ *Scope, name string, _ time.Duration, _ error) {
						if name == "dep" {
							depAfters.Add(1)
						}
					}},
				})
				require.NoError(t, root.Start())

				scope := root.self
				operation := root.Refresh
				if tt.child {
					scope = root.Scope("tenant")
					if !tt.viaRoot {
						operation = scope.Start
					}
				}
				opt := AsStarter
				if tt.runnable {
					opt = AsRunnable
				}
				provide := func(name string, probe *stepProbe, opts ...ProvideOption) {
					ProvideNamed(scope, name, func(Injector) (startOnly, error) { return startOnly{probe}, nil }, append(opts, opt())...)
				}
				recorded := func(name string) bool {
					wrapper, ok := scope.serviceGet(name)
					require.True(t, ok, "service %q is not registered", name)
					if tt.runnable {
						return root.isHandled(wrapper, runnableKind.id)
					}
					return root.isStarted(wrapper)
				}

				stuck := newStepProbe(0)
				provide("stuck", stuck)
				defer func() {
					stuck.unblock()
					<-stuck.ended
					root.Shutdown()
				}()
				require.ErrorIs(t, operation(), ErrStartTimeout)

				// worker and dep return at once
				worker, dep := newStepProbe(0), newStepProbe(0)
				worker.unblock()
				dep.unblock()
				provide("worker", worker)
				provide("dep", dep, DependsOnNamed("stuck"))

				err := operation()

				var startErr *StartError
				require.ErrorAs(t, err, &startErr)
				assert.Equal(t, "stuck", startErr.Service)
				require.ErrorIs(t, err, ErrStartTimeout)
				assert.Equal(t, int32(1), worker.calls.Load(), "calls of worker")
				assert.True(t, recorded("worker"), "worker is recorded as started")
				assert.Zero(t, dep.calls.Load(), "calls of dep, which depends on stuck")
				assert.False(t, recorded("dep"), "dep is not recorded as started")
				assert.Zero(t, depBefores.Load(), "HookBeforeStart calls for dep, a skipped dependent")
				assert.Zero(t, depAfters.Load(), "HookAfterStart calls for dep, a skipped dependent")
				assert.Equal(t, int32(1), stuck.calls.Load(), "calls of stuck")
			})
		})
	}
}

// TestStartTimeout_FailureWinsOverStillRunning pins container-start "An
// abandoned step is not started again while it runs": when a covering run
// skips a still-running step and another step of the run fails, that
// failure is the run's error.
func TestStartTimeout_FailureWinsOverStillRunning(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		parallelism uint
	}{
		{name: "sequential"},
		{name: "parallel", parallelism: 4},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			synctest.Test(t, func(t *testing.T) {
				root := NewWithOpts(&InjectorOpts{StartTimeout: 5 * time.Second, StartParallelism: tt.parallelism})
				require.NoError(t, root.Start())
				provide := func(name string, probe *stepProbe) {
					ProvideNamed(root, name, func(Injector) (startOnly, error) { return startOnly{probe}, nil }, AsStarter())
				}

				stuck := newStepProbe(0)
				provide("stuck", stuck)
				defer func() {
					stuck.unblock()
					<-stuck.ended
					root.Shutdown()
				}()
				require.ErrorIs(t, root.Refresh(), ErrStartTimeout)

				// failing returns errBoom at once; it is registered after
				// stuck, so the run skips stuck first
				errBoom := errors.New("boom")
				failing := newStepProbe(0)
				failing.outcome = func(int32) error { return errBoom }
				failing.unblock()
				provide("failing", failing)

				err := root.Refresh()

				require.ErrorIs(t, err, errBoom)
				var startErr *StartError
				require.ErrorAs(t, err, &startErr)
				assert.Equal(t, "failing", startErr.Service)
				assert.NotErrorIs(t, err, ErrStartTimeout, "the failure, not the still-running step, is the run's error")
				assert.Equal(t, int32(1), failing.calls.Load(), "calls of failing")
				assert.Equal(t, int32(1), stuck.calls.Load(), "calls of stuck")
			})
		})
	}
}

// TestStartTimeout_RootStartAbandonsThenOnlyShutdown pins container-start
// "After root Start abandons a step, only Shutdown follows": the late nil
// return is recorded, but Start has failed, so Refresh and Scope.Start
// return ErrNotStarted, Start returns ErrAlreadyStarted, and Shutdown is
// the way on.
func TestStartTimeout_RootStartAbandonsThenOnlyShutdown(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		root := NewWithOpts(&InjectorOpts{StartTimeout: 10 * time.Millisecond})
		tenant := root.Scope("tenant")
		srv := provideCounted(root, "srv", newStepProbe(50*time.Millisecond), AsStarter())

		require.ErrorIs(t, root.Start(), ErrStartTimeout)
		time.Sleep(time.Second)
		synctest.Wait() // the abandoned call's late outcome is handled

		assert.Equal(t, int32(1), srv.calls.Load(), "calls of srv")
		assert.True(t, startedNamed(t, root, root.self, "srv"), "the late nil return is recorded")
		assert.ErrorIs(t, root.Refresh(), ErrNotStarted)
		assert.ErrorIs(t, tenant.Start(), ErrNotStarted)
		assert.ErrorIs(t, root.Start(), ErrAlreadyStarted)
		assert.Equal(t, int32(1), srv.calls.Load(), "no later run calls srv")

		report := root.Shutdown()

		assert.True(t, report.Succeed, "report errors: %v", report.Errors)
		assert.Equal(t, int32(1), srv.shutdowns.Load(), "Shutdown shuts srv down")
	})
}

// TestStartTimeout_LateReturnRacesRun pins container-start "A late return
// races a covering run": the abandoned call returns nil while a second
// Refresh is between discovering "late" and starting it.
func TestStartTimeout_LateReturnRacesRun(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		root := NewWithOpts(&InjectorOpts{StartTimeout: 5 * time.Second})
		require.NoError(t, root.Start())
		probe := newStepProbe(0)
		ProvideNamed(root, "late", func(Injector) (startOnly, error) { return startOnly{probe}, nil }, AsStarter())
		require.ErrorIs(t, root.Refresh(), ErrStartTimeout)

		// the gate component's handler runs after the second Refresh
		// discovered "late" and before it starts its Starters
		gate := NewKind[*startWithCtxErr, struct{}]("gate")
		gate.Handle(root, StageBeforeStart, func(context.Context, Component[*startWithCtxErr, struct{}]) error {
			probe.unblock()
			<-probe.ended
			synctest.Wait() // the late outcome is handled
			return nil
		})
		provideCounted(root, "gate", &startWithCtxErr{}, gate.With(struct{}{}))

		err := root.Refresh()

		require.NoError(t, err)
		assert.Equal(t, int32(1), probe.calls.Load(), "late's Start was called exactly once")

		root.Shutdown()
	})
}

// TestStartTimeout_LateReturnAfterScopeShutdown pins container-start "A
// late return after the scope began shutting down is not recorded".
func TestStartTimeout_LateReturnAfterScopeShutdown(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		root := NewWithOpts(&InjectorOpts{StartTimeout: 5 * time.Second})
		require.NoError(t, root.Start())
		tenant := root.Scope("tenant")
		probe := newStepProbe(0)
		ProvideNamed(tenant, "late", func(Injector) (startOnly, error) { return startOnly{probe}, nil }, AsStarter())
		wrapper, ok := tenant.serviceGet("late")
		require.True(t, ok)
		defer func() {
			probe.unblock()
			<-probe.ended
			root.Shutdown()
		}()
		require.ErrorIs(t, tenant.Start(), ErrStartTimeout)

		begin := time.Now()
		tenant.Shutdown()
		assert.Zero(t, time.Since(begin), "tenant.Shutdown does not wait for the abandoned Start call")

		probe.unblock()
		<-probe.ended
		synctest.Wait() // the late outcome is handled

		assert.False(t, root.isStarted(wrapper), "a late return after the scope began shutting down is not recorded")
		root.startedMu.Lock()
		_, abandoned := root.abandoned[wrapper]
		root.startedMu.Unlock()
		assert.False(t, abandoned, "no abandoned entry is left for a removed service")
	})
}

// shutdownGate is a service whose Shutdown signals entered, then blocks
// until release is closed: it holds a shutdown open at a known point.
type shutdownGate struct {
	entered chan struct{}
	release chan struct{}
}

func newShutdownGate() *shutdownGate {
	return &shutdownGate{entered: make(chan struct{}), release: make(chan struct{})}
}

func (g *shutdownGate) Shutdown() error {
	close(g.entered)
	<-g.release
	return nil
}

// unblock releases a blocked Shutdown.
func (g *shutdownGate) unblock() {
	select {
	case <-g.release:
	default:
		close(g.release)
	}
}

// TestStartTimeout_LateReturnDuringShutdown pins container-start "A late
// return during a shutdown is not recorded". The window is held open by
// "gate", a dependent of "srv" whose Shutdown blocks: the shutdown removes
// dependents first, so while "gate"'s Shutdown blocks, "srv" is still
// registered and only the closing state can refuse the late record. The
// gate's entered channel makes the window deterministic, unlike a hook or
// the seam, which run outside the shutdown.
func TestStartTimeout_LateReturnDuringShutdown(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		runnable     bool // "srv" is opted in with AsRunnable, not AsStarter
		child        bool // "srv" and "gate" live in child scope "tenant"
		rootShutdown bool // with child: the shutdown is root.Shutdown(), not tenant.Shutdown()
	}{
		{name: "root_starter"},
		{name: "root_runnable", runnable: true},
		{name: "tenant_own", child: true},
		{name: "tenant_root", child: true, rootShutdown: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			synctest.Test(t, func(t *testing.T) {
				root := NewWithOpts(&InjectorOpts{StartTimeout: 5 * time.Second})
				require.NoError(t, root.Start())

				scope, operation := root.self, root.Refresh
				shutdown := func() { root.Shutdown() }
				if tt.child {
					scope = root.Scope("tenant")
					operation = scope.Start
					if !tt.rootShutdown {
						shutdown = func() { _ = scope.Shutdown() }
					}
				}
				opt := AsStarter
				if tt.runnable {
					opt = AsRunnable
				}

				probe := newStepProbe(0)
				ProvideNamed(scope, "srv", func(Injector) (startOnly, error) { return startOnly{probe}, nil }, opt())
				wrapper, ok := scope.serviceGet("srv")
				require.True(t, ok)
				recorded := func() bool {
					if tt.runnable {
						return root.isHandled(wrapper, runnableKind.id)
					}
					return root.isStarted(wrapper)
				}

				gate := newShutdownGate()
				shutdownDone := make(chan struct{})
				defer func() {
					probe.unblock()
					<-probe.ended
					gate.unblock()
					root.Shutdown()
				}()
				require.ErrorIs(t, operation(), ErrStartTimeout)
				ProvideNamedValue(scope, "gate", gate, DependsOnNamed("srv"))
				_, err := InvokeNamed[*shutdownGate](scope, "gate")
				require.NoError(t, err)

				go func() {
					defer close(shutdownDone)
					shutdown()
				}()
				<-gate.entered

				probe.unblock()
				<-probe.ended
				synctest.Wait() // the late outcome is handled

				assert.False(t, recorded(), "a late return during the shutdown is not recorded")

				gate.unblock()
				<-shutdownDone
			})
		})
	}
}

// TestStartTimeout_LateReturnDuringAncestorShutdown pins that the refusal
// of a late record walks the ancestors of the service's scope: "srv" in
// tenant/deep returns late while tenant.Shutdown() waits for a run that
// starts "blocker" in "tenant", before "deep" is itself marked closing. A
// row of TestStartTimeout_LateReturnDuringShutdown cannot hold this window:
// the shutdown marks "deep" closing before it shuts down deep's services.
func TestStartTimeout_LateReturnDuringAncestorShutdown(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		root := NewWithOpts(&InjectorOpts{StartTimeout: 5 * time.Second})
		require.NoError(t, root.Start())
		tenant := root.Scope("tenant")
		deep := tenant.Scope("deep")
		probe := provideCounted(deep, "srv", newStepProbe(0), AsStarter())
		wrapper := wrapperOf(t, deep, "srv")
		blocker := newStepProbe(0)
		runDone, shutdownDone := make(chan struct{}), make(chan struct{})
		defer func() {
			probe.unblock()
			<-probe.ended
			blocker.unblock()
			<-blocker.ended
			root.Shutdown()
		}()

		require.ErrorIs(t, deep.Start(), ErrStartTimeout)
		provideCounted(tenant, "blocker", blocker, AsStarter())
		go func() {
			defer close(runDone)
			_ = tenant.Start()
		}()
		synctest.Wait()
		require.Equal(t, int32(1), blocker.calls.Load(), "blocker has entered Start")

		go func() {
			defer close(shutdownDone)
			tenant.Shutdown()
		}()
		synctest.Wait() // tenant is closing and waits for the run; deep is not

		probe.unblock()
		<-probe.ended
		synctest.Wait() // the late outcome is handled

		assert.False(t, root.isStarted(wrapper), "a late return during an ancestor's shutdown is not recorded")

		blocker.unblock()
		<-runDone
		<-shutdownDone
	})
}

// TestStart_RemovalBeforeRecord pins container-start "A removal between the
// registration check and the record": the startSeam's beforeRecord runs
// ShutdownNamed inside the window between the check that the root still
// holds "srv" and the record of its start. The inline row is the regression
// proof of the defect that predates StartTimeout.
func TestStart_RemovalBeforeRecord(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		timeout   time.Duration
		abandoned bool // "srv" returns only after Refresh abandoned it
	}{
		{name: "inline", timeout: 0},
		{name: "awaited", timeout: 5 * time.Second},
		{name: "abandoned", timeout: 5 * time.Second, abandoned: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			synctest.Test(t, func(t *testing.T) {
				root := NewWithOpts(&InjectorOpts{StartTimeout: tt.timeout})
				require.NoError(t, root.Start())
				probe := provideCounted(root, "srv", newStepProbe(0), AsStarter())
				if !tt.abandoned {
					probe.unblock() // Start returns at once
				}
				wrapper, ok := root.serviceGet("srv")
				require.True(t, ok)
				defer func() {
					probe.unblock()
					<-probe.ended
					synctest.Wait()
					root.Shutdown()
				}()

				var seamRuns atomic.Int32
				var shutdownErr error
				root.startSeam.beforeRecord = func(step startStep) {
					if step.candidate.name == "srv" && seamRuns.Add(1) == 1 {
						shutdownErr = ShutdownNamed(root, "srv")
					}
				}

				err := root.Refresh()
				if tt.abandoned {
					require.ErrorIs(t, err, ErrStartTimeout)
					probe.unblock()
					<-probe.ended
					synctest.Wait() // the late outcome is handled
				} else {
					require.NoError(t, err)
				}

				require.Equal(t, int32(1), seamRuns.Load(), "the removal ran inside the window")
				require.NoError(t, shutdownErr)
				assert.False(t, root.isStarted(wrapper), "no start record survives for the removed service")
				root.startedMu.Lock()
				_, abandoned := root.abandoned[wrapper]
				root.startedMu.Unlock()
				assert.False(t, abandoned, "no abandoned entry survives for the removed service")
			})
		})
	}
}

// ctxKeeper is a Starter whose Start keeps its context and returns nil.
type ctxKeeper struct {
	ctx atomic.Pointer[context.Context]
}

func (k *ctxKeeper) Start(ctx context.Context) error {
	k.ctx.Store(&ctx)
	return nil
}

// ctxObserver is a Starter whose Start records the Err() of the context
// kept by keeper, and whether that context has a deadline.
type ctxObserver struct {
	keeper      *ctxKeeper
	err         error
	hasDeadline bool
}

func (o *ctxObserver) Start(context.Context) error {
	kept := *o.keeper.ctx.Load()
	o.err = kept.Err()
	_, o.hasDeadline = kept.Deadline()
	return nil
}

// TestStartTimeout_StepContextLifetime pins container-start "A step that
// returns in time keeps its context until the run ends": StartTimeout does
// not shorten the life of the context a step receives.
func TestStartTimeout_StepContextLifetime(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		timeout time.Duration
	}{
		{name: "no_timeout", timeout: 0},
		{name: "timeout", timeout: 5 * time.Second},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			synctest.Test(t, func(t *testing.T) {
				root := NewWithOpts(&InjectorOpts{StartTimeout: tt.timeout})
				a := provideCounted(root, "a", &ctxKeeper{}, AsStarter())
				b := provideCounted(root, "b", &ctxObserver{keeper: a}, AsStarter())

				require.NoError(t, root.Start())

				assert.NoError(t, b.err, "a's context is still live while b starts")
				assert.False(t, b.hasDeadline, "a's context reports no deadline")
				kept := a.ctx.Load()
				require.NotNil(t, kept)
				assert.ErrorIs(t, (*kept).Err(), context.Canceled, "a's context ends with the run")

				root.Shutdown()
			})
		})
	}
}

// TestStartTimeout_TimeoutWinsOverLateCancel pins container-start "A timeout
// stays a timeout when the run's context ends right after it": the
// startSeam's afterStepDone cancels the caller's ctx after the 5s timer
// fired and before the run decides the step's outcome.
func TestStartTimeout_TimeoutWinsOverLateCancel(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		root := NewWithOpts(&InjectorOpts{StartTimeout: 5 * time.Second})
		probe := provideCounted(root, "srv", newStepProbe(0), AsStarter())
		defer func() {
			probe.unblock()
			<-probe.ended
			synctest.Wait()
			root.Shutdown()
		}()

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		root.startSeam.afterStepDone = func(step startStep) {
			if step.candidate.name == "srv" {
				cancel()
			}
		}

		err := root.StartWithContext(ctx)

		require.EqualError(t, err, "doger: failed to start service `srv` in scope `[root]`: start timeout after 5s: context deadline exceeded (start aborted: context canceled)")
		assert.ErrorIs(t, err, ErrStartTimeout)
		assert.ErrorIs(t, err, context.DeadlineExceeded)
		assert.ErrorIs(t, err, context.Canceled)
	})
}

// exactKeeper is a Starter whose Start keeps its context, sleeps d and
// returns nil.
type exactKeeper struct {
	ctxKeeper
	d time.Duration
}

func (k *exactKeeper) Start(ctx context.Context) error {
	k.ctx.Store(&ctx)
	time.Sleep(k.d)
	return nil
}

// TestStartTimeout_ReturnAtTimeout pins container-start "A step that
// returns exactly at the timeout": "a" returns at the very instant its 5s
// timer fires. Run it with -count=200: each run either starts "a" with a
// live context or abandons it, never returns nil with "a"'s context
// cancelled by the timeout.
func TestStartTimeout_ReturnAtTimeout(t *testing.T) {
	t.Parallel()

	for _, where := range []string{"root_start", "root_refresh", "tenant_start"} {
		t.Run(where, func(t *testing.T) {
			t.Parallel()

			synctest.Test(t, func(t *testing.T) {
				root := NewWithOpts(&InjectorOpts{StartTimeout: 5 * time.Second})
				scope, op := root.self, root.Start
				switch where {
				case "root_refresh":
					require.NoError(t, root.Start())
					op = root.Refresh
				case "tenant_start":
					require.NoError(t, root.Start())
					scope = root.Scope("tenant")
					op = scope.Start
				}
				a := provideCounted(scope, "a", &exactKeeper{d: 5 * time.Second}, AsStarter())
				b := provideCounted(scope, "b", &ctxObserver{keeper: &a.ctxKeeper}, AsStarter())

				err := op()
				synctest.Wait() // a late return is handled

				if err == nil {
					assert.NoError(t, b.err, "a step that returned in time keeps a live context")
				} else {
					require.ErrorIs(t, err, ErrStartTimeout)
					assert.True(t, startedNamed(t, root, scope, "a"), "the abandoned step is recorded once its late return was handled")
				}
				root.Shutdown()
			})
		})
	}
}

// isAbandoned reports whether root holds an abandoned entry for wrapper.
func isAbandoned(root *RootScope, wrapper any) bool {
	root.startedMu.Lock()
	defer root.startedMu.Unlock()

	_, abandoned := root.abandoned[wrapper]
	return abandoned
}

// TestChildShutdown_CancelsSubtreeSteps pins container-start "Child
// shutdown cancels the running start steps of its subtree": tenant.Shutdown
// cancels the context of "w"'s running Start with a cause matching
// ErrShutdown, waits for the Start that honours it, and the run succeeds.
// The timeout_5s/root_start row is not in the scenario. The
// root_refresh_parallel row adds Starter "x" to "tenant", so that the run
// takes the parallel path.
func TestChildShutdown_CancelsSubtreeSteps(t *testing.T) {
	t.Parallel()

	runs := []struct {
		name        string
		parallelism uint
		run         func(root *RootScope, tenant *Scope, provideW func()) error
	}{
		{
			name: "root_start",
			run: func(root *RootScope, _ *Scope, provideW func()) error {
				provideW()
				return root.Start()
			},
		},
		{
			name: "root_refresh",
			run: func(root *RootScope, _ *Scope, provideW func()) error {
				if err := root.Start(); err != nil {
					return err
				}
				provideW()
				return root.Refresh()
			},
		},
		{
			name: "tenant_start",
			run: func(root *RootScope, tenant *Scope, provideW func()) error {
				if err := root.Start(); err != nil {
					return err
				}
				provideW()
				return tenant.Start()
			},
		},
		{
			name:        "root_refresh_parallel",
			parallelism: 4,
			run: func(root *RootScope, tenant *Scope, provideW func()) error {
				if err := root.Start(); err != nil {
					return err
				}
				provideW()
				provideCounted(tenant, "x", &sleepStarter{}, AsStarter())
				return root.Refresh()
			},
		},
	}
	timeouts := []struct {
		name    string
		timeout time.Duration
		skip    map[string]bool // the runs not in the scenario for this timeout
	}{
		{name: "timeout_0", timeout: 0},
		{name: "timeout_5s", timeout: 5 * time.Second, skip: map[string]bool{"root_start": true, "root_refresh_parallel": true}},
	}

	for _, tm := range timeouts {
		t.Run(tm.name, func(t *testing.T) {
			t.Parallel()

			for _, tt := range runs {
				if tm.skip[tt.name] {
					continue
				}
				t.Run(tt.name, func(t *testing.T) {
					t.Parallel()

					synctest.Test(t, func(t *testing.T) {
						root := NewWithOpts(&InjectorOpts{StartTimeout: tm.timeout, StartParallelism: tt.parallelism})
						tenant := root.Scope("tenant")
						w := newHonouringStarter(100 * time.Millisecond)
						provideW := func() { provideCounted(tenant, "w", w, AsStarter()) }
						defer func() {
							root.Shutdown()
							<-w.returned
						}()

						runErr := make(chan error, 1)
						go func() { runErr <- tt.run(root, tenant, provideW) }()
						<-w.entered
						wrapper := wrapperOf(t, tenant, "w")

						time.Sleep(100 * time.Millisecond)
						begin := time.Now()
						report := tenant.Shutdown()
						shutdownTook := time.Since(begin)

						assert.ErrorIs(t, w.startCause(), ErrShutdown, "w's Start context ends with a cause matching ErrShutdown")
						require.NotNil(t, report)
						assert.True(t, report.Succeed, "tenant.Shutdown() succeeds: %v", report.Errors)
						assert.Equal(t, 100*time.Millisecond, shutdownTook, "tenant.Shutdown() waits for w's Start cleanup")
						assert.NoError(t, <-runErr, "the cancelled step does not fail the run")
						assert.Equal(t, int32(1), w.shutdowns.Load(), "w's Shutdown method was called exactly once")
						assert.False(t, w.shutdownBeforeClean.Load(), "w's Shutdown method was called after its Start cleanup finished")
						started, handled := tracked(root, wrapper)
						assert.False(t, started || handled, "the start bookkeeping holds no record for w")
						assert.False(t, isAbandoned(root, wrapper), "the start bookkeeping holds no abandoned entry for w")
					})
				})
			}
		})
	}
}

// TestChildShutdown_StuckStepWaitsTimeout pins container-start "Child
// shutdown waits for a stuck step up to the start timeout": "w" ignores its
// cancelled context, so the run abandons it at StartTimeout, does not fail,
// and tenant.Shutdown then releases it through its Shutdown method.
func TestChildShutdown_StuckStepWaitsTimeout(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		root := NewWithOpts(&InjectorOpts{StartTimeout: 5 * time.Second})
		require.NoError(t, root.Start())
		tenant := root.Scope("tenant")
		w := provideCounted(tenant, "w", newStepProbe(0), AsStarter())
		wrapper := wrapperOf(t, tenant, "w")
		defer func() {
			w.unblock()
			<-w.ended
			root.Shutdown()
		}()

		runErr := make(chan error, 1)
		go func() { runErr <- tenant.Start() }()
		synctest.Wait()
		require.Equal(t, int32(1), w.calls.Load(), "w has entered Start")

		time.Sleep(100 * time.Millisecond)
		begin := time.Now()
		report := tenant.Shutdown()
		shutdownTook := time.Since(begin)

		require.NotNil(t, report)
		assert.True(t, report.Succeed, "tenant.Shutdown() succeeds: %v", report.Errors)
		assert.Equal(t, 4900*time.Millisecond, shutdownTook, "tenant.Shutdown() waits for the step up to its StartTimeout")
		assert.NoError(t, <-runErr, "the abandoned step of the closing scope does not fail the run")
		assert.Equal(t, int32(1), w.shutdowns.Load(), "w's Shutdown method was called exactly once")

		<-w.ended
		synctest.Wait() // the late outcome is handled
		started, handled := tracked(root, wrapper)
		assert.False(t, started || handled, "the start bookkeeping holds no record for w")
		assert.False(t, isAbandoned(root, wrapper), "the start bookkeeping holds no abandoned entry for w")
	})
}

// TestChildShutdown_BeginsDuringBeforeStartHook pins the window between the
// run's closing check and the registration of the step's context: a
// tenant.Shutdown that begins in "w"'s HookBeforeStart has already swept the
// registered steps, so "w"'s context is cancelled as it registers.
func TestChildShutdown_BeginsDuringBeforeStartHook(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		root := New()
		require.NoError(t, root.Start())
		tenant := root.Scope("tenant")
		w := provideCounted(tenant, "w", newHonouringStarter(0), AsStarter())
		defer root.Shutdown()

		reports := make(chan *ShutdownReport, 1)
		root.AddBeforeStartHook(func(_ context.Context, _ *Scope, name string) {
			if name == "w" {
				go func() { reports <- tenant.Shutdown() }()
				synctest.Wait() // the shutdown waits for the run
			}
		})

		require.NoError(t, tenant.Start(), "the cancelled step does not fail the run")
		report := <-reports
		assert.True(t, report.Succeed, "tenant.Shutdown() succeeds: %v", report.Errors)
		assert.ErrorIs(t, w.startCause(), ErrShutdown, "w's Start context ends with a cause matching ErrShutdown")
		assert.Equal(t, int32(1), w.shutdowns.Load(), "w's Shutdown method was called exactly once")
		assert.False(t, w.shutdownBeforeClean.Load(), "w's Shutdown method was called after its Start returned")
	})
}

// TestChildShutdown_CancelsDescendantSteps pins that a child scope Shutdown
// cancels the running start steps of its descendant scopes too: the run
// has a candidate in "tenant", so tenant.Shutdown waits for it, while the
// run waits for "w" in grandchild "deep".
func TestChildShutdown_CancelsDescendantSteps(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		root := New()
		require.NoError(t, root.Start())
		tenant := root.Scope("tenant")
		deep := tenant.Scope("deep")
		provideCounted(tenant, "a", &sleepStarter{}, AsStarter())
		w := provideCounted(deep, "w", newHonouringStarter(100*time.Millisecond), AsStarter())
		defer func() {
			root.Shutdown()
			<-w.returned
		}()

		runErr := make(chan error, 1)
		go func() { runErr <- tenant.Start() }()
		<-w.entered

		report := tenant.Shutdown()

		assert.True(t, report.Succeed, "tenant.Shutdown() succeeds: %v", report.Errors)
		assert.ErrorIs(t, w.startCause(), ErrShutdown, "w's Start context ends with a cause matching ErrShutdown")
		assert.NoError(t, <-runErr, "the cancelled step does not fail the run")
		assert.Equal(t, int32(1), w.shutdowns.Load(), "w's Shutdown method was called exactly once")
		assert.False(t, w.shutdownBeforeClean.Load(), "w's Shutdown method was called after its Start cleanup finished")
	})
}

// TestChildShutdown_RunContinuesOutsideSubtree pins that the run skips the
// later steps of a closing subtree and goes on with the other scopes: once
// tenant.Shutdown cancelled "w", the run skips "v" in "tenant" and starts
// "x" in sibling scope "other".
func TestChildShutdown_RunContinuesOutsideSubtree(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		root := New()
		require.NoError(t, root.Start())
		tenant := root.Scope("tenant")
		other := root.Scope("other")
		starts := &countingStarters{}
		w := provideCounted(tenant, "w", newHonouringStarter(100*time.Millisecond), AsStarter())
		provideCountedStarter(tenant, starts, "v")
		provideCountedStarter(other, starts, "x")
		defer func() {
			root.Shutdown()
			<-w.returned
		}()

		runErr := make(chan error, 1)
		go func() { runErr <- root.Refresh() }()
		<-w.entered

		report := tenant.Shutdown()

		assert.True(t, report.Succeed, "tenant.Shutdown() succeeds: %v", report.Errors)
		assert.NoError(t, <-runErr, "the cancelled step does not fail the run")
		assert.Equal(t, 0, starts.count("v"), "the later step of the closing scope is skipped")
		assert.Equal(t, 1, starts.count("x"), "the run goes on with the other scopes")
		assert.True(t, startedNamed(t, root, other, "x"), "x is recorded as started")
	})
}

// TestChildShutdown_SkipsDependents pins container-start "Dependents of a
// step the child shutdown skipped are skipped": redis.Shutdown cancels
// "cache"'s running Start; "dep", which depends on "cache", is not
// started; a "dep" outside the closing subtree (in the root, through the
// export) fails the run, one inside it does not; "worker" starts.
func TestChildShutdown_SkipsDependents(t *testing.T) {
	t.Parallel()

	const (
		exportDependsOn = iota // "dep" in the root, DependsOnNamed the export
		exportInvoked          // "dep" in the root invokes the export in its provider
		insideDependsOn        // "dep" in "redis", DependsOnNamed("cache")
	)
	tests := []struct {
		name        string
		timeout     time.Duration
		parallelism uint
		stuck       bool // "cache" ignores its context until its Shutdown
		dependent   int
		wantErr     error  // nil: the run returns nil
		wantNaming  string // the service the run's *StartError names
	}{
		{name: "export_honours", dependent: exportDependsOn, wantErr: ErrShutdown, wantNaming: "dep"},
		{name: "export_honours_parallel", parallelism: 4, dependent: exportDependsOn, wantErr: ErrShutdown, wantNaming: "dep"},
		{name: "export_invoked_timeout", timeout: 5 * time.Second, dependent: exportInvoked, wantErr: ErrShutdown, wantNaming: "dep"},
		{name: "export_stuck_timeout", timeout: 5 * time.Second, stuck: true, dependent: exportDependsOn, wantErr: ErrStartTimeout, wantNaming: "cache"},
		{name: "inside_honours", dependent: insideDependsOn},
		{name: "inside_stuck_timeout", timeout: 5 * time.Second, stuck: true, dependent: insideDependsOn},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			synctest.Test(t, func(t *testing.T) {
				root := NewWithOpts(&InjectorOpts{StartTimeout: tt.timeout, StartParallelism: tt.parallelism})
				require.NoError(t, root.Start())
				redis := root.Scope("redis")
				starts := &countingStarters{}

				probe := newStepProbe(0)
				honouring := newHonouringStarter(0)
				entered := honouring.entered
				invokeCache := func(i Injector) error {
					_, err := InvokeNamed[*honouringStarter](i, "cache")
					return err
				}
				if tt.stuck {
					provideCounted(redis, "cache", probe, AsStarter())
					require.NoError(t, ExportNamed[*stepProbe](redis, "cache"))
					invokeCache = func(i Injector) error {
						_, err := InvokeNamed[*stepProbe](i, "cache")
						return err
					}
				} else {
					provideCounted(redis, "cache", honouring, AsStarter())
					require.NoError(t, ExportNamed[*honouringStarter](redis, "cache"))
				}
				defer func() {
					probe.unblock()
					root.Shutdown()
				}()
				cacheWrapper := wrapperOf(t, redis, "cache")

				depScope := root.self
				switch tt.dependent {
				case exportDependsOn:
					provideCountedStarter(root, starts, "dep", DependsOnNamed("cache"))
				case exportInvoked:
					ProvideNamed(root, "dep", func(i Injector) (*countedStarter, error) {
						if err := invokeCache(i); err != nil {
							return nil, err
						}
						return &countedStarter{name: "dep", starts: starts}, nil
					}, AsStarter())
				case insideDependsOn:
					depScope = redis
					provideCountedStarter(redis, starts, "dep", DependsOnNamed("cache"))
				}
				depWrapper := wrapperOf(t, depScope, "dep")
				provideCountedStarter(root, starts, "worker")

				runErr := make(chan error, 1)
				go func() { runErr <- root.Refresh() }()
				synctest.Wait()
				if tt.stuck {
					require.Equal(t, int32(1), probe.calls.Load(), "cache has entered Start")
				} else {
					<-entered
				}

				time.Sleep(100 * time.Millisecond)
				report := redis.Shutdown()

				err := <-runErr
				if tt.wantErr == nil {
					assert.NoError(t, err)
				} else {
					require.ErrorIs(t, err, tt.wantErr)
					var startErr *StartError
					require.ErrorAs(t, err, &startErr)
					assert.Equal(t, tt.wantNaming, startErr.Service, "the run's error names %s: %v", tt.wantNaming, err)
				}
				assert.Equal(t, 0, starts.count("dep"), "dep's Start was not called")
				assert.Equal(t, 1, starts.count("worker"), "worker's Start was called once")
				assert.False(t, root.isStarted(cacheWrapper), "cache is not recorded as started")
				assert.False(t, root.isStarted(depWrapper), "dep is not recorded as started")
				assert.True(t, report.Succeed, "redis.Shutdown() succeeds: %v", report.Errors)
			})
		})
	}
}

// TestChildShutdown_FailureWinsOverSkippedDependent pins container-start
// "A failure of the run wins over a skipped dependent".
func TestChildShutdown_FailureWinsOverSkippedDependent(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		root := New()
		require.NoError(t, root.Start())
		redis := root.Scope("redis")
		cache := provideCounted(redis, "cache", newHonouringStarter(0), AsStarter())
		require.NoError(t, ExportNamed[*honouringStarter](redis, "cache"))
		starts := &countingStarters{}
		provideCountedStarter(root, starts, "dep", DependsOnNamed("cache"))
		other := root.Scope("other")
		errBoom := errors.New("boom")
		bad := newStepProbe(0)
		bad.unblock()
		bad.outcome = func(int32) error { return errBoom }
		provideCounted(other, "bad", bad, AsStarter())
		defer root.Shutdown()

		runErr := make(chan error, 1)
		go func() { runErr <- root.Refresh() }()
		<-cache.entered

		time.Sleep(100 * time.Millisecond)
		redis.Shutdown()

		err := <-runErr
		require.ErrorIs(t, err, errBoom)
		assert.NotErrorIs(t, err, ErrShutdown)
		var startErr *StartError
		require.ErrorAs(t, err, &startErr)
		assert.Equal(t, "bad", startErr.Service)
		assert.Equal(t, 0, starts.count("dep"), "dep's Start was not called")
	})
}

// TestChildShutdown_RegistryEmpty pins that the registry of running start
// step contexts holds nothing once a run returned, whatever its step did.
func TestChildShutdown_RegistryEmpty(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		timeout time.Duration
		outcome string // returned, panicked, abandoned or cancelled
	}{
		{name: "returned", outcome: "returned"},
		{name: "panicked", outcome: "panicked"},
		{name: "abandoned", timeout: 5 * time.Second, outcome: "abandoned"},
		{name: "cancelled", outcome: "cancelled"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			synctest.Test(t, func(t *testing.T) {
				root := NewWithOpts(&InjectorOpts{StartTimeout: tt.timeout})
				require.NoError(t, root.Start())
				tenant := root.Scope("tenant")
				probe := newStepProbe(0)
				defer func() {
					probe.unblock()
					if probe.calls.Load() > 0 {
						<-probe.ended
					}
					root.Shutdown()
				}()

				var err error
				switch tt.outcome {
				case "returned":
					probe.unblock()
					provideCounted(tenant, "w", probe, AsStarter())
					err = tenant.Start()
					require.NoError(t, err)
				case "panicked":
					probe.unblock()
					probe.outcome = func(int32) error { panic("boom") }
					provideCounted(tenant, "w", probe, AsStarter())
					err = tenant.Start()
					require.Error(t, err)
				case "abandoned":
					provideCounted(tenant, "w", probe, AsStarter())
					err = tenant.Start()
					require.ErrorIs(t, err, ErrStartTimeout)
				case "cancelled":
					w := provideCounted(tenant, "w", newHonouringStarter(0), AsStarter())
					runErr := make(chan error, 1)
					go func() { runErr <- tenant.Start() }()
					<-w.entered
					tenant.Shutdown()
					require.NoError(t, <-runErr)
				}

				root.lifecycleMu.Lock()
				registered := len(root.runSteps)
				root.lifecycleMu.Unlock()
				assert.Zero(t, registered, "no step context stays registered once the run returned")
			})
		})
	}
}

// TestChildShutdown_SkipsLateDependent pins that the dependents check reads
// the dependency graph as it is when it runs: an edge from root "server" to
// "cache", an export of "redis", declared by a StageBeforeStart handler
// after redis.Shutdown() made the run's first skip (it cancelled "blk"'s
// handler), still skips "server" and fails the run with ErrShutdown. The
// declared_before row is the control.
func TestChildShutdown_SkipsLateDependent(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name        string
		declareLate bool
	}{
		{name: "declared_before"},
		{name: "late_edge", declareLate: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			synctest.Test(t, func(t *testing.T) {
				root := New()
				require.NoError(t, root.Start())
				redis := root.Scope("redis")
				starts := &countingStarters{}

				entered := make(chan struct{})
				block := NewKind[Job, Spec]("block")
				ProvideNamed(redis, "blk", func(Injector) (*CleanupJob, error) { return &CleanupJob{name: "blk"}, nil }, block.With(Spec{}))
				block.Handle(root, StageBeforeStart, func(ctx context.Context, _ Component[Job, Spec]) error {
					close(entered)
					<-ctx.Done()
					return ctx.Err()
				})

				provideCountedStarter(redis, starts, "cache")
				require.NoError(t, ExportNamed[*countedStarter](redis, "cache"))
				provideCountedStarter(root, starts, "server")
				_, err := InvokeNamed[*countedStarter](root, "cache") // builds the export
				require.NoError(t, err)

				declare := NewKind[Job, Spec]("declare")
				ProvideNamed(root, "decl", func(Injector) (*CleanupJob, error) { return &CleanupJob{name: "decl"}, nil }, declare.With(Spec{}))
				declare.Handle(root, StageBeforeStart, func(context.Context, Component[Job, Spec]) error {
					if tt.declareLate {
						return DeclareDependency(root, "server", "cache")
					}
					return nil
				})
				if !tt.declareLate {
					require.NoError(t, DeclareDependency(root, "server", "cache"))
				}
				defer root.Shutdown()

				runErr := make(chan error, 1)
				go func() { runErr <- root.Refresh() }()
				<-entered
				report := redis.Shutdown()

				err = <-runErr
				assert.True(t, report.Succeed, "redis.Shutdown() succeeds: %v", report.Errors)
				assert.Equal(t, 0, starts.count("cache"), "cache is not started: its scope is closing")
				assert.Equal(t, 0, starts.count("server"), "server depends on cache: it is skipped")
				require.ErrorIs(t, err, ErrShutdown, "the run reports the skipped dependent")
				var startErr *StartError
				require.ErrorAs(t, err, &startErr)
				assert.Equal(t, "server", startErr.Service)
			})
		})
	}
}

// TestChildShutdown_StillRunningStepOfClosingScope pins that a step whose
// abandoned call from an earlier run still runs, in a scope that begins
// shutting down during the run before the step is claimed, keeps its own
// error: root "dep", which depends on "cache" (an export of "redis"), is
// skipped and the run fails with "cache"'s *StartError matching
// ErrStartTimeout, not with ErrShutdown naming "dep".
func TestChildShutdown_StillRunningStepOfClosingScope(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		root := NewWithOpts(&InjectorOpts{StartTimeout: 5 * time.Second})
		require.NoError(t, root.Start())
		redis := root.Scope("redis")
		starts := &countingStarters{}
		provideCountedStarter(root, starts, "gate")
		cache := provideCounted(redis, "cache", newStepProbe(0), AsStarter(), DependsOnNamed("gate"))
		require.NoError(t, ExportNamed[*stepProbe](redis, "cache"))
		defer func() {
			cache.unblock()
			<-cache.ended
			root.Shutdown()
		}()

		require.ErrorIs(t, root.Refresh(), ErrStartTimeout, "the first run abandons cache")
		require.Equal(t, int32(1), cache.calls.Load())

		provideCountedStarter(root, starts, "gate2")
		provideCountedStarter(root, starts, "dep", DependsOnNamed("cache"))
		reports := make(chan *ShutdownReport, 1)
		root.AddBeforeStartHook(func(_ context.Context, _ *Scope, name string) {
			if name == "gate2" {
				go func() { reports <- redis.Shutdown() }()
				synctest.Wait() // redis is closing and waits for the run
			}
		})

		err := root.Refresh() // cache's abandoned call still runs
		report := <-reports

		assert.True(t, report.Succeed, "redis.Shutdown() succeeds: %v", report.Errors)
		assert.Equal(t, int32(1), cache.calls.Load(), "cache's Start is not called again")
		assert.Equal(t, 0, starts.count("dep"), "dep is skipped")
		require.ErrorIs(t, err, ErrStartTimeout, "the run fails with the still-running step's error")
		var startErr *StartError
		require.ErrorAs(t, err, &startErr)
		assert.Equal(t, "cache", startErr.Service)
	})
}

// TestChildShutdown_SkippedDependencyErrorIsStable pins which error a run
// returns for a dependent that reaches several skipped services of a
// closing scope: a still-running one wins over a closing-only one, and
// among the same reason the first by scope, then service name. Each row
// repeats the run 50 times and expects the same error every time.
func TestChildShutdown_SkippedDependencyErrorIsStable(t *testing.T) {
	t.Parallel()

	// mixedRun: "dep" in the root declares edges to the exports of "redis"
	// "cache" and "cacheC", whose abandoned calls still run, and "cacheB"
	// and "cacheD", skipped only because redis begins shutting down in
	// gate2's HookBeforeStart.
	mixedRun := func(t *testing.T) error {
		t.Helper()
		root := NewWithOpts(&InjectorOpts{StartTimeout: 5 * time.Second})
		require.NoError(t, root.Start())
		redis := root.Scope("redis")
		starts := &countingStarters{}
		provideCountedStarter(root, starts, "gate")
		cache := provideCounted(redis, "cache", newStepProbe(0), AsStarter(), DependsOnNamed("gate"))
		require.NoError(t, ExportNamed[*stepProbe](redis, "cache"))
		cacheC := newStepProbe(0)
		defer func() {
			cache.unblock()
			cacheC.unblock()
			<-cache.ended
			<-cacheC.ended
			root.Shutdown()
		}()
		require.ErrorIs(t, root.Refresh(), ErrStartTimeout, "a first run abandons cache")
		provideCounted(redis, "cacheC", cacheC, AsStarter())
		require.NoError(t, ExportNamed[*stepProbe](redis, "cacheC"))
		require.ErrorIs(t, root.Refresh(), ErrStartTimeout, "a second run abandons cacheC")

		for _, name := range []string{"cacheB", "cacheD"} {
			provideCountedStarter(redis, starts, name)
			require.NoError(t, ExportNamed[*countedStarter](redis, name))
		}
		provideCountedStarter(root, starts, "gate2")
		provideCountedStarter(root, starts, "dep")
		for _, name := range []string{"cache", "cacheC"} {
			_, err := InvokeNamed[*stepProbe](root, name) // builds the export
			require.NoError(t, err)
		}
		for _, name := range []string{"cacheB", "cacheD"} {
			_, err := InvokeNamed[*countedStarter](root, name)
			require.NoError(t, err)
		}
		for _, name := range []string{"cache", "cacheB", "cacheC", "cacheD"} {
			require.NoError(t, DeclareDependency(root, "dep", name))
		}
		reports := make(chan *ShutdownReport, 1)
		root.AddBeforeStartHook(func(_ context.Context, _ *Scope, name string) {
			if name == "gate2" {
				go func() { reports <- redis.Shutdown() }()
				synctest.Wait() // redis is closing and waits for the run
			}
		})

		err := root.Refresh()
		<-reports
		assert.Equal(t, 0, starts.count("dep"), "dep is skipped")
		return err
	}
	// closingRun: "server" in the root declares edges to the exports
	// "cacheD", "cacheB", "cacheC" and "cacheA" of "redis", all skipped only
	// because redis.Shutdown() cancelled "blk"'s handler first.
	closingRun := func(t *testing.T) error {
		t.Helper()
		root := New()
		require.NoError(t, root.Start())
		redis := root.Scope("redis")
		starts := &countingStarters{}
		entered := make(chan struct{})
		block := NewKind[Job, Spec]("block")
		ProvideNamed(redis, "blk", func(Injector) (*CleanupJob, error) { return &CleanupJob{name: "blk"}, nil }, block.With(Spec{}))
		block.Handle(root, StageBeforeStart, func(ctx context.Context, _ Component[Job, Spec]) error {
			close(entered)
			<-ctx.Done()
			return ctx.Err()
		})
		names := []string{"cacheD", "cacheB", "cacheC", "cacheA"}
		for _, name := range names {
			provideCountedStarter(redis, starts, name)
			require.NoError(t, ExportNamed[*countedStarter](redis, name))
		}
		provideCountedStarter(root, starts, "server")
		for _, name := range names {
			require.NoError(t, DeclareDependency(root, "server", name))
			_, err := InvokeNamed[*countedStarter](root, name) // builds the export
			require.NoError(t, err)
		}
		defer root.Shutdown()

		runErr := make(chan error, 1)
		go func() { runErr <- root.Refresh() }()
		<-entered
		redis.Shutdown()
		err := <-runErr
		assert.Equal(t, 0, starts.count("server"), "server is skipped")
		return err
	}

	tests := []struct {
		name        string
		run         func(t *testing.T) error
		wantErr     error
		wantService string // the service the run's *StartError names
		wantText    string // in the error's message
	}{
		{name: "mixed", run: mixedRun, wantErr: ErrStartTimeout, wantService: "cache", wantText: "start timeout"},
		{name: "closing_only", run: closingRun, wantErr: ErrShutdown, wantService: "server", wantText: "dependency `cacheA` was not started"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			seen := map[string]int{}
			for range 50 {
				synctest.Test(t, func(t *testing.T) {
					err := tt.run(t)
					require.ErrorIs(t, err, tt.wantErr)
					var startErr *StartError
					require.ErrorAs(t, err, &startErr)
					assert.Equal(t, tt.wantService, startErr.Service)
					assert.Contains(t, err.Error(), tt.wantText)
					seen[err.Error()]++
				})
			}
			assert.Len(t, seen, 1, "the run returns the same error every time: %v", seen)
		})
	}
}

// TestChildShutdown_RecordedBeforeClosingClaim pins that a step recorded as
// started between discovery and its claim is not reported as skipped when
// its scope begins closing before the claim: run 1 abandons "cache", its
// late call returns nil during run 2 and is recorded, then redis begins
// shutting down; "dep", which depends on "cache", starts and the run
// returns nil, as when "cache" started normally in run 1 (the control).
func TestChildShutdown_RecordedBeforeClosingClaim(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name      string
		abandoned bool // run 1 abandons cache; its call ends during run 2
	}{
		{name: "started_in_run1"},
		{name: "recorded_mid_run2", abandoned: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			synctest.Test(t, func(t *testing.T) {
				root := NewWithOpts(&InjectorOpts{StartTimeout: 5 * time.Second})
				require.NoError(t, root.Start())
				redis := root.Scope("redis")
				starts := &countingStarters{}
				provideCountedStarter(root, starts, "gate")
				probe := newStepProbe(0)
				if !tt.abandoned {
					probe.unblock()
				}
				cache := provideCounted(redis, "cache", probe, AsStarter(), DependsOnNamed("gate"))
				require.NoError(t, ExportNamed[*stepProbe](redis, "cache"))
				defer func() {
					cache.unblock()
					<-cache.ended
					root.Shutdown()
				}()
				err := root.Refresh()
				if tt.abandoned {
					require.ErrorIs(t, err, ErrStartTimeout)
				} else {
					require.NoError(t, err)
				}

				provideCountedStarter(root, starts, "gate2")
				provideCountedStarter(root, starts, "dep", DependsOnNamed("cache"))
				reports := make(chan *ShutdownReport, 1)
				root.AddBeforeStartHook(func(_ context.Context, _ *Scope, name string) {
					if name == "gate2" {
						if tt.abandoned {
							cache.unblock()
							<-cache.ended
							synctest.Wait() // the late outcome records cache
							require.True(t, startedNamed(t, root, redis, "cache"), "cache is recorded before redis closes")
						}
						go func() { reports <- redis.Shutdown() }()
						synctest.Wait() // redis is closing and waits for the run
					}
				})

				err = root.Refresh()
				<-reports
				assert.NoError(t, err, "cache is started: dep's dependency is met")
				assert.Equal(t, 1, starts.count("dep"), "dep is started")
			})
		})
	}
}
