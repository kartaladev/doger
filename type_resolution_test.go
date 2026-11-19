package doger

import (
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInvokeAs_NilInterfaceInstance(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		invoke func(t *testing.T, root *RootScope) io.Reader
	}{
		{
			name: "InvokeAs returns a nil error",
			invoke: func(t *testing.T, root *RootScope) io.Reader {
				t.Helper()
				reader, err := InvokeAs[io.Reader](root)
				assert.NoError(t, err)

				return reader
			},
		},
		{
			name: "MustInvokeAs does not panic",
			invoke: func(t *testing.T, root *RootScope) io.Reader {
				t.Helper()
				var reader io.Reader
				assert.NotPanics(t, func() { reader = MustInvokeAs[io.Reader](root) })

				return reader
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			root := New()
			ProvideNamedValue[io.Reader](root, "reader", nil)

			assert.Nil(t, tt.invoke(t, root))
		})
	}
}

func TestInvokeAs_NonNilInstance(t *testing.T) {
	t.Parallel()
	is := assert.New(t)
	must := require.New(t)

	root := New()
	ProvideNamedValue[io.Reader](root, "reader", strings.NewReader("x"))

	reader, err := InvokeAs[io.Reader](root)
	must.NoError(err)
	must.NotNil(reader)

	content, err := io.ReadAll(reader)
	must.NoError(err)
	is.Equal("x", string(content))
}
