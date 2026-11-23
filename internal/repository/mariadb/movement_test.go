package mariadb

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Panazule/guildbank/internal/domain"
)

func TestNormalizeMovementFilter(t *testing.T) {
	normalized, err := normalizeMovementFilter(domain.MovementFilter{})
	if err != nil {
		t.Fatalf("default filter rejected: %v", err)
	}
	if normalized.Page != 1 || normalized.PageSize != movementDefaultPageSize {
		t.Fatalf("defaults = page %d size %d", normalized.Page, normalized.PageSize)
	}

	from := time.Date(2026, 11, 22, 12, 0, 0, 0, time.UTC)
	to := from.Add(time.Hour)
	normalized, err = normalizeMovementFilter(domain.MovementFilter{
		GuildBankItemID: 12, EQUID: 25210, MovementType: domain.MovementIssue,
		Recipient: "  Recipient  ", CorrelationID: " corr ", From: from, To: to,
		Page: 2, PageSize: 25,
	})
	if err != nil {
		t.Fatalf("valid filter rejected: %v", err)
	}
	if normalized.Recipient != "Recipient" || normalized.CorrelationID != "corr" {
		t.Fatalf("trim failed: %#v", normalized)
	}

	if _, err := normalizeMovementFilter(domain.MovementFilter{PageSize: movementMaxPage + 1}); err == nil {
		t.Fatal("oversized page accepted")
	}
	if _, err := normalizeMovementFilter(domain.MovementFilter{MovementType: "BAD"}); err == nil {
		t.Fatal("unsafe movement type accepted")
	}
	if _, err := normalizeMovementFilter(domain.MovementFilter{From: to, To: from}); err == nil {
		t.Fatal("reversed date range accepted")
	}
}

func TestMovementWhereUsesParameterizedPredicates(t *testing.T) {
	from := time.Date(2026, 11, 22, 12, 0, 0, 0, time.UTC)
	to := from.Add(time.Hour)
	where, args := movementWhere(domain.MovementFilter{
		GuildBankItemID: 12, EQUID: 25210, MovementType: domain.MovementIssue,
		Recipient:     "Recipient'; DROP TABLE InventoryMovement; --",
		CorrelationID: "corr", From: from, To: to,
	})
	for _, fragment := range []string{
		"GuildBankItemID = ?", "EQUID = ?", "MovementType = ?", "Recipient = ?",
		"CorrelationID = ?", "OccurredAt >= ?", "OccurredAt <= ?",
	} {
		if !strings.Contains(where, fragment) {
			t.Fatalf("where %q missing %q", where, fragment)
		}
	}
	if len(args) != 7 {
		t.Fatalf("args = %#v", args)
	}
	if strings.Contains(where, "DROP TABLE") {
		t.Fatalf("caller text leaked into SQL: %q", where)
	}

	empty, emptyArgs := movementWhere(domain.MovementFilter{})
	if empty != "1 = 1" || len(emptyArgs) != 0 {
		t.Fatalf("empty where = %q %#v", empty, emptyArgs)
	}
}

func TestMovementRepositoryIsAppendOnly(t *testing.T) {
	body, err := os.ReadFile("movement.go")
	if err != nil {
		t.Fatal(err)
	}
	source := string(body)
	for _, forbidden := range []string{
		"UPDATE InventoryMovement", "DELETE FROM InventoryMovement", "SELECT *",
	} {
		if strings.Contains(source, forbidden) {
			t.Fatalf("movement repository contains %q", forbidden)
		}
	}
	for _, required := range []string{
		"INSERT INTO InventoryMovement", "FROM InventoryMovement", "ORDER BY OccurredAt DESC, MovementID DESC",
	} {
		if !strings.Contains(source, required) {
			t.Fatalf("movement repository is missing %q", required)
		}
	}
}
