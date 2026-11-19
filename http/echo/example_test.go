package dogerecho_test

import (
	"crypto/subtle"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"

	"github.com/kartaladev/doger"
	dogerecho "github.com/kartaladev/doger/http/echo"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
)

// debugAuth returns a basic-auth middleware for the debug UI. It refuses
// empty credentials, so an unset variable never opens the UI, and compares
// in constant time. The Web UI docs show this function.
func debugAuth(user, password string) (echo.MiddlewareFunc, error) {
	if user == "" || password == "" {
		return nil, errors.New("debug UI: set DEBUG_USER and DEBUG_PASSWORD")
	}

	return middleware.BasicAuth(func(u, p string, _ echo.Context) (bool, error) {
		return subtle.ConstantTimeCompare([]byte(u), []byte(user)) == 1 &&
			subtle.ConstantTimeCompare([]byte(p), []byte(password)) == 1, nil
	}), nil
}

// Pass your middleware to the group the UI is mounted on.
func ExampleUse() {
	injector := doger.New()
	doger.ProvideNamedValue(injector, "db", "postgres://localhost")

	auth, err := debugAuth("admin", "s3cret") // os.Getenv("DEBUG_USER"), os.Getenv("DEBUG_PASSWORD")
	if err != nil {
		panic(err)
	}

	e := echo.New()
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

// Unset credentials refuse to mount the UI instead of opening it.
func ExampleUse_emptyCredentials() {
	_, err := debugAuth("", "")
	fmt.Println(err)
	// Output: debug UI: set DEBUG_USER and DEBUG_PASSWORD
}
