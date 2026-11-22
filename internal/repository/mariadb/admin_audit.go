package mariadb

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"strings"

	"github.com/Panazule/guildbank/internal/domain"
)

const adminAuditColumns = `AuditID, EventDate, UserID, UserName, EventType,
	TableName, RecordID, OldValues, NewValues, Notes, CorrelationID, SourceIP`

// ListAudit reads one page of audit entries and its total from one read-only
// snapshot. Every predicate is parameterized; the order is fixed.
func (s *Store) ListAudit(ctx context.Context, filter domain.AuditFilter) (domain.AuditPage, error) {
	where, args := auditWhere(filter)
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return domain.AuditPage{}, mapStoreError("begin audit read", err)
	}
	defer func() { _ = tx.Rollback() }()

	var total int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM Audit WHERE `+where, args...).Scan(&total); err != nil {
		return domain.AuditPage{}, mapStoreError("count audit entries", err)
	}
	page := domain.AuditPage{
		Entries: []domain.AuditEntry{}, Total: total,
		Page: filter.Page, PageSize: filter.PageSize, Filter: filter,
	}
	if total > 0 {
		page.TotalPages = (total + filter.PageSize - 1) / filter.PageSize
	}
	queryArgs := append(append([]any(nil), args...), filter.PageSize, (filter.Page-1)*filter.PageSize)
	rows, err := tx.QueryContext(ctx,
		`SELECT `+adminAuditColumns+` FROM Audit WHERE `+where+
			` ORDER BY EventDate DESC, AuditID DESC LIMIT ? OFFSET ?`, queryArgs...)
	if err != nil {
		return domain.AuditPage{}, mapStoreError("list audit entries", err)
	}
	for rows.Next() {
		entry, err := scanAuditEntry(rows)
		if err != nil {
			_ = rows.Close()
			return domain.AuditPage{}, err
		}
		page.Entries = append(page.Entries, entry)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return domain.AuditPage{}, mapStoreError("read audit entries", err)
	}
	if err := rows.Close(); err != nil {
		return domain.AuditPage{}, mapStoreError("close audit entries", err)
	}
	if err := tx.Commit(); err != nil {
		return domain.AuditPage{}, mapStoreError("commit audit read", err)
	}
	return page, nil
}

// GetAudit reads one audit entry by primary key.
func (s *Store) GetAudit(ctx context.Context, id int64) (domain.AuditEntry, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT `+adminAuditColumns+` FROM Audit WHERE AuditID = ?`, id)
	return scanAuditEntry(row)
}

func auditWhere(filter domain.AuditFilter) (string, []any) {
	where := make([]string, 0, 7)
	args := make([]any, 0, 7)
	if filter.EventType != "" {
		where = append(where, "EventType = ?")
		args = append(args, filter.EventType)
	}
	if filter.User != "" {
		where = append(where, "UserName = ?")
		args = append(args, filter.User)
	}
	if filter.Table != "" {
		where = append(where, "TableName = ?")
		args = append(args, filter.Table)
	}
	if filter.RecordID != "" {
		where = append(where, "RecordID = ?")
		args = append(args, filter.RecordID)
	}
	if filter.CorrelationID != "" {
		where = append(where, "CorrelationID = ?")
		args = append(args, filter.CorrelationID)
	}
	if filter.From != nil {
		where = append(where, "EventDate >= ?")
		args = append(args, *filter.From)
	}
	if filter.To != nil {
		where = append(where, "EventDate < ?")
		args = append(args, filter.To.AddDate(0, 0, 1))
	}
	if len(where) == 0 {
		return "1 = 1", args
	}
	return strings.Join(where, " AND "), args
}

func scanAuditEntry(scanner interface{ Scan(...any) error }) (domain.AuditEntry, error) {
	var (
		entry   domain.AuditEntry
		auditID uint64
		userID  sql.NullInt64
		table   sql.NullString
		record  sql.NullString
		oldJSON []byte
		newJSON []byte
		notes   sql.NullString
		corr    sql.NullString
		source  sql.NullString
	)
	if err := scanner.Scan(&auditID, &entry.EventDate, &userID, &entry.UserName,
		&entry.EventType, &table, &record, &oldJSON, &newJSON, &notes, &corr, &source); err != nil {
		if errorsIsNoRows(err) {
			return domain.AuditEntry{}, domain.ErrNotFound
		}
		return domain.AuditEntry{}, mapStoreError("scan audit entry", err)
	}
	if auditID > math.MaxInt64 {
		return domain.AuditEntry{}, fmt.Errorf("mariadb: audit id %d exceeds the signed range", auditID)
	}
	entry.AuditID = int64(auditID)
	entry.EventDate = entry.EventDate.UTC()
	if userID.Valid {
		entry.UserID = userID.Int64
	}
	entry.TableName = table.String
	entry.RecordID = record.String
	entry.OldValues = json.RawMessage(oldJSON)
	entry.NewValues = json.RawMessage(newJSON)
	entry.Notes = notes.String
	entry.CorrelationID = corr.String
	entry.SourceIP = source.String
	return entry, nil
}
