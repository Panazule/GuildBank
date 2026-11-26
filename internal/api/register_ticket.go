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

	"github.com/Panazule/guildbank/internal/domain"
)

const (
	// registrationTicketCookie carries the verified identity between the
	// Discord callback and the username form. It is HttpOnly and never placed in
	// a URL, a form field or a log.
	registrationTicketCookie = "guildbank_register_ticket"
	// registrationTicketMaxAge matches service.registrationTicketTTL.
	registrationTicketMaxAge = 10 * time.Minute
)

// registrationTicketSigner signs the post-OAuth registration ticket. It is a
// dedicated signer rather than the OAuth state manager: the payload answers a
// different question and mixing the two formats would make each one do two
// jobs. The secret is process-local, so a restart abandons in-flight
// registrations, which is safe: nothing has been written yet.
type registrationTicketSigner struct {
	secret []byte
	secure bool
}

func newRegistrationTicketSigner(secure bool) (*registrationTicketSigner, error) {
	secret := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, secret); err != nil {
		return nil, fmt.Errorf("api: registration ticket secret: %w", err)
	}
	return &registrationTicketSigner{secret: secret, secure: secure}, nil
}

// issue encodes the ticket as base64url(payload) + "." + base64url(HMAC).
// Payload: v1|discordUserID|guildCheck|guildID|issuedAtUnix|expiryUnix|nonce|displayName
// Every free-form value is base64url encoded, so no value can introduce a
// field separator.
func (m *registrationTicketSigner) issue(t domain.RegistrationTicket) string {
	nonce := make([]byte, 16)
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return ""
	}
	expires := t.ExpiresAt
	if expires.IsZero() {
		expires = t.IssuedAt.Add(registrationTicketMaxAge)
	}
	payload := strings.Join([]string{
		"v1",
		encodeToken([]byte(t.DiscordUserID)),
		t.GuildCheck.String(),
		encodeToken([]byte(t.GuildID)),
		strconv.FormatInt(t.IssuedAt.Unix(), 10),
		strconv.FormatInt(expires.Unix(), 10),
		encodeToken(nonce),
		encodeToken([]byte(t.DiscordUserName)),
	}, "|")
	return encodeToken([]byte(payload)) + "." + encodeToken(m.sign(payload))
}

// verify checks the signature, the format and the expiry. A ticket whose guild
// check is not one of the approved outcomes is rejected rather than trusted.
func (m *registrationTicketSigner) verify(token string, at time.Time) (domain.RegistrationTicket, bool) {
	parts := strings.Split(token, ".")
	if len(parts) != 2 {
		return domain.RegistrationTicket{}, false
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return domain.RegistrationTicket{}, false
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return domain.RegistrationTicket{}, false
	}
	payload := string(raw)
	if !hmac.Equal(sig, m.sign(payload)) {
		return domain.RegistrationTicket{}, false
	}
	fields := strings.Split(payload, "|")
	if len(fields) != 8 || fields[0] != "v1" {
		return domain.RegistrationTicket{}, false
	}
	discordUserID, err := base64.RawURLEncoding.DecodeString(fields[1])
	if err != nil {
		return domain.RegistrationTicket{}, false
	}
	guildID, err := base64.RawURLEncoding.DecodeString(fields[3])
	if err != nil {
		return domain.RegistrationTicket{}, false
	}
	issued, err := strconv.ParseInt(fields[4], 10, 64)
	if err != nil {
		return domain.RegistrationTicket{}, false
	}
	expires, err := strconv.ParseInt(fields[5], 10, 64)
	if err != nil || at.After(time.Unix(expires, 0)) {
		return domain.RegistrationTicket{}, false
	}
	displayName, err := base64.RawURLEncoding.DecodeString(fields[7])
	if err != nil {
		return domain.RegistrationTicket{}, false
	}
	guildCheck := domain.GuildCheck(fields[2])
	if !guildCheck.Valid() {
		return domain.RegistrationTicket{}, false
	}
	return domain.RegistrationTicket{
		DiscordUserID:   string(discordUserID),
		DiscordUserName: string(displayName),
		GuildCheck:      guildCheck,
		GuildID:         string(guildID),
		IssuedAt:        time.Unix(issued, 0).UTC(),
		ExpiresAt:       time.Unix(expires, 0).UTC(),
	}, true
}

func (m *registrationTicketSigner) sign(payload string) []byte {
	mac := hmac.New(sha256.New, m.secret)
	_, _ = mac.Write([]byte(payload))
	return mac.Sum(nil)
}

func (m *registrationTicketSigner) setCookie(c *gin.Context, token string) {
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(registrationTicketCookie, token, int(registrationTicketMaxAge.Seconds()), "/", "", m.secure, true)
}

func (m *registrationTicketSigner) clearCookie(c *gin.Context) {
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(registrationTicketCookie, "", -1, "/", "", m.secure, true)
}

// ticket reads and verifies the cookie. An absent, expired or tampered ticket
// reports false; the caller then clears the cookie and redirects.
func (m *registrationTicketSigner) ticket(c *gin.Context, at time.Time) (domain.RegistrationTicket, bool) {
	raw, err := c.Cookie(registrationTicketCookie)
	if err != nil || raw == "" {
		return domain.RegistrationTicket{}, false
	}
	return m.verify(raw, at)
}
