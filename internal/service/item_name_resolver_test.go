package service

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Panazule/guildbank/internal/domain"
)

// nameReader is a map-backed ItemNameReader. Lookups are keyed by the exact
// normalized name; ci simulates a case-insensitive source collation.
type nameReader struct {
	byName map[string][]domain.Item
	ci     bool
	calls  int
	err    error
}

func (r *nameReader) FindItemsByName(_ context.Context, name string, limit int) ([]domain.Item, error) {
	r.calls++
	if r.err != nil {
		return nil, r.err
	}
	matches, ok := r.byName[name]
	if !ok && r.ci {
		for key, value := range r.byName {
			if strings.EqualFold(key, name) {
				matches = value
				break
			}
		}
	}
	if len(matches) > limit {
		matches = matches[:limit]
	}
	return matches, nil
}

func TestNormalizeItemName(t *testing.T) {
	table := map[string]string{
		"  Sword  ":              "Sword",
		"Ring\nof\tFire":         "Ring of Fire",
		"Swo\x00rd":              "Sword",
		"Multiple   spaces":      "Multiple spaces",
		"":                       "",
		"   \t\n  ":              "",
		"Ancient: Lcea's Lament": "Ancient: Lcea's Lament",
	}
	for input, want := range table {
		if got := NormalizeItemName(input); got != want {
			t.Fatalf("NormalizeItemName(%q) = %q, want %q", input, got, want)
		}
	}
	long := strings.Repeat("a", 200)
	if got := len([]rune(NormalizeItemName(long))); got != itemNameMaxRunes {
		t.Fatalf("long name truncated to %d runes, want %d", got, itemNameMaxRunes)
	}
}

func TestResolveUniqueName(t *testing.T) {
	reader := &nameReader{byName: map[string][]domain.Item{
		"Sword": {{EQUID: 100, Name: "Sword"}},
	}}
	resolver := NewItemNameResolver(reader)
	got, err := resolver.Resolve(context.Background(), "  Sword ")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got.Status != ItemNameStatusUnique || got.Match.EQUID != 100 {
		t.Fatalf("resolution = %+v, want a unique EQUID 100 match", got)
	}
	if got.CaseInsensitive {
		t.Fatal("exact-case match reported as case-insensitive")
	}
}

func TestResolveReportsCaseInsensitiveMatch(t *testing.T) {
	reader := &nameReader{ci: true, byName: map[string][]domain.Item{
		"sword": {{EQUID: 100, Name: "Sword"}},
	}}
	resolver := NewItemNameResolver(reader)
	got, err := resolver.Resolve(context.Background(), "SWORD")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got.Status != ItemNameStatusUnique || !got.CaseInsensitive {
		t.Fatalf("resolution = %+v, want a unique case-insensitive match", got)
	}
	if got.Match.Name != "Sword" {
		t.Fatalf("matched name = %q, want the stored casing", got.Match.Name)
	}
}

func TestResolveAmbiguousNameReturnsCandidates(t *testing.T) {
	reader := &nameReader{byName: map[string][]domain.Item{
		"Ring": {{EQUID: 10, Name: "Ring"}, {EQUID: 11, Name: "Ring"}, {EQUID: 12, Name: "Ring"}},
	}}
	resolver := NewItemNameResolver(reader)
	got, err := resolver.Resolve(context.Background(), "Ring")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got.Status != ItemNameStatusAmbiguous {
		t.Fatalf("status = %q, want ambiguous", got.Status)
	}
	if len(got.Candidates) != 3 {
		t.Fatalf("candidates = %d, want 3", len(got.Candidates))
	}
	if reader.calls != 2 {
		t.Fatalf("reader calls = %d, want 2 (probe + candidate list)", reader.calls)
	}
}

func TestResolveNotFoundAndEmpty(t *testing.T) {
	reader := &nameReader{}
	resolver := NewItemNameResolver(reader)
	got, err := resolver.Resolve(context.Background(), "Nothing")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got.Status != ItemNameStatusNotFound {
		t.Fatalf("status = %q, want not_found", got.Status)
	}
	empty, err := resolver.Resolve(context.Background(), "   ")
	if err != nil || empty.Status != ItemNameStatusNotFound {
		t.Fatalf("empty name = %+v, %v; want not_found and no error", empty, err)
	}
	if reader.calls != 1 {
		t.Fatalf("reader calls = %d, want 1 (the empty name must not query)", reader.calls)
	}
}

func TestResolveCachesResult(t *testing.T) {
	reader := &nameReader{byName: map[string][]domain.Item{
		"Sword": {{EQUID: 100, Name: "Sword"}},
	}}
	resolver := NewItemNameResolver(reader)
	for i := 0; i < 3; i++ {
		if _, err := resolver.Resolve(context.Background(), "Sword"); err != nil {
			t.Fatalf("Resolve: %v", err)
		}
	}
	if reader.calls != 1 {
		t.Fatalf("reader calls = %d, want 1 cached lookup", reader.calls)
	}
}

func TestResolveUnavailableWithoutReader(t *testing.T) {
	resolver := NewItemNameResolver(nil)
	if _, err := resolver.Resolve(context.Background(), "Sword"); !errors.Is(err, domain.ErrUnavailable) {
		t.Fatalf("Resolve error = %v, want ErrUnavailable", err)
	}
}

func TestResolvePropagatesReaderError(t *testing.T) {
	sentinel := errors.New("database is down")
	reader := &nameReader{err: sentinel}
	resolver := NewItemNameResolver(reader)
	if _, err := resolver.Resolve(context.Background(), "Sword"); !errors.Is(err, sentinel) {
		t.Fatalf("Resolve error = %v, want the reader error", err)
	}
}
