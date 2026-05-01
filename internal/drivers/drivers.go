package drivers

import "context"

// Driver defines the database operations required by the migration runtime.
type Driver interface {
	// Exec executes one or more SQL statements against the target database.
	Exec(ctx context.Context, statements []string, transactional bool) error
	// Query executes a read-only statement and returns associative rows.
	Query(ctx context.Context, statement string) ([]map[string]any, error)
	// Close releases any resources held by the driver.
	Close() error
}
