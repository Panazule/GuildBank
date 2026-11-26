package sqlite

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Panazule/guildbank/internal/domain"
)

func TestSetUserAccessLevelGuardsLastAdministrator(t *testing.T) {
	store := migratedStore(t)
	ctx := context.Background()
	adminID := insertUserForTest(t, store, "Solo", domain.RoleAdmin, false)
	actor := adminActorForTest()

	if _, err := store.SetUserAccessLevel(ctx, adminID, domain.RoleOfficer, time.Now().UTC(), actor); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("demoting the only administrator = %v, want ErrConflict", err)
	}
	still, err := store.GetUserByID(ctx, adminID)
	if err != nil || !still.IsAdmin() {
		t.Fatalf("administrator changed after a refused demotion: %+v, %v", still, err)
	}

	secondID := insertUserForTest(t, store, "Second", domain.RoleAdmin, false)
	updated, err := store.SetUserAccessLevel(ctx, adminID, domain.RoleOfficer, time.Now().UTC(), actor)
	if err != nil {
		t.Fatalf("demote with a remaining administrator: %v", err)
	}
	if updated.Role() != domain.RoleOfficer {
		t.Fatalf("role = %q, want Officer", updated.Role())
	}

	if _, err := store.SetUserAccessLevel(ctx, secondID, domain.Role("Root"), time.Now().UTC(), actor); !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("unknown role error = %v, want ErrValidation", err)
	}
	if _, err := store.SetUserAccessLevel(ctx, secondID, domain.RoleMember, time.Now().UTC(),
		domain.Actor{UserID: 9, UserName: "officer", Role: domain.RoleOfficer}); !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("non-admin actor error = %v, want ErrForbidden", err)
	}
}

func TestSetUserDisabledRevokesSessionsAndGuardsLastAdministrator(t *testing.T) {
	store := migratedStore(t)
	ctx := context.Background()
	now := time.Date(2026, 11, 26, 12, 0, 0, 0, time.UTC)
	adminID := insertUserForTest(t, store, "Solo", domain.RoleAdmin, false)
	officerID := insertUserForTest(t, store, "Keeper", domain.RoleOfficer, false)
	actor := adminActorForTest()

	if _, err := store.SetUserDisabled(ctx, adminID, true, now, actor); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("disabling the only administrator = %v, want ErrConflict", err)
	}

	hash := "th-officer"
	if err := store.CreateSession(ctx, domain.Session{
		ID: "sid-officer", TokenHash: hash, UserID: officerID,
		CreatedAt: now, LastAccessed: now, ExpiresAt: now.Add(time.Hour),
		AbsoluteExpiresAt: now.Add(8 * time.Hour),
	}); err != nil {
		t.Fatalf("CreateSession(officer): %v", err)
	}
	if err := store.CreateSession(ctx, domain.Session{
		ID: "sid-admin", TokenHash: "th-admin", UserID: adminID,
		CreatedAt: now, LastAccessed: now, ExpiresAt: now.Add(time.Hour),
		AbsoluteExpiresAt: now.Add(8 * time.Hour),
	}); err != nil {
		t.Fatalf("CreateSession(admin): %v", err)
	}

	updated, err := store.SetUserDisabled(ctx, officerID, true, now, actor)
	if err != nil {
		t.Fatalf("disable officer: %v", err)
	}
	if !updated.Disabled {
		t.Fatal("officer was not disabled")
	}
	var officerSessions, adminSessions int
	if err := store.DB().QueryRowContext(ctx,
		`SELECT COUNT(1) FROM Sessions WHERE UserID = ?`, officerID).Scan(&officerSessions); err != nil {
		t.Fatalf("count officer sessions: %v", err)
	}
	if err := store.DB().QueryRowContext(ctx,
		`SELECT COUNT(1) FROM Sessions WHERE UserID = ?`, adminID).Scan(&adminSessions); err != nil {
		t.Fatalf("count admin sessions: %v", err)
	}
	if officerSessions != 0 || adminSessions != 1 {
		t.Fatalf("sessions = officer %d / admin %d; want 0 / 1", officerSessions, adminSessions)
	}

	// Disabled accounts do not count toward the guard.
	secondID := insertUserForTest(t, store, "Second", domain.RoleAdmin, false)
	if _, err := store.SetUserDisabled(ctx, secondID, true, now, actor); err != nil {
		t.Fatalf("disable second administrator: %v", err)
	}
	if _, err := store.SetUserDisabled(ctx, adminID, true, now, actor); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("disabling the last enabled administrator = %v, want ErrConflict", err)
	}
}

// A role change must be visible on the next session read, because the session
// lookup joins Users rather than caching the principal.
func TestRoleChangeAppliesToExistingSession(t *testing.T) {
	store := migratedStore(t)
	ctx := context.Background()
	now := time.Date(2026, 11, 26, 12, 0, 0, 0, time.UTC)
	adminID := insertUserForTest(t, store, "Solo", domain.RoleAdmin, false)
	officerID := insertUserForTest(t, store, "Keeper", domain.RoleOfficer, false)
	if err := store.CreateSession(ctx, domain.Session{
		ID: "sid", TokenHash: "th", UserID: officerID,
		CreatedAt: now, LastAccessed: now, ExpiresAt: now.Add(time.Hour),
		AbsoluteExpiresAt: now.Add(8 * time.Hour),
	}); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	if _, err := store.SetUserAccessLevel(ctx, officerID, domain.RoleAdmin, now,
		domain.Actor{UserID: adminID, UserName: "Solo", IsAdmin: true, Role: domain.RoleAdmin}); err != nil {
		t.Fatalf("promote: %v", err)
	}
	session, err := store.FindSessionByTokenHash(ctx, "th")
	if err != nil {
		t.Fatalf("FindSessionByTokenHash: %v", err)
	}
	if !session.User.IsAdmin() {
		t.Fatal("the promoted role is not visible on the existing session")
	}
}
