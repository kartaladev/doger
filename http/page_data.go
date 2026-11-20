package dogerhttp

import (
	"slices"
	"strconv"

	"github.com/kartaladev/doger"
	"github.com/kartaladev/doger/http/internal/webui"
)

// invokedAt returns where service was provided, as "<path>:<line>", once it
// was invoked, or "" before. The path is shortened to its file base name
// unless o asks for full source paths.
func invokedAt(scope doger.Injector, service doger.ExplainServiceOutput, o pageOptions) string {
	if service.Invoked == nil || !isInvoked(scope, service) {
		return ""
	}

	location := service.Invoked.File + ":" + strconv.Itoa(service.Invoked.Line)
	if o.FullSourcePaths {
		return location
	}

	return shortenSourcePath(location)
}

// isInvoked reports whether service, as seen from scope, was invoked.
func isInvoked(scope doger.Injector, service doger.ExplainServiceOutput) bool {
	return slices.ContainsFunc(scope.ListInvokedServices(), func(invoked doger.ServiceDescription) bool {
		return invoked.ScopeID == service.ScopeID && invoked.Service == service.ServiceName
	})
}

// The links of the pages, shared with the adapters' redirects.
var (
	pageLink    = webui.Link
	scopeLink   = webui.ScopeLink
	serviceLink = webui.ServiceLink
)

// menuLinks are the links of the menu shown on every page.
type menuLinks struct {
	Home     string
	Scopes   string
	Services string
}

func newMenuLinks(basePath string) menuLinks {
	return menuLinks{
		Home:     pageLink(basePath, "", nil),
		Scopes:   pageLink(basePath, "/scope", nil),
		Services: pageLink(basePath, "/service", nil),
	}
}

// scopeEntry is a scope of the scope tree or of the service list.
type scopeEntry struct {
	Name     string
	Link     string
	Services []serviceEntry
	Children []scopeEntry
}

// serviceEntry is a service of a scope entry.
type serviceEntry struct {
	Icon     string
	Name     string
	Link     string
	Features string
}

// scopeTreeScopeEntry returns the entry of a scope of the scope tree, with
// its child scopes.
func scopeTreeScopeEntry(basePath string, description doger.ExplainInjectorScopeOutput) scopeEntry {
	return scopeEntry{
		Name: description.ScopeName,
		Link: scopeLink(basePath, description.ScopeID),
		Services: mAp(description.Services, func(item doger.ExplainInjectorServiceOutput) serviceEntry {
			return newServiceEntry(basePath, description.ScopeID, item)
		}),
		Children: mAp(description.Children, func(item doger.ExplainInjectorScopeOutput) scopeEntry {
			return scopeTreeScopeEntry(basePath, item)
		}),
	}
}

// newServiceEntry returns the entry of a service of scopeID, with its type
// icon and the icons of the features it implements.
func newServiceEntry(basePath string, scopeID string, description doger.ExplainInjectorServiceOutput) serviceEntry {
	featuresIcons := ""

	if description.IsHealthchecker {
		featuresIcons += " 🫀"
	}

	if description.IsShutdowner {
		featuresIcons += " 🙅"
	}

	return serviceEntry{
		Icon:     description.ServiceTypeIcon,
		Name:     description.ServiceName,
		Link:     serviceLink(basePath, scopeID, description.ServiceName),
		Features: featuresIcons,
	}
}

// dependencyEntry is a service of a dependency or dependent tree.
type dependencyEntry struct {
	Name string
	Link string
	// Ref marks a service already expanded elsewhere in the tree: it links
	// to the service's page and has no children.
	Ref      bool
	Children []dependencyEntry
}

// dependencyEntries returns the entries of a dependency or dependent tree.
// Each entry links to the page of its service and holds its own tree, except
// a reference, which has none.
func dependencyEntries(basePath string, services []doger.ExplainServiceDependencyOutput) []dependencyEntry {
	return mAp(services, func(service doger.ExplainServiceDependencyOutput) dependencyEntry {
		entry := dependencyEntry{
			Name: service.Service,
			Link: serviceLink(basePath, service.ScopeID, service.Service),
			Ref:  service.Ref,
		}
		if !service.Ref {
			entry.Children = dependencyEntries(basePath, service.Recursive)
		}

		return entry
	})
}

// serviceListScopeEntry returns the entry of a scope of the service list,
// without its child scopes: the list shows every scope at the same level.
func serviceListScopeEntry(basePath string, description doger.ExplainInjectorScopeOutput) scopeEntry {
	return scopeEntry{
		Name: description.ScopeName,
		Link: scopeLink(basePath, description.ScopeID),
		Services: mAp(description.Services, func(item doger.ExplainInjectorServiceOutput) serviceEntry {
			return newServiceEntry(basePath, description.ScopeID, item)
		}),
	}
}
