package dogerwmsql_test

import (
	"context"
	stdsql "database/sql"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ThreeDotsLabs/watermill"
	"github.com/ThreeDotsLabs/watermill-sql/v4/pkg/sql"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kartaladev/doger"
	dogerwatermill "github.com/kartaladev/doger/events/watermill"
	dogerwmsql "github.com/kartaladev/doger/events/watermill/wmsql"
)

// OrderPlaced is the event of the integration tests.
type OrderPlaced struct{ ID string }

// newRoot returns a started root on a dogerwatermill transport over db,
// with OrderPlaced registered as externalized and setup run before Start;
// it shuts down when the test ends.
func newRoot(t *testing.T, e engine, db *stdsql.DB, instanceID string, setup func(*doger.RootScope)) *doger.RootScope {
	t.Helper()
	root := doger.NewWithOpts(&doger.InjectorOpts{EventTransport: newTestTransport(t, e, db), InstanceID: instanceID})
	require.NoError(t, doger.RegisterEventType[OrderPlaced](root, "orders.placed", doger.EventExternalized()))
	if setup != nil {
		setup(root)
	}
	require.NoError(t, root.Start())
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_ = root.ShutdownWithContext(ctx)
	})

	return root
}

func TestFactory_Groups(t *testing.T) {
	t.Parallel()

	for _, e := range engines {
		t.Run(e.name, func(t *testing.T) {
			t.Parallel()
			testWithTimeout(t, 2*time.Minute)
			db := openDB(t, e)

			var mail, cacheB1, cacheB2 atomic.Int64
			listeners := func(cache *atomic.Int64) func(*doger.RootScope) {
				return func(root *doger.RootScope) {
					doger.Listen(root, func(context.Context, OrderPlaced) error { mail.Add(1); return nil },
						doger.ListenerID("mail"), doger.ListenerDelivery(doger.DeliverOnce))
					doger.Listen(root, func(context.Context, OrderPlaced) error { cache.Add(1); return nil },
						doger.ListenerID("cache"), doger.ListenerDelivery(doger.DeliverAll))
				}
			}
			newRoot(t, e, db, "b1", listeners(&cacheB1))
			newRoot(t, e, db, "b2", listeners(&cacheB2))
			publisher := newRoot(t, e, db, "pub", nil)

			for range 20 {
				require.NoError(t, doger.Publish(context.Background(), publisher, OrderPlaced{ID: "1"}))
			}

			require.Eventually(t, func() bool {
				return mail.Load() >= 20 && cacheB1.Load() >= 20 && cacheB2.Load() >= 20
			}, 30*time.Second, 20*time.Millisecond)
			time.Sleep(quietPeriod)
			assert.Equal(t, int64(20), mail.Load(), "mail runs across b1 and b2")
			assert.Equal(t, int64(20), cacheB1.Load(), "cache runs on b1")
			assert.Equal(t, int64(20), cacheB2.Load(), "cache runs on b2")
		})
	}
}

func TestFactory_NilDB(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		factory func() dogerwatermill.SubscriberFactory
	}{
		{name: "postgres", factory: func() dogerwatermill.SubscriberFactory { return dogerwmsql.PostgresSubscriberFactory(nil) }},
		{name: "mysql", factory: func() dogerwatermill.SubscriberFactory { return dogerwmsql.MySQLSubscriberFactory(nil) }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.PanicsWithValue(t, "doger: dogerwmsql: nil *sql.DB", func() { tt.factory() })
		})
	}
}

// hiddenTables is a schema adapter that does not tell its table names.
type hiddenTables struct{ sql.SchemaAdapter }

// A schema adapter that does not tell its messages table cannot seed new
// groups: the factory must not guess the table.
func TestFactory_SchemaWithoutTableName(t *testing.T) {
	t.Parallel()
	testWithTimeout(t, time.Minute)
	db := openDB(t, postgres)
	schema := dogerwmsql.PostgresSchema()
	schema.GenerateMessagesTableName = func(topic string) string { return `"app_events_` + topic + `"` }
	factory := dogerwmsql.PostgresSubscriberFactory(db, fast(), dogerwmsql.WithLogger(watermill.NopLogger{}),
		dogerwmsql.WithSubscriberConfig(func(c *sql.SubscriberConfig) { c.SchemaAdapter = hiddenTables{schema} }))

	sub, err := factory(context.Background(), doger.Subscription{Destination: topic, Group: "mail", Mode: doger.DeliverOnce})

	if sub != nil {
		_ = sub.Close()
	}
	require.ErrorIs(t, err, dogerwatermill.ErrInvalidConfig)
}
