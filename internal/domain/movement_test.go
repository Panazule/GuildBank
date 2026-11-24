package domain

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestMovementTypeAllowlist(t *testing.T) {
	for _, movementType := range MovementTypes {
		if !movementType.Valid() {
			t.Fatalf("%q should be valid", movementType)
		}
	}
	if MovementType("TRANSFER").Valid() {
		t.Fatal("unapproved movement type accepted")
	}
	if !MovementIssue.FirstRelease() || !MovementReturn.FirstRelease() {
		t.Fatal("ISSUE and RETURN must be first-release types")
	}
	if MovementMove.FirstRelease() || MovementAdjust.FirstRelease() {
		t.Fatal("MOVE and ADJUST must remain deferred")
	}
	if !MovementIssue.RequiresRecipient() || MovementReturn.RequiresRecipient() {
		t.Fatal("recipient requirement is wrong")
	}
}

// TestStackabilityValid pins the descriptive classification allowlist. Partial
// issuance is deliberately not a method on this type: the rule is the on-hand
// lot quantity, and stackability is advisory display only.
func TestStackabilityValid(t *testing.T) {
	for _, value := range Stackabilities {
		if !value.Valid() {
			t.Fatalf("%q should be valid", value)
		}
	}
	if Stackability("maybe").Valid() {
		t.Fatal("unapproved stackability accepted")
	}
}

func TestInventoryMovementValidate(t *testing.T) {
	actorID := int64(7)
	valid := InventoryMovement{
		GuildBankItemID:   12,
		EQUID:             25210,
		MovementType:      MovementIssue,
		Quantity:          10,
		Recipient:         "Recipient",
		ActorUserID:       &actorID,
		ActorUserName:     "admin",
		OccurredAt:        time.Date(2026, 11, 22, 12, 0, 0, 0, time.UTC),
		RemainingQuantity: 45,
	}
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid movement rejected: %v", err)
	}

	tests := []struct {
		name   string
		mutate func(*InventoryMovement)
		field  string
	}{
		{"lot", func(m *InventoryMovement) { m.GuildBankItemID = 0 }, "guildBankItemId"},
		{"equid", func(m *InventoryMovement) { m.EQUID = 0 }, "equid"},
		{"type", func(m *InventoryMovement) { m.MovementType = "BAD" }, "movementType"},
		{"quantity", func(m *InventoryMovement) { m.Quantity = 0 }, "quantity"},
		{"recipient", func(m *InventoryMovement) { m.Recipient = " " }, "recipient"},
		{"actor", func(m *InventoryMovement) { m.ActorUserName = "" }, "actorUserName"},
		{"occurred", func(m *InventoryMovement) { m.OccurredAt = time.Time{} }, "occurredAt"},
		{"related", func(m *InventoryMovement) { zero := int64(0); m.RelatedItemID = &zero }, "relatedItemId"},
		{"idempotency", func(m *InventoryMovement) { m.IdempotencyKey = strings.Repeat("x", 65) }, "idempotencyKey"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			movement := valid
			tc.mutate(&movement)
			err := movement.Validate()
			if err == nil {
				t.Fatal("invalid movement accepted")
			}
			var validation *ValidationError
			if !errors.As(err, &validation) {
				t.Fatalf("want ValidationError, got %T", err)
			}
			if _, ok := validation.Fields[tc.field]; !ok {
				t.Fatalf("missing field %q in %#v", tc.field, validation.Fields)
			}
		})
	}
}

func TestInventoryMovementStringOmitsFreeText(t *testing.T) {
	movement := InventoryMovement{
		MovementID: 3, MovementType: MovementIssue, GuildBankItemID: 12, Quantity: 2,
		Recipient: "secret-recipient", Note: "private note",
	}
	got := movement.String()
	if !strings.Contains(got, "movement 3") || !strings.Contains(got, "ISSUE") {
		t.Fatalf("unexpected String output %q", got)
	}
	if strings.Contains(got, "secret-recipient") || strings.Contains(got, "private note") {
		t.Fatalf("String leaked free text: %q", got)
	}
}
