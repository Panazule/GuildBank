package sqlite

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/Panazule/guildbank/internal/domain"
)

// TestSelfRegistrationMigrationIsAdditive replays 0001-0003 against a populated
// database, then applies 0004 alone. The account keeps its identity and its
// sessions, reads as Approved, and the new CHECK rejects a corrupt value.
func TestSelfRegistrationMigrationIsAdditive(t *testing.T) {
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
	userID := insertUserForTest(t, store, "Keeper", domain.RoleAdmin, false)
	if _, err := store.DB().Exec(`
		INSERT INTO Sessions (SessionID, TokenHash, UserID, CreatedAt, LastAccessed,
		                      ExpiresAt, AbsoluteExpiresAt)
		VALUES ('s1', 'h1', ?, ?, ?, ?, ?)`,
		userID, formatTime(now), formatTime(now), formatTime(now.Add(time.Hour)),
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

	user, err := store.GetUserByID(ctx, userID)
	if err != nil {
		t.Fatalf("GetUserByID: %v", err)
	}
	if !user.IsApproved() || user.Status() != domain.AccountStatusApproved {
		t.Fatalf("existing account status = %q, want Approved", user.Status())
	}
	if user.Origin != "" || user.GuildVerifiedAt != nil {
		t.Fatalf("existing account gained origin/verification: %+v", user)
	}
	if !user.IsEnabled() {
		t.Fatal("an existing approved account must remain enabled")
	}

	// 0004 is metadata-only: the session written before the upgrade survives.
	var sessions int
	if err := store.DB().QueryRowContext(ctx, `SELECT COUNT(1) FROM Sessions`).Scan(&sessions); err != nil {
		t.Fatalf("count sessions: %v", err)
	}
	if sessions != 1 {
		t.Fatalf("sessions = %d, want 1 (no cascade)", sessions)
	}

	// The CHECK keeps a corrupt status out of the table.
	if _, err := store.DB().Exec(`UPDATE Users SET AccountStatus = 'Bogus' WHERE UserID = ?`, userID); err == nil {
		t.Fatal("the CHECK accepted an unknown account status")
	}
}
