package domain

import (
	"strings"
	"time"
)

// DiscordIdentity is the Discord account linked to an application account. A
// zero value with an empty DiscordUserID means "not linked"; unlinking returns
// the account to that state rather than deleting it.
type DiscordIdentity struct {
	UserID          int64
	DiscordUserID   string
	DiscordUserName string
	LinkedAt        *time.Time
}

// Linked reports whether the account has a Discord identity.
func (d DiscordIdentity) Linked() bool { return strings.TrimSpace(d.DiscordUserID) != "" }

// DiscordToken is the OAuth2 token response used for the linking callback. It is
// held only for the duration of the callback and never persisted.
type DiscordToken struct {
	AccessToken string
	TokenType   string
	ExpiresIn   int
}

// DiscordProfile is the subset of GET /users/@me the application stores.
type DiscordProfile struct {
	ID         string
	UserName   string
	GlobalName string
}

// DisplayName prefers the global display name and falls back to the username,
// which is what the profile page shows next to a linked identity.
func (p DiscordProfile) DisplayName() string {
	if name := strings.TrimSpace(p.GlobalName); name != "" {
		return name
	}
	return strings.TrimSpace(p.UserName)
}
