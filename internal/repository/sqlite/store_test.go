package sqlite

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/Panazule/guildbank/internal/domain"
)

func openTestStore(t *testing.T) *Store {
	t.Helper()
	path := filepath.Join(t.TempDir(), "nested", "guildbank.db")
	store, err := Open(context.Background(), path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func TestOpenAppliesPragmas(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	var journal string
	if err := store.DB().QueryRowContext(ctx, `PRAGMA journal_mode`).Scan(&journal); err != nil {
		t.Fatalf("journal_mode: %v", err)
	}
	if journal != "wal" {
		t.Fatalf("journal_mode = %q, want wal", journal)
	}

	var busy int
	if err := store.DB().QueryRowContext(ctx, `PRAGMA busy_timeout`).Scan(&busy); err != nil {
		t.Fatalf("busy_timeout: %v", err)
	}
	if busy != 5000 {
		t.Fatalf("busy_timeout = %d, want 5000", busy)
	}

	var fk int
	if err := store.DB().QueryRowContext(ctx, `PRAGMA foreign_keys`).Scan(&fk); err != nil {
		t.Fatalf("foreign_keys: %v", err)
	}
	if fk != 1 {
		t.Fatalf("foreign_keys = %d, want 1", fk)
	}

	if err := store.PingContext(ctx); err != nil {
		t.Fatalf("PingContext: %v", err)
	}
}

func TestSetupStatusFollowsUsersTable(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	state, err := store.SetupStatus(ctx)
	if err != nil {
		t.Fatalf("SetupStatus on empty store: %v", err)
	}
	if state != domain.SetupPending {
		t.Fatalf("state = %v, want pending", state)
	}

	if _, err := store.DB().ExecContext(ctx, `
		CREATE TABLE Users (
			UserID      INTEGER PRIMARY KEY AUTOINCREMENT,
			UserName    TEXT NOT NULL,
			AccessLevel TEXT NOT NULL,
			DisabledFg  INTEGER NOT NULL DEFAULT 0
		)`); err != nil {
		t.Fatalf("create Users: %v", err)
	}
	state, err = store.SetupStatus(ctx)
	if err != nil || state != domain.SetupPending {
		t.Fatalf("empty Users: state=%v err=%v, want pending/nil", state, err)
	}

	if _, err := store.DB().ExecContext(ctx,
		`INSERT INTO Users (UserName, AccessLevel) VALUES ('admin', 'Admin')`); err != nil {
		t.Fatalf("insert admin: %v", err)
	}
	state, err = store.SetupStatus(ctx)
	if err != nil || state != domain.SetupComplete {
		t.Fatalf("admin exists: state=%v err=%v, want complete/nil", state, err)
	}

	if _, err := store.DB().ExecContext(ctx, `UPDATE Users SET DisabledFg = 1`); err != nil {
		t.Fatalf("disable admin: %v", err)
	}
	state, err = store.SetupStatus(ctx)
	if err != nil || state != domain.SetupComplete {
		t.Fatalf("disabled admin must not reopen setup: state=%v err=%v", state, err)
	}
}

func TestCloseCheckpoints(t *testing.T) {
	path := filepath.Join(t.TempDir(), "guildbank.db")
	store, err := Open(context.Background(), path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if _, err := store.DB().Exec(`CREATE TABLE t (id INTEGER PRIMARY KEY)`); err != nil {
		t.Fatalf("exec: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := store.PingContext(context.Background()); err == nil {
		t.Fatal("PingContext after Close succeeded")
	}
}
