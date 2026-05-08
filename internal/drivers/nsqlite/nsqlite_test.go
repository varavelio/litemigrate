package nsqlite

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

func TestNew(t *testing.T) {
	t.Run("rejects empty dsn", func(t *testing.T) {
		_, err := New(Config{})

		require.Error(t, err)
		require.ErrorContains(t, err, "nsqlite DSN must not be empty")
	})

	t.Run("accepts a configured timeout", func(t *testing.T) {
		driver, err := New(Config{DSN: "http://example.invalid", Timeout: 5 * time.Second})

		require.NoError(t, err)
		require.Equal(t, 5*time.Second, driver.timeout)
		require.NoError(t, driver.Close())
	})
}

func TestDriverExec(t *testing.T) {
	t.Run("executes statements in a transaction", func(t *testing.T) {
		driver := newSQLiteBackedDriver(t)

		err := driver.Exec(context.Background(), []string{
			"CREATE TABLE users (id INTEGER PRIMARY KEY, email TEXT NOT NULL UNIQUE)",
			"INSERT INTO users (email) VALUES ('a@example.com')",
			"INSERT INTO users (email) VALUES ('a@example.com')",
		}, true)

		require.Error(t, err)

		rows, err := driver.Query(
			context.Background(),
			"SELECT name FROM sqlite_schema WHERE type = 'table' AND name = 'users'",
		)
		require.NoError(t, err)
		require.Empty(t, rows)
	})

	t.Run("executes statements without a transaction", func(t *testing.T) {
		driver := newSQLiteBackedDriver(t)

		err := driver.Exec(context.Background(), []string{
			"CREATE TABLE users (id INTEGER PRIMARY KEY)",
		}, false)

		require.NoError(t, err)

		rows, err := driver.Query(
			context.Background(),
			"SELECT name FROM sqlite_schema WHERE type = 'table' AND name = 'users'",
		)
		require.NoError(t, err)
		require.Len(t, rows, 1)
		require.Equal(t, "users", rows[0]["name"])
	})
}

func TestDriverQuery(t *testing.T) {
	t.Run("returns associative rows", func(t *testing.T) {
		driver := newSQLiteBackedDriver(t)
		err := driver.Exec(context.Background(), []string{
			"CREATE TABLE users (id INTEGER PRIMARY KEY, email TEXT NOT NULL)",
			"INSERT INTO users (email) VALUES ('a@example.com')",
		}, true)
		require.NoError(t, err)

		rows, err := driver.Query(context.Background(), "SELECT id, email FROM users")

		require.NoError(t, err)
		require.Len(t, rows, 1)
		require.Equal(t, int64(1), rows[0]["id"])
		require.Equal(t, "a@example.com", rows[0]["email"])
	})
}

func newSQLiteBackedDriver(t *testing.T) *Driver {
	t.Helper()

	database, err := sql.Open("sqlite", ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, database.Close())
	})

	return &Driver{db: database}
}
