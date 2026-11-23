package eventsql_test

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	eventsql "github.com/kartaladev/doger/events/sql"
)

// TestRetention checks what an acknowledgement leaves: nothing by default,
// and with KeepDelivered the delivered row and its event, which the
// maintenance deletes once the row is older than KeepDelivered and not
// before. It waits for what it checks instead of sleeping, so a slow
// machine cannot fail it.
func TestRetention(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		keep time.Duration // KeepDelivered; 0: the default
	}{
		{name: "default"},
		{name: "keep_1h", keep: time.Hour},
		{name: "keep_1s", keep: time.Second},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			forEachEngine(t, func(t *testing.T, db *testDB) {
				testWithBackstop(t)
				const dest = "orders.placed"
				opts := []eventsql.Option{eventsql.PollInterval(50 * time.Millisecond)}
				if tt.keep > 0 {
					opts = append(opts, eventsql.KeepDelivered(tt.keep))
				}
				tr := schemaTransport(t, db, opts...)
				c := newCollector()
				subscribe(t, tr, group(dest, "mail"), c.handle)
				send(t, tr, "e1", dest)
				c.expect(t, 1)
				events := func() int { return db.Count(t, "SELECT COUNT(*) FROM doger_events") }
				deliveries := func() int { return db.Count(t, "SELECT COUNT(*) FROM doger_event_deliveries") }

				if tt.keep == 0 {
					// the acknowledgement deletes the delivery and its event
					waitFor(t, func() bool { return deliveries() == 0 && events() == 0 }, "the acknowledgement did not delete e1")
					return
				}
				if tt.keep == time.Hour {
					const delivered = "SELECT COUNT(*) FROM doger_event_deliveries WHERE state = 'delivered'"
					waitFor(t, func() bool { return db.Count(t, delivered) == 1 }, "the acknowledgement did not mark e1 delivered")
					// a whole maintenance step ran on the delivered row
					maintenanceStep(t, db)
					assert.Equal(t, 1, events())
					assert.Equal(t, 1, db.Count(t, delivered))
					assert.Equal(t, 1, deliveries())
					return
				}

				// each look reads the row and the database time together:
				// the row is gone only once older than KeepDelivered. A
				// look that never saw it delivered (marked and deleted
				// between two looks) has no time to check.
				var deliveredAt int64 // 0: not seen delivered
				waitFor(t, func() bool {
					var n int
					var at sql.NullInt64
					var now int64
					require.NoError(t, db.DB.QueryRowContext(context.Background(),
						"SELECT COUNT(*), MAX("+db.epochMicros("delivered_at")+"), "+db.epochMicros(db.Now())+" FROM doger_event_deliveries").Scan(&n, &at, &now))
					if n > 0 {
						if at.Valid {
							deliveredAt = at.Int64
						}
						return false
					}
					if deliveredAt != 0 {
						assert.GreaterOrEqual(t, time.Duration(now-deliveredAt)*time.Microsecond, tt.keep, "the delivered row was deleted before KeepDelivered")
					}
					return true
				}, "the maintenance did not delete the delivered row")
				waitFor(t, func() bool { return events() == 0 }, "the maintenance did not delete the event")
			})
		})
	}
}

// epochMicros returns the SQL of the microseconds since the Unix epoch of
// the timestamp expression expr, a database time in UTC.
func (db *testDB) epochMicros(expr string) string {
	if db.kind == "mysql" {
		return "TIMESTAMPDIFF(MICROSECOND, '1970-01-01 00:00:00', " + expr + ")"
	}
	return "CAST(EXTRACT(EPOCH FROM " + expr + ") * 1000000 AS BIGINT)"
}

// maintenanceStep waits for a whole maintenance step of db's subscribed
// transport that started after the call. A step deletes orphan events
// last, listing them first: the step deleting a second orphan event,
// inserted once the first was gone, is a later one than the step that
// deleted the first, after the call.
func maintenanceStep(t *testing.T, db *testDB) {
	t.Helper()
	for range 2 {
		var raw [8]byte
		_, _ = rand.Read(raw[:])
		key := "orphan" + hex.EncodeToString(raw[:])
		db.Exec(t, "INSERT INTO doger_events (event_key, id, destination, event_type, version, content_type, payload, headers, published_at, created_at) VALUES (?, ?, 'orphan.destination', 'orphan', 1, 'text/plain', ?, '{}', 0, "+db.Now()+")",
			key, key, []byte("x"))
		waitFor(t, func() bool {
			return db.Count(t, "SELECT COUNT(*) FROM doger_events WHERE event_key = ?", key) == 0
		}, "no maintenance step deleted an orphan event")
	}
}

// staleGroupWarning is the message of the stale group warning.
const staleGroupWarning = "doger: eventsql: a group has pending deliveries older than a day; drop it with DropGroup if its listener was removed" //nolint:gosec // G101: a log message, not a credential

// TestRetention_StaleGroupWarning is a regression test: the table-wide
// maintenance ran once per subscription, so a transport with two
// subscriptions warned twice about the same stale group. It runs once
// per transport.
func TestRetention_StaleGroupWarning(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		subscriptions int
	}{
		{name: "one_subscription", subscriptions: 1},
		{name: "three_subscriptions", subscriptions: 3},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			forEachEngine(t, func(t *testing.T, db *testDB) {
				testWithBackstop(t)
				logs := &logRecorder{}
				tr := schemaTransport(t, db, fast(eventsql.Logger(slog.New(logs)))...)
				// "removed" lost its listener two days ago
				seedGroup(t, db, "orders.placed", "removed", false)
				send(t, tr, "e1", "orders.placed")
				db.Exec(t, "UPDATE doger_event_deliveries SET created_at = "+db.Now()+" - "+db.Interval(48*time.Hour))

				for i := range tt.subscriptions {
					subscribe(t, tr, group("other.destination", fmt.Sprintf("g%d", i)), newCollector().handle)
				}

				waitFor(t, func() bool { return logs.Count(staleGroupWarning) > 0 }, "the stale group was not warned about")
				time.Sleep(time.Second) // several maintenance steps
				assert.Equal(t, 1, logs.Count(staleGroupWarning))
			})
		})
	}
}
