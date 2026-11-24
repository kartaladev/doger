package doger

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"reflect"
	"slices"
	"sync"
)

// startPhase is the start state of a root container. It is an int32 so it
// shares a word with closePhase in RootScope.
type startPhase int32

const (
	startIdle startPhase = iota
	startStarting
	startStarted
	startReady // every Started hook returned
	startFailed
)

// Start builds and starts the container with context.Background(); see
// [RootScope.StartWithContext].
func (s *RootScope) Start() error {
	return s.StartWithContext(context.Background())
}

// StartWithContext starts the container: it builds every candidate service,
// then starts the Starters.
//
// Candidates are the non-transient, non-alias services of the root scope and
// of every descendant scope that exists when Start takes its snapshot that
// were registered with [AsStarter], or whose registered type implements a
// Starter variant when [InjectorOpts.EnableStarterAutoDetection] is set.
// Services registered after Start, and scopes created after it, are not
// started until [RootScope.Refresh] or [Scope.Start]; until then,
// health reports list such a Starter as [HealthUnknown] and do not check it
// (see [RootScope.HealthReport]), so readiness stays unknown until it starts.
//
// Health: once Start succeeded and the container is ready, the
// [InjectorOpts.HealthScheduler], when set, is called before the Ready hooks.
//
// A container starts at most once: any later call returns an error matching
// [ErrAlreadyStarted], even when the first one failed, and a call after root
// Shutdown began returns an error matching [ErrShutdown]. A clone
// starts independently of its source; clones share eager instances, so an
// eager Starter is started again by the clone.
//
// Context: Start derives its own cancellable context from ctx (a nil ctx is
// treated as context.Background()). Context-aware Start(ctx) methods, kind
// handlers and runners receive that derived context, which is cancelled when
// Start returns: do not retain it for long-running work; start a goroutine
// with its own context and stop it from a Shutdowner method instead. Start
// checks the context before each candidate build, each handler call and each
// Starter, and once at the end; a cancelled or expired ctx stops Start with
// an error wrapping ctx.Err().
//
// On the first failure (a build error, a Start error, or a panic in a Start
// method, recovered as an error), Start stops and returns a [*StartError]
// that names the service, its scope and the failed step, and wraps the
// cause, so errors.Is and errors.As reach it. Starters not reached yet are
// not started. Nothing is rolled back: cleanup is the caller's job,
// typically by calling Shutdown, so services must tolerate Shutdown without
// a prior Start. A panic in a per-service hook is not recovered as an error:
// Start runs the Failed hooks, then re-raises it.
//
// Slow and stuck steps: a Starter's Start method or a kind-handler call
// still running after [InjectorOpts.SlowStartThreshold] (10s by default) is
// logged once at Warn level when a Logger is set. With
// [InjectorOpts.StartTimeout] set, a step still running after that timeout
// is abandoned: Start stops waiting and returns a [*StartError] matching
// [ErrStartTimeout], while the call keeps running in its own goroutine and
// is never made a second time while it runs. When ctx ends first, the
// step's context is cancelled at once and Start waits for it up to the
// timeout, so a step that honours its context is not abandoned. Without
// StartTimeout, Start waits for every step, so a Start method that blocks
// until shutdown (such as an HTTP server's ListenAndServe) makes Start
// block too: return from Start once the service is ready, and serve in a
// goroutine.
//
// Shutdown during Start: a root Shutdown that begins while Start runs
// cancels the Start-time context with cause [ErrShutdown] and waits
// for Start to return, bounded by its own context. Start stops before its
// next Starter or handler and returns an error matching
// ErrShutdown, and also ctx's cause when ctx was cancelled before
// Shutdown began. A Starter that ignores its context and finishes
// after Shutdown stopped waiting is not rolled back: honour ctx.
//
// Never call Shutdown synchronously from code that runs during Start (a
// Starter, a kind handler, a runner, a provider or a hook): Shutdown waits
// for Start, which waits for that code, so it deadlocks unless Shutdown's
// context has a deadline. Return an error instead. Calling Start from such
// code returns [ErrAlreadyStarted] immediately.
//
// Only [RootScope] methods are container-level: they fire the container
// hooks and own the one-shot Start. [Scope.Start] starts a scope subtree
// incrementally, as [RootScope.Refresh] does, once Start succeeded; on the
// root's own *Scope (for example [Component.Scope] of a root service) it is
// Refresh. A child scope's Shutdown waits for a run
// that is starting its services (see [Scope.ShutdownWithContext]); a
// Shutdown reached through the root's own *Scope bypasses this
// coordination.
func (s *RootScope) StartWithContext(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}

	startCtx, err := s.beginStart(ctx)
	if err != nil {
		return err
	}

	recovered, panicked, err := s.runStart(startCtx)

	// pointAfterStart: startDone is closed, so code here may call Shutdown().
	if panicked {
		s.failAndRepanic(ctx, recovered, err)
	}
	var first hookPanic
	s.dispatchAfterStart(ctx, err, &first)
	s.dispatchFirstPanic(first)

	return err
}

// failAndRepanic runs the Failed hooks with err for a panic that escaped the
// start pipeline, then re-raises it. A Failed hook panic meanwhile goes to
// HookContainerPanic when set and is logged otherwise, so that the pipeline
// panic is the one re-raised.
func (s *RootScope) failAndRepanic(ctx context.Context, recovered any, err error) {
	var first hookPanic
	s.dispatchAfterStart(ctx, err, &first)

	switch {
	case !first.panicked:
	case s.opts.HookContainerPanic != nil:
		s.opts.HookContainerPanic(s, first.point, first.value)
	default:
		s.logHookPanic(first)
	}

	panic(recovered)
}

// dispatchAfterStart fires the pointAfterStart hooks from Start's local
// outcome: Started, then Ready, on success; Failed with err otherwise.
func (s *RootScope) dispatchAfterStart(ctx context.Context, err error, first *hookPanic) {
	if err != nil {
		runObserverHooksWith(s, "container", "failed", s.hooks.containerFailed.load(), first, func(hook func(context.Context, *RootScope, error)) {
			hook(ctx, s, err)
		})
		s.publishLifecycle(ctx, ContainerFailed{Root: s, Err: err})
		return
	}

	s.self.log(slog.LevelInfo, "container started")
	s.runObserverHooks(ctx, "started", s.hooks.containerStarted.load(), first)
	s.publishLifecycle(ctx, ContainerStarted{Root: s})
	if s.markReady() {
		s.self.log(slog.LevelInfo, "container ready")
		s.startScheduling()
	}
	s.runObserverHooks(ctx, "ready", s.hooks.containerReady.load(), first)
	s.publishLifecycle(ctx, ContainerReady{Root: s})
}

// hookPanic is the first observer hook panic of one Start or Shutdown.
type hookPanic struct {
	subject  string // "container" or "scope"
	point    string // as HookContainerPanic receives it
	value    any
	panicked bool

	// services keeps the first service shutdown panic of a shutdown; nil
	// outside one
	services *shutdownPanics
}

// child returns the record of a part of p's operation run separately, such
// as a child scope shutdown: it shares p's service shutdown panics.
func (p *hookPanic) child() hookPanic {
	return hookPanic{services: p.services}
}

// callObserver calls an observer hook of subject, recovering its panic: the
// first one of the operation is kept in first, later ones are logged.
func (s *RootScope) callObserver(first *hookPanic, subject, point string, call func()) {
	if recovered, panicked := callHookSafely(call); panicked {
		s.keepFirstPanic(first, hookPanic{subject: subject, point: point, value: recovered, panicked: true})
	}
}

// keepFirstPanic keeps p in first when first holds no panic yet, and logs
// it otherwise.
func (s *RootScope) keepFirstPanic(first *hookPanic, p hookPanic) {
	if !p.panicked {
		return
	}

	if first.panicked {
		s.logHookPanic(p)
		return
	}
	p.services = first.services // the operation's service panics stay kept
	*first = p
}

// logHookPanic logs an observer hook panic that is not dispatched.
func (s *RootScope) logHookPanic(p hookPanic) {
	s.logRecoveredPanic(p.point, "", p.value)
}

// logRecoveredPanic logs a recovered panic: an Error record with its point
// and the type of its value, and the value itself only in a Debug record,
// since it could hold a secret. service is "" when it concerns none.
func (s *RootScope) logRecoveredPanic(point, service string, value any) {
	s.self.logRecoveredPanicAs(point, service, fmt.Sprintf("%T", value), value)
}

// logRecoveredPanicAs is logRecoveredPanic with panicType in place of the
// type of value, for an end that is not a panic value, such as
// runtime.Goexit.
func (s *RootScope) logRecoveredPanicAs(point, service, panicType string, value any) {
	s.self.logRecoveredPanicAs(point, service, panicType, value)
}

// logRecoveredPanic logs a recovered panic on scope s, as
// RootScope.logRecoveredPanic does.
func (s *Scope) logRecoveredPanic(point, service string, value any) {
	s.logRecoveredPanicAs(point, service, fmt.Sprintf("%T", value), value)
}

// logRecoveredPanicAs is Scope.logRecoveredPanic with panicType in place of
// the type of value.
func (s *Scope) logRecoveredPanicAs(point, service, panicType string, value any) {
	var attrs []slog.Attr
	if service != "" {
		attrs = append(attrs, slog.String("service", service))
	}
	attrs = append(attrs, slog.String("point", point))
	s.logPanicRecords(attrs, panicType, value)
}

// logPanicRecords logs the records of a recovered panic with attrs: an
// Error record with panicType, and value only in a Debug record.
func (s *Scope) logPanicRecords(attrs []slog.Attr, panicType string, value any) {
	if s.logEnabled(slog.LevelError) {
		s.log(slog.LevelError, "panic recovered", append(attrs, slog.String("panic_type", panicType))...)
	}
	if s.logEnabled(slog.LevelDebug) {
		s.log(slog.LevelDebug, "panic value", append(attrs, slog.String("value", fmt.Sprint(value)))...)
	}
}

// dispatchFirstPanic dispatches the first observer hook panic of an
// operation, if any. Call it once the operation is complete.
func (s *RootScope) dispatchFirstPanic(first hookPanic) {
	if first.panicked {
		s.dispatchHookPanic(first.point, first.value)
	}
}

// callHookSafely calls fn and recovers its panic.
func callHookSafely(fn func()) (recovered any, panicked bool) {
	defer func() {
		if r := recover(); r != nil {
			recovered, panicked = r, true
		}
	}()

	fn()

	return nil, false
}

// dispatchHookPanic passes an observer hook panic to HookContainerPanic, or
// re-panics with the recovered value when it is nil.
func (s *RootScope) dispatchHookPanic(point string, value any) {
	if s.opts.HookContainerPanic == nil {
		panic(value)
	}

	s.opts.HookContainerPanic(s, point, value)
}

// runObserverHooks runs container hooks in order, on a copy of the list,
// skipping nil entries.
func (s *RootScope) runObserverHooks(ctx context.Context, point string, hooks []func(context.Context, *RootScope), first *hookPanic) {
	runObserverHooksWith(s, "container", point, hooks, first, func(hook func(context.Context, *RootScope)) { hook(ctx, s) })
}

// observerHook is the type of an observer hook: a container or scope hook
// without a veto.
type observerHook interface {
	~func(context.Context, *RootScope) |
		~func(context.Context, *Scope) |
		~func(context.Context, *RootScope, error) |
		~func(context.Context, *RootScope, *ShutdownReport)
}

// runObserverHooksWith calls each non-nil observer hook of subject
// ("container" or "scope") through call, in order, on a copy of the list,
// recovering their panics into first under point.
func runObserverHooksWith[H observerHook](root *RootScope, subject, point string, hooks []H, first *hookPanic, call func(hook H)) {
	for _, hook := range slices.Clone(hooks) {
		if hook != nil {
			root.callObserver(first, subject, point, func() { call(hook) })
		}
	}
}

// markReady moves the start phase from started to ready. It reports whether
// the container became ready while root Shutdown has not begun.
func (s *RootScope) markReady() bool {
	s.lifecycleMu.Lock()
	defer s.lifecycleMu.Unlock()

	if s.startPhase != startStarted {
		return false
	}
	s.startPhase = startReady

	return s.closePhase == closeOpen
}

// beginStart moves the start phase from idle to starting.
// It returns the Start-time context, derived from ctx and cancelled when
// Start returns.
func (s *RootScope) beginStart(ctx context.Context) (context.Context, error) {
	s.lifecycleMu.Lock()
	defer s.lifecycleMu.Unlock()

	if s.closePhase != closeOpen {
		return nil, libErrorf("cannot start: %w", ErrShutdown)
	}
	if s.startPhase != startIdle {
		return nil, ErrAlreadyStarted
	}

	startCtx, startCancel := context.WithCancelCause(ctx)
	s.startPhase = startStarting
	s.startCancel = startCancel
	s.startDone = make(chan struct{})

	return startCtx, nil
}

// runStart runs the pipeline. A panic that escapes it, such as one from a
// per-service hook, is recovered, and err is then the error the Failed
// hooks receive. Its deferred finishStart runs either way, so the lifecycle
// state is always released and the outcome carries the abort cause.
func (s *RootScope) runStart(startCtx context.Context) (recovered any, panicked bool, err error) {
	panicked = true
	defer func() {
		if panicked {
			recovered = recover()
			err = recoverAsError(recovered)
		}
		s.finishStart(startCtx, &err)
	}()

	s.pipelineMu.Lock()
	defer s.pipelineMu.Unlock()

	err = s.runStartPipeline(startCtx)
	panicked = false

	return nil, false, err
}

// finishStart records the outcome of Start, ends the Start-time context and
// releases its waiters. Once Shutdown began, the outcome is abortOutcome's.
func (s *RootScope) finishStart(startCtx context.Context, err *error) {
	s.lifecycleMu.Lock()
	defer s.lifecycleMu.Unlock()

	if s.closePhase != closeOpen {
		*err = abortOutcome(startCtx, *err)
	}

	if *err != nil {
		s.startPhase = startFailed
	} else {
		s.startPhase = startStarted
	}
	s.startCancel(nil)
	close(s.startDone)
	s.runScopes = nil
}

// abortOutcome is the outcome of a run that Shutdown began during: a run
// that succeeded after its last abort check is reported as aborted, and a
// failure also carries the abort cause unless it already does. The abort
// cause always matches ErrShutdown, even when the caller cancelled
// first. lifecycleMu must be held, so that beginClose has set the cause.
func abortOutcome(ctx context.Context, err error) error {
	cause := shutdownCause(ctx)
	switch {
	case err == nil:
		return libErrorf("start aborted: %w", cause)
	case errors.Is(err, ErrShutdown):
		return err
	case errors.Is(err, context.Cause(ctx)):
		// carries the caller's cause already
		return abortedWith(err, ErrShutdown)
	default:
		return abortedWith(err, cause)
	}
}

// abortedWith returns err followed by the abort cause, as
// "<err> (start aborted: <cause>)". The message is led by err, so it carries
// the library prefix only when err does.
func abortedWith(err, cause error) error {
	return ledErrorf(err, "%w (start aborted: %w)", cause)
}

// shutdownCause returns the cause of the run's cancellation, made to match
// ErrShutdown when the caller cancelled before Shutdown did.
func shutdownCause(ctx context.Context) error {
	cause := context.Cause(ctx)
	if errors.Is(cause, ErrShutdown) {
		return cause
	}

	return fmt.Errorf("%w: %w", ErrShutdown, cause)
}

// startCandidate is a service discovered by Start.
type startCandidate struct {
	placement
	scope    *Scope
	service  serviceWrapperAny
	starter  bool
	kinds    []kindBinding
	instance any
}

func (s *RootScope) runStartPipeline(startCtx context.Context) error {
	// pointStarting
	if err := s.runStartingHooks(startCtx); err != nil {
		return err
	}
	s.publishLifecycle(startCtx, ContainerStarting{Root: s})

	if err := checkAbort(startCtx); err != nil {
		return err
	}

	if _, err := s.runPipeline(startCtx, []*Scope{s.self}); err != nil {
		return err
	}

	return s.validateEventTransport()
}

// runPipeline builds, handles and starts what is not started or handled yet
// in the subtrees of roots, with the abort checks of start-lifecycle D7.
// Start, Refresh and Scope.Start share it. closing reports that every root
// was shutting down, or under a scope that was, when the candidates were
// claimed, so that nothing of them was started.
func (s *RootScope) runPipeline(ctx context.Context, roots []*Scope) (closing bool, err error) {
	candidates, closing := s.claimCandidates(roots, s.discoverCandidates(roots))
	run := &startRun{root: s}

	if err := buildCandidates(ctx, candidates); err != nil {
		return closing, err
	}

	if err := s.runStage(ctx, run, StageBeforeStart, candidates); err != nil {
		return closing, err
	}

	if err := s.checkCycles(candidates); err != nil {
		return closing, withAbortCause(ctx, err)
	}

	if err := s.startStarters(ctx, run, s.orderByDependencies(starterCandidates(candidates))); err != nil {
		return closing, err
	}

	// pointStartersUp
	if err := s.runStage(ctx, run, StageAfterStart, candidates); err != nil {
		return closing, err
	}

	if err := checkAbort(ctx); err != nil {
		return closing, err
	}

	return closing, run.pendingError()
}

// startRun is the state of one run of the start pipeline that its stages
// share: the services with a step it skipped, and the first pending error,
// which the run returns when no step failed.
type startRun struct {
	root *RootScope

	mu      sync.Mutex
	skipped map[ServiceDescription]stepSkip
	// skipCount is how many times skipped changed
	skipCount uint64
	pending   error
}

// stepSkip is why the run skipped a step of a service: an abandoned call
// of it still runs, or a shutdown of its scope, or of an ancestor scope,
// began, before its call or by cancelling it; or both.
type stepSkip struct {
	service string
	// closing is the scope shutdown's cancellation cause, which matches
	// ErrShutdown, or nil when the step was skipped only because its
	// abandoned call still runs
	closing error
	// stillRunning is the step's timeout error while its call, abandoned
	// at StartTimeout, still runs, or nil
	stillRunning error
}

// dependentError is the run's error for step, skipped outside a closing
// scope because it depends on the skipped service: the service's timeout
// error while its abandoned call still runs, otherwise a *StartError naming
// step's service that matches ErrShutdown.
func (skip stepSkip) dependentError(step startStep) error {
	if skip.stillRunning != nil {
		return skip.stillRunning
	}

	return step.startError(libErrorf("dependency `%s` was not started: %w", skip.service, skip.closing))
}

// skip records that the run skipped a step of candidate. A step skipped
// only because its abandoned call still runs is the run's pending error
// unless one is already set; a step of candidate still running stays
// recorded as such.
func (r *startRun) skip(candidate *startCandidate, skip stepSkip) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.skipped == nil {
		r.skipped = map[ServiceDescription]stepSkip{}
	}
	if skip.closing == nil && r.pending == nil {
		r.pending = skip.stillRunning
	}
	desc := candidate.desc()
	if previous, ok := r.skipped[desc]; ok && previous.stillRunning != nil {
		return
	}
	skip.service = candidate.name
	r.skipped[desc] = skip
	r.skipCount++
}

// skipCancelled records a step that a scope shutdown cancelled, which
// ended as end, as skipped because its scope is shutting down.
func (r *startRun) skipCancelled(startCtx context.Context, step startStep, end stepEnd) {
	skip := stepSkip{closing: end.cancelled}
	if end.abandoned {
		skip.stillRunning = withAbortCause(startCtx, step.startError(end.err))
	}
	r.skip(step.candidate, skip)
}

// skippedStep reports whether the run skipped a step of candidate.
func (r *startRun) skippedStep(candidate *startCandidate) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	_, skipped := r.skipped[candidate.desc()]
	return skipped
}

// pendingError returns the run's first pending error: that of the first
// step it skipped because an abandoned call of it still runs, or of the
// first dependent of a closing scope's skipped step it skipped outside
// that scope; nil when there is none.
func (r *startRun) pendingError() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	return r.pending
}

// claim is claimStep for the run: it reports whether step is skipped,
// because its scope or an ancestor scope is shutting down, because it is
// already recorded, or because an abandoned call of it still runs, which it
// records in the run.
func (r *startRun) claim(startCtx context.Context, step startStep) (skip bool) {
	if closing := r.root.closingScope(step.candidate.scope); closing != nil {
		recorded, err := r.root.claimStep(step)
		if recorded {
			// started or handled since discovery: its dependents may start
			return true
		}
		skip := stepSkip{closing: scopeShutdownCause(closing)}
		// a step whose abandoned call still runs keeps its own error
		if err != nil {
			skip.stillRunning = withAbortCause(startCtx, err)
		}
		r.skip(step.candidate, skip)
		return true
	}
	skip, err := r.root.claimStep(step)
	if err != nil {
		r.skip(step.candidate, stepSkip{stillRunning: withAbortCause(startCtx, err)})
		return true
	}

	return skip
}

// runStartingHooks runs the container Starting hooks in order. The first
// error or panic is a veto: the remaining hooks do not run and Start fails
// with it.
func (s *RootScope) runStartingHooks(startCtx context.Context) error {
	if err := runVetoHooks(startCtx, s.hooks.containerStarting.load(), s, startingHookPanicError("container")); err != nil {
		return withAbortCause(startCtx, libErrorf("container start aborted: %w", err))
	}

	return nil
}

// runVetoHooks calls each non-nil hook with arg, in order. The first error,
// or a panic turned into an error by panicError, stops the loop and is
// returned.
func runVetoHooks[T any](ctx context.Context, hooks []func(context.Context, T) error, arg T, panicError func(recovered any) error) error {
	for _, hook := range slices.Clone(hooks) {
		if hook == nil {
			continue
		}

		var err error
		if recovered, panicked := callHookSafely(func() { err = hook(ctx, arg) }); panicked {
			err = panicError(recovered)
		}
		if err != nil {
			return err
		}
	}

	return nil
}

// startingHookPanicError returns how a panic in a Starting hook of subject
// ("container" or "scope") becomes a veto error. An error value is wrapped,
// so errors.Is and errors.As reach it.
func startingHookPanicError(subject string) func(recovered any) error {
	return func(recovered any) error {
		if err, ok := recovered.(error); ok {
			return libErrorf("%s starting hook panicked: %w", subject, err)
		}

		return libErrorf("%s starting hook panicked: %v", subject, recovered)
	}
}

func (c *startCandidate) desc() ServiceDescription {
	return newServiceDescription(c.scope.ID(), c.scope.Name(), c.name)
}

// discoverCandidates snapshots the subtrees of roots breadth-first and
// returns the services the pipeline must build, ordered by absolute scope
// depth then registration order. Starters already started are skipped.
func (s *RootScope) discoverCandidates(roots []*Scope) []*startCandidate {
	candidates := []*startCandidate{}

	type level struct {
		scope *Scope
		depth int
	}
	queue := make([]level, 0, len(roots))
	for _, root := range roots {
		queue = append(queue, level{scope: root, depth: scopeDepth(root)})
	}
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]

		current.scope.serviceForEach(func(name string, scope *Scope, serviceAny any) bool {
			if candidate := s.newStartCandidate(scope, name, current.depth, serviceAny); candidate != nil {
				candidates = append(candidates, candidate)
			}
			return true
		})

		for _, child := range current.scope.Children() {
			queue = append(queue, level{scope: child, depth: current.depth + 1})
		}
	}

	slices.SortFunc(candidates, func(a, b *startCandidate) int {
		return a.compare(b.placement)
	})

	return candidates
}

// claimCandidates drops the candidates of scopes that are shutting down, or
// whose ancestor is, and those their scope no longer holds (a shutdown that
// ran entirely after discovery), and records the scopes of the others, so
// that a child scope shutdown waits for this run (see
// Scope.ShutdownWithContext). closing reports that every root was shutting
// down, or under a scope that was.
func (s *RootScope) claimCandidates(roots []*Scope, candidates []*startCandidate) (claimed []*startCandidate, closing bool) {
	s.lifecycleMu.Lock()
	defer s.lifecycleMu.Unlock()

	// a shutdown marks its scope closing under lifecycleMu before it removes
	// services, so a candidate here is closing, still held, or removed
	candidates = slices.DeleteFunc(candidates, func(c *startCandidate) bool {
		return closingInLine(c.scope) || !stillRegistered(c)
	})
	s.runScopes = make(map[*Scope]struct{}, len(candidates))
	for _, candidate := range candidates {
		s.runScopes[candidate.scope] = struct{}{}
	}

	return candidates, !slices.ContainsFunc(roots, func(root *Scope) bool { return !closingInLine(root) })
}

// closingInLine reports whether scope or one of its ancestors is shutting
// down. lifecycleMu must be held.
func closingInLine(scope *Scope) bool {
	return closingScopeInLine(scope) != nil
}

// closingScopeInLine returns scope or the nearest ancestor of it that is
// shutting down, or nil when none is. lifecycleMu must be held.
func closingScopeInLine(scope *Scope) *Scope {
	for ; scope != nil; scope = scope.parentScope {
		if scope.closing != nil && scope.closing.count > 0 {
			return scope
		}
	}

	return nil
}

// newStartCandidate returns the candidate for a registered service, or nil
// when Start has nothing to do with it.
func (s *RootScope) newStartCandidate(scope *Scope, name string, depth int, serviceAny any) *startCandidate {
	service, ok := serviceAny.(serviceWrapperAny)
	if !ok {
		return nil
	}

	serviceType := service.getServiceType()
	if serviceType == ServiceTypeTransient || serviceType == ServiceTypeAlias {
		return nil
	}

	starter := s.declaresStarter(service) && !s.isStarted(service)
	// a component is handled at most once per kind per container
	kinds := slices.DeleteFunc(slices.Clone(s.kindBindings(service)), func(b kindBinding) bool { return s.isHandled(service, b.id) })
	if !starter && len(kinds) == 0 {
		return nil
	}
	// a default that yields is an alias of its target, which Start handles
	// through its own registration
	if isYieldedDefaultIn(scope, serviceAny) {
		return nil
	}

	return &startCandidate{
		name:     name,
		depth:    depth,
		regIndex: service.getRegistrationIndex(),
		scope:    scope,
		service:  service,
		starter:  starter,
		kinds:    kinds,
	}
}

// declaresStarter reports whether Start, Refresh and Scope.Start start
// service; see isStartCandidate.
func (s *RootScope) declaresStarter(service serviceWrapperAny) bool {
	return isStartCandidate(s.opts, service)
}

// kindBindings returns the kinds service is opted into: those given at
// registration, plus the Runnable kind when EnableRunnerAutoDetection
// detects a Runner. Transient services and aliases have none.
func (s *RootScope) kindBindings(service serviceWrapperAny) []kindBinding {
	if serviceType := service.getServiceType(); serviceType == ServiceTypeTransient || serviceType == ServiceTypeAlias {
		return nil
	}

	var kinds []kindBinding
	if opts := service.getOptions(); opts != nil {
		kinds = opts.kinds
	}
	if s.opts.EnableRunnerAutoDetection && typeCanCastToType(service.getReflectType(), runnableKind.target()) &&
		!slices.ContainsFunc(kinds, func(b kindBinding) bool { return b.id == runnableKind.id }) {
		// implicit binding, so a runner opted in explicitly still runs once
		kinds = append(slices.Clone(kinds), kindBinding{id: runnableKind.id, meta: struct{}{}, target: runnableKind.target()})
	}

	return kinds
}

// kindEntries returns the kind handler entries, in order of first Handle.
// The list is never written in place, so it is read after kindsMu is
// released.
func (s *RootScope) kindEntries() []kindEntry {
	s.kindsMu.Lock()
	defer s.kindsMu.Unlock()

	return s.kinds
}

// isStartCandidate reports whether a container with opts starts service: it
// opted in with AsStarter, or its registered type is a Starter and
// EnableStarterAutoDetection is on. Transient services and aliases never are.
func isStartCandidate(opts *InjectorOpts, service serviceWrapperAny) bool {
	if serviceType := service.getServiceType(); serviceType == ServiceTypeTransient || serviceType == ServiceTypeAlias {
		return false
	}

	serviceOpts := service.getOptions()
	return (serviceOpts != nil && serviceOpts.starter) ||
		(opts.EnableStarterAutoDetection && typeIsStarter(service.getReflectType()))
}

// buildCandidates invokes every candidate, keeping its instance.
func buildCandidates(startCtx context.Context, candidates []*startCandidate) error {
	for _, candidate := range candidates {
		if err := checkAbort(startCtx); err != nil {
			return err
		}

		instance, err := invokeAnyByName(candidate.scope, candidate.name)
		if err != nil {
			err = newStartError(StartOpBuild, candidate.scope, candidate.name, "", err)
			return withAbortCause(startCtx, err)
		}
		candidate.instance = instance

		for _, binding := range candidate.kinds {
			if !binding.deferred {
				continue
			}
			if instance == nil || !reflect.TypeOf(instance).Implements(binding.target) {
				err := libErrorf("service `%s` does not implement `%s` required by kind `%s`", candidate.name, binding.target.String(), binding.id.name)
				return withAbortCause(startCtx, newStartError(StartOpBuild, candidate.scope, candidate.name, "", err))
			}
		}
	}

	return nil
}

// starterCandidates returns the Starter candidates, keeping their order.
func starterCandidates(candidates []*startCandidate) []*startCandidate {
	starters := make([]*startCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		if candidate.starter {
			starters = append(starters, candidate)
		}
	}

	return starters
}

// startStarters starts the Starter candidates in order, stopping at the
// first error. With StartParallelism n > 1, independent Starters start
// concurrently.
func (s *RootScope) startStarters(startCtx context.Context, run *startRun, candidates []*startCandidate) error {
	// more workers than Starters is pointless; clamping keeps the int
	// conversion in range for huge values such as ^uint(0)
	if n := min(s.opts.StartParallelism, uint(len(candidates))); n > 1 {
		return s.startStartersParallel(startCtx, run, candidates, int(n))
	}

	var check *dependentsCheck
	for _, candidate := range candidates {
		if err := checkAbort(startCtx); err != nil {
			return err
		}

		check = run.dependentsCheckSince(check)
		if check.skips(startStep{candidate: candidate, op: StartOpStart}) {
			continue
		}

		if err := s.startCandidate(startCtx, run, candidate); err != nil {
			return err
		}
	}

	return nil
}

// startStartersParallel starts up to n Starters at a time, each once every
// Starter it depends on has started, picking the ready one with the smallest
// scope depth, then registration index. The first failure stops scheduling;
// running Starters are awaited before it is returned. A Starter skipped
// because an abandoned call of it still runs is not a failure: scheduling
// goes on, and the Starters that depend on it are never scheduled. A panic in a
// per-service hook stops scheduling too, and is re-raised on the calling
// goroutine once the running Starters have returned. Every goroutine has
// returned when it returns.
func (s *RootScope) startStartersParallel(startCtx context.Context, run *startRun, candidates []*startCandidate, n int) error {
	requires := s.dependencyRequirements(candidates)
	pending := slices.Clone(candidates)
	slices.SortFunc(pending, func(a, b *startCandidate) int {
		return a.compare(b.placement)
	})
	started := map[ServiceDescription]bool{}

	outcomes := make(chan starterOutcome)
	var check *dependentsCheck
	running := 0
	var firstErr error
	var firstPanic *starterOutcome

	for {
		for firstErr == nil && firstPanic == nil && running < n {
			// dependents of a still-running step are never scheduled; one
			// check answers every pass until what it read changes
			check = run.dependentsCheckSince(check)
			pending = slices.DeleteFunc(pending, func(c *startCandidate) bool {
				return check.skips(startStep{candidate: c, op: StartOpStart})
			})
			ready := slices.IndexFunc(pending, func(c *startCandidate) bool {
				return !slices.ContainsFunc(requires[c.desc()], func(d ServiceDescription) bool { return !started[d] })
			})
			if ready < 0 {
				break
			}
			if err := checkAbort(startCtx); err != nil {
				firstErr = err
				break
			}

			candidate := pending[ready]
			pending = slices.Delete(pending, ready, ready+1)
			running++
			go s.startCandidateTo(startCtx, run, candidate, outcomes)
		}

		if running == 0 {
			if firstPanic != nil {
				panic(firstPanic.recovered)
			}
			return firstErr
		}

		o := <-outcomes
		running--
		if o.panicked {
			if firstPanic == nil {
				firstPanic = &o
			}
			continue
		}
		if o.err != nil && firstErr == nil {
			firstErr = o.err
		}
		if o.err == nil && !run.skippedStep(o.candidate) {
			started[o.candidate.desc()] = true
		}
	}
}

// starterOutcome is how the start of one Starter of a parallel start ended.
type starterOutcome struct {
	candidate *startCandidate
	err       error
	panicked  bool // a per-service hook panicked
	recovered any  // its panic value, when panicked
}

// startCandidateTo starts candidate with startCandidate and sends how it
// ended to outcomes, a panic included.
func (s *RootScope) startCandidateTo(startCtx context.Context, run *startRun, candidate *startCandidate, outcomes chan<- starterOutcome) {
	o := starterOutcome{candidate: candidate, panicked: true}
	defer func() {
		if o.panicked {
			o.recovered = recover()
		}
		outcomes <- o
	}()
	o.err = s.startCandidate(startCtx, run, candidate)
	o.panicked = false
}

// startCandidate starts one Starter candidate with its hooks. A candidate
// with an abandoned Start call still running is skipped and recorded in
// run, and nil is returned.
func (s *RootScope) startCandidate(startCtx context.Context, run *startRun, candidate *startCandidate) error {
	if !instanceIsStarter(candidate.instance) {
		startErr := newStartError(StartOpStart, candidate.scope, candidate.name, "", libErrorf("service `%s` was opted in with AsStarter but does not implement any Starter interface", candidate.name))
		startErr.unprefixed = true // the cause names the service already
		return startErr
	}

	step := startStep{candidate: candidate, op: StartOpStart}
	if run.claim(startCtx, step) {
		return nil
	}

	op := serviceOp{ctx: startCtx, scope: candidate.scope, name: candidate.name}
	s.hooks.onBeforeStart(op)
	op = beginServiceOp(startCtx, candidate.scope, candidate.name)
	end := s.runStartStep(startCtx, step, func(ctx context.Context) error {
		return startInstance(ctx, candidate.instance)
	})
	s.hooks.onAfterStart(op, op.elapsed(), end.err)

	return s.endStep(startCtx, run, step, end)
}

// endStep settles step, whose wait ended as end: a step a scope shutdown
// cancelled is skipped in run, since the scope shuts it down; a failed one
// returns the run's error; a successful one is recorded.
func (s *RootScope) endStep(startCtx context.Context, run *startRun, step startStep, end stepEnd) error {
	if end.skipped() {
		run.skipCancelled(startCtx, step, end)
		return nil
	}
	if end.err != nil {
		return withAbortCause(startCtx, step.startError(end.err))
	}
	// a service shut down meanwhile (a bounded scope shutdown gave up
	// waiting) stays out of the bookkeeping
	s.recordIfHeld(step)

	return nil
}

// stillRegistered reports whether the candidate's scope still holds its
// wrapper.
func stillRegistered(candidate *startCandidate) bool {
	return scopeHolds(candidate.scope, candidate.name, candidate.service)
}

// startInstance calls the Start method of the instance's Starter variant.
// The type switch mirrors the one of the shutdown path. A panic is
// recovered by runStartStep, its caller.
func startInstance(ctx context.Context, instance any) error {
	switch instance := instance.(type) {
	case StarterWithContextAndError:
		return instance.Start(ctx)
	case StarterWithError:
		return instance.Start()
	case StarterWithContext:
		instance.Start(ctx)
	case Starter:
		instance.Start()
	}

	return nil
}

// instanceIsStarter reports whether instance implements a Starter variant.
func instanceIsStarter(instance any) bool {
	switch instance.(type) {
	case StarterWithContextAndError, StarterWithError, StarterWithContext, Starter:
		return true
	}

	return false
}

// recoverAsError turns a recovered panic value into an error. An error value
// is wrapped, so errors.Is and errors.As reach it.
func recoverAsError(r any) error {
	if err, ok := r.(error); ok {
		return libErrorf("recovered panic: %w", err)
	}

	return libErrorf("recovered panic: %v", r)
}

// checkAbort returns an error wrapping the cause when the Start-time context
// is done: the caller's ctx.Err(), or ErrShutdown when Shutdown
// cancelled Start.
func checkAbort(startCtx context.Context) error {
	if startCtx.Err() != nil {
		return libErrorf("start aborted: %w", context.Cause(startCtx))
	}

	return nil
}

// withAbortCause additionally wraps the abort cause into a step error when
// the step failed while the Start-time context was done.
func withAbortCause(startCtx context.Context, err error) error {
	if startCtx.Err() != nil {
		return abortedWith(err, context.Cause(startCtx))
	}

	return err
}

// runStage runs the handlers registered for stage, kind by kind in
// order of first Handle, each over its components in dependency order.
func (s *RootScope) runStage(startCtx context.Context, run *startRun, stage Stage, candidates []*startCandidate) error {
	entries := s.kindEntries()

	var check *dependentsCheck
	for _, entry := range entries {
		if entry.stage != stage {
			continue
		}

		components := []*startCandidate{}
		metas := map[*startCandidate]any{}
		for _, candidate := range candidates {
			for _, binding := range candidate.kinds {
				if binding.id == entry.id {
					components = append(components, candidate)
					metas[candidate] = binding.meta
				}
			}
		}

		for _, component := range s.orderByDependencies(components) {
			if err := checkAbort(startCtx); err != nil {
				return err
			}

			step := startStep{candidate: component, op: StartOpHandle, kind: entry.id}
			check = run.dependentsCheckSince(check)
			if check.skips(step) {
				continue
			}

			if run.claim(startCtx, step) {
				continue
			}
			end := s.runStartStep(startCtx, step, func(ctx context.Context) error {
				return callKindHandler(ctx, entry, component, metas[component])
			})
			if err := s.endStep(startCtx, run, step, end); err != nil {
				return err
			}
		}
	}

	return nil
}

// callKindHandler calls entry's handler for one component. A panic is
// recovered by runStartStep, its caller.
func callKindHandler(startCtx context.Context, entry kindEntry, component *startCandidate, meta any) error {
	return entry.run(startCtx, component.scope, component.name, component.instance, meta)
}
