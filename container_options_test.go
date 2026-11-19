package doger

import (
	"reflect"
	"testing"

	"github.com/jonboulle/clockwork"
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

	// MaxInvocationFrames and the 10s HealthCheckTimeout default arrive in
	// later groups; Logf is today's name of Logger.
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
			name: "hook_slice_with_spare_capacity",
			opts: func() *InjectorOpts {
				hooks := make([]func(*Scope, string), 1, 4)
				hooks[0] = func(*Scope, string) {}
				return &InjectorOpts{HookBeforeInvocation: hooks}
			},
			construct: func(opts *InjectorOpts) *RootScope { return NewWithOpts(opts) },
			effective: func(t *testing.T, root *RootScope, opts *InjectorOpts) {
				t.Helper()
				assert.Len(t, root.opts.HookBeforeInvocation, 1)
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
