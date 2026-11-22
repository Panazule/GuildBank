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

// adminInventoryColumns is the explicit administrative projection. It adds the
// persisted location, recipient, attribution and soft-delete columns that the
// public projection deliberately omits.
const adminInventoryColumns = `GuildBankItemID, EQUID, Name, ItemType, Quantity,
	ClassString, RaceString, MetadataMissingFg, SourceLocation, LocationGroup,
	LocationSort, GivenTo, DeletedFg, NameSort, DateAdded, DateOpen, DateBazaar,
	ModAt, ModBy, ModByUserID`

// ListAdminInventory reads one page and its total from one read-only snapshot.
func (s *Store) ListAdminInventory(ctx context.Context, filter domain.AdminInventoryFilter) (domain.AdminInventoryPage, error) {
	where, args := adminInventoryWhere(filter)
	order, ok := adminInventoryOrder(filter.Sort)
	if !ok {
		return domain.AdminInventoryPage{}, domain.NewValidationError("sort", "sort must be one of the approved values")
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return domain.AdminInventoryPage{}, mapStoreError("begin admin inventory read", err)
	}
	defer func() { _ = tx.Rollback() }()

	var total int
	if err := tx.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM GuildBankItems WHERE `+where, args...).Scan(&total); err != nil {
		return domain.AdminInventoryPage{}, mapStoreError("count admin inventory", err)
	}
	page := domain.AdminInventoryPage{
		Records: []domain.AdminInventoryItem{}, Total: total,
		Page: filter.Page, PageSize: filter.PageSize, Filter: filter,
	}
	if total > 0 {
		page.TotalPages = (total + filter.PageSize - 1) / filter.PageSize
	}
	queryArgs := append(append([]any(nil), args...), filter.PageSize, (filter.Page-1)*filter.PageSize)
	rows, err := tx.QueryContext(ctx,
		`SELECT `+adminInventoryColumns+` FROM GuildBankItems WHERE `+where+
			` ORDER BY `+order+` LIMIT ? OFFSET ?`, queryArgs...)
	if err != nil {
		return domain.AdminInventoryPage{}, mapStoreError("list admin inventory", err)
	}
	for rows.Next() {
		record, err := scanAdminInventory(rows)
		if err != nil {
			_ = rows.Close()
			return domain.AdminInventoryPage{}, err
		}
		page.Records = append(page.Records, record)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return domain.AdminInventoryPage{}, mapStoreError("read admin inventory", err)
	}
	if err := rows.Close(); err != nil {
		return domain.AdminInventoryPage{}, mapStoreError("close admin inventory", err)
	}
	if err := tx.Commit(); err != nil {
		return domain.AdminInventoryPage{}, mapStoreError("commit admin inventory read", err)
	}
	return page, nil
}

// GetAdminInventory reads one row by primary key, including removed rows.
func (s *Store) GetAdminInventory(ctx context.Context, id int64) (domain.AdminInventoryItem, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT `+adminInventoryColumns+` FROM GuildBankItems WHERE GuildBankItemID = ?`, id)
	return scanAdminInventory(row)
}

// ListLocationGroups returns the distinct persisted groups in natural order.
// The 200-row bound is a selector display limit, not a filter constraint.
func (s *Store) ListLocationGroups(ctx context.Context) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT LocationGroup FROM GuildBankItems
		WHERE LocationGroup IS NOT NULL
		GROUP BY LocationGroup ORDER BY MIN(LocationSort), LocationGroup LIMIT 200`)
	if err != nil {
		return nil, mapStoreError("list location groups", err)
	}
	defer rows.Close()
	groups := make([]string, 0)
	for rows.Next() {
		var group string
		if err := rows.Scan(&group); err != nil {
			return nil, mapStoreError("scan location group", err)
		}
		groups = append(groups, group)
	}
	if err := rows.Err(); err != nil {
		return nil, mapStoreError("read location groups", err)
	}
	return groups, nil
}

func adminInventoryWhere(filter domain.AdminInventoryFilter) (string, []any) {
	where := make([]string, 0, 8)
	args := make([]any, 0, 8)
	switch filter.Status {
	case domain.AdminStatusActive:
		where = append(where, "DeletedFg = 0")
	case domain.AdminStatusRemoved:
		where = append(where, "DeletedFg = 1")
	}
	if filter.ID > 0 {
		where = append(where, "GuildBankItemID = ?")
		args = append(args, filter.ID)
	}
	if filter.EQUID > 0 {
		where = append(where, "EQUID = ?")
		args = append(args, filter.EQUID)
	}
	if filter.Search != "" {
		where = append(where, `Name LIKE ? ESCAPE '!'`)
		args = append(args, "%"+escapeLike(filter.Search)+"%")
	}
	if filter.GivenTo != "" {
		where = append(where, "GivenTo = ?")
		args = append(args, filter.GivenTo)
	}
	switch filter.Metadata {
	case domain.AdminMetadataMissing:
		where = append(where, "MetadataMissingFg = 1")
	case domain.AdminMetadataPresent:
		where = append(where, "MetadataMissingFg = 0")
	}
	if filter.LocationGroup != "" {
		where = append(where, "LocationGroup = ?")
		args = append(args, filter.LocationGroup)
	}
	if filter.ItemType != "" {
		where = append(where, "ItemType = ?")
		args = append(args, string(filter.ItemType))
	}
	if len(where) == 0 {
		return "1 = 1", args
	}
	return strings.Join(where, " AND "), args
}

func adminInventoryOrder(sort domain.AdminInventorySort) (string, bool) {
	switch sort {
	case domain.AdminSortModifiedNewest, "":
		return "ModAt DESC, GuildBankItemID DESC", true
	case domain.AdminSortModifiedOldest:
		return "ModAt ASC, GuildBankItemID ASC", true
	case domain.AdminSortName:
		return "NameSort ASC, GuildBankItemID ASC", true
	case domain.AdminSortNameDesc:
		return "NameSort DESC, GuildBankItemID DESC", true
	case domain.AdminSortLocation:
		return "LocationSort ASC, NameSort ASC, GuildBankItemID ASC", true
	case domain.AdminSortQuantity:
		return "Quantity DESC, NameSort ASC, GuildBankItemID ASC", true
	case domain.AdminSortDateAdded:
		return "DateAdded ASC, GuildBankItemID ASC", true
	case domain.AdminSortDateAddedDesc:
		return "DateAdded DESC, GuildBankItemID DESC", true
	case domain.AdminSortDateOpen:
		return "DateOpen ASC, GuildBankItemID ASC", true
	case domain.AdminSortDateOpenDesc:
		return "DateOpen DESC, GuildBankItemID DESC", true
	case domain.AdminSortDateBazaar:
		return "DateBazaar ASC, GuildBankItemID ASC", true
	case domain.AdminSortDateBazaarDesc:
		return "DateBazaar DESC, GuildBankItemID DESC", true
	default:
		return "", false
	}
}

func scanAdminInventory(scanner interface{ Scan(...any) error }) (domain.AdminInventoryItem, error) {
	var (
		item          domain.AdminInventoryItem
		equid         int64
		itemType      string
		quantity      int64
		sourceLoc     sql.NullString
		locationGroup sql.NullString
		locationSort  sql.NullString
		givenTo       sql.NullString
		nameSort      sql.NullString
		modByUserID   sql.NullInt64
	)
	if err := scanner.Scan(&item.ID, &equid, &item.Name, &itemType, &quantity,
		&item.ClassString, &item.RaceString, &item.MetadataMissingFg,
		&sourceLoc, &locationGroup, &locationSort, &givenTo, &item.DeletedFg,
		&nameSort, &item.DateAdded, &item.DateOpen, &item.DateBazaar,
		&item.ModAt, &item.ModBy, &modByUserID); err != nil {
		if errorsIsNoRows(err) {
			return domain.AdminInventoryItem{}, domain.ErrNotFound
		}
		return domain.AdminInventoryItem{}, mapStoreError("scan admin inventory row", err)
	}
	if item.ID <= 0 || equid < 0 || quantity < 1 {
		return domain.AdminInventoryItem{}, fmt.Errorf("mariadb: malformed admin inventory row %d", item.ID)
	}
	if itemType != string(domain.ItemTypeItem) && itemType != string(domain.ItemTypeSpell) {
		return domain.AdminInventoryItem{}, fmt.Errorf("mariadb: admin inventory row %d has invalid item type", item.ID)
	}
	item.EQUID = uint64(equid)
	item.ItemType = domain.ItemType(itemType)
	item.Quantity = uint32(quantity)
	item.SourceLocation = sourceLoc.String
	item.LocationGroup = locationGroup.String
	item.LocationSort = locationSort.String
	item.GivenTo = givenTo.String
	item.NameSort = nameSort.String
	item.DateAdded = utcCalendarDate(item.DateAdded)
	item.DateOpen = utcCalendarDate(item.DateOpen)
	item.DateBazaar = utcCalendarDate(item.DateBazaar)
	item.ModAt = item.ModAt.UTC()
	if modByUserID.Valid {
		value := modByUserID.Int64
		item.ModByUserID = &value
	}
	return item, nil
}

// adminWriteTx is the transaction-bound admin write surface. It owns the
// dedicated connection, the advisory lock and the transaction.
type adminWriteTx struct {
	ctx      context.Context
	conn     *sql.Conn
	tx       *sql.Tx
	finished bool
}

// BeginAdminWrite acquires the import commit advisory lock on a dedicated
// connection and opens the write transaction. Import commit and admin writes
// therefore serialize.
func (s *Store) BeginAdminWrite(ctx context.Context) (service.AdminWriteTx, error) {
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return nil, mapStoreError("open admin write connection", err)
	}
	var acquired sql.NullInt64
	if err := conn.QueryRowContext(ctx,
		`SELECT GET_LOCK(CONCAT('`+commitLockName+`', LEFT(SHA2(DATABASE(), 256), 32)), 30)`).
		Scan(&acquired); err != nil {
		_ = conn.Close()
		return nil, mapStoreError("acquire admin write lock", err)
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
		return nil, mapStoreError("begin admin write", err)
	}
	return &adminWriteTx{ctx: ctx, conn: conn, tx: tx}, nil
}

func (c *adminWriteTx) LockInventory(ctx context.Context, id int64) (domain.AdminInventoryItem, error) {
	row := c.tx.QueryRowContext(ctx,
		`SELECT `+adminInventoryColumns+` FROM GuildBankItems
		WHERE GuildBankItemID = ? FOR UPDATE`, id)
	return scanAdminInventory(row)
}

func (c *adminWriteTx) FindActiveSpells(ctx context.Context, equid uint64) ([]domain.AdminInventoryItem, error) {
	rows, err := c.tx.QueryContext(ctx,
		`SELECT `+adminInventoryColumns+` FROM GuildBankItems
		WHERE DeletedFg = 0 AND ItemType = 'Spell' AND EQUID = ?
		ORDER BY GuildBankItemID FOR UPDATE`, equid)
	if err != nil {
		return nil, mapStoreError("lock active spells", err)
	}
	defer rows.Close()
	items := make([]domain.AdminInventoryItem, 0, 1)
	for rows.Next() {
		item, err := scanAdminInventory(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, mapStoreError("read active spells", err)
	}
	return items, nil
}

func (c *adminWriteTx) InsertInventory(ctx context.Context, rows []domain.GuildBankItem) ([]int64, error) {
	return insertInventoryRows(ctx, c.tx, rows)
}

func (c *adminWriteTx) AddSpellQuantity(ctx context.Context, id int64, qty uint32, at time.Time, actor domain.Actor) error {
	result, err := c.tx.ExecContext(ctx, `UPDATE GuildBankItems
		SET Quantity=Quantity+?, ModAt=?, ModBy=?, ModByUserID=?
		WHERE GuildBankItemID=? AND DeletedFg=0 AND ItemType='Spell'`,
		qty, at.UTC(), actor.UserName, nullableInt64(actor.UserID), id)
	if err != nil {
		return mapStoreError("merge spell quantity", err)
	}
	affected, err := result.RowsAffected()
	if err != nil || affected != 1 {
		return fmt.Errorf("%w: spell merge target changed", domain.ErrConflict)
	}
	return nil
}

func (c *adminWriteTx) UpdateInventory(ctx context.Context, in domain.AdminUpdateRow) error {
	var quantity any
	if in.Quantity != nil {
		quantity = *in.Quantity
	}
	var dateOpen, dateBazaar any
	if in.DateOpen != nil {
		dateOpen = in.DateOpen.UTC()
	}
	if in.DateBazaar != nil {
		dateBazaar = in.DateBazaar.UTC()
	}
	result, err := c.tx.ExecContext(ctx, `UPDATE GuildBankItems SET
			Quantity = COALESCE(?, Quantity),
			SourceLocation = ?, LocationGroup = ?, LocationSort = ?, GivenTo = ?,
			DateOpen = COALESCE(?, DateOpen), DateBazaar = COALESCE(?, DateBazaar),
			ModAt = ?, ModBy = ?, ModByUserID = ?
		WHERE GuildBankItemID = ? AND DeletedFg = 0`,
		quantity, nullableString(in.SourceLocation), nullableString(in.LocationGroup),
		nullableString(in.LocationSort), nullableString(in.GivenTo), dateOpen, dateBazaar,
		in.ModAt.UTC(), in.Actor.UserName, nullableInt64(in.Actor.UserID), in.ID)
	if err != nil {
		return mapStoreError("update admin inventory row", err)
	}
	affected, err := result.RowsAffected()
	if err != nil || affected != 1 {
		return fmt.Errorf("%w: inventory row changed", domain.ErrConflict)
	}
	return nil
}

func (c *adminWriteTx) SetDeleted(ctx context.Context, id int64, deleted bool, at time.Time, actor domain.Actor) error {
	result, err := c.tx.ExecContext(ctx, `UPDATE GuildBankItems
		SET DeletedFg=?, ModAt=?, ModBy=?, ModByUserID=?
		WHERE GuildBankItemID=? AND DeletedFg=?`,
		boolInt(deleted), at.UTC(), actor.UserName, nullableInt64(actor.UserID),
		id, boolInt(!deleted))
	if err != nil {
		return mapStoreError("set inventory deleted flag", err)
	}
	affected, err := result.RowsAffected()
	if err != nil || affected != 1 {
		return fmt.Errorf("%w: inventory state changed", domain.ErrConflict)
	}
	return nil
}

func (c *adminWriteTx) InsertAudit(ctx context.Context, entry domain.AuditEntry) error {
	return insertAudit(ctx, c.tx, []domain.AuditEntry{entry})
}

func (c *adminWriteTx) Commit() error {
	if c.finished {
		return nil
	}
	if err := c.tx.Commit(); err != nil {
		c.finish(true)
		return mapStoreError("commit admin write", err)
	}
	c.finish(false)
	return nil
}

func (c *adminWriteTx) Rollback() error {
	if c.finished {
		return nil
	}
	err := c.tx.Rollback()
	c.finish(false)
	if err != nil && err != sql.ErrTxDone {
		return mapStoreError("rollback admin write", err)
	}
	return nil
}

func (c *adminWriteTx) finish(rollback bool) {
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
