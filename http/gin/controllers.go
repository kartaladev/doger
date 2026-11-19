package dogergin

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/kartaladev/doger"
	dogerhttp "github.com/kartaladev/doger/http"
	"github.com/kartaladev/doger/http/internal/webui"
)

// Use integrates the doger library's web-based debugging interface with a Gin router.
// This function sets up HTTP routes for the debugging UI, allowing you to inspect
// your DI container through a web browser.
//
// Parameters:
//   - router: The Gin router group to add the debugging routes to
//   - injector: The injector instance to debug
//
// The function sets up the following routes:
//   - GET /: The main debugging interface home page
//   - GET /scope: Scope tree visualization with optional scope_id parameter
//   - GET /service: Service inspection with optional scope_id and service_name parameters
//
// Example:
//
//	router := gin.Default()
//	api := router.Group("/api")
//	debug := router.Group("/debug/di")
//
//	// Add the debugging interface
//	dogergin.Use(debug, injector)
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
// (e.g., Basic Auth) and/or network restrictions, since it can leak internals
// about your application's DI graph. Attach auth middleware to the router group
// before calling Use.
//
// opts configure the pages, such as [dogerhttp.WithFullSourcePaths] and
// [dogerhttp.WithErrorHandler].
func Use(router *gin.RouterGroup, injector doger.Injector, opts ...dogerhttp.Option) {
	use(router, injector, defaultRenderer, opts)
}

func use(router *gin.RouterGroup, injector doger.Injector, pages renderer, opts []dogerhttp.Option) {
	base := webui.Base(router.BasePath())

	router.Handle("GET", "", func(c *gin.Context) {
		output, err := pages.index(base, opts...)
		response(c, output, err, opts)
	})

	router.Handle("GET", "/scope", func(c *gin.Context) {
		scopeID := c.Query("scope_id")
		if scopeID == "" {
			dogerhttp.WriteHeaders(c.Writer.Header())
			c.Redirect(http.StatusFound, webui.ScopeLink(base, injector.ID()))
			return
		}

		output, err := pages.scopeTree(base, injector, scopeID, opts...)
		response(c, output, err, opts)
	})

	router.Handle("GET", "/service", func(c *gin.Context) {
		scopeID := c.Query("scope_id")
		serviceName := c.Query("service_name")

		if scopeID == "" || serviceName == "" {
			output, err := pages.serviceList(base, injector, opts...)
			response(c, output, err, opts)
			return
		}

		output, err := pages.service(base, injector, scopeID, serviceName, opts...)
		response(c, output, err, opts)
	})
}

// response writes data as the page, or a generic 500 when it failed to
// render: the error text never reaches the client, only the error handler
// of opts.
func response(c *gin.Context, data string, err error, opts []dogerhttp.Option) {
	webui.WritePage(c.Writer, c.Request, data, err, webui.Resolve(opts))
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
