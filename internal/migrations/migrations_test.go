package migrations

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestNewFileName(t *testing.T) {
	t.Run("builds a UTC timestamped migration filename", func(t *testing.T) {
		createdAt := time.Date(2026, time.May, 1, 14, 30, 15, 0, time.UTC)

		name, err := NewFileName(createdAt, "Create Users Table")

		require.NoError(t, err)
		require.Equal(t, "20260501143015_create_users_table.sql", name)
	})

	t.Run("rejects an empty description", func(t *testing.T) {
		_, err := NewFileName(time.Now().UTC(), "   ")

		require.Error(t, err)
	})
}

func TestSplitStatements(t *testing.T) {
	t.Run("splits multiple regular statements", func(t *testing.T) {
		statements, err := SplitStatements(`
CREATE TABLE users (id INTEGER PRIMARY KEY);
CREATE INDEX idx_users_id ON users(id);
`)

		require.NoError(t, err)
		require.Equal(t, []string{
			"CREATE TABLE users (id INTEGER PRIMARY KEY)",
			"CREATE INDEX idx_users_id ON users(id)",
		}, statements)
	})

	t.Run("ignores semicolons inside quoted strings", func(t *testing.T) {
		statements, err := SplitStatements(`
INSERT INTO audit_log(message) VALUES('created; user');
UPDATE users SET note = "semi;colon" WHERE id = 1;
`)

		require.NoError(t, err)
		require.Len(t, statements, 2)
		require.Equal(t, "INSERT INTO audit_log(message) VALUES('created; user')", statements[0])
		require.Equal(t, `UPDATE users SET note = "semi;colon" WHERE id = 1`, statements[1])
	})

	t.Run("keeps a trigger definition as a single statement", func(t *testing.T) {
		statements, err := SplitStatements(`
CREATE TRIGGER users_ai
AFTER INSERT ON users
BEGIN
  INSERT INTO audit_log(user_id, action) VALUES (NEW.id, 'created');
  UPDATE users SET updated_at = CURRENT_TIMESTAMP WHERE id = NEW.id;
END;

CREATE TABLE audit_log (
  id INTEGER PRIMARY KEY,
  user_id INTEGER NOT NULL,
  action TEXT NOT NULL
);
`)

		require.NoError(t, err)
		require.Len(t, statements, 2)
		require.Contains(t, statements[0], "CREATE TRIGGER users_ai")
		require.Contains(t, statements[0], "UPDATE users SET updated_at = CURRENT_TIMESTAMP")
		require.Equal(
			t,
			"CREATE TABLE audit_log (\n  id INTEGER PRIMARY KEY,\n  user_id INTEGER NOT NULL,\n  action TEXT NOT NULL\n)",
			statements[1],
		)
	})

	t.Run("ignores semicolons inside comments", func(t *testing.T) {
		statements, err := SplitStatements(`
-- comment with a ; semicolon
CREATE TABLE users (id INTEGER PRIMARY KEY); /* trailing ; comment */
/* block ; comment */
CREATE INDEX idx_users_id ON users(id);
`)

		require.NoError(t, err)
		require.Equal(t, []string{
			"-- comment with a ; semicolon\nCREATE TABLE users (id INTEGER PRIMARY KEY)",
			"/* trailing ; comment */\n/* block ; comment */\nCREATE INDEX idx_users_id ON users(id)",
		}, statements)
	})

	t.Run("splits multiple statements on one line", func(t *testing.T) {
		statements, err := SplitStatements(
			"CREATE TABLE users (id INTEGER PRIMARY KEY); CREATE INDEX idx_users_id ON users(id);",
		)

		require.NoError(t, err)
		require.Equal(t, []string{
			"CREATE TABLE users (id INTEGER PRIMARY KEY)",
			"CREATE INDEX idx_users_id ON users(id)",
		}, statements)
	})

	t.Run("rejects an incomplete trailing statement", func(t *testing.T) {
		_, err := SplitStatements(`
CREATE TABLE users (id INTEGER PRIMARY KEY);
CREATE INDEX idx_users_id ON users(id)
`)

		require.Error(t, err)
		require.Contains(t, err.Error(), "incomplete")
	})
}

func TestParseFile(t *testing.T) {
	t.Run("parses implicit up and explicit down sections", func(t *testing.T) {
		migration, err := ParseFile(
			filepath.Join("/tmp", "20260501143015_create_users.sql"),
			[]byte(`
CREATE TABLE users (
  id INTEGER PRIMARY KEY,
  email TEXT NOT NULL UNIQUE
);

-- litemigrate down
DROP TABLE users;
`),
		)

		require.NoError(t, err)
		require.Equal(t, "20260501143015", migration.Version)
		require.Equal(t, "create_users", migration.Description)
		require.Equal(t, "20260501143015_create_users.sql", migration.Name)
		require.Equal(
			t,
			[]string{
				"CREATE TABLE users (\n  id INTEGER PRIMARY KEY,\n  email TEXT NOT NULL UNIQUE\n)",
			},
			migration.UpStatements,
		)
		require.Equal(t, []string{"DROP TABLE users"}, migration.DownStatements)
	})

	t.Run("rejects empty up content", func(t *testing.T) {
		_, err := ParseFile("20260501143015_bad.sql", []byte(`
-- litemigrate down
DROP TABLE users;
`))

		require.Error(t, err)
	})

	t.Run("rejects repeated down markers", func(t *testing.T) {
		_, err := ParseFile("20260501143015_bad.sql", []byte(`
CREATE TABLE users (id INTEGER PRIMARY KEY);
-- litemigrate down
DROP TABLE users;
-- litemigrate down
DROP TABLE users;
`))

		require.Error(t, err)
	})
}

func TestLoadDir(t *testing.T) {
	t.Run("loads migrations in version order", func(t *testing.T) {
		dir := t.TempDir()

		writeMigrationFile(
			t,
			dir,
			"20260501143016_create_accounts.sql",
			"CREATE TABLE accounts (id INTEGER PRIMARY KEY);",
		)
		writeMigrationFile(
			t,
			dir,
			"20260501143015_create_users.sql",
			"CREATE TABLE users (id INTEGER PRIMARY KEY);",
		)

		loaded, err := LoadDir(dir)

		require.NoError(t, err)
		require.Len(t, loaded, 2)
		require.Equal(t, "20260501143015_create_users.sql", loaded[0].Name)
		require.Equal(t, "20260501143016_create_accounts.sql", loaded[1].Name)
	})

	t.Run("rejects invalid sql filenames", func(t *testing.T) {
		dir := t.TempDir()

		writeMigrationFile(
			t,
			dir,
			"not-a-migration.sql",
			"CREATE TABLE users (id INTEGER PRIMARY KEY);",
		)

		_, err := LoadDir(dir)

		require.Error(t, err)
	})

	t.Run("rejects duplicate versions", func(t *testing.T) {
		dir := t.TempDir()

		writeMigrationFile(
			t,
			dir,
			"20260501143015_create_users.sql",
			"CREATE TABLE users (id INTEGER PRIMARY KEY);",
		)
		writeMigrationFile(
			t,
			dir,
			"20260501143015_create_accounts.sql",
			"CREATE TABLE accounts (id INTEGER PRIMARY KEY);",
		)

		_, err := LoadDir(dir)

		require.Error(t, err)
	})
}

func writeMigrationFile(t *testing.T, dir, name, contents string) {
	t.Helper()

	path := filepath.Join(dir, name)
	err := os.WriteFile(path, []byte(contents), 0o600)
	require.NoError(t, err)
}
