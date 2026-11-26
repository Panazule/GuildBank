package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/Panazule/guildbank/internal/domain"
)

// errTestDiscordDown stands in for a provider outage.
var errTestDiscordDown = errors.New("discord is unreachable")

// discordSettingsPayload enables linking against a public origin used in the
// redirect URI assertion.
func discordSettingsPayload() map[string]string {
	return map[string]string{
		"PublicBaseUrl":       "https://guildbank.example.com",
		"DiscordEnabled":      "true",
		"DiscordClientId":     "123456789012345678",
		"DiscordClientSecret": "client-secret",
	}
}

func (h *harness) saveSettings(t *testing.T, values map[string]string) {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"settings": values})
	resp, data := h.request(http.MethodPut, "/api/v1/settings", h.csrf(), body, "application/json")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("save settings = %d: %s", resp.StatusCode, data)
	}
}

// startDiscordLink follows the profile page's navigation link and returns the
// state carried in the authorization redirect. It sends no CSRF token: the
// browser path is a plain GET navigation, not a form submission.
func (h *harness) startDiscordLink(t *testing.T) string {
	t.Helper()
	resp, body := h.request(http.MethodGet, "/account/discord/link", "", nil, "text/html")
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("start link = %d: %s", resp.StatusCode, body)
	}
	location, err := url.Parse(resp.Header.Get("Location"))
	if err != nil {
		t.Fatalf("authorize location: %v", err)
	}
	if location.Host != "discord.com" || location.Path != "/oauth2/authorize" {
		t.Fatalf("authorize location = %s", location)
	}
	if got := location.Query().Get("redirect_uri"); got != "https://guildbank.example.com/account/discord/callback" {
		t.Fatalf("redirect_uri = %q", got)
	}
	state := location.Query().Get("state")
	if state == "" {
		t.Fatal("authorize redirect carried no state")
	}
	return state
}

type discordEnvelope struct {
	Data struct {
		Discord discordIdentityResponse `json:"discord"`
	} `json:"data"`
}

func (h *harness) discordIdentity(t *testing.T) discordIdentityResponse {
	t.Helper()
	resp, body := h.request(http.MethodGet, "/api/v1/me/discord", "", nil, "application/json")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("identity = %d: %s", resp.StatusCode, body)
	}
	var env discordEnvelope
	if err := json.Unmarshal(body, &env); err != nil {
		t.Fatalf("identity json: %v", err)
	}
	return env.Data.Discord
}

func (h *harness) callback(params url.Values) *http.Response {
	h.t.Helper()
	resp, _ := h.request(http.MethodGet, "/account/discord/callback?"+params.Encode(), "", nil, "text/html")
	return resp
}

func TestProfileDiscordSectionFollowsSettings(t *testing.T) {
	h := newHarness(t, false)
	h.completeSetup()

	_, body := h.request(http.MethodGet, "/account/profile", "", nil, "text/html")
	if !strings.Contains(string(body), "Discord linking is not enabled") {
		t.Fatalf("disabled profile missing the explanation: %s", body)
	}
	if strings.Contains(string(body), "/account/discord/link") {
		t.Fatal("a disabled configuration rendered a link button")
	}

	h.saveSettings(t, discordSettingsPayload())
	_, body = h.request(http.MethodGet, "/account/profile", "", nil, "text/html")
	if !strings.Contains(string(body), `href="/account/discord/link"`) {
		t.Fatalf("enabled profile has no link anchor: %s", body)
	}
	// The link must not be a form: a cross-origin 303 after a form submission is
	// cancelled by the page's Content-Security-Policy form-action 'self'.
	if strings.Contains(string(body), `action="/account/discord/link"`) {
		t.Fatalf("profile still submits the Discord link as a form: %s", body)
	}
	if strings.Contains(string(body), "/account/discord/unlink") {
		t.Fatal("an unlinked account rendered an unlink form")
	}
}

func TestDiscordLinkFlow(t *testing.T) {
	h := newHarness(t, false)
	h.completeSetup()
	h.saveSettings(t, discordSettingsPayload())
	h.seedUser(t, "member1", domain.RoleMember, false)
	h.loginAs(t, "member1")

	state := h.startDiscordLink(t)
	resp := h.callback(url.Values{"code": {"the-code"}, "state": {state}})
	if resp.StatusCode != http.StatusSeeOther ||
		resp.Header.Get("Location") != "/account/profile?notice=discord-linked" {
		t.Fatalf("callback = %d %q", resp.StatusCode, resp.Header.Get("Location"))
	}
	if len(h.discordAPI.codes) != 1 || h.discordAPI.codes[0] != "the-code" {
		t.Fatalf("provider codes = %v", h.discordAPI.codes)
	}

	identity := h.discordIdentity(t)
	if !identity.Linked || identity.UserName != "Keldor" || identity.UserID != "123456789012345678" {
		t.Fatalf("identity = %+v", identity)
	}
	_, body := h.request(http.MethodGet, "/account/profile", "", nil, "text/html")
	if !strings.Contains(string(body), "Linked to <strong>Keldor</strong>") {
		t.Fatalf("profile does not show the linked name: %s", body)
	}
	if !strings.Contains(string(body), `action="/account/discord/unlink"`) {
		t.Fatalf("profile has no unlink form: %s", body)
	}

	// The state is single use: replaying the callback is rejected.
	replay := h.callback(url.Values{"code": {"the-code"}, "state": {state}})
	if replay.Header.Get("Location") != "/account/profile?notice=discord-state" {
		t.Fatalf("replay = %q", replay.Header.Get("Location"))
	}
	if len(h.discordAPI.codes) != 1 {
		t.Fatalf("a replayed callback reached the provider: %v", h.discordAPI.codes)
	}

	// Password login still works and the identity survives it.
	h.logout(t)
	if resp, _ := h.loginWith(t, "member1", seedPassword); resp.StatusCode != http.StatusOK {
		t.Fatalf("password login after linking = %d", resp.StatusCode)
	}
	if identity := h.discordIdentity(t); !identity.Linked {
		t.Fatal("the link was lost across a sign-in")
	}
}

func TestDiscordCallbackRejectsForeignState(t *testing.T) {
	h := newHarness(t, false)
	h.completeSetup()
	h.saveSettings(t, discordSettingsPayload())
	h.seedUser(t, "member1", domain.RoleMember, false)
	h.loginAs(t, "member1")

	h.startDiscordLink(t)
	resp := h.callback(url.Values{"code": {"the-code"}, "state": {"forged-state"}})
	if resp.StatusCode != http.StatusSeeOther ||
		resp.Header.Get("Location") != "/account/profile?notice=discord-state" {
		t.Fatalf("foreign state = %d %q", resp.StatusCode, resp.Header.Get("Location"))
	}
	if len(h.discordAPI.codes) != 0 {
		t.Fatalf("a forged state reached the provider: %v", h.discordAPI.codes)
	}
	if identity := h.discordIdentity(t); identity.Linked {
		t.Fatalf("a forged state linked an identity: %+v", identity)
	}
}

func TestDiscordIdentityCannotBeSharedAcrossAccounts(t *testing.T) {
	h := newHarness(t, false)
	h.completeSetup()
	h.seedUser(t, "member1", domain.RoleMember, false)
	h.seedUser(t, "member2", domain.RoleMember, false)
	h.saveSettings(t, discordSettingsPayload())

	h.loginAs(t, "member1")
	state := h.startDiscordLink(t)
	h.callback(url.Values{"code": {"code-one"}, "state": {state}})

	h.loginAs(t, "member2")
	state = h.startDiscordLink(t)
	resp := h.callback(url.Values{"code": {"code-two"}, "state": {state}})
	if resp.Header.Get("Location") != "/account/profile?notice=discord-taken" {
		t.Fatalf("second account = %q", resp.Header.Get("Location"))
	}
	if identity := h.discordIdentity(t); identity.Linked {
		t.Fatalf("the second account holds an identity: %+v", identity)
	}
}

func TestDiscordProviderFailureIsReported(t *testing.T) {
	h := newHarness(t, false)
	h.completeSetup()
	h.saveSettings(t, discordSettingsPayload())
	h.seedUser(t, "member1", domain.RoleMember, false)
	h.loginAs(t, "member1")
	h.discordAPI.exchangeErr = errTestDiscordDown

	state := h.startDiscordLink(t)
	resp := h.callback(url.Values{"code": {"the-code"}, "state": {state}})
	if resp.Header.Get("Location") != "/account/profile?notice=discord-provider" {
		t.Fatalf("provider failure = %q", resp.Header.Get("Location"))
	}
	if identity := h.discordIdentity(t); identity.Linked {
		t.Fatalf("a failed link stored an identity: %+v", identity)
	}
}

func TestDiscordUnlink(t *testing.T) {
	h := newHarness(t, false)
	h.completeSetup()
	h.saveSettings(t, discordSettingsPayload())
	h.seedUser(t, "member1", domain.RoleMember, false)
	h.loginAs(t, "member1")

	state := h.startDiscordLink(t)
	h.callback(url.Values{"code": {"the-code"}, "state": {state}})

	csrf := h.csrf()
	resp, _ := h.request(http.MethodDelete, "/api/v1/me/discord", csrf, nil, "application/json")
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("unlink = %d, want 204", resp.StatusCode)
	}
	if identity := h.discordIdentity(t); identity.Linked {
		t.Fatalf("unlink left an identity: %+v", identity)
	}
	resp, _ = h.request(http.MethodDelete, "/api/v1/me/discord", csrf, nil, "application/json")
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("second unlink = %d, want 404", resp.StatusCode)
	}

	// Link again, then use the HTML form adapter.
	state = h.startDiscordLink(t)
	h.callback(url.Values{"code": {"the-code"}, "state": {state}})
	csrf = h.csrf()
	form := url.Values{"csrf_token": {csrf}}
	resp, _ = h.requestForm(http.MethodPost, "/account/discord/unlink", csrf, form, "text/html")
	if resp.StatusCode != http.StatusSeeOther ||
		resp.Header.Get("Location") != "/account/profile?notice=discord-unlinked" {
		t.Fatalf("unlink form = %d %q", resp.StatusCode, resp.Header.Get("Location"))
	}
	if identity := h.discordIdentity(t); identity.Linked {
		t.Fatalf("the form unlink left an identity: %+v", identity)
	}
}

func TestDiscordLinkStartJsonContract(t *testing.T) {
	h := newHarness(t, false)
	h.completeSetup()
	h.saveSettings(t, discordSettingsPayload())

	csrf := h.csrf()
	resp, data := h.request(http.MethodPost, "/account/discord/link", csrf, nil, "application/json")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("json start = %d: %s", resp.StatusCode, data)
	}
	var payload struct {
		Data struct {
			AuthorizeURL string `json:"authorizeUrl"`
		} `json:"data"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		t.Fatalf("json start body: %v", err)
	}
	target, err := url.Parse(payload.Data.AuthorizeURL)
	if err != nil {
		t.Fatalf("authorizeUrl: %v", err)
	}
	if target.Host != "discord.com" || target.Path != "/oauth2/authorize" {
		t.Fatalf("authorizeUrl = %s", payload.Data.AuthorizeURL)
	}
	if target.Query().Get("state") == "" {
		t.Fatal("authorizeUrl carried no state")
	}
	// The JSON path also issues the state cookie, so the same browser could
	// finish the flow after following the returned URL.
	origin, _ := url.Parse(h.server.URL)
	if len(h.client.Jar.Cookies(origin)) == 0 {
		t.Fatal("json start issued no cookies")
	}
	resp, _ = h.request(http.MethodGet, "/account/profile", "", nil, "text/html")
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		t.Fatalf("json start broke the session: %d", resp.StatusCode)
	}
}

func TestDiscordRoutesRequireEnabledConfiguration(t *testing.T) {
	h := newHarness(t, false)
	h.completeSetup()
	h.seedUser(t, "member1", domain.RoleMember, false)
	h.loginAs(t, "member1")

	csrf := h.csrf()
	resp, _ := h.request(http.MethodGet, "/account/discord/link", "", nil, "text/html")
	if resp.StatusCode != http.StatusSeeOther ||
		resp.Header.Get("Location") != "/account/profile?notice=discord-unavailable" {
		t.Fatalf("disabled link navigation = %d %q", resp.StatusCode, resp.Header.Get("Location"))
	}
	form := url.Values{"csrf_token": {csrf}}
	resp, _ = h.requestForm(http.MethodPost, "/account/discord/link", csrf, form, "text/html")
	if resp.StatusCode != http.StatusSeeOther ||
		resp.Header.Get("Location") != "/account/profile?notice=discord-unavailable" {
		t.Fatalf("disabled link form = %d %q", resp.StatusCode, resp.Header.Get("Location"))
	}
	resp, data := h.request(http.MethodPost, "/account/discord/link", csrf, nil, "application/json")
	if resp.StatusCode != http.StatusServiceUnavailable || !strings.Contains(string(data), "discord_unavailable") {
		t.Fatalf("disabled link API = %d: %s", resp.StatusCode, data)
	}
	resp, data = h.request(http.MethodGet, "/api/v1/me/discord", "", nil, "application/json")
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(data), `"linked":false`) {
		t.Fatalf("disabled identity API = %d: %s", resp.StatusCode, data)
	}
	if len(h.discordAPI.codes) != 0 {
		t.Fatalf("a disabled configuration reached the provider: %v", h.discordAPI.codes)
	}
}

func TestDiscordLinkIsConfinedByMustChangePassword(t *testing.T) {
	h := newHarness(t, false)
	h.completeSetup()
	h.saveSettings(t, discordSettingsPayload())
	const temporary = "a temporary password"
	h.createAccount(t, "Keeper", "Member", temporary)
	if resp, _ := h.loginWith(t, "Keeper", temporary); resp.StatusCode != http.StatusOK {
		t.Fatalf("flagged login = %d", resp.StatusCode)
	}
	csrf := h.csrf()
	form := url.Values{"csrf_token": {csrf}}
	resp, _ := h.requestForm(http.MethodPost, "/account/discord/link", csrf, form, "text/html")
	if resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != "/account/password" {
		t.Fatalf("flagged link = %d %q", resp.StatusCode, resp.Header.Get("Location"))
	}
}
