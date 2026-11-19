package doger

import (
	"testing"

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
