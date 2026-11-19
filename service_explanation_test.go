package doger

import (
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// slowBuildTime returns the build time explain reports for the service
// "slow" of root, through ExplainNamedService or ExplainInjector.
type slowBuildTime func(root *RootScope) (time.Duration, bool)

func explainNamedSlow(root *RootScope) (time.Duration, bool) {
	output, ok := ExplainNamedService(root, "slow")
	return output.ServiceBuildTime, ok
}

func explainInjectorSlow(root *RootScope) (time.Duration, bool) {
	output := ExplainInjector(root)
	for _, scope := range output.DAG {
		for _, service := range scope.Services {
			if service.ServiceName == "slow" {
				return service.ServiceBuildTime, true
			}
		}
	}

	return 0, false
}

func TestExplain_ConcurrentWithLazyBuild(t *testing.T) {
	t.Parallel()

	const (
		goroutines   = 8
		calls        = 100
		releaseAfter = 50
	)

	tests := []struct {
		name    string
		explain slowBuildTime
	}{
		{name: "named", explain: explainNamedSlow},
		{name: "injector", explain: explainInjectorSlow},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			testWithTimeout(t, 10*time.Second)
			is := assert.New(t)
			must := require.New(t)

			root := New()
			started := make(chan struct{})
			release := make(chan struct{})
			ProvideNamed(root, "slow", func(Injector) (string, error) {
				close(started)
				<-release
				return "slow-instance", nil
			})

			invoked := make(chan error, 1)
			go func() {
				_, err := InvokeNamed[string](root, "slow")
				invoked <- err
			}()
			<-started

			var (
				count       atomic.Int64
				released    atomic.Bool
				releaseOnce sync.Once
				wg          sync.WaitGroup
				mu          sync.Mutex
				early       []time.Duration // build times observed before the release
			)
			for range goroutines {
				wg.Go(func() {
					for range calls {
						buildTime, ok := tt.explain(root)
						if !released.Load() { // the call ended before the release
							mu.Lock()
							early = append(early, buildTime)
							mu.Unlock()
						}
						is.True(ok)

						if count.Add(1) == releaseAfter {
							releaseOnce.Do(func() {
								released.Store(true)
								close(release)
							})
						}
					}
				})
			}
			wg.Wait()
			must.NoError(<-invoked)

			is.NotEmpty(early)
			for _, buildTime := range early {
				is.Zero(buildTime)
			}

			buildTime, ok := tt.explain(root)
			must.True(ok)
			is.Positive(buildTime)
		})
	}
}

func TestExplain_ConcurrentWithShutdown(t *testing.T) {
	t.Parallel()
	testWithTimeout(t, 10*time.Second)
	must := require.New(t)

	const (
		services   = 20
		goroutines = 4
		calls      = 100
	)

	root := New()
	req := root.Scope("req")
	for k := range services {
		name := fmt.Sprintf("svc-%d", k)
		ProvideNamed(req, name, func(Injector) (int, error) { return k, nil })
		_, err := InvokeNamed[int](req, name)
		must.NoError(err)
	}

	// the shutdown starts once the explaining goroutines run, so they overlap
	var (
		wg        sync.WaitGroup
		explained = make(chan struct{})
		once      sync.Once
	)
	wg.Go(func() {
		<-explained
		_ = req.Shutdown()
	})
	for range goroutines {
		wg.Go(func() {
			for range calls {
				assert.NotPanics(t, func() { _ = ExplainInjector(root) })
				once.Do(func() { close(explained) })
			}
		})
	}
	wg.Wait()
}

type explainEdgeA struct{}

type explainEdgeB struct{}

// explainEdgeFields resolves "b" by struct injection.
type explainEdgeFields struct {
	B *explainEdgeB `do:"b"`
}

func TestExplain_FailedDependencyNoEdge(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		b       string                 // the name "b" is registered under
		resolve func(i Injector) error // how "a" resolves "b"
	}{
		{name: "named", b: "b", resolve: func(i Injector) error { _, err := InvokeNamed[*explainEdgeB](i, "b"); return err }},
		{name: "named_any", b: "b", resolve: func(i Injector) error { _, err := InvokeNamed[any](i, "b"); return err }},
		{name: "by_type", b: NameOf[*explainEdgeB](), resolve: func(i Injector) error { _, err := Invoke[*explainEdgeB](i); return err }},
		{name: "struct", b: "b", resolve: func(i Injector) error { _, err := InvokeStruct[explainEdgeFields](i); return err }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			is := assert.New(t)
			must := require.New(t)

			root := New()
			errBDown := errors.New("b down")
			ProvideNamed(root, tt.b, func(Injector) (*explainEdgeB, error) { return nil, errBDown })
			ProvideNamed(root, "a", func(i Injector) (*explainEdgeA, error) { return &explainEdgeA{}, tt.resolve(i) })

			_, err := InvokeNamed[*explainEdgeA](root, "a")
			must.ErrorIs(err, errBDown)

			a, ok := ExplainNamedService(root, "a")
			must.True(ok)
			is.Empty(a.Dependencies)
			b, ok := ExplainNamedService(root, tt.b)
			must.True(ok)
			is.Empty(b.Dependents)
		})
	}
}

func TestExplain_RetryRecordsEdgeOnce(t *testing.T) {
	t.Parallel()
	is := assert.New(t)
	must := require.New(t)

	root := New()
	var calls atomic.Int64
	ProvideNamed(root, "b", func(Injector) (*explainEdgeB, error) {
		if calls.Add(1) == 1 {
			return nil, errors.New("b down")
		}
		return &explainEdgeB{}, nil
	})
	ProvideNamed(root, "a", func(i Injector) (*explainEdgeA, error) {
		_, err := InvokeNamed[*explainEdgeB](i, "b")
		return &explainEdgeA{}, err
	})

	_, err := InvokeNamed[*explainEdgeA](root, "a")
	must.Error(err)
	for range 2 {
		_, err = InvokeNamed[*explainEdgeA](root, "a")
		must.NoError(err)
	}

	a, ok := ExplainNamedService(root, "a")
	must.True(ok)
	must.Len(a.Dependencies, 1)
	is.Equal("b", a.Dependencies[0].Service)
}
