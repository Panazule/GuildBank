package discord

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestExchangeCodeSendsCredentialsAndParsesToken(t *testing.T) {
	var (
		gotAuth   string
		gotForm   url.Values
		gotMethod string
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotAuth = r.Header.Get("Authorization")
		_ = r.ParseForm()
		gotForm = r.PostForm
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"token-1","token_type":"Bearer","expires_in":3600}`))
	}))
	defer server.Close()

	client := NewClient(server.Client())
	client.TokenURL = server.URL + "/oauth2/token"
	token, err := client.ExchangeCode(context.Background(), "client-id", "client-secret",
		"https://guildbank.example.com/account/discord/callback", "the-code")
	if err != nil {
		t.Fatalf("ExchangeCode: %v", err)
	}
	if gotMethod != http.MethodPost {
		t.Fatalf("method = %s, want POST", gotMethod)
	}
	wantAuth := "Basic " + base64.StdEncoding.EncodeToString([]byte("client-id:client-secret"))
	if gotAuth != wantAuth {
		t.Fatalf("authorization = %q, want %q", gotAuth, wantAuth)
	}
	if gotForm.Get("grant_type") != "authorization_code" || gotForm.Get("code") != "the-code" ||
		gotForm.Get("redirect_uri") != "https://guildbank.example.com/account/discord/callback" {
		t.Fatalf("form = %v", gotForm)
	}
	if token.AccessToken != "token-1" || token.TokenType != "Bearer" || token.ExpiresIn != 3600 {
		t.Fatalf("token = %+v", token)
	}
}

func TestProviderErrorsAreReportedWithoutTokens(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"invalid_client"}`))
	}))
	defer server.Close()

	client := NewClient(server.Client())
	client.TokenURL = server.URL + "/oauth2/token"
	if _, err := client.ExchangeCode(context.Background(), "id", "secret", "https://example.test/cb", "code"); err == nil {
		t.Fatal("ExchangeCode accepted a 401")
	} else if !strings.Contains(err.Error(), "401") {
		t.Fatalf("error = %v, want the status", err)
	}

	client.APIBaseURL = server.URL
	if _, err := client.CurrentUser(context.Background(), "access-token"); err == nil {
		t.Fatal("CurrentUser accepted a 401")
	}
}

func TestCurrentUserAndGuildMembership(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer access-token" {
			t.Errorf("authorization = %q", got)
		}
		switch r.URL.Path {
		case "/users/@me":
			_, _ = w.Write([]byte(`{"id":"123456789012345678","username":"keldor","global_name":"Keldor"}`))
		case "/users/@me/guilds":
			_, _ = w.Write([]byte(`[{"id":"111"},{"id":"222"}]`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	client := NewClient(server.Client())
	client.APIBaseURL = server.URL
	profile, err := client.CurrentUser(context.Background(), "access-token")
	if err != nil {
		t.Fatalf("CurrentUser: %v", err)
	}
	if profile.ID != "123456789012345678" || profile.DisplayName() != "Keldor" {
		t.Fatalf("profile = %+v", profile)
	}
	member, err := client.GuildMember(context.Background(), "access-token", "222")
	if err != nil || !member {
		t.Fatalf("GuildMember(222) = %v, %v; want true", member, err)
	}
	member, err = client.GuildMember(context.Background(), "access-token", "333")
	if err != nil || member {
		t.Fatalf("GuildMember(333) = %v, %v; want false", member, err)
	}
}
