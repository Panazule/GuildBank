package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Panazule/guildbank/internal/domain"
)

type fakeDiscordSettings struct {
	values map[string]string
	err    error
}

func (f fakeDiscordSettings) Values(ctx context.Context) (map[string]string, error) {
	return f.values, f.err
}

type fakeDiscordStore struct {
	identities map[int64]domain.DiscordIdentity
	users      map[int64]domain.User
	setErr     error
	setCalls   int
	clearCalls int
	now        time.Time
}

func newFakeDiscordStore() *fakeDiscordStore {
	return &fakeDiscordStore{identities: map[int64]domain.DiscordIdentity{},
		users: map[int64]domain.User{}}
}

func (f *fakeDiscordStore) UserByDiscordID(ctx context.Context, discordUserID string) (domain.User, error) {
	for userID, identity := range f.identities {
		if identity.DiscordUserID != discordUserID {
			continue
		}
		if user, ok := f.users[userID]; ok {
			return user, nil
		}
		return domain.User{ID: userID, UserName: "Keeper", AccessLevel: string(domain.RoleMember)}, nil
	}
	return domain.User{}, domain.ErrNotFound
}

// fakeSessionIssuer records the accounts that were granted a session and can be
// told to fail, so the sign-in service is tested without real persistence.
type fakeSessionIssuer struct {
	issued []domain.User
	err    error
}

func (f *fakeSessionIssuer) IssueSession(ctx context.Context, user domain.User, clientIP, userAgent string) (LoginResult, error) {
	if f.err != nil {
		return LoginResult{}, f.err
	}
	f.issued = append(f.issued, user)
	now := time.Now().UTC()
	return LoginResult{User: user, Token: "session-token",
		ExpiresAt: now.Add(30 * time.Minute), AbsoluteExpiresAt: now.Add(8 * time.Hour)}, nil
}

func (f *fakeDiscordStore) DiscordIdentity(ctx context.Context, userID int64) (domain.DiscordIdentity, error) {
	identity, ok := f.identities[userID]
	if !ok {
		return domain.DiscordIdentity{UserID: userID}, nil
	}
	return identity, nil
}

func (f *fakeDiscordStore) DiscordUserIDOwner(ctx context.Context, discordUserID string) (int64, error) {
	for userID, identity := range f.identities {
		if identity.DiscordUserID == discordUserID {
			return userID, nil
		}
	}
	return 0, domain.ErrNotFound
}

func (f *fakeDiscordStore) SetDiscordIdentity(ctx context.Context, userID int64, discordUserID, discordUserName string,
	at time.Time, modBy string) error {
	f.setCalls++
	if f.setErr != nil {
		return f.setErr
	}
	linkedAt := at
	f.identities[userID] = domain.DiscordIdentity{UserID: userID, DiscordUserID: discordUserID,
		DiscordUserName: discordUserName, LinkedAt: &linkedAt}
	return nil
}

func (f *fakeDiscordStore) ClearDiscordIdentity(ctx context.Context, userID int64, at time.Time, modBy string) error {
	f.clearCalls++
	delete(f.identities, userID)
	return nil
}

type stubDiscordAPI struct {
	profile      domain.DiscordProfile
	token        domain.DiscordToken
	exchangeErr  error
	profileErr   error
	guildErr     error
	guildMember  bool
	exchangeArgs []string
	guildChecks  int
}

func (f *stubDiscordAPI) ExchangeCode(ctx context.Context, clientID, clientSecret, redirectURI, code string) (domain.DiscordToken, error) {
	f.exchangeArgs = []string{clientID, clientSecret, redirectURI, code}
	if f.exchangeErr != nil {
		return domain.DiscordToken{}, f.exchangeErr
	}
	return f.token, nil
}

func (f *stubDiscordAPI) CurrentUser(ctx context.Context, accessToken string) (domain.DiscordProfile, error) {
	if f.profileErr != nil {
		return domain.DiscordProfile{}, f.profileErr
	}
	return f.profile, nil
}

func (f *stubDiscordAPI) GuildMember(ctx context.Context, accessToken, guildID string) (bool, error) {
	f.guildChecks++
	if f.guildErr != nil {
		return false, f.guildErr
	}
	return f.guildMember, nil
}

func enabledDiscordSettings() map[string]string {
	return map[string]string{
		"DiscordEnabled":      "true",
		"DiscordClientId":     "123456789012345678",
		"DiscordClientSecret": "client-secret",
		"PublicBaseUrl":       "https://guildbank.example.com",
	}
}

func discordUser() domain.User {
	// HasPassword is true because these fixtures model an account that can still
	// sign in after unlinking (D-SR-18).
	return domain.User{ID: 2, UserName: "Keeper", AccessLevel: string(domain.RoleMember), HasPassword: true}
}

func TestParseDiscordConfig(t *testing.T) {
	cfg := ParseDiscordConfig(enabledDiscordSettings())
	if !cfg.Ready() {
		t.Fatalf("config should be ready: %+v", cfg)
	}
	if cfg.RedirectURI() != "https://guildbank.example.com/account/discord/callback" {
		t.Fatalf("redirect URI = %q", cfg.RedirectURI())
	}

	trailing := ParseDiscordConfig(map[string]string{
		"DiscordEnabled": "true", "DiscordClientId": "1", "DiscordClientSecret": "s",
		"PublicBaseUrl": "https://example.test/",
	})
	if trailing.RedirectURI() != "https://example.test/account/discord/callback" {
		t.Fatalf("trailing slash redirect = %q", trailing.RedirectURI())
	}

	disabled := ParseDiscordConfig(map[string]string{"DiscordEnabled": "false"})
	if disabled.Ready() {
		t.Fatal("a disabled configuration must not be ready")
	}
	if ParseDiscordConfig(map[string]string{"DiscordEnabled": "yes", "DiscordClientId": "1"}).Enabled {
		t.Fatal("an unparseable boolean must fail closed")
	}
	missingSecret := ParseDiscordConfig(map[string]string{
		"DiscordEnabled": "true", "DiscordClientId": "1", "PublicBaseUrl": "https://example.test"})
	if missingSecret.Ready() {
		t.Fatal("a missing client secret must not be ready")
	}
}

func TestAuthorizeURL(t *testing.T) {
	svc, _, _, _ := newDiscordFixture(enabledDiscordSettings())
	cfg, err := svc.Config(context.Background())
	if err != nil {
		t.Fatalf("Config: %v", err)
	}
	got := svc.AuthorizeURL(cfg, "state-value")
	for _, want := range []string{
		"https://discord.com/oauth2/authorize?",
		"client_id=123456789012345678",
		"response_type=code",
		"scope=identify",
		"state=state-value",
		"redirect_uri=https%3A%2F%2Fguildbank.example.com%2Faccount%2Fdiscord%2Fcallback",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("authorize URL %q missing %q", got, want)
		}
	}
	if strings.Contains(got, "email") {
		t.Fatalf("authorize URL requests more than identify: %s", got)
	}
}

func TestLinkStoresIdentityAndAudits(t *testing.T) {
	svc, store, api, sink := newDiscordFixture(enabledDiscordSettings())
	identity, err := svc.Link(context.Background(), discordUser(), "the-code", "req-1", "203.0.113.5")
	if err != nil {
		t.Fatalf("Link: %v", err)
	}
	if !identity.Linked() || identity.DiscordUserName != "Keldor" || identity.UserID != 2 {
		t.Fatalf("identity = %+v", identity)
	}
	if identity.LinkedAt == nil {
		t.Fatal("LinkedAt was not set")
	}
	if store.identities[2].DiscordUserID != "123456789012345678" {
		t.Fatalf("store = %+v", store.identities[2])
	}
	if api.exchangeArgs[2] != "https://guildbank.example.com/account/discord/callback" {
		t.Fatalf("redirect URI sent = %q", api.exchangeArgs[2])
	}
	if len(sink.entries) != 1 {
		t.Fatalf("audit entries = %d, want 1", len(sink.entries))
	}
	entry := sink.entries[0]
	if entry.EventType != "DISCORD_LINKED" || entry.TableName != "Users" || entry.UserName != "Keeper" {
		t.Fatalf("audit = %+v", entry)
	}
	values := string(entry.NewValues)
	if !strings.Contains(values, "123456789012345678") || !strings.Contains(values, "Keldor") {
		t.Fatalf("audit values = %s", values)
	}
	if strings.Contains(values, "access-token") {
		t.Fatalf("audit leaked the provider token: %s", values)
	}
	if entry.CorrelationID != "req-1" || entry.SourceIP != "203.0.113.5" {
		t.Fatalf("audit correlation = %+v", entry)
	}
}

func TestLinkRecordsGuildVerification(t *testing.T) {
	values := enabledDiscordSettings()
	values["DiscordGuildId"] = "987654321098765432"
	svc, _, api, sink := newDiscordFixture(values)
	api.guildMember = true
	if _, err := svc.Link(context.Background(), discordUser(), "code", "", ""); err != nil {
		t.Fatalf("Link: %v", err)
	}
	if api.guildChecks != 1 {
		t.Fatalf("guild checks = %d, want 1", api.guildChecks)
	}
	if !strings.Contains(string(sink.entries[0].NewValues), `"guildVerified":"true"`) {
		t.Fatalf("audit = %s", sink.entries[0].NewValues)
	}

	// A failed guild check links anyway and records the failure as unverified.
	svc, _, api, sink = newDiscordFixture(values)
	api.guildErr = errors.New("discord unavailable")
	if _, err := svc.Link(context.Background(), discordUser(), "code", "", ""); err != nil {
		t.Fatalf("Link with a failing guild check: %v", err)
	}
	if !strings.Contains(string(sink.entries[0].NewValues), `"guildVerified":"false"`) {
		t.Fatalf("audit = %s", sink.entries[0].NewValues)
	}
}

func TestLinkRefusesIdentityOwnedByAnotherAccount(t *testing.T) {
	svc, store, _, _ := newDiscordFixture(enabledDiscordSettings())
	owner := int64(7)
	store.identities[owner] = domain.DiscordIdentity{UserID: owner, DiscordUserID: "123456789012345678"}
	if _, err := svc.Link(context.Background(), discordUser(), "code", "", ""); !errors.Is(err, domain.ErrAlreadyExists) {
		t.Fatalf("Link = %v, want ErrAlreadyExists", err)
	}
	if store.setCalls != 0 {
		t.Fatalf("a refused link wrote %d identities", store.setCalls)
	}
}

func TestLinkProviderFailureIsUpstreamAndWritesNothing(t *testing.T) {
	cases := map[string]func(*stubDiscordAPI){
		"exchange": func(a *stubDiscordAPI) { a.exchangeErr = errors.New("boom") },
		"profile":  func(a *stubDiscordAPI) { a.profileErr = errors.New("boom") },
		"empty id": func(a *stubDiscordAPI) { a.profile = domain.DiscordProfile{UserName: "x"} },
	}
	for name, prepare := range cases {
		svc, store, api, sink := newDiscordFixture(enabledDiscordSettings())
		prepare(api)
		if _, err := svc.Link(context.Background(), discordUser(), "code", "", ""); !errors.Is(err, domain.ErrUpstream) {
			t.Fatalf("%s: Link = %v, want ErrUpstream", name, err)
		}
		if store.setCalls != 0 || len(sink.entries) != 0 {
			t.Fatalf("%s: wrote %d identities and %d audit rows", name, store.setCalls, len(sink.entries))
		}
	}
}

func TestLinkRequiresEnabledConfiguration(t *testing.T) {
	svc, _, api, _ := newDiscordFixture(map[string]string{"DiscordEnabled": "false"})
	if _, err := svc.Link(context.Background(), discordUser(), "code", "", ""); !errors.Is(err, domain.ErrUnavailable) {
		t.Fatalf("Link = %v, want ErrUnavailable", err)
	}
	if len(api.exchangeArgs) != 0 {
		t.Fatal("a disabled configuration called the provider")
	}
}

func TestUnlinkClearsAndAudits(t *testing.T) {
	svc, store, _, sink := newDiscordFixture(enabledDiscordSettings())
	if _, err := svc.Link(context.Background(), discordUser(), "code", "", ""); err != nil {
		t.Fatalf("Link: %v", err)
	}
	if err := svc.Unlink(context.Background(), discordUser(), "req-2", ""); err != nil {
		t.Fatalf("Unlink: %v", err)
	}
	if store.clearCalls != 1 || len(store.identities) != 0 {
		t.Fatalf("clear calls = %d, identities = %v", store.clearCalls, store.identities)
	}
	entry := sink.entries[len(sink.entries)-1]
	if entry.EventType != "DISCORD_UNLINKED" || !strings.Contains(string(entry.OldValues), "Keldor") {
		t.Fatalf("audit = %+v (%s)", entry, entry.OldValues)
	}
	if err := svc.Unlink(context.Background(), discordUser(), "", ""); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("second Unlink = %v, want ErrNotFound", err)
	}
}

func TestIdentityReportsLinkedState(t *testing.T) {
	svc, _, _, _ := newDiscordFixture(enabledDiscordSettings())
	identity, err := svc.Identity(context.Background(), 2)
	if err != nil || identity.Linked() {
		t.Fatalf("Identity = %+v, %v; want unlinked", identity, err)
	}
	if _, err := svc.Identity(context.Background(), 0); !errors.Is(err, domain.ErrUnauthorized) {
		t.Fatalf("Identity(0) = %v, want ErrUnauthorized", err)
	}
	unready := NewDiscordService(nil, nil, nil, nil, nil, nil)
	if _, err := unready.Identity(context.Background(), 2); !errors.Is(err, domain.ErrUnavailable) {
		t.Fatalf("unready Identity = %v, want ErrUnavailable", err)
	}
}

func newDiscordLoginFixture(values map[string]string, user domain.User) (*DiscordService, *fakeDiscordStore,
	*stubDiscordAPI, *fakeAuditSink, *fakeSessionIssuer) {
	store := newFakeDiscordStore()
	store.identities[user.ID] = domain.DiscordIdentity{UserID: user.ID,
		DiscordUserID: "123456789012345678", DiscordUserName: "Keldor"}
	store.users[user.ID] = user
	api := &stubDiscordAPI{
		profile: domain.DiscordProfile{ID: "123456789012345678", UserName: "keldor", GlobalName: "Keldor"},
		token:   domain.DiscordToken{AccessToken: "access-token", TokenType: "Bearer", ExpiresIn: 3600},
	}
	sink := &fakeAuditSink{}
	issuer := &fakeSessionIssuer{}
	svc := NewDiscordService(fakeDiscordSettings{values: values}, api, store, issuer, sink, nil)
	return svc, store, api, sink, issuer
}

func TestLoginIssuesSessionForLinkedAccount(t *testing.T) {
	svc, _, api, sink, issuer := newDiscordLoginFixture(enabledDiscordSettings(), discordUser())
	result, err := svc.Login(context.Background(), "the-code", "203.0.113.9", "test-agent", "req-9")
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	if result.User.ID != 2 || result.Token == "" {
		t.Fatalf("result = %+v", result)
	}
	if len(issuer.issued) != 1 || issuer.issued[0].ID != 2 {
		t.Fatalf("issued = %+v", issuer.issued)
	}
	if len(api.exchangeArgs) != 4 || api.exchangeArgs[2] !=
		"https://guildbank.example.com/auth/discord/callback" {
		t.Fatalf("redirect URI used = %+v", api.exchangeArgs)
	}
	if len(sink.entries) != 1 {
		t.Fatalf("audit entries = %d, want 1", len(sink.entries))
	}
	entry := sink.entries[0]
	if entry.EventType != "DISCORD_LOGIN" || entry.TableName != "Users" ||
		entry.UserName != "Keeper" || entry.SourceIP != "203.0.113.9" {
		t.Fatalf("audit = %+v", entry)
	}
	values := string(entry.NewValues)
	if !strings.Contains(values, `"method":"discord"`) || !strings.Contains(values, "123456789012345678") {
		t.Fatalf("audit values = %s", values)
	}
	if strings.Contains(values, "access-token") || strings.Contains(values, "the-code") {
		t.Fatalf("audit leaked a credential: %s", values)
	}
}

func TestLoginRefusesUnlinkedDiscordUser(t *testing.T) {
	svc, store, _, sink, issuer := newDiscordLoginFixture(enabledDiscordSettings(), discordUser())
	store.identities = map[int64]domain.DiscordIdentity{}

	if _, err := svc.Login(context.Background(), "the-code", "203.0.113.9", "", ""); !errors.Is(err, domain.ErrUnauthorized) {
		t.Fatalf("Login = %v, want ErrUnauthorized", err)
	}
	if len(issuer.issued) != 0 || len(sink.entries) != 0 {
		t.Fatalf("an unlinked sign-in issued %d sessions and %d audit rows",
			len(issuer.issued), len(sink.entries))
	}
	if len(store.identities) != 0 || store.setCalls != 0 || store.clearCalls != 0 {
		t.Fatal("an unlinked sign-in modified identity state")
	}
}

func TestLoginRefusesDisabledAccount(t *testing.T) {
	disabled := discordUser()
	disabled.Disabled = true
	svc, _, _, sink, issuer := newDiscordLoginFixture(enabledDiscordSettings(), disabled)

	_, err := svc.Login(context.Background(), "the-code", "203.0.113.9", "", "")
	var state *domain.AccountStateError
	if !errors.As(err, &state) {
		t.Fatalf("Login = %v, want AccountStateError", err)
	}
	// The reason travels with a typed error, but callers that only know
	// "not allowed" must keep working unchanged.
	if !errors.Is(err, domain.ErrUnauthorized) {
		t.Fatalf("Login = %v, want it to wrap ErrUnauthorized", err)
	}
	if !state.Disabled || state.Status != domain.AccountStatusApproved {
		t.Fatalf("state = %+v, want disabled/approved", state)
	}
	if len(issuer.issued) != 0 || len(sink.entries) != 0 {
		t.Fatal("a disabled account received a session or an audit row")
	}
}

func TestLoginRefusesPendingAccount(t *testing.T) {
	pending := discordUser()
	pending.AccountStatus = string(domain.AccountStatusPendingApproval)
	svc, _, _, sink, issuer := newDiscordLoginFixture(enabledDiscordSettings(), pending)

	_, err := svc.Login(context.Background(), "the-code", "203.0.113.9", "", "")
	var state *domain.AccountStateError
	if !errors.As(err, &state) {
		t.Fatalf("Login = %v, want AccountStateError", err)
	}
	if !errors.Is(err, domain.ErrUnauthorized) {
		t.Fatalf("Login = %v, want it to wrap ErrUnauthorized", err)
	}
	if state.Disabled || state.Status != domain.AccountStatusPendingApproval {
		t.Fatalf("state = %+v, want pending/approved", state)
	}
	if len(issuer.issued) != 0 || len(sink.entries) != 0 {
		t.Fatal("a pending account received a session or an audit row")
	}

	// The refusal is still a failure for the limiter, so a pending account
	// cannot drive unbounded provider calls.
	fresh, _, _, _, _ := newDiscordLoginFixture(enabledDiscordSettings(), pending)
	var lastErr error
	for i := 0; i < 10; i++ {
		if _, lastErr = fresh.Login(context.Background(), "the-code", "203.0.113.9", "", ""); !errors.Is(lastErr, domain.ErrUnauthorized) {
			t.Fatalf("attempt %d = %v, want ErrUnauthorized", i, lastErr)
		}
	}
	_, err = fresh.Login(context.Background(), "the-code", "203.0.113.9", "", "")
	var rate *domain.RateLimitError
	if !errors.As(err, &rate) {
		t.Fatalf("login past the limit = %v, want RateLimitError", err)
	}
}

func TestLoginRequiresEnabledConfigurationAndIssuer(t *testing.T) {
	svc, _, api, _, _ := newDiscordLoginFixture(map[string]string{"DiscordEnabled": "false"}, discordUser())
	if _, err := svc.Login(context.Background(), "code", "ip", "", ""); !errors.Is(err, domain.ErrUnavailable) {
		t.Fatalf("disabled Login = %v, want ErrUnavailable", err)
	}
	if len(api.exchangeArgs) != 0 {
		t.Fatal("a disabled configuration called the provider")
	}

	noIssuer := NewDiscordService(fakeDiscordSettings{values: enabledDiscordSettings()},
		&stubDiscordAPI{}, newFakeDiscordStore(), nil, nil, nil)
	if _, err := noIssuer.Login(context.Background(), "code", "ip", "", ""); !errors.Is(err, domain.ErrUnavailable) {
		t.Fatalf("Login without an issuer = %v, want ErrUnavailable", err)
	}
}

func TestLoginProviderFailureWritesNothing(t *testing.T) {
	svc, _, api, sink, issuer := newDiscordLoginFixture(enabledDiscordSettings(), discordUser())
	api.exchangeErr = errors.New("boom")

	if _, err := svc.Login(context.Background(), "code", "ip", "", ""); !errors.Is(err, domain.ErrUpstream) {
		t.Fatalf("Login = %v, want ErrUpstream", err)
	}
	if len(issuer.issued) != 0 || len(sink.entries) != 0 {
		t.Fatal("a provider failure issued a session or an audit row")
	}
}

func TestLoginRateLimitsRepeatedFailures(t *testing.T) {
	svc, store, _, _, _ := newDiscordLoginFixture(enabledDiscordSettings(), discordUser())
	store.identities = map[int64]domain.DiscordIdentity{}

	var lastErr error
	for i := 0; i < 10; i++ {
		_, lastErr = svc.Login(context.Background(), "code", "203.0.113.9", "", "")
	}
	if !errors.Is(lastErr, domain.ErrUnauthorized) {
		t.Fatalf("tenth failure = %v, want ErrUnauthorized", lastErr)
	}
	_, err := svc.Login(context.Background(), "code", "203.0.113.9", "", "")
	var rate *domain.RateLimitError
	if !errors.As(err, &rate) {
		t.Fatalf("eleventh attempt = %v, want RateLimitError", err)
	}

	// A different client address is unaffected.
	if _, err := svc.Login(context.Background(), "code", "198.51.100.4", "", ""); !errors.Is(err, domain.ErrUnauthorized) {
		t.Fatalf("other address = %v, want ErrUnauthorized", err)
	}
}

func newDiscordFixture(values map[string]string) (*DiscordService, *fakeDiscordStore, *stubDiscordAPI, *fakeAuditSink) {
	store := newFakeDiscordStore()
	api := &stubDiscordAPI{
		profile: domain.DiscordProfile{ID: "123456789012345678", UserName: "keldor", GlobalName: "Keldor"},
		token:   domain.DiscordToken{AccessToken: "access-token", TokenType: "Bearer", ExpiresIn: 3600},
	}
	sink := &fakeAuditSink{}
	svc := NewDiscordService(fakeDiscordSettings{values: values}, api, store, nil, sink, nil)
	return svc, store, api, sink
}
