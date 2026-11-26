package sqlite

import (
	"context"
	"testing"
	"time"

	"github.com/Panazule/guildbank/internal/domain"
)

// newPasswordlessUserForTest creates a self-registered account with the
// discarded hash the registration path stores, so the row matches production
// semantics.
func newPasswordlessUserForTest(t *testing.T, store *Store, name string) domain.User {
	t.Helper()
	hash, err := hashForTest("a discarded random secret")
	if err != nil {
		t.Fatalf("hashForTest: %v", err)
	}
	now := time.Date(2026, 11, 26, 12, 0, 0, 0, time.UTC)
	created, err := store.CreateRegisteredUser(context.Background(),
		domain.User{UserName: name, AccessLevel: string(domain.RoleMember), PasswordHash: hash,
			AccountStatus: string(domain.AccountStatusApproved),
			Origin:        domain.OriginSelfRegistration, ModAt: now, CreatedAt: now},
		domain.DiscordIdentity{DiscordUserID: "123456789012345678", DiscordUserName: "keldor"})
	if err != nil {
		t.Fatalf("CreateRegisteredUser: %v", err)
	}
	return created
}

// TestCreateUserRecordsPasswordCapability proves the administrator path always
// writes a password-capable account: the operator typed a password.
func TestCreateUserRecordsPasswordCapability(t *testing.T) {
	store := migratedStore(t)
	ctx := context.Background()

	hash, err := hashForTest("a temporary password")
	if err != nil {
		t.Fatalf("hashForTest: %v", err)
	}
	created, err := store.CreateUser(ctx, domain.User{
		UserName: "Keeper", AccessLevel: string(domain.RoleMember), PasswordHash: hash,
	})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if !created.HasPassword {
		t.Fatalf("created account = %+v, want HasPassword", created)
	}
	stored, err := store.GetUserByID(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetUserByID: %v", err)
	}
	if !stored.HasPassword {
		t.Fatal("the capability flag did not survive the read")
	}
}

// TestCreateRegisteredUserStartsPasswordless proves the registration path writes
// a hash but no capability, so the stored random secret can never authenticate.
func TestCreateRegisteredUserStartsPasswordless(t *testing.T) {
	store := migratedStore(t)
	ctx := context.Background()

	created := newPasswordlessUserForTest(t, store, "keldor")
	if created.HasPassword {
		t.Fatalf("registered account = %+v, want no password capability", created)
	}
	if created.PasswordHash == "" {
		t.Fatal("the registration path must still store a hash (discarded secret)")
	}
	stored, err := store.GetUserByID(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetUserByID: %v", err)
	}
	if stored.HasPassword {
		t.Fatal("a later read reported a password capability that was never granted")
	}
}

// TestSetOwnPasswordGrantsCapabilityOnce proves the store's monotone write:
// HasPassword becomes true and stays true, and only the caller's current session
// survives.
func TestSetOwnPasswordGrantsCapabilityOnce(t *testing.T) {
	store := migratedStore(t)
	ctx := context.Background()
	user := newPasswordlessUserForTest(t, store, "keldor")

	now := time.Date(2026, 11, 26, 12, 0, 0, 0, time.UTC)
	for _, id := range []string{"sid-current", "sid-other"} {
		if err := store.CreateSession(ctx, domain.Session{
			ID: id, TokenHash: "hash-" + id, UserID: user.ID,
			CreatedAt: now, LastAccessed: now,
			ExpiresAt: now.Add(time.Hour), AbsoluteExpiresAt: now.Add(time.Hour),
		}); err != nil {
			t.Fatalf("CreateSession(%s): %v", id, err)
		}
	}

	hash, err := hashForTest("a brand new password")
	if err != nil {
		t.Fatalf("hashForTest: %v", err)
	}
	updated, err := store.SetOwnPassword(ctx, user.ID, hash, now, "sid-current")
	if err != nil {
		t.Fatalf("SetOwnPassword: %v", err)
	}
	if !updated.HasPassword || updated.MustChangePassword {
		t.Fatalf("updated account = %+v, want HasPassword and no forced change", updated)
	}
	var sessions int
	if err := store.DB().QueryRowContext(ctx,
		`SELECT COUNT(1) FROM Sessions WHERE UserID = ?`, user.ID).Scan(&sessions); err != nil {
		t.Fatalf("count sessions: %v", err)
	}
	if sessions != 1 {
		t.Fatalf("sessions = %d, want 1 (only the current session survives)", sessions)
	}

	// A second write cannot lower the flag, so the transition is monotone.
	second, err := hashForTest("a second new password")
	if err != nil {
		t.Fatalf("hashForTest: %v", err)
	}
	if _, err := store.SetOwnPassword(ctx, user.ID, second, now, ""); err != nil {
		t.Fatalf("second SetOwnPassword: %v", err)
	}
	stored, err := store.GetUserByID(ctx, user.ID)
	if err != nil {
		t.Fatalf("GetUserByID: %v", err)
	}
	if !stored.HasPassword {
		t.Fatal("a later password write cleared the capability flag")
	}
}

// TestResetUserPasswordGrantsCapability proves the administrator reset is the
// second write that sets the flag, so a Discord-only account can be given a
// temporary password.
func TestResetUserPasswordGrantsCapability(t *testing.T) {
	store := migratedStore(t)
	ctx := context.Background()
	user := newPasswordlessUserForTest(t, store, "keldor")

	hash, err := hashForTest("a temporary password")
	if err != nil {
		t.Fatalf("hashForTest: %v", err)
	}
	updated, err := store.ResetUserPassword(ctx, user.ID, hash,
		time.Date(2026, 11, 26, 12, 0, 0, 0, time.UTC), adminActorForTest())
	if err != nil {
		t.Fatalf("ResetUserPassword: %v", err)
	}
	if !updated.HasPassword || !updated.MustChangePassword {
		t.Fatalf("reset account = %+v, want HasPassword and a forced change", updated)
	}
}

// TestSessionReadExposesPasswordCapability proves the session join projects the
// same flag an account read would, so middleware can answer Set vs Change from
// the session alone.
func TestSessionReadExposesPasswordCapability(t *testing.T) {
	store := migratedStore(t)
	ctx := context.Background()
	user := newPasswordlessUserForTest(t, store, "keldor")
	now := time.Date(2026, 11, 26, 12, 0, 0, 0, time.UTC)

	if err := store.CreateSession(ctx, domain.Session{
		ID: "sid", TokenHash: "token-hash", UserID: user.ID,
		CreatedAt: now, LastAccessed: now,
		ExpiresAt: now.Add(time.Hour), AbsoluteExpiresAt: now.Add(time.Hour),
	}); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	found, err := store.FindSessionByTokenHash(ctx, "token-hash")
	if err != nil {
		t.Fatalf("FindSessionByTokenHash: %v", err)
	}
	if found.User.HasPassword {
		t.Fatal("a passwordless account's session reported a password capability")
	}

	hash, err := hashForTest("a brand new password")
	if err != nil {
		t.Fatalf("hashForTest: %v", err)
	}
	if _, err := store.SetOwnPassword(ctx, user.ID, hash, now, "sid"); err != nil {
		t.Fatalf("SetOwnPassword: %v", err)
	}
	found, err = store.FindSessionByTokenHash(ctx, "token-hash")
	if err != nil {
		t.Fatalf("FindSessionByTokenHash after set: %v", err)
	}
	if !found.User.HasPassword {
		t.Fatal("the session join did not pick up the new capability")
	}
}
