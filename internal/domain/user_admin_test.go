package domain

import (
	"errors"
	"strings"
	"testing"
)

func TestValidateUserName(t *testing.T) {
	valid := []string{"Alice", "  Keeper  ", "Ünïcode Näme", strings.Repeat("a", UserNameMaxRunes)}
	for _, name := range valid {
		if err := ValidateUserName(name); err != nil {
			t.Fatalf("ValidateUserName(%q) = %v, want nil", name, err)
		}
	}

	invalid := []struct {
		name string
		want string
	}{
		{"", "must not be blank"},
		{"   ", "must not be blank"},
		{strings.Repeat("a", UserNameMaxRunes+1), "must be at most 64 characters"},
		{"bad\nname", "must not contain control characters"},
		{"tab\tname", "must not contain control characters"},
		{string([]byte{0xff, 0xfe}), "must be valid UTF-8"},
	}
	for _, c := range invalid {
		err := ValidateUserName(c.name)
		var v *ValidationError
		if !errors.As(err, &v) {
			t.Fatalf("ValidateUserName(%q) = %v, want a validation error", c.name, err)
		}
		if got := v.Fields["userName"]; got != c.want {
			t.Fatalf("ValidateUserName(%q) field = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestParseRoleInput(t *testing.T) {
	for _, c := range []struct {
		in   string
		want Role
	}{
		{"", RoleMember},
		{"   ", RoleMember},
		{"Member", RoleMember},
		{"Officer", RoleOfficer},
		{" Officer ", RoleOfficer},
		{"Admin", RoleAdmin},
	} {
		got, err := ParseRoleInput(c.in)
		if err != nil || got != c.want {
			t.Fatalf("ParseRoleInput(%q) = %q, %v; want %q", c.in, got, err, c.want)
		}
	}

	// Input is rejected rather than silently downgraded, even though the stored
	// value parser fails closed.
	for _, bad := range []string{"Root", "administrator", "ADMIN"} {
		_, err := ParseRoleInput(bad)
		var v *ValidationError
		if !errors.As(err, &v) || v.Fields["accessLevel"] == "" {
			t.Fatalf("ParseRoleInput(%q) = %v, want an accessLevel validation error", bad, err)
		}
	}
}

func TestCreateUserInputValidate(t *testing.T) {
	valid := CreateUserInput{UserName: "Keeper", AccessLevel: "Officer", Password: "a long password", ConfirmPassword: "a long password"}
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid input: %v", err)
	}
	if valid.RoleValue() != RoleOfficer || valid.NormalizedUserName() != "Keeper" {
		t.Fatalf("valid input helpers = %q / %q", valid.RoleValue(), valid.NormalizedUserName())
	}

	// A blank role means Member, the least privileged role.
	blankRole := CreateUserInput{UserName: "Keeper", Password: "a long password", ConfirmPassword: "a long password"}
	if err := blankRole.Validate(); err != nil {
		t.Fatalf("blank role: %v", err)
	}
	if blankRole.RoleValue() != RoleMember {
		t.Fatalf("blank role value = %q, want Member", blankRole.RoleValue())
	}

	err := CreateUserInput{}.Validate()
	var v *ValidationError
	if !errors.As(err, &v) {
		t.Fatalf("empty input = %v, want a validation error", err)
	}
	if v.Fields["userName"] == "" || v.Fields["password"] == "" {
		t.Fatalf("empty input fields = %+v, want userName and password", v.Fields)
	}
	if _, reported := v.Fields["accessLevel"]; reported {
		t.Fatalf("blank role reported as invalid: %+v", v.Fields)
	}

	if err := (CreateUserInput{UserName: "Keeper", AccessLevel: "Root", Password: "a long password",
		ConfirmPassword: "a long password"}).Validate(); !errors.As(err, &v) || v.Fields["accessLevel"] == "" {
		t.Fatalf("unknown role = %v, want an accessLevel validation error", err)
	}
	if err := (CreateUserInput{UserName: "Keeper", Password: "a long password",
		ConfirmPassword: "a different password"}).Validate(); !errors.As(err, &v) || v.Fields["confirmPassword"] == "" {
		t.Fatalf("mismatched confirmation = %v, want a confirmPassword validation error", err)
	}
}

func TestResetPasswordInputValidate(t *testing.T) {
	if err := (ResetPasswordInput{Password: "a long password", ConfirmPassword: "a long password"}).Validate(); err != nil {
		t.Fatalf("valid input: %v", err)
	}
	var v *ValidationError
	err := ResetPasswordInput{ConfirmPassword: "a long password"}.Validate()
	if !errors.As(err, &v) || v.Fields["password"] == "" {
		t.Fatalf("blank password = %v, want a password validation error", err)
	}
	err = ResetPasswordInput{Password: "a long password", ConfirmPassword: "another password"}.Validate()
	if !errors.As(err, &v) || v.Fields["confirmPassword"] == "" {
		t.Fatalf("mismatched confirmation = %v, want a confirmPassword validation error", err)
	}
}

func TestChangePasswordInputValidate(t *testing.T) {
	valid := ChangePasswordInput{CurrentPassword: "an old long password", NewPassword: "a new long password",
		ConfirmPassword: "a new long password"}
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid input: %v", err)
	}

	cases := []struct {
		in    ChangePasswordInput
		field string
	}{
		{ChangePasswordInput{NewPassword: "a new long password", ConfirmPassword: "a new long password"}, "currentPassword"},
		{ChangePasswordInput{CurrentPassword: "an old long password", ConfirmPassword: "a new long password"}, "newPassword"},
		{ChangePasswordInput{CurrentPassword: "an old long password", NewPassword: "a new long password",
			ConfirmPassword: "different password"}, "confirmPassword"},
		{ChangePasswordInput{CurrentPassword: "the same password", NewPassword: "the same password",
			ConfirmPassword: "the same password"}, "newPassword"},
	}
	for _, c := range cases {
		err := c.in.Validate()
		var v *ValidationError
		if !errors.As(err, &v) || v.Fields[c.field] == "" {
			t.Fatalf("%+v = %v, want a %s validation error", c.in, err, c.field)
		}
	}
}

func TestDeleteUserInputValidate(t *testing.T) {
	if err := (DeleteUserInput{ConfirmUserName: " keeper "}).Validate("Keeper"); err != nil {
		t.Fatalf("case-insensitive match: %v", err)
	}
	var v *ValidationError
	err := (DeleteUserInput{ConfirmUserName: "Keper"}).Validate("Keeper")
	if !errors.As(err, &v) || v.Fields["confirmUserName"] == "" {
		t.Fatalf("mismatched confirmation = %v, want a confirmUserName validation error", err)
	}
}
