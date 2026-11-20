//go:build unix

package doger

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"
)

// exampleSignalSelf sends sig to the process every millisecond until the
// returned stop is called: the example has no operator to press Ctrl-C. sig
// stays handled meanwhile, so a send before ShutdownOnSignals listens, or
// after it returned, cannot end the process.
func exampleSignalSelf(sig syscall.Signal) (stop func()) {
	guard := make(chan os.Signal, 1)
	signal.Notify(guard, sig)
	done, exited := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(exited)
		for {
			_ = syscall.Kill(os.Getpid(), sig)
			select {
			case <-done:
				return
			case <-time.After(time.Millisecond):
			}
		}
	}()

	return func() {
		close(done)
		<-exited
		signal.Stop(guard)
	}
}

func ExampleRootScope_ShutdownOnSignals() {
	injector := New()
	ProvideNamed(injector, "db", func(Injector) (*exampleSignalDB, error) { return &exampleSignalDB{}, nil })
	MustInvokeNamed[*exampleSignalDB](injector, "db")

	// Blocks until one of the signals arrives, then shuts the container
	// down; with no signal given it waits for SIGTERM or SIGINT. Here the
	// process sends itself SIGUSR1.
	stop := exampleSignalSelf(syscall.SIGUSR1)
	defer stop()

	sig, report := injector.ShutdownOnSignals(syscall.SIGUSR1)
	fmt.Println("signal:", sig)
	fmt.Println("succeeded:", report.Succeed)
	// Output:
	// db shut down, ctx.Err(): <nil>
	// signal: user defined signal 1
	// succeeded: true
}
