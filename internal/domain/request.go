package domain

import (
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"
)

// Request workflow sentinel errors. They let the API map one documented status
// and code per outcome without inspecting message text, exactly as the ledger's
// ErrConflict and ErrNotFound do.
var (
	// ErrRequestItemUnavailable reports a lot that is missing or removed. A
	// request may only target an active lot (design section 12.3).
	ErrRequestItemUnavailable = errors.New("request item unavailable")
	// ErrRequestInsufficientStock reports a quantity above what the lot holds.
	ErrRequestInsufficientStock = errors.New("insufficient stock")
	// ErrRequestAlreadyPending reports the uq_request_pending rule: one Pending
	// request per character per item, for members and public visitors alike.
	ErrRequestAlreadyPending = errors.New("request already pending")
	// ErrRequestTooManyPending reports the per-account Pending budget.
	ErrRequestTooManyPending = errors.New("too many pending requests")
	// ErrRequestNotPending reports a transition attempted against a terminal
	// request. Terminal states are immutable (design section 3.1).
	ErrRequestNotPending = errors.New("request not pending")
	// ErrRequestStaleItem reports a courtesy expectedModAt mismatch: the stock
	// changed between rendering the form and submitting it.
	ErrRequestStaleItem = errors.New("item changed since it was loaded")
	// ErrRequestCharacterInvalid reports a character name that is not on the
	// submitting account. It is a 422 with its own code, not a generic field
	// error, because the form must reload the current character list.
	ErrRequestCharacterInvalid = errors.New("character invalid")
)

// Request workflow bounds. The note bound mirrors InventoryMovement.Note so an
// approval can carry the requester's note into the ledger without truncation;
// the contact bound mirrors the InventoryRequest.ContactDiscord column.
const (
	RequestMaxNoteRunes    = 255
	RequestMaxContactRunes = 64
	// RequestMaxPendingPerUser is the Pending budget for one member account. It
	// bounds the queue one enthusiastic member can create; officers have no
	// budget because they are the ones who drain it.
	RequestMaxPendingPerUser = 5
	// RequestDefaultPageSize, RequestMaxPageSize and RequestMaxPage bound the
	// officer queue and the member "my requests" list, matching the admin
	// inventory bounds.
	RequestDefaultPageSize = 50
	RequestMaxPageSize     = 200
	RequestMaxPage         = 10000
	// RequestPublicActorName is the stored CreatedByName for a public request.
	// A public requester has no account, so the row and its audit event use one
	// fixed name instead of inventing an identity.
	RequestPublicActorName = "public"
)

// RequestStatus is the closed lifecycle set. Approval is not a state: it is the
// transition that completes the request and issues the item.
type RequestStatus string

const (
	RequestPending   RequestStatus = "Pending"
	RequestCompleted RequestStatus = "Completed"
	RequestDenied    RequestStatus = "Denied"
	RequestCancelled RequestStatus = "Cancelled"
)

// RequestStatuses is the canonical allowlist in schema order.
var RequestStatuses = []RequestStatus{RequestPending, RequestCompleted, RequestDenied, RequestCancelled}

// Valid reports whether the status is one of the four lifecycle values.
func (s RequestStatus) Valid() bool {
	for _, candidate := range RequestStatuses {
		if s == candidate {
			return true
		}
	}
	return false
}

// Terminal reports whether the status is final. Every state except Pending is
// terminal and therefore immutable.
func (s RequestStatus) Terminal() bool { return s.Valid() && s != RequestPending }

// String returns the stored value.
func (s RequestStatus) String() string { return string(s) }

// RequesterKind separates an authenticated member from an anonymous visitor.
type RequesterKind string

const (
	RequesterMember RequesterKind = "Member"
	RequesterPublic RequesterKind = "Public"
)

// RequesterKinds is the canonical allowlist for validation and presentation.
var RequesterKinds = []RequesterKind{RequesterMember, RequesterPublic}

// Valid reports whether the kind is one of the two approved values.
func (k RequesterKind) Valid() bool {
	for _, candidate := range RequesterKinds {
		if k == candidate {
			return true
		}
	}
	return false
}

// String returns the stored value.
func (k RequesterKind) String() string { return string(k) }

// InventoryRequest is one workflow row. Every identity and stock value on it is
// a request-time snapshot, so the row still reads correctly after an account is
// deleted, a character is renamed or the lot is issued or consolidated.
// CurrentOnHand and LotActive are read-only projections filled by the queue query
// and are never stored.
type InventoryRequest struct {
	RequestID           int64
	GuildBankItemID     int64
	EQUID               uint64
	ItemName            string
	RequestedQuantity   uint32
	OnHandAtCreate      uint32
	CharacterName       string
	RequesterKind       RequesterKind
	RequesterUserID     *int64
	RequesterUserName   string
	ContactDiscord      string
	Note                string
	Status              RequestStatus
	DecisionNote        string
	IssuedQuantity      *uint32
	CompletedMovementID *int64
	CorrelationID       string
	IdempotencyKey      string
	CreatedAt           time.Time
	CreatedByUserID     *int64
	CreatedByName       string
	DecidedAt           *time.Time
	DecidedByUserID     *int64
	DecidedByName       string
	// Read-only projection fields, filled by the queue query.
	CurrentOnHand uint32
	LotActive     bool
}

// Available reports whether the lot can still satisfy the request right now.
// It is the derived display state of the design's section 4.3, never a stored
// flag.
func (r InventoryRequest) Available() bool { return r.LotActive && r.CurrentOnHand > 0 }

// ShortIssued reports whether fewer units left the bank than were requested.
func (r InventoryRequest) ShortIssued() bool {
	return r.IssuedQuantity != nil && *r.IssuedQuantity < r.RequestedQuantity
}

// CreateRequestInput is the creation command. The snapshot fields EQUID,
// ItemName, OnHandAtCreate and CreatedAt are resolved by the service from the
// lot it validated, so the caller cannot state an identity the lot contradicts.
type CreateRequestInput struct {
	GuildBankItemID int64
	Quantity        uint32
	CharacterName   string
	Note            string
	ContactDiscord  string
	Kind            RequesterKind
	Actor           Actor // zero value for a public request
	IdempotencyKey  string
	CorrelationID   string
	SourceIP        string

	// ExpectedModAt is the lot version the form was rendered from. It is a
	// courtesy check only (design section 9.6): a zero value skips it, and a
	// mismatch is "the stock changed since you looked", not a lock.
	ExpectedModAt time.Time

	EQUID          uint64
	ItemName       string
	OnHandAtCreate uint32
	CreatedAt      time.Time
}

// Validate checks the creation command against the structural rules that need
// no store: quantity, character name, note, contact handle, requester kind and
// its agreement with the actor. Character ownership, lot availability and the
// pending budget are store-backed and checked by the service.
func (in CreateRequestInput) Validate() error {
	fields := make(map[string]string)
	if in.GuildBankItemID <= 0 {
		fields["guildBankItemId"] = "guildBankItemId must be a positive integer"
	}
	if in.Quantity == 0 {
		fields["quantity"] = "quantity must be at least 1"
	}
	if problem := characterNameProblem(in.CharacterName); problem != "" {
		fields["characterName"] = problem
	}
	if problem := requestTextProblem(in.Note, RequestMaxNoteRunes); problem != "" {
		fields["note"] = problem
	}
	if !in.Kind.Valid() {
		fields["requesterKind"] = "requesterKind must be one of the approved values"
	} else {
		switch in.Kind {
		case RequesterPublic:
			// A public request must not carry an account; anything else would
			// store an account snapshot that never authenticated.
			if in.Actor.UserID != 0 {
				fields["requesterKind"] = "a public request must not carry an account"
			}
			if problem := requestTextProblem(in.ContactDiscord, RequestMaxContactRunes); problem != "" {
				fields["contactDiscord"] = problem
			} else if strings.TrimSpace(in.ContactDiscord) == "" {
				fields["contactDiscord"] = "contactDiscord is required for a public request"
			}
		case RequesterMember:
			if in.Actor.UserID <= 0 || strings.TrimSpace(in.Actor.UserName) == "" {
				fields["requesterKind"] = "a member request requires an authenticated account"
			}
		}
	}
	if problem := requestTextProblem(in.IdempotencyKey, 64); problem != "" {
		fields["idempotencyKey"] = problem
	}
	if strings.TrimSpace(in.CorrelationID) == "" {
		fields["correlationId"] = "correlationId is required"
	}
	if len(fields) > 0 {
		return &ValidationError{Fields: fields}
	}
	return nil
}

// ApproveRequestInput is the approval command. IssueQuantity is posted only when
// the lot cannot cover the request, where the officer is offered a reduced
// amount; zero means "no quantity was prompted".
type ApproveRequestInput struct {
	RequestID     int64
	ExpectedModAt time.Time
	IssueQuantity uint32
	Note          string
	Actor         Actor
	CorrelationID string
	SourceIP      string
}

// DenyRequestInput is the denial command. Reason is required so a denied member
// learns why and the audit row explains itself.
type DenyRequestInput struct {
	RequestID     int64
	Reason        string
	Actor         Actor
	CorrelationID string
	SourceIP      string
}

// CancelRequestInput is the requester's withdrawal command. The actor must match
// the request's RequesterUserID; the service resolves "own" from the session and
// never from a posted value.
type CancelRequestInput struct {
	RequestID     int64
	Reason        string
	Actor         Actor
	CorrelationID string
	SourceIP      string
}

// Validate checks the cancellation command. The reason is optional but bounded
// by the same 255-rune rule as every other request note, so it can be carried
// into the audit row without truncation.
func (in CancelRequestInput) Validate() error {
	fields := make(map[string]string)
	if in.RequestID <= 0 {
		fields["requestId"] = "requestId must be a positive integer"
	}
	if in.Actor.UserID <= 0 || strings.TrimSpace(in.Actor.UserName) == "" {
		fields["actor"] = "an authenticated requester is required"
	}
	if problem := requestTextProblem(in.Reason, RequestMaxNoteRunes); problem != "" {
		fields["reason"] = problem
	}
	if strings.TrimSpace(in.CorrelationID) == "" {
		fields["correlationId"] = "correlationId is required"
	}
	if len(fields) > 0 {
		return &ValidationError{Fields: fields}
	}
	return nil
}

// CompleteRequestParams is the guarded Pending -> Completed transition written
// inside the approval transaction, after the ISSUE movement exists.
type CompleteRequestParams struct {
	RequestID      int64
	MovementID     int64
	IssuedQuantity uint32
	DecisionNote   string
	// Actor is the deciding officer. It is stored as the DecidedBy* snapshot.
	Actor     Actor
	DecidedAt time.Time
}

// DecideRequestParams is the guarded Pending -> Denied|Cancelled transition. It
// never touches inventory.
type DecideRequestParams struct {
	RequestID    int64
	Status       RequestStatus
	DecisionNote string
	Actor        Actor
	DecidedAt    time.Time
}

// RequestFilter is the bounded, allowlisted request query. Zero values mean no
// predicate. The order is always newest-first and is not caller-selectable.
type RequestFilter struct {
	Status          RequestStatus
	RequesterKind   RequesterKind
	RequesterUserID int64 // member "my requests" scope; 0 = not scoped
	GuildBankItemID int64
	CharacterName   string
	ItemName        string
	From            time.Time
	To              time.Time
	Page            int
	PageSize        int
}

// RequestPage is one request page plus the total from the same snapshot.
type RequestPage struct {
	Requests   []InventoryRequest
	Total      int
	Page       int
	PageSize   int
	TotalPages int
	Filter     RequestFilter
}

// requestTextProblem trims one optional free-text value and reports its rune
// bound and control characters. An empty value is always valid.
func requestTextProblem(value string, maxRunes int) string {
	trimmed := strings.TrimSpace(value)
	if len([]rune(trimmed)) > maxRunes {
		return fmt.Sprintf("must be at most %d characters", maxRunes)
	}
	for _, r := range trimmed {
		if unicode.IsControl(r) {
			return "must not contain control characters"
		}
	}
	return ""
}
