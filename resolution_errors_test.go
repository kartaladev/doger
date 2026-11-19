package doger

import (
	"errors"
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

	// The alias rows of the scenario need AliasNamed, which group 4 adds.
	tests := []struct {
		name     string
		register func(root *RootScope)
	}{
		{name: "provide_named", register: func(root *RootScope) { ProvideNamed(root, "db", two) }},
		{name: "provide_named_value", register: func(root *RootScope) { ProvideNamedValue(root, "db", 2) }},
		{name: "provide_named_transient", register: func(root *RootScope) { ProvideNamedTransient(root, "db", two) }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			is := assert.New(t)

			root := New()
			ProvideNamed(root, "db", func(Injector) (int, error) { return 1, nil })

			recovered := recoverPanic(func() { tt.register(root) })

			err, ok := recovered.(error)
			require.True(t, ok, "panic value %v must be an error", recovered)
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
