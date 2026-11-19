package doger

import (
	"bytes"
	"runtime"
	"slices"
	"strconv"
	"sync"
)

// buildRun is a build of a service in flight (D1).
type buildRun struct {
	name  string        // the service being built
	root  *RootScope    // the container building it, nil when unknown
	owner uint64        // id of the goroutine running the build; 0 when unknown
	done  chan struct{} // closed once the build's outcome is recorded
	err   error         // the failure the waiters share; set before done is closed
}

// ended reports whether the outcome of r is recorded.
func (r *buildRun) ended() bool {
	select {
	case <-r.done:
		return true
	default:
		return false
	}
}

// buildState is the build state of a lazy service, or of the declared
// dependencies of an eager value (D1): idle, or building with a run others
// wait for. Built and Terminal are the owner's own flags. The owner's mutex
// guards it, and is never held while a build runs.
type buildState struct {
	run *buildRun // the build in flight, nil when idle
}

// claimLocked returns the build in flight and true, or starts a build of
// the service name of root owned by the calling goroutine and returns it
// and false.
func (b *buildState) claimLocked(name string, root *RootScope) (run *buildRun, inFlight bool) {
	if b.run != nil {
		return b.run, true
	}
	b.run = &buildRun{name: name, root: root, owner: goroutineID(), done: make(chan struct{})}
	waits.own(b.run)

	return b.run, false
}

// endLocked records err as the outcome of run, which ends; the caller then
// releases run's waiters with run.release once the owner's mutex is free.
func (b *buildState) endLocked(run *buildRun, err error) {
	b.run = nil
	run.err = err
	waits.disown(run)
}

// inFlight returns the build in flight, or nil; the owner's mutex is held.
func (b *buildState) inFlight() *buildRun {
	return b.run
}

// release lets the waiters of run read its outcome.
func (r *buildRun) release() {
	close(r.done)
}

// buildWaits is the wait-for registry of the builds in flight (D1), one for
// the process: goroutine ids are process-wide, and a provider may capture
// the injector of another container. It records which build each goroutine
// waits for, one at a time, and the builds each goroutine owns, innermost
// last.
type buildWaits struct {
	mu    sync.Mutex
	edges map[uint64]*buildRun   // waiter -> the build it waits for
	owned map[uint64][]*buildRun // owner -> its builds in flight
}

// waits is the process's wait-for registry.
var waits buildWaits

// own records that run's owner builds it.
func (w *buildWaits) own(run *buildRun) {
	if run.owner == 0 {
		return
	}

	w.mu.Lock()
	defer w.mu.Unlock()

	if w.owned == nil {
		w.owned = map[uint64][]*buildRun{}
	}
	w.owned[run.owner] = append(w.owned[run.owner], run)
}

// disown forgets run among its owner's builds.
func (w *buildWaits) disown(run *buildRun) {
	if run.owner == 0 {
		return
	}

	w.mu.Lock()
	defer w.mu.Unlock()

	runs := w.owned[run.owner]
	for n, owned := range slices.Backward(runs) {
		if owned == run {
			runs = slices.Delete(runs, n, n+1)
			break
		}
	}
	if len(runs) == 0 {
		delete(w.owned, run.owner)
	} else {
		w.owned[run.owner] = runs
	}
}

// chainFrom returns the names of the builds owner runs, from the build of
// name to the innermost, followed by name: the cycle a re-entry closes.
func (w *buildWaits) chainFrom(owner uint64, name string) []string {
	w.mu.Lock()
	defer w.mu.Unlock()

	runs := w.owned[owner]
	start := 0
	for n, run := range runs {
		if run.name == name {
			start = n
			break
		}
	}
	chain := make([]string, 0, len(runs)-start+1)
	for _, run := range runs[start:] {
		chain = append(chain, run.name)
	}

	return append(chain, name)
}

// add records that waiter waits for run, unless run's owner, directly or
// through the builds it waits for, waits for waiter: then it records
// nothing and reports false. A wait for a build that already ended is no
// edge: its waiter has not woken yet to remove it.
func (w *buildWaits) add(waiter uint64, run *buildRun) bool {
	w.mu.Lock()
	defer w.mu.Unlock()

	for r := run; ; {
		if r.owner == waiter {
			return false
		}
		next, ok := w.edges[r.owner]
		if !ok || next.ended() {
			break
		}
		r = next
	}
	if w.edges == nil {
		w.edges = map[uint64]*buildRun{}
	}
	w.edges[waiter] = run

	return true
}

// remove forgets the wait of waiter.
func (w *buildWaits) remove(waiter uint64) {
	w.mu.Lock()
	defer w.mu.Unlock()

	delete(w.edges, waiter)
}

// buildPanicError is the error the waiters of a build share when a panic
// outside the provider, such as in an invocation hook, ended the build: it
// matches ErrProviderPanic, and an error value too, without blaming the
// provider.
func buildPanicError(value any) error {
	cause, _ := value.(error)
	return &buildPanic{message: libErrorf("panic during the build, outside the provider: %v", value).Error(), cause: cause}
}

// buildPanic is the error of buildPanicError.
type buildPanic struct {
	message string
	cause   error // the panic value when it is an error
}

func (e *buildPanic) Error() string { return e.message }

// Is matches ErrProviderPanic.
func (e *buildPanic) Is(target error) bool { return target == ErrProviderPanic }

// Unwrap returns the panic value when it is an error.
func (e *buildPanic) Unwrap() error { return e.cause }

// rootOf returns the container of i, nil for a nil i.
func rootOf(i Injector) *RootScope {
	if i == nil {
		return nil
	}

	return i.RootScope()
}

// waitForBuild waits for run, a build of the service name in flight, and
// returns the failure it shares, nil when it succeeded. A build owned by
// the calling goroutine, or a wait that would close a cycle of goroutines
// waiting for each other's builds, returns an error matching
// ErrCircularDependency instead of blocking. When the goroutine id is
// unknown, it only waits: the invocation chain still detects a cycle
// through the injectors providers receive.
func waitForBuild(name string, run *buildRun) error {
	self := goroutineID()
	if self == 0 {
		<-run.done
		return run.err
	}
	if run.owner == self {
		return libErrorMatching(ErrCircularDependency, "circular dependency detected: %s", humanReadableInvokerChain(waits.chainFrom(self, name)))
	}

	if !waits.add(self, run) {
		return libErrorMatching(ErrCircularDependency, "service `%s` is being built by a goroutine that waits for this one", name)
	}
	defer waits.remove(self)

	<-run.done

	return run.err
}

// goroutineID returns the id of the calling goroutine, read from the first
// line of its stack ("goroutine 123 [running]:"). It is read only when a
// build starts or a wait is needed, never to return a built service.
func goroutineID() uint64 {
	return goroutineIDFunc()
}

// goroutineIDFunc reads the goroutine id; tests replace it to simulate an
// unknown id.
var goroutineIDFunc = func() uint64 {
	var buf [64]byte
	return parseGoroutineID(buf[:runtime.Stack(buf[:], false)])
}

// parseGoroutineID returns the id in the first line of a goroutine's stack,
// such as "goroutine 123 [running]:", and 0 when there is none.
func parseGoroutineID(stack []byte) uint64 {
	line, ok := bytes.CutPrefix(stack, []byte("goroutine "))
	if !ok {
		return 0
	}
	if end := bytes.IndexByte(line, ' '); end >= 0 {
		line = line[:end]
	}
	id, err := strconv.ParseUint(string(line), 10, 64)
	if err != nil {
		return 0
	}

	return id
}
