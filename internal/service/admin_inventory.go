package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/Panazule/guildbank/internal/domain"
)

const (
	// AdminMaxSearchRunes, AdminMaxNameRunes and AdminMaxLocationRunes reuse
	// the documented import/public bounds.
	AdminMaxSearchRunes   = PublicInventoryMaxSearchRunes
	AdminMaxNameRunes     = ImportMaxName
	AdminMaxLocationRunes = ImportMaxLocation
	AdminMaxGivenToRunes  = 128
	// AdminMaxItemQuantity is the per-record quantity cap for an ordinary item.
	// A single manual create writes one row, so this bounds one value rather
	// than the number of inserted rows.
	AdminMaxItemQuantity = ImportMaxRows
	// AdminMaxSpellQuantity is the uint32 storage ceiling.
	AdminMaxSpellQuantity = math.MaxUint32
	AdminMaxPageSize      = 200
	AdminDefaultPageSize  = 50
	AdminMaxPage          = 10000
	// AdminMaxBulkRemove caps one page-local bulk removal. The admin list page
	// size is at most 200, so a bulk action can never span more than one page.
	AdminMaxBulkRemove = 200
	// AdminAuditMaxPageSize caps the audit viewer page size.
	AdminAuditMaxPageSize = 200
)

// New warning codes for manual inventory writes. The existing
// ItemWarningMetadataMissing is reused unchanged.
const (
	ItemWarningLocationUnrecognized = "location_unrecognized"
	ItemWarningDuplicateSpellTarget = "duplicate_spell_target"
)

// AdminAuditEventTypes is the closed event-type allowlist for the audit viewer.
var AdminAuditEventTypes = []string{
	"ITEM_CREATED", "ITEM_UPDATED", "ITEM_REMOVED", "IMPORT_CREATED", "IMPORT_COMMITTED",
}

// AdminStore is the narrow repository surface used by the administrative
// service. It exposes reads and one write transaction factory; no raw SQL type
// crosses the boundary.
type AdminStore interface {
	Ready() bool
	ListAdminInventory(ctx context.Context, filter domain.AdminInventoryFilter) (domain.AdminInventoryPage, error)
	GetAdminInventory(ctx context.Context, id int64) (domain.AdminInventoryItem, error)
	ListLocationGroups(ctx context.Context) ([]string, error)
	BeginAdminWrite(ctx context.Context) (AdminWriteTx, error)
	ListAudit(ctx context.Context, filter domain.AuditFilter) (domain.AuditPage, error)
	GetAudit(ctx context.Context, id int64) (domain.AuditEntry, error)
}

// AdminWriteTx is the transaction-bound write surface. The repository
// implementation owns the advisory lock, the connection and the transaction.
type AdminWriteTx interface {
	LockInventory(ctx context.Context, id int64) (domain.AdminInventoryItem, error)
	FindActiveSpells(ctx context.Context, equid uint64) ([]domain.AdminInventoryItem, error)
	InsertInventory(ctx context.Context, rows []domain.GuildBankItem) ([]int64, error)
	AddSpellQuantity(ctx context.Context, id int64, qty uint32, at time.Time, actor domain.Actor) error
	UpdateInventory(ctx context.Context, in domain.AdminUpdateRow) error
	SetDeleted(ctx context.Context, id int64, deleted bool, at time.Time, actor domain.Actor) error
	InsertAudit(ctx context.Context, entry domain.AuditEntry) error
	Commit() error
	Rollback() error
}

// AdminMetadataResolver resolves item metadata for manual creation.
type AdminMetadataResolver interface {
	Resolve(ctx context.Context, ref domain.ItemReference) (ItemMetadata, error)
}

// AdminWriteResult is the outcome of one create/update/remove/restore.
type AdminWriteResult struct {
	Item         domain.AdminInventoryItem
	CreatedIDs   []int64
	CreatedCount int
	Warnings     []ItemWarning
	Changed      bool
	// FieldsChanged reports whether editable fields changed. It distinguishes
	// "saved and removed" from "removed with no edits" for the notice text.
	FieldsChanged bool
}

// AdminInventoryService owns administrative filter validation, lifecycle date
// derivation, location normalization and the single audit row per write.
type AdminInventoryService struct {
	store    AdminStore
	settings InventorySettingsReader
	metadata AdminMetadataResolver
	now      func() time.Time
	logger   *slog.Logger
}

// NewAdminInventoryService wires the administrative inventory path. A nil store
// produces a degraded service whose Ready() is false.
func NewAdminInventoryService(store AdminStore, settings InventorySettingsReader,
	metadata AdminMetadataResolver, now func() time.Time, logger *slog.Logger) *AdminInventoryService {
	if now == nil {
		now = time.Now
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &AdminInventoryService{store: store, settings: settings, metadata: metadata, now: now, logger: logger}
}

// Ready reports whether the guild store is available.
func (s *AdminInventoryService) Ready() bool {
	return s != nil && s.store != nil && s.store.Ready()
}

func (s *AdminInventoryService) requireReady() error {
	if !s.Ready() {
		return domain.ErrUnavailable
	}
	return nil
}

// List validates the admin filter, reads one snapshot and maps the page.
func (s *AdminInventoryService) List(ctx context.Context, filter domain.AdminInventoryFilter) (domain.AdminInventoryPage, error) {
	if err := s.requireReady(); err != nil {
		return domain.AdminInventoryPage{}, err
	}
	normalized, err := NormalizeAdminInventoryFilter(filter)
	if err != nil {
		return domain.AdminInventoryPage{}, err
	}
	return s.store.ListAdminInventory(ctx, normalized)
}

// Get reads one administrative record, including a removed row.
func (s *AdminInventoryService) Get(ctx context.Context, id int64) (domain.AdminInventoryItem, error) {
	if err := s.requireReady(); err != nil {
		return domain.AdminInventoryItem{}, err
	}
	if id <= 0 {
		return domain.AdminInventoryItem{}, domain.ErrNotFound
	}
	return s.store.GetAdminInventory(ctx, id)
}

// LocationGroups returns the selector values that exist in the store.
func (s *AdminInventoryService) LocationGroups(ctx context.Context) ([]string, error) {
	if err := s.requireReady(); err != nil {
		return nil, err
	}
	return s.store.ListLocationGroups(ctx)
}

// NormalizeAdminInventoryFilter trims and validates every admin list field so
// the HTML and JSON paths share one contract.
func NormalizeAdminInventoryFilter(filter domain.AdminInventoryFilter) (domain.AdminInventoryFilter, error) {
	fields := make(map[string]string)

	filter.Search = strings.TrimSpace(filter.Search)
	if len([]rune(filter.Search)) > AdminMaxSearchRunes {
		fields["q"] = fmt.Sprintf("q must be at most %d characters", AdminMaxSearchRunes)
	}
	filter.GivenTo = strings.TrimSpace(filter.GivenTo)
	if len([]rune(filter.GivenTo)) > AdminMaxGivenToRunes {
		fields["givenTo"] = fmt.Sprintf("givenTo must be at most %d characters", AdminMaxGivenToRunes)
	}
	filter.LocationGroup = strings.TrimSpace(filter.LocationGroup)
	if len([]rune(filter.LocationGroup)) > 64 {
		fields["locationGroup"] = "locationGroup must be at most 64 characters"
	}
	if filter.ID < 0 {
		fields["id"] = "id must be a positive integer"
	}

	switch filter.Metadata {
	case "", domain.AdminMetadataAny:
		filter.Metadata = domain.AdminMetadataAny
	case domain.AdminMetadataMissing, domain.AdminMetadataPresent:
	default:
		fields["metadata"] = "metadata must be any, missing or present"
	}

	itemType := strings.TrimSpace(string(filter.ItemType))
	switch {
	case itemType == "" || strings.EqualFold(itemType, "all"):
		filter.ItemType = ""
	case strings.EqualFold(itemType, string(domain.ItemTypeItem)):
		filter.ItemType = domain.ItemTypeItem
	case strings.EqualFold(itemType, string(domain.ItemTypeSpell)):
		filter.ItemType = domain.ItemTypeSpell
	default:
		fields["itemType"] = "itemType must be Item, Spell or all"
	}

	switch filter.Status {
	case "":
		filter.Status = domain.AdminStatusActive
	case domain.AdminStatusActive, domain.AdminStatusRemoved, domain.AdminStatusAll:
	default:
		fields["status"] = "status must be active, removed or all"
	}

	if filter.Sort == "" {
		filter.Sort = domain.AdminSortModifiedNewest
	}
	if !filter.Sort.Valid() {
		fields["sort"] = "sort must be one of the approved values"
	}
	if filter.Page < 1 || filter.Page > AdminMaxPage {
		fields["page"] = fmt.Sprintf("page must be between 1 and %d", AdminMaxPage)
	}
	if filter.PageSize < 1 || filter.PageSize > AdminMaxPageSize {
		fields["pageSize"] = fmt.Sprintf("pageSize must be between 1 and %d", AdminMaxPageSize)
	}
	if len(fields) > 0 {
		return domain.AdminInventoryFilter{}, &domain.ValidationError{Fields: fields}
	}
	return filter, nil
}

// adminText trims one free-text value and validates rune length and control
// characters. It records a field error rather than returning early so the
// caller can report every invalid field at once.
func adminText(field, value string, maxRunes int, required bool, fields map[string]string) string {
	trimmed := strings.TrimSpace(value)
	if required && trimmed == "" {
		fields[field] = field + " is required"
		return trimmed
	}
	if len([]rune(trimmed)) > maxRunes {
		fields[field] = fmt.Sprintf("%s must be at most %d characters", field, maxRunes)
		return trimmed
	}
	for _, r := range trimmed {
		if unicode.IsControl(r) {
			fields[field] = field + " must not contain control characters"
			break
		}
	}
	return trimmed
}

// adminOptionalText normalizes one optional string field. An absent or null key
// and an empty value all become nil, matching the NULL column semantics.
func adminOptionalText(field string, value domain.OptionalString, maxRunes int, fields map[string]string) domain.OptionalString {
	if !value.Set || value.Value == nil {
		return domain.OptionalString{Set: value.Set}
	}
	trimmed := adminText(field, *value.Value, maxRunes, false, fields)
	if trimmed == "" {
		return domain.OptionalString{Set: true}
	}
	return domain.OptionalString{Value: &trimmed, Set: true}
}

// adminDateInRange reports whether a calendar date fits MariaDB's DATE range.
func adminDateInRange(value time.Time) bool {
	year := value.UTC().Year()
	return year >= 1000 && year <= 9999
}

// validateAdminDate records a field error for a date outside the allowed range.
func validateAdminDate(field string, value domain.OptionalDate, fields map[string]string) {
	if value.Set && value.Value != nil && !adminDateInRange(*value.Value) {
		fields[field] = field + " must be between 1000-01-01 and 9999-12-31"
	}
}

// Create adds one EQUID and quantity. Item metadata, lifecycle dates and the
// normalized location are all derived server-side; warnings are non-fatal.
func (s *AdminInventoryService) Create(ctx context.Context, in domain.CreateItemInput) (AdminWriteResult, error) {
	if err := s.requireReady(); err != nil {
		return AdminWriteResult{}, err
	}
	if err := validateActor(in.Actor); err != nil {
		return AdminWriteResult{}, err
	}
	prepared, row, warnings, err := s.prepareCreate(ctx, in)
	if err != nil {
		return AdminWriteResult{}, err
	}
	return s.withDeadlockRetry("create", func() (AdminWriteResult, error) {
		return s.createAttempt(ctx, prepared, row, warnings)
	})
}

// dateOffsets reads both lifecycle offsets once from Settings.
func (s *AdminInventoryService) dateOffsets(ctx context.Context) (int, int, error) {
	if s.settings == nil {
		return 0, 0, fmt.Errorf("%w: settings are unavailable", domain.ErrUnavailable)
	}
	values, err := s.settings.Values(ctx)
	if err != nil {
		return 0, 0, fmt.Errorf("%w: settings are unavailable", domain.ErrUnavailable)
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

// withDeadlockRetry retries one deadlocked write once; the attempt closure
// captures the timestamp, so the retry uses the same value.
func (s *AdminInventoryService) withDeadlockRetry(operation string, attempt func() (AdminWriteResult, error)) (AdminWriteResult, error) {
	result, err := attempt()
	var retry *domain.RetryableError
	if err != nil && errors.As(err, &retry) {
		s.logger.Warn("admin write deadlock; retrying once", "operation", operation)
		result, err = attempt()
	}
	return result, err
}

// prepareCreate validates the request, resolves metadata, derives the dates and
// builds the row template shared by every created row.
func (s *AdminInventoryService) prepareCreate(ctx context.Context, in domain.CreateItemInput) (domain.CreateItemInput, domain.GuildBankItem, []ItemWarning, error) {
	fields := make(map[string]string)
	switch in.ItemType {
	case domain.ItemTypeItem:
		if in.Quantity < 1 || uint64(in.Quantity) > uint64(AdminMaxItemQuantity) {
			fields["quantity"] = fmt.Sprintf("quantity must be between 1 and %d for an item", AdminMaxItemQuantity)
		}
	case domain.ItemTypeSpell:
		if in.Quantity < 1 {
			fields["quantity"] = "quantity must be at least 1"
		}
	default:
		fields["itemType"] = "itemType must be Item or Spell"
	}
	if in.EQUID == 0 || in.EQUID > math.MaxUint32 {
		fields["equid"] = "equid must be between 1 and 4294967295"
	}
	in.Name = adminText("name", in.Name, AdminMaxNameRunes, false, fields)
	in.SourceLocation = adminOptionalText("sourceLocation", in.SourceLocation, AdminMaxLocationRunes, fields)
	in.GivenTo = adminOptionalText("givenTo", in.GivenTo, AdminMaxGivenToRunes, fields)
	validateAdminDate("dateOpen", in.DateOpen, fields)
	validateAdminDate("dateBazaar", in.DateBazaar, fields)
	if len(fields) > 0 {
		return in, domain.GuildBankItem{}, nil, &domain.ValidationError{Fields: fields}
	}
	if s.metadata == nil {
		return in, domain.GuildBankItem{}, nil, domain.ErrUnavailable
	}
	meta, err := s.metadata.Resolve(ctx, domain.ItemReference{EQUID: in.EQUID, Name: in.Name})
	if err != nil {
		return in, domain.GuildBankItem{}, nil, err
	}
	name := meta.Name
	classString, raceString := meta.ClassString, meta.RaceString
	missing := meta.MetadataMissing
	if missing {
		if in.Name == "" {
			return in, domain.GuildBankItem{}, nil,
				domain.NewValidationError("name", "name is required when item metadata is missing")
		}
		name, classString, raceString = in.Name, "", ""
	}
	warnings := make([]ItemWarning, 0, len(meta.Warnings)+1)
	warnings = append(warnings, meta.Warnings...)

	now := s.now().UTC()
	added := utcDate(now)
	open, bazaar := added, added
	bothDefault := !in.DateOpen.Set && !in.DateBazaar.Set
	openDays, bazaarDays := 0, 0
	if !in.DateOpen.Set || !in.DateBazaar.Set {
		openDays, bazaarDays, err = s.dateOffsets(ctx)
		if err != nil {
			return in, domain.GuildBankItem{}, nil, err
		}
	}
	if in.DateOpen.Set && in.DateOpen.Value != nil {
		open = utcDate(*in.DateOpen.Value)
	} else {
		open = added.AddDate(0, 0, openDays)
	}
	if in.DateBazaar.Set && in.DateBazaar.Value != nil {
		bazaar = utcDate(*in.DateBazaar.Value)
	} else {
		bazaar = added.AddDate(0, 0, bazaarDays)
	}
	switch {
	case !open.Before(added) && !bazaar.Before(added) && !open.After(bazaar):
		// window is valid
	case bothDefault && open.After(bazaar):
		return in, domain.GuildBankItem{}, nil,
			fmt.Errorf("%w: GuildOpenDays is greater than BazaarDays", ErrSettingsInvalid)
	case open.Before(added):
		field := "dateOpen"
		if !in.DateOpen.Set {
			field = "dateBazaar"
		}
		return in, domain.GuildBankItem{}, nil,
			domain.NewValidationError(field, field+" cannot be before the date added")
	case bazaar.Before(added):
		return in, domain.GuildBankItem{}, nil,
			domain.NewValidationError("dateBazaar", "dateBazaar cannot be before the date added")
	default:
		field := "dateOpen"
		if in.DateBazaar.Set && !in.DateOpen.Set {
			field = "dateBazaar"
		}
		return in, domain.GuildBankItem{}, nil,
			domain.NewValidationError(field, field+" cannot be after the other lifecycle date")
	}

	location := NormalizeAdminLocation(in.SourceLocation.String())
	for _, warning := range location.Warnings {
		warnings = append(warnings, ItemWarning{Code: warning.Code, Field: warning.Field, Detail: warning.Detail})
	}
	row := domain.GuildBankItem{
		ItemType: in.ItemType, EQUID: in.EQUID, MetadataMissingFg: missing,
		Name: name, ClassString: classString, RaceString: raceString,
		DateAdded: added, DateOpen: open, DateBazaar: bazaar, Quantity: in.Quantity,
		SourceLocation: in.SourceLocation.String(), LocationGroup: location.Group,
		LocationSort: TruncateRunes(location.Sort, 80),
		GivenTo:      in.GivenTo.String(), DeletedFg: false,
		ModAt: now, ModBy: in.Actor.UserName, ModByUserID: &in.Actor.UserID,
		NameSort: TruncateRunes(NaturalSortKey(name), 160),
	}
	return in, row, warnings, nil
}

// createAttempt performs one locked create transaction and its audit row.
func (s *AdminInventoryService) createAttempt(ctx context.Context, in domain.CreateItemInput,
	row domain.GuildBankItem, warnings []ItemWarning) (AdminWriteResult, error) {
	tx, err := s.store.BeginAdminWrite(ctx)
	if err != nil {
		return AdminWriteResult{}, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	if row.ItemType == domain.ItemTypeSpell {
		spells, err := tx.FindActiveSpells(ctx, row.EQUID)
		if err != nil {
			return AdminWriteResult{}, err
		}
		if len(spells) > 0 {
			if in.DateOpen.Set {
				return AdminWriteResult{}, domain.NewValidationError("dateOpen",
					"a spell that merges into an existing row cannot set dateOpen")
			}
			if in.DateBazaar.Set {
				return AdminWriteResult{}, domain.NewValidationError("dateBazaar",
					"a spell that merges into an existing row cannot set dateBazaar")
			}
			target := spells[0]
			if len(spells) > 1 {
				warnings = append(warnings, ItemWarning{
					Code: ItemWarningDuplicateSpellTarget, Field: "equid",
					Detail: fmt.Sprintf("%d active spell rows matched; quantity added to the lowest ID %d",
						len(spells), target.ID),
				})
			}
			sum := uint64(target.Quantity) + uint64(in.Quantity)
			if sum > math.MaxUint32 {
				return AdminWriteResult{}, domain.NewValidationError("quantity",
					"the merged spell quantity exceeds the storage range")
			}
			if err := tx.AddSpellQuantity(ctx, target.ID, in.Quantity, row.ModAt, in.Actor); err != nil {
				return AdminWriteResult{}, err
			}
			entry, err := spellMergeAudit(target, uint32(sum), in, row.ModAt)
			if err != nil {
				return AdminWriteResult{}, err
			}
			if err := tx.InsertAudit(ctx, entry); err != nil {
				return AdminWriteResult{}, err
			}
			if err := tx.Commit(); err != nil {
				return AdminWriteResult{}, err
			}
			committed = true
			target.Quantity = uint32(sum)
			target.ModAt = row.ModAt
			target.ModBy = in.Actor.UserName
			actorID := in.Actor.UserID
			target.ModByUserID = &actorID
			return AdminWriteResult{
				Item: target, CreatedIDs: []int64{}, CreatedCount: 0,
				Warnings: warnings, Changed: true,
			}, nil
		}
	}

	// QTY-07: manual create follows the import model. One row stores the
	// entered quantity; an ordinary item is no longer expanded per unit.
	rows := []domain.GuildBankItem{row}
	ids, err := tx.InsertInventory(ctx, rows)
	if err != nil {
		return AdminWriteResult{}, err
	}
	if len(ids) != len(rows) {
		return AdminWriteResult{}, fmt.Errorf("%w: inserted inventory id count mismatch", domain.ErrConflict)
	}
	entry, err := createAudit(rows[0], in, ids)
	if err != nil {
		return AdminWriteResult{}, err
	}
	if err := tx.InsertAudit(ctx, entry); err != nil {
		return AdminWriteResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return AdminWriteResult{}, err
	}
	committed = true

	created := ids
	if len(created) > 10000 {
		created = created[:10000]
	}
	item := adminItemFromRow(rows[0], ids[0])
	return AdminWriteResult{
		Item: item, CreatedIDs: created, CreatedCount: len(ids),
		Warnings: warnings, Changed: true,
	}, nil
}

// adminItemFromRow maps a prepared insert row plus its generated ID.
func adminItemFromRow(row domain.GuildBankItem, id int64) domain.AdminInventoryItem {
	return domain.AdminInventoryItem{
		ID: id, EQUID: row.EQUID, Name: row.Name, ItemType: row.ItemType,
		Quantity: row.Quantity, ClassString: row.ClassString, RaceString: row.RaceString,
		MetadataMissingFg: row.MetadataMissingFg, SourceLocation: row.SourceLocation,
		LocationGroup: row.LocationGroup, LocationSort: row.LocationSort,
		GivenTo: row.GivenTo, DeletedFg: row.DeletedFg, NameSort: row.NameSort,
		DateAdded: row.DateAdded, DateOpen: row.DateOpen, DateBazaar: row.DateBazaar,
		ModAt: row.ModAt, ModBy: row.ModBy, ModByUserID: row.ModByUserID,
	}
}

func adminDateText(value time.Time) string { return value.UTC().Format("2006-01-02") }

// createAudit builds the redacted ITEM_CREATED payload. createdIds is bounded
// to the first 100 IDs; createdIdCount carries the total when larger.
func createAudit(row domain.GuildBankItem, in domain.CreateItemInput, ids []int64) (domain.AuditEntry, error) {
	createdIDs := ids
	if len(createdIDs) > 100 {
		createdIDs = createdIDs[:100]
	}
	payload := map[string]any{
		"equid": row.EQUID, "itemType": string(row.ItemType), "name": row.Name,
		"quantity": in.Quantity, "sourceLocation": row.SourceLocation,
		"givenTo": row.GivenTo, "metadataMissing": row.MetadataMissingFg,
		"dateOpen": adminDateText(row.DateOpen), "dateBazaar": adminDateText(row.DateBazaar),
		"createdIds": createdIDs,
	}
	if len(ids) > len(createdIDs) {
		payload["createdIdCount"] = len(ids)
	}
	newValues, err := json.Marshal(payload)
	if err != nil {
		return domain.AuditEntry{}, err
	}
	return domain.AuditEntry{
		EventDate: row.ModAt, UserID: in.Actor.UserID, UserName: in.Actor.UserName,
		EventType: "ITEM_CREATED", TableName: "GuildBankItems",
		RecordID: fmt.Sprintf("%d", ids[0]), OldValues: json.RawMessage("{}"),
		NewValues: newValues, CorrelationID: in.CorrelationID, SourceIP: in.SourceIP,
	}, nil
}

// spellMergeAudit builds the ITEM_UPDATED payload for a quantity merge.
func spellMergeAudit(target domain.AdminInventoryItem, sum uint32, in domain.CreateItemInput, at time.Time) (domain.AuditEntry, error) {
	oldValues, err := json.Marshal(map[string]any{"quantity": target.Quantity})
	if err != nil {
		return domain.AuditEntry{}, err
	}
	newValues, err := json.Marshal(map[string]any{"quantity": sum, "added": in.Quantity})
	if err != nil {
		return domain.AuditEntry{}, err
	}
	return domain.AuditEntry{
		EventDate: at, UserID: in.Actor.UserID, UserName: in.Actor.UserName,
		EventType: "ITEM_UPDATED", TableName: "GuildBankItems",
		RecordID: fmt.Sprintf("%d", target.ID), OldValues: oldValues,
		NewValues: newValues, Notes: "spell quantity merged",
		CorrelationID: in.CorrelationID, SourceIP: in.SourceIP,
	}, nil
}

// Update applies the editable fields of one row. All editable keys are required;
// DateAdded, identity and the name sort never change.
func (s *AdminInventoryService) Update(ctx context.Context, in domain.UpdateItemInput) (AdminWriteResult, error) {
	if err := s.requireReady(); err != nil {
		return AdminWriteResult{}, err
	}
	if err := validateActor(in.Actor); err != nil {
		return AdminWriteResult{}, err
	}
	if in.ID <= 0 {
		return AdminWriteResult{}, domain.ErrNotFound
	}
	fields := make(map[string]string)
	if in.Quantity < 1 {
		fields["quantity"] = "quantity must be at least 1"
	}
	if in.ExpectedModAt.IsZero() {
		fields["expectedModAt"] = "expectedModAt is required"
	}
	if !in.SourceLocation.Set {
		fields["sourceLocation"] = "sourceLocation is required (use null to clear)"
	} else {
		in.SourceLocation = adminOptionalText("sourceLocation", in.SourceLocation, AdminMaxLocationRunes, fields)
	}
	if !in.GivenTo.Set {
		fields["givenTo"] = "givenTo is required (use null to clear)"
	} else {
		in.GivenTo = adminOptionalText("givenTo", in.GivenTo, AdminMaxGivenToRunes, fields)
	}
	if !in.DateOpen.Set {
		fields["dateOpen"] = "dateOpen is required (use null to reset)"
	} else {
		validateAdminDate("dateOpen", in.DateOpen, fields)
	}
	if !in.DateBazaar.Set {
		fields["dateBazaar"] = "dateBazaar is required (use null to reset)"
	} else {
		validateAdminDate("dateBazaar", in.DateBazaar, fields)
	}
	if len(fields) > 0 {
		return AdminWriteResult{}, &domain.ValidationError{Fields: fields}
	}
	return s.withDeadlockRetry("update", func() (AdminWriteResult, error) {
		return s.updateAttempt(ctx, in)
	})
}

func (s *AdminInventoryService) updateAttempt(ctx context.Context, in domain.UpdateItemInput) (AdminWriteResult, error) {
	tx, err := s.store.BeginAdminWrite(ctx)
	if err != nil {
		return AdminWriteResult{}, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	current, err := tx.LockInventory(ctx, in.ID)
	if err != nil {
		return AdminWriteResult{}, err
	}
	if current.DeletedFg {
		return AdminWriteResult{}, fmt.Errorf("%w: inventory row is removed", domain.ErrConflict)
	}
	if !sameModAt(current.ModAt, in.ExpectedModAt) {
		return AdminWriteResult{}, fmt.Errorf("%w: the record changed since it was loaded", domain.ErrConflict)
	}
	if current.ItemType == domain.ItemTypeItem && uint64(in.Quantity) > uint64(AdminMaxItemQuantity) {
		return AdminWriteResult{}, domain.NewValidationError("quantity",
			fmt.Sprintf("quantity must be between 1 and %d for an item", AdminMaxItemQuantity))
	}

	open, bazaar := current.DateOpen, current.DateBazaar
	openDays, bazaarDays := 0, 0
	needSettings := in.DateOpen.Reset() || in.DateBazaar.Reset()
	if needSettings {
		openDays, bazaarDays, err = s.dateOffsets(ctx)
		if err != nil {
			return AdminWriteResult{}, err
		}
	}
	resetFromSettings := false
	if in.DateOpen.Value != nil {
		open = utcDate(*in.DateOpen.Value)
	} else if needSettings {
		open = current.DateAdded.AddDate(0, 0, openDays)
		resetFromSettings = true
	}
	if in.DateBazaar.Value != nil {
		bazaar = utcDate(*in.DateBazaar.Value)
	} else if needSettings {
		bazaar = current.DateAdded.AddDate(0, 0, bazaarDays)
		resetFromSettings = true
	}
	switch {
	case open.Before(current.DateAdded):
		return AdminWriteResult{}, domain.NewValidationError("dateOpen", "dateOpen cannot be before the date added")
	case bazaar.Before(current.DateAdded):
		return AdminWriteResult{}, domain.NewValidationError("dateBazaar", "dateBazaar cannot be before the date added")
	case open.After(bazaar):
		if resetFromSettings {
			return AdminWriteResult{}, fmt.Errorf("%w: GuildOpenDays is greater than BazaarDays", ErrSettingsInvalid)
		}
		field := "dateOpen"
		if in.DateOpen.Value != nil && in.DateBazaar.Value == nil {
			field = "dateBazaar"
		}
		return AdminWriteResult{}, domain.NewValidationError(field, field+" cannot be after the other lifecycle date")
	}

	location := NormalizeAdminLocation(in.SourceLocation.String())
	warnings := make([]ItemWarning, 0, 1)
	for _, warning := range location.Warnings {
		warnings = append(warnings, ItemWarning{Code: warning.Code, Field: warning.Field, Detail: warning.Detail})
	}

	changed := make(map[string]bool, 5)
	if in.Quantity != current.Quantity {
		changed["quantity"] = true
	}
	if in.SourceLocation.String() != current.SourceLocation {
		changed["sourceLocation"] = true
	}
	if in.GivenTo.String() != current.GivenTo {
		changed["givenTo"] = true
	}
	if !sameDay(open, current.DateOpen) {
		changed["dateOpen"] = true
	}
	if !sameDay(bazaar, current.DateBazaar) {
		changed["dateBazaar"] = true
	}
	if len(changed) == 0 && !in.Remove {
		if err := tx.Commit(); err != nil {
			return AdminWriteResult{}, err
		}
		committed = true
		return AdminWriteResult{Item: current, Warnings: warnings, Changed: false}, nil
	}

	modAt := s.now().UTC()
	row := domain.AdminUpdateRow{
		ID: current.ID, SourceLocation: in.SourceLocation.String(),
		LocationGroup: location.Group, LocationSort: TruncateRunes(location.Sort, 80),
		GivenTo: in.GivenTo.String(), ModAt: modAt, Actor: in.Actor,
	}
	if changed["quantity"] {
		quantity := in.Quantity
		row.Quantity = &quantity
	}
	if changed["dateOpen"] {
		value := open
		row.DateOpen = &value
	}
	if changed["dateBazaar"] {
		value := bazaar
		row.DateBazaar = &value
	}
	// AI-02: when the administrator asks to save and remove, the field update
	// and the soft delete happen in this one transaction. The ITEM_UPDATED row
	// is written only when editable fields actually changed; ITEM_REMOVED is
	// always written. Both share the request's correlation ID.
	if len(changed) > 0 {
		if err := tx.UpdateInventory(ctx, row); err != nil {
			return AdminWriteResult{}, err
		}
	}
	if in.Remove {
		if err := tx.SetDeleted(ctx, current.ID, true, modAt, in.Actor); err != nil {
			return AdminWriteResult{}, err
		}
	}
	if len(changed) > 0 {
		entry, err := updateAudit(current, changed, in, open, bazaar, modAt)
		if err != nil {
			return AdminWriteResult{}, err
		}
		if err := tx.InsertAudit(ctx, entry); err != nil {
			return AdminWriteResult{}, err
		}
	}
	if in.Remove {
		entry, err := stateAudit(current, true, in.Actor, modAt, in.CorrelationID, in.SourceIP)
		if err != nil {
			return AdminWriteResult{}, err
		}
		if err := tx.InsertAudit(ctx, entry); err != nil {
			return AdminWriteResult{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return AdminWriteResult{}, err
	}
	committed = true

	updated := current
	updated.Quantity = in.Quantity
	updated.SourceLocation = in.SourceLocation.String()
	updated.LocationGroup = location.Group
	updated.LocationSort = TruncateRunes(location.Sort, 80)
	updated.GivenTo = in.GivenTo.String()
	updated.DateOpen = open
	updated.DateBazaar = bazaar
	updated.DeletedFg = in.Remove
	updated.ModAt = modAt
	updated.ModBy = in.Actor.UserName
	actorID := in.Actor.UserID
	updated.ModByUserID = &actorID
	return AdminWriteResult{Item: updated, Warnings: warnings, Changed: true, FieldsChanged: len(changed) > 0}, nil
}

func sameModAt(stored, expected time.Time) bool {
	return stored.UTC().Truncate(time.Microsecond).Equal(expected.UTC().Truncate(time.Microsecond))
}

func sameDay(a, b time.Time) bool {
	return utcDate(a).Equal(utcDate(b))
}

// updateAudit writes only the editable fields that actually changed.
func updateAudit(current domain.AdminInventoryItem, changed map[string]bool,
	in domain.UpdateItemInput, open, bazaar, at time.Time) (domain.AuditEntry, error) {
	oldValues := make(map[string]any, len(changed))
	newValues := make(map[string]any, len(changed))
	if changed["quantity"] {
		oldValues["quantity"] = current.Quantity
		newValues["quantity"] = in.Quantity
	}
	if changed["sourceLocation"] {
		oldValues["sourceLocation"] = current.SourceLocation
		newValues["sourceLocation"] = in.SourceLocation.String()
	}
	if changed["givenTo"] {
		oldValues["givenTo"] = current.GivenTo
		newValues["givenTo"] = in.GivenTo.String()
	}
	if changed["dateOpen"] {
		oldValues["dateOpen"] = adminDateText(current.DateOpen)
		newValues["dateOpen"] = adminDateText(open)
	}
	if changed["dateBazaar"] {
		oldValues["dateBazaar"] = adminDateText(current.DateBazaar)
		newValues["dateBazaar"] = adminDateText(bazaar)
	}
	oldJSON, err := json.Marshal(oldValues)
	if err != nil {
		return domain.AuditEntry{}, err
	}
	newJSON, err := json.Marshal(newValues)
	if err != nil {
		return domain.AuditEntry{}, err
	}
	return domain.AuditEntry{
		EventDate: at, UserID: in.Actor.UserID, UserName: in.Actor.UserName,
		EventType: "ITEM_UPDATED", TableName: "GuildBankItems",
		RecordID: fmt.Sprintf("%d", current.ID), OldValues: oldJSON,
		NewValues: newJSON, CorrelationID: in.CorrelationID, SourceIP: in.SourceIP,
	}, nil
}

// Remove soft-deletes one active row and writes ITEM_REMOVED.
func (s *AdminInventoryService) Remove(ctx context.Context, id int64, expected time.Time,
	actor domain.Actor, correlationID, sourceIP string) (AdminWriteResult, error) {
	return s.setDeleted(ctx, id, expected, true, actor, correlationID, sourceIP)
}

// Restore clears the soft delete of one removed row and writes ITEM_UPDATED
// with the fixed note "restored".
func (s *AdminInventoryService) Restore(ctx context.Context, id int64, expected time.Time,
	actor domain.Actor, correlationID, sourceIP string) (AdminWriteResult, error) {
	return s.setDeleted(ctx, id, expected, false, actor, correlationID, sourceIP)
}

func (s *AdminInventoryService) setDeleted(ctx context.Context, id int64, expected time.Time,
	deleted bool, actor domain.Actor, correlationID, sourceIP string) (AdminWriteResult, error) {
	if err := s.requireReady(); err != nil {
		return AdminWriteResult{}, err
	}
	if err := validateActor(actor); err != nil {
		return AdminWriteResult{}, err
	}
	if id <= 0 {
		return AdminWriteResult{}, domain.ErrNotFound
	}
	fields := make(map[string]string)
	if expected.IsZero() {
		fields["expectedModAt"] = "expectedModAt is required"
	}
	if len(fields) > 0 {
		return AdminWriteResult{}, &domain.ValidationError{Fields: fields}
	}
	return s.withDeadlockRetry("state", func() (AdminWriteResult, error) {
		return s.setDeletedAttempt(ctx, id, expected, deleted, actor, correlationID, sourceIP)
	})
}

func (s *AdminInventoryService) setDeletedAttempt(ctx context.Context, id int64, expected time.Time,
	deleted bool, actor domain.Actor, correlationID, sourceIP string) (AdminWriteResult, error) {
	tx, err := s.store.BeginAdminWrite(ctx)
	if err != nil {
		return AdminWriteResult{}, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	current, err := tx.LockInventory(ctx, id)
	if err != nil {
		return AdminWriteResult{}, err
	}
	if !deleted && current.ConsolidatedInto != nil {
		return AdminWriteResult{}, fmt.Errorf("%w: the record was consolidated into row %d and cannot be restored",
			domain.ErrConflict, *current.ConsolidatedInto)
	}
	if current.DeletedFg == deleted {
		if deleted {
			return AdminWriteResult{}, fmt.Errorf("%w: the record is already removed", domain.ErrConflict)
		}
		return AdminWriteResult{}, fmt.Errorf("%w: the record is not removed", domain.ErrConflict)
	}
	if !sameModAt(current.ModAt, expected) {
		return AdminWriteResult{}, fmt.Errorf("%w: the record changed since it was loaded", domain.ErrConflict)
	}
	at := s.now().UTC()
	if err := tx.SetDeleted(ctx, id, deleted, at, actor); err != nil {
		return AdminWriteResult{}, err
	}
	entry, err := stateAudit(current, deleted, actor, at, correlationID, sourceIP)
	if err != nil {
		return AdminWriteResult{}, err
	}
	if err := tx.InsertAudit(ctx, entry); err != nil {
		return AdminWriteResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return AdminWriteResult{}, err
	}
	committed = true
	current.DeletedFg = deleted
	current.ModAt = at
	current.ModBy = actor.UserName
	actorID := actor.UserID
	current.ModByUserID = &actorID
	return AdminWriteResult{Item: current, Changed: true}, nil
}

func stateAudit(current domain.AdminInventoryItem, deleted bool,
	actor domain.Actor, at time.Time, correlationID, sourceIP string) (domain.AuditEntry, error) {
	oldJSON, err := json.Marshal(map[string]any{"deletedFg": current.DeletedFg})
	if err != nil {
		return domain.AuditEntry{}, err
	}
	newJSON, err := json.Marshal(map[string]any{"deletedFg": deleted})
	if err != nil {
		return domain.AuditEntry{}, err
	}
	entryType := "ITEM_UPDATED"
	notes := "restored"
	if deleted {
		entryType, notes = "ITEM_REMOVED", ""
	}
	return domain.AuditEntry{
		EventDate: at, UserID: actor.UserID, UserName: actor.UserName,
		EventType: entryType, TableName: "GuildBankItems",
		RecordID: fmt.Sprintf("%d", current.ID), OldValues: oldJSON,
		NewValues: newJSON, Notes: notes,
		CorrelationID: correlationID, SourceIP: sourceIP,
	}, nil
}

// PreviewRemoveMany validates one page-local bulk-removal selection and returns
// the records that would be removed. It is a read: RemoveMany re-validates every
// record under lock so a preview cannot become a stale authorization.
func (s *AdminInventoryService) PreviewRemoveMany(ctx context.Context, targets []domain.RemoveItemTarget) ([]domain.AdminInventoryItem, error) {
	if err := s.requireReady(); err != nil {
		return nil, err
	}
	if err := validateRemoveTargets(targets); err != nil {
		return nil, err
	}
	items := make([]domain.AdminInventoryItem, 0, len(targets))
	for _, target := range targets {
		item, err := s.store.GetAdminInventory(ctx, target.ID)
		if err != nil {
			return nil, err
		}
		if item.DeletedFg {
			return nil, fmt.Errorf("%w: a selected record is already removed", domain.ErrConflict)
		}
		if !sameModAt(item.ModAt, target.ExpectedModAt) {
			return nil, fmt.Errorf("%w: a selected record changed since the page was loaded", domain.ErrConflict)
		}
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	return items, nil
}

// RemoveMany soft-deletes every selected record in one transaction, locking the
// rows in ascending primary-key order to keep concurrent writers deadlock-free.
// It is all-or-nothing: one stale token, removed row or missing ID rolls back the
// whole selection. Each record gets its own ITEM_REMOVED audit row; all rows of
// the request share the caller's correlation ID (AI-01).
func (s *AdminInventoryService) RemoveMany(ctx context.Context, targets []domain.RemoveItemTarget,
	actor domain.Actor, correlationID, sourceIP string) (domain.BulkRemoveResult, error) {
	if err := s.requireReady(); err != nil {
		return domain.BulkRemoveResult{}, err
	}
	if err := validateActor(actor); err != nil {
		return domain.BulkRemoveResult{}, err
	}
	if err := validateRemoveTargets(targets); err != nil {
		return domain.BulkRemoveResult{}, err
	}
	ordered := append([]domain.RemoveItemTarget(nil), targets...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].ID < ordered[j].ID })

	result, err := s.removeManyAttempt(ctx, ordered, actor, correlationID, sourceIP)
	var retry *domain.RetryableError
	if err != nil && errors.As(err, &retry) {
		s.logger.Warn("bulk remove deadlock; retrying once", "count", len(ordered))
		result, err = s.removeManyAttempt(ctx, ordered, actor, correlationID, sourceIP)
	}
	return result, err
}

func validateRemoveTargets(targets []domain.RemoveItemTarget) error {
	fields := make(map[string]string)
	switch {
	case len(targets) == 0:
		fields["remove"] = "select at least one record"
	case len(targets) > AdminMaxBulkRemove:
		fields["remove"] = fmt.Sprintf("at most %d records may be removed at once", AdminMaxBulkRemove)
	}
	seen := make(map[int64]bool, len(targets))
	for _, target := range targets {
		if target.ID <= 0 || target.ExpectedModAt.IsZero() {
			fields["remove"] = "every selected record needs an ID and its version token"
			continue
		}
		if seen[target.ID] {
			fields["remove"] = "a record was selected more than once"
			continue
		}
		seen[target.ID] = true
	}
	if len(fields) > 0 {
		return &domain.ValidationError{Fields: fields}
	}
	return nil
}

func (s *AdminInventoryService) removeManyAttempt(ctx context.Context, targets []domain.RemoveItemTarget,
	actor domain.Actor, correlationID, sourceIP string) (domain.BulkRemoveResult, error) {
	tx, err := s.store.BeginAdminWrite(ctx)
	if err != nil {
		return domain.BulkRemoveResult{}, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	at := s.now().UTC()
	items := make([]domain.AdminInventoryItem, 0, len(targets))
	for _, target := range targets {
		current, err := tx.LockInventory(ctx, target.ID)
		if err != nil {
			return domain.BulkRemoveResult{}, err
		}
		if current.DeletedFg {
			return domain.BulkRemoveResult{}, fmt.Errorf("%w: a selected record is already removed", domain.ErrConflict)
		}
		if !sameModAt(current.ModAt, target.ExpectedModAt) {
			return domain.BulkRemoveResult{}, fmt.Errorf("%w: a selected record changed since the page was loaded", domain.ErrConflict)
		}
		if err := tx.SetDeleted(ctx, current.ID, true, at, actor); err != nil {
			return domain.BulkRemoveResult{}, err
		}
		entry, err := stateAudit(current, true, actor, at, correlationID, sourceIP)
		if err != nil {
			return domain.BulkRemoveResult{}, err
		}
		if err := tx.InsertAudit(ctx, entry); err != nil {
			return domain.BulkRemoveResult{}, err
		}
		current.DeletedFg = true
		current.ModAt = at
		current.ModBy = actor.UserName
		actorID := actor.UserID
		current.ModByUserID = &actorID
		items = append(items, current)
	}
	if err := tx.Commit(); err != nil {
		return domain.BulkRemoveResult{}, err
	}
	committed = true
	return domain.BulkRemoveResult{Items: items, Removed: len(items)}, nil
}

// ListAudit validates the audit filters and reads one page newest-first.
func (s *AdminInventoryService) ListAudit(ctx context.Context, filter domain.AuditFilter) (domain.AuditPage, error) {
	if err := s.requireReady(); err != nil {
		return domain.AuditPage{}, err
	}
	normalized, err := NormalizeAuditFilter(filter)
	if err != nil {
		return domain.AuditPage{}, err
	}
	return s.store.ListAudit(ctx, normalized)
}

// GetAudit reads one audit entry by ID.
func (s *AdminInventoryService) GetAudit(ctx context.Context, id int64) (domain.AuditEntry, error) {
	if err := s.requireReady(); err != nil {
		return domain.AuditEntry{}, err
	}
	if id <= 0 {
		return domain.AuditEntry{}, domain.ErrNotFound
	}
	return s.store.GetAudit(ctx, id)
}

// NormalizeAuditFilter validates exact-value filters and paging bounds. Date
// bounds are inclusive UTC calendar dates; To becomes an exclusive next day in
// the repository.
func NormalizeAuditFilter(filter domain.AuditFilter) (domain.AuditFilter, error) {
	fields := make(map[string]string)
	filter.EventType = strings.TrimSpace(filter.EventType)
	if filter.EventType != "" && !auditEventAllowed(filter.EventType) {
		fields["eventType"] = "eventType must be one of the approved values"
	}
	filter.User = strings.TrimSpace(filter.User)
	if len([]rune(filter.User)) > 64 {
		fields["user"] = "user must be at most 64 characters"
	}
	filter.Table = strings.TrimSpace(filter.Table)
	if len([]rune(filter.Table)) > 64 {
		fields["table"] = "table must be at most 64 characters"
	}
	filter.RecordID = strings.TrimSpace(filter.RecordID)
	if len([]rune(filter.RecordID)) > 64 {
		fields["recordId"] = "recordId must be at most 64 characters"
	}
	filter.CorrelationID = strings.TrimSpace(filter.CorrelationID)
	if len([]rune(filter.CorrelationID)) > 36 {
		fields["correlationId"] = "correlationId must be at most 36 characters"
	}
	if filter.From != nil && filter.To != nil && filter.To.Before(*filter.From) {
		fields["to"] = "to must not be before from"
	}
	if filter.Page < 1 || filter.Page > AdminMaxPage {
		fields["page"] = fmt.Sprintf("page must be between 1 and %d", AdminMaxPage)
	}
	if filter.PageSize < 1 || filter.PageSize > AdminAuditMaxPageSize {
		fields["pageSize"] = fmt.Sprintf("pageSize must be between 1 and %d", AdminAuditMaxPageSize)
	}
	if len(fields) > 0 {
		return domain.AuditFilter{}, &domain.ValidationError{Fields: fields}
	}
	return filter, nil
}

func auditEventAllowed(eventType string) bool {
	for _, candidate := range AdminAuditEventTypes {
		if candidate == eventType {
			return true
		}
	}
	return false
}
