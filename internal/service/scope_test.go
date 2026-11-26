package service

import (
	"strings"
	"testing"
)

// TestAuthorizeScopeIsPerFlow pins D-SR-13: authorization URLs carry an explicit
// scope, and only the registration flow requests `guilds`. A caller that forgot
// to pass a scope cannot compile, and no flow inherits another's permissions.
func TestAuthorizeScopeIsPerFlow(t *testing.T) {
	svc := NewDiscordService(nil, nil, nil, nil, nil, nil)
	cfg := serviceConfigForTest()

	cases := []struct {
		name string
		url  string
		want string
	}{
		{"link", svc.AuthorizeURL(cfg, "state"), "scope=identify&"},
		{"sign-in", svc.LoginAuthorizeURL(cfg, "state"), "scope=identify&"},
	}
	for _, tc := range cases {
		if !strings.Contains(tc.url, tc.want) || strings.Contains(tc.url, "guilds") {
			t.Fatalf("%s authorize URL = %s, want %q and no guilds", tc.name, tc.url, tc.want)
		}
	}

	registration := NewRegistrationService(nil, nil, nil, nil, nil)
	registerURL := registration.AuthorizeURL(cfg, "state")
	if !strings.Contains(registerURL, "scope=identify+guilds") {
		t.Fatalf("registration authorize URL = %s, want scope=identify+guilds", registerURL)
	}
	if !strings.Contains(registerURL, "redirect_uri=https%3A%2F%2Fguildbank.example.com%2Fregister%2Fdiscord%2Fcallback") {
		t.Fatalf("registration authorize URL has the wrong redirect: %s", registerURL)
	}
	if !strings.Contains(svc.AuthorizeURL(cfg, "state"), "redirect_uri=https%3A%2F%2Fguildbank.example.com%2Faccount%2Fdiscord%2Fcallback") {
		t.Fatalf("link authorize URL has the wrong redirect: %s", svc.AuthorizeURL(cfg, "state"))
	}
}
