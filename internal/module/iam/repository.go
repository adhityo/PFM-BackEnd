package iam

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// UserRepository persists app_user.
type UserRepository interface {
	CredentialByLogin(ctx context.Context, login string) (Credential, error)
	CredentialByID(ctx context.Context, id int64) (Credential, error)
	CredentialByUUID(ctx context.Context, id uuid.UUID) (Credential, error)
	RecordLoginFailure(ctx context.Context, id int64, maxAttempts int, lockFor time.Duration) error
	RecordLoginSuccess(ctx context.Context, id int64) error
	SetPassword(ctx context.Context, id int64, hash, actor string) error
	ActivationTargetForUpdate(ctx context.Context, keyHash string) (ActivationTarget, error)

	GetByUUID(ctx context.Context, id uuid.UUID, scopeClientID *int64) (User, error)
	List(ctx context.Context, f UserFilter, p Paging) ([]User, int64, error)
	Create(ctx context.Context, u NewUser) (int64, error)
	Update(ctx context.Context, u UserChange) (int64, error)
	SoftDelete(ctx context.Context, id uuid.UUID, actor string, expected time.Time) (int64, error)
	ResetCredential(ctx context.Context, id uuid.UUID, keyHash string, expires time.Time, actor string, expected time.Time) (int64, error)
	SetLockedUntil(ctx context.Context, id uuid.UUID, until *time.Time, actor string, expected time.Time) (int64, error)
	CountByRole(ctx context.Context, roleID int64) (int64, error)
}

// RefreshTokenRepository persists auth_refresh_token.
type RefreshTokenRepository interface {
	Insert(ctx context.Context, t NewRefreshToken) (int64, error)
	GetByHashForUpdate(ctx context.Context, hash string) (RefreshToken, error)
	Revoke(ctx context.Context, id int64, replacedBy *int64) error
	RevokeFamily(ctx context.Context, family uuid.UUID) error
	RevokeAllForUser(ctx context.Context, userID int64) error
}

// RoleRepository persists role and role_action.
type RoleRepository interface {
	GetByUUID(ctx context.Context, id uuid.UUID) (Role, error)
	GetByUUIDForUpdate(ctx context.Context, id uuid.UUID) (Role, error)
	List(ctx context.Context, q *string, p Paging) ([]Role, int64, error)
	Create(ctx context.Context, in RoleInput) (Role, error)
	Update(ctx context.Context, id uuid.UUID, in RoleInput, expected time.Time) (Role, error)
	SoftDelete(ctx context.Context, id uuid.UUID, actor string, expected time.Time) error
	Actions(ctx context.Context, roleID int64) ([]Action, error)
	ReplaceActions(ctx context.Context, role Role, actions []Action, actor string) error
	PermissionCodes(ctx context.Context, roleID int64) ([]string, error)
	UpsertByCode(ctx context.Context, code, name, actor string) (Role, error)
}

// ActionRepository persists the action catalog.
type ActionRepository interface {
	GetByUUID(ctx context.Context, id uuid.UUID) (Action, error)
	GetByUUIDs(ctx context.Context, ids []uuid.UUID) ([]Action, error)
	List(ctx context.Context, q *string, p Paging) ([]Action, int64, error)
	Create(ctx context.Context, in ActionInput) (Action, error)
	Update(ctx context.Context, id uuid.UUID, in ActionInput, expected time.Time) (Action, error)
	SoftDelete(ctx context.Context, id uuid.UUID, actor string, expected time.Time) error
	CountRoleUsage(ctx context.Context, actionID int64) (int64, error)
	EnsureCode(ctx context.Context, code, name, actor string) error
}

// ActivityLogRepository reads activity_log.
type ActivityLogRepository interface {
	GetByUUID(ctx context.Context, id uuid.UUID) (ActivityLog, error)
	List(ctx context.Context, f ActivityLogFilter, p Paging) ([]ActivityLog, int64, error)
}

// ClientLookup resolves client UUIDs (owned by the portfolio module later).
type ClientLookup interface {
	ClientIDByUUID(ctx context.Context, id uuid.UUID) (int64, error)
}
