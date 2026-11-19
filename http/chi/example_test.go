package dogerchi_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/kartaladev/doger"
	dogerchi "github.com/kartaladev/doger/http/chi"
)

// Use accepts any chi.Router: mount the UI on r.With(auth) (or inside
// r.Group) to put it behind your middleware.
func ExampleUse() {
	injector := doger.New()
	doger.ProvideNamedValue(injector, "db", "postgres://localhost")

	r := chi.NewRouter()
	auth := middleware.BasicAuth("debug", map[string]string{"admin": "s3cret"})
	dogerchi.Use(r.With(auth), "/debug/doger", injector)

	anonymous := httptest.NewRecorder()
	r.ServeHTTP(anonymous, httptest.NewRequest(http.MethodGet, "/debug/doger/service", http.NoBody))

	req := httptest.NewRequest(http.MethodGet, "/debug/doger/service", http.NoBody)
	req.SetBasicAuth("admin", "s3cret")
	admin := httptest.NewRecorder()
	r.ServeHTTP(admin, req)

	fmt.Println(anonymous.Code, admin.Code)
	// Output: 401 200
}
