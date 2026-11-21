package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/Panazule/guildbank/internal/domain"
)

// UserReader is the account lookup surface needed for authentication.
type UserReader interface {
	GetUserByName(ctx context.Context, name string) (domain.User, error)
	UpdateLastLogin(ctx context.Context, userID int64, at time.Time) error
}

// SessionStore is the persistence surface for D-09 sessions.
type SessionStore interface {
	CreateSession(ctx context.Context, session domain.Session) error
	FindSessionByTokenHash(ctx context.Context, tokenHash string) (domain.Session, error)
	TouchSession(ctx context.Context, id string, at, expires time.Time) error
	DeleteSession(ctx context.Context, tokenHash string) error
	DeleteSessionsForUser(ctx context.Context, userID int64) (int64, error)
	DeleteExpiredSessions(ctx context.Context, now time.Time) (int64, error)
}

// SessionPolicyReader supplies idle and absolute expiry.
type SessionPolicyReader interface {
	SessionPolicy(ctx context.Context) (domain.SessionPolicy, error)
}

// LoginInput is a credential attempt.
type LoginInput struct {
	UserName  string
	Password  string
	ClientIP  string
	UserAgent string
}

// LoginResult is returned only after successful authentication.
type LoginResult struct {
	User              domain.User
	Token             string
	ExpiresAt         time.Time
	AbsoluteExpiresAt time.Time
}

// AuthenticatedSession is the principal attached to a request.
type AuthenticatedSession struct {
	Session           domain.Session
	User              domain.User
	ExpiresAt         time.Time
	AbsoluteExpiresAt time.Time
}

// AuthService implements login, session validation, sliding expiry and logout.
type AuthService struct {
	users    UserReader
	sessions SessionStore
	policy   SessionPolicyReader
	limiter  *LoginLimiter
	now      func() time.Time
	random   io.Reader
}

// NewAuthService builds the authentication service with D-12 defaults.
func NewAuthService(users UserReader, sessions SessionStore, policy SessionPolicyReader) *AuthService {
	return &AuthService{
		users:    users,
		sessions: sessions,
		policy:   policy,
		limiter:  NewLoginLimiter(),
		now:      time.Now,
		random:   rand.Reader,
	}
}

// Login verifies credentials and issues a fresh session token. Unknown users,
// wrong passwords and disabled accounts share one error.
func (s *AuthService) Login(ctx context.Context, in LoginInput) (LoginResult, error) {
	username := strings.TrimSpace(in.UserName)
	key := loginKey(in.ClientIP, username)
	if retry, blocked := s.limiter.Blocked(key, s.now().UTC()); blocked {
		return LoginResult{}, &domain.RateLimitError{RetryAfter: retry}
	}

	user, err := s.users.GetUserByName(ctx, username)
	if err != nil && !errors.Is(err, domain.ErrNotFound) {
		return LoginResult{}, fmt.Errorf("lookup user: %w", err)
	}
	if errors.Is(err, domain.ErrNotFound) {
		verifyUnknown(in.Password)
	}
	failed := errors.Is(err, domain.ErrNotFound)
	if !failed {
		failed = !VerifyPassword(user.PasswordHash, in.Password) || !user.IsEnabled()
	}
	if failed {
		if retry, locked := s.limiter.Failure(key, s.now().UTC()); locked {
			return LoginResult{}, &domain.RateLimitError{RetryAfter: retry}
		}
		return LoginResult{}, domain.ErrUnauthorized
	}

	now := s.now().UTC()
	if err := s.users.UpdateLastLogin(ctx, user.ID, now); err != nil {
		return LoginResult{}, fmt.Errorf("record login: %w", err)
	}
	user.LastLogin = &now
	s.limiter.Reset(key)

	return s.issueSession(ctx, user, in.ClientIP, in.UserAgent)
}

// IssueSession creates a session for an already-authenticated user. Setup uses
// it to finish with a working administrator session.
func (s *AuthService) IssueSession(ctx context.Context, user domain.User, clientIP, userAgent string) (LoginResult, error) {
	return s.issueSession(ctx, user, clientIP, userAgent)
}

func (s *AuthService) issueSession(ctx context.Context, user domain.User, clientIP, userAgent string) (LoginResult, error) {
	if !user.IsEnabled() || !user.IsAdmin() {
		return LoginResult{}, domain.ErrUnauthorized
	}
	policy, err := s.policy.SessionPolicy(ctx)
	if err != nil {
		policy = domain.DefaultSessionPolicy
	}
	now := s.now().UTC()
	id, err := randomHex(s.random, 16)
	if err != nil {
		return LoginResult{}, fmt.Errorf("generate session id: %w", err)
	}
	tokenBytes := make([]byte, 32)
	if _, err := io.ReadFull(s.random, tokenBytes); err != nil {
		return LoginResult{}, fmt.Errorf("generate session token: %w", err)
	}
	token := base64.RawURLEncoding.EncodeToString(tokenBytes)
	session := domain.Session{
		ID:                id,
		TokenHash:         hashToken(token),
		UserID:            user.ID,
		CreatedAt:         now,
		LastAccessed:      now,
		ExpiresAt:         now.Add(policy.Idle),
		AbsoluteExpiresAt: now.Add(policy.Absolute),
		ClientIP:          clientIP,
		UserAgent:         truncate(userAgent, 512),
		User:              user,
	}
	if err := s.sessions.CreateSession(ctx, session); err != nil {
		return LoginResult{}, err
	}
	return LoginResult{
		User:              user,
		Token:             token,
		ExpiresAt:         session.ExpiresAt,
		AbsoluteExpiresAt: session.AbsoluteExpiresAt,
	}, nil
}

// Authenticate resolves the raw cookie token, enforces idle and absolute
// expiry, re-checks DisabledFg, and refreshes LastAccessed at most once per
// minute. It never reloads expiry from a client-supplied value.
func (s *AuthService) Authenticate(ctx context.Context, token string) (AuthenticatedSession, error) {
	if token == "" || len(token) > 512 {
		return AuthenticatedSession{}, domain.ErrUnauthorized
	}
	tokenHash := hashToken(token)
	session, err := s.sessions.FindSessionByTokenHash(ctx, tokenHash)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return AuthenticatedSession{}, domain.ErrUnauthorized
		}
		return AuthenticatedSession{}, err
	}
	now := s.now().UTC()
	if !now.Before(session.ExpiresAt) || !now.Before(session.AbsoluteExpiresAt) {
		_ = s.sessions.DeleteSession(ctx, tokenHash)
		return AuthenticatedSession{}, domain.ErrUnauthorized
	}
	if !session.User.IsEnabled() {
		_ = s.sessions.DeleteSession(ctx, tokenHash)
		return AuthenticatedSession{}, domain.ErrUnauthorized
	}
	if now.Sub(session.LastAccessed) >= time.Minute {
		policy, perr := s.policy.SessionPolicy(ctx)
		if perr != nil {
			policy = domain.DefaultSessionPolicy
		}
		expires := now.Add(policy.Idle)
		if expires.After(session.AbsoluteExpiresAt) {
			expires = session.AbsoluteExpiresAt
		}
		if err := s.sessions.TouchSession(ctx, session.ID, now, expires); err == nil {
			session.LastAccessed = now
			session.ExpiresAt = expires
		}
	}
	return AuthenticatedSession{
		Session:           session,
		User:              session.User,
		ExpiresAt:         session.ExpiresAt,
		AbsoluteExpiresAt: session.AbsoluteExpiresAt,
	}, nil
}

// Logout revokes the presented session.
func (s *AuthService) Logout(ctx context.Context, token string) error {
	if token == "" {
		return nil
	}
	return s.sessions.DeleteSession(ctx, hashToken(token))
}

// RevokeAll deletes every session for an account (used by later user
// management; exposed now so disabling can invalidate immediately).
func (s *AuthService) RevokeAll(ctx context.Context, userID int64) (int64, error) {
	return s.sessions.DeleteSessionsForUser(ctx, userID)
}

// PurgeExpired removes timed-out rows; the application schedules it hourly.
func (s *AuthService) PurgeExpired(ctx context.Context) (int64, error) {
	return s.sessions.DeleteExpiredSessions(ctx, s.now().UTC())
}

func loginKey(ip, username string) string {
	return strings.ToLower(strings.TrimSpace(ip)) + "|" + strings.ToLower(strings.TrimSpace(username))
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func randomHex(r io.Reader, n int) (string, error) {
	b := make([]byte, n)
	if _, err := io.ReadFull(r, b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
