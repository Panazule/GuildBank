package service

import (
	"context"
	"errors"
	"log/slog"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/Panazule/guildbank/internal/domain"
)

// Discord linking constants (P3 Phase B). Login is deliberately absent: the
// callback only attaches an identity to an existing application session, so a
// Discord identity can never authenticate on its own.
const (
	DiscordCallbackPath = "/account/discord/callback"
	DiscordAuthorizeURL = "https://discord.com/oauth2/authorize"
	// DiscordScope is the minimum that yields a stable user ID and a display
	// name. Email is never requested.
	DiscordScope = "identify"
)

// DiscordConfig is the validated Discord subset of Settings.
type DiscordConfig struct {
	Enabled       bool
	ClientID      string
	ClientSecret  string
	PublicBaseURL string
	GuildID       string
}

// RedirectURI is the callback URL registered in the Discord developer portal.
func (c DiscordConfig) RedirectURI() string {
	if strings.TrimSpace(c.PublicBaseURL) == "" {
		return ""
	}
	return strings.TrimRight(c.PublicBaseURL, "/") + DiscordCallbackPath
}

// Ready reports whether linking can be attempted. Every value is required: an
// incomplete configuration fails closed rather than sending a broken request.
func (c DiscordConfig) Ready() bool {
	return c.Enabled && c.ClientID != "" && c.ClientSecret != "" && c.RedirectURI() != ""
}

// ParseDiscordConfig reads the Discord keys out of a settings snapshot.
func ParseDiscordConfig(values map[string]string) DiscordConfig {
	enabled, err := strconv.ParseBool(strings.TrimSpace(values["DiscordEnabled"]))
	if err != nil {
		enabled = false
	}
	return DiscordConfig{
		Enabled:       enabled,
		ClientID:      strings.TrimSpace(values["DiscordClientId"]),
		ClientSecret:  values["DiscordClientSecret"],
		PublicBaseURL: strings.TrimSpace(values["PublicBaseUrl"]),
		GuildID:       strings.TrimSpace(values["DiscordGuildId"]),
	}
}

// DiscordSettings is the read-only settings surface the service needs.
type DiscordSettings interface {
	Values(ctx context.Context) (map[string]string, error)
}

// DiscordAPI is the provider surface. It is an interface so the link flow is
// tested without network access, and so the real client stays in the platform
// layer.
type DiscordAPI interface {
	ExchangeCode(ctx context.Context, clientID, clientSecret, redirectURI, code string) (domain.DiscordToken, error)
	CurrentUser(ctx context.Context, accessToken string) (domain.DiscordProfile, error)
	GuildMember(ctx context.Context, accessToken, guildID string) (bool, error)
}

// DiscordIdentityStore persists the identity columns and answers the "who
// already owns this Discord account" question.
type DiscordIdentityStore interface {
	DiscordIdentity(ctx context.Context, userID int64) (domain.DiscordIdentity, error)
	DiscordUserIDOwner(ctx context.Context, discordUserID string) (int64, error)
	SetDiscordIdentity(ctx context.Context, userID int64, discordUserID, discordUserName string,
		at time.Time, modBy string) error
	ClearDiscordIdentity(ctx context.Context, userID int64, at time.Time, modBy string) error
}

// DiscordService owns the linking workflow. Linking is additive: it never
// creates an account, never changes a password and never grants a session.
type DiscordService struct {
	settings DiscordSettings
	client   DiscordAPI
	store    DiscordIdentityStore
	audit    AuditSink
	logger   *slog.Logger
	now      func() time.Time
}

// NewDiscordService builds the linking service. A nil dependency leaves the
// service unavailable rather than panicking, matching the other services.
func NewDiscordService(settings DiscordSettings, client DiscordAPI, store DiscordIdentityStore,
	audit AuditSink, logger *slog.Logger) *DiscordService {
	return &DiscordService{settings: settings, client: client, store: store, audit: audit,
		logger: logger, now: time.Now}
}

// Ready reports whether every dependency is present.
func (s *DiscordService) Ready() bool {
	return s != nil && s.settings != nil && s.client != nil && s.store != nil
}

// Config returns the current Discord configuration. A missing settings store is
// reported as a disabled configuration, not an error, so the profile page can
// still render.
func (s *DiscordService) Config(ctx context.Context) (DiscordConfig, error) {
	if s == nil || s.settings == nil {
		return DiscordConfig{}, nil
	}
	values, err := s.settings.Values(ctx)
	if err != nil {
		return DiscordConfig{}, err
	}
	return ParseDiscordConfig(values), nil
}

// AuthorizeURL builds the consent URL. The caller supplies the state value and
// is responsible for binding it to the session.
func (s *DiscordService) AuthorizeURL(cfg DiscordConfig, state string) string {
	query := url.Values{}
	query.Set("client_id", cfg.ClientID)
	query.Set("redirect_uri", cfg.RedirectURI())
	query.Set("response_type", "code")
	query.Set("scope", DiscordScope)
	query.Set("state", state)
	return DiscordAuthorizeURL + "?" + query.Encode()
}

// Identity returns the account's linked Discord identity. An unlinked account
// yields a zero identity with no error.
func (s *DiscordService) Identity(ctx context.Context, userID int64) (domain.DiscordIdentity, error) {
	if !s.Ready() {
		return domain.DiscordIdentity{}, domain.ErrUnavailable
	}
	if userID <= 0 {
		return domain.DiscordIdentity{}, domain.ErrUnauthorized
	}
	return s.store.DiscordIdentity(ctx, userID)
}

func (s *DiscordService) warn(message, correlationID string, err error) {
	if s.logger == nil {
		return
	}
	s.logger.Warn(message, "request_id", correlationID, "error", err)
}

// Link exchanges the authorization code, reads the Discord profile and stores
// the identity. A Discord account already linked to a different application
// account is refused, so one identity can never be shared.
func (s *DiscordService) Link(ctx context.Context, user domain.User, code string,
	correlationID, sourceIP string) (domain.DiscordIdentity, error) {
	if !s.Ready() {
		return domain.DiscordIdentity{}, domain.ErrUnavailable
	}
	if strings.TrimSpace(code) == "" {
		return domain.DiscordIdentity{}, domain.NewValidationError("code", "the Discord authorization code is missing")
	}
	cfg, err := s.Config(ctx)
	if err != nil {
		return domain.DiscordIdentity{}, err
	}
	if !cfg.Ready() {
		return domain.DiscordIdentity{}, domain.ErrUnavailable
	}

	token, err := s.client.ExchangeCode(ctx, cfg.ClientID, cfg.ClientSecret, cfg.RedirectURI(), code)
	if err != nil {
		s.warn("discord code exchange failed", correlationID, err)
		return domain.DiscordIdentity{}, domain.ErrUpstream
	}
	profile, err := s.client.CurrentUser(ctx, token.AccessToken)
	if err != nil {
		s.warn("discord profile lookup failed", correlationID, err)
		return domain.DiscordIdentity{}, domain.ErrUpstream
	}
	if strings.TrimSpace(profile.ID) == "" {
		s.warn("discord profile had no id", correlationID, nil)
		return domain.DiscordIdentity{}, domain.ErrUpstream
	}

	owner, err := s.store.DiscordUserIDOwner(ctx, profile.ID)
	switch {
	case err == nil && owner != user.ID:
		return domain.DiscordIdentity{}, domain.ErrAlreadyExists
	case err != nil && !errors.Is(err, domain.ErrNotFound):
		return domain.DiscordIdentity{}, err
	}

	// Guild membership is optional and never blocks linking; the result is
	// recorded so an officer can see whether the link was verified.
	guildVerified := false
	if cfg.GuildID != "" {
		member, memberErr := s.client.GuildMember(ctx, token.AccessToken, cfg.GuildID)
		if memberErr != nil {
			s.warn("discord guild check failed", correlationID, memberErr)
		} else {
			guildVerified = member
		}
	}

	at := s.now().UTC()
	displayName := profile.DisplayName()
	if err := s.store.SetDiscordIdentity(ctx, user.ID, profile.ID, displayName, at, user.UserName); err != nil {
		return domain.DiscordIdentity{}, err
	}
	linkedAt := at
	identity := domain.DiscordIdentity{UserID: user.ID, DiscordUserID: profile.ID,
		DiscordUserName: displayName, LinkedAt: &linkedAt}

	actor := domain.Actor{UserID: user.ID, UserName: user.UserName, Role: user.Role(), IsAdmin: user.IsAdmin()}
	writeAudit(ctx, s.audit, s.logger, at, "DISCORD_LINKED", "Users",
		strconv.FormatInt(user.ID, 10), actor, map[string]string{},
		map[string]string{
			"discordUserId":   profile.ID,
			"discordUserName": displayName,
			"guildId":         cfg.GuildID,
			"guildVerified":   strconv.FormatBool(guildVerified),
		}, correlationID, sourceIP)
	return identity, nil
}

// Unlink removes the identity from the signed-in account. A session proves
// ownership, so no password re-entry is required (approved decision D8).
func (s *DiscordService) Unlink(ctx context.Context, user domain.User, correlationID, sourceIP string) error {
	if !s.Ready() {
		return domain.ErrUnavailable
	}
	identity, err := s.store.DiscordIdentity(ctx, user.ID)
	if err != nil {
		return err
	}
	if !identity.Linked() {
		return domain.ErrNotFound
	}
	at := s.now().UTC()
	if err := s.store.ClearDiscordIdentity(ctx, user.ID, at, user.UserName); err != nil {
		return err
	}
	actor := domain.Actor{UserID: user.ID, UserName: user.UserName, Role: user.Role(), IsAdmin: user.IsAdmin()}
	writeAudit(ctx, s.audit, s.logger, at, "DISCORD_UNLINKED", "Users",
		strconv.FormatInt(user.ID, 10), actor,
		map[string]string{"discordUserId": identity.DiscordUserID,
			"discordUserName": identity.DiscordUserName},
		map[string]string{}, correlationID, sourceIP)
	return nil
}
