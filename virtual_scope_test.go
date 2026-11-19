package doger

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// vsHealthy is a health checker that always succeeds.
type vsHealthy struct{}

func (vsHealthy) HealthCheck() error { return nil }

// providerInjector registers lazy "probe" in scope, whose provider stores the
// injector it receives, invokes it, and returns that injector: the virtual
// scope of the invocation.
func providerInjector(t *testing.T, scope Injector) *virtualScope {
	t.Helper()

	var captured Injector
	ProvideNamed(scope, "probe", func(i Injector) (int, error) {
		captured = i
		return 1, nil
	})
	_, err := InvokeNamed[int](scope, "probe")
	require.NoError(t, err)

	vs, ok := captured.(*virtualScope)
	require.True(t, ok, "a provider receives a virtual scope, got %T", captured)

	return vs
}

// TestVirtualScope_PassThrough pins that each method a provider's injector
// passes through reaches the scope the service is registered in.
func TestVirtualScope_PassThrough(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		check func(t *testing.T, root *RootScope, tenant *Scope, vs *virtualScope)
	}{
		{
			name: "Ancestors",
			check: func(t *testing.T, _ *RootScope, tenant *Scope, vs *virtualScope) {
				t.Helper()
				assert.Equal(t, tenant.Ancestors(), vs.Ancestors())
			},
		},
		{
			name: "Children, ChildByID and ChildByName",
			check: func(t *testing.T, _ *RootScope, tenant *Scope, vs *virtualScope) {
				t.Helper()
				child := tenant.Scope("child")
				assert.Equal(t, []*Scope{child}, vs.Children())
				byID, ok := vs.ChildByID(child.ID())
				assert.True(t, ok)
				assert.Same(t, child, byID)
				byName, ok := vs.ChildByName("child")
				assert.True(t, ok)
				assert.Same(t, child, byName)
			},
		},
		{
			name: "ListInvokedServices",
			check: func(t *testing.T, _ *RootScope, tenant *Scope, vs *virtualScope) {
				t.Helper()
				assert.Equal(t, tenant.ListInvokedServices(), vs.ListInvokedServices())
				assert.NotEmpty(t, vs.ListInvokedServices())
			},
		},
		{
			name: "HealthCheck and HealthCheckWithContext",
			check: func(t *testing.T, _ *RootScope, tenant *Scope, vs *virtualScope) {
				t.Helper()
				ProvideNamedValue(tenant, "healthy", vsHealthy{})
				want := tenant.HealthCheck()
				assert.Contains(t, want, "healthy")
				assert.Equal(t, want, vs.HealthCheck())
				assert.Equal(t, want, vs.HealthCheckWithContext(context.Background()))
			},
		},
		{
			name: "HealthReport and HealthReportWithContext",
			check: func(t *testing.T, _ *RootScope, tenant *Scope, vs *virtualScope) {
				t.Helper()
				ProvideNamedValue(tenant, "healthy", vsHealthy{})
				for _, report := range []HealthReport{vs.HealthReport(), vs.HealthReportWithContext(context.Background())} {
					assert.Equal(t, tenant.HealthReport().State, report.State)
					if assert.Len(t, report.Services, 1) {
						assert.Equal(t, "healthy", report.Services[0].Service.Service)
					}
				}
			},
		},
		{
			name: "serviceExist, serviceGet and serviceSet",
			check: func(t *testing.T, _ *RootScope, tenant *Scope, vs *virtualScope) {
				t.Helper()
				assert.True(t, vs.serviceExist("probe"))
				assert.False(t, vs.serviceExist("missing"))
				vs.serviceSet("set", newServiceEager("set", 7))
				got, ok := vs.serviceGet("set")
				assert.True(t, ok)
				assert.Same(t, got, func() any { s, _ := tenant.serviceGet("set"); return s }())
			},
		},
		{
			name: "serviceForEach",
			check: func(t *testing.T, _ *RootScope, _ *Scope, vs *virtualScope) {
				t.Helper()
				var names []string
				vs.serviceForEach(func(name string, _ *Scope, _ any) bool {
					names = append(names, name)
					return true
				})
				assert.Equal(t, []string{"probe"}, names)
			},
		},
		{
			name: "serviceHealthCheck",
			check: func(t *testing.T, _ *RootScope, tenant *Scope, vs *virtualScope) {
				t.Helper()
				ProvideNamedValue(tenant, "healthy", vsHealthy{})
				assert.NoError(t, vs.serviceHealthCheck(context.Background(), "healthy"))
				assert.ErrorIs(t, vs.serviceHealthCheck(context.Background(), "missing"), ErrServiceNotFound)
			},
		},
		{
			name: "shutdownService",
			check: func(t *testing.T, _ *RootScope, tenant *Scope, vs *virtualScope) {
				t.Helper()
				require.NoError(t, vs.shutdownService(context.Background(), "probe"))
				assert.False(t, tenant.serviceExist("probe"))
			},
		},
		{
			name: "onServiceInvoke",
			check: func(t *testing.T, _ *RootScope, tenant *Scope, vs *virtualScope) {
				t.Helper()
				ProvideNamedValue(tenant, "value", 1)
				vs.onServiceInvoke("value")
				invoked := tenant.ListInvokedServices()
				names := make([]string, 0, len(invoked))
				for _, desc := range invoked {
					names = append(names, desc.Service)
				}
				assert.Contains(t, names, "value")
			},
		},
		{
			name: "clone",
			check: func(t *testing.T, root *RootScope, tenant *Scope, vs *virtualScope) {
				t.Helper()
				clone := vs.clone(root, root.self)
				assert.Equal(t, tenant.Name(), clone.Name())
				assert.True(t, clone.serviceExist("probe"))
			},
		},
		{
			name: "ShutdownWithContext",
			check: func(t *testing.T, _ *RootScope, tenant *Scope, vs *virtualScope) {
				t.Helper()
				report := vs.ShutdownWithContext(context.Background())
				require.NotNil(t, report)
				assert.True(t, report.Succeed)
				assert.False(t, tenant.serviceExist("probe"))
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			testWithTimeout(t, time.Second)

			root := New()
			tenant := root.Scope("tenant")
			vs := providerInjector(t, tenant)

			tt.check(t, root, tenant, vs)
		})
	}
}

func TestVirtualScope_InvalidConstruction(t *testing.T) {
	t.Parallel()

	assert.PanicsWithValue(t, "doger: predecessor cannot be nil", func() { newVirtualScope(nil, []string{"a"}) })
	assert.PanicsWithValue(t, "doger: invokerChain cannot be empty", func() { newVirtualScope(New(), nil) })
}

// TestVirtualScope_EmptyBaseRecordsNoEdge covers the defensive branch of a
// virtual scope whose whole chain is empty: it has no invoker, so it
// records no dependency edge.
func TestVirtualScope_EmptyBaseRecordsNoEdge(t *testing.T) {
	t.Parallel()

	root := New()
	ProvideNamedValue(root, "db", 1)
	vs := &virtualScope{self: root, base: []string{}}

	_, ok := vs.getLastInvokerName()
	assert.False(t, ok)

	vs.addDependency(root, "db", root.self, DependencySourceInvoked)
	explained, found := ExplainNamedService(root, "db")
	require.True(t, found)
	assert.Empty(t, explained.Dependents)
}

// TestAliasResolvesTo_BrokenChains pins that a chain whose next hop is gone
// does not resolve to any name.
func TestAliasResolvesTo_BrokenChains(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		service func(t *testing.T, root *RootScope) any
	}{
		{
			name: "alias whose target was removed",
			service: func(t *testing.T, root *RootScope) any {
				t.Helper()
				ProvideNamedValue(root, "target", 1)
				require.NoError(t, AliasNamed[int, int](root, "target", "alias"))
				require.NoError(t, ShutdownNamed(root, "target"))
				alias, ok := root.serviceGet("alias")
				require.True(t, ok)
				return alias
			},
		},
		{
			name: "export whose source scope is unknown",
			service: func(_ *testing.T, root *RootScope) any {
				return &serviceExport[int]{name: "svc", owner: root.self, childName: "gone", sourceName: "svc"}
			},
		},
		{
			name: "export whose source service was removed",
			service: func(t *testing.T, root *RootScope) any {
				t.Helper()
				child := root.Scope("child")
				ProvideNamedValue(child, "svc", 1)
				require.NoError(t, ExportNamed[int](child, "svc"))
				export, ok := root.serviceGet("svc")
				require.True(t, ok)
				require.NoError(t, ShutdownNamed(child, "svc"))
				return export
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			root := New()
			service := tt.service(t, root)

			assert.False(t, aliasResolvesTo(service, root.self, "invoker"))
		})
	}
}
