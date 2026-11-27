package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/Panazule/guildbank/internal/domain"
)

// Every approved role must be able to sign in, read its session and sign out.
// Before P1 only administrators could reach a session at all.
func TestEveryRoleCanUseSessionAndLogout(t *testing.T) {
	h := newHarness(t, false)
	h.completeSetup()
	h.seedUser(t, "member1", domain.RoleMember, false)
	h.seedUser(t, "officer1", domain.RoleOfficer, false)

	for _, tc := range []struct {
		name string
		role domain.Role
	}{
		{"member1", domain.RoleMember},
		{"officer1", domain.RoleOfficer},
		{"admin", domain.RoleAdmin},
	} {
		h.loginAs(t, tc.name)
		resp, body := h.request(http.MethodGet, "/api/v1/auth/session", "", nil, "application/json")
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("session as %s = %d: %s", tc.name, resp.StatusCode, body)
		}
		var payload struct {
			Data struct {
				User struct {
					UserName    string `json:"userName"`
					AccessLevel string `json:"accessLevel"`
				} `json:"user"`
			} `json:"data"`
		}
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Fatalf("session json as %s: %v", tc.name, err)
		}
		if payload.Data.User.UserName != tc.name || payload.Data.User.AccessLevel != string(tc.role) {
			t.Fatalf("session as %s = %+v", tc.name, payload.Data.User)
		}
		h.logout(t)
	}
}

func TestDisabledAccountCannotLogin(t *testing.T) {
	h := newHarness(t, false)
	h.completeSetup()
	h.seedUser(t, "retired", domain.RoleOfficer, true)

	csrf := h.csrf()
	body, _ := json.Marshal(map[string]string{"userName": "retired", "password": seedPassword})
	resp, data := h.request(http.MethodPost, "/api/v1/auth/login", csrf, body, "application/json")
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("disabled login = %d, want 401: %s", resp.StatusCode, data)
	}
}

// The Administration landing is role-aware: an Officer sees operational
// inventory, import, request, audit and account destinations, while settings
// administration remains an Administrator-only set within the same page.
func TestAdministrationLandingFollowsRole(t *testing.T) {
	h := newHarness(t, false)
	h.completeSetup()
	h.seedUser(t, "officer1", domain.RoleOfficer, false)

	h.loginAs(t, "officer1")
	resp, body := h.request(http.MethodGet, "/admin", "", nil, "text/html")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("officer /admin = %d: %s", resp.StatusCode, body)
	}
	for _, want := range []string{
		"<h1>Administration</h1>",
		`href="/admin/requests"`, `href="/admin/inventory"`,
		`href="/admin/imports"`, `href="/admin/audit"`, `href="/admin/users"`,
	} {
		if !bytes.Contains(body, []byte(want)) {
			t.Fatalf("officer administration page missing %q\n%s", want, body)
		}
	}
	for _, unwanted := range []string{`href="/admin/settings"`} {
		if bytes.Contains(body, []byte(unwanted)) {
			t.Fatalf("officer administration page exposes %s\n%s", unwanted, body)
		}
	}

	// Accounts are Officer work; Settings remain closed to them.
	if resp, page := h.request(http.MethodGet, "/admin/users", "", nil, "text/html"); resp.StatusCode != http.StatusOK {
		t.Fatalf("officer /admin/users = %d: %s", resp.StatusCode, page)
	}

	resp, _ = h.request(http.MethodGet, "/admin/settings", "", nil, "text/html")
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("officer settings = %d, want 403", resp.StatusCode)
	}

	h.loginAs(t, "admin")
	resp, body = h.request(http.MethodGet, "/admin", "", nil, "text/html")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("admin /admin = %d: %s", resp.StatusCode, body)
	}
	for _, want := range []string{
		`href="/admin/users"`, `href="/admin/settings"`,
		`href="/admin/audit"`, `href="/admin/requests"`,
	} {
		if !bytes.Contains(body, []byte(want)) {
			t.Fatalf("administrator administration page missing %q\n%s", want, body)
		}
	}
}
