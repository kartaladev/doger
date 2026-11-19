package bench

// The benchmarks of design D17: each optimization of group 54 is compared
// with bench/baseline.txt on them.

import (
	"context"
	"fmt"
	"testing"

	"github.com/kartaladev/doger"
)

// scopeAtDepth returns a scope depth levels below root (root's own scope
// for 0).
func scopeAtDepth(root *doger.RootScope, depth int) doger.Injector {
	var scope doger.Injector = root
	for level := range depth {
		scope = scope.Scope(fmt.Sprintf("level-%d", level+1))
	}

	return scope
}

// BenchmarkInvokeBuilt invokes a built lazy singleton registered in the root,
// from the root and from a scope 5 levels below it.
func BenchmarkInvokeBuilt(b *testing.B) {
	for _, depth := range []int{0, 5} {
		b.Run(fmt.Sprintf("depth_%d", depth), func(b *testing.B) {
			root := doger.New()
			doger.Provide(root, func(doger.Injector) (*benchSvcA, error) { return &benchSvcA{}, nil })
			scope := scopeAtDepth(root, depth)
			_ = doger.MustInvoke[*benchSvcA](scope)

			b.ReportAllocs()
			for b.Loop() {
				if _, err := doger.Invoke[*benchSvcA](scope); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// BenchmarkInvokeBuiltParallel invokes one built lazy singleton from every
// GOMAXPROCS goroutine.
func BenchmarkInvokeBuiltParallel(b *testing.B) {
	root := doger.New()
	doger.Provide(root, func(doger.Injector) (*benchSvcA, error) { return &benchSvcA{}, nil })
	_ = doger.MustInvoke[*benchSvcA](root)

	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			if _, err := doger.Invoke[*benchSvcA](root); err != nil {
				b.Error(err)
				return
			}
		}
	})
}

// BenchmarkInvokeAll collects the 10 members of a type among 0, 1k or 10k
// unrelated services.
func BenchmarkInvokeAll(b *testing.B) {
	for _, unrelated := range []int{0, 1000, 10000} {
		b.Run(fmt.Sprintf("unrelated_%d", unrelated), func(b *testing.B) {
			root := doger.New()
			for j := range 10 {
				doger.ProvideNamedValue(root, fmt.Sprintf("member-%d", j), &benchSvcImpl{})
			}
			for j := range unrelated {
				doger.ProvideNamedValue(root, fmt.Sprintf("unrelated-%d", j), &benchSvcA{})
			}
			if members := doger.MustInvokeAll[benchIface](root); len(members) != 10 {
				b.Fatalf("%d members", len(members))
			}

			b.ReportAllocs()
			for b.Loop() {
				if _, err := doger.InvokeAll[benchIface](root); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// BenchmarkPerRequestScope creates a child scope, registers a request value,
// invokes a root singleton and a request service from it, and shuts it down.
func BenchmarkPerRequestScope(b *testing.B) {
	root := doger.New()
	doger.Provide(root, func(doger.Injector) (*benchSvcA, error) { return &benchSvcA{}, nil })
	_ = doger.MustInvoke[*benchSvcA](root)

	b.ReportAllocs()
	for b.Loop() {
		req := root.Scope("request")
		doger.ProvideNamedValue(req, "request-id", "id")
		doger.Provide(req, func(i doger.Injector) (*benchSvcB, error) {
			_ = doger.MustInvoke[*benchSvcA](i)
			return &benchSvcB{}, nil
		})
		if _, err := doger.Invoke[*benchSvcB](req); err != nil {
			b.Fatal(err)
		}
		if report := req.Shutdown(); !report.Succeed {
			b.Fatal(report)
		}
	}
}

// BenchmarkProvideValue100 creates a container and registers 100 values.
func BenchmarkProvideValue100(b *testing.B) {
	names := make([]string, 100)
	for j := range names {
		names[j] = fmt.Sprintf("value-%d", j)
	}

	b.ReportAllocs()
	for b.Loop() {
		root := doger.New()
		for _, name := range names {
			doger.ProvideNamedValue(root, name, &benchSvcA{})
		}
	}
}

// BenchmarkHealthCheck50 checks 50 built health checkers.
func BenchmarkHealthCheck50(b *testing.B) {
	root := doger.New()
	for j := range 50 {
		name := fmt.Sprintf("hc-%d", j)
		doger.ProvideNamed(root, name, func(doger.Injector) (*benchHealthchecker, error) { return &benchHealthchecker{}, nil })
		_ = doger.MustInvokeNamed[*benchHealthchecker](root, name)
	}

	b.ReportAllocs()
	for b.Loop() {
		if results := root.HealthCheckWithContext(context.Background()); len(results) != 50 {
			b.Fatalf("%d results", len(results))
		}
	}
}

// newShutdownChain returns a container holding a built chain of n lazy
// services, each depending on the previous one.
func newShutdownChain(b *testing.B, names []string) *doger.RootScope {
	b.Helper()

	root := doger.New()
	doger.ProvideNamed(root, names[0], func(doger.Injector) (*benchShutdowner, error) { return &benchShutdowner{}, nil })
	for j := 1; j < len(names); j++ {
		dependency := names[j-1]
		doger.ProvideNamed(root, names[j], func(i doger.Injector) (*benchShutdowner, error) {
			_ = doger.MustInvokeNamed[*benchShutdowner](i, dependency)
			return &benchShutdowner{}, nil
		})
	}
	_ = doger.MustInvokeNamed[*benchShutdowner](root, names[len(names)-1])

	return root
}

// BenchmarkShutdownChain shuts down a built chain of 1k or 4k services; only
// the shutdown is timed.
func BenchmarkShutdownChain(b *testing.B) {
	for _, n := range []int{1000, 4000} {
		b.Run(fmt.Sprintf("%d", n), func(b *testing.B) {
			names := make([]string, n)
			for j := range names {
				names[j] = fmt.Sprintf("chain-%d", j)
			}

			b.ReportAllocs()
			for b.Loop() {
				b.StopTimer()
				root := newShutdownChain(b, names)
				b.StartTimer()
				if report := root.Shutdown(); !report.Succeed {
					b.Fatal(report)
				}
			}
		})
	}
}
