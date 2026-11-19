package dogerchi

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/kartaladev/doger"
	dogerhttp "github.com/kartaladev/doger/http"
	"github.com/kartaladev/doger/http/internal/webui"
)

// Use integrates the doger library's web-based debugging interface with a Chi router.
// This function sets up HTTP routes for the debugging UI, allowing you to inspect
// your DI container through a web browser using the Chi framework.
//
// Parameters:
//   - router: The Chi router to add the debugging routes to
//   - basePath: The base URL path for the debugging interface
//   - injector: The injector instance to debug
//
// The function sets up the following routes:
//   - GET {basePath}: The main debugging interface home page
//   - GET {basePath}/scope: Scope tree visualization with optional scope_id parameter
//   - GET {basePath}/service: Service inspection with optional scope_id and service_name parameters
//
// Example:
//
//	r := chi.NewRouter()
//	api := r.Route("/api", nil)
//
//	// Add the debugging interface
//	dogerchi.Use(r, "/debug/di", injector)
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
// Chi routes are registered with the full basePath, so the debugging interface
// will be available at the exact path specified in basePath.
//
// Security:
// Do not expose these routes publicly in production. They reveal internal details
// about your DI graph. Protect the basePath with authentication (e.g., Basic Auth)
// and/or network restrictions. Apply your auth middleware before calling Use.
//
// opts configure the pages, such as [dogerhttp.WithFullSourcePaths] and
// [dogerhttp.WithErrorHandler].
func Use(router *chi.Mux, basePath string, injector doger.Injector, opts ...dogerhttp.Option) {
	use(router, basePath, injector, defaultRenderer, opts)
}

func use(router *chi.Mux, basePath string, injector doger.Injector, pages renderer, opts []dogerhttp.Option) {
	base := webui.Base(basePath)

	router.Get(webui.HomeRoute(base), func(w http.ResponseWriter, r *http.Request) {
		output, err := pages.index(base, opts...)
		response(w, r, output, err, opts)
	})

	router.Get(base+"/scope", func(w http.ResponseWriter, r *http.Request) {
		scopeID := r.URL.Query().Get("scope_id")
		if scopeID == "" {
			dogerhttp.WriteHeaders(w.Header())
			http.Redirect(w, r, webui.ScopeLink(base, injector.ID()), http.StatusFound)
			return
		}

		output, err := pages.scopeTree(base, injector, scopeID, opts...)
		response(w, r, output, err, opts)
	})

	router.Get(base+"/service", func(w http.ResponseWriter, r *http.Request) {
		scopeID := r.URL.Query().Get("scope_id")
		serviceName := r.URL.Query().Get("service_name")

		if scopeID == "" || serviceName == "" {
			output, err := pages.serviceList(base, injector, opts...)
			response(w, r, output, err, opts)
			return
		}

		output, err := pages.service(base, injector, scopeID, serviceName, opts...)
		response(w, r, output, err, opts)
	})
}

// response writes data as the page, or a generic 500 when it failed to
// render: the error text never reaches the client, only the error handler
// of opts.
func response(w http.ResponseWriter, r *http.Request, data string, err error, opts []dogerhttp.Option) {
	webui.WritePage(w, r, data, err, webui.Resolve(opts))
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
