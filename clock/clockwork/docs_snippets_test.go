package dogerclockwork_test

import (
	"github.com/jonboulle/clockwork"

	"github.com/kartaladev/doger"
	dogerclockwork "github.com/kartaladev/doger/clock/clockwork"
)

// docsClock is the clock snippet of the root module's migration guide,
// compiled: the text between the docs markers is the guide's block (see
// TestDocs_MigrationSnippetsCompiled in the root module).
func docsClock() *doger.RootScope {
	// docs:clock
	fake := clockwork.NewFakeClock()
	injector := doger.NewWithOpts(&doger.InjectorOpts{
		Clock: dogerclockwork.Wrap(fake), // was Clock: fake
	})
	// docs:end
	return injector
}

var _ = docsClock
