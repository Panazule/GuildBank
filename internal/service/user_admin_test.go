package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Panazule/guildbank/internal/domain"
)

// fakeUserAdminStore is the in-memory UserAdminStore. It records the calls whose
// arguments carry security meaning (which sessions survive, which accounts were
// reset) so the service contract can be asserted without SQLite.
type fakeUserAdminStore struct {
	users         map[int64]domain.User
	nextID        int64
	resetIDs      []int64
	ownPasswordID int64
	keepSessionID string
	deleteIDs     []int64
	createErr     error
}

func newFakeUserAdminStore(users ...domain.User) *fakeUserAdminStore {
	store := &fakeUserAdminStore{users: make(map[int64]domain.User, len(users)), nextID: 1}
	for _, user := range users {
		store.users[user.ID] = user
		if user.ID >= store.nextID {
			store.nextID = user.ID + 1
		}
	}
	return store
}

func (f *fakeUserAdminStore) ListUsers(ctx context.Context) ([]domain.User, error) {
	out := make([]domain.User, 0, len(f.users))
	for _, user := range f.users {
		out = append(out, user)
	}
	return out, nil
}

func (f *fakeUserAdminStore) GetUserByID(ctx context.Context, id int64) (domain.User, error) {
	user, ok := f.users[id]
	if !ok {
		return domain.User{}, domain.ErrNotFound
	}
	return user, nil
}

func (f *fakeUserAdminStore) CreateUser(ctx context.Context, u domain.User) (domain.User, error) {
	if f.createErr != nil {
		return domain.User{}, f.createErr
	}
	for _, existing := range f.users {
		if strings.EqualFold(existing.UserName, u.UserName) {
			return domain.User{}, domain.ErrAlreadyExists
		}
	}
	u.ID = f.nextID
	f.nextID++
	f.users[u.ID] = u
	return u, nil
}

func (f *fakeUserAdminStore) SetUserAccessLevel(ctx context.Context, id int64, role domain.Role,
	at time.Time, actor domain.Actor) (domain.User, error) {
	user, ok := f.users[id]
	if !ok {
		return domain.User{}, domain.ErrNotFound
	}
	user.AccessLevel = string(role)
	user.ModAt = at
	user.ModBy = actor.UserName
	f.users[id] = user
	return user, nil
}

func (f *fakeUserAdminStore) SetUserDisabled(ctx context.Context, id int64, disabled bool,
	at time.Time, actor domain.Actor) (domain.User, error) {
	user, ok := f.users[id]
	if !ok {
		return domain.User{}, domain.ErrNotFound
	}
	user.Disabled = disabled
	user.ModAt = at
	user.ModBy = actor.UserName
	f.users[id] = user
	return user, nil
}

func (f *fakeUserAdminStore) SetUserAccountStatus(ctx context.Context, id int64,
	status domain.AccountStatus, at time.Time, actor domain.Actor) (domain.User, error) {
	user, ok := f.users[id]
	if !ok {
		return domain.User{}, domain.ErrNotFound
	}
	user.AccountStatus = string(status)
	user.ModAt = at
	user.ModBy = actor.UserName
	f.users[id] = user
	return user, nil
}

func (f *fakeUserAdminStore) CountPending(ctx context.Context) (int, error) {
	count := 0
	for _, user := range f.users {
		if user.IsPending() {
			count++
		}
	}
	return count, nil
}

func (f *fakeUserAdminStore) ResetUserPassword(ctx context.Context, id int64, hash string,
	at time.Time, actor domain.Actor) (domain.User, error) {
	user, ok := f.users[id]
	if !ok {
		return domain.User{}, domain.ErrNotFound
	}
	user.PasswordHash = hash
	user.MustChangePassword = true
	user.HasPassword = true
	user.ModAt = at
	user.ModBy = actor.UserName
	f.users[id] = user
	f.resetIDs = append(f.resetIDs, id)
	return user, nil
}

func (f *fakeUserAdminStore) SetOwnPassword(ctx context.Context, id int64, hash string,
	at time.Time, keepSessionID string) (domain.User, error) {
	user, ok := f.users[id]
	if !ok {
		return domain.User{}, domain.ErrNotFound
	}
	user.PasswordHash = hash
	user.MustChangePassword = false
	user.HasPassword = true
	user.ModAt = at
	f.users[id] = user
	f.ownPasswordID = id
	f.keepSessionID = keepSessionID
	return user, nil
}

func (f *fakeUserAdminStore) DeleteUser(ctx context.Context, id int64, actor domain.Actor) error {
	if _, ok := f.users[id]; !ok {
		return domain.ErrNotFound
	}
	delete(f.users, id)
	f.deleteIDs = append(f.deleteIDs, id)
	return nil
}

type fakeAuditSink struct {
	entries []domain.AuditEntry
	err     error
}

func (f *fakeAuditSink) InsertAudit(ctx context.Context, entry domain.AuditEntry) error {
	if f.err != nil {
		return f.err
	}
	f.entries = append(f.entries, entry)
	return nil
}

func adminServiceActor() domain.Actor {
	return domain.Actor{UserID: 1, UserName: "admin", IsAdmin: true, Role: domain.RoleAdmin}
}

func TestCreateUserSetsTemporaryPasswordAndAudits(t *testing.T) {
	store := newFakeUserAdminStore(domain.User{ID: 1, UserName: "admin", AccessLevel: string(domain.RoleAdmin)})
	sink := &fakeAuditSink{}
	svc := NewUserAdminService(store, sink, nil)

	created, err := svc.Create(context.Background(), domain.CreateUserInput{
		UserName: "  Keeper  ", AccessLevel: "Officer",
		Password: "a temporary password", ConfirmPassword: "a temporary password",
	}, adminServiceActor(), "req-1", "203.0.113.9")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.ID == 0 || created.UserName != "Keeper" || created.Role() != domain.RoleOfficer {
		t.Fatalf("created = %+v", created)
	}
	if !created.MustChangePassword {
		t.Fatal("a created account must change its temporary password")
	}
	if created.PasswordHash != "" {
		t.Fatal("the service leaked the password hash")
	}
	stored := store.users[created.ID]
	if !VerifyPassword(stored.PasswordHash, "a temporary password") {
		t.Fatal("the stored hash does not verify the temporary password")
	}
	if stored.ModBy != "admin" {
		t.Fatalf("ModBy = %q, want admin", stored.ModBy)
	}
	if len(sink.entries) != 1 {
		t.Fatalf("audit entries = %d, want 1", len(sink.entries))
	}
	entry := sink.entries[0]
	if entry.EventType != "USER_CREATED" || entry.TableName != "Users" || entry.UserName != "admin" {
		t.Fatalf("audit entry = %+v", entry)
	}
	if entry.CorrelationID != "req-1" || entry.SourceIP != "203.0.113.9" {
		t.Fatalf("audit correlation/source = %+v", entry)
	}
}

func TestCreateUserRefusals(t *testing.T) {
	store := newFakeUserAdminStore(domain.User{ID: 1, UserName: "admin", AccessLevel: string(domain.RoleAdmin)})
	svc := NewUserAdminService(store, nil, nil)
	ctx := context.Background()
	in := domain.CreateUserInput{UserName: "Keeper", Password: "a temporary password",
		ConfirmPassword: "a temporary password"}

	officer := domain.Actor{UserID: 2, UserName: "officer", Role: domain.RoleOfficer}
	if _, err := svc.Create(ctx, in, officer, "", ""); !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("non-admin create = %v, want ErrForbidden", err)
	}
	if len(store.users) != 1 {
		t.Fatalf("a forbidden create added an account: %+v", store.users)
	}

	// The password policy is applied with the username, so the equality rule is
	// enforced here and not only in the form.
	sameAsName := domain.CreateUserInput{UserName: "KeeperOfSecrets",
		Password: "keeperofsecrets", ConfirmPassword: "keeperofsecrets"}
	if _, err := svc.Create(ctx, sameAsName, adminServiceActor(), "", ""); !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("username as password = %v, want ErrValidation", err)
	}

	if _, err := svc.Create(ctx, in, adminServiceActor(), "", ""); err != nil {
		t.Fatalf("first create: %v", err)
	}
	if _, err := svc.Create(ctx, domain.CreateUserInput{UserName: "KEEPER",
		Password: "a temporary password", ConfirmPassword: "a temporary password"},
		adminServiceActor(), "", ""); !errors.Is(err, domain.ErrAlreadyExists) {
		t.Fatalf("duplicate name = %v, want ErrAlreadyExists", err)
	}
}

func TestSelfDisableAndSelfDeleteAreRefused(t *testing.T) {
	admin := domain.User{ID: 1, UserName: "admin", AccessLevel: string(domain.RoleAdmin),
		PasswordHash: "hash"}
	store := newFakeUserAdminStore(admin)
	svc := NewUserAdminService(store, nil, nil)
	actor := adminServiceActor()
	ctx := context.Background()

	if _, err := svc.SetDisabled(ctx, admin.ID, true, actor, "", ""); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("self-disable = %v, want ErrConflict", err)
	}
	if store.users[admin.ID].Disabled {
		t.Fatal("self-disable changed the account")
	}
	err := svc.Delete(ctx, admin.ID, domain.DeleteUserInput{ConfirmUserName: "admin"}, actor, "", "")
	if !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("self-delete = %v, want ErrConflict", err)
	}
	if len(store.deleteIDs) != 0 {
		t.Fatalf("self-delete reached the store: %v", store.deleteIDs)
	}
}

func TestResetPasswordRequiresChangeAndAudits(t *testing.T) {
	k := domain.User{ID: 2, UserName: "Keeper", AccessLevel: string(domain.RoleOfficer)}
	store := newFakeUserAdminStore(k)
	sink := &fakeAuditSink{}
	svc := NewUserAdminService(store, sink, nil)

	updated, err := svc.ResetPassword(context.Background(), k.ID, domain.ResetPasswordInput{
		Password: "a fresh temporary password", ConfirmPassword: "a fresh temporary password",
	}, adminServiceActor(), "req-2", "")
	if err != nil {
		t.Fatalf("ResetPassword: %v", err)
	}
	if !updated.MustChangePassword {
		t.Fatal("a reset password must require a change")
	}
	if len(store.resetIDs) != 1 || store.resetIDs[0] != k.ID {
		t.Fatalf("reset calls = %v", store.resetIDs)
	}
	if len(sink.entries) != 1 || sink.entries[0].EventType != "USER_PASSWORD_RESET" {
		t.Fatalf("audit entries = %+v", sink.entries)
	}

	if _, err := svc.ResetPassword(context.Background(), k.ID, domain.ResetPasswordInput{
		Password: "short", ConfirmPassword: "short",
	}, adminServiceActor(), "", ""); !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("reset to a short password = %v, want ErrValidation", err)
	}
}

func TestChangeOwnPasswordVerifiesCurrentAndKeepsSession(t *testing.T) {
	realHash, err := HashUserPassword("Keeper", "the original password")
	if err != nil {
		t.Fatalf("HashUserPassword: %v", err)
	}
	k := domain.User{ID: 3, UserName: "Keeper", AccessLevel: string(domain.RoleMember),
		PasswordHash: realHash, MustChangePassword: true, HasPassword: true}
	store := newFakeUserAdminStore(k)
	sink := &fakeAuditSink{}
	svc := NewUserAdminService(store, sink, nil)

	in := domain.ChangePasswordInput{CurrentPassword: "wrong password value",
		NewPassword: "a new permanent password", ConfirmPassword: "a new permanent password"}
	_, err = svc.ChangeOwnPassword(context.Background(), k, "sid-current", in, "req-3", "")
	var validation *domain.ValidationError
	if !errors.As(err, &validation) || validation.Fields["currentPassword"] == "" {
		t.Fatalf("wrong current password = %v, want a currentPassword validation error", err)
	}
	if store.ownPasswordID != 0 {
		t.Fatal("a failed verification changed the password")
	}

	in.CurrentPassword = "the original password"
	updated, err := svc.ChangeOwnPassword(context.Background(), k, "sid-current", in, "req-3", "")
	if err != nil {
		t.Fatalf("ChangeOwnPassword: %v", err)
	}
	if updated.MustChangePassword {
		t.Fatal("the change did not clear MustChangePassword")
	}
	if store.keepSessionID != "sid-current" {
		t.Fatalf("kept session = %q, want sid-current", store.keepSessionID)
	}
	if !VerifyPassword(store.users[k.ID].PasswordHash, "a new permanent password") {
		t.Fatal("the new password does not verify")
	}
	if len(sink.entries) != 1 || sink.entries[0].EventType != "USER_PASSWORD_CHANGED" {
		t.Fatalf("audit entries = %+v", sink.entries)
	}
}

func TestAuditFailureDoesNotFailAccountChanges(t *testing.T) {
	store := newFakeUserAdminStore(domain.User{ID: 1, UserName: "admin", AccessLevel: string(domain.RoleAdmin)})
	sink := &fakeAuditSink{err: errors.New("guild store unavailable")}
	svc := NewUserAdminService(store, sink, nil)

	created, err := svc.Create(context.Background(), domain.CreateUserInput{
		UserName: "Keeper", Password: "a temporary password", ConfirmPassword: "a temporary password",
	}, adminServiceActor(), "req-4", "")
	if err != nil {
		t.Fatalf("an audit failure must not fail the create: %v", err)
	}
	if _, ok := store.users[created.ID]; !ok {
		t.Fatal("the account was not stored")
	}
}

func TestRoleChangeRecordsOldAndNewLevel(t *testing.T) {
	k := domain.User{ID: 2, UserName: "Keeper", AccessLevel: string(domain.RoleOfficer)}
	store := newFakeUserAdminStore(k)
	sink := &fakeAuditSink{}
	svc := NewUserAdminService(store, sink, nil)

	if _, err := svc.SetRole(context.Background(), k.ID, "Admin", adminServiceActor(), "req-5", ""); err != nil {
		t.Fatalf("SetRole: %v", err)
	}
	if store.users[k.ID].Role() != domain.RoleAdmin {
		t.Fatalf("role = %q, want Admin", store.users[k.ID].AccessLevel)
	}
	if len(sink.entries) != 1 {
		t.Fatalf("audit entries = %d, want 1", len(sink.entries))
	}
	entry := sink.entries[0]
	if entry.EventType != "USER_ROLE_CHANGED" ||
		!strings.Contains(string(entry.OldValues), "Officer") ||
		!strings.Contains(string(entry.NewValues), "Admin") {
		t.Fatalf("audit entry = %+v (%s -> %s)", entry, entry.OldValues, entry.NewValues)
	}

	if _, err := svc.SetRole(context.Background(), k.ID, "Root", adminServiceActor(), "", ""); !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("unknown role = %v, want ErrValidation", err)
	}
}
