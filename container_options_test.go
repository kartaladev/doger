package doger

import (
	"context"
	"fmt"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jonboulle/clockwork"
	"github.com/kartaladev/doger/stacktrace"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestContainerOptions_NilTouchesNoContainer(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		call func() error
	}{
		{name: "provide_value", call: func() error {
			recovered := recoverPanic(func() { ProvideValue[int](nil, 1) })
			err, _ := recovered.(error)
			return err
		}},
		{name: "provide_named_value", call: func() error {
			recovered := recoverPanic(func() { ProvideNamedValue[int](nil, "int", 1) })
			err, _ := recovered.(error)
			return err
		}},
		{name: "invoke", call: func() error {
			_, err := Invoke[int](nil)
			return err
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			is := assert.New(t)

			a := New()

			err := tt.call()

			is.ErrorIs(err, ErrNilInjector)
			_, err = InvokeNamed[int](a, "int")
			is.ErrorIs(err, ErrServiceNotFound)
		})
	}
}

func TestContainerOptions_Isolated(t *testing.T) {
	t.Parallel()

	a := New()
	b := New()
	ProvideNamedValue(a, "int", 1)

	_, err := InvokeNamed[int](b, "int")

	require.Error(t, err)
	assert.ErrorIs(t, err, ErrServiceNotFound)
}

// optsSnapshot describes every field of opts so two snapshots compare equal
// exactly when no field was written: a func by its code pointer, a slice by
// its header (pointer, length, capacity), any other field by its value.
func optsSnapshot(opts *InjectorOpts) map[string]any {
	snapshot := map[string]any{}
	value := reflect.ValueOf(opts).Elem()
	for i := range value.NumField() {
		field := value.Field(i)
		name := value.Type().Field(i).Name
		switch field.Kind() { //nolint:exhaustive // every other kind compares by value
		case reflect.Func:
			snapshot[name] = field.Pointer()
		case reflect.Slice:
			snapshot[name] = [3]uintptr{field.Pointer(), uintptr(field.Len()), uintptr(field.Cap())}
		default:
			snapshot[name] = field.Interface()
		}
	}

	return snapshot
}

func TestContainerOptions_CallerOptsUnchanged(t *testing.T) {
	t.Parallel()

	// The 10s HealthCheckTimeout default arrives in a later group; Logf is
	// today's name of Logger.
	tests := []struct {
		name      string
		opts      func() *InjectorOpts
		construct func(opts *InjectorOpts) *RootScope
		effective func(t *testing.T, root *RootScope, opts *InjectorOpts)
	}{
		{
			name:      "logf_nil",
			opts:      func() *InjectorOpts { return &InjectorOpts{} },
			construct: func(opts *InjectorOpts) *RootScope { return NewWithOpts(opts) },
			effective: func(t *testing.T, root *RootScope, _ *InjectorOpts) {
				t.Helper()
				require.NotNil(t, root.opts.Logf)
				assert.NotPanics(t, func() { root.opts.Logf("discarded") })
			},
		},
		{
			name:      "health_check_timeout_zero",
			opts:      func() *InjectorOpts { return &InjectorOpts{HealthCheckTimeout: 0} },
			construct: func(opts *InjectorOpts) *RootScope { return NewWithOpts(opts) },
			effective: func(t *testing.T, root *RootScope, _ *InjectorOpts) {
				t.Helper()
				assert.Empty(t, root.HealthCheckWithContext(t.Context()))
			},
		},
		{
			name:      "max_invocation_frames_zero",
			opts:      func() *InjectorOpts { return &InjectorOpts{MaxInvocationFrames: 0} },
			construct: func(opts *InjectorOpts) *RootScope { return NewWithOpts(opts) },
			effective: func(t *testing.T, root *RootScope, _ *InjectorOpts) {
				t.Helper()
				assert.Equal(t, 100, invocationFrameCap(root))
			},
		},
		{
			name: "hook_slice_with_spare_capacity",
			opts: func() *InjectorOpts {
				hooks := make([]func(*Scope, string), 1, 4)
				hooks[0] = func(*Scope, string) {}
				return &InjectorOpts{HookBeforeInvocation: hooks}
			},
			construct: func(opts *InjectorOpts) *RootScope { return NewWithOpts(opts) },
			effective: func(t *testing.T, root *RootScope, opts *InjectorOpts) {
				t.Helper()
				assert.Len(t, root.hooks.beforeInvocation.load(), 1)
				root.AddBeforeInvocationHook(func(*Scope, string) {})
				assert.Nil(t, opts.HookBeforeInvocation[:2][1], "the caller's spare capacity was written")
			},
		},
		{
			name:      "clone_clock_nil",
			opts:      func() *InjectorOpts { return &InjectorOpts{} },
			construct: func(opts *InjectorOpts) *RootScope { return New().CloneWithOpts(opts) },
			effective: func(t *testing.T, root *RootScope, _ *InjectorOpts) {
				t.Helper()
				assert.Equal(t, clockwork.NewRealClock(), root.opts.Clock)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			opts := tt.opts()
			before := optsSnapshot(opts)

			root := tt.construct(opts)

			assert.Equal(t, before, optsSnapshot(opts))
			tt.effective(t, root, opts)
			assert.Equal(t, before, optsSnapshot(opts))
		})
	}
}

func TestContainerOptions_NilOpts(t *testing.T) {
	t.Parallel()
	is := assert.New(t)

	root := NewWithOpts(nil)

	require.NotNil(t, root)
	ProvideNamedValue(root, "int", 1)
	got, err := InvokeNamed[int](root, "int")
	require.NoError(t, err)
	is.Equal(1, got)
	is.Equal(optsSnapshot(New().opts), optsSnapshot(root.opts), "same defaults as New()")
}

func TestContainerOptions_SharedOptsIndependent(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		second func(a *RootScope, opts *InjectorOpts) *RootScope
		add    func(a, b *RootScope, hook func(name string) func(*Scope, string))
		ran    []string
	}{
		{
			name:   "new_twice_add_to_a",
			second: func(_ *RootScope, opts *InjectorOpts) *RootScope { return NewWithOpts(opts) },
			add: func(a, _ *RootScope, hook func(string) func(*Scope, string)) {
				a.AddBeforeInvocationHook(hook("only-a"))
			},
			ran: []string{"shared"},
		},
		{
			name:   "clone_with_opts_add_to_a",
			second: func(a *RootScope, opts *InjectorOpts) *RootScope { return a.CloneWithOpts(opts) },
			add: func(a, _ *RootScope, hook func(string) func(*Scope, string)) {
				a.AddBeforeInvocationHook(hook("only-a"))
			},
			ran: []string{"shared"},
		},
		{
			name:   "new_twice_add_to_b",
			second: func(_ *RootScope, opts *InjectorOpts) *RootScope { return NewWithOpts(opts) },
			add: func(_, b *RootScope, hook func(string) func(*Scope, string)) {
				b.AddBeforeInvocationHook(hook("only-b"))
			},
			ran: []string{"shared", "only-b"},
		},
		{
			name:   "clone_add_to_a",
			second: func(a *RootScope, _ *InjectorOpts) *RootScope { return a.Clone() },
			add: func(a, _ *RootScope, hook func(string) func(*Scope, string)) {
				a.AddBeforeInvocationHook(hook("only-a"))
			},
			ran: []string{"shared"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var ran []string
			hook := func(name string) func(*Scope, string) {
				return func(_ *Scope, service string) {
					if service == "svc" {
						ran = append(ran, name)
					}
				}
			}
			opts := &InjectorOpts{HookBeforeInvocation: []func(*Scope, string){hook("shared")}}
			a := NewWithOpts(opts)
			ProvideNamed(a, "svc", func(Injector) (int, error) { return 1, nil })
			b := tt.second(a, opts)
			if !b.serviceExist("svc") {
				ProvideNamed(b, "svc", func(Injector) (int, error) { return 1, nil })
			}

			tt.add(a, b, hook)
			_, err := InvokeNamed[int](b, "svc")

			require.NoError(t, err)
			assert.Equal(t, tt.ran, ran)
			assert.Len(t, opts.HookBeforeInvocation, 1)
		})
	}
}

// addHookSvc is a lazy service that is both a Starter and a Shutdowner.
type addHookSvc struct{}

func (*addHookSvc) Start(context.Context) error { return nil }
func (*addHookSvc) Shutdown() error             { return nil }

func TestContainerOptions_AddHookConcurrent(t *testing.T) {
	t.Parallel()

	invoke := func(root *RootScope, n int) {
		_, _ = InvokeNamed[*addHookSvc](root, fmt.Sprintf("svc-%d", n%10))
	}
	shutdown := func(root *RootScope, n int) {
		_ = ShutdownNamed(root, fmt.Sprintf("svc-%d", n%10))
	}

	tests := []struct {
		name      string
		operation func(root *RootScope, n int)
		adder     func(root *RootScope, hook func())
		// after, when set, runs once every goroutine returned.
		after func(t *testing.T, root *RootScope, counter *atomic.Int64)
	}{
		{
			name:      "before_invocation",
			operation: invoke,
			adder: func(root *RootScope, hook func()) {
				root.AddBeforeInvocationHook(func(*Scope, string) { hook() })
			},
		},
		{
			name:      "after_invocation",
			operation: invoke,
			adder: func(root *RootScope, hook func()) {
				root.AddAfterInvocationHook(func(*Scope, string, error) { hook() })
			},
		},
		{
			name: "before_registration",
			operation: func(root *RootScope, n int) {
				ProvideNamedValue(root, fmt.Sprintf("v-%d", n), n)
			},
			adder: func(root *RootScope, hook func()) {
				root.AddBeforeRegistrationHook(func(*Scope, string) { hook() })
			},
		},
		{
			name:      "after_build",
			operation: invoke,
			adder: func(root *RootScope, hook func()) {
				root.AddAfterBuildHook(func(*Scope, string, any) { hook() })
			},
		},
		{
			name:      "after_shutdown",
			operation: shutdown,
			adder: func(root *RootScope, hook func()) {
				root.AddAfterShutdownHook(func(*Scope, string, error) { hook() })
			},
		},
		{
			name:      "before_shutdown",
			operation: shutdown,
			adder: func(root *RootScope, hook func()) {
				root.AddBeforeShutdownHook(func(*Scope, string) { hook() })
			},
		},
		{
			name:      "before_start",
			operation: func(root *RootScope, _ int) { _ = root.Start() },
			adder: func(root *RootScope, hook func()) {
				root.AddBeforeStartHook(func(*Scope, string) { hook() })
			},
		},
		{
			name:      "container_closing",
			operation: func(root *RootScope, _ int) { _ = root.Shutdown() },
			adder: func(root *RootScope, hook func()) {
				root.AddContainerClosingHook(func(context.Context, *RootScope) { hook() })
			},
		},
		{
			name:      "nil_hook_ignored",
			operation: invoke,
			adder:     func(root *RootScope, _ func()) { root.AddBeforeInvocationHook(nil) },
		},
		{
			name:      "same_hook_twice_runs_twice",
			operation: invoke,
			adder: func(root *RootScope, hook func()) {
				same := func(*Scope, string) { hook() }
				root.AddBeforeInvocationHook(same)
				root.AddBeforeInvocationHook(same)
			},
			after: func(t *testing.T, root *RootScope, counter *atomic.Int64) {
				t.Helper()
				counter.Store(0)
				invoke(root, 0)
				assert.Equal(t, int64(2*4*100), counter.Load())
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			testWithTimeout(t, 10*time.Second)

			root := New()
			for i := range 10 {
				ProvideNamed(root, fmt.Sprintf("svc-%d", i), func(Injector) (*addHookSvc, error) {
					return &addHookSvc{}, nil
				}, AsStarter())
			}
			var counter atomic.Int64
			hook := func() { counter.Add(1) }

			var (
				adding    sync.WaitGroup
				operating sync.WaitGroup
				done      atomic.Bool
				panics    atomic.Int64
				next      atomic.Int64
			)
			for range 4 {
				adding.Go(func() {
					for range 100 {
						if recoverPanic(func() { tt.adder(root, hook) }) != nil {
							panics.Add(1)
						}
					}
				})
				operating.Go(func() {
					for {
						n := int(next.Add(1)) // unique across goroutines
						if recoverPanic(func() { tt.operation(root, n) }) != nil {
							panics.Add(1)
						}
						if done.Load() {
							return
						}
					}
				})
			}
			adding.Wait()
			done.Store(true)
			operating.Wait()

			assert.Zero(t, panics.Load(), "no call panics")
			if tt.after != nil {
				tt.after(t, root, &counter)
			}
			_ = root.Shutdown()
		})
	}
}

func TestContainerOptions_AddHookAppliesNext(t *testing.T) {
	t.Parallel()

	root := New()
	ProvideNamed(root, "svc", func(Injector) (int, error) { return 1, nil })
	var recorded []string
	root.AddBeforeInvocationHook(func(_ *Scope, name string) { recorded = append(recorded, name) })

	_, err := InvokeNamed[int](root, "svc")

	require.NoError(t, err)
	assert.Equal(t, []string{"svc"}, recorded)
}

// siteFrame returns a frame function giving a distinct call site per n, as
// stacktrace.NewFrameFromCaller would for n different callers.
func siteFrame(n int) func() (stacktrace.Frame, bool) {
	return func() (stacktrace.Frame, bool) {
		return stacktrace.Frame{File: "site.go", Line: n + 1}, true
	}
}

// invokeFromSites records the invocation of the lazy "svc" of root from
// sites distinct call sites, through the per-container cap the wrappers use,
// and returns how many frames "svc" recorded.
func invokeFromSites(t *testing.T, root *RootScope, sites int) int {
	t.Helper()

	wrapper, ok := root.self.serviceGet("svc")
	require.True(t, ok)
	lazy, ok := wrapper.(*serviceLazy[int])
	require.True(t, ok)
	for n := range sites {
		lazy.recordInvocationFrame(invocationFrameCap(root), siteFrame(n))
	}
	_, frames := lazy.source()

	return len(frames)
}

func TestContainerOptions_MaxInvocationFrames(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		option   int
		sites    int
		recorded int
	}{
		{name: "default_above_cap", option: 0, sites: 150, recorded: 100},
		{name: "default_below_cap", option: 0, sites: 99, recorded: 99},
		{name: "cap_one", option: 1, sites: 3, recorded: 1},
		{name: "cap_reached", option: 5, sites: 5, recorded: 5},
		{name: "cap_exceeded", option: 5, sites: 6, recorded: 5},
		{name: "negative_off", option: -1, sites: 10, recorded: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			root := NewWithOpts(&InjectorOpts{MaxInvocationFrames: tt.option})
			ProvideNamed(root, "svc", func(Injector) (int, error) { return 1, nil })

			assert.Equal(t, tt.recorded, invokeFromSites(t, root, tt.sites))
		})
	}
}

func TestContainerOptions_FramesPerContainer(t *testing.T) {
	t.Parallel()

	a := NewWithOpts(&InjectorOpts{MaxInvocationFrames: 2})
	b := NewWithOpts(&InjectorOpts{MaxInvocationFrames: 0})
	ProvideNamed(a, "svc", func(Injector) (int, error) { return 1, nil })
	ProvideNamed(b, "svc", func(Injector) (int, error) { return 1, nil })

	var recordedA, recordedB int
	var wg sync.WaitGroup
	wg.Go(func() { recordedA = invokeFromSites(t, a, 5) })
	wg.Go(func() { recordedB = invokeFromSites(t, b, 5) })
	wg.Wait()

	assert.Equal(t, 2, recordedA)
	assert.Equal(t, 5, recordedB)
}
