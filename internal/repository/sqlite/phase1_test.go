package sqlite

import (
	"context"
	"errors"
	"io/fs"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/Panazule/guildbank/internal/domain"
)

func migratedStore(t *testing.T) *Store {
	t.Helper()
	store, err := Open(context.Background(), filepath.Join(t.TempDir(), "guildbank.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	version, err := store.Migrate(context.Background())
	if err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	if want := latestSchemaVersion(t); version != want {
		t.Fatalf("version = %d, want %d", version, want)
	}
	return store
}

// latestSchemaVersion is the number of embedded migrations, so adding a
// migration updates every version assertion at once.
func latestSchemaVersion(t *testing.T) int {
	t.Helper()
	names, err := fs.Glob(migrationFiles, "migrations/*.sql")
	if err != nil {
		t.Fatalf("glob migrations: %v", err)
	}
	return len(names)
}

func hashForTest(password string) (string, error) {
	h, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.MinCost)
	return string(h), err
}

func TestMigrateCreatesPhase1SchemaAndSeeds(t *testing.T) {
	store := migratedStore(t)
	ctx := context.Background()

	for _, table := range []string{"Users", "Settings", "Sessions"} {
		var n int
		if err := store.DB().QueryRowContext(ctx,
			`SELECT COUNT(1) FROM sqlite_master WHERE type='table' AND name=?`, table).Scan(&n); err != nil {
			t.Fatalf("inspect %s: %v", table, err)
		}
		if n != 1 {
			t.Fatalf("table %s missing", table)
		}
	}

	settings, err := store.ListSettings(ctx)
	if err != nil {
		t.Fatalf("ListSettings: %v", err)
	}
	values := map[string]string{}
	for _, s := range settings {
		values[s.Key] = s.Value
	}
	if values["GuildName"] != "Aeternus" || values["GuildOpenDays"] != "14" ||
		values["BazaarDays"] != "30" || values["SetupComplete"] != "0" {
		t.Fatalf("unexpected seeds: %+v", values)
	}
	if values["EQResourceBaseUrl"] != "https://items.eqresource.com/items.php?id=" {
		t.Fatalf("EQResourceBaseUrl = %q", values["EQResourceBaseUrl"])
	}

	version, err := store.Migrate(ctx)
	if want := latestSchemaVersion(t); err != nil || version != want {
		t.Fatalf("second Migrate = %d, %v; want %d, nil", version, err, want)
	}
}

func TestMigrateRefusesNewerSchema(t *testing.T) {
	store := migratedStore(t)
	if _, err := store.DB().Exec(`PRAGMA user_version = 99`); err != nil {
		t.Fatalf("set version: %v", err)
	}
	version, err := store.Migrate(context.Background())
	if err == nil || version != 99 {
		t.Fatalf("Migrate = %d, %v; want 99 and an error", version, err)
	}
}

func TestCompleteSetupIsAtomicAndCloses(t *testing.T) {
	store := migratedStore(t)
	ctx := context.Background()
	now := time.Date(2026, 11, 21, 12, 0, 0, 0, time.UTC)
	hash, err := hashForTest("correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	data := domain.SetupData{
		Admin: domain.User{
			UserName: "Admin", PasswordHash: hash, AccessLevel: domain.AccessAdmin,
			ModAt: now, CreatedAt: now, ModBy: "setup", LastLogin: &now,
		},
		Settings: []domain.SettingInput{
			{Key: "GuildName", Value: "Aeternus", Type: domain.SettingString},
			{Key: "SetupComplete", Value: "1", Type: domain.SettingBoolean},
		},
	}
	user, err := store.CompleteSetup(ctx, data)
	if err != nil {
		t.Fatalf("CompleteSetup: %v", err)
	}
	if user.ID == 0 || user.UserName != "Admin" {
		t.Fatalf("user = %+v", user)
	}
	state, err := store.SetupStatus(ctx)
	if err != nil || state != domain.SetupComplete {
		t.Fatalf("SetupStatus = %v, %v", state, err)
	}
	values, _ := store.ListSettings(ctx)
	if v, _ := domain.GetSetting(values, "GuildName"); v != "Aeternus" {
		t.Fatalf("GuildName = %q", v)
	}
	if v, _ := domain.GetSetting(values, "SetupComplete"); v != "1" {
		t.Fatalf("SetupComplete = %q", v)
	}

	if _, err := store.CompleteSetup(ctx, data); !errors.Is(err, domain.ErrSetupClosed) {
		t.Fatalf("second CompleteSetup err = %v, want ErrSetupClosed", err)
	}

	// A failing settings write must roll the administrator back; the setup
	// switch cannot close with partial state.
	store2, err := Open(ctx, filepath.Join(t.TempDir(), "guildbank.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store2.Close()
	if _, err := store2.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	data.Settings = append(data.Settings, domain.SettingInput{Key: "Bad", Value: "x", Type: "unsupported"})
	if _, err := store2.CompleteSetup(ctx, data); err == nil {
		t.Fatal("CompleteSetup with invalid setting type succeeded")
	}
	state, err = store2.SetupStatus(ctx)
	if err != nil || state != domain.SetupPending {
		t.Fatalf("state after rollback = %v, %v; want pending", state, err)
	}
	admins, _ := store2.CountAdministrators(ctx)
	if admins != 0 {
		t.Fatalf("administrators after rollback = %d", admins)
	}
}

func TestUsersAndSessionsRoundTrip(t *testing.T) {
	store := migratedStore(t)
	ctx := context.Background()
	now := time.Date(2026, 11, 21, 12, 0, 0, 0, time.UTC)
	hash, _ := hashForTest("a sufficiently long password")
	user, err := store.CompleteSetup(ctx, domain.SetupData{
		Admin: domain.User{
			UserName: "Keeper", PasswordHash: hash, AccessLevel: domain.AccessAdmin,
			ModAt: now, CreatedAt: now, LastLogin: &now,
		},
	})
	if err != nil {
		t.Fatalf("CompleteSetup: %v", err)
	}

	if err := store.UpdateLastLogin(ctx, user.ID, now.Add(time.Hour)); err != nil {
		t.Fatalf("UpdateLastLogin: %v", err)
	}
	got, err := store.GetUserByName(ctx, "keeper")
	if err != nil {
		t.Fatalf("GetUserByName (case-insensitive): %v", err)
	}
	if got.LastLogin == nil || !got.LastLogin.Equal(now.Add(time.Hour)) {
		t.Fatalf("LastLogin = %v", got.LastLogin)
	}
	if got.PasswordHash != hash {
		t.Fatal("password hash changed")
	}

	session := domain.Session{
		ID: "session-id", TokenHash: "hash-value", UserID: user.ID,
		CreatedAt: now, LastAccessed: now, ExpiresAt: now.Add(30 * time.Minute),
		AbsoluteExpiresAt: now.Add(8 * time.Hour), ClientIP: "127.0.0.1", UserAgent: "test",
	}
	if err := store.CreateSession(ctx, session); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	found, err := store.FindSessionByTokenHash(ctx, "hash-value")
	if err != nil {
		t.Fatalf("FindSessionByTokenHash: %v", err)
	}
	if found.ID != session.ID || found.User.UserName != "Keeper" || found.TokenHash != "hash-value" {
		t.Fatalf("session = %+v", found)
	}
	if err := store.TouchSession(ctx, session.ID, now.Add(time.Minute), now.Add(31*time.Minute)); err != nil {
		t.Fatalf("TouchSession: %v", err)
	}
	if n, err := store.DeleteExpiredSessions(ctx, now.Add(9*time.Hour)); err != nil || n != 1 {
		t.Fatalf("DeleteExpiredSessions = %d, %v", n, err)
	}
	if err := store.DeleteSession(ctx, "hash-value"); err != nil {
		t.Fatalf("DeleteSession: %v", err)
	}
}

func TestSettingsUpsertPreservesMetadata(t *testing.T) {
	store := migratedStore(t)
	ctx := context.Background()
	now := time.Date(2026, 11, 21, 12, 0, 0, 0, time.UTC)
	err := store.UpsertSettings(ctx, []domain.SettingInput{
		{Key: "GuildName", Value: "New Name", Type: domain.SettingString},
		{Key: "GuildOpenDays", Value: "21", Type: domain.SettingInteger},
	}, "admin", now)
	if err != nil {
		t.Fatalf("UpsertSettings: %v", err)
	}
	rows, err := store.ListSettings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if v, _ := domain.GetSetting(rows, "GuildName"); v != "New Name" {
		t.Fatalf("GuildName = %q", v)
	}
	if v, _ := domain.GetSetting(rows, "GuildOpenDays"); v != "21" {
		t.Fatalf("GuildOpenDays = %q", v)
	}
	for _, row := range rows {
		if row.Key == "GuildName" && (row.ModBy != "admin" || !row.ModAt.Equal(now)) {
			t.Fatalf("metadata = %+v", row)
		}
	}
}
