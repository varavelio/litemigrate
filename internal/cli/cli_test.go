package cli

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParse(t *testing.T) {
	t.Run("parses common flags before and after the command", func(t *testing.T) {
		parsed, err := Parse([]string{
			"--directory", "root-migrations",
			"up",
			"--all",
			"--rqlite-url", "http://localhost:4001",
			"--rqlite-timeout", "45s",
			"--rqlite-headers", "X-Test=one,X-Trace=two",
		})

		require.NoError(t, err)
		require.Equal(t, CommandUp, parsed.Name)
		require.True(t, parsed.All)
		require.Equal(t, "root-migrations", parsed.Flags.Directory)
		require.Equal(t, "http://localhost:4001", parsed.Flags.RQLiteURL)
		require.Equal(t, "45s", parsed.Flags.RQLiteTimeout)
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

	t.Run("rejects unknown commands", func(t *testing.T) {
		_, err := Parse([]string{"unknown"})

		require.Error(t, err)
	})
}
