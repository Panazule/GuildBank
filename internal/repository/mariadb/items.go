package mariadb

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/Panazule/guildbank/internal/domain"
)

// The physical Items table is externally owned and lowercase in the
// provisioned development database (lower_case_table_names=0). This file is
// the only place that knows its physical table and column names; the rest of
// the application consumes domain.Item (PD-19).
const (
	itemColumns = `id, name, classes, races`
	// itemBatchSize keeps an IN predicate below driver and server limits.
	itemBatchSize = 500
	// ItemsPageSize is the default page size for a keyset scan.
	ItemsPageSize = 500
	// MaxItemsPageSize bounds a single read so callers cannot exhaust memory.
	MaxItemsPageSize = 5000
)

// GetItemByEQUID reads one item by its EQUID. The source table is read-only
// for this application; no write method exists here.
func (s *Store) GetItemByEQUID(ctx context.Context, equid uint64) (domain.Item, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT `+itemColumns+` FROM items WHERE id = ?`, equid)
	return scanItem(row)
}

// GetItemsByEQUIDs reads the items present for the supplied identifiers. The
// result is keyed by EQUID and omits identifiers absent from Items, which lets
// callers apply the AM-01/AM-02 missing-metadata rule without treating an
// absent row as an infrastructure error. Duplicate identifiers are read once.
func (s *Store) GetItemsByEQUIDs(ctx context.Context, equids []uint64) (map[uint64]domain.Item, error) {
	result := make(map[uint64]domain.Item)
	ids := uniqueEQUIDs(equids)
	for start := 0; start < len(ids); start += itemBatchSize {
		end := min(start+itemBatchSize, len(ids))
		if err := s.readItemBatch(ctx, ids[start:end], result); err != nil {
			return nil, err
		}
	}
	return result, nil
}

func (s *Store) readItemBatch(ctx context.Context, ids []uint64, result map[uint64]domain.Item) error {
	args := make([]any, len(ids))
	placeholders := make([]byte, 0, len(ids)*2-1)
	for i, id := range ids {
		args[i] = id
		if i > 0 {
			placeholders = append(placeholders, ',')
		}
		placeholders = append(placeholders, '?')
	}
	query := `SELECT ` + itemColumns + ` FROM items WHERE id IN (` + string(placeholders) + `) ORDER BY id`
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("mariadb: list items by EQUID: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		item, err := scanItem(rows)
		if err != nil {
			return err
		}
		result[item.EQUID] = item
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("mariadb: read item rows: %w", err)
	}
	return nil
}

// CountItems returns the number of source metadata rows. It exists for
// read-only diagnostics and full-dataset integration checks.
func (s *Store) CountItems(ctx context.Context) (int64, error) {
	var count int64
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM items`).Scan(&count); err != nil {
		return 0, fmt.Errorf("mariadb: count items: %w", err)
	}
	return count, nil
}

// ListItems returns a deterministic keyset page ordered by EQUID. Callers pass
// the final EQUID from the previous page as afterEQUID; zero starts at the
// beginning. The physical table remains read-only.
func (s *Store) ListItems(ctx context.Context, afterEQUID uint64, limit int) ([]domain.Item, error) {
	if limit < 1 || limit > MaxItemsPageSize {
		return nil, fmt.Errorf("mariadb: item page size %d is outside 1-%d", limit, MaxItemsPageSize)
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+itemColumns+` FROM items WHERE id > ? ORDER BY id LIMIT ?`,
		afterEQUID, limit)
	if err != nil {
		return nil, fmt.Errorf("mariadb: list items: %w", err)
	}
	defer rows.Close()

	items := make([]domain.Item, 0, limit)
	for rows.Next() {
		item, err := scanItem(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("mariadb: read item page: %w", err)
	}
	return items, nil
}

func uniqueEQUIDs(equids []uint64) []uint64 {
	seen := make(map[uint64]struct{}, len(equids))
	ids := make([]uint64, 0, len(equids))
	for _, id := range equids {
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	return ids
}

// scanItem maps the approved physical source columns to the logical Item
// contract:
//
//	items.id      -> EQUID
//	items.name    -> Name
//	items.classes -> ClassList
//	items.races   -> RaceList
func scanItem(scanner interface{ Scan(...any) error }) (domain.Item, error) {
	var (
		id      int64
		name    sql.NullString
		classes sql.NullString
		races   sql.NullString
	)
	if err := scanner.Scan(&id, &name, &classes, &races); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.Item{}, domain.ErrNotFound
		}
		return domain.Item{}, fmt.Errorf("mariadb: scan item: %w", err)
	}
	return itemFromValues(id, name, classes, races)
}

func itemFromValues(id int64, name, classes, races sql.NullString) (domain.Item, error) {
	if id < 0 {
		return domain.Item{}, fmt.Errorf("mariadb: item id %d is negative", id)
	}
	if !name.Valid || name.String == "" {
		return domain.Item{}, fmt.Errorf("mariadb: item %d has no name", id)
	}
	classMask, err := parseMaskColumn("classes", classes)
	if err != nil {
		return domain.Item{}, fmt.Errorf("mariadb: item %d: %w", id, err)
	}
	raceMask, err := parseMaskColumn("races", races)
	if err != nil {
		return domain.Item{}, fmt.Errorf("mariadb: item %d: %w", id, err)
	}
	return domain.Item{
		EQUID:     uint64(id),
		Name:      name.String,
		ClassList: classMask,
		RaceList:  raceMask,
	}, nil
}

// parseMaskColumn converts the source's LONGTEXT decimal representation into
// the logical numeric mask. A blank/NULL value is treated as zero; the decoder
// then produces a blank logical string as approved by PD-20.
func parseMaskColumn(field string, value sql.NullString) (uint64, error) {
	if !value.Valid {
		return 0, nil
	}
	raw := strings.TrimSpace(value.String)
	if raw == "" {
		return 0, nil
	}
	mask, err := strconv.ParseUint(raw, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("parse %s mask %q: %w", field, raw, err)
	}
	return mask, nil
}
