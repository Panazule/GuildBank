package api

import (
	"bytes"
	"net/http"
	"testing"
)

// The landing page shows the pending-approval count when it can read it, and a
// failed request-store count must not take the operational navigation down.
func TestAdministrationLandingShowsPendingAccountCountAndToleratesUnavailableRequestCount(t *testing.T) {
	h := newHarness(t, false)
	h.completeSetup()
	if _, err := h.store.DB().Exec(`
		INSERT INTO Users (UserName, PasswordHash, AccessLevel, DisabledFg, AccountStatus, ModAt, CreatedAt, ModBy)
		SELECT 'pending1', PasswordHash, 'Member', 0, 'PendingApproval', ModAt, CreatedAt, 'seed'
		FROM Users WHERE UserName = 'admin'`); err != nil {
		t.Fatalf("seed pending account: %v", err)
	}

	h.loginAs(t, "admin")
	resp, body := h.request(http.MethodGet, "/admin", "", nil, "text/html")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /admin = %d: %s", resp.StatusCode, body)
	}
	for _, want := range []string{
		`<h2 id="admin-home-requests">Request management</h2>`,
		`>1</span> awaiting approval`,
		"Pending count unavailable",
	} {
		if !bytes.Contains(body, []byte(want)) {
			t.Fatalf("administration page missing %q\n%s", want, body)
		}
	}
}
