package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Panazule/guildbank/internal/domain"
)

// fakeRegistrationStore records what the registration service writes.
type fakeRegistrationStore struct {
	users      map[int64]domain.User
	identities map[string]int64
	nextID     int64
	createErr  error
}

func newFakeRegistrationStore() *fakeRegistrationStore {
	return &fakeRegistrationStore{users: map[int64]domain.User{}, identities: map[string]int64{}, nextID: 1}
}

func (f *fakeRegistrationStore) CreateRegisteredUser(ctx context.Context, u domain.User,
	identity domain.DiscordIdentity) (domain.User, error) {
	if f.createErr != nil {
		return domain.User{}, f.createErr
	}
	u.ID = f.nextID
	f.nextID++
	f.users[u.ID] = u
	f.identities[identity.DiscordUserID] = u.ID
	return u, nil
}

func (f *fakeRegistrationStore) DiscordUserIDOwner(ctx context.Context, discordUserID string) (int64, error) {
	if owner, ok := f.identities[discordUserID]; ok {
		return owner, nil
	}
	return 0, domain.ErrNotFound
}

func (f *fakeRegistrationStore) GetUserByID(ctx context.Context, id int64) (domain.User, error) {
	user, ok := f.users[id]
	if !ok {
		return domain.User{}, domain.ErrNotFound
	}
	return user, nil
}

func registrationSettings(guildID string) fakeDiscordSettings {
	return fakeDiscordSettings{values: map[string]string{
		"SelfRegistrationEnabled": "true",
		"DiscordEnabled":          "true",
		"DiscordClientId":         "123456789012345678",
		"DiscordClientSecret":     "client-secret",
		"PublicBaseUrl":           "https://guildbank.example.com",
		"DiscordGuildId":          guildID,
	}}
}

func newRegistrationForTest(t *testing.T, settings fakeDiscordSettings, api DiscordAPI,
	store RegistrationStore) *RegistrationService {
	t.Helper()
	svc := NewRegistrationService(settings, api, store, nil, nil)
	svc.now = func() time.Time { return time.Date(2026, 11, 26, 12, 0, 0, 0, time.UTC) }
	return svc
}

func memberAPI() *stubDiscordAPI {
	return &stubDiscordAPI{
		profile:     domain.DiscordProfile{ID: "123456789012345678", UserName: "keldor", GlobalName: "Keldor"},
		token:       domain.DiscordToken{AccessToken: "access-token"},
		guildMember: true,
	}
}

func serviceConfigForTest() DiscordConfig {
	return DiscordConfig{Enabled: true, ClientID: "123456789012345678", ClientSecret: "s",
		PublicBaseURL: "https://guildbank.example.com"}
}

// TestRegistrationGuildCheckFailsClosed pins the decision table from §6.2: only
// a definitive member approves; errors and a missing guild ID never do.
func TestRegistrationGuildCheckFailsClosed(t *testing.T) {
	cases := []struct {
		name    string
		guildID string
		api     *stubDiscordAPI
		want    domain.GuildCheck
	}{
		{"member", "987654321098765432", memberAPI(), domain.GuildCheckMember},
		{"not a member", "987654321098765432", &stubDiscordAPI{
			profile: domain.DiscordProfile{ID: "123456789012345678"},
			token:   domain.DiscordToken{AccessToken: "t"}, guildMember: false}, domain.GuildCheckNotMember},
		{"provider error", "987654321098765432", &stubDiscordAPI{
			profile: domain.DiscordProfile{ID: "123456789012345678"},
			token:   domain.DiscordToken{AccessToken: "t"}, guildErr: errors.New("403")}, domain.GuildCheckError},
		{"no guild configured", "", memberAPI(), domain.GuildCheckNotConfigured},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store := newFakeRegistrationStore()
			svc := newRegistrationForTest(t, registrationSettings(tc.guildID), tc.api, store)
			ticket, err := svc.Verify(context.Background(), "code", "req")
			if err != nil {
				t.Fatalf("Verify: %v", err)
			}
			if ticket.GuildCheck != tc.want {
				t.Fatalf("guild check = %q, want %q", ticket.GuildCheck, tc.want)
			}
			if len(store.users) != 0 {
				t.Fatal("Verify must not write anything")
			}
		})
	}
}

// TestRegistrationCompleteAppliesStatus proves the status, origin and
// verification stamp follow the ticket rather than the request.
func TestRegistrationCompleteAppliesStatus(t *testing.T) {
	store := newFakeRegistrationStore()
	svc := newRegistrationForTest(t, registrationSettings("987654321098765432"), memberAPI(), store)

	approved, err := svc.Complete(context.Background(), domain.RegistrationTicket{
		DiscordUserID: "123456789012345678", DiscordUserName: "Keldor",
		GuildCheck: domain.GuildCheckMember, GuildID: "987654321098765432",
		IssuedAt: time.Now(), ExpiresAt: time.Now().Add(time.Minute),
	}, "Keldor", "req", "203.0.113.10")
	if err != nil {
		t.Fatalf("Complete(member): %v", err)
	}
	if !approved.IsApproved() || !approved.IsEnabled() {
		t.Fatalf("status = %q, want Approved", approved.Status())
	}
	if approved.Origin != domain.OriginSelfRegistration {
		t.Fatalf("origin = %q", approved.Origin)
	}
	if approved.AccessLevel != string(domain.RoleMember) {
		t.Fatalf("access level = %q, want Member", approved.AccessLevel)
	}
	if approved.GuildVerifiedAt == nil {
		t.Fatal("an approved registration must record GuildVerifiedAt")
	}
	if approved.PasswordHash != "" {
		t.Fatal("Complete must not return the password hash")
	}
	// The stored hash is a discarded secret: no password can verify against it.
	stored := store.users[approved.ID]
	if stored.PasswordHash == "" || VerifyPassword(stored.PasswordHash, "Keldor") {
		t.Fatal("the stored hash must be non-empty and must not verify a guess")
	}

	pending, err := svc.Complete(context.Background(), domain.RegistrationTicket{
		DiscordUserID: "223456789012345678", DiscordUserName: "Second",
		GuildCheck: domain.GuildCheckNotMember, GuildID: "987654321098765432",
		IssuedAt: time.Now(), ExpiresAt: time.Now().Add(time.Minute),
	}, "Second", "req", "203.0.113.10")
	if err != nil {
		t.Fatalf("Complete(not member): %v", err)
	}
	if !pending.IsPending() || pending.IsEnabled() {
		t.Fatalf("status = %q, want PendingApproval", pending.Status())
	}
	if pending.GuildVerifiedAt != nil {
		t.Fatal("a pending registration must not record a verification stamp")
	}
}

// TestRegistrationNeverClaimsAnExistingAccount proves a Discord identity that
// already owns an account stops the flow in both directions.
func TestRegistrationNeverClaimsAnExistingAccount(t *testing.T) {
	store := newFakeRegistrationStore()
	store.users[7] = domain.User{ID: 7, UserName: "Keldor", AccessLevel: string(domain.RoleMember),
		AccountStatus: string(domain.AccountStatusPendingApproval)}
	store.identities["123456789012345678"] = 7
	svc := newRegistrationForTest(t, registrationSettings("987654321098765432"), memberAPI(), store)

	_, err := svc.Verify(context.Background(), "code", "req")
	var claimed *RegistrationClaimedError
	if !errors.As(err, &claimed) || !claimed.Pending {
		t.Fatalf("Verify error = %v, want a pending claim", err)
	}
	if !errors.Is(err, domain.ErrAlreadyExists) {
		t.Fatal("a claim must wrap ErrAlreadyExists")
	}

	store.users[7] = domain.User{ID: 7, UserName: "Keldor", AccessLevel: string(domain.RoleMember),
		AccountStatus: string(domain.AccountStatusApproved)}
	if _, err := svc.Verify(context.Background(), "code", "req"); !errors.As(err, &claimed) || claimed.Pending {
		t.Fatalf("approved claim = %v", err)
	}

	// Complete refuses the claim too, even with a valid ticket.
	_, err = svc.Complete(context.Background(), domain.RegistrationTicket{
		DiscordUserID: "123456789012345678", GuildCheck: domain.GuildCheckMember,
		IssuedAt: time.Now(), ExpiresAt: time.Now().Add(time.Minute),
	}, "Someone", "req", "203.0.113.11")
	if !errors.As(err, &claimed) {
		t.Fatalf("Complete error = %v, want a claim", err)
	}
	if len(store.users) != 1 {
		t.Fatalf("users = %d, want 1 (nothing written)", len(store.users))
	}
}

// used, so registration can never redeem a link or sign-in code, and that its
// consent URL is the only one carrying the guilds scope.
func TestRegistrationExchangeUsesRegisterRedirectURI(t *testing.T) {
	store := newFakeRegistrationStore()
	api := memberAPI()
	svc := newRegistrationForTest(t, registrationSettings("987654321098765432"), api, store)
	if _, err := svc.Verify(context.Background(), "code", "req"); err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if len(api.exchangeArgs) != 4 {
		t.Fatalf("exchange args = %v", api.exchangeArgs)
	}
	if got := api.exchangeArgs[2]; got != "https://guildbank.example.com/register/discord/callback" {
		t.Fatalf("redirect_uri = %q", got)
	}
	if got := svc.AuthorizeURL(serviceConfigForTest(), "state"); !strings.Contains(got, "scope=identify+guilds") {
		t.Fatalf("registration authorize URL must request the guilds scope: %s", got)
	}
}

// TestRegistrationUnavailableWhenDisabled proves the settings switch gates both
// halves of the flow.
func TestRegistrationUnavailableWhenDisabled(t *testing.T) {
	for _, values := range []map[string]string{
		{"SelfRegistrationEnabled": "false", "DiscordEnabled": "true",
			"DiscordClientId": "123456789012345678", "DiscordClientSecret": "s",
			"PublicBaseUrl": "https://guildbank.example.com"},
		{"SelfRegistrationEnabled": "true", "DiscordEnabled": "false"},
	} {
		store := newFakeRegistrationStore()
		svc := NewRegistrationService(fakeDiscordSettings{values: values}, memberAPI(), store, nil, nil)
		if available, err := svc.Available(context.Background()); err != nil || available {
			t.Fatalf("Available = %v, %v; want false", available, err)
		}
		if _, err := svc.Verify(context.Background(), "code", "req"); !errors.Is(err, domain.ErrUnavailable) {
			t.Fatalf("Verify error = %v, want ErrUnavailable", err)
		}
		if _, err := svc.Complete(context.Background(), domain.RegistrationTicket{
			DiscordUserID: "123456789012345678", GuildCheck: domain.GuildCheckMember,
			IssuedAt: time.Now(), ExpiresAt: time.Now().Add(time.Minute),
		}, "Keldor", "req", "203.0.113.12"); !errors.Is(err, domain.ErrUnavailable) {
			t.Fatalf("Complete error = %v, want ErrUnavailable", err)
		}
	}
}
