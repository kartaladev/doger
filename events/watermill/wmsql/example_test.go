package dogerwmsql_test

import (
	"context"
	stdsql "database/sql"
	"time"

	"github.com/kartaladev/doger/events/sqltx"

	"github.com/ThreeDotsLabs/watermill"
	"github.com/ThreeDotsLabs/watermill-sql/v4/pkg/sql"

	"github.com/kartaladev/doger"
	dogerwatermill "github.com/kartaladev/doger/events/watermill"
	dogerwmsql "github.com/kartaladev/doger/events/watermill/wmsql"
)

// A dogerwatermill transport on PostgreSQL: a watermill-sql publisher and
// a factory whose subscribers use the subscription's group and start new
// groups at the latest message. (Compile-only: it needs a database.)
func ExamplePostgresSubscriberFactory() {
	db, err := stdsql.Open("pgx", "postgres://localhost/app")
	if err != nil {
		panic(err)
	}
	defer db.Close()

	pub, err := sql.NewPublisher(sql.BeginnerFromStdSQL(db), sql.PublisherConfig{
		SchemaAdapter:        dogerwmsql.PostgresSchema(), // binary payloads, as the factory
		AutoInitializeSchema: true,
	}, watermill.NewSlogLogger(nil))
	if err != nil {
		panic(err)
	}
	defer pub.Close()

	factory := dogerwmsql.PostgresSubscriberFactory(db,
		// optional: any watermill-sql subscriber setting
		dogerwmsql.WithSubscriberConfig(func(c *sql.SubscriberConfig) {
			c.PollInterval = 100 * time.Millisecond
		}),
	)
	transport, err := dogerwatermill.New(pub, factory)
	if err != nil {
		panic(err)
	}

	root := doger.NewWithOpts(&doger.InjectorOpts{EventTransport: transport})
	if err := root.StartWithContext(context.Background()); err != nil {
		panic(err)
	}
	defer root.Shutdown()
}

// OrderShipped is the event of the outbox example.
type OrderShipped struct{ ID string }

// An Outbox writes externalized events in the caller's transaction and
// relays them to the transport's publisher once it committed. Registered
// as the transport service with AsStarter, it starts and stops with the
// container. (Compile-only: it needs a database.)
func ExampleNewOutbox() {
	db, err := stdsql.Open("pgx", "postgres://localhost/app")
	if err != nil {
		panic(err)
	}
	defer db.Close()

	pub, err := sql.NewPublisher(sql.BeginnerFromStdSQL(db), sql.PublisherConfig{
		SchemaAdapter:        dogerwmsql.PostgresSchema(),
		AutoInitializeSchema: true,
	}, watermill.NewSlogLogger(nil))
	if err != nil {
		panic(err)
	}
	transport, err := dogerwatermill.New(pub, dogerwmsql.PostgresSubscriberFactory(db))
	if err != nil {
		panic(err)
	}
	outbox, err := dogerwmsql.NewOutbox(db, transport, dogerwmsql.OutboxConfig{})
	if err != nil {
		panic(err)
	}

	root := doger.New()
	doger.ProvideNamedValue[doger.EventTransport](root, doger.NameOf[doger.EventTransport](), outbox, doger.AsStarter())
	if err := doger.RegisterEventType[OrderShipped](root, "orders.shipped",
		doger.EventExternalize(doger.ExternalizeOutbox)); err != nil {
		panic(err)
	}
	if err := root.Start(); err != nil {
		panic(err)
	}
	defer root.Shutdown()

	// the event is written with the order row, and sent once both commit
	err = sqltx.InTx(context.Background(), db, func(ctx context.Context) error {
		tx, _ := sqltx.From(ctx)
		if _, err := tx.SQL().ExecContext(ctx, "UPDATE orders SET shipped = true WHERE id = $1", "1"); err != nil {
			return err
		}
		return doger.Publish(ctx, root, OrderShipped{ID: "1"})
	})
	if err != nil {
		panic(err)
	}
}
