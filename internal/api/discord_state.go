package api

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

const (
	// One cookie per flow: a link state and a sign-in state can be in flight at
	// the same time without overwriting each other.
	discordLinkStateCookie  = "guildbank_discord_link_state"
	discordLoginStateCookie = "guildbank_discord_login_state"
	// discordStateMaxAge bounds how long a started flow may take to come back.
	discordStateMaxAge = 10 * time.Minute
)

// discordState is the verified content of a state value.
type discordState struct {
	Scope  string
	UserID int64
	// Next is the post-sign-in destination. It is carried inside the signed
	// payload so the callback cannot be pointed elsewhere, and it is still
	// re-validated with safeNext before use.
	Next string
}

// discordStateManager signs the OAuth state value and binds it to the flow, the
// account and (for linking) the session that started it. The value travels both
// in the cookie and in the authorization URL, so a callback must present the
// exact value this browser received (double submit) and the signature catches
// tampering. The HMAC secret is process-local, so a restart abandons in-flight
// flows (safe).
type discordStateManager struct {
	secret []byte
	secure bool
}

func newDiscordStateManager(secure bool) (*discordStateManager, error) {
	secret := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, secret); err != nil {
		return nil, fmt.Errorf("api: discord state secret: %w", err)
	}
	return &discordStateManager{secret: secret, secure: secure}, nil
}

// issue returns a single-use state value for one flow.
func (m *discordStateManager) issue(scope string, userID int64, sessionID, next string, at time.Time) string {
	nonce := make([]byte, 16)
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return ""
	}
	encodedNext := encodeToken([]byte(next))
	payload := fmt.Sprintf("v2|%s|%d|%d|%s|%s", scope, userID,
		at.Add(discordStateMaxAge).Unix(), encodedNext, encodeToken(nonce))
	return encodeToken([]byte(payload)) + "." + encodeToken(m.sign(payload, sessionID))
}

// verify checks the signature, the expiry, the scope and the account/session
// binding, and returns the payload for the caller.
func (m *discordStateManager) verify(token, scope string, userID int64, sessionID string, at time.Time) (discordState, bool) {
	parts := strings.Split(token, ".")
	if len(parts) != 2 {
		return discordState{}, false
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return discordState{}, false
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return discordState{}, false
	}
	payload := string(raw)
	if !hmac.Equal(sig, m.sign(payload, sessionID)) {
		return discordState{}, false
	}
	fields := strings.Split(payload, "|")
	if len(fields) != 6 || fields[0] != "v2" {
		return discordState{}, false
	}
	if fields[1] != scope {
		return discordState{}, false
	}
	if fields[2] != strconv.FormatInt(userID, 10) {
		return discordState{}, false
	}
	expires, err := strconv.ParseInt(fields[3], 10, 64)
	if err != nil || at.After(time.Unix(expires, 0)) {
		return discordState{}, false
	}
	next, err := base64.RawURLEncoding.DecodeString(fields[4])
	if err != nil {
		return discordState{}, false
	}
	return discordState{Scope: scope, UserID: userID, Next: string(next)}, true
}

func (m *discordStateManager) sign(payload, sessionID string) []byte {
	mac := hmac.New(sha256.New, m.secret)
	_, _ = mac.Write([]byte(payload))
	_, _ = mac.Write([]byte("|" + sessionID))
	return mac.Sum(nil)
}

func (m *discordStateManager) setCookie(c *gin.Context, name, token string) {
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(name, token, int(discordStateMaxAge.Seconds()), "/", "", m.secure, true)
}

func (m *discordStateManager) clearCookie(c *gin.Context, name string) {
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(name, "", -1, "/", "", m.secure, true)
}
