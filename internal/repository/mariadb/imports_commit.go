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

const commitLockName = "guildbank:commit:"

type importCommitTx struct {
	store    *Store
	ctx      context.Context
	conn     *sql.Conn
	tx       *sql.Tx
	batch    domain.ImportBatch
	finished bool
}

// BeginCommit acquires the per-database advisory lock on a dedicated
// connection, opens the transaction and locks the batch row.
func (s *Store) BeginCommit(ctx context.Context, batchID int64) (service.ImportCommitTx, error) {
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return nil, mapStoreError("open commit connection", err)
	}
	var acquired sql.NullInt64
	if err := conn.QueryRowContext(ctx,
		`SELECT GET_LOCK(CONCAT('`+commitLockName+`', LEFT(SHA2(DATABASE(), 256), 32)), 30)`).
		Scan(&acquired); err != nil {
		_ = conn.Close()
		return nil, mapStoreError("acquire commit lock", err)
	}
	if !acquired.Valid || acquired.Int64 != 1 {
		releaseCommitLock(conn)
		_ = conn.Close()
		return nil, fmt.Errorf("%w: commit lock timeout", domain.ErrUnavailable)
	}

	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		releaseCommitLock(conn)
		_ = conn.Close()
		return nil, mapStoreError("begin commit", err)
	}
	commit := &importCommitTx{store: s, ctx: ctx, conn: conn, tx: tx}
	row := tx.QueryRowContext(ctx, `SELECT `+importBatchColumns+`
		FROM ImportBatch WHERE ImportBatchID=? FOR UPDATE`, batchID)
	batch, err := scanImportBatch(row)
	if err != nil {
		commit.finish(true)
		return nil, err
	}
	if batch.Status != domain.ImportStatusUploaded {
		commit.finish(true)
		return nil, fmt.Errorf("%w: batch is not editable", domain.ErrConflict)
	}
	result, err := tx.ExecContext(ctx, `UPDATE ImportBatch SET Status='Processing'
		WHERE ImportBatchID=? AND Status='Uploaded'`, batchID)
	if err != nil {
		commit.finish(true)
		return nil, mapStoreError("mark batch processing", err)
	}
	affected, err := result.RowsAffected()
	if err != nil || affected != 1 {
		commit.finish(true)
		return nil, fmt.Errorf("%w: batch is not editable", domain.ErrConflict)
	}
	commit.batch = batch
	return commit, nil
}

func (c *importCommitTx) Batch() domain.ImportBatch { return c.batch }

func (c *importCommitTx) SelectedRows() ([]domain.ImportRow, error) {
	rows, err := c.tx.QueryContext(c.ctx, `SELECT ImportID, ImportBatchID,
		SourceLine, ItemType, EQUID, Name, Quantity, LocationRaw, LocationGroup,
		LocationSort, NameSort, SelectedFg, WarningText FROM ImportList
		WHERE ImportBatchID=? AND SelectedFg=1 AND `+unspecifiedExclusion+`
		ORDER BY ImportID`, c.batch.ImportBatchID)
	if err != nil {
		return nil, mapStoreError("read committed rows", err)
	}
	defer rows.Close()
	selected := make([]domain.ImportRow, 0)
	for rows.Next() {
		row, err := scanImportRow(rows)
		if err != nil {
			return nil, err
		}
		selected = append(selected, row)
	}
	if err := rows.Err(); err != nil {
		return nil, mapStoreError("read committed rows", err)
	}
	return selected, nil
}

func (c *importCommitTx) ActiveSpells(equids []uint64) ([]domain.GuildBankItem, error) {
	active := make([]domain.GuildBankItem, 0)
	for start := 0; start < len(equids); start += 1000 {
		end := min(start+1000, len(equids))
		args := make([]any, 0, end-start)
		placeholders := make([]string, 0, end-start)
		for _, equid := range equids[start:end] {
			args = append(args, equid)
			placeholders = append(placeholders, "?")
		}
		rows, err := c.tx.QueryContext(c.ctx, `SELECT GuildBankItemID,
			ItemType, EQUID, Quantity, SourceLocation, Name
			FROM GuildBankItems WHERE DeletedFg=0 AND ItemType='Spell'
			AND EQUID IN (`+strings.Join(placeholders, ",")+`)
			ORDER BY GuildBankItemID FOR UPDATE`, args...)
		if err != nil {
			return nil, mapStoreError("lock active spells", err)
		}
		for rows.Next() {
			var item domain.GuildBankItem
			var itemType string
			var location sql.NullString
			if err := rows.Scan(&item.GuildBankItemID, &itemType, &item.EQUID,
				&item.Quantity, &location, &item.Name); err != nil {
				_ = rows.Close()
				return nil, mapStoreError("scan active spell", err)
			}
			item.ItemType = domain.ItemType(itemType)
			item.SourceLocation = location.String
			active = append(active, item)
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return nil, mapStoreError("read active spell locks", err)
		}
		_ = rows.Close()
	}
	return active, nil
}

func (c *importCommitTx) InsertItems(rows []domain.GuildBankItem) ([]int64, error) {
	return c.insertInventory(rows)
}

func (c *importCommitTx) InsertSpells(rows []domain.GuildBankItem) ([]int64, error) {
	return c.insertInventory(rows)
}

func (c *importCommitTx) insertInventory(rows []domain.GuildBankItem) ([]int64, error) {
	return insertInventoryRows(c.ctx, c.tx, rows)
}

// execer is the subset of *sql.Tx and *sql.Conn used by the shared row insert.
type execer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

// insertInventoryRows is the single inventory INSERT used by import commit and
// manual admin creation. Both callers share this 19-column list so the two
// paths cannot drift; callers keep their own transaction and locking.
func insertInventoryRows(ctx context.Context, tx execer, rows []domain.GuildBankItem) ([]int64, error) {
	ids := make([]int64, 0, len(rows))
	for start := 0; start < len(rows); start += importChunkSize {
		end := min(start+importChunkSize, len(rows))
		chunk := rows[start:end]
		placeholders := make([]string, 0, len(chunk))
		args := make([]any, 0, len(chunk)*19)
		for _, row := range chunk {
			placeholders = append(placeholders, "(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)")
			args = append(args, string(row.ItemType), row.EQUID, boolInt(row.MetadataMissingFg),
				row.Name, row.ClassString, row.RaceString, row.DateAdded, row.DateOpen,
				row.DateBazaar, row.Quantity, nullableString(row.SourceLocation),
				nullableString(row.LocationGroup), nullableString(row.LocationSort),
				nullableString(row.GivenTo), boolInt(row.DeletedFg), row.ModAt.UTC(),
				row.ModBy, row.ModByUserID, row.NameSort)
		}
		query := `INSERT INTO GuildBankItems
			(ItemType, EQUID, MetadataMissingFg, Name, ClassString, RaceString,
			 DateAdded, DateOpen, DateBazaar, Quantity, SourceLocation, LocationGroup,
			 LocationSort, GivenTo, DeletedFg, ModAt, ModBy, ModByUserID, NameSort)
			VALUES ` + strings.Join(placeholders, ",")
		result, err := tx.ExecContext(ctx, query, args...)
		if err != nil {
			return nil, mapStoreError("insert inventory rows", err)
		}
		first, err := result.LastInsertId()
		if err != nil {
			return nil, fmt.Errorf("mariadb: inserted inventory id: %w", err)
		}
		for i := range chunk {
			ids = append(ids, first+int64(i))
		}
	}
	return ids, nil
}

func (c *importCommitTx) AddSpellQuantity(id int64, qty uint32, at time.Time, actorName string, actorID int64) error {
	result, err := c.tx.ExecContext(c.ctx, `UPDATE GuildBankItems
		SET Quantity=Quantity+?, ModAt=?, ModBy=?, ModByUserID=?
		WHERE GuildBankItemID=? AND DeletedFg=0 AND ItemType='Spell'`,
		qty, at.UTC(), actorName, nullableInt64(actorID), id)
	if err != nil {
		return mapStoreError("merge spell quantity", err)
	}
	affected, err := result.RowsAffected()
	if err != nil || affected != 1 {
		return fmt.Errorf("%w: spell merge target changed", domain.ErrConflict)
	}
	return nil
}

func (c *importCommitTx) ReplaceRowWarnings(warnings map[int64][]domain.ImportWarning) error {
	for id, values := range warnings {
		body, err := service.MarshalWarnings(values)
		if err != nil {
			return err
		}
		var arg any
		if len(body) > 0 {
			arg = string(body)
		}
		if _, err := c.tx.ExecContext(c.ctx,
			`UPDATE ImportList SET WarningText=? WHERE ImportID=?`, arg, id); err != nil {
			return mapStoreError("replace row warnings", err)
		}
	}
	return nil
}

func (c *importCommitTx) WriteAudit(entries []domain.AuditEntry) error {
	return insertAudit(c.ctx, c.tx, entries)
}

func (c *importCommitTx) MarkCompleted() error {
	_, err := c.tx.ExecContext(c.ctx,
		`UPDATE ImportBatch SET Status='Completed' WHERE ImportBatchID=? AND Status='Processing'`,
		c.batch.ImportBatchID)
	if err != nil {
		return mapStoreError("mark batch completed", err)
	}
	return nil
}

func (c *importCommitTx) Commit() error {
	if c.finished {
		return nil
	}
	if err := c.tx.Commit(); err != nil {
		c.finish(true)
		return mapStoreError("commit import transaction", err)
	}
	c.finish(false)
	return nil
}

func (c *importCommitTx) Rollback() error {
	if c.finished {
		return nil
	}
	err := c.tx.Rollback()
	c.finish(false)
	if err != nil && err != sql.ErrTxDone {
		return mapStoreError("rollback import transaction", err)
	}
	return nil
}

func (c *importCommitTx) finish(rollback bool) {
	if c.finished {
		return
	}
	c.finished = true
	if rollback {
		_ = c.tx.Rollback()
	}
	releaseCommitLock(c.conn)
	_ = c.conn.Close()
}

func releaseCommitLock(conn *sql.Conn) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, _ = conn.ExecContext(ctx,
		`SELECT RELEASE_LOCK(CONCAT('`+commitLockName+`', LEFT(SHA2(DATABASE(), 256), 32)))`)
}
