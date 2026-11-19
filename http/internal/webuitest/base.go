package webuitest

import (
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// RootBasePath runs the base path "/" checks against mountRoot, which mounts
// the UI at the root of the server: every link and the scope redirect stay
// on the server ("/scope", never the protocol-relative "//scope"), and
// following a link opens its page.
func RootBasePath(t *testing.T, mountRoot Mount) {
	t.Helper()

	root := newDBRoot()
	do := mountRoot(t, root)

	home := Get(t, do, "/")
	assert.Equal(t, http.StatusOK, home.StatusCode, "home")

	redirect := Get(t, do, "/scope")
	require.Equal(t, http.StatusFound, redirect.StatusCode, "scope redirect")
	assert.Equal(t, "/scope?scope_id="+root.ID(), redirect.Header.Get("Location"))

	list := Get(t, do, "/service")
	require.Equal(t, http.StatusOK, list.StatusCode, "service list")

	for _, link := range Links(list.Body) {
		if strings.HasPrefix(link.Href, "https://") {
			continue
		}

		assert.True(t, strings.HasPrefix(link.Href, "/"), "link %q is not absolute", link.Href)
		assert.False(t, strings.HasPrefix(link.Href, "//"), "link %q leaves the server", link.Href)
	}

	page := Get(t, do, LinkByText(t, list.Body, "db").Href)
	assert.Equal(t, http.StatusOK, page.StatusCode)
	assert.Contains(t, page.Body, "Service name: db")
}
