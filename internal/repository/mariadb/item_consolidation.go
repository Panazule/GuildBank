package mariadb

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"time"

	"github.com/Panazule/guildbank/internal/domain"
)

const consolidationBatchSize = 100

// ConsolidationResult reports one run of the item-stack consolidation command.
// DryRun results are estimates from the grouping query; a real run reports the
// rows it actually changed.
type ConsolidationResult struct {
	DryRun         bool
	Groups         int
	AbsorbedRows   int
	UnitsInGroups  uint64
	UnitsAbsorbed  uint64
	UnitsRemaining uint64
}

// ConsolidateItemStacks implements QTY-09 Option C. Active ordinary item rows
// that share the complete provenance of one imported line — EQUID,
// SourceLocation, LocationGroup, GivenTo, DateAdded, DateOpen and DateBazaar —
// are folded into the lowest GuildBankItemID. The survivor's quantity becomes
// the group sum; every absorbed row is soft deleted, points at the survivor
// through ConsolidatedInto, and receives an ITEM_REMOVED audit row. The
// survivor receives one ITEM_UPDATED audit row in the same transaction.
//
// The command is idempotent (a second run finds no group with more than one
// active row), processes keyset batches, and holds the same per-database lock
// as import commit and admin writes so a stack cannot change mid-group.
func (s *Store) ConsolidateItemStacks(ctx context.Context, batchSize int, dryRun bool,
	actor domain.Actor) (ConsolidationResult, error) {
	if batchSize < 1 {
		batchSize = consolidationBatchSize
	}
	if actor.UserName == "" {
		actor.UserName = "system"
	}
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return ConsolidationResult{}, mapStoreError("open consolidation connection", err)
	}
	var acquired sql.NullInt64
	if err := conn.QueryRowContext(ctx,
		`SELECT GET_LOCK(CONCAT('`+commitLockName+`', LEFT(SHA2(DATABASE(), 256), 32)), 300)`).
		Scan(&acquired); err != nil {
		_ = conn.Close()
		return ConsolidationResult{}, mapStoreError("acquire consolidation lock", err)
	}
	if !acquired.Valid || acquired.Int64 != 1 {
		_ = conn.Close()
		return ConsolidationResult{}, fmt.Errorf("%w: consolidation lock timeout", domain.ErrUnavailable)
	}
	defer func() {
		releaseCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, _ = conn.ExecContext(releaseCtx,
			`SELECT RELEASE_LOCK(CONCAT('`+commitLockName+`', LEFT(SHA2(DATABASE(), 256), 32)))`)
		_ = conn.Close()
	}()

	result := ConsolidationResult{DryRun: dryRun}
	cursor := int64(0)
	for {
		groups, err := consolidationGroups(ctx, conn, cursor, batchSize)
		if err != nil {
			return result, err
		}
		if len(groups) == 0 {
			return result, nil
		}
		for _, group := range groups {
			result.Groups++
			result.UnitsInGroups += group.quantity
			result.UnitsRemaining += group.quantity
			if dryRun {
				result.AbsorbedRows += group.rows - 1
				cursor = group.survivorID
				continue
			}
			absorbed, moved, err := s.consolidateGroup(ctx, conn, group, actor)
			if err != nil {
				return result, err
			}
			result.AbsorbedRows += absorbed
			result.UnitsAbsorbed += moved
			cursor = group.survivorID
		}
		if len(groups) < batchSize {
			return result, nil
		}
	}
}

type consolidationGroup struct {
	survivorID       int64
	rows             int
	quantity         uint64
	survivorQuantity uint64
	equid            uint64
	sourceLocation   sql.NullString
	locationGroup    sql.NullString
	givenTo          sql.NullString
	dateAdded        time.Time
	dateOpen         time.Time
	dateBazaar       time.Time
}

// consolidationGroups returns active duplicate stacks ordered by their lowest
// row ID. MIN(GuildBankItemID) is the survivor, matching the "lowest ID wins"
// rule used by the spell merge.
//
// Rows holding the unresolved-identity placeholder EQUID are excluded: they
// share one identifier by definition while representing different items, so
// grouping them would merge unrelated stock. Their stored name is authoritative.
func consolidationGroups(ctx context.Context, conn *sql.Conn, cursor int64,
	limit int) ([]consolidationGroup, error) {
	rows, err := conn.QueryContext(ctx, `SELECT MIN(GuildBankItemID), COUNT(*), SUM(Quantity),
			EQUID, SourceLocation, LocationGroup, GivenTo, DateAdded, DateOpen, DateBazaar
		FROM GuildBankItems
		WHERE ItemType='Item' AND DeletedFg=0 AND GuildBankItemID > ? AND EQUID <> ?
		GROUP BY EQUID, SourceLocation, LocationGroup, GivenTo, DateAdded, DateOpen, DateBazaar
		HAVING COUNT(*) > 1
		ORDER BY MIN(GuildBankItemID)
		LIMIT ?`, cursor, domain.UnresolvedEQUID, limit)
	if err != nil {
		return nil, mapStoreError("read consolidation groups", err)
	}
	defer rows.Close()
	groups := make([]consolidationGroup, 0, limit)
	for rows.Next() {
		var group consolidationGroup
		var count, quantity int64
		if err := rows.Scan(&group.survivorID, &count, &quantity, &group.equid,
			&group.sourceLocation, &group.locationGroup, &group.givenTo,
			&group.dateAdded, &group.dateOpen, &group.dateBazaar); err != nil {
			return nil, mapStoreError("scan consolidation group", err)
		}
		if count < 2 || quantity < 1 {
			return nil, fmt.Errorf("mariadb: malformed consolidation group at row %d", group.survivorID)
		}
		group.rows = int(count)
		group.quantity = uint64(quantity)
		groups = append(groups, group)
	}
	if err := rows.Err(); err != nil {
		return nil, mapStoreError("read consolidation groups", err)
	}
	return groups, nil
}

// consolidateGroup locks one group, re-reads it, writes the survivor update,
// the absorbed soft deletes and every audit row in one transaction.
func (s *Store) consolidateGroup(ctx context.Context, conn *sql.Conn,
	group consolidationGroup, actor domain.Actor) (absorbed int, moved uint64, err error) {
	members, err := lockConsolidationGroup(ctx, conn, group)
	if err != nil {
		return 0, 0, err
	}
	if len(members) < 2 {
		// Idempotency guard: a concurrent run already folded this group.
		return 0, 0, nil
	}
	group.rows = len(members)
	group.quantity = 0
	group.survivorQuantity = 0
	for _, member := range members {
		group.quantity += uint64(member.quantity)
		if member.id == group.survivorID {
			group.survivorQuantity = uint64(member.quantity)
		}
	}
	if group.quantity > math.MaxUint32 {
		return 0, 0, fmt.Errorf("mariadb: consolidation group at row %d sums to %d, above the quantity range",
			group.survivorID, group.quantity)
	}

	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return 0, 0, mapStoreError("begin consolidation group", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	at := time.Now().UTC()
	result, err := tx.ExecContext(ctx, `UPDATE GuildBankItems
		SET Quantity=?, ModAt=?, ModBy=?, ModByUserID=?
		WHERE GuildBankItemID=? AND ItemType='Item' AND DeletedFg=0`,
		group.quantity, at, actor.UserName, nullableInt64(actor.UserID), group.survivorID)
	if err != nil {
		return 0, 0, mapStoreError("update consolidation survivor", err)
	}
	if affected, err := result.RowsAffected(); err != nil || affected != 1 {
		return 0, 0, fmt.Errorf("%w: consolidation survivor changed", domain.ErrConflict)
	}

	absorbedIDs := make([]int64, 0, len(members)-1)
	for _, member := range members {
		if member.id == group.survivorID {
			continue
		}
		result, err := tx.ExecContext(ctx, `UPDATE GuildBankItems
			SET DeletedFg=1, ConsolidatedInto=?, ModAt=?, ModBy=?, ModByUserID=?
			WHERE GuildBankItemID=? AND ItemType='Item' AND DeletedFg=0`,
			group.survivorID, at, actor.UserName, nullableInt64(actor.UserID), member.id)
		if err != nil {
			return 0, 0, mapStoreError("absorb consolidation row", err)
		}
		if affected, err := result.RowsAffected(); err != nil || affected != 1 {
			return 0, 0, fmt.Errorf("%w: consolidation row %d changed", domain.ErrConflict, member.id)
		}
		absorbedIDs = append(absorbedIDs, member.id)
		moved += uint64(member.quantity)
	}

	entries, err := consolidationAudit(group, members, absorbedIDs, actor, at)
	if err != nil {
		return 0, 0, err
	}
	if err := insertAudit(ctx, tx, entries); err != nil {
		return 0, 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, 0, mapStoreError("commit consolidation group", err)
	}
	committed = true
	return len(absorbedIDs), moved, nil
}

type consolidationMember struct {
	id       int64
	quantity uint32
}

func lockConsolidationGroup(ctx context.Context, conn *sql.Conn,
	group consolidationGroup) ([]consolidationMember, error) {
	rows, err := conn.QueryContext(ctx, `SELECT GuildBankItemID, Quantity
		FROM GuildBankItems
		WHERE ItemType='Item' AND DeletedFg=0 AND EQUID=?
		  AND SourceLocation <=> ? AND LocationGroup <=> ? AND GivenTo <=> ?
		  AND DateAdded=? AND DateOpen=? AND DateBazaar=?
		ORDER BY GuildBankItemID FOR UPDATE`,
		group.equid, group.sourceLocation, group.locationGroup, group.givenTo,
		group.dateAdded, group.dateOpen, group.dateBazaar)
	if err != nil {
		return nil, mapStoreError("lock consolidation group", err)
	}
	defer rows.Close()
	members := make([]consolidationMember, 0, group.rows)
	for rows.Next() {
		var member consolidationMember
		if err := rows.Scan(&member.id, &member.quantity); err != nil {
			return nil, mapStoreError("scan consolidation member", err)
		}
		members = append(members, member)
	}
	if err := rows.Err(); err != nil {
		return nil, mapStoreError("read consolidation members", err)
	}
	return members, nil
}

func consolidationAudit(group consolidationGroup, members []consolidationMember,
	absorbed []int64, actor domain.Actor, at time.Time) ([]domain.AuditEntry, error) {
	survivorOld, err := json.Marshal(map[string]any{
		"quantity": group.survivorQuantity, "consolidatedFrom": len(members),
	})
	if err != nil {
		return nil, err
	}
	survivorNew, err := json.Marshal(map[string]any{
		"quantity": group.quantity, "absorbedIds": absorbed,
		"modAt": at, "modBy": actor.UserName, "modByUserId": actor.UserID,
	})
	if err != nil {
		return nil, err
	}
	entries := []domain.AuditEntry{{
		EventDate: at, UserID: actor.UserID, UserName: actor.UserName,
		EventType: "ITEM_UPDATED", TableName: "GuildBankItems",
		RecordID: fmt.Sprintf("%d", group.survivorID), OldValues: survivorOld,
		NewValues: survivorNew,
		Notes:     fmt.Sprintf("quantity consolidated from %d rows", len(members)),
	}}
	byID := make(map[int64]uint32, len(members))
	for _, member := range members {
		byID[member.id] = member.quantity
	}
	for _, id := range absorbed {
		oldValues, err := json.Marshal(map[string]any{"quantity": byID[id], "deletedFg": false})
		if err != nil {
			return nil, err
		}
		newValues, err := json.Marshal(map[string]any{
			"deletedFg": true, "consolidatedInto": group.survivorID,
		})
		if err != nil {
			return nil, err
		}
		entries = append(entries, domain.AuditEntry{
			EventDate: at, UserID: actor.UserID, UserName: actor.UserName,
			EventType: "ITEM_REMOVED", TableName: "GuildBankItems",
			RecordID: fmt.Sprintf("%d", id), OldValues: oldValues,
			NewValues: newValues, Notes: fmt.Sprintf("consolidated into %d", group.survivorID),
		})
	}
	return entries, nil
}
