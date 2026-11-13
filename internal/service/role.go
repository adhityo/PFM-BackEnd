package service

import (
	"context"
	"strconv"
	"time"

	"github.com/google/uuid"

	"pfm-backend/internal/apperr"
	"pfm-backend/internal/dto"
	"pfm-backend/internal/model"
	"pfm-backend/internal/platform/audit"
	"pfm-backend/internal/platform/auth"
	"pfm-backend/internal/platform/crud"
	"pfm-backend/internal/platform/database"
	"pfm-backend/internal/repository"
)

// RoleService manages roles and their permissions.
type RoleService struct {
	tx      *database.TxManager
	roles   repository.RoleRepository
	actions repository.ActionRepository
	users   repository.UserRepository
	cache   *PermissionCache
	audit   *audit.Recorder
}

func NewRoleService(tx *database.TxManager, roles repository.RoleRepository, actions repository.ActionRepository, users repository.UserRepository,
	cache *PermissionCache, rec *audit.Recorder) *RoleService {
	return &RoleService{tx: tx, roles: roles, actions: actions, users: users, cache: cache, audit: rec}
}

// RoleSorts is the sort whitelist for role lists.
var RoleSorts = []string{"role_code", "-role_code", "role_name", "-role_name", "-created_at"}

func (s *RoleService) List(ctx context.Context, q *string, p model.Paging) ([]dto.RoleResponse, int64, error) {
	roles, total, err := s.roles.List(ctx, q, p)
	if err != nil {
		return nil, 0, err
	}
	out := make([]dto.RoleResponse, 0, len(roles))
	for _, r := range roles {
		out = append(out, dto.RoleFromModel(r))
	}
	return out, total, nil
}

func (s *RoleService) Get(ctx context.Context, id uuid.UUID) (model.Role, error) {
	return s.roles.GetByUUID(ctx, id)
}

func roleInput(in dto.RoleRequest, actor auth.Actor) model.RoleInput {
	return model.RoleInput{RoleCode: in.RoleCode, RoleName: in.RoleName, Description: crud.Text(in.Description),
		MenuURL: crud.Text(in.MenuURL), Status: model.StatusCode(in.Status), Actor: actor.AuditName()}
}

func (s *RoleService) Create(ctx context.Context, in dto.RoleRequest) (model.Role, error) {
	actor, err := auth.MustActor(ctx)
	if err != nil {
		return model.Role{}, err
	}
	var out model.Role
	err = s.tx.WithinTx(ctx, func(ctx context.Context) error {
		out, err = s.roles.Create(ctx, roleInput(in, actor))
		if err != nil {
			return err
		}
		return s.audit.Record(ctx, audit.Entry{Action: "role.create", Entity: "role", EntityUUID: out.UUID,
			Description: "code=" + out.RoleCode, Route: "POST /roles"})
	})
	return out, err
}

func (s *RoleService) Update(ctx context.Context, id uuid.UUID, in dto.RoleRequest, expected time.Time) (model.Role, error) {
	actor, err := auth.MustActor(ctx)
	if err != nil {
		return model.Role{}, err
	}
	var out model.Role
	err = s.tx.WithinTx(ctx, func(ctx context.Context) error {
		cur, err := s.roles.GetByUUID(ctx, id)
		if err != nil {
			return err
		}
		if cur.RoleCode == auth.RoleAdmin && (in.RoleCode != auth.RoleAdmin || in.Status != "active") {
			return apperr.Conflict("kode dan status role ADMIN tidak dapat diubah")
		}
		out, err = s.roles.Update(ctx, id, roleInput(in, actor), expected)
		if err != nil {
			return err
		}
		return s.audit.Record(ctx, audit.Entry{Action: "role.update", Entity: "role", EntityUUID: id,
			Description: "code=" + out.RoleCode, Route: "PUT /roles/{uuid}"})
	})
	return out, err
}

func (s *RoleService) Delete(ctx context.Context, id uuid.UUID, expected time.Time) error {
	actor, err := auth.MustActor(ctx)
	if err != nil {
		return err
	}
	return s.tx.WithinTx(ctx, func(ctx context.Context) error {
		cur, err := s.roles.GetByUUIDForUpdate(ctx, id)
		if err != nil {
			return err
		}
		if cur.RoleCode == auth.RoleAdmin {
			return apperr.Conflict("role ADMIN tidak dapat dihapus")
		}
		n, err := s.users.CountByRole(ctx, cur.ID)
		if err != nil {
			return err
		}
		if n > 0 {
			return apperr.Conflict("role masih dipakai oleh pengguna aktif")
		}
		if err := s.roles.SoftDelete(ctx, id, actor.AuditName(), expected); err != nil {
			return err
		}
		s.cache.Invalidate(cur.ID)
		return s.audit.Record(ctx, audit.Entry{Action: "role.delete", Entity: "role", EntityUUID: id,
			Route: "DELETE /roles/{uuid}"})
	})
}

func (s *RoleService) Actions(ctx context.Context, id uuid.UUID) (model.Role, []model.Action, error) {
	role, err := s.roles.GetByUUID(ctx, id)
	if err != nil {
		return model.Role{}, nil, err
	}
	actions, err := s.roles.Actions(ctx, role.ID)
	return role, actions, err
}

// ReplaceActions replaces the full permission set of a role in one transaction.
func (s *RoleService) ReplaceActions(ctx context.Context, id uuid.UUID, in dto.RoleActionsRequest, expected time.Time) (model.Role, []model.Action, error) {
	actor, err := auth.MustActor(ctx)
	if err != nil {
		return model.Role{}, nil, err
	}
	ids := make([]uuid.UUID, 0, len(in.ActionUUIDs))
	for _, s := range in.ActionUUIDs {
		u, _ := uuid.Parse(s)
		ids = append(ids, u)
	}
	var (
		role    model.Role
		actions []model.Action
	)
	err = s.tx.WithinTx(ctx, func(ctx context.Context) error {
		role, err = s.roles.GetByUUIDForUpdate(ctx, id)
		if err != nil {
			return err
		}
		if !role.UpdatedAt.Equal(expected) {
			return apperr.ErrPreconditionFailed
		}
		if role.RoleCode == auth.RoleAdmin {
			return apperr.Conflict("role ADMIN otomatis memiliki semua permission")
		}
		found, err := s.actions.GetByUUIDs(ctx, ids)
		if err != nil {
			return err
		}
		if len(found) != len(ids) {
			return apperr.Invalid("action_uuids", "exists", "sebagian action tidak ditemukan")
		}
		if err := s.roles.ReplaceActions(ctx, role, found, actor.AuditName()); err != nil {
			return err
		}
		if role, err = s.roles.GetByUUID(ctx, id); err != nil {
			return err
		}
		if actions, err = s.roles.Actions(ctx, role.ID); err != nil {
			return err
		}
		return s.audit.Record(ctx, audit.Entry{Action: "role_action.update", Entity: "role", EntityUUID: id,
			Description: "jumlah permission=" + strconv.Itoa(len(actions)), Route: "PUT /roles/{uuid}/actions"})
	})
	if err != nil {
		return model.Role{}, nil, err
	}
	s.cache.Invalidate(role.ID)
	return role, actions, nil
}
