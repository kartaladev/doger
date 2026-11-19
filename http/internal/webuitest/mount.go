package webuitest

import (
	"crypto/subtle"
	"fmt"
	"net/http"
	"net/url"
	"sync"
	"testing"

	"github.com/kartaladev/doger"
	"github.com/stretchr/testify/assert"
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
			assert.Equal(t, tt.status, resp.StatusCode)
		})
	}
}

// ConcurrentViewing runs scenario "Viewers race with container churn"
// against mount: 4 goroutines each create, invoke a service in and shut
// down 50 child scopes while 4 goroutines each send 50 GET requests across
// all pages. Run it under -race.
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

	var wg sync.WaitGroup
	for w := range workers {
		wg.Go(func() {
			for i := range iterations {
				child := root.Scope(fmt.Sprintf("churn-%d-%d", w, i))
				doger.ProvideNamed(child, "req", func(doger.Injector) (string, error) { return "req", nil })
				_, err := doger.InvokeNamed[string](child, "req")
				assert.NoError(t, err)
				assert.True(t, child.Shutdown().Succeed)
			}
		})
		wg.Go(func() {
			for i := range iterations {
				path := paths[(w+i)%len(paths)]
				resp := Get(t, do, path)
				assert.Contains(t, []int{http.StatusOK, http.StatusFound}, resp.StatusCode, path)
			}
		})
	}
	wg.Wait()

	assert.True(t, root.Shutdown().Succeed)

	resp := Get(t, do, BasePath+"/service")
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}
