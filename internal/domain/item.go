package domain

import (
	"fmt"
	"math"
	"strings"
)

// ItemType is the inventory kind stored in GuildBankItems. The two values are
// the only values accepted by the MariaDB ENUM (Design.md L266-L374).
type ItemType string

const (
	ItemTypeItem  ItemType = "Item"
	ItemTypeSpell ItemType = "Spell"
)

// UnresolvedEQUID is the documented placeholder identity for a manually added
// inventory row whose item could not be resolved against the Items source.
//
// It is the maximum value of GuildBankItems.EQUID (INT UNSIGNED), which is far
// above every real EverQuest item identifier, so it can never collide with
// source metadata. The owner approved it on 2026-11-26 as an AM-01 exception
// for manual entry only: imports always carry their real EQUID and existing
// rows are never rewritten. The value is server-assigned only — a client may
// never submit it — and MetadataMissingFg is set on such a row. While the
// placeholder is in place the stored Name is the authoritative identity.
const UnresolvedEQUID uint64 = math.MaxUint32

// IsUnresolvedEQUID reports whether the identifier is the unresolved-identity
// placeholder. A zero identifier (identity not supplied) is not unresolved.
func IsUnresolvedEQUID(equid uint64) bool { return equid == UnresolvedEQUID }

// ItemNameAmbiguousError reports that a supplied item name matched more than
// one metadata row. It is a validation failure that also carries the bounded
// candidate list so the API and the HTML form can offer a choice instead of
// guessing. It implements Unwrap so errors.Is(err, ErrValidation) holds.
type ItemNameAmbiguousError struct {
	Name       string
	Candidates []Item
}

func (e *ItemNameAmbiguousError) Error() string {
	if e == nil {
		return "item name is ambiguous"
	}
	ids := make([]string, 0, len(e.Candidates))
	for _, candidate := range e.Candidates {
		ids = append(ids, fmt.Sprintf("%d", candidate.EQUID))
	}
	return fmt.Sprintf("item name %q matches more than one item (EQUIDs %s)",
		e.Name, strings.Join(ids, ", "))
}

// Unwrap allows errors.Is(err, ErrValidation).
func (e *ItemNameAmbiguousError) Unwrap() error { return ErrValidation }

// Item is the application's logical view of the externally owned item
// metadata. The MariaDB repository maps the physical source columns to these
// fields; callers must not depend on the source names.
//
// D-01 makes EQUID the canonical key and Items.ID is the same value. ClassList
// and RaceList remain encoded masks until a decoder service converts them into
// the ClassString and RaceString stored in GuildBankItems.
type Item struct {
	EQUID     uint64
	Name      string
	ClassList uint64
	RaceList  uint64
}

// ItemReference is the minimum imported item identity needed to resolve
// metadata. Name is the imported snapshot and is used only when EQUID is
// absent from Items (D-08, AM-01/AM-02).
type ItemReference struct {
	EQUID uint64
	Name  string
}
