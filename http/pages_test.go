package dogerhttp

import (
	"strings"
	"testing"

	"github.com/kartaladev/doger"
	"github.com/stretchr/testify/assert"
)

func TestIndexHTML(t *testing.T) {
	t.Parallel()
	is := assert.New(t)

	basePath := "/debug/di"
	html, err := IndexHTML(basePath)
	is.NoError(err)
	is.NotEmpty(html)
	is.Contains(html, "Welcome to do UI")
	is.Contains(html, basePath+"/scope")
	is.Contains(html, basePath+"/service")
}

func TestIndexHTML_BasePathLinks(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		base string
	}{
		{name: "debug_do", base: "/debug/do"},
		{name: "admin_di", base: "/admin/di"},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			is := assert.New(t)

			html, err := IndexHTML(tt.base)
			is.NoError(err)
			is.Contains(html, `href="`+tt.base+`/scope"`)
			is.Contains(html, `href="`+tt.base+`/service"`)
		})
	}
}

func TestScopeTreeHTML_Basic(t *testing.T) {
	t.Parallel()
	is := assert.New(t)

	basePath := "/debug/di"
	root := doger.New()
	child := root.Scope("child")

	doger.ProvideNamedValue(root, "db-service", "db-value")
	doger.ProvideNamedTransient(root, "request-id", func(i doger.Injector) (string, error) { return "rid", nil })
	doger.ProvideNamedValue(child, "cache-service", 123)

	html, err := ScopeTreeHTML(basePath, root, "")
	is.NoError(err)
	is.NotEmpty(html)
	is.Contains(html, "Scope description")
	// Links to scopes and services should be present
	is.Contains(html, basePath+"/scope")
	is.True(strings.Contains(html, "db-service") || strings.Contains(html, "cache-service"))
}

func TestServiceListHTML_Basic(t *testing.T) {
	t.Parallel()
	is := assert.New(t)

	basePath := "/debug/di"
	root := doger.New()
	_ = root // ensure non-nil
	doger.ProvideNamedValue(root, "cfg", "x")

	html, err := ServiceListHTML(basePath, root)
	is.NoError(err)
	is.NotEmpty(html)
	is.Contains(html, "Service description")
	is.Contains(html, basePath+"/service")
}

func TestServiceHTML_Basic(t *testing.T) {
	t.Parallel()
	is := assert.New(t)

	basePath := "/debug/di"
	root := doger.New()
	doger.ProvideNamedValue(root, "cfg", "x")

	html, err := ServiceHTML(basePath, root, root.ID(), "cfg")
	is.NoError(err)
	is.NotEmpty(html)
	is.Contains(html, "Scope id:")
	is.Contains(html, "Service name: cfg")
	// eager value should report eager type
	is.Contains(html, "Service type: eager")
}
