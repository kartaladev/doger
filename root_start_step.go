package doger

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"sync"
	"time"
)

// DefaultSlowStartThreshold is the SlowStartThreshold used when it is 0:
// a start step still running after it is logged at Warn level.
const DefaultSlowStartThreshold = 10 * time.Second

// startStep is one call of user start code for a candidate of Start,
// Refresh or Scope.Start: its Starter Start method, or one kind handler
// call for it.
type startStep struct {
	candidate *startCandidate
	op        StartOp // StartOpStart or StartOpHandle
	kind      *kindID // the handler's kind; nil unless op is StartOpHandle
}

// startSeam is a test seam into the start pipeline: optional callbacks run
// at points that no public hook reaches, so that tests in this package can
// pin an interleaving. Every callback is nil in production, and set only by
// tests, before the run it observes begins.
type startSeam struct {
	// beforeRecord runs between the check that the step's scope still holds
	// its service and the record of the step as started or handled.
	beforeRecord func(step startStep)
	// afterStepDone runs in awaitStep between the end of its wait and the
	// decision of the step's outcome.
	afterStepDone func(step startStep)
}

// seamBeforeRecord runs the beforeRecord seam, if set.
func (s *RootScope) seamBeforeRecord(step startStep) {
	if s.startSeam.beforeRecord != nil {
		s.startSeam.beforeRecord(step)
	}
}

// seamAfterStepDone runs the afterStepDone seam, if set.
func (s *RootScope) seamAfterStepDone(step startStep) {
	if s.startSeam.afterStepDone != nil {
		s.startSeam.afterStepDone(step)
	}
}

// runStartStep runs call, the user code of step, with a child of the run's
// context ctx, recovering a panic as an error. Every Starter Start method
// and every kind handler call of the start pipeline goes through it.
//
// The step's context is registered with its scope while the run runs or
// awaits the step, so that a shutdown of that scope, or of an ancestor
// scope, cancels it (see Scope.ShutdownWithContext). The step is then
// neither recorded nor a failure of the run, whatever it returned, and
// end.cancelled holds the shutdown's cause; a cancelled call that returned
// an error or panicked is logged, since the run does not report it.
func (s *RootScope) runStartStep(ctx context.Context, step startStep, call func(context.Context) error) (end stepEnd) {
	stopWarning := s.armSlowStartWarning(step)
	defer stopWarning()

	stepCtx, running := s.registerStep(ctx, step.candidate.scope)
	var outcome stepOutcome
	defer func() {
		end.cancelled = s.unregisterStep(running)
		if end.skipped() && !end.abandoned {
			logCancelledOutcome(step, outcome)
		}
	}()

	if s.opts.StartTimeout <= 0 {
		outcome = callStep(stepCtx, call)
		return stepEnd{err: outcome.err}
	}

	outcome, abandoned := s.awaitStep(stepCtx, running.cancel, step, call, stopWarning)

	return stepEnd{err: outcome.err, abandoned: abandoned}
}

// stepEnd is how the run's wait for a start step ended.
type stepEnd struct {
	err       error // the call's error, or the timeout error when abandoned
	abandoned bool  // the StartTimeout timer abandoned the call, which still runs
	// cancelled is the cause with which a shutdown of the step's scope, or
	// of an ancestor scope, cancelled its context, or nil
	cancelled error
}

// skipped reports whether a scope shutdown cancelled the step, which the
// run then skips instead of recording it or failing with it.
func (end stepEnd) skipped() bool {
	return end.cancelled != nil
}

// logCancelledOutcome logs the outcome of a call of step that a scope
// shutdown cancelled: an error at Debug level, a panic as a recovered
// panic of point "start", nothing for a nil return.
func logCancelledOutcome(step startStep, outcome stepOutcome) {
	scope := step.candidate.scope
	if outcome.panicked {
		attrs := slices.Insert(step.attrs(), 1, slog.String("point", "start"))
		scope.logPanicRecords(slices.Clip(attrs), outcome.panicType, outcome.value)
		return
	}
	if outcome.err != nil && scope.logEnabled(slog.LevelDebug) {
		scope.log(slog.LevelDebug, "cancelled start step error", append(step.attrs(), slog.String("error", outcome.err.Error()))...)
	}
}

// runningStep is the context of a start step that the pipeline runs or
// awaits now, registered with the step's scope.
type runningStep struct {
	scope  *Scope
	cancel context.CancelCauseFunc
	// cancelled is set, under lifecycleMu, to the cause with which a
	// shutdown of scope or of an ancestor scope cancelled the context
	cancelled error
}

// registerStep returns the context of a start step of scope, a child of
// the run's context ctx, registered with scope until unregisterStep. The
// context is not cancelled when the step returns: it keeps the run
// context's lifetime, which releases it when the run ends, unless a scope
// shutdown (cancelSubtreeSteps) or the StartTimeout timer cancels it first.
func (s *RootScope) registerStep(ctx context.Context, scope *Scope) (context.Context, *runningStep) {
	stepCtx, cancel := context.WithCancelCause(ctx)
	running := &runningStep{scope: scope, cancel: cancel}

	s.lifecycleMu.Lock()
	defer s.lifecycleMu.Unlock()

	if s.runSteps == nil {
		s.runSteps = map[*runningStep]struct{}{}
	}
	s.runSteps[running] = struct{}{}
	// a shutdown that began since the run's closing check, during the
	// step's before hooks, has already cancelled the registered steps
	if closing := closingScopeInLine(scope); closing != nil {
		running.cancelled = scopeShutdownCause(closing)
		cancel(running.cancelled)
	}

	return stepCtx, running
}

// unregisterStep ends the registration of running, and returns the cause
// with which a scope shutdown cancelled its context, or nil.
func (s *RootScope) unregisterStep(running *runningStep) (cancelled error) {
	s.lifecycleMu.Lock()
	defer s.lifecycleMu.Unlock()

	delete(s.runSteps, running)

	return running.cancelled
}

// cancelSubtreeSteps cancels the registered contexts of the start steps of
// scope and of its descendant scopes, with scopeShutdownCause(scope).
// lifecycleMu must be held. Cancelling under it runs no user code: the
// steps' contexts are the library's own, and their children only propagate
// the cancellation (a context.AfterFunc callback runs on its own
// goroutine).
func (s *RootScope) cancelSubtreeSteps(scope *Scope) {
	for running := range s.runSteps {
		if running.cancelled == nil && isScopeOrDescendant(running.scope, scope) {
			running.cancelled = scopeShutdownCause(scope)
			running.cancel(running.cancelled)
		}
	}
}

// scopeShutdownCause is the cause with which a shutdown of scope cancels
// the running start steps of its subtree. It matches ErrShutdown.
func scopeShutdownCause(scope *Scope) error {
	return libErrorf("%w: scope `%s` is shutting down", ErrShutdown, scope.name)
}

// stepOutcome is how a call of user start code ended.
type stepOutcome struct {
	err       error  // the returned error, or the recovered panic as an error
	panicked  bool   // the call panicked or ended its goroutine with runtime.Goexit
	value     any    // the panic value, when panicked
	panicType string // the type of value, or "runtime.Goexit"
}

// goexitOutcome is the outcome of a call that ended its goroutine with
// runtime.Goexit.
var goexitOutcome = stepOutcome{
	err:       recoverAsError("runtime.Goexit"),
	panicked:  true,
	value:     "runtime.Goexit",
	panicType: "runtime.Goexit",
}

// callStep calls call with ctx and recovers its panic.
func callStep(ctx context.Context, call func(context.Context) error) (outcome stepOutcome) {
	defer func() {
		if r := recover(); r != nil {
			outcome = stepOutcome{err: recoverAsError(r), panicked: true, value: r, panicType: fmt.Sprintf("%T", r)}
		}
	}()

	return stepOutcome{err: call(ctx)}
}

// logSafely runs log, a logging call made on a goroutine that no caller
// owns, and drops a panic of the Logger's handler: there is no caller to
// raise it to, and it must not crash the process.
func logSafely(log func()) {
	defer func() { _ = recover() }()

	log()
}

// armSlowStartWarning arms the slow start step warning of step on the
// container clock, and returns the function that disarms it. Nothing is
// armed when the warning is disabled, when its threshold is at or above a
// set StartTimeout, or when the Logger discards Warn records.
func (s *RootScope) armSlowStartWarning(step startStep) (stop func()) {
	threshold := s.opts.SlowStartThreshold
	if threshold == 0 {
		threshold = DefaultSlowStartThreshold
	}
	scope := step.candidate.scope
	// with a start timeout at or below the threshold, the abandonment
	// reports the step: no warning races it
	abandonedFirst := s.opts.StartTimeout > 0 && threshold >= s.opts.StartTimeout
	if threshold < 0 || abandonedFirst || !scope.logEnabled(slog.LevelWarn) {
		return func() {}
	}

	timer := scope.clock().AfterFunc(threshold, func() {
		logSafely(func() {
			scope.log(slog.LevelWarn, "start step is slow", append(step.attrs(), slog.Duration("threshold", threshold))...)
		})
	})

	return func() { timer.Stop() }
}

// awaitStep runs call in its own goroutine with the step's context stepCtx,
// a child of the run's context that cancel cancels, and waits until the
// call returns or a StartTimeout timer on the container clock times it out
// (see timeOut). An ended stepCtx does not end the wait. A timed-out call
// is abandoned: it keeps running, its step stays registered as abandoned
// until it ends, and abandoned is true with the timeout error as outcome.
// A call that returned in time keeps its context until the run ends, which
// reports no deadline, and its outcome is returned.
// stopWarning, the step's slow warning disarm, is called as soon as the
// wait ends, so no warning is logged once the step is abandoned.
func (s *RootScope) awaitStep(stepCtx context.Context, cancel context.CancelCauseFunc, step startStep, call func(context.Context) error, stopWarning func()) (outcome stepOutcome, abandoned bool) {
	clock := step.candidate.scope.clock()
	c := &stepCall{returned: make(chan struct{}), timedOut: make(chan struct{}), clock: clock, begin: clock.Now()}
	timer := clock.AfterFunc(s.opts.StartTimeout, func() { s.timeOut(step, c, cancel) })
	defer timer.Stop()

	go s.runStepCall(stepCtx, step, c, call)

	// only the timer abandons: an ended run context has cancelled stepCtx,
	// its child, and the run keeps waiting for a step that honours it
	select {
	case <-c.returned:
	case <-c.timedOut:
	}
	stopWarning()
	s.seamAfterStepDone(step)

	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.abandoned {
		return c.outcome, false
	}

	return stepOutcome{err: libErrorf("%w after %s: %w", ErrStartTimeout, s.opts.StartTimeout, context.DeadlineExceeded)}, true
}

// timeOut is the StartTimeout timer callback of call c of step. It and the
// end of the call are ordered by c's mutex, so the timeout is decided once:
// a call that ended first is not timed out and keeps its context; otherwise
// the step is registered as abandoned before its context is cancelled with
// cancel, and the call's end, even one that comes before the run notices,
// is its late outcome.
func (s *RootScope) timeOut(step startStep, c *stepCall, cancel context.CancelCauseFunc) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.done {
		return
	}
	c.abandoned = true
	s.markAbandoned(step)
	cancel(errStepTimedOut)
	close(c.timedOut)
}

// errStepTimedOut is the cancellation cause of the context of a step that
// outlived StartTimeout. It never reaches a caller: the timeout error is
// built from ErrStartTimeout.
var errStepTimedOut = errors.New("doger: start step timed out")

// stepCall is one call of user start code run on its own goroutine.
type stepCall struct {
	returned chan struct{} // closed once the call ended
	timedOut chan struct{} // closed once the timer abandoned the call
	clock    Clock
	begin    time.Time // when the call began, on clock

	mu        sync.Mutex
	done      bool        // the call ended
	outcome   stepOutcome // its outcome, once done
	abandoned bool        // the timer fired before it ended: the run stops waiting
}

// runStepCall makes the call of c with ctx and records its outcome. When
// the run abandoned it, the late outcome is handled here: recorded, then
// logged.
func (s *RootScope) runStepCall(ctx context.Context, step startStep, c *stepCall, call func(context.Context) error) {
	// a call that ends its goroutine with runtime.Goexit leaves this value
	// in place
	outcome := goexitOutcome
	defer func() {
		c.mu.Lock()
		c.done, c.outcome = true, outcome
		abandoned := c.abandoned
		c.mu.Unlock()
		close(c.returned)

		if abandoned {
			s.finishAbandoned(step, outcome.err)
			s.logLateOutcome(step, outcome, c.clock.Since(c.begin))
		}
	}()

	outcome = callStep(ctx, call)
}

// logLateOutcome logs how an abandoned call of step ended, elapsed after it
// began: a returned call at Warn level, with its error text only at Debug
// level, and a panic as a recovered panic of point "start". A panic of the
// Logger's handler is dropped.
func (s *RootScope) logLateOutcome(step startStep, outcome stepOutcome, elapsed time.Duration) {
	scope := step.candidate.scope
	logSafely(func() {
		if outcome.panicked {
			scope.logRecoveredPanicAs("start", step.candidate.name, outcome.panicType, outcome.value)
			return
		}

		attrs := append(step.attrs(), slog.Duration("elapsed", elapsed))
		scope.log(slog.LevelWarn, "abandoned start step returned", attrs...)
		if outcome.err != nil && scope.logEnabled(slog.LevelDebug) {
			scope.log(slog.LevelDebug, "abandoned start step error", append(slices.Clip(attrs), slog.String("error", outcome.err.Error()))...)
		}
	})
}

// attrs returns the log attributes naming step, after the scope one:
// service, step and, for a handler call, kind.
func (step startStep) attrs() []slog.Attr {
	attrs := []slog.Attr{slog.String("service", step.candidate.name), slog.String("step", string(step.op))}
	if step.kind != nil {
		attrs = append(attrs, slog.String("kind", step.kind.name))
	}

	return attrs
}

// stillRunningError is the error of a run that reached step while an
// abandoned call of it still runs.
func (step startStep) stillRunningError() *StartError {
	return step.startError(libErrorf("%w: a previous call is still running", ErrStartTimeout))
}

// startError returns the *StartError of step with cause.
func (step startStep) startError(cause error) *StartError {
	kind := ""
	if step.kind != nil {
		kind = step.kind.name
	}

	return newStartError(step.op, step.candidate.scope, step.candidate.name, kind, cause)
}
