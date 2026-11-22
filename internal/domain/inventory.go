package domain

import "time"

// InventorySort is the allowlisted public inventory order. The zero value is
// the default natural-name order; no caller-supplied SQL expression is ever
// accepted.
type InventorySort string

const (
	InventorySortName           InventorySort = "name"
	InventorySortNameDesc       InventorySort = "name_desc"
	InventorySortDateAdded      InventorySort = "date_added"
	InventorySortDateAddedDesc  InventorySort = "date_added_desc"
	InventorySortDateOpen       InventorySort = "date_open"
	InventorySortDateOpenDesc   InventorySort = "date_open_desc"
	InventorySortDateBazaar     InventorySort = "date_bazaar"
	InventorySortDateBazaarDesc InventorySort = "date_bazaar_desc"
)

// InventorySorts is the canonical, ordered allowlist for API/UI validation.
var InventorySorts = []InventorySort{
	InventorySortName,
	InventorySortNameDesc,
	InventorySortDateAdded,
	InventorySortDateAddedDesc,
	InventorySortDateOpen,
	InventorySortDateOpenDesc,
	InventorySortDateBazaar,
	InventorySortDateBazaarDesc,
}

// Valid reports whether the sort is one of the eight approved values.
func (s InventorySort) Valid() bool {
	for _, candidate := range InventorySorts {
		if s == candidate {
			return true
		}
	}
	return false
}

// InventoryFilter is the normalized public list query. Search is trimmed and
// bounded by the service; Class and Race are canonical labels; ItemType is
// empty when no type predicate applies.
type InventoryFilter struct {
	Search   string
	Class    string
	Race     string
	ItemType ItemType
	Sort     InventorySort
	Page     int
	PageSize int
}

// InventoryRecord is the narrow public repository projection. It deliberately
// omits SourceLocation, GivenTo, ModAt, ModBy, ModByUserID, DeletedFg and
// NameSort. Dates are UTC calendar dates at midnight.
type InventoryRecord struct {
	GuildBankItemID int64
	EQUID           uint64
	Name            string
	ItemType        ItemType
	Quantity        uint32
	ClassString     string
	RaceString      string
	MetadataMissing bool
	DateAdded       time.Time
	DateOpen        time.Time
	DateBazaar      time.Time
}

// InventoryRecordPage is one repository page plus the count from the same
// repeatable-read snapshot.
type InventoryRecordPage struct {
	Records []InventoryRecord
	Total   int
}

// AvailabilityState is the single public availability value shared by the API
// and HTML pages.
type AvailabilityState string

const (
	AvailabilityUpcoming AvailabilityState = "upcoming"
	AvailabilityOpen     AvailabilityState = "open"
	AvailabilityBazaar   AvailabilityState = "bazaar"
)

// PublicInventoryItem is the complete public projection. A future internal
// field must be added explicitly here before it can reach a response.
type PublicInventoryItem struct {
	ID              int64
	EQUID           uint64
	Name            string
	ItemType        ItemType
	Quantity        uint32
	ClassString     string
	RaceString      string
	MetadataMissing bool
	DateAdded       time.Time
	DateOpen        time.Time
	DateBazaar      time.Time
	Availability    AvailabilityState
	EQResourceURL   *string
}

// PublicInventoryPage is the service result for one public list request.
type PublicInventoryPage struct {
	Items      []PublicInventoryItem
	Filter     InventoryFilter
	Page       int
	PageSize   int
	Total      int
	TotalPages int
	Sort       InventorySort
}
