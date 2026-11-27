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

	"github.com/Panazule/guildbank/internal/domain"
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
	Requests       *service.RequestService
	Admin          *service.AdminInventoryService
	Users          *service.UserAdminService
	Characters     *service.CharacterService
	Discord        *service.DiscordService
	Registration   *service.RegistrationService
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
		deps.Admin = service.NewAdminInventoryService(nil, nil, nil, nil, nil, deps.Logger)
	}
	if deps.Users == nil {
		// Accounts live in SQLite, so this is only reached by a caller that
		// forgot to wire the service; the routes then answer 503.
		deps.Users = service.NewUserAdminService(nil, nil, deps.Logger)
	}
	if deps.Characters == nil {
		deps.Characters = service.NewCharacterService(nil, nil, deps.Logger)
	}
	if deps.Discord == nil {
		deps.Discord = service.NewDiscordService(nil, nil, nil, nil, nil, deps.Logger)
	}
	if deps.Requests == nil {
		// A degraded request service keeps the member routes answering 503
		// rather than panicking when the guild store is absent.
		deps.Requests = service.NewRequestService(nil, nil, nil, deps.Logger)
	}
	if deps.Registration == nil {
		// Self-registration degrades to "unavailable" when it is not wired; the
		// public routes still render, they just never offer the consent anchor.
		deps.Registration = service.NewRegistrationService(nil, nil, nil, nil, deps.Logger)
	}
	if deps.StartTime.IsZero() {
		deps.StartTime = time.Now()
	}

	csrf, err := newCSRFManager(deps.SecureCookies)
	if err != nil {
		return nil, err
	}
	discordState, err := newDiscordStateManager(deps.SecureCookies)
	if err != nil {
		return nil, err
	}
	ticketSigner, err := newRegistrationTicketSigner(deps.SecureCookies)
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

	authHandler := &authHandler{auth: deps.Auth, discord: deps.Discord, registration: deps.Registration,
		views: viewSet, logger: deps.Logger, secure: deps.SecureCookies, csrfMgr: csrf}
	setupHandler := &setupHandler{setup: deps.Setup, auth: authHandler, views: viewSet, logger: deps.Logger}
	settingsHandler := &settingsHandler{settings: deps.Settings, views: viewSet, logger: deps.Logger}
	importHandler := &importsHandler{imports: deps.Imports, views: viewSet, logger: deps.Logger}
	inventoryHandler := &inventoryHandler{inventory: deps.Inventory, requests: deps.Requests,
		auth: deps.Auth, views: viewSet, logger: deps.Logger}
	requestsHandler := &requestHandler{requests: deps.Requests, views: viewSet, logger: deps.Logger}
	adminHandler := &adminInventoryHandler{admin: deps.Admin, views: viewSet, logger: deps.Logger}
	adminRequestsHandler := &adminRequestHandler{admin: deps.Admin, views: viewSet, logger: deps.Logger}
	usersHandler := &usersHandler{users: deps.Users, characters: deps.Characters, discord: deps.Discord,
		views: viewSet, logger: deps.Logger}
	profileHandler := &profileHandler{characters: deps.Characters, discord: deps.Discord, views: viewSet, logger: deps.Logger}
	discordHandler := &discordHandler{discord: deps.Discord, auth: deps.Auth, state: discordState,
		csrfMgr: csrf, views: viewSet, logger: deps.Logger, secure: deps.SecureCookies}
	registrationHandler := &registrationHandler{registration: deps.Registration, auth: deps.Auth,
		state: discordState, ticketSigner: ticketSigner, csrfMgr: csrf, views: viewSet,
		logger: deps.Logger, secure: deps.SecureCookies}
	ui := &uiHandler{admin: deps.Admin, users: deps.Users, views: viewSet}

	// Public forms (login and setup) need a CSRF token even though no session
	// exists yet. Protected routes authenticate first so an absent session
	// yields the documented 401/redirect rather than a CSRF 403.
	public := r.Group("")
	public.Use(csrf.middleware())
	public.GET("/api/v1/auth/csrf", authHandler.csrf)
	public.GET("/login", authHandler.loginPage)
	public.POST("/api/v1/auth/login", authHandler.login)
	// Discord sign-in is an alternative credential for an existing account. It
	// never registers anyone; an unlinked Discord user is refused neutrally.
	public.GET("/auth/discord", discordHandler.loginStart)
	public.GET("/auth/discord/callback", discordHandler.loginCallback)
	// Self-registration (P4-SR). Public by design: it creates a Member account
	// and never signs into an existing one. Every OAuth start is a GET
	// navigation so CSP form-action 'self' cannot cancel the cross-origin hop.
	public.GET("/register", registrationHandler.page)
	public.GET("/register/discord", registrationHandler.start)
	public.GET("/register/discord/callback", registrationHandler.callback)
	public.GET("/register/complete", registrationHandler.form)
	public.POST("/register/complete", registrationHandler.complete)
	public.GET("/setup", setupHandler.page)
	public.POST("/setup/admin", setupHandler.validateAdmin)
	public.POST("/setup/guild", setupHandler.validateGuild)
	public.POST("/setup/database/test", setupHandler.testDatabase)
	public.POST("/setup/complete", setupHandler.complete)
	public.GET("/", inventoryHandler.page)
	public.GET("/inventory/:id", inventoryHandler.detailPage)
	public.GET("/api/v1/items", inventoryHandler.list)
	public.GET("/api/v1/items/:id", inventoryHandler.get)

	// Authenticated routes. Deny-by-default: every route belongs to exactly one
	// group below, and authorization_matrix_test.go fails when a route is added
	// without a classification. Order is authentication, then role, then CSRF so
	// a caller who may not use a route never needs a token for it.
	member := r.Group("")
	member.Use(RequireAuth(deps.Auth, deps.SecureCookies), RequirePasswordChanged(),
		RequireRole(domain.RoleMember), csrf.middleware())
	// Session inspection and logout are available to every signed-in role, and
	// the forced password change is the only other thing a flagged account may
	// reach (see RequirePasswordChanged).
	member.GET("/api/v1/auth/session", authHandler.session)
	member.POST("/api/v1/auth/logout", authHandler.logout)
	member.GET("/account/password", usersHandler.passwordPage)
	// Separate endpoints for the two account states (D-SR-19). /set refuses an
	// account that already has a password; /change requires the current one.
	// /api/v1/auth/password remains as the pre-0005 alias of /change so existing
	// API clients keep working; it can only ever change an existing password.
	member.POST("/api/v1/auth/password/set", usersHandler.setPassword)
	member.POST("/api/v1/auth/password/change", usersHandler.changePassword)
	member.POST("/api/v1/auth/password", usersHandler.changePassword)
	// The profile is the account owner's own character list (P3 Phase A).
	member.GET("/account/profile", profileHandler.page)
	member.GET("/api/v1/me/characters", profileHandler.list)
	member.POST("/api/v1/me/characters", profileHandler.add)
	member.DELETE("/api/v1/me/characters/:id", profileHandler.remove)
	// HTML forms cannot issue DELETE; this is a thin adapter onto the same service.
	member.POST("/api/v1/me/characters/:id/delete", profileHandler.removeForm)
	// Member request workflow (Phase 3). Officer decisions and the public form
	// are later phases; /request/new is member-only until Phase 5 makes it the
	// shared public entry point.
	member.GET("/request/new", requestsHandler.newForm)
	member.POST("/request/new", requestsHandler.create)
	member.GET("/my/requests", requestsHandler.mine)
	member.GET("/my/requests/:id", requestsHandler.mineDetail)
	member.POST("/my/requests/:id/cancel", requestsHandler.cancel)
	// Discord account linking (P3 Phase B). There is no Discord login route: the
	// callback only attaches an identity to this existing session.
	// The browser starts the flow with a GET navigation (like sign-in) because a
	// form submission's cross-origin 303 would be cancelled by form-action 'self'.
	member.GET("/account/discord/link", discordHandler.start)
	member.POST("/account/discord/link", discordHandler.start)
	member.GET("/account/discord/callback", discordHandler.callback)
	member.POST("/account/discord/unlink", discordHandler.unlinkForm)
	member.GET("/api/v1/me/discord", discordHandler.identity)
	member.DELETE("/api/v1/me/discord", discordHandler.unlink)

	officer := r.Group("")
	officer.Use(RequireAuth(deps.Auth, deps.SecureCookies), RequirePasswordChanged(),
		RequireRole(domain.RoleOfficer), csrf.middleware())
	officer.GET("/admin", ui.adminHome)
	officer.GET("/admin/imports", importHandler.page)
	officer.GET("/admin/imports/:batchID", importHandler.detailPage)
	officer.GET("/api/v1/imports", importHandler.list)
	officer.POST("/api/v1/imports", importHandler.upload)
	officer.GET("/api/v1/imports/:batchID", importHandler.get)
	officer.GET("/api/v1/imports/:batchID/locations", importHandler.locations)
	officer.GET("/api/v1/imports/:batchID/rows", importHandler.rows)
	officer.GET("/api/v1/imports/:batchID/row-ids", importHandler.rowIDs)
	officer.GET("/api/v1/imports/:batchID/default-selection", importHandler.defaultSelection)
	officer.GET("/api/v1/imports/:batchID/selection", importHandler.selection)
	officer.PUT("/api/v1/imports/:batchID/selection", importHandler.putSelection)
	officer.GET("/api/v1/imports/:batchID/summary", importHandler.summary)
	officer.POST("/api/v1/imports/:batchID/commit", importHandler.commit)

	// Administrative inventory and the audit viewer are Officer work: adding,
	// editing, issuing, returning, removing and restoring inventory, imports and
	// reading history (owner decision 2026-11-26, Q1/Q2).
	officer.GET("/admin/inventory", adminHandler.page)
	officer.GET("/admin/inventory/new", adminHandler.newPage)
	officer.GET("/admin/inventory/:id", adminHandler.detailPage)
	officer.GET("/admin/inventory/:id/edit", adminHandler.editPage)
	officer.GET("/admin/inventory/:id/remove", adminHandler.removePage)
	officer.GET("/admin/inventory/:id/restore", adminHandler.restorePage)
	officer.GET("/admin/inventory/:id/issue", adminHandler.issuePage)
	officer.GET("/admin/inventory/:id/return", adminHandler.returnPage)
	officer.GET("/admin/inventory/:id/movements", adminHandler.movementsPage)
	officer.GET("/admin/inventory/movements/:movementID/return", adminHandler.returnIssuancePage)

	officer.POST("/admin/inventory", adminHandler.createForm)
	officer.POST("/admin/inventory/bulk-remove", adminHandler.bulkRemovePreview)
	officer.POST("/admin/inventory/bulk-remove/apply", adminHandler.bulkRemoveForm)
	officer.POST("/admin/inventory/:id", adminHandler.updateForm)
	officer.POST("/admin/inventory/:id/remove", adminHandler.removeForm)
	officer.POST("/admin/inventory/:id/restore", adminHandler.restoreForm)
	officer.POST("/admin/inventory/:id/issue", adminHandler.issueForm)
	officer.POST("/admin/inventory/:id/return", adminHandler.returnForm)
	officer.POST("/admin/inventory/movements/:movementID/return", adminHandler.returnIssuanceForm)

	// Request decisions are Officer work: approving issues inventory through the
	// same ledger path as a manual issue, and denying records the reason.
	officer.GET("/admin/requests", adminRequestsHandler.queue)
	officer.GET("/admin/requests/:id", adminRequestsHandler.detail)
	officer.POST("/admin/requests/:id/approve", adminRequestsHandler.approve)
	officer.POST("/admin/requests/:id/deny", adminRequestsHandler.deny)

	officer.GET("/admin/audit", adminHandler.auditPage)
	officer.GET("/api/v1/admin/items", adminHandler.list)
	officer.GET("/api/v1/admin/items/:id", adminHandler.getItem)
	officer.GET("/api/v1/admin/item-names", adminHandler.itemNames)
	officer.POST("/api/v1/items", adminHandler.create)
	officer.PUT("/api/v1/items/:id", adminHandler.update)
	officer.DELETE("/api/v1/items/:id", adminHandler.remove)
	officer.POST("/api/v1/items/:id/restore", adminHandler.restore)
	officer.POST("/api/v1/items/:id/issue", adminHandler.issue)
	officer.POST("/api/v1/items/:id/return", adminHandler.returnItem)
	officer.GET("/api/v1/admin/items/:id/movements", adminHandler.movements)
	officer.GET("/api/v1/admin/movements/:movementID", adminHandler.movement)
	officer.GET("/api/v1/audit", adminHandler.auditList)
	officer.GET("/api/v1/audit/:id", adminHandler.auditEntry)

	// Account administration (P2) is Officer work: Officers approve pending
	// accounts and manage users up to their own rank. The service and store
	// refuse any action on an Administrator by a non-Administrator, and an
	// Officer cannot create or promote to Administrator. The HTML pages and the
	// JSON API share one service, so a form and an API client cannot diverge.
	officer.GET("/admin/users", usersHandler.page)
	officer.GET("/admin/users/new", usersHandler.newPage)
	officer.GET("/admin/users/:id", usersHandler.detailPage)
	officer.POST("/admin/users", usersHandler.createForm)
	officer.POST("/admin/users/:id", usersHandler.updateForm)
	officer.POST("/admin/users/:id/password", usersHandler.resetForm)
	officer.POST("/admin/users/:id/delete", usersHandler.deleteForm)
	// Approval is an explicit action rather than a third value in the status
	// select, so each call maps to exactly one audit event (D-SR §8.2).
	officer.POST("/admin/users/:id/approve", usersHandler.approveForm)
	officer.POST("/admin/users/:id/withdraw-approval", usersHandler.withdrawApprovalForm)
	officer.GET("/api/v1/users", usersHandler.list)
	officer.POST("/api/v1/users", usersHandler.create)
	officer.PUT("/api/v1/users/:id", usersHandler.update)
	officer.POST("/api/v1/users/:id", usersHandler.update)
	officer.POST("/api/v1/users/:id/password", usersHandler.resetPassword)
	officer.POST("/api/v1/users/:id/approve", usersHandler.approveAPI)
	officer.POST("/api/v1/users/:id/withdraw-approval", usersHandler.withdrawApprovalAPI)
	officer.DELETE("/api/v1/users/:id", usersHandler.remove)

	// Application configuration and character-name removal stay
	// Administrator-only.
	admin := r.Group("")
	admin.Use(RequireAuth(deps.Auth, deps.SecureCookies), RequirePasswordChanged(),
		RequireRole(domain.RoleAdmin), csrf.middleware())
	admin.GET("/admin/settings", settingsHandler.page)
	admin.GET("/api/v1/settings", settingsHandler.get)
	admin.PUT("/api/v1/settings", settingsHandler.update)
	// HTML forms cannot issue PUT; POST is a thin adapter onto the same REST
	// handler and service, preserving the D-03 single write path.
	admin.POST("/api/v1/settings", settingsHandler.update)

	// Administrators may remove a character name from any account but cannot
	// move it to another account (owner decision D4).
	admin.POST("/admin/users/:id/characters/:characterID/delete", usersHandler.removeCharacterForm)
	admin.DELETE("/api/v1/users/:id/characters/:characterID", usersHandler.removeCharacter)

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
