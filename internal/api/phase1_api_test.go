package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/Panazule/guildbank/internal/domain"
	"github.com/Panazule/guildbank/internal/repository/sqlite"
	"github.com/Panazule/guildbank/internal/service"
)

type setupDBStub struct{ err error }

func (s setupDBStub) TestDatabase(context.Context, service.DatabaseConfig) error { return s.err }

type harness struct {
	t      *testing.T
	server *httptest.Server
	client *http.Client
	store  *sqlite.Store
	router *gin.Engine
}

func newHarness(t *testing.T, secure bool) *harness {
	t.Helper()
	store, err := sqlite.Open(context.Background(), filepath.Join(t.TempDir(), "guildbank.db"))
	if err != nil {
		t.Fatalf("sqlite.Open: %v", err)
	}
	if _, err := store.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	settings := service.NewSettingsService(store)
	setup := service.NewSetupService(store, store, setupDBStub{})
	auth := service.NewAuthService(store, store, settings)
	users := service.NewUserAdminService(store, nil, slog.New(slog.NewJSONHandler(io.Discard, nil)))
	characters := service.NewCharacterService(store, nil, slog.New(slog.NewJSONHandler(io.Discard, nil)))
	router, err := NewRouter(Deps{
		Logger:        slog.New(slog.NewJSONHandler(io.Discard, nil)),
		Health:        fakeHealth{result: service.Readiness{Ready: true}},
		Setup:         setup,
		Auth:          auth,
		Settings:      settings,
		Users:         users,
		Characters:    characters,
		Imports:       service.NewImportService(nil, nil, settings, nil, nil),
		Inventory:     service.NewInventoryService(nil, settings, nil, nil),
		Version:       "test",
		SecureCookies: secure,
	})
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}
	server := httptest.NewServer(router)
	jar, _ := cookiejar.New(nil)
	client := &http.Client{
		Jar: jar,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
		Timeout: 10 * time.Second,
	}
	t.Cleanup(func() {
		server.Close()
		_ = store.Close()
	})
	return &harness{t: t, server: server, client: client, store: store, router: router}
}

func (h *harness) request(method, path, csrf string, body []byte, accept string) (*http.Response, []byte) {
	h.t.Helper()
	req, err := http.NewRequest(method, h.server.URL+path, bytes.NewReader(body))
	if err != nil {
		h.t.Fatal(err)
	}
	if len(body) > 0 {
		req.Header.Set("Content-Type", "application/json")
	}
	if csrf != "" {
		req.Header.Set("X-CSRF-Token", csrf)
	}
	if accept != "" {
		req.Header.Set("Accept", accept)
	}
	resp, err := h.client.Do(req)
	if err != nil {
		h.t.Fatalf("%s %s: %v", method, path, err)
	}
	data, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	return resp, data
}

func (h *harness) csrf() string {
	h.t.Helper()
	resp, body := h.request(http.MethodGet, "/api/v1/auth/csrf", "", nil, "application/json")
	if resp.StatusCode != http.StatusOK {
		h.t.Fatalf("csrf status = %d: %s", resp.StatusCode, body)
	}
	var payload struct {
		Data struct {
			Token string `json:"csrfToken"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		h.t.Fatalf("csrf json: %v", err)
	}
	if payload.Data.Token == "" {
		h.t.Fatal("empty csrf token")
	}
	return payload.Data.Token
}

func setupPayload() map[string]string {
	return map[string]string{
		"userName": "admin", "password": "a long enough password", "confirmPassword": "a long enough password",
		"guildName": "Aeternus", "guildOpenDays": "14", "bazaarDays": "30",
		"eqResourceBaseUrl": "https://items.eqresource.com/items.php?id=",
		"mariaDbHost":       "db.internal", "mariaDbPort": "3306", "mariaDbDatabase": "GuildBank",
		"mariaDbUsername": "guildbank", "mariaDbPassword": "db-password",
	}
}

func (h *harness) completeSetup() string {
	h.t.Helper()
	csrf := h.csrf()
	body, _ := json.Marshal(setupPayload())
	resp, data := h.request(http.MethodPost, "/setup/complete", csrf, body, "application/json")
	if resp.StatusCode != http.StatusOK {
		h.t.Fatalf("setup complete = %d: %s", resp.StatusCode, data)
	}
	return csrf
}

func TestSetupWizardCompletesAndPersistsSettings(t *testing.T) {
	h := newHarness(t, false)

	resp, body := h.request(http.MethodGet, "/setup", "", nil, "text/html")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /setup = %d", resp.StatusCode)
	}
	for _, want := range []string{"Setup wizard", `name="UserName"`, `name="MariaDbHost"`, `name="csrf_token"`} {
		if !strings.Contains(string(body), want) {
			t.Fatalf("setup page missing %q", want)
		}
	}

	payload, _ := json.Marshal(setupPayload())
	resp, _ = h.request(http.MethodPost, "/setup/complete", "", payload, "application/json")
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("setup without CSRF = %d, want 403", resp.StatusCode)
	}
	state, err := h.store.SetupStatus(context.Background())
	if err != nil || state.String() != "pending" {
		t.Fatalf("setup state after blocked request = %v, %v", state, err)
	}

	h.completeSetup()
	state, err = h.store.SetupStatus(context.Background())
	if err != nil || state.String() != "complete" {
		t.Fatalf("setup state = %v, %v", state, err)
	}
	admins, _ := h.store.CountAdministrators(context.Background())
	if admins != 1 {
		t.Fatalf("administrators = %d, want 1", admins)
	}

	resp, _ = h.request(http.MethodGet, "/api/v1/auth/session", "", nil, "application/json")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("session after setup = %d", resp.StatusCode)
	}

	resp, body = h.request(http.MethodGet, "/api/v1/settings", "", nil, "application/json")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("settings GET = %d: %s", resp.StatusCode, body)
	}
	if strings.Contains(string(body), "db-password") {
		t.Fatalf("settings response leaked the MariaDB password: %s", body)
	}
	var settingsBody struct {
		Data []settingResponse `json:"data"`
	}
	if err := json.Unmarshal(body, &settingsBody); err != nil {
		t.Fatalf("settings json: %v", err)
	}
	secretSeen := false
	for _, row := range settingsBody.Data {
		if row.Key == "MariaDbPassword" {
			secretSeen = true
			if row.Value != "" || !row.IsSecret {
				t.Fatalf("secret setting = %+v", row)
			}
		}
	}
	if !secretSeen {
		t.Fatal("MariaDbPassword not returned as a redacted descriptor")
	}

	csrf := h.csrf()
	update, _ := json.Marshal(map[string]any{"settings": map[string]string{
		"GuildName": "Renamed", "GuildOpenDays": "21", "BazaarDays": "30",
	}})
	resp, body = h.request(http.MethodPut, "/api/v1/settings", csrf, update, "application/json")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("settings update = %d: %s", resp.StatusCode, body)
	}
	values, _ := h.store.ListSettings(context.Background())
	if v, _ := settingsValue(values, "GuildName"); v != "Renamed" {
		t.Fatalf("GuildName = %q", v)
	}

	bad, _ := json.Marshal(map[string]any{"settings": map[string]string{"GuildOpenDays": "-1"}})
	resp, body = h.request(http.MethodPut, "/api/v1/settings", csrf, bad, "application/json")
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("invalid settings = %d: %s", resp.StatusCode, body)
	}
	values, _ = h.store.ListSettings(context.Background())
	if v, _ := settingsValue(values, "GuildOpenDays"); v != "21" {
		t.Fatalf("invalid update changed GuildOpenDays to %q", v)
	}

	resp, body = h.request(http.MethodGet, "/setup", "", nil, "text/html")
	if resp.StatusCode != http.StatusGone {
		t.Fatalf("GET /setup after completion = %d, want 410", resp.StatusCode)
	}
	if strings.Contains(string(body), "Setup wizard") {
		t.Fatalf("410 response leaked the setup page body: %s", body)
	}
	resp, _ = h.request(http.MethodPost, "/setup/admin", "", []byte(`{}`), "application/json")
	if resp.StatusCode != http.StatusGone {
		t.Fatalf("POST /setup/admin after completion = %d, want 410", resp.StatusCode)
	}
}

func settingsValue(rows []domain.Setting, key string) (string, bool) {
	for _, row := range rows {
		if row.Key == key {
			return row.Value, true
		}
	}
	return "", false
}

func TestDefaultDenyAndCSRF(t *testing.T) {
	h := newHarness(t, false)
	h.completeSetup()

	unauth := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	req, _ := http.NewRequest(http.MethodGet, h.server.URL+"/admin/settings", nil)
	req.Header.Set("Accept", "text/html")
	resp, err := unauth.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther || !strings.HasPrefix(resp.Header.Get("Location"), "/login") {
		t.Fatalf("HTML admin without session = %d %q", resp.StatusCode, resp.Header.Get("Location"))
	}

	req, _ = http.NewRequest(http.MethodGet, h.server.URL+"/api/v1/settings", nil)
	req.Header.Set("Accept", "application/json")
	resp, err = unauth.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("API settings without session = %d, want 401", resp.StatusCode)
	}

	req, _ = http.NewRequest(http.MethodPost, h.server.URL+"/api/v1/auth/logout", strings.NewReader("{}"))
	req.Header.Set("Content-Type", "application/json")
	resp, err = unauth.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("logout without session = %d, want 401", resp.StatusCode)
	}

	csrf := h.csrf()
	update, _ := json.Marshal(map[string]any{"settings": map[string]string{"GuildName": "Hacked"}})
	resp, body := h.request(http.MethodPut, "/api/v1/settings", "", update, "application/json")
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("settings without CSRF = %d: %s", resp.StatusCode, body)
	}
	values, _ := h.store.ListSettings(context.Background())
	if v, _ := settingsValue(values, "GuildName"); v == "Hacked" {
		t.Fatal("CSRF rejection still changed settings")
	}
	_ = csrf
}

func TestLoginFailureParityAndCookieFlags(t *testing.T) {
	h := newHarness(t, false)
	h.completeSetup()

	jar, _ := cookiejar.New(nil)
	client := &http.Client{
		Jar: jar,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	getCSRF := func() string {
		resp, err := client.Get(h.server.URL + "/api/v1/auth/csrf")
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var payload struct {
			Data struct {
				Token string `json:"csrfToken"`
			} `json:"data"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		return payload.Data.Token
	}
	postLogin := func(token, user, password string) (*http.Response, []byte) {
		body, _ := json.Marshal(loginRequest{UserName: user, Password: password})
		req, _ := http.NewRequest(http.MethodPost, h.server.URL+"/api/v1/auth/login", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json")
		req.Header.Set("X-CSRF-Token", token)
		resp, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		data, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		return resp, data
	}

	token := getCSRF()
	wrongResp, wrongBody := postLogin(token, "admin", "wrong password")
	unknownResp, unknownBody := postLogin(token, "ghost", "wrong password")
	if wrongResp.StatusCode != http.StatusUnauthorized || unknownResp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("wrong=%d unknown=%d", wrongResp.StatusCode, unknownResp.StatusCode)
	}
	var wrongErr, unknownErr struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(wrongBody, &wrongErr); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(unknownBody, &unknownErr); err != nil {
		t.Fatal(err)
	}
	if wrongErr.Error.Code != unknownErr.Error.Code || wrongErr.Error.Message != unknownErr.Error.Message {
		t.Fatalf("failure responses differ:\nwrong  %+v\nunknown %+v", wrongErr, unknownErr)
	}

	resp, body := postLogin(token, "admin", "a long enough password")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("login = %d: %s", resp.StatusCode, body)
	}
	cookies := resp.Cookies()
	var session *http.Cookie
	for _, cookie := range cookies {
		if cookie.Name == sessionCookieName {
			session = cookie
		}
	}
	if session == nil {
		t.Fatal("login did not set the session cookie")
	}
	if !session.HttpOnly || session.Secure || session.SameSite != http.SameSiteLaxMode {
		t.Fatalf("session cookie flags = %+v", session)
	}
	if session.Value == "" || session.Value == "admin" {
		t.Fatalf("session cookie value = %q", session.Value)
	}
	var rawMatches int
	if err := h.store.DB().QueryRow(`SELECT COUNT(1) FROM Sessions WHERE SessionID = ? OR TokenHash = ?`,
		session.Value, session.Value).Scan(&rawMatches); err != nil {
		t.Fatalf("inspect sessions: %v", err)
	}
	if rawMatches != 0 {
		t.Fatal("raw session token was stored in SQLite")
	}

	req, err := http.NewRequest(http.MethodGet, h.server.URL+"/api/v1/auth/session", nil)
	req.Header.Set("Accept", "application/json")
	resp, err = client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, _ = io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("session = %d: %s", resp.StatusCode, body)
	}

	token = getCSRF()
	req, _ = http.NewRequest(http.MethodPost, h.server.URL+"/api/v1/auth/logout", nil)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-CSRF-Token", token)
	resp, err = client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("logout = %d, want 204", resp.StatusCode)
	}
	req, _ = http.NewRequest(http.MethodGet, h.server.URL+"/api/v1/auth/session", nil)
	req.Header.Set("Accept", "application/json")
	resp, err = client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("session after logout = %d, want 401", resp.StatusCode)
	}
}

func TestLoginRateLimitReturns429OnFifthFailure(t *testing.T) {
	h := newHarness(t, false)
	h.completeSetup()
	token := h.csrf()
	body, _ := json.Marshal(loginRequest{UserName: "blockme", Password: "wrong password"})
	for i := 1; i <= 4; i++ {
		resp, data := h.request(http.MethodPost, "/api/v1/auth/login", token, body, "application/json")
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("attempt %d = %d: %s", i, resp.StatusCode, data)
		}
	}
	resp, data := h.request(http.MethodPost, "/api/v1/auth/login", token, body, "application/json")
	if resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("fifth attempt = %d: %s", resp.StatusCode, data)
	}
	if resp.Header.Get("Retry-After") == "" {
		t.Fatal("429 response lacks Retry-After")
	}
}

func TestSecureSessionCookieFlag(t *testing.T) {
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	setSessionCookie(c, "token", 60, true)
	cookies := rec.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("cookies = %d", len(cookies))
	}
	cookie := cookies[0]
	if !cookie.Secure || !cookie.HttpOnly || cookie.SameSite != http.SameSiteLaxMode {
		t.Fatalf("cookie = %+v", cookie)
	}
}
