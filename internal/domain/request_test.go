package domain

import (
	"errors"
	"strings"
	"testing"
)

// requestValidationFields asserts the error is the structured validation error
// the handlers map to a 422 and returns its field map.
func requestValidationFields(t *testing.T, err error) map[string]string {
	t.Helper()
	var validation *ValidationError
	if !errors.As(err, &validation) {
		t.Fatalf("err = %v, want *ValidationError", err)
	}
	return validation.Fields
}

func validMemberRequest() CreateRequestInput {
	return CreateRequestInput{
		GuildBankItemID: 7, Quantity: 2, CharacterName: "Icefear",
		Kind: RequesterMember, Actor: Actor{UserID: 1, UserName: "member"},
		CorrelationID: "corr-1",
	}
}

func validPublicRequest() CreateRequestInput {
	return CreateRequestInput{
		GuildBankItemID: 7, Quantity: 2, CharacterName: "Icefear",
		ContactDiscord: "guildie#1234", Kind: RequesterPublic,
		CorrelationID: "corr-1",
	}
}

// TestCreateRequestInputValidateAcceptsBothKinds pins the happy path for a
// member request and a public request, including the public contact requirement.
func TestCreateRequestInputValidateAcceptsBothKinds(t *testing.T) {
	if err := validMemberRequest().Validate(); err != nil {
		t.Fatalf("member request rejected: %v", err)
	}
	if err := validPublicRequest().Validate(); err != nil {
		t.Fatalf("public request rejected: %v", err)
	}
}

// TestCreateRequestInputValidateReportsEveryField proves validation collects
// all invalid fields together rather than stopping at the first.
func TestCreateRequestInputValidateReportsEveryField(t *testing.T) {
	in := CreateRequestInput{
		GuildBankItemID: 0, Quantity: 0, CharacterName: "Al",
		Note: strings.Repeat("x", RequestMaxNoteRunes+1), Kind: "Officer",
		CorrelationID: "",
	}
	fields := requestValidationFields(t, in.Validate())
	for _, name := range []string{"guildBankItemId", "quantity", "characterName", "note", "requesterKind", "correlationId"} {
		if fields[name] == "" {
			t.Fatalf("field %q missing from %v", name, fields)
		}
	}
}

// TestCreateRequestInputValidateRejectsBadValues covers the structural rules
// that need no store: character ownership and lot availability are checked by
// the service.
func TestCreateRequestInputValidateRejectsBadValues(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*CreateRequestInput)
		field  string
	}{
		{
			name:   "quantity zero",
			mutate: func(in *CreateRequestInput) { in.Quantity = 0 },
			field:  "quantity",
		},
		{
			name:   "character too short",
			mutate: func(in *CreateRequestInput) { in.CharacterName = "Al" },
			field:  "characterName",
		},
		{
			name:   "character not letters",
			mutate: func(in *CreateRequestInput) { in.CharacterName = "Ice9fear" },
			field:  "characterName",
		},
		{
			name:   "note too long",
			mutate: func(in *CreateRequestInput) { in.Note = strings.Repeat("x", RequestMaxNoteRunes+1) },
			field:  "note",
		},
		{
			name:   "note control character",
			mutate: func(in *CreateRequestInput) { in.Note = "for\x00raid" },
			field:  "note",
		},
		{
			name:   "public without contact",
			mutate: func(in *CreateRequestInput) { *in = validPublicRequest(); in.ContactDiscord = "   " },
			field:  "contactDiscord",
		},
		{
			name: "public contact too long",
			mutate: func(in *CreateRequestInput) {
				*in = validPublicRequest()
				in.ContactDiscord = strings.Repeat("x", RequestMaxContactRunes+1)
			},
			field: "contactDiscord",
		},
		{
			name: "public carrying an account",
			mutate: func(in *CreateRequestInput) {
				*in = validPublicRequest()
				in.Actor = Actor{UserID: 1, UserName: "member"}
			},
			field: "requesterKind",
		},
		{
			name:   "member without an account",
			mutate: func(in *CreateRequestInput) { *in = validMemberRequest(); in.Actor = Actor{} },
			field:  "requesterKind",
		},
		{
			name:   "unknown kind",
			mutate: func(in *CreateRequestInput) { in.Kind = "Officer" },
			field:  "requesterKind",
		},
		{
			name:   "idempotency key too long",
			mutate: func(in *CreateRequestInput) { in.IdempotencyKey = strings.Repeat("k", 65) },
			field:  "idempotencyKey",
		},
		{
			name:   "missing correlation id",
			mutate: func(in *CreateRequestInput) { in.CorrelationID = " " },
			field:  "correlationId",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := validMemberRequest()
			tc.mutate(&in)
			fields := requestValidationFields(t, in.Validate())
			if fields[tc.field] == "" {
				t.Fatalf("field %q missing from %v", tc.field, fields)
			}
		})
	}
}

// TestRequestStatusLifecycle pins the closed status set and the terminal rule
// that makes every non-Pending state immutable.
func TestRequestStatusLifecycle(t *testing.T) {
	for _, status := range RequestStatuses {
		if !status.Valid() {
			t.Fatalf("%q should be valid", status)
		}
		if got, want := status.Terminal(), status != RequestPending; got != want {
			t.Fatalf("%q Terminal() = %t, want %t", status, got, want)
		}
	}
	for _, invalid := range []RequestStatus{"", "Approved", "pending"} {
		if invalid.Valid() {
			t.Fatalf("%q should be invalid", invalid)
		}
		if invalid.Terminal() {
			t.Fatalf("%q Terminal() = true, want false", invalid)
		}
	}
	if !RequesterMember.Valid() || !RequesterPublic.Valid() {
		t.Fatal("approved requester kinds must be valid")
	}
	if RequesterKind("Anonymous").Valid() {
		t.Fatal("unknown requester kind should be invalid")
	}
}

// TestInventoryRequestProjections pins the derived display state: availability
// needs an active lot with stock, and a short issue is a completed request whose
// issued quantity is below the requested quantity.
func TestInventoryRequestProjections(t *testing.T) {
	request := InventoryRequest{RequestedQuantity: 5, LotActive: true, CurrentOnHand: 5}
	if !request.Available() {
		t.Fatal("active stocked lot should be available")
	}
	request.LotActive = false
	if request.Available() {
		t.Fatal("soft-deleted lot should not be available")
	}
	request.LotActive = true
	request.CurrentOnHand = 0
	if request.Available() {
		t.Fatal("empty lot should not be available")
	}

	if request.ShortIssued() {
		t.Fatal("a pending request has no issued quantity and cannot be short")
	}
	issued, full := uint32(4), uint32(5)
	request.IssuedQuantity = &full
	if request.ShortIssued() {
		t.Fatalf("issued %d of %d must not be short", full, request.RequestedQuantity)
	}
	request.IssuedQuantity = &issued
	if !request.ShortIssued() {
		t.Fatalf("issued %d of %d must be short", issued, request.RequestedQuantity)
	}
}
