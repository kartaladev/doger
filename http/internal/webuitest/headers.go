package webuitest

import (
	"net/http"
	"net/url"
	"sync/atomic"
	"testing"

	"github.com/kartaladev/doger"
)

// The defensive headers every UI response carries.
const (
	ContentTypeHTML       = "text/html; charset=utf-8"
	ContentTypeText       = "text/plain; charset=utf-8"
	ContentSecurityPolicy = "default-src 'none'; style-src 'unsafe-inline'; frame-ancestors 'none'"
	NoSniff               = "nosniff"
	NoStore               = "no-store"
)

// RenderFailure is the message of the render failure of the error scenarios;
// RenderFailurePath is the detail of it no response may reveal.
const (
	RenderFailure     = "template: secret path " + RenderFailurePath
	RenderFailurePath = "/srv/app/x.go"
)

// AssertDefensiveHeaders asserts that header carries the defensive headers
// other than Content-Type.
func AssertDefensiveHeaders(t *testing.T, header http.Header) {
	t.Helper()

	checkEqual(t, NoSniff, header.Get("X-Content-Type-Options"))
	checkEqual(t, ContentSecurityPolicy, header.Get("Content-Security-Policy"))
	checkEqual(t, NoStore, header.Get("Cache-Control"))
}

// newDBRoot returns a root holding a lazy service "db".
func newDBRoot() *doger.RootScope {
	root := doger.New()
	doger.ProvideNamed(root, "db", func(doger.Injector) (string, error) { return "v", nil })

	return root
}

// DefensiveHeaders runs scenario "Headers are set on every route" against
// mount.
func DefensiveHeaders(t *testing.T, mount Mount) {
	t.Helper()

	tests := []struct {
		name   string
		path   func(root doger.Injector) string
		status int
	}{
		{name: "home", path: func(doger.Injector) string { return BasePath }, status: http.StatusOK},
		{name: "scope_redirect", path: func(doger.Injector) string { return BasePath + "/scope" }, status: http.StatusFound},
		{name: "scope_tree", path: func(root doger.Injector) string {
			return BasePath + "/scope?scope_id=" + url.QueryEscape(root.ID())
		}, status: http.StatusOK},
		{name: "service_list", path: func(doger.Injector) string { return BasePath + "/service" }, status: http.StatusOK},
		{name: "service_page", path: func(root doger.Injector) string {
			return ServicePath(root.ID(), "db")
		}, status: http.StatusOK},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			root := newDBRoot()
			resp := Get(t, mount(t, root), tt.path(root))

			requireEqual(t, tt.status, resp.StatusCode)
			AssertDefensiveHeaders(t, resp.Header)
			if tt.status == http.StatusOK {
				checkEqual(t, ContentTypeHTML, resp.Header.Get("Content-Type"))
			}
		})
	}
}

// NonGETRejected runs scenario "Non-GET methods are rejected" against mount.
func NonGETRejected(t *testing.T, mount Mount) {
	t.Helper()

	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodDelete} {
		t.Run(method, func(t *testing.T) {
			t.Parallel()

			resp := Do(t, mount(t, newDBRoot()), NewRequest(t, method, BasePath+"/service"))

			checkTrue(t, resp.StatusCode >= 400 && resp.StatusCode < 500, "status %d, want 4xx", resp.StatusCode)
			checkNotContains(t, resp.Body, "<!DOCTYPE html>")
		})
	}
}

// countingDB counts the lifecycle calls it receives.
type countingDB struct {
	healthChecks *atomic.Int32
	shutdowns    *atomic.Int32
}

func (d *countingDB) HealthCheck() error {
	d.healthChecks.Add(1)
	return nil
}

func (d *countingDB) Shutdown() {
	d.shutdowns.Add(1)
}

// ReadOnly runs scenario "Viewing every page builds nothing" against mount.
func ReadOnly(t *testing.T, mount Mount) {
	t.Helper()

	var provides, healthChecks, shutdowns atomic.Int32

	root := doger.New()
	doger.ProvideNamed(root, "db", func(doger.Injector) (*countingDB, error) {
		provides.Add(1)
		return &countingDB{healthChecks: &healthChecks, shutdowns: &shutdowns}, nil
	})
	tenant := root.Scope("tenant")
	doger.ProvideNamedTransient(tenant, "req", func(doger.Injector) (string, error) {
		provides.Add(1)
		return "req", nil
	})

	servicesBefore := root.ListProvidedServices()
	childrenBefore := len(root.Children())
	do := mount(t, root)

	paths := []string{
		BasePath,
		BasePath + "/scope?scope_id=" + url.QueryEscape(root.ID()),
		BasePath + "/service",
		ServicePath(root.ID(), "db"),
		ServicePath(tenant.ID(), "req"),
	}
	for _, path := range paths {
		resp := Get(t, do, path)
		checkStatusIn(t, resp.StatusCode, []int{http.StatusOK, http.StatusFound}, path)
	}

	checkEqual(t, int32(0), provides.Load(), "providers called")
	checkEqual(t, int32(0), healthChecks.Load(), "health checks")
	checkEqual(t, int32(0), shutdowns.Load(), "shutdowns")
	checkTrue(t, sameServices(servicesBefore, root.ListProvidedServices()), "provided services changed")
	checkEqual(t, childrenBefore, len(root.Children()), "child scopes")
}

// Fallbacks runs scenario "Fallbacks for missing and unknown identifiers"
// against mount.
func Fallbacks(t *testing.T, mount Mount) {
	t.Helper()

	const (
		serviceList = "<h1>Service description</h1>"
		scopeTree   = "<h1>Scope description</h1>"
	)

	tests := []struct {
		name     string
		path     func(rootID string) string
		status   int
		location bool
		heading  string
	}{
		{name: "scope_without_id", path: func(string) string { return BasePath + "/scope" }, status: http.StatusFound, location: true},
		{name: "scope_empty_id", path: func(string) string { return BasePath + "/scope?scope_id=" }, status: http.StatusFound, location: true},
		{name: "scope_unknown_id", path: func(string) string { return BasePath + "/scope?scope_id=unknown" }, status: http.StatusOK, heading: scopeTree},
		{name: "service_without_name", path: func(id string) string {
			return BasePath + "/service?scope_id=" + url.QueryEscape(id)
		}, status: http.StatusOK, heading: serviceList},
		{name: "service_without_scope", path: func(string) string { return BasePath + "/service?service_name=db" }, status: http.StatusOK, heading: serviceList},
		{name: "service_unknown_scope", path: func(string) string {
			return BasePath + "/service?scope_id=unknown&service_name=db"
		}, status: http.StatusOK, heading: serviceList},
		{name: "service_missing", path: func(id string) string { return ServicePath(id, "missing") }, status: http.StatusOK, heading: serviceList},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			root := newDBRoot()
			resp := Get(t, mount(t, root), tt.path(root.ID()))

			requireEqual(t, tt.status, resp.StatusCode)
			if tt.location {
				checkEqual(t, BasePath+"/scope?scope_id="+root.ID(), resp.Header.Get("Location"))
			}
			if tt.heading != "" {
				checkContains(t, resp.Body, tt.heading)
			}
			if tt.heading == scopeTree {
				checkContains(t, resp.Body, "[root]", "the whole scope tree")
				checkContains(t, resp.Body, "db", "the whole scope tree")
			}
		})
	}
}
