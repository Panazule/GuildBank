package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/Panazule/guildbank/internal/domain"
)

// seedPassword is the setup wizard password; seeded accounts copy the
// administrator's hash because user administration does not exist until P2.
const seedPassword = "a long enough password"

// seedUser adds one account with the approved role so the matrix can log in as
// each identity. The administrator row must already exist (completeSetup).
func (h *harness) seedUser(t *testing.T, name string, role domain.Role, disabled bool) {
	t.Helper()
	disabledFlag := 0
	if disabled {
		disabledFlag = 1
	}
	if _, err := h.store.DB().Exec(`
		INSERT INTO Users (UserName, PasswordHash, AccessLevel, DisabledFg, ModAt, CreatedAt, ModBy)
		SELECT ?, PasswordHash, ?, ?, ModAt, CreatedAt, 'seed' FROM Users WHERE UserName = ?`,
		name, string(role), disabledFlag, "admin"); err != nil {
		t.Fatalf("seed %s: %v", name, err)
	}
}

func (h *harness) loginAs(t *testing.T, name string) {
	t.Helper()
	csrf := h.csrf()
	body, _ := json.Marshal(map[string]string{"userName": name, "password": seedPassword})
	resp, data := h.request(http.MethodPost, "/api/v1/auth/login", csrf, body, "application/json")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("login %s = %d: %s", name, resp.StatusCode, data)
	}
}

func (h *harness) logout(t *testing.T) {
	t.Helper()
	resp, data := h.request(http.MethodPost, "/api/v1/auth/logout", h.csrf(), nil, "application/json")
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("logout = %d: %s", resp.StatusCode, data)
	}
}

// concretePath substitutes a benign value for every route parameter so a route
// can be probed without depending on stored data.
func concretePath(path string) string {
	replacer := strings.NewReplacer(
		":characterID", "1", ":batchID", "1", ":movementID", "1", ":id", "1", "*filepath", "app.css")
	return replacer.Replace(path)
}

func classRank(class routeClass) int {
	switch class {
	case classAdmin:
		return 3
	case classOfficer:
		return 2
	case classMember:
		return 1
	default:
		return 0
	}
}

// TestRouteMatrixEnforcement logs in as each role and probes every route: a
// caller below the requirement must be rejected, a caller at or above it must
// not be. Business outcomes (400/404/409/422/503) are not asserted here.
func TestRouteMatrixEnforcement(t *testing.T) {
	h := newHarness(t, false)
	csrf := h.completeSetup()
	_ = csrf
	h.seedUser(t, "member1", domain.RoleMember, false)
	h.seedUser(t, "officer1", domain.RoleOfficer, false)

	identities := []struct {
		name      string
		rank      int
		anonymous bool
	}{
		{"anonymous", 0, true},
		{"member1", 1, false},
		{"officer1", 2, false},
		{"admin", 3, false},
	}

	for _, identity := range identities {
		if identity.anonymous {
			h.logout(t)
		} else {
			h.loginAs(t, identity.name)
		}
		for key, required := range routeMatrix {
			// Logging out mid-sweep would invalidate the identity under test, and
			// the login route answers 401 for the empty probe body.
			if key == "POST /api/v1/auth/logout" || key == "POST /api/v1/auth/login" {
				continue
			}
			method, path, found := strings.Cut(key, " ")
			if !found {
				t.Fatalf("malformed matrix key %q", key)
			}
			probePath := concretePath(path)
			accept := "text/html"
			if strings.HasPrefix(probePath, "/api/") || probePath == "/healthz" || probePath == "/readyz" {
				accept = "application/json"
			}
			// A fresh token per probe keeps the double-submit cookie and header
			// in step, so a 403 always means the role gate rejected the caller.
			resp, body := h.request(method, probePath, h.csrf(), nil, accept)
			allowed := identity.rank >= classRank(required)
			if allowed {
				if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
					t.Errorf("%s as %s = %d, want allowed: %s", key, identity.name, resp.StatusCode, body)
				}
				continue
			}
			if identity.anonymous && resp.StatusCode == http.StatusSeeOther {
				continue // HTML requests redirect to /login.
			}
			if resp.StatusCode != http.StatusUnauthorized && resp.StatusCode != http.StatusForbidden {
				t.Errorf("%s as %s = %d, want 401 or 403: %s", key, identity.name, resp.StatusCode, body)
			}
		}
	}
}
