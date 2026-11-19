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
//
// opts configure the pages, such as [dogerhttp.WithFullSourcePaths] and
// [dogerhttp.WithErrorHandler]. The error handler receives c.UserContext().
func Use(router fiber.Router, basePath string, injector doger.Injector, opts ...dogerhttp.Option) {
	use(router, basePath, injector, defaultRenderer, opts)
}

func use(router fiber.Router, basePath string, injector doger.Injector, pages renderer, opts []dogerhttp.Option) {
	router.Get("", func(c *fiber.Ctx) error {
		output, err := pages.index(basePath, opts...)
		return response(c, output, err, opts)
	})

	router.Get("/scope", func(c *fiber.Ctx) error {
		scopeID := c.Query("scope_id")
		if scopeID == "" {
			target := fmt.Sprintf("%s/scope?scope_id=%s", basePath, injector.ID())
			writeHeaders(c)
			return c.Redirect(target, http.StatusFound)
		}

		output, err := pages.scopeTree(basePath, injector, scopeID, opts...)
		return response(c, output, err, opts)
	})

	router.Get("/service", func(c *fiber.Ctx) error {
		scopeID := c.Query("scope_id")
		serviceName := c.Query("service_name")

		if scopeID == "" || serviceName == "" {
			output, err := pages.serviceList(basePath, injector, opts...)
			return response(c, output, err, opts)
		}

		output, err := pages.service(basePath, injector, scopeID, serviceName, opts...)
		return response(c, output, err, opts)
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
// render: the error text never reaches the client nor Fiber's error
// handler, only the error handler of opts, with c.UserContext().
func response(c *fiber.Ctx, data string, err error, opts []dogerhttp.Option) error {
	writeHeaders(c)

	if err != nil {
		dogerhttp.ReportError(c.UserContext(), err, opts...)
		return c.Status(http.StatusInternalServerError).SendString(http.StatusText(http.StatusInternalServerError))
	}

	return c.Status(http.StatusOK).SendString(data)
}

// renderer renders the pages of the UI. Tests replace a page to make it fail.
type renderer struct {
	index       func(basePath string, opts ...dogerhttp.Option) (string, error)
	scopeTree   func(basePath string, injector doger.Injector, scopeID string, opts ...dogerhttp.Option) (string, error)
	service     func(basePath string, injector doger.Injector, scopeID, serviceName string, opts ...dogerhttp.Option) (string, error)
	serviceList func(basePath string, injector doger.Injector, opts ...dogerhttp.Option) (string, error)
}

// defaultRenderer renders the pages of package dogerhttp.
var defaultRenderer = renderer{
	index:       dogerhttp.IndexHTML,
	scopeTree:   dogerhttp.ScopeTreeHTML,
	service:     dogerhttp.ServiceHTML,
	serviceList: dogerhttp.ServiceListHTML,
}
