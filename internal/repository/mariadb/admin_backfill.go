package mariadb

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/Panazule/guildbank/internal/domain"
	"github.com/Panazule/guildbank/internal/service"
)

const (
	backfillLockName  = "guildbank:location-backfill:"
	backfillBatchSize = 500
)

// BackfillInventoryLocations fills LocationGroup/LocationSort for rows that
// still have a NULL derived column. It is restartable and idempotent: a rerun
// resumes at the first remaining null row, never touches SourceLocation,
// ModAt, ModBy or audit data, and holds a dedicated advisory lock so two
// replicas cannot race. It returns the number of rows it updated.
func (s *Store) BackfillInventoryLocations(ctx context.Context, batchSize int) (int, error) {
	if batchSize < 1 {
		batchSize = backfillBatchSize
	}
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return 0, mapStoreError("open backfill connection", err)
	}
	var acquired sql.NullInt64
	if err := conn.QueryRowContext(ctx,
		`SELECT GET_LOCK(CONCAT('`+backfillLockName+`', LEFT(SHA2(DATABASE(), 256), 32)), 300)`).
		Scan(&acquired); err != nil {
		_ = conn.Close()
		return 0, mapStoreError("acquire location backfill lock", err)
	}
	if !acquired.Valid || acquired.Int64 != 1 {
		_ = conn.Close()
		return 0, fmt.Errorf("%w: location backfill lock timeout", domain.ErrUnavailable)
	}
	defer func() {
		releaseCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, _ = conn.ExecContext(releaseCtx,
			`SELECT RELEASE_LOCK(CONCAT('`+backfillLockName+`', LEFT(SHA2(DATABASE(), 256), 32)))`)
		_ = conn.Close()
	}()

	updated := 0
	cursor := int64(0)
	for {
		rows, err := conn.QueryContext(ctx, `SELECT GuildBankItemID, SourceLocation
			FROM GuildBankItems
			WHERE GuildBankItemID > ?
			  AND (LocationGroup IS NULL OR LocationSort IS NULL)
			ORDER BY GuildBankItemID
			LIMIT ?`, cursor, batchSize)
		if err != nil {
			return updated, mapStoreError("read backfill batch", err)
		}
		type pending struct {
			id       int64
			group    string
			sort     string
			location string
		}
		batch := make([]pending, 0, batchSize)
		for rows.Next() {
			var id int64
			var source sql.NullString
			if err := rows.Scan(&id, &source); err != nil {
				_ = rows.Close()
				return updated, mapStoreError("scan backfill row", err)
			}
			normalized := service.NormalizeLocation(source.String)
			batch = append(batch, pending{
				id: id, group: normalized.Group,
				sort:     service.TruncateRunes(normalized.Sort, 80),
				location: source.String,
			})
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return updated, mapStoreError("read backfill batch", err)
		}
		if err := rows.Close(); err != nil {
			return updated, mapStoreError("close backfill batch", err)
		}
		if len(batch) == 0 {
			return updated, nil
		}

		tx, err := conn.BeginTx(ctx, nil)
		if err != nil {
			return updated, mapStoreError("begin backfill batch", err)
		}
		for _, row := range batch {
			if _, err := tx.ExecContext(ctx, `UPDATE GuildBankItems
				SET LocationGroup = ?, LocationSort = ?
				WHERE GuildBankItemID = ?
				  AND (LocationGroup IS NULL OR LocationSort IS NULL)`,
				row.group, row.sort, row.id); err != nil {
				_ = tx.Rollback()
				return updated, mapStoreError("update backfill row", err)
			}
			updated++
		}
		if err := tx.Commit(); err != nil {
			return updated, mapStoreError("commit backfill batch", err)
		}
		cursor = batch[len(batch)-1].id
	}
}
