// Package config loads and validates the runtime configuration of the
// application.
//
// Precedence, highest first: command-line flags, environment variables,
// built-in defaults. MariaDB settings loaded here are a development
// bootstrap only: once the setup wizard exists they are read from the
// SQLite Settings table (Design.md L147-L164, E-11).
package config

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
)

// Defaults mandated by the specification and the implementation plan.
const (
	DefaultPort        = 8080
	DefaultSQLitePath  = "/config/guildbank.db"
	DefaultLogLevel    = "info"
	DefaultLogFormat   = "json"
	DefaultMariaDBPort = 3306
)

// Config is the complete runtime configuration.
type Config struct {
	Port           int
	SQLitePath     string
	LogLevel       string
	LogFormat      string
	TrustedProxies []string
	MariaDB        MariaDB
}

// MariaDB holds the bootstrap connection settings for the guild store.
// Password is never logged or serialized by this package.
type MariaDB struct {
	Host     string
	Port     int
	Database string
	Username string
	Password string
}

// Configured reports whether a MariaDB connection was requested.
func (m MariaDB) Configured() bool { return m.Host != "" }

// Address returns host:port for display and dialing.
func (m MariaDB) Address() string {
	return net.JoinHostPort(m.Host, strconv.Itoa(m.Port))
}

// Redacted returns a log-safe description without the password.
func (m MariaDB) Redacted() string {
	if !m.Configured() {
		return "not configured"
	}
	return fmt.Sprintf("%s@%s/%s", m.Username, m.Address(), m.Database)
}

// Load reads configuration from the environment and the supplied arguments
// (without the program name). It returns flag.ErrHelp for -h/-help.
func Load(args []string, getenv func(string) string) (Config, error) {
	cfg := Config{
		Port:       DefaultPort,
		SQLitePath: DefaultSQLitePath,
		LogLevel:   DefaultLogLevel,
		LogFormat:  DefaultLogFormat,
		MariaDB: MariaDB{
			Port:     DefaultMariaDBPort,
			Database: "GuildBank",
		},
	}

	if err := applyEnv(&cfg, getenv); err != nil {
		return Config{}, err
	}

	fs := flag.NewFlagSet("guildbank", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.IntVar(&cfg.Port, "port", cfg.Port, "HTTP listen port")
	fs.StringVar(&cfg.SQLitePath, "sqlite-path", cfg.SQLitePath, "SQLite database file")
	fs.StringVar(&cfg.LogLevel, "log-level", cfg.LogLevel, "log level: debug, info, warn, error")
	fs.StringVar(&cfg.LogFormat, "log-format", cfg.LogFormat, "log format: json or text")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return Config{}, flag.ErrHelp
		}
		return Config{}, fmt.Errorf("parse flags: %w", err)
	}
	if fs.NArg() > 0 {
		return Config{}, fmt.Errorf("unexpected arguments: %s", strings.Join(fs.Args(), " "))
	}

	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func applyEnv(cfg *Config, getenv func(string) string) error {
	if v := strings.TrimSpace(getenv("GUILDBANK_PORT")); v != "" {
		port, err := strconv.Atoi(v)
		if err != nil {
			return fmt.Errorf("GUILDBANK_PORT: %q is not an integer", v)
		}
		cfg.Port = port
	}
	if v := strings.TrimSpace(getenv("GUILDBANK_SQLITE_PATH")); v != "" {
		cfg.SQLitePath = v
	}
	if v := strings.TrimSpace(getenv("GUILDBANK_LOG_LEVEL")); v != "" {
		cfg.LogLevel = v
	}
	if v := strings.TrimSpace(getenv("GUILDBANK_LOG_FORMAT")); v != "" {
		cfg.LogFormat = v
	}
	if v := strings.TrimSpace(getenv("GUILDBANK_TRUSTED_PROXIES")); v != "" {
		cfg.TrustedProxies = splitList(v)
	}

	// Development bootstrap only; Phase 3 replaces this with Settings.
	cfg.MariaDB.Host = strings.TrimSpace(getenv("GUILDBANK_MARIADB_HOST"))
	if v := strings.TrimSpace(getenv("GUILDBANK_MARIADB_PORT")); v != "" {
		port, err := strconv.Atoi(v)
		if err != nil {
			return fmt.Errorf("GUILDBANK_MARIADB_PORT: %q is not an integer", v)
		}
		cfg.MariaDB.Port = port
	}
	if cfg.MariaDB.Configured() {
		if v := strings.TrimSpace(getenv("GUILDBANK_MARIADB_DATABASE")); v != "" {
			cfg.MariaDB.Database = v
		}
		cfg.MariaDB.Username = strings.TrimSpace(getenv("GUILDBANK_MARIADB_USERNAME"))
		cfg.MariaDB.Password = getenv("GUILDBANK_MARIADB_PASSWORD")
	} else {
		cfg.MariaDB.Port = DefaultMariaDBPort
	}
	return nil
}

func splitList(v string) []string {
	parts := strings.Split(v, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// Validate rejects values that would make the process unusable.
func (c Config) Validate() error {
	if c.Port < 1 || c.Port > 65535 {
		return fmt.Errorf("port %d is outside 1-65535", c.Port)
	}
	if strings.TrimSpace(c.SQLitePath) == "" {
		return errors.New("sqlite path must not be empty")
	}
	if strings.ContainsRune(c.SQLitePath, 0) {
		return errors.New("sqlite path contains a NUL byte")
	}
	switch strings.ToLower(c.LogLevel) {
	case "debug", "info", "warn", "error":
	default:
		return fmt.Errorf("log level %q is not one of debug, info, warn, error", c.LogLevel)
	}
	switch strings.ToLower(c.LogFormat) {
	case "json", "text":
	default:
		return fmt.Errorf("log format %q is not one of json, text", c.LogFormat)
	}
	for _, p := range c.TrustedProxies {
		if net.ParseIP(p) == nil {
			if _, _, err := net.ParseCIDR(p); err != nil {
				return fmt.Errorf("trusted proxy %q is not an IP address or CIDR", p)
			}
		}
	}
	if c.MariaDB.Configured() {
		if c.MariaDB.Port < 1 || c.MariaDB.Port > 65535 {
			return fmt.Errorf("MariaDB port %d is outside 1-65535", c.MariaDB.Port)
		}
		if strings.TrimSpace(c.MariaDB.Database) == "" {
			return errors.New("MariaDB database must not be empty")
		}
		if strings.TrimSpace(c.MariaDB.Username) == "" {
			return errors.New("MariaDB username must not be empty")
		}
	}
	return nil
}

// Usage documents the flags and environment variables for operators.
func Usage() string {
	return `Usage: guildbank [flags]

Flags:
  -port int            HTTP listen port (default 8080)
  -sqlite-path string  SQLite database file (default /config/guildbank.db)
  -log-level string    debug, info, warn or error (default info)
  -log-format string   json or text (default json)

Environment (flags override environment):
  GUILDBANK_PORT, GUILDBANK_SQLITE_PATH, GUILDBANK_LOG_LEVEL,
  GUILDBANK_LOG_FORMAT, GUILDBANK_TRUSTED_PROXIES

Development bootstrap only, superseded by Settings in Phase 3:
  GUILDBANK_MARIADB_HOST, GUILDBANK_MARIADB_PORT,
  GUILDBANK_MARIADB_DATABASE, GUILDBANK_MARIADB_USERNAME,
  GUILDBANK_MARIADB_PASSWORD
`
}
