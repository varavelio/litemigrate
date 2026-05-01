package compiler

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/varavelio/litemigrate/internal/migrations"
)

func TestCompile(t *testing.T) {
	t.Run("compiles the final schema from ordered migrations", func(t *testing.T) {
		schema, err := Compile([]migrations.File{
			{
				Name: "20260501143015_create_users.sql",
				UpStatements: []string{
					"CREATE TABLE users (id INTEGER PRIMARY KEY, email TEXT NOT NULL UNIQUE)",
				},
			},
			{
				Name:         "20260501143016_add_users_email_index.sql",
				UpStatements: []string{"CREATE INDEX idx_users_email ON users(email)"},
			},
		})

		require.NoError(t, err)
		require.Contains(t, schema, "CREATE TABLE users")
		require.Contains(t, schema, "CREATE INDEX idx_users_email")
		require.NotContains(t, schema, "_litemigrate_migrations")
	})

	t.Run("returns an error when a migration fails locally", func(t *testing.T) {
		_, err := Compile([]migrations.File{
			{
				Name:         "20260501143015_bad.sql",
				UpStatements: []string{"CREATE TABL broken"},
			},
		})

		require.Error(t, err)
	})
}
