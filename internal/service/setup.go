package service

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/Panazule/guildbank/internal/domain"
)

// SetupStatusReader is the D-02 probe.
type SetupStatusReader interface {
	SetupStatus(ctx context.Context) (domain.SetupState, error)
}

// SetupCommitter performs the atomic administrator + settings write.
type SetupCommitter interface {
	CompleteSetup(ctx context.Context, data domain.SetupData) (domain.User, error)
}

// DatabaseConfig mirrors the wizard's MariaDB fields without importing the
// repository layer.
type DatabaseConfig struct {
	Host     string
	Port     int
	Database string
	Username string
	Password string
}

// DatabaseTester validates a connection without persisting anything.
type DatabaseTester interface {
	TestDatabase(ctx context.Context, cfg DatabaseConfig) error
}

// SetupInput is the complete first-run wizard payload.
type SetupInput struct {
	UserName          string `json:"userName" form:"UserName"`
	Password          string `json:"password" form:"Password"`
	ConfirmPassword   string `json:"confirmPassword" form:"ConfirmPassword"`
	GuildName         string `json:"guildName" form:"GuildName"`
	GuildOpenDays     string `json:"guildOpenDays" form:"GuildOpenDays"`
	BazaarDays        string `json:"bazaarDays" form:"BazaarDays"`
	EQResourceBaseURL string `json:"eqResourceBaseUrl" form:"EQResourceBaseUrl"`
	MariaDBHost       string `json:"mariaDbHost" form:"MariaDbHost"`
	MariaDBPort       string `json:"mariaDbPort" form:"MariaDbPort"`
	MariaDBDatabase   string `json:"mariaDbDatabase" form:"MariaDbDatabase"`
	MariaDBUsername   string `json:"mariaDbUsername" form:"MariaDbUsername"`
	MariaDBPassword   string `json:"mariaDbPassword" form:"MariaDbPassword"`
}

// SetupService validates and commits setup, and tests the guild store without
// touching it. No partial administrator is ever written (DC-24).
type SetupService struct {
	status SetupStatusReader
	store  SetupCommitter
	tester DatabaseTester
	now    func() time.Time
}

// NewSetupService builds the setup service.
func NewSetupService(status SetupStatusReader, store SetupCommitter, tester DatabaseTester) *SetupService {
	return &SetupService{status: status, store: store, tester: tester, now: time.Now}
}

// Status exposes the D-02 probe to the HTTP setup gate.
func (s *SetupService) Status(ctx context.Context) (domain.SetupState, error) {
	return s.status.SetupStatus(ctx)
}

// DefaultSetupInput contains the documented defaults for the wizard form.
func (s *SetupService) DefaultSetupInput() SetupInput {
	return SetupInput{
		GuildName:         "Aeternus",
		GuildOpenDays:     "14",
		BazaarDays:        "30",
		EQResourceBaseURL: "https://items.eqresource.com/items.php?id=",
		MariaDBPort:       "3306",
		MariaDBDatabase:   "GuildBank",
	}
}

// ValidateAdmin checks Step 1 fields before the wizard advances. Nothing is
// persisted until Complete.
func (s *SetupService) ValidateAdmin(in SetupInput) error {
	fields := map[string]string{}
	validateAdminFields(in, fields)
	return settingsValidationError(fields)
}

// ValidateGuild checks Step 2 fields before the wizard advances.
func (s *SetupService) ValidateGuild(in SetupInput) error {
	fields := map[string]string{}
	validateGuildFields(in, fields)
	return settingsValidationError(fields)
}

// ValidateDatabase checks Step 3 fields before a connection test.
func (s *SetupService) ValidateDatabase(in SetupInput) error {
	fields := map[string]string{}
	validateDatabaseFields(in, fields)
	return settingsValidationError(fields)
}

// TestDatabase validates and tests the connection. It performs no writes.
func (s *SetupService) TestDatabase(ctx context.Context, in SetupInput) error {
	if err := s.ValidateDatabase(in); err != nil {
		return err
	}
	if s.tester == nil {
		return fmt.Errorf("%w: no database tester configured", domain.ErrUnavailable)
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := s.tester.TestDatabase(ctx, databaseConfig(in)); err != nil {
		return fmt.Errorf("%w: %v", domain.ErrUnavailable, err)
	}
	return nil
}

// Complete validates every step, proves the guild store connection, hashes the
// password, and commits the administrator and settings together. A retry after
// a crash therefore sees either no administrator or a complete setup.
func (s *SetupService) Complete(ctx context.Context, in SetupInput) (domain.User, error) {
	state, err := s.status.SetupStatus(ctx)
	if err != nil {
		return domain.User{}, fmt.Errorf("setup status: %w", err)
	}
	if state == domain.SetupComplete {
		return domain.User{}, domain.ErrSetupClosed
	}
	if state != domain.SetupPending {
		return domain.User{}, errors.New("setup state is unknown")
	}

	fields := map[string]string{}
	validateAdminFields(in, fields)
	validateGuildFields(in, fields)
	validateDatabaseFields(in, fields)
	if len(fields) > 0 {
		return domain.User{}, &domain.ValidationError{Fields: fields}
	}
	if err := s.TestDatabase(ctx, in); err != nil {
		return domain.User{}, err
	}

	hash, err := HashPassword(in.Password)
	if err != nil {
		return domain.User{}, err
	}
	now := s.now().UTC()
	admin := domain.User{
		UserName:     strings.TrimSpace(in.UserName),
		PasswordHash: hash,
		AccessLevel:  domain.AccessAdmin,
		LastLogin:    &now,
		ModAt:        now,
		CreatedAt:    now,
		ModBy:        "setup",
	}

	settings := []domain.SettingInput{
		setupSetting("GuildName", strings.TrimSpace(in.GuildName)),
		setupSetting("GuildOpenDays", strings.TrimSpace(in.GuildOpenDays)),
		setupSetting("BazaarDays", strings.TrimSpace(in.BazaarDays)),
		setupSetting("EQResourceBaseUrl", strings.TrimSpace(in.EQResourceBaseURL)),
		setupSetting("MariaDbHost", strings.TrimSpace(in.MariaDBHost)),
		setupSetting("MariaDbPort", strings.TrimSpace(in.MariaDBPort)),
		setupSetting("MariaDbDatabase", strings.TrimSpace(in.MariaDBDatabase)),
		setupSetting("MariaDbUsername", strings.TrimSpace(in.MariaDBUsername)),
		setupSetting("MariaDbPassword", in.MariaDBPassword),
		{Key: "SetupComplete", Value: "1", Type: domain.SettingBoolean},
	}
	user, err := s.store.CompleteSetup(ctx, domain.SetupData{Admin: admin, Settings: settings})
	if err != nil {
		if errors.Is(err, domain.ErrConflict) {
			return domain.User{}, domain.ErrSetupClosed
		}
		return domain.User{}, err
	}
	return user, nil
}

func setupSetting(key, value string) domain.SettingInput {
	def, _ := Definition(key)
	return domain.SettingInput{Key: key, Value: value, Type: def.Type, Secret: def.Secret}
}

func validateAdminFields(in SetupInput, fields map[string]string) {
	username := strings.TrimSpace(in.UserName)
	if msg := validateUsername(username); msg != "" {
		settingError("UserName", msg, fields)
	}
	if msg := passwordPolicyError(username, in.Password); msg != "" {
		settingError("Password", msg, fields)
	}
	if in.ConfirmPassword != in.Password {
		settingError("ConfirmPassword", "must match the password", fields)
	}
}

func validateGuildFields(in SetupInput, fields map[string]string) {
	for key, value := range map[string]string{
		"GuildName":         strings.TrimSpace(in.GuildName),
		"GuildOpenDays":     strings.TrimSpace(in.GuildOpenDays),
		"BazaarDays":        strings.TrimSpace(in.BazaarDays),
		"EQResourceBaseUrl": strings.TrimSpace(in.EQResourceBaseURL),
	} {
		def, _ := Definition(key)
		if msg := validateSettingValue(def, value); msg != "" {
			settingError(key, msg, fields)
		}
	}
}

func validateDatabaseFields(in SetupInput, fields map[string]string) {
	for key, value := range map[string]string{
		"MariaDbHost":     strings.TrimSpace(in.MariaDBHost),
		"MariaDbPort":     strings.TrimSpace(in.MariaDBPort),
		"MariaDbDatabase": strings.TrimSpace(in.MariaDBDatabase),
		"MariaDbUsername": strings.TrimSpace(in.MariaDBUsername),
		"MariaDbPassword": in.MariaDBPassword,
	} {
		def, _ := Definition(key)
		if msg := validateSettingValue(def, value); msg != "" {
			settingError(key, msg, fields)
		}
	}
}

func databaseConfig(in SetupInput) DatabaseConfig {
	port, _ := strconv.Atoi(strings.TrimSpace(in.MariaDBPort))
	return DatabaseConfig{
		Host:     strings.TrimSpace(in.MariaDBHost),
		Port:     port,
		Database: strings.TrimSpace(in.MariaDBDatabase),
		Username: strings.TrimSpace(in.MariaDBUsername),
		Password: in.MariaDBPassword,
	}
}
