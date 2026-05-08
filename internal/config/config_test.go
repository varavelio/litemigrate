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
		require.Equal(t, "nsqlite", cfg.Driver)
		require.Equal(t, "./migrations", cfg.Directory)
		require.Equal(t, "30s", cfg.RQLite.Timeout.String())
		require.Equal(t, "30s", cfg.NSQLite.Timeout.String())
		require.Empty(t, cfg.Compile.Output)
	})

	t.Run("rejects explicit driver in yaml", func(t *testing.T) {
		tempDir := t.TempDir()
		configPath := filepath.Join(tempDir, "litemigrate.yaml")
		err := os.WriteFile(configPath, []byte("driver: nsqlite\n"), 0o600)
		require.NoError(t, err)

		loader := NewLoader()
		loader.Getwd = func() (string, error) {
			return tempDir, nil
		}

		_, err = loader.Load(Flags{ConfigPath: configPath})

		require.Error(t, err)
		require.ErrorContains(t, err, "explicit driver configuration is not supported")
	})

	t.Run("rejects explicit driver in environment", func(t *testing.T) {
		t.Setenv("LITEMIGRATE_DRIVER", "nsqlite")

		loader := NewLoader()
		loader.Getwd = func() (string, error) {
			return t.TempDir(), nil
		}

		_, err := loader.Load(Flags{})

		require.Error(t, err)
		require.ErrorContains(t, err, "explicit driver configuration is not supported")
	})

	t.Run("infers rqlite driver from configured settings", func(t *testing.T) {
		tempDir := t.TempDir()
		configPath := filepath.Join(tempDir, "litemigrate.yaml")
		err := os.WriteFile(configPath, []byte(`
rqlite:
  url: http://file.example:4001
`), 0o600)
		require.NoError(t, err)

		loader := NewLoader()
		loader.Getwd = func() (string, error) {
			return tempDir, nil
		}

		cfg, err := loader.Load(Flags{ConfigPath: configPath})

		require.NoError(t, err)
		require.Equal(t, "rqlite", cfg.Driver)
		require.Equal(t, "http://file.example:4001", cfg.RQLite.URL)
	})

	t.Run("infers nsqlite driver from configured settings", func(t *testing.T) {
		tempDir := t.TempDir()
		configPath := filepath.Join(tempDir, "litemigrate.yaml")
		err := os.WriteFile(configPath, []byte(`
nsqlite:
  dsn: http://file.example:9876?authToken=file
  timeout: 45s
`), 0o600)
		require.NoError(t, err)

		loader := NewLoader()
		loader.Getwd = func() (string, error) {
			return tempDir, nil
		}

		cfg, err := loader.Load(Flags{ConfigPath: configPath})

		require.NoError(t, err)
		require.Equal(t, "nsqlite", cfg.Driver)
		require.Equal(t, "http://file.example:9876?authToken=file", cfg.NSQLite.DSN)
		require.Equal(t, "45s", cfg.NSQLite.Timeout.String())
	})

	t.Run("applies yaml env and flag precedence", func(t *testing.T) {
		tempDir := t.TempDir()
		configPath := filepath.Join(tempDir, "litemigrate.yaml")
		err := os.WriteFile(configPath, []byte(`
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

	t.Run("resolves variables using env file and environment", func(t *testing.T) {
		tempDir := t.TempDir()

		envPath := filepath.Join(tempDir, ".env")
		err := os.WriteFile(envPath, []byte(`
DB_PASS=mysecret
DB_PORT=4001
ENV_TIMEOUT=1m
ENV_HEADER_VAL=header-from-env
`), 0o600)
		require.NoError(t, err)

		configPath := filepath.Join(tempDir, "litemigrate.yaml")
		err = os.WriteFile(configPath, []byte(`
rqlite:
  url: "env:DB_PORT"
  password: "env:DB_PASS"
  timeout: "env:ENV_TIMEOUT"
  headers:
    X-Custom: "env:ENV_HEADER_VAL"
`), 0o600)
		require.NoError(t, err)

		loader := NewLoader()
		loader.Getwd = func() (string, error) {
			return tempDir, nil
		}

		cfg, err := loader.Load(Flags{ConfigPath: configPath, Dotenv: envPath})

		require.NoError(t, err)
		require.Equal(t, "4001", cfg.RQLite.URL)
		require.Equal(t, "mysecret", cfg.RQLite.Password)
		require.Equal(t, "1m0s", cfg.RQLite.Timeout.String())
		require.Equal(t, "header-from-env", cfg.RQLite.Headers["X-Custom"])
	})

	t.Run("resolves variables in flags using env file", func(t *testing.T) {
		tempDir := t.TempDir()

		envPath := filepath.Join(tempDir, ".env")
		err := os.WriteFile(envPath, []byte(`
FLAG_USER=flag-user
FLAG_PASS=flag-pass
`), 0o600)
		require.NoError(t, err)

		loader := NewLoader()
		loader.Getwd = func() (string, error) {
			return tempDir, nil
		}

		cfg, err := loader.Load(Flags{
			Dotenv:         envPath,
			RQLiteUsername: "env:FLAG_USER",
			RQLitePassword: "env:FLAG_PASS",
		})

		require.NoError(t, err)
		require.Equal(t, "flag-user", cfg.RQLite.Username)
		require.Equal(t, "flag-pass", cfg.RQLite.Password)
	})

	t.Run("applies nsqlite dsn from yaml env and flags", func(t *testing.T) {
		tempDir := t.TempDir()
		configPath := filepath.Join(tempDir, "litemigrate.yaml")
		err := os.WriteFile(configPath, []byte(`
nsqlite:
  dsn: http://file.example:9876?authToken=file
`), 0o600)
		require.NoError(t, err)

		t.Setenv("LITEMIGRATE_NSQLITE_DSN", "http://env.example:9876?authToken=env")

		loader := NewLoader()
		loader.Getwd = func() (string, error) {
			return tempDir, nil
		}

		cfg, err := loader.Load(Flags{
			NSQLiteDSN:     "http://flag.example:9876?authToken=flag",
			NSQLiteTimeout: "45s",
		})

		require.NoError(t, err)
		require.Equal(t, "nsqlite", cfg.Driver)
		require.Equal(t, "http://flag.example:9876?authToken=flag", cfg.NSQLite.DSN)
		require.Equal(t, "45s", cfg.NSQLite.Timeout.String())
	})

	t.Run("resolves nsqlite dsn variables", func(t *testing.T) {
		tempDir := t.TempDir()

		envPath := filepath.Join(tempDir, ".env")
		err := os.WriteFile(envPath, []byte(`
NSQLITE_DSN=http://localhost:9876?authToken=secret
`), 0o600)
		require.NoError(t, err)

		configPath := filepath.Join(tempDir, "litemigrate.yaml")
		err = os.WriteFile(configPath, []byte(`
nsqlite:
  dsn: "env:NSQLITE_DSN"
`), 0o600)
		require.NoError(t, err)

		loader := NewLoader()
		loader.Getwd = func() (string, error) {
			return tempDir, nil
		}

		cfg, err := loader.Load(Flags{ConfigPath: configPath, Dotenv: envPath})

		require.NoError(t, err)
		require.Equal(t, "http://localhost:9876?authToken=secret", cfg.NSQLite.DSN)
	})

	t.Run("rejects mixed rqlite and nsqlite settings", func(t *testing.T) {
		tempDir := t.TempDir()
		configPath := filepath.Join(tempDir, "litemigrate.yaml")
		err := os.WriteFile(configPath, []byte(`
rqlite:
  url: http://file.example:4001
nsqlite:
  dsn: http://file.example:9876?authToken=file
`), 0o600)
		require.NoError(t, err)

		loader := NewLoader()
		loader.Getwd = func() (string, error) {
			return tempDir, nil
		}

		_, err = loader.Load(Flags{ConfigPath: configPath})

		require.Error(t, err)
		require.ErrorContains(t, err, "rqlite and nsqlite settings cannot be used together")
	})

	t.Run("resolves dotenv using yaml configuration", func(t *testing.T) {
		tempDir := t.TempDir()

		envPath := filepath.Join(tempDir, "custom.env")
		err := os.WriteFile(envPath, []byte(`
YAML_DB_PASS=yaml-secret
`), 0o600)
		require.NoError(t, err)

		configPath := filepath.Join(tempDir, "litemigrate.yaml")
		err = os.WriteFile(configPath, []byte(`
dotenv: custom.env
rqlite:
  password: "env:YAML_DB_PASS"
`), 0o600)
		require.NoError(t, err)

		loader := NewLoader()
		loader.Getwd = func() (string, error) {
			return tempDir, nil
		}

		cfg, err := loader.Load(Flags{ConfigPath: configPath})

		require.NoError(t, err)
		require.Equal(t, "yaml-secret", cfg.RQLite.Password)
	})
}
