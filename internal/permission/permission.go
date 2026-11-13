// Package permission holds the permission codes (table `action`) used to
// guard routes and the static part of the catalog.
package permission

import "pfm-backend/internal/platform/auth"

const (
	UserRead          = "app_user:read"
	UserCreate        = "app_user:create"
	UserUpdate        = "app_user:update"
	UserDelete        = "app_user:delete"
	UserResetPassword = "app_user:reset_password" //nolint:gosec // permission code, not a credential
	UserLock          = "app_user:lock"

	RoleRead         = "role:read"
	RoleCreate       = "role:create"
	RoleUpdate       = "role:update"
	RoleDelete       = "role:delete"
	RoleActionRead   = "role_action:read"
	RoleActionUpdate = "role_action:update"

	ActionRead   = "action:read"
	ActionCreate = "action:create"
	ActionUpdate = "action:update"
	ActionDelete = "action:delete"

	ActivityLogRead = "activity_log:read"

	FileRead   = "file_object:read"
	FileCreate = "file_object:create"
	FileDelete = "file_object:delete"

	ConfigurationRead   = "configuration:read"
	ConfigurationUpdate = "configuration:update"
)

// Static returns the hand-defined permissions. CRUD resources add their own
// <table>:read|create|update|delete entries (see router.Catalog).
func Static() []auth.Permission {
	p := append(auth.CRUD("app_user", "pengguna"),
		auth.Permission{Code: UserResetPassword, Name: "Reset kredensial pengguna"},
		auth.Permission{Code: UserLock, Name: "Kunci/buka kunci pengguna"},
	)
	p = append(p, auth.CRUD("role", "role")...)
	p = append(p,
		auth.Permission{Code: RoleActionRead, Name: "Lihat permission role"},
		auth.Permission{Code: RoleActionUpdate, Name: "Ubah permission role"},
	)
	p = append(p, auth.CRUD("action", "katalog permission")...)
	return append(p,
		auth.Permission{Code: ActivityLogRead, Name: "Lihat log aktivitas"},
		auth.Permission{Code: auth.PermPIIRead, Name: "Lihat data pribadi tanpa masking"},
		auth.Permission{Code: FileRead, Name: "Lihat/unduh file milik pengguna lain"},
		auth.Permission{Code: FileCreate, Name: "Unggah file"},
		auth.Permission{Code: FileDelete, Name: "Hapus file pending milik pengguna lain"},
		auth.Permission{Code: ConfigurationRead, Name: "Lihat konfigurasi sistem"},
		auth.Permission{Code: ConfigurationUpdate, Name: "Ubah konfigurasi sistem"},
	)
}
