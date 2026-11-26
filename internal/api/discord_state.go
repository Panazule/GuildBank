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
	discordStateCookieName = "guildbank_discord_state"
	// discordStateMaxAge bounds how long a started link may take to come back.
	discordStateMaxAge = 10 * time.Minute
)

// discordStateManager signs the OAuth state value and binds it to the account
// and session that started the flow. The value travels both in the cookie and in
// the authorization URL, so a callback must present the exact value this browser
// received (double submit) and the signature catches tampering. The HMAC secret
// is process-local, so a restart abandons in-flight links.
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

// issue returns a single-use state value for one account and session.
func (m *discordStateManager) issue(userID int64, sessionID string, at time.Time) string {
	nonce := make([]byte, 16)
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return ""
	}
	payload := fmt.Sprintf("v1|%d|%d|%s", userID, at.Add(discordStateMaxAge).Unix(), encodeToken(nonce))
	return encodeToken([]byte(payload)) + "." + encodeToken(m.sign(payload, sessionID))
}

// verify checks the signature, the expiry and the account/session binding.
func (m *discordStateManager) verify(token string, userID int64, sessionID string, at time.Time) bool {
	parts := strings.Split(token, ".")
	if len(parts) != 2 {
		return false
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return false
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return false
	}
	payload := string(raw)
	if !hmac.Equal(sig, m.sign(payload, sessionID)) {
		return false
	}
	fields := strings.Split(payload, "|")
	if len(fields) != 4 || fields[0] != "v1" {
		return false
	}
	if fields[1] != strconv.FormatInt(userID, 10) {
		return false
	}
	expires, err := strconv.ParseInt(fields[2], 10, 64)
	if err != nil || at.After(time.Unix(expires, 0)) {
		return false
	}
	return true
}

func (m *discordStateManager) sign(payload, sessionID string) []byte {
	mac := hmac.New(sha256.New, m.secret)
	_, _ = mac.Write([]byte(payload))
	_, _ = mac.Write([]byte("|" + sessionID))
	return mac.Sum(nil)
}

func (m *discordStateManager) setCookie(c *gin.Context, token string) {
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(discordStateCookieName, token, int(discordStateMaxAge.Seconds()), "/account", "", m.secure, true)
}

func (m *discordStateManager) clearCookie(c *gin.Context) {
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(discordStateCookieName, "", -1, "/account", "", m.secure, true)
}
