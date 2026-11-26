package domain

import (
	"errors"
	"sort"
	"strings"
	"time"
)

// AccessAdmin is the administrative access level (Design.md L185-L187). The
// approved role hierarchy is Member < Officer < Admin; AccessLevel stores the
// role name and remains the single source of truth.
const AccessAdmin = "Admin"

// Role is the approved authorization role. A flat hierarchy is used: an Admin
// may do everything an Officer may do, and an Officer everything a Member may
// do, so no per-permission table is needed.
type Role string

const (
	RoleMember  Role = "Member"
	RoleOfficer Role = "Officer"
	RoleAdmin   Role = "Admin"
)

// Roles is the canonical ordered allowlist (least to most privileged).
var Roles = []Role{RoleMember, RoleOfficer, RoleAdmin}

// ParseRole maps a stored AccessLevel onto the hierarchy. Anything that is not
// an approved value, including a blank legacy row, is treated as the least
// privileged role so a corrupt value can never grant access.
func ParseRole(value string) Role {
	switch Role(value) {
	case RoleAdmin:
		return RoleAdmin
	case RoleOfficer:
		return RoleOfficer
	default:
		return RoleMember
	}
}

// Valid reports whether the role is one of the approved values.
func (r Role) Valid() bool {
	return r == RoleMember || r == RoleOfficer || r == RoleAdmin
}

// Rank orders the hierarchy: Member 1, Officer 2, Admin 3. An unknown role has
// the Member rank, matching ParseRole's fail-closed rule.
func (r Role) Rank() int {
	switch r {
	case RoleAdmin:
		return 3
	case RoleOfficer:
		return 2
	default:
		return 1
	}
}

// AtLeast reports whether the role is at or above min.
func (r Role) AtLeast(min Role) bool { return r.Rank() >= min.Rank() }

// String returns the stored value.
func (r Role) String() string { return string(r) }

// Sentinel errors shared by the service and API layers. Repositories translate
// driver errors into these values so the API can map status codes without
// knowing which store produced them.
var (
	ErrNotFound      = errors.New("not found")
	ErrConflict      = errors.New("conflict")
	ErrUnauthorized  = errors.New("unauthorized")
	ErrForbidden     = errors.New("forbidden")
	ErrSetupClosed   = errors.New("setup is already complete")
	ErrUnavailable   = errors.New("dependency unavailable")
	ErrValidation    = errors.New("validation failed")
	ErrRateLimited   = errors.New("too many authentication attempts")
	ErrAlreadyExists = errors.New("already exists")
)

// ValidationError carries per-field validation messages for a 422 response.
type ValidationError struct {
	Fields map[string]string
}

func (e *ValidationError) Error() string {
	if e == nil || len(e.Fields) == 0 {
		return ErrValidation.Error()
	}
	keys := make([]string, 0, len(e.Fields))
	for k := range e.Fields {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, k+": "+e.Fields[k])
	}
	return "validation failed: " + strings.Join(parts, "; ")
}

// Unwrap allows errors.Is(err, ErrValidation).
func (e *ValidationError) Unwrap() error { return ErrValidation }

// NewValidationError builds a validation error with one field message.
func NewValidationError(field, message string) *ValidationError {
	return &ValidationError{Fields: map[string]string{field: message}}
}

// RateLimitError reports the remaining lockout for a login attempt.
type RateLimitError struct {
	RetryAfter time.Duration
}

func (e *RateLimitError) Error() string { return ErrRateLimited.Error() }

// Unwrap allows errors.Is(err, ErrRateLimited).
func (e *RateLimitError) Unwrap() error { return ErrRateLimited }

// User is an application account. PasswordHash is never serialized.
type User struct {
	ID                 int64
	UserName           string
	PasswordHash       string
	AccessLevel        string
	LastLogin          *time.Time
	Disabled           bool
	ModAt              time.Time
	CreatedAt          time.Time
	ModBy              string
	MustChangePassword bool
}

// Role reports the account's authorization role. An unrecognized AccessLevel
// fails closed to Member.
func (u User) Role() Role { return ParseRole(u.AccessLevel) }

// IsAdmin reports whether the account has administrative access.
func (u User) IsAdmin() bool { return u.Role() == RoleAdmin }

// IsOfficer reports whether the account may administer inventory. Admin
// includes Officer, so this is a hierarchy check rather than an equality check.
func (u User) IsOfficer() bool { return u.Role().AtLeast(RoleOfficer) }

// CanAdministerInventory reports whether the account may add, edit, issue,
// return, remove, restore and import inventory.
func (u User) CanAdministerInventory() bool { return u.IsOfficer() }

// CanViewAudit reports whether the account may read the audit viewer and
// movement history. User-management events are Administrator-only and are
// excluded from the Officer view by the audit scope filter.
func (u User) CanViewAudit() bool { return u.IsOfficer() }

// CanManageUsers reports whether the account may administer accounts.
func (u User) CanManageUsers() bool { return u.IsAdmin() }

// CanManageSettings reports whether the account may change application
// configuration.
func (u User) CanManageSettings() bool { return u.IsAdmin() }

// IsEnabled reports whether the account may authenticate.
func (u User) IsEnabled() bool { return !u.Disabled }

// Session is a server-side authentication session. ID is an opaque identifier;
// the raw bearer token is never stored (D-09).
type Session struct {
	ID                string
	TokenHash         string
	UserID            int64
	CreatedAt         time.Time
	LastAccessed      time.Time
	ExpiresAt         time.Time
	AbsoluteExpiresAt time.Time
	RevokedAt         *time.Time
	ClientIP          string
	UserAgent         string
	User              User
}

// SettingType constrains Setting.Type (DatabaseDesign.md §3.2).
type SettingType string

const (
	SettingString  SettingType = "string"
	SettingInteger SettingType = "integer"
	SettingBoolean SettingType = "boolean"
	SettingSecret  SettingType = "secret"
	SettingURL     SettingType = "url"
)

// Setting is one row of the application configuration table.
type Setting struct {
	Key      string
	Value    string
	Type     SettingType
	IsSecret bool
	ModAt    time.Time
	ModBy    string
}

// SettingInput is a validated setting value waiting to be persisted.
type SettingInput struct {
	Key   string
	Value string
	Type  SettingType
	// Secret marks a setting whose value must never be returned by the API.
	Secret bool
}

// SetupData is the atomic first-run write: the administrator plus every
// setting produced by the wizard. D-02 makes the administrator row the setup
// switch, so both are committed in one transaction.
type SetupData struct {
	Admin    User
	Settings []SettingInput
}

// SessionPolicy is the E-08 lifecycle used when new sessions are created and
// when sliding expiry is refreshed.
type SessionPolicy struct {
	Idle     time.Duration
	Absolute time.Duration
}

// DefaultSessionPolicy applies the proposed E-08 defaults until Settings are
// changed through the administration surface.
var DefaultSessionPolicy = SessionPolicy{
	Idle:     30 * time.Minute,
	Absolute: 8 * time.Hour,
}

// Get retrieves a value by key from a settings snapshot.
func GetSetting(settings []Setting, key string) (string, bool) {
	for _, s := range settings {
		if s.Key == key {
			return s.Value, true
		}
	}
	return "", false
}

// String returns a short, log-safe description of a setting type.
func (t SettingType) String() string { return string(t) }
