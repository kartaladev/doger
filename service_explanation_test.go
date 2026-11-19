package doger

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
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
		// Invoke[T] resolves by the type's name, through invokeByName
		{name: "by_type_name", b: NameOf[*explainEdgeB](), resolve: func(i Injector) error { _, err := Invoke[*explainEdgeB](i); return err }},
		// InvokeAs[T] selects by type, through invokeServiceInstance
		{name: "by_type_selection", b: "b", resolve: func(i Injector) error { _, err := InvokeAs[*explainEdgeB](i); return err }},
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

// provideLayered registers count lazy services "svc-0" to "svc-<count-1>",
// where "svc-1" invokes "svc-0" and every "svc-k" with k >= 2 invokes
// "svc-<k-1>" then "svc-<k-2>", and invokes the last one.
func provideLayered(t *testing.T, root *RootScope, count int) {
	t.Helper()

	for k := range count {
		ProvideNamed(root, fmt.Sprintf("svc-%d", k), func(i Injector) (int, error) {
			for _, dep := range []int{k - 1, k - 2} {
				if dep < 0 {
					continue
				}
				if _, err := InvokeNamed[int](i, fmt.Sprintf("svc-%d", dep)); err != nil {
					return 0, err
				}
			}
			return k, nil
		})
	}
	_, err := InvokeNamed[int](root, fmt.Sprintf("svc-%d", count-1))
	require.NoError(t, err)
}

// explainNodeCounts returns the number of nodes of a tree, of nodes with Ref
// false, and of nodes with Ref true and no Recursive children.
func explainNodeCounts(nodes []ExplainServiceDependencyOutput) (total, expanded, refs int) {
	for _, node := range nodes {
		total++
		if node.Ref {
			if len(node.Recursive) == 0 {
				refs++
			}
		} else {
			expanded++
		}
		t, e, r := explainNodeCounts(node.Recursive)
		total, expanded, refs = total+t, expanded+e, refs+r
	}

	return total, expanded, refs
}

func TestExplain_LayeredGraphBounded(t *testing.T) {
	t.Parallel()

	tests := []struct {
		count, nodes, expanded, refs int
	}{
		{count: 1, nodes: 0, expanded: 0, refs: 0},
		{count: 2, nodes: 1, expanded: 1, refs: 0},
		{count: 3, nodes: 3, expanded: 2, refs: 1},
		{count: 24, nodes: 45, expanded: 23, refs: 22},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprint(tt.count), func(t *testing.T) {
			t.Parallel()
			testWithTimeout(t, 5*time.Second)
			is := assert.New(t)
			must := require.New(t)

			root := New()
			provideLayered(t, root, tt.count)

			output, ok := ExplainNamedService(root, fmt.Sprintf("svc-%d", tt.count-1))
			must.True(ok)
			is.NotNil(output.Dependencies)
			nodes, expanded, refs := explainNodeCounts(output.Dependencies)
			is.Equal(tt.nodes, nodes)
			is.Equal(tt.expanded, expanded)
			is.Equal(tt.refs, refs)
		})
	}
}

func TestExplain_DependentsBounded(t *testing.T) {
	t.Parallel()
	testWithTimeout(t, 5*time.Second)
	is := assert.New(t)
	must := require.New(t)

	root := New()
	provideLayered(t, root, 24)

	output, ok := ExplainNamedService(root, "svc-0")
	must.True(ok)
	nodes, expanded, _ := explainNodeCounts(output.Dependents)
	is.Equal(45, nodes)
	is.Equal(23, expanded)
}

// provideRefGraph registers "a" invoking "b" then "c", and "b" invoking
// "c", and invokes "a".
func provideRefGraph(t *testing.T) *RootScope {
	t.Helper()

	root := New()
	ProvideNamedValue(root, "c", 3)
	ProvideNamed(root, "b", func(i Injector) (int64, error) {
		_, err := InvokeNamed[int](i, "c")
		return 2, err
	})
	ProvideNamed(root, "a", func(i Injector) (string, error) {
		if _, err := InvokeNamed[int64](i, "b"); err != nil {
			return "", err
		}
		_, err := InvokeNamed[int](i, "c")
		return "a", err
	})
	_, err := InvokeNamed[string](root, "a")
	require.NoError(t, err)

	return root
}

func TestExplain_RefRendersSingleLine(t *testing.T) {
	t.Parallel()
	is := assert.New(t)
	must := require.New(t)

	output, ok := ExplainNamedService(provideRefGraph(t), "a")
	must.True(ok)

	text := output.String()
	start := strings.Index(text, "Dependencies:\n")
	end := strings.Index(text, "\nDependents:")
	must.True(start >= 0 && end > start, text)
	lines := strings.Split(strings.Trim(text[start+len("Dependencies:\n"):end], "\n"), "\n")

	var cLines, refLines []int
	for k, line := range lines {
		if strings.HasPrefix(strings.TrimLeft(line, " "), "* c ") {
			cLines = append(cLines, k)
		}
		if strings.HasSuffix(line, " (ref)") {
			refLines = append(refLines, k)
		}
	}
	is.Len(cLines, 2, text)
	must.Len(refLines, 1, text)
	is.Contains(cLines, refLines[0])

	// no line is nested under the reference
	ref := refLines[0]
	indent := len(lines[ref]) - len(strings.TrimLeft(lines[ref], " "))
	if ref+1 < len(lines) {
		next := lines[ref+1]
		is.LessOrEqual(len(next)-len(strings.TrimLeft(next, " ")), indent, text)
	}
}

func TestExplain_RefJSON(t *testing.T) {
	t.Parallel()
	must := require.New(t)

	output, ok := ExplainNamedService(provideRefGraph(t), "a")
	must.True(ok)
	data, err := json.Marshal(output)
	must.NoError(err)

	var decoded struct {
		Dependencies []map[string]any `json:"dependencies"`
	}
	must.NoError(json.Unmarshal(data, &decoded))

	var refTrue, refKeys int
	var walk func(nodes []any)
	walk = func(nodes []any) {
		for _, node := range nodes {
			object, ok := node.(map[string]any)
			must.True(ok)
			if ref, ok := object["ref"]; ok {
				refKeys++
				if ref == true {
					refTrue++
				}
			}
			if children, ok := object["recursive"].([]any); ok {
				walk(children)
			}
		}
	}
	for _, node := range decoded.Dependencies {
		walk([]any{node})
	}

	assert.Equal(t, 1, refTrue, string(data))
	assert.Equal(t, 1, refKeys, string(data))
}

func TestExplain_PlainText(t *testing.T) {
	t.Parallel()

	explainNamed := func(t *testing.T, _ *RootScope, scope *Scope, service string) string {
		t.Helper()
		output, ok := ExplainNamedService(scope, service)
		require.True(t, ok)
		return output.String()
	}
	explainInjector := func(_ *testing.T, root *RootScope, _ *Scope, _ string) string {
		output := ExplainInjector(root)
		return output.String()
	}

	tests := []struct {
		name    string
		scope   string
		service string
		explain func(t *testing.T, root *RootScope, scope *Scope, service string) string
	}{
		{name: "named", scope: "ext", service: `a<b>&'c"`, explain: explainNamed},
		{name: "injector", scope: "ext", service: `a<b>&'c"`, explain: explainInjector},
		{name: "scope_name", scope: "t<1>", service: "db", explain: explainInjector},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			is := assert.New(t)

			root := New()
			scope := root.Scope(tt.scope)
			ProvideNamedValue(scope, tt.service, 1)
			_, err := InvokeNamed[int](scope, tt.service)
			require.NoError(t, err)

			text := tt.explain(t, root, scope, tt.service)
			is.Contains(text, tt.service)
			is.Contains(text, tt.scope)
			for _, escaped := range []string{"&lt;", "&gt;", "&amp;", "&#39;", "&#34;"} {
				is.NotContains(text, escaped)
			}
		})
	}
}

func TestExplain_RefOrderRepeatable(t *testing.T) {
	t.Parallel()
	is := assert.New(t)

	const runs = 30

	// "a" (scope "child") invokes "p" (scope "child") then "q" (root), and
	// both invoke "shared" (root): siblings are ordered by scope name, then
	// service name, so "q" ("[root]") is expanded first and "p" holds the
	// reference, on every run whatever the random scope IDs
	for range runs {
		root := New()
		child := root.Scope("child")
		ProvideNamedValue(root, "shared", 1)
		ProvideNamed(root, "q", func(i Injector) (int64, error) {
			_, err := InvokeNamed[int](i, "shared")
			return 2, err
		})
		ProvideNamed(child, "p", func(i Injector) (int32, error) {
			_, err := InvokeNamed[int](i, "shared")
			return 3, err
		})
		ProvideNamed(child, "a", func(i Injector) (string, error) {
			if _, err := InvokeNamed[int32](i, "p"); err != nil {
				return "", err
			}
			_, err := InvokeNamed[int64](i, "q")
			return "a", err
		})
		_, err := InvokeNamed[string](child, "a")
		require.NoError(t, err)

		output, ok := ExplainNamedService(child, "a")
		require.True(t, ok)
		require.Len(t, output.Dependencies, 2)
		q, p := output.Dependencies[0], output.Dependencies[1]
		if !is.Equal([]string{"q", "p"}, []string{q.Service, p.Service}) {
			return
		}
		require.Len(t, q.Recursive, 1)
		require.Len(t, p.Recursive, 1)
		is.False(q.Recursive[0].Ref, "shared under q")
		if !is.True(p.Recursive[0].Ref, "shared under p") {
			return
		}
	}
}

func TestExplain_EdgeRemovedDuringExplanation(t *testing.T) {
	t.Parallel()
	is := assert.New(t)
	must := require.New(t)

	root := New()
	ProvideNamedValue(root, "a", 1)
	ProvideNamedValue(root, "b", 2)
	must.NoError(DeclareDependency(root, "a", "b"))

	// the edge a -> b leaves the dependencies of "a" while the dependents of
	// "b" still list it, as when it is removed between two reads of the graph
	a := newServiceDescription(root.ID(), root.Name(), "a")
	b := newServiceDescription(root.ID(), root.Name(), "b")
	root.dag.mu.Lock()
	delete(root.dag.dependencies[a], b)
	root.dag.mu.Unlock()

	output, ok := ExplainNamedService(root, "b")
	must.True(ok)
	must.Len(output.Dependents, 1)
	is.Equal(DependencySourceDeclared, output.Dependents[0].Source, "the source read with the listed edge")
	_, err := json.Marshal(output)
	is.NoError(err)
}

func TestServiceDescription_JSONKeys(t *testing.T) {
	t.Parallel()

	data, err := json.Marshal(ServiceDescription{ScopeID: "id", ScopeName: "[root]", Service: "db"})
	require.NoError(t, err)
	assert.JSONEq(t, `{"scopeId":"id","scopeName":"[root]","service":"db"}`, string(data))
}

// FuzzExplainText checks that any scope and service name renders without a
// panic and verbatim in the text of ExplainNamedService and ExplainInjector.
// ExplainInjector indents every line of its tree, so a name holding a
// newline is checked line by line there.
func FuzzExplainText(f *testing.F) {
	f.Add("ext", `a<b>&'c"`)
	f.Add("t<1>", "db")
	f.Add("", "")
	f.Add("{{.ScopeID}}", "{{.Service}}")
	f.Add("\xff", "\n* fake from scope x")

	f.Fuzz(func(t *testing.T, scopeName, serviceName string) {
		root := New()
		scope := root.Scope(scopeName)
		ProvideNamedValue(scope, serviceName, 1)

		named, ok := ExplainNamedService(scope, serviceName)
		require.True(t, ok)
		injector := ExplainInjector(root)

		namedText := named.String()
		require.Contains(t, namedText, serviceName)
		require.Contains(t, namedText, scopeName)

		injectorText := injector.String()
		for _, name := range []string{serviceName, scopeName} {
			for line := range strings.SplitSeq(name, "\n") {
				require.Contains(t, injectorText, line)
			}
		}
	})
}
