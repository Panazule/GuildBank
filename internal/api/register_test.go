package api

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// registrationSettingsPayload turns self-registration on against the same public
// origin the Discord tests use.
func registrationSettingsPayload(guildID string) map[string]string {
	values := discordSettingsPayload()
	values["SelfRegistrationEnabled"] = "true"
	if guildID != "" {
		values["DiscordGuildId"] = guildID
	}
	return values
}

// startRegistration follows the landing page's anchor and returns the state
// carried in the Discord redirect.
func (h *harness) startRegistration(t *testing.T) string {
	t.Helper()
	resp, body := h.request(http.MethodGet, "/register/discord", "", nil, "text/html")
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("start registration = %d: %s", resp.StatusCode, body)
	}
	location, err := url.Parse(resp.Header.Get("Location"))
	if err != nil {
		t.Fatalf("authorize location: %v", err)
	}
	if location.Host != "discord.com" {
		t.Fatalf("authorize location = %s", location)
	}
	if got := location.Query().Get("redirect_uri"); got != "https://guildbank.example.com/register/discord/callback" {
		t.Fatalf("redirect_uri = %q", got)
	}
	if got := location.Query().Get("scope"); got != "identify guilds" {
		t.Fatalf("scope = %q, want identify guilds", got)
	}
	state := location.Query().Get("state")
	if state == "" {
		t.Fatal("authorize redirect carried no state")
	}
	return state
}

func (h *harness) registrationCallback(t *testing.T, state string) *http.Response {
	t.Helper()
	resp, _ := h.request(http.MethodGet, "/register/discord/callback?state="+url.QueryEscape(state)+"&code=test-code",
		"", nil, "text/html")
	return resp
}

// completeRegistration runs the whole flow and returns the completion response.
func (h *harness) completeRegistration(t *testing.T, userName string) (*http.Response, []byte) {
	t.Helper()
	state := h.startRegistration(t)
	resp := h.registrationCallback(t, state)
	if resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != "/register/complete" {
		t.Fatalf("registration callback = %d %q", resp.StatusCode, resp.Header.Get("Location"))
	}
	form := url.Values{"userName": {userName}}
	return h.requestForm(http.MethodPost, "/register/complete", h.csrf(), form, "text/html")
}

func (h *harness) accountStatus(t *testing.T, name string) string {
	t.Helper()
	var status string
	if err := h.store.DB().QueryRow(
		`SELECT AccountStatus FROM Users WHERE UserName = ?`, name).Scan(&status); err != nil {
		t.Fatalf("account status for %s: %v", name, err)
	}
	return status
}

// TestRegistrationApprovesVerifiedGuildMember runs the approved path end to end:
// consent, guild check, account write, automatic sign-in.
func TestRegistrationApprovesVerifiedGuildMember(t *testing.T) {
	h := newHarness(t, false)
	h.completeSetup()
	h.discordAPI.guildMember = true
	h.saveSettings(t, registrationSettingsPayload("987654321098765432"))

	resp, body := h.request(http.MethodGet, "/register", "", nil, "text/html")
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(body), "Continue with Discord") {
		t.Fatalf("GET /register = %d: %s", resp.StatusCode, body)
	}

	resp, body = h.completeRegistration(t, "keldor")
	if resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != "/" {
		t.Fatalf("complete registration = %d %q: %s", resp.StatusCode, resp.Header.Get("Location"), body)
	}
	if status := h.accountStatus(t, "keldor"); status != "Approved" {
		t.Fatalf("account status = %q, want Approved", status)
	}

	var origin, discordID string
	var guildVerified *string
	if err := h.store.DB().QueryRow(
		`SELECT COALESCE(Origin, ''), GuildVerifiedAt, COALESCE(DiscordUserID, '') FROM Users WHERE UserName = 'keldor'`,
	).Scan(&origin, &guildVerified, &discordID); err != nil {
		t.Fatalf("read registered row: %v", err)
	}
	if origin != "SelfRegistration" || guildVerified == nil || discordID != "123456789012345678" {
		t.Fatalf("registered row origin=%q verified=%v discord=%q", origin, guildVerified, discordID)
	}

	// The approved account was signed in immediately through the normal session
	// machinery, so the session endpoint answers for it.
	session, data := h.request(http.MethodGet, "/api/v1/auth/session", "", nil, "application/json")
	if session.StatusCode != http.StatusOK {
		t.Fatalf("session after registration = %d: %s", session.StatusCode, data)
	}
	var env sessionEnvelope
	if err := json.Unmarshal(data, &env); err != nil {
		t.Fatalf("session json: %v", err)
	}
	if env.Data.User.UserName != "keldor" || env.Data.User.AccessLevel != "Member" {
		t.Fatalf("session user = %+v", env.Data.User)
	}
}

// TestRegistrationCreatesPendingAccountWithoutSession proves the fail-closed
// path: no guild match means PendingApproval and no session at all.
func TestRegistrationCreatesPendingAccountWithoutSession(t *testing.T) {
	h := newHarness(t, false)
	h.completeSetup()
	h.discordAPI.guildMember = false
	h.saveSettings(t, registrationSettingsPayload("987654321098765432"))

	resp, body := h.completeRegistration(t, "keldor")
	if resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != "/login?notice=registration-pending" {
		t.Fatalf("complete registration = %d %q: %s", resp.StatusCode, resp.Header.Get("Location"), body)
	}
	if status := h.accountStatus(t, "keldor"); status != "PendingApproval" {
		t.Fatalf("account status = %q, want PendingApproval", status)
	}
	if sessions := h.sessionCount(t, h.userID(t, "keldor")); sessions != 0 {
		t.Fatalf("sessions = %d, want 0 for a pending account", sessions)
	}
	// completeSetup leaves an administrator session in the jar; sign out first so
	// this probe answers for the registration, not for that session.
	h.logout(t)
	if resp, _ := h.request(http.MethodGet, "/api/v1/auth/session", "", nil, "application/json"); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("session endpoint = %d, want 401", resp.StatusCode)
	}

	// The queue lists it.
	h.loginAs(t, "admin")
	resp, data := h.request(http.MethodGet, "/api/v1/users?status=pending", "", nil, "application/json")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("pending list = %d: %s", resp.StatusCode, data)
	}
	if !strings.Contains(string(data), `"pending":true`) || !strings.Contains(string(data), `"keldor"`) {
		t.Fatalf("pending list does not contain the account: %s", data)
	}
}

// TestRegistrationRejectsDuplicateUsername keeps the ticket valid so the visitor
// can pick another name.
func TestRegistrationRejectsDuplicateUsername(t *testing.T) {
	h := newHarness(t, false)
	h.completeSetup()
	h.discordAPI.guildMember = true
	h.saveSettings(t, registrationSettingsPayload("987654321098765432"))
	h.seedUser(t, "keldor", "Member", false)

	resp, body := h.completeRegistration(t, "keldor")
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("duplicate username = %d: %s", resp.StatusCode, body)
	}
	if !strings.Contains(string(body), "that username is already in use") {
		t.Fatalf("re-render is missing the field message: %s", body)
	}
	if resp, _ := h.request(http.MethodGet, "/register/complete", "", nil, "text/html"); resp.StatusCode != http.StatusOK {
		t.Fatalf("ticket was not kept after a validation failure: %d", resp.StatusCode)
	}
}

// TestRegistrationIsUnavailableUntilEnabled proves the switch is off by default
// and that the landing page still renders (the link never 404s).
func TestRegistrationIsUnavailableUntilEnabled(t *testing.T) {
	h := newHarness(t, false)
	h.completeSetup()

	resp, body := h.request(http.MethodGet, "/register", "", nil, "text/html")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /register = %d", resp.StatusCode)
	}
	if !strings.Contains(string(body), "Self-registration is not enabled") ||
		strings.Contains(string(body), "Continue with Discord") {
		t.Fatalf("unavailable landing page = %s", body)
	}
	resp, _ = h.request(http.MethodGet, "/register/discord", "", nil, "text/html")
	if resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != "/login?notice=registration-unavailable" {
		t.Fatalf("start while disabled = %d %q", resp.StatusCode, resp.Header.Get("Location"))
	}
	if resp, _ := h.request(http.MethodGet, "/register/complete", "", nil, "text/html"); resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("completion without a ticket = %d, want a redirect", resp.StatusCode)
	}
}
