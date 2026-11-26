package service

import (
	"context"
	"errors"
	"testing"

	"github.com/Panazule/guildbank/internal/domain"
)

func TestAuditEventTypesForRole(t *testing.T) {
	if scope := AuditEventTypesFor(domain.RoleAdmin); scope != nil {
		t.Fatalf("administrator scope = %v, want unrestricted", scope)
	}
	officer := AuditEventTypesFor(domain.RoleOfficer)
	member := AuditEventTypesFor(domain.RoleMember)
	if len(officer) == 0 || len(member) == 0 {
		t.Fatalf("scope = officer %v / member %v, want the inventory allowlist", officer, member)
	}
	for _, scope := range [][]string{officer, member} {
		for _, forbidden := range UserAuditEventTypes {
			if auditEventIn(scope, forbidden) {
				t.Fatalf("non-administrator scope contains %s", forbidden)
			}
		}
		if !auditEventIn(scope, "ITEM_CREATED") || !auditEventIn(scope, "IMPORT_COMMITTED") {
			t.Fatalf("scope %v is missing an inventory or import event", scope)
		}
	}
}

func TestNormalizeAuditFilterRejectsOutOfScopeEventType(t *testing.T) {
	_, err := NormalizeAuditFilter(domain.AuditFilter{
		EventTypes: OfficerAuditEventTypes, EventType: "USER_CREATED", Page: 1, PageSize: 50,
	})
	var validation *domain.ValidationError
	if !errors.As(err, &validation) || validation.Fields["eventType"] == "" {
		t.Fatalf("out-of-scope filter = %v, want an eventType validation error", err)
	}

	// The same request is valid for an Administrator, whose scope is nil.
	if _, err := NormalizeAuditFilter(domain.AuditFilter{
		EventType: "USER_CREATED", Page: 1, PageSize: 50,
	}); err != nil {
		t.Fatalf("administrator filter = %v, want nil", err)
	}

	if _, err := NormalizeAuditFilter(domain.AuditFilter{
		EventTypes: OfficerAuditEventTypes, EventType: "ITEM_CREATED", Page: 1, PageSize: 50,
	}); err != nil {
		t.Fatalf("in-scope filter = %v, want nil", err)
	}
}

func TestGetAuditScopedHidesUserEventsFromOfficers(t *testing.T) {
	store := newFakeAdmin()
	store.audits = []domain.AuditEntry{
		{AuditID: 1, EventType: "ITEM_CREATED", TableName: "GuildBankItems"},
		{AuditID: 2, EventType: "USER_CREATED", TableName: "Users"},
	}
	svc := NewAdminInventoryService(store, nil, nil, nil, nil, nil)
	ctx := context.Background()

	if _, err := svc.GetAuditScoped(ctx, 2, AuditEventTypesFor(domain.RoleOfficer)); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("officer read of a user event = %v, want ErrNotFound", err)
	}
	if _, err := svc.GetAuditScoped(ctx, 2, AuditEventTypesFor(domain.RoleAdmin)); err != nil {
		t.Fatalf("administrator read of a user event = %v, want success", err)
	}
	if _, err := svc.GetAuditScoped(ctx, 1, AuditEventTypesFor(domain.RoleOfficer)); err != nil {
		t.Fatalf("officer read of an inventory event = %v, want success", err)
	}
}
