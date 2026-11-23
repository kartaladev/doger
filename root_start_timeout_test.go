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
// then cleans up for cleanup and returns ctx.Err(). Its Shutdown records
// whether that cleanup had finished.
type honouringStarter struct {
	cleanup time.Duration

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
	time.Sleep(s.cleanup)
	s.cleanupDone.Store(true)

	return ctx.Err()
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
