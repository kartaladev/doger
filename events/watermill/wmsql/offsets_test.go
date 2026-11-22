package dogerwmsql_test

import (
	"context"
	stdsql "database/sql"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ThreeDotsLabs/watermill-sql/v4/pkg/sql"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kartaladev/doger"
)

const topic = "orders.placed"

// quietPeriod is how long a test watches for a message that must not come.
const quietPeriod = 500 * time.Millisecond

// consumer counts and acks the messages of one factory subscriber.
type consumer struct {
	handled atomic.Int64
	done    chan struct{}
	close   func()
}

// consume subscribes a subscriber of factory for group to topic and acks
// what it receives until the test ends.
func consume(t *testing.T, e engine, db *stdsql.DB, group string) *consumer {
	t.Helper()
	factory := e.factory(db, fast())
	sub, err := factory(context.Background(), doger.Subscription{Destination: topic, Group: group, Mode: doger.DeliverOnce})
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	messages, err := sub.Subscribe(ctx, topic)
	if err != nil {
		cancel()
		_ = sub.Close()
		require.NoError(t, err)
	}
	c := &consumer{done: make(chan struct{})}
	go func() {
		defer close(c.done)
		for msg := range messages {
			c.handled.Add(1)
			msg.Ack()
		}
	}()
	var once sync.Once
	c.close = func() {
		once.Do(func() {
			cancel()
			_ = sub.Close()
			<-c.done
		})
	}
	t.Cleanup(c.close)

	return c
}

// storeOffset makes group acknowledge the first n messages of topic.
func storeOffset(t *testing.T, db *stdsql.DB, group string, n int) {
	t.Helper()
	ctx := context.Background()
	offsets := sql.DefaultPostgreSQLOffsetsAdapter{}
	queries, err := offsets.SchemaInitializingQueries(sql.OffsetsSchemaInitializingQueriesParams{Topic: topic})
	require.NoError(t, err)
	for _, q := range queries {
		_, err := db.ExecContext(ctx, q.Query, q.Args...)
		require.NoError(t, err)
	}
	var (
		offset int64
		txID   string
	)
	err = db.QueryRowContext(ctx, `SELECT "offset", transaction_id::text FROM `+sql.DefaultPostgreSQLSchema{}.MessagesTable(topic)+
		` ORDER BY transaction_id, "offset" LIMIT 1 OFFSET $1`, n-1).Scan(&offset, &txID)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `INSERT INTO `+offsets.MessagesOffsetsTable(topic)+ //nolint:gosec // G202: a constant topic
		` (consumer_group, offset_acked, last_processed_transaction_id) VALUES ($1, $2, $3::xid8)`, group, offset, txID)
	require.NoError(t, err)
}

func TestOffsets_Start(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		engine   engine
		existing int // messages in the topic before the subscription
		stored   int // messages a stored offset of "mail" acknowledges
		handled  int64
	}{
		{name: "postgres/new_group", engine: postgres, existing: 5, handled: 1},
		{name: "mysql/new_group", engine: mysql, existing: 5, handled: 1},
		{name: "postgres/stored_offset", engine: postgres, existing: 5, stored: 3, handled: 3},
		{name: "postgres/empty_topic", engine: postgres, handled: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			testWithTimeout(t, time.Minute)
			db := openDB(t, tt.engine)
			pub := newPublisher(t, tt.engine, db)
			publishN(t, pub, topic, tt.existing)
			if tt.stored > 0 {
				storeOffset(t, db, "mail", tt.stored)
			}

			c := consume(t, tt.engine, db, "mail")
			publishN(t, pub, topic, 1)

			require.Eventually(t, func() bool { return c.handled.Load() >= tt.handled }, 10*time.Second, 10*time.Millisecond)
			time.Sleep(quietPeriod)
			assert.Equal(t, tt.handled, c.handled.Load(), "handled messages")
		})
	}
}

func TestOffsets_ConcurrentSeed(t *testing.T) {
	t.Parallel()
	testWithTimeout(t, time.Minute)
	db := openDB(t, postgres)
	pub := newPublisher(t, postgres, db)
	publishN(t, pub, topic, 5)

	consumers := make([]*consumer, 4)
	var wg sync.WaitGroup
	for i := range consumers {
		wg.Go(func() { consumers[i] = consume(t, postgres, db, "mail") })
	}
	wg.Wait()
	publishN(t, pub, topic, 1)

	total := func() (n int64) {
		for _, c := range consumers {
			n += c.handled.Load()
		}
		return n
	}
	require.Eventually(t, func() bool { return total() >= 1 }, 10*time.Second, 10*time.Millisecond)
	time.Sleep(quietPeriod)
	handledBy := 0
	for _, c := range consumers {
		if c.handled.Load() > 0 {
			handledBy++
			assert.Equal(t, int64(1), c.handled.Load(), "messages of the handling subscriber")
		}
	}
	assert.Equal(t, 1, handledBy, "subscribers that handled a message")
}
