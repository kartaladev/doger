package dogerchi_test

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"

	"github.com/go-chi/chi/v5"
	"github.com/kartaladev/doger"
	dogerchi "github.com/kartaladev/doger/http/chi"
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

// Use accepts any chi.Router: mount the UI on r.With(auth) (or inside
// r.Group) to put it behind your middleware.
func ExampleUse() {
	injector := doger.New()
	doger.ProvideNamedValue(injector, "db", "postgres://localhost")

	auth, err := debugAuth("admin", "s3cret") // os.Getenv("DEBUG_USER"), os.Getenv("DEBUG_PASSWORD")
	if err != nil {
		panic(err)
	}

	r := chi.NewRouter()
	dogerchi.Use(r.With(auth), "/debug/doger", injector)

	anonymous := httptest.NewRecorder()
	r.ServeHTTP(anonymous, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/debug/doger/service", http.NoBody))

	empty := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/debug/doger/service", http.NoBody)
	empty.SetBasicAuth("", "")
	emptyCredentials := httptest.NewRecorder()
	r.ServeHTTP(emptyCredentials, empty)

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/debug/doger/service", http.NoBody)
	req.SetBasicAuth("admin", "s3cret")
	admin := httptest.NewRecorder()
	r.ServeHTTP(admin, req)

	fmt.Println(anonymous.Code, emptyCredentials.Code, admin.Code)
	// Output: 401 401 200
}

// Unset credentials refuse to mount the UI instead of opening it.
func ExampleUse_emptyCredentials() {
	_, err := debugAuth("", "")
	fmt.Println(err)
	// Output: debug UI: set DEBUG_USER and DEBUG_PASSWORD
}
