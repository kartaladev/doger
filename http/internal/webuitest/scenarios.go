package webuitest

import (
	"net/http"
	"net/url"
	"testing"

	"github.com/kartaladev/doger"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// HostileName is a name that would inject markup if rendered unescaped,
// with the form the page must show instead.
type HostileName struct {
	Label   string
	Name    string
	Escaped string
}

// HostileNames are the hostile names of the escaping scenarios.
var HostileNames = []HostileName{
	{
		Label:   "script_tag",
		Name:    "<script>alert(1)</script>",
		Escaped: "&lt;script&gt;alert(1)&lt;/script&gt;",
	},
	{
		Label:   "img_onerror",
		Name:    `"><img src=x onerror=alert(1)>`,
		Escaped: "&#34;&gt;&lt;img src=x onerror=alert(1)&gt;",
	},
}

// ServicePath returns the path of the service page of name in scopeID.
func ServicePath(scopeID, name string) string {
	return BasePath + "/service?" + url.Values{
		"scope_id":     {scopeID},
		"service_name": {name},
	}.Encode()
}

// HostileGraph returns a root holding a lazy service "dep-<name>" and a child
// scope named name holding a lazy service named name that depends on it.
// The service was invoked once.
func HostileGraph(t *testing.T, name string) (*doger.RootScope, *doger.Scope) {
	t.Helper()

	root := doger.New()
	doger.ProvideNamed(root, "dep-"+name, func(doger.Injector) (string, error) {
		return "dep", nil
	})

	child := root.Scope(name)
	doger.ProvideNamed(child, name, func(i doger.Injector) (string, error) {
		return doger.InvokeNamed[string](i, "dep-"+name)
	})

	_, err := doger.InvokeNamed[string](child, name)
	require.NoError(t, err)

	return root, child
}

// HostileNamesEscaped runs scenario "Hostile names are escaped on every page"
// against mount.
func HostileNamesEscaped(t *testing.T, mount Mount) {
	t.Helper()

	for _, hostile := range HostileNames {
		pages := []struct {
			name string
			path func(root, child doger.Injector) string
		}{
			{name: "scope_tree", path: func(root, _ doger.Injector) string {
				return BasePath + "/scope?scope_id=" + url.QueryEscape(root.ID())
			}},
			{name: "service_list", path: func(_, _ doger.Injector) string {
				return BasePath + "/service"
			}},
			{name: "service_page", path: func(_, child doger.Injector) string {
				return ServicePath(child.ID(), hostile.Name)
			}},
		}

		for _, page := range pages {
			t.Run(hostile.Label+"/"+page.name, func(t *testing.T) {
				t.Parallel()

				root, child := HostileGraph(t, hostile.Name)
				resp := Get(t, mount(t, root), page.path(root, child))

				assert.Equal(t, http.StatusOK, resp.StatusCode)
				assert.NotContains(t, resp.Body, hostile.Name)
				assert.Contains(t, resp.Body, hostile.Escaped)
				AssertNoInjectedMarkup(t, resp.Body)
			})
		}
	}
}

// LinkedName is a service name with its query-encoded form.
type LinkedName struct {
	Label   string
	Name    string
	Encoded string
	Escaped string
}

// plain is a name that every escaping leaves unchanged.
const plain = "plain"

// LinkedNames are the names of scenario "A link opens the service it names".
var LinkedNames = []LinkedName{
	{Label: "amp_hash", Name: "a&b#c", Encoded: "a%26b%23c", Escaped: "a&amp;b#c"},
	{
		Label:   "script_tag",
		Name:    "<script>alert(1)</script>",
		Encoded: "%3Cscript%3Ealert%281%29%3C%2Fscript%3E",
		Escaped: "&lt;script&gt;alert(1)&lt;/script&gt;",
	},
	{Label: "space", Name: "with space", Encoded: "with+space", Escaped: "with space"},
	{Label: plain, Name: plain, Encoded: plain, Escaped: plain},
}

// LinkOpensNamedService runs scenario "A link opens the service it names"
// against mount.
func LinkOpensNamedService(t *testing.T, mount Mount) {
	t.Helper()

	for _, tt := range LinkedNames {
		t.Run(tt.Label, func(t *testing.T) {
			t.Parallel()

			root := doger.New()
			doger.ProvideNamed(root, tt.Name, func(doger.Injector) (string, error) { return "v", nil })
			do := mount(t, root)

			list := Get(t, do, BasePath+"/service").Body
			link := LinkByText(t, list, tt.Name)

			target, err := url.Parse(link.Href)
			require.NoError(t, err)
			assert.Contains(t, target.RawQuery, "service_name="+tt.Encoded)

			resp := Get(t, do, link.Href)
			assert.Equal(t, http.StatusOK, resp.StatusCode)
			assert.Contains(t, resp.Body, "Service name: "+tt.Escaped)
		})
	}
}

// LinksResolve runs scenario "Links resolve on every adapter" against mount.
func LinksResolve(t *testing.T, mount Mount) {
	t.Helper()

	root := doger.New()
	doger.ProvideNamed(root, "a&b#c", func(doger.Injector) (string, error) { return "v", nil })
	do := mount(t, root)

	list := Get(t, do, BasePath+"/service").Body
	link := LinkByText(t, list, "a&b#c")

	resp := Get(t, do, link.Href)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Contains(t, resp.Body, "Service name: a&amp;b#c")
	assert.NotContains(t, resp.Body, "<h1>Service description</h1>", "the link fell back to the service list")
}

// NoReflection runs scenario "Query values are never reflected" against
// mount.
func NoReflection(t *testing.T, mount Mount) {
	t.Helper()

	root := doger.New()
	doger.ProvideNamed(root, "db", func(doger.Injector) (string, error) { return "v", nil })

	resp := Get(t, mount(t, root),
		BasePath+"/service?scope_id=%3Cscript%3Ex%3C%2Fscript%3E&service_name=%3Cimg%20src%3Dx%3E")

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.NotContains(t, resp.Body, "<script>x</script>")
	assert.NotContains(t, resp.Body, "&lt;script&gt;x")
	assert.NotContains(t, resp.Body, "<img src=x>")
}
