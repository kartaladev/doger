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
//
// opts configure the pages, such as [dogerhttp.WithFullSourcePaths] and
// [dogerhttp.WithErrorHandler].
func Use(router *echo.Group, basePath string, injector doger.Injector, opts ...dogerhttp.Option) {
	use(router, basePath, injector, defaultRenderer, opts)
}

func use(router *echo.Group, basePath string, injector doger.Injector, pages renderer, opts []dogerhttp.Option) {
	router.GET("", func(c echo.Context) error {
		output, err := pages.index(basePath, opts...)
		return response(c, output, err, opts)
	})

	router.GET("/scope", func(c echo.Context) error {
		scopeID := c.QueryParam("scope_id")
		if scopeID == "" {
			target := fmt.Sprintf("%s/scope?scope_id=%s", basePath, injector.ID())
			dogerhttp.WriteHeaders(c.Response().Header())
			return c.Redirect(http.StatusFound, target)
		}

		output, err := pages.scopeTree(basePath, injector, scopeID, opts...)
		return response(c, output, err, opts)
	})

	router.GET("/service", func(c echo.Context) error {
		scopeID := c.QueryParam("scope_id")
		serviceName := c.QueryParam("service_name")

		if scopeID == "" || serviceName == "" {
			output, err := pages.serviceList(basePath, injector, opts...)
			return response(c, output, err, opts)
		}

		output, err := pages.service(basePath, injector, scopeID, serviceName, opts...)
		return response(c, output, err, opts)
	})
}

// response writes data as the page, or a generic 500 when it failed to
// render: the error text never reaches the client nor Echo's error handler,
// only the error handler of opts.
func response(c echo.Context, data string, err error, opts []dogerhttp.Option) error {
	dogerhttp.WritePage(c.Response(), c.Request(), data, err, opts...)

	return nil
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
