package tests

import (
	"sync"
	"testing"
	"time"

	"go.uber.org/goleak"
)

func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}

// minTestTimeout is the least time testWithTimeout waits before it reports
// a test as hung.
const minTestTimeout = 5 * time.Second

// testWithTimeout fails t once it has run for timeout without finishing. It
// guards against hangs, not timing: a timeout below minTestTimeout is raised
// to it, so a test starved of CPU under -race and parallel load is not
// reported as hung. The failure is reported through t, so it names the test;
// a test that never returns is then ended by go test's -timeout.
//
// https://github.com/stretchr/testify/issues/1101
func testWithTimeout(t *testing.T, timeout time.Duration) {
	t.Helper()
	timeout = max(timeout, minTestTimeout)

	var mu sync.Mutex
	finished := false
	done := make(chan struct{})
	t.Cleanup(func() {
		mu.Lock()
		finished = true
		mu.Unlock()
		close(done)
	})

	go func() {
		timer := time.NewTimer(timeout)
		defer timer.Stop()

		select {
		case <-done:
		case <-timer.C:
			// t must not be used once the test finished: the lock orders
			// this report before the cleanup that marks it finished.
			mu.Lock()
			defer mu.Unlock()
			if !finished {
				t.Errorf("test still running after %s: it may be hung", timeout)
			}
		}
	}()
}
