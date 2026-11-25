package mariadb

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"strings"

	"github.com/Panazule/guildbank/internal/domain"
)

const (
	movementDefaultPageSize = 50
	movementMaxPageSize     = 200
	movementMaxPage         = 10000
)

// movementColumns is the explicit ledger projection. The table is append-only;
// no update or delete statement exists in this package.
const movementColumns = `MovementID, GuildBankItemID, EQUID, MovementType, Quantity,
	Recipient, ActorUserID, ActorName, OccurredAt, FromLocation, ToLocation,
	RemainingQuantity, RelatedItemID, RelatedMovementID, Note, CorrelationID, IdempotencyKey`

// ListMovements reads one bounded page from one read-only snapshot.
func (s *Store) ListMovements(ctx context.Context, filter domain.MovementFilter) (domain.MovementPage, error) {
	normalized, err := normalizeMovementFilter(filter)
	if err != nil {
		return domain.MovementPage{}, err
	}
	where, args := movementWhere(normalized)
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return domain.MovementPage{}, mapStoreError("begin movement read", err)
	}
	defer func() { _ = tx.Rollback() }()

	var total int
	if err := tx.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM InventoryMovement WHERE `+where, args...).Scan(&total); err != nil {
		return domain.MovementPage{}, mapStoreError("count inventory movements", err)
	}
	page := domain.MovementPage{
		Movements: []domain.InventoryMovement{}, Total: total,
		Page: normalized.Page, PageSize: normalized.PageSize, Filter: normalized,
	}
	if total > 0 {
		page.TotalPages = (total + normalized.PageSize - 1) / normalized.PageSize
	}
	queryArgs := append(append([]any(nil), args...), normalized.PageSize, (normalized.Page-1)*normalized.PageSize)
	rows, err := tx.QueryContext(ctx,
		`SELECT `+movementColumns+` FROM InventoryMovement WHERE `+where+
			` ORDER BY OccurredAt DESC, MovementID DESC LIMIT ? OFFSET ?`, queryArgs...)
	if err != nil {
		return domain.MovementPage{}, mapStoreError("list inventory movements", err)
	}
	for rows.Next() {
		movement, err := scanMovement(rows)
		if err != nil {
			_ = rows.Close()
			return domain.MovementPage{}, err
		}
		page.Movements = append(page.Movements, movement)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return domain.MovementPage{}, mapStoreError("read inventory movements", err)
	}
	if err := rows.Close(); err != nil {
		return domain.MovementPage{}, mapStoreError("close inventory movements", err)
	}
	if err := tx.Commit(); err != nil {
		return domain.MovementPage{}, mapStoreError("commit movement read", err)
	}
	return page, nil
}

// GetMovement reads one ledger row by primary key.
func (s *Store) GetMovement(ctx context.Context, movementID int64) (domain.InventoryMovement, error) {
	if movementID <= 0 {
		return domain.InventoryMovement{}, domain.ErrNotFound
	}
	row := s.db.QueryRowContext(ctx,
		`SELECT `+movementColumns+` FROM InventoryMovement WHERE MovementID = ?`, movementID)
	return scanMovement(row)
}

// GetMovementOutstanding returns the outstanding quantity of one ISSUE
// movement: its issued quantity minus every attributed RETURN. A movement that
// is not an ISSUE, or an ISSUE that is fully returned, returns 0.
func (s *Store) GetMovementOutstanding(ctx context.Context, movementID int64) (uint32, error) {
	if movementID <= 0 {
		return 0, domain.ErrNotFound
	}
	var movementType string
	var quantity int64
	var returned sql.NullInt64
	if err := s.db.QueryRowContext(ctx, `
		SELECT m.MovementType, m.Quantity, (
			SELECT SUM(r.Quantity) FROM InventoryMovement r
			WHERE r.MovementType = 'RETURN' AND r.RelatedMovementID = m.MovementID
		) FROM InventoryMovement m WHERE m.MovementID = ?`, movementID).
		Scan(&movementType, &quantity, &returned); err != nil {
		if errorsIsNoRows(err) {
			return 0, domain.ErrNotFound
		}
		return 0, mapStoreError("read movement outstanding", err)
	}
	return outstandingOf(movementType, quantity, returned), nil
}

// outstandingOf clamps the issued-minus-returned arithmetic to a uint32 and
// reports 0 for anything that is not an outstanding ISSUE.
func outstandingOf(movementType string, quantity int64, returned sql.NullInt64) uint32 {
	if domain.MovementType(movementType) != domain.MovementIssue || quantity < 1 {
		return 0
	}
	outstanding := quantity
	if returned.Valid {
		outstanding -= returned.Int64
	}
	if outstanding <= 0 {
		return 0
	}
	return uint32(outstanding)
}

// LockMovementOutstanding locks one movement row and returns it with its
// outstanding quantity, both read inside the caller's write transaction.
func (c *adminWriteTx) LockMovementOutstanding(ctx context.Context, movementID int64) (domain.InventoryMovement, uint32, error) {
	row := c.tx.QueryRowContext(ctx,
		`SELECT `+movementColumns+` FROM InventoryMovement WHERE MovementID = ? FOR UPDATE`, movementID)
	movement, err := scanMovement(row)
	if err != nil {
		return domain.InventoryMovement{}, 0, err
	}
	var quantity int64
	var returned sql.NullInt64
	if err := c.tx.QueryRowContext(ctx, `
		SELECT m.Quantity, (
			SELECT SUM(r.Quantity) FROM InventoryMovement r
			WHERE r.MovementType = 'RETURN' AND r.RelatedMovementID = m.MovementID
		) FROM InventoryMovement m WHERE m.MovementID = ?`, movementID).
		Scan(&quantity, &returned); err != nil {
		return domain.InventoryMovement{}, 0, mapStoreError("read movement outstanding", err)
	}
	return movement, outstandingOf(string(movement.MovementType), quantity, returned), nil
}

// EarliestOutstandingIssue returns the earliest ISSUE movement on the lot whose
// outstanding quantity is still greater than zero, or 0 when the lot has no
// outstanding issuance. It is the FIFO attribution used by the lot-level return.
func (c *adminWriteTx) EarliestOutstandingIssue(ctx context.Context, lotID int64) (int64, error) {
	var movementID sql.NullInt64
	if err := c.tx.QueryRowContext(ctx, `
		SELECT m.MovementID FROM InventoryMovement m
		WHERE m.MovementType = 'ISSUE' AND m.GuildBankItemID = ?
		  AND m.Quantity > COALESCE((
			SELECT SUM(r.Quantity) FROM InventoryMovement r
			WHERE r.MovementType = 'RETURN' AND r.RelatedMovementID = m.MovementID
		  ), 0)
		ORDER BY m.MovementID ASC LIMIT 1 FOR UPDATE`, lotID).Scan(&movementID); err != nil {
		if errorsIsNoRows(err) {
			return 0, nil
		}
		return 0, mapStoreError("find outstanding issue", err)
	}
	if !movementID.Valid {
		return 0, nil
	}
	return movementID.Int64, nil
}

// InsertMovement appends one ledger row inside the caller's admin write
// transaction. A duplicate non-null IdempotencyKey maps to ErrConflict.
func (c *adminWriteTx) InsertMovement(ctx context.Context, movement domain.InventoryMovement) (int64, error) {
	if err := movement.Validate(); err != nil {
		return 0, err
	}
	result, err := c.tx.ExecContext(ctx, `INSERT INTO InventoryMovement
		(GuildBankItemID, EQUID, MovementType, Quantity, Recipient, ActorUserID,
		 ActorName, OccurredAt, FromLocation, ToLocation, RemainingQuantity,
		 RelatedItemID, RelatedMovementID, Note, CorrelationID, IdempotencyKey)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		movement.GuildBankItemID, movement.EQUID, string(movement.MovementType),
		movement.Quantity, nullableString(movement.Recipient),
		nullableInt64Ptr(movement.ActorUserID), movement.ActorUserName,
		movement.OccurredAt.UTC(), nullableString(movement.FromLocation),
		nullableString(movement.ToLocation), movement.RemainingQuantity,
		nullableInt64Ptr(movement.RelatedItemID), nullableInt64Ptr(movement.RelatedMovementID),
		nullableString(movement.Note),
		nullableString(movement.CorrelationID), nullableString(movement.IdempotencyKey))
	if err != nil {
		return 0, mapStoreError("insert inventory movement", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return 0, mapStoreError("read inventory movement id", err)
	}
	if id <= 0 {
		return 0, fmt.Errorf("mariadb: inventory movement insert returned id %d", id)
	}
	return id, nil
}

// SumActiveQuantity returns the approved on-hand unit total. It filters to
// active lots and never reads InventoryMovement. Other admin predicates are
// retained so a filtered total uses the same definition as the list.
func (s *Store) SumActiveQuantity(ctx context.Context, filter domain.AdminInventoryFilter) (uint64, error) {
	filter.Status = domain.AdminStatusActive
	where, args := adminInventoryWhere(filter)
	var sum sql.NullString
	if err := s.db.QueryRowContext(ctx,
		`SELECT COALESCE(SUM(Quantity), 0) FROM GuildBankItems WHERE `+where, args...).Scan(&sum); err != nil {
		return 0, mapStoreError("sum active inventory quantity", err)
	}
	if !sum.Valid || sum.String == "" {
		return 0, nil
	}
	value, err := strconv.ParseUint(sum.String, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("mariadb: invalid active quantity sum %q: %w", sum.String, err)
	}
	return value, nil
}

func normalizeMovementFilter(filter domain.MovementFilter) (domain.MovementFilter, error) {
	fields := make(map[string]string)
	filter.Recipient = strings.TrimSpace(filter.Recipient)
	filter.CorrelationID = strings.TrimSpace(filter.CorrelationID)
	if filter.GuildBankItemID < 0 {
		fields["guildBankItemId"] = "guildBankItemId must be a positive integer"
	}
	if filter.MovementType != "" && !filter.MovementType.Valid() {
		fields["movementType"] = "movementType must be one of the approved values"
	}
	if filter.Page <= 0 {
		filter.Page = 1
	}
	if filter.Page > movementMaxPage {
		fields["page"] = fmt.Sprintf("page must be between 1 and %d", movementMaxPage)
	}
	if filter.PageSize <= 0 {
		filter.PageSize = movementDefaultPageSize
	}
	if filter.PageSize > movementMaxPageSize {
		fields["pageSize"] = fmt.Sprintf("pageSize must be between 1 and %d", movementMaxPageSize)
	}
	if !filter.From.IsZero() && !filter.To.IsZero() && filter.From.After(filter.To) {
		fields["from"] = "from must not be after to"
	}
	if len(fields) > 0 {
		return domain.MovementFilter{}, &domain.ValidationError{Fields: fields}
	}
	return filter, nil
}

func movementWhere(filter domain.MovementFilter) (string, []any) {
	where := []string{"1 = 1"}
	args := make([]any, 0, 7)
	if filter.GuildBankItemID > 0 {
		where = append(where, "GuildBankItemID = ?")
		args = append(args, filter.GuildBankItemID)
	}
	if filter.EQUID > 0 {
		where = append(where, "EQUID = ?")
		args = append(args, filter.EQUID)
	}
	if filter.MovementType != "" {
		where = append(where, "MovementType = ?")
		args = append(args, string(filter.MovementType))
	}
	if filter.Recipient != "" {
		where = append(where, "Recipient = ?")
		args = append(args, filter.Recipient)
	}
	if filter.CorrelationID != "" {
		where = append(where, "CorrelationID = ?")
		args = append(args, filter.CorrelationID)
	}
	if !filter.From.IsZero() {
		where = append(where, "OccurredAt >= ?")
		args = append(args, filter.From.UTC())
	}
	if !filter.To.IsZero() {
		where = append(where, "OccurredAt <= ?")
		args = append(args, filter.To.UTC())
	}
	return strings.Join(where, " AND "), args
}

func scanMovement(scanner interface{ Scan(...any) error }) (domain.InventoryMovement, error) {
	var (
		movement       domain.InventoryMovement
		movementType   string
		equid          int64
		quantity       int64
		remaining      int64
		recipient      sql.NullString
		actorUserID    sql.NullInt64
		fromLocation   sql.NullString
		toLocation     sql.NullString
		relatedItemID  sql.NullInt64
		relatedMoveID  sql.NullInt64
		note           sql.NullString
		correlationID  sql.NullString
		idempotencyKey sql.NullString
	)
	if err := scanner.Scan(&movement.MovementID, &movement.GuildBankItemID, &equid,
		&movementType, &quantity, &recipient, &actorUserID, &movement.ActorUserName,
		&movement.OccurredAt, &fromLocation, &toLocation, &remaining, &relatedItemID,
		&relatedMoveID, &note, &correlationID, &idempotencyKey); err != nil {
		if errorsIsNoRows(err) {
			return domain.InventoryMovement{}, domain.ErrNotFound
		}
		return domain.InventoryMovement{}, mapStoreError("scan inventory movement row", err)
	}
	if movement.MovementID <= 0 || movement.GuildBankItemID <= 0 || equid < 0 ||
		quantity < 1 || remaining < 0 {
		return domain.InventoryMovement{}, fmt.Errorf("mariadb: malformed inventory movement row %d", movement.MovementID)
	}
	if !domain.MovementType(movementType).Valid() {
		return domain.InventoryMovement{}, fmt.Errorf("mariadb: inventory movement row %d has invalid type", movement.MovementID)
	}
	movement.EQUID = uint64(equid)
	movement.MovementType = domain.MovementType(movementType)
	movement.Quantity = uint32(quantity)
	movement.RemainingQuantity = uint32(remaining)
	movement.Recipient = recipient.String
	movement.FromLocation = fromLocation.String
	movement.ToLocation = toLocation.String
	movement.Note = note.String
	movement.CorrelationID = correlationID.String
	movement.IdempotencyKey = idempotencyKey.String
	movement.OccurredAt = movement.OccurredAt.UTC()
	if actorUserID.Valid {
		value := actorUserID.Int64
		movement.ActorUserID = &value
	}
	if relatedItemID.Valid {
		value := relatedItemID.Int64
		movement.RelatedItemID = &value
	}
	if relatedMoveID.Valid {
		value := relatedMoveID.Int64
		movement.RelatedMovementID = &value
	}
	return movement, nil
}

func nullableInt64Ptr(value *int64) any {
	if value == nil {
		return nil
	}
	return *value
}
