package router

import (
	"context"
	"slices"
	"testing"

	"pfm-backend/internal/platform/auth"
)

func (e *env) actionUUIDs(codes ...string) []string {
	e.t.Helper()
	out := make([]string, 0, len(codes))
	for _, c := range codes {
		var id string
		if err := e.secPool.QueryRow(context.Background(), "SELECT uuid::text FROM action WHERE action_code = $1 AND deleted_at IS NULL", c).Scan(&id); err != nil {
			e.t.Fatal(c, err)
		}
		out = append(out, id)
	}
	return out
}

func (e *env) roleUUID(code string) string {
	e.t.Helper()
	var id string
	if err := e.secPool.QueryRow(context.Background(), "SELECT uuid::text FROM role WHERE role_code = $1 AND deleted_at IS NULL", code).Scan(&id); err != nil {
		e.t.Fatal(code, err)
	}
	return id
}

// setActions replaces the permissions of a role as token, expecting want.
func (e *env) setActions(token, role string, want int, codes ...string) resp {
	e.t.Helper()
	path := "/roles/" + role
	return e.expect(e.do("PUT", "/api/v1"+path+"/actions", token, map[string]any{"action_uuids": e.actionUUIDs(codes...)},
		"If-Match", e.etag(token, path)), want, "set actions")
}

func stringList(v any) []string {
	var out []string
	for _, x := range v.([]any) {
		out = append(out, x.(string))
	}
	return out
}

func deniedPermission(r resp) string {
	d, _ := r.body["error"].(map[string]any)["details"].([]any)
	if len(d) == 0 {
		return ""
	}
	f := d[0].(map[string]any)
	if f["field"] != "permission" {
		return ""
	}
	return f["rule"].(string)
}

// The frontend renders pages and buttons from GET /me and reloads it when
// X-Permissions-Version changes; the backend enforces the same set.
func TestPermissionsDriveFrontendAndBackend(t *testing.T) {
	e := setup(t)
	admin, _ := e.login("admin", adminPassword)
	ops := e.userWith(admin, "OPS", "ops.user", "Ops-Passw0rd-1", "", "bond:read", "bond_trade:read", "bond_trade:approve")
	role := e.roleUUID("OPS")

	r := e.expect(e.do("GET", "/api/v1/me", ops, nil), 200, "me")
	me := r.data()
	got := stringList(me["permissions"])
	if !slices.Equal(got, []string{"bond:read", "bond_trade:approve", "bond_trade:read"}) || me["is_admin"] != false {
		t.Fatalf("permissions = %v, is_admin = %v", got, me["is_admin"])
	}
	roleInfo := me["role"].(map[string]any)
	if roleInfo["code"] != "OPS" || roleInfo["active"] != true || roleInfo["uuid"] != role {
		t.Fatalf("role = %v", roleInfo)
	}
	v1 := me["permissions_version"].(string)
	if v1 == "" || r.header.Get(auth.HeaderPermissionsVersion) != v1 {
		t.Fatalf("version %q, header %q", v1, r.header.Get(auth.HeaderPermissionsVersion))
	}

	e.expect(e.do("GET", "/api/v1/bonds", ops, nil), 200, "list bonds")
	denied := e.expect(e.do("POST", "/api/v1/bonds", ops, map[string]any{}), 403, "create bond without permission")
	if deniedPermission(denied) != "bond:create" {
		t.Fatalf("403 must name the missing permission: %s", denied.raw)
	}

	// Granting takes effect on the next request, with a new version.
	e.setActions(admin, role, 200, "bond:read", "bond:create", "bond_trade:read", "bond_trade:approve")
	r = e.expect(e.do("POST", "/api/v1/bonds", ops, map[string]any{}), 422, "create bond after grant (validation, not 403)")
	v2 := r.header.Get(auth.HeaderPermissionsVersion)
	if v2 == "" || v2 == v1 {
		t.Fatalf("version must change after grant: %q -> %q", v1, v2)
	}
	if me := e.expect(e.do("GET", "/api/v1/me", ops, nil), 200, "me").data(); len(stringList(me["permissions"])) != 4 || me["permissions_version"] != v2 {
		t.Fatalf("me after grant: %v", me)
	}

	// An inactive role grants nothing.
	path := "/roles/" + role
	e.expect(e.do("PUT", "/api/v1"+path, admin, map[string]any{"role_code": "OPS", "role_name": "OPS", "status": "inactive"},
		"If-Match", e.etag(admin, path)), 200, "deactivate role")
	e.expect(e.do("GET", "/api/v1/bonds", ops, nil), 403, "inactive role")
	me = e.expect(e.do("GET", "/api/v1/me", ops, nil), 200, "me").data()
	if len(stringList(me["permissions"])) != 0 || me["role"].(map[string]any)["active"] != false {
		t.Fatalf("me with inactive role: %v", me)
	}
}

func TestPermissionCatalogForRoleEditor(t *testing.T) {
	e := setup(t)
	admin, _ := e.login("admin", adminPassword)
	cat := e.expect(e.do("GET", "/api/v1/permission-catalog", admin, nil), 200, "catalog").data()
	modules := cat["modules"].([]any)
	if modules[0].(map[string]any)["code"] != "system" {
		t.Fatalf("first module = %v", modules[0])
	}
	var tradeVerbs []string
	for _, m := range modules {
		mod := m.(map[string]any)
		if mod["code"] != "bond" {
			continue
		}
		if mod["label"] != "Obligasi" {
			t.Fatalf("bond module label = %v", mod["label"])
		}
		for _, r := range mod["resources"].([]any) {
			res := r.(map[string]any)
			if res["code"] == "bond_trade" {
				if res["label"] != "subscription obligasi" {
					t.Fatalf("bond_trade label = %v", res["label"])
				}
				for _, a := range res["actions"].([]any) {
					tradeVerbs = append(tradeVerbs, a.(map[string]any)["verb"].(string))
				}
			}
		}
	}
	if !slices.Equal(tradeVerbs, []string{"read", "create", "update", "delete", "approve", "cancel"}) {
		t.Fatalf("bond_trade verbs = %v", tradeVerbs)
	}
	adminMe := e.expect(e.do("GET", "/api/v1/me", admin, nil), 200, "me").data()
	if cat["version"] != adminMe["permissions_version"] {
		t.Fatal("catalog version must equal the ADMIN permissions version")
	}
	for _, a := range e.expect(e.do("GET", "/api/v1/actions?module=pension&per_page=100", admin, nil), 200, "pension actions").body["data"].([]any) {
		if a.(map[string]any)["module"] != "pension" {
			t.Fatalf("module filter: %v", a)
		}
	}

	// System permissions are owned by the code: read-only through the API.
	sys := "/actions/" + e.actionUUIDs("bond:read")[0]
	e.expect(e.do("PUT", "/api/v1"+sys, admin, map[string]any{"action_code": "bond:read", "action_name": "x"},
		"If-Match", e.etag(admin, sys)), 409, "edit system action")
	e.expect(e.do("DELETE", "/api/v1"+sys, admin, nil, "If-Match", e.etag(admin, sys)), 409, "delete system action")

	// Custom actions (frontend-only switches) can be created, granted and removed.
	custom := e.create(admin, "/actions", map[string]any{"action_code": "dashboard_widget:view", "action_name": "Lihat widget dashboard",
		"resource_label": "widget dashboard"})
	if custom["module"] != "custom" || custom["is_system"] != false || custom["verb"] != "view" {
		t.Fatalf("custom action: %v", custom)
	}
	cat2 := e.expect(e.do("GET", "/api/v1/permission-catalog", admin, nil), 200, "catalog").data()
	last := cat2["modules"].([]any)[len(cat2["modules"].([]any))-1].(map[string]any)
	if last["code"] != "custom" || cat2["version"] == cat["version"] {
		t.Fatalf("custom module / version: %v %v", last["code"], cat2["version"])
	}
	viewer := e.userWith(admin, "VIEWER", "viewer", "Lihat-Saja-Pass-9", "", "dashboard_widget:view")
	if got := stringList(e.expect(e.do("GET", "/api/v1/me", viewer, nil), 200, "me").data()["permissions"]); !slices.Equal(got, []string{"dashboard_widget:view"}) {
		t.Fatalf("custom permission in /me: %v", got)
	}
	e.expect(e.do("GET", "/api/v1/permission-catalog", viewer, nil), 403, "catalog needs role_action:read")
}

// A non-ADMIN manager cannot grant, assign or manage more than it holds.
func TestNoPrivilegeEscalationThroughRolesAndUsers(t *testing.T) {
	e := setup(t)
	admin, _ := e.login("admin", adminPassword)
	mgr := e.userWith(admin, "MANAGER", "manager", "Kelola-Tim-Pass-9", "", "role:read", "role:create", "role:update",
		"role_action:read", "role_action:update", "app_user:read", "app_user:create", "app_user:update",
		"app_user:reset_password", "bond:read")
	junior := e.create(admin, "/roles", map[string]any{"role_code": "JUNIOR", "role_name": "Junior", "status": "active"})["uuid"].(string)
	e.setActions(admin, junior, 200, "bond:read")
	senior := e.create(admin, "/roles", map[string]any{"role_code": "SENIOR", "role_name": "Senior", "status": "active"})["uuid"].(string)
	e.setActions(admin, senior, 200, "bond:read", "pii:read")

	if r := e.setActions(mgr, junior, 403, "bond:read", "bond:create"); deniedPermission(r) != "bond:create" {
		t.Fatalf("grant beyond own permissions: %s", r.raw)
	}
	e.setActions(mgr, e.roleUUID("MANAGER"), 403, "role:read", "pii:read")
	e.setActions(mgr, junior, 200, "bond:read")

	// Roles holding more than the manager cannot be edited, emptied or deleted.
	e.setActions(mgr, senior, 403, "bond:read")
	sPath := "/roles/" + senior
	e.expect(e.do("PUT", "/api/v1"+sPath, mgr, map[string]any{"role_code": "SENIOR", "role_name": "x", "status": "active"},
		"If-Match", e.etag(mgr, sPath)), 403, "update stronger role")
	e.expect(e.do("DELETE", "/api/v1"+sPath, mgr, nil, "If-Match", e.etag(mgr, sPath)), 403, "delete stronger role")

	// Users: only roles within the manager's permissions may be assigned or managed.
	e.expect(e.do("POST", "/api/v1/users", mgr, map[string]any{"username": "too.strong", "email": "s@example.test",
		"role_uuid": senior, "status": "active"}), 403, "assign stronger role")
	e.expect(e.do("POST", "/api/v1/users", mgr, map[string]any{"username": "junior.user", "email": "j@example.test",
		"role_uuid": junior, "status": "active"}), 201, "assign covered role")
	strong := e.create(admin, "/users", map[string]any{"username": "senior.user", "email": "senior@example.test",
		"role_uuid": senior, "status": "active"})["user"].(map[string]any)["uuid"].(string)
	uPath := "/users/" + strong
	e.expect(e.do("POST", "/api/v1"+uPath+"/reset-password", mgr, nil, "If-Match", e.etag(mgr, uPath)), 403, "reset stronger user")
}

// GET /users: the route permission decides whether the list is reachable at
// all, the tenant scope decides which rows a non-ADMIN sees.
func TestUserListVisibility(t *testing.T) {
	e := setup(t)
	admin, _ := e.login("admin", adminPassword)
	ca := e.create(admin, "/clients", map[string]any{"client_name": "Client A", "client_code": "CA"})["uuid"].(string)
	cb := e.create(admin, "/clients", map[string]any{"client_name": "Client B", "client_code": "CB"})["uuid"].(string)

	plain := e.userWith(admin, "PLAIN", "staf.biasa", "Biasa-Saja-Pass-9", "", "bond:read")
	viewerA := e.userWith(admin, "USER-VIEWER-A", "viewer.a", "Lihat-Klien-Pass-9", ca, "app_user:read")
	e.userWith(admin, "USER-VIEWER-B", "viewer.b", "Lihat-Klien-Pass-9", cb, "app_user:read")
	var userB string
	_ = e.secPool.QueryRow(context.Background(), "SELECT uuid::text FROM app_user WHERE username = 'viewer.b'").Scan(&userB)

	usernames := func(token string) []string {
		var out []string
		for _, u := range items(e.expect(e.do("GET", "/api/v1/users?per_page=100", token, nil), 200, "list users")) {
			out = append(out, u["username"].(string))
		}
		slices.Sort(out)
		return out
	}

	// 1. Without app_user:read the route itself is closed.
	if r := e.expect(e.do("GET", "/api/v1/users", plain, nil), 403, "list without permission"); deniedPermission(r) != "app_user:read" {
		t.Fatalf("403 must name app_user:read: %s", r.raw)
	}
	// 2. With app_user:read, a client user sees only users of its own client.
	if got := usernames(viewerA); !slices.Equal(got, []string{"viewer.a"}) {
		t.Fatalf("client A sees %v", got)
	}
	e.expect(e.do("GET", "/api/v1/users/"+userB, viewerA, nil), 404, "user of another client")
	// 3. ADMIN holds every permission and has no tenant filter.
	if got := usernames(admin); !slices.Equal(got, []string{"admin", "staf.biasa", "viewer.a", "viewer.b"}) {
		t.Fatalf("admin sees %v", got)
	}
}
