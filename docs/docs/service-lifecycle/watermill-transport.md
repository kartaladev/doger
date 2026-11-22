---
title: Watermill transport
description: Carry externalized events over any watermill backend (SQL, Kafka, NATS JetStream, Redis Streams) with the dogerwatermill adapter, with consumer groups, retries, poison topics and a SQL outbox.
sidebar_position: 8
---

# Watermill transport

`github.com/kartaladev/doger/events/watermill` (package `dogerwatermill`) is an [event transport](./event-transports.md) over [watermill](https://watermill.io): any watermill backend whose subscribers can be built per consumer group carries a container's externalized events. It is a module of its own, so doger core and applications that do not use it never download watermill or a broker driver.

```sh
go get github.com/kartaladev/doger/events/watermill@latest
# PostgreSQL or MySQL through watermill-sql, with an outbox:
go get github.com/kartaladev/doger/events/watermill/wmsql@latest
```

## Backends {#backends}

watermill configures the consumer group per subscriber, not per subscription, and most backends start a new group at the oldest message, while doger starts a new group at the **latest** event. So the adapter takes a `SubscriberFactory` that builds one subscriber per subscription, and each backend needs the setting below.

| Backend                           | Factory sets the group with                                                              | New group at the latest event                                                                                                                           | Status                                                |
| --------------------------------- | ---------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------- | ----------------------------------------------------- |
| watermill-sql (PostgreSQL, MySQL) | `dogerwmsql.PostgresSubscriberFactory(db)` / `MySQLSubscriberFactory(db)`                | done by the factory (its offsets adapter seeds a new group at the newest row)                                                                           | tested in CI with the conformance suite               |
| watermill-kafka                   | `kafka.SubscriberConfig{ConsumerGroup: sub.Group}`                                       | sarama's default `Consumer.Offsets.Initial = sarama.OffsetNewest`, which `kafka.DefaultSaramaSubscriberConfig()` keeps; never set `sarama.OffsetOldest` | recipe                                                |
| watermill-redisstream             | `redisstream.SubscriberConfig{ConsumerGroup: sub.Group}`                                 | `OldestId: "$"` (the default `"0"` replays the stream)                                                                                                  | recipe                                                |
| watermill-nats JetStream          | `JetStream.DurablePrefix` (and `QueueGroupPrefix`) derived from `sub.Group`              | add `nats.DeliverNew()` to `JetStream.SubscribeOptions` (the default delivers everything)                                                               | recipe                                                |
| watermill-nats core, gochannel    | none: no durable groups, no redelivery on `Nack`, gochannel fans out to every subscriber | not possible                                                                                                                                            | unsupported for `DeliverOnce`; use them in tests only |

A recipe is a factory you write. For example, for Kafka:

```go
factory := func(_ context.Context, sub doger.Subscription) (message.Subscriber, error) {
    return kafka.NewSubscriber(kafka.SubscriberConfig{
        Brokers:               brokers,
        Unmarshaler:           kafka.DefaultMarshaler{},
        OverwriteSaramaConfig: kafka.DefaultSaramaSubscriberConfig(), // OffsetNewest
        ConsumerGroup:         sub.Group,
    }, logger)
}
transport, err := dogerwatermill.New(publisher, factory)
```

`sub.Ephemeral` is true for a `DeliverAll` group named after a generated instance ID: it ends with the subscription, so a factory can map it to a non-durable consumer where the backend has one.

## Setup {#setup}

Build a watermill publisher for your backend and a factory, then make the transport the container's:

```go
transport, err := dogerwatermill.New(publisher, factory)
if err != nil {
    return err // ErrInvalidConfig: nil publisher or factory, or an invalid option
}
root := doger.NewWithOpts(&doger.InjectorOpts{EventTransport: transport})
```

`New` starts nothing and calls neither the publisher nor the factory until the first `Send` or `Subscribe`. The transport never closes the publisher (you own it), and it closes each subscriber the factory built when that subscription stops.

## Options {#options}

| Behavior                                                 | Default                        | Override                                                                      |
| -------------------------------------------------------- | ------------------------------ | ----------------------------------------------------------------------------- |
| Wait after a handler error before nacking                | 1s                             | `WithRetryDelay(d)`; 0 nacks at once                                          |
| Attempts before giving up on a message                   | 10, counted per process        | `WithMaxAttempts(n)`; 0 means unlimited                                       |
| Failing messages whose attempts a subscription remembers | 1000                           | `WithMaxTrackedMessages(n)`; beyond it the least recently failed is forgotten |
| A message given up on                                    | acked and logged               | `WithPoisonTopic(topic)` publishes it there first                             |
| Metadata key prefix                                      | `doger_`                       | `WithMetadataPrefix(p)`                                                       |
| Logger                                                   | `watermill.NewSlogLogger(nil)` | `WithLogger(l)`                                                               |
| Clock of the retry wait                                  | real clock                     | `WithClock(c)`                                                                |

Each message carries the envelope ID as its UUID and the payload as its payload. The other fields are in its metadata: `doger_event_type`, `doger_version`, `doger_content_type`, `doger_published_at` (RFC 3339, UTC) and `doger_h_<name>` per header. A received message without `doger_event_type` is not doger's: it never reaches a listener and is given up on.

## Delivery, retries and poison messages {#delivery}

Delivery is **at least once**. Each subscription handles one message at a time, as watermill delivers them:

- the listener returns nil: the message is acked;
- it returns an error: the transport waits the retry delay, then nacks, and the backend redelivers it;
- it returns a `doger.PermanentError`, or the attempts are exhausted: the message goes to the poison topic and is acked, or, with no poison topic, it is logged and acked. A failed poison publish waits the retry delay, then nacks the message; its redelivery goes to the poison topic again without running the listener;
- it panics: the panic is an ordinary error.

`Subscribe` rejects a subscription without a destination or a group, or a nil handler, with an error matching `doger.ErrSubscriptionNotSupported`. When a backend closes its message channel on its own, the transport logs an error: that subscription receives nothing more.

Attempts are counted per message UUID in the consuming process. They reset when the process restarts, so a process that crashes on a message retries it from one again. A subscription remembers at most 1000 failing messages (`WithMaxTrackedMessages`): a message that failed here and was then handled by another member of the group never comes back, so the oldest is forgotten. Permanent errors and the poison topic are the intended exits. Make listeners idempotent: deduplicate on `doger.EnvelopeFromContext(ctx).ID`, which is also the watermill message UUID, so backend deduplication (for example NATS `TrackMsgID`) agrees with yours.

A failing message blocks its subscription until it succeeds or is given up on: at the defaults, about 10 seconds. To handle in parallel, scale through backend partitions or more instances; a worker pool per subscription would break the per-key ordering Kafka users rely on.

Stopping a subscription (container shutdown) stops consuming (a message received meanwhile is nacked, not handled), waits for the handler in flight, and closes the subscriber. If the shutdown context ends first, it cancels that handler's context and returns at once; the subscriber then closes in the background, its error logged.

## SQL helpers {#sql}

`events/watermill/wmsql` (package `dogerwmsql`) makes watermill-sql meet the doger contract on PostgreSQL and MySQL:

```go
pub, err := sql.NewPublisher(sql.BeginnerFromStdSQL(db), sql.PublisherConfig{
    SchemaAdapter:        dogerwmsql.PostgresSchema(), // or MySQLSchema()
    AutoInitializeSchema: true,
}, logger)
transport, err := dogerwatermill.New(pub, dogerwmsql.PostgresSubscriberFactory(db))
```

- **Groups.** The subscription's group is the watermill-sql consumer group: members of one group compete, different groups each get every message.
- **New groups at the latest message.** watermill-sql starts a new group at offset 0. The factories wrap its default offsets adapter so that a group with no stored offset starts after the newest row of the topic that watermill-sql can read, the readable head: on PostgreSQL, rows below the oldest transaction still open on the server. A transaction open while the group is created may commit a row ordered before the newest one; starting at the readable head delivers it instead of skipping it, along with the rows committed after that head, which no group has received yet. MySQL tells no such horizon: a transaction open while the group is created that took a lower auto-increment offset commits below the seed, and the new group skips it, as watermill-sql's MySQL subscriber skips it for any group already past that offset. A group with a stored offset resumes after it, and concurrent first subscribers of one group race safely on the offsets table's primary key. If you set your own offsets adapter with `WithSubscriberConfig`, it is used as-is and owns the start position. A schema adapter set there must have a `MessagesTable(topic string) string` method (watermill-sql's do), or the factory returns an error matching `ErrInvalidConfig` instead of guessing the table.
- **Binary payloads.** `PostgresSchema()` and `MySQLSchema()` are watermill-sql's schema adapters with a `BYTEA` / `LONGBLOB` payload column, the factories' default. Pass the same one to your publisher. watermill-sql's default `JSON` column rejects payloads that are not JSON (a protobuf codec), and MySQL's JSON column rewrites JSON (whitespace, key order). Tables that already exist keep their column type.
- **Ephemeral groups.** When a `DeliverAll` subscription with a generated instance ID stops, its group's offset row is deleted (the messages stay), within 30s (`WithCleanupTimeout(d)`; a deletion cut short is returned by the stop). `WithKeepEphemeralGroups()` keeps it, for debugging.
- **Other settings.** `WithSubscriberConfig(func(*sql.SubscriberConfig))` adjusts any watermill-sql subscriber setting (poll interval, ack deadline, schema adapter); the factory then sets the group and the offsets adapter. The factories create the tables they need (`InitializeSchema: true`); turn that off there if a migration tool owns them. `WithLogger(l)` sets the subscribers' logger.

## Outbox {#outbox}

`dogerwmsql.Outbox` is a transactional outbox on PostgreSQL in front of the transport: a `doger.TxEventSender` whose `SendTx` writes the event in the caller's transaction, and whose forwarder (watermill's `forwarder` component) relays committed events to the transport's publisher. A rollback discards them.

```go
outbox, err := dogerwmsql.NewOutbox(db, transport, dogerwmsql.OutboxConfig{}) // topic "doger_outbox"
doger.ProvideNamedValue[doger.EventTransport](root, doger.NameOf[doger.EventTransport](), outbox, doger.AsStarter())
_ = doger.RegisterEventType[OrderShipped](root, "orders.shipped", doger.EventExternalize(doger.ExternalizeOutbox))

err = sqltx.InTx(ctx, db, func(ctx context.Context) error {
    // write your rows with sqltx.From(ctx), then:
    return doger.Publish(ctx, root, OrderShipped{ID: "1"})
})
```

- **Transactions** come from the shared `events/sqltx` module: open them with `sqltx.BeginTx` or `sqltx.InTx`, or bind your own `*sql.Tx` with `sqltx.BindTx`. `TxSource()` is `sqltx.Source()`, so after-commit listeners, the SQL transport and the outbox all see the same transaction. `SendTx` without a transaction returns an error matching `doger.ErrNoTransaction`. A transaction on another `*sql.DB` returns `sqltx.ErrForeignTransaction`, since a row written there would lose the atomicity the outbox exists for.
- **Lifecycle.** Registered as the transport service with `doger.AsStarter()`, the outbox's forwarder starts and stops with the container (the `EventTransport` interface has no `Start` method, hence the option). With `InjectorOpts.EventTransport`, call `outbox.Start(ctx)` and `outbox.Shutdown(ctx)` yourself. Both honor their context, `Start` while it creates the tables too. Events committed while the forwarder is stopped are relayed once it starts.
- **Tables.** The outbox creates only the tables that do not exist, so a role that may not create tables works once a migration created them. `Start` creates the outbox table and the forwarder's offsets table. Before that, `SendTx` creates the outbox table in the caller's transaction, on the connection it holds (a pool of one connection works), under an advisory lock of the outbox's own, so other watermill-sql schema setup, such as a publisher on a new topic, does not wait for that transaction. `OutboxConfig.SkipSchemaInitialization` turns creation off, for a migration tool that owns the tables.
- **Configuration.** `OutboxConfig.ForwarderTopic` (default `doger_outbox`) names the outbox topic; `SchemaAdapter` (default `PostgresSchema()`) and `OffsetsAdapter` (default watermill-sql's PostgreSQL one) shape the tables, and `NewOutbox` rejects watermill-sql's MySQL adapters (MySQL commits the caller's transaction on `CREATE TABLE`); `Logger` sets the forwarder's logger.

## Testing {#testing}

The conformance suite runs against watermill-sql on PostgreSQL and MySQL in CI, and against the outbox on PostgreSQL with `eventtransporttest.WithTx`. Run it locally with `make test-watermill`, which starts PostgreSQL 17 and MySQL 8.4 in Docker and sets `DOGER_TEST_POSTGRES_DSN` and `DOGER_TEST_MYSQL_DSN`. Without them, the database tests skip, except in CI on Linux, where they fail.
