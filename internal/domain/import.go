package domain

import (
	"encoding/json"
	"time"
)

// ImportStatus is the lifecycle value stored in ImportBatch.Status.
type ImportStatus string

const (
	ImportStatusProcessing ImportStatus = "Processing"
	ImportStatusUploaded   ImportStatus = "Uploaded"
	ImportStatusFailed     ImportStatus = "Failed"
	ImportStatusCompleted  ImportStatus = "Completed"
)

// Actor is the authenticated account snapshot used for guild-store writes. It
// is built from the session user and deliberately excludes password data.
// Role carries the approved hierarchy so a service can make a capability
// decision without re-reading the account.
type Actor struct {
	UserID   int64
	UserName string
	IsAdmin  bool
	Role     Role
}

// CanManageInventory reports whether the actor may perform inventory and import
// writes. It honours Role when supplied and otherwise falls back to the legacy
// IsAdmin flag, so callers that predate the role model keep working.
func (a Actor) CanManageInventory() bool {
	if a.Role != "" {
		return a.Role.AtLeast(RoleOfficer)
	}
	return a.IsAdmin
}

// ImportBatch is one uploaded file and its durable lifecycle state.
type ImportBatch struct {
	ImportBatchID    int64
	FileName         string
	FileSha256       *string
	SourceRowCount   *int
	AcceptedRowCount *int
	// IgnoredRowCount counts physical rows dropped by the nested-slot rule
	// (IMP-01). It is separate from parser discards so an operator can tell an
	// ignored bag slot from a blank inventory line. NULL on legacy batches.
	IgnoredRowCount  *int
	Status           ImportStatus
	FailureReason    *string
	ImportedBy       string
	ImportedByUserID *int64
	ImportedAt       time.Time
}

// ImportLocation is one selector entry derived from LocationGroup. LocationRaw
// is never the filter key; it remains on the row for traceability.
type ImportLocation struct {
	LocationGroup string
	LocationSort  string
	RowCount      int
	SelectedCount int
}

// ImportRow is one staged ImportList row.
type ImportRow struct {
	ImportID      int64
	ImportBatchID int64
	SourceLine    *int
	ItemType      ItemType
	EQUID         uint64
	Name          string
	Quantity      uint32
	LocationRaw   string
	LocationGroup string
	LocationSort  string
	NameSort      string
	SelectedFg    bool
	Warnings      []ImportWarning
}

// ImportWarning is a non-fatal diagnostic stored as JSON on ImportList.
type ImportWarning struct {
	Code     string `json:"code"`
	Field    string `json:"field,omitempty"`
	EQUID    uint64 `json:"equid,omitempty"`
	Mask     uint64 `json:"mask,omitempty"`
	HighBits uint64 `json:"highBits,omitempty"`
	Detail   string `json:"detail,omitempty"`
}

// ImportRowFilter is the validated paging/filter shape for staged rows. The
// exclusion list is the review page's group scope (IMP-06): groups a reviewer
// has unchecked are hidden unless IgnoreGroups asks for every group, which is
// what the "unselected" view does so no row is lost from the review.
type ImportRowFilter struct {
	LocationGroup string
	ExcludeGroups []string
	IgnoreGroups  bool
	Selected      string // "all", "selected" or "unselected"
	Search        string
	Page          int
	PageSize      int
}

// ImportRowPage is one page of staged rows plus the total under the filters.
type ImportRowPage struct {
	Rows     []ImportRow
	Total    int
	Page     int
	PageSize int
}

// ImportTotals aggregates the persisted selection. ItemUnits and SpellQuantity
// are sums, so they may exceed a single row's quantity.
type ImportTotals struct {
	SelectedRows   int
	ItemRows       int
	ItemUnits      uint64
	SpellRows      int
	SpellQuantity  uint64
	DistinctSpells int
	WarningRows    int
}

// ImportSummary is the unit-aware summary payload. It is composed from
// ImportTotals, ListLocations, SpellPreview and the batch, not persisted.
type ImportSummary struct {
	BatchID                  int64
	Status                   ImportStatus
	SelectedRows             int
	StagedRows               int
	SelectedLocations        int
	TotalLocations           int
	ItemRows                 int
	ItemUnits                uint64
	SpellRows                int
	SpellQuantity            uint64
	DistinctSpells           int
	NewSpells                int
	MergedSpells             int
	InventoryRecordsToCreate uint64
	WarningRows              int
	TotalRecordsToImport     uint64
}

// SpellPreview is one selected spell EQUID and its current inventory target.
type SpellPreview struct {
	EQUID          uint64
	StagedRows     int
	StagedQuantity uint64
	ActiveRecords  int
	ActiveQuantity uint64
	MergeTargetID  *int64 // lowest active GuildBankItemID; nil when no active row exists
}

// UploadResult is returned by the upload service.
type UploadResult struct {
	Batch          ImportBatch
	StructuralRows int
	DiscardedRows  int
	IgnoredRows    int
	BlankRows      int
	DuplicateOf    *int64
	WarningCounts  map[string]int
}

// ImportFailure attaches the durable batch ID to a parse/staging failure.
type ImportFailure struct {
	BatchID int64
	Err     error
}

func (f *ImportFailure) Error() string { return f.Err.Error() }
func (f *ImportFailure) Unwrap() error { return f.Err }

// RetryableError marks a transient deadlock that the service may retry once.
type RetryableError struct {
	Err error
}

func (e *RetryableError) Error() string { return e.Err.Error() }
func (e *RetryableError) Unwrap() error { return e.Err }

// DuplicateSpellTarget records a corruption anomaly: merged into the lowest ID.
type DuplicateSpellTarget struct {
	EQUID      uint64 `json:"equid"`
	MergedInto int64  `json:"mergedInto"`
}

// CommitResult is the commit response body.
type CommitResult struct {
	BatchID               int64
	Status                ImportStatus
	ItemRecordsCreated    int
	SpellRecordsCreated   int
	SpellRecordsMerged    int
	SpellQuantityAdded    uint64
	TotalRecordsAffected  int
	DuplicateSpellTargets []DuplicateSpellTarget
	Warnings              []ImportWarning
	DurationMS            int64
}

// GuildBankItem is the import-owned projection of one GuildBankItems row.
// Dates are calendar dates at 00:00 UTC; the repository writes them as DATE.
// LocationGroup and LocationSort are the persisted D-05 values staged at
// upload; the repository never recomputes them.
type GuildBankItem struct {
	GuildBankItemID   int64
	ItemType          ItemType
	EQUID             uint64
	MetadataMissingFg bool
	Name              string
	ClassString       string
	RaceString        string
	DateAdded         time.Time
	DateOpen          time.Time
	DateBazaar        time.Time
	Quantity          uint32
	SourceLocation    string // nullable in the table; empty is written as NULL
	LocationGroup     string // nullable; empty is written as NULL
	LocationSort      string // nullable; empty is written as NULL
	GivenTo           string // nullable in the table; empty is written as NULL
	DeletedFg         bool
	ModAt             time.Time
	ModBy             string
	ModByUserID       *int64
	NameSort          string
}

// AuditEntry maps the D-10 Audit columns. AuditID is filled only by audit
// reads; writers never supply it.
type AuditEntry struct {
	AuditID       int64
	EventDate     time.Time
	UserID        int64
	UserName      string
	EventType     string
	TableName     string
	RecordID      string
	OldValues     json.RawMessage
	NewValues     json.RawMessage
	Notes         string
	CorrelationID string
	SourceIP      string
}
