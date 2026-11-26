package api

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

type approvalEnvelope struct {
	Data struct {
		User userResponse `json:"user"`
	} `json:"data"`
}

// pendingRegistration creates one pending account through the real flow and
// returns its user ID.
func (h *harness) pendingRegistration(t *testing.T) int64 {
	t.Helper()
	h.discordAPI.guildMember = false
	h.saveSettings(t, registrationSettingsPayload("987654321098765432"))
	resp, body := h.completeRegistration(t, "keldor")
	if resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != "/login?notice=registration-pending" {
		t.Fatalf("pending registration = %d %q: %s", resp.StatusCode, resp.Header.Get("Location"), body)
	}
	return h.userID(t, "keldor")
}

// TestApprovalAPIAdmitsAPendingAccount covers the approval contract: the account
// becomes Approved, no session is created, and a second call is idempotent.
func TestApprovalAPIAdmitsAPendingAccount(t *testing.T) {
	h := newHarness(t, false)
	h.completeSetup()
	id := h.pendingRegistration(t)
	h.loginAs(t, "admin")

	resp, body := h.request(http.MethodPost, "/api/v1/users/"+itoa(id)+"/approve", h.csrf(), nil, "application/json")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("approve = %d: %s", resp.StatusCode, body)
	}
	var env approvalEnvelope
	if err := json.Unmarshal(body, &env); err != nil {
		t.Fatalf("approve json: %v", err)
	}
	if env.Data.User.AccountStatus != "Approved" || env.Data.User.Pending {
		t.Fatalf("approved user = %+v", env.Data.User)
	}
	if env.Data.User.Origin != "SelfRegistration" {
		t.Fatalf("origin = %q", env.Data.User.Origin)
	}
	if sessions := h.sessionCount(t, id); sessions != 0 {
		t.Fatalf("approval created %d session(s), want 0", sessions)
	}

	// Idempotent: a second approval is a no-op that still answers 200.
	resp, body = h.request(http.MethodPost, "/api/v1/users/"+itoa(id)+"/approve", h.csrf(), nil, "application/json")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("second approve = %d: %s", resp.StatusCode, body)
	}
	if status := h.accountStatus(t, "keldor"); status != "Approved" {
		t.Fatalf("account status = %q", status)
	}
}

// TestApprovalHTMLFormRendersAndApproves proves the detail page offers the action
// and that the form posts through the same service.
func TestApprovalHTMLFormRendersAndApproves(t *testing.T) {
	h := newHarness(t, false)
	h.completeSetup()
	id := h.pendingRegistration(t)
	h.loginAs(t, "admin")

	resp, body := h.request(http.MethodGet, "/admin/users/"+itoa(id), "", nil, "text/html")
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(body), "Approve account") {
		t.Fatalf("detail page = %d: %s", resp.StatusCode, body)
	}
	resp, body = h.requestForm(http.MethodPost, "/admin/users/"+itoa(id)+"/approve", h.csrf(),
		url.Values{}, "text/html")
	if resp.StatusCode != http.StatusSeeOther ||
		resp.Header.Get("Location") != "/admin/users/"+itoa(id)+"?notice=approved" {
		t.Fatalf("approve form = %d %q: %s", resp.StatusCode, resp.Header.Get("Location"), body)
	}
	if status := h.accountStatus(t, "keldor"); status != "Approved" {
		t.Fatalf("account status = %q", status)
	}
}

// TestWithdrawApprovalRevokesSessions proves the second transition: approval is
// withdrawn without disabling the account, and every session is deleted.
func TestWithdrawApprovalRevokesSessions(t *testing.T) {
	h := newHarness(t, false)
	h.completeSetup()
	h.seedUser(t, "member1", "Member", false)
	memberID := h.userID(t, "member1")
	h.loginAs(t, "member1")
	if sessions := h.sessionCount(t, memberID); sessions != 1 {
		t.Fatalf("sessions before withdrawal = %d, want 1", sessions)
	}

	h.logout(t)
	h.loginAs(t, "admin")
	resp, body := h.request(http.MethodPost, "/api/v1/users/"+itoa(memberID)+"/withdraw-approval",
		h.csrf(), nil, "application/json")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("withdraw = %d: %s", resp.StatusCode, body)
	}
	var env approvalEnvelope
	if err := json.Unmarshal(body, &env); err != nil {
		t.Fatalf("withdraw json: %v", err)
	}
	if env.Data.User.AccountStatus != "PendingApproval" || !env.Data.User.Pending {
		t.Fatalf("withdrawn user = %+v", env.Data.User)
	}
	if env.Data.User.Disabled {
		t.Fatal("withdrawing approval must not disable the account")
	}
	if sessions := h.sessionCount(t, memberID); sessions != 0 {
		t.Fatalf("sessions after withdrawal = %d, want 0", sessions)
	}

	// A withdrawn account cannot sign in again until it is approved.
	body, _ = json.Marshal(map[string]string{"userName": "member1", "password": seedPassword})
	resp, data := h.request(http.MethodPost, "/api/v1/auth/login", h.csrf(), body, "application/json")
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("login for a pending account = %d: %s", resp.StatusCode, data)
	}
}
