package api

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/Panazule/guildbank/internal/domain"
)

// registeredHarness signs in a freshly self-registered guild member, which is
// the only account shape that starts without a password.
func registeredHarness(t *testing.T) *harness {
	t.Helper()
	h := newHarness(t, false)
	h.completeSetup()
	h.discordAPI.guildMember = true
	h.saveSettings(t, registrationSettingsPayload("987654321098765432"))
	if resp, body := h.completeRegistration(t, "keldor"); resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("complete registration = %d: %s", resp.StatusCode, body)
	}
	return h
}

func (h *harness) passwordFlag(t *testing.T, name string) bool {
	t.Helper()
	var flag int
	if err := h.store.DB().QueryRow(
		`SELECT HasPassword FROM Users WHERE UserName = ?`, name).Scan(&flag); err != nil {
		t.Fatalf("read HasPassword for %s: %v", name, err)
	}
	return flag == 1
}

// TestPasswordlessAccountSetsFirstPassword covers the whole Set story end to
// end: the page offers Set (never Change), the endpoint accepts a first password
// without proof, the flag turns on, and the account can then sign in with it.
func TestPasswordlessAccountSetsFirstPassword(t *testing.T) {
	h := registeredHarness(t)
	if h.passwordFlag(t, "keldor") {
		t.Fatal("a self-registered account started password-capable")
	}

	// The session projection must say the same thing the page will render.
	_, data := h.request(http.MethodGet, "/api/v1/auth/session", "", nil, "application/json")
	var session sessionEnvelope
	if err := json.Unmarshal(data, &session); err != nil {
		t.Fatalf("session json: %v", err)
	}
	if session.Data.User.HasPassword {
		t.Fatal("the session reported a password capability that is not set")
	}

	resp, body := h.request(http.MethodGet, "/account/password", "", nil, "text/html")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("password page = %d: %s", resp.StatusCode, body)
	}
	for _, want := range []string{"Set password", `action="/api/v1/auth/password/set"`, `name="newPassword"`} {
		if !strings.Contains(string(body), want) {
			t.Fatalf("password page missing %q: %s", want, body)
		}
	}
	if strings.Contains(string(body), `name="currentPassword"`) {
		t.Fatal("the set page asked for a current password")
	}
	if strings.Contains(string(body), `action="/api/v1/auth/password/change"`) {
		t.Fatal("the set page posts to the change endpoint")
	}

	// A mismatched confirmation is a field error, not a capability grant.
	mismatch, _ := json.Marshal(map[string]string{
		"newPassword": "a brand new password", "confirmPassword": "a different password"})
	resp, data = h.request(http.MethodPost, "/api/v1/auth/password/set", h.csrf(), mismatch, "application/json")
	if resp.StatusCode != http.StatusUnprocessableEntity || !strings.Contains(string(data), "confirmPassword") {
		t.Fatalf("mismatched set = %d: %s", resp.StatusCode, data)
	}
	if h.passwordFlag(t, "keldor") {
		t.Fatal("a rejected set still granted the capability")
	}

	// The HTML adapter redirects back to the page with a notice.
	csrf := h.csrf()
	form := url.Values{"csrf_token": {csrf}, "newPassword": {"a brand new password"},
		"confirmPassword": {"a brand new password"}}
	resp, body = h.requestForm(http.MethodPost, "/api/v1/auth/password/set", csrf, form, "text/html")
	if resp.StatusCode != http.StatusSeeOther ||
		resp.Header.Get("Location") != "/account/password?set=1" {
		t.Fatalf("set form = %d %q: %s", resp.StatusCode, resp.Header.Get("Location"), body)
	}
	if !h.passwordFlag(t, "keldor") {
		t.Fatal("the set did not grant the capability")
	}
	var mustChange int
	if err := h.store.DB().QueryRow(
		`SELECT MustChangePassword FROM Users WHERE UserName = 'keldor'`).Scan(&mustChange); err != nil {
		t.Fatalf("read MustChangePassword: %v", err)
	}
	if mustChange != 0 {
		t.Fatal("setting a first password forced a change")
	}

	// The page now offers Change and asks for the current password.
	resp, body = h.request(http.MethodGet, "/account/password?set=1", "", nil, "text/html")
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(body), "Change password") ||
		!strings.Contains(string(body), `name="currentPassword"`) ||
		!strings.Contains(string(body), "Password set.") {
		t.Fatalf("post-set page = %d: %s", resp.StatusCode, body)
	}

	// The new password is a real credential: it signs the account in.
	h.logout(t)
	resp, data = h.loginWith(t, "keldor", "a brand new password")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("password login after set = %d: %s", resp.StatusCode, data)
	}
}

// TestPasswordRoutesEnforceAccountState proves the two endpoints stay disjoint:
// Set refuses an account that already has a password, and Change refuses an
// account that has none, over both transports.
func TestPasswordRoutesEnforceAccountState(t *testing.T) {
	h := registeredHarness(t)
	csrf := h.csrf()

	// Change on a passwordless account: the service refuses with a conflict.
	change, _ := json.Marshal(map[string]string{
		"currentPassword": "anything at all", "newPassword": "a brand new password",
		"confirmPassword": "a brand new password"})
	resp, data := h.request(http.MethodPost, "/api/v1/auth/password/change", csrf, change, "application/json")
	if resp.StatusCode != http.StatusUnprocessableEntity ||
		!strings.Contains(string(data), "validation_failed") {
		t.Fatalf("change on passwordless account = %d: %s", resp.StatusCode, data)
	}
	if h.passwordFlag(t, "keldor") {
		t.Fatal("a refused change granted the capability")
	}

	// The HTML adapter re-renders the page with the same message.
	form := url.Values{"csrf_token": {csrf}, "currentPassword": {"anything at all"},
		"newPassword": {"a brand new password"}, "confirmPassword": {"a brand new password"}}
	resp, body := h.requestForm(http.MethodPost, "/api/v1/auth/password/change", csrf, form, "text/html")
	const stateChanged = "The account state changed. Reload the page and try again."
	if resp.StatusCode != http.StatusUnprocessableEntity || !strings.Contains(string(body), stateChanged) {
		t.Fatalf("change form = %d: %s", resp.StatusCode, body)
	}

	// Establish the password, then the Set route must refuse to replace it.
	set, _ := json.Marshal(map[string]string{
		"newPassword": "a brand new password", "confirmPassword": "a brand new password"})
	resp, data = h.request(http.MethodPost, "/api/v1/auth/password/set", csrf, set, "application/json")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("set = %d: %s", resp.StatusCode, data)
	}
	var env userEnvelope
	if err := json.Unmarshal(data, &env); err != nil {
		t.Fatalf("set json: %v", err)
	}
	if !env.Data.User.HasPassword || strings.Contains(string(data), "passwordHash") {
		t.Fatalf("set envelope = %s", data)
	}

	setAgain, _ := json.Marshal(map[string]string{
		"newPassword": "another brand new password", "confirmPassword": "another brand new password"})
	resp, data = h.request(http.MethodPost, "/api/v1/auth/password/set", csrf, setAgain, "application/json")
	if resp.StatusCode != http.StatusUnprocessableEntity ||
		!strings.Contains(string(data), "validation_failed") {
		t.Fatalf("second set = %d: %s", resp.StatusCode, data)
	}

	// A wrong current password is a field error, and the stored hash is unchanged.
	wrong, _ := json.Marshal(map[string]string{
		"currentPassword": "not the password", "newPassword": "another brand new password",
		"confirmPassword": "another brand new password"})
	resp, data = h.request(http.MethodPost, "/api/v1/auth/password/change", csrf, wrong, "application/json")
	if resp.StatusCode != http.StatusUnprocessableEntity || !strings.Contains(string(data), "currentPassword") {
		t.Fatalf("wrong current password = %d: %s", resp.StatusCode, data)
	}

	// The real change succeeds and keeps this session.
	correct, _ := json.Marshal(map[string]string{
		"currentPassword": "a brand new password", "newPassword": "another brand new password",
		"confirmPassword": "another brand new password"})
	resp, data = h.request(http.MethodPost, "/api/v1/auth/password/change", csrf, correct, "application/json")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("change = %d: %s", resp.StatusCode, data)
	}
	if resp, _ := h.request(http.MethodGet, "/api/v1/auth/session", "", nil, "application/json"); resp.StatusCode != http.StatusOK {
		t.Fatal("the change signed the acting session out")
	}
}

// TestPasswordlessAccountCannotUnlinkDiscord proves D-SR-18: unlinking is refused
// while Discord is the only credential, the profile explains why, and the unlink
// becomes available once a password exists.
func TestPasswordlessAccountCannotUnlinkDiscord(t *testing.T) {
	h := registeredHarness(t)

	resp, body := h.request(http.MethodGet, "/account/profile", "", nil, "text/html")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("profile = %d: %s", resp.StatusCode, body)
	}
	if !strings.Contains(string(body), "Set Password") {
		t.Fatalf("passwordless profile has no Set Password link: %s", body)
	}
	if strings.Contains(string(body), `action="/account/discord/unlink"`) {
		t.Fatal("a passwordless account rendered the unlink form")
	}
	if !strings.Contains(string(body), "Set a password above before unlinking Discord") {
		t.Fatalf("profile does not explain the refusal: %s", body)
	}

	resp, data := h.request(http.MethodDelete, "/api/v1/me/discord", h.csrf(), nil, "application/json")
	if resp.StatusCode != http.StatusConflict || !strings.Contains(string(data), "password_required") {
		t.Fatalf("unlink without password = %d: %s", resp.StatusCode, data)
	}

	csrf := h.csrf()
	resp, body = h.requestForm(http.MethodPost, "/account/discord/unlink", csrf,
		url.Values{"csrf_token": {csrf}}, "text/html")
	if resp.StatusCode != http.StatusSeeOther ||
		resp.Header.Get("Location") != "/account/profile?notice=discord-password-required" {
		t.Fatalf("unlink form = %d %q: %s", resp.StatusCode, resp.Header.Get("Location"), body)
	}
	_, body = h.request(http.MethodGet, "/account/profile?notice=discord-password-required", "", nil, "text/html")
	if !strings.Contains(string(body), "Set a password before unlinking Discord") {
		t.Fatalf("the refusal notice is missing: %s", body)
	}

	// After a Set the same account may unlink, and the profile now says Change.
	set := url.Values{"csrf_token": {csrf}, "newPassword": {"a brand new password"},
		"confirmPassword": {"a brand new password"}}
	if resp, body := h.requestForm(http.MethodPost, "/api/v1/auth/password/set", csrf, set, "text/html"); resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("set for unlink = %d: %s", resp.StatusCode, body)
	}
	_, body = h.request(http.MethodGet, "/account/profile", "", nil, "text/html")
	if !strings.Contains(string(body), "Change Password") ||
		!strings.Contains(string(body), `action="/account/discord/unlink"`) {
		t.Fatalf("profile after set = %s", body)
	}
	resp, _ = h.requestForm(http.MethodPost, "/account/discord/unlink", h.csrf(),
		url.Values{"csrf_token": {csrf}}, "text/html")
	if resp.StatusCode != http.StatusSeeOther ||
		resp.Header.Get("Location") != "/account/profile?notice=discord-unlinked" {
		t.Fatalf("unlink after set = %d %q", resp.StatusCode, resp.Header.Get("Location"))
	}
}

// TestSeededAccountChangesPasswordWithoutSet covers the other starting state: an
// administrator-created account is password-capable from the start, so Set is
// refused and Change is the only route that works.
func TestSeededAccountChangesPasswordWithoutSet(t *testing.T) {
	h := newHarness(t, false)
	h.completeSetup()
	h.seedUser(t, "member1", domain.RoleMember, false)
	h.loginAs(t, "member1")
	if !h.passwordFlag(t, "member1") {
		t.Fatal("an administrator-created account started passwordless")
	}

	set, _ := json.Marshal(map[string]string{
		"newPassword": "a brand new password", "confirmPassword": "a brand new password"})
	resp, data := h.request(http.MethodPost, "/api/v1/auth/password/set", h.csrf(), set, "application/json")
	if resp.StatusCode != http.StatusUnprocessableEntity || !strings.Contains(string(data), "validation_failed") {
		t.Fatalf("set on a password account = %d: %s", resp.StatusCode, data)
	}

	csrf := h.csrf()
	form := url.Values{"csrf_token": {csrf}, "currentPassword": {seedPassword},
		"newPassword": {"another brand new password"}, "confirmPassword": {"another brand new password"}}
	resp, body := h.requestForm(http.MethodPost, "/api/v1/auth/password/change", csrf, form, "text/html")
	if resp.StatusCode != http.StatusSeeOther ||
		resp.Header.Get("Location") != "/account/password?changed=1" {
		t.Fatalf("change form = %d %q: %s", resp.StatusCode, resp.Header.Get("Location"), body)
	}
	_, body = h.request(http.MethodGet, "/account/password?changed=1", "", nil, "text/html")
	if !strings.Contains(string(body), "Password updated. Other sessions for this account were signed out.") {
		t.Fatalf("change notice missing: %s", body)
	}

	h.logout(t)
	if resp, data := h.loginWith(t, "member1", "another brand new password"); resp.StatusCode != http.StatusOK {
		t.Fatalf("login with the changed password = %d: %s", resp.StatusCode, data)
	}
}
