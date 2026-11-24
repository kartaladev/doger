package doger

import (
	"fmt"
	"slices"
)

// DeclareDependency records that dependent depends on dependency, without
// building either service. It is typically called by a component-kind
// handler during Start, for example to make a scheduler depend on the jobs
// it drives.
//
// dependent is resolved from i, and
// dependency from the dependent's own scope or its ancestors, as Invoke
// would. A name that cannot be resolved gives an error matching
// [ErrServiceNotFound]. If dependency already reaches dependent through
// recorded edges or static [DependsOn] declarations, the edge would close a
// cycle and an error matching [ErrCircularDependency] is returned. An
// export reaches its source, even before anything resolved the export: with
// an edge to an export, dependent starts after that source and shuts down
// before it, and the edge closes a cycle when the source depends back on
// dependent (such a back edge added later, by an override, makes Start fail
// with a cycle). Declaring an edge twice changes nothing. A nil i, including a typed-nil *RootScope
// or *Scope, gives [ErrNilInjector] and records nothing: there is no
// default container.
//
// A declared edge orders start (when declared before or during
// [StageBeforeStart]) and shutdown, but not construction, because it builds
// nothing. Clones do not copy declared edges.
func DeclareDependency(i Injector, dependent, dependency string) error {
	injector, err := getInjector(i)
	if err != nil {
		return err
	}

	_, dependentScope, ok := injector.serviceGetRec(dependent)
	if !ok {
		return serviceNotFound(injector, ErrServiceNotFound, []string{dependent})
	}

	_, dependencyScope, ok := dependentScope.serviceGetRec(dependency)
	if !ok {
		return serviceNotFound(dependentScope, ErrServiceNotFound, []string{dependent, dependency})
	}

	from := newServiceDescription(dependentScope.ID(), dependentScope.Name(), dependent)
	to := newServiceDescription(dependencyScope.ID(), dependencyScope.Name(), dependency)

	root := injector.RootScope()
	root.declareMu.Lock()
	defer root.declareMu.Unlock()

	if path := root.dependencyPath(to, from); path != nil {
		chain := []string{dependent}
		for _, desc := range path {
			chain = append(chain, desc.Service)
		}
		return fmt.Errorf("%w: %s", ErrCircularDependency, humanReadableInvokerChain(chain))
	}

	root.dag.addDependency(from.ScopeID, from.ScopeName, from.Service, to.ScopeID, to.ScopeName, to.Service, DependencySourceDeclared)

	return nil
}

// dependencyPath returns a path from -> ... -> to over recorded DAG edges and
// static DependsOn declarations, or nil when to is not reachable from from.
func (s *RootScope) dependencyPath(from, to ServiceDescription) []ServiceDescription {
	visited := map[ServiceDescription]struct{}{}

	var walk func(node ServiceDescription, path []ServiceDescription) []ServiceDescription
	walk = func(node ServiceDescription, path []ServiceDescription) []ServiceDescription {
		path = append(path, node)
		if node == to {
			return slices.Clone(path)
		}
		if _, ok := visited[node]; ok {
			return nil
		}
		visited[node] = struct{}{}

		for _, neighbor := range s.dependencyNeighbors(node) {
			if found := walk(neighbor, path); found != nil {
				return found
			}
		}

		return nil
	}

	return walk(from, nil)
}

// dependencyNeighbors returns the services desc depends on: its recorded DAG
// edges plus the static DependsOn declarations of its registered wrapper,
// resolved from its scope, and, for an export, its source, which an edge
// declared to the export reaches before anything resolved it. A neighbour
// may be listed twice. The DAG lock is never held while a scope lock is
// taken.
func (s *RootScope) dependencyNeighbors(desc ServiceDescription) []ServiceDescription {
	s.seamDependencyNeighbors(desc)

	s.dag.mu.RLock()
	neighbors := keys(s.dag.dependencies[desc])
	s.dag.mu.RUnlock()

	scope := s.scopeByID(desc.ScopeID)
	if scope == nil {
		return neighbors
	}

	serviceAny, ok := scope.serviceGet(desc.Service)
	if !ok {
		return neighbors
	}

	if source, ok := exportSourceOf(serviceAny); ok {
		neighbors = append(neighbors, source)
	}

	service, ok := serviceAny.(serviceWrapperAny)
	if !ok || service.getOptions() == nil {
		return neighbors
	}

	for _, name := range service.getOptions().dependsOn {
		if _, targetScope, ok := scope.serviceGetRec(name); ok {
			neighbors = append(neighbors, newServiceDescription(targetScope.ID(), targetScope.Name(), name))
		}
	}

	return neighbors
}

// scopeByID returns the scope of this container with the given ID, or nil.
func (s *RootScope) scopeByID(id string) *Scope {
	if s.self.ID() == id {
		return s.self
	}

	scope, ok := s.self.ChildByID(id)
	if !ok {
		return nil
	}

	return scope
}
