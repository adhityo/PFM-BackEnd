package auth

import (
	"fmt"
	"sort"
	"strings"

	"github.com/gofiber/fiber/v3"
)

// Permission is an entry of the action catalog (table `action`). Codes are
// `<resource>:<verb>`; Module and ResourceLabel group them for role editors
// and frontend menus.
type Permission struct {
	Code          string
	Name          string
	Module        string // e.g. "bond" (labels: permission.ModuleLabels)
	ResourceLabel string // e.g. "obligasi"
}

// Resource is the part of the code before ':' (usually a table).
func (p Permission) Resource() string {
	r, _, _ := strings.Cut(p.Code, ":")
	return r
}

// Verb is the part of the code after ':' (read, create, approve, ...).
func (p Permission) Verb() string {
	_, v, _ := strings.Cut(p.Code, ":")
	return v
}

// Router registers routes fail-closed: every protected route must declare a
// permission, and every declared permission must exist in the catalog.
// It also records each route with its access rule (API documentation).
type Router struct {
	r       fiber.Router
	authn   fiber.Handler
	catalog map[string]Permission
	prefix  string
	routes  *[]RouteInfo
}

// Access rules of a route.
const (
	AccessPublic        = "public"        // no token (login, refresh, ...)
	AccessAuthenticated = "authenticated" // any valid session (/me, ...)
	AccessPermission    = "permission"    // session + Permission
)

// RouteInfo describes a registered route; Path is relative to the API root.
type RouteInfo struct {
	Method, Path, Access, Permission string
}

// NewRouter wraps r. catalog is the union of all module permission lists.
func NewRouter(r fiber.Router, authn fiber.Handler, catalog []Permission) *Router {
	m := make(map[string]Permission, len(catalog))
	for _, p := range catalog {
		m[p.Code] = p
	}
	return &Router{r: r, authn: authn, catalog: m, routes: &[]RouteInfo{}}
}

// Group returns a router for a path prefix.
func (rt *Router) Group(prefix string) *Router {
	return &Router{r: rt.r.Group(prefix), authn: rt.authn, catalog: rt.catalog, prefix: rt.prefix + prefix, routes: rt.routes}
}

// Routes returns every route registered through this router tree, in order.
func (rt *Router) Routes() []RouteInfo {
	return append([]RouteInfo(nil), *rt.routes...)
}

func (rt *Router) record(method, path, access, perm string) {
	*rt.routes = append(*rt.routes, RouteInfo{Method: method, Path: rt.prefix + path, Access: access, Permission: perm})
}

// Handle registers an authenticated route guarded by perm.
func (rt *Router) Handle(method, path, perm string, h fiber.Handler) {
	if _, ok := rt.catalog[perm]; !ok {
		panic(fmt.Sprintf("route %s %s: permission %q tidak terdaftar di katalog", method, path, perm))
	}
	rt.record(method, path, AccessPermission, perm)
	rt.r.Add([]string{method}, path, rt.authn, RequirePermission(perm), h)
}

func (rt *Router) Get(path, perm string, h fiber.Handler) { rt.Handle(fiber.MethodGet, path, perm, h) }
func (rt *Router) Post(path, perm string, h fiber.Handler) {
	rt.Handle(fiber.MethodPost, path, perm, h)
}
func (rt *Router) Put(path, perm string, h fiber.Handler) { rt.Handle(fiber.MethodPut, path, perm, h) }
func (rt *Router) Delete(path, perm string, h fiber.Handler) {
	rt.Handle(fiber.MethodDelete, path, perm, h)
}

// Authenticated registers a route that only needs a valid session (e.g. /me).
func (rt *Router) Authenticated(method, path string, h fiber.Handler) {
	rt.record(method, path, AccessAuthenticated, "")
	rt.r.Add([]string{method}, path, rt.authn, h)
}

// Public registers a route without authentication (login, refresh, activation).
func (rt *Router) Public(method, path string, handlers ...fiber.Handler) {
	if len(handlers) == 0 {
		panic("public route tanpa handler")
	}
	rt.record(method, path, AccessPublic, "")
	rest := make([]any, 0, len(handlers)-1)
	for _, h := range handlers[1:] {
		rest = append(rest, h)
	}
	rt.r.Add([]string{method}, path, handlers[0], rest...)
}

// MergeCatalogs joins module catalogs and fails on duplicate codes.
func MergeCatalogs(lists ...[]Permission) []Permission {
	seen := map[string]bool{}
	var out []Permission
	for _, l := range lists {
		for _, p := range l {
			if seen[p.Code] {
				panic("permission duplikat: " + p.Code)
			}
			seen[p.Code] = true
			out = append(out, p)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Code < out[j].Code })
	return out
}

// CRUD returns the standard permission set for a table (PRD §10.3).
func CRUD(table, label string) []Permission {
	return []Permission{
		{Code: table + ":read", Name: "Lihat " + label, ResourceLabel: label},
		{Code: table + ":create", Name: "Tambah " + label, ResourceLabel: label},
		{Code: table + ":update", Name: "Ubah " + label, ResourceLabel: label},
		{Code: table + ":delete", Name: "Hapus " + label, ResourceLabel: label},
	}
}

// InModule sets the module of every permission.
func InModule(module string, ps ...Permission) []Permission {
	for i := range ps {
		ps[i].Module = module
	}
	return ps
}
