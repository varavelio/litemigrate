package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLoaderLoad(t *testing.T) {
	t.Run("returns defaults when no other source is present", func(t *testing.T) {
		tempDir := t.TempDir()
		loader := NewLoader()
		loader.Getwd = func() (string, error) {
			return tempDir, nil
		}

		cfg, err := loader.Load(Flags{})

		require.NoError(t, err)
		require.Equal(t, "rqlite", cfg.Driver)
		require.Equal(t, "./migrations", cfg.Directory)
		require.Equal(t, "30s", cfg.RQLite.Timeout.String())
		require.Empty(t, cfg.Compile.Output)
	})

	t.Run("applies yaml env and flag precedence", func(t *testing.T) {
		tempDir := t.TempDir()
		configPath := filepath.Join(tempDir, "litemigrate.yaml")
		err := os.WriteFile(configPath, []byte(`
driver: rqlite
directory: file-migrations
compile:
  output: file.sql
rqlite:
  url: http://file.example:4001
  timeout: 45s
  username: file-user
  password: file-pass
  headers:
    X-File: from-file
    X-Shared: from-file
`), 0o600)
		require.NoError(t, err)

		t.Setenv("LITEMIGRATE_DIRECTORY", "env-migrations")
		t.Setenv("LITEMIGRATE_RQLITE_URL", "http://env.example:4001")
		t.Setenv("LITEMIGRATE_RQLITE_TIMEOUT", "1m")
		t.Setenv("LITEMIGRATE_RQLITE_HEADERS", "X-Env=from-env,X-Shared=from-env")

		loader := NewLoader()
		loader.Getwd = func() (string, error) {
			return tempDir, nil
		}

		cfg, err := loader.Load(Flags{
			Directory:      "flag-migrations",
			RQLiteTimeout:  "90s",
			RQLiteHeaders:  map[string]string{"X-Flag": "from-flag", "X-Shared": "from-flag"},
			RQLiteUsername: "flag-user",
		})

		require.NoError(t, err)
		require.Equal(t, "flag-migrations", cfg.Directory)
		require.Equal(t, "http://env.example:4001", cfg.RQLite.URL)
		require.Equal(t, "1m30s", cfg.RQLite.Timeout.String())
		require.Equal(t, "flag-user", cfg.RQLite.Username)
		require.Equal(t, "file-pass", cfg.RQLite.Password)
		require.Equal(t, "file.sql", cfg.Compile.Output)
		require.Equal(t, map[string]string{
			"X-File":   "from-file",
			"X-Env":    "from-env",
			"X-Flag":   "from-flag",
			"X-Shared": "from-flag",
		}, cfg.RQLite.Headers)
	})

	t.Run("uses an explicit config path from flags", func(t *testing.T) {
		tempDir := t.TempDir()
		configPath := filepath.Join(tempDir, "custom.yaml")
		err := os.WriteFile(configPath, []byte(`
directory: custom-migrations
rqlite:
  url: http://custom.example:4001
`), 0o600)
		require.NoError(t, err)

		loader := NewLoader()
		loader.Getwd = func() (string, error) {
			return t.TempDir(), nil
		}

		cfg, err := loader.Load(Flags{ConfigPath: configPath})

		require.NoError(t, err)
		require.Equal(t, "custom-migrations", cfg.Directory)
		require.Equal(t, "http://custom.example:4001", cfg.RQLite.URL)
	})

	t.Run("fails when an explicit config path does not exist", func(t *testing.T) {
		loader := NewLoader()

		_, err := loader.Load(Flags{ConfigPath: filepath.Join(t.TempDir(), "missing.yaml")})

		require.Error(t, err)
	})
}
