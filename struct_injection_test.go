package doger

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime/debug"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// structInjectionExported has an exported nil-interface field and an int.
type structInjectionExported struct {
	Reader io.Reader `do:"reader"`
	Count  int       `do:""`
}

// structInjectionUnexported has an unexported nil-interface field and an int.
type structInjectionUnexported struct {
	reader io.Reader `do:"reader"`
	count  int       `do:""`
}

func TestStructInjection_NilInterfaceDependency(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		invoke func(root *RootScope) (io.Reader, int, error)
	}{
		{
			name: "exported field",
			invoke: func(root *RootScope) (io.Reader, int, error) {
				app, err := InvokeStruct[structInjectionExported](root)
				return app.Reader, app.Count, err
			},
		},
		{
			name: "unexported field",
			invoke: func(root *RootScope) (io.Reader, int, error) {
				app, err := InvokeStruct[structInjectionUnexported](root)
				return app.reader, app.count, err
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			is := assert.New(t)

			root := New()
			ProvideNamedValue[io.Reader](root, "reader", nil)
			ProvideValue(root, 7)

			var (
				reader io.Reader
				count  int
				err    error
			)
			is.NotPanics(func() { reader, count, err = tt.invoke(root) })
			is.NoError(err)
			is.Nil(reader)
			is.Equal(7, count)
		})
	}
}

func TestStructInjection_MustNilInterfaceDependency(t *testing.T) {
	t.Parallel()
	is := assert.New(t)

	type app struct {
		Reader io.Reader `do:"reader"`
	}

	root := New()
	ProvideNamedValue[io.Reader](root, "reader", nil)

	var got app
	is.NotPanics(func() { got = MustInvokeStruct[app](root) })
	is.Nil(got.Reader)
}

// structInjectionRecursive is a recursive pointer type: its element type is
// itself, so an unbounded walk of the pointer chain never ends.
type structInjectionRecursive *structInjectionRecursive

// structInjectionRecursiveChild is set in the child process of
// TestStructInjection_MustRecursivePointerType.
const structInjectionRecursiveChild = "DOGER_TEST_STRUCT_INJECTION_RECURSIVE_CHILD"

func TestStructInjection_MustRecursivePointerType(t *testing.T) {
	if os.Getenv(structInjectionRecursiveChild) == "1" {
		// A small stack limit makes an unbounded recursion fail fast.
		debug.SetMaxStack(16 << 20)
		err := panicError(t, func() { MustInvokeStruct[structInjectionRecursive](New()) })
		require.ErrorIs(t, err, ErrStructInjection)
		return
	}
	t.Parallel()
	testWithTimeout(t, 60*time.Second)
	is := assert.New(t)

	cmd := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestStructInjection_MustRecursivePointerType$", "-test.count=1") //nolint:gosec // the test binary itself
	cmd.Env = append(os.Environ(), structInjectionRecursiveChild+"=1")
	out, err := cmd.CombinedOutput()

	// A crash prints a long trace: compare its ends only. The fatal error
	// comes first, a passing child ends with PASS.
	head := string(out[:min(len(out), 300)])
	tail := string(out[max(len(out)-300, 0):])
	is.NoError(err, "the child process failed: %s", head)
	is.NotContains(head, "stack overflow")
	is.Contains(tail, "PASS")
}

// structInjectionApp has an int field filled from a service named "name".
type structInjectionApp struct {
	Port int `do:"name"`
}

// structInjectionAppAll has an int field tagged `*`.
type structInjectionAppAll struct {
	Port int `do:"*"`
}

func TestStructInjection_InvalidTargets(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		invoke  func(root *RootScope) (any, error)
		message string
	}{
		{
			name: "not a struct",
			invoke: func(root *RootScope) (any, error) {
				return InvokeStruct[int](root)
			},
			message: "doger: must be a struct or a pointer to a struct, but got `int`",
		},
		{
			// reflect.TypeOf of a nil interface value is nil: the target
			// type must come from reflect.TypeFor.
			name: "interface type",
			invoke: func(root *RootScope) (any, error) {
				return InvokeStruct[io.Reader](root)
			},
			message: "doger: must be a struct or a pointer to a struct, but got `io.Reader`",
		},
		{
			name: "recursive pointer type",
			invoke: func(root *RootScope) (any, error) {
				return InvokeStruct[structInjectionRecursive](root)
			},
			message: fmt.Sprintf("doger: recursive pointer type `%s` cannot be injected", NameOf[structInjectionRecursive]()),
		},
		{
			name: "pointer to a recursive pointer type",
			invoke: func(root *RootScope) (any, error) {
				return InvokeStruct[*structInjectionRecursive](root)
			},
			message: fmt.Sprintf("doger: recursive pointer type `%s` cannot be injected", NameOf[structInjectionRecursive]()),
		},
		{
			name: "service not assignable to the field",
			invoke: func(root *RootScope) (any, error) {
				return InvokeStruct[structInjectionApp](root)
			},
			message: fmt.Sprintf("doger: `name` is not assignable to field `%s.Port`", NameOf[structInjectionApp]()),
		},
		{
			name: "tag * on a field that is not a collection",
			invoke: func(root *RootScope) (any, error) {
				return InvokeStruct[structInjectionAppAll](root)
			},
			message: fmt.Sprintf("doger: tag `*` requires a slice or map[string] field, got `%s.Port`", NameOf[structInjectionAppAll]()),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			is := assert.New(t)

			root := New()
			ProvideNamedValue(root, "name", "x")

			result, err := tt.invoke(root)
			is.Zero(result)
			is.ErrorIs(err, ErrStructInjection)
			is.EqualError(err, tt.message)
		})
	}
}

// structInjectionDB is a dependency nothing registers.
type structInjectionDB struct{}

func TestStructInjection_DependencyFailureNotStructError(t *testing.T) {
	t.Parallel()
	is := assert.New(t)

	type app struct {
		DB *structInjectionDB `do:""`
	}

	_, err := InvokeStruct[app](New())
	is.ErrorIs(err, ErrServiceNotFound)
	is.NotErrorIs(err, ErrStructInjection)
}
