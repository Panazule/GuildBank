package mariadb

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/Panazule/guildbank/internal/domain"
)

const publicInventoryColumns = `GuildBankItemID, EQUID, Name, ItemType, Quantity,
	ClassString, RaceString, MetadataMissingFg, DateAdded, DateOpen, DateBazaar`

// ListPublicInventory reads a bounded page and its total from one
// repeatable-read, read-only snapshot. Predicates are parameterized and the
// order expression comes only from an enum switch.
func (s *Store) ListPublicInventory(ctx context.Context, filter domain.InventoryFilter) (domain.InventoryRecordPage, error) {
	where, args := publicInventoryWhere(filter)
	order, ok := publicInventoryOrder(filter.Sort)
	if !ok {
		return domain.InventoryRecordPage{}, domain.NewValidationError("sort", "sort must be one of the approved values")
	}

	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return domain.InventoryRecordPage{}, mapStoreError("begin public inventory read", err)
	}
	defer func() { _ = tx.Rollback() }()

	records, err := queryPublicInventoryPage(ctx, tx, where, args, order, filter)
	if err != nil {
		return domain.InventoryRecordPage{}, err
	}
	// Page 1 with no rows proves the filtered snapshot is empty, so an exact
	// count would repeat a full leading-wildcard scan for no benefit.
	if filter.Page == 1 && len(records) == 0 {
		if err := tx.Commit(); err != nil {
			return domain.InventoryRecordPage{}, mapStoreError("commit public inventory read", err)
		}
		return domain.InventoryRecordPage{Records: []domain.InventoryRecord{}, Total: 0}, nil
	}

	var total, nullSorts int
	if err := tx.QueryRowContext(ctx,
		`SELECT COUNT(*), COALESCE(SUM(NameSort IS NULL), 0) FROM GuildBankItems WHERE `+where,
		args...).Scan(&total, &nullSorts); err != nil {
		return domain.InventoryRecordPage{}, mapStoreError("count public inventory", err)
	}
	nullOrder := publicInventoryOrderWithNulls(filter.Sort, order, nullSorts > 0)
	if nullOrder != order {
		records, err = queryPublicInventoryPage(ctx, tx, where, args, nullOrder, filter)
		if err != nil {
			return domain.InventoryRecordPage{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return domain.InventoryRecordPage{}, mapStoreError("commit public inventory read", err)
	}
	if records == nil {
		records = []domain.InventoryRecord{}
	}
	return domain.InventoryRecordPage{Records: records, Total: total}, nil
}

func queryPublicInventoryPage(ctx context.Context, tx *sql.Tx, where string, args []any, order string, filter domain.InventoryFilter) ([]domain.InventoryRecord, error) {
	queryArgs := append(append([]any(nil), args...), filter.PageSize, (filter.Page-1)*filter.PageSize)
	rows, err := tx.QueryContext(ctx,
		`SELECT `+publicInventoryColumns+` FROM GuildBankItems WHERE `+where+
			` ORDER BY `+order+` LIMIT ? OFFSET ?`, queryArgs...)
	if err != nil {
		return nil, mapStoreError("list public inventory", err)
	}
	records := make([]domain.InventoryRecord, 0, filter.PageSize)
	for rows.Next() {
		record, err := scanPublicInventoryRecord(rows)
		if err != nil {
			_ = rows.Close()
			return nil, err
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, mapStoreError("read public inventory", err)
	}
	if err := rows.Close(); err != nil {
		return nil, mapStoreError("close public inventory", err)
	}
	return records, nil
}

// GetPublicInventory reads one active public row by GuildBankItemID.
func (s *Store) GetPublicInventory(ctx context.Context, id int64) (domain.InventoryRecord, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT `+publicInventoryColumns+` FROM GuildBankItems
		WHERE GuildBankItemID = ? AND DeletedFg = 0`, id)
	return scanPublicInventoryRecord(row)
}

func publicInventoryWhere(filter domain.InventoryFilter) (string, []any) {
	where := []string{"DeletedFg = 0"}
	args := make([]any, 0, 4)
	if filter.Search != "" {
		where = append(where, `Name LIKE ? ESCAPE '!'`)
		args = append(args, "%"+escapeLike(filter.Search)+"%")
	}
	if filter.Class != "" {
		where = append(where, `(ClassString = 'ALL' OR FIND_IN_SET(?, ClassString) > 0)`)
		args = append(args, filter.Class)
	}
	if filter.Race != "" {
		where = append(where, `(RaceString = 'ALL' OR FIND_IN_SET(?, RaceString) > 0)`)
		args = append(args, filter.Race)
	}
	if filter.ItemType != "" {
		where = append(where, "ItemType = ?")
		args = append(args, string(filter.ItemType))
	}
	return strings.Join(where, " AND "), args
}

func publicInventoryOrder(sort domain.InventorySort) (string, bool) {
	switch sort {
	case domain.InventorySortName, "":
		return "NameSort ASC, GuildBankItemID ASC", true
	case domain.InventorySortNameDesc:
		return "NameSort DESC, GuildBankItemID DESC", true
	case domain.InventorySortDateAdded:
		return "DateAdded ASC, GuildBankItemID ASC", true
	case domain.InventorySortDateAddedDesc:
		return "DateAdded DESC, GuildBankItemID DESC", true
	case domain.InventorySortDateOpen:
		return "DateOpen ASC, GuildBankItemID ASC", true
	case domain.InventorySortDateOpenDesc:
		return "DateOpen DESC, GuildBankItemID DESC", true
	case domain.InventorySortDateBazaar:
		return "DateBazaar ASC, GuildBankItemID ASC", true
	case domain.InventorySortDateBazaarDesc:
		return "DateBazaar DESC, GuildBankItemID DESC", true
	default:
		return "", false
	}
}

// publicInventoryOrderWithNulls preserves the approved nulls-last behavior only
// when legacy null keys exist in the filtered snapshot. Production rows always
// carry a NameSort key, so the normal path stays on the sort index instead of
// forcing a filesort for the IS NULL ordering expression.
func publicInventoryOrderWithNulls(sort domain.InventorySort, order string, hasNulls bool) string {
	if !hasNulls {
		return order
	}
	switch sort {
	case domain.InventorySortName, domain.InventorySortNameDesc, "":
		return "NameSort IS NULL ASC, " + order
	default:
		return order
	}
}

func scanPublicInventoryRecord(scanner interface{ Scan(...any) error }) (domain.InventoryRecord, error) {
	var (
		record     domain.InventoryRecord
		equid      int64
		itemType   string
		quantity   int64
		dateAdded  time.Time
		dateOpen   time.Time
		dateBazaar time.Time
	)
	if err := scanner.Scan(&record.GuildBankItemID, &equid, &record.Name, &itemType,
		&quantity, &record.ClassString, &record.RaceString, &record.MetadataMissing,
		&dateAdded, &dateOpen, &dateBazaar); err != nil {
		return domain.InventoryRecord{}, mapStoreError("scan public inventory row", err)
	}
	if record.GuildBankItemID <= 0 || equid < 0 || quantity < 1 {
		return domain.InventoryRecord{}, fmt.Errorf("mariadb: malformed public inventory row %d", record.GuildBankItemID)
	}
	if itemType != string(domain.ItemTypeItem) && itemType != string(domain.ItemTypeSpell) {
		return domain.InventoryRecord{}, fmt.Errorf("mariadb: public inventory row %d has invalid item type", record.GuildBankItemID)
	}
	record.EQUID = uint64(equid)
	record.ItemType = domain.ItemType(itemType)
	record.Quantity = uint32(quantity)
	record.DateAdded = utcCalendarDate(dateAdded)
	record.DateOpen = utcCalendarDate(dateOpen)
	record.DateBazaar = utcCalendarDate(dateBazaar)
	return record, nil
}

func utcCalendarDate(value time.Time) time.Time {
	value = value.UTC()
	return time.Date(value.Year(), value.Month(), value.Day(), 0, 0, 0, 0, time.UTC)
}
