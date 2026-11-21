// Package sqlite owns the application store (/config/guildbank.db): Users,
// Settings, Sessions and AuditOutbox. No guild data is written here
// (Design.md L78-L91, D-13).
package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Panazule/guildbank/internal/domain"

	_ "modernc.org/sqlite" // registers the "sqlite" driver (pure Go, CGO_ENABLED=0)
)

// DriverName is the database/sql driver registered by modernc.org/sqlite.
const DriverName = "sqlite"

// Store is the SQLite application store.
type Store struct {
	db   *sql.DB
	path string
}

// Open creates the parent directory when needed, opens the database with the
// pragmas mandated by IC-08 (WAL, busy timeout, foreign keys) and verifies
// connectivity. A single connection is used: SQLite serializes writers and
// this avoids "database is locked" under the session-touch workload.
func Open(ctx context.Context, path string) (*Store, error) {
	if strings.TrimSpace(path) == "" {
		return nil, errors.New("sqlite: path must not be empty")
	}
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o750); err != nil {
			return nil, fmt.Errorf("sqlite: create directory %s: %w", dir, err)
		}
	}
	db, err := sql.Open(DriverName, dataSourceName(path))
	if err != nil {
		return nil, fmt.Errorf("sqlite: open %s: %w", path, err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(0)

	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := db.PingContext(pingCtx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("sqlite: ping %s: %w", path, err)
	}
	return &Store{db: db, path: path}, nil
}

// dataSourceName builds a SQLite URI. Pragmas:
// busy_timeout(5000) waits instead of failing on a locked writer;
// journal_mode(WAL) allows concurrent readers;
// synchronous(NORMAL) is safe with WAL; foreign_keys(1) enforces constraints.
func dataSourceName(path string) string {
	p := filepath.ToSlash(path)
	if !strings.HasPrefix(p, "/") {
		p = "/" + p // Windows: C:/x -> /C:/x
	}
	q := url.Values{}
	q.Add("_pragma", "busy_timeout(5000)")
	q.Add("_pragma", "journal_mode(WAL)")
	q.Add("_pragma", "synchronous(NORMAL)")
	q.Add("_pragma", "foreign_keys(1)")
	return "file://" + p + "?" + q.Encode()
}

// DB exposes the pool to repositories in this package only.
func (s *Store) DB() *sql.DB { return s.db }

// Path returns the configured database file.
func (s *Store) Path() string { return s.path }

// PingContext implements service.Pinger.
func (s *Store) PingContext(ctx context.Context) error { return s.db.PingContext(ctx) }

// SetupStatus implements the D-02 probe: setup exists exactly while the
// Users table contains no administrator. A missing Users table means the
// first-run schema has not been installed yet, so setup is still pending.
// Administrators are counted regardless of DisabledFg so disabling every
// account cannot reopen unauthenticated setup (B-6).
func (s *Store) SetupStatus(ctx context.Context) (domain.SetupState, error) {
	var tables int
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(1) FROM sqlite_master WHERE type = 'table' AND name = 'Users'`).Scan(&tables)
	if err != nil {
		return domain.SetupUnknown, fmt.Errorf("sqlite: inspect schema: %w", err)
	}
	if tables == 0 {
		return domain.SetupPending, nil
	}
	var admins int
	err = s.db.QueryRowContext(ctx,
		`SELECT COUNT(1) FROM Users WHERE AccessLevel = 'Admin'`).Scan(&admins)
	if err != nil {
		return domain.SetupUnknown, fmt.Errorf("sqlite: count administrators: %w", err)
	}
	if admins == 0 {
		return domain.SetupPending, nil
	}
	return domain.SetupComplete, nil
}

// Close checkpoints the WAL and closes the pool (IP §7.3 step 6).
func (s *Store) Close() error {
	if s.db == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := s.db.ExecContext(ctx, `PRAGMA wal_checkpoint(TRUNCATE)`); err != nil {
		_ = s.db.Close()
		return fmt.Errorf("sqlite: checkpoint: %w", err)
	}
	return s.db.Close()
}
