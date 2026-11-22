package dogerwmsql

import (
	"context"
	stdsql "database/sql"
	"time"

	"github.com/ThreeDotsLabs/watermill"
	"github.com/ThreeDotsLabs/watermill-sql/v4/pkg/sql"
	"github.com/ThreeDotsLabs/watermill/message"

	"github.com/kartaladev/doger"
	dogerwatermill "github.com/kartaladev/doger/events/watermill"
)

// Option configures [PostgresSubscriberFactory] and [MySQLSubscriberFactory].
type Option func(*factoryConfig)

// factoryConfig is the configuration the options set.
type factoryConfig struct {
	configure      []func(*sql.SubscriberConfig)
	logger         watermill.LoggerAdapter
	keepEphemeral  bool
	cleanupTimeout time.Duration
}

// WithSubscriberConfig adjusts the watermill-sql subscriber configuration
// of every subscriber the factory builds, such as its poll interval or
// schema adapter. The factory then sets ConsumerGroup to the
// subscription's group and, unless fn set an offsets adapter of another
// type, wraps the default offsets adapter so that a new group starts at
// the latest message. The defaults are watermill-sql's, with
// InitializeSchema true and the schema adapter of [PostgresSchema] or
// [MySQLSchema]. Several options apply in order.
func WithSubscriberConfig(fn func(*sql.SubscriberConfig)) Option {
	return func(c *factoryConfig) {
		if fn != nil {
			c.configure = append(c.configure, fn)
		}
	}
}

// WithLogger sets the logger of the watermill-sql subscribers. Default
// watermill.NewSlogLogger(nil), the slog default logger; nil restores it.
func WithLogger(logger watermill.LoggerAdapter) Option {
	return func(c *factoryConfig) {
		c.logger = logger
	}
}

// PostgresSubscriberFactory returns a factory of watermill-sql PostgreSQL
// subscribers on db, one per subscription, whose consumer group is the
// subscription's group: members of one group compete, and different
// groups each receive every message. A group without a stored offset
// starts after the newest message of the topic. A nil db panics.
func PostgresSubscriberFactory(db *stdsql.DB, opts ...Option) dogerwatermill.SubscriberFactory {
	return newFactory(db, opts, postgresDialect)
}

// MySQLSubscriberFactory is [PostgresSubscriberFactory] for MySQL.
func MySQLSubscriberFactory(db *stdsql.DB, opts ...Option) dogerwatermill.SubscriberFactory {
	return newFactory(db, opts, mysqlDialect)
}

// dialect is what the factories of one database differ by.
type dialect struct {
	schema  sql.SchemaAdapter
	offsets sql.OffsetsAdapter
	// headOffsets wraps offsets so that new groups start at the head,
	// reporting false for an offsets adapter it does not know.
	headOffsets func(offsets sql.OffsetsAdapter, messagesTable func(string) string) (sql.OffsetsAdapter, bool)
	// defaultTable names the messages table of a schema adapter that
	// does not tell.
	defaultTable func(topic string) string
	// deleteGroup deletes the offset of one group from offsetsTable.
	deleteGroup func(offsetsTable string) string
}

var (
	postgresDialect = dialect{
		schema:  PostgresSchema(),
		offsets: sql.DefaultPostgreSQLOffsetsAdapter{},
		headOffsets: func(offsets sql.OffsetsAdapter, table func(string) string) (sql.OffsetsAdapter, bool) {
			base, ok := offsets.(sql.DefaultPostgreSQLOffsetsAdapter)
			return postgresHeadOffsets{DefaultPostgreSQLOffsetsAdapter: base, messagesTable: table}, ok
		},
		defaultTable: sql.DefaultPostgreSQLSchema{}.MessagesTable,
		deleteGroup: func(offsetsTable string) string {
			return `DELETE FROM ` + offsetsTable + ` WHERE consumer_group = $1`
		},
	}
	mysqlDialect = dialect{
		schema:  MySQLSchema(),
		offsets: sql.DefaultMySQLOffsetsAdapter{},
		headOffsets: func(offsets sql.OffsetsAdapter, table func(string) string) (sql.OffsetsAdapter, bool) {
			base, ok := offsets.(sql.DefaultMySQLOffsetsAdapter)
			return mysqlHeadOffsets{DefaultMySQLOffsetsAdapter: base, messagesTable: table}, ok
		},
		defaultTable: sql.DefaultMySQLSchema{}.MessagesTable,
		deleteGroup: func(offsetsTable string) string {
			return `DELETE FROM ` + offsetsTable + ` WHERE consumer_group = ?`
		},
	}
)

// newFactory returns the subscriber factory of d on db with opts.
func newFactory(db *stdsql.DB, opts []Option, d dialect) dogerwatermill.SubscriberFactory {
	if db == nil {
		panic("doger: dogerwmsql: nil *sql.DB")
	}
	cfg := applyOptions(opts)

	return func(_ context.Context, sub doger.Subscription) (message.Subscriber, error) {
		sc := sql.SubscriberConfig{
			SchemaAdapter:    d.schema,
			OffsetsAdapter:   d.offsets,
			InitializeSchema: true,
		}
		for _, fn := range cfg.configure {
			fn(&sc)
		}
		sc.ConsumerGroup = sub.Group
		if head, ok := d.headOffsets(sc.OffsetsAdapter, messagesTable(sc.SchemaAdapter, d.defaultTable)); ok {
			sc.OffsetsAdapter = head
		}

		subscriber, err := sql.NewSubscriber(sql.BeginnerFromStdSQL(db), sc, cfg.logger)
		if err != nil {
			return nil, err
		}
		if !sub.Ephemeral || cfg.keepEphemeral {
			return subscriber, nil
		}
		offsets, ok := sc.OffsetsAdapter.(interface{ MessagesOffsetsTable(topic string) string })
		if !ok {
			return subscriber, nil
		}

		return &ephemeralSubscriber{
			Subscriber: subscriber,
			db:         db,
			group:      sub.Group,
			timeout:    cfg.cleanupTimeout,
			deleteQuery: func(topic string) string {
				return d.deleteGroup(offsets.MessagesOffsetsTable(topic))
			},
		}, nil
	}
}

// applyOptions returns the configuration of opts.
func applyOptions(opts []Option) factoryConfig {
	var cfg factoryConfig
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}
	if cfg.logger == nil {
		cfg.logger = watermill.NewSlogLogger(nil)
	}
	if cfg.cleanupTimeout <= 0 {
		cfg.cleanupTimeout = DefaultCleanupTimeout
	}

	return cfg
}

// messagesTable returns the messages table naming of schema, or fallback
// for a schema adapter that does not tell.
func messagesTable(schema sql.SchemaAdapter, fallback func(topic string) string) func(topic string) string {
	if named, ok := schema.(interface{ MessagesTable(topic string) string }); ok {
		return named.MessagesTable
	}

	return fallback
}

// WithKeepEphemeralGroups keeps the stored offset of an ephemeral group
// (Subscription.Ephemeral) when its subscription stops, for debugging. By
// default the subscriber's Close deletes it; the messages are kept either
// way.
func WithKeepEphemeralGroups() Option {
	return func(c *factoryConfig) {
		c.keepEphemeral = true
	}
}

// WithCleanupTimeout bounds the deletion of an ephemeral group's stored
// offset when its subscription stops; a deletion cut short is returned by
// the subscription's stop and the offset row is left. Default 30s; 0 or a
// negative duration restores it.
func WithCleanupTimeout(d time.Duration) Option {
	return func(c *factoryConfig) {
		c.cleanupTimeout = d
	}
}
