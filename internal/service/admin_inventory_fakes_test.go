package service

import (
	"context"
	"errors"
	"sort"
	"time"

	"github.com/Panazule/guildbank/internal/domain"
)

type fakeAdmin struct {
	ready        bool
	nextID       int64
	items        map[int64]domain.AdminInventoryItem
	audits       []domain.AuditEntry
	movements    []domain.InventoryMovement
	stackability map[uint64]domain.Stackability
	groups       []string
	failAudit    bool
	committed    int
	rolled       int
	// backupItems/backupAudits/backupMovements make the fake honour
	// AdminWriteTx rollback so an all-or-nothing service path can be asserted
	// without a live database.
	backupItems     map[int64]domain.AdminInventoryItem
	backupAudits    []domain.AuditEntry
	backupMovements []domain.InventoryMovement
	inTx            bool
}

func newFakeAdmin() *fakeAdmin {
	return &fakeAdmin{ready: true, nextID: 1, items: map[int64]domain.AdminInventoryItem{}}
}

func (f *fakeAdmin) Ready() bool { return f.ready }

func (f *fakeAdmin) ListAdminInventory(ctx context.Context, filter domain.AdminInventoryFilter) (domain.AdminInventoryPage, error) {
	items := make([]domain.AdminInventoryItem, 0, len(f.items))
	for _, item := range f.items {
		items = append(items, item)
	}
	return domain.AdminInventoryPage{
		Records: items, Total: len(items), Page: filter.Page, PageSize: filter.PageSize, Filter: filter,
	}, nil
}

func (f *fakeAdmin) GetAdminInventory(ctx context.Context, id int64) (domain.AdminInventoryItem, error) {
	item, ok := f.items[id]
	if !ok {
		return domain.AdminInventoryItem{}, domain.ErrNotFound
	}
	return item, nil
}

func (f *fakeAdmin) ListLocationGroups(ctx context.Context) ([]string, error) {
	return f.groups, nil
}

func (f *fakeAdmin) BeginAdminWrite(ctx context.Context) (AdminWriteTx, error) {
	f.backupItems = make(map[int64]domain.AdminInventoryItem, len(f.items))
	for id, item := range f.items {
		f.backupItems[id] = item
	}
	f.backupAudits = append([]domain.AuditEntry(nil), f.audits...)
	f.backupMovements = append([]domain.InventoryMovement(nil), f.movements...)
	f.inTx = true
	return f, nil
}

func (f *fakeAdmin) SumActiveQuantity(ctx context.Context, filter domain.AdminInventoryFilter) (uint64, error) {
	var total uint64
	for _, item := range f.items {
		if item.DeletedFg {
			continue
		}
		total += uint64(item.Quantity)
	}
	return total, nil
}

func (f *fakeAdmin) ListMovements(ctx context.Context, filter domain.MovementFilter) (domain.MovementPage, error) {
	movements := make([]domain.InventoryMovement, 0, len(f.movements))
	for _, movement := range f.movements {
		if filter.GuildBankItemID > 0 && movement.GuildBankItemID != filter.GuildBankItemID {
			continue
		}
		if filter.EQUID > 0 && movement.EQUID != filter.EQUID {
			continue
		}
		if filter.MovementType != "" && movement.MovementType != filter.MovementType {
			continue
		}
		if filter.Recipient != "" && movement.Recipient != filter.Recipient {
			continue
		}
		if filter.CorrelationID != "" && movement.CorrelationID != filter.CorrelationID {
			continue
		}
		movements = append(movements, movement)
	}
	return domain.MovementPage{
		Movements: movements, Total: len(movements), Page: filter.Page, PageSize: filter.PageSize,
	}, nil
}

func (f *fakeAdmin) GetMovement(ctx context.Context, movementID int64) (domain.InventoryMovement, error) {
	for _, movement := range f.movements {
		if movement.MovementID == movementID {
			return movement, nil
		}
	}
	return domain.InventoryMovement{}, domain.ErrNotFound
}

func (f *fakeAdmin) LookupStackability(ctx context.Context, equids []uint64) (map[uint64]domain.Stackability, error) {
	result := make(map[uint64]domain.Stackability, len(equids))
	for _, equid := range equids {
		value, ok := f.stackability[equid]
		if !ok {
			value = domain.StackabilityUnknown
		}
		result[equid] = value
	}
	return result, nil
}

func (f *fakeAdmin) ListAudit(ctx context.Context, filter domain.AuditFilter) (domain.AuditPage, error) {
	return domain.AuditPage{Entries: f.audits, Total: len(f.audits), Page: 1, PageSize: 50}, nil
}

func (f *fakeAdmin) GetAudit(ctx context.Context, id int64) (domain.AuditEntry, error) {
	for _, entry := range f.audits {
		if entry.AuditID == id {
			return entry, nil
		}
	}
	return domain.AuditEntry{}, domain.ErrNotFound
}

func (f *fakeAdmin) LockInventory(ctx context.Context, id int64) (domain.AdminInventoryItem, error) {
	item, ok := f.items[id]
	if !ok {
		return domain.AdminInventoryItem{}, domain.ErrNotFound
	}
	return item, nil
}

func (f *fakeAdmin) FindActiveSpells(ctx context.Context, equid uint64) ([]domain.AdminInventoryItem, error) {
	matches := make([]domain.AdminInventoryItem, 0, 1)
	ids := make([]int64, 0, len(f.items))
	for id := range f.items {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	for _, id := range ids {
		item := f.items[id]
		if item.DeletedFg || item.ItemType != domain.ItemTypeSpell || item.EQUID != equid {
			continue
		}
		matches = append(matches, item)
	}
	return matches, nil
}

func (f *fakeAdmin) InsertInventory(ctx context.Context, rows []domain.GuildBankItem) ([]int64, error) {
	ids := make([]int64, 0, len(rows))
	for _, row := range rows {
		id := f.nextID
		f.nextID++
		f.items[id] = adminItemFromRow(row, id)
		ids = append(ids, id)
	}
	return ids, nil
}

func (f *fakeAdmin) AddSpellQuantity(ctx context.Context, id int64, qty uint32, at time.Time, actor domain.Actor) error {
	item := f.items[id]
	item.Quantity += qty
	item.ModAt = at
	item.ModBy = actor.UserName
	f.items[id] = item
	return nil
}

func (f *fakeAdmin) UpdateInventory(ctx context.Context, in domain.AdminUpdateRow) error {
	item := f.items[in.ID]
	if in.Quantity != nil {
		item.Quantity = *in.Quantity
	}
	item.SourceLocation = in.SourceLocation
	item.LocationGroup = in.LocationGroup
	item.LocationSort = in.LocationSort
	item.GivenTo = in.GivenTo
	if in.DateOpen != nil {
		item.DateOpen = *in.DateOpen
	}
	if in.DateBazaar != nil {
		item.DateBazaar = *in.DateBazaar
	}
	item.ModAt = in.ModAt
	item.ModBy = in.Actor.UserName
	f.items[in.ID] = item
	return nil
}

func (f *fakeAdmin) SetDeleted(ctx context.Context, id int64, deleted bool, at time.Time, actor domain.Actor) error {
	item := f.items[id]
	item.DeletedFg = deleted
	item.ModAt = at
	item.ModBy = actor.UserName
	f.items[id] = item
	return nil
}

func (f *fakeAdmin) InsertMovement(ctx context.Context, movement domain.InventoryMovement) (int64, error) {
	if err := movement.Validate(); err != nil {
		return 0, err
	}
	movement.MovementID = int64(len(f.movements) + 1)
	f.movements = append(f.movements, movement)
	return movement.MovementID, nil
}

func (f *fakeAdmin) InsertAudit(ctx context.Context, entry domain.AuditEntry) error {
	if f.failAudit {
		return errors.New("audit insert failed")
	}
	entry.AuditID = int64(len(f.audits) + 1)
	f.audits = append(f.audits, entry)
	return nil
}

func (f *fakeAdmin) Commit() error {
	f.committed++
	f.inTx = false
	f.backupItems = nil
	f.backupAudits = nil
	f.backupMovements = nil
	return nil
}

func (f *fakeAdmin) Rollback() error {
	f.rolled++
	if f.inTx {
		if f.backupItems != nil {
			f.items = f.backupItems
		}
		f.audits = f.backupAudits
		if f.backupMovements != nil {
			f.movements = f.backupMovements
		}
	}
	f.inTx = false
	f.backupItems = nil
	f.backupAudits = nil
	f.backupMovements = nil
	return nil
}

type fakeSettings map[string]string

func (f fakeSettings) Values(ctx context.Context) (map[string]string, error) {
	return map[string]string(f), nil
}

type fakeMetadata map[uint64]ItemMetadata

func (f fakeMetadata) Resolve(ctx context.Context, ref domain.ItemReference) (ItemMetadata, error) {
	meta, ok := f[ref.EQUID]
	if !ok {
		return ItemMetadata{}, domain.NewValidationError("EQUID", "missing metadata fixture")
	}
	return meta, nil
}

func testAdminService(store *fakeAdmin, metadata fakeMetadata, now time.Time) *AdminInventoryService {
	if metadata == nil {
		metadata = fakeMetadata{
			100: {EQUID: 100, Name: "Sword", ClassString: "WAR", RaceString: "ALL"},
			200: {EQUID: 200, Name: "Clarity", ClassString: "CLR", RaceString: "ALL"},
		}
	}
	return NewAdminInventoryService(store,
		fakeSettings{"GuildOpenDays": "14", "BazaarDays": "30"},
		metadata, func() time.Time { return now }, nil)
}

func testActor() domain.Actor {
	return domain.Actor{UserID: 1, UserName: "admin", IsAdmin: true}
}
