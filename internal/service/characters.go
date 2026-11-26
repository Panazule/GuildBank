package service

import (
	"context"
	"log/slog"
	"strconv"
	"time"

	"github.com/Panazule/guildbank/internal/domain"
)

// CharacterStore is the profile persistence surface. Ownership is part of every
// query: the user ID is never applied as a filter after the fact.
type CharacterStore interface {
	ListCharacters(ctx context.Context, userID int64) ([]domain.Character, error)
	CountCharacters(ctx context.Context, userID int64) (int, error)
	AddCharacter(ctx context.Context, userID int64, name string, at time.Time, createdBy string) (domain.Character, error)
	DeleteCharacter(ctx context.Context, characterID, userID int64) (domain.Character, error)
}

// CharacterService owns the per-account character list. Members manage their own
// names; an Administrator may remove any account's name (owner decision D4) but
// can never move one to a different account.
type CharacterService struct {
	store  CharacterStore
	audit  AuditSink
	logger *slog.Logger
	now    func() time.Time
}

// NewCharacterService builds the profile service. audit may be nil when no guild
// store is configured, exactly as for account administration.
func NewCharacterService(store CharacterStore, audit AuditSink, logger *slog.Logger) *CharacterService {
	return &CharacterService{store: store, audit: audit, logger: logger, now: time.Now}
}

// Ready reports whether profile storage is available.
func (s *CharacterService) Ready() bool { return s != nil && s.store != nil }

func (s *CharacterService) requireReady() error {
	if !s.Ready() {
		return domain.ErrUnavailable
	}
	return nil
}

// List returns one account's names, ordered case-insensitively.
func (s *CharacterService) List(ctx context.Context, userID int64) ([]domain.Character, error) {
	if err := s.requireReady(); err != nil {
		return nil, err
	}
	if userID <= 0 {
		return nil, domain.ErrUnauthorized
	}
	return s.store.ListCharacters(ctx, userID)
}

// Add stores one name for the signed-in account. The global uniqueness and the
// per-account cap are enforced by the store.
func (s *CharacterService) Add(ctx context.Context, user domain.User, in domain.AddCharacterInput,
	correlationID, sourceIP string) (domain.Character, error) {
	if err := s.requireReady(); err != nil {
		return domain.Character{}, err
	}
	// A pending account holds no session, so this is defence in depth: the name
	// space is globally unique and only an approved account may claim a name.
	if !user.IsEnabled() {
		return domain.Character{}, domain.ErrUnauthorized
	}
	if err := in.Validate(); err != nil {
		return domain.Character{}, err
	}
	at := s.now().UTC()
	character, err := s.store.AddCharacter(ctx, user.ID, in.Normalized(), at, user.UserName)
	if err != nil {
		return domain.Character{}, err
	}
	actor := domain.Actor{UserID: user.ID, UserName: user.UserName, Role: user.Role(),
		IsAdmin: user.IsAdmin()}
	writeAudit(ctx, s.audit, s.logger, at, "CHARACTER_ADDED", "UserCharacters",
		strconv.FormatInt(character.ID, 10), actor, map[string]string{},
		map[string]string{"characterName": character.Name, "userName": user.UserName},
		correlationID, sourceIP)
	return character, nil
}

// Remove deletes one name owned by the signed-in account.
func (s *CharacterService) Remove(ctx context.Context, user domain.User, characterID int64,
	correlationID, sourceIP string) error {
	if err := s.requireReady(); err != nil {
		return err
	}
	if characterID <= 0 {
		return domain.ErrNotFound
	}
	removed, err := s.store.DeleteCharacter(ctx, characterID, user.ID)
	if err != nil {
		return err
	}
	actor := domain.Actor{UserID: user.ID, UserName: user.UserName, Role: user.Role(),
		IsAdmin: user.IsAdmin()}
	writeAudit(ctx, s.audit, s.logger, s.now().UTC(), "CHARACTER_REMOVED", "UserCharacters",
		strconv.FormatInt(removed.ID, 10), actor,
		map[string]string{"characterName": removed.Name, "userName": user.UserName},
		map[string]string{}, correlationID, sourceIP)
	return nil
}

// RemoveForUser lets an Administrator remove a name from another account. The
// character must belong to the supplied user, so an administrator cannot use
// this to move or claim ownership.
func (s *CharacterService) RemoveForUser(ctx context.Context, actor domain.Actor,
	userID, characterID int64, correlationID, sourceIP string) error {
	if err := s.requireReady(); err != nil {
		return err
	}
	if !actor.IsAdmin {
		return domain.ErrForbidden
	}
	if userID <= 0 || characterID <= 0 {
		return domain.ErrNotFound
	}
	removed, err := s.store.DeleteCharacter(ctx, characterID, userID)
	if err != nil {
		return err
	}
	writeAudit(ctx, s.audit, s.logger, s.now().UTC(), "CHARACTER_REMOVED", "UserCharacters",
		strconv.FormatInt(removed.ID, 10), actor,
		map[string]string{"characterName": removed.Name, "userId": strconv.FormatInt(userID, 10)},
		map[string]string{}, correlationID, sourceIP)
	return nil
}
