package doger

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// hookedService implements Start(ctx) error and Shutdown(ctx) error,
// running optional callbacks.
type hookedService struct {
	start    func(ctx context.Context) error
	shutdown func(ctx context.Context) error
}

func (s *hookedService) Start(ctx context.Context) error {
	if s.start != nil {
		return s.start(ctx)
	}
	return nil
}

func (s *hookedService) Shutdown(ctx context.Context) error {
	if s.shutdown != nil {
		return s.shutdown(ctx)
	}
	return nil
}

// hookCall is what a per-service hook received.
type hookCall struct {
	ctx     context.Context
	scope   string
	service string
	elapsed time.Duration
	err     error
}

// hookCalls records hook calls.
type hookCalls struct {
	mu    sync.Mutex
	calls []hookCall
}

func (r *hookCalls) add(c hookCall) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, c)
}

func (r *hookCalls) of(service string) []hookCall {
	r.mu.Lock()
	defer r.mu.Unlock()
	var found []hookCall
	for _, c := range r.calls {
		if c.service == service {
			found = append(found, c)
		}
	}
	return found
}

// hookRecorders returns hooks of the five D9 signatures recording into r.
func hookRecorders(r *hookCalls) (
	before func(context.Context, *Scope, string),
	after func(context.Context, *Scope, string, time.Duration, error),
	invocation func(*Scope, string, time.Duration, error),
) {
	before = func(ctx context.Context, scope *Scope, name string) {
		r.add(hookCall{ctx: ctx, scope: scope.Name(), service: name})
	}
	after = func(ctx context.Context, scope *Scope, name string, elapsed time.Duration, err error) {
		r.add(hookCall{ctx: ctx, scope: scope.Name(), service: name, elapsed: elapsed, err: err})
	}
	invocation = func(scope *Scope, name string, elapsed time.Duration, err error) {
		r.add(hookCall{scope: scope.Name(), service: name, elapsed: elapsed, err: err})
	}
	return before, after, invocation
}

// hookOperation runs an operation on a root holding "svc".
type hookOperation func(t *testing.T, root *RootScope)

func startOp(t *testing.T, root *RootScope) { t.Helper(); require.NoError(t, root.Start()) }

func shutdownOp(t *testing.T, root *RootScope) {
	t.Helper()
	_, err := InvokeNamed[*hookedService](root, "svc")
	require.NoError(t, err)
	require.NoError(t, root.Shutdown().Err())
}

func invokeOp(t *testing.T, root *RootScope) {
	t.Helper()
	_, err := InvokeNamed[*hookedService](root, "svc")
	require.NoError(t, err)
}

func TestServiceHooks_Arguments(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		opts    func(r *hookCalls) *InjectorOpts
		op      hookOperation
		hasCtx  bool
		hasTime bool
	}{
		{name: "before_start", op: startOp, hasCtx: true, opts: func(r *hookCalls) *InjectorOpts {
			before, _, _ := hookRecorders(r)
			return &InjectorOpts{HookBeforeStart: []func(context.Context, *Scope, string){before}}
		}},
		{name: "after_start", op: startOp, hasCtx: true, hasTime: true, opts: func(r *hookCalls) *InjectorOpts {
			_, after, _ := hookRecorders(r)
			return &InjectorOpts{HookAfterStart: []func(context.Context, *Scope, string, time.Duration, error){after}}
		}},
		{name: "before_shutdown", op: shutdownOp, hasCtx: true, opts: func(r *hookCalls) *InjectorOpts {
			before, _, _ := hookRecorders(r)
			return &InjectorOpts{HookBeforeShutdown: []func(context.Context, *Scope, string){before}}
		}},
		{name: "after_shutdown", op: shutdownOp, hasCtx: true, hasTime: true, opts: func(r *hookCalls) *InjectorOpts {
			_, after, _ := hookRecorders(r)
			return &InjectorOpts{HookAfterShutdown: []func(context.Context, *Scope, string, time.Duration, error){after}}
		}},
		{name: "after_invocation", op: invokeOp, hasTime: true, opts: func(r *hookCalls) *InjectorOpts {
			_, _, invocation := hookRecorders(r)
			return &InjectorOpts{HookAfterInvocation: []func(*Scope, string, time.Duration, error){invocation}}
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			testWithTimeout(t, 5*time.Second)
			is := assert.New(t)

			r := &hookCalls{}
			root := NewWithOpts(tt.opts(r))
			ProvideNamed(root, "svc", func(Injector) (*hookedService, error) { return &hookedService{}, nil }, AsStarter())

			tt.op(t, root)

			calls := r.of("svc")
			if is.Len(calls, 1) {
				is.Equal("[root]", calls[0].scope)
				if tt.hasCtx {
					is.NotNil(calls[0].ctx)
				}
				if tt.hasTime {
					is.GreaterOrEqual(calls[0].elapsed, time.Duration(0))
				}
				is.NoError(calls[0].err)
			}
		})
	}
}

func TestServiceHooks_AddMethods(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		add  func(root *RootScope, r *hookCalls)
		op   hookOperation
	}{
		{name: "before_start", op: startOp, add: func(root *RootScope, r *hookCalls) {
			before, _, _ := hookRecorders(r)
			root.AddBeforeStartHook(before)
		}},
		{name: "after_start", op: startOp, add: func(root *RootScope, r *hookCalls) {
			_, after, _ := hookRecorders(r)
			root.AddAfterStartHook(after)
		}},
		{name: "before_shutdown", op: shutdownOp, add: func(root *RootScope, r *hookCalls) {
			before, _, _ := hookRecorders(r)
			root.AddBeforeShutdownHook(before)
		}},
		{name: "after_shutdown", op: shutdownOp, add: func(root *RootScope, r *hookCalls) {
			_, after, _ := hookRecorders(r)
			root.AddAfterShutdownHook(after)
		}},
		{name: "after_invocation", op: invokeOp, add: func(root *RootScope, r *hookCalls) {
			_, _, invocation := hookRecorders(r)
			root.AddAfterInvocationHook(invocation)
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			testWithTimeout(t, 5*time.Second)

			r := &hookCalls{}
			root := New()
			tt.add(root, r)
			ProvideNamed(root, "svc", func(Injector) (*hookedService, error) { return &hookedService{}, nil }, AsStarter())

			tt.op(t, root)

			calls := r.of("svc")
			if assert.Len(t, calls, 1) {
				assert.Equal(t, "[root]", calls[0].scope)
			}
		})
	}
}

type serviceHooksTraceKey struct{}

func TestServiceHooks_Context(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		opts func(r *hookCalls) *InjectorOpts
		op   func(t *testing.T, root *RootScope, ctx context.Context)
	}{
		{name: "before_start", opts: func(r *hookCalls) *InjectorOpts {
			before, _, _ := hookRecorders(r)
			return &InjectorOpts{HookBeforeStart: []func(context.Context, *Scope, string){before}}
		}, op: func(t *testing.T, root *RootScope, ctx context.Context) {
			t.Helper()
			require.NoError(t, root.StartWithContext(ctx))
		}},
		{name: "after_start", opts: func(r *hookCalls) *InjectorOpts {
			_, after, _ := hookRecorders(r)
			return &InjectorOpts{HookAfterStart: []func(context.Context, *Scope, string, time.Duration, error){after}}
		}, op: func(t *testing.T, root *RootScope, ctx context.Context) {
			t.Helper()
			require.NoError(t, root.StartWithContext(ctx))
		}},
		{name: "before_shutdown", opts: func(r *hookCalls) *InjectorOpts {
			before, _, _ := hookRecorders(r)
			return &InjectorOpts{HookBeforeShutdown: []func(context.Context, *Scope, string){before}}
		}, op: func(t *testing.T, root *RootScope, ctx context.Context) {
			t.Helper()
			invokeOp(t, root)
			require.NoError(t, root.ShutdownWithContext(ctx).Err())
		}},
		{name: "after_shutdown", opts: func(r *hookCalls) *InjectorOpts {
			_, after, _ := hookRecorders(r)
			return &InjectorOpts{HookAfterShutdown: []func(context.Context, *Scope, string, time.Duration, error){after}}
		}, op: func(t *testing.T, root *RootScope, ctx context.Context) {
			t.Helper()
			invokeOp(t, root)
			require.NoError(t, root.ShutdownWithContext(ctx).Err())
		}},
		{name: "before_shutdown_named", opts: func(r *hookCalls) *InjectorOpts {
			before, _, _ := hookRecorders(r)
			return &InjectorOpts{HookBeforeShutdown: []func(context.Context, *Scope, string){before}}
		}, op: func(t *testing.T, root *RootScope, ctx context.Context) {
			t.Helper()
			invokeOp(t, root)
			require.NoError(t, ShutdownNamedWithContext(ctx, root, "svc"))
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			testWithTimeout(t, 5*time.Second)
			is := assert.New(t)

			r := &hookCalls{}
			root := NewWithOpts(tt.opts(r))
			ProvideNamed(root, "svc", func(Injector) (*hookedService, error) { return &hookedService{}, nil }, AsStarter())
			ctx, cancel := context.WithTimeout(context.WithValue(context.Background(), serviceHooksTraceKey{}, "t-1"), 5*time.Second)
			defer cancel()
			want, _ := ctx.Deadline()

			tt.op(t, root, ctx)

			calls := r.of("svc")
			if is.Len(calls, 1) {
				is.Equal("t-1", calls[0].ctx.Value(serviceHooksTraceKey{}))
				deadline, ok := calls[0].ctx.Deadline()
				is.True(ok, "the hook context has a deadline")
				is.Equal(want, deadline)
			}
		})
	}
}

// errShutdowner implements Shutdown() error.
type errShutdowner struct{ err error }

func (s *errShutdowner) Shutdown() error { return s.err }

func TestServiceHooks_ShutdownBackgroundContext(t *testing.T) {
	t.Parallel()
	testWithTimeout(t, 5*time.Second)
	is := assert.New(t)

	r := &hookCalls{}
	before, _, _ := hookRecorders(r)
	root := NewWithOpts(&InjectorOpts{HookBeforeShutdown: []func(context.Context, *Scope, string){before}})
	provideBuilt(t, root, "svc", &errShutdowner{})

	root.Shutdown()

	calls := r.of("svc")
	if is.Len(calls, 1) && is.NotNil(calls[0].ctx) {
		is.NoError(calls[0].ctx.Err())
	}
}

func TestServiceHooks_CancelledShutdownContext(t *testing.T) {
	t.Parallel()
	testWithTimeout(t, 5*time.Second)
	is := assert.New(t)

	r := &hookCalls{}
	_, after, _ := hookRecorders(r)
	root := NewWithOpts(&InjectorOpts{HookAfterShutdown: []func(context.Context, *Scope, string, time.Duration, error){after}})
	ctx, cancel := context.WithCancel(context.Background())
	provideBuilt(t, root, "svc", &hookedService{shutdown: func(ctx context.Context) error {
		cancel()
		return ctx.Err()
	}})

	root.ShutdownWithContext(ctx)

	calls := r.of("svc")
	if is.Len(calls, 1) {
		is.ErrorIs(calls[0].ctx.Err(), context.Canceled)
		is.ErrorIs(calls[0].err, context.Canceled)
	}
}

// advancingService advances a fake clock in its Start and Shutdown.
type advancingService struct {
	clock    *fakeClock
	start    time.Duration
	shutdown time.Duration
}

func (s *advancingService) Start(context.Context) error {
	s.clock.Advance(s.start)
	return nil
}

func (s *advancingService) Shutdown() error {
	s.clock.Advance(s.shutdown)
	return nil
}

func TestServiceHooks_Elapsed(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		hook    string // "invocation", "start" or "shutdown"
		advance time.Duration
		cached  bool // a second invocation
		want    time.Duration
	}{
		{name: "invocation_3s", hook: "invocation", advance: 3 * time.Second, want: 3 * time.Second},
		{name: "invocation_cached", hook: "invocation", advance: 3 * time.Second, cached: true, want: 0},
		{name: "start_250ms", hook: "start", advance: 250 * time.Millisecond, want: 250 * time.Millisecond},
		{name: "shutdown_1s", hook: "shutdown", advance: time.Second, want: time.Second},
		{name: "shutdown_0s", hook: "shutdown", want: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			testWithTimeout(t, 5*time.Second)

			fc := newFakeClock()
			r := &hookCalls{}
			_, after, invocation := hookRecorders(r)
			opts := &InjectorOpts{Clock: fc}
			switch tt.hook {
			case "invocation":
				opts.HookAfterInvocation = []func(*Scope, string, time.Duration, error){invocation}
			case "start":
				opts.HookAfterStart = []func(context.Context, *Scope, string, time.Duration, error){after}
			case "shutdown":
				opts.HookAfterShutdown = []func(context.Context, *Scope, string, time.Duration, error){after}
			}
			root := NewWithOpts(opts)
			svc := &advancingService{clock: fc}
			switch tt.hook {
			case "start":
				svc.start = tt.advance
			case "shutdown":
				svc.shutdown = tt.advance
			}
			ProvideNamed(root, "svc", func(Injector) (*advancingService, error) {
				if tt.hook == "invocation" {
					fc.Advance(tt.advance)
				}
				return svc, nil
			}, AsStarter())

			switch tt.hook {
			case "invocation":
				_, err := InvokeNamed[*advancingService](root, "svc")
				require.NoError(t, err)
				if tt.cached {
					_, err = InvokeNamed[*advancingService](root, "svc")
					require.NoError(t, err)
				}
			case "start":
				require.NoError(t, root.Start())
			case "shutdown":
				_, err := InvokeNamed[*advancingService](root, "svc")
				require.NoError(t, err)
				root.Shutdown()
			}

			calls := r.of("svc")
			if assert.NotEmpty(t, calls) {
				assert.Equal(t, tt.want, calls[len(calls)-1].elapsed)
			}
		})
	}
}

func TestServiceHooks_ElapsedOnFailure(t *testing.T) {
	t.Parallel()
	testWithTimeout(t, 5*time.Second)
	is := assert.New(t)

	fc := newFakeClock()
	r := &hookCalls{}
	_, _, invocation := hookRecorders(r)
	root := NewWithOpts(&InjectorOpts{Clock: fc, HookAfterInvocation: []func(*Scope, string, time.Duration, error){invocation}})
	ProvideNamed(root, "svc", func(Injector) (int, error) {
		fc.Advance(2 * time.Second)
		return 0, errors.New("boom")
	})

	_, err := InvokeNamed[int](root, "svc")
	require.ErrorAs(t, err, new(*ServiceError))

	calls := r.of("svc")
	if is.Len(calls, 1) {
		is.Equal(2*time.Second, calls[0].elapsed)
		is.ErrorContains(calls[0].err, "boom")
	}
}

// orderLog is a shared, ordered log of labels.
type orderLog struct {
	mu      sync.Mutex
	entries []string
}

func (l *orderLog) add(label string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.entries = append(l.entries, label)
}

func (l *orderLog) list() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.entries...)
}

func TestServiceHooks_Order(t *testing.T) {
	t.Parallel()

	errNoPort := errors.New("no port")
	tests := []struct {
		name    string
		kind    string // "start", "shutdown" or "invocation"
		err     error
		wantErr error
	}{
		{name: "start_ok", kind: "start"},
		{name: "start_error", kind: "start", err: errNoPort, wantErr: errNoPort},
		{name: "shutdown_ok", kind: "shutdown"},
		{name: "invocation_ok", kind: "invocation"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			testWithTimeout(t, 5*time.Second)
			is := assert.New(t)

			log := &orderLog{}
			var afterErrs []error
			var mu sync.Mutex
			beforeHook := func(label string) func(context.Context, *Scope, string) {
				return func(_ context.Context, _ *Scope, name string) {
					if name == "svc" {
						log.add(label)
					}
				}
			}
			afterHook := func(label string) func(context.Context, *Scope, string, time.Duration, error) {
				return func(_ context.Context, _ *Scope, name string, _ time.Duration, err error) {
					if name == "svc" {
						log.add(label)
						mu.Lock()
						afterErrs = append(afterErrs, err)
						mu.Unlock()
					}
				}
			}
			opts := &InjectorOpts{}
			switch tt.kind {
			case "start":
				opts.HookBeforeStart = []func(context.Context, *Scope, string){beforeHook("before-1"), beforeHook("before-2")}
				opts.HookAfterStart = []func(context.Context, *Scope, string, time.Duration, error){afterHook("after-1"), afterHook("after-2")}
			case "shutdown":
				opts.HookBeforeShutdown = []func(context.Context, *Scope, string){beforeHook("before-1"), beforeHook("before-2")}
				opts.HookAfterShutdown = []func(context.Context, *Scope, string, time.Duration, error){afterHook("after-1"), afterHook("after-2")}
			case "invocation":
				opts.HookBeforeInvocation = []func(*Scope, string){
					func(_ *Scope, name string) { log.add("before-1") },
					func(_ *Scope, name string) { log.add("before-2") },
				}
				opts.HookAfterInvocation = []func(*Scope, string, time.Duration, error){
					func(_ *Scope, _ string, _ time.Duration, err error) {
						log.add("after-1")
						afterErrs = append(afterErrs, err)
					},
					func(_ *Scope, _ string, _ time.Duration, err error) {
						log.add("after-2")
						afterErrs = append(afterErrs, err)
					},
				}
			}
			root := NewWithOpts(opts)
			ProvideNamed(root, "svc", func(Injector) (*hookedService, error) {
				svc := &hookedService{
					start:    func(context.Context) error { log.add("op"); return tt.err },
					shutdown: func(context.Context) error { log.add("op"); return nil },
				}
				if tt.kind == "invocation" {
					log.add("op")
				}
				return svc, nil
			}, AsStarter())

			switch tt.kind {
			case "start":
				_ = root.Start()
			case "shutdown":
				shutdownOp(t, root)
			case "invocation":
				invokeOp(t, root)
			}

			is.Equal([]string{"before-1", "before-2", "op", "after-1", "after-2"}, log.list())
			for _, err := range afterErrs {
				if tt.wantErr == nil {
					is.NoError(err)
				} else {
					is.ErrorIs(err, tt.wantErr)
				}
			}
		})
	}
}

func TestServiceHooks_PanicPolicy(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		opts  func() *InjectorOpts
		check func(t *testing.T, root *RootScope)
	}{
		{name: "before_shutdown", opts: func() *InjectorOpts {
			return &InjectorOpts{HookBeforeShutdown: []func(context.Context, *Scope, string){func(context.Context, *Scope, string) { panic("hook-boom") }}}
		}, check: func(t *testing.T, root *RootScope) {
			t.Helper()
			invokeOp(t, root)
			err := root.Shutdown().Err()
			assert.ErrorIs(t, err, ErrShutdownPanic)
			assert.ErrorContains(t, err, "hook-boom")
		}},
		{name: "after_shutdown", opts: func() *InjectorOpts {
			return &InjectorOpts{HookAfterShutdown: []func(context.Context, *Scope, string, time.Duration, error){
				func(context.Context, *Scope, string, time.Duration, error) { panic("hook-boom") },
			}}
		}, check: func(t *testing.T, root *RootScope) {
			t.Helper()
			invokeOp(t, root)
			err := root.Shutdown().Err()
			assert.ErrorIs(t, err, ErrShutdownPanic)
			assert.ErrorContains(t, err, "hook-boom")
		}},
		{name: "after_invocation", opts: func() *InjectorOpts {
			return &InjectorOpts{HookAfterInvocation: []func(*Scope, string, time.Duration, error){
				func(*Scope, string, time.Duration, error) { panic("hook-boom") },
			}}
		}, check: func(t *testing.T, root *RootScope) {
			t.Helper()
			assert.PanicsWithValue(t, "hook-boom", func() { _, _ = InvokeNamed[*hookedService](root, "svc") })
		}},
		{name: "before_start", opts: func() *InjectorOpts {
			return &InjectorOpts{HookBeforeStart: []func(context.Context, *Scope, string){func(context.Context, *Scope, string) { panic("hook-boom") }}}
		}, check: func(t *testing.T, root *RootScope) {
			t.Helper()
			var failed error
			root.AddContainerFailedHook(func(_ context.Context, _ *RootScope, err error) { failed = err })
			assert.Panics(t, func() { _ = root.Start() })
			assert.ErrorContains(t, failed, "hook-boom")
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			testWithTimeout(t, 5*time.Second)

			root := NewWithOpts(tt.opts())
			ProvideNamed(root, "svc", func(Injector) (*hookedService, error) { return &hookedService{}, nil }, AsStarter())

			tt.check(t, root)
		})
	}
}
