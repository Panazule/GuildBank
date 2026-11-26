package sqlite

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Panazule/guildbank/internal/domain"
)

func TestDiscordIdentityRoundTrip(t *testing.T) {
	store := migratedStore(t)
	ctx := context.Background()
	userID := insertUserForTest(t, store, "Keeper", domain.RoleMember, false)

	identity, err := store.DiscordIdentity(ctx, userID)
	if err != nil {
		t.Fatalf("DiscordIdentity (unlinked): %v", err)
	}
	if identity.Linked() {
		t.Fatalf("a fresh account is linked: %+v", identity)
	}

	at := time.Date(2026, 11, 26, 12, 0, 0, 0, time.UTC)
	if err := store.SetDiscordIdentity(ctx, userID, "123456789012345678", "Keldor", at, "Keeper"); err != nil {
		t.Fatalf("SetDiscordIdentity: %v", err)
	}
	identity, err = store.DiscordIdentity(ctx, userID)
	if err != nil {
		t.Fatalf("DiscordIdentity: %v", err)
	}
	if !identity.Linked() || identity.DiscordUserID != "123456789012345678" ||
		identity.DiscordUserName != "Keldor" || identity.LinkedAt == nil || !identity.LinkedAt.Equal(at) {
		t.Fatalf("identity = %+v", identity)
	}
	owner, err := store.DiscordUserIDOwner(ctx, "123456789012345678")
	if err != nil || owner != userID {
		t.Fatalf("DiscordUserIDOwner = %d, %v; want %d", owner, err, userID)
	}

	if err := store.ClearDiscordIdentity(ctx, userID, at.Add(time.Hour), "Keeper"); err != nil {
		t.Fatalf("ClearDiscordIdentity: %v", err)
	}
	identity, err = store.DiscordIdentity(ctx, userID)
	if err != nil || identity.Linked() {
		t.Fatalf("after clear = %+v, %v; want unlinked", identity, err)
	}
	if _, err := store.DiscordUserIDOwner(ctx, "123456789012345678"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("owner after clear = %v, want ErrNotFound", err)
	}
}

func TestDiscordIdentityCannotBeShared(t *testing.T) {
	store := migratedStore(t)
	ctx := context.Background()
	first := insertUserForTest(t, store, "Keeper", domain.RoleMember, false)
	second := insertUserForTest(t, store, "Second", domain.RoleMember, false)
	at := time.Date(2026, 11, 26, 12, 0, 0, 0, time.UTC)

	if err := store.SetDiscordIdentity(ctx, first, "123456789012345678", "Keldor", at, "Keeper"); err != nil {
		t.Fatalf("first link: %v", err)
	}
	if err := store.SetDiscordIdentity(ctx, second, "123456789012345678", "Keldor", at, "Second"); !errors.Is(err, domain.ErrAlreadyExists) {
		t.Fatalf("shared link = %v, want ErrAlreadyExists", err)
	}
	identity, err := store.DiscordIdentity(ctx, second)
	if err != nil || identity.Linked() {
		t.Fatalf("second account = %+v, %v; want unlinked", identity, err)
	}

	// Relinking the same account, and taking over a released identity, work.
	if err := store.SetDiscordIdentity(ctx, first, "123456789012345678", "Keldor (renamed)", at, "Keeper"); err != nil {
		t.Fatalf("relink: %v", err)
	}
	if err := store.ClearDiscordIdentity(ctx, first, at, "Keeper"); err != nil {
		t.Fatalf("clear: %v", err)
	}
	if err := store.SetDiscordIdentity(ctx, second, "123456789012345678", "Keldor", at, "Second"); err != nil {
		t.Fatalf("link after release: %v", err)
	}
}

func TestDiscordIdentityUnknownAccount(t *testing.T) {
	store := migratedStore(t)
	ctx := context.Background()
	if _, err := store.DiscordIdentity(ctx, 999); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("DiscordIdentity(999) = %v, want ErrNotFound", err)
	}
	if err := store.SetDiscordIdentity(ctx, 999, "123", "x", time.Now(), "x"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("SetDiscordIdentity(999) = %v, want ErrNotFound", err)
	}
	if err := store.ClearDiscordIdentity(ctx, 999, time.Now(), "x"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("ClearDiscordIdentity(999) = %v, want ErrNotFound", err)
	}
}
