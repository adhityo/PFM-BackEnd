package iam

import (
	"net/netip"

	"github.com/gofiber/fiber/v3"

	"pfm-backend/internal/platform/httpx"
)

// Handler exposes the IAM HTTP API.
type Handler struct {
	v       httpx.Validator
	auth    *AuthService
	users   *UserService
	roles   *RoleService
	actions *ActionService
	logs    *ActivityLogService
}

func NewHandler(v httpx.Validator, a *AuthService, u *UserService, r *RoleService, ac *ActionService, l *ActivityLogService) *Handler {
	return &Handler{v: v, auth: a, users: u, roles: r, actions: ac, logs: l}
}

func clientInfo(c fiber.Ctx) ClientInfo {
	ci := ClientInfo{UserAgent: c.Get(fiber.HeaderUserAgent), RequestID: httpx.RequestID(c)}
	if len(ci.UserAgent) > 255 {
		ci.UserAgent = ci.UserAgent[:255]
	}
	if ip, err := netip.ParseAddr(c.IP()); err == nil {
		ci.IP = &ip
	}
	return ci
}

func paging(q httpx.ListQuery) Paging {
	return Paging{Sort: q.Sort, Limit: q.Page.Limit(), Offset: q.Page.Offset()}
}

// ---------------------------------------------------------------- auth

func (h *Handler) Login(c fiber.Ctx) error {
	var req LoginRequest
	if err := httpx.BindJSON(c, h.v, &req); err != nil {
		return err
	}
	res, err := h.auth.Login(c.Context(), req, clientInfo(c))
	if err != nil {
		return err
	}
	return httpx.OK(c, res)
}

func (h *Handler) Refresh(c fiber.Ctx) error {
	var req RefreshRequest
	if err := httpx.BindJSON(c, h.v, &req); err != nil {
		return err
	}
	res, err := h.auth.Refresh(c.Context(), req.RefreshToken, clientInfo(c))
	if err != nil {
		return err
	}
	return httpx.OK(c, res)
}

func (h *Handler) Logout(c fiber.Ctx) error {
	var req RefreshRequest
	if err := httpx.BindJSON(c, h.v, &req); err != nil {
		return err
	}
	if err := h.auth.Logout(c.Context(), req.RefreshToken); err != nil {
		return err
	}
	return httpx.NoContent(c)
}

func (h *Handler) LogoutAll(c fiber.Ctx) error {
	if err := h.auth.LogoutAll(c.Context()); err != nil {
		return err
	}
	return httpx.NoContent(c)
}

func (h *Handler) Activate(c fiber.Ctx) error {
	var req ActivateRequest
	if err := httpx.BindJSON(c, h.v, &req); err != nil {
		return err
	}
	if err := h.auth.Activate(c.Context(), req, clientInfo(c)); err != nil {
		return err
	}
	return httpx.NoContent(c)
}

func (h *Handler) ChangePassword(c fiber.Ctx) error {
	var req ChangePasswordRequest
	if err := httpx.BindJSON(c, h.v, &req); err != nil {
		return err
	}
	if err := h.auth.ChangePassword(c.Context(), req); err != nil {
		return err
	}
	return httpx.NoContent(c)
}

func (h *Handler) Me(c fiber.Ctx) error {
	res, err := h.auth.Me(c.Context())
	if err != nil {
		return err
	}
	return httpx.OK(c, res)
}

// ---------------------------------------------------------------- users

func (h *Handler) ListUsers(c fiber.Ctx) error {
	q, err := httpx.ParseListQuery(c, UserSorts, "-created_at", "q", "role_uuid", "status")
	if err != nil {
		return err
	}
	f := UserFilter{Q: q.Like("q")}
	if roleUUID, err := q.UUID("role_uuid"); err != nil {
		return err
	} else if roleUUID != nil {
		role, err := h.roles.Get(c.Context(), *roleUUID)
		if err != nil {
			return httpx.List(c, []UserResponse{}, q.Page, 0)
		}
		f.RoleID = &role.ID
	}
	if st := q.Text("status"); st != nil {
		switch *st {
		case "active", "inactive":
			code := statusCode(*st)
			f.Status = &code
		default:
			return invalidStatusFilter()
		}
	}
	items, total, err := h.users.List(c.Context(), f, paging(q))
	if err != nil {
		return err
	}
	return httpx.List(c, items, q.Page, total)
}

func (h *Handler) GetUser(c fiber.Ctx) error {
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

func (h *Handler) CreateUser(c fiber.Ctx) error {
	var req CreateUserRequest
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

func (h *Handler) UpdateUser(c fiber.Ctx) error {
	id, err := httpx.UUIDParam(c, "uuid")
	if err != nil {
		return err
	}
	expected, err := httpx.IfMatch(c)
	if err != nil {
		return err
	}
	var req UpdateUserRequest
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

func (h *Handler) DeleteUser(c fiber.Ctx) error {
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

func (h *Handler) ResetUserPassword(c fiber.Ctx) error {
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

func (h *Handler) lockUser(lock bool) fiber.Handler {
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

// ---------------------------------------------------------------- roles

func (h *Handler) ListRoles(c fiber.Ctx) error {
	q, err := httpx.ParseListQuery(c, RoleSorts, "role_code", "q")
	if err != nil {
		return err
	}
	items, total, err := h.roles.List(c.Context(), q.Like("q"), paging(q))
	if err != nil {
		return err
	}
	return httpx.List(c, items, q.Page, total)
}

func (h *Handler) GetRole(c fiber.Ctx) error {
	id, err := httpx.UUIDParam(c, "uuid")
	if err != nil {
		return err
	}
	r, err := h.roles.Get(c.Context(), id)
	if err != nil {
		return err
	}
	httpx.SetETag(c, r.UpdatedAt)
	return httpx.OK(c, toRoleResponse(r))
}

func (h *Handler) CreateRole(c fiber.Ctx) error {
	var req RoleRequest
	if err := httpx.BindJSON(c, h.v, &req); err != nil {
		return err
	}
	r, err := h.roles.Create(c.Context(), req)
	if err != nil {
		return err
	}
	httpx.SetETag(c, r.UpdatedAt)
	return httpx.Created(c, "/api/v1/roles/"+r.UUID.String(), toRoleResponse(r))
}

func (h *Handler) UpdateRole(c fiber.Ctx) error {
	id, err := httpx.UUIDParam(c, "uuid")
	if err != nil {
		return err
	}
	expected, err := httpx.IfMatch(c)
	if err != nil {
		return err
	}
	var req RoleRequest
	if err := httpx.BindJSON(c, h.v, &req); err != nil {
		return err
	}
	r, err := h.roles.Update(c.Context(), id, req, expected)
	if err != nil {
		return err
	}
	httpx.SetETag(c, r.UpdatedAt)
	return httpx.OK(c, toRoleResponse(r))
}

func (h *Handler) DeleteRole(c fiber.Ctx) error {
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

func roleActionsResponse(r Role, actions []Action) RoleActionsResponse {
	out := RoleActionsResponse{RoleUUID: r.UUID, Actions: make([]ActionResponse, 0, len(actions))}
	for _, a := range actions {
		out.Actions = append(out.Actions, toActionResponse(a))
	}
	return out
}

func (h *Handler) GetRoleActions(c fiber.Ctx) error {
	id, err := httpx.UUIDParam(c, "uuid")
	if err != nil {
		return err
	}
	r, actions, err := h.roles.Actions(c.Context(), id)
	if err != nil {
		return err
	}
	httpx.SetETag(c, r.UpdatedAt)
	return httpx.OK(c, roleActionsResponse(r, actions))
}

func (h *Handler) PutRoleActions(c fiber.Ctx) error {
	id, err := httpx.UUIDParam(c, "uuid")
	if err != nil {
		return err
	}
	expected, err := httpx.IfMatch(c)
	if err != nil {
		return err
	}
	var req RoleActionsRequest
	if err := httpx.BindJSON(c, h.v, &req); err != nil {
		return err
	}
	r, actions, err := h.roles.ReplaceActions(c.Context(), id, req, expected)
	if err != nil {
		return err
	}
	httpx.SetETag(c, r.UpdatedAt)
	return httpx.OK(c, roleActionsResponse(r, actions))
}

// ---------------------------------------------------------------- actions

func (h *Handler) ListActions(c fiber.Ctx) error {
	q, err := httpx.ParseListQuery(c, ActionSorts, "action_code", "q")
	if err != nil {
		return err
	}
	items, total, err := h.actions.List(c.Context(), q.Like("q"), paging(q))
	if err != nil {
		return err
	}
	return httpx.List(c, items, q.Page, total)
}

func (h *Handler) GetAction(c fiber.Ctx) error {
	id, err := httpx.UUIDParam(c, "uuid")
	if err != nil {
		return err
	}
	a, err := h.actions.Get(c.Context(), id)
	if err != nil {
		return err
	}
	httpx.SetETag(c, a.UpdatedAt)
	return httpx.OK(c, toActionResponse(a))
}

func (h *Handler) CreateAction(c fiber.Ctx) error {
	var req ActionRequest
	if err := httpx.BindJSON(c, h.v, &req); err != nil {
		return err
	}
	a, err := h.actions.Create(c.Context(), req)
	if err != nil {
		return err
	}
	httpx.SetETag(c, a.UpdatedAt)
	return httpx.Created(c, "/api/v1/actions/"+a.UUID.String(), toActionResponse(a))
}

func (h *Handler) UpdateAction(c fiber.Ctx) error {
	id, err := httpx.UUIDParam(c, "uuid")
	if err != nil {
		return err
	}
	expected, err := httpx.IfMatch(c)
	if err != nil {
		return err
	}
	var req ActionRequest
	if err := httpx.BindJSON(c, h.v, &req); err != nil {
		return err
	}
	a, err := h.actions.Update(c.Context(), id, req, expected)
	if err != nil {
		return err
	}
	httpx.SetETag(c, a.UpdatedAt)
	return httpx.OK(c, toActionResponse(a))
}

func (h *Handler) DeleteAction(c fiber.Ctx) error {
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

// ---------------------------------------------------------------- activity log

func (h *Handler) ListActivityLogs(c fiber.Ctx) error {
	q, err := httpx.ParseListQuery(c, ActivityLogSorts, "-log_time", "username", "log_action", "from", "to")
	if err != nil {
		return err
	}
	f := ActivityLogFilter{Username: q.Text("username"), LogAction: q.Text("log_action")}
	if f.From, err = q.Time("from"); err != nil {
		return err
	}
	if f.To, err = q.Time("to"); err != nil {
		return err
	}
	items, total, err := h.logs.List(c.Context(), f, paging(q))
	if err != nil {
		return err
	}
	return httpx.List(c, items, q.Page, total)
}

func (h *Handler) GetActivityLog(c fiber.Ctx) error {
	id, err := httpx.UUIDParam(c, "uuid")
	if err != nil {
		return err
	}
	l, err := h.logs.Get(c.Context(), id)
	if err != nil {
		return err
	}
	return httpx.OK(c, l)
}
