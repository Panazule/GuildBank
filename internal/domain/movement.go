package domain

import (
	"fmt"
	"strings"
	"time"
)

// MovementType is the approved closed set for the append-only inventory
// ledger. The first release exposes only ISSUE and RETURN (OD-04).
type MovementType string

const (
	MovementIssue    MovementType = "ISSUE"
	MovementReturn   MovementType = "RETURN"
	MovementReceive  MovementType = "RECEIVE"
	MovementMove     MovementType = "MOVE"
	MovementAdjust   MovementType = "ADJUST"
	MovementWriteOff MovementType = "WRITE_OFF"
	MovementMerge    MovementType = "MERGE"
)

// MovementTypes is the canonical allowlist in schema order.
var MovementTypes = []MovementType{
	MovementIssue, MovementReturn, MovementReceive, MovementMove,
	MovementAdjust, MovementWriteOff, MovementMerge,
}

// Valid reports whether the type is one of the approved ledger values.
func (t MovementType) Valid() bool {
	for _, candidate := range MovementTypes {
		if t == candidate {
			return true
		}
	}
	return false
}

// FirstRelease reports whether the type is exposed in the first release.
func (t MovementType) FirstRelease() bool {
	return t == MovementIssue || t == MovementReturn
}

// RequiresRecipient reports whether the movement type requires a recipient.
func (t MovementType) RequiresRecipient() bool { return t == MovementIssue }

// Stackability is the advisory EQUID-level classification. It is descriptive
// metadata only: partial issuance is governed by the on-hand lot quantity, so no
// write path consults this value.
type Stackability string

const (
	StackabilityStackable    Stackability = "stackable"
	StackabilityNonStackable Stackability = "non_stackable"
	StackabilityUnknown      Stackability = "unknown"
)

// Stackabilities is the canonical allowlist for validation and presentation.
var Stackabilities = []Stackability{
	StackabilityStackable, StackabilityNonStackable, StackabilityUnknown,
}

// Valid reports whether the value is one of the approved classifications.
func (s Stackability) Valid() bool {
	for _, candidate := range Stackabilities {
		if s == candidate {
			return true
		}
	}
	return false
}

// InventoryMovement is one append-only ledger row. MovementID is assigned by
// the repository; all other fields are supplied by the caller. Nullable string
// columns are represented by the empty string and nullable integers by nil.
type InventoryMovement struct {
	MovementID        int64
	GuildBankItemID   int64
	EQUID             uint64
	MovementType      MovementType
	Quantity          uint32
	Recipient         string
	ActorUserID       *int64
	ActorUserName     string
	OccurredAt        time.Time
	FromLocation      string
	ToLocation        string
	RemainingQuantity uint32
	RelatedItemID     *int64
	// RelatedMovementID attributes a RETURN to the specific ISSUE movement it
	// closes. It is nil on ISSUE rows and on returns that have no issuance
	// behind them (a stock return against an active lot).
	RelatedMovementID *int64
	// RequestID attributes the ISSUE written by a request approval to the
	// InventoryRequest it satisfied. It is nil for every manual issue, because
	// the column is nullable and its unique index therefore tolerates them.
	RequestID      *int64
	Note           string
	CorrelationID  string
	IdempotencyKey string
}

// Validate checks the ledger row against the approved schema constraints.
func (m InventoryMovement) Validate() error {
	fields := make(map[string]string)
	if m.GuildBankItemID <= 0 {
		fields["guildBankItemId"] = "guildBankItemId must be a positive integer"
	}
	if m.EQUID == 0 {
		fields["equid"] = "equid must be a positive integer"
	}
	if !m.MovementType.Valid() {
		fields["movementType"] = "movementType must be one of the approved values"
	}
	if m.Quantity == 0 {
		fields["quantity"] = "quantity must be greater than zero"
	}
	if m.MovementType.RequiresRecipient() && strings.TrimSpace(m.Recipient) == "" {
		fields["recipient"] = "recipient is required for ISSUE"
	}
	if strings.TrimSpace(m.ActorUserName) == "" {
		fields["actorUserName"] = "actorUserName is required"
	}
	if m.ActorUserID != nil && *m.ActorUserID <= 0 {
		fields["actorUserId"] = "actorUserId must be a positive integer when supplied"
	}
	if m.OccurredAt.IsZero() {
		fields["occurredAt"] = "occurredAt is required"
	}
	if m.RelatedItemID != nil && *m.RelatedItemID <= 0 {
		fields["relatedItemId"] = "relatedItemId must be a positive integer when supplied"
	}
	if m.RelatedMovementID != nil && *m.RelatedMovementID <= 0 {
		fields["relatedMovementId"] = "relatedMovementId must be a positive integer when supplied"
	}
	if m.RequestID != nil && *m.RequestID <= 0 {
		fields["requestId"] = "requestId must be a positive integer when supplied"
	}
	if len([]rune(m.Recipient)) > 128 {
		fields["recipient"] = "recipient must be at most 128 characters"
	}
	if len([]rune(m.ActorUserName)) > 64 {
		fields["actorUserName"] = "actorUserName must be at most 64 characters"
	}
	if len([]rune(m.FromLocation)) > 128 {
		fields["fromLocation"] = "fromLocation must be at most 128 characters"
	}
	if len([]rune(m.ToLocation)) > 128 {
		fields["toLocation"] = "toLocation must be at most 128 characters"
	}
	if len([]rune(m.Note)) > 255 {
		fields["note"] = "note must be at most 255 characters"
	}
	if len([]rune(m.CorrelationID)) > 36 {
		fields["correlationId"] = "correlationId must be at most 36 characters"
	}
	if len([]rune(m.IdempotencyKey)) > 64 {
		fields["idempotencyKey"] = "idempotencyKey must be at most 64 characters"
	}
	if len(fields) > 0 {
		return &ValidationError{Fields: fields}
	}
	return nil
}

// MovementFilter is the bounded, allowlisted ledger query. Zero values mean no
// predicate. The order is always newest-first and is not caller-selectable.
type MovementFilter struct {
	GuildBankItemID int64
	EQUID           uint64
	MovementType    MovementType
	Recipient       string
	CorrelationID   string
	From            time.Time
	To              time.Time
	Page            int
	PageSize        int
}

// MovementPage is one ledger page plus the total from the same snapshot.
type MovementPage struct {
	Movements  []InventoryMovement
	Total      int
	Page       int
	PageSize   int
	TotalPages int
	Filter     MovementFilter
}

// IssueItemInput is the approved issue command shape. The service operation is
// introduced in a later phase; this type only fixes the contract.
type IssueItemInput struct {
	GuildBankItemID int64
	ExpectedModAt   time.Time
	Quantity        uint32
	Recipient       string
	Note            string
	IdempotencyKey  string
	Actor           Actor
	CorrelationID   string
	SourceIP        string
}

// ReturnItemInput is the approved return command shape. Dates are copied from
// the source lot by the later service operation (OD-03).
type ReturnItemInput struct {
	GuildBankItemID int64
	ExpectedModAt   time.Time
	Quantity        uint32
	Recipient       string
	Note            string
	IdempotencyKey  string
	Actor           Actor
	CorrelationID   string
	SourceIP        string
}

// ReturnIssuanceInput closes part or all of one ISSUE movement. ExpectedModAt is
// the lot's ModAt as shown on the issued row; the movement's outstanding
// quantity is re-checked under the write lock.
type ReturnIssuanceInput struct {
	MovementID     int64
	ExpectedModAt  time.Time
	Quantity       uint32
	Recipient      string
	Note           string
	IdempotencyKey string
	Actor          Actor
	CorrelationID  string
	SourceIP       string
}

// AdjustItemInput is defined for schema completeness and is deferred by OD-04.
type AdjustItemInput struct {
	GuildBankItemID int64
	ExpectedModAt   time.Time
	Quantity        uint32
	Reason          string
	Note            string
	IdempotencyKey  string
	Actor           Actor
	CorrelationID   string
	SourceIP        string
}

// MoveItemInput is defined for schema completeness and is deferred by OD-04.
type MoveItemInput struct {
	GuildBankItemID int64
	ExpectedModAt   time.Time
	Quantity        uint32
	ToLocation      string
	Note            string
	IdempotencyKey  string
	Actor           Actor
	CorrelationID   string
	SourceIP        string
}

// String returns a compact diagnostic form without exposing free text.
func (m InventoryMovement) String() string {
	return fmt.Sprintf("movement %d type %s lot %d quantity %d", m.MovementID, m.MovementType, m.GuildBankItemID, m.Quantity)
}
