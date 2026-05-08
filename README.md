# Lite Migrate

<p>
  <a href="https://github.com/varavelio/litemigrate/actions">
    <img src="https://github.com/varavelio/litemigrate/actions/workflows/ci.yaml/badge.svg" alt="CI status"/>
  </a>
  <a href="https://goreportcard.com/report/varavelio/litemigrate">
    <img src="https://goreportcard.com/badge/varavelio/litemigrate" alt="Go Report Card"/>
  </a>
  <a href="https://github.com/varavelio/litemigrate/releases/latest">
    <img src="https://img.shields.io/github/release/varavelio/litemigrate.svg" alt="Release Version"/>
  </a>
  <a href="LICENSE">
    <img src="https://img.shields.io/github/license/varavelio/litemigrate.svg" alt="License"/>
  </a>
  <a href="https://github.com/varavelio/litemigrate">
    <img src="https://img.shields.io/github/stars/varavelio/litemigrate?style=flat&label=github+stars"/>
  </a>
</p>

<p>
  <a href="https://varavel.com">
    <img src="https://cdn.jsdelivr.net/gh/varavelio/brand@1.0.0/dist/badges/project.svg" alt="A Varavel project"/>
  </a>
</p>

`litemigrate` is a language agnostic schema migration tool built for NSQLite and other SQLite-compatible databases.

`litemigrate` supports `nsqlite` and `rqlite`. The runtime is inferred from the configured settings.

## Features

1. One `.sql` file per migration.
2. Implicit `up` section and optional `down` section in the same file.
3. Atomic execution of each migration.
4. `up` and `down` operate on one migration by default.
5. `--all` opt-in for bulk apply or bulk rollback.
6. `compile` command to materialize the final SQLite schema locally.
7. Configuration through flags, environment variables, and YAML.

## Commands

```text
litemigrate new <name>
litemigrate up [--all]
litemigrate down [--all]
litemigrate status
litemigrate compile [--compile-output path]
```

## Migration Format

Migration filenames use UTC timestamps with second precision:

```text
YYYYMMDDHHMMSS_<name>.sql
```

Example:

```text
20260501143015_create_users.sql
```

Migration contents use implicit `up` SQL and optional `down` SQL separated by the literal marker `-- litemigrate down`.

Each statement must be a complete SQLite statement terminated the way SQLite expects.

```sql
CREATE TABLE users (
  id INTEGER PRIMARY KEY,
  email TEXT NOT NULL UNIQUE
);

CREATE INDEX idx_users_email ON users(email);

-- litemigrate down

DROP INDEX idx_users_email;
DROP TABLE users;
```

If the `down` marker is absent, the migration is considered irreversible.

## Configuration Precedence

Configuration is resolved in this order:

1. CLI flags
2. Environment variables
3. YAML config file
4. Internal defaults

If `--config` is not provided, `litemigrate` automatically looks for `litemigrate.yaml` or `litemigrate.yml` in the current working directory.

You can also use a `.env` file to load variables. `litemigrate` automatically looks for a `.env` file in the current directory, or you can specify a path using the `--dotenv` flag, the `LITEMIGRATE_DOTENV` environment variable, or the `dotenv` key in your YAML config.
Variables loaded from `.env` or the environment can be injected into your YAML configuration using `env:VAR_NAME`.

Flags and environment variables intentionally mirror the YAML structure:

1. YAML keys use nesting, such as `nsqlite.dsn` or `rqlite.url`.
2. Environment variables replace dots with underscores, such as `LITEMIGRATE_NSQLITE_DSN` or `LITEMIGRATE_RQLITE_URL`.
3. Flags replace dots with hyphens, such as `--nsqlite-dsn` or `--rqlite-url`.

## YAML Configuration

```yaml
dotenv: ./.env
directory: ./migrations

compile:
  output: ./schema.sql

nsqlite:
  dsn: "env:NSQLITE_DSN"
  timeout: 30s
```

The NSQLite DSN uses the same format as the NSQLite Go driver:

```yaml
nsqlite:
  dsn: "http://localhost:9876?authToken=secret"
  timeout: 30s
```

To use rqlite, configure its HTTP endpoint:

```yaml
rqlite:
  url: "http://localhost:4001"
  timeout: 30s
  username: admin
  password: "pass"
  headers:
    X-Environment: local
```

## Environment Variables

```text
LITEMIGRATE_DOTENV
LITEMIGRATE_CONFIG
LITEMIGRATE_DIRECTORY
LITEMIGRATE_COMPILE_OUTPUT
LITEMIGRATE_NSQLITE_DSN
LITEMIGRATE_NSQLITE_TIMEOUT
LITEMIGRATE_RQLITE_URL
LITEMIGRATE_RQLITE_TIMEOUT
LITEMIGRATE_RQLITE_USERNAME
LITEMIGRATE_RQLITE_PASSWORD
LITEMIGRATE_RQLITE_HEADERS
```

`LITEMIGRATE_RQLITE_HEADERS` accepts comma-separated `Key=Value` pairs.

Example:

```text
LITEMIGRATE_RQLITE_HEADERS=Authorization=Bearer token,X-Trace-ID=abc123
```

## Common Flags

```text
--dotenv <path>
--config <path>
--directory <path>
--compile-output <path>
--nsqlite-dsn <dsn>
--nsqlite-timeout <duration>
--rqlite-url <url>
--rqlite-timeout <duration>
--rqlite-username <value>
--rqlite-password <value>
--rqlite-headers <Key=Value,Key=Value>
```

## Examples

Create a migration:

```bash
litemigrate new create_users
```

Apply the next pending migration:

```bash
litemigrate up --directory ./migrations --nsqlite-dsn 'http://localhost:9876?authToken=secret'
```

Apply the next pending migration with an explicit NSQLite timeout:

```bash
litemigrate up --directory ./migrations --nsqlite-dsn 'http://localhost:9876?authToken=secret' --nsqlite-timeout 45s
```

Apply all pending migrations:

```bash
litemigrate up --all --directory ./migrations --nsqlite-dsn 'http://localhost:9876?authToken=secret'
```

Roll back the most recent migration:

```bash
litemigrate down --directory ./migrations --nsqlite-dsn 'http://localhost:9876?authToken=secret'
```

Compile the final schema to stdout:

```bash
litemigrate compile --directory ./migrations
```

Compile the final schema to a file:

```bash
litemigrate compile --directory ./migrations --compile-output ./schema.sql
```

Show a short migration summary:

```bash
litemigrate status --directory ./migrations --nsqlite-dsn 'http://localhost:9876?authToken=secret'
```

If both `nsqlite` and `rqlite` settings are configured at the same time, `litemigrate` fails fast instead of guessing.

Example output:

```text
Applied: 12
Pending: 2
Last applied: 20260501143015_create_users.sql
Next pending: 20260501143110_add_accounts.sql
```

## Metadata Table

Applied migrations are tracked in the target database using the internal table:

```text
_litemigrate_migrations
```

This table is excluded from `compile` output.

## License

This project is released under the MIT License. See [LICENSE](LICENSE).
