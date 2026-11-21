package mariadb

import (
	"context"
	"os"
	"strings"
	"testing"
)

func TestUniqueEQUIDs(t *testing.T) {
	got := uniqueEQUIDs([]uint64{3, 1, 3, 2, 1})
	want := []uint64{3, 1, 2}
	if len(got) != len(want) {
		t.Fatalf("ids = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("ids = %v, want %v", got, want)
		}
	}
	if got := uniqueEQUIDs(nil); len(got) != 0 {
		t.Fatalf("uniqueEQUIDs(nil) = %v, want empty", got)
	}
}

func TestListItemsRejectsInvalidPageSize(t *testing.T) {
	for _, limit := range []int{-1, 0, MaxItemsPageSize + 1} {
		_, err := (&Store{}).ListItems(context.Background(), 0, limit)
		if err == nil || !strings.Contains(err.Error(), "page size") {
			t.Fatalf("ListItems(limit=%d) err = %v, want page-size error", limit, err)
		}
	}
}

// TestItemsRepositoryIsReadOnly keeps the runtime source contract visible:
// this repository may select metadata but must never mutate the externally
// owned table (Design.md L242-L244, SR-13).
func TestItemsRepositoryIsReadOnly(t *testing.T) {
	source, err := os.ReadFile("items.go")
	if err != nil {
		t.Fatal(err)
	}
	upper := strings.ToUpper(string(source))
	for _, verb := range []string{
		"INSERT ", "UPDATE ", "DELETE ", "REPLACE ", "CREATE ", "ALTER ", "DROP ", "TRUNCATE ",
	} {
		if strings.Contains(upper, verb) {
			t.Fatalf("items.go contains write statement %q; the source table is read-only", strings.TrimSpace(verb))
		}
	}
}
