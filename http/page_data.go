package dogerhttp

import (
	"net/url"

	"github.com/kartaladev/doger"
)

// pageLink returns the link to page under basePath with query. The path is
// escaped as a URL path and the query values with URL query escaping.
func pageLink(basePath, page string, query url.Values) string {
	u := url.URL{Path: basePath + page, RawQuery: query.Encode()}

	return u.String()
}

// scopeLink returns the link to the scope tree page of scopeID.
func scopeLink(basePath, scopeID string) string {
	return pageLink(basePath, "/scope", url.Values{"scope_id": {scopeID}})
}

// serviceLink returns the link to the page of the service serviceName of
// scopeID.
func serviceLink(basePath, scopeID, serviceName string) string {
	return pageLink(basePath, "/service", url.Values{
		"scope_id":     {scopeID},
		"service_name": {serviceName},
	})
}

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
	Name     string
	Link     string
	Children []dependencyEntry
}

// dependencyEntries returns the entries of a dependency or dependent tree.
// Each entry links to the page of its service and holds its own tree.
func dependencyEntries(basePath string, services []doger.ExplainServiceDependencyOutput) []dependencyEntry {
	return mAp(services, func(service doger.ExplainServiceDependencyOutput) dependencyEntry {
		return dependencyEntry{
			Name:     service.Service,
			Link:     serviceLink(basePath, service.ScopeID, service.Service),
			Children: dependencyEntries(basePath, service.Recursive),
		}
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
