package dogerwmsql_test

import (
	"context"
	stdsql "database/sql"
	"testing"
	"time"

	"github.com/ThreeDotsLabs/watermill"
	"github.com/ThreeDotsLabs/watermill-sql/v4/pkg/sql"
	"github.com/ThreeDotsLabs/watermill/message"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kartaladev/doger"
	dogerwatermill "github.com/kartaladev/doger/events/watermill"
	dogerwmsql "github.com/kartaladev/doger/events/watermill/wmsql"
)

// groupStored reports whether the offsets table of topic holds a row for
// group.
func groupStored(t *testing.T, db *stdsql.DB, group string) bool {
	t.Helper()
	var n int
	err := db.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM `+
		sql.DefaultPostgreSQLOffsetsAdapter{}.MessagesOffsetsTable(topic)+` WHERE consumer_group = $1`, group).Scan(&n)
	require.NoError(t, err)

	return n > 0
}

// envelope returns an envelope for topic.
func envelope() doger.Envelope {
	return doger.Envelope{
		ID: watermill.NewUUID(), Destination: topic, EventType: topic, ContentType: "application/json",
		Version: 1, Payload: []byte(`{"ID":"1"}`), PublishedAt: time.Now().UTC(),
	}
}

// subscribeOne subscribes a transport over db with factory options opts to
// topic as sub, sends one envelope and waits until it was handled. It
// returns the stop function.
func subscribeOne(t *testing.T, db *stdsql.DB, sub doger.Subscription, opts ...dogerwmsql.Option) func(context.Context) error {
	t.Helper()
	transport := newTestTransport(t, postgres, db, opts...)
	handled := make(chan struct{}, 1)
	stop, err := transport.Subscribe(context.Background(), sub, func(context.Context, doger.Envelope) error {
		handled <- struct{}{}
		return nil
	})
	require.NoError(t, err)
	require.NoError(t, transport.Send(context.Background(), envelope()))
	select {
	case <-handled:
	case <-time.After(10 * time.Second):
		t.Fatal("the envelope was not handled")
	}

	return stop
}

func TestFactory_EphemeralCleanup(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		opts      []dogerwmsql.Option
		group     string
		ephemeral bool
		kept      bool
	}{
		{name: "ephemeral_deleted", group: "cache/7f3a", ephemeral: true},
		{name: "ephemeral_kept_on_request", opts: []dogerwmsql.Option{dogerwmsql.WithKeepEphemeralGroups()}, group: "cache/7f3a", ephemeral: true, kept: true},
		{name: "durable_kept", group: "mail", kept: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			testWithTimeout(t, time.Minute)
			db := openDB(t, postgres)
			mode := doger.DeliverOnce
			if tt.ephemeral {
				mode = doger.DeliverAll
			}
			sub := doger.Subscription{Destination: topic, Group: tt.group, Mode: mode, Ephemeral: tt.ephemeral}
			stop := subscribeOne(t, db, sub, tt.opts...)

			require.NoError(t, stop(context.Background()))

			assert.Equal(t, tt.kept, groupStored(t, db, tt.group), "stored offset of %s", tt.group)
		})
	}
}

func TestFactory_CleanupError(t *testing.T) {
	t.Parallel()
	testWithTimeout(t, time.Minute)
	db := openDB(t, postgres)
	// keep the subscriber the factory builds, to check it was closed
	var built message.Subscriber
	factory := dogerwmsql.PostgresSubscriberFactory(db, fast(), dogerwmsql.WithLogger(watermill.NopLogger{}))
	transport, err := dogerwatermill.New(newPublisher(t, postgres, db),
		func(ctx context.Context, sub doger.Subscription) (message.Subscriber, error) {
			s, err := factory(ctx, sub)
			built = s
			return s, err
		}, dogerwatermill.WithLogger(watermill.NopLogger{}))
	require.NoError(t, err)
	stop, err := transport.Subscribe(context.Background(),
		doger.Subscription{Destination: topic, Group: "cache/7f3a", Mode: doger.DeliverAll, Ephemeral: true},
		func(context.Context, doger.Envelope) error { return nil })
	require.NoError(t, err)

	require.NoError(t, db.Close())
	err = stop(context.Background())

	assert.ErrorContains(t, err, "database is closed")
	_, err = built.Subscribe(context.Background(), topic)
	assert.ErrorIs(t, err, sql.ErrSubscriberClosed, "the subscriber was closed")
}
