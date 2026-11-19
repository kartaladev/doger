package dogerhttpstd_test

import (
	"crypto/subtle"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"

	"github.com/kartaladev/doger"
	dogerhttpstd "github.com/kartaladev/doger/http/std"
)

// debugAuth returns a basic-auth middleware for the debug UI. It refuses
// empty credentials, so an unset variable never opens the UI, and compares
// in constant time. The Web UI docs show this function.
func debugAuth(user, password string) (func(http.Handler) http.Handler, error) {
	if user == "" || password == "" {
		return nil, errors.New("debug UI: set DEBUG_USER and DEBUG_PASSWORD")
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			u, p, ok := r.BasicAuth()
			if !ok ||
				subtle.ConstantTimeCompare([]byte(u), []byte(user)) != 1 ||
				subtle.ConstantTimeCompare([]byte(p), []byte(password)) != 1 {
				w.Header().Set("WWW-Authenticate", `Basic realm="debug"`)
				http.Error(w, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)

				return
			}

			next.ServeHTTP(w, r)
		})
	}, nil
}

// The debug UI is a plain http.Handler: wrap it in your own middleware.
func ExampleUse() {
	injector := doger.New()
	doger.ProvideNamedValue(injector, "db", "postgres://localhost")

	auth, err := debugAuth("admin", "s3cret") // os.Getenv("DEBUG_USER"), os.Getenv("DEBUG_PASSWORD")
	if err != nil {
		panic(err)
	}

	mux := http.NewServeMux()
	mux.Handle("/debug/doger/", auth(dogerhttpstd.Use("/debug/doger", injector)))

	anonymous := httptest.NewRecorder()
	mux.ServeHTTP(anonymous, httptest.NewRequest(http.MethodGet, "/debug/doger/service", http.NoBody))

	req := httptest.NewRequest(http.MethodGet, "/debug/doger/service", http.NoBody)
	req.SetBasicAuth("admin", "s3cret")
	admin := httptest.NewRecorder()
	mux.ServeHTTP(admin, req)

	fmt.Println(anonymous.Code, admin.Code, admin.Header().Get("X-Content-Type-Options"))
	// Output: 401 200 nosniff
}

// Unset credentials refuse to mount the UI instead of opening it.
func ExampleUse_emptyCredentials() {
	_, err := debugAuth("", "")
	fmt.Println(err)
	// Output: debug UI: set DEBUG_USER and DEBUG_PASSWORD
}
