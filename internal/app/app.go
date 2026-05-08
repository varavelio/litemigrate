package app

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/varavelio/litemigrate/internal/cli"
	"github.com/varavelio/litemigrate/internal/compiler"
	"github.com/varavelio/litemigrate/internal/config"
	"github.com/varavelio/litemigrate/internal/drivers"
	"github.com/varavelio/litemigrate/internal/drivers/nsqlite"
	"github.com/varavelio/litemigrate/internal/drivers/rqlite"
	"github.com/varavelio/litemigrate/internal/migrations"
	"github.com/varavelio/litemigrate/internal/store"
	"github.com/varavelio/litemigrate/internal/version"
)

// App coordinates CLI parsing, configuration loading, and command execution.
type App struct {
	// Stdout receives successful command output.
	Stdout io.Writer
	// Stderr receives auxiliary command output.
	Stderr io.Writer
	// ConfigLoader resolves configuration from files, environment variables, and flags.
	ConfigLoader config.Loader
	// OpenDriver constructs the runtime driver used by execution commands.
	OpenDriver func(config.Config) (drivers.Driver, error)
	// Now returns the current time used for timestamped operations.
	Now func() time.Time
}

// New creates an application with production defaults.
func New(stdout, stderr io.Writer) *App {
	return &App{
		Stdout:       stdout,
		Stderr:       stderr,
		ConfigLoader: config.NewLoader(),
		OpenDriver:   openDriver,
		Now:          func() time.Time { return time.Now().UTC() },
	}
}

// Run executes the application for the provided CLI arguments.
func (app *App) Run(ctx context.Context, args []string) error {
	parsed, err := cli.Parse(args)
	if err != nil {
		return fmt.Errorf("parse CLI arguments: %w", err)
	}

	loadedConfig, err := app.ConfigLoader.Load(parsed.Flags)
	if err != nil {
		return fmt.Errorf("load configuration: %w", err)
	}

	switch parsed.Name {
	case cli.CommandNew:
		return app.runNew(loadedConfig, parsed.MigrationName)
	case cli.CommandUp:
		return app.runUp(ctx, loadedConfig, parsed.All)
	case cli.CommandDown:
		return app.runDown(ctx, loadedConfig, parsed.All)
	case cli.CommandStatus:
		return app.runStatus(ctx, loadedConfig)
	case cli.CommandCompile:
		return app.runCompile(loadedConfig)
	case cli.CommandVersion:
		return app.runVersion()
	case cli.CommandHelp:
		return app.runHelp()
	default:
		return fmt.Errorf("unsupported command %q", parsed.Name)
	}
}

func (app *App) runNew(cfg config.Config, name string) error {
	fileName, err := migrations.NewFileName(app.Now(), name)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(cfg.Directory, 0o755); err != nil {
		return fmt.Errorf("create migration directory %q: %w", cfg.Directory, err)
	}

	path := filepath.Join(cfg.Directory, fileName)
	contents := strings.TrimSpace(`
-- Write your migration's forward changes here.

-- litemigrate down

-- Write your migration's rollback changes here.
`) + "\n"

	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		return fmt.Errorf("write migration file %q: %w", path, err)
	}

	_, err = fmt.Fprintln(app.Stdout, path)
	return err
}

func (app *App) runUp(ctx context.Context, cfg config.Config, all bool) error {
	database, err := app.openValidatedDriver(cfg)
	if err != nil {
		return err
	}
	defer func() {
		_ = database.Close()
	}()

	migrationStore := store.New(database)
	if err := migrationStore.Ensure(ctx); err != nil {
		return err
	}

	files, err := migrations.LoadDir(cfg.Directory)
	if err != nil {
		return err
	}

	applied, err := migrationStore.ListApplied(ctx)
	if err != nil {
		return err
	}

	appliedVersions := make(map[string]struct{}, len(applied))
	for _, record := range applied {
		appliedVersions[record.Version] = struct{}{}
	}

	pending := make([]migrations.File, 0, len(files))
	for _, file := range files {
		if _, ok := appliedVersions[file.Version]; ok {
			continue
		}
		pending = append(pending, file)
	}

	if len(pending) == 0 {
		_, err = fmt.Fprintln(app.Stdout, "no pending migrations")
		return err
	}

	selected := pending[:1]
	if all {
		selected = pending
	}

	for _, file := range selected {
		if err := database.Exec(ctx, file.UpStatements, true); err != nil {
			return fmt.Errorf("apply migration %q: %w", file.Name, err)
		}
		if err := migrationStore.Insert(ctx, file.Version, file.Name, app.Now()); err != nil {
			return err
		}
	}

	_, err = fmt.Fprintf(
		app.Stdout,
		"applied %d %s\n",
		len(selected),
		pluralize("migration", len(selected)),
	)
	return err
}

func (app *App) runDown(ctx context.Context, cfg config.Config, all bool) error {
	database, err := app.openValidatedDriver(cfg)
	if err != nil {
		return err
	}
	defer func() {
		_ = database.Close()
	}()

	migrationStore := store.New(database)
	if err := migrationStore.Ensure(ctx); err != nil {
		return err
	}

	files, err := migrations.LoadDir(cfg.Directory)
	if err != nil {
		return err
	}

	localByVersion := make(map[string]migrations.File, len(files))
	for _, file := range files {
		localByVersion[file.Version] = file
	}

	applied, err := migrationStore.ListApplied(ctx)
	if err != nil {
		return err
	}
	if len(applied) == 0 {
		return fmt.Errorf("no applied migrations")
	}

	selected := reverseRecords(applied)
	if !all {
		selected = selected[:1]
	}

	for _, record := range selected {
		file, ok := localByVersion[record.Version]
		if !ok {
			return fmt.Errorf("applied migration %q is missing locally", record.Name)
		}
		if len(file.DownStatements) == 0 {
			return fmt.Errorf("migration %q has no down migration", file.Name)
		}

		if err := database.Exec(ctx, file.DownStatements, true); err != nil {
			return fmt.Errorf("roll back migration %q: %w", file.Name, err)
		}
		if err := migrationStore.Delete(ctx, file.Version); err != nil {
			return err
		}
	}

	_, err = fmt.Fprintf(
		app.Stdout,
		"rolled back %d %s\n",
		len(selected),
		pluralize("migration", len(selected)),
	)
	return err
}

func (app *App) runCompile(cfg config.Config) error {
	driverName, err := cfg.ResolveDriver()
	if err != nil {
		return err
	}
	cfg.Driver = driverName

	files, err := migrations.LoadDir(cfg.Directory)
	if err != nil {
		return err
	}

	schema, err := compiler.Compile(files)
	if err != nil {
		return err
	}

	if cfg.Compile.Output == "" {
		if _, err := io.WriteString(app.Stdout, schema); err != nil {
			return fmt.Errorf("write compiled schema to stdout: %w", err)
		}
		if schema != "" && !strings.HasSuffix(schema, "\n") {
			_, err = fmt.Fprintln(app.Stdout)
			return err
		}
		return nil
	}

	if err := os.MkdirAll(filepath.Dir(cfg.Compile.Output), 0o755); err != nil {
		return fmt.Errorf("create compile output directory: %w", err)
	}
	if err := os.WriteFile(cfg.Compile.Output, []byte(schema+"\n"), 0o600); err != nil {
		return fmt.Errorf("write compiled schema file %q: %w", cfg.Compile.Output, err)
	}

	_, err = fmt.Fprintln(app.Stdout, cfg.Compile.Output)
	return err
}

func (app *App) runStatus(ctx context.Context, cfg config.Config) error {
	database, err := app.openValidatedDriver(cfg)
	if err != nil {
		return err
	}
	defer func() {
		_ = database.Close()
	}()

	migrationStore := store.New(database)
	if err := migrationStore.Ensure(ctx); err != nil {
		return err
	}

	files, err := migrations.LoadDir(cfg.Directory)
	if err != nil {
		return err
	}

	applied, err := migrationStore.ListApplied(ctx)
	if err != nil {
		return err
	}

	appliedVersions := make(map[string]struct{}, len(applied))
	for _, record := range applied {
		appliedVersions[record.Version] = struct{}{}
	}

	pending := make([]migrations.File, 0, len(files))
	for _, file := range files {
		if _, ok := appliedVersions[file.Version]; ok {
			continue
		}
		pending = append(pending, file)
	}

	lastApplied := "none"
	if len(applied) > 0 {
		lastApplied = applied[len(applied)-1].Name
	}

	nextPending := "none"
	if len(pending) > 0 {
		nextPending = pending[0].Name
	}

	_, err = fmt.Fprintf(
		app.Stdout,
		"Applied: %d\nPending: %d\nLast applied: %s\nNext pending: %s\n",
		len(applied),
		len(pending),
		lastApplied,
		nextPending,
	)
	return err
}

func (app *App) runVersion() error {
	_, err := fmt.Fprintf(
		app.Stdout,
		"litemigrate version %s, commit %s, built at %s\n",
		version.Version,
		version.Commit,
		version.Date,
	)
	return err
}

func (app *App) runHelp() error {
	_, err := fmt.Fprintln(app.Stdout, cli.Usage())
	return err
}

func (app *App) openValidatedDriver(cfg config.Config) (drivers.Driver, error) {
	driverName, err := cfg.ResolveDriver()
	if err != nil {
		return nil, err
	}
	cfg.Driver = driverName

	switch driverName {
	case "rqlite":
		if strings.TrimSpace(cfg.RQLite.URL) == "" {
			return nil, fmt.Errorf("rqlite URL must not be empty")
		}
	case "nsqlite":
		if strings.TrimSpace(cfg.NSQLite.DSN) == "" {
			return nil, fmt.Errorf("nsqlite DSN must not be empty")
		}
	}
	return app.OpenDriver(cfg)
}

func openDriver(cfg config.Config) (drivers.Driver, error) {
	driverName, err := cfg.ResolveDriver()
	if err != nil {
		return nil, err
	}
	cfg.Driver = driverName

	switch driverName {
	case "rqlite":
		return rqlite.New(rqlite.Config{
			URL:      cfg.RQLite.URL,
			Timeout:  cfg.RQLite.Timeout,
			Username: cfg.RQLite.Username,
			Password: cfg.RQLite.Password,
			Headers:  cfg.RQLite.Headers,
		})
	case "nsqlite":
		return nsqlite.New(nsqlite.Config{
			DSN:     cfg.NSQLite.DSN,
			Timeout: cfg.NSQLite.Timeout,
		})
	default:
		return nil, fmt.Errorf("unsupported driver %q", driverName)
	}
}

func reverseRecords(records []store.Record) []store.Record {
	reversed := make([]store.Record, len(records))
	for index := range records {
		reversed[index] = records[len(records)-1-index]
	}
	return reversed
}

func pluralize(noun string, count int) string {
	if count == 1 {
		return noun
	}
	return noun + "s"
}
