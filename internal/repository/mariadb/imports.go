package mariadb

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/Panazule/guildbank/internal/domain"
	"github.com/Panazule/guildbank/internal/service"
)

const importChunkSize = 500

// unspecifiedExclusion keeps rows whose LocationGroup is Other/Unspecified out
// of the import review (IMP-C). The rows stay staged and counted on the batch,
// but they are never listed, never selectable and never committed: the
// classification stays internal bookkeeping rather than a bank location the
// reviewer can see or act on.
const unspecifiedExclusion = "LocationGroup <> 'Unspecified'"

// importBatchColumns is the explicit ImportBatch projection shared by every
// batch read, including the commit transaction.
const importBatchColumns = `ImportBatchID, FileName, FileSha256, SourceRowCount,
	AcceptedRowCount, IgnoredRowCount, Status, FailureReason, ImportedBy,
	ImportedByUserID, ImportedAt`

// CreateBatch inserts a Processing batch and returns its generated ID.
func (s *Store) CreateBatch(ctx context.Context, batch domain.ImportBatch) (int64, error) {
	result, err := s.db.ExecContext(ctx, `INSERT INTO ImportBatch
		(FileName, ImportedBy, ImportedAt, Status, ImportedByUserID, FailureReason,
		 SourceRowCount, AcceptedRowCount, FileSha256)
		VALUES (?, ?, ?, ?, ?, NULL, NULL, NULL, NULL)`,
		batch.FileName, batch.ImportedBy, batch.ImportedAt.UTC(),
		string(domain.ImportStatusProcessing), batch.ImportedByUserID)
	if err != nil {
		return 0, mapStoreError("create import batch", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("mariadb: create import batch id: %w", err)
	}
	return id, nil
}

// FailBatch records a durable failed state only while the batch is Processing.
func (s *Store) FailBatch(ctx context.Context, batchID int64, reason *string, sourceRows *int, sha *string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE ImportBatch
		SET Status='Failed', FailureReason=?,
		    SourceRowCount=COALESCE(?, SourceRowCount),
		    FileSha256=COALESCE(?, FileSha256)
		WHERE ImportBatchID=? AND Status='Processing'`,
		reason, sourceRows, sha, batchID)
	if err != nil {
		return mapStoreError("fail import batch", err)
	}
	return nil
}

// StageBatch inserts every parsed row, marks the batch Uploaded and writes the
// upload audit entry in one transaction.
func (s *Store) StageBatch(ctx context.Context, batchID int64, rows []domain.ImportRow, sourceRows, ignoredRows int, sha string, audit domain.AuditEntry) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return mapStoreError("begin staging", err)
	}
	defer func() { _ = tx.Rollback() }()

	for start := 0; start < len(rows); start += importChunkSize {
		end := min(start+importChunkSize, len(rows))
		if err := insertImportRows(ctx, tx, batchID, rows[start:end]); err != nil {
			return err
		}
	}
	result, err := tx.ExecContext(ctx, `UPDATE ImportBatch
		SET Status='Uploaded', SourceRowCount=?, AcceptedRowCount=?, IgnoredRowCount=?, FileSha256=?
		WHERE ImportBatchID=? AND Status='Processing'`,
		sourceRows, len(rows), ignoredRows, sha, batchID)
	if err != nil {
		return mapStoreError("mark batch uploaded", err)
	}
	affected, err := result.RowsAffected()
	if err != nil || affected != 1 {
		return fmt.Errorf("%w: batch is no longer processing", domain.ErrConflict)
	}
	if err := insertAudit(ctx, tx, []domain.AuditEntry{audit}); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return mapStoreError("commit staging", err)
	}
	return nil
}

func insertImportRows(ctx context.Context, tx *sql.Tx, batchID int64, rows []domain.ImportRow) error {
	if len(rows) == 0 {
		return nil
	}
	args := make([]any, 0, len(rows)*12)
	placeholders := make([]string, 0, len(rows))
	for _, row := range rows {
		placeholders = append(placeholders, "(?,?,?,?,?,?,?,?,?,?,?,?)")
		var warnings any
		if len(row.Warnings) > 0 {
			body, err := service.MarshalWarnings(row.Warnings)
			if err != nil {
				return err
			}
			warnings = string(body)
		}
		var sourceLine any
		if row.SourceLine != nil {
			sourceLine = *row.SourceLine
		}
		args = append(args, batchID, row.EQUID, row.Name, row.Quantity,
			row.LocationRaw, row.LocationGroup, boolInt(row.SelectedFg), string(row.ItemType),
			warnings, sourceLine, row.LocationSort, row.NameSort)
	}
	query := `INSERT INTO ImportList
		(ImportBatchID, EQUID, Name, Quantity, LocationRaw, LocationGroup,
		 SelectedFg, ItemType, WarningText, SourceLine, LocationSort, NameSort)
		VALUES ` + strings.Join(placeholders, ",")
	if _, err := tx.ExecContext(ctx, query, args...); err != nil {
		return mapStoreError("insert import rows", err)
	}
	return nil
}

func insertAudit(ctx context.Context, tx *sql.Tx, entries []domain.AuditEntry) error {
	return insertAuditRows(ctx, tx, entries)
}

// auditExecer is satisfied by *sql.DB and *sql.Tx, so the same insert serves a
// write transaction and the standalone user-administration path.
type auditExecer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

func insertAuditRows(ctx context.Context, exec auditExecer, entries []domain.AuditEntry) error {
	if len(entries) == 0 {
		return nil
	}
	placeholders := make([]string, 0, len(entries))
	args := make([]any, 0, len(entries)*11)
	for _, entry := range entries {
		placeholders = append(placeholders, "(?,?,?,?,?,?,?,?,?,?,?)")
		args = append(args, entry.EventDate.UTC(), nullableInt64(entry.UserID),
			entry.UserName, entry.EventType, nullableString(entry.TableName),
			nullableString(entry.RecordID), nullableJSON(entry.OldValues),
			nullableJSON(entry.NewValues), nullableString(entry.Notes),
			nullableString(entry.CorrelationID), nullableString(entry.SourceIP))
	}
	query := `INSERT INTO Audit
		(EventDate, UserID, UserName, EventType, TableName, RecordID,
		 OldValues, NewValues, Notes, CorrelationID, SourceIP)
		VALUES ` + strings.Join(placeholders, ",")
	if _, err := exec.ExecContext(ctx, query, args...); err != nil {
		return mapStoreError("insert audit", err)
	}
	return nil
}

// InsertAudit writes one audit row outside a write transaction. Account
// administration is stored in SQLite, so its audit rows cannot share that
// transaction; the service treats a failure here as a warning, never a rollback.
func (s *Store) InsertAudit(ctx context.Context, entry domain.AuditEntry) error {
	return insertAuditRows(ctx, s.db, []domain.AuditEntry{entry})
}

// GetBatch reads one batch record.
func (s *Store) GetBatch(ctx context.Context, batchID int64) (domain.ImportBatch, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+importBatchColumns+`
		FROM ImportBatch WHERE ImportBatchID=?`, batchID)
	return scanImportBatch(row)
}

// ListBatches returns newest batches first, optionally before a known ID.
func (s *Store) ListBatches(ctx context.Context, limit int, beforeID int64) ([]domain.ImportBatch, error) {
	if limit < 1 {
		limit = 50
	}
	var rows *sql.Rows
	var err error
	if beforeID > 0 {
		rows, err = s.db.QueryContext(ctx, `SELECT `+importBatchColumns+`
			FROM ImportBatch WHERE ImportBatchID < ? ORDER BY ImportBatchID DESC LIMIT ?`, beforeID, limit)
	} else {
		rows, err = s.db.QueryContext(ctx, `SELECT `+importBatchColumns+`
			FROM ImportBatch ORDER BY ImportBatchID DESC LIMIT ?`, limit)
	}
	if err != nil {
		return nil, mapStoreError("list import batches", err)
	}
	defer rows.Close()
	batches := make([]domain.ImportBatch, 0, limit)
	for rows.Next() {
		batch, err := scanImportBatch(rows)
		if err != nil {
			return nil, err
		}
		batches = append(batches, batch)
	}
	if err := rows.Err(); err != nil {
		return nil, mapStoreError("read import batches", err)
	}
	return batches, nil
}

// FindDuplicateBatch returns the newest earlier Uploaded/Completed batch with
// the same SHA-256 hash, or nil when none exists.
func (s *Store) FindDuplicateBatch(ctx context.Context, sha string, excludeID int64) (*int64, error) {
	var id int64
	err := s.db.QueryRowContext(ctx, `SELECT ImportBatchID FROM ImportBatch
		WHERE FileSha256=? AND ImportBatchID<>? AND Status IN ('Uploaded','Completed')
		ORDER BY ImportBatchID DESC LIMIT 1`, sha, excludeID).Scan(&id)
	if errorsIsNoRows(err) {
		return nil, nil
	}
	if err != nil {
		return nil, mapStoreError("find duplicate batch", err)
	}
	return &id, nil
}

func scanImportBatch(scanner interface{ Scan(...any) error }) (domain.ImportBatch, error) {
	var (
		batch        domain.ImportBatch
		sha          sql.NullString
		sourceRows   sql.NullInt64
		acceptedRows sql.NullInt64
		ignoredRows  sql.NullInt64
		status       string
		failure      sql.NullString
		importedByID sql.NullInt64
		importedAt   time.Time
	)
	if err := scanner.Scan(&batch.ImportBatchID, &batch.FileName, &sha, &sourceRows,
		&acceptedRows, &ignoredRows, &status, &failure, &batch.ImportedBy, &importedByID, &importedAt); err != nil {
		return domain.ImportBatch{}, mapStoreError("scan import batch", err)
	}
	if sha.Valid {
		batch.FileSha256 = &sha.String
	}
	if sourceRows.Valid {
		value := int(sourceRows.Int64)
		batch.SourceRowCount = &value
	}
	if acceptedRows.Valid {
		value := int(acceptedRows.Int64)
		batch.AcceptedRowCount = &value
	}
	if ignoredRows.Valid {
		value := int(ignoredRows.Int64)
		batch.IgnoredRowCount = &value
	}
	batch.Status = domain.ImportStatus(status)
	if failure.Valid {
		batch.FailureReason = &failure.String
	}
	if importedByID.Valid {
		batch.ImportedByUserID = &importedByID.Int64
	}
	batch.ImportedAt = importedAt.UTC()
	return batch, nil
}

// ListLocations derives selector entries from LocationGroup. The selector is
// never built from LocationRaw, and all Hoard variants are one group.
func (s *Store) ListLocations(ctx context.Context, batchID int64) ([]domain.ImportLocation, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT LocationGroup, MIN(LocationSort) AS LocationSort,
		COUNT(*) AS RowCount, SUM(SelectedFg) AS SelectedCount
		FROM ImportList WHERE ImportBatchID=? AND `+unspecifiedExclusion+`
		GROUP BY LocationGroup ORDER BY LocationSort, LocationGroup`, batchID)
	if err != nil {
		return nil, mapStoreError("list import locations", err)
	}
	defer rows.Close()
	locations := make([]domain.ImportLocation, 0)
	for rows.Next() {
		var location domain.ImportLocation
		if err := rows.Scan(&location.LocationGroup, &location.LocationSort,
			&location.RowCount, &location.SelectedCount); err != nil {
			return nil, mapStoreError("scan import location", err)
		}
		locations = append(locations, location)
	}
	if err := rows.Err(); err != nil {
		return nil, mapStoreError("read import locations", err)
	}
	return locations, nil
}

// ListRows pages staged rows using LocationGroup as the only location filter.
func (s *Store) ListRows(ctx context.Context, batchID int64, filter domain.ImportRowFilter) (domain.ImportRowPage, error) {
	if filter.Page < 1 {
		filter.Page = 1
	}
	if filter.PageSize < 1 || filter.PageSize > 200 {
		filter.PageSize = 50
	}
	where := []string{"ImportBatchID = ?", unspecifiedExclusion}
	args := []any{batchID}
	if filter.LocationGroup != "" {
		where = append(where, "LocationGroup = ?")
		args = append(args, filter.LocationGroup)
	}
	// Group scope (IMP-06): a group the reviewer unchecked is hidden from the
	// grid and excluded from bulk selection. The "unselected" view sets
	// IgnoreGroups so rows in a hidden group remain reviewable.
	if !filter.IgnoreGroups && len(filter.ExcludeGroups) > 0 {
		placeholders := make([]string, 0, len(filter.ExcludeGroups))
		for _, group := range filter.ExcludeGroups {
			placeholders = append(placeholders, "?")
			args = append(args, group)
		}
		where = append(where, "LocationGroup NOT IN ("+strings.Join(placeholders, ",")+")")
	}
	switch filter.Selected {
	case "selected":
		where = append(where, "SelectedFg = 1")
	case "unselected":
		where = append(where, "SelectedFg = 0")
	}
	if filter.Search != "" {
		where = append(where, `Name LIKE ? ESCAPE '!'`)
		args = append(args, "%"+escapeLike(filter.Search)+"%")
	}
	predicate := strings.Join(where, " AND ")
	var total int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM ImportList WHERE `+predicate, args...).Scan(&total); err != nil {
		return domain.ImportRowPage{}, mapStoreError("count import rows", err)
	}
	queryArgs := append(append([]any(nil), args...), filter.PageSize, (filter.Page-1)*filter.PageSize)
	rows, err := s.db.QueryContext(ctx, `SELECT ImportID, ImportBatchID, SourceLine, ItemType,
		EQUID, Name, Quantity, LocationRaw, LocationGroup, LocationSort, NameSort,
		SelectedFg, WarningText FROM ImportList WHERE `+predicate+`
		ORDER BY LocationSort, NameSort, ImportID LIMIT ? OFFSET ?`, queryArgs...)
	if err != nil {
		return domain.ImportRowPage{}, mapStoreError("list import rows", err)
	}
	defer rows.Close()
	page := domain.ImportRowPage{Page: filter.Page, PageSize: filter.PageSize, Total: total}
	for rows.Next() {
		row, err := scanImportRow(rows)
		if err != nil {
			return domain.ImportRowPage{}, err
		}
		page.Rows = append(page.Rows, row)
	}
	if err := rows.Err(); err != nil {
		return domain.ImportRowPage{}, mapStoreError("read import rows", err)
	}
	return page, nil
}

// RowIDs returns every staged ID, optionally restricted to one LocationGroup
// and excluding the groups outside the review page's current scope (IMP-06).
func (s *Store) RowIDs(ctx context.Context, batchID int64, locationGroup string, excludeGroups []string) ([]int64, error) {
	query := `SELECT ImportID FROM ImportList WHERE ImportBatchID=? AND ` + unspecifiedExclusion
	args := []any{batchID}
	if locationGroup != "" {
		query += ` AND LocationGroup=?`
		args = append(args, locationGroup)
	}
	if len(excludeGroups) > 0 {
		placeholders := make([]string, 0, len(excludeGroups))
		for _, group := range excludeGroups {
			placeholders = append(placeholders, "?")
			args = append(args, group)
		}
		query += ` AND LocationGroup NOT IN (` + strings.Join(placeholders, ",") + `)`
	}
	query += ` ORDER BY ImportID`
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, mapStoreError("list import row ids", err)
	}
	defer rows.Close()
	ids := make([]int64, 0)
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, mapStoreError("scan import row id", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, mapStoreError("read import row ids", err)
	}
	return ids, nil
}

// SelectedIDs returns the full selected set for UI bootstrap. Rows in the
// Other/Unspecified group are never part of the import selection (IMP-C), even
// on a legacy batch that still has SelectedFg set on them.
func (s *Store) SelectedIDs(ctx context.Context, batchID int64) ([]int64, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT ImportID FROM ImportList
		WHERE ImportBatchID=? AND SelectedFg=1 AND `+unspecifiedExclusion+` ORDER BY ImportID`, batchID)
	if err != nil {
		return nil, mapStoreError("list selected rows", err)
	}
	defer rows.Close()
	ids := make([]int64, 0)
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, mapStoreError("scan selected row", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, mapStoreError("read selected rows", err)
	}
	return ids, nil
}

// AllRows returns every staged row; commit metadata resolution uses it.
func (s *Store) AllRows(ctx context.Context, batchID int64) ([]domain.ImportRow, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT ImportID, ImportBatchID, SourceLine, ItemType,
		EQUID, Name, Quantity, LocationRaw, LocationGroup, LocationSort, NameSort,
		SelectedFg, WarningText FROM ImportList WHERE ImportBatchID=?
		ORDER BY ImportID`, batchID)
	if err != nil {
		return nil, mapStoreError("list all import rows", err)
	}
	defer rows.Close()
	all := make([]domain.ImportRow, 0)
	for rows.Next() {
		row, err := scanImportRow(rows)
		if err != nil {
			return nil, err
		}
		all = append(all, row)
	}
	if err := rows.Err(); err != nil {
		return nil, mapStoreError("read all import rows", err)
	}
	return all, nil
}

func scanImportRow(scanner interface{ Scan(...any) error }) (domain.ImportRow, error) {
	var (
		row          domain.ImportRow
		sourceLine   sql.NullInt64
		itemType     string
		locationSort sql.NullString
		nameSort     sql.NullString
		selected     bool
		warningText  sql.NullString
	)
	if err := scanner.Scan(&row.ImportID, &row.ImportBatchID, &sourceLine, &itemType,
		&row.EQUID, &row.Name, &row.Quantity, &row.LocationRaw, &row.LocationGroup,
		&locationSort, &nameSort, &selected, &warningText); err != nil {
		return domain.ImportRow{}, mapStoreError("scan import row", err)
	}
	if sourceLine.Valid {
		value := int(sourceLine.Int64)
		row.SourceLine = &value
	}
	row.ItemType = domain.ItemType(itemType)
	row.LocationSort = locationSort.String
	row.NameSort = nameSort.String
	row.SelectedFg = selected
	if warningText.Valid {
		row.Warnings = service.ParseWarnings(warningText.String)
	}
	return row, nil
}

// ReplaceSelection atomically replaces the selected set for an Uploaded batch.
func (s *Store) ReplaceSelection(ctx context.Context, batchID int64, ids []int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return mapStoreError("begin selection", err)
	}
	defer func() { _ = tx.Rollback() }()
	var status string
	if err := tx.QueryRowContext(ctx, `SELECT Status FROM ImportBatch
		WHERE ImportBatchID=? FOR UPDATE`, batchID).Scan(&status); err != nil {
		return mapStoreError("lock import batch", err)
	}
	if domain.ImportStatus(status) != domain.ImportStatusUploaded {
		return fmt.Errorf("%w: batch is not editable", domain.ErrConflict)
	}
	if len(ids) > 0 {
		matched, selectable, err := countImportIDs(ctx, tx, batchID, ids)
		if err != nil {
			return err
		}
		if matched != len(ids) {
			return domain.NewValidationError("selectedImportIds", "one or more IDs do not belong to this batch")
		}
		if selectable != len(ids) {
			return domain.NewValidationError("selectedImportIds", "Other location rows cannot be selected for import")
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE ImportList SET SelectedFg=0 WHERE ImportBatchID=?`, batchID); err != nil {
		return mapStoreError("clear import selection", err)
	}
	for start := 0; start < len(ids); start += 1000 {
		end := min(start+1000, len(ids))
		args := []any{batchID}
		placeholders := make([]string, 0, end-start)
		for _, id := range ids[start:end] {
			args = append(args, id)
			placeholders = append(placeholders, "?")
		}
		query := `UPDATE ImportList SET SelectedFg=1 WHERE ImportBatchID=? AND ImportID IN (` +
			strings.Join(placeholders, ",") + `)`
		if _, err := tx.ExecContext(ctx, query, args...); err != nil {
			return mapStoreError("select import rows", err)
		}
	}
	var selected int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM ImportList
		WHERE ImportBatchID=? AND SelectedFg=1`, batchID).Scan(&selected); err != nil {
		return mapStoreError("count selected rows", err)
	}
	if selected != len(ids) {
		return fmt.Errorf("%w: selected row count did not match the requested set", domain.ErrConflict)
	}
	if err := tx.Commit(); err != nil {
		return mapStoreError("commit selection", err)
	}
	return nil
}

// countImportIDs reports how many of the supplied IDs belong to the batch and
// how many of those are selectable rows. Other/Unspecified rows are staged but
// are not part of the import review, so they are counted separately (IMP-C).
func countImportIDs(ctx context.Context, tx *sql.Tx, batchID int64, ids []int64) (matched, selectable int, err error) {
	for start := 0; start < len(ids); start += 1000 {
		end := min(start+1000, len(ids))
		args := []any{batchID}
		placeholders := make([]string, 0, end-start)
		for _, id := range ids[start:end] {
			args = append(args, id)
			placeholders = append(placeholders, "?")
		}
		var batchMatched, batchSelectable int
		query := `SELECT COUNT(*), COALESCE(SUM(` + unspecifiedExclusion + `),0)
			FROM ImportList WHERE ImportBatchID=? AND ImportID IN (` +
			strings.Join(placeholders, ",") + `)`
		if err := tx.QueryRowContext(ctx, query, args...).Scan(&batchMatched, &batchSelectable); err != nil {
			return 0, 0, mapStoreError("count batch row membership", err)
		}
		matched += batchMatched
		selectable += batchSelectable
	}
	return matched, selectable, nil
}

// SelectedTotals aggregates the persisted selection.
func (s *Store) SelectedTotals(ctx context.Context, batchID int64) (domain.ImportTotals, error) {
	var totals domain.ImportTotals
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) AS SelectedRows,
		COALESCE(SUM(ItemType='Item'),0) AS ItemRows,
		COALESCE(SUM(CASE WHEN ItemType='Item' THEN Quantity ELSE 0 END),0) AS ItemUnits,
		COALESCE(SUM(ItemType='Spell'),0) AS SpellRows,
		COALESCE(SUM(CASE WHEN ItemType='Spell' THEN Quantity ELSE 0 END),0) AS SpellQuantity,
		COUNT(DISTINCT CASE WHEN ItemType='Spell' THEN EQUID END) AS DistinctSpells,
		COALESCE(SUM(CASE WHEN WarningText IS NOT NULL AND WarningText <> ''
		                  AND WarningText <> '[]' THEN 1 ELSE 0 END),0) AS WarningRows
		FROM ImportList WHERE ImportBatchID=? AND SelectedFg=1 AND `+unspecifiedExclusion, batchID).Scan(
		&totals.SelectedRows, &totals.ItemRows, &totals.ItemUnits, &totals.SpellRows,
		&totals.SpellQuantity, &totals.DistinctSpells, &totals.WarningRows)
	if err != nil {
		return domain.ImportTotals{}, mapStoreError("read selected totals", err)
	}
	return totals, nil
}

// SpellPreview reports selected spell aggregates and active merge targets.
func (s *Store) SpellPreview(ctx context.Context, batchID int64) ([]domain.SpellPreview, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT EQUID, SUM(Quantity), COUNT(*)
		FROM ImportList WHERE ImportBatchID=? AND SelectedFg=1 AND ItemType='Spell'
		  AND `+unspecifiedExclusion+`
		GROUP BY EQUID ORDER BY EQUID`, batchID)
	if err != nil {
		return nil, mapStoreError("read spell preview", err)
	}
	previews := make([]domain.SpellPreview, 0)
	equids := make([]uint64, 0)
	for rows.Next() {
		var preview domain.SpellPreview
		if err := rows.Scan(&preview.EQUID, &preview.StagedQuantity, &preview.StagedRows); err != nil {
			_ = rows.Close()
			return nil, mapStoreError("scan spell preview", err)
		}
		previews = append(previews, preview)
		equids = append(equids, preview.EQUID)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, mapStoreError("read spell preview", err)
	}
	if err := rows.Close(); err != nil {
		return nil, mapStoreError("close spell preview", err)
	}
	for start := 0; start < len(equids); start += 1000 {
		end := min(start+1000, len(equids))
		if err := s.fillActiveSpellTotals(ctx, previews, equids[start:end]); err != nil {
			return nil, err
		}
	}
	return previews, nil
}

func (s *Store) fillActiveSpellTotals(ctx context.Context, previews []domain.SpellPreview, equids []uint64) error {
	args := make([]any, 0, len(equids))
	placeholders := make([]string, 0, len(equids))
	for _, equid := range equids {
		args = append(args, equid)
		placeholders = append(placeholders, "?")
	}
	rows, err := s.db.QueryContext(ctx, `SELECT GuildBankItemID, EQUID, Quantity
		FROM GuildBankItems WHERE DeletedFg=0 AND ItemType='Spell' AND EQUID IN (`+
		strings.Join(placeholders, ",")+`) ORDER BY GuildBankItemID`, args...)
	if err != nil {
		return mapStoreError("read active spells", err)
	}
	defer rows.Close()
	byEQUID := make(map[uint64]*domain.SpellPreview, len(previews))
	for i := range previews {
		byEQUID[previews[i].EQUID] = &previews[i]
	}
	for rows.Next() {
		var id int64
		var equid, quantity uint64
		if err := rows.Scan(&id, &equid, &quantity); err != nil {
			return mapStoreError("scan active spell", err)
		}
		preview, ok := byEQUID[equid]
		if !ok {
			continue
		}
		preview.ActiveRecords++
		preview.ActiveQuantity += quantity
		if preview.MergeTargetID == nil || id < *preview.MergeTargetID {
			value := id
			preview.MergeTargetID = &value
		}
	}
	if err := rows.Err(); err != nil {
		return mapStoreError("read active spell rows", err)
	}
	return nil
}

// RecoverInterrupted marks stale Processing batches Failed after a restart.
func (s *Store) RecoverInterrupted(ctx context.Context) (int, error) {
	cutoff := time.Now().UTC().Add(-15 * time.Minute)
	reason := "interrupted before completion"
	result, err := s.db.ExecContext(ctx, `UPDATE ImportBatch
		SET Status='Failed', FailureReason=?
		WHERE Status='Processing' AND ImportedAt < ?`, reason, cutoff)
	if err != nil {
		return 0, mapStoreError("recover interrupted imports", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("mariadb: recovery count: %w", err)
	}
	return int(count), nil
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func nullableString(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func nullableInt64(value int64) any {
	if value == 0 {
		return nil
	}
	return value
}

func nullableJSON(value []byte) any {
	if len(value) == 0 || string(value) == "null" {
		return nil
	}
	return string(value)
}

func escapeLike(value string) string {
	value = strings.ReplaceAll(value, "!", "!!")
	value = strings.ReplaceAll(value, "%", "!%")
	value = strings.ReplaceAll(value, "_", "!_")
	return value
}

func errorsIsNoRows(err error) bool { return err == sql.ErrNoRows }
