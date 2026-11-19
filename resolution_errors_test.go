package doger

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolutionErrors_TypeMismatch(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		invoke  func(i Injector) (any, error)
		zero    any
		message string
	}{
		{
			name:    "string",
			invoke:  func(i Injector) (any, error) { return InvokeNamed[string](i, "x") },
			zero:    "",
			message: "doger: service found, but type mismatch: invoking `string` but registered `int`",
		},
		{
			name:    "int64",
			invoke:  func(i Injector) (any, error) { return InvokeNamed[int64](i, "x") },
			zero:    int64(0),
			message: "doger: service found, but type mismatch: invoking `int64` but registered `int`",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			is := assert.New(t)

			root := New()
			ProvideNamedValue(root, "x", 42)

			got, err := tt.invoke(root)

			is.Equal(tt.zero, got)
			is.ErrorIs(err, ErrServiceTypeMismatch)
			is.NotErrorIs(err, ErrServiceNotFound)
			is.NotErrorIs(err, ErrServiceNotMatch)
			is.EqualError(err, tt.message)
		})
	}
}

func TestResolutionErrors_MustTypeMismatchPanics(t *testing.T) {
	t.Parallel()

	root := New()
	ProvideNamedValue(root, "x", 42)

	recovered := recoverPanic(func() { MustInvokeNamed[string](root, "x") })

	err, ok := recovered.(error)
	require.True(t, ok, "panic value %v must be an error", recovered)
	assert.ErrorIs(t, err, ErrServiceTypeMismatch)
}

// errRefused is the error of the refusing providers of the resolution error
// tests.
var errRefused = errors.New("refused")

func TestResolutionErrors_ServiceErrorWrapping(t *testing.T) {
	t.Parallel()

	refuse := func(Injector) (int, error) { return 0, errRefused }

	tests := []struct {
		name           string
		setup          func(root *RootScope)
		isServiceError bool
		service        string
		sentinel       error
		message        string
	}{
		{
			name:     "nothing_registered",
			setup:    func(*RootScope) {},
			sentinel: ErrServiceNotFound,
			message:  "doger: could not find service `a`, no service available",
		},
		{
			name:           "lazy_provider_error",
			setup:          func(root *RootScope) { ProvideNamed(root, "a", refuse) },
			isServiceError: true,
			service:        "a",
			sentinel:       errRefused,
			message:        `doger: service "a" in scope "[root]": refused`,
		},
		{
			name: "lazy_missing_dependency",
			setup: func(root *RootScope) {
				ProvideNamed(root, "a", func(i Injector) (int, error) { return InvokeNamed[int](i, "b") })
			},
			isServiceError: true,
			service:        "a",
			sentinel:       ErrServiceNotFound,
			message:        `doger: service "a" in scope "[root]": could not find service ` + "`b`, available services: `a`, path: `a` -> `b`",
		},
		{
			name: "lazy_dependency_provider_error",
			setup: func(root *RootScope) {
				ProvideNamed(root, "a", func(i Injector) (int, error) { return InvokeNamed[int](i, "b") })
				ProvideNamed(root, "b", refuse)
			},
			isServiceError: true,
			service:        "a",
			sentinel:       errRefused,
			message:        `doger: service "a" in scope "[root]": service "b" in scope "[root]": refused`,
		},
		{
			name: "eager_missing_declared_dependency",
			setup: func(root *RootScope) {
				ProvideNamedValue(root, "a", 1, DependsOnNamed("missing"))
			},
			isServiceError: true,
			service:        "a",
			sentinel:       ErrServiceNotFound,
			message:        `doger: service "a" in scope "[root]": could not find service ` + "`missing`, available services: `a`, path: `a` -> `missing`",
		},
		{
			name: "transient_missing_declared_dependency",
			setup: func(root *RootScope) {
				ProvideNamedTransient(root, "a", func(Injector) (int, error) { return 1, nil }, DependsOnNamed("missing"))
			},
			isServiceError: true,
			service:        "a",
			sentinel:       ErrServiceNotFound,
			message:        `doger: service "a" in scope "[root]": could not find service ` + "`missing`, available services: `a`, path: `a` -> `missing`",
		},
		{
			name:           "transient_provider_error",
			setup:          func(root *RootScope) { ProvideNamedTransient(root, "a", refuse) },
			isServiceError: true,
			service:        "a",
			sentinel:       errRefused,
			message:        `doger: service "a" in scope "[root]": refused`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			is := assert.New(t)

			root := New()
			tt.setup(root)

			got, err := InvokeNamed[int](root, "a")

			is.Zero(got)
			is.ErrorIs(err, tt.sentinel)
			is.EqualError(err, tt.message)

			var serviceErr *ServiceError
			is.Equal(tt.isServiceError, errors.As(err, &serviceErr))
			if tt.isServiceError {
				is.Equal(tt.service, serviceErr.Service)
				is.Equal("[root]", serviceErr.Scope)
			}
		})
	}
}

func TestResolutionErrors_ServiceErrorScope(t *testing.T) {
	t.Parallel()
	is := assert.New(t)

	root := New()
	tenant := root.Scope("tenant-1")
	ProvideNamed(tenant, "repo", func(Injector) (int, error) { return 0, errRefused })

	_, err := InvokeNamed[int](tenant, "repo")

	var serviceErr *ServiceError
	require.ErrorAs(t, err, &serviceErr)
	is.Equal("repo", serviceErr.Service)
	is.Equal("tenant-1", serviceErr.Scope)
	is.ErrorIs(err, errRefused)
}

func TestResolutionErrors_DuplicateRegistration(t *testing.T) {
	t.Parallel()

	two := func(Injector) (int, error) { return 2, nil }

	tests := []struct {
		name     string
		register func(root *RootScope)       // panics with the error
		returns  func(root *RootScope) error // returns the error, when set
	}{
		{name: "provide_named", register: func(root *RootScope) { ProvideNamed(root, "db", two) }},
		{name: "provide_named_value", register: func(root *RootScope) { ProvideNamedValue(root, "db", 2) }},
		{name: "provide_named_transient", register: func(root *RootScope) { ProvideNamedTransient(root, "db", two) }},
		{name: "alias_named", returns: func(root *RootScope) error {
			ProvideNamedValue(root, "other", 3)
			return AliasNamed[int, int](root, "other", "db")
		}},
		{name: "must_alias_named", register: func(root *RootScope) {
			ProvideNamedValue(root, "other", 3)
			MustAliasNamed[int, int](root, "other", "db")
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			is := assert.New(t)

			root := New()
			ProvideNamed(root, "db", func(Injector) (int, error) { return 1, nil })

			var err error
			if tt.returns != nil {
				is.NotPanics(func() { err = tt.returns(root) })
			} else {
				recovered := recoverPanic(func() { tt.register(root) })
				var ok bool
				err, ok = recovered.(error)
				require.True(t, ok, "panic value %v must be an error", recovered)
			}

			is.ErrorIs(err, ErrServiceAlreadyDeclared)
			is.EqualError(err, "doger: service `db` has already been declared")

			got, err := InvokeNamed[int](root, "db")
			require.NoError(t, err)
			is.Equal(1, got)
		})
	}
}

func TestResolutionErrors_OverrideNoError(t *testing.T) {
	t.Parallel()
	is := assert.New(t)

	root := New()
	ProvideNamed(root, "db", func(Injector) (int, error) { return 1, nil })

	is.NotPanics(func() { OverrideNamedValue(root, "db", 2) })

	got, err := InvokeNamed[int](root, "db")
	require.NoError(t, err)
	is.Equal(2, got)
}

type nilInjectorApp struct {
	DB int `do:"db"`
}

func TestResolutionErrors_NilInjectorReturnsError(t *testing.T) {
	t.Parallel()

	var typedNilRoot *RootScope
	var typedNilScope *Scope

	tests := []struct {
		name string
		call func() error
	}{
		{name: "invoke", call: func() error { _, err := Invoke[int](nil); return err }},
		{name: "invoke_named", call: func() error { _, err := InvokeNamed[int](nil, "db"); return err }},
		{name: "invoke_as", call: func() error { _, err := InvokeAs[fmt.Stringer](nil); return err }},
		{name: "invoke_all", call: func() error { _, err := InvokeAll[int](nil); return err }},
		{name: "invoke_struct", call: func() error { _, err := InvokeStruct[nilInjectorApp](nil); return err }},
		{name: "health_check_named", call: func() error { return HealthCheckNamed(nil, "db") }},
		{name: "shutdown_named", call: func() error { return ShutdownNamed(nil, "db") }},
		{name: "alias_named", call: func() error { return AliasNamed[int, int](nil, "a", "b") }},
		{name: "declare_dependency", call: func() error { return DeclareDependency(nil, "a", "b") }},
		{name: "typed_nil_root_invoke", call: func() error { _, err := Invoke[int](typedNilRoot); return err }},
		{name: "typed_nil_scope_invoke_named", call: func() error { _, err := InvokeNamed[int](typedNilScope, "db"); return err }},
		{name: "extend_nil_root", call: func() error { return Extend(nil, &testExtension{name: "redis"}) }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			is := assert.New(t)

			err := tt.call()

			is.ErrorIs(err, ErrNilInjector)
			is.EqualError(err, "doger: nil injector")
		})
	}
}

func TestResolutionErrors_NilInjectorPanics(t *testing.T) {
	t.Parallel()

	provider := func(Injector) (int, error) { return 1, nil }
	handler := func(context.Context, Component[Runner, struct{}]) error { return nil }

	tests := []struct {
		name string
		call func()
	}{
		{name: "provide", call: func() { Provide[int](nil, provider) }},
		{name: "provide_named_value", call: func() { ProvideNamedValue(nil, "db", 1) }},
		{name: "provide_transient", call: func() { ProvideTransient[int](nil, provider) }},
		{name: "override_named_value", call: func() { OverrideNamedValue(nil, "db", 1) }},
		{name: "must_invoke", call: func() { MustInvoke[int](nil) }},
		{name: "must_alias", call: func() { MustAlias[int, int](nil) }},
		{name: "invoke_collection", call: func() { InvokeCollection[int](nil) }},
		{name: "explain_injector", call: func() { ExplainInjector(nil) }},
		{name: "package", call: func() { Package(Lazy[int](provider))(nil) }},
		{name: "runnable_handle", call: func() { Runnable().Handle(nil, StageAfterStart, handler) }},
		{name: "extension_properties", call: func() { _, _ = ExtensionProperties(nil) }},
		{name: "extension_properties_typed_nil_scope", call: func() { _, _ = ExtensionProperties((*Scope)(nil)) }},
		{name: "extension_properties_typed_nil_root", call: func() { _, _ = ExtensionProperties((*RootScope)(nil)) }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			recovered := recoverPanic(tt.call)

			err, ok := recovered.(error)
			require.True(t, ok, "panic value %v must be an error", recovered)
			assert.ErrorIs(t, err, ErrNilInjector)
		})
	}
}

// provideNumbered registers count int values named "s00", "s01", and so on.
func provideNumbered(root *RootScope, count int) {
	for n := range count {
		ProvideNamedValue(root, fmt.Sprintf("s%02d", n), n)
	}
}

func TestResolutionErrors_NotFoundCap(t *testing.T) {
	t.Parallel()

	const first10 = "`s00`, `s01`, `s02`, `s03`, `s04`, `s05`, `s06`, `s07`, `s08`, `s09`"

	tests := []struct {
		name    string
		count   int
		message string
	}{
		{
			name:    "0",
			count:   0,
			message: "doger: could not find service `missing`, no service available",
		},
		{
			name:    "1",
			count:   1,
			message: "doger: could not find service `missing`, available services: `s00`",
		},
		{
			name:    "10",
			count:   10,
			message: "doger: could not find service `missing`, available services: " + first10,
		},
		{
			name:    "11",
			count:   11,
			message: "doger: could not find service `missing`, available services: " + first10 + " and 1 more",
		},
		{
			name:    "2000",
			count:   2000,
			message: "doger: could not find service `missing`, available services: " + first10 + " and 1990 more",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			is := assert.New(t)

			root := New()
			provideNumbered(root, tt.count)

			_, err := InvokeNamed[int](root, "missing")
			is.ErrorIs(err, ErrServiceNotFound)
			is.EqualError(err, tt.message)
		})
	}
}

func TestResolutionErrors_NotFoundCapWithChain(t *testing.T) {
	t.Parallel()
	is := assert.New(t)

	root := New()
	provideNumbered(root, 12)
	ProvideNamed(root, "a", func(i Injector) (int, error) { return InvokeNamed[int](i, "missing") })

	// The 13 visible services sort as `a`, `s00` … `s11`: the list keeps
	// `a` to `s08`.
	_, err := InvokeNamed[int](root, "a")
	is.ErrorIs(err, ErrServiceNotFound)
	is.ErrorContains(err, "`s08` and 3 more, path: `a` -> `missing`")
}
