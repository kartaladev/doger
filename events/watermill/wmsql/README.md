# dogerwmsql

`github.com/kartaladev/doger/events/watermill/wmsql` (package `dogerwmsql`) makes [watermill-sql](https://github.com/ThreeDotsLabs/watermill-sql) on PostgreSQL and MySQL meet the doger event transport contract, for use with [`dogerwatermill`](..).

```sh
go get github.com/kartaladev/doger/events/watermill/wmsql@latest
```

## Quick start

```go
pub, err := sql.NewPublisher(sql.BeginnerFromStdSQL(db), sql.PublisherConfig{
    SchemaAdapter:        dogerwmsql.PostgresSchema(), // or MySQLSchema(); use the same one as the factory
    AutoInitializeSchema: true,
}, logger)
transport, err := dogerwatermill.New(pub, dogerwmsql.PostgresSubscriberFactory(db)) // or MySQLSubscriberFactory
```

- **Groups:** the subscription's group is the watermill-sql consumer group, and a new group starts after the newest message instead of at offset 0.
- **Ephemeral groups:** their offset rows are deleted when the subscription stops, within 30s (`WithCleanupTimeout(d)`; `WithKeepEphemeralGroups()` keeps them).
- **Binary payloads:** `PostgresSchema()` / `MySQLSchema()` use `BYTEA` / `LONGBLOB` payload columns, so any codec's payload comes back byte for byte.
- **Settings:** `WithSubscriberConfig(fn)` adjusts any watermill-sql subscriber setting; `WithLogger(l)` sets the subscribers' logger.

## Outbox (PostgreSQL)

```go
outbox, err := dogerwmsql.NewOutbox(db, transport, dogerwmsql.OutboxConfig{})
doger.ProvideNamedValue[doger.EventTransport](root, doger.NameOf[doger.EventTransport](), outbox, doger.AsStarter())
```

`SendTx` writes the event in the transaction `events/sqltx` binds to the context (`sqltx.InTx`, `BeginTx`, `BindTx`), and watermill's forwarder relays it once the transaction commits. `OutboxConfig.SchemaAdapter` and `OffsetsAdapter` shape its tables; it creates those missing, unless `SkipSchemaInitialization` is set. Register the outbox with `doger.AsStarter()` so that the container starts and stops the forwarder.

See the [Watermill transport guide](https://github.com/kartaladev/doger/blob/main/docs/docs/service-lifecycle/watermill-transport.md) for details.
