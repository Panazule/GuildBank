package api

import (
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/Panazule/guildbank/internal/domain"
)

func itoa(id int64) string { return strconv.FormatInt(id, 10) }

// requestForm posts an HTML form, which is how the account pages submit.
func (h *harness) requestForm(method, path, csrf string, form url.Values, accept string) (*http.Response, []byte) {
	h.t.Helper()
	req, err := http.NewRequest(method, h.server.URL+path, strings.NewReader(form.Encode()))
	if err != nil {
		h.t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
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

func (h *harness) loginWith(t *testing.T, name, password string) (*http.Response, []byte) {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"userName": name, "password": password})
	return h.request(http.MethodPost, "/api/v1/auth/login", h.csrf(), body, "application/json")
}

type userEnvelope struct {
	Data struct {
		User userResponse `json:"user"`
	} `json:"data"`
}

type sessionEnvelope struct {
	Data sessionResponse `json:"data"`
}

func decodeSession(t *testing.T, body []byte) sessionResponse {
	t.Helper()
	var env sessionEnvelope
	if err := json.Unmarshal(body, &env); err != nil {
		t.Fatalf("session json: %v (%s)", err, body)
	}
	return env.Data
}

func (h *harness) userID(t *testing.T, name string) int64 {
	t.Helper()
	var id int64
	if err := h.store.DB().QueryRow(`SELECT UserID FROM Users WHERE UserName = ?`, name).Scan(&id); err != nil {
		t.Fatalf("user id for %s: %v", name, err)
	}
	return id
}

// userExists reports whether an account row with the name is present.
func (h *harness) userExists(t *testing.T, name string) bool {
	t.Helper()
	var n int
	if err := h.store.DB().QueryRow(`SELECT COUNT(1) FROM Users WHERE UserName = ?`, name).Scan(&n); err != nil {
		t.Fatalf("count user %s: %v", name, err)
	}
	return n > 0
}

// userRole reads the stored access level for a name.
func (h *harness) userRole(t *testing.T, name string) domain.Role {
	t.Helper()
	var level string
	if err := h.store.DB().QueryRow(`SELECT AccessLevel FROM Users WHERE UserName = ?`, name).Scan(&level); err != nil {
		t.Fatalf("role for %s: %v", name, err)
	}
	return domain.ParseRole(level)
}

func (h *harness) sessionCount(t *testing.T, userID int64) int {
	t.Helper()
	var n int
	if err := h.store.DB().QueryRow(`SELECT COUNT(1) FROM Sessions WHERE UserID = ?`, userID).Scan(&n); err != nil {
		t.Fatalf("count sessions: %v", err)
	}
	return n
}

func (h *harness) createAccount(t *testing.T, name, role, password string) int64 {
	t.Helper()
	body, _ := json.Marshal(map[string]string{
		"userName": name, "accessLevel": role, "password": password, "confirmPassword": password,
	})
	resp, data := h.request(http.MethodPost, "/api/v1/users", h.csrf(), body, "application/json")
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create %s = %d: %s", name, resp.StatusCode, data)
	}
	var env userEnvelope
	if err := json.Unmarshal(data, &env); err != nil {
		t.Fatalf("create json: %v", err)
	}
	return env.Data.User.ID
}

// TestAccountAdministrationRequiresOfficer proves the account routes refuse a
// Member, for both the pages and the API. Account administration used to be
// Administrator-only; Members must still be turned away.
func TestAccountAdministrationRequiresOfficer(t *testing.T) {
	h := newHarness(t, false)
	h.completeSetup()
	h.seedUser(t, "member1", domain.RoleMember, false)

	h.loginAs(t, "member1")
	for _, probe := range []struct {
		method string
		path   string
		accept string
	}{
		{http.MethodGet, "/api/v1/users", "application/json"},
		{http.MethodGet, "/admin/users", "text/html"},
		{http.MethodGet, "/admin/users/new", "text/html"},
		{http.MethodGet, "/admin/users/1", "text/html"},
	} {
		resp, _ := h.request(probe.method, probe.path, "", nil, probe.accept)
		if resp.StatusCode != http.StatusForbidden {
			t.Fatalf("%s %s as member = %d, want 403", probe.method, probe.path, resp.StatusCode)
		}
	}
	body, _ := json.Marshal(map[string]string{"userName": "Intruder", "accessLevel": "Member",
		"password": "a temporary password", "confirmPassword": "a temporary password"})
	resp, _ := h.request(http.MethodPost, "/api/v1/users", h.csrf(), body, "application/json")
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("create as member = %d, want 403", resp.StatusCode)
	}
}

// TestOfficerAdministersAccountsWithinRankCeiling covers the widened gate: an
// Officer runs day-to-day accounts, but the rank ceiling stops every path to
// creating, promoting or touching an Administrator, and Settings stay closed.
func TestOfficerAdministersAccountsWithinRankCeiling(t *testing.T) {
	h := newHarness(t, false)
	h.completeSetup()
	const temporary = "a temporary password"
	h.seedUser(t, "officer1", domain.RoleOfficer, false)
	adminID := h.userID(t, "admin")

	h.loginAs(t, "officer1")

	// Pages and the list API open for an Officer.
	for _, probe := range []struct {
		method string
		path   string
		accept string
	}{
		{http.MethodGet, "/api/v1/users", "application/json"},
		{http.MethodGet, "/admin/users", "text/html"},
		{http.MethodGet, "/admin/users/new", "text/html"},
		{http.MethodGet, "/admin/users/" + itoa(adminID), "text/html"},
	} {
		resp, data := h.request(probe.method, probe.path, "", nil, probe.accept)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("%s %s as officer = %d: %s", probe.method, probe.path, resp.StatusCode, data)
		}
	}

	// The pages explain the ceiling rather than offering controls that would fail.
	_, listPage := h.request(http.MethodGet, "/admin/users", "", nil, "text/html")
	if !strings.Contains(string(listPage), "outranks yours") {
		t.Fatalf("officer account list does not mark the Administrator as out of reach: %s", listPage)
	}
	_, newPage := h.request(http.MethodGet, "/admin/users/new", "", nil, "text/html")
	if strings.Contains(string(newPage), `<option value="Admin"`) {
		t.Fatalf("officer create form offers an Administrator role: %s", newPage)
	}
	if !strings.Contains(string(newPage), `<option value="Officer"`) {
		t.Fatalf("officer create form does not offer the Officer role: %s", newPage)
	}
	_, adminPage := h.request(http.MethodGet, "/admin/users/"+itoa(adminID), "", nil, "text/html")
	if !strings.Contains(string(adminPage), "This account outranks yours.") {
		t.Fatalf("officer detail page for an Administrator lacks the out-of-reach notice: %s", adminPage)
	}
	if !strings.Contains(string(adminPage), `<select name="accessLevel" disabled>`) {
		t.Fatalf("officer detail page leaves the Administrator role select enabled: %s", adminPage)
	}

	// Settings remain Administrator-only.
	for _, path := range []string{"/admin/settings"} {
		if resp, _ := h.request(http.MethodGet, path, "", nil, "text/html"); resp.StatusCode != http.StatusForbidden {
			t.Fatalf("officer %s = %d, want 403", path, resp.StatusCode)
		}
	}

	// Creating Members and Officers succeeds.
	h.createAccount(t, "Recruit", "Member", temporary)
	h.createAccount(t, "Lieutenant", "Officer", temporary)

	// Creating an Administrator does not.
	body, _ := json.Marshal(map[string]string{"userName": "Overlord", "accessLevel": "Admin",
		"password": temporary, "confirmPassword": temporary})
	resp, data := h.request(http.MethodPost, "/api/v1/users", h.csrf(), body, "application/json")
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("officer create Admin = %d, want 403: %s", resp.StatusCode, data)
	}
	if h.userExists(t, "Overlord") {
		t.Fatal("a refused Administrator create still added the account")
	}

	// Promoting someone to Administrator does not.
	recruitID := h.userID(t, "Recruit")
	roleBody, _ := json.Marshal(map[string]string{"accessLevel": "Admin"})
	resp, data = h.request(http.MethodPut, "/api/v1/users/"+itoa(recruitID), h.csrf(), roleBody, "application/json")
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("officer promote to Admin = %d, want 403: %s", resp.StatusCode, data)
	}
	if role := h.userRole(t, "Recruit"); role != domain.RoleMember {
		t.Fatalf("Recruit role = %q after the refused promotion", role)
	}

	// Promoting a Member to Officer is inside the ceiling.
	officerBody, _ := json.Marshal(map[string]string{"accessLevel": "Officer"})
	resp, data = h.request(http.MethodPut, "/api/v1/users/"+itoa(recruitID), h.csrf(), officerBody, "application/json")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("officer promote to Officer = %d: %s", resp.StatusCode, data)
	}

	// An Administrator account is out of reach: demote, disable, reset and delete.
	demote, _ := json.Marshal(map[string]string{"accessLevel": "Member"})
	resp, data = h.request(http.MethodPut, "/api/v1/users/"+itoa(adminID), h.csrf(), demote, "application/json")
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("officer demote Admin = %d, want 403: %s", resp.StatusCode, data)
	}
	disable, _ := json.Marshal(map[string]bool{"disabled": true})
	resp, data = h.request(http.MethodPost, "/api/v1/users/"+itoa(adminID), h.csrf(), disable, "application/json")
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("officer disable Admin = %d, want 403: %s", resp.StatusCode, data)
	}
	reset, _ := json.Marshal(map[string]string{"password": temporary, "confirmPassword": temporary})
	resp, data = h.request(http.MethodPost, "/api/v1/users/"+itoa(adminID)+"/password", h.csrf(), reset, "application/json")
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("officer reset Admin password = %d, want 403: %s", resp.StatusCode, data)
	}
	confirm, _ := json.Marshal(map[string]string{"confirmUserName": "admin"})
	resp, data = h.request(http.MethodDelete, "/api/v1/users/"+itoa(adminID), h.csrf(), confirm, "application/json")
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("officer delete Admin = %d, want 403: %s", resp.StatusCode, data)
	}
	if role := h.userRole(t, "admin"); role != domain.RoleAdmin {
		t.Fatalf("admin role = %q after the refused modifications", role)
	}
}

func TestAdministratorCreatesPromotesDisablesAndDeletes(t *testing.T) {
	h := newHarness(t, false)
	h.completeSetup()
	const temporary = "a temporary password"

	keeperID := h.createAccount(t, "Keeper", "Officer", temporary)

	// The hash never leaves the service.
	_, listBody := h.request(http.MethodGet, "/api/v1/users?q=keep", "", nil, "application/json")
	if strings.Contains(string(listBody), "passwordHash") {
		t.Fatalf("account list leaked a password hash: %s", listBody)
	}
	if !strings.Contains(string(listBody), "Keeper") {
		t.Fatalf("account list is missing the created account: %s", listBody)
	}

	dupBody, _ := json.Marshal(map[string]string{"userName": "KEEPER", "accessLevel": "Member",
		"password": temporary, "confirmPassword": temporary})
	resp, data := h.request(http.MethodPost, "/api/v1/users", h.csrf(), dupBody, "application/json")
	if resp.StatusCode != http.StatusConflict || !strings.Contains(string(data), "already in use") {
		t.Fatalf("duplicate name = %d: %s", resp.StatusCode, data)
	}

	roleBody, _ := json.Marshal(map[string]string{"accessLevel": "Admin"})
	resp, data = h.request(http.MethodPut, "/api/v1/users/"+itoa(keeperID), h.csrf(), roleBody, "application/json")
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(data), `"accessLevel":"Admin"`) {
		t.Fatalf("promote = %d: %s", resp.StatusCode, data)
	}

	disableBody, _ := json.Marshal(map[string]bool{"disabled": true})
	resp, data = h.request(http.MethodPost, "/api/v1/users/"+itoa(keeperID), h.csrf(), disableBody, "application/json")
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(data), `"disabled":true`) {
		t.Fatalf("disable = %d: %s", resp.StatusCode, data)
	}
	if resp, _ := h.loginWith(t, "Keeper", temporary); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("disabled account login = %d, want 401", resp.StatusCode)
	}

	enableBody, _ := json.Marshal(map[string]bool{"disabled": false})
	resp, _ = h.request(http.MethodPost, "/api/v1/users/"+itoa(keeperID), h.csrf(), enableBody, "application/json")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("enable = %d", resp.StatusCode)
	}
	resp, data = h.loginWith(t, "Keeper", temporary)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("enabled account login = %d: %s", resp.StatusCode, data)
	}
	if !decodeSession(t, data).User.MustChangePassword {
		t.Fatal("a created account did not require a password change at sign-in")
	}

	// Back to the administrator for the remaining writes.
	if resp, _ := h.loginWith(t, "admin", seedPassword); resp.StatusCode != http.StatusOK {
		t.Fatalf("administrator re-login = %d", resp.StatusCode)
	}

	wrongConfirm, _ := json.Marshal(map[string]string{"confirmUserName": "Keper"})
	resp, _ = h.request(http.MethodDelete, "/api/v1/users/"+itoa(keeperID), h.csrf(), wrongConfirm, "application/json")
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("delete with the wrong confirmation = %d, want 422", resp.StatusCode)
	}

	confirm, _ := json.Marshal(map[string]string{"confirmUserName": "keeper"})
	resp, data = h.request(http.MethodDelete, "/api/v1/users/"+itoa(keeperID), h.csrf(), confirm, "application/json")
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("delete = %d: %s", resp.StatusCode, data)
	}
	_, listBody = h.request(http.MethodGet, "/api/v1/users", "", nil, "application/json")
	if strings.Contains(string(listBody), "Keeper") {
		t.Fatalf("the deleted account is still listed: %s", listBody)
	}
	if resp, _ := h.loginWith(t, "Keeper", temporary); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("deleted account login = %d, want 401", resp.StatusCode)
	}
}

func TestLastAdministratorGuardsThroughTheApi(t *testing.T) {
	h := newHarness(t, false)
	h.completeSetup()
	const temporary = "a temporary password"
	adminID := h.userID(t, "admin")
	secondID := h.createAccount(t, "Second", "Admin", temporary)

	// Self-disable is refused even though two administrators exist.
	selfBody, _ := json.Marshal(map[string]bool{"disabled": true})
	resp, _ := h.request(http.MethodPost, "/api/v1/users/"+itoa(adminID), h.csrf(), selfBody, "application/json")
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("self-disable = %d, want 409", resp.StatusCode)
	}

	// Sign in as the second administrator. The account was created with a
	// temporary password, so the forced change comes first.
	if resp, _ := h.loginWith(t, "Second", temporary); resp.StatusCode != http.StatusOK {
		t.Fatalf("second administrator login = %d", resp.StatusCode)
	}
	change, _ := json.Marshal(map[string]string{"currentPassword": temporary,
		"newPassword": "a permanent password", "confirmPassword": "a permanent password"})
	if resp, data := h.request(http.MethodPost, "/api/v1/auth/password", h.csrf(), change, "application/json"); resp.StatusCode != http.StatusOK {
		t.Fatalf("second administrator password change = %d: %s", resp.StatusCode, data)
	}

	// Then disable the first administrator: allowed, because one enabled
	// administrator still remains.
	csrf := h.csrf()
	resp, _ = h.request(http.MethodPost, "/api/v1/users/"+itoa(adminID), csrf, selfBody, "application/json")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("disable the other administrator = %d, want 200", resp.StatusCode)
	}

	// The remaining enabled administrator cannot be demoted or deleted.
	demote, _ := json.Marshal(map[string]string{"accessLevel": "Officer"})
	resp, data := h.request(http.MethodPut, "/api/v1/users/"+itoa(secondID), csrf, demote, "application/json")
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("demote the last enabled administrator = %d, want 409: %s", resp.StatusCode, data)
	}
	confirm, _ := json.Marshal(map[string]string{"confirmUserName": "Second"})
	resp, _ = h.request(http.MethodDelete, "/api/v1/users/"+itoa(secondID), csrf, confirm, "application/json")
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("delete the last enabled administrator = %d, want 409", resp.StatusCode)
	}
}

func TestForcedPasswordChangeConfinesTheAccount(t *testing.T) {
	h := newHarness(t, false)
	h.completeSetup()
	const temporary = "a temporary password"
	keeperID := h.createAccount(t, "Keeper", "Member", temporary)

	resp, data := h.loginWith(t, "Keeper", temporary)
	if resp.StatusCode != http.StatusOK || !decodeSession(t, data).User.MustChangePassword {
		t.Fatalf("flagged login = %d: %s", resp.StatusCode, data)
	}
	if n := h.sessionCount(t, keeperID); n != 1 {
		t.Fatalf("sessions = %d, want 1", n)
	}

	// API callers are told exactly why they are blocked.
	resp, data = h.request(http.MethodGet, "/api/v1/admin/items", "", nil, "application/json")
	if resp.StatusCode != http.StatusForbidden || !strings.Contains(string(data), "password_change_required") {
		t.Fatalf("flagged API call = %d: %s", resp.StatusCode, data)
	}
	// HTML callers are sent to the change page.
	resp, _ = h.request(http.MethodGet, "/admin/inventory", "", nil, "text/html")
	if resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != "/account/password" {
		t.Fatalf("flagged page = %d %q, want 303 /account/password", resp.StatusCode, resp.Header.Get("Location"))
	}
	resp, page := h.request(http.MethodGet, "/account/password", "", nil, "text/html")
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(page), "temporary password") {
		t.Fatalf("change page = %d: %s", resp.StatusCode, page)
	}

	wrong, _ := json.Marshal(map[string]string{"currentPassword": "not the temporary password",
		"newPassword": "a permanent password", "confirmPassword": "a permanent password"})
	resp, data = h.request(http.MethodPost, "/api/v1/auth/password", h.csrf(), wrong, "application/json")
	if resp.StatusCode != http.StatusUnprocessableEntity || !strings.Contains(string(data), "currentPassword") {
		t.Fatalf("wrong current password = %d: %s", resp.StatusCode, data)
	}

	change, _ := json.Marshal(map[string]string{"currentPassword": temporary,
		"newPassword": "a permanent password", "confirmPassword": "a permanent password"})
	resp, data = h.request(http.MethodPost, "/api/v1/auth/password", h.csrf(), change, "application/json")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("password change = %d: %s", resp.StatusCode, data)
	}
	if decodeSession(t, data).User.MustChangePassword {
		t.Fatal("the change did not clear MustChangePassword")
	}
	_, sessionBody := h.request(http.MethodGet, "/api/v1/auth/session", "", nil, "application/json")
	if decodeSession(t, sessionBody).User.MustChangePassword {
		t.Fatal("the session still reports a required password change")
	}
	// The account is now confined only by its role, not by the flag.
	if resp, _ := h.request(http.MethodGet, "/admin/inventory", "", nil, "text/html"); resp.StatusCode != http.StatusForbidden {
		t.Fatalf("member access to the officer area = %d, want 403", resp.StatusCode)
	}
	// The current session survived the change.
	if n := h.sessionCount(t, keeperID); n != 1 {
		t.Fatalf("sessions after the change = %d, want the current one", n)
	}
}

func TestAdministratorResetRevokesSessionsAndForcesChange(t *testing.T) {
	h := newHarness(t, false)
	h.completeSetup()
	const temporary = "a temporary password"
	keeperID := h.createAccount(t, "Keeper", "Member", temporary)

	if resp, _ := h.loginWith(t, "Keeper", temporary); resp.StatusCode != http.StatusOK {
		t.Fatalf("keeper login = %d", resp.StatusCode)
	}
	if n := h.sessionCount(t, keeperID); n != 1 {
		t.Fatalf("sessions = %d, want 1", n)
	}
	if resp, _ := h.loginWith(t, "admin", seedPassword); resp.StatusCode != http.StatusOK {
		t.Fatalf("administrator login = %d", resp.StatusCode)
	}

	const reset = "a reset temporary password"
	body, _ := json.Marshal(map[string]string{"password": reset, "confirmPassword": reset})
	resp, data := h.request(http.MethodPost, "/api/v1/users/"+itoa(keeperID)+"/password", h.csrf(), body, "application/json")
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(data), `"mustChangePassword":true`) {
		t.Fatalf("reset = %d: %s", resp.StatusCode, data)
	}
	if n := h.sessionCount(t, keeperID); n != 0 {
		t.Fatalf("sessions after the reset = %d, want 0", n)
	}
	if resp, _ := h.loginWith(t, "Keeper", temporary); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("old password login = %d, want 401", resp.StatusCode)
	}
	resp, data = h.loginWith(t, "Keeper", reset)
	if resp.StatusCode != http.StatusOK || !decodeSession(t, data).User.MustChangePassword {
		t.Fatalf("temporary password login = %d: %s", resp.StatusCode, data)
	}
}

func TestAccountPagesRenderAndAcceptForms(t *testing.T) {
	h := newHarness(t, false)
	h.completeSetup()

	resp, body := h.request(http.MethodGet, "/admin/users", "", nil, "text/html")
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(body), "Create account") {
		t.Fatalf("account list = %d: %s", resp.StatusCode, body)
	}
	resp, body = h.request(http.MethodGet, "/admin/users/new", "", nil, "text/html")
	for _, want := range []string{`name="userName"`, `name="accessLevel"`, `name="password"`,
		`name="confirmPassword"`, `action="/admin/users"`} {
		if !strings.Contains(string(body), want) {
			t.Fatalf("create page missing %q: %s", want, body)
		}
	}

	csrf := h.csrf()
	form := url.Values{
		"csrf_token": {csrf}, "userName": {"Keeper"}, "accessLevel": {"Officer"},
		"password": {"a temporary password"}, "confirmPassword": {"a temporary password"},
	}
	resp, body = h.requestForm(http.MethodPost, "/admin/users", csrf, form, "text/html")
	if resp.StatusCode != http.StatusSeeOther || !strings.HasPrefix(resp.Header.Get("Location"), "/admin/users/") {
		t.Fatalf("create form = %d %q: %s", resp.StatusCode, resp.Header.Get("Location"), body)
	}
	keeperID := h.userID(t, "Keeper")

	resp, body = h.request(http.MethodGet, "/admin/users/"+itoa(keeperID), "", nil, "text/html")
	detailOK := strings.Contains(string(body), "Reset password") &&
		strings.Contains(string(body), "Delete account") &&
		strings.Contains(string(body), `name="confirmUserName"`)
	if resp.StatusCode != http.StatusOK || !detailOK {
		t.Fatalf("detail page = %d: %s", resp.StatusCode, body)
	}

	// A duplicate name re-renders the create form with a field message.
	duplicate := url.Values{
		"csrf_token": {csrf}, "userName": {"KEEPER"}, "accessLevel": {"Member"},
		"password": {"a temporary password"}, "confirmPassword": {"a temporary password"},
	}
	resp, body = h.requestForm(http.MethodPost, "/admin/users", csrf, duplicate, "text/html")
	if resp.StatusCode != http.StatusConflict || !strings.Contains(string(body), "already in use") {
		t.Fatalf("duplicate create form = %d: %s", resp.StatusCode, body)
	}

	update := url.Values{"csrf_token": {csrf}, "accessLevel": {"Member"}, "status": {"active"}}
	resp, _ = h.requestForm(http.MethodPost, "/admin/users/"+itoa(keeperID), csrf, update, "text/html")
	if resp.StatusCode != http.StatusSeeOther ||
		resp.Header.Get("Location") != "/admin/users/"+itoa(keeperID)+"?notice=updated" {
		t.Fatalf("update form = %d %q", resp.StatusCode, resp.Header.Get("Location"))
	}

	reset := url.Values{"csrf_token": {csrf}, "password": {"another temporary password"},
		"confirmPassword": {"another temporary password"}}
	resp, _ = h.requestForm(http.MethodPost, "/admin/users/"+itoa(keeperID)+"/password", csrf, reset, "text/html")
	if resp.StatusCode != http.StatusSeeOther ||
		resp.Header.Get("Location") != "/admin/users/"+itoa(keeperID)+"?notice=password-reset" {
		t.Fatalf("reset form = %d %q", resp.StatusCode, resp.Header.Get("Location"))
	}

	remove := url.Values{"csrf_token": {csrf}, "confirmUserName": {"Keeper"}}
	resp, _ = h.requestForm(http.MethodPost, "/admin/users/"+itoa(keeperID)+"/delete", csrf, remove, "text/html")
	if resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != "/admin/users?notice=deleted" {
		t.Fatalf("delete form = %d %q", resp.StatusCode, resp.Header.Get("Location"))
	}
	_, body = h.request(http.MethodGet, "/admin/users", "", nil, "text/html")
	if strings.Contains(string(body), ">Keeper<") {
		t.Fatalf("the deleted account is still listed: %s", body)
	}
}
