package dogerhttp

import (
	"html/template"
	"regexp"
	"strings"
	"testing"

	"github.com/kartaladev/doger"
	"github.com/kartaladev/doger/http/internal/webuitest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var htmlTitleRe = regexp.MustCompile(`<title>(.*?)</title>`)

// htmlTitle returns the text of the first <title> element of page.
func htmlTitle(t *testing.T, page string) string {
	t.Helper()

	m := htmlTitleRe.FindStringSubmatch(page)
	require.Len(t, m, 2, "page has no <title>")

	return m[1]
}

// renderPage renders one of the debug UI pages for a root scope with the
// value "db-value" registered as "db-service".
func renderPage(t *testing.T, page string) string {
	t.Helper()

	const basePath = "/debug/do"
	root := doger.New()
	doger.ProvideNamedValue(root, "db-service", "db-value")

	var (
		html string
		err  error
	)
	switch page {
	case "index":
		html, err = IndexHTML(basePath)
	case "scope_tree":
		html, err = ScopeTreeHTML(basePath, root, "")
	case "service_list":
		html, err = ServiceListHTML(basePath, root)
	case "service_detail":
		html, err = ServiceHTML(basePath, root, root.ID(), "db-service")
	default:
		require.FailNowf(t, "unknown page", "%q", page)
	}
	require.NoError(t, err)

	return html
}

func TestIndexHTML(t *testing.T) {
	t.Parallel()
	is := assert.New(t)

	basePath := "/debug/di"
	html, err := IndexHTML(basePath)
	is.NoError(err)
	is.NotEmpty(html)
	is.Contains(html, "Welcome to doger UI")
	is.Equal("Dependency injection UI - doger", htmlTitle(t, html))
	is.Contains(html, basePath+"/scope")
	is.Contains(html, basePath+"/service")
}

func TestIndexHTML_Branding(t *testing.T) {
	t.Parallel()

	tests := []struct {
		label string
		href  string
	}{
		{label: "Repository", href: "https://github.com/kartaladev/doger"},
		{label: "New issue", href: "https://github.com/kartaladev/doger/issues"},
		{label: "Documentation", href: "https://kartaladev.github.io/doger/"},
		{label: "Godoc", href: "https://pkg.go.dev/github.com/kartaladev/doger"},
		{label: "Changelog", href: "https://github.com/kartaladev/doger/releases"},
		{label: "License", href: "https://github.com/kartaladev/doger/blob/main/LICENSE"},
		{label: "Upstream project", href: "https://github.com/samber/do"},
	}

	html, err := IndexHTML("/debug/do")
	require.NoError(t, err)

	for _, tt := range tests {
		t.Run(tt.label, func(t *testing.T) {
			t.Parallel()

			link := `<a href="` + regexp.QuoteMeta(tt.href) + `"[^>]*>` + regexp.QuoteMeta(tt.label) + `</a>`
			assert.Regexp(t, link, html)
		})
	}
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

func TestPagesHTML_Title(t *testing.T) {
	t.Parallel()

	tests := []struct {
		page  string
		title string
	}{
		{page: "scope_tree", title: "Inspect scope tree - doger"},
		{page: "service_list", title: "Inspect service - doger"},
		{page: "service_detail", title: "Inspect service - doger"},
	}

	for _, tt := range tests {
		t.Run(tt.page, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.title, htmlTitle(t, renderPage(t, tt.page)))
		})
	}
}

func TestPagesHTML_UpstreamAttributionOnly(t *testing.T) {
	t.Parallel()

	tests := []struct {
		page  string
		count int
	}{
		{page: "index", count: 1},
		{page: "scope_tree", count: 0},
		{page: "service_list", count: 0},
		{page: "service_detail", count: 0},
	}

	for _, tt := range tests {
		t.Run(tt.page, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.count, strings.Count(renderPage(t, tt.page), "samber/do"))
		})
	}
}

func TestPages_EscapeNames(t *testing.T) {
	t.Parallel()

	const basePath = "/debug/doger"

	tests := []struct {
		name     string
		function string
		service  string
		escaped  string
	}{
		{
			name:     "ServiceListHTML/script",
			function: "ServiceListHTML",
			service:  "<script>alert(1)</script>",
			escaped:  "&lt;script&gt;alert(1)&lt;/script&gt;",
		},
		{
			name:     "ScopeTreeHTML/script",
			function: "ScopeTreeHTML",
			service:  "<script>alert(1)</script>",
			escaped:  "&lt;script&gt;alert(1)&lt;/script&gt;",
		},
		{
			name:     "ServiceHTML/img_onerror",
			function: "ServiceHTML",
			service:  `"><img src=x onerror=alert(1)>`,
			escaped:  "&#34;&gt;&lt;img src=x onerror=alert(1)&gt;",
		},
		{
			name:     "ServiceListHTML/plain",
			function: "ServiceListHTML",
			service:  "plain-name",
			escaped:  "plain-name",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			is := assert.New(t)

			root := doger.New()
			doger.ProvideNamed(root, tt.service, func(doger.Injector) (string, error) { return "v", nil })

			var (
				html string
				err  error
			)
			switch tt.function {
			case "ServiceListHTML":
				html, err = ServiceListHTML(basePath, root)
			case "ScopeTreeHTML":
				html, err = ScopeTreeHTML(basePath, root, root.ID())
			case "ServiceHTML":
				html, err = ServiceHTML(basePath, root, root.ID(), tt.service)
			}

			require.NoError(t, err)
			if tt.service != tt.escaped {
				is.NotContains(html, tt.service)
			}
			is.Contains(html, tt.escaped)
			webuitest.AssertNoInjectedMarkup(t, html)
		})
	}
}

func TestPages_ShortenSourcePath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		recorded string
		shown    string
	}{
		{name: "unix", recorded: "/home/ci/work/app/main.go:42", shown: "main.go:42"},
		{name: "windows", recorded: `C:\Users\ci\app\main.go:42`, shown: "main.go:42"},
		{name: "base_name", recorded: "main.go:42", shown: "main.go:42"},
		{name: "empty", recorded: "", shown: ""},
		{name: "hostile", recorded: "/a/<script>.go:7", shown: "&lt;script&gt;.go:7"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.shown, template.HTMLEscapeString(shortenSourcePath(tt.recorded)))
		})
	}
}

func FuzzShortenSourcePath(f *testing.F) {
	for _, seed := range []string{
		"/home/ci/work/app/main.go:42", `C:\Users\ci\app\main.go:42`, "main.go:42", "", "/a/<script>.go:7", "/", `\`, "a/b\\c:1",
	} {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, recorded string) {
		shown := shortenSourcePath(recorded)

		assert.True(t, strings.HasSuffix(recorded, shown), "%q is not a suffix of %q", shown, recorded)
		assert.NotContains(t, shown, "/")
		assert.NotContains(t, shown, `\`)
		if !strings.ContainsAny(recorded, `/\`) {
			assert.Equal(t, recorded, shown)
		}
	})
}

func TestPages_NeverInvokedNoLocation(t *testing.T) {
	t.Parallel()

	root := doger.New()
	doger.ProvideNamed(root, "db", func(doger.Injector) (string, error) { return "v", nil })

	html, err := ServiceHTML("/debug/doger", root, root.ID(), "db")
	require.NoError(t, err)

	assert.Empty(t, webuitest.InvokedAt(t, html))
}

func TestPages_BasePathEscaped(t *testing.T) {
	t.Parallel()

	html, err := IndexHTML(`/x"><script>alert(1)</script>`)
	require.NoError(t, err)

	webuitest.AssertNoInjectedMarkup(t, html)

	internal := 0
	for _, link := range webuitest.Links(html) {
		if strings.HasPrefix(link.Href, "https://") {
			continue // the static links to the project's resources
		}

		internal++
		assert.True(t, strings.HasPrefix(link.Href, "/x%22%3E%3Cscript%3Ealert%281%29%3C/script%3E"),
			"link %q does not start with the escaped base path", link.Href)
	}
	assert.Equal(t, 2, internal, "the scope and service links")
}
