package dogerhttpstd

import (
	"fmt"
	"net/http"

	"github.com/kartaladev/doger"
	dogerhttp "github.com/kartaladev/doger/http"
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
func Use(basePath string, injector doger.Injector) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		output, err := dogerhttp.IndexHTML(basePath)
		response(w, []byte(output), err)
	})

	mux.HandleFunc("GET /scope", func(w http.ResponseWriter, r *http.Request) {
		scopeID := r.URL.Query().Get("scope_id")
		if scopeID == "" {
			target := fmt.Sprintf("%s/scope?scope_id=%s", basePath, injector.ID())
			http.Redirect(w, r, target, http.StatusFound)
			return
		}

		output, err := dogerhttp.ScopeTreeHTML(basePath, injector, scopeID)
		response(w, []byte(output), err)
	})

	mux.HandleFunc("GET /service", func(w http.ResponseWriter, r *http.Request) {
		scopeID := r.URL.Query().Get("scope_id")
		serviceName := r.URL.Query().Get("service_name")

		if scopeID == "" || serviceName == "" {
			output, err := dogerhttp.ServiceListHTML(basePath, injector)
			response(w, []byte(output), err)
			return
		}

		output, err := dogerhttp.ServiceHTML(basePath, injector, scopeID, serviceName)
		response(w, []byte(output), err)
	})

	return http.StripPrefix(basePath, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		dogerhttp.WriteHeaders(w.Header())

		if r.URL.Path == "" {
			// The base path itself is the home page. StripPrefix gave r its
			// own URL, so this does not change the caller's request.
			r.URL.Path = "/"
		}

		mux.ServeHTTP(w, r)
	}))
}

// response writes output as the page, or a generic 500 when it failed to
// render: the error text never reaches the client.
func response(w http.ResponseWriter, output []byte, err error) {
	dogerhttp.WritePage(w, string(output), err)
}
