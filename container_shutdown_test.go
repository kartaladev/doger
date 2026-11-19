package doger

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// shutdownTestPoolError is a typed error a Shutdown panics with.
type shutdownTestPoolError struct{ Code int }

func (e *shutdownTestPoolError) Error() string { return fmt.Sprintf("pool error %d", e.Code) }

// shutdownTestPanicPlain panics with value in Shutdown().
type shutdownTestPanicPlain struct{ value any }

func (s *shutdownTestPanicPlain) Shutdown() { panic(s.value) }

// shutdownTestPanicErr panics with value in Shutdown() error.
type shutdownTestPanicErr struct{ value any }

func (s *shutdownTestPanicErr) Shutdown() error { panic(s.value) }

// shutdownTestPanicCtx panics with value in Shutdown(ctx).
type shutdownTestPanicCtx struct{ value any }

func (s *shutdownTestPanicCtx) Shutdown(context.Context) { panic(s.value) }

// shutdownTestPanicCtxErr panics with value in Shutdown(ctx) error.
type shutdownTestPanicCtxErr struct{ value any }

func (s *shutdownTestPanicCtxErr) Shutdown(context.Context) error { panic(s.value) }

// shutdownTestCounter counts its Shutdown calls and returns err.
type shutdownTestCounter struct {
	calls atomic.Int32
	err   error
}

func (s *shutdownTestCounter) Shutdown() error {
	s.calls.Add(1)
	return s.err
}

// provideBuilt registers value as the lazy name of i and builds it.
func provideBuilt[T any](t *testing.T, i Injector, name string, value T) {
	t.Helper()
	ProvideNamed(i, name, func(Injector) (T, error) { return value, nil })
	_, err := InvokeNamed[T](i, name)
	require.NoError(t, err)
}

// reportError returns the error the report holds for name in scope.
func reportError(report *ShutdownReport, scope *Scope, name string) error {
	return report.Errors[newServiceDescription(scope.ID(), scope.Name(), name)]
}

func TestContainerShutdown_ServiceShutdownPanic(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		bad      func(Injector, *testing.T)
		inChild  bool
		shutRoot bool // shut the root down, not the child
		also     func(t *testing.T, err error)
		text     string
	}{
		{
			name: "root_shutdown_plain",
			bad:  func(i Injector, t *testing.T) { provideBuilt(t, i, "bad", &shutdownTestPanicPlain{value: "boom"}) },
			text: "boom",
		},
		{
			name: "root_shutdown_error",
			bad: func(i Injector, t *testing.T) {
				provideBuilt(t, i, "bad", &shutdownTestPanicErr{value: io.ErrClosedPipe})
			},
			also: func(t *testing.T, err error) { t.Helper(); assert.ErrorIs(t, err, io.ErrClosedPipe) },
			text: "io: read/write on closed pipe",
		},
		{
			name: "root_shutdown_ctx",
			bad: func(i Injector, t *testing.T) {
				provideBuilt(t, i, "bad", &shutdownTestPanicCtx{value: &shutdownTestPoolError{Code: 7}})
			},
			also: func(t *testing.T, err error) {
				t.Helper()
				var target *shutdownTestPoolError
				if assert.ErrorAs(t, err, &target) {
					assert.Equal(t, 7, target.Code)
				}
			},
			text: "pool error 7",
		},
		{
			name: "root_shutdown_ctx_error_panic_nil",
			bad:  func(i Injector, t *testing.T) { provideBuilt(t, i, "bad", &shutdownTestPanicCtxErr{value: nil}) },
			also: func(t *testing.T, err error) {
				t.Helper()
				var target *runtime.PanicNilError
				assert.ErrorAs(t, err, &target)
			},
			text: "panic called with nil",
		},
		{
			name:    "child_shutdown_plain",
			bad:     func(i Injector, t *testing.T) { provideBuilt(t, i, "bad", &shutdownTestPanicPlain{value: "boom"}) },
			inChild: true,
			text:    "boom",
		},
		{
			name:     "child_via_root_shutdown_plain",
			bad:      func(i Injector, t *testing.T) { provideBuilt(t, i, "bad", &shutdownTestPanicPlain{value: "boom"}) },
			inChild:  true,
			shutRoot: true,
			text:     "boom",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			testWithTimeout(t, 5*time.Second)
			is := assert.New(t)

			root := New()
			tenant := root.Scope("tenant")
			scope := root.self
			if tt.inChild {
				scope = tenant
			}
			tt.bad(scope, t)
			good := &shutdownTestCounter{}
			provideBuilt(t, scope, "good", good)

			var report *ShutdownReport
			if tt.inChild && !tt.shutRoot {
				report = tenant.Shutdown()
			} else {
				report = root.Shutdown()
			}

			is.False(report.Succeed)
			err := reportError(report, scope, "bad")
			is.ErrorIs(err, ErrShutdownPanic)
			if tt.also != nil {
				tt.also(t, err)
			}
			is.ErrorContains(err, tt.text)
			is.Equal(int32(1), good.calls.Load(), "Shutdown calls of good")
			is.NoError(reportError(report, scope, "good"))
		})
	}
}

func TestContainerShutdown_EagerShutdownPanic(t *testing.T) {
	t.Parallel()
	testWithTimeout(t, 5*time.Second)
	is := assert.New(t)

	root := New()
	ProvideNamedValue(root, "cfg", &shutdownTestPanicPlain{value: "eager boom"})

	report := root.Shutdown()

	is.False(report.Succeed)
	err := reportError(report, root.self, "cfg")
	is.ErrorIs(err, ErrShutdownPanic)
	is.ErrorContains(err, "eager boom")
	is.Equal(PhaseClosed, root.Phase())
}

// shutdownPanicChildEnv makes the test binary run the child side of
// TestContainerShutdown_ShutdownPanicDoesNotCrashProcess.
const shutdownPanicChildEnv = "DOGER_SHUTDOWN_PANIC_CHILD"

func TestContainerShutdown_ShutdownPanicDoesNotCrashProcess(t *testing.T) {
	if os.Getenv(shutdownPanicChildEnv) == "1" {
		root := New()
		ProvideNamed(root, "bad", func(Injector) (*shutdownTestPanicPlain, error) {
			return &shutdownTestPanicPlain{value: "boom"}, nil
		})
		if _, err := InvokeNamed[*shutdownTestPanicPlain](root, "bad"); err != nil {
			panic(err)
		}
		fmt.Println(root.Shutdown().Err())
		return
	}

	t.Parallel()

	cmd := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestContainerShutdown_ShutdownPanicDoesNotCrashProcess$")
	cmd.Env = append(os.Environ(), shutdownPanicChildEnv+"=1")
	output, err := cmd.CombinedOutput()

	require.NoError(t, err, "the child process failed:\n%s", output)
	assert.Contains(t, string(output), "doger: ")
	assert.Contains(t, string(output), "boom")
}

// shutdownHookRecorder records the services its hooks ran for.
type shutdownHookRecorder struct {
	mu     sync.Mutex
	before []string
	after  []string
}

func (r *shutdownHookRecorder) recordBefore(_ *Scope, name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.before = append(r.before, name)
}

func (r *shutdownHookRecorder) recordAfter(_ *Scope, name string, _ error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.after = append(r.after, name)
}

func TestContainerShutdown_ShutdownHookPanic(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		opts        func(r *shutdownHookRecorder) *InjectorOpts
		shutdownErr error
		also        func(t *testing.T, err error)
		before      bool // the hook list under test
	}{
		{
			name: "before_panics",
			opts: func(r *shutdownHookRecorder) *InjectorOpts {
				return &InjectorOpts{HookBeforeShutdown: []func(*Scope, string){func(*Scope, string) { panic("before boom") }, r.recordBefore}}
			},
			also:   func(t *testing.T, err error) { t.Helper(); assert.ErrorContains(t, err, "before boom") },
			before: true,
		},
		{
			name: "after_panics",
			opts: func(r *shutdownHookRecorder) *InjectorOpts {
				return &InjectorOpts{HookAfterShutdown: []func(*Scope, string, error){func(*Scope, string, error) { panic("after boom") }, r.recordAfter}}
			},
			also: func(t *testing.T, err error) { t.Helper(); assert.ErrorContains(t, err, "after boom") },
		},
		{
			name: "after_panics_shutdown_fails",
			opts: func(r *shutdownHookRecorder) *InjectorOpts {
				return &InjectorOpts{HookAfterShutdown: []func(*Scope, string, error){func(*Scope, string, error) { panic("after boom") }, r.recordAfter}}
			},
			shutdownErr: io.ErrClosedPipe,
			also:        func(t *testing.T, err error) { t.Helper(); assert.ErrorIs(t, err, io.ErrClosedPipe) },
		},
		{
			name: "before_panics_with_error",
			opts: func(r *shutdownHookRecorder) *InjectorOpts {
				return &InjectorOpts{HookBeforeShutdown: []func(*Scope, string){func(*Scope, string) { panic(io.EOF) }, r.recordBefore}}
			},
			also:   func(t *testing.T, err error) { t.Helper(); assert.ErrorIs(t, err, io.EOF) },
			before: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			testWithTimeout(t, 5*time.Second)
			is := assert.New(t)

			recorder := &shutdownHookRecorder{}
			root := NewWithOpts(tt.opts(recorder))
			db := &shutdownTestCounter{err: tt.shutdownErr}
			provideBuilt(t, root, "db", db)

			report := root.Shutdown()

			is.Equal(int32(1), db.calls.Load(), "Shutdown calls of db")
			err := reportError(report, root.self, "db")
			is.ErrorIs(err, ErrShutdownPanic)
			tt.also(t, err)
			recorder.mu.Lock()
			defer recorder.mu.Unlock()
			if tt.before {
				is.Equal([]string{"db"}, recorder.before, "runs of the second before hook")
			} else {
				is.Equal([]string{"db"}, recorder.after, "runs of the second after hook")
			}
		})
	}
}

// shutdownPanicCall is a HookContainerPanic call.
type shutdownPanicCall struct {
	point       string
	value       any
	afterClosed bool // the Closed hook had run
}

func TestContainerShutdown_FirstPanicToHook(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		withHook bool
		services []string
		calls    int
	}{
		{name: "one_panic", withHook: true, services: []string{"a"}, calls: 1},
		{name: "three_panics", withHook: true, services: []string{"a", "b", "c"}, calls: 1},
		{name: "no_hook", services: []string{"a"}, calls: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			testWithTimeout(t, 5*time.Second)
			is := assert.New(t)

			var mu sync.Mutex
			var closed bool
			var calls []shutdownPanicCall
			opts := &InjectorOpts{
				HookContainerClosed: []func(context.Context, *RootScope, *ShutdownReport){func(context.Context, *RootScope, *ShutdownReport) {
					mu.Lock()
					defer mu.Unlock()
					closed = true
				}},
			}
			if tt.withHook {
				opts.HookContainerPanic = func(_ *RootScope, point string, value any) {
					mu.Lock()
					defer mu.Unlock()
					calls = append(calls, shutdownPanicCall{point: point, value: value, afterClosed: closed})
				}
			}
			root := NewWithOpts(opts)
			for _, name := range tt.services {
				provideBuilt(t, root, name, &shutdownTestPanicPlain{value: name})
			}

			report := root.Shutdown()

			mu.Lock()
			defer mu.Unlock()
			if is.Len(calls, tt.calls) && tt.calls > 0 {
				is.Equal("service-shutdown", calls[0].point)
				is.Contains(tt.services, calls[0].value)
				is.True(calls[0].afterClosed, "called after the Closed hook ran")
			}
			for _, name := range tt.services {
				is.ErrorIs(reportError(report, root.self, name), ErrShutdownPanic, name)
			}
		})
	}
}

func TestContainerShutdown_PanicHookRepanics(t *testing.T) {
	t.Parallel()
	testWithTimeout(t, 5*time.Second)
	is := assert.New(t)

	root := NewWithOpts(&InjectorOpts{HookContainerPanic: func(_ *RootScope, _ string, value any) { panic(value) }})
	provideBuilt(t, root, "a", &shutdownTestPanicPlain{value: "boom"})

	is.PanicsWithValue("boom", func() { root.Shutdown() })
	is.Equal(PhaseClosed, root.Phase())
}

// closedTestCounts counts the builds and Shutdown calls of closedTestSvc
// values.
type closedTestCounts struct {
	builds, shutdowns atomic.Int32
}

// closedTestSvc is a service counting its Shutdown calls in counts.
type closedTestSvc struct{ counts *closedTestCounts }

func (s *closedTestSvc) Shutdown() error {
	s.counts.shutdowns.Add(1)
	return nil
}

func (s *closedTestSvc) Name() string { return "svc" }

// closedTestIface is implemented by *closedTestSvc.
type closedTestIface interface{ Name() string }

// provideClosedTestSvc registers the lazy name of i, counting in counts.
func provideClosedTestSvc(i Injector, name string, counts *closedTestCounts) {
	ProvideNamed(i, name, func(Injector) (*closedTestSvc, error) {
		counts.builds.Add(1)
		return &closedTestSvc{counts: counts}, nil
	})
}

// recoverError calls fn and returns the error it panicked with: nil when it
// did not panic, an error describing the value when it is not an error.
func recoverError(fn func()) (err error) {
	defer func() {
		if r := recover(); r != nil {
			if e, ok := r.(error); ok {
				err = e
				return
			}
			err = fmt.Errorf("panic: %v", r)
		}
	}()
	fn()
	return nil
}

func TestContainerShutdown_NoResurrection(t *testing.T) {
	t.Parallel()
	testWithTimeout(t, 5*time.Second)
	is := assert.New(t)

	counts := &closedTestCounts{}
	lookedUp, shutDown := make(chan struct{}), make(chan struct{})
	var invocations atomic.Int32
	root := NewWithOpts(&InjectorOpts{HookBeforeInvocation: []func(*Scope, string){func(_ *Scope, name string) {
		if name == "svc" && invocations.Add(1) == 2 {
			close(lookedUp)
			<-shutDown
		}
	}}})
	provideClosedTestSvc(root, "svc", counts)
	_, err := InvokeNamed[*closedTestSvc](root, "svc")
	require.NoError(t, err)

	type result struct {
		svc *closedTestSvc
		err error
	}
	results := make(chan result, 1)
	go func() {
		svc, err := InvokeNamed[*closedTestSvc](root, "svc")
		results <- result{svc, err}
	}()
	<-lookedUp
	root.Shutdown()
	close(shutDown)

	got := <-results
	is.Nil(got.svc)
	is.ErrorIs(got.err, ErrShutdown)
	is.Equal(int32(1), counts.builds.Load(), "builds")
	is.Equal(int32(1), counts.shutdowns.Load(), "Shutdown calls")
}

func TestContainerShutdown_InvokeAfterShutdownNamed(t *testing.T) {
	t.Parallel()
	is := assert.New(t)

	counts := &closedTestCounts{}
	root := New()
	provideClosedTestSvc(root, "svc", counts)
	_, err := InvokeNamed[*closedTestSvc](root, "svc")
	require.NoError(t, err)
	require.NoError(t, ShutdownNamed(root, "svc"))

	_, err = InvokeNamed[*closedTestSvc](root, "svc")

	is.ErrorIs(err, ErrServiceNotFound)
	is.Equal(int32(1), counts.builds.Load(), "builds")
}

func TestContainerShutdown_ConcurrentInvokeDuringShutdown(t *testing.T) {
	t.Parallel()
	testWithTimeout(t, 30*time.Second)

	for repeat := range 50 {
		counts := &closedTestCounts{}
		root := New()
		scopes := make([]*Scope, 10)
		for n := range scopes {
			scopes[n] = root.Scope(fmt.Sprintf("s%d", n))
			provideClosedTestSvc(scopes[n], "svc", counts)
		}

		var unexpected atomic.Pointer[error]
		var wg sync.WaitGroup
		for g := range 32 {
			wg.Go(func() {
				for n := g; ; n++ {
					_, err := InvokeNamed[*closedTestSvc](scopes[n%len(scopes)], "svc")
					if err != nil {
						if !errors.Is(err, ErrShutdown) && !errors.Is(err, ErrServiceNotFound) {
							unexpected.Store(&err)
						}
						return
					}
				}
			})
		}
		root.Shutdown()
		wg.Wait()

		if err := unexpected.Load(); err != nil {
			t.Fatalf("repeat %d: unexpected error %v", repeat, *err)
		}
		require.Equal(t, counts.builds.Load(), counts.shutdowns.Load(), "repeat %d: builds and Shutdown calls", repeat)
	}
}

func TestContainerShutdown_ClosedScopeRefuses(t *testing.T) {
	t.Parallel()

	type outcome int
	const (
		returnsShutdown outcome = iota
		panicsShutdown
		returnsInstance
	)

	tests := []struct {
		name     string
		shutRoot bool // root.Shutdown(), else tenant.Shutdown()
		onRoot   bool // the call targets the root, else "tenant"
		call     func(i Injector) error
		outcome  outcome
	}{
		{name: "root_invoke", shutRoot: true, onRoot: true, outcome: returnsShutdown, call: func(i Injector) error {
			_, err := Invoke[*closedTestSvc](i)
			return err
		}},
		{name: "root_invoke_named", shutRoot: true, onRoot: true, outcome: returnsShutdown, call: func(i Injector) error {
			_, err := InvokeNamed[*closedTestSvc](i, "svc")
			return err
		}},
		{name: "root_invoke_all", shutRoot: true, onRoot: true, outcome: returnsShutdown, call: func(i Injector) error {
			_, err := InvokeAll[*closedTestSvc](i)
			return err
		}},
		{name: "root_provide", shutRoot: true, onRoot: true, outcome: panicsShutdown, call: func(i Injector) error {
			return recoverError(func() { ProvideNamedValue(i, "late", 1) })
		}},
		{name: "root_override", shutRoot: true, onRoot: true, outcome: panicsShutdown, call: func(i Injector) error {
			return recoverError(func() { OverrideNamedValue(i, "svc", &closedTestSvc{}) })
		}},
		{name: "root_scope", shutRoot: true, onRoot: true, outcome: panicsShutdown, call: func(i Injector) error {
			return recoverError(func() { i.Scope("late") })
		}},
		{name: "root_alias", shutRoot: true, onRoot: true, outcome: returnsShutdown, call: func(i Injector) error {
			return AliasNamed[*closedTestSvc, closedTestIface](i, "svc", "i")
		}},
		{name: "tenant_invoke_named", outcome: returnsShutdown, call: func(i Injector) error {
			_, err := InvokeNamed[*closedTestSvc](i, "svc")
			return err
		}},
		{name: "tenant_provide", outcome: panicsShutdown, call: func(i Injector) error {
			return recoverError(func() { ProvideNamedValue(i, "late", 1) })
		}},
		{name: "tenant_scope", outcome: panicsShutdown, call: func(i Injector) error {
			return recoverError(func() { i.Scope("late") })
		}},
		{name: "tenant_closed_root_invoke_named", onRoot: true, outcome: returnsInstance, call: func(i Injector) error {
			svc, err := InvokeNamed[*closedTestSvc](i, "svc")
			if err == nil && svc == nil {
				return errors.New("no instance")
			}
			return err
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			testWithTimeout(t, 5*time.Second)

			root := New()
			tenant := root.Scope("tenant")
			for _, scope := range []Injector{root, tenant} {
				provideClosedTestSvc(scope, "svc", &closedTestCounts{})
				_, err := InvokeNamed[*closedTestSvc](scope, "svc")
				require.NoError(t, err)
			}
			if tt.shutRoot {
				root.Shutdown()
			} else {
				tenant.Shutdown()
			}

			var target Injector = tenant
			if tt.onRoot {
				target = root
			}
			err := tt.call(target)

			switch tt.outcome {
			case returnsShutdown, panicsShutdown:
				assert.ErrorIs(t, err, ErrShutdown)
			case returnsInstance:
				assert.NoError(t, err)
			}
		})
	}
}

func TestContainerShutdown_SecondShutdownNoop(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		firstRoot bool
	}{
		{name: "tenant_then_tenant"},
		{name: "root_then_tenant", firstRoot: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			testWithTimeout(t, 5*time.Second)
			is := assert.New(t)

			root := New()
			tenant := root.Scope("tenant")
			svc := &shutdownTestCounter{}
			provideBuilt(t, tenant, "svc", svc)
			if tt.firstRoot {
				root.Shutdown()
			} else {
				tenant.Shutdown()
			}

			report := tenant.Shutdown()

			is.True(report.Succeed)
			is.Empty(report.Services)
			is.Equal(int32(1), svc.calls.Load(), "Shutdown calls")
		})
	}
}

func TestContainerShutdown_ScopeDuringShutdownRefused(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		// opts returns the options whose closing hook creates a scope "late"
		// and stores the panic in got; parent is the scope whose children
		// must be empty afterwards
		setup func(got *error) (root *RootScope, parent *Scope)
	}{
		{name: "container_closing", setup: func(got *error) (*RootScope, *Scope) {
			var root *RootScope
			root = NewWithOpts(&InjectorOpts{HookContainerClosing: []func(context.Context, *RootScope){func(context.Context, *RootScope) {
				*got = recoverError(func() { root.Scope("late") })
			}}})
			root.Scope("a")
			return root, root.self
		}},
		{name: "scope_closing", setup: func(got *error) (*RootScope, *Scope) {
			var a *Scope
			root := NewWithOpts(&InjectorOpts{HookScopeClosing: []func(context.Context, *Scope){func(_ context.Context, scope *Scope) {
				if scope == a {
					*got = recoverError(func() { a.Scope("late") })
				}
			}}})
			a = root.Scope("a")
			return root, a
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			testWithTimeout(t, 5*time.Second)

			var got error
			root, parent := tt.setup(&got)

			root.Shutdown()

			assert.ErrorIs(t, got, ErrShutdown)
			assert.Empty(t, parent.Children())
		})
	}
}

// shutdownPanicRecorder records the HookContainerPanic calls it receives.
type shutdownPanicRecorder struct {
	mu    sync.Mutex
	calls []shutdownPanicCall
}

func (r *shutdownPanicRecorder) hook(_ *RootScope, point string, value any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, shutdownPanicCall{point: point, value: value})
}

// points returns the points of the recorded calls, in order.
func (r *shutdownPanicRecorder) points() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	points := make([]string, 0, len(r.calls))
	for _, call := range r.calls {
		points = append(points, call.point)
	}
	return points
}

func TestContainerShutdown_ServicePanicReachesHook(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		// setup registers a built "svc" panicking in Shutdown and returns the
		// shutdown to run
		setup func(t *testing.T, opts *InjectorOpts) func()
		want  []string
	}{
		{
			name: "container_closing_hook_panics_too",
			setup: func(t *testing.T, opts *InjectorOpts) func() {
				t.Helper()
				opts.HookContainerClosing = []func(context.Context, *RootScope){func(context.Context, *RootScope) { panic("closing boom") }}
				root := NewWithOpts(opts)
				provideBuilt(t, root, "svc", &shutdownTestPanicPlain{value: "svc boom"})
				return func() { root.Shutdown() }
			},
			want: []string{"closing", "service-shutdown"},
		},
		{
			name: "scope_closing_hook_panics_too",
			setup: func(t *testing.T, opts *InjectorOpts) func() {
				t.Helper()
				opts.HookScopeClosing = []func(context.Context, *Scope){func(context.Context, *Scope) { panic("scope boom") }}
				root := NewWithOpts(opts)
				root.Scope("tenant")
				provideBuilt(t, root, "svc", &shutdownTestPanicPlain{value: "svc boom"})
				return func() { root.Shutdown() }
			},
			want: []string{"scope-closing", "service-shutdown"},
		},
		{
			name: "child_scope_shutdown",
			setup: func(t *testing.T, opts *InjectorOpts) func() {
				t.Helper()
				root := NewWithOpts(opts)
				tenant := root.Scope("tenant")
				provideBuilt(t, tenant, "svc", &shutdownTestPanicPlain{value: "svc boom"})
				return func() { tenant.Shutdown() }
			},
			want: []string{"service-shutdown"},
		},
		{
			name: "child_service_during_root_shutdown",
			setup: func(t *testing.T, opts *InjectorOpts) func() {
				t.Helper()
				root := NewWithOpts(opts)
				tenant := root.Scope("tenant")
				provideBuilt(t, tenant, "svc", &shutdownTestPanicPlain{value: "svc boom"})
				return func() { root.Shutdown() }
			},
			want: []string{"service-shutdown"},
		},
		{
			name: "exported_service_during_root_shutdown",
			setup: func(t *testing.T, opts *InjectorOpts) func() {
				t.Helper()
				root := NewWithOpts(opts)
				tenant := root.Scope("tenant")
				provideBuilt(t, tenant, "svc", &shutdownTestPanicPlain{value: "svc boom"})
				require.NoError(t, ExportNamed[*shutdownTestPanicPlain](tenant, "svc"))
				return func() { root.Shutdown() }
			},
			want: []string{"service-shutdown"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			testWithTimeout(t, 5*time.Second)

			recorder := &shutdownPanicRecorder{}
			shutdown := tt.setup(t, &InjectorOpts{HookContainerPanic: recorder.hook})

			shutdown()

			assert.Equal(t, tt.want, recorder.points())
		})
	}
}

func TestContainerShutdown_KeptPanicLoggedWhenDispatchStops(t *testing.T) {
	t.Parallel()
	testWithTimeout(t, 5*time.Second)
	is := assert.New(t)

	recorder := newLogRecorder(slog.LevelDebug)
	root := NewWithOpts(&InjectorOpts{
		Logger: recorder.logger(),
		// re-panics on the observer point, so the service panic is never
		// dispatched
		HookContainerPanic: func(_ *RootScope, point string, value any) {
			if point == "closing" {
				panic(value)
			}
		},
		HookContainerClosing: []func(context.Context, *RootScope){func(context.Context, *RootScope) { panic("closing boom") }},
	})
	provideBuilt(t, root, "svc", &shutdownTestPanicPlain{value: "token=hunter2"})

	is.PanicsWithValue("closing boom", func() { root.Shutdown() })

	var recovered []logRecord
	for _, rec := range recorder.withMessage("panic recovered") {
		if rec.attrs["point"] == "service-shutdown" && rec.attrs["service"] == "svc" {
			recovered = append(recovered, rec)
		}
	}
	if is.Len(recovered, 1, "records: %v", recorder.all()) {
		is.Equal(slog.LevelError, recovered[0].level)
	}
	for _, rec := range recorder.all() {
		if strings.Contains(rec.String(), "hunter2") {
			is.Equal(slog.LevelDebug, rec.level, "the panic value only at Debug")
		}
	}
	is.NotEmpty(recorder.containing("hunter2"), "the panic value at Debug")
}

func TestContainerShutdown_ChildDetached(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		shutdown func(t *testing.T, req *Scope)
	}{
		{name: "shutdown", shutdown: func(_ *testing.T, req *Scope) { req.Shutdown() }},
		{name: "shutdown_with_deadline", shutdown: func(t *testing.T, req *Scope) {
			ctx, cancel := context.WithTimeout(t.Context(), time.Second)
			defer cancel()
			req.ShutdownWithContext(ctx)
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			testWithTimeout(t, 5*time.Second)
			is := assert.New(t)

			root := New()
			req := root.Scope("req")
			provideBuilt(t, req, "svc", &shutdownTestCounter{})

			tt.shutdown(t, req)

			_, found := root.ChildByName("req")
			is.False(found)
			is.Empty(root.Children())
			var again *Scope
			is.NotPanics(func() { again = root.Scope("req") })
			if is.NotNil(again) {
				is.NoError(again.checkOpen(), "the new scope is open")
			}
		})
	}
}

func TestContainerShutdown_ChildDetachNoGrowth(t *testing.T) {
	t.Parallel()

	for _, cycles := range []int{1, 1000} {
		t.Run(fmt.Sprintf("cycles_%d", cycles), func(t *testing.T) {
			t.Parallel()
			testWithTimeout(t, 10*time.Second)
			is := assert.New(t)

			root := New()
			for range cycles {
				req := root.Scope("req")
				ProvideNamedValue(req, "value", &shutdownTestCounter{})
				_, err := InvokeNamed[*shutdownTestCounter](req, "value")
				require.NoError(t, err)
				req.Shutdown()
			}

			is.Empty(root.Children())
			for _, entry := range root.HealthReport().Services {
				is.NotEqual("req", entry.Service.ScopeName, "a service of a req scope")
			}
		})
	}
}

func TestContainerShutdown_ChildDetachConcurrent(t *testing.T) {
	t.Parallel()
	testWithTimeout(t, 20*time.Second)

	root := New()
	var wg sync.WaitGroup
	for g := range 16 {
		wg.Go(func() {
			for n := range 200 {
				root.Scope(fmt.Sprintf("req-%d-%d", g, n)).Shutdown()
			}
		})
	}
	wg.Wait()

	assert.Empty(t, root.Children())
}

func TestContainerShutdown_GrandchildDetach(t *testing.T) {
	t.Parallel()
	testWithTimeout(t, 5*time.Second)
	is := assert.New(t)

	root := New()
	a := root.Scope("a")
	b := a.Scope("b")

	b.Shutdown()

	is.Empty(a.Children())
	_, found := root.ChildByName("a")
	is.True(found, `root still holds "a"`)
	is.Len(root.Children(), 1)
}

func TestContainerShutdown_SharedEagerOnce(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		shutdown func(root, clone *RootScope) []*ShutdownReport
	}{
		{name: "root_then_clone", shutdown: func(root, clone *RootScope) []*ShutdownReport {
			return []*ShutdownReport{root.Shutdown(), clone.Shutdown()}
		}},
		{name: "clone_then_root", shutdown: func(root, clone *RootScope) []*ShutdownReport {
			return []*ShutdownReport{clone.Shutdown(), root.Shutdown()}
		}},
		{name: "concurrently", shutdown: func(root, clone *RootScope) []*ShutdownReport {
			reports := make([]*ShutdownReport, 2)
			var wg sync.WaitGroup
			wg.Go(func() { reports[0] = root.Shutdown() })
			wg.Go(func() { reports[1] = clone.Shutdown() })
			wg.Wait()
			return reports
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			testWithTimeout(t, 5*time.Second)
			is := assert.New(t)

			pool := &shutdownTestCounter{}
			root := New()
			ProvideNamedValue(root, "pool", pool)
			clone := root.Clone()

			for _, report := range tt.shutdown(root, clone) {
				is.True(report.Succeed, "report errors: %v", report.Errors)
			}
			is.Equal(int32(1), pool.calls.Load(), "Shutdown calls of pool")
		})
	}
}

func TestContainerShutdown_CloneLazyPerContainer(t *testing.T) {
	t.Parallel()
	testWithTimeout(t, 5*time.Second)
	is := assert.New(t)

	counts := &closedTestCounts{}
	root := New()
	provideClosedTestSvc(root, "svc", counts)
	clone := root.Clone()
	for _, container := range []*RootScope{root, clone} {
		_, err := InvokeNamed[*closedTestSvc](container, "svc")
		require.NoError(t, err)
	}

	root.Shutdown()
	clone.Shutdown()

	is.Equal(int32(2), counts.builds.Load(), "builds")
	is.Equal(int32(2), counts.shutdowns.Load(), "Shutdown calls")
}

// reentrantSupervisor calls inner from its Shutdown(ctx) and records the
// report and how long the call took.
type reentrantSupervisor struct {
	inner   func(ctx context.Context) *ShutdownReport
	report  *ShutdownReport
	elapsed time.Duration
}

func (s *reentrantSupervisor) Shutdown(ctx context.Context) error {
	start := time.Now()
	s.report = s.inner(ctx)
	s.elapsed = time.Since(start)
	if s.report.Succeed {
		return nil
	}
	errs := make([]error, 0, len(s.report.Errors))
	for _, err := range s.report.Errors {
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

func TestContainerShutdown_ReentrantRootShutdown(t *testing.T) {
	t.Parallel()

	type ctxKey struct{}
	tests := []struct {
		name    string
		inner   func(root *RootScope, ctx context.Context) *ShutdownReport
		bounded bool // the inner call waits for its own 50ms deadline
	}{
		{name: "shutdown_context", inner: func(root *RootScope, ctx context.Context) *ShutdownReport {
			return root.ShutdownWithContext(ctx)
		}},
		{name: "derived_context", inner: func(root *RootScope, ctx context.Context) *ShutdownReport {
			return root.ShutdownWithContext(context.WithValue(ctx, ctxKey{}, "v"))
		}},
		{name: "unrelated_context", bounded: true, inner: func(root *RootScope, _ context.Context) *ShutdownReport {
			ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
			defer cancel()
			return root.ShutdownWithContext(ctx)
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			testWithTimeout(t, 5*time.Second)

			synctest.Test(t, func(t *testing.T) {
				is := assert.New(t)

				var closed atomic.Int32
				root := NewWithOpts(&InjectorOpts{HookContainerClosed: []func(context.Context, *RootScope, *ShutdownReport){
					func(context.Context, *RootScope, *ShutdownReport) { closed.Add(1) },
				}})
				supervisor := &reentrantSupervisor{inner: func(ctx context.Context) *ShutdownReport { return tt.inner(root, ctx) }}
				db := &shutdownTestCounter{}
				provideBuilt(t, root, "supervisor", supervisor)
				provideBuilt(t, root, "db", db)

				outer := root.Shutdown()

				if tt.bounded {
					is.Equal(50*time.Millisecond, supervisor.elapsed)
					is.False(supervisor.report.Succeed)
					for _, err := range supervisor.report.Errors {
						is.ErrorIs(err, context.DeadlineExceeded)
					}
					is.False(outer.Succeed)
					is.Len(outer.Errors, 1)
					is.ErrorIs(reportError(outer, root.self, "supervisor"), context.DeadlineExceeded)
				} else {
					is.Zero(supervisor.elapsed, "returned at once")
					is.NoError(supervisor.report.Err())
					is.Empty(supervisor.report.Services)
					is.True(outer.Succeed, "outer errors: %v", outer.Errors)
				}
				is.Equal(int32(1), db.calls.Load(), "Shutdown calls of db")
				is.Equal(int32(1), closed.Load(), "Closed hook runs")
			})
		})
	}
}

func TestShutdownReport_ErrUnwrap(t *testing.T) {
	t.Parallel()

	type want struct {
		scope, service string
		cause          error
	}
	tests := []struct {
		name     string
		setup    func(t *testing.T, root *RootScope, tenant *Scope)
		matches  []error
		unwrap   []want
		contains []string // in Error(), in this order
	}{
		{
			name:  "no_error",
			setup: func(t *testing.T, root *RootScope, _ *Scope) { provideBuilt(t, root, "a", &shutdownTestCounter{}) },
		},
		{
			name: "one_error",
			setup: func(t *testing.T, root *RootScope, _ *Scope) {
				provideBuilt(t, root, "a", &shutdownTestCounter{err: io.EOF})
			},
			matches:  []error{io.EOF},
			unwrap:   []want{{"[root]", "a", io.EOF}},
			contains: []string{"doger: shutdown errors:", "a"},
		},
		{
			name: "two_errors_two_scopes",
			setup: func(t *testing.T, root *RootScope, tenant *Scope) {
				provideBuilt(t, tenant, "b", &shutdownTestCounter{err: io.EOF})
				provideBuilt(t, root, "a", &shutdownTestCounter{err: io.ErrClosedPipe})
			},
			matches:  []error{io.EOF, io.ErrClosedPipe},
			unwrap:   []want{{"[root]", "a", io.ErrClosedPipe}, {"tenant", "b", io.EOF}},
			contains: []string{"doger: shutdown errors:", "a", "b"},
		},
		{
			name: "panic",
			setup: func(t *testing.T, root *RootScope, _ *Scope) {
				provideBuilt(t, root, "a", &shutdownTestPanicPlain{value: "boom"})
			},
			matches:  []error{ErrShutdownPanic},
			unwrap:   []want{{"[root]", "a", ErrShutdownPanic}},
			contains: []string{"boom"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			testWithTimeout(t, 5*time.Second)
			is := assert.New(t)

			root := New()
			tenant := root.Scope("tenant")
			tt.setup(t, root, tenant)

			report := root.Shutdown()

			if len(tt.matches) == 0 {
				is.NoError(report.Err())
				is.Empty(report.Unwrap())
				is.Empty(report.Error())
				return
			}
			for _, target := range tt.matches {
				is.ErrorIs(report.Err(), target)
			}
			unwrapped := report.Unwrap()
			if is.Len(unwrapped, len(tt.unwrap)) {
				for n, w := range tt.unwrap {
					var serviceErr *ServiceError
					if is.ErrorAs(unwrapped[n], &serviceErr) {
						is.Equal(w.scope, serviceErr.Scope)
						is.Equal(w.service, serviceErr.Service)
						is.ErrorIs(serviceErr, w.cause)
					}
				}
			}
			text := report.Error()
			last := -1
			for _, part := range tt.contains {
				index := strings.Index(text[last+1:], part)
				if is.GreaterOrEqual(index, 0, "%q in %q after %d", part, text, last) {
					last += 1 + index
				}
			}
		})
	}
}

func TestShutdownReport_ErrorsAsServiceError(t *testing.T) {
	t.Parallel()
	testWithTimeout(t, 5*time.Second)
	is := assert.New(t)

	root := New()
	provideBuilt(t, root, "db", &shutdownTestCounter{err: io.EOF})

	var serviceErr *ServiceError
	if is.ErrorAs(root.Shutdown().Err(), &serviceErr) {
		is.Equal("db", serviceErr.Service)
		is.Equal("[root]", serviceErr.Scope)
	}
}

func TestShutdownReport_MarshalJSON(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		setup func(t *testing.T, root *RootScope)
		want  string
	}{
		{
			name:  "success",
			setup: func(t *testing.T, root *RootScope) { provideBuilt(t, root, "a", &shutdownTestCounter{}) },
			want:  `{"succeed":true,"shutdownTime":"0s","services":[{"scope":"[root]","service":"a"}],"errors":[]}`,
		},
		{
			name: "one_error",
			setup: func(t *testing.T, root *RootScope) {
				provideBuilt(t, root, "b", &shutdownTestCounter{})
				provideBuilt(t, root, "a", &shutdownTestCounter{err: errors.New("closing failed")})
			},
			want: `{"succeed":false,"shutdownTime":"0s","services":[{"scope":"[root]","service":"a"},{"scope":"[root]","service":"b"}],` +
				`"errors":[{"scope":"[root]","service":"a","error":"closing failed"}]}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			synctest.Test(t, func(t *testing.T) {
				root := New()
				tt.setup(t, root)

				out, err := json.Marshal(root.Shutdown())

				require.NoError(t, err)
				assert.JSONEq(t, tt.want, string(out))
			})
		})
	}
}

// lookedUpThenShutDown invokes name of root on another goroutine, lets the
// lookup happen, shuts root down, then lets the invocation continue, and
// returns its error.
func lookedUpThenShutDown(t *testing.T, invokedFrom Injector, name string, shutdown func(), setHook func(func(*Scope, string))) error {
	t.Helper()

	lookedUp, shutDown := make(chan struct{}), make(chan struct{})
	var once sync.Once
	setHook(func(_ *Scope, invoked string) {
		if invoked == name {
			once.Do(func() {
				close(lookedUp)
				<-shutDown
			})
		}
	})
	result := make(chan error, 1)
	go func() {
		_, err := invokeAnyByName(invokedFrom, name)
		result <- err
	}()
	<-lookedUp
	shutdown()
	close(shutDown)

	return <-result
}

func TestContainerShutdown_ClosedScopeReviewGaps(t *testing.T) {
	t.Parallel()

	t.Run("transient_looked_up_before_shutdown", func(t *testing.T) {
		t.Parallel()
		testWithTimeout(t, 5*time.Second)

		root := New()
		var builds atomic.Int32
		ProvideNamedTransient(root, "t", func(Injector) (*shutdownTestCounter, error) {
			builds.Add(1)
			return &shutdownTestCounter{}, nil
		})

		err := lookedUpThenShutDown(t, root, "t", func() { root.Shutdown() }, func(hook func(*Scope, string)) {
			root.AddBeforeInvocationHook(hook)
		})

		assert.ErrorIs(t, err, ErrShutdown)
		assert.Equal(t, int32(0), builds.Load(), "builds")
	})

	t.Run("default_yield_after_scope_closed", func(t *testing.T) {
		t.Parallel()
		testWithTimeout(t, 5*time.Second)

		root := New()
		tenant := root.Scope("tenant")
		var rootBuilds atomic.Int32
		ProvideNamed(root, "svc", func(Injector) (*shutdownTestCounter, error) {
			rootBuilds.Add(1)
			return &shutdownTestCounter{}, nil
		})
		ProvideNamed(tenant, "svc", func(Injector) (*shutdownTestCounter, error) { return &shutdownTestCounter{}, nil }, AsDefault())

		err := lookedUpThenShutDown(t, tenant, "svc", func() { tenant.Shutdown() }, func(hook func(*Scope, string)) {
			root.AddBeforeInvocationHook(hook)
		})

		assert.ErrorIs(t, err, ErrShutdown)
		assert.Equal(t, int32(0), rootBuilds.Load(), "builds of the root svc")
	})

	t.Run("superseded_default_after_scope_closed", func(t *testing.T) {
		t.Parallel()
		testWithTimeout(t, 5*time.Second)

		root := New()
		tenant := root.Scope("tenant")
		ProvideNamed(tenant, "svc", func(Injector) (*shutdownTestCounter, error) { return &shutdownTestCounter{}, nil }, AsDefault())
		wrapper, ok := tenant.serviceGet("svc")
		require.True(t, ok)
		ProvideNamed(tenant, "svc", func(Injector) (*shutdownTestCounter, error) { return &shutdownTestCounter{}, nil })
		tenant.Shutdown()

		_, err := wrapper.(serviceWrapper[*shutdownTestCounter]).getInstance(tenant)

		assert.ErrorIs(t, err, ErrShutdown)
	})

	t.Run("invoke_struct", func(t *testing.T) {
		t.Parallel()

		root := New()
		root.Shutdown()

		_, err := InvokeStruct[struct{}](root)

		assert.ErrorIs(t, err, ErrShutdown)
	})
}

// customInjector is an Injector implemented outside the library's own
// types, by embedding one: the only way, since Injector has unexported
// methods.
type customInjector struct{ Injector }

func TestProvide_CustomInjectorEmbeddingScope(t *testing.T) {
	t.Parallel()
	is := assert.New(t)

	root := New()
	custom := customInjector{root.Scope("custom")}

	is.NotPanics(func() { ProvideNamedValue(custom, "value", 42) })
	value, err := InvokeNamed[int](custom, "value")
	is.NoError(err)
	is.Equal(42, value)
}
