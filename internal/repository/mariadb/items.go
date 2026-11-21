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
const itemColumns = `id, name, classes, races`

// GetItemByEQUID reads one item by its EQUID. The source table is read-only
// for this application; no write method exists here.
func (s *Store) GetItemByEQUID(ctx context.Context, equid uint64) (domain.Item, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT `+itemColumns+` FROM items WHERE id = ?`, equid)
	return scanItem(row)
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
