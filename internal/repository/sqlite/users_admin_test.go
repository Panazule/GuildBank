package sqlite

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Panazule/guildbank/internal/domain"
)

func createSessionForTest(t *testing.T, store *Store, id, token string, userID int64, now time.Time) {
	t.Helper()
	if err := store.CreateSession(context.Background(), domain.Session{
		ID: id, TokenHash: token, UserID: userID,
		CreatedAt: now, LastAccessed: now, ExpiresAt: now.Add(time.Hour),
		AbsoluteExpiresAt: now.Add(8 * time.Hour),
	}); err != nil {
		t.Fatalf("CreateSession(%s): %v", id, err)
	}
}

func countSessionsForTest(t *testing.T, store *Store, userID int64) int {
	t.Helper()
	var n int
	if err := store.DB().QueryRowContext(context.Background(),
		`SELECT COUNT(1) FROM Sessions WHERE UserID = ?`, userID).Scan(&n); err != nil {
		t.Fatalf("count sessions: %v", err)
	}
	return n
}

func TestCreateUserStoresAccountAndRejectsDuplicateNames(t *testing.T) {
	store := migratedStore(t)
	ctx := context.Background()
	now := time.Date(2026, 11, 26, 12, 0, 0, 0, time.UTC)
	hash, err := hashForTest("a sufficiently long password")
	if err != nil {
		t.Fatalf("hashForTest: %v", err)
	}

	created, err := store.CreateUser(ctx, domain.User{
		UserName: "  Keeper  ", PasswordHash: hash, AccessLevel: string(domain.RoleOfficer),
		MustChangePassword: true, ModAt: now, ModBy: "admin",
	})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if created.ID == 0 || created.UserName != "Keeper" {
		t.Fatalf("created = %+v; want a stored id and the trimmed name", created)
	}
	if created.Role() != domain.RoleOfficer || !created.MustChangePassword || created.Disabled {
		t.Fatalf("created role/flags = %+v", created)
	}
	if !created.CreatedAt.Equal(now) || !created.ModAt.Equal(now) || created.ModBy != "admin" {
		t.Fatalf("created timestamps = %+v", created)
	}
	if created.LastLogin != nil {
		t.Fatalf("new account has a last login: %v", created.LastLogin)
	}

	// Uniqueness is case-insensitive, matching the index created by the role
	// migration.
	if _, err := store.CreateUser(ctx, domain.User{
		UserName: "KEEPER", PasswordHash: hash, AccessLevel: string(domain.RoleMember),
	}); !errors.Is(err, domain.ErrAlreadyExists) {
		t.Fatalf("duplicate name = %v, want ErrAlreadyExists", err)
	}
	byName, err := store.GetUserByName(ctx, "keeper")
	if err != nil || byName.ID != created.ID {
		t.Fatalf("GetUserByName = %+v, %v; want the created account", byName, err)
	}

	if _, err := store.CreateUser(ctx, domain.User{
		UserName: "", PasswordHash: hash, AccessLevel: string(domain.RoleMember),
	}); !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("blank name = %v, want ErrValidation", err)
	}
	if _, err := store.CreateUser(ctx, domain.User{
		UserName: "Root", PasswordHash: hash, AccessLevel: "Root",
	}); !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("unknown role = %v, want ErrValidation", err)
	}
	if _, err := store.CreateUser(ctx, domain.User{
		UserName: "Hashless", AccessLevel: string(domain.RoleMember),
	}); !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("missing hash = %v, want ErrValidation", err)
	}
}

func TestListUsersOrdersByUsername(t *testing.T) {
	store := migratedStore(t)
	ctx := context.Background()
	hash, err := hashForTest("a sufficiently long password")
	if err != nil {
		t.Fatalf("hashForTest: %v", err)
	}
	for _, name := range []string{"zoe", "Alice", "bob"} {
		if _, err := store.CreateUser(ctx, domain.User{
			UserName: name, PasswordHash: hash, AccessLevel: string(domain.RoleMember),
		}); err != nil {
			t.Fatalf("CreateUser(%s): %v", name, err)
		}
	}

	users, err := store.ListUsers(ctx)
	if err != nil {
		t.Fatalf("ListUsers: %v", err)
	}
	got := make([]string, 0, len(users))
	for _, u := range users {
		got = append(got, u.UserName)
	}
	if want := "Alice,bob,zoe"; strings.Join(got, ",") != want {
		t.Fatalf("ListUsers order = %v, want %s", got, want)
	}
}

func TestResetUserPasswordRevokesSessionsAndRequiresChange(t *testing.T) {
	store := migratedStore(t)
	ctx := context.Background()
	now := time.Date(2026, 11, 26, 12, 0, 0, 0, time.UTC)
	adminID := insertUserForTest(t, store, "Solo", domain.RoleAdmin, false)
	targetID := insertUserForTest(t, store, "Keeper", domain.RoleOfficer, false)
	createSessionForTest(t, store, "sid-one", "th-one", targetID, now)
	createSessionForTest(t, store, "sid-two", "th-two", targetID, now)
	createSessionForTest(t, store, "sid-admin", "th-admin", adminID, now)

	updated, err := store.ResetUserPassword(ctx, targetID, "$2a$04$temporaryhash", now, adminActorForTest())
	if err != nil {
		t.Fatalf("ResetUserPassword: %v", err)
	}
	if !updated.MustChangePassword {
		t.Fatal("reset did not require a password change")
	}
	if updated.PasswordHash != "$2a$04$temporaryhash" || updated.ModBy != "admin" || !updated.ModAt.Equal(now) {
		t.Fatalf("updated = %+v", updated)
	}
	if n := countSessionsForTest(t, store, targetID); n != 0 {
		t.Fatalf("target sessions = %d, want 0", n)
	}
	if n := countSessionsForTest(t, store, adminID); n != 1 {
		t.Fatalf("admin sessions = %d, want 1", n)
	}

	// Members can never reset a password, and the rank ceiling stops an Officer
	// from touching an Administrator even though Officer-rank work is normally
	// theirs.
	memberActor := domain.Actor{UserID: 9, UserName: "member", Role: domain.RoleMember}
	if _, err := store.ResetUserPassword(ctx, targetID, "hash", now, memberActor); !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("member actor = %v, want ErrForbidden", err)
	}
	resetterID := insertUserForTest(t, store, "Resetter", domain.RoleOfficer, false)
	officerActor := domain.Actor{UserID: resetterID, UserName: "Resetter", Role: domain.RoleOfficer}
	if _, err := store.ResetUserPassword(ctx, targetID, "$2a$04$officerreset", now, officerActor); err != nil {
		t.Fatalf("officer resetting an equal-rank account: %v", err)
	}
	if _, err := store.ResetUserPassword(ctx, adminID, "hash", now, officerActor); !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("officer resetting an administrator = %v, want ErrForbidden", err)
	}
	if _, err := store.ResetUserPassword(ctx, 999, "hash", now, adminActorForTest()); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("unknown account = %v, want ErrNotFound", err)
	}
	if _, err := store.ResetUserPassword(ctx, targetID, "  ", now, adminActorForTest()); !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("blank hash = %v, want ErrValidation", err)
	}
}

func TestSetOwnPasswordKeepsCurrentSession(t *testing.T) {
	store := migratedStore(t)
	ctx := context.Background()
	now := time.Date(2026, 11, 26, 12, 0, 0, 0, time.UTC)
	adminID := insertUserForTest(t, store, "Solo", domain.RoleAdmin, false)
	officerID := insertUserForTest(t, store, "Keeper", domain.RoleOfficer, false)

	if _, err := store.ResetUserPassword(ctx, officerID, "$2a$04$temporaryhash", now, adminActorForTest()); err != nil {
		t.Fatalf("ResetUserPassword: %v", err)
	}
	createSessionForTest(t, store, "sid-current", "th-current", officerID, now)
	createSessionForTest(t, store, "sid-other", "th-other", officerID, now)

	updated, err := store.SetOwnPassword(ctx, officerID, "$2a$04$permanenthash", now.Add(time.Minute), "sid-current")
	if err != nil {
		t.Fatalf("SetOwnPassword: %v", err)
	}
	if updated.MustChangePassword {
		t.Fatal("own password change did not clear MustChangePassword")
	}
	if updated.PasswordHash != "$2a$04$permanenthash" || updated.ModBy != "Keeper" {
		t.Fatalf("updated = %+v", updated)
	}
	if n := countSessionsForTest(t, store, officerID); n != 1 {
		t.Fatalf("sessions = %d, want the current one only", n)
	}
	var sessionID string
	if err := store.DB().QueryRowContext(ctx,
		`SELECT SessionID FROM Sessions WHERE UserID = ?`, officerID).Scan(&sessionID); err != nil {
		t.Fatalf("read surviving session: %v", err)
	}
	if sessionID != "sid-current" {
		t.Fatalf("surviving session = %q, want sid-current", sessionID)
	}
	if n := countSessionsForTest(t, store, adminID); n != 0 {
		t.Fatalf("admin sessions = %d, want 0", n)
	}

	// A blank keepSessionID revokes every session.
	if _, err := store.SetOwnPassword(ctx, officerID, "$2a$04$anotherhash", now.Add(time.Hour), ""); err != nil {
		t.Fatalf("SetOwnPassword(no keep): %v", err)
	}
	if n := countSessionsForTest(t, store, officerID); n != 0 {
		t.Fatalf("sessions after a blank keep id = %d, want 0", n)
	}
	if _, err := store.SetOwnPassword(ctx, 999, "hash", now, ""); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("unknown account = %v, want ErrNotFound", err)
	}
}

func TestDeleteUserGuardsLastAdministrator(t *testing.T) {
	store := migratedStore(t)
	ctx := context.Background()
	now := time.Date(2026, 11, 26, 12, 0, 0, 0, time.UTC)
	soloID := insertUserForTest(t, store, "Solo", domain.RoleAdmin, false)
	createSessionForTest(t, store, "sid-solo", "th-solo", soloID, now)

	if err := store.DeleteUser(ctx, soloID, adminActorForTest()); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("deleting the only administrator = %v, want ErrConflict", err)
	}
	if _, err := store.GetUserByID(ctx, soloID); err != nil {
		t.Fatalf("administrator was removed by a refused delete: %v", err)
	}

	secondID := insertUserForTest(t, store, "Second", domain.RoleAdmin, false)
	officerID := insertUserForTest(t, store, "Keeper", domain.RoleOfficer, false)
	createSessionForTest(t, store, "sid-officer", "th-officer", officerID, now)

	if err := store.DeleteUser(ctx, officerID, adminActorForTest()); err != nil {
		t.Fatalf("delete officer: %v", err)
	}
	if _, err := store.GetUserByID(ctx, officerID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("deleted account is still readable: %v", err)
	}
	if n := countSessionsForTest(t, store, officerID); n != 0 {
		t.Fatalf("deleted account sessions = %d, want 0", n)
	}

	nonAdmin := domain.Actor{UserID: 7, UserName: "officer", Role: domain.RoleOfficer}
	if err := store.DeleteUser(ctx, secondID, nonAdmin); !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("non-admin actor = %v, want ErrForbidden", err)
	}
	if err := store.DeleteUser(ctx, 999, adminActorForTest()); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("unknown account = %v, want ErrNotFound", err)
	}
	if err := store.DeleteUser(ctx, secondID, adminActorForTest()); err != nil {
		t.Fatalf("delete second administrator while another remains: %v", err)
	}
	if err := store.DeleteUser(ctx, soloID, adminActorForTest()); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("deleting the last administrator after the second is gone = %v, want ErrConflict", err)
	}
}
