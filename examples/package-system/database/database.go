package database

import (
	"fmt"
)

// Database represents a database connection.
type Database struct {
	URL       string
	Connected bool
}

// Connect connects to the database.
func (db *Database) Connect() error {
	fmt.Printf("Connecting to database: %s\n", db.URL)
	db.Connected = true
	return nil
}

// Query runs sql and returns its result.
func (db *Database) Query(sql string) string {
	return fmt.Sprintf("Query result: %s", sql)
}
