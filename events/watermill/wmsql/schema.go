package dogerwmsql

import "github.com/ThreeDotsLabs/watermill-sql/v4/pkg/sql"

// PostgresSchema returns watermill-sql's PostgreSQL schema adapter with a
// BYTEA payload column, the default of [PostgresSubscriberFactory] and
// [Outbox]. Pass it to the watermill-sql publisher too: the default JSON
// column rejects payloads that are not JSON, such as those of a protobuf
// codec. Its fields can be adjusted, such as GenerateMessagesTableName.
// Tables created earlier keep their column type.
func PostgresSchema() sql.DefaultPostgreSQLSchema {
	return sql.DefaultPostgreSQLSchema{GeneratePayloadType: func(string) string { return "BYTEA" }}
}

// MySQLSchema returns watermill-sql's MySQL schema adapter with a LONGBLOB
// payload column, the default of [MySQLSubscriberFactory]. Pass it to the
// watermill-sql publisher too: the default JSON column rewrites JSON
// payloads (whitespace, key order) and rejects others. Tables created
// earlier keep their column type.
func MySQLSchema() sql.DefaultMySQLSchema {
	return sql.DefaultMySQLSchema{GeneratePayloadType: func(string) string { return "LONGBLOB" }}
}
