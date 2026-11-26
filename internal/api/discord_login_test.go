package api

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/Panazule/guildbank/internal/domain"
)

// linkDiscordDirect attaches an identity without the OAuth round trip, for cases
// where the member cannot reach the profile page (MustChangePassword, disabled).
func (h *harness) linkDiscordDirect(t *testing.T, userName, discordID string) {
	t.Helper()
	if _, err := h.store.DB().Exec(`
		UPDATE Users SET DiscordUserID = ?, DiscordUserName = 'Keldor', DiscordLinkedAt = ?
		WHERE UserName = ?`, discordID, time.Now().UTC().Format(time.RFC3339Nano), userName); err != nil {
		t.Fatalf("link %s directly: %v", userName, err)
	}
}

// startDiscordLogin begins sign-in from the login page and returns the state
// carried in the authorize redirect.
func (h *harness) startDiscordLogin(t *testing.T, next string) string {
	t.Helper()
	path := "/auth/discord"
	if next != "" {
		path += "?next=" + url.QueryEscape(next)
	}
	resp, body := h.request(http.MethodGet, path, "", nil, "text/html")
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("start discord login = %d: %s", resp.StatusCode, body)
	}
	location, err := url.Parse(resp.Header.Get("Location"))
	if err != nil {
		t.Fatalf("authorize location: %v", err)
	}
	if location.Host != "discord.com" || location.Path != "/oauth2/authorize" {
		t.Fatalf("authorize location = %s", location)
	}
	if got := location.Query().Get("redirect_uri"); got != "https://guildbank.example.com/auth/discord/callback" {
		t.Fatalf("login redirect_uri = %q", got)
	}
	state := location.Query().Get("state")
	if state == "" {
		t.Fatal("login authorize redirect carried no state")
	}
	return state
}

func (h *harness) loginCallback(params url.Values) *http.Response {
	h.t.Helper()
	resp, _ := h.request(http.MethodGet, "/auth/discord/callback?"+params.Encode(), "", nil, "text/html")
	return resp
}

func (h *harness) sessionUser(t *testing.T) (string, bool) {
	t.Helper()
	resp, body := h.request(http.MethodGet, "/api/v1/auth/session", "", nil, "application/json")
	if resp.StatusCode != http.StatusOK {
		return "", false
	}
	var payload struct {
		Data struct {
			User struct {
				UserName string `json:"userName"`
			} `json:"user"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("session json: %v", err)
	}
	return payload.Data.User.UserName, true
}

func TestLoginPageShowsDiscordButtonOnlyWhenConfigured(t *testing.T) {
	h := newHarness(t, false)
	h.completeSetup()
	h.logout(t)

	_, body := h.request(http.MethodGet, "/login", "", nil, "text/html")
	if strings.Contains(string(body), "/auth/discord") {
		t.Fatal("the Discord button is rendered while sign-in is not configured")
	}

	h.loginAs(t, "admin")
	h.saveSettings(t, discordSettingsPayload())
	h.logout(t)
	_, body = h.request(http.MethodGet, "/login", "", nil, "text/html")
	if !strings.Contains(string(body), `href="/auth/discord"`) {
		t.Fatalf("login page has no Discord button: %s", body)
	}
	if !strings.Contains(string(body), `action="/api/v1/auth/login"`) {
		t.Fatal("the password form disappeared")
	}
}

func TestDiscordLoginSignsInLinkedAccount(t *testing.T) {
	h := newHarness(t, false)
	h.completeSetup()
	h.saveSettings(t, discordSettingsPayload())
	h.seedUser(t, "member1", domain.RoleMember, false)

	// Link the Discord identity through the approved Phase B flow.
	h.loginAs(t, "member1")
	state := h.startDiscordLink(t)
	h.callback(url.Values{"code": {"link-code"}, "state": {state}})
	if identity := h.discordIdentity(t); !identity.Linked {
		t.Fatal("linking failed, so sign-in cannot be tested")
	}
	h.logout(t)
	if _, ok := h.sessionUser(t); ok {
		t.Fatal("logout left a session")
	}

	state = h.startDiscordLogin(t, "/account/profile")
	resp := h.loginCallback(url.Values{"code": {"login-code"}, "state": {state}})
	if resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != "/account/profile" {
		t.Fatalf("login callback = %d %q", resp.StatusCode, resp.Header.Get("Location"))
	}
	if got := h.discordAPI.codes[len(h.discordAPI.codes)-1]; got != "login-code" {
		t.Fatalf("provider code = %q", got)
	}
	if got := h.discordAPI.redirectURIs[len(h.discordAPI.redirectURIs)-1]; got !=
		"https://guildbank.example.com/auth/discord/callback" {
		t.Fatalf("login redirect URI = %q", got)
	}
	user, ok := h.sessionUser(t)
	if !ok || user != "member1" {
		t.Fatalf("session user = %q, ok = %v", user, ok)
	}

	// Password login still works for the same account.
	h.logout(t)
	if resp, data := h.loginWith(t, "member1", seedPassword); resp.StatusCode != http.StatusOK {
		t.Fatalf("password login = %d: %s", resp.StatusCode, data)
	}

	// The sign-in state is single use.
	h.logout(t)
	replay := h.loginCallback(url.Values{"code": {"login-code"}, "state": {state}})
	if replay.Header.Get("Location") != "/login?notice=discord-state" {
		t.Fatalf("replay = %q", replay.Header.Get("Location"))
	}
	if _, ok := h.sessionUser(t); ok {
		t.Fatal("a replayed callback produced a session")
	}
}

func TestDiscordLoginIsNeutralForAnUnlinkedDiscordAccount(t *testing.T) {
	h := newHarness(t, false)
	h.completeSetup()
	h.saveSettings(t, discordSettingsPayload())
	h.seedUser(t, "member1", domain.RoleMember, false)
	h.logout(t)
	var before int
	if err := h.store.DB().QueryRow(`SELECT COUNT(1) FROM Users`).Scan(&before); err != nil {
		t.Fatalf("count users: %v", err)
	}

	state := h.startDiscordLogin(t, "/admin/users")
	resp := h.loginCallback(url.Values{"code": {"login-code"}, "state": {state}})
	want := "/login?notice=discord-unlinked&next=%2Fadmin%2Fusers"
	if resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != want {
		t.Fatalf("unlinked login = %d %q", resp.StatusCode, resp.Header.Get("Location"))
	}
	if _, ok := h.sessionUser(t); ok {
		t.Fatal("an unlinked Discord account received a session")
	}
	var after int
	if err := h.store.DB().QueryRow(`SELECT COUNT(1) FROM Users`).Scan(&after); err != nil {
		t.Fatalf("count users: %v", err)
	}
	if after != before {
		t.Fatalf("users = %d, want %d", after, before)
	}

	// The login page renders the fixed neutral text.
	_, body := h.request(http.MethodGet, "/login?notice=discord-unlinked", "", nil, "text/html")
	if !strings.Contains(string(body), "No GuildBank account is linked to that Discord account") {
		t.Fatalf("neutral notice missing: %s", body)
	}
}

func TestDiscordLoginHonoursMustChangePassword(t *testing.T) {
	h := newHarness(t, false)
	h.completeSetup()
	h.saveSettings(t, discordSettingsPayload())
	const temporary = "a temporary password"
	h.createAccount(t, "Keeper", "Member", temporary)
	h.linkDiscordDirect(t, "Keeper", "123456789012345678")
	h.logout(t)

	state := h.startDiscordLogin(t, "/admin/users")
	resp := h.loginCallback(url.Values{"code": {"login-code"}, "state": {state}})
	if resp.Header.Get("Location") != "/account/password" {
		t.Fatalf("flagged discord login = %q", resp.Header.Get("Location"))
	}
	if user, ok := h.sessionUser(t); !ok || user != "Keeper" {
		t.Fatalf("session user = %q, ok = %v", user, ok)
	}
	// The flag still confines the account after the Discord sign-in.
	resp, _ = h.request(http.MethodGet, "/account/profile", "", nil, "text/html")
	if resp.Header.Get("Location") != "/account/password" {
		t.Fatalf("flagged profile = %q", resp.Header.Get("Location"))
	}
}

func TestDiscordLoginRefusesDisabledAccount(t *testing.T) {
	h := newHarness(t, false)
	h.completeSetup()
	h.saveSettings(t, discordSettingsPayload())
	h.seedUser(t, "member1", domain.RoleMember, false)
	h.linkDiscordDirect(t, "member1", "123456789012345678")
	if _, err := h.store.DB().Exec(`UPDATE Users SET DisabledFg = 1 WHERE UserName = ?`, "member1"); err != nil {
		t.Fatalf("disable member1: %v", err)
	}
	h.logout(t)

	state := h.startDiscordLogin(t, "")
	resp := h.loginCallback(url.Values{"code": {"login-code"}, "state": {state}})
	if resp.Header.Get("Location") != "/login?notice=discord-unlinked" {
		t.Fatalf("disabled login = %q", resp.Header.Get("Location"))
	}
	if _, ok := h.sessionUser(t); ok {
		t.Fatal("a disabled account received a session")
	}
}

func TestDiscordLoginRejectsForgedAndMismatchedState(t *testing.T) {
	h := newHarness(t, false)
	h.completeSetup()
	h.saveSettings(t, discordSettingsPayload())
	h.seedUser(t, "member1", domain.RoleMember, false)
	h.linkDiscordDirect(t, "member1", "123456789012345678")
	h.logout(t)

	h.startDiscordLogin(t, "")
	resp := h.loginCallback(url.Values{"code": {"login-code"}, "state": {"forged"}})
	if resp.Header.Get("Location") != "/login?notice=discord-state" {
		t.Fatalf("forged state = %q", resp.Header.Get("Location"))
	}
	if len(h.discordAPI.codes) != 0 {
		t.Fatal("a forged state reached the provider")
	}

	// A link state cannot be replayed as a sign-in: the login callback only
	// accepts a value minted for the login scope.
	h.loginAs(t, "member1")
	linkState := h.startDiscordLink(t)
	h.logout(t)
	resp = h.loginCallback(url.Values{"code": {"login-code"}, "state": {linkState}})
	if resp.Header.Get("Location") != "/login?notice=discord-state" {
		t.Fatalf("link state at login = %q", resp.Header.Get("Location"))
	}
	if _, ok := h.sessionUser(t); ok {
		t.Fatal("a link state produced a session")
	}
}

func TestDiscordSignInSkippedForExistingSession(t *testing.T) {
	h := newHarness(t, false)
	h.completeSetup()
	h.saveSettings(t, discordSettingsPayload())

	resp, _ := h.request(http.MethodGet, "/auth/discord?next=/admin/users", "", nil, "text/html")
	if resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != "/admin/users" {
		t.Fatalf("signed-in start = %d %q", resp.StatusCode, resp.Header.Get("Location"))
	}
	if len(h.discordAPI.codes) != 0 {
		t.Fatal("an existing session started a provider round trip")
	}
}

func TestDiscordLoginUnavailableWhenNotConfigured(t *testing.T) {
	h := newHarness(t, false)
	h.completeSetup()
	h.logout(t)

	resp, _ := h.request(http.MethodGet, "/auth/discord", "", nil, "text/html")
	if resp.StatusCode != http.StatusSeeOther ||
		resp.Header.Get("Location") != "/login?notice=discord-unavailable" {
		t.Fatalf("disabled sign-in = %d %q", resp.StatusCode, resp.Header.Get("Location"))
	}
	if len(h.discordAPI.codes) != 0 {
		t.Fatal("a disabled configuration reached the provider")
	}
}
