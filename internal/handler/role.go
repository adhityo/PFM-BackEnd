package handler

import (
	"github.com/gofiber/fiber/v3"

	"pfm-backend/internal/dto"
	"pfm-backend/internal/permission"
	"pfm-backend/internal/platform/auth"
	"pfm-backend/internal/platform/httpx"
	"pfm-backend/internal/service"
)

// RoleHandler serves /roles and /roles/{uuid}/actions.
type RoleHandler struct {
	v     httpx.Validator
	roles *service.RoleService
}

func NewRoleHandler(v httpx.Validator, roles *service.RoleService) *RoleHandler {
	return &RoleHandler{v: v, roles: roles}
}

func (h *RoleHandler) Register(r *auth.Router) {
	g := r.Group("/roles")
	g.Get("", permission.RoleRead, h.List)
	g.Post("", permission.RoleCreate, h.Create)
	g.Get("/:uuid", permission.RoleRead, h.Get)
	g.Put("/:uuid", permission.RoleUpdate, h.Update)
	g.Delete("/:uuid", permission.RoleDelete, h.Delete)
	g.Get("/:uuid/actions", permission.RoleActionRead, h.GetActions)
	g.Put("/:uuid/actions", permission.RoleActionUpdate, h.PutActions)
}

func (h *RoleHandler) List(c fiber.Ctx) error {
	q, err := httpx.ParseListQuery(c, service.RoleSorts, "role_code", "q")
	if err != nil {
		return err
	}
	items, total, err := h.roles.List(c.Context(), q.Like("q"), paging(q))
	if err != nil {
		return err
	}
	return httpx.List(c, items, q.Page, total)
}

func (h *RoleHandler) Get(c fiber.Ctx) error {
	id, err := httpx.UUIDParam(c, "uuid")
	if err != nil {
		return err
	}
	r, err := h.roles.Get(c.Context(), id)
	if err != nil {
		return err
	}
	httpx.SetETag(c, r.UpdatedAt)
	return httpx.OK(c, dto.RoleFromModel(r))
}

func (h *RoleHandler) Create(c fiber.Ctx) error {
	var req dto.RoleRequest
	if err := httpx.BindJSON(c, h.v, &req); err != nil {
		return err
	}
	r, err := h.roles.Create(c.Context(), req)
	if err != nil {
		return err
	}
	httpx.SetETag(c, r.UpdatedAt)
	return httpx.Created(c, "/api/v1/roles/"+r.UUID.String(), dto.RoleFromModel(r))
}

func (h *RoleHandler) Update(c fiber.Ctx) error {
	id, err := httpx.UUIDParam(c, "uuid")
	if err != nil {
		return err
	}
	expected, err := httpx.IfMatch(c)
	if err != nil {
		return err
	}
	var req dto.RoleRequest
	if err := httpx.BindJSON(c, h.v, &req); err != nil {
		return err
	}
	r, err := h.roles.Update(c.Context(), id, req, expected)
	if err != nil {
		return err
	}
	httpx.SetETag(c, r.UpdatedAt)
	return httpx.OK(c, dto.RoleFromModel(r))
}

func (h *RoleHandler) Delete(c fiber.Ctx) error {
	id, err := httpx.UUIDParam(c, "uuid")
	if err != nil {
		return err
	}
	expected, err := httpx.IfMatch(c)
	if err != nil {
		return err
	}
	if err := h.roles.Delete(c.Context(), id, expected); err != nil {
		return err
	}
	return httpx.NoContent(c)
}

func (h *RoleHandler) GetActions(c fiber.Ctx) error {
	id, err := httpx.UUIDParam(c, "uuid")
	if err != nil {
		return err
	}
	r, actions, err := h.roles.Actions(c.Context(), id)
	if err != nil {
		return err
	}
	httpx.SetETag(c, r.UpdatedAt)
	return httpx.OK(c, dto.RoleActionsFromModel(r, actions))
}

func (h *RoleHandler) PutActions(c fiber.Ctx) error {
	id, err := httpx.UUIDParam(c, "uuid")
	if err != nil {
		return err
	}
	expected, err := httpx.IfMatch(c)
	if err != nil {
		return err
	}
	var req dto.RoleActionsRequest
	if err := httpx.BindJSON(c, h.v, &req); err != nil {
		return err
	}
	r, actions, err := h.roles.ReplaceActions(c.Context(), id, req, expected)
	if err != nil {
		return err
	}
	httpx.SetETag(c, r.UpdatedAt)
	return httpx.OK(c, dto.RoleActionsFromModel(r, actions))
}
