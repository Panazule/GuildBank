package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
	"time"

	"github.com/Panazule/guildbank/internal/domain"
)

type mergePlan struct {
	target   domain.GuildBankItem
	quantity uint32
	row      domain.ImportRow
	oldQty   uint32
}

type commitPlan struct {
	items            []domain.GuildBankItem
	newSpells        []domain.GuildBankItem
	merges           []mergePlan
	duplicates       []domain.DuplicateSpellTarget
	warnings         []domain.ImportWarning
	spellQuantity    uint64
	itemCount        int
	itemUnits        uint64
	newSpellCount    int
	duplicateRecords int
}

// Commit atomically applies the persisted selection. A deadlock is retried
// once with the same captured timestamp.
func (s *ImportService) Commit(ctx context.Context, batchID int64, actor domain.Actor, correlationID, sourceIP string) (domain.CommitResult, error) {
	if err := s.requireReady(); err != nil {
		return domain.CommitResult{}, err
	}
	if err := validateActor(actor); err != nil {
		return domain.CommitResult{}, err
	}
	batch, err := s.store.GetBatch(ctx, batchID)
	if err != nil {
		return domain.CommitResult{}, err
	}
	if batch.Status != domain.ImportStatusUploaded {
		return domain.CommitResult{}, fmt.Errorf("%w: batch is not editable", domain.ErrConflict)
	}
	selected, err := s.store.SelectedIDs(ctx, batchID)
	if err != nil {
		return domain.CommitResult{}, err
	}
	if len(selected) == 0 {
		return domain.CommitResult{}, ErrSelectionEmpty
	}
	allRows, err := s.store.AllRows(ctx, batchID)
	if err != nil {
		return domain.CommitResult{}, err
	}
	metadata, err := s.resolveCommitMetadata(ctx, allRows)
	if err != nil {
		return domain.CommitResult{}, err
	}
	openDays, bazaarDays, err := s.commitDateOffsets(ctx)
	if err != nil {
		return domain.CommitResult{}, err
	}
	now := s.now().UTC()
	start := time.Now()
	attempt := func() (domain.CommitResult, error) {
		return s.commitAttempt(ctx, batchID, actor, metadata, openDays, bazaarDays, now,
			correlationID, sourceIP, time.Since(start))
	}
	result, err := attempt()
	var retry *domain.RetryableError
	if err != nil && errors.As(err, &retry) {
		s.logger.Warn("import commit deadlock; retrying once", "batch_id", batchID)
		result, err = attempt()
	}
	return result, err
}

func (s *ImportService) resolveCommitMetadata(ctx context.Context, rows []domain.ImportRow) (map[uint64]ItemMetadata, error) {
	seen := make(map[uint64]bool)
	refs := make([]domain.ItemReference, 0)
	for _, row := range rows {
		if seen[row.EQUID] {
			continue
		}
		seen[row.EQUID] = true
		refs = append(refs, domain.ItemReference{EQUID: row.EQUID, Name: row.Name})
	}
	resolved, err := s.metadata.ResolveBatch(ctx, refs)
	if err != nil {
		return nil, err
	}
	byEQUID := make(map[uint64]ItemMetadata, len(resolved))
	for _, item := range resolved {
		byEQUID[item.EQUID] = item
	}
	return byEQUID, nil
}

func (s *ImportService) commitDateOffsets(ctx context.Context) (int, int, error) {
	values, err := s.settings.Values(ctx)
	if err != nil {
		return 0, 0, err
	}
	openDays, err := importDateSetting(values, "GuildOpenDays")
	if err != nil {
		return 0, 0, err
	}
	bazaarDays, err := importDateSetting(values, "BazaarDays")
	if err != nil {
		return 0, 0, err
	}
	return openDays, bazaarDays, nil
}

func importDateSetting(values map[string]string, key string) (int, error) {
	value, ok := values[key]
	if !ok || value == "" {
		if def, found := Definition(key); found {
			value = def.Default
		}
	}
	days, err := strconv.Atoi(value)
	if err != nil || days < 0 || days > 3650 {
		return 0, fmt.Errorf("%w: %s must be a whole number from 0 to 3650", ErrSettingsInvalid, key)
	}
	return days, nil
}

func (s *ImportService) commitAttempt(ctx context.Context, batchID int64, actor domain.Actor,
	metadata map[uint64]ItemMetadata, openDays, bazaarDays int, now time.Time,
	correlationID, sourceIP string, elapsed time.Duration) (domain.CommitResult, error) {
	tx, err := s.store.BeginCommit(ctx, batchID)
	if err != nil {
		return domain.CommitResult{}, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	rows, err := tx.SelectedRows()
	if err != nil {
		return domain.CommitResult{}, err
	}
	if len(rows) == 0 {
		return domain.CommitResult{}, ErrSelectionEmpty
	}
	equids := make([]uint64, 0)
	seen := make(map[uint64]bool)
	for _, row := range rows {
		if row.ItemType == domain.ItemTypeSpell && !seen[row.EQUID] {
			seen[row.EQUID] = true
			equids = append(equids, row.EQUID)
		}
	}
	active, err := tx.ActiveSpells(equids)
	if err != nil {
		return domain.CommitResult{}, err
	}
	plan, err := buildCommitPlan(rows, active, metadata, actor, now, openDays, bazaarDays)
	if err != nil {
		return domain.CommitResult{}, err
	}
	if uint64(plan.itemCount+plan.newSpellCount) > ImportMaxRows {
		return domain.CommitResult{}, domain.NewValidationError("selection",
			"the selection would create more than 100,000 inventory records")
	}
	if len(plan.items) > 0 {
		if _, err := tx.InsertItems(plan.items); err != nil {
			return domain.CommitResult{}, err
		}
	}
	if len(plan.newSpells) > 0 {
		if _, err := tx.InsertSpells(plan.newSpells); err != nil {
			return domain.CommitResult{}, err
		}
	}
	for _, merge := range plan.merges {
		if err := tx.AddSpellQuantity(merge.target.GuildBankItemID, merge.quantity,
			now, actor.UserName, actor.UserID); err != nil {
			return domain.CommitResult{}, err
		}
	}
	if err := tx.MarkCompleted(); err != nil {
		return domain.CommitResult{}, err
	}
	audit, err := commitAuditEntries(batchID, actor, plan, len(rows), correlationID,
		sourceIP, now, elapsed)
	if err != nil {
		return domain.CommitResult{}, err
	}
	if err := tx.WriteAudit(audit); err != nil {
		return domain.CommitResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return domain.CommitResult{}, err
	}
	committed = true

	started := now
	result := domain.CommitResult{
		BatchID: batchID, Status: domain.ImportStatusCompleted,
		ItemRecordsCreated: plan.itemCount, SpellRecordsCreated: plan.newSpellCount,
		SpellRecordsMerged: len(plan.merges), SpellQuantityAdded: plan.spellQuantity,
		TotalRecordsAffected:  plan.itemCount + plan.newSpellCount + len(plan.merges),
		DuplicateSpellTargets: plan.duplicates, Warnings: plan.warnings,
		DurationMS: elapsed.Milliseconds(),
	}
	if result.DurationMS == 0 && time.Since(started) > 0 {
		result.DurationMS = time.Since(started).Milliseconds()
	}
	s.logger.Info("import batch committed", "batch_id", batchID,
		"user_id", actor.UserID, "items", result.ItemRecordsCreated,
		"new_spells", result.SpellRecordsCreated, "merged_spells", result.SpellRecordsMerged,
		"duration_ms", result.DurationMS, "request_id", correlationID)
	return result, nil
}

func buildCommitPlan(rows []domain.ImportRow, active []domain.GuildBankItem,
	metadata map[uint64]ItemMetadata, actor domain.Actor, now time.Time,
	openDays, bazaarDays int) (commitPlan, error) {
	dates := dateFields(now, openDays, bazaarDays)
	var plan commitPlan
	activeByEQUID := make(map[uint64][]domain.GuildBankItem)
	for _, item := range active {
		activeByEQUID[item.EQUID] = append(activeByEQUID[item.EQUID], item)
	}
	for _, targets := range activeByEQUID {
		sort.Slice(targets, func(i, j int) bool {
			return targets[i].GuildBankItemID < targets[j].GuildBankItemID
		})
	}
	type spellAggregate struct {
		first    domain.ImportRow
		quantity uint64
	}
	spells := make(map[uint64]*spellAggregate)
	spellOrder := make([]uint64, 0)
	for _, row := range rows {
		if row.ItemType == domain.ItemTypeSpell {
			aggregate, ok := spells[row.EQUID]
			if !ok {
				aggregate = &spellAggregate{first: row}
				spells[row.EQUID] = aggregate
				spellOrder = append(spellOrder, row.EQUID)
			}
			aggregate.quantity += uint64(row.Quantity)
			continue
		}
		item := metadata[row.EQUID]
		name := row.Name
		classString, raceString := "", ""
		missing := true
		if item.EQUID != 0 {
			name = item.Name
			classString, raceString = item.ClassString, item.RaceString
			missing = item.MetadataMissing
		}
		// QTY-01/QTY-03: one inventory row per staged item line carries the line
		// quantity. Items are never merged into an existing row, so a later
		// import of the same EQUID produces a second row with its own quantity.
		plan.items = append(plan.items, domain.GuildBankItem{
			ItemType: domain.ItemTypeItem, EQUID: row.EQUID, MetadataMissingFg: missing,
			Name: name, ClassString: classString, RaceString: raceString,
			DateAdded: dates.added, DateOpen: dates.open, DateBazaar: dates.bazaar,
			Quantity: row.Quantity, SourceLocation: row.LocationRaw, DeletedFg: false,
			LocationGroup: row.LocationGroup, LocationSort: row.LocationSort,
			ModAt: now.UTC(), ModBy: actor.UserName, ModByUserID: &actor.UserID,
			NameSort: TruncateRunes(NaturalSortKey(name), 160),
		})
		plan.itemCount++
		plan.itemUnits += uint64(row.Quantity)
	}
	for _, equid := range spellOrder {
		aggregate := spells[equid]
		if aggregate.quantity > math.MaxUint32 {
			return commitPlan{}, domain.NewValidationError("selection", "spell quantity exceeds the storage range")
		}
		item := metadata[equid]
		missing := item.EQUID == 0 || item.MetadataMissing
		// DEFECT-01: a spell row uses exactly the same Items metadata rule as
		// an item row. The resolved Items row supplies the authoritative name
		// and the decoded class/race strings; only an unresolved EQUID keeps
		// the imported name with blank metadata (AM-01/AM-02).
		name := aggregate.first.Name
		classString, raceString := "", ""
		if !missing {
			name = item.Name
			classString, raceString = item.ClassString, item.RaceString
		}
		targets := activeByEQUID[equid]
		if len(targets) == 0 {
			plan.newSpells = append(plan.newSpells, domain.GuildBankItem{
				ItemType: domain.ItemTypeSpell, EQUID: equid, MetadataMissingFg: missing,
				Name: name, ClassString: classString, RaceString: raceString,
				DateAdded: dates.added, DateOpen: dates.open,
				DateBazaar: dates.bazaar, Quantity: uint32(aggregate.quantity),
				SourceLocation: aggregate.first.LocationRaw, DeletedFg: false,
				LocationGroup: aggregate.first.LocationGroup,
				LocationSort:  aggregate.first.LocationSort,
				ModAt:         now.UTC(), ModBy: actor.UserName, ModByUserID: &actor.UserID,
				NameSort: TruncateRunes(NaturalSortKey(name), 160),
			})
			plan.newSpellCount++
			plan.spellQuantity += aggregate.quantity
			continue
		}
		target := targets[0]
		if len(targets) > 1 {
			plan.duplicates = append(plan.duplicates, domain.DuplicateSpellTarget{
				EQUID: equid, MergedInto: target.GuildBankItemID,
			})
			plan.warnings = append(plan.warnings, domain.ImportWarning{
				Code: "duplicate_spell_target", Field: "EQUID", EQUID: equid,
				Detail: fmt.Sprintf("%d active spell rows matched; quantity added to the lowest ID %d",
					len(targets), target.GuildBankItemID),
			})
			plan.duplicateRecords += len(targets) - 1
		}
		plan.merges = append(plan.merges, mergePlan{
			target: target, quantity: uint32(aggregate.quantity),
			row: aggregate.first, oldQty: target.Quantity,
		})
		plan.spellQuantity += aggregate.quantity
	}
	return plan, nil
}

type commitDates struct {
	added  time.Time
	open   time.Time
	bazaar time.Time
}

func dateFields(now time.Time, openDays, bazaarDays int) commitDates {
	utc := now.UTC()
	added := time.Date(utc.Year(), utc.Month(), utc.Day(), 0, 0, 0, 0, time.UTC)
	return commitDates{
		added: added, open: added.AddDate(0, 0, openDays),
		bazaar: added.AddDate(0, 0, bazaarDays),
	}
}

func commitAuditEntries(batchID int64, actor domain.Actor, plan commitPlan,
	selectedRows int, correlationID, sourceIP string, now time.Time,
	elapsed time.Duration) ([]domain.AuditEntry, error) {
	recordID := fmt.Sprintf("%d", batchID)
	oldStatus, _ := json.Marshal(map[string]any{"status": domain.ImportStatusUploaded})
	newStatus, _ := json.Marshal(map[string]any{
		"status": domain.ImportStatusCompleted, "selectedRows": selectedRows,
		"itemUnits": plan.itemUnits, "itemRecords": plan.itemCount,
		"spellsCreated": plan.newSpellCount,
		"spellsMerged":  len(plan.merges), "spellQuantity": plan.spellQuantity,
		"duplicateSpellRecords": plan.duplicateRecords,
		"durationMs":            elapsed.Milliseconds(),
	})
	createdSummary, _ := json.Marshal(map[string]any{
		"itemUnits": plan.itemUnits, "itemRecords": plan.itemCount,
		"spellsCreated": plan.newSpellCount,
		"spellsMerged":  len(plan.merges), "batchId": batchID,
	})
	entries := []domain.AuditEntry{
		{
			EventDate: now, UserID: actor.UserID, UserName: actor.UserName,
			EventType: "IMPORT_COMMITTED", TableName: "ImportBatch", RecordID: recordID,
			OldValues: oldStatus, NewValues: newStatus,
			CorrelationID: correlationID, SourceIP: sourceIP,
		},
		{
			EventDate: now, UserID: actor.UserID, UserName: actor.UserName,
			EventType: "ITEM_CREATED", TableName: "GuildBankItems", RecordID: recordID,
			NewValues: createdSummary, Notes: "import batch summary",
			CorrelationID: correlationID, SourceIP: sourceIP,
		},
	}
	for _, merge := range plan.merges {
		oldValues, _ := json.Marshal(map[string]any{
			"quantity": merge.oldQty, "sourceLocation": merge.target.SourceLocation,
		})
		newValues, _ := json.Marshal(map[string]any{
			"quantity": uint64(merge.oldQty) + uint64(merge.quantity),
			"modAt":    now.UTC(), "modBy": actor.UserName, "modByUserId": actor.UserID,
			"batchId": batchID, "importId": merge.row.ImportID,
			"locationRaw": merge.row.LocationRaw, "stagedQuantity": merge.quantity,
		})
		entries = append(entries, domain.AuditEntry{
			EventDate: now, UserID: actor.UserID, UserName: actor.UserName,
			EventType: "ITEM_UPDATED", TableName: "GuildBankItems",
			RecordID:  fmt.Sprintf("%d", merge.target.GuildBankItemID),
			OldValues: oldValues, NewValues: newValues,
			CorrelationID: correlationID, SourceIP: sourceIP,
		})
	}
	return entries, nil
}
