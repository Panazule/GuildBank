package domain

// ItemType is the inventory kind stored in GuildBankItems. The two values are
// the only values accepted by the MariaDB ENUM (Design.md L266-L374).
type ItemType string

const (
	ItemTypeItem  ItemType = "Item"
	ItemTypeSpell ItemType = "Spell"
)

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
