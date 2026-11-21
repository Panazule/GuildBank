package service

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/Panazule/guildbank/internal/domain"
)

type fakeItemMetadataReader struct {
	items     map[uint64]domain.Item
	err       error
	batchIDs  []uint64
	batchCall int
}

func (f *fakeItemMetadataReader) GetItemByEQUID(_ context.Context, equid uint64) (domain.Item, error) {
	if f.err != nil {
		return domain.Item{}, f.err
	}
	item, ok := f.items[equid]
	if !ok {
		return domain.Item{}, domain.ErrNotFound
	}
	return item, nil
}

func (f *fakeItemMetadataReader) GetItemsByEQUIDs(_ context.Context, equids []uint64) (map[uint64]domain.Item, error) {
	f.batchCall++
	f.batchIDs = append([]uint64(nil), equids...)
	if f.err != nil {
		return nil, f.err
	}
	result := make(map[uint64]domain.Item)
	for _, id := range equids {
		if item, ok := f.items[id]; ok {
			result[id] = item
		}
	}
	return result, nil
}

func approvedItemFixtures() map[uint64]domain.Item {
	return map[uint64]domain.Item{
		25210: {EQUID: 25210, Name: "Blade of Carnage", ClassList: 157, RaceList: 65535},
		10802: {EQUID: 10802, Name: "Flayed Skin Cap", ClassList: 1040, RaceList: 5},
	}
}

func TestItemMetadataServiceGeneratesApprovedFixtures(t *testing.T) {
	store := &fakeItemMetadataReader{items: approvedItemFixtures()}
	svc := NewItemMetadataService(store)

	tests := []struct {
		ref         domain.ItemReference
		wantName    string
		wantClasses string
		wantRaces   string
	}{
		{
			ref:         domain.ItemReference{EQUID: 25210, Name: "stale imported name"},
			wantName:    "Blade of Carnage",
			wantClasses: "WAR,PAL,RNG,SHD,BRD",
			wantRaces:   "ALL",
		},
		{
			ref:         domain.ItemReference{EQUID: 10802, Name: "stale imported name"},
			wantName:    "Flayed Skin Cap",
			wantClasses: "SHD,NEC",
			wantRaces:   "Human,Erudite",
		},
	}
	for _, tc := range tests {
		t.Run(tc.wantName, func(t *testing.T) {
			got, err := svc.Resolve(context.Background(), tc.ref)
			if err != nil {
				t.Fatalf("Resolve: %v", err)
			}
			if got.EQUID != tc.ref.EQUID || got.Name != tc.wantName {
				t.Fatalf("identity = %d %q, want %d %q", got.EQUID, got.Name, tc.ref.EQUID, tc.wantName)
			}
			if got.ClassString != tc.wantClasses || got.RaceString != tc.wantRaces {
				t.Fatalf("decoded = %q / %q, want %q / %q", got.ClassString, got.RaceString, tc.wantClasses, tc.wantRaces)
			}
			if got.MetadataMissing {
				t.Fatal("MetadataMissing = true for a resolved fixture")
			}
			if len(got.Warnings) != 0 {
				t.Fatalf("warnings = %+v, want none", got.Warnings)
			}
		})
	}
}

func TestItemMetadataServiceMissingMetadataPreservesIdentity(t *testing.T) {
	svc := NewItemMetadataService(&fakeItemMetadataReader{items: approvedItemFixtures()})

	got, err := svc.Resolve(context.Background(), domain.ItemReference{EQUID: 999999, Name: "Unresolved Widget"})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got.EQUID != 999999 || got.Name != "Unresolved Widget" {
		t.Fatalf("identity = %d %q, want 999999 %q", got.EQUID, got.Name, "Unresolved Widget")
	}
	if got.ClassString != "" || got.RaceString != "" {
		t.Fatalf("decoded = %q / %q, want both blank", got.ClassString, got.RaceString)
	}
	if !got.MetadataMissing {
		t.Fatal("MetadataMissing = false, want true")
	}
	if len(got.Warnings) != 1 {
		t.Fatalf("warnings = %+v, want exactly one", got.Warnings)
	}
	warning := got.Warnings[0]
	if warning.Code != ItemWarningMetadataMissing {
		t.Fatalf("warning code = %q, want %q", warning.Code, ItemWarningMetadataMissing)
	}
	if warning.Field != "EQUID" || warning.EQUID != 999999 || warning.Detail == "" {
		t.Fatalf("warning = %+v, want EQUID/999999/detail", warning)
	}
}
func TestItemMetadataServiceCarriesMaskWarnings(t *testing.T) {
	store := &fakeItemMetadataReader{items: map[uint64]domain.Item{
		7: {
			EQUID:     7,
			Name:      "Wide Mask",
			ClassList: uint64(1)<<16 | 157,
			RaceList:  uint64(1)<<20 | 5,
		},
	}}
	got, err := NewItemMetadataService(store).Resolve(context.Background(),
		domain.ItemReference{EQUID: 7, Name: "Wide Mask"})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got.ClassString != "WAR,PAL,RNG,SHD,BRD" || got.RaceString != "Human,Erudite" {
		t.Fatalf("decoded = %q / %q", got.ClassString, got.RaceString)
	}
	if got.MetadataMissing {
		t.Fatal("MetadataMissing = true for a resolved item")
	}
	if len(got.Warnings) != 2 {
		t.Fatalf("warnings = %+v, want class and race warnings", got.Warnings)
	}
	for _, warning := range got.Warnings {
		if warning.Code != MaskWarningUndefinedHighBits {
			t.Fatalf("warning code = %q, want %q", warning.Code, MaskWarningUndefinedHighBits)
		}
		if warning.EQUID != 7 {
			t.Fatalf("warning EQUID = %d, want 7", warning.EQUID)
		}
	}
}

func TestItemMetadataServiceRejectsZeroEQUIDBeforeReading(t *testing.T) {
	store := &fakeItemMetadataReader{items: approvedItemFixtures()}
	svc := NewItemMetadataService(store)

	_, err := svc.Resolve(context.Background(), domain.ItemReference{EQUID: 0, Name: "No Identifier"})
	if !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("Resolve(zero EQUID) err = %v, want validation", err)
	}
	if store.batchCall != 0 {
		t.Fatalf("source was read %d times for invalid input", store.batchCall)
	}
}

func TestItemMetadataServiceUsesSourceNameWithoutImportedFallback(t *testing.T) {
	store := &fakeItemMetadataReader{items: approvedItemFixtures()}
	got, err := NewItemMetadataService(store).Resolve(context.Background(), domain.ItemReference{EQUID: 25210})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got.Name != "Blade of Carnage" || got.MetadataMissing {
		t.Fatalf("result = %+v, want resolved source name", got)
	}
}

func TestItemMetadataServiceRequiresNameForMissingMetadata(t *testing.T) {
	store := &fakeItemMetadataReader{}
	_, err := NewItemMetadataService(store).Resolve(context.Background(), domain.ItemReference{EQUID: 42})
	if !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("Resolve(missing without name) err = %v, want validation", err)
	}
	if store.batchCall != 1 {
		t.Fatalf("batch calls = %d, want 1", store.batchCall)
	}
}

func TestItemMetadataServicePropagatesSourceErrors(t *testing.T) {
	want := errors.New("source unavailable")
	svc := NewItemMetadataService(&fakeItemMetadataReader{err: want})
	_, err := svc.Resolve(context.Background(), domain.ItemReference{EQUID: 1, Name: "Item"})
	if !errors.Is(err, want) {
		t.Fatalf("err = %v, want %v", err, want)
	}
}

func TestItemMetadataServiceResolveBatchPreservesOrderAndMissingRows(t *testing.T) {
	store := &fakeItemMetadataReader{items: approvedItemFixtures()}
	svc := NewItemMetadataService(store)
	refs := []domain.ItemReference{
		{EQUID: 10802, Name: "Flayed Skin Cap"},
		{EQUID: 888888, Name: "Missing One"},
		{EQUID: 25210, Name: "Blade of Carnage"},
		{EQUID: 10802, Name: "Flayed Skin Cap duplicate"},
	}

	got, err := svc.ResolveBatch(context.Background(), refs)
	if err != nil {
		t.Fatalf("ResolveBatch: %v", err)
	}
	if len(got) != len(refs) {
		t.Fatalf("results = %d, want %d", len(got), len(refs))
	}
	want := []struct {
		equid   uint64
		name    string
		missing bool
		classes string
		races   string
	}{
		{10802, "Flayed Skin Cap", false, "SHD,NEC", "Human,Erudite"},
		{888888, "Missing One", true, "", ""},
		{25210, "Blade of Carnage", false, "WAR,PAL,RNG,SHD,BRD", "ALL"},
		{10802, "Flayed Skin Cap", false, "SHD,NEC", "Human,Erudite"},
	}
	for i, w := range want {
		if got[i].EQUID != w.equid || got[i].Name != w.name ||
			got[i].MetadataMissing != w.missing ||
			got[i].ClassString != w.classes || got[i].RaceString != w.races {
			t.Fatalf("result %d = %+v, want %+v", i, got[i], w)
		}
	}
	if store.batchCall != 1 {
		t.Fatalf("batch calls = %d, want 1", store.batchCall)
	}
	if !reflect.DeepEqual(store.batchIDs, []uint64{10802, 888888, 25210, 10802}) {
		t.Fatalf("batch ids = %v", store.batchIDs)
	}
}

func TestItemMetadataServiceEmptyBatch(t *testing.T) {
	got, err := NewItemMetadataService(&fakeItemMetadataReader{}).ResolveBatch(context.Background(), nil)
	if err != nil {
		t.Fatalf("ResolveBatch: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("results = %v, want empty", got)
	}
}

func TestMetadataMissingWarningIsStructured(t *testing.T) {
	got, err := NewItemMetadataService(&fakeItemMetadataReader{}).Resolve(context.Background(),
		domain.ItemReference{EQUID: 42, Name: "Item"})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(got.Warnings) != 1 || !strings.Contains(got.Warnings[0].Detail, "absent") {
		t.Fatalf("warnings = %+v", got.Warnings)
	}
}
