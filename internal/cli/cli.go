package cli

import (
	"errors"
	"flag"
	"fmt"
	"slices"
	"strings"

	"github.com/varavelio/litemigrate/internal/config"
)

// CommandName identifies a CLI subcommand.
type CommandName string

const (
	// CommandNew creates a new migration file.
	CommandNew CommandName = "new"
	// CommandUp applies pending migrations.
	CommandUp CommandName = "up"
	// CommandDown rolls back applied migrations.
	CommandDown CommandName = "down"
	// CommandStatus reports a short summary of applied and pending migrations.
	CommandStatus CommandName = "status"
	// CommandCompile renders the final schema from local migrations.
	CommandCompile CommandName = "compile"
	// CommandVersion prints the version of the application.
	CommandVersion CommandName = "version"
	// CommandHelp prints usage instructions.
	CommandHelp CommandName = "help"
)

// Parsed contains the parsed CLI command and options.
type Parsed struct {
	// Name is the selected CLI subcommand.
	Name CommandName
	// Flags contains the configuration-style flags collected from the command line.
	Flags config.Flags
	// MigrationName carries the positional name argument used by the new command.
	MigrationName string
	// All enables bulk behavior for commands that support it.
	All bool
}

// Parse parses CLI arguments into a command structure.
func Parse(args []string) (Parsed, error) {
	if len(args) == 0 {
		return Parsed{Name: CommandHelp}, nil
	}

	if args[0] == "--version" || args[0] == "-v" || args[0] == "version" {
		return Parsed{Name: CommandVersion}, nil
	}

	if args[0] == "--help" || args[0] == "-h" || args[0] == "help" {
		return Parsed{Name: CommandHelp}, nil
	}

	prefixLength := prefixFlagLength(args)
	if prefixLength >= len(args) {
		return Parsed{Name: CommandHelp}, nil
	}

	common := config.Flags{}
	if err := parseCommonFlags("litemigrate", args[:prefixLength], &common); err != nil {
		return Parsed{}, err
	}

	parsed := Parsed{
		Name:  CommandName(args[prefixLength]),
		Flags: common,
	}

	if string(parsed.Name) == "--help" || string(parsed.Name) == "-h" ||
		string(parsed.Name) == "help" {
		return Parsed{Name: CommandHelp}, nil
	}

	if string(parsed.Name) == "--version" || string(parsed.Name) == "-v" ||
		string(parsed.Name) == "version" {
		return Parsed{Name: CommandVersion}, nil
	}

	commandArgs := args[prefixLength+1:]

	switch parsed.Name {
	case CommandNew:
		flagSet := newCommandFlagSet(string(CommandNew))
		if err := bindCommonFlags(flagSet, &parsed.Flags).parse(commandArgs); err != nil {
			if errors.Is(err, flag.ErrHelp) {
				return Parsed{Name: CommandHelp}, nil
			}
			return Parsed{}, err
		}
		if flagSet.NArg() != 1 {
			return Parsed{}, errors.New("new requires exactly one migration name")
		}
		parsed.MigrationName = flagSet.Arg(0)
	case CommandUp:
		flagSet := newCommandFlagSet(string(CommandUp))
		binder := bindCommonFlags(flagSet, &parsed.Flags)
		flagSet.BoolVar(&parsed.All, "all", false, "apply all pending migrations")
		if err := binder.parse(commandArgs); err != nil {
			if errors.Is(err, flag.ErrHelp) {
				return Parsed{Name: CommandHelp}, nil
			}
			return Parsed{}, err
		}
		if flagSet.NArg() != 0 {
			return Parsed{}, errors.New("up does not accept positional arguments")
		}
	case CommandDown:
		flagSet := newCommandFlagSet(string(CommandDown))
		binder := bindCommonFlags(flagSet, &parsed.Flags)
		flagSet.BoolVar(&parsed.All, "all", false, "roll back all applied migrations")
		if err := binder.parse(commandArgs); err != nil {
			if errors.Is(err, flag.ErrHelp) {
				return Parsed{Name: CommandHelp}, nil
			}
			return Parsed{}, err
		}
		if flagSet.NArg() != 0 {
			return Parsed{}, errors.New("down does not accept positional arguments")
		}
	case CommandCompile:
		flagSet := newCommandFlagSet(string(CommandCompile))
		binder := bindCommonFlags(flagSet, &parsed.Flags)
		if err := binder.parse(commandArgs); err != nil {
			if errors.Is(err, flag.ErrHelp) {
				return Parsed{Name: CommandHelp}, nil
			}
			return Parsed{}, err
		}
		if flagSet.NArg() != 0 {
			return Parsed{}, errors.New("compile does not accept positional arguments")
		}
	case CommandStatus:
		flagSet := newCommandFlagSet(string(CommandStatus))
		binder := bindCommonFlags(flagSet, &parsed.Flags)
		if err := binder.parse(commandArgs); err != nil {
			if errors.Is(err, flag.ErrHelp) {
				return Parsed{Name: CommandHelp}, nil
			}
			return Parsed{}, err
		}
		if flagSet.NArg() != 0 {
			return Parsed{}, errors.New("status does not accept positional arguments")
		}
	default:
		return Parsed{}, fmt.Errorf("unknown command %q", parsed.Name)
	}

	return parsed, nil
}

// Usage returns the root command usage text.
func Usage() string {
	return strings.TrimSpace(`
Usage:
  litemigrate new <name> [flags]
  litemigrate up [--all] [flags]
  litemigrate down [--all] [flags]
  litemigrate status [flags]
  litemigrate compile [--compile-output path] [flags]
  litemigrate version
  litemigrate help

Common flags:
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
`)
}

type commonFlagBinder struct {
	set          *flag.FlagSet
	flags        *config.Flags
	headersValue *string
}

func bindCommonFlags(flagSet *flag.FlagSet, flags *config.Flags) commonFlagBinder {
	headersValue := formatHeaderList(flags.RQLiteHeaders)
	binder := commonFlagBinder{
		set:          flagSet,
		flags:        flags,
		headersValue: &headersValue,
	}

	flagSet.StringVar(
		&flags.Dotenv,
		"dotenv",
		flags.Dotenv,
		"path to the .env file",
	)
	flagSet.StringVar(
		&flags.ConfigPath,
		"config",
		flags.ConfigPath,
		"path to the configuration file",
	)
	flagSet.StringVar(&flags.Directory, "directory", flags.Directory, "migration directory")
	flagSet.StringVar(
		&flags.CompileOutput,
		"compile-output",
		flags.CompileOutput,
		"compiled schema output path",
	)
	flagSet.StringVar(&flags.RQLiteURL, "rqlite-url", flags.RQLiteURL, "rqlite base URL")
	flagSet.StringVar(
		&flags.RQLiteTimeout,
		"rqlite-timeout",
		flags.RQLiteTimeout,
		"rqlite request timeout",
	)
	flagSet.StringVar(
		&flags.RQLiteUsername,
		"rqlite-username",
		flags.RQLiteUsername,
		"rqlite basic auth username",
	)
	flagSet.StringVar(
		&flags.RQLitePassword,
		"rqlite-password",
		flags.RQLitePassword,
		"rqlite basic auth password",
	)
	flagSet.StringVar(
		binder.headersValue,
		"rqlite-headers",
		headersValue,
		"additional rqlite HTTP headers in Key=Value,Key=Value format",
	)
	flagSet.StringVar(
		&flags.NSQLiteDSN,
		"nsqlite-dsn",
		flags.NSQLiteDSN,
		"nsqlite database/sql DSN",
	)
	flagSet.StringVar(
		&flags.NSQLiteTimeout,
		"nsqlite-timeout",
		flags.NSQLiteTimeout,
		"nsqlite operation timeout",
	)

	return binder
}

func (binder commonFlagBinder) parse(args []string) error {
	if err := binder.set.Parse(args); err != nil {
		return err
	}
	binder.flags.RQLiteHeaders = parseHeaderList(*binder.headersValue)
	return nil
}

func newCommandFlagSet(name string) *flag.FlagSet {
	flagSet := flag.NewFlagSet(name, flag.ContinueOnError)
	flagSet.SetOutput(ioDiscard{})
	return flagSet
}

func prefixFlagLength(args []string) int {
	for index := 0; index < len(args); {
		argument := args[index]
		if !strings.HasPrefix(argument, "-") || argument == "-" {
			return index
		}

		if consumesRootValue(argument) {
			if strings.Contains(argument, "=") {
				index++
				continue
			}
			if index+1 >= len(args) {
				return len(args)
			}
			index += 2
			continue
		}

		return index
	}

	return len(args)
}

func parseCommonFlags(name string, args []string, flags *config.Flags) error {
	flagSet := newCommandFlagSet(name)
	binder := bindCommonFlags(flagSet, flags)
	return binder.parse(args)
}

func consumesRootValue(argument string) bool {
	for _, name := range []string{
		"--dotenv",
		"--config",
		"--directory",
		"--compile-output",
		"--rqlite-url",
		"--rqlite-timeout",
		"--rqlite-username",
		"--rqlite-password",
		"--rqlite-headers",
		"--nsqlite-dsn",
		"--nsqlite-timeout",
	} {
		if argument == name || strings.HasPrefix(argument, name+"=") {
			return true
		}
	}
	return false
}

func formatHeaderList(headers map[string]string) string {
	if len(headers) == 0 {
		return ""
	}

	keys := make([]string, 0, len(headers))
	for key := range headers {
		keys = append(keys, key)
	}
	slices.Sort(keys)

	parts := make([]string, 0, len(headers))
	for _, key := range keys {
		value := headers[key]
		parts = append(parts, fmt.Sprintf("%s=%s", key, value))
	}

	return strings.Join(parts, ",")
}

func parseHeaderList(value string) map[string]string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}

	headers := make(map[string]string)
	for _, pair := range strings.Split(value, ",") {
		pair = strings.TrimSpace(pair)
		if pair == "" {
			continue
		}

		key, rest, ok := strings.Cut(pair, "=")
		if !ok {
			continue
		}

		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}

		headers[key] = strings.TrimSpace(rest)
	}

	return headers
}

type ioDiscard struct{}

func (ioDiscard) Write(values []byte) (int, error) {
	return len(values), nil
}
