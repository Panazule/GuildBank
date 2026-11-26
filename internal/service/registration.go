package service

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/Panazule/guildbank/internal/domain"
)

// registrationTicketTTL bounds how long a verified consent may wait for a
// username.
const registrationTicketTTL = 10 * time.Minute

// RegistrationStore is the persistence surface of self-registration. Account
// creation carries the Discord identity in the same statement, so a registration
// can never leave an account without its identity.
type RegistrationStore interface {
	CreateRegisteredUser(ctx context.Context, u domain.User, identity domain.DiscordIdentity) (domain.User, error)
	DiscordUserIDOwner(ctx context.Context, discordUserID string) (int64, error)
	GetUserByID(ctx context.Context, id int64) (domain.User, error)
}

// RegistrationClaimedError reports a Discord identity that already owns an
// account. It wraps ErrAlreadyExists so callers that only care about the
// duplicate keep their existing mapping, while the registration flow can tell
// the person whether the existing account is pending or usable.
type RegistrationClaimedError struct {
	Pending bool
}

func (e *RegistrationClaimedError) Error() string {
	if e.Pending {
		return "discord identity already owns a pending account"
	}
	return "discord identity already owns an account"
}

// Unwrap allows errors.Is(err, domain.ErrAlreadyExists).
func (e *RegistrationClaimedError) Unwrap() error { return domain.ErrAlreadyExists }

// RegistrationService owns the self-registration workflow: the availability
// gate, the code exchange, the guild check, the identity-ownership check, the
// account write and the audit row.
//
// It deliberately holds no SessionIssuer. Registration creates accounts, never
// credentials or sessions; the API layer issues a session only when the created
// account reports IsEnabled().
type RegistrationService struct {
	settings DiscordSettings
	client   DiscordAPI
	store    RegistrationStore
	audit    AuditSink
	logger   *slog.Logger
	limiter  *LoginLimiter
	now      func() time.Time
	random   io.Reader
}

// NewRegistrationService builds the registration service. A nil dependency
// leaves registration unavailable rather than panicking, matching the other
// services.
func NewRegistrationService(settings DiscordSettings, client DiscordAPI, store RegistrationStore,
	audit AuditSink, logger *slog.Logger) *RegistrationService {
	limiter := NewLoginLimiter()
	// A registration legitimately retries a taken username, so the threshold is
	// higher than a password attempt while the lockout stays the D-12 fifteen
	// minutes.
	limiter.maxFailures = 10
	return &RegistrationService{settings: settings, client: client, store: store, audit: audit,
		logger: logger, limiter: limiter, now: time.Now, random: rand.Reader}
}

// Ready reports whether every dependency is present.
func (s *RegistrationService) Ready() bool {
	return s != nil && s.settings != nil && s.client != nil && s.store != nil
}

// Config returns the current Discord configuration.
func (s *RegistrationService) Config(ctx context.Context) (DiscordConfig, error) {
	if s == nil || s.settings == nil {
		return DiscordConfig{}, nil
	}
	values, err := s.settings.Values(ctx)
	if err != nil {
		return DiscordConfig{}, err
	}
	return ParseDiscordConfig(values), nil
}

// Available reports whether a visitor may start a registration: the settings
// switch must be on and the Discord configuration must be complete. Both are
// required, and a settings read failure is reported as unavailable.
func (s *RegistrationService) Available(ctx context.Context) (bool, error) {
	if !s.Ready() {
		return false, nil
	}
	values, err := s.settings.Values(ctx)
	if err != nil {
		return false, err
	}
	enabled, err := strconv.ParseBool(strings.TrimSpace(values["SelfRegistrationEnabled"]))
	if err != nil || !enabled {
		return false, nil
	}
	return ParseDiscordConfig(values).Ready(), nil
}

// GuildConfigured reports whether a guild ID is set. It lets the landing page
// state, before consent, whether a successful registration will be approved
// automatically or will wait for an officer.
func (s *RegistrationService) GuildConfigured(cfg DiscordConfig) bool {
	return strings.TrimSpace(cfg.GuildID) != ""
}

// AuthorizeURL builds the consent URL for registration. It is the only caller
// that passes DiscordScopeRegister, which is what makes D-SR-13 structural
// rather than a convention.
func (s *RegistrationService) AuthorizeURL(cfg DiscordConfig, state string) string {
	return authorizeURL(cfg.ClientID, cfg.RegisterRedirectURI(), state, DiscordScopeRegister)
}

func (s *RegistrationService) warn(message, correlationID string, err error) {
	if s.logger == nil {
		return
	}
	s.logger.Warn(message, "request_id", correlationID, "error", err)
}

// Verify exchanges the authorization code, reads the Discord profile, refuses a
// Discord identity that already owns an account and runs the guild check. The
// result is a ticket; nothing is written.
//
// Fail closed: only a definitive "member" result approves, and an empty guild ID
// is "not configured", never "member".
func (s *RegistrationService) Verify(ctx context.Context, code, correlationID string) (domain.RegistrationTicket, error) {
	if !s.Ready() {
		return domain.RegistrationTicket{}, domain.ErrUnavailable
	}
	available, err := s.Available(ctx)
	if err != nil {
		return domain.RegistrationTicket{}, err
	}
	if !available {
		return domain.RegistrationTicket{}, domain.ErrUnavailable
	}
	if strings.TrimSpace(code) == "" {
		return domain.RegistrationTicket{}, domain.NewValidationError("code", "the Discord authorization code is missing")
	}
	cfg, err := s.Config(ctx)
	if err != nil {
		return domain.RegistrationTicket{}, err
	}
	if !cfg.Ready() {
		return domain.RegistrationTicket{}, domain.ErrUnavailable
	}

	token, err := s.client.ExchangeCode(ctx, cfg.ClientID, cfg.ClientSecret, cfg.RegisterRedirectURI(), code)
	if err != nil {
		s.warn("discord registration exchange failed", correlationID, err)
		return domain.RegistrationTicket{}, domain.ErrUpstream
	}
	profile, err := s.client.CurrentUser(ctx, token.AccessToken)
	if err != nil || strings.TrimSpace(profile.ID) == "" {
		s.warn("discord registration profile lookup failed", correlationID, err)
		return domain.RegistrationTicket{}, domain.ErrUpstream
	}

	ownerUserID, err := s.store.DiscordUserIDOwner(ctx, profile.ID)
	switch {
	case err == nil:
		// Registration never signs into or merges with an existing account. The
		// flow stops here, before anything is written.
		pending := false
		if owner, getErr := s.store.GetUserByID(ctx, ownerUserID); getErr == nil {
			pending = owner.IsPending()
		}
		return domain.RegistrationTicket{}, &RegistrationClaimedError{Pending: pending}
	case !errors.Is(err, domain.ErrNotFound):
		return domain.RegistrationTicket{}, err
	}

	guildCheck := domain.GuildCheckNotConfigured
	if strings.TrimSpace(cfg.GuildID) != "" {
		member, memberErr := s.client.GuildMember(ctx, token.AccessToken, cfg.GuildID)
		switch {
		case memberErr != nil:
			// An error is "unverified", never "not a member": a 403 for a token
			// minted without the guilds scope must not be reported as absence.
			s.warn("discord registration guild check failed", correlationID, memberErr)
			guildCheck = domain.GuildCheckError
		case member:
			guildCheck = domain.GuildCheckMember
		default:
			guildCheck = domain.GuildCheckNotMember
		}
	}

	at := s.now().UTC()
	return domain.RegistrationTicket{
		DiscordUserID:   profile.ID,
		DiscordUserName: profile.DisplayName(),
		GuildCheck:      guildCheck,
		GuildID:         cfg.GuildID,
		IssuedAt:        at,
		ExpiresAt:       at.Add(registrationTicketTTL),
	}, nil
}

// Complete creates the account for a verified ticket. The role is Member, the
// status is Approved only for a definitive membership result, and the origin is
// SelfRegistration. accessLevel, accountStatus and origin are never read from a
// request.
func (s *RegistrationService) Complete(ctx context.Context, ticket domain.RegistrationTicket,
	userName, correlationID, sourceIP string) (domain.User, error) {
	if !s.Ready() {
		return domain.User{}, domain.ErrUnavailable
	}
	available, err := s.Available(ctx)
	if err != nil {
		return domain.User{}, err
	}
	if !available {
		return domain.User{}, domain.ErrUnavailable
	}
	if !ticket.Valid() {
		return domain.User{}, domain.ErrUnauthorized
	}

	key := "registration|" + strings.ToLower(strings.TrimSpace(sourceIP))
	if retry, blocked := s.limiter.Blocked(key, s.now().UTC()); blocked {
		return domain.User{}, &domain.RateLimitError{RetryAfter: retry}
	}
	fail := func(err error) (domain.User, error) {
		s.limiter.Failure(key, s.now().UTC())
		return domain.User{}, err
	}

	input := domain.RegistrationInput{UserName: userName}
	if err := input.Validate(); err != nil {
		return fail(err)
	}
	username := input.NormalizedUserName()

	// A second tab may have completed in the meantime. The pre-check produces a
	// clearer message; the unique indexes remain the authority.
	if ownerUserID, ownerErr := s.store.DiscordUserIDOwner(ctx, ticket.DiscordUserID); ownerErr == nil {
		pending := false
		if owner, getErr := s.store.GetUserByID(ctx, ownerUserID); getErr == nil {
			pending = owner.IsPending()
		}
		return fail(&RegistrationClaimedError{Pending: pending})
	} else if !errors.Is(ownerErr, domain.ErrNotFound) {
		return fail(ownerErr)
	}

	// A discarded random secret keeps PasswordHash NOT NULL without granting a
	// usable credential: VerifyPassword can only succeed for a value that no
	// longer exists anywhere.
	secret := make([]byte, 32)
	if _, err := io.ReadFull(s.random, secret); err != nil {
		return fail(fmt.Errorf("generate registration secret: %w", err))
	}
	hash, err := HashPassword(base64.RawURLEncoding.EncodeToString(secret))
	if err != nil {
		return fail(err)
	}

	at := s.now().UTC()
	status := domain.AccountStatusPendingApproval
	var verifiedAt *time.Time
	if ticket.Approved() {
		status = domain.AccountStatusApproved
		verifiedAt = &at
	}
	created, err := s.store.CreateRegisteredUser(ctx, domain.User{
		UserName: username, PasswordHash: hash, AccessLevel: string(domain.RoleMember),
		MustChangePassword: false, ModAt: at, CreatedAt: at,
		AccountStatus: string(status), Origin: domain.OriginSelfRegistration,
		GuildVerifiedAt: verifiedAt,
	}, domain.DiscordIdentity{
		DiscordUserID: ticket.DiscordUserID, DiscordUserName: ticket.DiscordUserName,
		LinkedAt: &at,
	})
	if err != nil {
		if errors.Is(err, domain.ErrAlreadyExists) {
			// Either the username or the identity lost the race. Re-check the
			// identity so the right message is shown; otherwise it is the name.
			if ownerUserID, ownerErr := s.store.DiscordUserIDOwner(ctx, ticket.DiscordUserID); ownerErr == nil {
				pending := false
				if owner, getErr := s.store.GetUserByID(ctx, ownerUserID); getErr == nil {
					pending = owner.IsPending()
				}
				return fail(&RegistrationClaimedError{Pending: pending})
			}
		}
		return fail(err)
	}
	s.limiter.Reset(key)

	actor := domain.Actor{UserID: created.ID, UserName: created.UserName,
		Role: created.Role(), IsAdmin: created.IsAdmin()}
	writeAudit(ctx, s.audit, s.logger, at, "USER_SELF_REGISTERED", "Users",
		strconv.FormatInt(created.ID, 10), actor, map[string]string{},
		map[string]string{
			"userName":        created.UserName,
			"accessLevel":     created.AccessLevel,
			"accountStatus":   string(status),
			"discordUserId":   ticket.DiscordUserID,
			"discordUserName": ticket.DiscordUserName,
			"guildId":         ticket.GuildID,
			"guildCheck":      ticket.GuildCheck.String(),
		}, correlationID, sourceIP)

	created.PasswordHash = ""
	return created, nil
}
