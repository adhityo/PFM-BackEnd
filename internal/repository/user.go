package repository

import (
	"context"
	"time"

	"github.com/google/uuid"

	"pfm-backend/internal/db/sqlc"
	"pfm-backend/internal/model"
	"pfm-backend/internal/platform/database"
)

// UserRepository persists app_user.
type UserRepository interface {
	CredentialByLogin(ctx context.Context, login string) (model.Credential, error)
	CredentialByID(ctx context.Context, id int64) (model.Credential, error)
	CredentialByUUID(ctx context.Context, id uuid.UUID) (model.Credential, error)
	RecordLoginFailure(ctx context.Context, id int64, maxAttempts int, lockFor time.Duration) error
	RecordLoginSuccess(ctx context.Context, id int64) error
	SetPassword(ctx context.Context, id int64, hash, actor string) error
	ActivationTargetForUpdate(ctx context.Context, keyHash string) (model.ActivationTarget, error)

	GetByUUID(ctx context.Context, id uuid.UUID, scopeClientID *int64) (model.User, error)
	List(ctx context.Context, f model.UserFilter, p model.Paging) ([]model.User, int64, error)
	Create(ctx context.Context, u model.NewUser) (int64, error)
	Update(ctx context.Context, u model.UserChange) (int64, error)
	SoftDelete(ctx context.Context, id uuid.UUID, actor string, expected time.Time) (int64, error)
	ResetCredential(ctx context.Context, id uuid.UUID, keyHash string, expires time.Time, actor string, expected time.Time) (int64, error)
	SetLockedUntil(ctx context.Context, id uuid.UUID, until *time.Time, actor string, expected time.Time) (int64, error)
	CountByRole(ctx context.Context, roleID int64) (int64, error)
}

type userRepository struct{ tx *database.TxManager }

func NewUserRepository(tx *database.TxManager) UserRepository { return &userRepository{tx: tx} }

type credentialRow interface {
	sqlc.GetUserAuthByLoginRow | sqlc.GetUserAuthByIDRow | sqlc.GetUserAuthByUUIDRow
}

func toCredential[T credentialRow](row T) model.Credential {
	r := sqlc.GetUserAuthByLoginRow(row)
	c := model.Credential{ID: r.ID, UUID: r.Uuid, Username: deref(r.Username), PasswordHash: r.PasswordHash,
		LockedUntil: r.LockedUntil, RoleID: r.RoleID, RoleCode: deref(r.RoleCode), ClientID: r.ClientID,
		ClientUUID: r.ClientUuid}
	if r.Status != nil {
		c.Status = *r.Status
	}
	return c
}

func (r *userRepository) CredentialByLogin(ctx context.Context, login string) (model.Credential, error) {
	row, err := r.tx.Queries(ctx).GetUserAuthByLogin(ctx, login)
	if err != nil {
		return model.Credential{}, database.MapError("credential by login", err)
	}
	return toCredential(row), nil
}

func (r *userRepository) CredentialByID(ctx context.Context, id int64) (model.Credential, error) {
	row, err := r.tx.Queries(ctx).GetUserAuthByID(ctx, id)
	if err != nil {
		return model.Credential{}, database.MapError("credential by id", err)
	}
	return toCredential(row), nil
}

func (r *userRepository) CredentialByUUID(ctx context.Context, id uuid.UUID) (model.Credential, error) {
	row, err := r.tx.Queries(ctx).GetUserAuthByUUID(ctx, id)
	if err != nil {
		return model.Credential{}, database.MapError("credential by uuid", err)
	}
	return toCredential(row), nil
}

func (r *userRepository) RecordLoginFailure(ctx context.Context, id int64, maxAttempts int, lockFor time.Duration) error {
	_, err := r.tx.Queries(ctx).RecordLoginFailure(ctx, sqlc.RecordLoginFailureParams{
		ID: id, MaxAttempts: int32(maxAttempts), LockSeconds: int32(lockFor.Seconds()),
	})
	return database.MapError("record login failure", err)
}

func (r *userRepository) RecordLoginSuccess(ctx context.Context, id int64) error {
	return database.MapError("record login success", r.tx.Queries(ctx).RecordLoginSuccess(ctx, id))
}

func (r *userRepository) SetPassword(ctx context.Context, id int64, hash, actor string) error {
	err := r.tx.Queries(ctx).SetUserPassword(ctx, sqlc.SetUserPasswordParams{ID: id, PasswordHash: &hash, Actor: &actor})
	return database.MapError("set password", err)
}

func (r *userRepository) ActivationTargetForUpdate(ctx context.Context, keyHash string) (model.ActivationTarget, error) {
	row, err := r.tx.Queries(ctx).GetUserByActivationKeyHashForUpdate(ctx, &keyHash)
	if err != nil {
		return model.ActivationTarget{}, database.MapError("activation target", err)
	}
	return model.ActivationTarget{ID: row.ID, UUID: row.Uuid, Username: deref(row.Username), ExpiresAt: row.ActivationKeyExpiresAt}, nil
}

func toUser(r sqlc.GetUserByUUIDRow) model.User {
	return model.User{ID: r.ID, UUID: r.Uuid, Username: deref(r.Username), Email: deref(r.Email), FirstName: r.FirstName,
		Description: r.Description, Status: r.Status, RoleID: r.RoleID, RoleUUID: r.RoleUuid, RoleCode: r.RoleCode,
		ClientID: r.ClientID, ClientUUID: r.ClientUuid, IsSupervisor: r.IsSupervisor, PasswordSet: r.PasswordHash != nil,
		LockedUntil: r.LockedUntil, LastLoginAt: r.LastLoginAt, CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt}
}

func (r *userRepository) GetByUUID(ctx context.Context, id uuid.UUID, scopeClientID *int64) (model.User, error) {
	row, err := r.tx.Queries(ctx).GetUserByUUID(ctx, sqlc.GetUserByUUIDParams{Uuid: id, ScopeClientID: scopeClientID})
	if err != nil {
		return model.User{}, database.MapError("get user", err)
	}
	return toUser(row), nil
}

func (r *userRepository) List(ctx context.Context, f model.UserFilter, p model.Paging) ([]model.User, int64, error) {
	q := r.tx.Queries(ctx)
	rows, err := q.ListUsers(ctx, sqlc.ListUsersParams{ScopeClientID: f.ScopeClientID, Q: f.Q, RoleID: f.RoleID,
		Status: f.Status, Sort: p.Sort, Limit: p.Limit, Offset: p.Offset})
	if err != nil {
		return nil, 0, database.MapError("list users", err)
	}
	total, err := q.CountUsers(ctx, sqlc.CountUsersParams{ScopeClientID: f.ScopeClientID, Q: f.Q, RoleID: f.RoleID, Status: f.Status})
	if err != nil {
		return nil, 0, database.MapError("count users", err)
	}
	out := make([]model.User, 0, len(rows))
	for _, row := range rows {
		out = append(out, toUser(sqlc.GetUserByUUIDRow(row)))
	}
	return out, total, nil
}

func (r *userRepository) Create(ctx context.Context, u model.NewUser) (int64, error) {
	row, err := r.tx.Queries(ctx).CreateUser(ctx, sqlc.CreateUserParams{
		Uuid: u.UUID, Username: &u.Username, Email: &u.Email, FirstName: u.FirstName, Description: u.Description,
		Status: &u.Status, RoleID: &u.RoleID, ClientID: u.ClientID, IsSupervisor: u.IsSupervisor,
		ActivationKeyHash: &u.ActivationKeyHash, ActivationKeyExpiresAt: &u.ActivationExpires, Actor: &u.Actor,
	})
	if err != nil {
		return 0, database.MapError("create user", err)
	}
	return row.ID, nil
}

func (r *userRepository) Update(ctx context.Context, u model.UserChange) (int64, error) {
	row, err := r.tx.Queries(ctx).UpdateUser(ctx, sqlc.UpdateUserParams{
		Email: &u.Email, FirstName: u.FirstName, Description: u.Description, Status: &u.Status, RoleID: &u.RoleID,
		ClientID: u.ClientID, IsSupervisor: u.IsSupervisor, Actor: &u.Actor, Uuid: u.UUID, ExpectedUpdatedAt: u.Expected,
	})
	if err != nil {
		return 0, staleIfNoRows("update user", err)
	}
	return row.ID, nil
}

func (r *userRepository) SoftDelete(ctx context.Context, id uuid.UUID, actor string, expected time.Time) (int64, error) {
	uid, err := r.tx.Queries(ctx).SoftDeleteUser(ctx, sqlc.SoftDeleteUserParams{Uuid: id, Actor: &actor, ExpectedUpdatedAt: expected})
	if err != nil {
		return 0, staleIfNoRows("delete user", err)
	}
	return uid, nil
}

func (r *userRepository) ResetCredential(ctx context.Context, id uuid.UUID, keyHash string, expires time.Time, actor string, expected time.Time) (int64, error) {
	row, err := r.tx.Queries(ctx).ResetUserCredential(ctx, sqlc.ResetUserCredentialParams{
		ActivationKeyHash: &keyHash, ActivationKeyExpiresAt: &expires, Actor: &actor, Uuid: id, ExpectedUpdatedAt: expected,
	})
	if err != nil {
		return 0, staleIfNoRows("reset credential", err)
	}
	return row.ID, nil
}

func (r *userRepository) SetLockedUntil(ctx context.Context, id uuid.UUID, until *time.Time, actor string, expected time.Time) (int64, error) {
	row, err := r.tx.Queries(ctx).SetUserLockedUntil(ctx, sqlc.SetUserLockedUntilParams{
		LockedUntil: until, Actor: &actor, Uuid: id, ExpectedUpdatedAt: expected,
	})
	if err != nil {
		return 0, staleIfNoRows("lock user", err)
	}
	return row.ID, nil
}

func (r *userRepository) CountByRole(ctx context.Context, roleID int64) (int64, error) {
	n, err := r.tx.Queries(ctx).CountActiveUsersByRole(ctx, &roleID)
	return n, database.MapError("count users by role", err)
}
