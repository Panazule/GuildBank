package api

import (
	"bytes"
	"net/http"
	"testing"
)

// navLinkOrder asserts that every wanted href is present and that the links
// appear in the documented navigation order.
func navLinkOrder(t *testing.T, body []byte, hrefs ...string) {
	t.Helper()
	previous := -1
	for _, href := range hrefs {
		marker := []byte(`href="` + href + `"`)
		index := bytes.Index(body, marker)
		if index < 0 {
			t.Fatalf("navigation missing %s\n%s", href, body)
		}
		if index < previous {
			t.Fatalf("navigation order wrong at %s\n%s", href, body)
		}
		previous = index
	}
}

func TestAnonymousNavigationOffersLogin(t *testing.T) {
	h := newHarness(t, false)
	h.completeSetup()
	// Sign out so the same client browses anonymously against a configured app.
	csrf := h.csrf()
	logout, _ := h.request(http.MethodPost, "/api/v1/auth/logout", csrf, nil, "")
	if logout.StatusCode != http.StatusNoContent {
		t.Fatalf("POST /api/v1/auth/logout = %d", logout.StatusCode)
	}
	// The anonymous inventory page renders without MariaDB, so it may answer
	// 503; the shared navigation must still offer a way to sign in.
	resp, body := h.request(http.MethodGet, "/", "", nil, "text/html")
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("GET / = %d: %s", resp.StatusCode, body)
	}
	navLinkOrder(t, body, "/", "/login")
	if bytes.Contains(body, []byte(`href="/admin/`)) || bytes.Contains(body, []byte("Sign out")) {
		t.Fatalf("anonymous navigation exposes authenticated entries: %s", body)
	}

	loginResp, _ := h.request(http.MethodGet, "/login", "", nil, "text/html")
	if loginResp.StatusCode != http.StatusOK {
		t.Fatalf("GET /login = %d", loginResp.StatusCode)
	}
}

func TestAdministratorNavigationAndSettingsPage(t *testing.T) {
	h := newHarness(t, false)
	h.completeSetup()

	resp, body := h.request(http.MethodGet, "/admin/settings", "", nil, "text/html")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /admin/settings = %d: %s", resp.StatusCode, body)
	}
	if len(body) == 0 {
		t.Fatal("GET /admin/settings returned an empty body")
	}
	for _, want := range []string{
		`<h1>Settings</h1>`, `name="csrf_token"`, `action="/api/v1/settings"`,
		`name="GuildName"`, `name="GuildOpenDays" value="14" type="number" min="0" max="3650"`,
		`name="MariaDbPort" value="3306" type="number" min="1" max="65535"`,
		`name="EQResourceBaseUrl" value="https://items.eqresource.com/items.php?id=" type="url"`,
		`name="MariaDbPassword" type="password"`,
	} {
		if !bytes.Contains(body, []byte(want)) {
			t.Fatalf("settings page missing %q\n%s", want, body)
		}
	}
	if bytes.Contains(body, []byte(`href="/login"`)) {
		t.Fatalf("authenticated navigation still offers login: %s", body)
	}
	navLinkOrder(t, body, "/", "/admin/inventory", "/admin/imports", "/admin/audit", "/admin/settings")
	if !bytes.Contains(body, []byte("Sign out")) {
		t.Fatalf("authenticated navigation missing sign out: %s", body)
	}

	noticeResp, noticeBody := h.request(http.MethodGet, "/admin/settings?saved=1", "", nil, "text/html")
	if noticeResp.StatusCode != http.StatusOK || !bytes.Contains(noticeBody, []byte("Settings saved.")) {
		t.Fatalf("saved notice = %d: %s", noticeResp.StatusCode, noticeBody)
	}
}
