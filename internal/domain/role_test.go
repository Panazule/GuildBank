package domain

import "testing"

func TestParseRoleFailsClosed(t *testing.T) {
	table := map[string]Role{
		"Admin":   RoleAdmin,
		"Officer": RoleOfficer,
		"Member":  RoleMember,
		"admin":   RoleMember, // stored values are canonical; anything else is least privilege
		"Root":    RoleMember,
		"":        RoleMember,
	}
	for input, want := range table {
		if got := ParseRole(input); got != want {
			t.Fatalf("ParseRole(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestRoleHierarchyOrder(t *testing.T) {
	if !RoleAdmin.AtLeast(RoleAdmin) || !RoleAdmin.AtLeast(RoleOfficer) || !RoleAdmin.AtLeast(RoleMember) {
		t.Fatal("Admin must satisfy every level")
	}
	if RoleOfficer.AtLeast(RoleAdmin) {
		t.Fatal("Officer must not satisfy Admin")
	}
	if !RoleOfficer.AtLeast(RoleMember) || RoleMember.AtLeast(RoleOfficer) {
		t.Fatal("Member/Officer ordering is wrong")
	}
	for _, role := range Roles {
		if !role.Valid() {
			t.Fatalf("role %q is not valid", role)
		}
	}
	if Role("Root").Valid() {
		t.Fatal("unknown role reported as valid")
	}
}

func TestUserCapabilitiesFollowRole(t *testing.T) {
	member := User{AccessLevel: "Member"}
	officer := User{AccessLevel: "Officer"}
	admin := User{AccessLevel: AccessAdmin}
	unknown := User{AccessLevel: "Root"}

	if member.IsAdmin() || member.IsOfficer() || member.CanAdministerInventory() || member.CanManageSettings() {
		t.Fatal("Member was granted a privileged capability")
	}
	if !officer.IsOfficer() || !officer.CanAdministerInventory() || !officer.CanViewAudit() {
		t.Fatal("Officer is missing an inventory or audit capability")
	}
	if officer.IsAdmin() || officer.CanManageSettings() || officer.CanManageUsers() {
		t.Fatal("Officer was granted an Administrator capability")
	}
	if !admin.IsAdmin() || !admin.IsOfficer() || !admin.CanManageSettings() || !admin.CanManageUsers() {
		t.Fatal("Admin is missing a capability")
	}
	if unknown.IsOfficer() || unknown.CanAdministerInventory() || unknown.CanManageSettings() {
		t.Fatal("an unknown access level must fail closed to Member")
	}
}

func TestActorCapabilitySupportsLegacyFlag(t *testing.T) {
	if !(Actor{IsAdmin: true}).CanManageInventory() {
		t.Fatal("legacy IsAdmin actor lost inventory rights")
	}
	if (Actor{IsAdmin: false}).CanManageInventory() {
		t.Fatal("non-admin legacy actor was granted inventory rights")
	}
	if !(Actor{Role: RoleOfficer}).CanManageInventory() {
		t.Fatal("officer actor was denied inventory rights")
	}
	if !(Actor{Role: RoleAdmin}).CanManageInventory() {
		t.Fatal("admin actor was denied inventory rights")
	}
	if (Actor{Role: RoleMember}).CanManageInventory() {
		t.Fatal("member actor was granted inventory rights")
	}
}
