package doger

import (
	"errors"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// resolutionBarrier releases its waiters once n of them arrived.
type resolutionBarrier struct {
	mu      sync.Mutex
	n       int
	arrived int
	all     chan struct{}
}

func newResolutionBarrier(n int) *resolutionBarrier {
	return &resolutionBarrier{n: n, all: make(chan struct{})}
}

// wait records an arrival and blocks until n arrived.
func (b *resolutionBarrier) wait() {
	b.mu.Lock()
	b.arrived++
	if b.arrived == b.n {
		close(b.all)
	}
	b.mu.Unlock()
	<-b.all
}

// provideCycleMember registers lazy name, whose provider counts its calls in
// calls, waits on barrier (when not nil) and invokes next through the
// injector it receives.
func provideCycleMember(root *RootScope, name, next string, barrier *resolutionBarrier, calls *atomic.Int32) {
	var once sync.Once
	ProvideNamed(root, name, func(i Injector) (int, error) {
		calls.Add(1)
		if barrier != nil {
			once.Do(barrier.wait)
		}
		value, err := InvokeNamed[int](i, next)
		return value + 1, err
	})
}

// invokeConcurrently invokes each of names on its own goroutine and returns
// the errors, in the order of names.
func invokeConcurrently(root *RootScope, names []string) []error {
	errs := make([]error, len(names))
	var wg sync.WaitGroup
	for n, name := range names {
		wg.Go(func() {
			_, errs[n] = InvokeNamed[int](root, name)
		})
	}
	wg.Wait()
	return errs
}

func TestConcurrentResolution_CycleReturnsError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		cycle   []string // each invokes the next, the last the first
		invoked []string // one goroutine each
	}{
		{name: "two_services_two_goroutines", cycle: []string{"a", "b"}, invoked: []string{"a", "b"}},
		{name: "three_services_three_goroutines", cycle: []string{"a", "b", "c"}, invoked: []string{"a", "b", "c"}},
		{name: "three_services_two_goroutines", cycle: []string{"a", "b", "c"}, invoked: []string{"a", "b"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			testWithTimeout(t, 2*time.Second)

			root := New()
			barrier := newResolutionBarrier(len(tt.invoked))
			var calls atomic.Int32
			for n, name := range tt.cycle {
				var b *resolutionBarrier
				for _, invoked := range tt.invoked {
					if invoked == name {
						b = barrier
					}
				}
				provideCycleMember(root, name, tt.cycle[(n+1)%len(tt.cycle)], b, &calls)
			}

			for n, err := range invokeConcurrently(root, tt.invoked) {
				assert.ErrorIs(t, err, ErrCircularDependency, "invoke of %q", tt.invoked[n])
			}
		})
	}
}

func TestConcurrentResolution_DeclaredDependencyCycle(t *testing.T) {
	t.Parallel()
	testWithTimeout(t, 2*time.Second)

	root := New()
	// both goroutines are inside their builds before either resolves the
	// other: G1 builds "a" and is about to resolve "b" (its declared
	// dependency), G2 is in the provider of "b"
	barrier := newResolutionBarrier(2)
	var onceB sync.Once
	ProvideNamed(root, "a", func(Injector) (int, error) { return 1, nil }, DependsOnNamed("b"))
	ProvideNamed(root, "b", func(i Injector) (int, error) {
		onceB.Do(barrier.wait)
		return InvokeNamed[int](i, "a")
	})
	var g1 atomic.Value
	var onceG1 sync.Once
	root.AddBeforeInvocationHook(func(_ *Scope, name string) {
		switch {
		case name == "a":
			onceG1.Do(func() { g1.Store(goroutineTestKey()) })
		case name == "b" && g1.Load() == goroutineTestKey():
			barrier.wait()
		}
	})

	for n, err := range invokeConcurrently(root, []string{"a", "b"}) {
		assert.ErrorIs(t, err, ErrCircularDependency, "call %d", n)
	}
}

// goroutineTestKey identifies the calling goroutine, for tests only.
func goroutineTestKey() string {
	var buf [64]byte
	n := runtime.Stack(buf[:], false)
	line := string(buf[:n])
	for i := len("goroutine "); i < len(line); i++ {
		if line[i] == ' ' {
			return line[:i]
		}
	}
	return line
}

func TestConcurrentResolution_CycleRetried(t *testing.T) {
	t.Parallel()
	testWithTimeout(t, 2*time.Second)
	is := assert.New(t)

	root := New()
	barrier := newResolutionBarrier(2)
	var callsA, callsB atomic.Int32
	provideCycleMember(root, "a", "b", barrier, &callsA)
	provideCycleMember(root, "b", "a", barrier, &callsB)
	for _, err := range invokeConcurrently(root, []string{"a", "b"}) {
		require.ErrorIs(t, err, ErrCircularDependency)
	}
	beforeA, beforeB := callsA.Load(), callsB.Load()

	_, err := InvokeNamed[int](root, "a")

	is.ErrorIs(err, ErrCircularDependency)
	is.Greater(callsA.Load(), beforeA, "provider of a called again")
	is.Greater(callsB.Load(), beforeB, "provider of b called again")
}

// stringerService is the only fmt.Stringer of its test.
type stringerService struct{}

func (stringerService) String() string { return "stringer" }

func TestConcurrentResolution_ReentrantCapturedInjector(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		setup func(root *RootScope) Injector // returns where "a" is invoked from
	}{
		{name: "self", setup: func(root *RootScope) Injector {
			ProvideNamed(root, "a", func(Injector) (int, error) { return InvokeNamed[int](root, "a") })
			return root
		}},
		{name: "through_b_with_its_injector", setup: func(root *RootScope) Injector {
			ProvideNamed(root, "a", func(Injector) (int, error) { return InvokeNamed[int](root, "b") })
			ProvideNamed(root, "b", func(i Injector) (int, error) { return InvokeNamed[int](i, "a") })
			return root
		}},
		{name: "through_b_captured", setup: func(root *RootScope) Injector {
			ProvideNamed(root, "a", func(Injector) (int, error) { return InvokeNamed[int](root, "b") })
			ProvideNamed(root, "b", func(Injector) (int, error) { return InvokeNamed[int](root, "a") })
			return root
		}},
		{name: "child_scope", setup: func(root *RootScope) Injector {
			c := root.Scope("c")
			ProvideNamed(c, "a", func(Injector) (int, error) { return InvokeNamed[int](c, "a") })
			return c
		}},
		{name: "eager_depends_on", setup: func(root *RootScope) Injector {
			// D1: an eager value with declared dependencies resolves them
			// with the same build state as a lazy service
			ProvideNamedValue(root, "a", 1, DependsOnNamed("b"))
			ProvideNamed(root, "b", func(Injector) (int, error) { return InvokeNamed[int](root, "a") })
			return root
		}},
		{name: "invoke_as", setup: func(root *RootScope) Injector {
			ProvideNamed(root, "a", func(Injector) (stringerService, error) {
				_, err := InvokeAs[fmt.Stringer](root)
				return stringerService{}, err
			})
			return root
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			testWithTimeout(t, 2*time.Second)

			root := New()
			from := tt.setup(root)

			_, err := invokeAnyByName(from, "a")

			assert.ErrorIs(t, err, ErrCircularDependency)
		})
	}
}

func TestConcurrentResolution_CapturedInjectorNoCycle(t *testing.T) {
	t.Parallel()
	testWithTimeout(t, 2*time.Second)

	root := New()
	var callsA, callsB atomic.Int32
	ProvideNamed(root, "b", func(Injector) (int, error) { callsB.Add(1); return 2, nil })
	ProvideNamed(root, "a", func(Injector) (int, error) {
		callsA.Add(1)
		b, err := InvokeNamed[int](root, "b")
		return b + 1, err
	})

	value, err := InvokeNamed[int](root, "a")

	require.NoError(t, err)
	assert.Equal(t, 3, value)
	assert.Equal(t, int32(1), callsA.Load(), "provider calls of a")
	assert.Equal(t, int32(1), callsB.Load(), "provider calls of b")
}

// waitingForBuilds returns how many goroutines wait for a build in flight
// in root.
func waitingForBuilds(root *RootScope) int {
	waits.mu.Lock()
	defer waits.mu.Unlock()

	n := 0
	for _, run := range waits.edges {
		if run.root == root && !run.ended() {
			n++
		}
	}
	return n
}

// resolutionDB is the instance of "db" in the single-build tests.
type resolutionDB struct{}

func TestConcurrentResolution_SingleBuild(t *testing.T) {
	t.Parallel()

	for _, callers := range []int{1, 2, 64} {
		t.Run(fmt.Sprintf("callers_%d", callers), func(t *testing.T) {
			t.Parallel()
			testWithTimeout(t, 5*time.Second)
			is := assert.New(t)

			root := New()
			var calls atomic.Int32
			ProvideNamed(root, "db", func(Injector) (*resolutionDB, error) {
				calls.Add(1)
				// returns once every other caller waits for this build
				for waitingForBuilds(root) < callers-1 {
					runtime.Gosched()
				}
				return &resolutionDB{}, nil
			})

			results := make([]*resolutionDB, callers)
			errs := make([]error, callers)
			var wg sync.WaitGroup
			for n := range callers {
				wg.Go(func() { results[n], errs[n] = InvokeNamed[*resolutionDB](root, "db") })
			}
			wg.Wait()

			for n := range callers {
				is.NoError(errs[n])
				is.Same(results[0], results[n])
			}
			is.Equal(int32(1), calls.Load(), "provider calls")
		})
	}
}

func TestConcurrentResolution_SharedDependencyNoFalseCycle(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		deps map[string][]string // provider -> dependencies; "d" has none
		top  []string            // invoked, one goroutine each
	}{
		{name: "two_on_d", deps: map[string][]string{"a": {"d"}, "b": {"d"}}, top: []string{"a", "b"}},
		{name: "diamond", deps: map[string][]string{"a": {"b", "c"}, "b": {"d"}, "c": {"d"}}, top: []string{"a"}},
		{name: "chain_and_direct", deps: map[string][]string{"a": {"d"}, "b": {"a", "d"}}, top: []string{"a", "b"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			testWithTimeout(t, 5*time.Second)
			is := assert.New(t)

			root := New()
			others := len(tt.deps)
			var started atomic.Int32
			var dCalls atomic.Int32
			ProvideNamed(root, "d", func(Injector) (int, error) {
				dCalls.Add(1)
				// blocks until every other provider started, or briefly when
				// some never start concurrently (the diamond's single
				// goroutine starts them in turn)
				deadline := time.Now().Add(200 * time.Millisecond)
				for started.Load() < int32(others) && time.Now().Before(deadline) {
					runtime.Gosched()
				}
				return 1, nil
			})
			for name, deps := range tt.deps {
				ProvideNamed(root, name, func(i Injector) (int, error) {
					started.Add(1)
					sum := 0
					for _, dep := range deps {
						value, err := InvokeNamed[int](i, dep)
						if err != nil {
							return 0, err
						}
						sum += value
					}
					return sum, nil
				})
			}

			for n, err := range invokeConcurrently(root, tt.top) {
				is.NoError(err, "invoke of %q", tt.top[n])
			}
			is.Equal(int32(1), dCalls.Load(), "provider calls of d")
		})
	}
}

func TestConcurrentResolution_WaitersShareFailure(t *testing.T) {
	t.Parallel()

	errRefused := errors.New("refused")
	tests := []struct {
		name    string
		outcome func() (*resolutionDB, error)
		check   func(t *testing.T, err error)
	}{
		{name: "error", outcome: func() (*resolutionDB, error) { return nil, errRefused }, check: func(t *testing.T, err error) {
			t.Helper()
			assert.ErrorIs(t, err, errRefused)
		}},
		{name: "panic", outcome: func() (*resolutionDB, error) { panic("boom") }, check: func(t *testing.T, err error) {
			t.Helper()
			assert.ErrorIs(t, err, ErrProviderPanic)
			assert.ErrorContains(t, err, "boom")
		}},
		{name: "goexit", outcome: func() (*resolutionDB, error) { runtime.Goexit(); return nil, nil }, check: func(t *testing.T, err error) {
			t.Helper()
			assert.ErrorIs(t, err, ErrProviderPanic)
			assert.ErrorContains(t, err, "runtime.Goexit")
			var serviceErr *ServiceError
			if assert.ErrorAs(t, err, &serviceErr) {
				assert.Equal(t, "db", serviceErr.Service)
			}
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			testWithTimeout(t, 5*time.Second)
			is := assert.New(t)

			root := New()
			var calls atomic.Int32
			ProvideNamed(root, "db", func(Injector) (*resolutionDB, error) {
				if calls.Add(1) == 1 {
					// the 8 others wait for this build
					for waitingForBuilds(root) < 8 {
						runtime.Gosched()
					}
					return tt.outcome()
				}
				return &resolutionDB{}, nil
			})

			// the builder may be any of the 9; a goroutine that called
			// runtime.Goexit never reports
			errs := make(chan error, 9)
			var wg sync.WaitGroup
			for range 9 {
				wg.Go(func() {
					_, err := InvokeNamed[*resolutionDB](root, "db")
					errs <- err
				})
			}
			wg.Wait()
			close(errs)

			reported := 0
			for err := range errs {
				reported++
				tt.check(t, err)
			}
			is.GreaterOrEqual(reported, 8, "waiting calls that returned")
			is.Equal(int32(1), calls.Load(), "provider calls")

			_, err := InvokeNamed[*resolutionDB](root, "db")
			is.NoError(err)
			is.Equal(int32(2), calls.Load(), "a later invoke builds again")
		})
	}
}

func TestConcurrentResolution_StaleWaitNoFalseCycle(t *testing.T) {
	t.Parallel()
	testWithTimeout(t, 60*time.Second)

	// No cycle: a -> d, then a -> c; c -> d. G1 invokes a (and builds d), G2
	// invokes c and waits for d; once d is built, G1 goes on to c, which G2
	// still builds while its wait edge on G1 may still be registered.
	falseCycles := 0
	const rounds = 300
	for range rounds {
		root := New()
		ProvideNamed(root, "d", func(Injector) (int, error) {
			for waitingForBuilds(root) < 1 { // G2 waits for d, owned by G1
				runtime.Gosched()
			}
			return 1, nil
		})
		ProvideNamed(root, "c", func(i Injector) (int, error) { return InvokeNamed[int](i, "d") })
		cStarted := make(chan struct{})
		var once sync.Once
		ProvideNamed(root, "a", func(i Injector) (int, error) {
			<-cStarted
			if _, err := InvokeNamed[int](i, "d"); err != nil {
				return 0, err
			}
			return InvokeNamed[int](i, "c")
		})
		root.AddBeforeInvocationHook(func(_ *Scope, name string) {
			if name == "c" {
				once.Do(func() { close(cStarted) })
			}
		})

		errs := invokeConcurrently(root, []string{"a", "c"})
		if errs[0] != nil || errs[1] != nil {
			falseCycles++
		}
	}

	assert.Zero(t, falseCycles, "rounds with a false ErrCircularDependency out of %d", rounds)
}

func TestConcurrentResolution_CrossContainerCycle(t *testing.T) {
	t.Parallel()
	testWithTimeout(t, 3*time.Second)

	r1, r2 := New(), New()
	barrier := newResolutionBarrier(2)
	var o1, o2 sync.Once
	ProvideNamed(r1, "a", func(Injector) (int, error) {
		o1.Do(barrier.wait)
		return InvokeNamed[int](r2, "b")
	})
	ProvideNamed(r2, "b", func(Injector) (int, error) {
		o2.Do(barrier.wait)
		return InvokeNamed[int](r1, "a")
	})

	var wg sync.WaitGroup
	var e1, e2 error
	wg.Go(func() { _, e1 = InvokeNamed[int](r1, "a") })
	wg.Go(func() { _, e2 = InvokeNamed[int](r2, "b") })
	wg.Wait()

	assert.ErrorIs(t, e1, ErrCircularDependency)
	assert.ErrorIs(t, e2, ErrCircularDependency)
}

func TestConcurrentResolution_HookPanicDuringBuild(t *testing.T) {
	t.Parallel()
	testWithTimeout(t, 5*time.Second)
	is := assert.New(t)

	root := New()
	ProvideNamed(root, "dep", func(Injector) (int, error) { return 1, nil })
	ProvideNamed(root, "a", func(Injector) (int, error) { return 1, nil }, DependsOnNamed("dep"))
	var panicked atomic.Bool
	root.AddBeforeInvocationHook(func(_ *Scope, name string) {
		if name == "dep" && panicked.CompareAndSwap(false, true) {
			for waitingForBuilds(root) < 4 {
				runtime.Gosched()
			}
			panic("hook boom")
		}
	})

	errs := make(chan error, 5)
	recovered := make(chan any, 5)
	var wg sync.WaitGroup
	for range 5 {
		wg.Go(func() {
			defer func() {
				if r := recover(); r != nil {
					recovered <- r
				}
			}()
			_, err := InvokeNamed[int](root, "a")
			errs <- err
		})
	}
	wg.Wait()
	close(errs)
	close(recovered)

	is.Len(recovered, 1, "the builder re-panics")
	is.Len(errs, 4, "waiters")
	for err := range errs {
		is.ErrorIs(err, ErrProviderPanic)
		var serviceErr *ServiceError
		if is.ErrorAs(err, &serviceErr) {
			is.Equal("a", serviceErr.Service)
		}
		is.ErrorContains(err, "hook boom")
		is.NotContains(err.Error(), "provider panicked", "the provider did not panic")
	}
	_, err := InvokeNamed[int](root, "a")
	is.NoError(err, "a later invocation builds again")
}

func TestConcurrentResolution_SelfOwnedChainMessage(t *testing.T) {
	t.Parallel()
	testWithTimeout(t, 2*time.Second)

	root := New()
	ProvideNamed(root, "a", func(Injector) (int, error) { return InvokeNamed[int](root, "b") })
	ProvideNamed(root, "b", func(Injector) (int, error) { return InvokeNamed[int](root, "a") })

	_, err := InvokeNamed[int](root, "a")

	assert.ErrorIs(t, err, ErrCircularDependency)
	assert.ErrorContains(t, err, "`a` -> `b` -> `a`")
}
