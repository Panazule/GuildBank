package service

import (
	"context"
	"net"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Panazule/guildbank/internal/domain"
)

// SettingsStore is the narrow persistence surface used by SettingsService.
type SettingsStore interface {
	ListSettings(ctx context.Context) ([]domain.Setting, error)
	UpsertSettings(ctx context.Context, inputs []domain.SettingInput, modBy string, at time.Time) error
}

// SettingDefinition describes an editable setting for validation and UI
// rendering. SetupComplete is intentionally absent: it is telemetry, not a
// user-editable value (D-02).
type SettingDefinition struct {
	Key     string
	Label   string
	Type    domain.SettingType
	Secret  bool
	Default string
	Help    string
}

// SettingDefinitions is the ordered allowlist. Unknown keys are rejected.
var SettingDefinitions = []SettingDefinition{
	{Key: "GuildName", Label: "Guild name", Type: domain.SettingString, Default: "Aeternus", Help: "1–64 characters"},
	{Key: "GuildOpenDays", Label: "Guild open days", Type: domain.SettingInteger, Default: "14", Help: "0–3650 days"},
	{Key: "BazaarDays", Label: "Bazaar days", Type: domain.SettingInteger, Default: "30", Help: "0–3650 days"},
	{Key: "EQResourceBaseUrl", Label: "EQResource base URL", Type: domain.SettingURL, Default: "https://items.eqresource.com/items.php?id=", Help: "Must be the HTTPS EQResource item URL ending in ?id="},
	{Key: "MariaDbHost", Label: "MariaDB host", Type: domain.SettingString, Default: "", Help: "Hostname or IP address"},
	{Key: "MariaDbPort", Label: "MariaDB port", Type: domain.SettingInteger, Default: "3306", Help: "1–65535"},
	{Key: "MariaDbDatabase", Label: "MariaDB database", Type: domain.SettingString, Default: "GuildBank", Help: "Letters, digits and underscores"},
	{Key: "MariaDbUsername", Label: "MariaDB username", Type: domain.SettingString, Default: "", Help: "Dedicated application account"},
	{Key: "MariaDbPassword", Label: "MariaDB password", Type: domain.SettingSecret, Secret: true, Default: "", Help: "Stored in SQLite; blank keeps the existing value"},
	{Key: "SessionIdleMinutes", Label: "Session idle timeout", Type: domain.SettingInteger, Default: "30", Help: "5–1440 minutes"},
	{Key: "SessionAbsoluteHours", Label: "Session absolute lifetime", Type: domain.SettingInteger, Default: "8", Help: "1–168 hours"},
	{Key: "PublicBaseUrl", Label: "Public base URL", Type: domain.SettingURL, Default: "", Help: "Public origin used to build the Discord redirect URI, e.g. https://guildbank.example.com"},
	{Key: "DiscordEnabled", Label: "Discord account linking", Type: domain.SettingBoolean, Default: "false", Help: "true or false; requires the client ID, client secret and public base URL"},
	{Key: "DiscordClientId", Label: "Discord client ID", Type: domain.SettingString, Default: "", Help: "Application ID from the Discord developer portal"},
	{Key: "DiscordClientSecret", Label: "Discord client secret", Type: domain.SettingSecret, Secret: true, Default: "", Help: "Stored in SQLite; blank keeps the existing value"},
	{Key: "DiscordGuildId", Label: "Discord guild ID (optional)", Type: domain.SettingString, Default: "", Help: "When set, guild membership is checked at link time"},
}

var settingByName = func() map[string]SettingDefinition {
	m := make(map[string]SettingDefinition, len(SettingDefinitions))
	for _, d := range SettingDefinitions {
		m[d.Key] = d
	}
	return m
}()

var (
	identifierPattern = regexp.MustCompile(`^[A-Za-z0-9_]{1,64}$`)
	usernamePattern   = regexp.MustCompile(`^[A-Za-z0-9._-]{3,64}$`)
	// discordSnowflakePattern matches Discord's numeric identifiers.
	discordSnowflakePattern = regexp.MustCompile(`^[0-9]{15,25}$`)
)

// SettingsService owns typed reads, validation and persistence.
type SettingsService struct {
	store SettingsStore
	now   func() time.Time
}

// NewSettingsService builds the settings service.
func NewSettingsService(store SettingsStore) *SettingsService {
	return &SettingsService{store: store, now: time.Now}
}

// List returns every setting including secrets. Callers that serialize values
// must redact them.
func (s *SettingsService) List(ctx context.Context) ([]domain.Setting, error) {
	return s.store.ListSettings(ctx)
}

// Update validates and writes every supplied value atomically. A blank secret
// value means "leave unchanged", allowing the UI to avoid round-tripping the
// MariaDB password.
func (s *SettingsService) Update(ctx context.Context, updates map[string]string, actor string) ([]domain.Setting, error) {
	if len(updates) == 0 {
		return s.List(ctx)
	}
	fields := map[string]string{}
	inputs := make([]domain.SettingInput, 0, len(updates))
	for key, value := range updates {
		key = strings.TrimSpace(key)
		def, ok := settingByName[key]
		if !ok {
			fields[key] = "unknown setting"
			continue
		}
		if def.Secret && value == "" {
			continue
		}
		if msg := validateSettingValue(def, value); msg != "" {
			fields[key] = msg
			continue
		}
		inputs = append(inputs, domain.SettingInput{
			Key:    key,
			Value:  value,
			Type:   def.Type,
			Secret: def.Secret,
		})
	}
	if len(fields) > 0 {
		return nil, &domain.ValidationError{Fields: fields}
	}
	if err := s.store.UpsertSettings(ctx, inputs, actor, s.now().UTC()); err != nil {
		return nil, err
	}
	return s.List(ctx)
}

// Values returns settings as a lookup map.
func (s *SettingsService) Values(ctx context.Context) (map[string]string, error) {
	rows, err := s.store.ListSettings(ctx)
	if err != nil {
		return nil, err
	}
	out := make(map[string]string, len(rows))
	for _, row := range rows {
		out[row.Key] = row.Value
	}
	return out, nil
}

// SessionPolicy implements AuthService's policy reader.
func (s *SettingsService) SessionPolicy(ctx context.Context) (domain.SessionPolicy, error) {
	values, err := s.Values(ctx)
	if err != nil {
		return domain.DefaultSessionPolicy, err
	}
	policy := domain.DefaultSessionPolicy
	if n, err := strconv.Atoi(values["SessionIdleMinutes"]); err == nil && n >= 5 && n <= 1440 {
		policy.Idle = time.Duration(n) * time.Minute
	}
	if n, err := strconv.Atoi(values["SessionAbsoluteHours"]); err == nil && n >= 1 && n <= 168 {
		policy.Absolute = time.Duration(n) * time.Hour
	}
	return policy, nil
}

// Definition returns the type declaration for a known setting.
func Definition(key string) (SettingDefinition, bool) {
	def, ok := settingByName[key]
	return def, ok
}

func validateSettingValue(def SettingDefinition, value string) string {
	switch def.Key {
	case "GuildName":
		if strings.TrimSpace(value) == "" || utf8.RuneCountInString(value) > 64 {
			return "must be 1–64 characters"
		}
	case "GuildOpenDays", "BazaarDays":
		n, err := strconv.Atoi(strings.TrimSpace(value))
		if err != nil || n < 0 || n > 3650 {
			return "must be a whole number from 0 to 3650"
		}
	case "EQResourceBaseUrl":
		if !validEQResourceURL(value) {
			return "must be an HTTPS items.eqresource.com URL ending in ?id="
		}
	case "MariaDbHost":
		if !validHostname(value) {
			return "must be a hostname or IP address"
		}
	case "MariaDbPort":
		n, err := strconv.Atoi(strings.TrimSpace(value))
		if err != nil || n < 1 || n > 65535 {
			return "must be a port from 1 to 65535"
		}
	case "MariaDbDatabase":
		if !identifierPattern.MatchString(value) {
			return "must be 1–64 letters, digits or underscores"
		}
	case "MariaDbUsername":
		if strings.TrimSpace(value) == "" || len(value) > 128 {
			return "must be 1–128 characters"
		}
	case "MariaDbPassword":
		if value == "" {
			return "must not be blank"
		}
		if len(value) > 1024 {
			return "must be at most 1024 bytes"
		}
	case "SessionIdleMinutes":
		n, err := strconv.Atoi(strings.TrimSpace(value))
		if err != nil || n < 5 || n > 1440 {
			return "must be a whole number from 5 to 1440"
		}
	case "SessionAbsoluteHours":
		n, err := strconv.Atoi(strings.TrimSpace(value))
		if err != nil || n < 1 || n > 168 {
			return "must be a whole number from 1 to 168"
		}
	case "PublicBaseUrl":
		if value == "" {
			return ""
		}
		if !validPublicBaseURL(value) {
			return "must be an http or https origin without a path, query or fragment"
		}
	case "DiscordEnabled":
		if _, err := strconv.ParseBool(strings.TrimSpace(value)); err != nil {
			return "must be true or false"
		}
	case "DiscordClientId":
		if value == "" {
			return ""
		}
		if !discordSnowflakePattern.MatchString(value) {
			return "must be the 15–25 digit Discord application ID"
		}
	case "DiscordClientSecret":
		if value == "" {
			return "must not be blank"
		}
		if len(value) > 200 {
			return "must be at most 200 bytes"
		}
	case "DiscordGuildId":
		if value == "" {
			return ""
		}
		if !discordSnowflakePattern.MatchString(value) {
			return "must be a 15–25 digit Discord guild ID, or blank"
		}
	default:
		return "unknown setting"
	}
	return ""
}

func validEQResourceURL(value string) bool {
	if !strings.HasSuffix(value, "?id=") {
		return false
	}
	u, err := url.Parse(value)
	if err != nil {
		return false
	}
	return u.Scheme == "https" &&
		strings.EqualFold(u.Host, "items.eqresource.com") &&
		u.Path == "/items.php" &&
		u.RawQuery == "id="
}

// validPublicBaseURL accepts an http(s) origin only: no path, query or
// fragment, because it is concatenated with a fixed callback path.
func validPublicBaseURL(value string) bool {
	u, err := url.Parse(strings.TrimSpace(value))
	if err != nil {
		return false
	}
	if u.Scheme != "https" && u.Scheme != "http" {
		return false
	}
	if u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return false
	}
	return u.Path == "" || u.Path == "/"
}

func validHostname(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > 255 {
		return false
	}
	if net.ParseIP(value) != nil {
		return true
	}
	if strings.ContainsAny(value, " /\\:@[]") {
		return false
	}
	for _, label := range strings.Split(value, ".") {
		if label == "" || len(label) > 63 || strings.HasPrefix(label, "-") || strings.HasSuffix(label, "-") {
			return false
		}
		for _, r := range label {
			if (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') && (r < '0' || r > '9') && r != '-' {
				return false
			}
		}
	}
	return true
}

func validateUsername(value string) string {
	if !usernamePattern.MatchString(value) {
		return "must be 3–64 characters using letters, digits, dot, dash or underscore"
	}
	return ""
}

func settingsValidationError(fields map[string]string) error {
	if len(fields) == 0 {
		return nil
	}
	return &domain.ValidationError{Fields: fields}
}

func settingError(key, message string, fields map[string]string) {
	if _, exists := fields[key]; !exists {
		fields[key] = message
	}
}
