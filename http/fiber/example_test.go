package dogerfiber_test

import (
	"crypto/subtle"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/basicauth"
	"github.com/kartaladev/doger"
	dogerfiber "github.com/kartaladev/doger/http/fiber"
)

// debugAuth returns a basic-auth middleware for the debug UI. It refuses
// empty credentials, so an unset variable never opens the UI, and compares
// in constant time. The Web UI docs show this function.
func debugAuth(user, password string) (fiber.Handler, error) {
	if user == "" || password == "" {
		return nil, errors.New("debug UI: set DEBUG_USER and DEBUG_PASSWORD")
	}

	return basicauth.New(basicauth.Config{
		Authorizer: func(u, p string) bool {
			return subtle.ConstantTimeCompare([]byte(u), []byte(user)) == 1 &&
				subtle.ConstantTimeCompare([]byte(p), []byte(password)) == 1
		},
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

	app := fiber.New(fiber.Config{DisableStartupMessage: true})
	dogerfiber.Use(app.Group("/debug/doger", auth), "/debug/doger", injector)

	anonymous, err := app.Test(httptest.NewRequest(http.MethodGet, "/debug/doger/service", http.NoBody))
	if err != nil {
		panic(err)
	}
	defer anonymous.Body.Close()

	empty := httptest.NewRequest(http.MethodGet, "/debug/doger/service", http.NoBody)
	empty.SetBasicAuth("", "")
	emptyCredentials, err := app.Test(empty)
	if err != nil {
		panic(err)
	}
	defer emptyCredentials.Body.Close()

	req := httptest.NewRequest(http.MethodGet, "/debug/doger/service", http.NoBody)
	req.SetBasicAuth("admin", "s3cret")
	admin, err := app.Test(req)
	if err != nil {
		panic(err)
	}
	defer admin.Body.Close()

	fmt.Println(anonymous.StatusCode, emptyCredentials.StatusCode, admin.StatusCode)
	// Output: 401 401 200
}

// Unset credentials refuse to mount the UI instead of opening it.
func ExampleUse_emptyCredentials() {
	_, err := debugAuth("", "")
	fmt.Println(err)
	// Output: debug UI: set DEBUG_USER and DEBUG_PASSWORD
}
