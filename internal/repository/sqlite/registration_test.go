package sqlite

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/Panazule/guildbank/internal/domain"
)

// newRegistrationStore opens a fully migrated store for the registration tests.
func newRegistrationStore(t *testing.T) *Store {
	t.Helper()
	store, err := Open(context.Background(), filepath.Join(t.TempDir(), "guildbank.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if _, err := store.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func registeredUserForTest(name, discordID, status string) (domain.User, domain.DiscordIdentity) {
	now := time.Date(2026, 11, 26, 12, 0, 0, 0, time.UTC)
	verified := now
	user := domain.User{
		UserName: name, PasswordHash: "$2a$10$notarealhashforregistrationtest",
		AccessLevel: string(domain.RoleMember), AccountStatus: status,
		Origin: domain.OriginSelfRegistration, ModAt: now, CreatedAt: now,
	}
	if status == string(domain.AccountStatusApproved) {
		user.GuildVerifiedAt = &verified
	}
	return user, domain.DiscordIdentity{DiscordUserID: discordID, DiscordUserName: "Keldor", LinkedAt: &now}
}

// TestCreateRegisteredUserWritesAccountAndIdentity proves the account and the
// Discord identity are written by one statement and read back consistently.
func TestCreateRegisteredUserWritesAccountAndIdentity(t *testing.T) {
	ctx := context.Background()
	store := newRegistrationStore(t)

	user, identity := registeredUserForTest("Keldor", "123456789012345678", string(domain.AccountStatusApproved))
	created, err := store.CreateRegisteredUser(ctx, user, identity)
	if err != nil {
		t.Fatalf("CreateRegisteredUser: %v", err)
	}
	if created.ID == 0 || created.UserName != "Keldor" {
		t.Fatalf("created = %+v", created)
	}
	if !created.IsApproved() || !created.IsEnabled() {
		t.Fatalf("created status = %q, want Approved", created.Status())
	}
	if created.Origin != domain.OriginSelfRegistration {
		t.Fatalf("origin = %q", created.Origin)
	}
	if created.GuildVerifiedAt == nil {
		t.Fatal("GuildVerifiedAt was not stored for an approved registration")
	}

	read, err := store.DiscordIdentity(ctx, created.ID)
	if err != nil {
		t.Fatalf("DiscordIdentity: %v", err)
	}
	if read.DiscordUserID != "123456789012345678" || read.DiscordUserName != "Keldor" {
		t.Fatalf("identity = %+v", read)
	}
	owner, err := store.DiscordUserIDOwner(ctx, "123456789012345678")
	if err != nil || owner != created.ID {
		t.Fatalf("owner = %d, %v", owner, err)
	}
}

// TestCreateRegisteredUserRejectsDuplicates proves both unique indexes are the
// authority and that a rejected insert writes nothing.
func TestCreateRegisteredUserRejectsDuplicates(t *testing.T) {
	ctx := context.Background()
	store := newRegistrationStore(t)

	first, firstIdentity := registeredUserForTest("Keldor", "111111111111111111", string(domain.AccountStatusPendingApproval))
	if _, err := store.CreateRegisteredUser(ctx, first, firstIdentity); err != nil {
		t.Fatalf("first registration: %v", err)
	}

	sameName, otherIdentity := registeredUserForTest("keldor", "222222222222222222", string(domain.AccountStatusPendingApproval))
	if _, err := store.CreateRegisteredUser(ctx, sameName, otherIdentity); !errors.Is(err, domain.ErrAlreadyExists) {
		t.Fatalf("duplicate username error = %v, want ErrAlreadyExists", err)
	}

	otherName, sameIdentity := registeredUserForTest("OtherName", "111111111111111111", string(domain.AccountStatusPendingApproval))
	if _, err := store.CreateRegisteredUser(ctx, otherName, sameIdentity); !errors.Is(err, domain.ErrAlreadyExists) {
		t.Fatalf("duplicate identity error = %v, want ErrAlreadyExists", err)
	}

	users, err := store.ListUsers(ctx)
	if err != nil {
		t.Fatalf("ListUsers: %v", err)
	}
	if len(users) != 1 {
		t.Fatalf("users = %d, want 1 after two rejected inserts", len(users))
	}
}

// TestSetUserAccountStatusWithdrawsApproval proves withdrawal flips the status
// and deletes every session in the same transaction.
func TestSetUserAccountStatusWithdrawsApproval(t *testing.T) {
	ctx := context.Background()
	store := newRegistrationStore(t)
	actor := adminActorForTest()

	user, identity := registeredUserForTest("Keldor", "333333333333333333", string(domain.AccountStatusApproved))
	created, err := store.CreateRegisteredUser(ctx, user, identity)
	if err != nil {
		t.Fatalf("CreateRegisteredUser: %v", err)
	}
	now := time.Date(2026, 11, 26, 12, 30, 0, 0, time.UTC)
	if _, err := store.DB().Exec(`
		INSERT INTO Sessions (SessionID, TokenHash, UserID, CreatedAt, LastAccessed, ExpiresAt, AbsoluteExpiresAt)
		VALUES ('s1', 'h1', ?, ?, ?, ?, ?)`,
		created.ID, formatTime(now), formatTime(now), formatTime(now.Add(time.Hour)),
		formatTime(now.Add(time.Hour))); err != nil {
		t.Fatalf("insert session: %v", err)
	}

	updated, err := store.SetUserAccountStatus(ctx, created.ID, domain.AccountStatusPendingApproval, now, actor)
	if err != nil {
		t.Fatalf("SetUserAccountStatus: %v", err)
	}
	if !updated.IsPending() || updated.IsEnabled() {
		t.Fatalf("updated status = %q, want PendingApproval", updated.Status())
	}
	var sessions int
	if err := store.DB().QueryRowContext(ctx, `SELECT COUNT(1) FROM Sessions WHERE UserID = ?`, created.ID).Scan(&sessions); err != nil {
		t.Fatalf("count sessions: %v", err)
	}
	if sessions != 0 {
		t.Fatalf("sessions = %d, want 0 after withdrawal", sessions)
	}

	// Idempotent: withdrawing again changes nothing and does not error.
	if _, err := store.SetUserAccountStatus(ctx, created.ID, domain.AccountStatusPendingApproval, now, actor); err != nil {
		t.Fatalf("second withdrawal: %v", err)
	}
	if pending, err := store.CountPending(ctx); err != nil || pending != 1 {
		t.Fatalf("CountPending = %d, %v", pending, err)
	}

	// A pending account may not be given a role: approval and promotion are
	// separate, separately audited acts.
	if _, err := store.SetUserAccessLevel(ctx, created.ID, domain.RoleOfficer, now, actor); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("role change on pending account = %v, want ErrConflict", err)
	}

	if _, err := store.SetUserAccountStatus(ctx, created.ID, domain.AccountStatusApproved, now, actor); err != nil {
		t.Fatalf("approve: %v", err)
	}
	if pending, err := store.CountPending(ctx); err != nil || pending != 0 {
		t.Fatalf("CountPending after approval = %d, %v", pending, err)
	}
}

// TestSetUserAccountStatusRequiresAdmin proves the store guard, not only the
// route guard, refuses a non-administrator.
func TestSetUserAccountStatusRequiresAdmin(t *testing.T) {
	ctx := context.Background()
	store := newRegistrationStore(t)
	user, identity := registeredUserForTest("Keldor", "444444444444444444", string(domain.AccountStatusPendingApproval))
	created, err := store.CreateRegisteredUser(ctx, user, identity)
	if err != nil {
		t.Fatalf("CreateRegisteredUser: %v", err)
	}
	actor := domain.Actor{UserID: created.ID, UserName: created.UserName, Role: domain.RoleMember}
	if _, err := store.SetUserAccountStatus(ctx, created.ID, domain.AccountStatusApproved, time.Now().UTC(), actor); !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("non-admin approval = %v, want ErrForbidden", err)
	}
}
