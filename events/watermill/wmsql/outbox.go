package dogerwmsql

import (
	"context"
	stdsql "database/sql"
	"errors"
	"fmt"
	"sync/atomic"

	"github.com/ThreeDotsLabs/watermill"
	"github.com/ThreeDotsLabs/watermill-sql/v4/pkg/sql"
	"github.com/ThreeDotsLabs/watermill/components/forwarder"

	"github.com/kartaladev/doger"
	"github.com/kartaladev/doger/events/sqltx"
	dogerwatermill "github.com/kartaladev/doger/events/watermill"
)

// DefaultForwarderTopic is the default topic of the outbox table.
const DefaultForwarderTopic = "doger_outbox"

// OutboxConfig configures [NewOutbox].
type OutboxConfig struct {
	// ForwarderTopic is the watermill-sql topic (and so the table) the
	// outbox writes to and the forwarder relays from. Default
	// "doger_outbox".
	ForwarderTopic string
	// Logger logs the forwarder's failures. Default
	// watermill.NewSlogLogger(nil), the slog default logger.
	Logger watermill.LoggerAdapter
	// SchemaAdapter shapes the outbox table: its name, payload column and
	// queries. Default [PostgresSchema](). It must be a PostgreSQL one:
	// NewOutbox rejects watermill-sql's MySQL adapters, since MySQL
	// commits the caller's transaction on CREATE TABLE. A
	// sql.DefaultPostgreSQLSchema without its own lock creates the tables
	// under an advisory lock of the outbox's own,
	// sql.DefaultSchemaInitializationLock("doger_outbox"), not the one
	// other watermill-sql schema setup shares.
	SchemaAdapter sql.SchemaAdapter
	// OffsetsAdapter shapes the forwarder's offsets table. Default
	// watermill-sql's sql.DefaultPostgreSQLOffsetsAdapter{}, whose new
	// group starts at the oldest message, so the forwarder relays what was
	// committed before it first ran.
	OffsetsAdapter sql.OffsetsAdapter
	// SkipSchemaInitialization makes the outbox never create its tables,
	// for a migration tool that owns them. By default the outbox creates
	// those missing: Start both, and SendTx the outbox table in the
	// caller's transaction until a transaction that did committed. Tables
	// that exist are never created again, so a role that may not create
	// tables works either way once they exist.
	SkipSchemaInitialization bool
}

// outboxSchemaLock is the advisory lock the outbox's default schema
// adapter creates its tables under.
var outboxSchemaLock = sql.DefaultSchemaInitializationLock("doger_outbox")

// Outbox is a transactional outbox on PostgreSQL in front of a
// dogerwatermill transport: a doger.TxEventSender whose SendTx writes the
// event in the caller's transaction, and whose forwarder relays committed
// events to the transport's publisher. Send and Subscribe delegate to the
// transport. Register it as the container's transport service, named
// doger.NameOf[doger.EventTransport]() and with doger.AsStarter() (the
// EventTransport interface has no Start method), so that the container
// starts and stops the forwarder; with InjectorOpts.EventTransport, call
// Start and Shutdown yourself.
type Outbox struct {
	db        *stdsql.DB
	transport *dogerwatermill.Transport
	topic     string
	logger    watermill.LoggerAdapter
	schema    sql.SchemaAdapter
	offsets   sql.OffsetsAdapter
	skipInit  bool

	tableReady atomic.Bool // the outbox table exists

	lock      chan struct{} // held by Start and Shutdown; a channel, so waiting honors a context
	forwarder *forwarder.Forwarder
	cancelRun context.CancelFunc // cancels the forwarder's Run context
	done      chan struct{}      // closed when the forwarder's Run returned
}

var (
	_ doger.TxEventSender                 = (*Outbox)(nil)
	_ doger.StarterWithContextAndError    = (*Outbox)(nil)
	_ doger.ShutdownerWithContextAndError = (*Outbox)(nil)
)

// NewOutbox returns an outbox writing to db in front of transport. It
// starts nothing and touches no table until SendTx or Start. A nil db or
// transport, or a MySQL schema or offsets adapter, returns an error
// matching dogerwatermill.ErrInvalidConfig.
func NewOutbox(db *stdsql.DB, transport *dogerwatermill.Transport, cfg OutboxConfig) (*Outbox, error) {
	if db == nil {
		return nil, fmt.Errorf("%w: nil *sql.DB", dogerwatermill.ErrInvalidConfig)
	}
	if transport == nil {
		return nil, fmt.Errorf("%w: nil transport", dogerwatermill.ErrInvalidConfig)
	}
	if cfg.ForwarderTopic == "" {
		cfg.ForwarderTopic = DefaultForwarderTopic
	}
	if cfg.Logger == nil {
		cfg.Logger = watermill.NewSlogLogger(nil)
	}
	if cfg.SchemaAdapter == nil {
		cfg.SchemaAdapter = PostgresSchema()
	}
	if cfg.OffsetsAdapter == nil {
		cfg.OffsetsAdapter = sql.DefaultPostgreSQLOffsetsAdapter{}
	}
	switch cfg.SchemaAdapter.(type) {
	case sql.DefaultMySQLSchema, *sql.DefaultMySQLSchema:
		return nil, fmt.Errorf("%w: the outbox needs PostgreSQL: MySQL commits the caller's transaction on CREATE TABLE", dogerwatermill.ErrInvalidConfig)
	}
	switch cfg.OffsetsAdapter.(type) {
	case sql.DefaultMySQLOffsetsAdapter, *sql.DefaultMySQLOffsetsAdapter:
		return nil, fmt.Errorf("%w: the outbox needs PostgreSQL offsets", dogerwatermill.ErrInvalidConfig)
	}
	if pg, ok := cfg.SchemaAdapter.(sql.DefaultPostgreSQLSchema); ok && pg.InitializeSchemaLock == 0 {
		pg.InitializeSchemaLock = outboxSchemaLock
		cfg.SchemaAdapter = pg
	}

	return &Outbox{
		db: db, transport: transport, topic: cfg.ForwarderTopic, logger: cfg.Logger,
		schema: cfg.SchemaAdapter, offsets: cfg.OffsetsAdapter, skipInit: cfg.SkipSchemaInitialization,
		lock: make(chan struct{}, 1),
	}, nil
}

// Send sends env through the transport, outside any transaction.
func (o *Outbox) Send(ctx context.Context, env doger.Envelope) error {
	return o.transport.Send(ctx, env)
}

// Subscribe subscribes through the transport.
func (o *Outbox) Subscribe(ctx context.Context, sub doger.Subscription, h doger.EnvelopeHandler) (func(context.Context) error, error) {
	return o.transport.Subscribe(ctx, sub, h)
}

// TxSource returns sqltx.Source(): open transactions with sqltx.BeginTx or
// sqltx.InTx, or bind your own *sql.Tx with sqltx.BindTx.
func (o *Outbox) TxSource() doger.TxSource { return sqltx.Source() }

// SendTx writes env, addressed to env.Destination, to the outbox table in
// the transaction sqltx binds to ctx; the forwarder relays it once that
// transaction committed. Without a transaction bound to the outbox's
// database it returns the error of sqltx.FromDB unchanged: one matching
// doger.ErrNoTransaction, or sqltx.ErrForeignTransaction for a
// transaction on another *sql.DB.
func (o *Outbox) SendTx(ctx context.Context, env doger.Envelope) error {
	tx, err := o.boundTx(ctx)
	if err != nil {
		return err
	}
	pub, err := o.txPublisher(tx)
	if err != nil {
		return err
	}
	msg := o.transport.Message(env)
	msg.SetContext(ctx)
	if err := pub.Publish(env.Destination, msg); err != nil {
		return fmt.Errorf("doger: dogerwmsql: outbox write for %q: %w", env.Destination, err)
	}

	return nil
}

// boundTx returns the transaction sqltx binds to ctx on the outbox's
// database, its error unchanged, with the outbox table created.
func (o *Outbox) boundTx(ctx context.Context) (*sqltx.Tx, error) {
	tx, err := sqltx.FromDB(ctx, o.db)
	if err != nil {
		return nil, err
	}
	if err := o.ensureTable(ctx, tx); err != nil {
		return nil, err
	}

	return tx, nil
}

// ensureTable creates the outbox table in tx, on the connection tx holds
// (the pool may have no other to spare), unless it exists: until Start or
// a transaction through sqltx committed, each SendTx checks. The creation
// queries take the schema adapter's advisory lock until tx ends, so
// concurrent first transactions create the table in turn; the outbox's own
// lock keeps other watermill-sql schema setup out of that wait.
func (o *Outbox) ensureTable(ctx context.Context, tx *sqltx.Tx) error {
	if o.skipInit || o.tableReady.Load() {
		return nil
	}
	markReady := func(context.Context) { o.tableReady.Store(true) }
	exists, err := tablesExist(ctx, tx.SQL(), messagesTableOf(o.schema, o.topic))
	if err != nil {
		return fmt.Errorf("doger: dogerwmsql: find the outbox table: %w", err)
	}
	if exists { // possibly created by tx itself: ready once it commits
		tx.OnCommit(markReady)
		return nil
	}
	queries, err := o.schema.SchemaInitializingQueries(sql.SchemaInitializingQueriesParams{Topic: o.topic})
	if err != nil {
		return fmt.Errorf("doger: dogerwmsql: create the outbox table: %w", err)
	}
	if err := execAll(ctx, tx.SQL(), queries); err != nil {
		return fmt.Errorf("doger: dogerwmsql: create the outbox table: %w", err)
	}
	tx.OnCommit(markReady)

	return nil
}

// queryExecer is a *sql.DB or a *sql.Tx.
type queryExecer interface {
	ExecContext(ctx context.Context, query string, args ...any) (stdsql.Result, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *stdsql.Row
}

// tablesExist reports whether every table exists, false for a table whose
// name is unknown ("").
func tablesExist(ctx context.Context, q queryExecer, tables ...string) (bool, error) {
	for _, table := range tables {
		if table == "" {
			return false, nil
		}
		var exists bool
		if err := q.QueryRowContext(ctx, `SELECT to_regclass($1) IS NOT NULL`, table).Scan(&exists); err != nil {
			return false, err
		}
		if !exists {
			return false, nil
		}
	}

	return true, nil
}

// execAll runs queries in order on q.
func execAll(ctx context.Context, q queryExecer, queries []sql.Query) error {
	for _, query := range queries {
		if _, err := q.ExecContext(ctx, query.Query, query.Args...); err != nil {
			return err
		}
	}

	return nil
}

// messagesTableOf returns the messages table of topic in schema, "" when
// the adapter does not tell.
func messagesTableOf(schema sql.SchemaAdapter, topic string) string {
	if named, ok := schema.(interface{ MessagesTable(topic string) string }); ok {
		return named.MessagesTable(topic)
	}

	return ""
}

// offsetsTableOf returns the offsets table of topic in offsets, "" when
// the adapter does not tell.
func offsetsTableOf(offsets sql.OffsetsAdapter, topic string) string {
	if named, ok := offsets.(interface{ MessagesOffsetsTable(topic string) string }); ok {
		return named.MessagesOffsetsTable(topic)
	}

	return ""
}

// txPublisher returns a publisher writing enveloped messages to the
// forwarder topic in tx; watermill-sql needs one per transaction.
func (o *Outbox) txPublisher(tx *sqltx.Tx) (*forwarder.Publisher, error) {
	pub, err := sql.NewPublisher(sql.TxFromStdSQL(tx.SQL()), sql.PublisherConfig{SchemaAdapter: o.schema}, o.logger)
	if err != nil {
		return nil, fmt.Errorf("doger: dogerwmsql: outbox publisher: %w", err)
	}

	return forwarder.NewPublisher(pub, forwarder.PublisherConfig{ForwarderTopic: o.topic}), nil
}

// ensureSchema creates, for Start, the outbox tables (the messages and
// the forwarder's offsets) that do not exist, in a transaction of its own
// bounded by ctx. With SkipSchemaInitialization it only checks that the
// tables it can name exist: a forwarder whose subscribe fails would leak a
// watermill router goroutine.
func (o *Outbox) ensureSchema(ctx context.Context) error {
	messages, offsets := messagesTableOf(o.schema, o.topic), offsetsTableOf(o.offsets, o.topic)
	if o.skipInit {
		for _, table := range []string{messages, offsets} {
			if table == "" {
				continue
			}
			exists, err := tablesExist(ctx, o.db, table)
			if err != nil {
				return fmt.Errorf("doger: dogerwmsql: find the outbox tables: %w", err)
			}
			if !exists {
				return fmt.Errorf("doger: dogerwmsql: the outbox table %s does not exist, and SkipSchemaInitialization is set", table)
			}
		}
		return nil
	}
	exists, err := tablesExist(ctx, o.db, messages, offsets)
	if err != nil {
		return fmt.Errorf("doger: dogerwmsql: find the outbox tables: %w", err)
	}
	if !exists {
		if err := o.createSchema(ctx); err != nil {
			return fmt.Errorf("doger: dogerwmsql: create the outbox tables: %w", err)
		}
	}
	o.tableReady.Store(true)

	return nil
}

// createSchema runs the schema and offsets adapters' creation queries in
// one transaction bounded by ctx.
func (o *Outbox) createSchema(ctx context.Context) error {
	queries, err := o.schema.SchemaInitializingQueries(sql.SchemaInitializingQueriesParams{Topic: o.topic})
	if err != nil {
		return err
	}
	offsetQueries, err := o.offsets.SchemaInitializingQueries(sql.OffsetsSchemaInitializingQueriesParams{Topic: o.topic})
	if err != nil {
		return err
	}
	tx, err := o.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if err := execAll(ctx, tx, append(queries, offsetQueries...)); err != nil {
		return errors.Join(err, tx.Rollback())
	}

	return tx.Commit()
}

// newSubscriber returns a watermill-sql subscriber of the outbox table.
// Its group starts at the oldest message, so the forwarder relays what
// was committed while it was not running.
func (o *Outbox) newSubscriber() (*sql.Subscriber, error) {
	sub, err := sql.NewSubscriber(sql.BeginnerFromStdSQL(o.db), sql.SubscriberConfig{
		SchemaAdapter:    o.schema,
		OffsetsAdapter:   o.offsets,
		InitializeSchema: false, // ensureSchema's, or a migration's
	}, o.logger)
	if err != nil {
		return nil, fmt.Errorf("doger: dogerwmsql: outbox subscriber: %w", err)
	}

	return sub, nil
}

// acquire takes the Start/Shutdown lock, or returns ctx's error.
func (o *Outbox) acquire(ctx context.Context) error {
	select {
	case o.lock <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// release frees the Start/Shutdown lock.
func (o *Outbox) release() { <-o.lock }

// Start creates the missing tables (see
// OutboxConfig.SkipSchemaInitialization), runs the forwarder in a
// goroutine and returns once it is running, or its error. When ctx ends
// first, Start stops what it started, once the forwarder finished
// subscribing, and returns ctx's error. Starting a
// started outbox does nothing.
func (o *Outbox) Start(ctx context.Context) error {
	if err := o.acquire(ctx); err != nil {
		return err
	}
	defer o.release()
	if o.forwarder != nil {
		return nil
	}
	if err := o.ensureSchema(ctx); err != nil {
		return err
	}
	sub, err := o.newSubscriber()
	if err != nil {
		return err
	}
	fwd, err := forwarder.NewForwarder(sub, o.transport.Publisher(), o.logger, forwarder.Config{ForwarderTopic: o.topic})
	if err != nil {
		return errors.Join(fmt.Errorf("doger: dogerwmsql: outbox forwarder: %w", err), sub.Close())
	}

	done := make(chan struct{})
	var runErr error
	// Start's context ends when the container's Start returns; the
	// forwarder runs until Shutdown closes it
	runCtx, cancelRun := context.WithCancel(context.WithoutCancel(ctx))
	go func() {
		defer close(done)
		runErr = fwd.Run(runCtx)
	}()
	select {
	case <-fwd.Running():
	case <-done:
		cancelRun()
		return fmt.Errorf("doger: dogerwmsql: outbox forwarder: %w", runErr)
	case <-ctx.Done():
		return errors.Join(ctx.Err(), stopForwarder(fwd, cancelRun, done))
	}
	o.forwarder, o.cancelRun, o.done = fwd, cancelRun, done

	return nil
}

// stopForwarder stops a forwarder whose Run may still be starting: it
// lets Run finish starting (one subscribe), closes it, and waits for Run
// to return. The router's Close must not run before the router is
// running, or it waits for handlers that never start; and cancelling Run's
// context while it subscribes leaks a router goroutine waiting for a
// handler that never ran.
func stopForwarder(fwd *forwarder.Forwarder, cancelRun context.CancelFunc, done <-chan struct{}) error {
	defer cancelRun()
	var err error
	select {
	case <-fwd.Running():
		err = fwd.Close()
	case <-done:
	}
	<-done

	return err
}

// Shutdown closes the forwarder and waits for it to exit, or for ctx to
// end, as it waits for a Start in progress. Shutting down an outbox that
// is not running returns nil.
func (o *Outbox) Shutdown(ctx context.Context) error {
	if err := o.acquire(ctx); err != nil {
		return err
	}
	defer o.release()
	if o.forwarder == nil {
		return nil
	}
	fwd, cancelRun, done := o.forwarder, o.cancelRun, o.done
	o.forwarder, o.cancelRun, o.done = nil, nil, nil
	defer cancelRun()
	err := fwd.Close()
	select {
	case <-done:
	case <-ctx.Done():
		err = errors.Join(err, ctx.Err())
	}

	return err
}
