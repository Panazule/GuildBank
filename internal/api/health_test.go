package api

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/Panazule/guildbank/internal/repository/sqlite"
	"github.com/Panazule/guildbank/internal/service"
)

type fakeHealth struct{ result service.Readiness }

func (f fakeHealth) Ready(context.Context) service.Readiness { return f.result }

func newTestServices(t *testing.T) (service.SetupService, *service.AuthService, *service.SettingsService) {
	t.Helper()
	store, err := sqlite.Open(context.Background(), filepath.Join(t.TempDir(), "guildbank.db"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if _, err := store.Migrate(context.Background()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if _, err := store.DB().Exec(`
		INSERT INTO Users (UserName, PasswordHash, AccessLevel, ModAt, CreatedAt)
		VALUES ('admin', 'unused-test-hash', 'Admin', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`); err != nil {
		t.Fatalf("seed admin: %v", err)
	}
	settings := service.NewSettingsService(store)
	return *service.NewSetupService(store, store, nil), service.NewAuthService(store, store, settings), settings
}

func newTestRouter(t *testing.T, result service.Readiness) *gin.Engine {
	t.Helper()
	setup, auth, settings := newTestServices(t)
	r, err := NewRouter(Deps{
		Logger:   slog.New(slog.NewJSONHandler(&strings.Builder{}, nil)),
		Health:   fakeHealth{result: result},
		Setup:    &setup,
		Auth:     auth,
		Settings: settings,
		Imports:  service.NewImportService(nil, nil, settings, nil, nil),
		Version:  "test",
	})
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}
	return r
}

func TestHealthz(t *testing.T) {
	r := newTestRouter(t, service.Readiness{Ready: true})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/healthz", nil))

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	var body liveResponse
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("json: %v", err)
	}
	if body.Status != "ok" || body.Version != "test" {
		t.Fatalf("body = %+v", body)
	}
}

func TestReadyzStatuses(t *testing.T) {
	ready := service.Readiness{Ready: true, Checks: []service.Check{{Name: "sqlite", Status: "ok"}}}
	r := newTestRouter(t, ready)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("ready: status = %d, want 200", w.Code)
	}

	notReady := service.Readiness{Ready: false, Checks: []service.Check{
		{Name: "sqlite", Status: "ok"},
		{Name: "setup", Status: "pending", Reason: "setup required"},
	}}
	r = newTestRouter(t, notReady)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("not ready: status = %d, want 503", w.Code)
	}
	var body readyResponse
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("json: %v", err)
	}
	if body.Status != "not_ready" || len(body.Checks) != 2 {
		t.Fatalf("body = %+v", body)
	}
}

func TestRequestIDGeneratedAndPropagated(t *testing.T) {
	r := newTestRouter(t, service.Readiness{Ready: true})

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	id := w.Header().Get("X-Request-ID")
	if len(id) != 32 {
		t.Fatalf("generated request id %q, want 32 hex chars", id)
	}

	w = httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	req.Header.Set("X-Request-ID", "proxy-request-123")
	r.ServeHTTP(w, req)
	if got := w.Header().Get("X-Request-ID"); got != "proxy-request-123" {
		t.Fatalf("request id = %q, want supplied value", got)
	}

	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/healthz", nil)
	req.Header.Set("X-Request-ID", "bad id\n")
	r.ServeHTTP(w, req)
	if got := w.Header().Get("X-Request-ID"); got == "bad id\n" || len(got) != 32 {
		t.Fatalf("unsafe request id was kept: %q", got)
	}
}

func TestRecoveryReturnsJSON(t *testing.T) {
	r := newTestRouter(t, service.Readiness{Ready: true})
	r.GET("/panic", func(*gin.Context) { panic("boom") })

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/panic", nil))
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", w.Code)
	}
	if !strings.Contains(w.Body.String(), `"internal_error"`) {
		t.Fatalf("body = %s", w.Body.String())
	}
}

func TestNotFound(t *testing.T) {
	r := newTestRouter(t, service.Readiness{Ready: true})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/missing", nil))
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", w.Code)
	}
	if !strings.Contains(w.Body.String(), `"not_found"`) {
		t.Fatalf("body = %s", w.Body.String())
	}
}
