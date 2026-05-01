package store

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestStoreEnsure(t *testing.T) {
	database := &stubDB{}
	migrationStore := New(database)

	err := migrationStore.Ensure(context.Background())

	require.NoError(t, err)
	require.Len(t, database.execCalls, 1)
	require.Contains(
		t,
		database.execCalls[0].Statements[0],
		"CREATE TABLE IF NOT EXISTS _litemigrate_migrations",
	)
	require.False(t, database.execCalls[0].Transactional)
}

func TestStoreListApplied(t *testing.T) {
	database := &stubDB{
		queryRows: []map[string]any{
			{
				"version":    "20260501143015",
				"name":       "20260501143015_create_users.sql",
				"applied_at": "2026-05-01T14:30:15Z",
			},
		},
	}
	migrationStore := New(database)

	records, err := migrationStore.ListApplied(context.Background())

	require.NoError(t, err)
	require.Equal(
		t,
		"SELECT version, name, applied_at FROM _litemigrate_migrations ORDER BY version ASC",
		database.queryStatement,
	)
	require.Len(t, records, 1)
	require.Equal(t, "20260501143015", records[0].Version)
	require.Equal(t, "20260501143015_create_users.sql", records[0].Name)
	require.Equal(t, time.Date(2026, time.May, 1, 14, 30, 15, 0, time.UTC), records[0].AppliedAt)
}

func TestStoreInsertAndDelete(t *testing.T) {
	database := &stubDB{}
	migrationStore := New(database)
	appliedAt := time.Date(2026, time.May, 1, 14, 30, 15, 0, time.UTC)

	err := migrationStore.Insert(
		context.Background(),
		"20260501143015",
		"20260501143015_create_users.sql",
		appliedAt,
	)
	require.NoError(t, err)

	err = migrationStore.Delete(context.Background(), "20260501143015")
	require.NoError(t, err)

	require.Len(t, database.execCalls, 2)
	require.Contains(t, database.execCalls[0].Statements[0], "INSERT INTO _litemigrate_migrations")
	require.Contains(t, database.execCalls[0].Statements[0], "2026-05-01T14:30:15Z")
	require.Contains(t, database.execCalls[1].Statements[0], "DELETE FROM _litemigrate_migrations")
	require.Contains(t, database.execCalls[1].Statements[0], "20260501143015")
}

type stubDB struct {
	execCalls      []execCall
	queryStatement string
	queryRows      []map[string]any
}

type execCall struct {
	Statements    []string
	Transactional bool
}

func (database *stubDB) Exec(_ context.Context, statements []string, transactional bool) error {
	database.execCalls = append(
		database.execCalls,
		execCall{Statements: statements, Transactional: transactional},
	)
	return nil
}

func (database *stubDB) Query(_ context.Context, statement string) ([]map[string]any, error) {
	database.queryStatement = statement
	return database.queryRows, nil
}

func (database *stubDB) Close() error {
	return nil
}
