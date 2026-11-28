package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/Panazule/guildbank/internal/domain"
)

func testHash(password string) string {
	h, _ := bcrypt.GenerateFromPassword([]byte(password), bcrypt.MinCost)
	return string(h)
}

func TestPasswordHashRoundTripAndPolicy(t *testing.T) {
	hash, err := HashPassword("a long enough password")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	if hash == "a long enough password" || !strings.HasPrefix(hash, "$2") {
		t.Fatalf("hash = %q", hash)
	}
	if !VerifyPassword(hash, "a long enough password") {
		t.Fatal("correct password did not verify")
	}
	if VerifyPassword(hash, "wrong password") {
		t.Fatal("wrong password verified")
	}
	for _, bad := range []string{"short", strings.Repeat("x", 73)} {
		if _, err := HashPassword(bad); !errors.Is(err, domain.ErrValidation) {
			t.Fatalf("HashPassword(%d bytes) err = %v, want validation", len(bad), err)
		}
	}
	if _, err := HashPassword("AdminAdminAdmin"); err != nil {
		t.Fatalf("valid password rejected: %v", err)
	}
	if passwordPolicyError("Admin", "Admin") == "" {
		t.Fatal("password equal to username was accepted")
	}
}

type fakeUsers struct {
	users   map[string]domain.User
	updated time.Time
	err     error
}

func (f *fakeUsers) GetUserByName(_ context.Context, name string) (domain.User, error) {
	if f.err != nil {
		return domain.User{}, f.err
	}
	for key, u := range f.users {
		if strings.EqualFold(key, name) {
			return u, nil
		}
	}
	return domain.User{}, domain.ErrNotFound
}

func (f *fakeUsers) UpdateLastLogin(_ context.Context, id int64, at time.Time) error {
	f.updated = at
	for key, u := range f.users {
		if u.ID == id {
			u.LastLogin = &at
			f.users[key] = u
			return nil
		}
	}
	return domain.ErrNotFound
}

type fakeSessions struct {
	byHash  map[string]domain.Session
	created []domain.Session
	deleted []string
	touched []string
}

func newFakeSessions() *fakeSessions {
	return &fakeSessions{byHash: make(map[string]domain.Session)}
}

func (f *fakeSessions) CreateSession(_ context.Context, s domain.Session) error {
	f.byHash[s.TokenHash] = s
	f.created = append(f.created, s)
	return nil
}

func (f *fakeSessions) FindSessionByTokenHash(_ context.Context, hash string) (domain.Session, error) {
	s, ok := f.byHash[hash]
	if !ok {
		return domain.Session{}, domain.ErrNotFound
	}
	return s, nil
}

func (f *fakeSessions) TouchSession(_ context.Context, id string, at, expires time.Time) error {
	f.touched = append(f.touched, id)
	for hash, s := range f.byHash {
		if s.ID == id {
			s.LastAccessed, s.ExpiresAt = at, expires
			f.byHash[hash] = s
		}
	}
	return nil
}

func (f *fakeSessions) DeleteSession(_ context.Context, hash string) error {
	delete(f.byHash, hash)
	f.deleted = append(f.deleted, hash)
	return nil
}

func (f *fakeSessions) DeleteSessionsForUser(_ context.Context, userID int64) (int64, error) {
	var n int64
	for hash, s := range f.byHash {
		if s.UserID == userID {
			delete(f.byHash, hash)
			n++
		}
	}
	return n, nil
}

func (f *fakeSessions) DeleteExpiredSessions(_ context.Context, now time.Time) (int64, error) {
	var n int64
	for hash, s := range f.byHash {
		if !now.Before(s.ExpiresAt) || !now.Before(s.AbsoluteExpiresAt) {
			delete(f.byHash, hash)
			n++
		}
	}
	return n, nil
}

type fixedPolicy struct {
	policy domain.SessionPolicy
	err    error
}

func (f fixedPolicy) SessionPolicy(context.Context) (domain.SessionPolicy, error) {
	return f.policy, f.err
}

func authFixture(t *testing.T) (*AuthService, *fakeUsers, *fakeSessions, func(time.Duration)) {
	t.Helper()
	users := &fakeUsers{users: map[string]domain.User{
		"admin": {ID: 1, UserName: "admin", PasswordHash: testHash("a long enough password"),
			AccessLevel: domain.AccessAdmin, HasPassword: true},
	}}
	sessions := newFakeSessions()
	auth := NewAuthService(users, sessions, fixedPolicy{policy: domain.SessionPolicy{Idle: 30 * time.Minute, Absolute: 8 * time.Hour}})
	now := time.Date(2026, 11, 21, 12, 0, 0, 0, time.UTC)
	auth.now = func() time.Time { return now }
	advance := func(d time.Duration) { now = now.Add(d) }
	return auth, users, sessions, advance
}

func TestLoginCreatesHashedSessionAndAuthenticates(t *testing.T) {
	auth, _, sessions, _ := authFixture(t)
	result, err := auth.Login(context.Background(), LoginInput{
		UserName: "ADMIN", Password: "a long enough password", ClientIP: "10.0.0.1", UserAgent: "test",
	})
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	if result.Token == "" || result.User.UserName != "admin" {
		t.Fatalf("result = %+v", result)
	}
	if len(sessions.created) != 1 {
		t.Fatalf("sessions created = %d", len(sessions.created))
	}
	stored := sessions.created[0]
	if stored.TokenHash == result.Token || stored.ID == result.Token {
		t.Fatal("raw token was stored in the session row")
	}
	if stored.TokenHash != hashToken(result.Token) || stored.ID == stored.TokenHash {
		t.Fatalf("stored = %+v", stored)
	}
	if !stored.ExpiresAt.Equal(result.ExpiresAt) || !stored.AbsoluteExpiresAt.Equal(result.AbsoluteExpiresAt) {
		t.Fatal("expiry mismatch")
	}

	principal, err := auth.Authenticate(context.Background(), result.Token)
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	if principal.User.ID != 1 || principal.User.PasswordHash == "" {
		t.Fatalf("principal = %+v", principal.User)
	}
	if _, err := auth.Authenticate(context.Background(), result.Token+"x"); !errors.Is(err, domain.ErrUnauthorized) {
		t.Fatalf("invalid token err = %v", err)
	}
}

func TestLoginFailuresAreGenericAndRateLimited(t *testing.T) {
	auth, _, _, _ := authFixture(t)
	ctx := context.Background()
	wrong, err := auth.Login(ctx, LoginInput{UserName: "admin", Password: "wrong", ClientIP: "10.0.0.2"})
	if !errors.Is(err, domain.ErrUnauthorized) || wrong.Token != "" {
		t.Fatalf("wrong password = %+v, %v", wrong, err)
	}
	unknown, err := auth.Login(ctx, LoginInput{UserName: "ghost", Password: "wrong", ClientIP: "10.0.0.3"})
	if !errors.Is(err, domain.ErrUnauthorized) || unknown.Token != "" {
		t.Fatalf("unknown user = %+v, %v", unknown, err)
	}

	for i := 0; i < 4; i++ {
		_, err = auth.Login(ctx, LoginInput{UserName: "target", Password: "bad", ClientIP: "10.0.0.4"})
		if !errors.Is(err, domain.ErrUnauthorized) {
			t.Fatalf("attempt %d err = %v", i+1, err)
		}
	}
	_, err = auth.Login(ctx, LoginInput{UserName: "target", Password: "bad", ClientIP: "10.0.0.4"})
	var rate *domain.RateLimitError
	if !errors.As(err, &rate) || rate.RetryAfter != 15*time.Minute {
		t.Fatalf("fifth attempt err = %v", err)
	}
}

func TestSessionExpiryDisableAndSlidingTouch(t *testing.T) {
	auth, users, sessions, advance := authFixture(t)
	ctx := context.Background()
	result, err := auth.Login(ctx, LoginInput{UserName: "admin", Password: "a long enough password", ClientIP: "10.0.0.5"})
	if err != nil {
		t.Fatal(err)
	}

	advance(2 * time.Minute)
	if _, err := auth.Authenticate(ctx, result.Token); err != nil {
		t.Fatalf("Authenticate after 2m: %v", err)
	}
	if len(sessions.touched) != 1 {
		t.Fatalf("touch count = %d, want 1", len(sessions.touched))
	}

	advance(31 * time.Minute)
	if _, err := auth.Authenticate(ctx, result.Token); !errors.Is(err, domain.ErrUnauthorized) {
		t.Fatalf("expired session err = %v", err)
	}
	if len(sessions.byHash) != 0 {
		t.Fatal("expired session was not deleted")
	}

	result, err = auth.Login(ctx, LoginInput{UserName: "admin", Password: "a long enough password", ClientIP: "10.0.0.5"})
	if err != nil {
		t.Fatal(err)
	}
	u := users.users["admin"]
	u.Disabled = true
	users.users["admin"] = u
	for hash, s := range sessions.byHash {
		s.User.Disabled = true
		sessions.byHash[hash] = s
	}
	if _, err := auth.Authenticate(ctx, result.Token); !errors.Is(err, domain.ErrUnauthorized) {
		t.Fatalf("disabled session err = %v", err)
	}
	if len(sessions.byHash) != 0 {
		t.Fatal("disabled user's session was not revoked")
	}
}

func TestLogoutAndRevokeAll(t *testing.T) {
	auth, _, sessions, _ := authFixture(t)
	ctx := context.Background()
	first, err := auth.Login(ctx, LoginInput{UserName: "admin", Password: "a long enough password", ClientIP: "10.0.0.6"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := auth.IssueSession(ctx, domain.User{ID: 1, UserName: "admin", AccessLevel: domain.AccessAdmin}, "10.0.0.6", "test")
	if err != nil {
		t.Fatal(err)
	}
	if err := auth.Logout(ctx, first.Token); err != nil {
		t.Fatal(err)
	}
	if _, err := auth.Authenticate(ctx, first.Token); !errors.Is(err, domain.ErrUnauthorized) {
		t.Fatalf("logged-out session err = %v", err)
	}
	n, err := auth.RevokeAll(ctx, 1)
	if err != nil || n != 1 {
		t.Fatalf("RevokeAll = %d, %v", n, err)
	}
	if _, err := auth.Authenticate(ctx, second.Token); !errors.Is(err, domain.ErrUnauthorized) {
		t.Fatalf("revoked session err = %v", err)
	}
	if len(sessions.byHash) != 0 {
		t.Fatal("sessions remain after revoke-all")
	}
}

type fakeSetupStatus struct {
	state domain.SetupState
	err   error
}

func (f fakeSetupStatus) SetupStatus(context.Context) (domain.SetupState, error) {
	return f.state, f.err
}

type fakeSetupCommitter struct {
	data  domain.SetupData
	err   error
	calls int
}

func (f *fakeSetupCommitter) CompleteSetup(_ context.Context, data domain.SetupData) (domain.User, error) {
	f.calls++
	f.data = data
	if f.err != nil {
		return domain.User{}, f.err
	}
	user := data.Admin
	user.ID = 1
	return user, nil
}

type fakeTester struct {
	err   error
	calls int
	cfg   DatabaseConfig
}

func (f *fakeTester) TestDatabase(_ context.Context, cfg DatabaseConfig) error {
	f.calls++
	f.cfg = cfg
	return f.err
}

func validSetupInput() SetupInput {
	return SetupInput{
		UserName: "admin", Password: "a long enough password", ConfirmPassword: "a long enough password",
		GuildName: "Aeternus", GuildOpenDays: "14", BazaarDays: "30",
		EQResourceBaseURL: "https://items.eqresource.com/items.php?id=",
		MariaDBHost:       "db.internal", MariaDBPort: "3306", MariaDBDatabase: "GuildBank",
		MariaDBUsername: "guildbank", MariaDBPassword: "db-password",
	}
}

func TestSetupCompleteHashesAndCommitsAtomically(t *testing.T) {
	status := fakeSetupStatus{state: domain.SetupPending}
	committer := &fakeSetupCommitter{}
	tester := &fakeTester{}
	setup := NewSetupService(status, committer, tester)
	now := time.Date(2026, 11, 21, 12, 0, 0, 0, time.UTC)
	setup.now = func() time.Time { return now }

	user, err := setup.Complete(context.Background(), validSetupInput())
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if tester.calls != 1 || committer.calls != 1 {
		t.Fatalf("tester calls=%d committer calls=%d", tester.calls, committer.calls)
	}
	if tester.cfg.Host != "db.internal" || tester.cfg.Port != 3306 || tester.cfg.Password != "db-password" {
		t.Fatalf("tester cfg = %+v", tester.cfg)
	}
	if user.UserName != "admin" || user.AccessLevel != domain.AccessAdmin || user.LastLogin == nil {
		t.Fatalf("user = %+v", user)
	}
	if !VerifyPassword(committer.data.Admin.PasswordHash, "a long enough password") {
		t.Fatal("committed password is not a verifiable bcrypt hash")
	}
	if committer.data.Admin.PasswordHash == "a long enough password" {
		t.Fatal("plaintext password was committed")
	}
	settings := map[string]string{}
	for _, s := range committer.data.Settings {
		settings[s.Key] = s.Value
	}
	if settings["SetupComplete"] != "1" || settings["GuildName"] != "Aeternus" ||
		settings["MariaDbHost"] != "db.internal" || settings["MariaDbPassword"] != "db-password" {
		t.Fatalf("settings = %+v", settings)
	}
}

func TestSetupValidationAndFailuresCreateNothing(t *testing.T) {
	committer := &fakeSetupCommitter{}
	tester := &fakeTester{}
	setup := NewSetupService(fakeSetupStatus{state: domain.SetupPending}, committer, tester)

	bad := validSetupInput()
	bad.GuildOpenDays = "-1"
	bad.ConfirmPassword = "different"
	if _, err := setup.Complete(context.Background(), bad); !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("bad input err = %v", err)
	}
	if tester.calls != 0 || committer.calls != 0 {
		t.Fatal("invalid setup reached a dependency")
	}

	tester.err = errors.New("connection refused")
	if _, err := setup.Complete(context.Background(), validSetupInput()); !errors.Is(err, domain.ErrUnavailable) {
		t.Fatalf("database failure err = %v", err)
	}
	if committer.calls != 0 {
		t.Fatal("database failure committed the administrator")
	}

	closed := NewSetupService(fakeSetupStatus{state: domain.SetupComplete}, committer, tester)
	if _, err := closed.Complete(context.Background(), validSetupInput()); !errors.Is(err, domain.ErrSetupClosed) {
		t.Fatalf("closed setup err = %v", err)
	}
}

func TestSetupStepValidationAndDatabaseTest(t *testing.T) {
	tester := &fakeTester{}
	setup := NewSetupService(fakeSetupStatus{state: domain.SetupPending}, &fakeSetupCommitter{}, tester)
	in := validSetupInput()
	if err := setup.ValidateAdmin(in); err != nil {
		t.Fatalf("admin validation: %v", err)
	}
	if err := setup.ValidateGuild(in); err != nil {
		t.Fatalf("guild validation: %v", err)
	}
	if err := setup.ValidateDatabase(in); err != nil {
		t.Fatalf("database validation: %v", err)
	}
	if err := setup.TestDatabase(context.Background(), in); err != nil {
		t.Fatalf("TestDatabase: %v", err)
	}
	if tester.calls != 1 {
		t.Fatalf("tester calls = %d", tester.calls)
	}

	in.EQResourceBaseURL = "http://example.com/"
	if err := setup.ValidateGuild(in); !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("bad URL err = %v", err)
	}
	in = validSetupInput()
	in.MariaDBPort = "0"
	if err := setup.ValidateDatabase(in); !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("bad port err = %v", err)
	}
}

type fakeSettingsStore struct {
	rows []domain.Setting
}

func (f *fakeSettingsStore) ListSettings(context.Context) ([]domain.Setting, error) {
	out := make([]domain.Setting, len(f.rows))
	copy(out, f.rows)
	return out, nil
}

func (f *fakeSettingsStore) UpsertSettings(_ context.Context, inputs []domain.SettingInput, modBy string, at time.Time) error {
	for _, in := range inputs {
		found := false
		for i := range f.rows {
			if f.rows[i].Key == in.Key {
				f.rows[i].Value, f.rows[i].Type, f.rows[i].IsSecret, f.rows[i].ModBy, f.rows[i].ModAt = in.Value, in.Type, in.Secret, modBy, at
				found = true
			}
		}
		if !found {
			f.rows = append(f.rows, domain.Setting{Key: in.Key, Value: in.Value, Type: in.Type, IsSecret: in.Secret, ModBy: modBy, ModAt: at})
		}
	}
	return nil
}

func TestSettingsUpdateValidatesAndPreservesBlankSecret(t *testing.T) {
	store := &fakeSettingsStore{rows: []domain.Setting{
		{Key: "GuildName", Value: "Aeternus", Type: domain.SettingString},
		{Key: "GuildOpenDays", Value: "14", Type: domain.SettingInteger},
		{Key: "MariaDbPassword", Value: "secret", Type: domain.SettingSecret, IsSecret: true},
		{Key: "SessionIdleMinutes", Value: "30", Type: domain.SettingInteger},
		{Key: "SessionAbsoluteHours", Value: "8", Type: domain.SettingInteger},
	}}
	settings := NewSettingsService(store)
	if _, err := settings.Update(context.Background(), map[string]string{
		"GuildName": "Renamed", "GuildOpenDays": "-1",
	}, "admin"); !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("invalid update err = %v", err)
	}
	rows, err := settings.Update(context.Background(), map[string]string{
		"GuildName": "Renamed", "GuildOpenDays": "21", "MariaDbPassword": "",
	}, "admin")
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if v, _ := domain.GetSetting(rows, "GuildName"); v != "Renamed" {
		t.Fatalf("GuildName = %q", v)
	}
	if v, _ := domain.GetSetting(rows, "MariaDbPassword"); v != "secret" {
		t.Fatalf("blank secret overwrote stored value: %q", v)
	}
	policy, err := settings.SessionPolicy(context.Background())
	if err != nil || policy.Idle != 30*time.Minute || policy.Absolute != 8*24*time.Hour {
		t.Fatalf("policy = %+v, %v", policy, err)
	}
	if _, err := settings.Update(context.Background(), map[string]string{"Unknown": "x"}, "admin"); !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("unknown key err = %v", err)
	}
}

func settingsStoreWithAbsolute(value string) *fakeSettingsStore {
	return &fakeSettingsStore{rows: []domain.Setting{
		{Key: "SessionIdleMinutes", Value: "30", Type: domain.SettingInteger},
		{Key: sessionAbsoluteKey, Value: value, Type: domain.SettingInteger},
	}}
}

// TestSessionAbsoluteLifetimeIsDays pins the setting's unit: the stored number
// is a number of days, so 1 becomes 24h and the idle timeout is untouched.
func TestSessionAbsoluteLifetimeIsDays(t *testing.T) {
	for _, tc := range []struct {
		value string
		want  time.Duration
	}{
		{"1", 24 * time.Hour},
		{"8", 8 * 24 * time.Hour},
		{"30", 30 * 24 * time.Hour},
		{"730", 730 * 24 * time.Hour},
	} {
		policy, err := NewSettingsService(settingsStoreWithAbsolute(tc.value)).SessionPolicy(context.Background())
		if err != nil {
			t.Fatalf("value %q: SessionPolicy: %v", tc.value, err)
		}
		if policy.Absolute != tc.want {
			t.Fatalf("value %q: Absolute = %v, want %v (days, not hours)", tc.value, policy.Absolute, tc.want)
		}
		if policy.Idle != 30*time.Minute {
			t.Fatalf("value %q: Idle = %v, want 30m", tc.value, policy.Idle)
		}
	}
}

// TestSessionAbsoluteLifetimeFallsBackWhenOutOfRange keeps the old defensive
// behaviour: an unreadable or out-of-range stored value uses the default
// policy instead of a nonsensical duration.
func TestSessionAbsoluteLifetimeFallsBackWhenOutOfRange(t *testing.T) {
	for _, value := range []string{"0", "731", "-1", "abc", ""} {
		policy, err := NewSettingsService(settingsStoreWithAbsolute(value)).SessionPolicy(context.Background())
		if err != nil {
			t.Fatalf("value %q: SessionPolicy: %v", value, err)
		}
		if policy.Absolute != domain.DefaultSessionPolicy.Absolute {
			t.Fatalf("value %q: Absolute = %v, want default %v", value, policy.Absolute, domain.DefaultSessionPolicy.Absolute)
		}
	}
}

// TestSessionExpiryWithDayBasedSetting drives the real SettingsService into
// AuthService: a 2-day absolute lifetime creates a two-day session and the
// absolute cap still ends it even while the session stays active.
func TestSessionExpiryWithDayBasedSetting(t *testing.T) {
	settings := NewSettingsService(&fakeSettingsStore{rows: []domain.Setting{
		{Key: "SessionIdleMinutes", Value: "1440", Type: domain.SettingInteger},
		{Key: sessionAbsoluteKey, Value: "2", Type: domain.SettingInteger},
	}})
	users := &fakeUsers{users: map[string]domain.User{
		"admin": {ID: 1, UserName: "admin", PasswordHash: testHash("a long enough password"),
			AccessLevel: domain.AccessAdmin, HasPassword: true},
	}}
	sessions := newFakeSessions()
	auth := NewAuthService(users, sessions, settings)
	now := time.Date(2026, 11, 21, 12, 0, 0, 0, time.UTC)
	auth.now = func() time.Time { return now }

	ctx := context.Background()
	result, err := auth.Login(ctx, LoginInput{UserName: "admin", Password: "a long enough password"})
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	if want := now.Add(48 * time.Hour); !result.AbsoluteExpiresAt.Equal(want) {
		t.Fatalf("AbsoluteExpiresAt = %v, want %v (2 days)", result.AbsoluteExpiresAt, want)
	}

	now = now.Add(23 * time.Hour)
	if _, err := auth.Authenticate(ctx, result.Token); err != nil {
		t.Fatalf("authenticate at +23h: %v", err)
	}

	// Activity keeps the idle window open, but the refreshed expiry may never
	// pass the day-based absolute cap.
	now = now.Add(23*time.Hour + 55*time.Minute)
	if _, err := auth.Authenticate(ctx, result.Token); err != nil {
		t.Fatalf("authenticate at +46h55m: %v", err)
	}
	stored := sessions.byHash[hashToken(result.Token)]
	if !stored.ExpiresAt.Equal(result.AbsoluteExpiresAt) {
		t.Fatalf("refreshed idle expiry = %v, want the absolute cap %v", stored.ExpiresAt, result.AbsoluteExpiresAt)
	}

	now = now.Add(1*time.Hour + 5*time.Minute)
	if _, err := auth.Authenticate(ctx, result.Token); !errors.Is(err, domain.ErrUnauthorized) {
		t.Fatalf("authenticate at +48h err = %v, want unauthorized", err)
	}
}

// TestSessionAbsoluteLifetimeValidationRange pins the accepted day range.
func TestSessionAbsoluteLifetimeValidationRange(t *testing.T) {
	settings := NewSettingsService(settingsStoreWithAbsolute("8"))
	ctx := context.Background()
	for _, value := range []string{"0", "731", "-1", "1.5", "days", ""} {
		_, err := settings.Update(ctx, map[string]string{sessionAbsoluteKey: value}, "admin")
		var validation *domain.ValidationError
		if !errors.As(err, &validation) {
			t.Fatalf("value %q err = %v, want a validation error", value, err)
		}
		if got := validation.Fields[sessionAbsoluteKey]; got != "must be a whole number from 1 to 730 days" {
			t.Fatalf("value %q message = %q", value, got)
		}
	}
	for _, value := range []string{"1", "730"} {
		rows, err := settings.Update(ctx, map[string]string{sessionAbsoluteKey: value}, "admin")
		if err != nil {
			t.Fatalf("value %q rejected: %v", value, err)
		}
		if got, _ := domain.GetSetting(rows, sessionAbsoluteKey); got != value {
			t.Fatalf("stored value = %q, want %q", got, value)
		}
	}
}
