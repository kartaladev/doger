package dogerwmsql_test

import (
	"testing"
	"time"

	"github.com/kartaladev/doger"
	"github.com/kartaladev/doger/eventtransporttest"
)

func TestConformance(t *testing.T) {
	t.Parallel()

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
