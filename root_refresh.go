package doger

import (
	"context"
	"slices"
	"strings"
)

// Refresh starts what is not started yet, with context.Background(); see
// [RootScope.RefreshWithContext].
func (s *RootScope) Refresh() error {
	return s.RefreshWithContext(context.Background())
}

// RefreshWithContext runs the start pipeline over the services and scopes of
// the whole container that are not started or handled yet: services
// registered after Start, and scopes created after it. It builds them, runs
// kind handlers for their new components and starts their Starters, with the
// same order, context, error and per-service hook rules as Start. Each
// service is started successfully at most once per container: a Starter
// whose build or Start failed, or a component whose kind handler failed, is
// tried again by the next Refresh or Scope.Start that covers it. Refresh
// fires no container hook.
//
// It returns an error matching [ErrNotStarted] until the first Start
// succeeded, at once and without waiting while that Start is still running,
// and one matching [ErrShutdown] once root Shutdown began, which
// takes precedence; it starts nothing then. Concurrent Refresh
// and [Scope.Start] calls run one at a time. Never call Refresh
// synchronously from code that runs during a Refresh or Scope.Start: it
// waits for itself.
func (s *RootScope) RefreshWithContext(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}

	_, err := s.startIncremental(ctx, s.self)
	return err
}

// startIncremental runs the pipeline over the subtree of root. For a child
// scope, the scope Starting hooks run first and can veto it. closing reports
// that root was shutting down, or under a scope that was, so that nothing of
// it was started, or that a shutdown of root or of an ancestor began after
// the run claimed its candidates, so that the run skipped what that
// shutdown cancelled.
func (s *RootScope) startIncremental(ctx context.Context, root *Scope) (closing bool, err error) {
	// fail fast instead of waiting for a running Start on pipelineMu
	if err := s.incrementalPhaseError(); err != nil {
		return false, err
	}

	s.pipelineMu.Lock()
	defer s.pipelineMu.Unlock()

	incCtx, err := s.beginIncremental(ctx)
	if err != nil {
		return false, err
	}
	defer func() {
		if s.finishIncremental(incCtx, &err) {
			closing = true
		}
	}()

	if root != s.self {
		// a scope shutting down starts nothing and runs no scope hook
		if s.scopeClosing(root) {
			return true, nil
		}

		if err := s.runScopeStartingHooks(incCtx, root); err != nil {
			return false, err
		}
	}

	return s.runPipeline(incCtx, []*Scope{root})
}

// scopeClosing reports whether scope or one of its ancestors is shutting
// down.
func (s *RootScope) scopeClosing(scope *Scope) bool {
	s.lifecycleMu.Lock()
	defer s.lifecycleMu.Unlock()

	return closingInLine(scope)
}

// closingScope returns, for a step of the running pipeline, scope or the
// nearest ancestor of it that is shutting down, or else, while root
// Shutdown has not begun, the nearest scope in that line whose shutdown
// already completed (finishScopeClose): a bounded scope shutdown that gave
// up waiting for the run removed the scope's services and detached it, and
// the run must skip its later steps as it skips those of a closing scope.
// Such a shutdown joined the run before (joinClose), so a run on that
// scope also reports it (noteRunRootShutdown). It returns nil when there
// is none.
func (s *RootScope) closingScope(scope *Scope) *Scope {
	s.lifecycleMu.Lock()
	defer s.lifecycleMu.Unlock()

	return s.closingScopeLocked(scope)
}

// closingScopeLocked is closingScope with lifecycleMu held.
func (s *RootScope) closingScopeLocked(scope *Scope) *Scope {
	if closing := closingScopeInLine(scope); closing != nil {
		return closing
	}
	if !s.scopeShutdownSkipsSteps() {
		return nil
	}
	for ; scope != nil; scope = scope.parentScope {
		if scope.closing != nil && scope.closing.done {
			return scope
		}
	}

	return nil
}

// runScopeStartingHooks runs the scope Starting hooks for scope in order.
// The first error or panic is a veto: the remaining hooks do not run and
// Scope.Start fails with it.
func (s *RootScope) runScopeStartingHooks(ctx context.Context, scope *Scope) error {
	if err := runVetoHooks(ctx, s.hooks.scopeStarting.load(), scope, startingHookPanicError("scope")); err != nil {
		return withAbortCause(ctx, libErrorf("scope `%s` start aborted: %w", scope.Name(), err))
	}

	return nil
}

// incrementalPhaseError returns why the container may not start
// incrementally now, or nil.
func (s *RootScope) incrementalPhaseError() error {
	s.lifecycleMu.Lock()
	defer s.lifecycleMu.Unlock()

	return s.checkIncrementalPhase()
}

// checkIncrementalPhase returns why the container may not start
// incrementally, or nil. lifecycleMu must be held.
func (s *RootScope) checkIncrementalPhase() error {
	if s.closePhase != closeOpen {
		return libErrorf("cannot start: %w", ErrShutdown)
	}
	if s.startPhase != startStarted && s.startPhase != startReady {
		return ErrNotStarted
	}

	return nil
}

// beginIncremental checks that the container may start incrementally and
// returns the context of the run, derived from ctx.
func (s *RootScope) beginIncremental(ctx context.Context) (context.Context, error) {
	s.lifecycleMu.Lock()
	defer s.lifecycleMu.Unlock()

	// re-checked: Shutdown may have begun while waiting for pipelineMu
	if err := s.checkIncrementalPhase(); err != nil {
		return nil, err
	}

	incCtx, incCancel := context.WithCancelCause(ctx)
	s.incCancel = incCancel
	s.incDone = make(chan struct{})

	return incCtx, nil
}

// finishIncremental ends the context of the run and releases its waiters.
// Once Shutdown began, the outcome is abortOutcome's. rootShutDown reports
// that a shutdown of the run's root, or of an ancestor of it, began after
// the run claimed its candidates, whether that shutdown still runs or, once
// it gave up waiting for the run, completed already.
func (s *RootScope) finishIncremental(incCtx context.Context, err *error) (rootShutDown bool) {
	s.lifecycleMu.Lock()
	defer s.lifecycleMu.Unlock()

	if s.closePhase != closeOpen {
		*err = abortOutcome(incCtx, *err)
	}

	rootShutDown = s.runRootsShutDown
	s.incCancel(nil)
	close(s.incDone)
	s.endRunClaim()
	s.incCancel = nil
	s.incDone = nil

	return rootShutDown
}

// isStarted reports whether the Starter service was already started.
func (s *RootScope) isStarted(service any) bool {
	s.startedMu.Lock()
	defer s.startedMu.Unlock()

	_, ok := s.started[service]
	return ok
}

// markStartedLocked records that the Starter service was started.
// startedMu must be held.
func (s *RootScope) markStartedLocked(service any) {
	if s.started == nil {
		s.started = map[any]struct{}{}
	}
	s.started[service] = struct{}{}
}

// Start starts what is not started yet in this scope and its descendants,
// with context.Background(); see [Scope.StartWithContext].
func (s *Scope) Start() error {
	return s.StartWithContext(context.Background())
}

// StartWithContext does what [RootScope.RefreshWithContext] does, restricted
// to this scope and its descendants: it builds their services that are not
// started or handled yet, runs kind handlers for their new components and
// starts their Starters. Ancestor services the subtree depends on are built
// as needed but not started by it. On the root's own scope it is Refresh.
//
// It follows the rules of Refresh: [ErrNotStarted] before the container's
// first successful Start, without waiting for a running Start,
// [ErrShutdown] once root Shutdown began,
// calls serialized with Refresh, and never call it synchronously from code
// running during a Refresh or Scope.Start.
//
// On a child scope, the [InjectorOpts.HookScopeStarting] hooks run first and
// can veto it, and the [InjectorOpts.HookScopeStarted] hooks run once its
// services started. When the scope, or one of its ancestors, is shutting
// down, it starts nothing, runs no scope hook and returns nil. When such a
// shutdown begins once it collected its services, that shutdown cancels
// their running start steps, which it skips, and it returns nil without
// running the Started hooks; a shutdown of a descendant scope only skips
// that scope's services.
func (s *Scope) StartWithContext(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}

	closing, err := s.rootScope.startIncremental(ctx, s)
	if err != nil {
		return err
	}

	// a scope whose shutdown began before or during the run did not start:
	// it has no Started hooks
	if s != s.rootScope.self && !closing {
		var first hookPanic
		s.runScopeObserverHooks(ctx, "scope-started", s.rootScope.hooks.scopeStarted.load(), &first)
		s.rootScope.dispatchFirstPanic(first)
	}

	return nil
}

// isHandled reports whether kind id already handled the service.
func (s *RootScope) isHandled(service any, id *kindID) bool {
	s.startedMu.Lock()
	defer s.startedMu.Unlock()

	_, ok := s.handled[service][id]
	return ok
}

// markHandledLocked records that kind id handled the service. startedMu
// must be held. No passed-over record of that kind exists then: the kind
// handled the service through a call of its handler, which runStage makes
// only once markAttempted cleared it, and a run never passes over a kind
// that has a handler (handlers are never removed).
func (s *RootScope) markHandledLocked(service any, id *kindID) {
	addKind(&s.handled, service, id)
}

// markAttempted records that the pipeline attempts kind id for service: it
// no longer passes over that kind, so the kind holds the component back
// from health checks until it handled it (see awaitsKindHandler).
func (s *RootScope) markAttempted(service any, id *kindID) {
	s.startedMu.Lock()
	defer s.startedMu.Unlock()

	deleteKind(s.passedOver, service, id)
}

// recordClaimedKinds records, for each candidate the run claimed, which of
// the kinds it is bound to and that have not handled it the run passes
// over, having no handler now, and which it attempts: see
// awaitsKindHandler. A container without kinds pays one pass over the
// candidates.
func (s *RootScope) recordClaimedKinds(candidates []*startCandidate) {
	if !slices.ContainsFunc(candidates, func(c *startCandidate) bool { return len(c.kinds) > 0 }) {
		return
	}

	// read before startedMu is taken: kindsMu is never taken under it
	entries := s.kindEntries()

	var passed []*startCandidate
	s.startedMu.Lock()
	for _, candidate := range candidates {
		for _, binding := range candidate.kinds {
			if hasKindHandler(entries, binding.id) {
				deleteKind(s.passedOver, candidate.service, binding.id)
				continue
			}
			addKind(&s.passedOver, candidate.service, binding.id)
			passed = append(passed, candidate)
		}
	}
	s.startedMu.Unlock()

	// a removal that forgot the service before its record was made would
	// leave the record behind: drop it then, as recordIfHeld does
	for _, candidate := range passed {
		if !stillRegistered(candidate) {
			s.startedMu.Lock()
			delete(s.passedOver, candidate.service)
			s.startedMu.Unlock()
		}
	}
}

// awaitsKindHandler reports whether a kind of service that has a handler
// has not handled it yet: its handler failed, was abandoned or still runs,
// or no run reached it, as for a component registered after Ready, which
// waits for Refresh as a Starter does. A kind without a handler holds
// nothing back, nor does a kind that the last run covering service passed
// over for having no handler then and that no run attempted since: a
// handler given to it later does not demote the started component, which
// stays checked until the next Refresh or Scope.Start collects it, and is
// then unknown until that handler handled it. A service bound to no kind
// takes no lock.
func (s *RootScope) awaitsKindHandler(service serviceWrapperAny) bool {
	bindings := s.kindBindings(service)
	if len(bindings) == 0 {
		return false
	}

	// read before startedMu is taken: kindsMu is never taken under it
	entries := s.kindEntries()

	s.startedMu.Lock()
	defer s.startedMu.Unlock()

	for _, binding := range bindings {
		if !hasKindHandler(entries, binding.id) {
			continue
		}
		_, handled := s.handled[service][binding.id]
		_, passedOver := s.passedOver[service][binding.id]
		if !handled && !passedOver {
			return true
		}
	}

	return false
}

// hasKindHandler reports whether entries, the kind handler list, holds a
// handler of kind id.
func hasKindHandler(entries []kindEntry, id *kindID) bool {
	return slices.ContainsFunc(entries, func(entry kindEntry) bool { return entry.id == id })
}

// addKind adds kind id of service to *set, creating the maps it needs.
func addKind(set *map[any]map[*kindID]struct{}, service any, id *kindID) {
	if *set == nil {
		*set = map[any]map[*kindID]struct{}{}
	}
	if (*set)[service] == nil {
		(*set)[service] = map[*kindID]struct{}{}
	}
	(*set)[service][id] = struct{}{}
}

// deleteKind removes kind id of service from set, and service once it has
// no kind left.
func deleteKind(set map[any]map[*kindID]struct{}, service any, id *kindID) {
	delete(set[service], id)
	if len(set[service]) == 0 {
		delete(set, service)
	}
}

// claimStep decides, just before step's hooks and call, whether it runs:
// skip is true when step is already recorded (its Starter started, or its
// component handled by its kind), and err matches ErrStartTimeout while an
// abandoned call of step still runs, or one of the same step on its
// instance (a pointer-like one) in this container or a container sharing
// s.instances. Discovery filters recorded steps
// earlier; this check is the authoritative one, since an abandoned call can
// end in between.
func (s *RootScope) claimStep(step startStep) (skip bool, err error) {
	service := step.candidate.service

	s.startedMu.Lock()
	defer s.startedMu.Unlock()

	if s.recordedLocked(step) {
		return true, nil
	}
	if _, running := s.abandoned[service][step.kind]; running {
		return false, step.stillRunningError()
	}
	// the same instance, registered again or shared with a clone
	if s.instances.running(step) {
		return false, step.stillRunningError()
	}

	return false, nil
}

// stillRunningErrors returns, by service, the error of a step whose
// abandoned call still runs, whether a run claims that service or not: the
// Start method's step first, then the handler call of the kind first by
// name. errs is nil when no abandoned call runs. changes is the count of
// changes of the abandoned steps it reflects (see abandonedChangeCount).
func (s *RootScope) stillRunningErrors() (errs map[ServiceDescription]error, changes uint64) {
	s.startedMu.Lock()
	defer s.startedMu.Unlock()

	if len(s.abandoned) == 0 {
		return nil, s.abandonedChanges
	}
	steps := map[ServiceDescription]startStep{}
	for _, kinds := range s.abandoned {
		for _, step := range kinds {
			desc := step.candidate.desc() // reads no lock
			if first, ok := steps[desc]; !ok || compareStepKinds(step.kind, first.kind) < 0 {
				steps[desc] = step
			}
		}
	}
	errs = make(map[ServiceDescription]error, len(steps))
	for desc, step := range steps {
		errs[desc] = step.stillRunningError()
	}

	return errs, s.abandonedChanges
}

// abandonedChangeCount returns how many times a step became abandoned or
// stopped being so.
func (s *RootScope) abandonedChangeCount() uint64 {
	s.startedMu.Lock()
	defer s.startedMu.Unlock()

	return s.abandonedChanges
}

// compareStepKinds orders the step kinds of one service: the Start method
// (a nil kind) first, then the kinds by name.
func compareStepKinds(a, b *kindID) int {
	switch {
	case a == b:
		return 0
	case a == nil:
		return -1
	case b == nil:
		return 1
	default:
		return strings.Compare(a.name, b.name)
	}
}

// recordedLocked reports whether step is recorded as started or handled.
// startedMu must be held.
func (s *RootScope) recordedLocked(step startStep) bool {
	service := step.candidate.service
	if step.op == StartOpStart {
		_, ok := s.started[service]
		return ok
	}
	_, ok := s.handled[service][step.kind]

	return ok
}

// markAbandoned records that the run abandoned a call of step that still
// runs.
func (s *RootScope) markAbandoned(step startStep) {
	service := step.candidate.service

	s.startedMu.Lock()
	defer s.startedMu.Unlock()

	if s.abandoned == nil {
		s.abandoned = map[any]map[*kindID]startStep{}
	}
	if s.abandoned[service] == nil {
		s.abandoned[service] = map[*kindID]startStep{}
	}
	s.abandoned[service][step.kind] = step
	s.abandonedChanges++
	s.instances.add(step)
}

// finishAbandoned handles the end of an abandoned call of step with
// outcome err. A nil outcome records step as started or handled, unless
// its scope or the root is shutting down, or its scope no longer holds the
// service: a scope shutdown marks its scope closing, and a root shutdown
// leaves closeOpen, under lifecycleMu before it removes services, so a
// record made here is either cleared by that shutdown's forget or not made
// at all, and recordIfHeld covers the removers that do not take
// lifecycleMu. The step stops being abandoned only after that.
func (s *RootScope) finishAbandoned(step startStep, err error) {
	candidate := step.candidate

	s.lifecycleMu.Lock()
	defer s.lifecycleMu.Unlock()

	shuttingDown := closingInLine(candidate.scope) || s.closePhase != closeOpen

	// recorded before it stops being abandoned, so a run never sees it as
	// neither recorded nor abandoned while its scope still holds it
	if err == nil && !shuttingDown {
		s.recordIfHeld(step)
	}

	s.startedMu.Lock()
	defer s.startedMu.Unlock()

	delete(s.abandoned[candidate.service], step.kind)
	if len(s.abandoned[candidate.service]) == 0 {
		delete(s.abandoned, candidate.service)
	}
	s.abandonedChanges++
	s.instances.remove(step)
}

// recordIfHeld records step as started or handled while its scope holds
// its service. Removers (ShutdownNamed, a scope or root Shutdown, an
// override) remove a service under the scope lock and only then forget it,
// so a check before the record alone can miss a removal and its forget
// landing in between. The record is therefore checked again once made, and
// undone when the service is gone: either that check sees the removal, or
// the removal comes after it and its forget clears the record.
func (s *RootScope) recordIfHeld(step startStep) {
	if !stillRegistered(step.candidate) {
		return
	}
	s.seamBeforeRecord(step)

	s.startedMu.Lock()
	s.recordLocked(step)
	s.startedMu.Unlock()

	if !stillRegistered(step.candidate) {
		s.startedMu.Lock()
		s.unrecordLocked(step)
		s.startedMu.Unlock()
	}
}

// recordLocked records step as started or handled. startedMu must be held.
func (s *RootScope) recordLocked(step startStep) {
	if step.op == StartOpStart {
		s.markStartedLocked(step.candidate.service)
		return
	}
	s.markHandledLocked(step.candidate.service, step.kind)
}

// unrecordLocked drops the record of step as started or handled. startedMu
// must be held.
func (s *RootScope) unrecordLocked(step startStep) {
	service := step.candidate.service
	if step.op == StartOpStart {
		delete(s.started, service)
		return
	}
	deleteKind(s.handled, service, step.kind)
}

// forget drops service from the started, handled and abandoned sets and its health
// record once shutdown removed it from its scope, so they only hold live
// registrations. The instance tracker keeps an abandoned call of it: the
// call still runs, and only its late outcome ends it there.
func (s *RootScope) forget(service any) {
	s.health.forget(service, true)
	s.deliverTransitions()

	s.startedMu.Lock()
	defer s.startedMu.Unlock()

	delete(s.started, service)
	delete(s.handled, service)
	delete(s.passedOver, service)
	if _, ok := s.abandoned[service]; ok {
		delete(s.abandoned, service)
		s.abandonedChanges++
	}
}
