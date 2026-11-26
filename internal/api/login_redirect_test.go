package api

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/Panazule/guildbank/internal/domain"
)

// loginHTML performs an HTML login so the redirect headers can be asserted.
func (h *harness) loginHTML(t *testing.T, name, password, next string) *http.Response {
	t.Helper()
	body, _ := json.Marshal(map[string]string{
		"userName": name, "password": password, "next": next,
	})
	resp, _ := h.request(http.MethodPost, "/api/v1/auth/login", h.csrf(), body, "text/html")
	return resp
}

// TestLoginLandsOnInventoryForEveryRole proves the administration pages are never
// the post-login default: inventory is the home page for all roles.
func TestLoginLandsOnInventoryForEveryRole(t *testing.T) {
	h := newHarness(t, false)
	h.completeSetup()
	h.seedUser(t, "member1", domain.RoleMember, false)
	h.seedUser(t, "officer1", domain.RoleOfficer, false)

	for _, name := range []string{"admin", "officer1", "member1"} {
		resp := h.loginHTML(t, name, seedPassword, "")
		if resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != "/" {
			t.Fatalf("login as %s = %d %q, want 303 /", name, resp.StatusCode, resp.Header.Get("Location"))
		}
	}
}

// TestLoginNextRespectsRole covers the saved destination: it is honoured only
// when the signed-in role may use it, otherwise the account lands on inventory.
func TestLoginNextRespectsRole(t *testing.T) {
	h := newHarness(t, false)
	h.completeSetup()
	h.seedUser(t, "member1", domain.RoleMember, false)
	h.seedUser(t, "officer1", domain.RoleOfficer, false)

	cases := []struct {
		actor string
		next  string
		want  string
	}{
		{"admin", "/admin/settings", "/admin/settings"},
		{"admin", "/admin/users", "/admin/users"},
		{"officer1", "/admin/audit", "/admin/audit"},
		{"officer1", "/admin/inventory?status=active", "/admin/inventory?status=active"},
		{"member1", "/", "/"},
		{"member1", "/account/password", "/account/password"},
		// Below the required role: fall back to inventory, never a 403 page.
		{"member1", "/admin/audit", "/"},
		{"member1", "/admin/inventory", "/"},
		{"member1", "/admin/users", "/"},
		{"officer1", "/admin/settings", "/"},
		{"officer1", "/admin/users", "/"},
		// Open redirects stay rejected.
		{"member1", "//evil.example/path", "/"},
		{"member1", "https://evil.example/path", "/"},
	}
	for _, c := range cases {
		resp := h.loginHTML(t, c.actor, seedPassword, c.next)
		if resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != c.want {
			t.Fatalf("login as %s with next %q = %d %q, want 303 %q",
				c.actor, c.next, resp.StatusCode, resp.Header.Get("Location"), c.want)
		}
	}
}

// TestProtectedPageRedirectReturnsOnlyWhenAuthorized drives the whole loop: an
// anonymous request stores `next`, and the login either returns there or falls
// back to inventory.
func TestProtectedPageRedirectReturnsOnlyWhenAuthorized(t *testing.T) {
	h := newHarness(t, false)
	h.completeSetup()
	h.seedUser(t, "member1", domain.RoleMember, false)
	h.seedUser(t, "officer1", domain.RoleOfficer, false)

	h.logout(t)
	resp, _ := h.request(http.MethodGet, "/admin/audit", "", nil, "text/html")
	if resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != "/login?next=%2Fadmin%2Faudit" {
		t.Fatalf("anonymous /admin/audit = %d %q", resp.StatusCode, resp.Header.Get("Location"))
	}

	// The officer may use the saved destination.
	if got := h.loginHTML(t, "officer1", seedPassword, "/admin/audit"); got.Header.Get("Location") != "/admin/audit" {
		t.Fatalf("officer return = %q, want /admin/audit", got.Header.Get("Location"))
	}

	// A member may not, so the loop ends on inventory instead of a Forbidden page.
	h.logout(t)
	if got := h.loginHTML(t, "member1", seedPassword, "/admin/audit"); got.Header.Get("Location") != "/" {
		t.Fatalf("member return = %q, want /", got.Header.Get("Location"))
	}
}

// TestAlreadySignedInLoginPageHonoursRole covers the second redirect path: a
// signed-in visitor to /login.
func TestAlreadySignedInLoginPageHonoursRole(t *testing.T) {
	h := newHarness(t, false)
	h.completeSetup()
	h.seedUser(t, "member1", domain.RoleMember, false)

	if resp, _ := h.loginWith(t, "member1", seedPassword); resp.StatusCode != http.StatusOK {
		t.Fatalf("member login = %d", resp.StatusCode)
	}
	resp, _ := h.request(http.MethodGet, "/login", "", nil, "text/html")
	if resp.Header.Get("Location") != "/" {
		t.Fatalf("signed-in /login = %d %q, want 303 /", resp.StatusCode, resp.Header.Get("Location"))
	}
	resp, _ = h.request(http.MethodGet, "/login?next=%2Fadmin%2Faudit", "", nil, "text/html")
	if resp.Header.Get("Location") != "/" {
		t.Fatalf("signed-in /login with a forbidden next = %q, want /", resp.Header.Get("Location"))
	}
}
