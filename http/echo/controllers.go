package dogerecho

import (
	"fmt"
	"net/http"

	"github.com/kartaladev/doger"
	dogerhttp "github.com/kartaladev/doger/http"
	"github.com/labstack/echo/v4"
)

// Use integrates the doger library's web-based debugging interface with an Echo router.
// This function sets up HTTP routes for the debugging UI, allowing you to inspect
// your DI container through a web browser using the Echo framework.
//
// Parameters:
//   - router: The Echo router group to add the debugging routes to
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
//	e := echo.New()
//	api := e.Group("/api")
//	debug := e.Group("/debug/di")
//
//	// Add the debugging interface
//	dogerecho.Use(debug, "/debug/di", injector)
//
//	// Your application routes
//	api.GET("/users", userHandler)
//
// The debugging interface will be available at /debug/di and provides:
//   - Visual representation of scope hierarchy
//   - Service dependency graphs
//   - Service inspection and debugging tools
//   - Navigation between different views
//
// Security:
// Do not expose this group publicly in production. Protect it with authentication
// (e.g., Basic Auth) and/or network restrictions, since it reveals internals
// about your DI graph. Attach auth middleware to the Echo group before Use.
func Use(router *echo.Group, basePath string, injector doger.Injector) {
	router.GET("", func(c echo.Context) error {
		output, err := dogerhttp.IndexHTML(basePath)
		return response(c, output, err)
	})

	router.GET("/scope", func(c echo.Context) error {
		scopeID := c.QueryParam("scope_id")
		if scopeID == "" {
			target := fmt.Sprintf("%s/scope?scope_id=%s", basePath, injector.ID())
			dogerhttp.WriteHeaders(c.Response().Header())
			return c.Redirect(http.StatusFound, target)
		}

		output, err := dogerhttp.ScopeTreeHTML(basePath, injector, scopeID)
		return response(c, output, err)
	})

	router.GET("/service", func(c echo.Context) error {
		scopeID := c.QueryParam("scope_id")
		serviceName := c.QueryParam("service_name")

		if scopeID == "" || serviceName == "" {
			output, err := dogerhttp.ServiceListHTML(basePath, injector)
			return response(c, output, err)
		}

		output, err := dogerhttp.ServiceHTML(basePath, injector, scopeID, serviceName)
		return response(c, output, err)
	})
}

// response writes data as the page, or a generic 500 when it failed to
// render: the error text never reaches the client, nor Echo's error handler.
func response(c echo.Context, data string, err error) error {
	dogerhttp.WritePage(c.Response(), data, err)

	return nil
}
