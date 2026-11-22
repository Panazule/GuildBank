package service

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/Panazule/guildbank/internal/domain"
)

type fakeInventoryStore struct {
	ready      bool
	records    []domain.InventoryRecord
	total      int
	err        error
	detailErr  error
	lastFilter domain.InventoryFilter
}

func (f *fakeInventoryStore) Ready() bool { return f.ready }

func (f *fakeInventoryStore) ListPublicInventory(_ context.Context, filter domain.InventoryFilter) (domain.InventoryRecordPage, error) {
	f.lastFilter = filter
	if f.err != nil {
		return domain.InventoryRecordPage{}, f.err
	}
	return domain.InventoryRecordPage{Records: f.records, Total: f.total}, nil
}

func (f *fakeInventoryStore) GetPublicInventory(_ context.Context, id int64) (domain.InventoryRecord, error) {
	if f.detailErr != nil {
		return domain.InventoryRecord{}, f.detailErr
	}
	for _, record := range f.records {
		if record.GuildBankItemID == id {
			return record, nil
		}
	}
	return domain.InventoryRecord{}, domain.ErrNotFound
}

type fakeInventorySettings struct {
	values map[string]string
	err    error
}

func (f fakeInventorySettings) Values(context.Context) (map[string]string, error) {
	return f.values, f.err
}

func testInventoryService(store InventoryStore, settings InventorySettingsReader, at time.Time) *InventoryService {
	return NewInventoryService(store, settings, func() time.Time { return at },
		slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func TestNormalizeInventoryFilter(t *testing.T) {
	tests := []struct {
		name       string
		filter     domain.InventoryFilter
		wantSearch string
		wantClass  string
		wantRace   string
		wantType   domain.ItemType
		wantSort   domain.InventorySort
		wantField  string
	}{
		{name: "defaults", filter: domain.InventoryFilter{Page: 1, PageSize: 50}, wantSort: domain.InventorySortName},
		{name: "trim and canonical", filter: domain.InventoryFilter{Search: "  Blade  ", Class: " war ", Race: "wood elf", ItemType: "spell", Sort: "NAME_DESC", Page: 2, PageSize: 25}, wantSearch: "Blade", wantClass: "WAR", wantRace: "Wood Elf", wantType: domain.ItemTypeSpell, wantSort: domain.InventorySortNameDesc},
		{name: "ALL means absent", filter: domain.InventoryFilter{Class: "all", Race: "ALL", ItemType: "all", Page: 1, PageSize: 50}, wantSort: domain.InventorySortName},
		{name: "bad class", filter: domain.InventoryFilter{Class: "XXX", Page: 1, PageSize: 50}, wantField: "class"},
		{name: "bad race", filter: domain.InventoryFilter{Race: "Martian", Page: 1, PageSize: 50}, wantField: "race"},
		{name: "bad type", filter: domain.InventoryFilter{ItemType: "Quest", Page: 1, PageSize: 50}, wantField: "itemType"},
		{name: "bad sort", filter: domain.InventoryFilter{Sort: "NameSort; DROP", Page: 1, PageSize: 50}, wantField: "sort"},
		{name: "bad page", filter: domain.InventoryFilter{Page: 0, PageSize: 50}, wantField: "page"},
		{name: "bad page cap", filter: domain.InventoryFilter{Page: PublicInventoryMaxPage + 1, PageSize: 50}, wantField: "page"},
		{name: "bad page size", filter: domain.InventoryFilter{Page: 1, PageSize: PublicInventoryMaxPageSize + 1}, wantField: "pageSize"},
		{name: "search boundary", filter: domain.InventoryFilter{Search: strings.Repeat("x", PublicInventoryMaxSearchRunes), Page: 1, PageSize: 50}, wantSearch: strings.Repeat("x", PublicInventoryMaxSearchRunes), wantSort: domain.InventorySortName},
		{name: "search over boundary", filter: domain.InventoryFilter{Search: strings.Repeat("x", PublicInventoryMaxSearchRunes+1), Page: 1, PageSize: 50}, wantField: "q"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := NormalizeInventoryFilter(tc.filter)
			if tc.wantField != "" {
				var validation *domain.ValidationError
				if !errors.As(err, &validation) || validation.Fields[tc.wantField] == "" {
					t.Fatalf("err = %v, want field %q", err, tc.wantField)
				}
				return
			}
			if err != nil {
				t.Fatalf("NormalizeInventoryFilter: %v", err)
			}
			if got.Search != tc.wantSearch || got.Class != tc.wantClass || got.Race != tc.wantRace ||
				got.ItemType != tc.wantType || got.Sort != tc.wantSort {
				t.Fatalf("normalized = %+v", got)
			}
		})
	}
}

func TestAvailabilityUTCBoundaries(t *testing.T) {
	open := time.Date(2026, 12, 5, 0, 0, 0, 0, time.UTC)
	bazaar := time.Date(2026, 12, 21, 0, 0, 0, 0, time.UTC)
	tests := []struct {
		name  string
		today time.Time
		want  domain.AvailabilityState
	}{
		{"day before", time.Date(2026, 12, 4, 23, 59, 0, 0, time.UTC), domain.AvailabilityUpcoming},
		{"exact open", time.Date(2026, 12, 5, 0, 0, 0, 0, time.UTC), domain.AvailabilityOpen},
		{"inside", time.Date(2026, 12, 10, 12, 0, 0, 0, time.UTC), domain.AvailabilityOpen},
		{"exact bazaar", time.Date(2026, 12, 21, 23, 59, 0, 0, time.UTC), domain.AvailabilityOpen},
		{"day after", time.Date(2026, 12, 22, 0, 0, 0, 0, time.UTC), domain.AvailabilityBazaar},
		{"non UTC date normalized", time.Date(2026, 12, 5, 0, 30, 0, 0, time.FixedZone("east", 3600)), domain.AvailabilityUpcoming},
		{"equal dates open", time.Date(2026, 12, 5, 8, 0, 0, 0, time.UTC), domain.AvailabilityOpen},
		{"equal dates next day", time.Date(2026, 12, 6, 8, 0, 0, 0, time.UTC), domain.AvailabilityBazaar},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			bazaarDate := bazaar
			if tc.name == "equal dates open" || tc.name == "equal dates next day" {
				bazaarDate = open
			}
			if got := Availability(tc.today, open, bazaarDate); got != tc.want {
				t.Fatalf("Availability = %q, want %q", got, tc.want)
			}
		})
	}
	invertedOpen := time.Date(2026, 12, 21, 0, 0, 0, 0, time.UTC)
	invertedBazaar := time.Date(2026, 12, 5, 0, 0, 0, 0, time.UTC)
	if got := Availability(time.Date(2026, 12, 10, 0, 0, 0, 0, time.UTC), invertedOpen, invertedBazaar); got != domain.AvailabilityUpcoming {
		t.Fatalf("inverted window before open = %q", got)
	}
	if got := Availability(time.Date(2026, 12, 22, 0, 0, 0, 0, time.UTC), invertedOpen, invertedBazaar); got != domain.AvailabilityBazaar {
		t.Fatalf("inverted window after open = %q", got)
	}
}

func TestBuildEQResourceURL(t *testing.T) {
	valid := "https://items.eqresource.com/items.php?id="
	if got := BuildEQResourceURL(valid, 28813); got == nil || *got != valid+"28813" {
		t.Fatalf("valid URL = %v", got)
	}
	invalid := []string{
		"http://items.eqresource.com/items.php?id=",
		"https://evil.example/items.php?id=",
		"https://items.eqresource.com/other.php?id=",
		"https://items.eqresource.com/items.php?id=1",
		"https://items.eqresource.com/items.php?id=#fragment",
		"https://user@items.eqresource.com/items.php?id=",
	}
	for _, base := range invalid {
		if got := BuildEQResourceURL(base, 1); got != nil {
			t.Fatalf("BuildEQResourceURL(%q) = %v, want nil", base, *got)
		}
	}
	if got := BuildEQResourceURL(valid, 0); got != nil {
		t.Fatalf("zero EQUID URL = %v, want nil", *got)
	}
}

func TestListPublicMapsProjectionAndSnapshotPage(t *testing.T) {
	record := domain.InventoryRecord{
		GuildBankItemID: 7, EQUID: 28813, Name: "Blade of Carnage", ItemType: domain.ItemTypeItem,
		Quantity: 1, ClassString: "WAR,CLR", RaceString: "ALL", MetadataMissing: false,
		DateAdded:  time.Date(2026, 11, 21, 0, 0, 0, 0, time.UTC),
		DateOpen:   time.Date(2026, 12, 5, 0, 0, 0, 0, time.UTC),
		DateBazaar: time.Date(2026, 12, 21, 0, 0, 0, 0, time.UTC),
	}
	store := &fakeInventoryStore{ready: true, records: []domain.InventoryRecord{record}, total: 101}
	svc := testInventoryService(store, fakeInventorySettings{values: map[string]string{"EQResourceBaseUrl": "https://items.eqresource.com/items.php?id="}},
		time.Date(2026, 11, 21, 23, 30, 0, 0, time.UTC))
	page, err := svc.ListPublic(context.Background(), domain.InventoryFilter{Page: 2, PageSize: 50})
	if err != nil {
		t.Fatalf("ListPublic: %v", err)
	}
	if page.Total != 101 || page.TotalPages != 3 || page.Page != 2 {
		t.Fatalf("page meta = %+v", page)
	}
	if len(page.Items) != 1 || page.Items[0].Availability != domain.AvailabilityUpcoming {
		t.Fatalf("items = %+v", page.Items)
	}
	if page.Items[0].EQResourceURL == nil || !strings.HasSuffix(*page.Items[0].EQResourceURL, "?id=28813") {
		t.Fatalf("URL = %v", page.Items[0].EQResourceURL)
	}
	if store.lastFilter.Class != "" || store.lastFilter.Race != "" || store.lastFilter.Sort != domain.InventorySortName {
		t.Fatalf("repository filter = %+v", store.lastFilter)
	}
}

func TestListPublicEmptySliceAndSettingsDegradation(t *testing.T) {
	store := &fakeInventoryStore{ready: true, records: nil, total: 0}
	svc := testInventoryService(store, fakeInventorySettings{err: errors.New("settings unavailable")}, time.Now())
	page, err := svc.ListPublic(context.Background(), domain.InventoryFilter{Page: 1, PageSize: 50})
	if err != nil {
		t.Fatalf("ListPublic: %v", err)
	}
	if page.Items == nil || len(page.Items) != 0 || page.TotalPages != 0 {
		t.Fatalf("page = %+v", page)
	}
}

func TestInventoryServiceDegradedAndNotFound(t *testing.T) {
	svc := NewInventoryService(nil, nil, nil, nil)
	if svc.Ready() {
		t.Fatal("nil store service is ready")
	}
	if _, err := svc.ListPublic(context.Background(), domain.InventoryFilter{Page: 1, PageSize: 50}); !errors.Is(err, domain.ErrUnavailable) {
		t.Fatalf("ListPublic err = %v", err)
	}
	store := &fakeInventoryStore{ready: true, detailErr: domain.ErrNotFound}
	svc = testInventoryService(store, fakeInventorySettings{}, time.Now())
	if _, err := svc.GetPublic(context.Background(), 1); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("GetPublic err = %v", err)
	}
	if _, err := svc.GetPublic(context.Background(), 0); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("GetPublic zero err = %v", err)
	}
}
