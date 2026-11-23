package domain

import "time"

// AdminInventoryStatus is the soft-delete filter on the administrative list.
type AdminInventoryStatus string

const (
	AdminStatusActive  AdminInventoryStatus = "active"
	AdminStatusRemoved AdminInventoryStatus = "removed"
	AdminStatusAll     AdminInventoryStatus = "all"
)

// AdminInventoryStatuses is the canonical allowlist for API/UI validation.
var AdminInventoryStatuses = []AdminInventoryStatus{
	AdminStatusActive, AdminStatusRemoved, AdminStatusAll,
}

// AdminInventorySort is the administrative order allowlist. No caller-supplied
// SQL expression is ever accepted.
type AdminInventorySort string

const (
	AdminSortModifiedNewest AdminInventorySort = "modified_newest"
	AdminSortModifiedOldest AdminInventorySort = "modified_oldest"
	AdminSortName           AdminInventorySort = "name"
	AdminSortNameDesc       AdminInventorySort = "name_desc"
	AdminSortLocation       AdminInventorySort = "location"
	AdminSortQuantity       AdminInventorySort = "quantity"
	AdminSortDateAdded      AdminInventorySort = "date_added"
	AdminSortDateAddedDesc  AdminInventorySort = "date_added_desc"
	AdminSortDateOpen       AdminInventorySort = "date_open"
	AdminSortDateOpenDesc   AdminInventorySort = "date_open_desc"
	AdminSortDateBazaar     AdminInventorySort = "date_bazaar"
	AdminSortDateBazaarDesc AdminInventorySort = "date_bazaar_desc"
)

// AdminInventorySorts is the canonical, ordered sort allowlist.
var AdminInventorySorts = []AdminInventorySort{
	AdminSortModifiedNewest,
	AdminSortModifiedOldest,
	AdminSortName,
	AdminSortNameDesc,
	AdminSortLocation,
	AdminSortQuantity,
	AdminSortDateAdded,
	AdminSortDateAddedDesc,
	AdminSortDateOpen,
	AdminSortDateOpenDesc,
	AdminSortDateBazaar,
	AdminSortDateBazaarDesc,
}

// Valid reports whether the sort is one of the approved admin values.
func (s AdminInventorySort) Valid() bool {
	for _, candidate := range AdminInventorySorts {
		if s == candidate {
			return true
		}
	}
	return false
}

// AdminMetadataFilter is the metadata-state predicate.
type AdminMetadataFilter string

const (
	AdminMetadataAny     AdminMetadataFilter = "any"
	AdminMetadataMissing AdminMetadataFilter = "missing"
	AdminMetadataPresent AdminMetadataFilter = "present"
)

// AdminMetadataFilters is the canonical allowlist for the metadata filter.
var AdminMetadataFilters = []AdminMetadataFilter{
	AdminMetadataAny, AdminMetadataMissing, AdminMetadataPresent,
}

// AdminInventoryFilter is the normalized administrative list query. Zero
// values mean "no predicate" for ID and EQUID.
type AdminInventoryFilter struct {
	ID            int64
	EQUID         uint64
	Search        string
	GivenTo       string
	Metadata      AdminMetadataFilter
	LocationGroup string
	ItemType      ItemType
	Status        AdminInventoryStatus
	Sort          AdminInventorySort
	Page          int
	PageSize      int
}

// AdminInventoryItem is the full administrative projection. Nullable columns
// are mapped to the domain's zero value (empty string or nil).
type AdminInventoryItem struct {
	ID                int64
	EQUID             uint64
	Name              string
	ItemType          ItemType
	Quantity          uint32
	ClassString       string
	RaceString        string
	MetadataMissingFg bool
	SourceLocation    string
	LocationGroup     string
	LocationSort      string
	GivenTo           string
	DeletedFg         bool
	// ConsolidatedInto is set on a row absorbed by the item-consolidation
	// maintenance command. It names the surviving row and blocks restore.
	ConsolidatedInto *int64
	NameSort         string
	DateAdded        time.Time
	DateOpen         time.Time
	DateBazaar       time.Time
	ModAt            time.Time
	ModBy            string
	ModByUserID      *int64
}

// AdminInventoryPage is one page of administrative records plus the total from
// the same snapshot.
type AdminInventoryPage struct {
	Records    []AdminInventoryItem
	Total      int
	Page       int
	PageSize   int
	TotalPages int
	Filter     AdminInventoryFilter
}

// OptionalString distinguishes "unchanged/absent" from "clear". Set with a nil
// Value is an explicit JSON null; Set with a Value is a value.
type OptionalString struct {
	Value *string
	Set   bool
}

// Clear reports whether the field is an explicit null.
func (o OptionalString) Clear() bool { return o.Set && o.Value == nil }

// String returns the value, or "" when absent or null.
func (o OptionalString) String() string {
	if o.Value == nil {
		return ""
	}
	return *o.Value
}

// OptionalDate distinguishes "absent" from "reset to the settings default". Set
// with a nil Value is an explicit null; Set with a Value is a calendar date at
// midnight UTC.
type OptionalDate struct {
	Value *time.Time
	Set   bool
}

// Reset reports whether the field is an explicit null.
func (o OptionalDate) Reset() bool { return o.Set && o.Value == nil }

// CreateItemInput is the validated manual-create command.
type CreateItemInput struct {
	ItemType       ItemType
	EQUID          uint64
	Quantity       uint32
	SourceLocation OptionalString
	GivenTo        OptionalString
	Name           string
	DateOpen       OptionalDate
	DateBazaar     OptionalDate
	Actor          Actor
	CorrelationID  string
	SourceIP       string
}

// UpdateItemInput is the validated manual-edit command. All editable fields are
// required keys; a nil date value resets that date to the settings default.
// Remove asks the same transaction to soft-delete the row after the field
// update (AI-02), so the edit and the removal share one commit and one
// correlation ID.
type UpdateItemInput struct {
	ID             int64
	ExpectedModAt  time.Time
	Quantity       uint32
	SourceLocation OptionalString
	GivenTo        OptionalString
	DateOpen       OptionalDate
	DateBazaar     OptionalDate
	Remove         bool
	Actor          Actor
	CorrelationID  string
	SourceIP       string
}

// RemoveItemTarget is one record selected for the page-local bulk removal
// (AI-01). ExpectedModAt is the per-row token rendered with the checkbox.
type RemoveItemTarget struct {
	ID            int64
	ExpectedModAt time.Time
}

// BulkRemoveResult is the outcome of one all-or-nothing bulk removal. Units is
// the sum of the quantities of the removed lots; Removed is the record count.
type BulkRemoveResult struct {
	Items   []AdminInventoryItem
	Removed int
	Units   uint32
}

// AdminUpdateRow is the repository update projection. Quantity updates either
// item type; a nil date leaves the stored column untouched.
type AdminUpdateRow struct {
	ID             int64
	Quantity       *uint32
	SourceLocation string
	LocationGroup  string
	LocationSort   string
	GivenTo        string
	DateOpen       *time.Time
	DateBazaar     *time.Time
	ModAt          time.Time
	Actor          Actor
}
