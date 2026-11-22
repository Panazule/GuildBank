package mariadb

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/Panazule/guildbank/internal/domain"
)

func TestConsolidationAuditWritesSurvivorAndAbsorbedRows(t *testing.T) {
	group := consolidationGroup{
		survivorID: 5, rows: 2, quantity: 63, survivorQuantity: 55, equid: 10,
	}
	members := []consolidationMember{{id: 5, quantity: 55}, {id: 9, quantity: 8}}
	at := time.Date(2026, 11, 22, 12, 0, 0, 0, time.UTC)
	actor := domain.Actor{UserName: "system"}
	entries, err := consolidationAudit(group, members, []int64{9}, actor, at)
	if err != nil {
		t.Fatalf("consolidationAudit: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("entries = %d, want 2", len(entries))
	}
	if entries[0].EventType != "ITEM_UPDATED" || entries[0].RecordID != "5" ||
		entries[0].Notes != "quantity consolidated from 2 rows" {
		t.Fatalf("survivor audit = %+v", entries[0])
	}
	if entries[1].EventType != "ITEM_REMOVED" || entries[1].RecordID != "9" ||
		entries[1].Notes != "consolidated into 5" {
		t.Fatalf("absorbed audit = %+v", entries[1])
	}
	var oldValues, newValues map[string]any
	if err := json.Unmarshal(entries[0].OldValues, &oldValues); err != nil {
		t.Fatalf("survivor old values: %v", err)
	}
	if err := json.Unmarshal(entries[0].NewValues, &newValues); err != nil {
		t.Fatalf("survivor new values: %v", err)
	}
	if oldValues["quantity"] != float64(55) || newValues["quantity"] != float64(63) {
		t.Fatalf("survivor quantity pair = %v -> %v", oldValues, newValues)
	}
	absorbedIDs, ok := newValues["absorbedIds"].([]any)
	if !ok || len(absorbedIDs) != 1 || absorbedIDs[0] != float64(9) {
		t.Fatalf("absorbedIds = %v", newValues["absorbedIds"])
	}
	newValues = nil
	if err := json.Unmarshal(entries[1].NewValues, &newValues); err != nil {
		t.Fatalf("absorbed new values: %v", err)
	}
	if newValues["deletedFg"] != true || newValues["consolidatedInto"] != float64(5) {
		t.Fatalf("absorbed new values = %v", newValues)
	}
}
