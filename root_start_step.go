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

// runStartStep runs call, the user code of step, with the run's context
// ctx, recovering a panic as an error. Every Starter Start method and every
// kind handler call of the start pipeline goes through it.
func (s *RootScope) runStartStep(ctx context.Context, step startStep, call func(context.Context) error) error {
	stopWarning := s.armSlowStartWarning(step)
	defer stopWarning()

	if s.opts.StartTimeout <= 0 {
		return callStep(ctx, call).err
	}

	return s.awaitStep(ctx, step, call, stopWarning)
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

// awaitStep runs call in its own goroutine with a context bounded by
// StartTimeout on the container clock, and waits for it until that context
// is done. A call still running then is abandoned: it keeps running, its
// step is recorded as abandoned until it ends, and the timeout error is
// returned. A call that ended meanwhile is never abandoned. stopWarning,
// the step's slow warning disarm, is called as soon as the wait ends, so no
// warning is logged once the step is abandoned.
func (s *RootScope) awaitStep(ctx context.Context, step startStep, call func(context.Context) error, stopWarning func()) error {
	clock := step.candidate.scope.clock()
	stepCtx, timedOut, stopTimer, abandon := newStepContext(ctx, clock, s.opts.StartTimeout)
	defer stopTimer()

	c := &stepCall{returned: make(chan struct{}), clock: clock, begin: clock.Now()}
	go s.runStepCall(stepCtx, step, c, call)

	// only the timer abandons: an ended run context has cancelled stepCtx,
	// its child, and the run keeps waiting for a step that honours it
	select {
	case <-c.returned:
	case <-timedOut:
	}
	stopWarning()
	s.seamAfterStepDone(step)

	c.mu.Lock()
	defer c.mu.Unlock()
	if c.done {
		return c.outcome.err
	}
	c.abandoned = true
	abandon()
	s.markAbandoned(step)

	return libErrorf("%w after %s: %w", ErrStartTimeout, s.opts.StartTimeout, context.DeadlineExceeded)
}

// newStepContext returns the context of a step run under StartTimeout,
// derived from the run's context runCtx: a timer on clock cancels it with
// errStepTimedOut once timeout elapsed, and then closes timedOut. A step
// that returns in time keeps it as long as it would without a timeout:
// stopTimer only stops the timer, and the run's context releases it when
// the run ends. abandon cancels it. It reports no deadline, since a step
// that returns in time outlives one.
func newStepContext(runCtx context.Context, clock Clock, timeout time.Duration) (ctx context.Context, timedOut <-chan struct{}, stopTimer, abandon func()) {
	ctx, cancel := context.WithCancelCause(runCtx)
	fired := make(chan struct{})
	timer := clock.AfterFunc(timeout, func() {
		cancel(errStepTimedOut)
		close(fired)
	})

	return ctx, fired, func() { timer.Stop() }, func() { cancel(errStepTimedOut) }
}

// errStepTimedOut is the cancellation cause of the context of a step that
// outlived StartTimeout. It never reaches a caller: the timeout error is
// built from ErrStartTimeout.
var errStepTimedOut = errors.New("doger: start step timed out")

// stepCall is one call of user start code run on its own goroutine.
type stepCall struct {
	returned chan struct{} // closed once the call ended
	clock    Clock
	begin    time.Time // when the call began, on clock

	mu        sync.Mutex
	done      bool        // the call ended
	outcome   stepOutcome // its outcome, once done
	abandoned bool        // the run stopped waiting before it ended
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
	kind := ""
	if step.kind != nil {
		kind = step.kind.name
	}

	return newStartError(step.op, step.candidate.scope, step.candidate.name, kind,
		libErrorf("%w: a previous call is still running", ErrStartTimeout))
}
