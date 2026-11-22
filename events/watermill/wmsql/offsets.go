package dogerwmsql

import (
	"github.com/ThreeDotsLabs/watermill-sql/v4/pkg/sql"
)

// postgresHeadOffsets is watermill-sql's PostgreSQL offsets adapter whose
// new groups start after the newest message instead of at offset 0.
type postgresHeadOffsets struct {
	sql.DefaultPostgreSQLOffsetsAdapter
	messagesTable func(topic string) string
}

// BeforeSubscribingQueries seeds the group's offset with the newest
// message of the topic, (0, '0') for an empty one. A group with a stored
// offset keeps it, and concurrent first subscribers race on the primary
// key.
func (a postgresHeadOffsets) BeforeSubscribingQueries(params sql.BeforeSubscribingQueriesParams) ([]sql.Query, error) {
	return []sql.Query{{
		Query: postgresSeedQuery(a.MessagesOffsetsTable(params.Topic), a.messagesTable(params.Topic)),
		Args:  []any{params.ConsumerGroup},
	}}, nil
}

// postgresSeedQuery inserts, unless one exists, the offset of the group
// $1 in offsetsTable at the newest row of messagesTable, in watermill-sql's
// (transaction_id, offset) order.
func postgresSeedQuery(offsetsTable, messagesTable string) string {
	return `INSERT INTO ` + offsetsTable + ` (consumer_group, offset_acked, last_processed_transaction_id)
		SELECT $1, COALESCE(head."offset", 0), COALESCE(head.transaction_id, '0'::xid8)
		FROM (SELECT 1) AS one
		LEFT JOIN (
			SELECT "offset", transaction_id FROM ` + messagesTable + `
			ORDER BY transaction_id DESC, "offset" DESC
			LIMIT 1
		) AS head ON true
		ON CONFLICT DO NOTHING`
}

// mysqlHeadOffsets is watermill-sql's MySQL offsets adapter whose new
// groups start after the newest message instead of at offset 0.
type mysqlHeadOffsets struct {
	sql.DefaultMySQLOffsetsAdapter
	messagesTable func(topic string) string
}

// BeforeSubscribingQueries seeds the group's offset with the newest
// message of the topic, 0 for an empty one; see postgresHeadOffsets.
func (a mysqlHeadOffsets) BeforeSubscribingQueries(params sql.BeforeSubscribingQueriesParams) ([]sql.Query, error) {
	return []sql.Query{{
		Query: mysqlSeedQuery(a.MessagesOffsetsTable(params.Topic), a.messagesTable(params.Topic)),
		Args:  []any{params.ConsumerGroup},
	}}, nil
}

// mysqlSeedQuery inserts, unless one exists, the offset of the group ? in
// offsetsTable at the newest row of messagesTable.
func mysqlSeedQuery(offsetsTable, messagesTable string) string {
	return "INSERT IGNORE INTO " + offsetsTable + " (consumer_group, offset_acked, offset_consumed)" +
		" SELECT ?, COALESCE(MAX(`offset`), 0), COALESCE(MAX(`offset`), 0) FROM " + messagesTable
}
