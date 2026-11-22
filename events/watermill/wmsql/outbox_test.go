package dogerwmsql_test

import (
	"context"
	stdsql "database/sql"
	"errors"
	"net/url"
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
		{name: "mysql_schema", db: db, transport: transport, cfg: dogerwmsql.OutboxConfig{SchemaAdapter: dogerwmsql.MySQLSchema()}, wantErr: true},
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
		require.FailNow(t, "SendTx waited for a second connection")
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
		require.FailNow(t, "the envelope was not relayed")
	}
	for _, table := range []string{"custom_doger_outbox", "custom_offsets_doger_outbox"} {
		var exists stdsql.NullString
		require.NoError(t, db.QueryRowContext(ctx, `SELECT to_regclass($1)::text`, table).Scan(&exists))
		assert.True(t, exists.Valid, "table %s", table)
	}
	assert.Zero(t, outboxRows(t, db), "nothing in the default table")
}

// newTopicEnvelope returns an envelope for a topic without a table yet: the
// transport's watermill-sql publisher creates it under watermill-sql's
// shared advisory lock.
func newTopicEnvelope() doger.Envelope {
	env := envelope()
	env.Destination = "orders.shipped"

	return env
}

// returnsWithin fails t unless fn returns within d.
func returnsWithin(t *testing.T, d time.Duration, what string, fn func() error) {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- fn() }()
	select {
	case err := <-done:
		require.NoError(t, err, what)
	case <-time.After(d):
		require.FailNow(t, what+" did not return", "within %v", d)
	}
}

// The outbox table must not be created under watermill-sql's shared
// schema lock in the caller's transaction: other watermill-sql schema
// setup, such as the transport's publisher on a new topic, would wait for
// that transaction.
func TestOutbox_SchemaLock(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		run  func(t *testing.T, db *stdsql.DB, outbox *dogerwmsql.Outbox)
	}{
		{
			name: "send_after_send_tx_in_one_transaction",
			run: func(t *testing.T, db *stdsql.DB, outbox *dogerwmsql.Outbox) {
				returnsWithin(t, 10*time.Second, "the transaction", func() error {
					return sqltx.InTx(context.Background(), db, func(ctx context.Context) error {
						if err := outbox.SendTx(ctx, envelope()); err != nil {
							return err
						}
						return outbox.Send(ctx, newTopicEnvelope())
					})
				})
			},
		},
		{
			name: "bound_transaction_committed_directly",
			run: func(t *testing.T, db *stdsql.DB, outbox *dogerwmsql.Outbox) {
				ctx := context.Background()
				first, err := db.BeginTx(ctx, nil)
				require.NoError(t, err)
				require.NoError(t, outbox.SendTx(sqltxBind(ctx, db, first), envelope()))
				require.NoError(t, first.Commit()) // not through sqltx: no OnCommit
				second, err := db.BeginTx(ctx, nil)
				require.NoError(t, err)
				defer second.Rollback()
				require.NoError(t, outbox.SendTx(sqltxBind(ctx, db, second), envelope()))

				returnsWithin(t, 10*time.Second, "Send to a new topic", func() error {
					return outbox.Send(ctx, newTopicEnvelope())
				})
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			testWithTimeout(t, time.Minute)
			db := openDB(t, postgres)
			tt.run(t, db, newOutbox(t, db))
		})
	}
}

// sqltxBind binds tx to ctx with sqltx.BindTx.
func sqltxBind(ctx context.Context, db *stdsql.DB, tx *stdsql.Tx) context.Context {
	ctx, _ = sqltx.BindTx(ctx, db, tx)
	return ctx
}

// restrictedDB returns db's schema opened as a new role allowed to read
// and write its tables but not to create any, dropped when the test ends.
func restrictedDB(t *testing.T, db *stdsql.DB) *stdsql.DB {
	t.Helper()
	ctx := context.Background()
	var schema string
	require.NoError(t, db.QueryRowContext(ctx, `SELECT current_schema()`).Scan(&schema))
	role := "r" + schema // schema names are random
	for _, q := range []string{
		`CREATE ROLE ` + role + ` LOGIN PASSWORD 'doger'`,
		`GRANT USAGE ON SCHEMA ` + schema + ` TO ` + role,
		`GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA ` + schema + ` TO ` + role,
		`GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA ` + schema + ` TO ` + role,
	} {
		_, err := db.ExecContext(ctx, q)
		require.NoError(t, err, q)
	}
	u, err := url.Parse(scopedDSN(t, postgres, requireDSN(t, envPostgresDSN), schema))
	require.NoError(t, err)
	u.User = url.UserPassword(role, "doger")
	roleDB, err := stdsql.Open("pgx", u.String())
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = roleDB.Close()
		_, _ = db.ExecContext(context.Background(), `DROP OWNED BY `+role)
		_, _ = db.ExecContext(context.Background(), `DROP ROLE `+role)
	})

	return roleDB
}

// A role that may not create tables uses the tables a migration (here, the
// owner's Start) created.
func TestOutbox_RestrictedRole(t *testing.T) {
	t.Parallel()
	testWithTimeout(t, time.Minute)
	ctx := context.Background()
	db := openDB(t, postgres)
	owner := newOutbox(t, db)
	require.NoError(t, owner.Start(ctx))
	require.NoError(t, owner.Shutdown(ctx))
	roleDB := restrictedDB(t, db)
	outbox, err := dogerwmsql.NewOutbox(roleDB, newTestTransport(t, postgres, roleDB), dogerwmsql.OutboxConfig{Logger: watermill.NopLogger{}})
	require.NoError(t, err)

	require.NoError(t, outbox.Start(ctx))
	defer func() { assert.NoError(t, outbox.Shutdown(ctx)) }()
	require.NoError(t, sqltx.InTx(ctx, roleDB, func(ctx context.Context) error { return outbox.SendTx(ctx, envelope()) }))
}

// holdSchemaLocks holds, until the test ends, the advisory locks
// watermill-sql and the outbox create their tables under.
func holdSchemaLocks(t *testing.T, db *stdsql.DB) {
	t.Helper()
	ctx := context.Background()
	tx, err := db.BeginTx(ctx, nil)
	require.NoError(t, err)
	for _, app := range []string{"watermill", "doger_outbox"} {
		_, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock($1)`, sql.DefaultSchemaInitializationLock(app))
		require.NoError(t, err)
	}
	t.Cleanup(func() { _ = tx.Rollback() })
}

// TestOutbox_StartHonorsContext is not parallel: advisory locks are shared
// by the whole database, so holding them would stall the parallel tests
// that create tables.
func TestOutbox_StartHonorsContext(t *testing.T) {
	testWithTimeout(t, time.Minute)
	db := openDB(t, postgres)
	outbox := newOutbox(t, db)
	holdSchemaLocks(t, db) // the tables cannot be created

	startCtx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	started := make(chan error, 1)
	go func() { started <- outbox.Start(startCtx) }()
	time.Sleep(50 * time.Millisecond) // Start is creating the tables
	shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancelShutdown()
	shut := make(chan error, 1)
	go func() { shut <- outbox.Shutdown(shutdownCtx) }()

	for name, ch := range map[string]chan error{"Start": started, "Shutdown": shut} {
		select {
		case err := <-ch:
			if name == "Start" {
				assert.ErrorIs(t, err, context.DeadlineExceeded, "Start")
			}
		case <-time.After(3 * time.Second):
			require.FailNow(t, name+" ignored its context")
		}
	}
}

// Start cut short by its context leaves no forwarder running.
func TestOutbox_StartCancelled(t *testing.T) {
	t.Parallel()
	testWithTimeout(t, 2*time.Minute)
	ctx := context.Background()
	db := openDB(t, postgres)
	owner := newOutbox(t, db) // the tables exist, so Start reaches the forwarder
	require.NoError(t, owner.Start(ctx))
	require.NoError(t, owner.Shutdown(ctx))
	transport := newTestTransport(t, postgres, db)
	handled := make(chan struct{}, 64)
	stop, err := transport.Subscribe(ctx, doger.Subscription{Destination: topic, Group: "mail", Mode: doger.DeliverOnce},
		func(context.Context, doger.Envelope) error { handled <- struct{}{}; return nil })
	require.NoError(t, err)
	defer func() { assert.NoError(t, stop(ctx)) }()

	for i := range 20 {
		outbox, err := dogerwmsql.NewOutbox(db, transport, dogerwmsql.OutboxConfig{Logger: watermill.NopLogger{}})
		require.NoError(t, err)
		startCtx, cancel := context.WithTimeout(ctx, time.Duration(i)*time.Millisecond)
		if outbox.Start(startCtx) == nil {
			require.NoError(t, outbox.Shutdown(ctx))
		}
		cancel()
	}
	require.NoError(t, sqltx.InTx(ctx, db, func(ctx context.Context) error { return owner.SendTx(ctx, envelope()) }))

	select {
	case <-handled:
		require.FailNow(t, "a forwarder of a cancelled Start relayed the message")
	case <-time.After(2 * time.Second):
	}
}

func TestOutbox_SkipSchemaInitialization(t *testing.T) {
	t.Parallel()
	testWithTimeout(t, time.Minute)
	ctx := context.Background()
	db := openDB(t, postgres)
	outbox, err := dogerwmsql.NewOutbox(db, newTestTransport(t, postgres, db), dogerwmsql.OutboxConfig{
		SkipSchemaInitialization: true, Logger: watermill.NopLogger{},
	})
	require.NoError(t, err)

	err = sqltx.InTx(ctx, db, func(ctx context.Context) error { return outbox.SendTx(ctx, envelope()) })

	require.ErrorContains(t, err, "does not exist", "SendTx created no table")
	assert.ErrorContains(t, outbox.Start(ctx), "does not exist", "Start created no table")
	assert.Equal(t, 0, outboxRows(t, db), "no outbox table")
}
