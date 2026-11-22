# Proposal

## Why

`add-event-transport` defines the `EventTransport` seam and ships only an in-memory transport. Teams that already run Kafka, NATS JetStream, Redis Streams or a SQL database as a message log need a production-ready transport today, without doger core taking on a messaging library. watermill (MIT, v1.5.3) already speaks to all of those backends behind two small interfaces (`message.Publisher`, `message.Subscriber`), so one adapter module covers them. Keeping it a separate module keeps watermill's dependencies (prometheus, protobuf, chi, gobreaker and the backend drivers) out of doger core, as `scheduler/gocron` does for gocron.

watermill configures consumer groups per subscriber instance, not per `Subscribe` call, and most of its backends start a new group at the oldest message. Both conflict with doger's contract (one `Subscription` carries its group; a new group starts at the latest event), so the adapter needs explicit design rather than a one-line wrapper.

## What Changes

**Module `github.com/kartaladev/doger/events/watermill` (package `dogerwatermill`)**
- `New(pub message.Publisher, subs SubscriberFactory, opts...) (*Transport, error)` returns a `doger.EventTransport`. `SubscriberFactory` builds one watermill subscriber per `doger.Subscription`, so the factory maps the group, mode and ephemeral flag onto the backend's own configuration.
- Envelope mapping: message UUID = envelope ID; payload = envelope payload; metadata keys with the prefix `doger_` carry the event type, version, content type, publish time and headers. A message without doger metadata is a permanent failure.
- Acknowledgement: nil from the handler acks; an error waits `WithRetryDelay` (default 1s) and nacks; after `WithMaxAttempts` (default 10, counted per process) or on a `doger.PermanentError`, the message goes to `WithPoisonTopic` when set, or is acked and logged. A failed poison publish waits the retry delay, then nacks, and the redelivery goes to the poison topic again without running the handler. A handler panic is an ordinary error. Each subscription remembers at most `WithMaxTrackedMessages` (default 1000) failing messages.
- Subscribe and stop: one consuming goroutine per subscription; Subscribe rejects a missing destination, group or handler with `doger.ErrSubscriptionNotSupported`; stop cancels the loop (a message received meanwhile is nacked), waits for the handler in flight up to the stop context, and closes the subscriber the factory built, in the background once that context ended.
- Backend recipes in the guide for Kafka, NATS JetStream and Redis Streams that start new groups at the latest event, and a statement of which backends cannot meet the contract (NATS core, gochannel).

**Module `github.com/kartaladev/doger/events/watermill/wmsql` (package `dogerwmsql`)**
- `PostgresSubscriberFactory` / `MySQLSubscriberFactory`: watermill-sql subscribers whose offsets adapter seeds a new group at the current head instead of offset 0 (on PostgreSQL the readable head, below the snapshot's `xmin`; on MySQL a transaction open at seed time with a lower offset is skipped, a documented watermill-sql limitation), and which delete an ephemeral group's offset row when the subscription stops, within `WithCleanupTimeout` (30s). A schema adapter that does not name its messages table is rejected rather than guessed.
- `PostgresSchema()` / `MySQLSchema()`: watermill-sql schema adapters with a binary payload column (`BYTEA` / `LONGBLOB`), the default of the factories and the outbox. Consumers pass the same one to their publisher: watermill-sql's default `JSON` column rejects non-JSON payloads and, on MySQL, rewrites JSON ones.
- `WithLogger(l)` sets the subscribers' logger.
- `Outbox`: a PostgreSQL `doger.TxEventSender` over the adapter. Its `TxSource()` is `sqltx.Source()` from the shared module `github.com/kartaladev/doger/events/sqltx`, so after-commit listeners and the outbox see the same transaction. `SendTx` writes the message through a watermill-sql publisher on the transaction from `sqltx.FromDB(ctx, db)` (returning its `doger.ErrNoTransaction` or `sqltx.ErrForeignTransaction` unchanged), wrapped by watermill's `forwarder` publisher; a `forwarder.Forwarder` relays committed messages to the real broker. The outbox starts and stops with the container when registered as the `NameOf[doger.EventTransport]()` service with `doger.AsStarter()` (the `EventTransport` interface has no `Start` method). It creates only the tables that are missing: `Start` in its own transaction bounded by its context, and before that `SendTx` in the caller's transaction under an advisory lock of the outbox's own; `OutboxConfig.SkipSchemaInitialization` turns creation off. `OutboxConfig.SchemaAdapter` / `OffsetsAdapter` shape the tables, and MySQL adapters are rejected.
- No transaction binder of its own: applications open transactions with `sqltx.BeginTx`, `sqltx.InTx` or `sqltx.BindTx`.

**Workspace and CI**
- Both modules join `go.work` and Dependabot. The conformance suite (`eventtransporttest.Run`) runs against watermill-sql on PostgreSQL and MySQL in CI, from the `events/sql/compose.yaml` services started on the Linux rows of the test matrix (service containers cannot run on its macOS and Windows rows), with DSNs in `DOGER_TEST_POSTGRES_DSN` and `DOGER_TEST_MYSQL_DSN`. `make test-watermill` runs them locally.
- The release dry run accepts pgx as a runtime requirement of `wmsql`: watermill-sql imports it outside its tests.

**Out of scope:** typed helpers for Kafka, NATS and Redis (recipes only), watermill's router and middleware (the adapter consumes subscribers directly), metrics, and exactly-once delivery.

## Capabilities

### New Capabilities
- `watermill-event-transport`: the adapter module: construction and options, envelope-to-message mapping, acknowledgement, retries and poison handling, the subscription lifecycle, and conformance.
- `watermill-sql-support`: the watermill-sql helper module: subscriber factories that start new groups at the latest event and clean up ephemeral groups, and the transactional outbox over `sqltx` transactions.

### Modified Capabilities
- None. Module identity and release rules already cover every workspace module.

## Impact

- New modules `events/watermill` and `events/watermill/wmsql`, Go 1.27, nested under the doger module path as `module-identity` requires.
- Dependencies: `events/watermill` requires doger and `github.com/ThreeDotsLabs/watermill`; `wmsql` adds `github.com/ThreeDotsLabs/watermill-sql/v4` and `github.com/kartaladev/doger/events/sqltx`. Doger core gains nothing.
- `go.work`, `.github/dependabot.yml`, the CI workflow (PostgreSQL and MySQL from `events/sql/compose.yaml` on the Linux rows), the Makefile, and the release dry run's test-only requirement check.
- `dogerwatermill.Transport` also exports `Publisher()` and `Message(env)`, which the outbox needs to relay messages and encode them with the transport's metadata prefix.
- Requires `add-event-transport` to be archived first, and `add-sql-event-transport` for the shared `events/sqltx` module (`sqltx.Tx`, `BeginTx`, `InTx`, `BindTx`, `From`, `Source`). The outbox work in this change cannot start before `sqltx` exists; the base adapter can.
- Docs: a "watermill transport" guide with backend recipes, `Example*` tests, a CHANGELOG entry.

## Defaults & Control

| Behavior | Zero-config default | Consumer control |
|---|---|---|
| Backend | None: the consumer passes a publisher and a subscriber factory | Any watermill publisher; `wmsql` factories or a hand-written factory |
| Metadata key prefix | `doger_` | `WithMetadataPrefix(p)` |
| Retry after a handler error | Wait 1s, then nack | `WithRetryDelay(d)`; 0 nacks at once |
| Attempts before giving up | 10 per process | `WithMaxAttempts(n)`; 0 means unlimited |
| Failing messages remembered per subscription | 1000, least recently failed forgotten first | `WithMaxTrackedMessages(n)` |
| Failed messages | Acked and logged | `WithPoisonTopic(topic)` publishes them there first |
| Logging | `watermill.NewSlogLogger(nil)` (slog default) | `WithLogger(l)` |
| Clock for retry waits | Real clock | `WithClock(c)` |
| watermill-sql subscriber settings (wmsql) | watermill-sql defaults with `InitializeSchema: true` and the schema adapter of `PostgresSchema()` / `MySQLSchema()`; the factory sets the group and the offsets adapter | `WithSubscriberConfig(func(*sql.SubscriberConfig))` |
| Payload column (wmsql) | `BYTEA` (PostgreSQL) / `LONGBLOB` (MySQL) | another schema adapter through `WithSubscriberConfig` and the publisher's config |
| Subscriber logging (wmsql) | `watermill.NewSlogLogger(nil)` | `WithLogger(l)` |
| Outbox logging (wmsql) | `watermill.NewSlogLogger(nil)` | `OutboxConfig.Logger` |
| New group start (wmsql) | Current head of the topic | None: the doger contract requires it; replay tools are the backend's |
| Ephemeral group cleanup (wmsql) | Offset row deleted on stop, within 30s | `WithKeepEphemeralGroups()`; `WithCleanupTimeout(d)` |
| Outbox forwarder topic (wmsql) | `doger_outbox` | `OutboxConfig.ForwarderTopic` |
| Outbox tables (wmsql) | `PostgresSchema()` and watermill-sql's PostgreSQL offsets adapter, created when missing | `OutboxConfig.SchemaAdapter`, `OffsetsAdapter` (MySQL ones rejected), `SkipSchemaInitialization` |
| Transaction binding (wmsql outbox) | `sqltx.Source()`: transactions opened with `sqltx.BeginTx`, `InTx` or bound with `BindTx` | None in the outbox: one shared binder keeps after-commit listeners, the SQL transport and the outbox on the same transaction; an application's own `*sql.Tx` is bound with `sqltx.BindTx` |
