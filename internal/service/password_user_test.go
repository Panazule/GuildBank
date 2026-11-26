package service

import (
	"errors"
	"strings"
	"testing"

	"github.com/Panazule/guildbank/internal/domain"
)

// A username longer than the 12-byte minimum is used so the equality rule is
// reached; a short username would fail the length rule first.
const userPasswordTestName = "KeeperOfSecrets"

func TestHashUserPasswordRejectsUsernameAsPassword(t *testing.T) {
	for _, password := range []string{"KeeperOfSecrets", "keeperofsecrets", "  KEEPEROFSECRETS  "} {
		_, err := HashUserPassword(userPasswordTestName, password)
		var v *domain.ValidationError
		if !errors.As(err, &v) {
			t.Fatalf("HashUserPassword(%q) = %v, want a validation error", password, err)
		}
		if v.Fields["password"] == "" {
			t.Fatalf("HashUserPassword(%q) fields = %+v, want a password message", password, v.Fields)
		}
	}
}

func TestHashUserPasswordRoundTripsAndEnforcesLength(t *testing.T) {
	hash, err := HashUserPassword(userPasswordTestName, "a sufficiently long password")
	if err != nil {
		t.Fatalf("HashUserPassword: %v", err)
	}
	if !strings.HasPrefix(hash, "$2") {
		t.Fatalf("hash = %q, want bcrypt", hash)
	}
	if !VerifyPassword(hash, "a sufficiently long password") {
		t.Fatal("correct password did not verify")
	}
	if VerifyPassword(hash, "a different long password") {
		t.Fatal("wrong password verified")
	}

	for _, short := range []string{"short", strings.Repeat("x", PasswordMaxBytes+1)} {
		if _, err := HashUserPassword(userPasswordTestName, short); !errors.Is(err, domain.ErrValidation) {
			t.Fatalf("HashUserPassword(%d bytes) = %v, want validation", len(short), err)
		}
	}
}
