package compiler

import (
	"database/sql"
	"fmt"
	"strings"

	_ "modernc.org/sqlite"

	"github.com/varavelio/litemigrate/internal/migrations"
	"github.com/varavelio/litemigrate/internal/store"
)

// Compile applies migrations locally and returns the final SQLite schema.
func Compile(files []migrations.File) (string, error) {
	database, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		return "", fmt.Errorf("open in-memory sqlite database: %w", err)
	}
	defer func() {
		_ = database.Close()
	}()

	for _, file := range files {
		for _, statement := range file.UpStatements {
			if _, err := database.Exec(statement); err != nil {
				return "", fmt.Errorf("apply migration %q locally: %w", file.Name, err)
			}
		}
	}

	rows, err := database.Query(`
SELECT sql
FROM sqlite_schema
WHERE sql IS NOT NULL
  AND name NOT LIKE 'sqlite_%'
  AND name <> ?
ORDER BY rowid
`, store.TableName)
	if err != nil {
		return "", fmt.Errorf("query compiled sqlite schema: %w", err)
	}
	defer func() {
		_ = rows.Close()
	}()

	statements := make([]string, 0)
	for rows.Next() {
		var statement string
		if err := rows.Scan(&statement); err != nil {
			return "", fmt.Errorf("scan compiled sqlite schema: %w", err)
		}

		statement = strings.TrimSpace(statement)
		if statement == "" {
			continue
		}

		statements = append(statements, ensureTerminated(statement))
	}
	if err := rows.Err(); err != nil {
		return "", fmt.Errorf("iterate compiled sqlite schema: %w", err)
	}

	return strings.Join(statements, "\n\n"), nil
}

func ensureTerminated(statement string) string {
	statement = strings.TrimSpace(statement)
	if strings.HasSuffix(statement, ";") {
		return statement
	}
	return statement + ";"
}
