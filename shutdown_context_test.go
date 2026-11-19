package doger

import (
	"context"
	"io"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
)

// stuckShutdowner blocks in Shutdown until release is closed, ignoring its
// context, in one of the four Shutdowner forms.
type (
	stuckPlain     struct{ release chan struct{} }
	stuckErr       struct{ release chan struct{} }
	stuckCtx       struct{ release chan struct{} }
	stuckCtxErr    struct{ release chan struct{} }
	afterShutdowns struct {
		mu    sync.Mutex
		calls map[string][]error
	}
)

func (s *stuckPlain) Shutdown()                       { <-s.release }
func (s *stuckErr) Shutdown() error                   { <-s.release; return nil }
func (s *stuckCtx) Shutdown(context.Context)          { <-s.release }
func (s *stuckCtxErr) Shutdown(context.Context) error { <-s.release; return nil }
func (r *afterShutdowns) hook(_ *Scope, name string, err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.calls == nil {
		r.calls = map[string][]error{}
	}
	r.calls[name] = append(r.calls[name], err)
}

func (r *afterShutdowns) of(name string) []error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]error(nil), r.calls[name]...)
}

func TestShutdownContext_HungShutdownBounded(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		inTenant bool
		viaRoot  bool
		stuck    func(release chan struct{}) any
	}{
		{name: "root_plain", stuck: func(r chan struct{}) any { return &stuckPlain{r} }},
		{name: "root_error", stuck: func(r chan struct{}) any { return &stuckErr{r} }},
		{name: "root_ctx", stuck: func(r chan struct{}) any { return &stuckCtx{r} }},
		{name: "root_ctx_error", stuck: func(r chan struct{}) any { return &stuckCtxErr{r} }},
		{name: "tenant", inTenant: true, stuck: func(r chan struct{}) any { return &stuckErr{r} }},
		{name: "tenant_via_root", inTenant: true, viaRoot: true, stuck: func(r chan struct{}) any { return &stuckErr{r} }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			synctest.Test(t, func(t *testing.T) {
				is := assert.New(t)

				hooks := &afterShutdowns{}
				root := NewWithOpts(&InjectorOpts{HookAfterShutdown: []func(*Scope, string, error){hooks.hook}})
				tenant := root.Scope("tenant")
				scope := root.self
				if tt.inTenant {
					scope = tenant
				}
				release := make(chan struct{})
				stuck := tt.stuck(release)
				ProvideNamedValue(scope, "stuck", stuck)
				ok := &shutdownTestCounter{}
				provideBuilt(t, scope, "ok", ok)
				ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
				defer cancel()

				start := time.Now()
				var report *ShutdownReport
				if tt.inTenant && !tt.viaRoot {
					report = tenant.ShutdownWithContext(ctx)
				} else {
					report = root.ShutdownWithContext(ctx)
				}

				is.Equal(50*time.Millisecond, time.Since(start), "returned at the deadline")
				is.False(report.Succeed)
				var serviceErr *ServiceError
				if is.ErrorAs(report.Err(), &serviceErr) {
					is.Equal("stuck", serviceErr.Service)
					is.Equal(scope.Name(), serviceErr.Scope)
					is.ErrorIs(serviceErr, context.DeadlineExceeded)
				}
				is.Equal(int32(1), ok.calls.Load(), "Shutdown calls of ok")
				is.NoError(reportError(report, scope, "ok"))
				is.Empty(hooks.of("stuck"), "after hooks of stuck before release")

				close(release)
				synctest.Wait()
				is.Equal([]error{nil}, hooks.of("stuck"), "after hooks of stuck once released")
			})
		})
	}
}

// sleepingShutdown sleeps for d in Shutdown.
type sleepingShutdown struct{ d time.Duration }

func (s *sleepingShutdown) Shutdown() error {
	time.Sleep(s.d)
	return nil
}

func TestShutdownContext_SlowShutdownWaited(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		sleep time.Duration
		ctx   func() (context.Context, context.CancelFunc)
	}{
		{name: "background", sleep: 2 * time.Second, ctx: func() (context.Context, context.CancelFunc) { return context.Background(), func() {} }},
		{name: "timeout_3s", sleep: 2 * time.Second, ctx: func() (context.Context, context.CancelFunc) {
			return context.WithTimeout(context.Background(), 3*time.Second)
		}},
		{name: "timeout_1ns_not_expired", ctx: func() (context.Context, context.CancelFunc) {
			return context.WithTimeout(context.Background(), time.Nanosecond)
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			synctest.Test(t, func(t *testing.T) {
				is := assert.New(t)

				root := New()
				provideBuilt(t, root, "slow", &sleepingShutdown{d: tt.sleep})
				ctx, cancel := tt.ctx()
				defer cancel()

				start := time.Now()
				report := root.ShutdownWithContext(ctx)

				is.Equal(tt.sleep, time.Since(start))
				is.NoError(report.Err())
			})
		})
	}
}

// shutdownContextConn counts its Shutdown calls.
type shutdownContextConn struct{ calls *atomic.Int32 }

func (c *shutdownContextConn) Shutdown() error {
	c.calls.Add(1)
	return nil
}

func TestShutdownContext_InFlightBuildBounded(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		fails  bool
		gErr   error
		closes int32
	}{
		{name: "late_instance", gErr: ErrShutdown, closes: 1},
		{name: "late_failure", fails: true, gErr: io.EOF, closes: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			synctest.Test(t, func(t *testing.T) {
				is := assert.New(t)

				root := New()
				release := make(chan struct{})
				var closes atomic.Int32
				ProvideNamed(root, "conn", func(Injector) (*shutdownContextConn, error) {
					<-release
					if tt.fails {
						return nil, io.EOF
					}
					return &shutdownContextConn{calls: &closes}, nil
				})
				type result struct {
					conn *shutdownContextConn
					err  error
				}
				g := make(chan result, 1)
				go func() {
					conn, err := InvokeNamed[*shutdownContextConn](root, "conn")
					g <- result{conn, err}
				}()
				synctest.Wait() // G is blocked in the provider
				ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
				defer cancel()

				start := time.Now()
				report := root.ShutdownWithContext(ctx)

				is.Equal(50*time.Millisecond, time.Since(start), "returned at the deadline")
				is.ErrorIs(reportError(report, root.self, "conn"), context.DeadlineExceeded)

				close(release)
				got := <-g
				synctest.Wait()
				is.Nil(got.conn)
				is.ErrorIs(got.err, tt.gErr)
				is.Equal(tt.closes, closes.Load(), "Shutdown calls of the late instance")
			})
		})
	}
}

func TestShutdownContext_InFlightBuildWaited(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		is := assert.New(t)

		root := New()
		var closes atomic.Int32
		ProvideNamed(root, "conn", func(Injector) (*shutdownContextConn, error) {
			time.Sleep(time.Second)
			return &shutdownContextConn{calls: &closes}, nil
		})
		g := make(chan error, 1)
		go func() {
			_, err := InvokeNamed[*shutdownContextConn](root, "conn")
			g <- err
		}()
		synctest.Wait()

		start := time.Now()
		report := root.ShutdownWithContext(context.Background())

		is.Equal(time.Second, time.Since(start))
		is.NoError(report.Err())
		if err := <-g; err != nil {
			is.ErrorIs(err, ErrShutdown)
		}
		is.Equal(int32(1), closes.Load(), "Shutdown calls of the instance")
	})
}

// blockingPool blocks in Shutdown until release is closed, counting calls.
type blockingPool struct {
	release chan struct{}
	calls   atomic.Int32
}

func (p *blockingPool) Shutdown() error {
	p.calls.Add(1)
	<-p.release
	return nil
}

func TestShutdownContext_SharedEagerBounded(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		is := assert.New(t)

		pool := &blockingPool{release: make(chan struct{})}
		root := New()
		ProvideNamedValue(root, "pool", pool)
		clone := root.Clone()
		go root.Shutdown() // runs the shared Shutdown, blocked
		synctest.Wait()
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
		defer cancel()

		start := time.Now()
		report := clone.ShutdownWithContext(ctx)

		is.Equal(30*time.Millisecond, time.Since(start), "the clone returned at its deadline")
		is.ErrorIs(reportError(report, clone.self, "pool"), context.DeadlineExceeded)

		close(pool.release)
		synctest.Wait()
		is.Equal(int32(1), pool.calls.Load(), "Shutdown calls of the shared value")
	})
}

// startedConn is a Starter built by a provider that blocks.
type startedConn struct{ closes *atomic.Int32 }

func (c *startedConn) Start(context.Context) error { return nil }

func (c *startedConn) Shutdown() error {
	c.closes.Add(1)
	return nil
}

func TestShutdownContext_LateBuildDuringStart(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		is := assert.New(t)

		root := New()
		release := make(chan struct{})
		var closes atomic.Int32
		ProvideNamed(root, "conn", func(Injector) (*startedConn, error) {
			<-release
			return &startedConn{closes: &closes}, nil
		}, AsStarter())
		started := make(chan error, 1)
		go func() { started <- root.Start() }()
		synctest.Wait() // Start is blocked in the provider
		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		defer cancel()

		start := time.Now()
		report := root.ShutdownWithContext(ctx)

		is.Equal(50*time.Millisecond, time.Since(start), "returned at the deadline")
		is.False(report.Succeed)

		close(release)
		err := <-started
		synctest.Wait()
		is.ErrorIs(err, ErrShutdown)
		is.Equal(int32(1), closes.Load(), "the late instance is shut down once")
	})
}
