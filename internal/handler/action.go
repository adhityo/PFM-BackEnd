package handler

import (
	"github.com/gofiber/fiber/v3"

	"pfm-backend/internal/dto"
	"pfm-backend/internal/model"
	"pfm-backend/internal/permission"
	"pfm-backend/internal/platform/auth"
	"pfm-backend/internal/platform/httpx"
	"pfm-backend/internal/service"
)

// ActionHandler serves /actions (the permission catalog).
type ActionHandler struct {
	v       httpx.Validator
	actions *service.ActionService
}

func NewActionHandler(v httpx.Validator, actions *service.ActionService) *ActionHandler {
	return &ActionHandler{v: v, actions: actions}
}

func (h *ActionHandler) Register(r *auth.Router) {
	g := r.Group("/actions")
	g.Get("", permission.ActionRead, h.List)
	g.Post("", permission.ActionCreate, h.Create)
	g.Get("/:uuid", permission.ActionRead, h.Get)
	g.Put("/:uuid", permission.ActionUpdate, h.Update)
	g.Delete("/:uuid", permission.ActionDelete, h.Delete)
	// Grouped catalog for the role permission editor (module -> resource -> action).
	r.Get("/permission-catalog", permission.RoleActionRead, h.Catalog)
}

func (h *ActionHandler) Catalog(c fiber.Ctx) error {
	out, err := h.actions.Catalog(c.Context())
	if err != nil {
		return err
	}
	return httpx.OK(c, out)
}

func (h *ActionHandler) List(c fiber.Ctx) error {
	q, err := httpx.ParseListQuery(c, service.ActionSorts, "action_code", "q", "module")
	if err != nil {
		return err
	}
	items, total, err := h.actions.List(c.Context(), model.ActionFilter{Q: q.Like("q"), Module: q.Text("module")}, paging(q))
	if err != nil {
		return err
	}
	return httpx.List(c, items, q.Page, total)
}

func (h *ActionHandler) Get(c fiber.Ctx) error {
	id, err := httpx.UUIDParam(c, "uuid")
	if err != nil {
		return err
	}
	a, err := h.actions.Get(c.Context(), id)
	if err != nil {
		return err
	}
	httpx.SetETag(c, a.UpdatedAt)
	return httpx.OK(c, dto.ActionFromModel(a))
}

func (h *ActionHandler) Create(c fiber.Ctx) error {
	var req dto.ActionRequest
	if err := httpx.BindJSON(c, h.v, &req); err != nil {
		return err
	}
	a, err := h.actions.Create(c.Context(), req)
	if err != nil {
		return err
	}
	httpx.SetETag(c, a.UpdatedAt)
	return httpx.Created(c, "/api/v1/actions/"+a.UUID.String(), dto.ActionFromModel(a))
}

func (h *ActionHandler) Update(c fiber.Ctx) error {
	id, err := httpx.UUIDParam(c, "uuid")
	if err != nil {
		return err
	}
	expected, err := httpx.IfMatch(c)
	if err != nil {
		return err
	}
	var req dto.ActionRequest
	if err := httpx.BindJSON(c, h.v, &req); err != nil {
		return err
	}
	a, err := h.actions.Update(c.Context(), id, req, expected)
	if err != nil {
		return err
	}
	httpx.SetETag(c, a.UpdatedAt)
	return httpx.OK(c, dto.ActionFromModel(a))
}

func (h *ActionHandler) Delete(c fiber.Ctx) error {
	id, err := httpx.UUIDParam(c, "uuid")
	if err != nil {
		return err
	}
	expected, err := httpx.IfMatch(c)
	if err != nil {
		return err
	}
	if err := h.actions.Delete(c.Context(), id, expected); err != nil {
		return err
	}
	return httpx.NoContent(c)
}
