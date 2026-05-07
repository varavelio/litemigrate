package cli

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParse(t *testing.T) {
	t.Run("parses common flags before and after the command", func(t *testing.T) {
		parsed, err := Parse([]string{
			"--directory", "root-migrations",
			"--driver", "nsqlite",
			"up",
			"--all",
			"--rqlite-url", "http://localhost:4001",
			"--rqlite-timeout", "45s",
			"--rqlite-headers", "X-Test=one,X-Trace=two",
			"--nsqlite-dsn", "http://localhost:9876?authToken=secret",
		})

		require.NoError(t, err)
		require.Equal(t, CommandUp, parsed.Name)
		require.True(t, parsed.All)
		require.Equal(t, "nsqlite", parsed.Flags.Driver)
		require.Equal(t, "root-migrations", parsed.Flags.Directory)
		require.Equal(t, "http://localhost:4001", parsed.Flags.RQLiteURL)
		require.Equal(t, "45s", parsed.Flags.RQLiteTimeout)
		require.Equal(t, "http://localhost:9876?authToken=secret", parsed.Flags.NSQLiteDSN)
		require.Equal(
			t,
			map[string]string{"X-Test": "one", "X-Trace": "two"},
			parsed.Flags.RQLiteHeaders,
		)
	})

	t.Run("parses new and compile command arguments", func(t *testing.T) {
		created, err := Parse([]string{"new", "create_users"})
		require.NoError(t, err)
		require.Equal(t, CommandNew, created.Name)
		require.Equal(t, "create_users", created.MigrationName)

		compiled, err := Parse([]string{"compile", "--compile-output", "schema.sql"})
		require.NoError(t, err)
		require.Equal(t, CommandCompile, compiled.Name)
		require.Equal(t, "schema.sql", compiled.Flags.CompileOutput)
	})

	t.Run("parses the status command", func(t *testing.T) {
		parsed, err := Parse([]string{"status", "--directory", "./migrations"})

		require.NoError(t, err)
		require.Equal(t, CommandStatus, parsed.Name)
		require.Equal(t, "./migrations", parsed.Flags.Directory)
	})

	t.Run("rejects unknown commands", func(t *testing.T) {
		_, err := Parse([]string{"unknown"})

		require.Error(t, err)
	})
}
