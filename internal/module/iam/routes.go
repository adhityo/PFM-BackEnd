package iam

import (
	"github.com/gofiber/fiber/v3"

	"pfm-backend/internal/apperr"
	"pfm-backend/internal/platform/auth"
)

func invalidStatusFilter() error {
	return apperr.Invalid("status", "oneof", "harus salah satu dari: active, inactive")
}

// Routes registers the IAM endpoints. authLimiter guards unauthenticated
// credential endpoints (login, refresh, activation).
func (h *Handler) Routes(r *auth.Router, authLimiter fiber.Handler) {
	a := r.Group("/auth")
	a.Public(fiber.MethodPost, "/login", authLimiter, h.Login)
	a.Public(fiber.MethodPost, "/refresh", authLimiter, h.Refresh)
	a.Public(fiber.MethodPost, "/logout", authLimiter, h.Logout)
	a.Public(fiber.MethodPost, "/activate", authLimiter, h.Activate)
	a.Authenticated(fiber.MethodPost, "/logout-all", h.LogoutAll)
	a.Authenticated(fiber.MethodPost, "/change-password", h.ChangePassword)
	r.Authenticated(fiber.MethodGet, "/me", h.Me)

	u := r.Group("/users")
	u.Get("", PermUserRead, h.ListUsers)
	u.Post("", PermUserCreate, h.CreateUser)
	u.Get("/:uuid", PermUserRead, h.GetUser)
	u.Put("/:uuid", PermUserUpdate, h.UpdateUser)
	u.Delete("/:uuid", PermUserDelete, h.DeleteUser)
	u.Post("/:uuid/reset-password", PermUserResetPassword, h.ResetUserPassword)
	u.Post("/:uuid/lock", PermUserLock, h.lockUser(true))
	u.Post("/:uuid/unlock", PermUserLock, h.lockUser(false))

	ro := r.Group("/roles")
	ro.Get("", PermRoleRead, h.ListRoles)
	ro.Post("", PermRoleCreate, h.CreateRole)
	ro.Get("/:uuid", PermRoleRead, h.GetRole)
	ro.Put("/:uuid", PermRoleUpdate, h.UpdateRole)
	ro.Delete("/:uuid", PermRoleDelete, h.DeleteRole)
	ro.Get("/:uuid/actions", PermRoleActionRead, h.GetRoleActions)
	ro.Put("/:uuid/actions", PermRoleActionUpdate, h.PutRoleActions)

	ac := r.Group("/actions")
	ac.Get("", PermActionRead, h.ListActions)
	ac.Post("", PermActionCreate, h.CreateAction)
	ac.Get("/:uuid", PermActionRead, h.GetAction)
	ac.Put("/:uuid", PermActionUpdate, h.UpdateAction)
	ac.Delete("/:uuid", PermActionDelete, h.DeleteAction)

	l := r.Group("/activity-logs")
	l.Get("", PermActivityLogRead, h.ListActivityLogs)
	l.Get("/:uuid", PermActivityLogRead, h.GetActivityLog)
}
