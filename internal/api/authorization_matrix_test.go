package api

import "testing"

// routeClass is the minimum role a route requires. classPublic means no session
// is needed at all.
type routeClass string

const (
	classPublic  routeClass = "public"
	classMember  routeClass = "member"
	classOfficer routeClass = "officer"
	classAdmin   routeClass = "admin"
)

// routeMatrix is the authoritative classification of every registered route.
// The completeness test fails when a route exists without an entry, so a new
// endpoint can never become public by accident.
var routeMatrix = map[string]routeClass{
	// Liveness, readiness and embedded assets are registered before the setup
	// gate and need no session.
	"GET /healthz":           classPublic,
	"GET /readyz":            classPublic,
	"GET /static/*filepath":  classPublic,
	"HEAD /static/*filepath": classPublic,

	// Bootstrap: CSRF tokens, login and the first-run wizard.
	"GET /api/v1/auth/csrf":     classPublic,
	"GET /login":                classPublic,
	"POST /api/v1/auth/login":   classPublic,
	"GET /setup":                classPublic,
	"POST /setup/admin":         classPublic,
	"POST /setup/guild":         classPublic,
	"POST /setup/database/test": classPublic,
	"POST /setup/complete":      classPublic,

	// Discord sign-in: public by design, creates no account.
	"GET /auth/discord":          classPublic,
	"GET /auth/discord/callback": classPublic,

	// Public guild inventory.
	"GET /":                 classPublic,
	"GET /inventory/:id":    classPublic,
	"GET /api/v1/items":     classPublic,
	"GET /api/v1/items/:id": classPublic,

	// Any authenticated role.
	"GET /api/v1/auth/session":              classMember,
	"POST /api/v1/auth/logout":              classMember,
	"GET /account/password":                 classMember,
	"POST /api/v1/auth/password":            classMember,
	"GET /account/profile":                  classMember,
	"GET /api/v1/me/characters":             classMember,
	"POST /api/v1/me/characters":            classMember,
	"DELETE /api/v1/me/characters/:id":      classMember,
	"POST /api/v1/me/characters/:id/delete": classMember,

	// Discord account linking (Phase B). The browser uses GET for the start so the
	// cross-origin redirect is a navigation, not a form submission (CSP
	// form-action 'self' cancels form redirects to discord.com). POST stays for
	// non-browser clients that want the authorizeUrl JSON.
	"GET /account/discord/link":     classMember,
	"POST /account/discord/link":    classMember,
	"GET /account/discord/callback": classMember,
	"POST /account/discord/unlink":  classMember,
	"GET /api/v1/me/discord":        classMember,
	"DELETE /api/v1/me/discord":     classMember,

	// Officer: imports, inventory administration and history.
	"GET /admin":                                     classOfficer,
	"GET /admin/imports":                             classOfficer,
	"GET /admin/imports/:batchID":                    classOfficer,
	"GET /api/v1/imports":                            classOfficer,
	"POST /api/v1/imports":                           classOfficer,
	"GET /api/v1/imports/:batchID":                   classOfficer,
	"GET /api/v1/imports/:batchID/locations":         classOfficer,
	"GET /api/v1/imports/:batchID/rows":              classOfficer,
	"GET /api/v1/imports/:batchID/row-ids":           classOfficer,
	"GET /api/v1/imports/:batchID/default-selection": classOfficer,
	"GET /api/v1/imports/:batchID/selection":         classOfficer,
	"PUT /api/v1/imports/:batchID/selection":         classOfficer,
	"GET /api/v1/imports/:batchID/summary":           classOfficer,
	"POST /api/v1/imports/:batchID/commit":           classOfficer,

	"GET /admin/inventory":                               classOfficer,
	"GET /admin/inventory/new":                           classOfficer,
	"GET /admin/inventory/:id":                           classOfficer,
	"GET /admin/inventory/:id/edit":                      classOfficer,
	"GET /admin/inventory/:id/remove":                    classOfficer,
	"GET /admin/inventory/:id/restore":                   classOfficer,
	"GET /admin/inventory/:id/issue":                     classOfficer,
	"GET /admin/inventory/:id/return":                    classOfficer,
	"GET /admin/inventory/:id/movements":                 classOfficer,
	"GET /admin/inventory/movements/:movementID/return":  classOfficer,
	"POST /admin/inventory":                              classOfficer,
	"POST /admin/inventory/bulk-remove":                  classOfficer,
	"POST /admin/inventory/bulk-remove/apply":            classOfficer,
	"POST /admin/inventory/:id":                          classOfficer,
	"POST /admin/inventory/:id/remove":                   classOfficer,
	"POST /admin/inventory/:id/restore":                  classOfficer,
	"POST /admin/inventory/:id/issue":                    classOfficer,
	"POST /admin/inventory/:id/return":                   classOfficer,
	"POST /admin/inventory/movements/:movementID/return": classOfficer,

	"GET /admin/audit":                        classOfficer,
	"GET /api/v1/admin/items":                 classOfficer,
	"GET /api/v1/admin/items/:id":             classOfficer,
	"GET /api/v1/admin/item-names":            classOfficer,
	"POST /api/v1/items":                      classOfficer,
	"PUT /api/v1/items/:id":                   classOfficer,
	"DELETE /api/v1/items/:id":                classOfficer,
	"POST /api/v1/items/:id/restore":          classOfficer,
	"POST /api/v1/items/:id/issue":            classOfficer,
	"POST /api/v1/items/:id/return":           classOfficer,
	"GET /api/v1/admin/items/:id/movements":   classOfficer,
	"GET /api/v1/admin/movements/:movementID": classOfficer,
	"GET /api/v1/audit":                       classOfficer,
	"GET /api/v1/audit/:id":                   classOfficer,

	// Administrator: application configuration and account administration.
	"GET /admin/settings":   classAdmin,
	"GET /api/v1/settings":  classAdmin,
	"PUT /api/v1/settings":  classAdmin,
	"POST /api/v1/settings": classAdmin,

	"GET /admin/users":                classAdmin,
	"GET /admin/users/new":            classAdmin,
	"GET /admin/users/:id":            classAdmin,
	"POST /admin/users":               classAdmin,
	"POST /admin/users/:id":           classAdmin,
	"POST /admin/users/:id/password":  classAdmin,
	"POST /admin/users/:id/delete":    classAdmin,
	"GET /api/v1/users":               classAdmin,
	"POST /api/v1/users":              classAdmin,
	"PUT /api/v1/users/:id":           classAdmin,
	"POST /api/v1/users/:id":          classAdmin,
	"DELETE /api/v1/users/:id":        classAdmin,
	"POST /api/v1/users/:id/password": classAdmin,

	// Administrators may remove a character name from any account (D4).
	"POST /admin/users/:id/characters/:characterID/delete": classAdmin,
	"DELETE /api/v1/users/:id/characters/:characterID":     classAdmin,
}

func TestRouteMatrixCoversEveryRoute(t *testing.T) {
	h := newHarness(t, false)
	registered := make(map[string]bool, len(routeMatrix))
	for _, route := range h.router.Routes() {
		key := route.Method + " " + route.Path
		registered[key] = true
		if _, ok := routeMatrix[key]; !ok {
			t.Errorf("route %s has no role classification; add it to routeMatrix", key)
		}
	}
	for key := range routeMatrix {
		if !registered[key] {
			t.Errorf("routeMatrix classifies %s but the router does not register it", key)
		}
	}
}
