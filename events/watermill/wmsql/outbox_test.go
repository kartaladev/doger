package dogerwmsql_test

import (
	"context"
	stdsql "database/sql"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ThreeDotsLabs/watermill"
	"github.com/ThreeDotsLabs/watermill-sql/v4/pkg/sql"
	"github.com/ThreeDotsLabs/watermill/message"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kartaladev/doger"
	"github.com/kartaladev/doger/events/sqltx"
	dogerwatermill "github.com/kartaladev/doger/events/watermill"
	dogerwmsql "github.com/kartaladev/doger/events/watermill/wmsql"
	"github.com/kartaladev/doger/eventtransporttest"
)

// outboxTable is the messages table of the default forwarder topic.
var outboxTable = dogerwmsql.PostgresSchema().MessagesTable("doger_outbox")

// newOutbox returns an Outbox over a test transport on db, not started.
func newOutbox(t *testing.T, db *stdsql.DB) *dogerwmsql.Outbox {
	t.Helper()
	outbox, err := dogerwmsql.NewOutbox(db, newTestTransport(t, postgres, db), dogerwmsql.OutboxConfig{Logger: watermill.NopLogger{}})
	require.NoError(t, err)

	return outbox
}

// startOutbox starts outbox until the test ends.
func startOutbox(t *testing.T, outbox *dogerwmsql.Outbox) {
	t.Helper()
	require.NoError(t, outbox.Start(context.Background()))
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		assert.NoError(t, outbox.Shutdown(ctx))
	})
}

// queryRower is a *sql.DB or a *sql.Tx.
type queryRower interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *stdsql.Row
}

// outboxRows counts the rows of the outbox table through q, 0 when the
// table does not exist.
func outboxRows(t *testing.T, q queryRower) int {
	t.Helper()
	var exists stdsql.NullString
	require.NoError(t, q.QueryRowContext(context.Background(), `SELECT to_regclass($1)::text`, outboxTable).Scan(&exists))
	if !exists.Valid {
		return 0
	}
	var n int
	require.NoError(t, q.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM `+outboxTable).Scan(&n))

	return n
}

// mailRoot returns a started root on a test transport over db with a
// remote listener "mail" (DeliverOnce) counting its runs in ran.
func mailRoot(t *testing.T, db *stdsql.DB, ran *atomic.Int64) {
	t.Helper()
	newRoot(t, postgres, db, "", func(root *doger.RootScope) {
		doger.Listen(root, func(context.Context, OrderPlaced) error { ran.Add(1); return nil },
			doger.ListenerID("mail"), doger.ListenerDelivery(doger.DeliverOnce))
	})
}

// expectRuns waits until ran reaches want, then checks it stays there.
func expectRuns(t *testing.T, ran *atomic.Int64, want int64) {
	t.Helper()
	if want > 0 {
		require.Eventually(t, func() bool { return ran.Load() >= want }, 5*time.Second, 10*time.Millisecond)
	}
	time.Sleep(quietPeriod)
	assert.Equal(t, want, ran.Load(), "mail runs")
}

func TestOutbox_Delivery(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		end     func(*sqltx.Tx) error
		wantRan int64
	}{
		{name: "commit", end: (*sqltx.Tx).Commit, wantRan: 1},
		{name: "rollback", end: (*sqltx.Tx).Rollback},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			testWithTimeout(t, time.Minute)
			db := openDB(t, postgres)
			var ran atomic.Int64
			mailRoot(t, db, &ran)
			outbox := newOutbox(t, db)
			startOutbox(t, outbox)
			a := doger.NewWithOpts(&doger.InjectorOpts{EventTransport: outbox})
			require.NoError(t, doger.RegisterEventType[OrderPlaced](a, "orders.placed", doger.EventExternalize(doger.ExternalizeOutbox)))
			require.NoError(t, a.Start())
			t.Cleanup(func() { a.Shutdown() })

			txCtx, tx, err := sqltx.BeginTx(context.Background(), db, nil)
			require.NoError(t, err)
			require.NoError(t, doger.Publish(txCtx, a, OrderPlaced{ID: "1"}))
			require.NoError(t, tt.end(tx))

			expectRuns(t, &ran, tt.wantRan)
		})
	}
}

func TestOutbox_SendTx(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		run  func(t *testing.T, db, otherDB *stdsql.DB, outbox *dogerwmsql.Outbox)
	}{
		{
			name: "bound_transaction",
			run: func(t *testing.T, db, _ *stdsql.DB, outbox *dogerwmsql.Outbox) {
				txCtx, tx, err := sqltx.BeginTx(context.Background(), db, nil)
				require.NoError(t, err)
				defer tx.Rollback()

				require.NoError(t, outbox.SendTx(txCtx, envelope()))

				assert.Equal(t, 1, outboxRows(t, tx.SQL()), "rows inside the transaction")
				assert.Equal(t, 0, outboxRows(t, db), "rows outside the transaction")
			},
		},
		{
			name: "in_tx",
			run: func(t *testing.T, db, _ *stdsql.DB, outbox *dogerwmsql.Outbox) {
				err := sqltx.InTx(context.Background(), db, func(ctx context.Context) error {
					return outbox.SendTx(ctx, envelope())
				})

				require.NoError(t, err)
				assert.Equal(t, 1, outboxRows(t, db), "committed rows")
			},
		},
		{
			name: "no_transaction",
			run: func(t *testing.T, db, _ *stdsql.DB, outbox *dogerwmsql.Outbox) {
				err := outbox.SendTx(context.Background(), envelope())

				require.ErrorIs(t, err, doger.ErrNoTransaction)
				assert.Equal(t, 0, outboxRows(t, db), "rows")
			},
		},
		{
			name: "foreign_transaction",
			run: func(t *testing.T, db, otherDB *stdsql.DB, outbox *dogerwmsql.Outbox) {
				txCtx, tx, err := sqltx.BeginTx(context.Background(), otherDB, nil)
				require.NoError(t, err)
				defer tx.Rollback()

				err = outbox.SendTx(txCtx, envelope())

				require.ErrorIs(t, err, sqltx.ErrForeignTransaction)
				assert.Equal(t, 0, outboxRows(t, db), "rows")
				assert.Equal(t, 0, outboxRows(t, tx.SQL()), "rows in the foreign transaction")
			},
		},
		{
			name: "committed_transaction",
			run: func(t *testing.T, db, _ *stdsql.DB, outbox *dogerwmsql.Outbox) {
				txCtx, tx, err := sqltx.BeginTx(context.Background(), db, nil)
				require.NoError(t, err)
				require.NoError(t, tx.Commit())

				require.ErrorIs(t, outbox.SendTx(txCtx, envelope()), doger.ErrNoTransaction)
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			testWithTimeout(t, time.Minute)
			db := openDB(t, postgres)
			otherDB := openDB(t, postgres)
			tt.run(t, db, otherDB, newOutbox(t, db))
		})
	}
}

// discardPublisher is a watermill publisher that drops every message.
type discardPublisher struct{}

func (discardPublisher) Publish(string, ...*message.Message) error { return nil }
func (discardPublisher) Close() error                              { return nil }

func TestNewOutbox(t *testing.T) {
	t.Parallel()

	db, err := stdsql.Open("pgx", "postgres://127.0.0.1:1/never")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	transport, err := dogerwatermill.New(discardPublisher{}, dogerwmsql.PostgresSubscriberFactory(db))
	require.NoError(t, err)

	tests := []struct {
		name      string
		db        *stdsql.DB
		transport *dogerwatermill.Transport
		cfg       dogerwmsql.OutboxConfig
		wantTopic string
		wantErr   bool
	}{
		{name: "defaults", db: db, transport: transport, wantTopic: "doger_outbox"},
		{name: "forwarder_topic", db: db, transport: transport, cfg: dogerwmsql.OutboxConfig{ForwarderTopic: "fwd"}, wantTopic: "fwd"},
		{name: "empty_forwarder_topic", db: db, transport: transport, cfg: dogerwmsql.OutboxConfig{ForwarderTopic: ""}, wantTopic: "doger_outbox"},
		{name: "default_logger", db: db, transport: transport, cfg: dogerwmsql.OutboxConfig{Logger: nil}, wantTopic: "doger_outbox"},
		{name: "nil_db", transport: transport, wantErr: true},
		{name: "nil_transport", db: db, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			outbox, err := dogerwmsql.NewOutbox(tt.db, tt.transport, tt.cfg)

			if tt.wantErr {
				require.ErrorIs(t, err, dogerwatermill.ErrInvalidConfig)
				assert.Nil(t, outbox)
				return
			}
			require.NoError(t, err)
			var _ doger.TxEventSender = outbox
			assert.Equal(t, sqltx.Source(), outbox.TxSource())
			assert.Equal(t, tt.wantTopic, dogerwmsql.ForwarderTopic(outbox))
			if tt.cfg.Logger == nil {
				assert.IsType(t, watermill.NewSlogLogger(nil), dogerwmsql.OutboxLogger(outbox), "default logger")
			}
		})
	}
}

func TestOutbox_Lifecycle(t *testing.T) {
	t.Parallel()

	publish := func(t *testing.T, db *stdsql.DB, root *doger.RootScope) {
		t.Helper()
		require.NoError(t, sqltx.InTx(context.Background(), db, func(ctx context.Context) error {
			return doger.Publish(ctx, root, OrderPlaced{ID: "1"})
		}))
	}
	tests := []struct {
		name    string
		run     func(t *testing.T, db *stdsql.DB, root *doger.RootScope, outbox *dogerwmsql.Outbox)
		wantRan int64
	}{
		{
			name: "publish_after_start",
			run: func(t *testing.T, db *stdsql.DB, root *doger.RootScope, outbox *dogerwmsql.Outbox) {
				require.NoError(t, root.Start())
				publish(t, db, root)
			},
			wantRan: 1,
		},
		{
			name: "committed_before_start",
			run: func(t *testing.T, db *stdsql.DB, root *doger.RootScope, outbox *dogerwmsql.Outbox) {
				publish(t, db, root)
				require.NoError(t, root.Start())
			},
			wantRan: 1,
		},
		{
			name: "committed_after_shutdown",
			run: func(t *testing.T, db *stdsql.DB, root *doger.RootScope, outbox *dogerwmsql.Outbox) {
				require.NoError(t, root.Start())
				require.NoError(t, root.Shutdown().Err())
				// the root is shut down: write through the outbox itself
				require.NoError(t, sqltx.InTx(context.Background(), db, func(ctx context.Context) error {
					return outbox.SendTx(ctx, envelope())
				}))
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			testWithTimeout(t, time.Minute)
			db := openDB(t, postgres)
			var ran atomic.Int64
			mailRoot(t, db, &ran)
			outbox := newOutbox(t, db)
			root := doger.New()
			doger.ProvideNamedValue[doger.EventTransport](root, doger.NameOf[doger.EventTransport](), outbox, doger.AsStarter())
			require.NoError(t, doger.RegisterEventType[OrderPlaced](root, "orders.placed", doger.EventExternalize(doger.ExternalizeOutbox)))
			t.Cleanup(func() { root.Shutdown() })

			tt.run(t, db, root, outbox)

			expectRuns(t, &ran, tt.wantRan)
		})
	}
}

func TestOutbox_ShutdownBeforeStart(t *testing.T) {
	t.Parallel()

	db, err := stdsql.Open("pgx", "postgres://127.0.0.1:1/never")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	transport, err := dogerwatermill.New(discardPublisher{}, dogerwmsql.PostgresSubscriberFactory(db))
	require.NoError(t, err)
	outbox, err := dogerwmsql.NewOutbox(db, transport, dogerwmsql.OutboxConfig{})
	require.NoError(t, err)

	assert.NoError(t, outbox.Shutdown(context.Background()))
}

// txHarness drives the conformance suite's transactions with sqltx.
type txHarness struct{ db *stdsql.DB }

func (h txHarness) Begin(ctx context.Context) (context.Context, error) {
	txCtx, _, err := sqltx.BeginTx(ctx, h.db, nil)
	return txCtx, err
}

func (h txHarness) Commit(ctx context.Context) error {
	tx, ok := sqltx.From(ctx)
	if !ok {
		return errors.New("no transaction")
	}
	return tx.Commit()
}

func (h txHarness) Rollback(ctx context.Context) error {
	tx, ok := sqltx.From(ctx)
	if !ok {
		return errors.New("no transaction")
	}
	return tx.Rollback()
}

// TestOutboxConformance is not parallel, as TestConformance.
func TestOutboxConformance(t *testing.T) {
	testWithTimeout(t, 5*time.Minute)
	db := openDB(t, postgres)

	eventtransporttest.Run(t, func(t *testing.T) doger.EventTransport {
		outbox := newOutbox(t, db)
		startOutbox(t, outbox)
		return outbox
	}, eventtransporttest.WithTx(txHarness{db: db}))
}

// The first SendTx creates the outbox table. With the caller's transaction
// holding the pool's only connection, it must not wait for another.
func TestOutbox_SendTxSingleConnection(t *testing.T) {
	t.Parallel()
	testWithTimeout(t, 2*time.Minute)
	db := openDB(t, postgres)
	db.SetMaxOpenConns(1)
	outbox := newOutbox(t, db) // not started: no table yet

	done := make(chan error, 1)
	go func() {
		done <- sqltx.InTx(context.Background(), db, func(ctx context.Context) error {
			return outbox.SendTx(ctx, envelope())
		})
	}()

	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(10 * time.Second):
		t.Fatal("SendTx waited for a second connection")
	}
	assert.Equal(t, 1, outboxRows(t, db), "committed rows")
}

func TestOutbox_SendTxConcurrentFirst(t *testing.T) {
	t.Parallel()
	testWithTimeout(t, 2*time.Minute)
	db := openDB(t, postgres)
	outbox := newOutbox(t, db) // not started: the transactions create the table

	errs := make(chan error, 8)
	for range cap(errs) {
		go func() {
			errs <- sqltx.InTx(context.Background(), db, func(ctx context.Context) error {
				return outbox.SendTx(ctx, envelope())
			})
		}()
	}
	for range cap(errs) {
		require.NoError(t, <-errs)
	}
	assert.Equal(t, cap(errs), outboxRows(t, db), "committed rows")
}

func TestOutbox_Adapters(t *testing.T) {
	t.Parallel()
	testWithTimeout(t, 2*time.Minute)
	ctx := context.Background()
	db := openDB(t, postgres)
	schema := dogerwmsql.PostgresSchema()
	schema.GenerateMessagesTableName = func(topic string) string { return `"custom_` + topic + `"` }
	offsets := sql.DefaultPostgreSQLOffsetsAdapter{
		GenerateMessagesOffsetsTableName: func(topic string) string { return `"custom_offsets_` + topic + `"` },
	}
	transport := newTestTransport(t, postgres, db)
	outbox, err := dogerwmsql.NewOutbox(db, transport, dogerwmsql.OutboxConfig{
		SchemaAdapter: schema, OffsetsAdapter: offsets, Logger: watermill.NopLogger{},
	})
	require.NoError(t, err)
	handled := make(chan struct{}, 1)
	stop, err := transport.Subscribe(ctx, doger.Subscription{Destination: topic, Group: "mail", Mode: doger.DeliverOnce},
		func(context.Context, doger.Envelope) error { handled <- struct{}{}; return nil })
	require.NoError(t, err)
	defer func() { assert.NoError(t, stop(ctx)) }()
	startOutbox(t, outbox)

	require.NoError(t, sqltx.InTx(ctx, db, func(ctx context.Context) error { return outbox.SendTx(ctx, envelope()) }))

	select {
	case <-handled:
	case <-time.After(10 * time.Second):
		t.Fatal("the envelope was not relayed")
	}
	for _, table := range []string{"custom_doger_outbox", "custom_offsets_doger_outbox"} {
		var exists stdsql.NullString
		require.NoError(t, db.QueryRowContext(ctx, `SELECT to_regclass($1)::text`, table).Scan(&exists))
		assert.True(t, exists.Valid, "table %s", table)
	}
	assert.Zero(t, outboxRows(t, db), "nothing in the default table")
}
