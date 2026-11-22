# Tasks

Specs: `specs/watermill-event-transport`, `specs/watermill-sql-support`. Test names and files follow the design's Test Strategy; decisions are D1-D7. Prerequisites: `add-event-transport` is archived and on `main`; `events/sqltx` from `add-sql-event-transport` exists before group 10. Commands for `events/watermill` run in that directory, for `wmsql` in `events/watermill/wmsql`. Database tests need `DOGER_TEST_POSTGRES_DSN` and `DOGER_TEST_MYSQL_DSN` (for example `docker run postgres:17` and `mysql:8.4` locally). Every RED must fail for the stated reason before its GREEN starts.

## 1. Module scaffolding (no behavior)

- [x] 1.1 Create modules `github.com/kartaladev/doger/events/watermill` and `.../events/watermill/wmsql` (Go 1.27, `TestMain` with goleak, `doc.go`), add both to `go.work`, `.github/dependabot.yml` and `make modules`; verify `make test-all` and `make lint-all` pass with the empty packages
- [x] 1.2 Start PostgreSQL and MySQL from `events/sql/compose.yaml` on the Linux rows of the CI test job (service containers cannot run on its macOS and Windows rows) setting `DOGER_TEST_POSTGRES_DSN` / `DOGER_TEST_MYSQL_DSN`, with SHA-pinned actions; verify the workflow passes `make lint-all` and the existing `release-integrity` checks

## 2. Construction (spec: "Constructing the transport")

- [x] 2.1 RED: add `TestNew` (7 rows) and `TestNew_Passive` in `events/watermill/transport_test.go` with the fakes from the design; verify `go test -race -run TestNew .` fails to compile naming only `New`, `SubscriberFactory`, `Transport`, `ErrInvalidConfig` and the `With*` options
- [x] 2.2 GREEN: add `New`, the options with validation and defaults (D2), and `var _ doger.EventTransport = (*Transport)(nil)`; verify the tests pass and `go test -race ./...` is green
- [x] 2.3 REFACTOR: Extract Function `applyOptions`; verify `go test -race ./...` and `make lint-all` pass
- [x] 2.4 Add `ExampleNew` and godoc on `New`, `SubscriberFactory` and each option; verify `go test -run ExampleNew .` passes

## 3. Sending (spec: "Send maps an envelope to a watermill message")

- [x] 3.1 RED: add `TestSend_Fields` (2 rows) and `TestSend_Failures` (3 rows) in `send_test.go`; verify `go test -race -run TestSend .` fails on the metadata assertions
- [x] 3.2 GREEN: implement the D5 mapping, the context check, `SetContext` and panic recovery; verify the tests pass and `go test -race ./...` is green
- [x] 3.3 REFACTOR: Extract Function `encodeMessage` and constants for metadata keys; verify `go test -race ./...` and `make lint-all` pass

## 4. Subscribing and decoding (spec: "Subscribe builds one subscriber per subscription", "Received messages map back to envelopes")

- [x] 4.1 RED: add `TestSubscribe` (4 rows), `TestRoundTrip` (2 rows), `TestForeignMessages` (2 rows) in `subscribe_test.go` with the in-process fake broker; verify `go test -race -run 'TestSubscribe|TestRoundTrip|TestForeignMessages' .` fails on the factory and round-trip assertions
- [x] 4.2 GREEN: implement `Subscribe` (factory call, subscriber close on error, one consuming goroutine) and `decodeMessage`; acks only, retries come in group 5; verify the tests pass and goleak is clean
- [x] 4.3 REFACTOR: Extract Function `decodeMessage`; Introduce Parameter Object `subscription` for loop state; verify `go test -race ./...` and `make lint-all` pass

## 5. Acknowledgement, retries and poison (spec: "Acknowledgement, retries and poison handling")

- [x] 5.1 RED: add `TestAck` (7 rows), `TestAck_RetryDelay` (3 rows, fake clock), `TestAck_PoisonFailure` in `ack_test.go`; verify `go test -race -run TestAck .` fails on the nack counts and poison rows
- [x] 5.2 GREEN: implement the D6 loop branches: per-UUID attempt map owned by the loop, clock-driven delay selecting on the loop context, give-up to poison topic or ack and log; verify the tests pass and `go test -race ./...` is green
- [x] 5.3 REFACTOR: Extract Function `giveUp` and `retryLater`; Replace Nested Conditional with Guard Clauses in the loop; verify `go test -race ./...` and `make lint-all` pass
- [x] 5.4 Add `ExampleWithPoisonTopic` and godoc on retry and poison behavior (including the per-process attempt count); verify `go test -run ExampleWithPoisonTopic .` passes

## 6. Stop and concurrency (spec: "Stop ends the subscription", "The transport is safe for concurrent use")

- [x] 6.1 RED: add `TestStop_WaitsHandler`, `TestStop_Deadline`, `TestStop_Edges` (3 rows) in `stop_test.go` and `TestConcurrent` in `transport_test.go`, hang-prone ones in `testWithTimeout`; verify `go test -race -run 'TestStop|TestConcurrent' .` fails on the wait and double-stop assertions
- [x] 6.2 GREEN: implement stop with `sync.Once`, loop cancel, done channel, bounded wait and subscriber `Close` with panic recovery; verify the tests pass, goleak is clean, and `go test -race ./...` is green
- [x] 6.3 REFACTOR: none expected beyond Extract Function `waitDone`; verify `go test -race ./...` and `make lint-all` pass

## 7. SQL factories and head seeding (spec: "Subscriber factories map subscriptions to consumer groups", "A new group starts at the latest message")

- [x] 7.1 RED: add `TestFactory_Groups` (2 rows), `TestFactory_NilDB` (2 rows), `TestOffsets_Start` (4 rows), `TestOffsets_ConcurrentSeed` and the DSN helper with `TestDSNRequired` (2 rows) in `wmsql`; verify `go test -race ./...` in `wmsql` fails to compile naming only `PostgresSubscriberFactory`, `MySQLSubscriberFactory`, `WithSubscriberConfig`
- [x] 7.2 GREEN: implement both factories and the wrapped offsets adapters whose seed query starts new groups at the newest row (D3); verify the tests pass against both databases and `go test -race ./...` is green
- [x] 7.3 REFACTOR: Extract Function per dialect seed query; Parameterize Function for the shared factory body; verify `go test -race ./...` and `make lint-all` pass
- [x] 7.4 Add `ExamplePostgresSubscriberFactory` and the guide's backend recipe table (Kafka `OffsetNewest`, Redis `OldestId: "$"`, JetStream `nats.DeliverNew()`, unsupported NATS core and gochannel); verify the example compiles with `go vet ./...`

## 8. Ephemeral cleanup (spec: "Ephemeral groups are removed when they stop")

- [x] 8.1 RED: add `TestFactory_EphemeralCleanup` (3 rows) and `TestFactory_CleanupError`; verify they fail on the "no longer exists" row
- [x] 8.2 GREEN: wrap ephemeral subscribers so `Close` deletes the group's offset row; add `WithKeepEphemeralGroups`; verify the tests pass and `go test -race ./...` is green
- [x] 8.3 REFACTOR: none needed if the wrapper is one type; otherwise Extract Function `deleteGroup`; verify `go test -race ./...` and `make lint-all` pass

## 9. Conformance of the transport (spec: "The transport passes the conformance suite")

- [x] 9.1 RED: add `TestConformance` (2 rows) in `wmsql/conformance_test.go` calling `eventtransporttest.Run`; verify it runs and any failing subtest is recorded (if all pass on arrival, record that and show a subtest fails with the head seeding temporarily disabled)
- [x] 9.2 GREEN: fix whatever the suite reports in `events/watermill` or `wmsql`; verify `TestConformance` passes on both databases with `-race`
- [x] 9.3 REFACTOR: Extract Function `newTestTransport` shared by conformance and integration tests; verify `go test -race ./...` and `make lint-all` pass

## 10. Outbox (spec: "The outbox writes in the caller's transaction", "The outbox forwarder follows the container lifecycle", "Outbox conformance")

- [x] 10.1 RED: add `TestOutbox_Delivery` (2 rows), `TestOutbox_SendTx` (5 rows), `TestNewOutbox` (5 rows), `TestOutbox_Lifecycle` (3 rows), `TestOutbox_ShutdownBeforeStart`, `TestOutboxConformance` (harness on `sqltx.BeginTx`); verify the run fails to compile naming only `NewOutbox`, `Outbox`, `OutboxConfig`
- [x] 10.2 GREEN: implement `NewOutbox` with `TxSource()` = `sqltx.Source()`, `SendTx` resolving `sqltx.FromDB(ctx, outboxDB)` and returning its `doger.ErrNoTransaction` / `sqltx.ErrForeignTransaction` errors unchanged and publishing through `forwarder.NewPublisher` over a watermill-sql publisher on `tx.SQL()` (D7), delegation of `Send`/`Subscribe`, and `Start`/`Shutdown` running and closing the `forwarder.Forwarder`; verify the tests pass, goleak is clean, and `go test -race ./...` is green
- [x] 10.3 REFACTOR: Extract Function `boundTx`; Move Function message mapping shared with `dogerwatermill` into an exported helper only if both modules need it (else none, with the reason); verify `go test -race ./...` and `make lint-all` pass
- [x] 10.4 Add `ExampleNewOutbox`; verify it compiles with `go vet ./...`

## 11. Integration and docs

- [x] 11.1 Write the "watermill transport" guide in `docs/` (setup, options, SQL helpers, outbox with `sqltx`, recipes, at-least-once and idempotency, attempt counting caveat) and a CHANGELOG entry; verify the docs build per `docs/` instructions
- [x] 11.2 Run `make test-all`, `make lint-all` and `make vuln` with both DSNs set; verify all pass

## 12. Verify follow-ups (from `/opsx:verify`)

- [x] 12.1 Binary payload schemas (D8): RED `TestSchema_PayloadBytes` failed on MySQL (both rows) and PostgreSQL (binary) with JSON columns; GREEN `PostgresSchema()` / `MySQLSchema()` as factory and outbox defaults; `TestConformance` passes on MySQL
- [x] 12.2 Stop order (D6): RED `TestStop_Edges/closes_before_cancelling` failed; GREEN close before cancelling the subscription context on a clean stop
- [x] 12.3 Coverage of existing behavior: `TestSubscribe/factory_panics` and `TestNew/nil_option_ignored` (both shown to fail with the protection disabled); `TestFactory_CleanupError` asserts the subscriber was closed (shown to fail with Close skipped)
- [x] 12.4 READMEs for `events/watermill` and `events/watermill/wmsql`; release dry run accepts pgx as a runtime requirement of `wmsql`

## 13. Code review follow-ups (from `/code-review high` on PR #35)

Each finding was proven with a test that failed before the fix.

- [x] 13.1 Poison publish failure: RED `TestAck_PoisonFailureRetry` (nacked at once; handler rerun after giving up); GREEN wait the retry delay, then nack, and give up on the redelivery without running the handler
- [x] 13.2 Bounded attempts: RED `TestSubscription_AttemptsBounded` (100000 entries); GREEN `attemptTracker` with `WithMaxTrackedMessages` (default 1000); `TestAttemptTracker`, `TestAck_MaxTrackedMessages`, `TestNew` rows
- [x] 13.3 Handler panic: RED `TestAck_HandlerPanic` crashed the test binary; GREEN recover in `callHandler` as an ordinary error
- [x] 13.4 Seed below open transactions (D3): RED `TestOffsets_SeedBelowOpenTransaction` lost the message; GREEN seed below `xmin`; `awaitReadable` in the offsets tests and non-parallel conformance tests
- [x] 13.5 Closed message channel: RED `TestSubscribe_ChannelClosed` (nothing logged); GREEN log an error
- [x] 13.6 Outbox table in the caller's transaction (D7): RED `TestOutbox_SendTxSingleConnection` waited for a second connection; GREEN create it in the transaction; `TestOutbox_SendTxConcurrentFirst`
- [x] 13.7 Stop past its deadline: RED `TestStop_CloseAfterDeadline` blocked on Close; GREEN close in the background, error logged
- [x] 13.8 No delivery after stop: RED `TestStop_NoDeliveryAfterStop` handled a message after stop; GREEN nack a message received once the loop stopped
- [x] 13.9 Rejected subscriptions: RED `TestSubscribe_Rejected` (3 rows accepted); GREEN error matching `doger.ErrSubscriptionNotSupported`
- [x] 13.10 Overridable defaults (rule 6): `OutboxConfig.SchemaAdapter` / `OffsetsAdapter` (`TestOutbox_Adapters`) and `WithCleanupTimeout` (`TestFactory_EphemeralCleanup` rows), each shown to fail with the override ignored
- [x] 13.11 Guide, READMEs, package docs and CHANGELOG updated

## Workflow follow-up

- Archive after review; requires `add-event-transport` archived first.
- Group 10 (outbox) starts only after `add-sql-event-transport` has delivered `events/sqltx`; groups 1-9 do not depend on it.
