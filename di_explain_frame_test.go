package do_test

import (
	"strings"
	"testing"

	"github.com/samber/do/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type explainFrameDB struct{}

func (*explainFrameDB) HealthCheck() error { return nil }

func newExplainFrameDB(do.Injector) (*explainFrameDB, error) {
	return &explainFrameDB{}, nil
}

func TestExplainService_ProviderFrame(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		register func(t *testing.T, i do.Injector)
		explain  func(i do.Injector) (do.ExplainServiceOutput, bool)
		file     string
		function string
	}{
		{
			name: "eager",
			register: func(t *testing.T, i do.Injector) {
				do.ProvideValue(i, &explainFrameDB{})
			},
			explain:  do.ExplainService[*explainFrameDB],
			file:     "/service_eager.go",
			function: "newServiceEager",
		},
		{
			name: "alias",
			register: func(t *testing.T, i do.Injector) {
				do.Provide(i, newExplainFrameDB)
				require.NoError(t, do.As[*explainFrameDB, do.Healthchecker](i))
			},
			explain:  do.ExplainService[do.Healthchecker],
			file:     "/service_alias.go",
			function: "newServiceAlias",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			is := assert.New(t)
			must := require.New(t)

			i := do.New()
			tt.register(t, i)

			output, ok := tt.explain(i)
			must.True(ok)
			must.NotNil(output.Invoked)
			is.True(strings.HasSuffix(output.Invoked.File, tt.file), "Invoked.File = %q", output.Invoked.File)
			is.True(strings.HasPrefix(output.Invoked.Function, tt.function), "Invoked.Function = %q", output.Invoked.Function)
			line := invokedLine(output.String())
			is.NotEmpty(line)
			is.NotContains(line, "stacktrace.go:NewFrameFromCaller")
		})
	}
}

// invokedLine returns the "Invoked:" line of an ExplainServiceOutput string.
func invokedLine(s string) string {
	for _, line := range strings.Split(s, "\n") {
		if strings.HasPrefix(line, "Invoked:") {
			return line
		}
	}
	return ""
}
