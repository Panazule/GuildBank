package domain

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// UserNameMaxRunes bounds a username. The approved limit is 64 characters, which
// keeps every name within the actor-name snapshots stored by audit rows and
// inventory movements.
const UserNameMaxRunes = 64

// NormalizeUserName trims the surrounding whitespace an HTML form or API client
// may send. Stored usernames are always normalized.
func NormalizeUserName(name string) string { return strings.TrimSpace(name) }

// ValidateUserName enforces the approved account-name rules: non-blank, valid
// UTF-8, at most UserNameMaxRunes runes and no control characters. Uniqueness is
// enforced by the store because it is case-insensitive and index-backed.
func ValidateUserName(name string) error {
	if msg := userNameProblem(name); msg != "" {
		return NewValidationError("userName", msg)
	}
	return nil
}

func userNameProblem(name string) string {
	if !utf8.ValidString(name) {
		return "must be valid UTF-8"
	}
	normalized := NormalizeUserName(name)
	if normalized == "" {
		return "must not be blank"
	}
	if utf8.RuneCountInString(normalized) > UserNameMaxRunes {
		return "must be at most 64 characters"
	}
	for _, r := range normalized {
		if unicode.IsControl(r) {
			return "must not contain control characters"
		}
	}
	return ""
}

// ParseRoleInput maps a role supplied by an administrator onto the hierarchy.
// Unlike ParseRole, which fails closed for values already stored, a role
// supplied as input is rejected when unrecognized: silently downgrading an
// explicit request would hide an administrator's mistake. A blank value means
// Member, the least privileged role.
func ParseRoleInput(value string) (Role, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return RoleMember, nil
	}
	role := Role(trimmed)
	if !role.Valid() {
		return "", NewValidationError("accessLevel", "must be Member, Officer or Admin")
	}
	return role, nil
}

// validationFields collects per-field messages into one error so a form can
// report every problem at once. It returns nil when nothing is invalid.
func validationFields(fields map[string]string) error {
	if len(fields) == 0 {
		return nil
	}
	return &ValidationError{Fields: fields}
}

// CreateUserInput is an administrator's request to add an account. The password
// policy itself (length, not equal to the username) is applied by the service
// when the password is hashed, so exactly one implementation of the policy
// exists.
type CreateUserInput struct {
	UserName        string
	AccessLevel     string
	Password        string
	ConfirmPassword string
}

// Validate checks the rules that do not need the store.
func (in CreateUserInput) Validate() error {
	fields := map[string]string{}
	if msg := userNameProblem(in.UserName); msg != "" {
		fields["userName"] = msg
	}
	if _, err := ParseRoleInput(in.AccessLevel); err != nil {
		fields["accessLevel"] = "must be Member, Officer or Admin"
	}
	switch {
	case strings.TrimSpace(in.Password) == "":
		fields["password"] = "must not be blank"
	case in.Password != in.ConfirmPassword:
		fields["confirmPassword"] = "must match the password"
	}
	return validationFields(fields)
}

// RoleValue returns the requested role, defaulting to Member for a blank value.
// Validate must pass before the result is used.
func (in CreateUserInput) RoleValue() Role {
	role, err := ParseRoleInput(in.AccessLevel)
	if err != nil {
		return RoleMember
	}
	return role
}

// NormalizedUserName returns the name that should be stored.
func (in CreateUserInput) NormalizedUserName() string { return NormalizeUserName(in.UserName) }

// ResetPasswordInput is an administrator's temporary-password replacement. The
// account always receives MustChangePassword, so the value is never permanent.
type ResetPasswordInput struct {
	Password        string
	ConfirmPassword string
}

// Validate checks the rules that do not need the store or the account name.
func (in ResetPasswordInput) Validate() error {
	fields := map[string]string{}
	switch {
	case strings.TrimSpace(in.Password) == "":
		fields["password"] = "must not be blank"
	case in.Password != in.ConfirmPassword:
		fields["confirmPassword"] = "must match the password"
	}
	return validationFields(fields)
}

// ChangePasswordInput is the signed-in account owner changing their own
// password. CurrentPassword is required even during a forced change, so holding
// a session alone is not enough to take over an account.
type ChangePasswordInput struct {
	CurrentPassword string
	NewPassword     string
	ConfirmPassword string
}

// Validate checks the rules that do not need the stored hash.
func (in ChangePasswordInput) Validate() error {
	fields := map[string]string{}
	if strings.TrimSpace(in.CurrentPassword) == "" {
		fields["currentPassword"] = "must not be blank"
	}
	switch {
	case strings.TrimSpace(in.NewPassword) == "":
		fields["newPassword"] = "must not be blank"
	case in.NewPassword != in.ConfirmPassword:
		fields["confirmPassword"] = "must match the new password"
	case in.NewPassword == in.CurrentPassword:
		fields["newPassword"] = "must be different from the current password"
	}
	return validationFields(fields)
}

// SetPasswordInput is a signed-in account establishing its first password. There
// is deliberately no CurrentPassword: no password exists yet, so a session is
// the only proof of ownership D-SR-17 requires.
type SetPasswordInput struct {
	NewPassword     string
	ConfirmPassword string
}

// Validate checks the rules that do not need the account name or the store. The
// 12–72 byte policy and the "not the username" rule are applied by
// HashUserPassword once the account is known.
func (in SetPasswordInput) Validate() error {
	fields := map[string]string{}
	switch {
	case strings.TrimSpace(in.NewPassword) == "":
		fields["newPassword"] = "must not be blank"
	case in.NewPassword != in.ConfirmPassword:
		fields["confirmPassword"] = "must match the new password"
	}
	return validationFields(fields)
}

// DeleteUserInput carries the destructive-action confirmation. An administrator
// must retype the account name so a stray click cannot remove an account.
type DeleteUserInput struct {
	ConfirmUserName string
}

// Validate requires the typed name to match target, ignoring case and
// surrounding whitespace.
func (in DeleteUserInput) Validate(target string) error {
	if !strings.EqualFold(NormalizeUserName(in.ConfirmUserName), NormalizeUserName(target)) {
		return NewValidationError("confirmUserName", "must match the account name")
	}
	return nil
}
