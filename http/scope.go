package dogerhttp

import (
	"github.com/kartaladev/doger"
)

// ScopeTreeHTML generates an HTML page that displays the scope tree structure of the DI container.
// This function creates a visual representation of the scope hierarchy, showing the relationships
// between scopes and the services they contain.
//
// Parameters:
//   - basePath: The base URL path for the web interface
//   - injector: The injector instance to analyze
//   - scopeID: The ID of the specific scope to highlight (optional)
//   - opts: Page options; none changes this page
//
// Returns the HTML content as a string and any error that occurred during generation.
//
// The generated page includes:
//   - Visual representation of the scope hierarchy
//   - Service icons indicating their type and capabilities
//   - Navigation links between different views
//   - Interactive scope inspection links
//
// Every scope and service name is rendered as escaped text.
//
// Example:
//
//	html, err := http.ScopeTreeHTML("/debug/di", injector, "")
//	if err != nil {
//	    log.Fatal(err)
//	}
//	fmt.Fprint(w, html)
func ScopeTreeHTML(basePath string, injector doger.Injector, scopeID string, opts ...Option) (string, error) {
	description := doger.ExplainInjector(injector)

	return render("scopeTree", struct {
		Menu   menuLinks
		Scopes []scopeEntry
	}{
		Menu: newMenuLinks(basePath),
		Scopes: mAp(description.DAG, func(item doger.ExplainInjectorScopeOutput) scopeEntry {
			return scopeTreeScopeEntry(basePath, item)
		}),
	})
}
