package config

import (
	"errors"
	"flag"
	"strings"
	"testing"
)

func envFrom(pairs map[string]string) func(string) string {
	return func(k string) string { return pairs[k] }
}

func TestLoadDefaults(t *testing.T) {
	cfg, err := Load(nil, envFrom(nil))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Port != DefaultPort || cfg.SQLitePath != DefaultSQLitePath {
		t.Fatalf("unexpected defaults: %+v", cfg)
	}
	if cfg.LogLevel != DefaultLogLevel || cfg.LogFormat != DefaultLogFormat {
		t.Fatalf("unexpected log defaults: %+v", cfg)
	}
	if cfg.MariaDB.Configured() {
		t.Fatalf("MariaDB must be unconfigured by default")
	}
}

func TestLoadPrecedenceFlagsOverEnv(t *testing.T) {
	env := envFrom(map[string]string{
		"GUILDBANK_PORT":        "9000",
		"GUILDBANK_SQLITE_PATH": "/data/env.db",
		"GUILDBANK_LOG_LEVEL":   "warn",
	})
	cfg, err := Load([]string{"-port", "9100", "-sqlite-path", "/data/flag.db", "-log-level", "debug"}, env)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Port != 9100 || cfg.SQLitePath != "/data/flag.db" || cfg.LogLevel != "debug" {
		t.Fatalf("flags did not override env: %+v", cfg)
	}
}

func TestLoadEnvironment(t *testing.T) {
	env := envFrom(map[string]string{
		"GUILDBANK_PORT":             "8081",
		"GUILDBANK_SQLITE_PATH":      "/config/test.db",
		"GUILDBANK_LOG_FORMAT":       "text",
		"GUILDBANK_TRUSTED_PROXIES":  "10.0.0.0/8, 192.168.1.5",
		"GUILDBANK_MARIADB_HOST":     "db.internal",
		"GUILDBANK_MARIADB_PORT":     "3307",
		"GUILDBANK_MARIADB_DATABASE": "GuildBank",
		"GUILDBANK_MARIADB_USERNAME": "guildbank",
		"GUILDBANK_MARIADB_PASSWORD": "s3cret",
	})
	cfg, err := Load(nil, env)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Port != 8081 || cfg.LogFormat != "text" {
		t.Fatalf("env not applied: %+v", cfg)
	}
	if len(cfg.TrustedProxies) != 2 {
		t.Fatalf("trusted proxies: %+v", cfg.TrustedProxies)
	}
	if !cfg.MariaDB.Configured() || cfg.MariaDB.Port != 3307 {
		t.Fatalf("mariadb env not applied: %+v", cfg.MariaDB)
	}
	if strings.Contains(cfg.MariaDB.Redacted(), "s3cret") {
		t.Fatalf("Redacted leaked the password: %s", cfg.MariaDB.Redacted())
	}
}

func TestLoadRejectsInvalidValues(t *testing.T) {
	tests := []struct {
		name string
		args []string
		env  map[string]string
		want string
	}{
		{"port range", []string{"-port", "0"}, nil, "outside 1-65535"},
		{"port not integer", nil, map[string]string{"GUILDBANK_PORT": "http"}, "not an integer"},
		{"empty sqlite", []string{"-sqlite-path", " "}, nil, "sqlite path"},
		{"bad level", []string{"-log-level", "verbose"}, nil, "log level"},
		{"bad format", []string{"-log-format", "xml"}, nil, "log format"},
		{"bad proxy", nil, map[string]string{"GUILDBANK_TRUSTED_PROXIES": "not-a-cidr"}, "trusted proxy"},
		{"mariadb without user", nil, map[string]string{"GUILDBANK_MARIADB_HOST": "db"}, "username"},
		{"mariadb bad port", nil, map[string]string{"GUILDBANK_MARIADB_HOST": "db", "GUILDBANK_MARIADB_PORT": "70000", "GUILDBANK_MARIADB_USERNAME": "u"}, "MariaDB port"},
		{"positional args", []string{"serve"}, nil, "unexpected arguments"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Load(tc.args, envFrom(tc.env))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("got %v, want error containing %q", err, tc.want)
			}
		})
	}
}

func TestLoadHelp(t *testing.T) {
	_, err := Load([]string{"-h"}, envFrom(nil))
	if !errors.Is(err, flag.ErrHelp) {
		t.Fatalf("got %v, want flag.ErrHelp", err)
	}
}
