package dogerfiber

import (
	"fmt"
	"net/http"

	"github.com/gofiber/fiber/v2"
	"github.com/kartaladev/doger"
	dogerhttp "github.com/kartaladev/doger/http"
)

// Use integrates the doger library's web-based debugging interface with a Fiber router.
// This function sets up HTTP routes for the debugging UI, allowing you to inspect
// your DI container through a web browser using the Fiber framework.
//
// Parameters:
//   - router: The Fiber router to add the debugging routes to
//   - basePath: The base URL path for the debugging interface
//   - injector: The injector instance to debug
//
// The function sets up the following routes:
//   - GET /: The main debugging interface home page
//   - GET /scope: Scope tree visualization with optional scope_id parameter
//   - GET /service: Service inspection with optional scope_id and service_name parameters
//
// Example:
//
//	app := fiber.New()
//	api := app.Group("/api")
//	debug := app.Group("/debug/di")
//
//	// Add the debugging interface
//	dogerfiber.Use(debug, "/debug/di", injector)
//
//	// Your application routes
//	api.Get("/users", userHandler)
//
// The debugging interface will be available at /debug/di and provides:
//   - Visual representation of scope hierarchy
//   - Service dependency graphs
//   - Service inspection and debugging tools
//   - Navigation between different views
//
// Security:
// Do not expose this group publicly in production. Protect it with authentication
// (e.g., Basic Auth) and/or network restrictions, since it reveals internal details
// about your application's DI graph. Attach auth middleware to the group before Use.
func Use(router fiber.Router, basePath string, injector doger.Injector) {
	router.Get("", func(c *fiber.Ctx) error {
		output, err := dogerhttp.IndexHTML(basePath)
		return response(c, output, err)
	})

	router.Get("/scope", func(c *fiber.Ctx) error {
		scopeID := c.Query("scope_id")
		if scopeID == "" {
			target := fmt.Sprintf("%s/scope?scope_id=%s", basePath, injector.ID())
			writeHeaders(c)
			return c.Redirect(target, http.StatusFound)
		}

		output, err := dogerhttp.ScopeTreeHTML(basePath, injector, scopeID)
		return response(c, output, err)
	})

	router.Get("/service", func(c *fiber.Ctx) error {
		scopeID := c.Query("scope_id")
		serviceName := c.Query("service_name")

		if scopeID == "" || serviceName == "" {
			output, err := dogerhttp.ServiceListHTML(basePath, injector)
			return response(c, output, err)
		}

		output, err := dogerhttp.ServiceHTML(basePath, injector, scopeID, serviceName)
		return response(c, output, err)
	})
}

// writeHeaders sets the headers of dogerhttp.WriteHeaders on the response
// of c.
func writeHeaders(c *fiber.Ctx) {
	h := http.Header{}
	dogerhttp.WriteHeaders(h)

	for key := range h {
		c.Set(key, h.Get(key))
	}
}

// response writes data as the page, or a generic 500 when it failed to
// render: the error text never reaches the client, nor Fiber's error handler.
func response(c *fiber.Ctx, data string, err error) error {
	writeHeaders(c)

	if err != nil {
		return c.Status(http.StatusInternalServerError).SendString(http.StatusText(http.StatusInternalServerError))
	}

	return c.Status(http.StatusOK).SendString(data)
}
