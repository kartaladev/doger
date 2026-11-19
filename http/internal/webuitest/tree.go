package webuitest

import (
	"fmt"
	"html"
	"net/http"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/kartaladev/doger"
)

// Graph maps each service to the services its provider invokes.
type Graph map[string][]string

// Provide registers every service of g in root as a lazy service whose
// provider invokes its dependencies, then invokes top so that every edge is
// recorded.
func (g Graph) Provide(t testing.TB, root doger.Injector, top string) {
	t.Helper()

	for name, deps := range g {
		doger.ProvideNamed(root, name, func(i doger.Injector) (string, error) {
			for _, dep := range deps {
				if _, err := doger.InvokeNamed[string](i, dep); err != nil {
					return "", err
				}
			}

			return name, nil
		})
	}

	if _, err := doger.InvokeNamed[string](root, top); err != nil {
		t.Fatalf("invoke %q: %v", top, err)
	}
}

// LayeredGraph returns the graph of n services "svc-00" to "svc-<n-1>" where
// each "svc-N" with N >= 2 depends on "svc-(N-1)" and "svc-(N-2)", and
// "svc-01" depends on "svc-00". Its top is "svc-<n-1>".
func LayeredGraph(n int) (Graph, string) {
	g := Graph{}
	for i := range n {
		var deps []string
		for _, d := range []int{i - 1, i - 2} {
			if d >= 0 {
				deps = append(deps, fmt.Sprintf("svc-%02d", d))
			}
		}

		g[fmt.Sprintf("svc-%02d", i)] = deps
	}

	return g, fmt.Sprintf("svc-%02d", n-1)
}

// DependencyEntry is an entry of the dependency tree of a service page.
type DependencyEntry struct {
	Name string
	Href string
	Ref  bool
	// HasChildren reports whether a nested list follows the entry's link.
	HasChildren bool
}

var dependencyEntryRe = regexp.MustCompile(`(?s)<li class="service( ref)?">\s*<a href="([^"]*)">\s*(.*?)\s*</a>`)

// DependencyEntries returns the entries of the dependency tree of a service
// page, in document order, without those of its dependent tree.
func DependencyEntries(t testing.TB, page string) []DependencyEntry {
	t.Helper()

	start := strings.Index(page, "<h2>Dependencies:</h2>")
	end := strings.Index(page, "<h2>Dependents:</h2>")
	if start < 0 || end < start {
		t.Fatalf("page has no dependency tree")
	}

	section := page[start:end]
	matches := dependencyEntryRe.FindAllStringSubmatchIndex(section, -1)
	entries := make([]DependencyEntry, 0, len(matches))
	for _, m := range matches {
		// A nested list, if any, opens before the entry's own item or its
		// first child's item closes.
		rest := section[m[1]:]
		if i := strings.Index(rest, "</li>"); i >= 0 {
			rest = rest[:i]
		}

		entries = append(entries, DependencyEntry{
			Name:        html.UnescapeString(section[m[6]:m[7]]),
			Href:        html.UnescapeString(section[m[4]:m[5]]),
			Ref:         m[2] >= 0,
			HasChildren: strings.Contains(rest, "<ul"),
		})
	}

	return entries
}

// CountEntries returns the number of entries and of reference entries.
func CountEntries(entries []DependencyEntry) (total, refs int) {
	for _, e := range entries {
		if e.Ref {
			refs++
		}
	}

	return len(entries), refs
}

// LayeredGraphBounded runs scenario "A layered graph renders each service
// once" against mount.
func LayeredGraphBounded(t *testing.T, mount Mount) {
	t.Helper()

	root := doger.New()
	graph, top := LayeredGraph(24)
	graph.Provide(t, root, top)
	do := mount(t, root)

	start := time.Now()
	resp := Get(t, do, ServicePath(root.ID(), top))
	elapsed := time.Since(start)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	total, refs := CountEntries(DependencyEntries(t, resp.Body))
	if total != 45 || refs != 22 {
		t.Errorf("dependency tree lists %d entries with %d references, want 45 with 22", total, refs)
	}
	if len(resp.Body) >= 65536 {
		t.Errorf("body is %d bytes, want under 65536", len(resp.Body))
	}
	if elapsed > time.Second {
		t.Errorf("response written in %s, want within 1s", elapsed)
	}
}
