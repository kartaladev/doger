package doger

import (
	"os/exec"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// exportedAPI returns the lines of go doc -short for this package.
func exportedAPI(t *testing.T) []string {
	t.Helper()

	out, err := exec.CommandContext(t.Context(), "go", "doc", "-short", ".").CombinedOutput()
	require.NoError(t, err, string(out))

	return strings.Split(string(out), "\n")
}

// hasLinePrefix reports whether a line of lines starts with prefix.
func hasLinePrefix(lines []string, prefix string) bool {
	for _, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), prefix) {
			return true
		}
	}

	return false
}

func TestAPISurface_AliasRename(t *testing.T) {
	t.Parallel()

	api := exportedAPI(t)

	tests := []struct {
		name    string
		prefix  string
		present bool
	}{
		{name: "alias", prefix: "func Alias[", present: true},
		{name: "alias_named", prefix: "func AliasNamed[", present: true},
		{name: "must_alias", prefix: "func MustAlias[", present: true},
		{name: "must_alias_named", prefix: "func MustAliasNamed[", present: true},
		{name: "as_gone", prefix: "func As["},
		{name: "as_named_gone", prefix: "func AsNamed["},
		{name: "must_as_gone", prefix: "func MustAs["},
		{name: "must_as_named_gone", prefix: "func MustAsNamed["},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.present, hasLinePrefix(api, tt.prefix), tt.prefix)
		})
	}
}

func TestAPISurface_ExportNameRename(t *testing.T) {
	t.Parallel()
	is := assert.New(t)

	api := exportedAPI(t)

	is.True(hasLinePrefix(api, "func WithExportName(name string) ExportOption"))
	is.False(hasLinePrefix(api, "func ExportAs("))
}
