package api

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/Panazule/guildbank/internal/domain"
)

type charactersEnvelope struct {
	Data struct {
		Characters []characterResponse `json:"characters"`
	} `json:"data"`
}

func (h *harness) addCharacter(t *testing.T, name string) characterResponse {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"characterName": name})
	resp, data := h.request(http.MethodPost, "/api/v1/me/characters", h.csrf(), body, "application/json")
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("add %s = %d: %s", name, resp.StatusCode, data)
	}
	var env struct {
		Data struct {
			Character characterResponse `json:"character"`
		} `json:"data"`
	}
	if err := json.Unmarshal(data, &env); err != nil {
		t.Fatalf("add json: %v", err)
	}
	return env.Data.Character
}

func TestProfileRequiresASession(t *testing.T) {
	h := newHarness(t, false)
	h.completeSetup()
	h.logout(t)

	resp, _ := h.request(http.MethodGet, "/account/profile", "", nil, "text/html")
	if resp.StatusCode != http.StatusSeeOther || !strings.HasPrefix(resp.Header.Get("Location"), "/login") {
		t.Fatalf("anonymous profile = %d %q, want 303 /login", resp.StatusCode, resp.Header.Get("Location"))
	}
	resp, _ = h.request(http.MethodGet, "/api/v1/me/characters", "", nil, "application/json")
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("anonymous character list = %d, want 401", resp.StatusCode)
	}
}

func TestMemberManagesOwnCharacters(t *testing.T) {
	h := newHarness(t, false)
	h.completeSetup()
	h.seedUser(t, "member1", domain.RoleMember, false)
	h.loginAs(t, "member1")

	resp, page := h.request(http.MethodGet, "/account/profile", "", nil, "text/html")
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(page), "Characters") ||
		!strings.Contains(string(page), `action="/api/v1/me/characters"`) {
		t.Fatalf("profile page = %d: %s", resp.StatusCode, page)
	}

	first := h.addCharacter(t, "Icefear")
	if first.Name != "Icefear" || first.ID == 0 {
		t.Fatalf("created character = %+v", first)
	}
	_, listBody := h.request(http.MethodGet, "/api/v1/me/characters", "", nil, "application/json")
	var list charactersEnvelope
	if err := json.Unmarshal(listBody, &list); err != nil {
		t.Fatalf("list json: %v", err)
	}
	if len(list.Data.Characters) != 1 || list.Data.Characters[0].Name != "Icefear" {
		t.Fatalf("list = %+v", list.Data.Characters)
	}
	if strings.Contains(string(listBody), "passwordHash") {
		t.Fatalf("character list leaked unrelated data: %s", listBody)
	}

	duplicate, _ := json.Marshal(map[string]string{"characterName": "icefear"})
	resp, data := h.request(http.MethodPost, "/api/v1/me/characters", h.csrf(), duplicate, "application/json")
	if resp.StatusCode != http.StatusConflict || !strings.Contains(string(data), "already registered") {
		t.Fatalf("duplicate = %d: %s", resp.StatusCode, data)
	}
	invalid, _ := json.Marshal(map[string]string{"characterName": "Ice fear"})
	resp, data = h.request(http.MethodPost, "/api/v1/me/characters", h.csrf(), invalid, "application/json")
	if resp.StatusCode != http.StatusUnprocessableEntity || !strings.Contains(string(data), "characterName") {
		t.Fatalf("invalid name = %d: %s", resp.StatusCode, data)
	}

	form := url.Values{"csrf_token": {h.csrf()}, "characterName": {"Banktoon"}}
	resp, body := h.requestForm(http.MethodPost, "/api/v1/me/characters", form.Get("csrf_token"), form, "text/html")
	if resp.StatusCode != http.StatusSeeOther ||
		resp.Header.Get("Location") != "/account/profile?notice=character-added" {
		t.Fatalf("form add = %d %q: %s", resp.StatusCode, resp.Header.Get("Location"), body)
	}

	resp, _ = h.request(http.MethodDelete, "/api/v1/me/characters/"+itoa(first.ID), h.csrf(), nil, "application/json")
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("delete = %d, want 204", resp.StatusCode)
	}
	// The name is available again.
	h.addCharacter(t, "Icefear")

	// Another account cannot remove this character.
	resp, _ = h.request(http.MethodDelete, "/api/v1/me/characters/1", h.csrf(), nil, "application/json")
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("delete of an unknown id = %d, want 404", resp.StatusCode)
	}
}

func TestCharacterNamesAreGloballyUniqueAcrossAccounts(t *testing.T) {
	h := newHarness(t, false)
	h.completeSetup()
	h.seedUser(t, "member1", domain.RoleMember, false)
	h.seedUser(t, "member2", domain.RoleMember, false)

	h.loginAs(t, "member1")
	h.addCharacter(t, "Icefear")

	h.loginAs(t, "member2")
	body, _ := json.Marshal(map[string]string{"characterName": "ICEFEAR"})
	resp, data := h.request(http.MethodPost, "/api/v1/me/characters", h.csrf(), body, "application/json")
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("cross-account duplicate = %d: %s", resp.StatusCode, data)
	}
	_, listBody := h.request(http.MethodGet, "/api/v1/me/characters", "", nil, "application/json")
	if strings.Contains(string(listBody), "Icefear") {
		t.Fatalf("another account's name appears in this profile: %s", listBody)
	}
}

func TestAdministratorRemovesCharacterFromUserDetail(t *testing.T) {
	h := newHarness(t, false)
	h.completeSetup()
	h.seedUser(t, "member1", domain.RoleMember, false)
	h.loginAs(t, "member1")
	first := h.addCharacter(t, "Icefear")
	second := h.addCharacter(t, "Banktoon")
	memberID := h.userID(t, "member1")

	h.loginAs(t, "admin")
	resp, body := h.request(http.MethodGet, "/admin/users/"+itoa(memberID), "", nil, "text/html")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("user detail = %d: %s", resp.StatusCode, body)
	}
	for _, want := range []string{"Icefear", "Banktoon",
		`/admin/users/` + itoa(memberID) + `/characters/` + itoa(first.ID) + `/delete`} {
		if !strings.Contains(string(body), want) {
			t.Fatalf("user detail missing %q: %s", want, body)
		}
	}

	csrf := h.csrf()
	form := url.Values{"csrf_token": {csrf}}
	resp, _ = h.requestForm(http.MethodPost,
		"/admin/users/"+itoa(memberID)+"/characters/"+itoa(first.ID)+"/delete", csrf, form, "text/html")
	if resp.StatusCode != http.StatusSeeOther ||
		resp.Header.Get("Location") != "/admin/users/"+itoa(memberID)+"?notice=character-removed" {
		t.Fatalf("admin remove form = %d %q", resp.StatusCode, resp.Header.Get("Location"))
	}

	resp, _ = h.request(http.MethodDelete,
		"/api/v1/users/"+itoa(memberID)+"/characters/"+itoa(second.ID), h.csrf(), nil, "application/json")
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("admin remove API = %d, want 204", resp.StatusCode)
	}

	// Ownership was removed, not transferred: the member's list is empty.
	h.loginAs(t, "member1")
	_, listBody := h.request(http.MethodGet, "/api/v1/me/characters", "", nil, "application/json")
	var list charactersEnvelope
	if err := json.Unmarshal(listBody, &list); err != nil {
		t.Fatalf("list json: %v", err)
	}
	if len(list.Data.Characters) != 0 {
		t.Fatalf("characters after administrative removal = %+v", list.Data.Characters)
	}
}

func TestOfficerCannotManageOtherAccountsCharacters(t *testing.T) {
	h := newHarness(t, false)
	h.completeSetup()
	h.seedUser(t, "member1", domain.RoleMember, false)
	h.seedUser(t, "officer1", domain.RoleOfficer, false)
	h.loginAs(t, "member1")
	character := h.addCharacter(t, "Icefear")

	h.loginAs(t, "officer1")
	csrf := h.csrf()
	form := url.Values{"csrf_token": {csrf}}
	resp, _ := h.requestForm(http.MethodPost, "/admin/users/1/characters/"+itoa(character.ID)+"/delete",
		csrf, form, "text/html")
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("officer admin form = %d, want 403", resp.StatusCode)
	}
	resp, _ = h.request(http.MethodDelete, "/api/v1/users/1/characters/"+itoa(character.ID),
		csrf, nil, "application/json")
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("officer admin API = %d, want 403", resp.StatusCode)
	}
}

func TestProfileIsConfinedByMustChangePassword(t *testing.T) {
	h := newHarness(t, false)
	h.completeSetup()
	const temporary = "a temporary password"
	h.createAccount(t, "Keeper", "Member", temporary)
	if resp, _ := h.loginWith(t, "Keeper", temporary); resp.StatusCode != http.StatusOK {
		t.Fatalf("flagged login = %d", resp.StatusCode)
	}
	resp, _ := h.request(http.MethodGet, "/account/profile", "", nil, "text/html")
	if resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != "/account/password" {
		t.Fatalf("flagged profile page = %d %q", resp.StatusCode, resp.Header.Get("Location"))
	}
	resp, data := h.request(http.MethodGet, "/api/v1/me/characters", "", nil, "application/json")
	if resp.StatusCode != http.StatusForbidden || !strings.Contains(string(data), "password_change_required") {
		t.Fatalf("flagged character list = %d: %s", resp.StatusCode, data)
	}
}
