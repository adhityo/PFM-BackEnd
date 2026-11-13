package iam

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"pfm-backend/internal/apperr"
	"pfm-backend/internal/db/sqlc"
	"pfm-backend/internal/platform/database"
)

// Conditional updates (WHERE updated_at = expected) return no rows when the
// row changed meanwhile. Services always load the row first (scope + 404),
// so "no rows" here means a stale ETag.
func staleIfNoRows(op string, err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return apperr.ErrPreconditionFailed
	}
	return database.MapError(op, err)
}

func ptr[T any](v T) *T { return &v }

func newUUID() uuid.UUID {
	id, err := uuid.NewV7()
	if err != nil {
		return uuid.New()
	}
	return id
}

// ---------------------------------------------------------------- users

type pgUserRepo struct{ tx *database.TxManager }

func NewUserRepository(tx *database.TxManager) UserRepository { return &pgUserRepo{tx: tx} }

type credentialRow interface {
	sqlc.GetUserAuthByLoginRow | sqlc.GetUserAuthByIDRow | sqlc.GetUserAuthByUUIDRow
}

func toCredential[T credentialRow](row T) Credential {
	r := sqlc.GetUserAuthByLoginRow(row)
	c := Credential{ID: r.ID, UUID: r.Uuid, Username: deref(r.Username), PasswordHash: r.PasswordHash,
		LockedUntil: r.LockedUntil, RoleID: r.RoleID, RoleCode: deref(r.RoleCode), ClientID: r.ClientID,
		ClientUUID: r.ClientUuid}
	if r.Status != nil {
		c.Status = *r.Status
	}
	return c
}

func (r *pgUserRepo) CredentialByLogin(ctx context.Context, login string) (Credential, error) {
	row, err := r.tx.Queries(ctx).GetUserAuthByLogin(ctx, login)
	if err != nil {
		return Credential{}, database.MapError("credential by login", err)
	}
	return toCredential(row), nil
}

func (r *pgUserRepo) CredentialByID(ctx context.Context, id int64) (Credential, error) {
	row, err := r.tx.Queries(ctx).GetUserAuthByID(ctx, id)
	if err != nil {
		return Credential{}, database.MapError("credential by id", err)
	}
	return toCredential(row), nil
}

func (r *pgUserRepo) CredentialByUUID(ctx context.Context, id uuid.UUID) (Credential, error) {
	row, err := r.tx.Queries(ctx).GetUserAuthByUUID(ctx, id)
	if err != nil {
		return Credential{}, database.MapError("credential by uuid", err)
	}
	return toCredential(row), nil
}

func (r *pgUserRepo) RecordLoginFailure(ctx context.Context, id int64, maxAttempts int, lockFor time.Duration) error {
	_, err := r.tx.Queries(ctx).RecordLoginFailure(ctx, sqlc.RecordLoginFailureParams{
		ID: id, MaxAttempts: int32(maxAttempts), LockSeconds: int32(lockFor.Seconds()),
	})
	return database.MapError("record login failure", err)
}

func (r *pgUserRepo) RecordLoginSuccess(ctx context.Context, id int64) error {
	return database.MapError("record login success", r.tx.Queries(ctx).RecordLoginSuccess(ctx, id))
}

func (r *pgUserRepo) SetPassword(ctx context.Context, id int64, hash, actor string) error {
	err := r.tx.Queries(ctx).SetUserPassword(ctx, sqlc.SetUserPasswordParams{ID: id, PasswordHash: &hash, Actor: &actor})
	return database.MapError("set password", err)
}

func (r *pgUserRepo) ActivationTargetForUpdate(ctx context.Context, keyHash string) (ActivationTarget, error) {
	row, err := r.tx.Queries(ctx).GetUserByActivationKeyHashForUpdate(ctx, &keyHash)
	if err != nil {
		return ActivationTarget{}, database.MapError("activation target", err)
	}
	return ActivationTarget{ID: row.ID, UUID: row.Uuid, Username: deref(row.Username), ExpiresAt: row.ActivationKeyExpiresAt}, nil
}

func toUser(r sqlc.GetUserByUUIDRow) User {
	return User{ID: r.ID, UUID: r.Uuid, Username: deref(r.Username), Email: deref(r.Email), FirstName: r.FirstName,
		Description: r.Description, Status: r.Status, RoleID: r.RoleID, RoleUUID: r.RoleUuid, RoleCode: r.RoleCode,
		ClientID: r.ClientID, ClientUUID: r.ClientUuid, IsSupervisor: r.IsSupervisor, PasswordSet: r.PasswordHash != nil,
		LockedUntil: r.LockedUntil, LastLoginAt: r.LastLoginAt, CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt}
}

func (r *pgUserRepo) GetByUUID(ctx context.Context, id uuid.UUID, scopeClientID *int64) (User, error) {
	row, err := r.tx.Queries(ctx).GetUserByUUID(ctx, sqlc.GetUserByUUIDParams{Uuid: id, ScopeClientID: scopeClientID})
	if err != nil {
		return User{}, database.MapError("get user", err)
	}
	return toUser(row), nil
}

func (r *pgUserRepo) List(ctx context.Context, f UserFilter, p Paging) ([]User, int64, error) {
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
	out := make([]User, 0, len(rows))
	for _, row := range rows {
		out = append(out, toUser(sqlc.GetUserByUUIDRow(row)))
	}
	return out, total, nil
}

func (r *pgUserRepo) Create(ctx context.Context, u NewUser) (int64, error) {
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

func (r *pgUserRepo) Update(ctx context.Context, u UserChange) (int64, error) {
	row, err := r.tx.Queries(ctx).UpdateUser(ctx, sqlc.UpdateUserParams{
		Email: &u.Email, FirstName: u.FirstName, Description: u.Description, Status: &u.Status, RoleID: &u.RoleID,
		ClientID: u.ClientID, IsSupervisor: u.IsSupervisor, Actor: &u.Actor, Uuid: u.UUID, ExpectedUpdatedAt: u.Expected,
	})
	if err != nil {
		return 0, staleIfNoRows("update user", err)
	}
	return row.ID, nil
}

func (r *pgUserRepo) SoftDelete(ctx context.Context, id uuid.UUID, actor string, expected time.Time) (int64, error) {
	uid, err := r.tx.Queries(ctx).SoftDeleteUser(ctx, sqlc.SoftDeleteUserParams{Uuid: id, Actor: &actor, ExpectedUpdatedAt: expected})
	if err != nil {
		return 0, staleIfNoRows("delete user", err)
	}
	return uid, nil
}

func (r *pgUserRepo) ResetCredential(ctx context.Context, id uuid.UUID, keyHash string, expires time.Time, actor string, expected time.Time) (int64, error) {
	row, err := r.tx.Queries(ctx).ResetUserCredential(ctx, sqlc.ResetUserCredentialParams{
		ActivationKeyHash: &keyHash, ActivationKeyExpiresAt: &expires, Actor: &actor, Uuid: id, ExpectedUpdatedAt: expected,
	})
	if err != nil {
		return 0, staleIfNoRows("reset credential", err)
	}
	return row.ID, nil
}

func (r *pgUserRepo) SetLockedUntil(ctx context.Context, id uuid.UUID, until *time.Time, actor string, expected time.Time) (int64, error) {
	row, err := r.tx.Queries(ctx).SetUserLockedUntil(ctx, sqlc.SetUserLockedUntilParams{
		LockedUntil: until, Actor: &actor, Uuid: id, ExpectedUpdatedAt: expected,
	})
	if err != nil {
		return 0, staleIfNoRows("lock user", err)
	}
	return row.ID, nil
}

func (r *pgUserRepo) CountByRole(ctx context.Context, roleID int64) (int64, error) {
	n, err := r.tx.Queries(ctx).CountActiveUsersByRole(ctx, &roleID)
	return n, database.MapError("count users by role", err)
}

// ---------------------------------------------------------------- refresh tokens

type pgRefreshTokenRepo struct{ tx *database.TxManager }

func NewRefreshTokenRepository(tx *database.TxManager) RefreshTokenRepository {
	return &pgRefreshTokenRepo{tx: tx}
}

func (r *pgRefreshTokenRepo) Insert(ctx context.Context, t NewRefreshToken) (int64, error) {
	var ua *string
	if t.UserAgent != "" {
		ua = &t.UserAgent
	}
	id, err := r.tx.Queries(ctx).InsertRefreshToken(ctx, sqlc.InsertRefreshTokenParams{
		Uuid: newUUID(), UserID: t.UserID, FamilyID: t.FamilyID, TokenHash: t.TokenHash,
		UserAgent: ua, IpAddress: t.IP, ExpiresAt: t.ExpiresAt,
	})
	return id, database.MapError("insert refresh token", err)
}

func (r *pgRefreshTokenRepo) GetByHashForUpdate(ctx context.Context, hash string) (RefreshToken, error) {
	row, err := r.tx.Queries(ctx).GetRefreshTokenByHashForUpdate(ctx, hash)
	if err != nil {
		return RefreshToken{}, database.MapError("get refresh token", err)
	}
	return RefreshToken{ID: row.ID, UserID: row.UserID, FamilyID: row.FamilyID, ExpiresAt: row.ExpiresAt, RevokedAt: row.RevokedAt}, nil
}

func (r *pgRefreshTokenRepo) Revoke(ctx context.Context, id int64, replacedBy *int64) error {
	err := r.tx.Queries(ctx).RevokeRefreshToken(ctx, sqlc.RevokeRefreshTokenParams{ID: id, ReplacedBy: replacedBy})
	return database.MapError("revoke refresh token", err)
}

func (r *pgRefreshTokenRepo) RevokeFamily(ctx context.Context, family uuid.UUID) error {
	return database.MapError("revoke token family", r.tx.Queries(ctx).RevokeRefreshTokenFamily(ctx, family))
}

func (r *pgRefreshTokenRepo) RevokeAllForUser(ctx context.Context, userID int64) error {
	return database.MapError("revoke user tokens", r.tx.Queries(ctx).RevokeUserRefreshTokens(ctx, userID))
}

// ---------------------------------------------------------------- roles

type pgRoleRepo struct{ tx *database.TxManager }

func NewRoleRepository(tx *database.TxManager) RoleRepository { return &pgRoleRepo{tx: tx} }

func toRole(r sqlc.Role) Role {
	return Role{ID: r.ID, UUID: r.Uuid, RoleCode: deref(r.RoleCode), RoleName: deref(r.RoleName),
		Description: r.Description, MenuURL: r.MenuUrl, Status: r.Status, CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt}
}

func (r *pgRoleRepo) GetByUUID(ctx context.Context, id uuid.UUID) (Role, error) {
	row, err := r.tx.Queries(ctx).GetRoleByUUID(ctx, id)
	if err != nil {
		return Role{}, database.MapError("get role", err)
	}
	return toRole(row), nil
}

func (r *pgRoleRepo) GetByUUIDForUpdate(ctx context.Context, id uuid.UUID) (Role, error) {
	row, err := r.tx.Queries(ctx).GetRoleByUUIDForUpdate(ctx, id)
	if err != nil {
		return Role{}, database.MapError("lock role", err)
	}
	return toRole(row), nil
}

func (r *pgRoleRepo) List(ctx context.Context, q *string, p Paging) ([]Role, int64, error) {
	qs := r.tx.Queries(ctx)
	rows, err := qs.ListRoles(ctx, sqlc.ListRolesParams{Q: q, Sort: p.Sort, Limit: p.Limit, Offset: p.Offset})
	if err != nil {
		return nil, 0, database.MapError("list roles", err)
	}
	total, err := qs.CountRoles(ctx, q)
	if err != nil {
		return nil, 0, database.MapError("count roles", err)
	}
	out := make([]Role, 0, len(rows))
	for _, row := range rows {
		out = append(out, toRole(row))
	}
	return out, total, nil
}

func (r *pgRoleRepo) Create(ctx context.Context, in RoleInput) (Role, error) {
	row, err := r.tx.Queries(ctx).CreateRole(ctx, sqlc.CreateRoleParams{Uuid: newUUID(), RoleCode: &in.RoleCode,
		RoleName: &in.RoleName, Description: in.Description, MenuUrl: in.MenuURL, Status: &in.Status, Actor: &in.Actor})
	if err != nil {
		return Role{}, database.MapError("create role", err)
	}
	return toRole(row), nil
}

func (r *pgRoleRepo) Update(ctx context.Context, id uuid.UUID, in RoleInput, expected time.Time) (Role, error) {
	row, err := r.tx.Queries(ctx).UpdateRole(ctx, sqlc.UpdateRoleParams{RoleCode: &in.RoleCode, RoleName: &in.RoleName,
		Description: in.Description, MenuUrl: in.MenuURL, Status: &in.Status, Actor: &in.Actor, Uuid: id, ExpectedUpdatedAt: expected})
	if err != nil {
		return Role{}, staleIfNoRows("update role", err)
	}
	return toRole(row), nil
}

func (r *pgRoleRepo) SoftDelete(ctx context.Context, id uuid.UUID, actor string, expected time.Time) error {
	_, err := r.tx.Queries(ctx).SoftDeleteRole(ctx, sqlc.SoftDeleteRoleParams{Uuid: id, Actor: &actor, ExpectedUpdatedAt: expected})
	return staleIfNoRows("delete role", err)
}

func (r *pgRoleRepo) Actions(ctx context.Context, roleID int64) ([]Action, error) {
	rows, err := r.tx.Queries(ctx).ListActionsByRole(ctx, &roleID)
	if err != nil {
		return nil, database.MapError("list role actions", err)
	}
	out := make([]Action, 0, len(rows))
	for _, row := range rows {
		out = append(out, toAction(row))
	}
	return out, nil
}

// ReplaceActions must run inside a transaction (the caller owns it).
func (r *pgRoleRepo) ReplaceActions(ctx context.Context, role Role, actions []Action, actor string) error {
	q := r.tx.Queries(ctx)
	if err := q.SoftDeleteRoleActionsByRole(ctx, sqlc.SoftDeleteRoleActionsByRoleParams{RoleID: &role.ID, Actor: &actor}); err != nil {
		return database.MapError("clear role actions", err)
	}
	for _, a := range actions {
		err := q.InsertRoleAction(ctx, sqlc.InsertRoleActionParams{Uuid: newUUID(), RoleID: &role.ID, ActionID: ptr(a.ID),
			RoleName: &role.RoleName, ActionName: &a.ActionName, Actor: &actor})
		if err != nil {
			return database.MapError("insert role action", err)
		}
	}
	return database.MapError("touch role", q.TouchRole(ctx, sqlc.TouchRoleParams{ID: role.ID, Actor: &actor}))
}

func (r *pgRoleRepo) PermissionCodes(ctx context.Context, roleID int64) ([]string, error) {
	codes, err := r.tx.Queries(ctx).ListPermissionCodesByRole(ctx, &roleID)
	return codes, database.MapError("permission codes", err)
}

func (r *pgRoleRepo) UpsertByCode(ctx context.Context, code, name, actor string) (Role, error) {
	row, err := r.tx.Queries(ctx).UpsertRoleByCode(ctx, sqlc.UpsertRoleByCodeParams{Uuid: newUUID(), RoleCode: &code,
		RoleName: &name, Actor: &actor})
	if err != nil {
		return Role{}, database.MapError("upsert role", err)
	}
	return toRole(row), nil
}

// ---------------------------------------------------------------- actions

type pgActionRepo struct{ tx *database.TxManager }

func NewActionRepository(tx *database.TxManager) ActionRepository { return &pgActionRepo{tx: tx} }

func toAction(a sqlc.Action) Action {
	return Action{ID: a.ID, UUID: a.Uuid, ActionCode: deref(a.ActionCode), ActionName: deref(a.ActionName),
		Description: a.Description, CreatedAt: a.CreatedAt, UpdatedAt: a.UpdatedAt}
}

func (r *pgActionRepo) GetByUUID(ctx context.Context, id uuid.UUID) (Action, error) {
	row, err := r.tx.Queries(ctx).GetActionByUUID(ctx, id)
	if err != nil {
		return Action{}, database.MapError("get action", err)
	}
	return toAction(row), nil
}

func (r *pgActionRepo) GetByUUIDs(ctx context.Context, ids []uuid.UUID) ([]Action, error) {
	rows, err := r.tx.Queries(ctx).GetActionsByUUIDs(ctx, ids)
	if err != nil {
		return nil, database.MapError("get actions", err)
	}
	out := make([]Action, 0, len(rows))
	for _, row := range rows {
		out = append(out, toAction(row))
	}
	return out, nil
}

func (r *pgActionRepo) List(ctx context.Context, q *string, p Paging) ([]Action, int64, error) {
	qs := r.tx.Queries(ctx)
	rows, err := qs.ListActions(ctx, sqlc.ListActionsParams{Q: q, Sort: p.Sort, Limit: p.Limit, Offset: p.Offset})
	if err != nil {
		return nil, 0, database.MapError("list actions", err)
	}
	total, err := qs.CountActions(ctx, q)
	if err != nil {
		return nil, 0, database.MapError("count actions", err)
	}
	out := make([]Action, 0, len(rows))
	for _, row := range rows {
		out = append(out, toAction(row))
	}
	return out, total, nil
}

func (r *pgActionRepo) Create(ctx context.Context, in ActionInput) (Action, error) {
	row, err := r.tx.Queries(ctx).CreateAction(ctx, sqlc.CreateActionParams{Uuid: newUUID(), ActionCode: &in.ActionCode,
		ActionName: &in.ActionName, Description: in.Description, Actor: &in.Actor})
	if err != nil {
		return Action{}, database.MapError("create action", err)
	}
	return toAction(row), nil
}

func (r *pgActionRepo) Update(ctx context.Context, id uuid.UUID, in ActionInput, expected time.Time) (Action, error) {
	row, err := r.tx.Queries(ctx).UpdateAction(ctx, sqlc.UpdateActionParams{ActionCode: &in.ActionCode,
		ActionName: &in.ActionName, Description: in.Description, Actor: &in.Actor, Uuid: id, ExpectedUpdatedAt: expected})
	if err != nil {
		return Action{}, staleIfNoRows("update action", err)
	}
	return toAction(row), nil
}

func (r *pgActionRepo) SoftDelete(ctx context.Context, id uuid.UUID, actor string, expected time.Time) error {
	_, err := r.tx.Queries(ctx).SoftDeleteAction(ctx, sqlc.SoftDeleteActionParams{Uuid: id, Actor: &actor, ExpectedUpdatedAt: expected})
	return staleIfNoRows("delete action", err)
}

func (r *pgActionRepo) CountRoleUsage(ctx context.Context, actionID int64) (int64, error) {
	n, err := r.tx.Queries(ctx).CountActiveRoleActionsByAction(ctx, &actionID)
	return n, database.MapError("count action usage", err)
}

func (r *pgActionRepo) EnsureCode(ctx context.Context, code, name, actor string) error {
	err := r.tx.Queries(ctx).UpsertActionByCode(ctx, sqlc.UpsertActionByCodeParams{Uuid: newUUID(), ActionCode: &code,
		ActionName: &name, Actor: &actor})
	return database.MapError("ensure action", err)
}

// ---------------------------------------------------------------- activity log

type pgActivityLogRepo struct{ tx *database.TxManager }

func NewActivityLogRepository(tx *database.TxManager) ActivityLogRepository {
	return &pgActivityLogRepo{tx: tx}
}

func toActivityLog(a sqlc.ActivityLog) ActivityLog {
	return ActivityLog{UUID: a.Uuid, ActionQuery: a.ActionQuery, IPAddress: a.IpAddress, LogAction: a.LogAction,
		LogDescription: a.LogDescription, LogSessionCode: a.LogSessionCode, LogTime: a.LogTime, LogTitle: a.LogTitle,
		RoleCode: a.RoleCode, Username: a.Username}
}

func (r *pgActivityLogRepo) GetByUUID(ctx context.Context, id uuid.UUID) (ActivityLog, error) {
	row, err := r.tx.Queries(ctx).GetActivityLogByUUID(ctx, id)
	if err != nil {
		return ActivityLog{}, database.MapError("get activity log", err)
	}
	return toActivityLog(row), nil
}

func (r *pgActivityLogRepo) List(ctx context.Context, f ActivityLogFilter, p Paging) ([]ActivityLog, int64, error) {
	q := r.tx.Queries(ctx)
	rows, err := q.ListActivityLogs(ctx, sqlc.ListActivityLogsParams{Username: f.Username, LogAction: f.LogAction,
		From: f.From, To: f.To, Sort: p.Sort, Limit: p.Limit, Offset: p.Offset})
	if err != nil {
		return nil, 0, database.MapError("list activity logs", err)
	}
	total, err := q.CountActivityLogs(ctx, sqlc.CountActivityLogsParams{Username: f.Username, LogAction: f.LogAction, From: f.From, To: f.To})
	if err != nil {
		return nil, 0, database.MapError("count activity logs", err)
	}
	out := make([]ActivityLog, 0, len(rows))
	for _, row := range rows {
		out = append(out, toActivityLog(row))
	}
	return out, total, nil
}

// ---------------------------------------------------------------- clients

type pgClientLookup struct{ tx *database.TxManager }

func NewClientLookup(tx *database.TxManager) ClientLookup { return &pgClientLookup{tx: tx} }

func (r *pgClientLookup) ClientIDByUUID(ctx context.Context, id uuid.UUID) (int64, error) {
	cid, err := r.tx.Queries(ctx).GetClientIDByUUID(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, apperr.Invalid("client_uuid", "exists", "client tidak ditemukan")
		}
		return 0, database.MapError("get client", err)
	}
	return cid, nil
}
