package service

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/Panazule/guildbank/internal/domain"
)

// TestIssueLockedMatchesManualIssueMovement is the Phase 2 ship gate (§16):
// the extracted issueLocked must write exactly the row the manual Issue path
// wrote before the extraction, for both a partial issue and a whole-stack
// issue. The manual path runs through Issue; the direct path calls issueLocked
// inside a transaction and commits it itself.
func TestIssueLockedMatchesManualIssueMovement(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 11, 24, 12, 0, 0, 0, time.UTC)

	cases := []struct {
		name     string
		quantity uint32
	}{
		{name: "partial issue", quantity: 2},
		{name: "whole-stack issue", quantity: 5},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			input := func() domain.IssueItemInput {
				in := domain.IssueItemInput{
					GuildBankItemID: 1, Quantity: tc.quantity, Recipient: "Joe",
					Note: "for raid", IdempotencyKey: "issue-1", CorrelationID: "corr-1",
					SourceIP: "127.0.0.1",
				}
				in.ExpectedModAt = issuanceTestItem(1, 5, false, "").ModAt
				in.Actor = testActor()
				return in
			}

			manualStore := newFakeAdmin()
			manualStore.items[1] = issuanceTestItem(1, 5, false, "")
			manualSvc := testAdminService(manualStore, nil, now)
			manualResult, err := manualSvc.Issue(ctx, input())
			if err != nil {
				t.Fatalf("Issue: %v", err)
			}
			if manualResult.Movement == nil {
				t.Fatal("Issue returned no movement")
			}

			lockedStore := newFakeAdmin()
			lockedStore.items[1] = issuanceTestItem(1, 5, false, "")
			lockedSvc := testAdminService(lockedStore, nil, now)
			tx, err := lockedStore.BeginAdminWrite(ctx)
			if err != nil {
				t.Fatalf("BeginAdminWrite: %v", err)
			}
			if _, _, err := lockedSvc.issueLocked(ctx, tx, input()); err != nil {
				t.Fatalf("issueLocked: %v", err)
			}
			if lockedStore.committed != 0 {
				t.Fatal("issueLocked committed the caller's transaction")
			}
			if err := tx.Commit(); err != nil {
				t.Fatalf("Commit: %v", err)
			}

			if !reflect.DeepEqual(*manualResult.Movement, lockedStore.movements[0]) {
				t.Fatalf("movement mismatch:\nmanual = %+v\nlocked = %+v",
					*manualResult.Movement, lockedStore.movements[0])
			}
			if !reflect.DeepEqual(manualStore.items[1], lockedStore.items[1]) {
				t.Fatalf("lot mismatch:\nmanual = %+v\nlocked = %+v",
					manualStore.items[1], lockedStore.items[1])
			}
			if !reflect.DeepEqual(manualStore.audits, lockedStore.audits) {
				t.Fatalf("audit mismatch:\nmanual = %+v\nlocked = %+v",
					manualStore.audits, lockedStore.audits)
			}
		})
	}
}

// TestIssueLockedLeavesRollbackToCaller proves issueLocked does not commit:
// without the caller's commit, the lot, the movement and the audit row all
// roll back, so a future approval can add its request transition to the same
// atomic unit.
func TestIssueLockedLeavesRollbackToCaller(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 11, 24, 12, 0, 0, 0, time.UTC)
	store := newFakeAdmin()
	item := issuanceTestItem(1, 5, false, "")
	store.items[1] = item
	svc := testAdminService(store, nil, now)

	tx, err := store.BeginAdminWrite(ctx)
	if err != nil {
		t.Fatalf("BeginAdminWrite: %v", err)
	}
	in := domain.IssueItemInput{
		GuildBankItemID: 1, Quantity: 2, Recipient: "Joe",
		ExpectedModAt: item.ModAt, CorrelationID: "corr-1",
	}
	in.Actor = testActor()
	if _, _, err := svc.issueLocked(ctx, tx, in); err != nil {
		t.Fatalf("issueLocked: %v", err)
	}
	if store.committed != 0 {
		t.Fatal("issueLocked committed the caller's transaction")
	}
	if err := tx.Rollback(); err != nil {
		t.Fatalf("Rollback: %v", err)
	}
	if got := store.items[1].Quantity; got != 5 {
		t.Fatalf("lot quantity after rollback = %d, want 5", got)
	}
	if len(store.movements) != 0 || len(store.audits) != 0 {
		t.Fatalf("rolled-back rows survived: %d movements, %d audits",
			len(store.movements), len(store.audits))
	}
}
