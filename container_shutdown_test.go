package doger

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
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
		report := root.Shutdown()
		for _, err := range report.Errors {
			fmt.Println(err)
		}
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
