package dogerhttp

import (
	"time"

	"github.com/kartaladev/doger"
)

// ServiceHTML generates an HTML page that displays detailed information about a specific service.
// This function creates a comprehensive service inspection page showing the service's scope,
// type, build time, invocation location, dependencies, and dependents.
//
// Parameters:
//   - basePath: The base URL path for the web interface
//   - injector: The injector containing the service
//   - scopeID: The ID of the scope containing the service
//   - serviceName: The name of the service to inspect
//
// Returns the HTML content as a string and any error that occurred during generation.
//
// The generated page includes:
//   - Service metadata (scope, type, build time, invocation location)
//   - List of dependencies with clickable links
//   - List of dependents with clickable links
//   - Navigation to other views
//
// If the scope or service is not found, it falls back to the service list page.
// Every name is rendered as escaped text.
//
// Example:
//
//	html, err := http.ServiceHTML("/debug/di", injector, "root", "database")
//	if err != nil {
//	    log.Fatal(err)
//	}
//	fmt.Fprint(w, html)
func ServiceHTML(basePath string, injector doger.Injector, scopeID string, serviceName string) (string, error) {
	scope, ok := getScopeByID(injector, scopeID)
	if !ok {
		return ServiceListHTML(basePath, injector)
	}

	service, ok := doger.ExplainNamedService(scope.Scope, serviceName)
	if !ok {
		return ServiceListHTML(basePath, injector)
	}

	invoked := ""
	if service.Invoked != nil {
		invoked = service.Invoked.String()
	}

	return render("service", struct {
		Menu             menuLinks
		ScopeID          string
		ScopeName        string
		ServiceName      string
		ServiceType      doger.ServiceType
		ServiceBuildTime time.Duration
		Invoked          string
		Dependencies     []dependencyEntry
		Dependents       []dependencyEntry
	}{
		Menu:             newMenuLinks(basePath),
		ScopeID:          service.ScopeID,
		ScopeName:        service.ScopeName,
		ServiceName:      service.ServiceName,
		ServiceType:      service.ServiceType,
		ServiceBuildTime: service.ServiceBuildTime,
		Invoked:          invoked,
		Dependencies:     dependencyEntries(basePath, service.Dependencies),
		Dependents:       dependencyEntries(basePath, service.Dependents),
	})
}

// ServiceListHTML generates an HTML page that displays a list of all services across all scopes.
// This function creates a comprehensive service listing page showing all services
// organized by their respective scopes.
//
// Parameters:
//   - basePath: The base URL path for the web interface
//   - injector: The injector containing the services to list
//
// Returns the HTML content as a string and any error that occurred during generation.
//
// The generated page includes:
//   - List of all scopes in the injector hierarchy
//   - Services within each scope with clickable links
//   - Navigation to other views
//   - Service type indicators and capabilities
//
// Every scope and service name is rendered as escaped text.
//
// Example:
//
//	html, err := http.ServiceListHTML("/debug/di", injector)
//	if err != nil {
//	    log.Fatal(err)
//	}
//	fmt.Fprint(w, html)
func ServiceListHTML(basePath string, injector doger.Injector) (string, error) {
	scopes := getAllScopes(injector)

	return render("serviceList", struct {
		Menu   menuLinks
		Scopes []scopeEntry
	}{
		Menu: newMenuLinks(basePath),
		Scopes: mAp(scopes, func(item doger.ExplainInjectorScopeOutput) scopeEntry {
			return serviceListScopeEntry(basePath, item)
		}),
	})
}
