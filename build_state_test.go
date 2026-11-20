package doger

import (
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestParseGoroutineID(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		stack string
		want  uint64
	}{
		{name: "running", stack: "goroutine 123 [running]:\nmain.main()", want: 123},
		{name: "one", stack: "goroutine 1 [running]:", want: 1},
		{name: "large", stack: "goroutine 18446744073709551615 [running]:", want: 18446744073709551615},
		{name: "truncated_after_id", stack: "goroutine 42 ", want: 42},
		{name: "no_prefix", stack: "thread 5 [running]:", want: 0},
		{name: "not_a_number", stack: "goroutine abc [running]:", want: 0},
		{name: "empty", stack: "", want: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, parseGoroutineID([]byte(tt.stack)))
		})
	}
}

func TestGoroutineID_DistinctPerGoroutine(t *testing.T) {
	t.Parallel()
	is := assert.New(t)

	own := goroutineID()
	other := make(chan uint64)
	go func() { other <- goroutineID() }()

	is.NotZero(own)
	is.Equal(own, goroutineID(), "stable on one goroutine")
	is.NotEqual(own, <-other)
}

func TestBuildWaits_StaleEdgeIsNoCycle(t *testing.T) {
	t.Parallel()
	is := assert.New(t)

	// G2 waits for "d", owned by G1; "d" completes, but G2 has not woken to
	// remove its edge yet. G1 now waits for "c", owned by G2: no cycle.
	var waits buildWaits
	d := &buildRun{owner: 1, done: make(chan struct{})}
	c := &buildRun{owner: 2, done: make(chan struct{})}
	is.True(waits.add(2, d))
	close(d.done)

	is.True(waits.add(1, c), "a wait on a completed build is no edge")
}

func TestBuildWaits_LiveCycle(t *testing.T) {
	t.Parallel()
	is := assert.New(t)

	var waits buildWaits
	a := &buildRun{owner: 1, done: make(chan struct{})}
	b := &buildRun{owner: 2, done: make(chan struct{})}
	is.True(waits.add(2, a))
	is.False(waits.add(1, b), "1 waits for 2, which waits for 1")
}

// TestGoroutineID_Unknown replaces goroutineIDFunc, a package-level reader
// every build uses. It is not parallel: Go runs a top-level test without
// t.Parallel before the parallel ones resume, so no other test builds while
// the reader is replaced, and t.Cleanup restores it before they do.
func TestGoroutineID_Unknown(t *testing.T) { //nolint:paralleltest // replaces the package-level goroutineIDFunc
	previous := goroutineIDFunc
	goroutineIDFunc = func() uint64 { return 0 }
	t.Cleanup(func() { goroutineIDFunc = previous })
	testWithTimeout(t, 5*time.Second)
	is := assert.New(t)

	root := New()
	var calls atomic.Int32
	release := make(chan struct{})
	ProvideNamed(root, "db", func(Injector) (*resolutionDB, error) {
		calls.Add(1)
		<-release
		return &resolutionDB{}, nil
	})
	var invoked atomic.Int32
	root.AddBeforeInvocationHook(func(*Scope, string) { invoked.Add(1) })

	const callers = 8
	errs := make([]error, callers)
	var wg sync.WaitGroup
	for n := range callers {
		wg.Go(func() { _, errs[n] = InvokeNamed[*resolutionDB](root, "db") })
	}
	for invoked.Load() < callers {
		runtime.Gosched()
	}
	time.Sleep(20 * time.Millisecond) // the callers reach the build or its wait
	close(release)
	wg.Wait()

	for _, err := range errs {
		is.NoError(err)
	}
	is.Equal(int32(1), calls.Load(), "provider calls")
}
