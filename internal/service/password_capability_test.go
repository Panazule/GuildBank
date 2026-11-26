package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/Panazule/guildbank/internal/domain"
)

// passwordlessKeldor is a signed-in self-registered account: no password, no
// forced change, Member rank.
func passwordlessKeldor() domain.User {
	return domain.User{ID: 3, UserName: "keldor", AccessLevel: string(domain.RoleMember),
		AccountStatus: string(domain.AccountStatusApproved), HasPassword: false}
}

// TestSetOwnPasswordEstablishesFirstCredential proves the Set path: no current
// password is asked for, the account keeps its current session, the flag turns
// on, MustChangePassword stays clear, and the transition is audited.
func TestSetOwnPasswordEstablishesFirstCredential(t *testing.T) {
	ctx := context.Background()
	keldor := passwordlessKeldor()
	store := newFakeUserAdminStore(keldor)
	sink := &fakeAuditSink{}
	svc := NewUserAdminService(store, sink, nil)

	updated, err := svc.SetOwnPassword(ctx, keldor, "sid-current",
		domain.SetPasswordInput{NewPassword: "a brand new strong password",
			ConfirmPassword: "a brand new strong password"},
		"corr-1", "203.0.113.9")
	if err != nil {
		t.Fatalf("SetOwnPassword: %v", err)
	}
	if !updated.HasPassword || updated.MustChangePassword {
		t.Fatalf("updated = %+v, want HasPassword and no forced change", updated)
	}
	if updated.PasswordHash != "" {
		t.Fatal("the service leaked the stored hash to the caller")
	}
	if store.ownPasswordID != keldor.ID || store.keepSessionID != "sid-current" {
		t.Fatalf("store call = id %d, keep %q", store.ownPasswordID, store.keepSessionID)
	}
	if !VerifyPassword(store.users[keldor.ID].PasswordHash, "a brand new strong password") {
		t.Fatal("the stored hash does not verify the new password")
	}

	// Discord-only accounts keep signing in with Discord, so a Set must not force
	// a change the owner did not ask for.
	if len(sink.entries) != 1 {
		t.Fatalf("audit entries = %d, want 1", len(sink.entries))
	}
	entry := sink.entries[0]
	if entry.EventType != "USER_PASSWORD_SET" || entry.TableName != "Users" ||
		entry.RecordID != "3" || entry.UserName != "keldor" {
		t.Fatalf("audit entry = %+v", entry)
	}
	if !strings.Contains(string(entry.OldValues), `"hasPassword":"false"`) ||
		!strings.Contains(string(entry.NewValues), `"hasPassword":"true"`) {
		t.Fatalf("audit values = %s -> %s", entry.OldValues, entry.NewValues)
	}
	var decoded map[string]string
	if err := json.Unmarshal(entry.NewValues, &decoded); err != nil {
		t.Fatalf("audit NewValues is not JSON: %v", err)
	}
}

// TestSetOwnPasswordRefusesAPasswordAccount proves Set is a one-time transition:
// once a password exists the owner must prove it on Change instead.
func TestSetOwnPasswordRefusesAPasswordAccount(t *testing.T) {
	ctx := context.Background()
	keeper := domain.User{ID: 1, UserName: "admin", AccessLevel: string(domain.RoleAdmin),
		PasswordHash: testHash("a long enough password"), HasPassword: true}
	store := newFakeUserAdminStore(keeper)
	svc := NewUserAdminService(store, nil, nil)

	_, err := svc.SetOwnPassword(ctx, keeper, "sid",
		domain.SetPasswordInput{NewPassword: "a brand new strong password",
			ConfirmPassword: "a brand new strong password"}, "corr", "")
	if !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("err = %v, want ErrConflict", err)
	}
	if store.ownPasswordID != 0 {
		t.Fatal("a refused set still wrote a password")
	}
}

// TestChangeOwnPasswordRefusesAPasswordlessAccount proves Change cannot create
// the first credential, keeping the two routes disjoint (D-SR-19).
func TestChangeOwnPasswordRefusesAPasswordlessAccount(t *testing.T) {
	ctx := context.Background()
	keldor := passwordlessKeldor()
	store := newFakeUserAdminStore(keldor)
	svc := NewUserAdminService(store, nil, nil)

	_, err := svc.ChangeOwnPassword(ctx, keldor, "sid",
		domain.ChangePasswordInput{CurrentPassword: "anything at all",
			NewPassword: "a brand new strong password", ConfirmPassword: "a brand new strong password"},
		"corr", "")
	if !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("err = %v, want ErrConflict", err)
	}
	if store.ownPasswordID != 0 {
		t.Fatal("a refused change still wrote a password")
	}
}

// TestPasswordLoginRefusedWithoutCapability proves the flag narrows
// authentication: the stored hash may verify, but a passwordless account cannot
// sign in with it (D-SR-16).
func TestPasswordLoginRefusedWithoutCapability(t *testing.T) {
	auth, users, _, _ := authFixture(t)
	passwordless := domain.User{ID: 2, UserName: "keldor",
		PasswordHash: testHash("a long enough password"), AccessLevel: string(domain.RoleMember)}
	users.users["keldor"] = passwordless

	if _, err := auth.Login(context.Background(), LoginInput{
		UserName: "keldor", Password: "a long enough password", ClientIP: "10.0.0.1",
	}); !errors.Is(err, domain.ErrUnauthorized) {
		t.Fatalf("passwordless login err = %v, want ErrUnauthorized", err)
	}

	passwordless.HasPassword = true
	users.users["keldor"] = passwordless
	if _, err := auth.Login(context.Background(), LoginInput{
		UserName: "keldor", Password: "a long enough password", ClientIP: "10.0.0.1",
	}); err != nil {
		t.Fatalf("login after capability grant: %v", err)
	}
}

// TestChangeOwnPasswordKeepsCurrentSessionAndAudits proves the Change path's
// session and audit contract, using a password-capable Member.
func TestChangeOwnPasswordKeepsCurrentSessionAndAudits(t *testing.T) {
	ctx := context.Background()
	keeper := domain.User{ID: 4, UserName: "officer", AccessLevel: string(domain.RoleOfficer),
		PasswordHash: testHash("a long enough password"), HasPassword: true}
	store := newFakeUserAdminStore(keeper)
	sink := &fakeAuditSink{}
	svc := NewUserAdminService(store, sink, nil)

	updated, err := svc.ChangeOwnPassword(ctx, keeper, "sid-current",
		domain.ChangePasswordInput{CurrentPassword: "a long enough password",
			NewPassword: "another strong password", ConfirmPassword: "another strong password"},
		"corr-2", "198.51.100.4")
	if err != nil {
		t.Fatalf("ChangeOwnPassword: %v", err)
	}
	if !updated.HasPassword {
		t.Fatalf("updated = %+v, want HasPassword", updated)
	}
	if store.keepSessionID != "sid-current" {
		t.Fatalf("keep session = %q, want sid-current", store.keepSessionID)
	}
	if !VerifyPassword(store.users[keeper.ID].PasswordHash, "another strong password") {
		t.Fatal("the stored hash does not verify the new password")
	}
	if len(sink.entries) != 1 || sink.entries[0].EventType != "USER_PASSWORD_CHANGED" {
		t.Fatalf("audit entries = %+v", sink.entries)
	}
}

// TestSetOwnPasswordAppliesPolicy keeps the set route honest about password
// policy: a weak choice is a validation error, not a capability grant.
func TestSetOwnPasswordAppliesPolicy(t *testing.T) {
	ctx := context.Background()
	keldor := passwordlessKeldor()
	store := newFakeUserAdminStore(keldor)
	svc := NewUserAdminService(store, nil, nil)

	_, err := svc.SetOwnPassword(ctx, keldor, "sid",
		domain.SetPasswordInput{NewPassword: "keldor", ConfirmPassword: "keldor"}, "corr", "")
	var invalid *domain.ValidationError
	if !errors.As(err, &invalid) {
		t.Fatalf("err = %v, want a validation error", err)
	}
	if store.ownPasswordID != 0 {
		t.Fatal("a policy failure still wrote a password")
	}
}
