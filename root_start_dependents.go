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
// that matches ErrShutdown. A step skipped for a dependency is also
// recorded as skipped in the run, so that its own dependents stay skipped
// for the rest of the run even once that dependency's abandoned call
// ended; one of a closing scope is recorded as skipped for its scope's
// shutdown, which reports nothing.
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
	// read once: the decision and the cause come from the same scope, which
	// may stop shutting down right after
	closing := c.run.root.shuttingDownScope(step.candidate.scope)
	closingItself := closing != nil
	c.run.root.seamAfterClosingDecision()
	reason := c.reasons[reached.desc]
	// the dependent's own record: it never starts in this run, so its
	// dependents stay skipped even once the reason it reached cleared (an
	// abandoned call that ended); a dependent of a closing scope is
	// skipped silently, for its scope's shutdown
	record := stepSkip{closing: reason.closing, stillRunning: reason.stillRunning}
	if closingItself {
		record = stepSkip{closing: scopeShutdownCause(closing)}
	}

	c.run.mu.Lock()
	defer c.run.mu.Unlock()

	// a dependency the run skipped only because its abandoned call still
	// runs set the pending error when it was skipped
	if !closingItself && c.run.pending == nil {
		c.run.pending = reason.dependentError(step)
	}
	c.recordLocked(step.candidate, record)

	return true
}

// recordLocked records in the run that it skipped a step of candidate for
// record, and folds that record into the batch when it is the only change
// of the run's skips since the batch began, so the batch stays current:
// the dependent reached a reason already, so what reaches it reached one
// too. c.run.mu must be held.
func (c *dependentsCheck) recordLocked(candidate *startCandidate, record stepSkip) {
	recorded, ok := c.run.skipLocked(candidate, record)
	if !ok || c.run.skipCount != c.skipCount+1 {
		return
	}

	desc := candidate.desc()
	if c.skipped == nil {
		c.skipped = map[ServiceDescription]stepSkip{}
	}
	c.skipped[desc] = recorded
	c.reasons[desc] = recorded
	c.skipCount = c.run.skipCount
}

// checkDetachedExport adds desc to the reasons when it is an export whose
// source scope a bounded shutdown detached during the run while the run
// skipped a step of that scope or of its descendants (see
// startRun.detachedExportSkip).
func (c *dependentsCheck) checkDetachedExport(desc ServiceDescription) {
	if _, known := c.reasons[desc]; known {
		return
	}
	if skip, ok := c.run.detachedExportSkip(desc, c.reasons); ok {
		c.reasons[desc] = skip
	}
}

// detachedExportSkip returns why a dependent of desc is skipped when desc
// is an export whose source scope, as the run claimed it, is no longer the
// one the export resolves from, and the run skipped a step of that scope or
// of its descendants (one of reasons); ok is false otherwise. A bounded
// child scope shutdown that gave up waiting for the run removes the
// services of that subtree, with their dependency edges, and detaches the
// scope, so the walk no longer leads from the export to its source, nor
// from the source to what it depends on there, while a dependent of the
// export outside the subtree must be skipped and reported all the same.
// Detachment is decided by scope identity, since the export looks its
// source scope up by name and a new scope may reuse that name. The edges
// inside the subtree are gone, so the dependent is taken to depend on the
// whole subtree: a still-running step of it decides the error, as for a
// dependent that reaches one; otherwise the run blames the removed export,
// not an unrelated skipped step. Resolving this as the walk reaches the
// export, rather than snapshotting the graph, keeps an edge declared at any
// time during the run. Cost: once per service a batch's walk reaches, a
// scope lookup by ID (scopeByID, which searches the scope tree) and a
// service lookup; for an export, a pass over the run's candidates. A walk
// runs only while the run has skipped a step, an abandoned call still
// runs, or the instance tracker holds one (newDependentsCheck); a run with
// none of these pays nothing.
func (r *startRun) detachedExportSkip(desc ServiceDescription, reasons map[ServiceDescription]stepSkip) (stepSkip, bool) {
	scope := r.root.scopeByID(desc.ScopeID)
	if scope == nil {
		return stepSkip{}, false
	}
	serviceAny, ok := scope.serviceGet(desc.Service)
	if !ok {
		return stepSkip{}, false
	}
	export, ok := asExport(serviceAny)
	if !ok {
		return stepSkip{}, false
	}
	live, _, found := export.exportSource()

	var best ServiceDescription
	var source *Scope
	for _, candidate := range r.candidates {
		if !export.withinSource(candidate.scope) {
			continue
		}
		claimed := sourceScopeOf(export, candidate.scope)
		if found && live == claimed {
			// dependencyNeighbors follows a live source
			continue
		}
		if _, skipped := reasons[candidate.desc()]; !skipped {
			continue
		}
		if source == nil || compareSkipped(reasons, candidate.desc(), best) < 0 {
			best, source = candidate.desc(), claimed
		}
	}
	if source == nil {
		return stepSkip{}, false
	}

	skip := reasons[best]
	if skip.stillRunning != nil {
		return skip, true
	}

	return stepSkip{service: desc.Service, removedFrom: source.name, closing: skip.closing}, true
}

// sourceScopeOf returns the source scope of export that scope is, or is a
// descendant of, scope being within that source.
func sourceScopeOf(export serviceWrapperExport, scope *Scope) *Scope {
	for scope.parentScope != nil && export.withinSource(scope.parentScope) {
		scope = scope.parentScope
	}

	return scope
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
	// once per service, before any service that depends on it reads its
	// reasons (asReached, below its own connect)
	c.checkDetachedExport(v)

	var best reachedSkip
	for _, n := range c.run.root.dependencyNeighbors(v) {
		if _, seen := c.index[n]; !seen {
			c.connect(n)
			c.low[v] = min(c.low[v], c.low[n])
		} else if c.onStack[n] {
			c.low[v] = min(c.low[v], c.index[n])
		}
		// n's own reasons are known once its connect began
		best = c.better(best, c.asReached(n))
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
