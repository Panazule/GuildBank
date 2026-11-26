package domain

import (
	"strings"
	"time"
	"unicode/utf8"
)

// Character limits. The name rules mirror EverQuest's own alphabet, and the cap
// bounds the requester dropdown the request workflow will build from this list
// (owner decisions D1-D3).
const (
	CharacterMinRunes   = 3
	CharacterMaxRunes   = 15
	CharacterMaxPerUser = 10
)

// Character is one EverQuest character name owned by an account. Names are
// unique across the whole installation, so a name identifies exactly one account.
type Character struct {
	ID        int64
	UserID    int64
	Name      string
	CreatedAt time.Time
	CreatedBy string
}

// NormalizeCharacterName trims the surrounding whitespace a form or API client
// may send. Stored names are always normalized.
func NormalizeCharacterName(name string) string { return strings.TrimSpace(name) }

// ValidateCharacterName enforces 3-15 ASCII letters. Uniqueness is a store
// concern because it is case-insensitive and index-backed.
func ValidateCharacterName(name string) error {
	if problem := characterNameProblem(name); problem != "" {
		return NewValidationError("characterName", problem)
	}
	return nil
}

func characterNameProblem(name string) string {
	normalized := NormalizeCharacterName(name)
	if normalized == "" {
		return "must not be blank"
	}
	if !utf8.ValidString(normalized) {
		return "must contain letters only"
	}
	count := utf8.RuneCountInString(normalized)
	if count < CharacterMinRunes {
		return "must be at least 3 letters"
	}
	if count > CharacterMaxRunes {
		return "must be at most 15 letters"
	}
	for _, r := range normalized {
		if (r < 'A' || r > 'Z') && (r < 'a' || r > 'z') {
			return "must contain letters only"
		}
	}
	return ""
}

// AddCharacterInput is a profile owner's request to add one name.
type AddCharacterInput struct {
	Name string
}

// Validate checks the structural rules that do not need the store.
func (in AddCharacterInput) Validate() error {
	if problem := characterNameProblem(in.Name); problem != "" {
		return NewValidationError("characterName", problem)
	}
	return nil
}

// Normalized returns the name that should be stored.
func (in AddCharacterInput) Normalized() string { return NormalizeCharacterName(in.Name) }
