package domain

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
