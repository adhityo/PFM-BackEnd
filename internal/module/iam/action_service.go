package iam

import (
	"context"
	"time"

	"github.com/google/uuid"

	"pfm-backend/internal/apperr"
	"pfm-backend/internal/platform/audit"
	"pfm-backend/internal/platform/auth"
	"pfm-backend/internal/platform/database"
)

// ActionService manages the permission catalog.
type ActionService struct {
	tx      *database.TxManager
	actions ActionRepository
	roles   RoleRepository
	audit   *audit.Recorder
}

func NewActionService(tx *database.TxManager, actions ActionRepository, roles RoleRepository, rec *audit.Recorder) *ActionService {
	return &ActionService{tx: tx, actions: actions, roles: roles, audit: rec}
}

// ActionSorts is the sort whitelist for action lists.
var ActionSorts = []string{"action_code", "-action_code", "-created_at"}

func (s *ActionService) List(ctx context.Context, q *string, p Paging) ([]ActionResponse, int64, error) {
	actions, total, err := s.actions.List(ctx, q, p)
	if err != nil {
		return nil, 0, err
	}
	out := make([]ActionResponse, 0, len(actions))
	for _, a := range actions {
		out = append(out, toActionResponse(a))
	}
	return out, total, nil
}

func (s *ActionService) Get(ctx context.Context, id uuid.UUID) (Action, error) {
	return s.actions.GetByUUID(ctx, id)
}

func (s *ActionService) Create(ctx context.Context, in ActionRequest) (Action, error) {
	actor, err := auth.MustActor(ctx)
	if err != nil {
		return Action{}, err
	}
	var out Action
	err = s.tx.WithinTx(ctx, func(ctx context.Context) error {
		out, err = s.actions.Create(ctx, ActionInput{ActionCode: in.ActionCode, ActionName: in.ActionName,
			Description: optional(in.Description), Actor: actor.AuditName()})
		if err != nil {
			return err
		}
		return s.audit.Record(ctx, audit.Entry{Action: "action.create", Entity: "action", EntityUUID: out.UUID,
			Description: "code=" + out.ActionCode, Route: "POST /actions"})
	})
	return out, err
}

func (s *ActionService) Update(ctx context.Context, id uuid.UUID, in ActionRequest, expected time.Time) (Action, error) {
	actor, err := auth.MustActor(ctx)
	if err != nil {
		return Action{}, err
	}
	var out Action
	err = s.tx.WithinTx(ctx, func(ctx context.Context) error {
		cur, err := s.actions.GetByUUID(ctx, id)
		if err != nil {
			return err
		}
		if cur.ActionCode != in.ActionCode {
			n, err := s.actions.CountRoleUsage(ctx, cur.ID)
			if err != nil {
				return err
			}
			if n > 0 {
				return apperr.Conflict("kode action yang sudah dipakai role tidak dapat diubah")
			}
		}
		out, err = s.actions.Update(ctx, id, ActionInput{ActionCode: in.ActionCode, ActionName: in.ActionName,
			Description: optional(in.Description), Actor: actor.AuditName()}, expected)
		if err != nil {
			return err
		}
		return s.audit.Record(ctx, audit.Entry{Action: "action.update", Entity: "action", EntityUUID: id,
			Description: "code=" + out.ActionCode, Route: "PUT /actions/{uuid}"})
	})
	return out, err
}

func (s *ActionService) Delete(ctx context.Context, id uuid.UUID, expected time.Time) error {
	actor, err := auth.MustActor(ctx)
	if err != nil {
		return err
	}
	return s.tx.WithinTx(ctx, func(ctx context.Context) error {
		cur, err := s.actions.GetByUUID(ctx, id)
		if err != nil {
			return err
		}
		n, err := s.actions.CountRoleUsage(ctx, cur.ID)
		if err != nil {
			return err
		}
		if n > 0 {
			return apperr.Conflict("action masih dipakai oleh role")
		}
		if err := s.actions.SoftDelete(ctx, id, actor.AuditName(), expected); err != nil {
			return err
		}
		return s.audit.Record(ctx, audit.Entry{Action: "action.delete", Entity: "action", EntityUUID: id,
			Route: "DELETE /actions/{uuid}"})
	})
}

// SyncCatalog makes sure every permission known to the code exists in `action`
// and that the default roles exist. Used by `cmd/migrate seed`.
func SyncCatalog(ctx context.Context, tx *database.TxManager, actions ActionRepository, roles RoleRepository, catalog []auth.Permission) error {
	return tx.WithinTx(ctx, func(ctx context.Context) error {
		for _, p := range catalog {
			if err := actions.EnsureCode(ctx, p.Code, p.Name, "system"); err != nil {
				return err
			}
		}
		for _, r := range DefaultRoles {
			if _, err := roles.UpsertByCode(ctx, r.Code, r.Name, "system"); err != nil {
				return err
			}
		}
		return nil
	})
}

// ActivityLogService reads the audit trail.
type ActivityLogService struct {
	logs ActivityLogRepository
}

func NewActivityLogService(logs ActivityLogRepository) *ActivityLogService {
	return &ActivityLogService{logs: logs}
}

// ActivityLogSorts is the sort whitelist for activity logs.
var ActivityLogSorts = []string{"-log_time", "log_time"}

func (s *ActivityLogService) List(ctx context.Context, f ActivityLogFilter, p Paging) ([]ActivityLogResponse, int64, error) {
	logs, total, err := s.logs.List(ctx, f, p)
	if err != nil {
		return nil, 0, err
	}
	out := make([]ActivityLogResponse, 0, len(logs))
	for _, l := range logs {
		out = append(out, toActivityLogResponse(l))
	}
	return out, total, nil
}

func (s *ActivityLogService) Get(ctx context.Context, id uuid.UUID) (ActivityLogResponse, error) {
	l, err := s.logs.GetByUUID(ctx, id)
	if err != nil {
		return ActivityLogResponse{}, err
	}
	return toActivityLogResponse(l), nil
}
