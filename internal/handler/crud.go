package handler

import (
	"pfm-backend/internal/platform/auth"
	"pfm-backend/internal/platform/crud"
)

// RegisterCRUD mounts scaffolded resources (e.g. service.MasterdataResources).
// Their list/get/create/update/delete handlers are the generic ones of
// platform/crud, so every resource behaves the same way (PRD §10.3).
func RegisterCRUD(env crud.Env, resources []crud.Registrar) {
	for _, r := range resources {
		r.Register(env)
	}
}

// CRUDPermissions returns the <table>:read|create|update|delete codes of resources.
func CRUDPermissions(resources []crud.Registrar) []auth.Permission {
	var out []auth.Permission
	for _, r := range resources {
		out = append(out, r.Permissions()...)
	}
	return out
}
