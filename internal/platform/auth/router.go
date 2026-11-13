package auth

import (
	"fmt"
	"sort"

	"github.com/gofiber/fiber/v3"
)

// Permission is an entry of the action catalog (table `action`).
type Permission struct {
	Code string
	Name string
}

// Router registers routes fail-closed: every protected route must declare a
// permission, and every declared permission must exist in the catalog.
type Router struct {
	r       fiber.Router
	authn   fiber.Handler
	catalog map[string]Permission
}

// NewRouter wraps r. catalog is the union of all module permission lists.
func NewRouter(r fiber.Router, authn fiber.Handler, catalog []Permission) *Router {
	m := make(map[string]Permission, len(catalog))
	for _, p := range catalog {
		m[p.Code] = p
	}
	return &Router{r: r, authn: authn, catalog: m}
}

// Group returns a router for a path prefix.
func (rt *Router) Group(prefix string) *Router {
	return &Router{r: rt.r.Group(prefix), authn: rt.authn, catalog: rt.catalog}
}

// Handle registers an authenticated route guarded by perm.
func (rt *Router) Handle(method, path, perm string, h fiber.Handler) {
	if _, ok := rt.catalog[perm]; !ok {
		panic(fmt.Sprintf("route %s %s: permission %q tidak terdaftar di katalog", method, path, perm))
	}
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
	rt.r.Add([]string{method}, path, rt.authn, h)
}

// Public registers a route without authentication (login, refresh, activation).
func (rt *Router) Public(method, path string, handlers ...fiber.Handler) {
	if len(handlers) == 0 {
		panic("public route tanpa handler")
	}
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
		{Code: table + ":read", Name: "Lihat " + label},
		{Code: table + ":create", Name: "Tambah " + label},
		{Code: table + ":update", Name: "Ubah " + label},
		{Code: table + ":delete", Name: "Hapus " + label},
	}
}
