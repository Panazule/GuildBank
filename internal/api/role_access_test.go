package api

import (
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

// The /admin landing is role-aware and an Officer is refused the
// Administrator-only settings page.
func TestAdminLandingFollowsRole(t *testing.T) {
	h := newHarness(t, false)
	h.completeSetup()
	h.seedUser(t, "officer1", domain.RoleOfficer, false)

	h.loginAs(t, "officer1")
	resp, _ := h.request(http.MethodGet, "/admin", "", nil, "text/html")
	if resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != "/admin/inventory" {
		t.Fatalf("officer /admin = %d %q, want 303 /admin/inventory",
			resp.StatusCode, resp.Header.Get("Location"))
	}
	resp, _ = h.request(http.MethodGet, "/admin/settings", "", nil, "text/html")
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("officer settings = %d, want 403", resp.StatusCode)
	}

	h.loginAs(t, "admin")
	resp, _ = h.request(http.MethodGet, "/admin", "", nil, "text/html")
	if resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != "/admin/settings" {
		t.Fatalf("admin /admin = %d %q, want 303 /admin/settings",
			resp.StatusCode, resp.Header.Get("Location"))
	}
}
