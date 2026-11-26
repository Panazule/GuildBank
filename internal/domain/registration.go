package domain

import (
	"strings"
	"time"
)

// OriginSelfRegistration marks an account created by the public registration
// flow. Administrator-created accounts leave Origin empty.
const OriginSelfRegistration = "SelfRegistration"

// GuildCheck is the outcome of the Discord guild-membership check performed
// during registration. It is a closed set: only Member approves, everything
// else (including a provider error) fails closed to pending approval.
type GuildCheck string

const (
	GuildCheckMember        GuildCheck = "member"
	GuildCheckNotMember     GuildCheck = "not-member"
	GuildCheckNotConfigured GuildCheck = "not-configured"
	GuildCheckError         GuildCheck = "error"
)

// Valid reports whether the value is one of the approved outcomes. A signed
// ticket is only accepted when its embedded value parses.
func (g GuildCheck) Valid() bool {
	switch g {
	case GuildCheckMember, GuildCheckNotMember, GuildCheckNotConfigured, GuildCheckError:
		return true
	}
	return false
}

// Member reports a definitive membership result. It is the single predicate
// that may approve a self-registration.
func (g GuildCheck) Member() bool { return g == GuildCheckMember }

// String returns the stored value.
func (g GuildCheck) String() string { return string(g) }

// RegistrationTicket is the verified outcome of a Discord consent that is
// waiting for a username. It travels only inside a signed, HttpOnly cookie, so
// the browser cannot choose which Discord identity is being registered.
type RegistrationTicket struct {
	DiscordUserID   string
	DiscordUserName string
	GuildCheck      GuildCheck
	GuildID         string
	IssuedAt        time.Time
	ExpiresAt       time.Time
}

// RegistrationInput is the only field a visitor supplies on the completion
// form. accessLevel, accountStatus and origin are server-set constants and are
// never bound from the request.
type RegistrationInput struct {
	UserName string
}

// Validate applies the approved account-name rules. Uniqueness is enforced by
// the store's case-insensitive unique index.
func (in RegistrationInput) Validate() error { return ValidateUserName(in.UserName) }

// NormalizedUserName returns the name that should be stored.
func (in RegistrationInput) NormalizedUserName() string {
	return NormalizeUserName(in.UserName)
}

// Approved reports whether this ticket's guild check permits immediate
// approval. Anything other than a definitive membership result fails closed.
func (t RegistrationTicket) Approved() bool { return t.GuildCheck.Member() }

// Valid reports whether the ticket carries an identity and a usable outcome.
func (t RegistrationTicket) Valid() bool {
	return strings.TrimSpace(t.DiscordUserID) != "" && t.GuildCheck.Valid()
}
