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
	"sync"
	"time"

	"github.com/Panazule/guildbank/internal/domain"
	"github.com/go-sql-driver/mysql"
)

// DriverName is the database/sql driver registered by go-sql-driver/mysql.
const DriverName = "mysql"

// openPingTimeout bounds the eager connectivity check performed by Open.
const openPingTimeout = 5 * time.Second

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
//
// One Store lives for the whole process. The pool it wraps reconnects on its
// own, so a Store built by OpenPool stays usable while MariaDB is unreachable
// and becomes usable as soon as the server returns; readiness is published by
// Run after Initialize succeeds.
type Store struct {
	db *sql.DB

	// statusMu guards the supervisor-published readiness state. Requests read
	// it through Ready, which is intentionally cheap (no ping per request).
	statusMu  sync.RWMutex
	ready     bool
	statusErr error
	fatalErr  error
	fatalCh   chan struct{}

	// stackability is the temporary OD-01 EQUID allow-list. It is in-process
	// configuration, not a schema table; an absent EQUID is unknown.
	stackabilityMu sync.RWMutex
	stackability   map[uint64]domain.Stackability
}

// Open builds a bounded pool, pings the server and returns an error when it
// is unreachable. The caller decides whether an unreachable store is fatal.
//
// The application uses OpenPool plus Run instead, so a server that is still
// starting does not discard the pool. Open remains the eager constructor for
// the setup wizard and the opt-in integration tests.
func Open(ctx context.Context, cfg Config) (*Store, error) {
	db, addr, err := openPool(cfg)
	if err != nil {
		return nil, err
	}

	pingCtx, cancel := context.WithTimeout(ctx, openPingTimeout)
	defer cancel()
	if err := db.PingContext(pingCtx); err != nil {
		_ = db.Close()
		// The driver error does not contain the password; addresses are
		// safe to log and are required for diagnosis.
		return nil, fmt.Errorf("mariadb: ping %s: %w", addr, err)
	}
	store := &Store{db: db, fatalCh: make(chan struct{})}
	store.markReady()
	return store, nil
}

// OpenPool builds the bounded pool without dialing. It returns a usable Store
// even when the server is unreachable, because database/sql dials on first use
// and re-dials after a connection is lost. Only invalid configuration is fatal.
//
// Callers must run the supervisor (Run) so the schema is initialized before
// readiness is published.
func OpenPool(_ context.Context, cfg Config) (*Store, error) {
	db, _, err := openPool(cfg)
	if err != nil {
		return nil, err
	}
	return &Store{db: db, fatalCh: make(chan struct{})}, nil
}

// openPool validates the configuration and builds the bounded pool. It never
// dials, so a DNS name that does not resolve yet is not an error here.
func openPool(cfg Config) (*sql.DB, string, error) {
	if cfg.Host == "" || cfg.Database == "" || cfg.Username == "" {
		return nil, "", errors.New("mariadb: host, database and username are required")
	}
	if cfg.Port < 1 || cfg.Port > 65535 {
		return nil, "", fmt.Errorf("mariadb: port %d is outside 1-65535", cfg.Port)
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
	my.ReadTimeout = 120 * time.Second
	my.WriteTimeout = 120 * time.Second
	my.Params = map[string]string{"charset": "utf8mb4"}

	db, err := sql.Open(DriverName, my.FormatDSN())
	if err != nil {
		return nil, "", fmt.Errorf("mariadb: open: %w", err)
	}
	db.SetMaxOpenConns(20)
	db.SetMaxIdleConns(10)
	db.SetConnMaxLifetime(30 * time.Minute)
	db.SetConnMaxIdleTime(5 * time.Minute)
	return db, my.Addr, nil
}

// Test opens a bounded connection, pings it and closes. It is used by the
// setup wizard to validate credentials without persisting anything.
func Test(ctx context.Context, cfg Config) error {
	store, err := Open(ctx, cfg)
	if err != nil {
		return err
	}
	return store.Close()
}

// DB exposes the pool to repositories in this package only.
func (s *Store) DB() *sql.DB { return s.db }

// PingContext implements service.Pinger.
func (s *Store) PingContext(ctx context.Context) error {
	if s == nil || s.db == nil {
		return errors.New("mariadb: store is not open")
	}
	return s.db.PingContext(ctx)
}

// Ready reports whether the supervisor has published a successful
// initialization and has not observed a lost connection since.
func (s *Store) Ready() bool {
	ready, _ := s.Status()
	return ready
}

// Status reports the supervisor's current view: ready is true only after a
// successful Initialize, and lastErr is the failure that made the store
// unavailable (a connectivity error or an initialization error).
func (s *Store) Status() (ready bool, lastErr error) {
	if s == nil || s.db == nil {
		return false, errors.New("mariadb: store is not open")
	}
	s.statusMu.RLock()
	defer s.statusMu.RUnlock()
	return s.ready, s.statusErr
}

// markReady publishes readiness. It is called by the supervisor after
// Initialize succeeds and by the eager Open constructor.
func (s *Store) markReady() {
	s.statusMu.Lock()
	defer s.statusMu.Unlock()
	s.ready = true
	s.statusErr = nil
}

// markUnavailable withdraws readiness and records why.
func (s *Store) markUnavailable(err error) {
	s.statusMu.Lock()
	defer s.statusMu.Unlock()
	s.ready = false
	s.statusErr = err
}

// recordFatal stores a terminal supervisor failure and closes fatalCh so
// WaitReady returns it instead of polling until its deadline.
func (s *Store) recordFatal(err error) {
	s.statusMu.Lock()
	s.fatalErr = err
	s.statusMu.Unlock()
	if s.fatalCh == nil {
		return
	}
	select {
	case <-s.fatalCh:
	default:
		close(s.fatalCh)
	}
}

// fatalError returns the terminal supervisor failure, if any.
func (s *Store) fatalError() error {
	s.statusMu.RLock()
	defer s.statusMu.RUnlock()
	return s.fatalErr
}

// WaitReady blocks until the store is ready, the supervisor fails fatally, or
// ctx ends. It returns the last status error rather than a bare deadline.
func (s *Store) WaitReady(ctx context.Context) error {
	ticker := time.NewTicker(waitReadyPollInterval)
	defer ticker.Stop()
	for {
		ready, lastErr := s.Status()
		if ready {
			return nil
		}
		select {
		case <-ctx.Done():
			if lastErr != nil {
				return lastErr
			}
			return ctx.Err()
		case <-s.fatalCh:
			return s.fatalError()
		case <-ticker.C:
		}
	}
}

// Close releases the pool.
func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}
