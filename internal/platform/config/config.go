// Package config loads and validates the runtime configuration of the
// application.
//
// Precedence, highest first: command-line flags, environment variables,
// built-in defaults. MariaDB settings loaded here are a development
// bootstrap only: the setup wizard persists credentials in SQLite, but the
// live pool still uses this bootstrap; Phase 3 did not adopt Settings-backed
// connections (PD-18, E-11).
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
	// DefaultMariaDBInitAttempts bounds guild-store initialization attempts
	// (migrations and startup repairs) before the process exits non-zero.
	// Connectivity failures are not bounded; they are retried until the
	// server answers.
	DefaultMariaDBInitAttempts = 3
)

// Config is the complete runtime configuration.
type Config struct {
	Port           int
	SQLitePath     string
	LogLevel       string
	LogFormat      string
	TrustedProxies []string
	SecureCookies  bool
	MariaDB        MariaDB
	// ConsolidateItems runs the QTY-09 item-stack consolidation maintenance
	// pass and exits instead of serving HTTP. ConsolidateDryRun reports the
	// groups without writing, and ConsolidateActor is recorded in the audit.
	ConsolidateItems  bool
	ConsolidateDryRun bool
	ConsolidateActor  string
}

// MariaDB holds the bootstrap connection settings for the guild store.
// Password is never logged or serialized by this package.
type MariaDB struct {
	Host     string
	Port     int
	Database string
	Username string
	Password string
	// InitAttempts bounds guild-store initialization (migration) attempts
	// before the process exits non-zero. Connectivity failures are retried
	// without limit, so a database that starts late is simply waited for.
	InitAttempts int
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
		Port:             DefaultPort,
		SQLitePath:       DefaultSQLitePath,
		LogLevel:         DefaultLogLevel,
		LogFormat:        DefaultLogFormat,
		ConsolidateActor: "system",
		MariaDB: MariaDB{
			Port:         DefaultMariaDBPort,
			Database:     "GuildBank",
			InitAttempts: DefaultMariaDBInitAttempts,
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
	fs.BoolVar(&cfg.SecureCookies, "secure-cookies", cfg.SecureCookies, "mark session and CSRF cookies Secure (enable behind HTTPS)")
	fs.IntVar(&cfg.MariaDB.InitAttempts, "mariadb-init-attempts", cfg.MariaDB.InitAttempts,
		"guild-store initialization attempts before exiting non-zero")
	fs.BoolVar(&cfg.ConsolidateItems, "consolidate-items", cfg.ConsolidateItems,
		"consolidate active duplicate item stacks and exit")
	fs.BoolVar(&cfg.ConsolidateDryRun, "dry-run", cfg.ConsolidateDryRun,
		"report consolidation groups without writing (requires -consolidate-items)")
	fs.StringVar(&cfg.ConsolidateActor, "consolidate-actor", cfg.ConsolidateActor,
		"audit username recorded by -consolidate-items")
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
	if v := strings.TrimSpace(getenv("GUILDBANK_SECURE_COOKIES")); v != "" {
		secure, err := strconv.ParseBool(v)
		if err != nil {
			return fmt.Errorf("GUILDBANK_SECURE_COOKIES: %q is not a boolean", v)
		}
		cfg.SecureCookies = secure
	}

	// Development bootstrap only; Settings-backed adoption remains a separate
	// approved change (PD-18).
	cfg.MariaDB.Host = strings.TrimSpace(getenv("GUILDBANK_MARIADB_HOST"))
	if v := strings.TrimSpace(getenv("GUILDBANK_MARIADB_PORT")); v != "" {
		port, err := strconv.Atoi(v)
		if err != nil {
			return fmt.Errorf("GUILDBANK_MARIADB_PORT: %q is not an integer", v)
		}
		cfg.MariaDB.Port = port
	}
	if v := strings.TrimSpace(getenv("GUILDBANK_MARIADB_INIT_ATTEMPTS")); v != "" {
		attempts, err := strconv.Atoi(v)
		if err != nil {
			return fmt.Errorf("GUILDBANK_MARIADB_INIT_ATTEMPTS: %q is not an integer", v)
		}
		cfg.MariaDB.InitAttempts = attempts
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
	if c.ConsolidateDryRun && !c.ConsolidateItems {
		return errors.New("-dry-run requires -consolidate-items")
	}
	if c.ConsolidateItems && strings.TrimSpace(c.ConsolidateActor) == "" {
		return errors.New("consolidate actor must not be empty")
	}
	if c.ConsolidateItems && !c.MariaDB.Configured() {
		return errors.New("-consolidate-items requires a configured MariaDB guild store")
	}
	if c.MariaDB.Configured() {
		if c.MariaDB.Port < 1 || c.MariaDB.Port > 65535 {
			return fmt.Errorf("MariaDB port %d is outside 1-65535", c.MariaDB.Port)
		}
		if c.MariaDB.InitAttempts < 1 || c.MariaDB.InitAttempts > 100 {
			return fmt.Errorf("MariaDB init attempts %d is outside 1-100", c.MariaDB.InitAttempts)
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
  -secure-cookies      mark cookies Secure; enable when served over HTTPS
  -mariadb-init-attempts int
                       guild-store initialization attempts before exiting (default 3)
  -consolidate-items   consolidate active duplicate item stacks to one row and exit
  -dry-run             report -consolidate-items groups without writing
  -consolidate-actor   audit username for -consolidate-items (default system)

Environment (flags override environment):
  GUILDBANK_PORT, GUILDBANK_SQLITE_PATH, GUILDBANK_LOG_LEVEL,
  GUILDBANK_LOG_FORMAT, GUILDBANK_TRUSTED_PROXIES, GUILDBANK_SECURE_COOKIES

Development bootstrap only, superseded by Settings in Phase 3:
  GUILDBANK_MARIADB_HOST, GUILDBANK_MARIADB_PORT,
  GUILDBANK_MARIADB_DATABASE, GUILDBANK_MARIADB_USERNAME,
  GUILDBANK_MARIADB_PASSWORD, GUILDBANK_MARIADB_INIT_ATTEMPTS
`
}
