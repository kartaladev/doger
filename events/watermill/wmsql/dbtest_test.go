package dogerwmsql_test

import (
	"context"
	"crypto/rand"
	stdsql "database/sql"
	"encoding/hex"
	"net/url"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/ThreeDotsLabs/watermill"
	"github.com/ThreeDotsLabs/watermill-sql/v4/pkg/sql"
	"github.com/ThreeDotsLabs/watermill/message"
	gomysql "github.com/go-sql-driver/mysql"
	_ "github.com/jackc/pgx/v5/stdlib" // registers the "pgx" driver

	dogerwatermill "github.com/kartaladev/doger/events/watermill"
	dogerwmsql "github.com/kartaladev/doger/events/watermill/wmsql"
)

// The database tests read their servers from the environment. Unset, they
// skip, except in CI on Linux, where the databases are provided and a
// missing DSN is a failure (macOS and Windows CI runners have none).
const (
	envPostgresDSN = "DOGER_TEST_POSTGRES_DSN"
	envMySQLDSN    = "DOGER_TEST_MYSQL_DSN"
)

// lookupDSN returns the DSN in env, or why the test cannot run: skip when
// the database is optional, fail when CI on Linux requires it.
func lookupDSN(env string, getenv func(string) string, goos string) (dsn, skip, fail string) {
	if dsn = getenv(env); dsn != "" {
		return dsn, "", ""
	}
	if getenv("CI") != "" && goos == "linux" {
		return "", "", env + " is required in CI"
	}

	return "", "set " + env + " to run", ""
}

// requireDSN returns the DSN in env, skipping or failing t without one.
func requireDSN(t *testing.T, env string) string {
	t.Helper()
	dsn, skip, fail := lookupDSN(env, os.Getenv, runtime.GOOS)
	if fail != "" {
		t.Fatal(fail)
	}
	if skip != "" {
		t.Skip(skip)
	}

	return dsn
}

// engine is one database kind under test.
type engine struct {
	name    string // "postgres" or "mysql"
	env     string
	driver  string
	schema  sql.SchemaAdapter
	offsets sql.OffsetsAdapter
	factory func(db *stdsql.DB, opts ...dogerwmsql.Option) dogerwatermill.SubscriberFactory
}

var (
	postgres = engine{
		name: "postgres", env: envPostgresDSN, driver: "pgx",
		schema: dogerwmsql.PostgresSchema(), offsets: sql.DefaultPostgreSQLOffsetsAdapter{},
		factory: dogerwmsql.PostgresSubscriberFactory,
	}
	mysql = engine{
		name: "mysql", env: envMySQLDSN, driver: "mysql",
		schema: dogerwmsql.MySQLSchema(), offsets: sql.DefaultMySQLOffsetsAdapter{},
		factory: dogerwmsql.MySQLSubscriberFactory,
	}
	engines = []engine{postgres, mysql}
)

// fast polls the database every 20ms instead of every second.
func fast() dogerwmsql.Option {
	return dogerwmsql.WithSubscriberConfig(func(c *sql.SubscriberConfig) {
		c.PollInterval = 20 * time.Millisecond
		c.ResendInterval = 20 * time.Millisecond
		c.RetryInterval = 20 * time.Millisecond
	})
}

// openDB returns a *sql.DB on a schema (PostgreSQL) or database (MySQL) of
// its own, dropped when the test ends.
func openDB(t *testing.T, e engine) *stdsql.DB {
	t.Helper()
	dsn := requireDSN(t, e.env)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	admin, err := stdsql.Open(e.driver, dsn)
	if err != nil {
		t.Fatalf("open %s: %v", e.name, err)
	}
	t.Cleanup(func() { _ = admin.Close() })

	var raw [6]byte
	_, _ = rand.Read(raw[:])
	schema := "w_" + hex.EncodeToString(raw[:])
	create, drop := "CREATE SCHEMA "+schema, "DROP SCHEMA "+schema+" CASCADE" //nolint:gosec // G202: schema is a random hex name
	if e.name == "mysql" {
		create, drop = "CREATE DATABASE "+schema, "DROP DATABASE "+schema
	}
	if _, err := admin.ExecContext(ctx, create); err != nil {
		t.Fatalf("%s: %s: %v", e.name, create, err)
	}

	db, err := stdsql.Open(e.driver, scopedDSN(t, e, dsn, schema))
	if err != nil {
		t.Fatalf("open %s: %v", e.name, err)
	}
	t.Cleanup(func() {
		_ = db.Close()
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if _, err := admin.ExecContext(ctx, drop); err != nil {
			t.Errorf("%s: %s: %v", e.name, drop, err)
		}
	})

	return db
}

// scopedDSN returns dsn using schema: search_path on PostgreSQL, the
// database name on MySQL.
func scopedDSN(t *testing.T, e engine, dsn, schema string) string {
	t.Helper()
	if e.name == "mysql" {
		cfg, err := gomysql.ParseDSN(dsn)
		if err != nil {
			t.Fatalf("parse %s DSN: %v", e.name, err)
		}
		cfg.DBName = schema
		return cfg.FormatDSN()
	}
	if !strings.Contains(dsn, "://") {
		return dsn + " search_path=" + schema
	}
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatalf("parse %s DSN: %v", e.name, err)
	}
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()

	return u.String()
}

// newPublisher returns a watermill-sql publisher of e on db.
func newPublisher(t *testing.T, e engine, db *stdsql.DB) *sql.Publisher {
	t.Helper()
	pub, err := sql.NewPublisher(sql.BeginnerFromStdSQL(db), sql.PublisherConfig{
		SchemaAdapter:        e.schema,
		AutoInitializeSchema: true,
	}, watermill.NopLogger{})
	if err != nil {
		t.Fatalf("watermill-sql publisher: %v", err)
	}
	t.Cleanup(func() { _ = pub.Close() })

	return pub
}

// publishN publishes n JSON messages to topic.
func publishN(t *testing.T, pub message.Publisher, topic string, n int) {
	t.Helper()
	for range n {
		if err := pub.Publish(topic, message.NewMessage(watermill.NewUUID(), []byte(`{"n":1}`))); err != nil {
			t.Fatalf("publish: %v", err)
		}
	}
}

// newTestTransport returns a dogerwatermill transport over db: a
// watermill-sql publisher of e and e's factory polling fast with
// factoryOpts, retrying after 10ms and logging nothing.
func newTestTransport(t *testing.T, e engine, db *stdsql.DB, factoryOpts ...dogerwmsql.Option) *dogerwatermill.Transport {
	t.Helper()
	transport, err := dogerwatermill.New(newPublisher(t, e, db),
		e.factory(db, append([]dogerwmsql.Option{fast(), dogerwmsql.WithLogger(watermill.NopLogger{})}, factoryOpts...)...),
		dogerwatermill.WithRetryDelay(10*time.Millisecond),
		dogerwatermill.WithLogger(watermill.NopLogger{}))
	if err != nil {
		t.Fatalf("dogerwatermill.New: %v", err)
	}

	return transport
}
