// Package api is the REST/HTML entry layer. Handlers call services only;
// neither this package nor internal/web may import a repository package
// (Design.md L109-L120; enforced by importguard_test.go).
package api

import (
	"context"
	"errors"
	"io/fs"
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/Panazule/guildbank/internal/service"
	"github.com/Panazule/guildbank/internal/web"
)

// HealthProbe is the readiness surface consumed by the health handlers.
type HealthProbe interface {
	Ready(ctx context.Context) service.Readiness
}

// Deps are the router dependencies wired by the application.
type Deps struct {
	Logger         *slog.Logger
	Health         HealthProbe
	Setup          *service.SetupService
	Auth           *service.AuthService
	Settings       *service.SettingsService
	Imports        *service.ImportService
	Inventory      *service.InventoryService
	Admin          *service.AdminInventoryService
	Version        string
	TrustedProxies []string
	StartTime      time.Time
	SecureCookies  bool
}

// NewRouter builds the Gin engine with the mandated middleware order
// recovery -> request ID -> access log -> setup gate -> CSRF -> session ->
// authorization (IC-20, IP Phase 2).
func NewRouter(deps Deps) (*gin.Engine, error) {
	if deps.Logger == nil {
		return nil, errors.New("api: logger is required")
	}
	if deps.Health == nil {
		return nil, errors.New("api: health probe is required")
	}
	if deps.Setup == nil || deps.Auth == nil || deps.Settings == nil || deps.Imports == nil || deps.Inventory == nil {
		return nil, errors.New("api: setup, auth, settings, imports and inventory services are required")
	}
	if deps.Admin == nil {
		// Tests and degraded configurations may omit the admin service; a nil
		// store keeps every admin route responding 503 rather than panicking.
		deps.Admin = service.NewAdminInventoryService(nil, nil, nil, nil, deps.Logger)
	}
	if deps.StartTime.IsZero() {
		deps.StartTime = time.Now()
	}

	csrf, err := newCSRFManager(deps.SecureCookies)
	if err != nil {
		return nil, err
	}
	viewSet, err := newViews(web.Templates)
	if err != nil {
		return nil, err
	}

	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(Recovery(deps.Logger), RequestID(), AccessLog(deps.Logger))
	if err := r.SetTrustedProxies(deps.TrustedProxies); err != nil {
		return nil, err
	}
	r.HandleMethodNotAllowed = true

	h := &healthHandler{health: deps.Health, version: deps.Version, start: deps.StartTime}
	r.GET("/healthz", h.healthz)
	r.GET("/readyz", h.readyz)

	staticRoot, err := fs.Sub(web.Static, "static")
	if err != nil {
		return nil, err
	}
	r.StaticFS("/static", http.FS(staticRoot))

	r.Use(SecurityHeaders(), SetupGate(deps.Setup, deps.Logger))

	authHandler := &authHandler{auth: deps.Auth, views: viewSet, logger: deps.Logger, secure: deps.SecureCookies, csrfMgr: csrf}
	setupHandler := &setupHandler{setup: deps.Setup, auth: authHandler, views: viewSet, logger: deps.Logger}
	settingsHandler := &settingsHandler{settings: deps.Settings, views: viewSet, logger: deps.Logger}
	importHandler := &importsHandler{imports: deps.Imports, views: viewSet, logger: deps.Logger}
	inventoryHandler := &inventoryHandler{inventory: deps.Inventory, auth: deps.Auth, views: viewSet, logger: deps.Logger}
	adminHandler := &adminInventoryHandler{admin: deps.Admin, views: viewSet, logger: deps.Logger}
	ui := &uiHandler{views: viewSet}

	// Public forms (login and setup) need a CSRF token even though no session
	// exists yet. Protected routes authenticate first so an absent session
	// yields the documented 401/redirect rather than a CSRF 403.
	public := r.Group("")
	public.Use(csrf.middleware())
	public.GET("/api/v1/auth/csrf", authHandler.csrf)
	public.GET("/login", authHandler.loginPage)
	public.POST("/api/v1/auth/login", authHandler.login)
	public.GET("/setup", setupHandler.page)
	public.POST("/setup/admin", setupHandler.validateAdmin)
	public.POST("/setup/guild", setupHandler.validateGuild)
	public.POST("/setup/database/test", setupHandler.testDatabase)
	public.POST("/setup/complete", setupHandler.complete)
	public.GET("/", inventoryHandler.page)
	public.GET("/inventory/:id", inventoryHandler.detailPage)
	public.GET("/api/v1/items", inventoryHandler.list)
	public.GET("/api/v1/items/:id", inventoryHandler.get)

	admin := r.Group("/")
	admin.Use(RequireAuth(deps.Auth, deps.SecureCookies), csrf.middleware(), RequireAdmin())
	admin.GET("/admin", ui.adminRoot)
	admin.GET("/admin/settings", settingsHandler.page)
	admin.GET("/admin/imports", importHandler.page)
	admin.GET("/admin/imports/:batchID", importHandler.detailPage)
	admin.GET("/api/v1/auth/session", authHandler.session)
	admin.POST("/api/v1/auth/logout", authHandler.logout)
	admin.GET("/api/v1/settings", settingsHandler.get)
	admin.PUT("/api/v1/settings", settingsHandler.update)
	// HTML forms cannot issue PUT; POST is a thin adapter onto the same REST
	// handler and service, preserving the D-03 single write path.
	admin.POST("/api/v1/settings", settingsHandler.update)
	admin.GET("/api/v1/imports", importHandler.list)
	admin.POST("/api/v1/imports", importHandler.upload)
	admin.GET("/api/v1/imports/:batchID", importHandler.get)
	admin.GET("/api/v1/imports/:batchID/locations", importHandler.locations)
	admin.GET("/api/v1/imports/:batchID/rows", importHandler.rows)
	admin.GET("/api/v1/imports/:batchID/row-ids", importHandler.rowIDs)
	admin.GET("/api/v1/imports/:batchID/selection", importHandler.selection)
	admin.PUT("/api/v1/imports/:batchID/selection", importHandler.putSelection)
	admin.GET("/api/v1/imports/:batchID/summary", importHandler.summary)
	admin.POST("/api/v1/imports/:batchID/commit", importHandler.commit)

	// Phase 5 administrative inventory and audit viewer. The group above
	// already enforces RequireAuth -> CSRF -> RequireAdmin for every route.
	admin.GET("/admin/inventory", adminHandler.page)
	admin.GET("/admin/inventory/new", adminHandler.newPage)
	admin.GET("/admin/inventory/:id", adminHandler.detailPage)
	admin.GET("/admin/inventory/:id/edit", adminHandler.editPage)
	admin.GET("/admin/inventory/:id/remove", adminHandler.removePage)
	admin.POST("/admin/inventory", adminHandler.createForm)
	admin.POST("/admin/inventory/:id", adminHandler.updateForm)
	admin.POST("/admin/inventory/:id/remove", adminHandler.removeForm)
	admin.POST("/admin/inventory/:id/restore", adminHandler.restoreForm)
	admin.GET("/admin/audit", adminHandler.auditPage)
	admin.GET("/api/v1/admin/items", adminHandler.list)
	admin.GET("/api/v1/admin/items/:id", adminHandler.getItem)
	admin.POST("/api/v1/items", adminHandler.create)
	admin.PUT("/api/v1/items/:id", adminHandler.update)
	admin.DELETE("/api/v1/items/:id", adminHandler.remove)
	admin.POST("/api/v1/items/:id/restore", adminHandler.restore)
	admin.GET("/api/v1/audit", adminHandler.auditList)
	admin.GET("/api/v1/audit/:id", adminHandler.auditEntry)

	r.NoRoute(notFound)
	r.NoMethod(methodNotAllowed)
	return r, nil
}

func notFound(c *gin.Context) {
	if wantsHTML(c) {
		writeHTMLStatus(c, http.StatusNotFound, "Not found", "The requested page does not exist.")
		return
	}
	writeError(c, http.StatusNotFound, "not_found", "route not found")
}

func methodNotAllowed(c *gin.Context) {
	if wantsHTML(c) {
		writeHTMLStatus(c, http.StatusMethodNotAllowed, "Method not allowed", "That method is not supported for this address.")
		return
	}
	writeError(c, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed for this route")
}
