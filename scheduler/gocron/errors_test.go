package dogergocron

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// oldLibPrefix is the message prefix inherited from samber/do, split so a
// source search for the old prefix finds no Go file.
const oldLibPrefix = "DI" + ": "

// exportedErrorVars returns the names of the exported package-level
// variables named Err* declared in the non-test Go files of this package.
func exportedErrorVars(t *testing.T) []string {
	t.Helper()

	entries, err := os.ReadDir(".")
	require.NoError(t, err)

	names := []string{}
	fset := token.NewFileSet()
	for _, entry := range entries {
		file := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(file, ".go") || strings.HasSuffix(file, "_test.go") {
			continue
		}

		parsed, err := parser.ParseFile(fset, file, nil, parser.SkipObjectResolution)
		require.NoError(t, err)

		for _, decl := range parsed.Decls {
			gen, ok := decl.(*ast.GenDecl)
			if !ok || gen.Tok != token.VAR {
				continue
			}
			for _, spec := range gen.Specs {
				for _, ident := range spec.(*ast.ValueSpec).Names {
					if ident.IsExported() && strings.HasPrefix(ident.Name, "Err") {
						names = append(names, ident.Name)
					}
				}
			}
		}
	}

	return names
}

func TestModuleIdentity_AllSentinelsPrefixed(t *testing.T) {
	t.Parallel()

	sentinels := map[string]error{
		"ErrInvalidOption":    ErrInvalidOption,
		"ErrUnsupportedClock": ErrUnsupportedClock,
	}

	covered := make([]string, 0, len(sentinels))
	for name := range sentinels {
		covered = append(covered, name)
	}
	require.ElementsMatch(t, exportedErrorVars(t), covered, "the table must list every exported Err* variable")

	for name, sentinel := range sentinels {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			is := assert.New(t)

			message := sentinel.Error()
			is.True(strings.HasPrefix(message, "doger: "), "%q must start with \"doger: \"", message)
			is.NotContains(message, oldLibPrefix)
			is.Equal(1, strings.Count(message, "doger: "), "%q must contain \"doger: \" once", message)
		})
	}
}
