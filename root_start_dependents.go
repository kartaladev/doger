package doger

import (
	"cmp"
	"maps"
	"strings"
)

// dependentsCheck answers a batch of dependents checks of a run: one
// sequential or stage check, or the parallel scheduling passes until what it
// read changes. It snapshots once the services the run skipped a step of
// and those with an abandoned call still running, and walks the dependency
// graph lazily, memoizing for each service the best skipped service it
// reaches, so a check costs a lookup once the walk reached its candidate.
type dependentsCheck struct {
	run *startRun
	// what the snapshot reflects: the run's skips, the abandoned steps, the
	// graph's edges and the registrations, as change counts (see current)
	skipCount, abandonedChanges, dagChanges, registrations, instanceChanges uint64
	// skipped is the run's skips when the batch began
	skipped map[ServiceDescription]stepSkip
	// reasons is skipped plus the services with an abandoned call still
	// running that the run did not skip, which a dependent must not start
	// before either; the walk adds the services whose instance is tracked
	reasons map[ServiceDescription]stepSkip
	// tracked is the instance tracker's snapshot, nil when it was empty;
	// instanceChecked holds the services the walk looked up in it
	tracked         map[any][]*kindID
	instanceChecked map[ServiceDescription]struct{}

	// the memoized walk: Tarjan's strongly connected components, so that
	// the members of a cycle share what they reach
	index   map[ServiceDescription]int
	low     map[ServiceDescription]int
	onStack map[ServiceDescription]bool
	stack   []ServiceDescription
	best    map[ServiceDescription]reachedSkip
}

// reachedSkip is the best skipped service that a service reaches, by
// compareSkipped; ok is false when it reaches none.
type reachedSkip struct {
	desc ServiceDescription
	ok   bool
}

// newDependentsCheck begins a batch of dependents checks of r. A run that
// skipped nothing while no abandoned call runs pays two lock round trips.
func (r *startRun) newDependentsCheck() *dependentsCheck {
	// read before the walk: an edge added or a service registered during it
	// makes the check stale
	dagChanges, registrations := r.root.dag.changeCount(), r.root.registrations.Load()

	r.mu.Lock()
	skipped, skipCount := maps.Clone(r.skipped), r.skipCount
	r.mu.Unlock()

	running, abandonedChanges := r.root.stillRunningErrors()
	tracked, instanceChanges := r.root.instances.snapshot()
	check := &dependentsCheck{
		run:              r,
		skipCount:        skipCount,
		abandonedChanges: abandonedChanges,
		dagChanges:       dagChanges,
		registrations:    registrations,
		instanceChanges:  instanceChanges,
		skipped:          skipped,
		tracked:          tracked,
	}
	if len(skipped) == 0 && len(running) == 0 && tracked == nil {
		return check
	}

	check.reasons = make(map[ServiceDescription]stepSkip, len(skipped)+len(running))
	check.instanceChecked = map[ServiceDescription]struct{}{}
	for desc, err := range running {
		check.reasons[desc] = stepSkip{service: desc.Service, stillRunning: err}
	}
	// the run's own record of a skip wins: it keeps both reasons
	maps.Copy(check.reasons, skipped)
	check.index = map[ServiceDescription]int{}
	check.low = map[ServiceDescription]int{}
	check.onStack = map[ServiceDescription]bool{}
	check.best = map[ServiceDescription]reachedSkip{}

	return check
}

// dependentsCheckSince returns previous while it is current, and a new
// check otherwise or when previous is nil.
func (r *startRun) dependentsCheckSince(previous *dependentsCheck) *dependentsCheck {
	if previous != nil && previous.current() {
		return previous
	}

	return r.newDependentsCheck()
}

// current reports whether nothing c read changed since: the run skipped
// nothing more, no step became or stopped being abandoned, the dependency
// graph gained no edge and lost no service, no scope stored a service
// (a registration or an override can change what a DependsOn name
// resolves to, or a service's own DependsOn), and the instance tracker,
// which clones share, did not change.
func (c *dependentsCheck) current() bool {
	root := c.run.root
	if root.dag.changeCount() != c.dagChanges || root.registrations.Load() != c.registrations ||
		root.instances.changes.Load() != c.instanceChanges || root.abandonedChangeCount() != c.abandonedChanges {
		return false
	}

	c.run.mu.Lock()
	defer c.run.mu.Unlock()

	return c.run.skipCount == c.skipCount
}

// skips reports whether the run skipped a step of the candidate of step,
// or whether that candidate depends, directly or through others, on a
// service the run skipped a step of (an abandoned call of that step still
// runs, or its scope is shutting down), or on a service with an abandoned
// call still running that the run has not claimed (in an ancestor scope, or
// whose step runs in a later stage): step is skipped too, with no hooks and
// no call. When step is skipped for a dependency and is not in a closing
// scope itself, the run reports it: the dependency's timeout error while
// its abandoned call runs, otherwise a *StartError naming step's service
// that matches ErrShutdown.
func (c *dependentsCheck) skips(step startStep) bool {
	desc := step.candidate.desc()
	if _, self := c.skipped[desc]; self {
		return true
	}
	if len(c.reasons) == 0 && c.tracked == nil {
		return false
	}

	reached := c.reached(desc)
	if !reached.ok {
		return false
	}

	// a dependent of a scope whose shutdown completed reaches nothing (its
	// services and edges are gone), so only a closing scope counts here
	closingItself := c.run.root.scopeClosing(step.candidate.scope)

	c.run.mu.Lock()
	defer c.run.mu.Unlock()

	// a dependency the run skipped only because its abandoned call still
	// runs set the pending error when it was skipped
	if !closingItself && c.run.pending == nil {
		c.run.pending = c.reasons[reached.desc].dependentError(step)
	}

	return true
}

// neighbors returns the services v depends on for the walk: its
// dependencyNeighbors, plus the source of an export whose source scope a
// bounded shutdown detached during the run (detachedExportSource).
func (c *dependentsCheck) neighbors(v ServiceDescription) []ServiceDescription {
	return append(c.run.root.dependencyNeighbors(v), c.run.detachedExportSources(v)...)
}

// detachedExportSources returns, when desc is an export whose source scope
// is no longer a child of the export's scope, every candidate the run
// claimed in that source scope or its descendants, and nil otherwise. A
// bounded child scope shutdown that gave up waiting for the run removes
// the services of that subtree, with their dependency edges, and detaches
// the scope, so dependencyNeighbors no longer leads from the export to its
// source, nor from the source to what it depends on there, while the run
// may still skip a step of that subtree afterwards: a dependent of the
// export outside the subtree must then be skipped and reported all the
// same. The edges inside the subtree are gone, so the export is taken to
// depend on all of its claimed candidates: a dependent of a scope that
// shut down is skipped once any step there was. Resolving this here,
// rather than snapshotting the graph, keeps an edge declared at any time
// during the run; it costs a scope lookup per service a walk visits, and
// walks run only once the run skipped a step.
func (r *startRun) detachedExportSources(desc ServiceDescription) []ServiceDescription {
	scope := r.root.scopeByID(desc.ScopeID)
	if scope == nil {
		return nil
	}
	serviceAny, ok := scope.serviceGet(desc.Service)
	if !ok {
		return nil
	}
	export, ok := asExport(serviceAny)
	if !ok {
		return nil
	}
	if _, _, found := export.exportSource(); found {
		// dependencyNeighbors follows a live source
		return nil
	}

	var sources []ServiceDescription
	for _, candidate := range r.candidates {
		if export.withinSource(candidate.scope) {
			sources = append(sources, candidate.desc())
		}
	}

	return sources
}

// reached returns the best skipped service that desc reaches.
func (c *dependentsCheck) reached(desc ServiceDescription) reachedSkip {
	if _, seen := c.index[desc]; !seen {
		c.connect(desc)
	}

	return c.best[desc]
}

// connect walks the services v reaches that the batch has not walked yet,
// and sets the best skipped service each reaches once its strongly
// connected component is complete (Tarjan).
func (c *dependentsCheck) connect(v ServiceDescription) {
	c.index[v] = len(c.index)
	c.low[v] = c.index[v]
	c.stack = append(c.stack, v)
	c.onStack[v] = true

	var best reachedSkip
	for _, n := range c.neighbors(v) {
		best = c.better(best, c.asReached(n))
		if _, seen := c.index[n]; !seen {
			c.connect(n)
			c.low[v] = min(c.low[v], c.low[n])
		} else if c.onStack[n] {
			c.low[v] = min(c.low[v], c.index[n])
		}
		// final once n's component is complete; otherwise n is in v's
		// component, which merges what its members reach below
		best = c.better(best, c.best[n])
	}
	c.best[v] = best

	if c.low[v] != c.index[v] {
		return
	}
	// v roots a component: its members reach each other and what any of
	// them reaches
	i := len(c.stack) - 1
	for c.stack[i] != v {
		i--
	}
	members := c.stack[i:]
	c.stack = c.stack[:i]

	cycle := len(members) > 1
	var component reachedSkip
	for _, m := range members {
		c.onStack[m] = false
		component = c.better(component, c.best[m])
		// defensive: each member of a cycle is a neighbour of another
		// member, whose best already holds it (asReached in the loop
		// above), so this changes nothing; it keeps the rule "a member of
		// a cycle reaches itself" explicit here
		if cycle {
			component = c.better(component, c.asReached(m))
		}
	}
	for _, m := range members {
		c.best[m] = component
	}
}

// asReached returns desc as a reached skipped service when it is one: a
// service reaches its neighbours, and a member of a cycle reaches itself.
func (c *dependentsCheck) asReached(desc ServiceDescription) reachedSkip {
	c.checkInstance(desc)
	_, skipped := c.reasons[desc]

	return reachedSkip{desc: desc, ok: skipped}
}

// checkInstance adds desc to the reasons, once per batch, when the batch's
// tracker snapshot holds an abandoned call still running on its built
// instance that this container has not recorded: the same instance,
// registered again or shared with a clone, is still starting.
func (c *dependentsCheck) checkInstance(desc ServiceDescription) {
	if c.tracked == nil {
		return
	}
	if _, checked := c.instanceChecked[desc]; checked {
		return
	}
	c.instanceChecked[desc] = struct{}{}
	if _, known := c.reasons[desc]; known {
		return
	}
	if err := c.run.root.trackedStillRunning(desc, c.tracked); err != nil {
		c.reasons[desc] = stepSkip{service: desc.Service, stillRunning: err}
	}
}

// better returns the better of two reached skipped services, by
// compareSkipped.
func (c *dependentsCheck) better(a, b reachedSkip) reachedSkip {
	switch {
	case !b.ok:
		return a
	case !a.ok:
		return b
	case compareSkipped(c.reasons, b.desc, a.desc) < 0:
		return b
	default:
		return a
	}
}

// compareSkipped orders two skipped services of reasons for the error of a
// dependent that reaches both: a still-running one first, then by scope
// name, service name and scope ID, so that the error does not depend on the
// order the graph is walked in.
func compareSkipped(reasons map[ServiceDescription]stepSkip, a, b ServiceDescription) int {
	aRunning, bRunning := reasons[a].stillRunning != nil, reasons[b].stillRunning != nil
	if aRunning != bRunning {
		if aRunning {
			return -1
		}
		return 1
	}

	return cmp.Or(
		strings.Compare(a.ScopeName, b.ScopeName),
		strings.Compare(a.Service, b.Service),
		strings.Compare(a.ScopeID, b.ScopeID),
	)
}
