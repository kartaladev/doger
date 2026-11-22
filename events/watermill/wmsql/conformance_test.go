package dogerwmsql_test

import (
	"testing"
	"time"

	"github.com/kartaladev/doger"
	"github.com/kartaladev/doger/eventtransporttest"
)

// The conformance tests are not parallel, so they run before the parallel
// tests and see a quiet server: on PostgreSQL a new group starts at the
// readable head, which a transaction another test holds open keeps behind
// the messages the suite sent before subscribing.
func TestConformance(t *testing.T) {
	for _, e := range engines {
		t.Run(e.name, func(t *testing.T) {
			t.Parallel()
			testWithTimeout(t, 5*time.Minute)
			db := openDB(t, e)
			eventtransporttest.Run(t, func(t *testing.T) doger.EventTransport {
				return newTestTransport(t, e, db)
			})
		})
	}
}
