package doger_test

import (
	"runtime"
	"strings"
	"testing"

	"github.com/kartaladev/doger"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type explainFrameDB struct{}

func (*explainFrameDB) HealthCheck() error { return nil }

func newExplainFrameDB(doger.Injector) (*explainFrameDB, error) {
	return &explainFrameDB{}, nil
}

// nextLine returns the file of its caller and the line following the call:
// the line on which the caller registers a service.
func nextLine(t *testing.T) (file string, line int) {
	t.Helper()

	_, file, line, ok := runtime.Caller(1)
	require.True(t, ok)

	return file, line + 1
}

// assertRegisteredAt asserts that frame names the consumer's file and line.
func assertRegisteredAt(t *testing.T, output doger.ExplainServiceOutput, file string, line int) {
	t.Helper()
	is := assert.New(t)

	require.NotNil(t, output.Invoked)
	is.True(strings.HasSuffix(file, output.Invoked.File), "Invoked.File = %q, want the end of %q", output.Invoked.File, file)
	is.Equal(line, output.Invoked.Line)
	for _, library := range []string{"service_eager.go", "service_alias.go", "di.go", "root_scope.go"} {
		is.False(strings.HasSuffix(output.Invoked.File, "/"+library), "Invoked.File = %q", output.Invoked.File)
	}
}

func TestSourceTracking_ProviderFrame(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		register func(t *testing.T, i doger.Injector) (file string, line int)
		explain  func(i doger.Injector) (doger.ExplainServiceOutput, bool)
	}{
		{
			name: "eager",
			register: func(t *testing.T, i doger.Injector) (string, int) {
				file, line := nextLine(t)
				doger.ProvideValue(i, &explainFrameDB{})
				return file, line
			},
			explain: doger.ExplainService[*explainFrameDB],
		},
		{
			name: "alias",
			register: func(t *testing.T, i doger.Injector) (string, int) {
				doger.Provide(i, newExplainFrameDB)
				file, line := nextLine(t)
				require.NoError(t, doger.Alias[*explainFrameDB, doger.Healthchecker](i))
				return file, line
			},
			explain: doger.ExplainService[doger.Healthchecker],
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			is := assert.New(t)
			must := require.New(t)

			i := doger.New()
			file, line := tt.register(t, i)

			output, ok := tt.explain(i)
			must.True(ok)
			must.NotNil(output.Invoked)
			is.True(strings.HasSuffix(output.Invoked.File, "/source_tracking_test.go"), "Invoked.File = %q", output.Invoked.File)
			is.True(strings.HasPrefix(output.Invoked.Function, "TestSourceTracking_ProviderFrame"), "Invoked.Function = %q", output.Invoked.Function)
			assertRegisteredAt(t, output, file, line)
			line2 := invokedLine(output.String())
			is.NotEmpty(line2)
			is.NotContains(line2, "stacktrace.go:NewFrameFromCaller")
		})
	}
}

func TestSourceTracking_RegistrationForms(t *testing.T) {
	t.Parallel()

	name := doger.NameOf[*explainFrameDB]()

	tests := []struct {
		name     string
		register func(t *testing.T, root *doger.RootScope) (file string, line int)
		explain  func(i doger.Injector) (doger.ExplainServiceOutput, bool)
	}{
		{
			name: "provide_value",
			register: func(t *testing.T, root *doger.RootScope) (string, int) {
				file, line := nextLine(t)
				doger.ProvideValue(root, &explainFrameDB{})
				return file, line
			},
		},
		{
			name: "provide_named_value",
			register: func(t *testing.T, root *doger.RootScope) (string, int) {
				file, line := nextLine(t)
				doger.ProvideNamedValue(root, name, &explainFrameDB{})
				return file, line
			},
		},
		{
			name: "override_value",
			register: func(t *testing.T, root *doger.RootScope) (string, int) {
				file, line := nextLine(t)
				doger.OverrideValue(root, &explainFrameDB{})
				return file, line
			},
		},
		{
			name: "override_named_value",
			register: func(t *testing.T, root *doger.RootScope) (string, int) {
				file, line := nextLine(t)
				doger.OverrideNamedValue(root, name, &explainFrameDB{})
				return file, line
			},
		},
		{
			name: "eager",
			register: func(t *testing.T, root *doger.RootScope) (string, int) {
				file, line := nextLine(t)
				doger.Package(doger.Eager(&explainFrameDB{}))(root)
				return file, line
			},
		},
		{
			name: "eager_named",
			register: func(t *testing.T, root *doger.RootScope) (string, int) {
				file, line := nextLine(t)
				doger.Package(doger.EagerNamed(name, &explainFrameDB{}))(root)
				return file, line
			},
		},
		{
			name: "alias",
			register: func(t *testing.T, root *doger.RootScope) (string, int) {
				doger.Provide(root, newExplainFrameDB)
				file, line := nextLine(t)
				require.NoError(t, doger.Alias[*explainFrameDB, doger.Healthchecker](root))
				return file, line
			},
			explain: doger.ExplainService[doger.Healthchecker],
		},
		{
			name: "nil_value",
			register: func(t *testing.T, root *doger.RootScope) (string, int) {
				file, line := nextLine(t)
				doger.ProvideValue[*explainFrameDB](root, nil)
				return file, line
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			root := doger.New()
			file, line := tt.register(t, root)

			explain := tt.explain
			if explain == nil {
				explain = doger.ExplainService[*explainFrameDB]
			}
			output, ok := explain(root)
			require.True(t, ok)
			assertRegisteredAt(t, output, file, line)
		})
	}
}

func TestSourceTracking_NewCallSite(t *testing.T) {
	t.Parallel()

	file, line := nextLine(t)
	root := doger.New(doger.Eager(&explainFrameDB{}))

	output, ok := doger.ExplainService[*explainFrameDB](root)
	require.True(t, ok)
	assertRegisteredAt(t, output, file, line)
}

// invokedLine returns the "Invoked:" line of an ExplainServiceOutput string.
func invokedLine(s string) string {
	for line := range strings.SplitSeq(s, "\n") {
		if strings.HasPrefix(line, "Invoked:") {
			return line
		}
	}
	return ""
}
