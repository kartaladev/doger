package dogerwmsql

import (
	"context"
	stdsql "database/sql"
	"errors"
	"fmt"
	"sync"
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
	// the first SendTx creates the table in the caller's transaction.
	SchemaAdapter sql.SchemaAdapter
	// OffsetsAdapter shapes the forwarder's offsets table. Default
	// watermill-sql's sql.DefaultPostgreSQLOffsetsAdapter{}, whose new
	// group starts at the oldest message, so the forwarder relays what was
	// committed before it first ran.
	OffsetsAdapter sql.OffsetsAdapter
}

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

	schemaMu    sync.Mutex
	schemaReady bool        // both tables exist: Start created them
	tableReady  atomic.Bool // the outbox table exists

	mu        sync.Mutex
	forwarder *forwarder.Forwarder
	done      chan struct{} // closed when the forwarder's Run returned
}

var (
	_ doger.TxEventSender                 = (*Outbox)(nil)
	_ doger.StarterWithContextAndError    = (*Outbox)(nil)
	_ doger.ShutdownerWithContextAndError = (*Outbox)(nil)
)

// NewOutbox returns an outbox writing to db in front of transport. It
// starts nothing and touches no table until SendTx or Start. A nil db or
// transport returns an error matching dogerwatermill.ErrInvalidConfig.
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

	return &Outbox{
		db: db, transport: transport, topic: cfg.ForwarderTopic, logger: cfg.Logger,
		schema: cfg.SchemaAdapter, offsets: cfg.OffsetsAdapter,
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

// ensureTable creates the outbox table in tx, on the connection tx holds,
// until Start or a committed transaction created it: the pool may have no
// other connection to spare. watermill-sql's queries take an advisory
// transaction lock, so concurrent first transactions create it in turn.
func (o *Outbox) ensureTable(ctx context.Context, tx *sqltx.Tx) error {
	if o.tableReady.Load() {
		return nil
	}
	queries, err := o.schema.SchemaInitializingQueries(sql.SchemaInitializingQueriesParams{Topic: o.topic})
	if err != nil {
		return fmt.Errorf("doger: dogerwmsql: create the outbox table: %w", err)
	}
	for _, q := range queries {
		if _, err := tx.SQL().ExecContext(ctx, q.Query, q.Args...); err != nil {
			return fmt.Errorf("doger: dogerwmsql: create the outbox table: %w", err)
		}
	}
	tx.OnCommit(func(context.Context) { o.tableReady.Store(true) })

	return nil
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

// ensureSchema creates the outbox tables (the messages and the
// forwarder's offsets) once, outside any transaction, for Start.
func (o *Outbox) ensureSchema() error {
	o.schemaMu.Lock()
	defer o.schemaMu.Unlock()
	if o.schemaReady {
		return nil
	}
	sub, err := o.newSubscriber()
	if err != nil {
		return err
	}
	defer sub.Close() //nolint:errcheck // never subscribed: nothing to close
	if err := sub.SubscribeInitialize(o.topic); err != nil {
		return fmt.Errorf("doger: dogerwmsql: create the outbox tables: %w", err)
	}
	o.schemaReady = true
	o.tableReady.Store(true)

	return nil
}

// newSubscriber returns a watermill-sql subscriber of the outbox table.
// Its group starts at the oldest message, so the forwarder relays what
// was committed while it was not running.
func (o *Outbox) newSubscriber() (*sql.Subscriber, error) {
	sub, err := sql.NewSubscriber(sql.BeginnerFromStdSQL(o.db), sql.SubscriberConfig{
		SchemaAdapter:    o.schema,
		OffsetsAdapter:   o.offsets,
		InitializeSchema: true,
	}, o.logger)
	if err != nil {
		return nil, fmt.Errorf("doger: dogerwmsql: outbox subscriber: %w", err)
	}

	return sub, nil
}

// Start runs the forwarder in a goroutine and returns once it is running,
// or its error. Starting a started outbox does nothing.
func (o *Outbox) Start(ctx context.Context) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.forwarder != nil {
		return nil
	}
	if err := o.ensureSchema(); err != nil {
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
	go func() {
		defer close(done)
		// Start's context ends when the container's Start returns; the
		// forwarder runs until Shutdown closes it
		runErr = fwd.Run(context.WithoutCancel(ctx))
	}()
	select {
	case <-fwd.Running():
	case <-done:
		return fmt.Errorf("doger: dogerwmsql: outbox forwarder: %w", runErr)
	case <-ctx.Done():
		return errors.Join(ctx.Err(), fwd.Close())
	}
	o.forwarder, o.done = fwd, done

	return nil
}

// Shutdown closes the forwarder and waits for it to exit, or for ctx to
// end. Shutting down an outbox that is not running returns nil.
func (o *Outbox) Shutdown(ctx context.Context) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.forwarder == nil {
		return nil
	}
	fwd, done := o.forwarder, o.done
	o.forwarder, o.done = nil, nil
	err := fwd.Close()
	select {
	case <-done:
	case <-ctx.Done():
		err = errors.Join(err, ctx.Err())
	}

	return err
}
