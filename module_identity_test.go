package do

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type compatDoTagged struct {
	Value int `do:""`
}

type compatInjectTagged struct {
	Value int `inject:""`
}

func TestCompat_StructTagKey(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		opts   *InjectorOpts
		invoke func(Injector) (int, error)
		want   int
	}{
		{
			name: "default_do",
			opts: &InjectorOpts{},
			invoke: func(i Injector) (int, error) {
				s, err := InvokeStruct[compatDoTagged](i)
				return s.Value, err
			},
			want: 7,
		},
		{
			name: "override_inject",
			opts: &InjectorOpts{StructTagKey: "inject"},
			invoke: func(i Injector) (int, error) {
				s, err := InvokeStruct[compatInjectTagged](i)
				return s.Value, err
			},
			want: 7,
		},
		{
			name: "override_ignores_do",
			opts: &InjectorOpts{StructTagKey: "inject"},
			invoke: func(i Injector) (int, error) {
				s, err := InvokeStruct[compatDoTagged](i)
				return s.Value, err
			},
			want: 0,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			is := assert.New(t)

			i := NewWithOpts(tt.opts)
			ProvideValue(i, 7)

			got, err := tt.invoke(i)
			require.NoError(t, err)
			is.Equal(tt.want, got)
		})
	}
}

func TestCompat_ErrorSentinelMessages(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		sentinel error
		want     string
	}{
		{name: "ErrServiceNotFound", sentinel: ErrServiceNotFound, want: "DI: could not find service"},
		{name: "ErrServiceNotMatch", sentinel: ErrServiceNotMatch, want: "DI: could not find service satisfying interface"},
		{name: "ErrCircularDependency", sentinel: ErrCircularDependency, want: "DI: circular dependency detected"},
		{name: "ErrHealthCheckTimeout", sentinel: ErrHealthCheckTimeout, want: "DI: health check timeout"},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, tt.sentinel.Error())
		})
	}
}
