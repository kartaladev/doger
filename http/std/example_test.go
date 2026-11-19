package dogerhttpstd_test

import (
	"crypto/subtle"
	"fmt"
	"net/http"
	"net/http/httptest"

	"github.com/kartaladev/doger"
	dogerhttpstd "github.com/kartaladev/doger/http/std"
)

// basicAuth accepts only user with password.
func basicAuth(user, password string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, p, ok := r.BasicAuth()
		if !ok || subtle.ConstantTimeCompare([]byte(u), []byte(user)) != 1 ||
			subtle.ConstantTimeCompare([]byte(p), []byte(password)) != 1 {
			http.Error(w, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// The debug UI is a plain http.Handler: wrap it in your own middleware.
func ExampleUse() {
	injector := doger.New()
	doger.ProvideNamedValue(injector, "db", "postgres://localhost")

	mux := http.NewServeMux()
	mux.Handle("/debug/doger/", basicAuth("admin", "s3cret", dogerhttpstd.Use("/debug/doger", injector)))

	anonymous := httptest.NewRecorder()
	mux.ServeHTTP(anonymous, httptest.NewRequest(http.MethodGet, "/debug/doger/service", http.NoBody))

	req := httptest.NewRequest(http.MethodGet, "/debug/doger/service", http.NoBody)
	req.SetBasicAuth("admin", "s3cret")
	admin := httptest.NewRecorder()
	mux.ServeHTTP(admin, req)

	fmt.Println(anonymous.Code, admin.Code, admin.Header().Get("X-Content-Type-Options"))
	// Output: 401 200 nosniff
}
