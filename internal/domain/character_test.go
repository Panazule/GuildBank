package domain

import (
	"errors"
	"strings"
	"testing"
)

func TestValidateCharacterName(t *testing.T) {
	valid := []string{"Icefear", "  Banktoon  ", "abc", strings.Repeat("a", CharacterMaxRunes)}
	for _, name := range valid {
		if err := ValidateCharacterName(name); err != nil {
			t.Fatalf("ValidateCharacterName(%q) = %v, want nil", name, err)
		}
	}

	invalid := []struct {
		name string
		want string
	}{
		{"", "must not be blank"},
		{"   ", "must not be blank"},
		{"ab", "must be at least 3 letters"},
		{strings.Repeat("a", CharacterMaxRunes+1), "must be at most 15 letters"},
		{"Ice fear", "must contain letters only"},
		{"Icefear1", "must contain letters only"},
		{"Icefear-", "must contain letters only"},
		{"Éclair", "must contain letters only"},
		{"bad\nname", "must contain letters only"},
	}
	for _, c := range invalid {
		err := ValidateCharacterName(c.name)
		var v *ValidationError
		if !errors.As(err, &v) {
			t.Fatalf("ValidateCharacterName(%q) = %v, want a validation error", c.name, err)
		}
		if got := v.Fields["characterName"]; got != c.want {
			t.Fatalf("ValidateCharacterName(%q) field = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestAddCharacterInput(t *testing.T) {
	in := AddCharacterInput{Name: "  Icefear  "}
	if err := in.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if in.Normalized() != "Icefear" {
		t.Fatalf("Normalized = %q, want Icefear", in.Normalized())
	}
	if err := (AddCharacterInput{Name: "ab"}).Validate(); !errors.Is(err, ErrValidation) {
		t.Fatalf("short name = %v, want ErrValidation", err)
	}
}
