package mariadb

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/Panazule/guildbank/internal/domain"
)

// requestColumns is the stored projection. Reads that render availability use
// requestReadColumns instead, which appends the two derived fields from the
// joined lot; the lock path uses this one so FOR UPDATE touches only the
// request row and never the lot (Phase 6 §6.2 fixes the lock order as
// request first, lot second).
const requestColumns = `RequestID, GuildBankItemID, EQUID, ItemName, RequestedQuantity,
	OnHandAtCreate, CharacterName, RequesterKind, RequesterUserID, RequesterUserName,
	ContactDiscord, Note, Status, DecisionNote, IssuedQuantity, CompletedMovementID,
	CorrelationID, IdempotencyKey, CreatedAt, CreatedByUserID, CreatedByName,
	DecidedAt, DecidedByUserID, DecidedByName`

// requestReadColumns is requestColumns plus CurrentOnHand and LotActive. The
// projection is derived, never stored, so a request still reads correctly after
// the lot is issued, consolidated or soft-deleted; LotActive simply reports
// whether the linked lot is still usable and CurrentOnHand what is left.
const requestReadColumns = `r.RequestID, r.GuildBankItemID, r.EQUID, r.ItemName,
	r.RequestedQuantity, r.OnHandAtCreate, r.CharacterName, r.RequesterKind,
	r.RequesterUserID, r.RequesterUserName, r.ContactDiscord, r.Note, r.Status,
	r.DecisionNote, r.IssuedQuantity, r.CompletedMovementID, r.CorrelationID,
	r.IdempotencyKey, r.CreatedAt, r.CreatedByUserID, r.CreatedByName, r.DecidedAt,
	r.DecidedByUserID, r.DecidedByName,
	COALESCE(g.Quantity, 0), (g.GuildBankItemID IS NOT NULL AND g.DeletedFg = 0)`

// GetRequest reads one request with its lot projection.
func (s *Store) GetRequest(ctx context.Context, requestID int64) (domain.InventoryRequest, error) {
	if requestID <= 0 {
		return domain.InventoryRequest{}, domain.ErrNotFound
	}
	row := s.db.QueryRowContext(ctx, `SELECT `+requestReadColumns+`
		FROM InventoryRequest r
		LEFT JOIN GuildBankItems g ON g.GuildBankItemID = r.GuildBankItemID
		WHERE r.RequestID = ?`, requestID)
	return scanRequest(row, true)
}

// RequestByIdempotencyKey resolves a replayed creation form to the row the
// first submission wrote, so the duplicate POST returns the original receipt
// (Phase 14.7) instead of queuing a second request.
func (s *Store) RequestByIdempotencyKey(ctx context.Context, key string) (domain.InventoryRequest, error) {
	key = strings.TrimSpace(key)
	if key == "" {
		return domain.InventoryRequest{}, domain.ErrNotFound
	}
	row := s.db.QueryRowContext(ctx, `SELECT `+requestReadColumns+`
		FROM InventoryRequest r
		LEFT JOIN GuildBankItems g ON g.GuildBankItemID = r.GuildBankItemID
		WHERE r.IdempotencyKey = ?`, key)
	return scanRequest(row, true)
}

// ListRequests reads one bounded page and its total from one read-only
// snapshot. Order is fixed newest-first: the queue is worked from the top and
// the order is not caller-selectable.
func (s *Store) ListRequests(ctx context.Context, filter domain.RequestFilter) (domain.RequestPage, error) {
	normalized, err := normalizeRequestFilter(filter)
	if err != nil {
		return domain.RequestPage{}, err
	}
	where, args := requestWhere(normalized)
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return domain.RequestPage{}, mapStoreError("begin request read", err)
	}
	defer func() { _ = tx.Rollback() }()

	var total int
	if err := tx.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM InventoryRequest r WHERE `+where, args...).Scan(&total); err != nil {
		return domain.RequestPage{}, mapStoreError("count requests", err)
	}
	page := domain.RequestPage{
		Requests: []domain.InventoryRequest{}, Total: total,
		Page: normalized.Page, PageSize: normalized.PageSize, Filter: normalized,
	}
	if total > 0 {
		page.TotalPages = (total + normalized.PageSize - 1) / normalized.PageSize
	}
	queryArgs := append(append([]any(nil), args...), normalized.PageSize, (normalized.Page-1)*normalized.PageSize)
	// The officer queue reads oldest-first so the oldest request cannot starve;
	// every other caller keeps newest-first. The order is fixed per call and is
	// not caller-selectable beyond this documented switch.
	order := " ORDER BY r.CreatedAt DESC, r.RequestID DESC"
	if normalized.OldestFirst {
		order = " ORDER BY r.CreatedAt ASC, r.RequestID ASC"
	}
	rows, err := tx.QueryContext(ctx, `SELECT `+requestReadColumns+`
		FROM InventoryRequest r
		LEFT JOIN GuildBankItems g ON g.GuildBankItemID = r.GuildBankItemID
		WHERE `+where+order+` LIMIT ? OFFSET ?`, queryArgs...)
	if err != nil {
		return domain.RequestPage{}, mapStoreError("list requests", err)
	}
	for rows.Next() {
		request, err := scanRequest(rows, true)
		if err != nil {
			_ = rows.Close()
			return domain.RequestPage{}, err
		}
		page.Requests = append(page.Requests, request)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return domain.RequestPage{}, mapStoreError("read requests", err)
	}
	if err := rows.Close(); err != nil {
		return domain.RequestPage{}, mapStoreError("close requests", err)
	}
	if err := tx.Commit(); err != nil {
		return domain.RequestPage{}, mapStoreError("commit request read", err)
	}
	return page, nil
}

// normalizeRequestFilter bounds the query and rejects values the schema could
// not hold, mirroring normalizeMovementFilter.
func normalizeRequestFilter(filter domain.RequestFilter) (domain.RequestFilter, error) {
	fields := make(map[string]string)
	filter.CharacterName = domain.NormalizeCharacterName(filter.CharacterName)
	filter.ItemName = strings.TrimSpace(filter.ItemName)
	if filter.Status != "" && !filter.Status.Valid() {
		fields["status"] = "status must be one of the approved values"
	}
	if filter.RequesterKind != "" && !filter.RequesterKind.Valid() {
		fields["requesterKind"] = "requesterKind must be one of the approved values"
	}
	if filter.RequesterUserID < 0 {
		fields["requesterUserId"] = "requesterUserId must not be negative"
	}
	if filter.GuildBankItemID < 0 {
		fields["guildBankItemId"] = "guildBankItemId must not be negative"
	}
	if len([]rune(filter.CharacterName)) > 64 {
		fields["characterName"] = "characterName must be at most 64 characters"
	}
	if len([]rune(filter.ItemName)) > 128 {
		fields["itemName"] = "itemName must be at most 128 characters"
	}
	if filter.Page <= 0 {
		filter.Page = 1
	}
	if filter.Page > domain.RequestMaxPage {
		fields["page"] = fmt.Sprintf("page must be between 1 and %d", domain.RequestMaxPage)
	}
	if filter.PageSize <= 0 {
		filter.PageSize = domain.RequestDefaultPageSize
	}
	if filter.PageSize > domain.RequestMaxPageSize {
		fields["pageSize"] = fmt.Sprintf("pageSize must be between 1 and %d", domain.RequestMaxPageSize)
	}
	if !filter.From.IsZero() && !filter.To.IsZero() && filter.From.After(filter.To) {
		fields["from"] = "from must not be after to"
	}
	if len(fields) > 0 {
		return domain.RequestFilter{}, &domain.ValidationError{Fields: fields}
	}
	return filter, nil
}

func requestWhere(filter domain.RequestFilter) (string, []any) {
	where := []string{"1 = 1"}
	args := make([]any, 0, 8)
	if filter.Status != "" {
		where = append(where, "r.Status = ?")
		args = append(args, string(filter.Status))
	}
	if filter.RequesterKind != "" {
		where = append(where, "r.RequesterKind = ?")
		args = append(args, string(filter.RequesterKind))
	}
	if filter.RequesterUserID > 0 {
		where = append(where, "r.RequesterUserID = ?")
		args = append(args, filter.RequesterUserID)
	}
	if filter.GuildBankItemID > 0 {
		where = append(where, "r.GuildBankItemID = ?")
		args = append(args, filter.GuildBankItemID)
	}
	if filter.CharacterName != "" {
		where = append(where, "r.CharacterName = ?")
		args = append(args, filter.CharacterName)
	}
	if filter.ItemName != "" {
		where = append(where, "r.ItemName = ?")
		args = append(args, filter.ItemName)
	}
	if !filter.From.IsZero() {
		where = append(where, "r.CreatedAt >= ?")
		args = append(args, filter.From.UTC())
	}
	if !filter.To.IsZero() {
		where = append(where, "r.CreatedAt <= ?")
		args = append(args, filter.To.UTC())
	}
	// The actionable predicate is an EXISTS subquery rather than a join
	// predicate so the same WHERE text is valid for the COUNT and the page
	// query, which already joins the lot for its projection.
	if filter.Actionable {
		where = append(where,
			`r.Status = 'Pending' AND EXISTS (SELECT 1 FROM GuildBankItems ag
			 WHERE ag.GuildBankItemID = r.GuildBankItemID
			 AND ag.DeletedFg = 0 AND ag.Quantity >= 1)`)
	}
	return strings.Join(where, " AND "), args
}

// scanRequest maps one row. withProjection selects the two derived columns that
// only the availability reads select; the lock path scans the stored columns
// alone.
func scanRequest(scanner interface{ Scan(...any) error }, withProjection bool) (domain.InventoryRequest, error) {
	var (
		request         domain.InventoryRequest
		equid           int64
		requested       int64
		onHand          int64
		requesterKind   string
		status          string
		requesterUserID sql.NullInt64
		requesterName   sql.NullString
		contact         sql.NullString
		note            sql.NullString
		decisionNote    sql.NullString
		issuedQuantity  sql.NullInt64
		movementID      sql.NullInt64
		correlationID   sql.NullString
		idempotencyKey  sql.NullString
		createdByUserID sql.NullInt64
		decidedAt       sql.NullTime
		decidedByUserID sql.NullInt64
		decidedByName   sql.NullString
		currentOnHand   int64
		lotActive       int64
	)
	dest := []any{
		&request.RequestID, &request.GuildBankItemID, &equid, &request.ItemName,
		&requested, &onHand, &request.CharacterName, &requesterKind,
		&requesterUserID, &requesterName, &contact, &note, &status,
		&decisionNote, &issuedQuantity, &movementID, &correlationID,
		&idempotencyKey, &request.CreatedAt, &createdByUserID, &request.CreatedByName,
		&decidedAt, &decidedByUserID, &decidedByName,
	}
	if withProjection {
		dest = append(dest, &currentOnHand, &lotActive)
	}
	if err := scanner.Scan(dest...); err != nil {
		if errorsIsNoRows(err) {
			return domain.InventoryRequest{}, domain.ErrNotFound
		}
		return domain.InventoryRequest{}, mapStoreError("scan request row", err)
	}
	if request.RequestID <= 0 || request.GuildBankItemID <= 0 || equid < 0 ||
		requested < 1 || onHand < 0 || currentOnHand < 0 {
		return domain.InventoryRequest{}, fmt.Errorf("mariadb: malformed request row %d", request.RequestID)
	}
	if !domain.RequestStatus(status).Valid() {
		return domain.InventoryRequest{}, fmt.Errorf("mariadb: request row %d has invalid status", request.RequestID)
	}
	if !domain.RequesterKind(requesterKind).Valid() {
		return domain.InventoryRequest{}, fmt.Errorf("mariadb: request row %d has invalid requester kind", request.RequestID)
	}
	request.EQUID = uint64(equid)
	request.RequestedQuantity = uint32(requested)
	request.OnHandAtCreate = uint32(onHand)
	request.RequesterKind = domain.RequesterKind(requesterKind)
	request.Status = domain.RequestStatus(status)
	request.RequesterUserName = requesterName.String
	request.ContactDiscord = contact.String
	request.Note = note.String
	request.DecisionNote = decisionNote.String
	request.CorrelationID = correlationID.String
	request.IdempotencyKey = idempotencyKey.String
	request.DecidedByName = decidedByName.String
	request.CreatedAt = request.CreatedAt.UTC()
	if requesterUserID.Valid {
		value := requesterUserID.Int64
		request.RequesterUserID = &value
	}
	if issuedQuantity.Valid {
		value := uint32(issuedQuantity.Int64)
		request.IssuedQuantity = &value
	}
	if movementID.Valid {
		value := movementID.Int64
		request.CompletedMovementID = &value
	}
	if createdByUserID.Valid {
		value := createdByUserID.Int64
		request.CreatedByUserID = &value
	}
	if decidedByUserID.Valid {
		value := decidedByUserID.Int64
		request.DecidedByUserID = &value
	}
	if decidedAt.Valid {
		value := decidedAt.Time.UTC()
		request.DecidedAt = &value
	}
	if withProjection {
		request.CurrentOnHand = uint32(currentOnHand)
		request.LotActive = lotActive == 1
	}
	return request, nil
}

// CountPendingRequests returns how much of one member's pending budget is spent.
// A public requester has no account, so there is nothing to count and the call
// is a programming error rather than a zero.
func (s *Store) CountPendingRequests(ctx context.Context, requesterUserID int64) (int, error) {
	if requesterUserID <= 0 {
		return 0, &domain.ValidationError{Fields: map[string]string{
			"requesterUserId": "requesterUserId must be a positive integer",
		}}
	}
	var count int
	if err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM InventoryRequest WHERE RequesterUserID = ? AND Status = 'Pending'`,
		requesterUserID).Scan(&count); err != nil {
		return 0, mapStoreError("count pending requests", err)
	}
	return count, nil
}

// PendingRequestForCharacter returns the ID of the Pending request one
// character already has for one lot, or 0 when there is none. The database
// enforces the same rule with uq_request_pending; this read exists so the
// service can answer "already pending" without provoking a duplicate-key error.
func (s *Store) PendingRequestForCharacter(ctx context.Context, guildBankItemID int64, characterName string) (int64, error) {
	if guildBankItemID <= 0 {
		return 0, domain.ErrNotFound
	}
	characterName = domain.NormalizeCharacterName(characterName)
	if characterName == "" {
		return 0, nil
	}
	var requestID int64
	if err := s.db.QueryRowContext(ctx, `SELECT RequestID FROM InventoryRequest
		WHERE GuildBankItemID = ? AND CharacterName = ? AND Status = 'Pending'
		ORDER BY RequestID ASC LIMIT 1`, guildBankItemID, characterName).Scan(&requestID); err != nil {
		if errorsIsNoRows(err) {
			return 0, nil
		}
		return 0, mapStoreError("read pending request", err)
	}
	return requestID, nil
}

// PendingCharactersForLot lists the requester's own characters that already
// have a Pending request for the lot, so the item page can say so instead of
// letting the member submit a duplicate.
func (s *Store) PendingCharactersForLot(ctx context.Context, guildBankItemID, requesterUserID int64) ([]string, error) {
	if guildBankItemID <= 0 || requesterUserID <= 0 {
		return []string{}, nil
	}
	rows, err := s.db.QueryContext(ctx, `SELECT CharacterName FROM InventoryRequest
		WHERE GuildBankItemID = ? AND RequesterUserID = ? AND Status = 'Pending'
		ORDER BY CreatedAt ASC, RequestID ASC`, guildBankItemID, requesterUserID)
	if err != nil {
		return nil, mapStoreError("list pending request characters", err)
	}
	defer func() { _ = rows.Close() }()
	names := make([]string, 0, 2)
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, mapStoreError("scan pending request character", err)
		}
		names = append(names, name)
	}
	if err := rows.Err(); err != nil {
		return nil, mapStoreError("read pending request characters", err)
	}
	return names, nil
}

// PendingRequestsForLot lists the oldest Pending requests for one lot, bounded
// so the officer panel on the item page stays a small read.
func (s *Store) PendingRequestsForLot(ctx context.Context, guildBankItemID int64, limit int) ([]domain.InventoryRequest, error) {
	if guildBankItemID <= 0 {
		return []domain.InventoryRequest{}, nil
	}
	if limit <= 0 {
		limit = domain.RequestDefaultPageSize
	}
	if limit > domain.RequestMaxPageSize {
		limit = domain.RequestMaxPageSize
	}
	rows, err := s.db.QueryContext(ctx, `SELECT `+requestReadColumns+`
		FROM InventoryRequest r
		LEFT JOIN GuildBankItems g ON g.GuildBankItemID = r.GuildBankItemID
		WHERE r.GuildBankItemID = ? AND r.Status = 'Pending'
		ORDER BY r.CreatedAt ASC, r.RequestID ASC LIMIT ?`, guildBankItemID, limit)
	if err != nil {
		return nil, mapStoreError("list lot requests", err)
	}
	defer func() { _ = rows.Close() }()
	requests := make([]domain.InventoryRequest, 0, limit)
	for rows.Next() {
		request, err := scanRequest(rows, true)
		if err != nil {
			return nil, err
		}
		requests = append(requests, request)
	}
	if err := rows.Err(); err != nil {
		return nil, mapStoreError("read lot requests", err)
	}
	return requests, nil
}

// LockRequest locks one request row for the approval or decision transaction.
// It deliberately does not join the lot: the approval path locks the request
// first and the lot second, and a joined FOR UPDATE would acquire them in the
// opposite order and risk a deadlock.
func (c *adminWriteTx) LockRequest(ctx context.Context, requestID int64) (domain.InventoryRequest, error) {
	if requestID <= 0 {
		return domain.InventoryRequest{}, domain.ErrNotFound
	}
	row := c.tx.QueryRowContext(ctx,
		`SELECT `+requestColumns+` FROM InventoryRequest WHERE RequestID = ? FOR UPDATE`, requestID)
	return scanRequest(row, false)
}

// InsertRequest writes one Pending row from a creation command the service has
// already checked against the lot, the character list and the pending budget.
// The stored identity fields are snapshots taken at this moment: EQUID,
// ItemName, OnHandAtCreate and CreatedAt come from the lot the service read, not
// from the form, so a request can never claim an identity the lot contradicts.
func (c *adminWriteTx) InsertRequest(ctx context.Context, in domain.CreateRequestInput) (int64, error) {
	if err := in.Validate(); err != nil {
		return 0, err
	}
	fields := make(map[string]string)
	if in.EQUID == 0 {
		fields["equid"] = "equid must be a positive integer"
	}
	if strings.TrimSpace(in.ItemName) == "" {
		fields["itemName"] = "itemName is required"
	}
	if in.CreatedAt.IsZero() {
		fields["createdAt"] = "createdAt is required"
	}
	if len(fields) > 0 {
		return 0, &domain.ValidationError{Fields: fields}
	}

	// A member row carries the account snapshot on both the requester and the
	// creator columns; a public row carries neither and stores the fixed
	// "public" creator name instead of inventing an identity.
	var requesterUserID, requesterName, createdByUserID, contact any
	createdByName := domain.RequestPublicActorName
	if in.Kind == domain.RequesterMember {
		requesterUserID = in.Actor.UserID
		requesterName = in.Actor.UserName
		createdByUserID = in.Actor.UserID
		createdByName = in.Actor.UserName
	} else {
		contact = nullableString(strings.TrimSpace(in.ContactDiscord))
	}

	result, err := c.tx.ExecContext(ctx, `INSERT INTO InventoryRequest
		(GuildBankItemID, EQUID, ItemName, RequestedQuantity, OnHandAtCreate,
		 CharacterName, RequesterKind, RequesterUserID, RequesterUserName,
		 ContactDiscord, Note, Status, CorrelationID, IdempotencyKey, CreatedAt,
		 CreatedByUserID, CreatedByName)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,'Pending',?,?,?,?,?)`,
		in.GuildBankItemID, in.EQUID, strings.TrimSpace(in.ItemName), in.Quantity,
		in.OnHandAtCreate, domain.NormalizeCharacterName(in.CharacterName),
		string(in.Kind), requesterUserID, requesterName, contact,
		nullableString(strings.TrimSpace(in.Note)),
		strings.TrimSpace(in.CorrelationID),
		nullableString(strings.TrimSpace(in.IdempotencyKey)),
		in.CreatedAt.UTC(), createdByUserID, createdByName)
	if err != nil {
		// A duplicate here is uq_request_pending (the character already has a
		// Pending request for this lot) or uq_request_idempotency (a replayed
		// form); both surface as ErrConflict.
		return 0, mapStoreError("insert inventory request", err)
	}
	requestID, err := result.LastInsertId()
	if err != nil {
		return 0, mapStoreError("read inserted request id", err)
	}
	return requestID, nil
}

// CompleteRequest performs the guarded Pending -> Completed transition. It runs
// inside the approval transaction after the ISSUE movement was written, so the
// request and the ledger row either both exist or neither does.
func (c *adminWriteTx) CompleteRequest(ctx context.Context, p domain.CompleteRequestParams) error {
	fields := make(map[string]string)
	if p.RequestID <= 0 {
		fields["requestId"] = "requestId must be a positive integer"
	}
	if p.MovementID <= 0 {
		fields["movementId"] = "movementId must be a positive integer"
	}
	if p.IssuedQuantity == 0 {
		fields["issuedQuantity"] = "issuedQuantity must be greater than zero"
	}
	if p.DecidedAt.IsZero() {
		fields["decidedAt"] = "decidedAt is required"
	}
	validateDecidingActor(fields, p.Actor, p.DecisionNote)
	if len(fields) > 0 {
		return &domain.ValidationError{Fields: fields}
	}

	result, err := c.tx.ExecContext(ctx, `UPDATE InventoryRequest
		SET Status = 'Completed', IssuedQuantity = ?, CompletedMovementID = ?,
			DecisionNote = ?, DecidedAt = ?, DecidedByUserID = ?, DecidedByName = ?
		WHERE RequestID = ? AND Status = 'Pending'`,
		p.IssuedQuantity, p.MovementID, nullableString(strings.TrimSpace(p.DecisionNote)),
		p.DecidedAt.UTC(), p.Actor.UserID, p.Actor.UserName, p.RequestID)
	if err != nil {
		return mapStoreError("complete inventory request", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return mapStoreError("read completed request rows", err)
	}
	if affected != 1 {
		return fmt.Errorf("%w: request %d is no longer pending", domain.ErrConflict, p.RequestID)
	}
	return nil
}

// DecideRequest performs the guarded Pending -> Denied|Cancelled transition. It
// never touches inventory: a denied or cancelled request was never issued.
func (c *adminWriteTx) DecideRequest(ctx context.Context, p domain.DecideRequestParams) error {
	fields := make(map[string]string)
	if p.RequestID <= 0 {
		fields["requestId"] = "requestId must be a positive integer"
	}
	if p.Status != domain.RequestDenied && p.Status != domain.RequestCancelled {
		fields["status"] = "status must be Denied or Cancelled"
	}
	if p.DecidedAt.IsZero() {
		fields["decidedAt"] = "decidedAt is required"
	}
	validateDecidingActor(fields, p.Actor, p.DecisionNote)
	if len(fields) > 0 {
		return &domain.ValidationError{Fields: fields}
	}

	result, err := c.tx.ExecContext(ctx, `UPDATE InventoryRequest
		SET Status = ?, DecisionNote = ?, DecidedAt = ?, DecidedByUserID = ?,
			DecidedByName = ?
		WHERE RequestID = ? AND Status = 'Pending'`,
		string(p.Status), nullableString(strings.TrimSpace(p.DecisionNote)),
		p.DecidedAt.UTC(), p.Actor.UserID, p.Actor.UserName, p.RequestID)
	if err != nil {
		return mapStoreError("decide inventory request", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return mapStoreError("read decided request rows", err)
	}
	if affected != 1 {
		return fmt.Errorf("%w: request %d is no longer pending", domain.ErrConflict, p.RequestID)
	}
	return nil
}

// validateDecidingActor enforces the decided-by snapshot every terminal row
// needs (ck_request_decided) plus the decision-note bound.
func validateDecidingActor(fields map[string]string, actor domain.Actor, note string) {
	if actor.UserID <= 0 {
		fields["actorUserId"] = "actorUserId must be a positive integer"
	}
	if strings.TrimSpace(actor.UserName) == "" {
		fields["actorUserName"] = "actorUserName is required"
	}
	if len([]rune(strings.TrimSpace(note))) > domain.RequestMaxNoteRunes {
		fields["decisionNote"] = fmt.Sprintf("decisionNote must be at most %d characters", domain.RequestMaxNoteRunes)
	}
}
