package iam

import "pfm-backend/internal/platform/auth"

// Permission codes owned by the IAM module.
const (
	PermUserRead          = "app_user:read"
	PermUserCreate        = "app_user:create"
	PermUserUpdate        = "app_user:update"
	PermUserDelete        = "app_user:delete"
	PermUserResetPassword = "app_user:reset_password" //nolint:gosec // permission code, not a credential
	PermUserLock          = "app_user:lock"
	PermRoleRead          = "role:read"
	PermRoleCreate        = "role:create"
	PermRoleUpdate        = "role:update"
	PermRoleDelete        = "role:delete"
	PermRoleActionRead    = "role_action:read"
	PermRoleActionUpdate  = "role_action:update"
	PermActionRead        = "action:read"
	PermActionCreate      = "action:create"
	PermActionUpdate      = "action:update"
	PermActionDelete      = "action:delete"
	PermActivityLogRead   = "activity_log:read"
)

// Permissions is the IAM part of the action catalog.
var Permissions = func() []auth.Permission {
	p := append(auth.CRUD("app_user", "pengguna"),
		auth.Permission{Code: PermUserResetPassword, Name: "Reset kredensial pengguna"},
		auth.Permission{Code: PermUserLock, Name: "Kunci/buka kunci pengguna"},
	)
	p = append(p, auth.CRUD("role", "role")...)
	p = append(p,
		auth.Permission{Code: PermRoleActionRead, Name: "Lihat permission role"},
		auth.Permission{Code: PermRoleActionUpdate, Name: "Ubah permission role"},
	)
	p = append(p, auth.CRUD("action", "katalog permission")...)
	p = append(p,
		auth.Permission{Code: PermActivityLogRead, Name: "Lihat log aktivitas"},
		auth.Permission{Code: auth.PermPIIRead, Name: "Lihat data pribadi tanpa masking"},
	)
	return p
}()

// DefaultRoles are the roles present in the legacy data (PRD §4).
var DefaultRoles = []struct{ Code, Name string }{
	{auth.RoleAdmin, "ADMINISTRATOR"},
	{"FM", "FUND MANAGER"},
	{"MEMBER", "MEMBER"},
	{"AGENT", "MARKETING AGENT"},
	{"CLIENT-MNG", "CLIENT MANAGEMENT"},
	{"CLIENT-ADMIN", "CLIENT ADMINISTRATION"},
	{"CSPV", "CLIENT SUPERVISOR"},
}
