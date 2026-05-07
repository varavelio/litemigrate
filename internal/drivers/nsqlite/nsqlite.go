package nsqlite

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	_ "github.com/varavelio/nsqlitego"
)

// Config contains the nsqlite driver configuration.
type Config struct {
	// DSN is the nsqlite database/sql connection string.
	DSN string
}

// Driver executes migration operations against NSQLite through database/sql.
type Driver struct {
	db *sql.DB
}

// New creates a new nsqlite database/sql driver.
func New(config Config) (*Driver, error) {
	dsn := strings.TrimSpace(config.DSN)
	if dsn == "" {
		return nil, fmt.Errorf("nsqlite DSN must not be empty")
	}

	database, err := sql.Open("nsqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open nsqlite database: %w", err)
	}

	return &Driver{db: database}, nil
}

// Exec executes write statements against NSQLite.
func (driver *Driver) Exec(ctx context.Context, statements []string, transactional bool) error {
	if len(statements) == 0 {
		return nil
	}

	if transactional {
		transaction, err := driver.db.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("begin nsqlite transaction: %w", err)
		}

		for _, statement := range statements {
			if _, err := transaction.ExecContext(ctx, statement); err != nil {
				_ = transaction.Rollback()
				return fmt.Errorf("execute nsqlite statement: %w", err)
			}
		}

		if err := transaction.Commit(); err != nil {
			return fmt.Errorf("commit nsqlite transaction: %w", err)
		}
		return nil
	}

	for _, statement := range statements {
		if _, err := driver.db.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("execute nsqlite statement: %w", err)
		}
	}

	return nil
}

// Query executes a read statement and returns associative rows.
func (driver *Driver) Query(ctx context.Context, statement string) ([]map[string]any, error) {
	rows, err := driver.db.QueryContext(ctx, statement)
	if err != nil {
		return nil, fmt.Errorf("query nsqlite statement: %w", err)
	}
	defer func() {
		_ = rows.Close()
	}()

	columns, err := rows.Columns()
	if err != nil {
		return nil, fmt.Errorf("read nsqlite columns: %w", err)
	}

	results := make([]map[string]any, 0)
	for rows.Next() {
		values := make([]any, len(columns))
		scanTargets := make([]any, len(columns))
		for index := range values {
			scanTargets[index] = &values[index]
		}

		if err := rows.Scan(scanTargets...); err != nil {
			return nil, fmt.Errorf("scan nsqlite row: %w", err)
		}

		row := make(map[string]any, len(columns))
		for index, column := range columns {
			row[column] = normalizeValue(values[index])
		}
		results = append(results, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate nsqlite rows: %w", err)
	}

	return results, nil
}

// Close releases the database/sql connection pool.
func (driver *Driver) Close() error {
	return driver.db.Close()
}

func normalizeValue(value any) any {
	if bytes, ok := value.([]byte); ok {
		return string(bytes)
	}
	return value
}
