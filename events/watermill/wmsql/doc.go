// Package dogerwmsql makes watermill-sql (PostgreSQL and MySQL) meet the
// doger event transport contract: [PostgresSubscriberFactory] and
// [MySQLSubscriberFactory] build subscribers whose consumer groups start
// at the latest message and clean up after ephemeral subscriptions, and
// [Outbox] writes events in the caller's database transaction, bound
// through github.com/kartaladev/doger/events/sqltx.
package dogerwmsql
