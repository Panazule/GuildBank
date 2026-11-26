package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Panazule/guildbank/internal/domain"
)

// UserAdminStore is the account-administration persistence surface. The guards
// for the last enabled Administrator live in the store, so every caller passes
// through them.
type UserAdminStore interface {
	ListUsers(ctx context.Context) ([]domain.User, error)
	GetUserByID(ctx context.Context, id int64) (domain.User, error)
	CreateUser(ctx context.Context, u domain.User) (domain.User, error)
	SetUserAccessLevel(ctx context.Context, id int64, role domain.Role, at time.Time, actor domain.Actor) (domain.User, error)
	SetUserDisabled(ctx context.Context, id int64, disabled bool, at time.Time, actor domain.Actor) (domain.User, error)
	SetUserAccountStatus(ctx context.Context, id int64, status domain.AccountStatus, at time.Time, actor domain.Actor) (domain.User, error)
	CountPending(ctx context.Context) (int, error)
	ResetUserPassword(ctx context.Context, id int64, passwordHash string, at time.Time, actor domain.Actor) (domain.User, error)
	SetOwnPassword(ctx context.Context, id int64, passwordHash string, at time.Time, keepSessionID string) (domain.User, error)
	DeleteUser(ctx context.Context, id int64, actor domain.Actor) error
}

// AuditSink writes one audit row. It is the MariaDB guild store; a nil sink
// degrades to logging only, because user administration must never be blocked by
// audit availability (owner decision P2 Q5).
type AuditSink interface {
	InsertAudit(ctx context.Context, entry domain.AuditEntry) error
}

// UserAdminService owns account creation, role and status changes, password
// resets, deletion and the account owner's own password change.
type UserAdminService struct {
	store  UserAdminStore
	audit  AuditSink
	logger *slog.Logger
	now    func() time.Time
}

// NewUserAdminService builds the account-administration service. audit may be
// nil when no guild store is configured.
func NewUserAdminService(store UserAdminStore, audit AuditSink, logger *slog.Logger) *UserAdminService {
	return &UserAdminService{store: store, audit: audit, logger: logger, now: time.Now}
}

// Ready reports whether account storage is available.
func (s *UserAdminService) Ready() bool { return s != nil && s.store != nil }

func (s *UserAdminService) requireReady() error {
	if !s.Ready() {
		return domain.ErrUnavailable
	}
	return nil
}

// List returns the accounts whose username contains filter, ordered by username.
// Password hashes are cleared before the rows leave the service.
func (s *UserAdminService) List(ctx context.Context, filter string) ([]domain.User, error) {
	return s.ListStatus(ctx, filter, "")
}

// ListStatus returns the accounts whose username contains filter and whose
// approval state matches status. Status values are the ones the accounts page
// and the API accept: "" or "all" (every account), "pending", "approved" and
// "disabled". An unknown value is treated as "all" so a hand-edited query string
// can only ever widen a read that the caller is already authorized to perform.
func (s *UserAdminService) ListStatus(ctx context.Context, filter, status string) ([]domain.User, error) {
	if err := s.requireReady(); err != nil {
		return nil, err
	}
	users, err := s.store.ListUsers(ctx)
	if err != nil {
		return nil, err
	}
	needle := strings.ToLower(strings.TrimSpace(filter))
	want := strings.ToLower(strings.TrimSpace(status))
	out := make([]domain.User, 0, len(users))
	for _, user := range users {
		if needle != "" && !strings.Contains(strings.ToLower(user.UserName), needle) {
			continue
		}
		switch want {
		case "", "all":
		case "pending":
			if !user.IsPending() {
				continue
			}
		case "approved":
			if !user.IsApproved() {
				continue
			}
		case "disabled":
			if !user.Disabled {
				continue
			}
		}
		user.PasswordHash = ""
		out = append(out, user)
	}
	// The approval queue is a queue: the oldest request must be first. Every
	// other view keeps the store's case-insensitive username order.
	if want == "pending" {
		sort.SliceStable(out, func(i, j int) bool {
			if out[i].CreatedAt.Equal(out[j].CreatedAt) {
				return out[i].ID < out[j].ID
			}
			return out[i].CreatedAt.Before(out[j].CreatedAt)
		})
	}
	return out, nil
}

// PendingCount returns how many accounts wait for approval. The accounts page
// and the navigation badge use it.
func (s *UserAdminService) PendingCount(ctx context.Context) (int, error) {
	if err := s.requireReady(); err != nil {
		return 0, err
	}
	return s.store.CountPending(ctx)
}

// Get returns one account without its password hash.
func (s *UserAdminService) Get(ctx context.Context, id int64) (domain.User, error) {
	if err := s.requireReady(); err != nil {
		return domain.User{}, err
	}
	if id <= 0 {
		return domain.User{}, domain.ErrNotFound
	}
	user, err := s.store.GetUserByID(ctx, id)
	if err != nil {
		return domain.User{}, err
	}
	user.PasswordHash = ""
	return user, nil
}

// Create adds an account with a temporary password and always requires a change
// at the next sign-in (P2 Q1).
func (s *UserAdminService) Create(ctx context.Context, in domain.CreateUserInput, actor domain.Actor,
	correlationID, sourceIP string) (domain.User, error) {
	if err := s.requireReady(); err != nil {
		return domain.User{}, err
	}
	if !actor.IsAdmin {
		return domain.User{}, domain.ErrForbidden
	}
	if err := in.Validate(); err != nil {
		return domain.User{}, err
	}
	username := in.NormalizedUserName()
	hash, err := HashUserPassword(username, in.Password)
	if err != nil {
		return domain.User{}, err
	}
	now := s.now().UTC()
	created, err := s.store.CreateUser(ctx, domain.User{
		UserName: username, PasswordHash: hash, AccessLevel: string(in.RoleValue()),
		MustChangePassword: true, ModAt: now, CreatedAt: now, ModBy: actor.UserName,
	})
	if err != nil {
		return domain.User{}, err
	}
	s.record(ctx, "USER_CREATED", created,
		map[string]string{}, map[string]string{"userName": created.UserName, "accessLevel": created.AccessLevel},
		actor, correlationID, sourceIP)
	created.PasswordHash = ""
	return created, nil
}

// Approve admits a pending account. It never assigns a role and never creates a
// session: the user signs in through the normal login or Discord sign-in path, so
// the login limiter stays authoritative. Approving an already-approved account is
// idempotent and writes no second audit row.
func (s *UserAdminService) Approve(ctx context.Context, id int64, actor domain.Actor,
	correlationID, sourceIP string) (domain.User, error) {
	return s.setAccountStatus(ctx, id, domain.AccountStatusApproved, actor, correlationID, sourceIP)
}

// WithdrawApproval returns an approved account to pending. Every session for the
// account is deleted in the same transaction, exactly as disabling does.
func (s *UserAdminService) WithdrawApproval(ctx context.Context, id int64, actor domain.Actor,
	correlationID, sourceIP string) (domain.User, error) {
	return s.setAccountStatus(ctx, id, domain.AccountStatusPendingApproval, actor, correlationID, sourceIP)
}

func (s *UserAdminService) setAccountStatus(ctx context.Context, id int64, status domain.AccountStatus,
	actor domain.Actor, correlationID, sourceIP string) (domain.User, error) {
	if err := s.requireReady(); err != nil {
		return domain.User{}, err
	}
	if !actor.IsAdmin {
		return domain.User{}, domain.ErrForbidden
	}
	current, err := s.store.GetUserByID(ctx, id)
	if err != nil {
		return domain.User{}, err
	}
	// Withdrawing an operator's own approval would end their own session
	// mid-request, exactly like disabling their own account.
	if status == domain.AccountStatusPendingApproval && current.ID == actor.UserID {
		return domain.User{}, fmt.Errorf("%w: you cannot withdraw approval from your own account", domain.ErrConflict)
	}
	if current.Status() == status {
		current.PasswordHash = ""
		return current, nil
	}
	updated, err := s.store.SetUserAccountStatus(ctx, id, status, s.now().UTC(), actor)
	if err != nil {
		return domain.User{}, err
	}
	event := "USER_APPROVED"
	newValues := map[string]string{
		"accountStatus": string(updated.Status()),
		"origin":        updated.Origin,
	}
	if status == domain.AccountStatusPendingApproval {
		event = "USER_APPROVAL_WITHDRAWN"
		// The store deletes every session for the account in the same
		// transaction, so "all" is the exact scope of the revocation.
		newValues["sessionsRevoked"] = "all"
	}
	s.record(ctx, event, updated,
		map[string]string{"accountStatus": string(current.Status())}, newValues,
		actor, correlationID, sourceIP)
	updated.PasswordHash = ""
	return updated, nil
}

// SetRole changes one account's role. The store refuses to remove the last
// enabled Administrator and refuses a role change while the account is pending.
func (s *UserAdminService) SetRole(ctx context.Context, id int64, roleValue string,
	actor domain.Actor, correlationID, sourceIP string) (domain.User, error) {
	if err := s.requireReady(); err != nil {
		return domain.User{}, err
	}
	if !actor.IsAdmin {
		return domain.User{}, domain.ErrForbidden
	}
	role, err := domain.ParseRoleInput(roleValue)
	if err != nil {
		return domain.User{}, err
	}
	current, err := s.store.GetUserByID(ctx, id)
	if err != nil {
		return domain.User{}, err
	}
	if current.Role() == role {
		current.PasswordHash = ""
		return current, nil
	}
	updated, err := s.store.SetUserAccessLevel(ctx, id, role, s.now().UTC(), actor)
	if err != nil {
		return domain.User{}, err
	}
	s.record(ctx, "USER_ROLE_CHANGED", updated,
		map[string]string{"accessLevel": current.AccessLevel},
		map[string]string{"accessLevel": updated.AccessLevel},
		actor, correlationID, sourceIP)
	updated.PasswordHash = ""
	return updated, nil
}

// SetDisabled disables or re-enables one account. Disabling revokes that
// account's sessions in the store. An administrator cannot disable their own
// account: it would end their own session mid-request and is never the intended
// outcome of the form.
func (s *UserAdminService) SetDisabled(ctx context.Context, id int64, disabled bool,
	actor domain.Actor, correlationID, sourceIP string) (domain.User, error) {
	if err := s.requireReady(); err != nil {
		return domain.User{}, err
	}
	if !actor.IsAdmin {
		return domain.User{}, domain.ErrForbidden
	}
	current, err := s.store.GetUserByID(ctx, id)
	if err != nil {
		return domain.User{}, err
	}
	if disabled && current.ID == actor.UserID {
		return domain.User{}, fmt.Errorf("%w: you cannot disable your own account", domain.ErrConflict)
	}
	if current.Disabled == disabled {
		current.PasswordHash = ""
		return current, nil
	}
	updated, err := s.store.SetUserDisabled(ctx, id, disabled, s.now().UTC(), actor)
	if err != nil {
		return domain.User{}, err
	}
	event := "USER_ENABLED"
	if disabled {
		event = "USER_DISABLED"
	}
	s.record(ctx, event, updated,
		map[string]string{"disabled": strconv.FormatBool(current.Disabled)},
		map[string]string{"disabled": strconv.FormatBool(updated.Disabled)},
		actor, correlationID, sourceIP)
	updated.PasswordHash = ""
	return updated, nil
}

// ResetPassword stores an administrator-supplied temporary password, requires a
// change at the next sign-in and revokes every session for the account.
func (s *UserAdminService) ResetPassword(ctx context.Context, id int64, in domain.ResetPasswordInput,
	actor domain.Actor, correlationID, sourceIP string) (domain.User, error) {
	if err := s.requireReady(); err != nil {
		return domain.User{}, err
	}
	if !actor.IsAdmin {
		return domain.User{}, domain.ErrForbidden
	}
	if err := in.Validate(); err != nil {
		return domain.User{}, err
	}
	target, err := s.store.GetUserByID(ctx, id)
	if err != nil {
		return domain.User{}, err
	}
	hash, err := HashUserPassword(target.UserName, in.Password)
	if err != nil {
		return domain.User{}, err
	}
	updated, err := s.store.ResetUserPassword(ctx, id, hash, s.now().UTC(), actor)
	if err != nil {
		return domain.User{}, err
	}
	s.record(ctx, "USER_PASSWORD_RESET", updated, map[string]string{},
		map[string]string{"mustChangePassword": "true"}, actor, correlationID, sourceIP)
	updated.PasswordHash = ""
	return updated, nil
}

// Delete permanently removes one account after the typed-name confirmation. An
// administrator cannot delete their own account, so a mistaken click cannot
// remove the operator's access mid-session.
func (s *UserAdminService) Delete(ctx context.Context, id int64, in domain.DeleteUserInput,
	actor domain.Actor, correlationID, sourceIP string) error {
	if err := s.requireReady(); err != nil {
		return err
	}
	if !actor.IsAdmin {
		return domain.ErrForbidden
	}
	target, err := s.store.GetUserByID(ctx, id)
	if err != nil {
		return err
	}
	if target.ID == actor.UserID {
		return fmt.Errorf("%w: you cannot delete your own account", domain.ErrConflict)
	}
	if err := in.Validate(target.UserName); err != nil {
		return err
	}
	if err := s.store.DeleteUser(ctx, id, actor); err != nil {
		return err
	}
	s.record(ctx, "USER_DELETED", target,
		map[string]string{"userName": target.UserName, "accessLevel": target.AccessLevel,
			"disabled": strconv.FormatBool(target.Disabled)},
		map[string]string{}, actor, correlationID, sourceIP)
	return nil
}

// ChangeOwnPassword verifies the current password, applies the policy to the new
// one, clears MustChangePassword and revokes every other session.
func (s *UserAdminService) ChangeOwnPassword(ctx context.Context, user domain.User, sessionID string,
	in domain.ChangePasswordInput, correlationID, sourceIP string) (domain.User, error) {
	if err := s.requireReady(); err != nil {
		return domain.User{}, err
	}
	if err := in.Validate(); err != nil {
		return domain.User{}, err
	}
	current, err := s.store.GetUserByID(ctx, user.ID)
	if err != nil {
		return domain.User{}, err
	}
	if !current.IsEnabled() {
		return domain.User{}, domain.ErrUnauthorized
	}
	if !current.HasPassword {
		// There is no current password to verify, so this route cannot be the
		// one that creates the first credential (D-SR-19).
		return domain.User{}, fmt.Errorf("%w: this account has no password yet", domain.ErrConflict)
	}
	if !VerifyPassword(current.PasswordHash, in.CurrentPassword) {
		return domain.User{}, domain.NewValidationError("currentPassword", "is not correct")
	}
	hash, err := HashUserPassword(current.UserName, in.NewPassword)
	if err != nil {
		return domain.User{}, err
	}
	updated, err := s.store.SetOwnPassword(ctx, current.ID, hash, s.now().UTC(), sessionID)
	if err != nil {
		return domain.User{}, err
	}
	actor := domain.Actor{UserID: updated.ID, UserName: updated.UserName, Role: updated.Role(),
		IsAdmin: updated.IsAdmin()}
	s.record(ctx, "USER_PASSWORD_CHANGED", updated, map[string]string{},
		map[string]string{"self": "true"}, actor, correlationID, sourceIP)
	updated.PasswordHash = ""
	return updated, nil
}

// SetOwnPassword establishes the first password for a signed-in account that has
// none (D-SR-15). A session proves ownership, so no current password is asked for
// (D-SR-17). The store writes HasPassword = 1, clears MustChangePassword and
// revokes every other session; the account may keep signing in with Discord.
func (s *UserAdminService) SetOwnPassword(ctx context.Context, user domain.User, sessionID string,
	in domain.SetPasswordInput, correlationID, sourceIP string) (domain.User, error) {
	if err := s.requireReady(); err != nil {
		return domain.User{}, err
	}
	if err := in.Validate(); err != nil {
		return domain.User{}, err
	}
	current, err := s.store.GetUserByID(ctx, user.ID)
	if err != nil {
		return domain.User{}, err
	}
	if !current.IsEnabled() {
		return domain.User{}, domain.ErrUnauthorized
	}
	if current.HasPassword {
		// Establishing a password is a one-time transition. Once it exists the
		// owner must prove it, so this endpoint cannot silently replace it.
		return domain.User{}, fmt.Errorf("%w: this account already has a password", domain.ErrConflict)
	}
	hash, err := HashUserPassword(current.UserName, in.NewPassword)
	if err != nil {
		return domain.User{}, err
	}
	updated, err := s.store.SetOwnPassword(ctx, current.ID, hash, s.now().UTC(), sessionID)
	if err != nil {
		return domain.User{}, err
	}
	actor := domain.Actor{UserID: updated.ID, UserName: updated.UserName, Role: updated.Role(),
		IsAdmin: updated.IsAdmin()}
	s.record(ctx, "USER_PASSWORD_SET", updated,
		map[string]string{"hasPassword": "false"},
		map[string]string{"hasPassword": "true"}, actor, correlationID, sourceIP)
	updated.PasswordHash = ""
	return updated, nil
}

// EnabledAdmins counts enabled Administrator accounts. The user list and detail
// pages use it to disable controls that the store would refuse anyway.
func (s *UserAdminService) EnabledAdmins(ctx context.Context) (int, error) {
	if err := s.requireReady(); err != nil {
		return 0, err
	}
	users, err := s.store.ListUsers(ctx)
	if err != nil {
		return 0, err
	}
	count := 0
	for _, user := range users {
		if user.Role() == domain.RoleAdmin && !user.Disabled {
			count++
		}
	}
	return count, nil
}

// record writes the best-effort audit row for an account change.
func (s *UserAdminService) record(ctx context.Context, eventType string, target domain.User,
	oldValues, newValues map[string]string, actor domain.Actor, correlationID, sourceIP string) {
	writeAudit(ctx, s.audit, s.logger, s.now().UTC(), eventType, "Users",
		strconv.FormatInt(target.ID, 10), actor, oldValues, newValues, correlationID, sourceIP)
}

// writeAudit is the shared best-effort audit writer for account and profile
// changes. A failure is logged and never changes the outcome of a change that has
// already been committed to SQLite.
func writeAudit(ctx context.Context, sink AuditSink, logger *slog.Logger, at time.Time,
	eventType, tableName, recordID string, actor domain.Actor,
	oldValues, newValues map[string]string, correlationID, sourceIP string) {
	if sink == nil {
		return
	}
	entry := domain.AuditEntry{
		EventDate: at, UserID: actor.UserID, UserName: actor.UserName,
		EventType: eventType, TableName: tableName, RecordID: recordID,
		OldValues: auditValuesJSON(oldValues), NewValues: auditValuesJSON(newValues),
		CorrelationID: correlationID, SourceIP: sourceIP,
	}
	if err := sink.InsertAudit(ctx, entry); err != nil {
		if logger != nil {
			logger.Warn("audit write failed",
				"event", eventType, "table", tableName, "record_id", recordID,
				"request_id", correlationID, "error", err)
		}
	}
}

func auditValuesJSON(values map[string]string) json.RawMessage {
	if len(values) == 0 {
		return json.RawMessage("{}")
	}
	encoded, err := json.Marshal(values)
	if err != nil {
		return json.RawMessage("{}")
	}
	return encoded
}

// IsAlreadyExists reports the duplicate-username condition so the API can answer
// 409 with a field-level message instead of a generic conflict.
func IsAlreadyExists(err error) bool { return errors.Is(err, domain.ErrAlreadyExists) }
