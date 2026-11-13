package handler

import (
	"github.com/gofiber/fiber/v3"

	"pfm-backend/internal/apperr"
	"pfm-backend/internal/dto"
	"pfm-backend/internal/model"
	"pfm-backend/internal/permission"
	"pfm-backend/internal/platform/auth"
	"pfm-backend/internal/platform/httpx"
	"pfm-backend/internal/service"
)

// UserHandler serves /users.
type UserHandler struct {
	v     httpx.Validator
	users *service.UserService
	roles *service.RoleService
}

func NewUserHandler(v httpx.Validator, users *service.UserService, roles *service.RoleService) *UserHandler {
	return &UserHandler{v: v, users: users, roles: roles}
}

func (h *UserHandler) Register(r *auth.Router) {
	u := r.Group("/users")
	u.Get("", permission.UserRead, h.List)
	u.Post("", permission.UserCreate, h.Create)
	u.Get("/:uuid", permission.UserRead, h.Get)
	u.Put("/:uuid", permission.UserUpdate, h.Update)
	u.Delete("/:uuid", permission.UserDelete, h.Delete)
	u.Post("/:uuid/reset-password", permission.UserResetPassword, h.ResetPassword)
	u.Post("/:uuid/lock", permission.UserLock, h.lock(true))
	u.Post("/:uuid/unlock", permission.UserLock, h.lock(false))
}

func (h *UserHandler) List(c fiber.Ctx) error {
	q, err := httpx.ParseListQuery(c, service.UserSorts, "-created_at", "q", "role_uuid", "status")
	if err != nil {
		return err
	}
	f := model.UserFilter{Q: q.Like("q")}
	roleUUID, err := q.UUID("role_uuid")
	if err != nil {
		return err
	}
	if roleUUID != nil {
		role, err := h.roles.Get(c.Context(), *roleUUID)
		if err != nil {
			return httpx.List(c, []dto.UserResponse{}, q.Page, 0)
		}
		f.RoleID = &role.ID
	}
	if st := q.Text("status"); st != nil {
		if *st != "active" && *st != "inactive" {
			return apperr.Invalid("status", "oneof", "harus salah satu dari: active, inactive")
		}
		code := model.StatusCode(*st)
		f.Status = &code
	}
	items, total, err := h.users.List(c.Context(), f, paging(q))
	if err != nil {
		return err
	}
	return httpx.List(c, items, q.Page, total)
}

func (h *UserHandler) Get(c fiber.Ctx) error {
	id, err := httpx.UUIDParam(c, "uuid")
	if err != nil {
		return err
	}
	u, err := h.users.Get(c.Context(), id)
	if err != nil {
		return err
	}
	httpx.SetETag(c, u.UpdatedAt)
	return httpx.OK(c, h.users.Response(u))
}

func (h *UserHandler) Create(c fiber.Ctx) error {
	var req dto.CreateUserRequest
	if err := httpx.BindJSON(c, h.v, &req); err != nil {
		return err
	}
	res, err := h.users.Create(c.Context(), req)
	if err != nil {
		return err
	}
	httpx.SetETag(c, res.User.UpdatedAt)
	return httpx.Created(c, "/api/v1/users/"+res.User.UUID.String(), res)
}

func (h *UserHandler) Update(c fiber.Ctx) error {
	id, err := httpx.UUIDParam(c, "uuid")
	if err != nil {
		return err
	}
	expected, err := httpx.IfMatch(c)
	if err != nil {
		return err
	}
	var req dto.UpdateUserRequest
	if err := httpx.BindJSON(c, h.v, &req); err != nil {
		return err
	}
	u, err := h.users.Update(c.Context(), id, req, expected)
	if err != nil {
		return err
	}
	httpx.SetETag(c, u.UpdatedAt)
	return httpx.OK(c, h.users.Response(u))
}

func (h *UserHandler) Delete(c fiber.Ctx) error {
	id, err := httpx.UUIDParam(c, "uuid")
	if err != nil {
		return err
	}
	expected, err := httpx.IfMatch(c)
	if err != nil {
		return err
	}
	if err := h.users.Delete(c.Context(), id, expected); err != nil {
		return err
	}
	return httpx.NoContent(c)
}

func (h *UserHandler) ResetPassword(c fiber.Ctx) error {
	id, err := httpx.UUIDParam(c, "uuid")
	if err != nil {
		return err
	}
	expected, err := httpx.IfMatch(c)
	if err != nil {
		return err
	}
	res, err := h.users.ResetCredential(c.Context(), id, expected)
	if err != nil {
		return err
	}
	httpx.SetETag(c, res.User.UpdatedAt)
	return httpx.OK(c, res)
}

func (h *UserHandler) lock(lock bool) fiber.Handler {
	return func(c fiber.Ctx) error {
		id, err := httpx.UUIDParam(c, "uuid")
		if err != nil {
			return err
		}
		expected, err := httpx.IfMatch(c)
		if err != nil {
			return err
		}
		u, err := h.users.SetLock(c.Context(), id, lock, expected)
		if err != nil {
			return err
		}
		httpx.SetETag(c, u.UpdatedAt)
		return httpx.OK(c, h.users.Response(u))
	}
}
