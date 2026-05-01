package store

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/varavelio/litemigrate/internal/drivers"
)

const (
	// TableName is the internal metadata table used to track applied migrations.
	TableName = "_litemigrate_migrations"
	// createTableSQL is the DDL statement to create the metadata table if it does not exist.
	createTableSQL = "CREATE TABLE IF NOT EXISTS _litemigrate_migrations (version TEXT NOT NULL PRIMARY KEY, name TEXT NOT NULL, applied_at TEXT NOT NULL)"
	// listAppliedSQL is the query to list all applied migrations ordered by version.
	listAppliedSQL = "SELECT version, name, applied_at FROM _litemigrate_migrations ORDER BY version ASC"
)

// Record represents one applied migration entry in the metadata table.
type Record struct {
	// Version is the migration version prefix.
	Version string
	// Name is the migration filename stored in the metadata table.
	Name string
	// AppliedAt is the UTC timestamp recorded when the migration succeeded.
	AppliedAt time.Time
}

// Store persists migration state in the target database.
type Store struct {
	database drivers.Driver
}

// New creates a metadata store for the given driver.
func New(database drivers.Driver) *Store {
	return &Store{database: database}
}

// Ensure creates the metadata table if it does not already exist.
func (store *Store) Ensure(ctx context.Context) error {
	if err := store.database.Exec(ctx, []string{createTableSQL}, false); err != nil {
		return fmt.Errorf("ensure migration metadata table: %w", err)
	}
	return nil
}

// ListApplied returns all applied migrations ordered by version.
func (store *Store) ListApplied(ctx context.Context) ([]Record, error) {
	rows, err := store.database.Query(ctx, listAppliedSQL)
	if err != nil {
		return nil, fmt.Errorf("list applied migrations: %w", err)
	}

	records := make([]Record, 0, len(rows))
	for _, row := range rows {
		version := stringify(row["version"])
		name := stringify(row["name"])
		appliedAtValue := stringify(row["applied_at"])

		appliedAt, err := time.Parse(time.RFC3339, appliedAtValue)
		if err != nil {
			return nil, fmt.Errorf("parse applied_at for version %q: %w", version, err)
		}

		records = append(records, Record{
			Version:   version,
			Name:      name,
			AppliedAt: appliedAt,
		})
	}

	return records, nil
}

// Insert records a newly applied migration.
func (store *Store) Insert(ctx context.Context, version, name string, appliedAt time.Time) error {
	statement := fmt.Sprintf(
		"INSERT INTO %s (version, name, applied_at) VALUES ('%s', '%s', '%s')",
		TableName,
		quoteLiteral(version),
		quoteLiteral(name),
		quoteLiteral(appliedAt.UTC().Format(time.RFC3339)),
	)

	if err := store.database.Exec(ctx, []string{statement}, false); err != nil {
		return fmt.Errorf("insert migration record %q: %w", version, err)
	}

	return nil
}

// Delete removes a migration record after a successful rollback.
func (store *Store) Delete(ctx context.Context, version string) error {
	statement := fmt.Sprintf(
		"DELETE FROM %s WHERE version = '%s'",
		TableName,
		quoteLiteral(version),
	)
	if err := store.database.Exec(ctx, []string{statement}, false); err != nil {
		return fmt.Errorf("delete migration record %q: %w", version, err)
	}
	return nil
}

func quoteLiteral(value string) string {
	return strings.ReplaceAll(value, "'", "''")
}

func stringify(value any) string {
	if text, ok := value.(string); ok {
		return text
	}
	return fmt.Sprint(value)
}
