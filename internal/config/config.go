package config

import (
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const (
	defaultDriver    = "rqlite"
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

// Flags contains flag-derived configuration overrides.
type Flags struct {
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
	Driver    string           `yaml:"driver"`
	Directory string           `yaml:"directory"`
	Compile   rawCompileConfig `yaml:"compile"`
	RQLite    rawRQLiteConfig  `yaml:"rqlite"`
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

	configPath, explicit, err := loader.resolveConfigPath(flags)
	if err != nil {
		return Config{}, err
	}
	if configPath != "" {
		fileConfig, loadErr := loader.loadFileConfig(configPath)
		if loadErr != nil {
			return Config{}, loadErr
		}
		if err := applyRawConfig(&config, fileConfig); err != nil {
			return Config{}, fmt.Errorf("apply config file %q: %w", configPath, err)
		}
	} else if explicit {
		return Config{}, fmt.Errorf("config file %q does not exist", flags.ConfigPath)
	}

	if err := applyRawConfig(&config, loader.loadEnvConfig()); err != nil {
		return Config{}, fmt.Errorf("apply environment configuration: %w", err)
	}
	if err := applyRawConfig(&config, rawFromFlags(flags)); err != nil {
		return Config{}, fmt.Errorf("apply flag configuration: %w", err)
	}

	return config, nil
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

func (loader Loader) loadEnvConfig() rawConfig {
	return rawConfig{
		Driver:    readEnv(loader.LookupEnv, "LITEMIGRATE_DRIVER"),
		Directory: readEnv(loader.LookupEnv, "LITEMIGRATE_DIRECTORY"),
		Compile: rawCompileConfig{
			Output: readEnv(loader.LookupEnv, "LITEMIGRATE_COMPILE_OUTPUT"),
		},
		RQLite: rawRQLiteConfig{
			URL:      readEnv(loader.LookupEnv, "LITEMIGRATE_RQLITE_URL"),
			Timeout:  readEnv(loader.LookupEnv, "LITEMIGRATE_RQLITE_TIMEOUT"),
			Username: readEnv(loader.LookupEnv, "LITEMIGRATE_RQLITE_USERNAME"),
			Password: readEnv(loader.LookupEnv, "LITEMIGRATE_RQLITE_PASSWORD"),
			Headers:  parseHeaderList(readEnv(loader.LookupEnv, "LITEMIGRATE_RQLITE_HEADERS")),
		},
	}
}

func rawFromFlags(flags Flags) rawConfig {
	return rawConfig{
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
	}
}

func applyRawConfig(config *Config, raw rawConfig) error {
	if strings.TrimSpace(raw.Driver) != "" {
		config.Driver = strings.TrimSpace(raw.Driver)
	}
	if strings.TrimSpace(raw.Directory) != "" {
		config.Directory = strings.TrimSpace(raw.Directory)
	}
	if strings.TrimSpace(raw.Compile.Output) != "" {
		config.Compile.Output = strings.TrimSpace(raw.Compile.Output)
	}
	if strings.TrimSpace(raw.RQLite.URL) != "" {
		config.RQLite.URL = strings.TrimSpace(raw.RQLite.URL)
	}
	if strings.TrimSpace(raw.RQLite.Timeout) != "" {
		parsed, err := time.ParseDuration(strings.TrimSpace(raw.RQLite.Timeout))
		if err != nil {
			return fmt.Errorf("parse rqlite timeout %q: %w", raw.RQLite.Timeout, err)
		}
		config.RQLite.Timeout = parsed
	}
	if strings.TrimSpace(raw.RQLite.Username) != "" {
		config.RQLite.Username = strings.TrimSpace(raw.RQLite.Username)
	}
	if strings.TrimSpace(raw.RQLite.Password) != "" {
		config.RQLite.Password = strings.TrimSpace(raw.RQLite.Password)
	}
	if len(raw.RQLite.Headers) > 0 {
		if config.RQLite.Headers == nil {
			config.RQLite.Headers = map[string]string{}
		}
		for key, value := range raw.RQLite.Headers {
			trimmedKey := strings.TrimSpace(key)
			if trimmedKey == "" {
				continue
			}
			config.RQLite.Headers[trimmedKey] = strings.TrimSpace(value)
		}
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
