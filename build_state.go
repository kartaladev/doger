package doger

import (
	"bytes"
	"runtime"
	"strconv"
	"sync"
)

// buildRun is a build of a service in flight (D1).
type buildRun struct {
	owner uint64        // id of the goroutine running the build
	done  chan struct{} // closed once the build's outcome is recorded
	err   error         // the failure the waiters share; set before done is closed
}

// buildState is the build state of a lazy service, or of the declared
// dependencies of an eager value (D1): idle, or building with a run others
// wait for. Built and Terminal are the owner's own flags. The owner's mutex
// guards it, and is never held while a build runs.
type buildState struct {
	run *buildRun // the build in flight, nil when idle
}

// claimLocked returns the build in flight and true, or starts a build owned
// by the calling goroutine and returns it and false.
func (b *buildState) claimLocked() (run *buildRun, inFlight bool) {
	if b.run != nil {
		return b.run, true
	}
	b.run = &buildRun{owner: goroutineID(), done: make(chan struct{})}

	return b.run, false
}

// endLocked records err as the outcome of run, which ends; the caller then
// releases run's waiters with run.release once the owner's mutex is free.
func (b *buildState) endLocked(run *buildRun, err error) {
	b.run = nil
	run.err = err
}

// inFlight returns the build in flight, or nil; the owner's mutex is held.
func (b *buildState) inFlight() *buildRun {
	return b.run
}

// release lets the waiters of run read its outcome.
func (r *buildRun) release() {
	close(r.done)
}

// buildWaits is a wait-for registry: which goroutine waits for a build
// owned by which goroutine. A goroutine waits for one build at a time.
type buildWaits struct {
	mu    sync.Mutex
	edges map[uint64]uint64 // waiter -> owner
}

// add records that waiter waits for a build owned by owner, unless the
// owner, directly or through the goroutines it waits for, waits for waiter:
// then it records nothing and reports false.
func (w *buildWaits) add(waiter, owner uint64) bool {
	w.mu.Lock()
	defer w.mu.Unlock()

	for x := owner; ; {
		if x == waiter {
			return false
		}
		next, ok := w.edges[x]
		if !ok {
			break
		}
		x = next
	}
	if w.edges == nil {
		w.edges = map[uint64]uint64{}
	}
	w.edges[waiter] = owner

	return true
}

// remove forgets the wait of waiter.
func (w *buildWaits) remove(waiter uint64) {
	w.mu.Lock()
	defer w.mu.Unlock()

	delete(w.edges, waiter)
}

// detachedWaits is the registry of builds reached without a container,
// such as a wrapper invoked directly.
var detachedWaits buildWaits

// waitsOf returns the wait-for registry of the container of i.
func waitsOf(i Injector) *buildWaits {
	if i == nil {
		return &detachedWaits
	}

	return i.RootScope().buildWaits()
}

// waitForBuild waits for run, a build of the service name in flight, and
// returns the failure it shares, nil when it succeeded. A build owned by
// the calling goroutine, or a wait that would close a cycle of goroutines
// waiting for each other's builds, returns an error matching
// ErrCircularDependency instead of blocking.
func waitForBuild(i Injector, name string, run *buildRun) error {
	self := goroutineID()
	if run.owner == self {
		return libErrorMatching(ErrCircularDependency, "service `%s` is already being built by this goroutine", name)
	}

	waits := waitsOf(i)
	if !waits.add(self, run.owner) {
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
