package sqlite

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Panazule/guildbank/internal/domain"
)

func TestAddAndListCharacters(t *testing.T) {
	store := migratedStore(t)
	ctx := context.Background()
	now := time.Date(2026, 11, 26, 12, 0, 0, 0, time.UTC)
	userID := insertUserForTest(t, store, "Keeper", domain.RoleMember, false)

	for _, name := range []string{"zoe", "Icefear", "Banktoon"} {
		if _, err := store.AddCharacter(ctx, userID, name, now, "Keeper"); err != nil {
			t.Fatalf("AddCharacter(%s): %v", name, err)
		}
	}
	characters, err := store.ListCharacters(ctx, userID)
	if err != nil {
		t.Fatalf("ListCharacters: %v", err)
	}
	names := make([]string, 0, len(characters))
	for _, character := range characters {
		names = append(names, character.Name)
	}
	if want := "Banktoon,Icefear,zoe"; strings.Join(names, ",") != want {
		t.Fatalf("order = %v, want %s", names, want)
	}
	if characters[1].CreatedBy != "Keeper" || !characters[1].CreatedAt.Equal(now) {
		t.Fatalf("row metadata = %+v", characters[1])
	}

	if n, err := store.CountCharacters(ctx, userID); err != nil || n != 3 {
		t.Fatalf("CountCharacters = %d, %v; want 3", n, err)
	}
}

func TestCharacterNamesAreGloballyUniqueCaseInsensitively(t *testing.T) {
	store := migratedStore(t)
	ctx := context.Background()
	now := time.Date(2026, 11, 26, 12, 0, 0, 0, time.UTC)
	first := insertUserForTest(t, store, "Keeper", domain.RoleMember, false)
	second := insertUserForTest(t, store, "Second", domain.RoleMember, false)

	if _, err := store.AddCharacter(ctx, first, "Icefear", now, "Keeper"); err != nil {
		t.Fatalf("first add: %v", err)
	}
	for _, name := range []string{"Icefear", "icefear", "ICEFEAR"} {
		if _, err := store.AddCharacter(ctx, second, name, now, "Second"); !errors.Is(err, domain.ErrAlreadyExists) {
			t.Fatalf("duplicate %q = %v, want ErrAlreadyExists", name, err)
		}
	}
	// The same account cannot repeat a name either, even with different case.
	if _, err := store.AddCharacter(ctx, first, "ICEFEAR", now, "Keeper"); !errors.Is(err, domain.ErrAlreadyExists) {
		t.Fatalf("own duplicate = %v, want ErrAlreadyExists", err)
	}
}

func TestCharacterCapPerAccount(t *testing.T) {
	store := migratedStore(t)
	ctx := context.Background()
	now := time.Date(2026, 11, 26, 12, 0, 0, 0, time.UTC)
	userID := insertUserForTest(t, store, "Keeper", domain.RoleMember, false)

	for i := 0; i < domain.CharacterMaxPerUser; i++ {
		name := "Name" + string(rune('A'+i))
		if _, err := store.AddCharacter(ctx, userID, name, now, "Keeper"); err != nil {
			t.Fatalf("add %d: %v", i, err)
		}
	}
	if _, err := store.AddCharacter(ctx, userID, "OneTooMany", now, "Keeper"); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("over the cap = %v, want ErrConflict", err)
	}
}

func TestDeleteCharacterIsOwnershipScoped(t *testing.T) {
	store := migratedStore(t)
	ctx := context.Background()
	now := time.Date(2026, 11, 26, 12, 0, 0, 0, time.UTC)
	owner := insertUserForTest(t, store, "Keeper", domain.RoleMember, false)
	other := insertUserForTest(t, store, "Second", domain.RoleMember, false)

	character, err := store.AddCharacter(ctx, owner, "Icefear", now, "Keeper")
	if err != nil {
		t.Fatalf("AddCharacter: %v", err)
	}
	if _, err := store.DeleteCharacter(ctx, character.ID, other); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("delete by another account = %v, want ErrNotFound", err)
	}
	if n, _ := store.CountCharacters(ctx, owner); n != 1 {
		t.Fatalf("count after a refused delete = %d, want 1", n)
	}

	removed, err := store.DeleteCharacter(ctx, character.ID, owner)
	if err != nil {
		t.Fatalf("DeleteCharacter: %v", err)
	}
	if removed.Name != "Icefear" {
		t.Fatalf("removed = %+v", removed)
	}
	if _, err := store.DeleteCharacter(ctx, character.ID, owner); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("second delete = %v, want ErrNotFound", err)
	}
	// The name becomes available again.
	if _, err := store.AddCharacter(ctx, other, "Icefear", now, "Second"); err != nil {
		t.Fatalf("re-add after delete: %v", err)
	}
}

func TestCharactersCascadeOnUserDelete(t *testing.T) {
	store := migratedStore(t)
	ctx := context.Background()
	now := time.Date(2026, 11, 26, 12, 0, 0, 0, time.UTC)
	adminID := insertUserForTest(t, store, "Solo", domain.RoleAdmin, false)
	targetID := insertUserForTest(t, store, "Keeper", domain.RoleMember, false)
	if _, err := store.AddCharacter(ctx, targetID, "Icefear", now, "Keeper"); err != nil {
		t.Fatalf("AddCharacter: %v", err)
	}
	actor := domain.Actor{UserID: adminID, UserName: "Solo", IsAdmin: true, Role: domain.RoleAdmin}
	if err := store.DeleteUser(ctx, targetID, actor); err != nil {
		t.Fatalf("DeleteUser: %v", err)
	}
	var count int
	if err := store.DB().QueryRowContext(ctx,
		`SELECT COUNT(1) FROM UserCharacters WHERE UserID = ?`, targetID).Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 0 {
		t.Fatalf("characters after user delete = %d, want 0", count)
	}
}
