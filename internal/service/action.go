package service

import (
	"cmp"
	"context"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"pfm-backend/internal/apperr"
	"pfm-backend/internal/dto"
	"pfm-backend/internal/model"
	"pfm-backend/internal/permission"
	"pfm-backend/internal/platform/audit"
	"pfm-backend/internal/platform/auth"
	"pfm-backend/internal/platform/crud"
	"pfm-backend/internal/platform/database"
	"pfm-backend/internal/repository"
)

// ActionService manages the permission catalog. System actions mirror the
// code catalog (route guards depend on them) and are read-only through the
// API; custom actions (module "custom") are free for frontend-only switches.
type ActionService struct {
	tx      *database.TxManager
	actions repository.ActionRepository
	roles   repository.RoleRepository
	cache   *PermissionCache
	audit   *audit.Recorder
}

func NewActionService(tx *database.TxManager, actions repository.ActionRepository, roles repository.RoleRepository,
	cache *PermissionCache, rec *audit.Recorder) *ActionService {
	return &ActionService{tx: tx, actions: actions, roles: roles, cache: cache, audit: rec}
}

// ActionSorts is the sort whitelist for action lists.
var ActionSorts = []string{"action_code", "-action_code", "-created_at"}

func (s *ActionService) List(ctx context.Context, f model.ActionFilter, p model.Paging) ([]dto.ActionResponse, int64, error) {
	actions, total, err := s.actions.List(ctx, f, p)
	if err != nil {
		return nil, 0, err
	}
	out := make([]dto.ActionResponse, 0, len(actions))
	for _, a := range actions {
		out = append(out, dto.ActionFromModel(a))
	}
	return out, total, nil
}

func (s *ActionService) Get(ctx context.Context, id uuid.UUID) (model.Action, error) {
	return s.actions.GetByUUID(ctx, id)
}

// verbOrder puts the CRUD verbs first, in their usual order.
var verbOrder = map[string]int{"read": 0, "create": 1, "update": 2, "delete": 3}

// Catalog groups every action module -> resource -> action (role editor).
func (s *ActionService) Catalog(ctx context.Context) (dto.PermissionCatalogResponse, error) {
	all, err := s.actions.Catalog(ctx)
	if err != nil {
		return dto.PermissionCatalogResponse{}, err
	}
	set, err := s.cache.AllPermissions(ctx)
	if err != nil {
		return dto.PermissionCatalogResponse{}, err
	}
	known := map[string]bool{}
	for _, m := range permission.Modules {
		known[m.Code] = true
	}
	byModule := map[string]map[string]*dto.PermissionResource{}
	for _, a := range all {
		module := a.Module
		if !known[module] {
			module = permission.ModuleCustom
		}
		resource, verb, _ := strings.Cut(a.ActionCode, ":")
		if byModule[module] == nil {
			byModule[module] = map[string]*dto.PermissionResource{}
		}
		r := byModule[module][resource]
		if r == nil {
			label := resource
			if a.ResourceLabel != nil && *a.ResourceLabel != "" {
				label = *a.ResourceLabel
			}
			r = &dto.PermissionResource{Code: resource, Label: label}
			byModule[module][resource] = r
		}
		r.Actions = append(r.Actions, dto.PermissionAction{UUID: a.UUID, Code: a.ActionCode, Verb: verb, Name: a.ActionName,
			IsSystem: a.IsSystem})
	}
	out := dto.PermissionCatalogResponse{Version: set.Version(), Modules: []dto.PermissionModule{}}
	for _, m := range permission.Modules {
		resources := byModule[m.Code]
		if len(resources) == 0 {
			continue
		}
		mod := dto.PermissionModule{Code: m.Code, Label: m.Label}
		for _, r := range resources {
			slices.SortFunc(r.Actions, func(a, b dto.PermissionAction) int {
				oa, okA := verbOrder[a.Verb]
				ob, okB := verbOrder[b.Verb]
				switch {
				case okA && okB:
					return cmp.Compare(oa, ob)
				case okA:
					return -1
				case okB:
					return 1
				}
				return cmp.Compare(a.Verb, b.Verb)
			})
			mod.Resources = append(mod.Resources, *r)
		}
		slices.SortFunc(mod.Resources, func(a, b dto.PermissionResource) int { return cmp.Compare(a.Label, b.Label) })
		out.Modules = append(out.Modules, mod)
	}
	return out, nil
}

func actionInput(in dto.ActionRequest, actor auth.Actor) model.ActionInput {
	return model.ActionInput{ActionCode: in.ActionCode, ActionName: in.ActionName, Description: crud.Text(in.Description),
		ResourceLabel: crud.Text(in.ResourceLabel), Actor: actor.AuditName()}
}

// Create adds a custom action (module "custom").
func (s *ActionService) Create(ctx context.Context, in dto.ActionRequest) (model.Action, error) {
	actor, err := auth.MustActor(ctx)
	if err != nil {
		return model.Action{}, err
	}
	var out model.Action
	err = s.tx.WithinTx(ctx, func(ctx context.Context) error {
		out, err = s.actions.Create(ctx, actionInput(in, actor))
		if err != nil {
			return err
		}
		return s.audit.Record(ctx, audit.Entry{Action: "action.create", Entity: "action", EntityUUID: out.UUID,
			Description: "code=" + out.ActionCode, Route: "POST /actions"})
	})
	if err == nil {
		s.cache.InvalidateCatalog()
	}
	return out, err
}

func (s *ActionService) Update(ctx context.Context, id uuid.UUID, in dto.ActionRequest, expected time.Time) (model.Action, error) {
	actor, err := auth.MustActor(ctx)
	if err != nil {
		return model.Action{}, err
	}
	var out model.Action
	err = s.tx.WithinTx(ctx, func(ctx context.Context) error {
		cur, err := s.actions.GetByUUID(ctx, id)
		if err != nil {
			return err
		}
		if cur.IsSystem {
			return apperr.Conflict("permission sistem tidak dapat diubah; nama & label diatur oleh kode (migrate seed)")
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
		out, err = s.actions.Update(ctx, id, actionInput(in, actor), expected)
		if err != nil {
			return err
		}
		return s.audit.Record(ctx, audit.Entry{Action: "action.update", Entity: "action", EntityUUID: id,
			Description: "code=" + out.ActionCode, Route: "PUT /actions/{uuid}"})
	})
	if err == nil {
		s.cache.InvalidateCatalog()
	}
	return out, err
}

func (s *ActionService) Delete(ctx context.Context, id uuid.UUID, expected time.Time) error {
	actor, err := auth.MustActor(ctx)
	if err != nil {
		return err
	}
	err = s.tx.WithinTx(ctx, func(ctx context.Context) error {
		cur, err := s.actions.GetByUUID(ctx, id)
		if err != nil {
			return err
		}
		if cur.IsSystem {
			return apperr.Conflict("permission sistem tidak dapat dihapus")
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
	if err == nil {
		s.cache.InvalidateCatalog()
	}
	return err
}
