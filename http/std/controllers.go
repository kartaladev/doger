package dogerhttpstd

import (
	"net/http"
	"path"

	"github.com/kartaladev/doger"
	dogerhttp "github.com/kartaladev/doger/http"
	"github.com/kartaladev/doger/http/internal/webui"
)

// Use creates an HTTP handler for the doger library's web-based debugging interface
// using the standard Go net/http package. This function returns a handler that
// can be integrated with any standard HTTP server or router.
//
// Parameters:
//   - basePath: The base URL path for the debugging interface
//   - injector: The injector instance to debug
//
// Returns an http.Handler that serves the debugging interface.
//
// The handler sets up the following routes:
//   - GET /: The main debugging interface home page
//   - GET /scope: Scope tree visualization with optional scope_id parameter
//   - GET /service: Service inspection with optional scope_id and service_name parameters
//
// Example:
//
//	// Create the debugging handler
//	debugHandler := dogerhttpstd.Use("/debug/di", injector)
//
//	// Mount it in your server
//	mux := http.NewServeMux()
//	mux.Handle("/debug/di/", debugHandler)
//
//	// Your application routes
//	mux.HandleFunc("/api/users", userHandler)
//
//	// Start the server
//	http.ListenAndServe(":8080", mux)
//
// The debugging interface will be available at /debug/di and provides:
//   - Visual representation of scope hierarchy
//   - Service dependency graphs
//   - Service inspection and debugging tools
//   - Navigation between different views
//
// The handler automatically strips the basePath prefix from incoming requests
// to ensure proper routing within the debugging interface.
//
// Security:
// Do not expose this debug UI publicly in production. It reveals internal details
// about your application's DI graph. Protect these routes with authentication
// (for example, Basic Auth) and/or network restrictions (IP allowlist, VPN, etc.).
// Wrap this handler with your auth/middleware before mounting it.
//
// The handler serves GET and HEAD only; any other method gets 405 Method Not
// Allowed. Every response carries the headers of [dogerhttp.WriteHeaders],
// and a page that fails to render gets a generic 500 that reveals no detail.
// opts configure the pages, such as [dogerhttp.WithFullSourcePaths] and
// [dogerhttp.WithErrorHandler].
func Use(basePath string, injector doger.Injector, opts ...dogerhttp.Option) http.Handler {
	return use(basePath, injector, defaultRenderer, opts)
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

func use(basePath string, injector doger.Injector, pages renderer, opts []dogerhttp.Option) http.Handler {
	base := webui.Base(basePath)
	mux := http.NewServeMux()

	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		output, err := pages.index(base, opts...)
		response(w, r, output, err, opts)
	})

	mux.HandleFunc("GET /scope", func(w http.ResponseWriter, r *http.Request) {
		scopeID := r.URL.Query().Get("scope_id")
		if scopeID == "" {
			http.Redirect(w, r, webui.ScopeLink(base, injector.ID()), http.StatusFound)
			return
		}

		output, err := pages.scopeTree(base, injector, scopeID, opts...)
		response(w, r, output, err, opts)
	})

	mux.HandleFunc("GET /service", func(w http.ResponseWriter, r *http.Request) {
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

	return http.StripPrefix(base, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		dogerhttp.WriteHeaders(w.Header())

		p := r.URL.Path
		if p == "" {
			// The base path itself is the home page. StripPrefix gave r its
			// own URL, so this does not change the caller's request.
			p = "/"
			r.URL.Path = p
		}

		if p[0] != '/' || path.Clean(p) != p {
			// A path the mux would clean, such as "//scope", "/../x" or the
			// "X" left of "/debug/dogerX", would get a redirect that drops
			// the base path: answer 404 instead.
			http.NotFound(w, r)
			return
		}

		mux.ServeHTTP(w, r)
	}))
}

// response writes output as the page, or a generic 500 when it failed to
// render: the error text never reaches the client, only the error handler
// of opts.
func response(w http.ResponseWriter, r *http.Request, output string, err error, opts []dogerhttp.Option) {
	webui.WritePage(w, r, output, err, webui.Resolve(opts))
}
