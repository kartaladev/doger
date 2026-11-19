package dogerecho_test

import (
	"crypto/subtle"
	"fmt"
	"net/http"
	"net/http/httptest"

	"github.com/kartaladev/doger"
	dogerecho "github.com/kartaladev/doger/http/echo"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
)

// Pass your middleware to the group the UI is mounted on.
func ExampleUse() {
	injector := doger.New()
	doger.ProvideNamedValue(injector, "db", "postgres://localhost")

	e := echo.New()
	auth := middleware.BasicAuth(func(user, password string, _ echo.Context) (bool, error) {
		return subtle.ConstantTimeCompare([]byte(user), []byte("admin")) == 1 &&
			subtle.ConstantTimeCompare([]byte(password), []byte("s3cret")) == 1, nil
	})
	dogerecho.Use(e.Group("/debug/doger", auth), "/debug/doger", injector)

	anonymous := httptest.NewRecorder()
	e.ServeHTTP(anonymous, httptest.NewRequest(http.MethodGet, "/debug/doger/service", http.NoBody))

	req := httptest.NewRequest(http.MethodGet, "/debug/doger/service", http.NoBody)
	req.SetBasicAuth("admin", "s3cret")
	admin := httptest.NewRecorder()
	e.ServeHTTP(admin, req)

	fmt.Println(anonymous.Code, admin.Code)
	// Output: 401 200
}
