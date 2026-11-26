package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Panazule/guildbank/internal/domain"
)

// fakeCharacterStore mirrors the store's invariants (global uniqueness, the cap,
// ownership-scoped delete) so the service contract is tested against realistic
// behaviour.
type fakeCharacterStore struct {
	characters []domain.Character
	nextID     int64
}

func newFakeCharacterStore() *fakeCharacterStore {
	return &fakeCharacterStore{nextID: 1}
}

func (f *fakeCharacterStore) ListCharacters(ctx context.Context, userID int64) ([]domain.Character, error) {
	out := make([]domain.Character, 0, len(f.characters))
	for _, character := range f.characters {
		if character.UserID == userID {
			out = append(out, character)
		}
	}
	return out, nil
}

func (f *fakeCharacterStore) CountCharacters(ctx context.Context, userID int64) (int, error) {
	count := 0
	for _, character := range f.characters {
		if character.UserID == userID {
			count++
		}
	}
	return count, nil
}

func (f *fakeCharacterStore) AddCharacter(ctx context.Context, userID int64, name string,
	at time.Time, createdBy string) (domain.Character, error) {
	count := 0
	for _, character := range f.characters {
		if strings.EqualFold(character.Name, name) {
			return domain.Character{}, domain.ErrAlreadyExists
		}
		if character.UserID == userID {
			count++
		}
	}
	if count >= domain.CharacterMaxPerUser {
		return domain.Character{}, domain.ErrConflict
	}
	character := domain.Character{ID: f.nextID, UserID: userID, Name: name,
		CreatedAt: at, CreatedBy: createdBy}
	f.nextID++
	f.characters = append(f.characters, character)
	return character, nil
}

func (f *fakeCharacterStore) DeleteCharacter(ctx context.Context, characterID, userID int64) (domain.Character, error) {
	for i, character := range f.characters {
		if character.ID == characterID && character.UserID == userID {
			f.characters = append(f.characters[:i], f.characters[i+1:]...)
			return character, nil
		}
	}
	return domain.Character{}, domain.ErrNotFound
}

func profileUser() domain.User {
	return domain.User{ID: 2, UserName: "Keeper", AccessLevel: string(domain.RoleMember)}
}

func TestAddCharacterValidatesAndAudits(t *testing.T) {
	store := newFakeCharacterStore()
	sink := &fakeAuditSink{}
	svc := NewCharacterService(store, sink, nil)

	character, err := svc.Add(context.Background(), profileUser(),
		domain.AddCharacterInput{Name: "  Icefear  "}, "req-1", "203.0.113.9")
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if character.Name != "Icefear" || character.UserID != 2 {
		t.Fatalf("character = %+v", character)
	}
	if len(sink.entries) != 1 {
		t.Fatalf("audit entries = %d, want 1", len(sink.entries))
	}
	entry := sink.entries[0]
	if entry.EventType != "CHARACTER_ADDED" || entry.TableName != "UserCharacters" ||
		entry.RecordID != "1" || entry.UserName != "Keeper" {
		t.Fatalf("audit entry = %+v", entry)
	}
	if entry.CorrelationID != "req-1" || entry.SourceIP != "203.0.113.9" {
		t.Fatalf("audit correlation/source = %+v", entry)
	}

	if _, err := svc.Add(context.Background(), profileUser(),
		domain.AddCharacterInput{Name: "ab"}, "", ""); !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("invalid name = %v, want ErrValidation", err)
	}
	if _, err := svc.Add(context.Background(), profileUser(),
		domain.AddCharacterInput{Name: "icefear"}, "", ""); !errors.Is(err, domain.ErrAlreadyExists) {
		t.Fatalf("duplicate name = %v, want ErrAlreadyExists", err)
	}
}

func TestRemoveCharacterIsOwnershipScoped(t *testing.T) {
	store := newFakeCharacterStore()
	sink := &fakeAuditSink{}
	svc := NewCharacterService(store, sink, nil)
	ctx := context.Background()

	character, err := svc.Add(ctx, profileUser(), domain.AddCharacterInput{Name: "Icefear"}, "", "")
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	other := domain.User{ID: 3, UserName: "Second", AccessLevel: string(domain.RoleMember)}
	if err := svc.Remove(ctx, other, character.ID, "", ""); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("remove another account's name = %v, want ErrNotFound", err)
	}
	if len(store.characters) != 1 {
		t.Fatal("a refused remove deleted the row")
	}

	if err := svc.Remove(ctx, profileUser(), character.ID, "req-2", ""); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if len(store.characters) != 0 {
		t.Fatal("the name was not removed")
	}
	if sink.entries[1].EventType != "CHARACTER_REMOVED" {
		t.Fatalf("audit entry = %+v", sink.entries[1])
	}
	if !strings.Contains(string(sink.entries[1].OldValues), "Icefear") {
		t.Fatalf("removal audit lost the name: %s", sink.entries[1].OldValues)
	}
}

func TestAdministratorCanRemoveButNotClaim(t *testing.T) {
	store := newFakeCharacterStore()
	sink := &fakeAuditSink{}
	svc := NewCharacterService(store, sink, nil)
	ctx := context.Background()

	character, err := svc.Add(ctx, profileUser(), domain.AddCharacterInput{Name: "Banktoon"}, "", "")
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	officer := domain.Actor{UserID: 9, UserName: "officer", Role: domain.RoleOfficer}
	if err := svc.RemoveForUser(ctx, officer, 2, character.ID, "", ""); !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("officer remove = %v, want ErrForbidden", err)
	}
	if len(store.characters) != 1 {
		t.Fatal("a forbidden remove deleted the row")
	}

	// The character must belong to the supplied account, so an administrator
	// cannot use this path to move ownership.
	admin := domain.Actor{UserID: 1, UserName: "admin", IsAdmin: true, Role: domain.RoleAdmin}
	if err := svc.RemoveForUser(ctx, admin, 999, character.ID, "", ""); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("mismatched owner = %v, want ErrNotFound", err)
	}
	if err := svc.RemoveForUser(ctx, admin, 2, character.ID, "req-3", ""); err != nil {
		t.Fatalf("admin remove: %v", err)
	}
	entry := sink.entries[len(sink.entries)-1]
	if entry.UserName != "admin" || entry.EventType != "CHARACTER_REMOVED" {
		t.Fatalf("administrator removal audit = %+v", entry)
	}
}

func TestCharacterAuditFailureDoesNotFailTheChange(t *testing.T) {
	store := newFakeCharacterStore()
	svc := NewCharacterService(store, &fakeAuditSink{err: errors.New("guild store down")}, nil)
	if _, err := svc.Add(context.Background(), profileUser(),
		domain.AddCharacterInput{Name: "Icefear"}, "", ""); err != nil {
		t.Fatalf("an audit failure must not fail the add: %v", err)
	}
	if len(store.characters) != 1 {
		t.Fatal("the character was not stored")
	}
}
