package config

import (
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/joho/godotenv"
	"gopkg.in/yaml.v3"
)

const (
	defaultDriver    = "nsqlite"
	defaultDirectory = "./migrations"
	defaultTimeout   = 30 * time.Second
)

// Config contains the fully merged application configuration.
type Config struct {
	// Driver selects the runtime backend used to execute migrations.
	Driver string
	// Directory points to the directory that contains migration files.
	Directory string
	// Compile contains settings for the compile command.
	Compile CompileConfig
	// RQLite contains the rqlite-specific runtime settings.
	RQLite RQLiteConfig
	// NSQLite contains the nsqlite-specific runtime settings.
	NSQLite NSQLiteConfig
}

// CompileConfig contains compile command settings.
type CompileConfig struct {
	// Output is the optional file path that receives compiled schema output.
	Output string
}

// RQLiteConfig contains the rqlite driver settings.
type RQLiteConfig struct {
	// URL is the base HTTP address of the target rqlite node.
	URL string
	// Timeout bounds each outbound rqlite HTTP request.
	Timeout time.Duration
	// Username is the optional HTTP basic auth username.
	Username string
	// Password is the optional HTTP basic auth password.
	Password string
	// Headers carries additional HTTP headers sent with every rqlite request.
	Headers map[string]string
}

// NSQLiteConfig contains the nsqlite driver settings.
type NSQLiteConfig struct {
	// DSN is the nsqlite database/sql connection string.
	DSN string
}

// Flags contains flag-derived configuration overrides.
type Flags struct {
	// Dotenv points to an explicit .env file.
	Dotenv string
	// ConfigPath points to an explicit configuration file.
	ConfigPath string
	// Driver overrides the configured runtime backend.
	Driver string
	// Directory overrides the configured migration directory.
	Directory string
	// CompileOutput overrides compile.output.
	CompileOutput string
	// RQLiteURL overrides rqlite.url.
	RQLiteURL string
	// RQLiteTimeout overrides rqlite.timeout.
	RQLiteTimeout string
	// RQLiteUsername overrides rqlite.username.
	RQLiteUsername string
	// RQLitePassword overrides rqlite.password.
	RQLitePassword string
	// RQLiteHeaders overrides rqlite.headers.
	RQLiteHeaders map[string]string
	// NSQLiteDSN overrides nsqlite.dsn.
	NSQLiteDSN string
}

// Loader resolves configuration from defaults, YAML, environment variables, and flags.
type Loader struct {
	// LookupEnv reads an environment variable.
	LookupEnv func(string) (string, bool)
	// ReadFile reads a file from disk.
	ReadFile func(string) ([]byte, error)
	// Stat inspects a filesystem path.
	Stat func(string) (os.FileInfo, error)
	// Getwd returns the current working directory.
	Getwd func() (string, error)
}

type rawConfig struct {
	Dotenv    string           `yaml:"dotenv"`
	Driver    string           `yaml:"driver"`
	Directory string           `yaml:"directory"`
	Compile   rawCompileConfig `yaml:"compile"`
	RQLite    rawRQLiteConfig  `yaml:"rqlite"`
	NSQLite   rawNSQLiteConfig `yaml:"nsqlite"`
}

type rawCompileConfig struct {
	Output string `yaml:"output"`
}

type rawRQLiteConfig struct {
	URL      string            `yaml:"url"`
	Timeout  string            `yaml:"timeout"`
	Username string            `yaml:"username"`
	Password string            `yaml:"password"`
	Headers  map[string]string `yaml:"headers"`
}

type rawNSQLiteConfig struct {
	DSN string `yaml:"dsn"`
}

// NewLoader returns the default configuration loader.
func NewLoader() Loader {
	return Loader{
		LookupEnv: os.LookupEnv,
		ReadFile:  os.ReadFile,
		Stat:      os.Stat,
		Getwd:     os.Getwd,
	}
}

// Load resolves configuration using flags, environment variables, YAML, and defaults.
func (loader Loader) Load(flags Flags) (Config, error) {
	config := Config{
		Driver:    defaultDriver,
		Directory: defaultDirectory,
		Compile: CompileConfig{
			Output: "",
		},
		RQLite: RQLiteConfig{
			Timeout: defaultTimeout,
			Headers: map[string]string{},
		},
	}

	configPath, explicitConfig, err := loader.resolveConfigPath(flags)
	if err != nil {
		return Config{}, err
	}

	var fileConfig rawConfig
	if configPath != "" {
		var loadErr error
		fileConfig, loadErr = loader.loadFileConfig(configPath)
		if loadErr != nil {
			return Config{}, loadErr
		}
	} else if explicitConfig {
		return Config{}, fmt.Errorf("config file %q does not exist", flags.ConfigPath)
	}

	envPath, envExplicit, err := loader.resolveDotenvPath(flags, fileConfig)
	if err != nil {
		return Config{}, err
	}

	envMap := make(map[string]string)
	if envPath != "" {
		content, err := loader.ReadFile(envPath)
		if err != nil {
			if envExplicit || !errors.Is(err, os.ErrNotExist) {
				return Config{}, fmt.Errorf("read dotenv file %q: %w", envPath, err)
			}
		} else {
			parsed, err := godotenv.Unmarshal(string(content))
			if err != nil {
				return Config{}, fmt.Errorf("parse dotenv file %q: %w", envPath, err)
			}
			envMap = parsed
		}
	}

	lookupEnv := func(key string) (string, bool) {
		if val, ok := envMap[key]; ok {
			return val, true
		}
		return loader.LookupEnv(key)
	}

	expand := func(val string) string {
		trimmed := strings.TrimSpace(val)
		if strings.HasPrefix(trimmed, "env:") {
			key := strings.TrimSpace(strings.TrimPrefix(trimmed, "env:"))
			if res, ok := lookupEnv(key); ok {
				return res
			}
			return val
		}
		return val
	}

	if configPath != "" {
		if err := applyRawConfig(&config, fileConfig, expand); err != nil {
			return Config{}, fmt.Errorf("apply config file %q: %w", configPath, err)
		}
	}

	if err := applyRawConfig(&config, loader.loadEnvConfig(lookupEnv), expand); err != nil {
		return Config{}, fmt.Errorf("apply environment configuration: %w", err)
	}
	if err := applyRawConfig(&config, rawFromFlags(flags), expand); err != nil {
		return Config{}, fmt.Errorf("apply flag configuration: %w", err)
	}

	return config, nil
}

func (loader Loader) resolveDotenvPath(flags Flags, fileConfig rawConfig) (string, bool, error) {
	dotenvPath := strings.TrimSpace(flags.Dotenv)
	if dotenvPath == "" {
		if value, ok := loader.LookupEnv("LITEMIGRATE_DOTENV"); ok {
			dotenvPath = strings.TrimSpace(value)
		}
	}
	if dotenvPath == "" {
		dotenvPath = strings.TrimSpace(fileConfig.Dotenv)
	}

	if dotenvPath != "" {
		if !filepath.IsAbs(dotenvPath) {
			workingDir, err := loader.Getwd()
			if err != nil {
				return "", false, fmt.Errorf("get working directory: %w", err)
			}
			dotenvPath = filepath.Join(workingDir, dotenvPath)
		}
		if _, err := loader.Stat(dotenvPath); err != nil {
			return "", true, fmt.Errorf("stat dotenv file %q: %w", dotenvPath, err)
		}
		return dotenvPath, true, nil
	}

	workingDir, err := loader.Getwd()
	if err != nil {
		return "", false, fmt.Errorf("get working directory: %w", err)
	}

	path := filepath.Join(workingDir, ".env")
	if _, statErr := loader.Stat(path); statErr == nil {
		return path, false, nil
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return "", false, fmt.Errorf("stat dotenv file %q: %w", path, statErr)
	}

	return path, false, nil
}

func (loader Loader) resolveConfigPath(flags Flags) (string, bool, error) {
	configPath := strings.TrimSpace(flags.ConfigPath)
	if configPath == "" {
		if value, ok := loader.LookupEnv("LITEMIGRATE_CONFIG"); ok {
			configPath = strings.TrimSpace(value)
		}
	}
	if configPath != "" {
		if _, err := loader.Stat(configPath); err != nil {
			return "", true, fmt.Errorf("stat config file %q: %w", configPath, err)
		}
		return configPath, true, nil
	}

	workingDir, err := loader.Getwd()
	if err != nil {
		return "", false, fmt.Errorf("get working directory: %w", err)
	}

	for _, candidate := range []string{"litemigrate.yaml", "litemigrate.yml"} {
		path := filepath.Join(workingDir, candidate)
		if _, statErr := loader.Stat(path); statErr == nil {
			return path, false, nil
		} else if !errors.Is(statErr, os.ErrNotExist) {
			return "", false, fmt.Errorf("stat config file %q: %w", path, statErr)
		}
	}

	return "", false, nil
}

func (loader Loader) loadFileConfig(path string) (rawConfig, error) {
	contents, err := loader.ReadFile(path)
	if err != nil {
		return rawConfig{}, fmt.Errorf("read config file %q: %w", path, err)
	}

	var loaded rawConfig
	if err := yaml.Unmarshal(contents, &loaded); err != nil {
		return rawConfig{}, fmt.Errorf("decode config file %q: %w", path, err)
	}

	return loaded, nil
}

func (loader Loader) loadEnvConfig(lookupEnv func(string) (string, bool)) rawConfig {
	return rawConfig{
		Dotenv:    readEnv(lookupEnv, "LITEMIGRATE_DOTENV"),
		Driver:    readEnv(lookupEnv, "LITEMIGRATE_DRIVER"),
		Directory: readEnv(lookupEnv, "LITEMIGRATE_DIRECTORY"),
		Compile: rawCompileConfig{
			Output: readEnv(lookupEnv, "LITEMIGRATE_COMPILE_OUTPUT"),
		},
		RQLite: rawRQLiteConfig{
			URL:      readEnv(lookupEnv, "LITEMIGRATE_RQLITE_URL"),
			Timeout:  readEnv(lookupEnv, "LITEMIGRATE_RQLITE_TIMEOUT"),
			Username: readEnv(lookupEnv, "LITEMIGRATE_RQLITE_USERNAME"),
			Password: readEnv(lookupEnv, "LITEMIGRATE_RQLITE_PASSWORD"),
			Headers:  parseHeaderList(readEnv(lookupEnv, "LITEMIGRATE_RQLITE_HEADERS")),
		},
		NSQLite: rawNSQLiteConfig{
			DSN: readEnv(lookupEnv, "LITEMIGRATE_NSQLITE_DSN"),
		},
	}
}

func rawFromFlags(flags Flags) rawConfig {
	return rawConfig{
		Dotenv:    flags.Dotenv,
		Driver:    flags.Driver,
		Directory: flags.Directory,
		Compile: rawCompileConfig{
			Output: flags.CompileOutput,
		},
		RQLite: rawRQLiteConfig{
			URL:      flags.RQLiteURL,
			Timeout:  flags.RQLiteTimeout,
			Username: flags.RQLiteUsername,
			Password: flags.RQLitePassword,
			Headers:  copyMap(flags.RQLiteHeaders),
		},
		NSQLite: rawNSQLiteConfig{
			DSN: flags.NSQLiteDSN,
		},
	}
}

func applyRawConfig(config *Config, raw rawConfig, expand func(string) string) error {
	if val := strings.TrimSpace(raw.Driver); val != "" {
		config.Driver = strings.TrimSpace(expand(val))
	}
	if val := strings.TrimSpace(raw.Directory); val != "" {
		config.Directory = strings.TrimSpace(expand(val))
	}
	if val := strings.TrimSpace(raw.Compile.Output); val != "" {
		config.Compile.Output = strings.TrimSpace(expand(val))
	}
	if val := strings.TrimSpace(raw.RQLite.URL); val != "" {
		config.RQLite.URL = strings.TrimSpace(expand(val))
	}
	if val := strings.TrimSpace(raw.RQLite.Timeout); val != "" {
		expandedTimeout := strings.TrimSpace(expand(val))
		if expandedTimeout != "" {
			parsed, err := time.ParseDuration(expandedTimeout)
			if err != nil {
				return fmt.Errorf("parse rqlite timeout %q: %w", expandedTimeout, err)
			}
			config.RQLite.Timeout = parsed
		}
	}
	if val := strings.TrimSpace(raw.RQLite.Username); val != "" {
		config.RQLite.Username = strings.TrimSpace(expand(val))
	}
	if val := strings.TrimSpace(raw.RQLite.Password); val != "" {
		config.RQLite.Password = strings.TrimSpace(expand(val))
	}
	if len(raw.RQLite.Headers) > 0 {
		if config.RQLite.Headers == nil {
			config.RQLite.Headers = map[string]string{}
		}
		for key, value := range raw.RQLite.Headers {
			trimmedKey := strings.TrimSpace(expand(key))
			if trimmedKey == "" {
				continue
			}
			config.RQLite.Headers[trimmedKey] = strings.TrimSpace(expand(value))
		}
	}
	if val := strings.TrimSpace(raw.NSQLite.DSN); val != "" {
		config.NSQLite.DSN = strings.TrimSpace(expand(val))
	}

	return nil
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

func copyMap(values map[string]string) map[string]string {
	if len(values) == 0 {
		return nil
	}

	cloned := make(map[string]string, len(values))
	maps.Copy(cloned, values)

	return cloned
}

func readEnv(lookupEnv func(string) (string, bool), key string) string {
	if lookupEnv == nil {
		return ""
	}

	value, ok := lookupEnv(key)
	if !ok {
		return ""
	}

	return strings.TrimSpace(value)
}
