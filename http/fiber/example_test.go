package dogerfiber_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/basicauth"
	"github.com/kartaladev/doger"
	dogerfiber "github.com/kartaladev/doger/http/fiber"
)

// Pass your middleware to the group the UI is mounted on.
func ExampleUse() {
	injector := doger.New()
	doger.ProvideNamedValue(injector, "db", "postgres://localhost")

	app := fiber.New(fiber.Config{DisableStartupMessage: true})
	auth := basicauth.New(basicauth.Config{Users: map[string]string{"admin": "s3cret"}})
	dogerfiber.Use(app.Group("/debug/doger", auth), "/debug/doger", injector)

	anonymous, err := app.Test(httptest.NewRequest(http.MethodGet, "/debug/doger/service", http.NoBody))
	if err != nil {
		panic(err)
	}
	defer anonymous.Body.Close()

	req := httptest.NewRequest(http.MethodGet, "/debug/doger/service", http.NoBody)
	req.SetBasicAuth("admin", "s3cret")
	admin, err := app.Test(req)
	if err != nil {
		panic(err)
	}
	defer admin.Body.Close()

	fmt.Println(anonymous.StatusCode, admin.StatusCode)
	// Output: 401 200
}
