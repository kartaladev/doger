//go:build unix

package doger

import (
	"os"
	"os/signal"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestShutdownOnSignals_Default replaces the default signal set with SIGUSR1
// and sends it to the test process, so it does not run in parallel and no
// other test uses SIGUSR1 meanwhile.
func TestShutdownOnSignals_Default(t *testing.T) { //nolint:paralleltest // swaps the default signal set and signals the process
	testWithTimeout(t, 10*time.Second)
	is := assert.New(t)

	// keeps SIGUSR1 handled, so a signal sent before the helper registers
	// cannot kill the process
	guard := make(chan os.Signal, 1)
	signal.Notify(guard, syscall.SIGUSR1)
	defer signal.Stop(guard)

	previous := defaultShutdownSignals
	defaultShutdownSignals = []os.Signal{syscall.SIGUSR1}
	t.Cleanup(func() { defaultShutdownSignals = previous })

	root := New()
	probe := &ctxShutdownProbe{}
	ProvideNamed(root, "s", func(Injector) (*ctxShutdownProbe, error) { return probe, nil })
	MustInvokeNamed[*ctxShutdownProbe](root, "s")

	type result struct {
		sig    os.Signal
		report *ShutdownReport
	}
	done := make(chan result, 1)
	go func() {
		sig, report := root.ShutdownOnSignals() // no signal given: the default set
		done <- result{sig: sig, report: report}
	}()

	var got result
wait:
	for {
		require.NoError(t, syscall.Kill(os.Getpid(), syscall.SIGUSR1))
		select {
		case got = <-done:
			break wait
		case <-time.After(time.Millisecond):
		}
	}

	is.Equal(syscall.SIGUSR1, got.sig)
	is.True(got.report.Succeed, got.report.Error())
	probe.mu.Lock()
	is.Len(probe.ctxs, 1, "the service was shut down")
	probe.mu.Unlock()
}
