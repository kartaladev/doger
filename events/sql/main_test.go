package eventsql_test

import (
	"fmt"
	"os"
	"runtime"
	"runtime/pprof"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"go.uber.org/goleak"
)

func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}

// hangBackstop is how long an integration test may run in total. Its
// waits fail on a lack of progress (waitTimeout), never on their total
// time; the backstop only ends a test hung where no wait watches it.
const hangBackstop = 15 * time.Minute

// testWithBackstop panics, naming the test and dumping every goroutine,
// once it has run for hangBackstop without finishing. Call it in the test
// (or the parallel subtest) that does the work.
func testWithBackstop(t *testing.T) {
	t.Helper()
	finished := make(chan struct{})
	t.Cleanup(func() { close(finished) })

	where := caller(t)
	go func() {
		select {
		case <-finished:
		case <-time.After(hangBackstop):
			dumpGoroutines()
			panic(fmt.Sprintf("%s: test still running after %v", where, hangBackstop))
		}
	}()
}

// watchProgress returns the function a test calls on each step of
// progress, and panics, naming the test and dumping every goroutine, once
// waitTimeout passed without one: a long test fails on a stall, however
// long it runs while it progresses.
func watchProgress(t *testing.T) (progress func()) {
	t.Helper()
	var last atomic.Int64
	progress = func() { last.Store(time.Now().UnixNano()) }
	progress()
	finished := make(chan struct{})
	t.Cleanup(func() { close(finished) })

	where := caller(t)
	go func() {
		ticker := time.NewTicker(waitTimeout / 20)
		defer ticker.Stop()
		for {
			select {
			case <-finished:
				return
			case <-ticker.C:
			}
			if idle := time.Since(time.Unix(0, last.Load())); idle >= waitTimeout {
				dumpGoroutines()
				panic(fmt.Sprintf("%s: no progress for %v", where, idle.Round(time.Second)))
			}
		}
	}()

	return progress
}

// caller returns the name of t and the place of the call of its caller.
func caller(t *testing.T) string {
	where := t.Name()
	if _, file, line, ok := runtime.Caller(2); ok {
		where += " (" + file + ":" + strconv.Itoa(line) + ")"
	}

	return where
}

// dumpGoroutines writes the stack of every goroutine to stderr.
func dumpGoroutines() {
	_ = pprof.Lookup("goroutine").WriteTo(os.Stderr, 2)
}
