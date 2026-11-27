package service

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"time"
	"unicode"

	"github.com/Panazule/guildbank/internal/domain"
)

// normalizeMovementText trims one movement free-text value and validates its
// rune length and control characters. It records a field error instead of
// returning early so every invalid field is reported together.
func normalizeMovementText(field, value string, maxRunes int, required bool, fields map[string]string) string {
	trimmed := strings.TrimSpace(value)
	if required && trimmed == "" {
		fields[field] = field + " is required"
		return trimmed
	}
	if len([]rune(trimmed)) > maxRunes {
		fields[field] = fmt.Sprintf("%s must be at most %d characters", field, maxRunes)
		return trimmed
	}
	for _, r := range trimmed {
		if unicode.IsControl(r) {
			fields[field] = field + " must not contain control characters"
			break
		}
	}
	return trimmed
}

// normalizeIdempotencyKey trims and bounds the optional request idempotency key.
// The ledger column is VARCHAR(64); the service rejects longer values before
// opening a transaction so the caller gets a field error rather than a
// database truncation.
func normalizeIdempotencyKey(value string, fields map[string]string) string {
	trimmed := strings.TrimSpace(value)
	if len([]rune(trimmed)) > 64 {
		fields["idempotencyKey"] = "idempotencyKey must be at most 64 characters"
	}
	for _, r := range trimmed {
		if unicode.IsControl(r) {
			fields["idempotencyKey"] = "idempotencyKey must not contain control characters"
			break
		}
	}
	return trimmed
}

// newMovement builds a validated ledger row for one lot operation. The caller
// supplies the already-authorized quantity and the resulting on-hand quantity.
func newMovement(item domain.AdminInventoryItem, movementType domain.MovementType,
	quantity, remaining uint32, recipient, fromLocation, toLocation, note string,
	actor domain.Actor, at time.Time, correlationID, idempotencyKey string) domain.InventoryMovement {
	actorID := actor.UserID
	return domain.InventoryMovement{
		GuildBankItemID:   item.ID,
		EQUID:             item.EQUID,
		MovementType:      movementType,
		Quantity:          quantity,
		RemainingQuantity: remaining,
		Recipient:         recipient,
		FromLocation:      fromLocation,
		ToLocation:        toLocation,
		Note:              note,
		ActorUserID:       &actorID,
		ActorUserName:     actor.UserName,
		OccurredAt:        at.UTC(),
		CorrelationID:     correlationID,
		IdempotencyKey:    idempotencyKey,
	}
}

// movementFields returns the structured audit fields shared by every movement
// event. The movement type is a structured field, never only free text.
func movementFields(movement *domain.InventoryMovement) map[string]any {
	if movement == nil {
		return nil
	}
	fields := map[string]any{
		"movementId":   movement.MovementID,
		"movementType": string(movement.MovementType),
		"equid":        movement.EQUID,
		"quantity":     movement.Quantity,
		"remaining":    movement.RemainingQuantity,
		"correlation":  movement.CorrelationID,
	}
	if movement.Recipient != "" {
		fields["recipient"] = movement.Recipient
	}
	if movement.FromLocation != "" {
		fields["fromLocation"] = movement.FromLocation
	}
	if movement.ToLocation != "" {
		fields["toLocation"] = movement.ToLocation
	}
	if movement.RelatedItemID != nil {
		fields["relatedItemId"] = *movement.RelatedItemID
	}
	if movement.RelatedMovementID != nil {
		fields["relatedMovementId"] = *movement.RelatedMovementID
	}
	return fields
}

// movementAudit builds the audit row for a movement-managed operation. The
// movement and its audit row are always written in the same transaction.
func movementAudit(item domain.AdminInventoryItem, movement domain.InventoryMovement,
	eventType, notes, sourceIP string) (domain.AuditEntry, error) {
	oldValues, err := json.Marshal(map[string]any{"quantity": item.Quantity})
	if err != nil {
		return domain.AuditEntry{}, err
	}
	newValues, err := json.Marshal(movementFields(&movement))
	if err != nil {
		return domain.AuditEntry{}, err
	}
	userID := int64(0)
	if movement.ActorUserID != nil {
		userID = *movement.ActorUserID
	}
	return domain.AuditEntry{
		EventDate:     movement.OccurredAt,
		UserID:        userID,
		UserName:      movement.ActorUserName,
		EventType:     eventType,
		TableName:     "GuildBankItems",
		RecordID:      fmt.Sprintf("%d", movement.GuildBankItemID),
		OldValues:     oldValues,
		NewValues:     newValues,
		Notes:         notes,
		CorrelationID: movement.CorrelationID,
		SourceIP:      sourceIP,
	}, nil
}

// lookupStackability resolves the advisory classification for one EQUID. The
// classification is presentation-only: no write path consults it, because
// partial issuance is governed by the on-hand lot quantity.
func (s *AdminInventoryService) lookupStackability(ctx context.Context, equid uint64) (domain.Stackability, error) {
	values, err := s.store.LookupStackability(ctx, []uint64{equid})
	if err != nil {
		return domain.StackabilityUnknown, err
	}
	level, ok := values[equid]
	if !ok {
		return domain.StackabilityUnknown, nil
	}
	return level, nil
}

// Issue records a partial or full-stack issue. A partial issue reduces the lot
// quantity; a full-stack issue soft-deletes the lot and records the recipient
// in GivenTo. The lot update, movement and audit row commit atomically.
func (s *AdminInventoryService) Issue(ctx context.Context, in domain.IssueItemInput) (AdminWriteResult, error) {
	if err := s.requireReady(); err != nil {
		return AdminWriteResult{}, err
	}
	if err := validateActor(in.Actor); err != nil {
		return AdminWriteResult{}, err
	}
	if in.GuildBankItemID <= 0 {
		return AdminWriteResult{}, domain.ErrNotFound
	}
	fields := make(map[string]string)
	if in.Quantity < 1 {
		fields["quantity"] = "quantity must be at least 1"
	}
	if in.ExpectedModAt.IsZero() {
		fields["expectedModAt"] = "expectedModAt is required"
	}
	in.Recipient = normalizeMovementText("recipient", in.Recipient, AdminMaxGivenToRunes, true, fields)
	in.Note = normalizeMovementText("note", in.Note, AdminMaxNoteRunes, false, fields)
	in.IdempotencyKey = normalizeIdempotencyKey(in.IdempotencyKey, fields)
	if len(fields) > 0 {
		return AdminWriteResult{}, &domain.ValidationError{Fields: fields}
	}
	return s.withDeadlockRetry("issue", func() (AdminWriteResult, error) {
		return s.issueAttempt(ctx, in)
	})
}

// issueLocked performs the issue sequence inside an already-open admin write
// transaction. It performs the lot lock, the removed/recipient checks, the
// stale-version check, the partial/full-stack decision, the lot update, the
// ledger row and its audit row. It never begins, commits or rolls back the
// transaction: the caller owns it, which is what lets request approval add the
// request transition to the same atomic unit (design section 9.1). The returned
// item is the locked snapshot; the returned movement carries the inserted
// MovementID.
func (s *AdminInventoryService) issueLocked(ctx context.Context, tx AdminWriteTx,
	in domain.IssueItemInput) (domain.AdminInventoryItem, domain.InventoryMovement, error) {
	current, err := tx.LockInventory(ctx, in.GuildBankItemID)
	if err != nil {
		return domain.AdminInventoryItem{}, domain.InventoryMovement{}, err
	}
	if current.DeletedFg {
		return domain.AdminInventoryItem{}, domain.InventoryMovement{},
			fmt.Errorf("%w: inventory row is removed", domain.ErrConflict)
	}
	if strings.TrimSpace(current.GivenTo) != "" {
		return domain.AdminInventoryItem{}, domain.InventoryMovement{},
			fmt.Errorf("%w: an active row cannot carry a recipient", domain.ErrConflict)
	}
	if !sameModAt(current.ModAt, in.ExpectedModAt) {
		return domain.AdminInventoryItem{}, domain.InventoryMovement{},
			fmt.Errorf("%w: the record changed since it was loaded", domain.ErrConflict)
	}
	if in.Quantity > current.Quantity {
		return domain.AdminInventoryItem{}, domain.InventoryMovement{},
			domain.NewValidationError("quantity", "quantity cannot exceed the on-hand quantity")
	}

	// Partial issuance is governed by the on-hand lot quantity, not by EQUID
	// stackability metadata: any active lot with more than one unit on hand may
	// be issued partially. in.Quantity is already bounded to 1..current.Quantity
	// above, so a partial issue always leaves at least one unit in the lot and an
	// equal quantity routes to the full-stack path.
	partial := in.Quantity < current.Quantity

	at := s.now().UTC()
	remaining := current.Quantity - in.Quantity
	row := domain.AdminUpdateRow{
		ID:             current.ID,
		SourceLocation: current.SourceLocation,
		LocationGroup:  current.LocationGroup,
		LocationSort:   current.LocationSort,
		GivenTo:        current.GivenTo,
		ModAt:          at,
		Actor:          in.Actor,
	}
	if partial {
		quantity := remaining
		row.Quantity = &quantity
	} else {
		row.GivenTo = in.Recipient
	}
	if err := tx.UpdateInventory(ctx, row); err != nil {
		return domain.AdminInventoryItem{}, domain.InventoryMovement{}, err
	}
	if !partial {
		if err := tx.SetDeleted(ctx, current.ID, true, at, in.Actor); err != nil {
			return domain.AdminInventoryItem{}, domain.InventoryMovement{}, err
		}
	}

	movement := newMovement(current, domain.MovementIssue, in.Quantity, remaining,
		in.Recipient, current.SourceLocation, "", in.Note, in.Actor, at,
		in.CorrelationID, in.IdempotencyKey)
	movementID, err := tx.InsertMovement(ctx, movement)
	if err != nil {
		return domain.AdminInventoryItem{}, domain.InventoryMovement{}, err
	}
	movement.MovementID = movementID
	entry, err := movementAudit(current, movement, "ITEM_ISSUED", "", in.SourceIP)
	if err != nil {
		return domain.AdminInventoryItem{}, domain.InventoryMovement{}, err
	}
	if err := tx.InsertAudit(ctx, entry); err != nil {
		return domain.AdminInventoryItem{}, domain.InventoryMovement{}, err
	}
	return current, movement, nil
}

// issueAttempt is the manual Issue wrapper: it owns the transaction, delegates
// the sequence to issueLocked and builds the view result after the commit.
func (s *AdminInventoryService) issueAttempt(ctx context.Context, in domain.IssueItemInput) (AdminWriteResult, error) {
	tx, err := s.store.BeginAdminWrite(ctx)
	if err != nil {
		return AdminWriteResult{}, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	current, movement, err := s.issueLocked(ctx, tx, in)
	if err != nil {
		return AdminWriteResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return AdminWriteResult{}, err
	}
	committed = true

	updated := current
	if in.Quantity < current.Quantity {
		updated.Quantity = movement.RemainingQuantity
	}
	updated.ModAt = movement.OccurredAt
	updated.ModBy = in.Actor.UserName
	actorID := in.Actor.UserID
	updated.ModByUserID = &actorID
	if in.Quantity == current.Quantity {
		updated.DeletedFg = true
		updated.GivenTo = in.Recipient
	}
	return AdminWriteResult{Item: updated, Changed: true, Movement: &movement}, nil
}

// Return records a return against an active lot or a whole-issued lot. A full
// return of a whole-issued lot restores the row and clears GivenTo; a partial
// return creates a new active lot and leaves the issued row as history. Dates
// are copied from the source lot (OD-03).
func (s *AdminInventoryService) Return(ctx context.Context, in domain.ReturnItemInput) (AdminWriteResult, error) {
	if err := s.requireReady(); err != nil {
		return AdminWriteResult{}, err
	}
	if err := validateActor(in.Actor); err != nil {
		return AdminWriteResult{}, err
	}
	if in.GuildBankItemID <= 0 {
		return AdminWriteResult{}, domain.ErrNotFound
	}
	fields := make(map[string]string)
	if in.Quantity < 1 {
		fields["quantity"] = "quantity must be at least 1"
	}
	if in.ExpectedModAt.IsZero() {
		fields["expectedModAt"] = "expectedModAt is required"
	}
	in.Recipient = normalizeMovementText("recipient", in.Recipient, AdminMaxGivenToRunes, false, fields)
	in.Note = normalizeMovementText("note", in.Note, AdminMaxNoteRunes, false, fields)
	in.IdempotencyKey = normalizeIdempotencyKey(in.IdempotencyKey, fields)
	if len(fields) > 0 {
		return AdminWriteResult{}, &domain.ValidationError{Fields: fields}
	}
	return s.withDeadlockRetry("return", func() (AdminWriteResult, error) {
		return s.returnAttempt(ctx, in)
	})
}

func (s *AdminInventoryService) returnAttempt(ctx context.Context, in domain.ReturnItemInput) (AdminWriteResult, error) {
	tx, err := s.store.BeginAdminWrite(ctx)
	if err != nil {
		return AdminWriteResult{}, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	current, err := tx.LockInventory(ctx, in.GuildBankItemID)
	if err != nil {
		return AdminWriteResult{}, err
	}
	if !sameModAt(current.ModAt, in.ExpectedModAt) {
		return AdminWriteResult{}, fmt.Errorf("%w: the record changed since it was loaded", domain.ErrConflict)
	}
	at := s.now().UTC()
	// A lot-level return is attributed FIFO to the earliest outstanding ISSUE
	// movement on the same lot, so the issued rows it closes leave the Issued
	// view. A stock return against a lot with no outstanding issuance stays
	// unattributed (RelatedMovementID = nil).
	relatedID, err := tx.EarliestOutstandingIssue(ctx, current.ID)
	if err != nil {
		return AdminWriteResult{}, err
	}
	var related *int64
	if relatedID > 0 {
		related = &relatedID
	}

	if !current.DeletedFg {
		result, err := s.returnToActive(ctx, tx, current, in, at, related)
		if err == nil {
			committed = true
		}
		return result, err
	}
	if strings.TrimSpace(current.GivenTo) == "" {
		return AdminWriteResult{}, domain.NewValidationError("guildBankItemId",
			"a row removed administratively cannot be returned; restore it instead")
	}
	if in.Quantity > current.Quantity {
		return AdminWriteResult{}, domain.NewValidationError("quantity",
			"return quantity cannot exceed the issued quantity")
	}
	if in.Quantity == current.Quantity {
		result, err := s.returnWholeIssued(ctx, tx, current, in, at, related)
		if err == nil {
			committed = true
		}
		return result, err
	}
	result, err := s.returnPartialIssued(ctx, tx, current, in, at, related)
	if err == nil {
		committed = true
	}
	return result, err
}

// ReturnIssuance closes part or all of one ISSUE movement. The lot-side branch
// is selected exactly as the lot-level return selects it, but the RETURN is
// attributed to the addressed movement rather than to the FIFO movement.
func (s *AdminInventoryService) ReturnIssuance(ctx context.Context, in domain.ReturnIssuanceInput) (AdminWriteResult, error) {
	if err := s.requireReady(); err != nil {
		return AdminWriteResult{}, err
	}
	if err := validateActor(in.Actor); err != nil {
		return AdminWriteResult{}, err
	}
	if in.MovementID <= 0 {
		return AdminWriteResult{}, domain.ErrNotFound
	}
	fields := make(map[string]string)
	if in.Quantity < 1 {
		fields["quantity"] = "quantity must be at least 1"
	}
	if in.ExpectedModAt.IsZero() {
		fields["expectedModAt"] = "expectedModAt is required"
	}
	in.Recipient = normalizeMovementText("recipient", in.Recipient, AdminMaxGivenToRunes, false, fields)
	in.Note = normalizeMovementText("note", in.Note, AdminMaxNoteRunes, false, fields)
	in.IdempotencyKey = normalizeIdempotencyKey(in.IdempotencyKey, fields)
	if len(fields) > 0 {
		return AdminWriteResult{}, &domain.ValidationError{Fields: fields}
	}
	return s.withDeadlockRetry("return", func() (AdminWriteResult, error) {
		return s.returnIssuanceAttempt(ctx, in)
	})
}

func (s *AdminInventoryService) returnIssuanceAttempt(ctx context.Context, in domain.ReturnIssuanceInput) (AdminWriteResult, error) {
	tx, err := s.store.BeginAdminWrite(ctx)
	if err != nil {
		return AdminWriteResult{}, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	movement, outstanding, err := tx.LockMovementOutstanding(ctx, in.MovementID)
	if err != nil {
		return AdminWriteResult{}, err
	}
	if movement.MovementType != domain.MovementIssue {
		return AdminWriteResult{}, domain.NewValidationError("movementId",
			"only an issued movement can be returned")
	}
	if outstanding == 0 {
		return AdminWriteResult{}, fmt.Errorf("%w: the issuance has no outstanding quantity", domain.ErrConflict)
	}
	if in.Quantity > outstanding {
		return AdminWriteResult{}, domain.NewValidationError("quantity",
			"return quantity cannot exceed the outstanding quantity")
	}
	current, err := tx.LockInventory(ctx, movement.GuildBankItemID)
	if err != nil {
		return AdminWriteResult{}, err
	}
	if !sameModAt(current.ModAt, in.ExpectedModAt) {
		return AdminWriteResult{}, fmt.Errorf("%w: the record changed since it was loaded", domain.ErrConflict)
	}
	if strings.TrimSpace(in.Recipient) == "" {
		in.Recipient = strings.TrimSpace(movement.Recipient)
	}
	at := s.now().UTC()
	related := movement.MovementID
	item := returnItemInputOf(in)
	var result AdminWriteResult
	switch {
	case !current.DeletedFg:
		result, err = s.returnToActive(ctx, tx, current, item, at, &related)
	case strings.TrimSpace(current.GivenTo) != "" && in.Quantity == current.Quantity:
		result, err = s.returnWholeIssued(ctx, tx, current, item, at, &related)
	default:
		// A partial return of a whole-stack issue, or any return against an
		// administratively removed lot, lands the units in a new active lot
		// while the source lot keeps its state (D6).
		result, err = s.returnPartialIssued(ctx, tx, current, item, at, &related)
	}
	if err == nil {
		committed = true
	}
	return result, err
}

// returnItemInputOf adapts the movement-targeted command to the shared lot-side
// return branches. GuildBankItemID is unused by those branches; the lot is
// passed explicitly.
func returnItemInputOf(in domain.ReturnIssuanceInput) domain.ReturnItemInput {
	return domain.ReturnItemInput{
		Quantity:       in.Quantity,
		Recipient:      in.Recipient,
		Note:           in.Note,
		IdempotencyKey: in.IdempotencyKey,
		Actor:          in.Actor,
		CorrelationID:  in.CorrelationID,
		SourceIP:       in.SourceIP,
	}
}

// IssuanceForReturn resolves one ISSUE movement into the lot record, the
// outstanding quantity and the movement itself, for the return form.
func (s *AdminInventoryService) IssuanceForReturn(ctx context.Context, movementID int64) (domain.InventoryMovement, domain.AdminInventoryItem, uint32, error) {
	if err := s.requireReady(); err != nil {
		return domain.InventoryMovement{}, domain.AdminInventoryItem{}, 0, err
	}
	if movementID <= 0 {
		return domain.InventoryMovement{}, domain.AdminInventoryItem{}, 0, domain.ErrNotFound
	}
	movement, err := s.store.GetMovement(ctx, movementID)
	if err != nil {
		return domain.InventoryMovement{}, domain.AdminInventoryItem{}, 0, err
	}
	if movement.MovementType != domain.MovementIssue {
		return domain.InventoryMovement{}, domain.AdminInventoryItem{}, 0, domain.ErrNotFound
	}
	item, err := s.store.GetAdminInventory(ctx, movement.GuildBankItemID)
	if err != nil {
		return domain.InventoryMovement{}, domain.AdminInventoryItem{}, 0, err
	}
	outstanding, err := s.store.GetMovementOutstanding(ctx, movementID)
	if err != nil {
		return domain.InventoryMovement{}, domain.AdminInventoryItem{}, 0, err
	}
	return movement, item, outstanding, nil
}

// returnToActive increases an active lot's quantity and records a RETURN.
func (s *AdminInventoryService) returnToActive(ctx context.Context, tx AdminWriteTx,
	current domain.AdminInventoryItem, in domain.ReturnItemInput, at time.Time, related *int64) (AdminWriteResult, error) {
	if strings.TrimSpace(current.GivenTo) != "" {
		return AdminWriteResult{}, fmt.Errorf("%w: an active row cannot carry a recipient", domain.ErrConflict)
	}
	sum := uint64(current.Quantity) + uint64(in.Quantity)
	if sum > math.MaxUint32 {
		return AdminWriteResult{}, domain.NewValidationError("quantity",
			"the returned quantity exceeds the storage range")
	}
	quantity := uint32(sum)
	row := domain.AdminUpdateRow{
		ID:             current.ID,
		SourceLocation: current.SourceLocation,
		LocationGroup:  current.LocationGroup,
		LocationSort:   current.LocationSort,
		GivenTo:        current.GivenTo,
		Quantity:       &quantity,
		ModAt:          at,
		Actor:          in.Actor,
	}
	if err := tx.UpdateInventory(ctx, row); err != nil {
		return AdminWriteResult{}, err
	}
	movement := newMovement(current, domain.MovementReturn, in.Quantity, quantity,
		in.Recipient, "", current.SourceLocation, in.Note, in.Actor, at,
		in.CorrelationID, in.IdempotencyKey)
	movement.RelatedMovementID = related
	movementID, err := tx.InsertMovement(ctx, movement)
	if err != nil {
		return AdminWriteResult{}, err
	}
	movement.MovementID = movementID
	entry, err := movementAudit(current, movement, "ITEM_RETURNED", "", in.SourceIP)
	if err != nil {
		return AdminWriteResult{}, err
	}
	if err := tx.InsertAudit(ctx, entry); err != nil {
		return AdminWriteResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return AdminWriteResult{}, err
	}
	updated := current
	updated.Quantity = quantity
	updated.ModAt = at
	updated.ModBy = in.Actor.UserName
	actorID := in.Actor.UserID
	updated.ModByUserID = &actorID
	return AdminWriteResult{Item: updated, Changed: true, Movement: &movement}, nil
}

// returnWholeIssued restores a whole-issued row and clears GivenTo (OD-02).
func (s *AdminInventoryService) returnWholeIssued(ctx context.Context, tx AdminWriteTx,
	current domain.AdminInventoryItem, in domain.ReturnItemInput, at time.Time, related *int64) (AdminWriteResult, error) {
	if err := tx.SetDeleted(ctx, current.ID, false, at, in.Actor); err != nil {
		return AdminWriteResult{}, err
	}
	row := domain.AdminUpdateRow{
		ID:             current.ID,
		SourceLocation: current.SourceLocation,
		LocationGroup:  current.LocationGroup,
		LocationSort:   current.LocationSort,
		GivenTo:        "",
		ModAt:          at,
		Actor:          in.Actor,
	}
	if err := tx.UpdateInventory(ctx, row); err != nil {
		return AdminWriteResult{}, err
	}
	movement := newMovement(current, domain.MovementReturn, in.Quantity, in.Quantity,
		in.Recipient, "", current.SourceLocation, in.Note, in.Actor, at,
		in.CorrelationID, in.IdempotencyKey)
	movement.RelatedMovementID = related
	movementID, err := tx.InsertMovement(ctx, movement)
	if err != nil {
		return AdminWriteResult{}, err
	}
	movement.MovementID = movementID
	entry, err := movementAudit(current, movement, "ITEM_RETURNED", "", in.SourceIP)
	if err != nil {
		return AdminWriteResult{}, err
	}
	if err := tx.InsertAudit(ctx, entry); err != nil {
		return AdminWriteResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return AdminWriteResult{}, err
	}
	updated := current
	updated.DeletedFg = false
	updated.GivenTo = ""
	updated.ModAt = at
	updated.ModBy = in.Actor.UserName
	actorID := in.Actor.UserID
	updated.ModByUserID = &actorID
	return AdminWriteResult{Item: updated, Changed: true, Movement: &movement}, nil
}

// returnPartialIssued creates a new active lot for the returned quantity and
// leaves the issued row as history. The movement references the issued row.
func (s *AdminInventoryService) returnPartialIssued(ctx context.Context, tx AdminWriteTx,
	current domain.AdminInventoryItem, in domain.ReturnItemInput, at time.Time, related *int64) (AdminWriteResult, error) {
	actorID := in.Actor.UserID
	row := domain.GuildBankItem{
		ItemType: current.ItemType, EQUID: current.EQUID,
		MetadataMissingFg: current.MetadataMissingFg, Name: current.Name,
		ClassString: current.ClassString, RaceString: current.RaceString,
		DateAdded: current.DateAdded, DateOpen: current.DateOpen, DateBazaar: current.DateBazaar,
		Quantity: in.Quantity, SourceLocation: current.SourceLocation,
		LocationGroup: current.LocationGroup, LocationSort: current.LocationSort,
		GivenTo: "", DeletedFg: false, NameSort: current.NameSort,
		ModAt: at, ModBy: in.Actor.UserName, ModByUserID: &actorID,
	}
	ids, err := tx.InsertInventory(ctx, []domain.GuildBankItem{row})
	if err != nil {
		return AdminWriteResult{}, err
	}
	if len(ids) != 1 {
		return AdminWriteResult{}, fmt.Errorf("%w: inserted inventory id count mismatch", domain.ErrConflict)
	}
	item := adminItemFromRow(row, ids[0])
	movement := newMovement(item, domain.MovementReturn, in.Quantity, in.Quantity,
		in.Recipient, "", row.SourceLocation, in.Note, in.Actor, at,
		in.CorrelationID, in.IdempotencyKey)
	relatedItem := current.ID
	movement.RelatedItemID = &relatedItem
	movement.RelatedMovementID = related
	movementID, err := tx.InsertMovement(ctx, movement)
	if err != nil {
		return AdminWriteResult{}, err
	}
	movement.MovementID = movementID
	entry, err := movementAudit(item, movement, "ITEM_RETURNED", "", in.SourceIP)
	if err != nil {
		return AdminWriteResult{}, err
	}
	if err := tx.InsertAudit(ctx, entry); err != nil {
		return AdminWriteResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return AdminWriteResult{}, err
	}
	return AdminWriteResult{
		Item: item, CreatedIDs: ids, CreatedCount: 1, Changed: true, Movement: &movement,
	}, nil
}

// OnHand returns the approved on-hand unit total for a validated filter. The
// repository sums active rows only; the movement ledger is never counted.
func (s *AdminInventoryService) OnHand(ctx context.Context, filter domain.AdminInventoryFilter) (uint64, error) {
	if err := s.requireReady(); err != nil {
		return 0, err
	}
	if filter.Page <= 0 {
		filter.Page = 1
	}
	if filter.PageSize <= 0 {
		filter.PageSize = AdminDefaultPageSize
	}
	normalized, err := NormalizeAdminInventoryFilter(filter)
	if err != nil {
		return 0, err
	}
	return s.store.SumActiveQuantity(ctx, normalized)
}

// ListMovements returns one bounded page of the append-only ledger. The
// repository applies the allowlisted filters and newest-first ordering. A
// history request scoped to one lot first resolves the lot so an unknown lot ID
// is a 404 rather than an empty page.
func (s *AdminInventoryService) ListMovements(ctx context.Context, filter domain.MovementFilter) (domain.MovementPage, error) {
	if err := s.requireReady(); err != nil {
		return domain.MovementPage{}, err
	}
	if filter.Page <= 0 {
		filter.Page = 1
	}
	if filter.PageSize <= 0 {
		filter.PageSize = AdminDefaultPageSize
	}
	if filter.GuildBankItemID > 0 {
		if _, err := s.store.GetAdminInventory(ctx, filter.GuildBankItemID); err != nil {
			return domain.MovementPage{}, err
		}
	}
	return s.store.ListMovements(ctx, filter)
}

// Stackability reports the advisory classification for one EQUID. It is
// presentation-only: the issue path never blocks on it, because partial issuance
// is governed by the on-hand lot quantity. The read is retained so a future
// EQUID metadata source can surface the value without changing the write rules.
func (s *AdminInventoryService) Stackability(ctx context.Context, equid uint64) (domain.Stackability, error) {
	if err := s.requireReady(); err != nil {
		return domain.StackabilityUnknown, err
	}
	if equid == 0 {
		return domain.StackabilityUnknown, nil
	}
	return s.lookupStackability(ctx, equid)
}

// GetMovement returns one ledger row by ID.
func (s *AdminInventoryService) GetMovement(ctx context.Context, id int64) (domain.InventoryMovement, error) {
	if err := s.requireReady(); err != nil {
		return domain.InventoryMovement{}, err
	}
	if id <= 0 {
		return domain.InventoryMovement{}, domain.ErrNotFound
	}
	return s.store.GetMovement(ctx, id)
}
