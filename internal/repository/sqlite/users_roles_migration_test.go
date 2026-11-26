package sqlite

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/Panazule/guildbank/internal/domain"
)

func adminActorForTest() domain.Actor {
	return domain.Actor{UserID: 1, UserName: "admin", IsAdmin: true, Role: domain.RoleAdmin}
}

// insertUserForTest adds one account with its own bcrypt hash and returns its ID.
func insertUserForTest(t *testing.T, store *Store, name string, role domain.Role,
	disabled bool) int64 {
	t.Helper()
	hash, err := bcrypt.GenerateFromPassword([]byte("a sufficiently long password"), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("bcrypt: %v", err)
	}
	now := formatTime(time.Date(2026, 11, 26, 12, 0, 0, 0, time.UTC))
	res, err := store.DB().Exec(`
		INSERT INTO Users (UserName, PasswordHash, AccessLevel, DisabledFg, ModAt, CreatedAt, ModBy)
		VALUES (?, ?, ?, ?, ?, ?, 'test')`,
		name, string(hash), string(role), boolInt(disabled), now, now)
	if err != nil {
		t.Fatalf("insert %s: %v", name, err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("id for %s: %v", name, err)
	}
	return id
}

// TestUserRolesMigrationUpgrade replays the v1 schema, then applies the role
// migration to a populated database.
func TestUserRolesMigrationUpgrade(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, filepath.Join(t.TempDir(), "guildbank.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	body, err := migrationFiles.ReadFile("migrations/0001_phase1.sql")
	if err != nil {
		t.Fatalf("read 0001: %v", err)
	}
	if err := store.applyMigration(ctx, 1, string(body)); err != nil {
		t.Fatalf("apply 0001: %v", err)
	}
	now := time.Date(2026, 11, 26, 12, 0, 0, 0, time.UTC)
	firstID := insertUserForTest(t, store, "Keeper", domain.RoleAdmin, false)
	secondID := insertUserForTest(t, store, "Retired", domain.RoleAdmin, true)
	if _, err := store.DB().Exec(`
		INSERT INTO Sessions (SessionID, TokenHash, UserID, CreatedAt, LastAccessed,
		                      ExpiresAt, AbsoluteExpiresAt)
		VALUES ('s1', 'h1', ?, ?, ?, ?, ?)`,
		firstID, formatTime(now), formatTime(now), formatTime(now.Add(time.Hour)),
		formatTime(now.Add(time.Hour))); err != nil {
		t.Fatalf("insert session: %v", err)
	}

	version, err := store.Migrate(ctx)
	if err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	if want := latestSchemaVersion(t); version != want {
		t.Fatalf("version = %d, want %d", version, want)
	}

	// Accounts survive with their identity, hash, flags and role.
	first, err := store.GetUserByID(ctx, firstID)
	if err != nil {
		t.Fatalf("GetUserByID: %v", err)
	}
	if first.UserName != "Keeper" || !first.IsAdmin() || first.Disabled {
		t.Fatalf("first user changed: %+v", first)
	}
	second, err := store.GetUserByID(ctx, secondID)
	if err != nil {
		t.Fatalf("GetUserByID(second): %v", err)
	}
	if !second.Disabled || second.UserName != "Retired" {
		t.Fatalf("disabled administrator changed: %+v", second)
	}

	// The rebuild drops Sessions through the ON DELETE CASCADE (approved).
	var sessions int
	if err := store.DB().QueryRowContext(ctx, `SELECT COUNT(1) FROM Sessions`).Scan(&sessions); err != nil {
		t.Fatalf("count sessions: %v", err)
	}
	if sessions != 0 {
		t.Fatalf("sessions = %d, want 0 after the rebuild", sessions)
	}

	// The widened CHECK accepts every approved role and still rejects others.
	insertUserForTest(t, store, "Officer One", domain.RoleOfficer, false)
	insertUserForTest(t, store, "Member One", domain.RoleMember, false)
	if _, err := store.DB().Exec(`
		INSERT INTO Users (UserName, PasswordHash, AccessLevel, ModAt, CreatedAt)
		VALUES ('Root', 'x', 'Root', '2026-11-26T12:00:00Z', '2026-11-26T12:00:00Z')`); err == nil {
		t.Fatal("the rebuilt CHECK accepted an unknown access level")
	}

	// AUTOINCREMENT continuity: a new account gets an ID above every existing row.
	newID := insertUserForTest(t, store, "Latest", domain.RoleMember, false)
	if newID <= secondID {
		t.Fatalf("new UserID = %d, want greater than %d", newID, secondID)
	}

	// The case-insensitive unique index survives the rebuild.
	if _, err := store.DB().Exec(`
		INSERT INTO Users (UserName, PasswordHash, AccessLevel, ModAt, CreatedAt)
		VALUES ('keeper', 'x', 'Member', '2026-11-26T12:00:00Z', '2026-11-26T12:00:00Z')`); err == nil {
		t.Fatal("duplicate username was accepted case-insensitively")
	}
}
