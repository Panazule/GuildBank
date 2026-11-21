// Package service implements authentication, setup, sessions and settings.
// Phase 1 deliberately excludes audit recording, inventory and the MariaDB
// business repositories; those arrive in later phases.
package service

import (
	"strings"

	"golang.org/x/crypto/bcrypt"

	"github.com/Panazule/guildbank/internal/domain"
)

const (
	// PasswordMinBytes is the proposed minimum length (MR-11). It is recorded
	// for owner approval in the Phase 1 decision log.
	PasswordMinBytes = 12
	// PasswordMaxBytes is bcrypt's hard input limit.
	PasswordMaxBytes = 72
)

// timingHash equalizes the work performed for unknown usernames. It is
// generated once at startup rather than shipped as a fixed hash.
var timingHash []byte

func init() {
	h, err := bcrypt.GenerateFromPassword([]byte("guildbank-timing-equalizer"), bcrypt.DefaultCost)
	if err == nil {
		timingHash = h
	}
}

// HashPassword validates the policy then returns a bcrypt hash.
func HashPassword(password string) (string, error) {
	if field := passwordPolicyError("", password); field != "" {
		return "", domain.NewValidationError("Password", field)
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(hash), nil
}

// VerifyPassword performs a constant-work bcrypt comparison.
func VerifyPassword(hash, password string) bool {
	if hash == "" {
		return false
	}
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}

// verifyUnknown performs a dummy bcrypt comparison so a missing account costs
// approximately the same as a wrong password.
func verifyUnknown(password string) {
	if len(timingHash) == 0 {
		return
	}
	_ = bcrypt.CompareHashAndPassword(timingHash, []byte(password))
}

func passwordPolicyError(username, password string) string {
	if len(password) < PasswordMinBytes {
		return "must be at least 12 characters"
	}
	if len(password) > PasswordMaxBytes {
		return "must be at most 72 bytes"
	}
	if username != "" && strings.EqualFold(strings.TrimSpace(username), strings.TrimSpace(password)) {
		return "must not equal the username"
	}
	if strings.TrimSpace(password) == "" {
		return "must not be blank"
	}
	return ""
}
