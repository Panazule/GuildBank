package api

import (
	"bytes"
	"testing"

	"github.com/Panazule/guildbank/internal/domain"
	"github.com/Panazule/guildbank/internal/web"
)

// renderNav executes the shared navigation partial for one account state so the
// visibility rules are asserted directly, without depending on a page that can
// reach a guild store.
func renderNav(t *testing.T, user *domain.User) string {
	t.Helper()
	viewSet, err := newViews(web.Templates)
	if err != nil {
		t.Fatalf("newViews: %v", err)
	}
	var buf bytes.Buffer
	if err := viewSet.templates.ExecuteTemplate(&buf, "nav", pageData{User: user}); err != nil {
		t.Fatalf("render nav: %v", err)
	}
	return buf.String()
}

func TestNavigationVisibilityFollowsRole(t *testing.T) {
	anonymous := renderNav(t, nil)
	if !bytes.Contains([]byte(anonymous), []byte(`href="/login"`)) {
		t.Fatal("anonymous navigation does not offer login")
	}
	if bytes.Contains([]byte(anonymous), []byte(`href="/admin/`)) {
		t.Fatal("anonymous navigation exposes administration links")
	}
	if bytes.Contains([]byte(anonymous), []byte(`href="/account/profile"`)) {
		t.Fatal("anonymous navigation exposes the profile link")
	}
	if bytes.Contains([]byte(anonymous), []byte(`href="/my/requests"`)) {
		t.Fatal("anonymous navigation exposes the member request list")
	}

	member := renderNav(t, &domain.User{UserName: "member1", AccessLevel: "Member"})
	if bytes.Contains([]byte(member), []byte(`href="/admin/`)) {
		t.Fatal("member navigation exposes administration links")
	}
	if !bytes.Contains([]byte(member), []byte(`href="/account/profile"`)) {
		t.Fatal("member navigation is missing the profile link")
	}
	if !bytes.Contains([]byte(member), []byte(`href="/my/requests"`)) {
		t.Fatal("member navigation is missing the request list link")
	}
	if !bytes.Contains([]byte(member), []byte("Sign out")) {
		t.Fatal("member navigation is missing sign out")
	}

	officer := renderNav(t, &domain.User{UserName: "officer1", AccessLevel: "Officer"})
	for _, href := range []string{`href="/admin/requests"`, `href="/admin/inventory"`, `href="/admin/imports"`, `href="/admin/audit"`} {
		if !bytes.Contains([]byte(officer), []byte(href)) {
			t.Fatalf("officer navigation missing %s", href)
		}
	}
	if bytes.Contains([]byte(officer), []byte(`href="/admin/settings"`)) {
		t.Fatal("officer navigation exposes settings")
	}

	admin := renderNav(t, &domain.User{UserName: "admin", AccessLevel: domain.AccessAdmin})
	for _, href := range []string{
		`href="/admin/requests"`, `href="/admin/inventory"`, `href="/admin/imports"`,
		`href="/admin/audit"`, `href="/admin/users"`, `href="/admin/settings"`,
	} {
		if !bytes.Contains([]byte(admin), []byte(href)) {
			t.Fatalf("administrator navigation missing %s", href)
		}
	}
	if bytes.Contains([]byte(officer), []byte(`href="/admin/users"`)) {
		t.Fatal("officer navigation exposes account management")
	}
}
