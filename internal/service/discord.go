package service

import (
	"context"
	"errors"
	"fmt"
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
	// DiscordLoginCallbackPath is the second redirect URI registered in the
	// developer portal. Sign-in and linking never share a callback, so a link
	// state can never be replayed as a sign-in (and vice versa).
	DiscordLoginCallbackPath = "/auth/discord/callback"
	// DiscordRegisterCallbackPath is the third redirect URI. Registration has its
	// own callback for the same reason: no flow can replay another's state.
	DiscordRegisterCallbackPath = "/register/discord/callback"
	DiscordAuthorizeURL         = "https://discord.com/oauth2/authorize"
	// DiscordScope is the minimum that yields a stable user ID and a display
	// name. Email is never requested. It is the scope for linking and sign-in
	// only (D-SR-13): those flows are never widened.
	DiscordScope = "identify"
	// DiscordScopeRegister is the only scope that permits GET /users/@me/guilds.
	// Exactly one flow, /register/discord, requests it.
	DiscordScopeRegister = "identify guilds"

	// State scopes. The scope is inside the signed payload, so a value minted
	// for one flow is rejected by the other. DiscordStateRegister is the state
	// value for registration; DiscordScopeRegister is the OAuth scope string.
	DiscordScopeLink     = "link"
	DiscordScopeLogin    = "login"
	DiscordStateRegister = "register"
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

// LoginRedirectURI is the sign-in callback URL. Linking and sign-in are separate
// registrations so the two flows cannot be confused.
func (c DiscordConfig) LoginRedirectURI() string {
	if strings.TrimSpace(c.PublicBaseURL) == "" {
		return ""
	}
	return strings.TrimRight(c.PublicBaseURL, "/") + DiscordLoginCallbackPath
}

// RegisterRedirectURI is the registration callback URL. Registration has a
// third portal registration, so its consent can never be replayed as a link or
// a sign-in.
func (c DiscordConfig) RegisterRedirectURI() string {
	if strings.TrimSpace(c.PublicBaseURL) == "" {
		return ""
	}
	return strings.TrimRight(c.PublicBaseURL, "/") + DiscordRegisterCallbackPath
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
// already owns this Discord account" question, in both directions.
type DiscordIdentityStore interface {
	DiscordIdentity(ctx context.Context, userID int64) (domain.DiscordIdentity, error)
	DiscordUserIDOwner(ctx context.Context, discordUserID string) (int64, error)
	UserByDiscordID(ctx context.Context, discordUserID string) (domain.User, error)
	SetDiscordIdentity(ctx context.Context, userID int64, discordUserID, discordUserName string,
		at time.Time, modBy string) error
	ClearDiscordIdentity(ctx context.Context, userID int64, at time.Time, modBy string) error
}

// SessionIssuer issues an application session once an alternative
// authentication method has proven which account is signing in. AuthService
// implements it; Discord never creates accounts or sessions on its own.
type SessionIssuer interface {
	IssueSession(ctx context.Context, user domain.User, clientIP, userAgent string) (LoginResult, error)
}

// DiscordService owns the linking workflow. Linking is additive: it never
// creates an account, never changes a password and never grants a session.
type DiscordService struct {
	settings DiscordSettings
	client   DiscordAPI
	store    DiscordIdentityStore
	auth     SessionIssuer
	audit    AuditSink
	logger   *slog.Logger
	limiter  *LoginLimiter
	now      func() time.Time
}

// NewDiscordService builds the linking and sign-in service. A nil dependency
// leaves the corresponding flow unavailable rather than panicking, matching the
// other services.
func NewDiscordService(settings DiscordSettings, client DiscordAPI, store DiscordIdentityStore,
	auth SessionIssuer, audit AuditSink, logger *slog.Logger) *DiscordService {
	return &DiscordService{settings: settings, client: client, store: store, auth: auth,
		audit: audit, logger: logger, limiter: newDiscordLoginLimiter(), now: time.Now}
}

// newDiscordLoginLimiter bounds failed sign-in attempts per client address. The
// threshold is higher than a password attempt because the OAuth round trip
// legitimately fails on expired codes and cancelled consent.
func newDiscordLoginLimiter() *LoginLimiter {
	limiter := NewLoginLimiter()
	limiter.maxFailures = 10
	return limiter
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

// AuthorizeURL builds the consent URL for linking. The caller supplies the state
// value and is responsible for binding it to the session. The scope is passed
// explicitly so no flow can inherit another flow's permissions (D-SR-13).
func (s *DiscordService) AuthorizeURL(cfg DiscordConfig, state string) string {
	return authorizeURL(cfg.ClientID, cfg.RedirectURI(), state, DiscordScope)
}

// LoginAuthorizeURL builds the consent URL for sign-in. It differs from
// AuthorizeURL only in the redirect URI, which is a separate portal registration,
// and shares the same minimum scope.
func (s *DiscordService) LoginAuthorizeURL(cfg DiscordConfig, state string) string {
	return authorizeURL(cfg.ClientID, cfg.LoginRedirectURI(), state, DiscordScope)
}

func authorizeURL(clientID, redirectURI, state, scope string) string {
	query := url.Values{}
	query.Set("client_id", clientID)
	query.Set("redirect_uri", redirectURI)
	query.Set("response_type", "code")
	query.Set("scope", scope)
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
	// A passwordless account signs in only with Discord. Removing the identity
	// would lock the owner out of their own account, so the unlink is refused
	// until a password exists (D-SR-18).
	if !user.HasPassword {
		return fmt.Errorf("%w: set a password before unlinking Discord", domain.ErrConflict)
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

// Login turns a Discord authorization code into an application session for the
// account that already owns that Discord identity. It never creates or matches
// an account, and nothing is written on any refusal. An unknown or unlinked
// Discord user is refused with the neutral ErrUnauthorized; an account that
// exists but is not enabled is refused with AccountStateError, which unwraps to
// the same ErrUnauthorized but lets the sign-in page say whether the account is
// pending approval or disabled (D-SR-14).
//
// Everything the password path enforces still applies, because the session comes
// from AuthService.IssueSession: DisabledFg is re-checked, the idle/absolute
// policy is applied, and MustChangePassword is left to the caller's redirect.
func (s *DiscordService) Login(ctx context.Context, code, clientIP, userAgent, correlationID string) (LoginResult, error) {
	if !s.Ready() || s.auth == nil {
		return LoginResult{}, domain.ErrUnavailable
	}
	key := "discord|" + strings.ToLower(strings.TrimSpace(clientIP))
	if retry, blocked := s.limiter.Blocked(key, s.now().UTC()); blocked {
		return LoginResult{}, &domain.RateLimitError{RetryAfter: retry}
	}
	fail := func(err error) (LoginResult, error) {
		s.limiter.Failure(key, s.now().UTC())
		return LoginResult{}, err
	}

	cfg, err := s.Config(ctx)
	if err != nil {
		return LoginResult{}, err
	}
	if !cfg.Ready() {
		return LoginResult{}, domain.ErrUnavailable
	}
	if strings.TrimSpace(code) == "" {
		return fail(domain.NewValidationError("code", "the Discord authorization code is missing"))
	}

	token, err := s.client.ExchangeCode(ctx, cfg.ClientID, cfg.ClientSecret, cfg.LoginRedirectURI(), code)
	if err != nil {
		s.warn("discord login exchange failed", correlationID, err)
		return fail(domain.ErrUpstream)
	}
	profile, err := s.client.CurrentUser(ctx, token.AccessToken)
	if err != nil || strings.TrimSpace(profile.ID) == "" {
		s.warn("discord login profile lookup failed", correlationID, err)
		return fail(domain.ErrUpstream)
	}

	user, err := s.store.UserByDiscordID(ctx, profile.ID)
	if errors.Is(err, domain.ErrNotFound) {
		return fail(domain.ErrUnauthorized)
	}
	if err != nil {
		return LoginResult{}, err
	}
	if !user.IsEnabled() {
		// The user row is already in hand, so the refusal can name the reason
		// without a second query. The failure still counts against the limiter.
		return fail(&domain.AccountStateError{Status: user.Status(), Disabled: user.Disabled})
	}

	result, err := s.auth.IssueSession(ctx, user, clientIP, userAgent)
	if err != nil {
		return LoginResult{}, err
	}
	s.limiter.Reset(key)

	at := s.now().UTC()
	actor := domain.Actor{UserID: user.ID, UserName: user.UserName, Role: user.Role(), IsAdmin: user.IsAdmin()}
	writeAudit(ctx, s.audit, s.logger, at, "DISCORD_LOGIN", "Users",
		strconv.FormatInt(user.ID, 10), actor, map[string]string{},
		map[string]string{
			"userName":        user.UserName,
			"discordUserId":   profile.ID,
			"discordUserName": profile.DisplayName(),
			"method":          "discord",
		}, correlationID, clientIP)
	return result, nil
}
