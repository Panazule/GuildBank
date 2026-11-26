package sqlite

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/Panazule/guildbank/internal/domain"
)

// TestPasswordCapabilityMigrationBackfills replays 0001-0004 against a populated
// database, then applies 0005 alone. Every row defaults to 1 and only the rows
// whose Origin says SelfRegistration are corrected to 0, so an account that
// predates the capability flag keeps its password and nobody is signed out.
func TestPasswordCapabilityMigrationBackfills(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, filepath.Join(t.TempDir(), "guildbank.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	for version, name := range []string{
		"migrations/0001_phase1.sql",
		"migrations/0002_user_roles.sql",
		"migrations/0003_user_profiles.sql",
		"migrations/0004_self_registration.sql",
	} {
		body, err := migrationFiles.ReadFile(name)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		if err := store.applyMigration(ctx, version+1, string(body)); err != nil {
			t.Fatalf("apply %s: %v", name, err)
		}
	}

	now := time.Date(2026, 11, 26, 12, 0, 0, 0, time.UTC)
	// An administrator-created account: Origin stays NULL, so 0005 defaults it
	// to a password-capable account, which is exact.
	keeperID := insertUserForTest(t, store, "Keeper", domain.RoleAdmin, false)
	// A self-registered account carries a discarded bcrypt hash and no password
	// the owner knows, so 0005 must correct it to 0.
	discardedHash, err := hashForTest("a discarded random secret")
	if err != nil {
		t.Fatalf("hashForTest: %v", err)
	}
	res, err := store.DB().Exec(`
		INSERT INTO Users (UserName, PasswordHash, AccessLevel, DisabledFg, ModAt, CreatedAt, ModBy,
		                   Origin, AccountStatus)
		VALUES ('keldor', ?, 'Member', 0, ?, ?, 'test', 'SelfRegistration', 'Approved')`,
		discardedHash, formatTime(now), formatTime(now))
	if err != nil {
		t.Fatalf("insert self-registered row: %v", err)
	}
	registeredID, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("registered id: %v", err)
	}

	if _, err := store.DB().Exec(`
		INSERT INTO Sessions (SessionID, TokenHash, UserID, CreatedAt, LastAccessed,
		                      ExpiresAt, AbsoluteExpiresAt)
		VALUES ('s1', 'h1', ?, ?, ?, ?, ?)`,
		keeperID, formatTime(now), formatTime(now), formatTime(now.Add(time.Hour)),
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

	keeper, err := store.GetUserByID(ctx, keeperID)
	if err != nil {
		t.Fatalf("GetUserByID(keeper): %v", err)
	}
	if !keeper.HasPassword {
		t.Fatal("an administrator-created account lost its password capability")
	}
	registered, err := store.GetUserByID(ctx, registeredID)
	if err != nil {
		t.Fatalf("GetUserByID(registered): %v", err)
	}
	if registered.HasPassword {
		t.Fatal("a self-registered account was marked password-capable")
	}
	if !registered.IsApproved() || !registered.IsEnabled() {
		t.Fatalf("the migration changed the self-registered account state: %+v", registered)
	}

	// 0005 is metadata-only: the session written before the upgrade survives.
	var sessions int
	if err := store.DB().QueryRowContext(ctx, `SELECT COUNT(1) FROM Sessions`).Scan(&sessions); err != nil {
		t.Fatalf("count sessions: %v", err)
	}
	if sessions != 1 {
		t.Fatalf("sessions = %d, want 1 (no cascade)", sessions)
	}

	// The CHECK keeps a value outside the capability domain out of the table.
	if _, err := store.DB().Exec(`UPDATE Users SET HasPassword = 2 WHERE UserID = ?`, keeperID); err == nil {
		t.Fatal("the CHECK accepted HasPassword = 2")
	}

	// The transition is monotone: establishing a password marks the account and
	// no later write lowers the flag.
	newHash, err := hashForTest("a brand new password")
	if err != nil {
		t.Fatalf("hashForTest: %v", err)
	}
	if _, err := store.SetOwnPassword(ctx, registeredID, newHash, now, ""); err != nil {
		t.Fatalf("SetOwnPassword: %v", err)
	}
	registered, err = store.GetUserByID(ctx, registeredID)
	if err != nil {
		t.Fatalf("GetUserByID after set: %v", err)
	}
	if !registered.HasPassword {
		t.Fatal("SetOwnPassword did not mark the account password-capable")
	}
	if _, err := store.SetOwnPassword(ctx, registeredID, newHash, now, ""); err != nil {
		t.Fatalf("second SetOwnPassword: %v", err)
	}
	registered, err = store.GetUserByID(ctx, registeredID)
	if err != nil {
		t.Fatalf("GetUserByID after second set: %v", err)
	}
	if !registered.HasPassword {
		t.Fatal("a later password write cleared the capability flag")
	}
	// The keeper's session still exists: only the registered account's sessions
	// are touched by its own password write.
	if err := store.DB().QueryRowContext(ctx,
		`SELECT COUNT(1) FROM Sessions WHERE UserID = ?`, keeperID).Scan(&sessions); err != nil {
		t.Fatalf("count keeper sessions: %v", err)
	}
	if sessions != 1 {
		t.Fatalf("keeper sessions = %d, want 1", sessions)
	}
}
