package webuitest

import (
	"crypto/subtle"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"

	"github.com/kartaladev/doger"
)

// The credentials the basic-auth middleware of the scenarios accepts.
const (
	AuthUser     = "admin"
	AuthPassword = "s3cret"
)

// BasicAuth is a net/http basic-auth middleware that accepts AuthUser with
// AuthPassword and answers anything else with 401.
func BasicAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, password, ok := r.BasicAuth()
		if !ok ||
			subtle.ConstantTimeCompare([]byte(user), []byte(AuthUser)) != 1 ||
			subtle.ConstantTimeCompare([]byte(password), []byte(AuthPassword)) != 1 {
			w.Header().Set("WWW-Authenticate", `Basic realm="debug"`)
			http.Error(w, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)

			return
		}

		next.ServeHTTP(w, r)
	})
}

// BehindAuthMiddleware runs scenario "Authentication middleware guards the
// UI" against mount, which places the UI behind a basic-auth middleware
// accepting AuthUser with AuthPassword.
func BehindAuthMiddleware(t *testing.T, mount Mount) {
	t.Helper()

	tests := []struct {
		name     string
		user     string
		password string
		status   int
	}{
		{name: "none", status: http.StatusUnauthorized},
		{name: "wrong_password", user: AuthUser, password: "wrong", status: http.StatusUnauthorized},
		{name: "admin", user: AuthUser, password: AuthPassword, status: http.StatusOK},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			req := NewRequest(t, http.MethodGet, BasePath+"/service")
			if tt.user != "" {
				req.SetBasicAuth(tt.user, tt.password)
			}

			resp := Do(t, mount(t, newDBRoot()), req)
			checkEqual(t, tt.status, resp.StatusCode)
		})
	}
}

// ConcurrentViewing runs scenario "Viewers race with container churn"
// against mount: 4 goroutines each create, invoke a service in and shut
// down 50 child scopes while 4 goroutines each send 50 GET requests across
// all pages. Each churning service depends on the root's "db", so the page
// of "db" lists dependents that are being added and shut down meanwhile.
// Run it under -race.
//
// The workers never stop the test: they collect their failures, and the
// test goroutine reports them.
func ConcurrentViewing(t *testing.T, mount Mount) {
	t.Helper()

	const (
		workers    = 4
		iterations = 50
	)

	root := newDBRoot()
	do := mount(t, root)
	paths := []string{
		BasePath,
		BasePath + "/scope",
		BasePath + "/scope?scope_id=" + url.QueryEscape(root.ID()),
		BasePath + "/service",
		ServicePath(root.ID(), "db"),
	}

	var (
		mu       sync.Mutex
		failures []string
	)
	failf := func(format string, args ...any) {
		mu.Lock()
		defer mu.Unlock()
		failures = append(failures, fmt.Sprintf(format, args...))
	}

	var wg sync.WaitGroup
	for w := range workers {
		wg.Go(func() {
			for i := range iterations {
				child := root.Scope(fmt.Sprintf("churn-%d-%d", w, i))
				doger.ProvideNamed(child, "req", func(i doger.Injector) (string, error) {
					return doger.InvokeNamed[string](i, "db")
				})
				if _, err := doger.InvokeNamed[string](child, "req"); err != nil {
					failf("invoke churn-%d-%d/req: %v", w, i, err)
				}
				if report := child.Shutdown(); !report.Succeed {
					failf("shut down churn-%d-%d: %v", w, i, report)
				}
			}
		})
		wg.Go(func() {
			for i := range iterations {
				path := paths[(w+i)%len(paths)]
				status, err := fetch(do, path)
				if err != nil {
					failf("GET %s: %v", path, err)
					continue
				}
				if status != http.StatusOK && status != http.StatusFound {
					failf("GET %s: status %d, want 200 or 302", path, status)
				}
			}
		})
	}
	wg.Wait()

	for _, failure := range failures {
		t.Error(failure)
	}

	checkTrue(t, root.Shutdown().Succeed)

	resp := Get(t, do, BasePath+"/service")
	checkEqual(t, http.StatusOK, resp.StatusCode)
}

// fetch sends GET target with do and returns the status. It reads the body
// in full and never touches a *testing.T, so any goroutine may call it.
func fetch(do Doer, target string) (int, error) {
	u, err := url.Parse(target)
	if err != nil {
		return 0, err
	}

	req := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
	req.URL = u
	req.RequestURI = u.RequestURI()

	resp := do(req)
	if resp == nil {
		return 0, errors.New("no response")
	}
	defer resp.Body.Close()

	if _, err := io.Copy(io.Discard, resp.Body); err != nil {
		return 0, err
	}

	return resp.StatusCode, nil
}
