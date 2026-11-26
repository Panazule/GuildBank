package domain

import "testing"

// TestParseAccountStatusFailsClosed pins the read rule: a blank value is
// Approved (existing rows and zero-value structs), an unknown value is
// PendingApproval so corruption can never authenticate.
func TestParseAccountStatusFailsClosed(t *testing.T) {
	cases := map[string]AccountStatus{
		"":                AccountStatusApproved,
		"Approved":        AccountStatusApproved,
		"PendingApproval": AccountStatusPendingApproval,
		"Bogus":           AccountStatusPendingApproval,
	}
	for stored, want := range cases {
		if got := ParseAccountStatus(stored); got != want {
			t.Errorf("ParseAccountStatus(%q) = %q, want %q", stored, got, want)
		}
	}
	if !AccountStatusApproved.Valid() || !AccountStatusPendingApproval.Valid() {
		t.Fatal("an approved status value must validate")
	}
	if AccountStatus("Bogus").Valid() {
		t.Fatal("an unknown status must not validate")
	}
}

// TestIsEnabledRequiresApproval is the safety property of the design: the
// existing authentication gates all call IsEnabled, so a pending account is
// refused by code that never changed.
func TestIsEnabledRequiresApproval(t *testing.T) {
	pending := User{AccessLevel: string(RoleMember), AccountStatus: string(AccountStatusPendingApproval)}
	if pending.IsEnabled() {
		t.Fatal("a pending account must not be enabled")
	}
	if pending.IsApproved() || !pending.IsPending() {
		t.Fatalf("pending predicates = approved:%v pending:%v", pending.IsApproved(), pending.IsPending())
	}

	approved := User{AccessLevel: string(RoleMember), AccountStatus: string(AccountStatusApproved)}
	if !approved.IsEnabled() || !approved.IsApproved() || approved.IsPending() {
		t.Fatal("an approved, enabled account must be usable")
	}

	// Blank is the zero value every existing test and partial read produces.
	zero := User{AccessLevel: string(RoleMember)}
	if !zero.IsEnabled() {
		t.Fatal("a zero-value AccountStatus must read as Approved")
	}

	disabled := approved
	disabled.Disabled = true
	if disabled.IsEnabled() {
		t.Fatal("a disabled account must not be enabled even when approved")
	}
}
