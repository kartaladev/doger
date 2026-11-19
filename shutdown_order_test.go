package doger

import (
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// chainLink is a service of a dependency chain whose Shutdown records when it
// begins and ends, taking hold of bubble time in between.
type chainLink struct {
	name string
	hold time.Duration
	rec  *callRecorder
}

func (l *chainLink) Shutdown() error {
	l.rec.record(l.name + " begin")
	time.Sleep(l.hold)
	l.rec.record(l.name + " end")

	return nil
}

// TestShutdownOrder_ThreeLevelChain pins that a service is shut down only
// after every service depending on it finished: with a -> b -> c in one
// scope, c must not begin before b ended, even when b takes time.
func TestShutdownOrder_ThreeLevelChain(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		rec := &callRecorder{}
		root := New()
		provideLink := func(name, dependency string, hold time.Duration) {
			ProvideNamed(root, name, func(i Injector) (*chainLink, error) {
				if dependency != "" {
					if _, err := InvokeNamed[*chainLink](i, dependency); err != nil {
						return nil, err
					}
				}
				return &chainLink{name: name, hold: hold, rec: rec}, nil
			})
		}
		provideLink("c", "", 0)
		provideLink("b", "c", time.Second)
		provideLink("a", "b", 0)

		_, err := InvokeNamed[*chainLink](root, "a")
		require.NoError(t, err)

		report := root.Shutdown()

		require.True(t, report.Succeed, report.Error())
		assert.Equal(t, []string{"a begin", "a end", "b begin", "b end", "c begin", "c end"}, rec.list())
	})
}

// TestDAG_removeDependent pins that removing a service in the middle of a
// chain removes it from both sides: its dependent no longer depends on it,
// and its dependency no longer lists it as a dependent.
func TestDAG_removeDependent(t *testing.T) {
	t.Parallel()
	is := assert.New(t)

	a := newServiceDescription("s", "s", "a")
	c := newServiceDescription("s", "s", "c")

	dag := newDAG()
	dag.addDependency("s", "s", "a", "s", "s", "b", DependencySourceInvoked)
	dag.addDependency("s", "s", "b", "s", "s", "c", DependencySourceInvoked)

	dag.removeService("s", "s", "b")

	dependencies, dependents := dag.explainService("s", "s", "a")
	is.Empty(dependencies, "a no longer depends on the removed b")
	is.Empty(dependents)
	dependencies, dependents = dag.explainService("s", "s", "c")
	is.Empty(dependencies)
	is.Empty(dependents, "c no longer lists the removed b as a dependent")
	is.Empty(dag.dependencies[a])
	is.Empty(dag.dependents[c])
}
