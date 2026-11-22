package service

import (
	"context"
	"errors"
	"sort"
	"time"

	"github.com/Panazule/guildbank/internal/domain"
)

type fakeAdmin struct {
	ready     bool
	nextID    int64
	items     map[int64]domain.AdminInventoryItem
	audits    []domain.AuditEntry
	groups    []string
	failAudit bool
	committed int
	rolled    int
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

func (f *fakeAdmin) BeginAdminWrite(ctx context.Context) (AdminWriteTx, error) { return f, nil }

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

func (f *fakeAdmin) InsertAudit(ctx context.Context, entry domain.AuditEntry) error {
	if f.failAudit {
		return errors.New("audit insert failed")
	}
	entry.AuditID = int64(len(f.audits) + 1)
	f.audits = append(f.audits, entry)
	return nil
}

func (f *fakeAdmin) Commit() error   { f.committed++; return nil }
func (f *fakeAdmin) Rollback() error { f.rolled++; return nil }

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
