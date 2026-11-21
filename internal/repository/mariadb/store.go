// Package mariadb owns the guild store (database GuildBank): Items,
// GuildBankItems, ImportBatch, ImportList and Audit. Items is externally
// owned and read-only for this application; runtime grants enforce that
// (Design.md L242-L244, SR-13).
package mariadb

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net"
	"time"

	"github.com/go-sql-driver/mysql"
)

// DriverName is the database/sql driver registered by go-sql-driver/mysql.
const DriverName = "mysql"

// Config is the connection contract consumed by Open. It mirrors the
// MariaDB settings stored by the setup wizard; no defaults are applied here.
type Config struct {
	Host     string
	Port     int
	Database string
	Username string
	Password string
}

// Store is the MariaDB guild store.
type Store struct {
	db *sql.DB
}

// Open builds a bounded pool, pings the server and returns an error when it
// is unreachable. The caller decides whether an unreachable store is fatal
// (Phase 0: not fatal, readiness reports it).
func Open(ctx context.Context, cfg Config) (*Store, error) {
	if cfg.Host == "" || cfg.Database == "" || cfg.Username == "" {
		return nil, errors.New("mariadb: host, database and username are required")
	}
	if cfg.Port < 1 || cfg.Port > 65535 {
		return nil, fmt.Errorf("mariadb: port %d is outside 1-65535", cfg.Port)
	}

	my := mysql.NewConfig()
	my.Net = "tcp"
	my.Addr = net.JoinHostPort(cfg.Host, fmt.Sprint(cfg.Port))
	my.DBName = cfg.Database
	my.User = cfg.Username
	my.Passwd = cfg.Password
	my.ParseTime = true
	my.Loc = time.UTC
	my.Collation = "utf8mb4_unicode_ci"
	my.Timeout = 5 * time.Second
	my.ReadTimeout = 30 * time.Second
	my.WriteTimeout = 30 * time.Second
	my.Params = map[string]string{"charset": "utf8mb4"}

	db, err := sql.Open(DriverName, my.FormatDSN())
	if err != nil {
		return nil, fmt.Errorf("mariadb: open: %w", err)
	}
	db.SetMaxOpenConns(20)
	db.SetMaxIdleConns(10)
	db.SetConnMaxLifetime(30 * time.Minute)
	db.SetConnMaxIdleTime(5 * time.Minute)

	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := db.PingContext(pingCtx); err != nil {
		_ = db.Close()
		// The driver error does not contain the password; addresses are
		// safe to log and are required for diagnosis.
		return nil, fmt.Errorf("mariadb: ping %s: %w", my.Addr, err)
	}
	return &Store{db: db}, nil
}

// DB exposes the pool to repositories in this package only.
func (s *Store) DB() *sql.DB { return s.db }

// PingContext implements service.Pinger.
func (s *Store) PingContext(ctx context.Context) error { return s.db.PingContext(ctx) }

// Close releases the pool.
func (s *Store) Close() error {
	if s.db == nil {
		return nil
	}
	return s.db.Close()
}
