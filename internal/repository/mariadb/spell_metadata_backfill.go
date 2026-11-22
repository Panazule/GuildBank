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

const (
	spellMetadataBackfillLockName = "guildbank:spell-metadata-backfill:"
	spellMetadataBackfillBatch    = 500
)

// BackfillSpellMetadata repairs spell rows created before the DEFECT-01 commit
// fix: those rows resolve their Items metadata but never persisted the decoded
// ClassString/RaceString, so the UI renders them as Unknown. The backfill joins
// the authoritative Items row by EQUID, decodes with the same class/race
// decoders used at commit time, and fills only the blank columns.
//
// It is restartable and idempotent: a rerun resumes after the last scanned row,
// never overwrites an existing non-blank string, never touches Name, ModAt,
// ModBy or audit data, and holds a dedicated advisory lock so two replicas
// cannot race. It returns the number of rows it updated.
func (s *Store) BackfillSpellMetadata(ctx context.Context, batchSize int) (int, error) {
	if batchSize < 1 {
		batchSize = spellMetadataBackfillBatch
	}
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return 0, mapStoreError("open spell metadata backfill connection", err)
	}
	var acquired sql.NullInt64
	if err := conn.QueryRowContext(ctx,
		`SELECT GET_LOCK(CONCAT('`+spellMetadataBackfillLockName+`', LEFT(SHA2(DATABASE(), 256), 32)), 300)`).
		Scan(&acquired); err != nil {
		_ = conn.Close()
		return 0, mapStoreError("acquire spell metadata backfill lock", err)
	}
	if !acquired.Valid || acquired.Int64 != 1 {
		_ = conn.Close()
		return 0, fmt.Errorf("%w: spell metadata backfill lock timeout", domain.ErrUnavailable)
	}
	defer func() {
		releaseCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, _ = conn.ExecContext(releaseCtx,
			`SELECT RELEASE_LOCK(CONCAT('`+spellMetadataBackfillLockName+`', LEFT(SHA2(DATABASE(), 256), 32)))`)
		_ = conn.Close()
	}()

	classDecoder := service.NewClassMaskDecoder()
	raceDecoder := service.NewRaceMaskDecoder()
	updated := 0
	cursor := int64(0)
	for {
		rows, err := conn.QueryContext(ctx, `SELECT g.GuildBankItemID, g.ClassString,
				g.RaceString, i.classes, i.races
			FROM GuildBankItems g
			JOIN items i ON i.id = g.EQUID
			WHERE g.ItemType = 'Spell'
			  AND g.MetadataMissingFg = 0
			  AND (g.ClassString IS NULL OR g.ClassString = ''
			       OR g.RaceString IS NULL OR g.RaceString = '')
			  AND g.GuildBankItemID > ?
			ORDER BY g.GuildBankItemID
			LIMIT ?`, cursor, batchSize)
		if err != nil {
			return updated, mapStoreError("read spell metadata backfill batch", err)
		}
		type pending struct {
			id          int64
			classString string
			raceString  string
		}
		batch := make([]pending, 0, batchSize)
		scanned := 0
		lastID := cursor
		for rows.Next() {
			var id int64
			var currentClass, currentRace, classes, races sql.NullString
			if err := rows.Scan(&id, &currentClass, &currentRace, &classes, &races); err != nil {
				_ = rows.Close()
				return updated, mapStoreError("scan spell metadata backfill row", err)
			}
			scanned++
			lastID = id
			classString, raceString, changed, err := repairedSpellMetadata(
				currentClass.String, currentRace.String, classes, races, classDecoder, raceDecoder)
			if err != nil {
				_ = rows.Close()
				return updated, fmt.Errorf("mariadb: spell %d: %w", id, err)
			}
			if changed {
				batch = append(batch, pending{id: id, classString: classString, raceString: raceString})
			}
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return updated, mapStoreError("read spell metadata backfill batch", err)
		}
		if err := rows.Close(); err != nil {
			return updated, mapStoreError("close spell metadata backfill batch", err)
		}
		if len(batch) > 0 {
			tx, err := conn.BeginTx(ctx, nil)
			if err != nil {
				return updated, mapStoreError("begin spell metadata backfill batch", err)
			}
			for _, row := range batch {
				if _, err := tx.ExecContext(ctx, `UPDATE GuildBankItems
					SET ClassString = ?, RaceString = ?
					WHERE GuildBankItemID = ? AND ItemType = 'Spell'`,
					row.classString, row.raceString, row.id); err != nil {
					_ = tx.Rollback()
					return updated, mapStoreError("update spell metadata backfill row", err)
				}
				updated++
			}
			if err := tx.Commit(); err != nil {
				return updated, mapStoreError("commit spell metadata backfill batch", err)
			}
		}
		if scanned == 0 || scanned < batchSize {
			return updated, nil
		}
		cursor = lastID
	}
}

// repairedSpellMetadata decodes the Items masks and returns the strings to
// store. An already populated column is never overwritten, and a row whose
// masks decode to nothing is left untouched so the backfill cannot blank data.
func repairedSpellMetadata(currentClass, currentRace string, classes, races sql.NullString,
	classDecoder *service.ClassMaskDecoder, raceDecoder *service.RaceMaskDecoder) (classString, raceString string, changed bool, err error) {
	classMask, err := parseMaskColumn("classes", classes)
	if err != nil {
		return "", "", false, err
	}
	raceMask, err := parseMaskColumn("races", races)
	if err != nil {
		return "", "", false, err
	}
	currentClass = strings.TrimSpace(currentClass)
	currentRace = strings.TrimSpace(currentRace)
	classString, raceString = currentClass, currentRace
	if classString == "" {
		classString = classDecoder.Decode(classMask).Value
	}
	if raceString == "" {
		raceString = raceDecoder.Decode(raceMask).Value
	}
	if classString == "" && raceString == "" {
		return currentClass, currentRace, false, nil
	}
	return classString, raceString,
		classString != currentClass || raceString != currentRace, nil
}
