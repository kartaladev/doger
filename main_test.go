package doger

import (
	"fmt"
	"reflect"
	"runtime"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"go.uber.org/goleak"
)

type ctxTestkey string

const (
	ctxTestKey ctxTestkey = "test-key"
)

func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}

// minTestTimeout is the least time testWithTimeout waits before it reports
// a test as hung.
const minTestTimeout = 5 * time.Second

// testWithTimeout panics, naming the test, once it has run for timeout
// without finishing. It guards against hangs, not timing: a timeout below
// minTestTimeout is raised to it, so a test starved of CPU under -race and
// parallel load is not reported as hung. Tests assert timing themselves, on
// a fake clock or in a synctest bubble.
//
// Call it in the test that does the work. A parent whose subtests call
// t.Parallel() waits for them, and they first wait for a free slot (-p,
// GOMAXPROCS), so a guard started in the parent also counts that queueing,
// which alone can exceed the timeout on a loaded runner: start the guard
// in each parallel subtest instead, after its t.Parallel().
//
// https://github.com/stretchr/testify/issues/1101
func testWithTimeout(t *testing.T, timeout time.Duration) {
	t.Helper()
	timeout = max(timeout, minTestTimeout)

	testFinished := make(chan struct{})

	t.Cleanup(func() {
		close(testFinished)
	})

	line := ""
	funcName := ""

	var pc [1]uintptr
	n := runtime.Callers(2, pc[:])
	if n > 0 {
		frames := runtime.CallersFrames(pc[:])
		frame, _ := frames.Next()
		line = frame.File + ":" + strconv.Itoa(frame.Line)
		funcName = frame.Function
	}

	go func() {
		select {
		case <-testFinished:
		case <-time.After(timeout):
			if line == "" || funcName == "" {
				panic(fmt.Sprintf("Test timed out after: %v", timeout))
			}
			panic(fmt.Sprintf("%s: Test timed out after: %v\n%s", funcName, timeout, line))

			// t.Errorf("Test timed out after: %v", timeout)
			// os.Exit(1)
		}
	}()
}

const (
	// progressStall is how long testWithProgress waits for the next round
	// before it reports the test as hung: thousands of times a round of the
	// tests using it, even under -race, -cpu=1 and a load average of 275.
	progressStall = 30 * time.Second
	// progressCeiling bounds a test guarded by testWithProgress however
	// steadily it progresses.
	progressCeiling = 10 * time.Minute
)

// testWithProgress guards a long loop against hangs by its progress, not by
// its total time: a fixed budget for thousands of rounds fails a starved
// test that is not hung, and proves nothing about hangs. It returns
// progress, which the test calls once per round. When no round ends for
// progressStall, or the test runs for progressCeiling, the watchdog panics,
// naming the test, with the stacks of all goroutines.
//
// Call it outside any synctest bubble, so the watchdog runs on real time;
// progress itself may be called from inside a bubble.
func testWithProgress(t *testing.T) (progress func()) {
	t.Helper()
	funcName, line := testCaller()

	var rounds atomic.Int64
	stop := make(chan struct{})
	var wg sync.WaitGroup
	t.Cleanup(func() {
		close(stop)
		wg.Wait()
	})

	wg.Go(func() {
		ticker := time.NewTicker(progressStall / 10)
		defer ticker.Stop()

		begin := time.Now()
		last, lastAt, prevTick := rounds.Load(), begin, begin
		for {
			select {
			case <-stop:
				return
			case now := <-ticker.C:
				n := rounds.Load()
				// a watchdog starved itself saw the whole process starved,
				// the test with it: that is no evidence of a hang
				if n != last || now.Sub(prevTick) > progressStall/2 {
					last, lastAt = n, now
				}
				prevTick = now

				switch {
				case now.Sub(lastAt) >= progressStall:
					panicWithAllGoroutines(fmt.Sprintf("%s: no progress for %v after %d rounds: test hung\n%s", funcName, progressStall, n, line))
				case now.Sub(begin) >= progressCeiling:
					panicWithAllGoroutines(fmt.Sprintf("%s: still running after %v (%d rounds)\n%s", funcName, progressCeiling, n, line))
				}
			}
		}
	})

	return func() { rounds.Add(1) }
}

// testCaller returns the function and file:line of the caller of the
// function calling testCaller.
func testCaller() (funcName, line string) {
	var pc [1]uintptr
	if runtime.Callers(3, pc[:]) == 0 {
		return "", ""
	}
	frame, _ := runtime.CallersFrames(pc[:]).Next()

	return frame.Function, frame.File + ":" + strconv.Itoa(frame.Line)
}

// panicWithAllGoroutines panics with msg, printing the stack of every
// goroutine, not only the panicking one: a hang shows in the others.
func panicWithAllGoroutines(msg string) {
	debug.SetTraceback("all")
	panic(msg)
}

// pkgName holds the current package name (eg. "do"). It won't break
// if the package name is changed, in case of a fork or anything.
var pkgName = func() string {
	t := reflect.TypeFor[__pkg_probe_type]()
	s := t.String() // eg. "do.__pkg_probe_type"
	if i := strings.IndexByte(s, '.'); i > 0 {
		return s[:i]
	}
	return "do"
}()

type __pkg_probe_type struct{}
