package app

import (
	"bytes"
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"

	"github.com/varavelio/litemigrate/internal/config"
	"github.com/varavelio/litemigrate/internal/drivers"
)

func TestAppRunNew(t *testing.T) {
	dir := t.TempDir()
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}

	application := New(stdout, stderr)
	application.Now = func() time.Time {
		return time.Date(2026, time.May, 1, 14, 30, 15, 0, time.UTC)
	}

	err := application.Run(
		context.Background(),
		[]string{"--directory", dir, "new", "Create Users"},
	)

	path := filepath.Join(dir, "20260501143015_create_users.sql")
	contents, readErr := os.ReadFile(path)

	require.NoError(t, err)
	require.NoError(t, readErr)
	require.Contains(t, string(contents), "-- litemigrate down")
	require.Contains(t, stdout.String(), path)
}

func TestAppRunUpAndDown(t *testing.T) {
	t.Run("applies one migration by default and all with the flag", func(t *testing.T) {
		dir := t.TempDir()
		writeMigration(
			t,
			dir,
			"20260501143015_create_users.sql",
			"CREATE TABLE users (id INTEGER PRIMARY KEY);",
		)
		writeMigration(
			t,
			dir,
			"20260501143016_create_accounts.sql",
			"CREATE TABLE accounts (id INTEGER PRIMARY KEY);",
		)

		database := newSQLiteTestDriver(t)
		stdout := &bytes.Buffer{}
		application := New(stdout, &bytes.Buffer{})
		application.OpenDriver = func(cfg config.Config) (drivers.Driver, error) {
			require.Equal(t, dir, cfg.Directory)
			require.Equal(t, "nsqlite", cfg.Driver)
			return database, nil
		}
		application.Now = func() time.Time {
			return time.Date(2026, time.May, 1, 14, 30, 15, 0, time.UTC)
		}

		err := application.Run(
			context.Background(),
			[]string{"up", "--directory", dir, "--nsqlite-dsn", "http://example.invalid"},
		)
		require.NoError(t, err)
		require.Equal(
			t,
			1,
			countRows(t, database.db, "SELECT COUNT(*) FROM _litemigrate_migrations"),
		)
		require.Equal(t, 1, tableExists(t, database.db, "users"))
		require.Contains(t, stdout.String(), "applied 1 migration")

		err = application.Run(
			context.Background(),
			[]string{"up", "--all", "--directory", dir, "--nsqlite-dsn", "http://example.invalid"},
		)
		require.NoError(t, err)
		require.Equal(
			t,
			2,
			countRows(t, database.db, "SELECT COUNT(*) FROM _litemigrate_migrations"),
		)
		require.Equal(t, 1, tableExists(t, database.db, "accounts"))
		require.Contains(t, stdout.String(), "applied 1 migration")
	})

	t.Run("rolls back the latest reversible migration", func(t *testing.T) {
		dir := t.TempDir()
		writeMigration(t, dir, "20260501143015_create_users.sql", `
CREATE TABLE users (id INTEGER PRIMARY KEY);
-- litemigrate down
DROP TABLE users;
`)

		database := newSQLiteTestDriver(t)
		application := New(&bytes.Buffer{}, &bytes.Buffer{})
		application.OpenDriver = func(cfg config.Config) (drivers.Driver, error) {
			return database, nil
		}
		application.Now = func() time.Time {
			return time.Date(2026, time.May, 1, 14, 30, 15, 0, time.UTC)
		}

		err := application.Run(
			context.Background(),
			[]string{"up", "--directory", dir, "--nsqlite-dsn", "http://example.invalid"},
		)
		require.NoError(t, err)

		err = application.Run(
			context.Background(),
			[]string{"down", "--directory", dir, "--nsqlite-dsn", "http://example.invalid"},
		)
		require.NoError(t, err)
		require.Equal(
			t,
			0,
			countRows(t, database.db, "SELECT COUNT(*) FROM _litemigrate_migrations"),
		)
		require.Equal(t, 0, tableExists(t, database.db, "users"))
	})

	t.Run("fails on irreversible rollback", func(t *testing.T) {
		dir := t.TempDir()
		writeMigration(
			t,
			dir,
			"20260501143015_create_users.sql",
			"CREATE TABLE users (id INTEGER PRIMARY KEY);",
		)

		database := newSQLiteTestDriver(t)
		application := New(&bytes.Buffer{}, &bytes.Buffer{})
		application.OpenDriver = func(cfg config.Config) (drivers.Driver, error) {
			return database, nil
		}
		application.Now = func() time.Time {
			return time.Date(2026, time.May, 1, 14, 30, 15, 0, time.UTC)
		}

		err := application.Run(
			context.Background(),
			[]string{"up", "--directory", dir, "--nsqlite-dsn", "http://example.invalid"},
		)
		require.NoError(t, err)

		err = application.Run(
			context.Background(),
			[]string{"down", "--directory", dir, "--nsqlite-dsn", "http://example.invalid"},
		)
		require.Error(t, err)
		require.ErrorContains(t, err, "has no down migration")
	})
}

func TestAppRunCompile(t *testing.T) {
	t.Run("writes compiled schema to stdout by default", func(t *testing.T) {
		dir := t.TempDir()
		writeMigration(
			t,
			dir,
			"20260501143015_create_users.sql",
			"CREATE TABLE users (id INTEGER PRIMARY KEY, email TEXT NOT NULL UNIQUE);",
		)

		stdout := &bytes.Buffer{}
		application := New(stdout, &bytes.Buffer{})

		err := application.Run(context.Background(), []string{"compile", "--directory", dir})

		require.NoError(t, err)
		require.Contains(t, stdout.String(), "CREATE TABLE users")
	})

	t.Run("writes compiled schema to a file when requested", func(t *testing.T) {
		dir := t.TempDir()
		writeMigration(
			t,
			dir,
			"20260501143015_create_users.sql",
			"CREATE TABLE users (id INTEGER PRIMARY KEY, email TEXT NOT NULL UNIQUE);",
		)

		outputPath := filepath.Join(t.TempDir(), "schema.sql")
		application := New(&bytes.Buffer{}, &bytes.Buffer{})

		err := application.Run(
			context.Background(),
			[]string{"compile", "--directory", dir, "--compile-output", outputPath},
		)
		require.NoError(t, err)

		compiled, readErr := os.ReadFile(outputPath)
		require.NoError(t, readErr)
		require.Contains(t, string(compiled), "CREATE TABLE users")
	})
}

func TestAppRunStatus(t *testing.T) {
	t.Run("prints a brief migration summary", func(t *testing.T) {
		dir := t.TempDir()
		writeMigration(
			t,
			dir,
			"20260501143015_create_users.sql",
			"CREATE TABLE users (id INTEGER PRIMARY KEY);",
		)
		writeMigration(
			t,
			dir,
			"20260501143016_create_accounts.sql",
			"CREATE TABLE accounts (id INTEGER PRIMARY KEY);",
		)

		database := newSQLiteTestDriver(t)
		stdout := &bytes.Buffer{}
		application := New(stdout, &bytes.Buffer{})
		application.OpenDriver = func(cfg config.Config) (drivers.Driver, error) {
			return database, nil
		}
		application.Now = func() time.Time {
			return time.Date(2026, time.May, 1, 14, 30, 15, 0, time.UTC)
		}

		err := application.Run(
			context.Background(),
			[]string{"up", "--directory", dir, "--nsqlite-dsn", "http://example.invalid"},
		)
		require.NoError(t, err)

		stdout.Reset()

		err = application.Run(
			context.Background(),
			[]string{"status", "--directory", dir, "--nsqlite-dsn", "http://example.invalid"},
		)
		require.NoError(t, err)
		require.Equal(
			t,
			"Applied: 1\nPending: 1\nLast applied: 20260501143015_create_users.sql\nNext pending: 20260501143016_create_accounts.sql\n",
			stdout.String(),
		)
	})
}

func TestOpenValidatedDriver(t *testing.T) {
	t.Run("accepts nsqlite with a dsn", func(t *testing.T) {
		application := New(&bytes.Buffer{}, &bytes.Buffer{})
		application.OpenDriver = func(cfg config.Config) (drivers.Driver, error) {
			require.Equal(t, "nsqlite", cfg.Driver)
			require.Equal(t, "http://localhost:9876?authToken=secret", cfg.NSQLite.DSN)
			return newSQLiteTestDriver(t), nil
		}

		database, err := application.openValidatedDriver(config.Config{
			Driver: "nsqlite",
			NSQLite: config.NSQLiteConfig{
				DSN: "http://localhost:9876?authToken=secret",
			},
		})

		require.NoError(t, err)
		require.NoError(t, database.Close())
	})

	t.Run("rejects nsqlite without a dsn", func(t *testing.T) {
		application := New(&bytes.Buffer{}, &bytes.Buffer{})

		_, err := application.openValidatedDriver(config.Config{Driver: "nsqlite"})

		require.Error(t, err)
		require.ErrorContains(t, err, "nsqlite DSN must not be empty")
	})

	t.Run("accepts rqlite with a url", func(t *testing.T) {
		application := New(&bytes.Buffer{}, &bytes.Buffer{})
		application.OpenDriver = func(cfg config.Config) (drivers.Driver, error) {
			require.Equal(t, "rqlite", cfg.Driver)
			require.Equal(t, "http://localhost:4001", cfg.RQLite.URL)
			return newSQLiteTestDriver(t), nil
		}

		database, err := application.openValidatedDriver(config.Config{
			Driver: "rqlite",
			RQLite: config.RQLiteConfig{
				URL: "http://localhost:4001",
			},
		})

		require.NoError(t, err)
		require.NoError(t, database.Close())
	})
}

type sqliteTestDriver struct {
	db *sql.DB
}

func newSQLiteTestDriver(t *testing.T) *sqliteTestDriver {
	t.Helper()

	database, err := sql.Open("sqlite", ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, database.Close())
	})

	return &sqliteTestDriver{db: database}
}

func (driver *sqliteTestDriver) Exec(
	ctx context.Context,
	statements []string,
	transactional bool,
) error {
	if transactional {
		transaction, err := driver.db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}

		for _, statement := range statements {
			if _, err := transaction.ExecContext(ctx, statement); err != nil {
				_ = transaction.Rollback()
				return err
			}
		}

		return transaction.Commit()
	}

	for _, statement := range statements {
		if _, err := driver.db.ExecContext(ctx, statement); err != nil {
			return err
		}
	}

	return nil
}

func (driver *sqliteTestDriver) Query(
	ctx context.Context,
	statement string,
) ([]map[string]any, error) {
	rows, err := driver.db.QueryContext(ctx, statement)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = rows.Close()
	}()

	columns, err := rows.Columns()
	if err != nil {
		return nil, err
	}

	results := make([]map[string]any, 0)
	for rows.Next() {
		values := make([]any, len(columns))
		scanTargets := make([]any, len(columns))
		for index := range values {
			scanTargets[index] = &values[index]
		}

		if err := rows.Scan(scanTargets...); err != nil {
			return nil, err
		}

		row := make(map[string]any, len(columns))
		for index, column := range columns {
			switch typed := values[index].(type) {
			case []byte:
				row[column] = string(typed)
			default:
				row[column] = typed
			}
		}

		results = append(results, row)
	}

	return results, rows.Err()
}

func (driver *sqliteTestDriver) Close() error {
	return nil
}

func writeMigration(t *testing.T, dir, name, contents string) {
	t.Helper()

	err := os.WriteFile(filepath.Join(dir, name), []byte(contents), 0o600)
	require.NoError(t, err)
}

func countRows(t *testing.T, database *sql.DB, statement string) int {
	t.Helper()

	var count int
	err := database.QueryRow(statement).Scan(&count)
	require.NoError(t, err)
	return count
}

func tableExists(t *testing.T, database *sql.DB, name string) int {
	t.Helper()

	var count int
	err := database.QueryRow("SELECT COUNT(*) FROM sqlite_schema WHERE type = 'table' AND name = ?", name).
		Scan(&count)
	require.NoError(t, err)
	return count
}
