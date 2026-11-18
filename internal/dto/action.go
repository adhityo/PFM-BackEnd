package dto

import (
	"strings"
	"time"

	"github.com/google/uuid"

	"pfm-backend/internal/model"
)

// ActionRequest creates or edits a custom action (module "custom"), e.g. a
// frontend-only switch. System actions come from the code catalog.
type ActionRequest struct {
	ActionCode    string `json:"action_code" validate:"required,perm_code"`
	ActionName    string `json:"action_name" validate:"required,max=255,safe_text"`
	Description   string `json:"description" validate:"omitempty,max=255,safe_text"`
	ResourceLabel string `json:"resource_label" validate:"omitempty,max=255,safe_text"`
}

func (r *ActionRequest) Normalize() {
	r.ActionCode = strings.TrimSpace(r.ActionCode)
	r.ActionName = strings.TrimSpace(r.ActionName)
	r.Description = strings.TrimSpace(r.Description)
	r.ResourceLabel = strings.TrimSpace(r.ResourceLabel)
}

// ActionResponse is one permission. action_code is "<resource>:<verb>"; the
// frontend shows a page when the user holds "<resource>:read" and a button
// when the user holds the button's code (e.g. "bond_trade:approve").
type ActionResponse struct {
	UUID          uuid.UUID `json:"uuid"`
	ActionCode    string    `json:"action_code"`
	ActionName    string    `json:"action_name"`
	Description   *string   `json:"description"`
	Module        string    `json:"module"`
	Resource      string    `json:"resource"`
	ResourceLabel *string   `json:"resource_label"`
	Verb          string    `json:"verb"`
	IsSystem      bool      `json:"is_system"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

func ActionFromModel(a model.Action) ActionResponse {
	resource, verb, _ := strings.Cut(a.ActionCode, ":")
	return ActionResponse{UUID: a.UUID, ActionCode: a.ActionCode, ActionName: a.ActionName,
		Description: a.Description, Module: a.Module, Resource: resource, ResourceLabel: a.ResourceLabel, Verb: verb,
		IsSystem: a.IsSystem, CreatedAt: a.CreatedAt, UpdatedAt: a.UpdatedAt}
}

// PermissionCatalogResponse groups the catalog module -> resource -> action
// for role editors. Version changes whenever the catalog changes.
type PermissionCatalogResponse struct {
	Version string             `json:"version"`
	Modules []PermissionModule `json:"modules"`
}

type PermissionModule struct {
	Code      string               `json:"code"`
	Label     string               `json:"label"`
	Resources []PermissionResource `json:"resources"`
}

type PermissionResource struct {
	Code    string             `json:"code"`
	Label   string             `json:"label"`
	Actions []PermissionAction `json:"actions"`
}

type PermissionAction struct {
	UUID     uuid.UUID `json:"uuid"`
	Code     string    `json:"code"`
	Verb     string    `json:"verb"`
	Name     string    `json:"name"`
	IsSystem bool      `json:"is_system"`
}

type ActivityLogResponse struct {
	UUID           uuid.UUID  `json:"uuid"`
	LogTime        *time.Time `json:"log_time"`
	LogAction      *string    `json:"log_action"`
	LogTitle       *string    `json:"log_title"`
	LogDescription *string    `json:"log_description"`
	Username       *string    `json:"username"`
	RoleCode       *string    `json:"role_code"`
	IPAddress      *string    `json:"ip_address"`
	Route          *string    `json:"route"`
	RequestID      *string    `json:"request_id"`
}

func ActivityLogFromModel(a model.ActivityLog) ActivityLogResponse {
	var ip *string
	if a.IPAddress != nil {
		s := a.IPAddress.String()
		ip = &s
	}
	return ActivityLogResponse{UUID: a.UUID, LogTime: a.LogTime, LogAction: a.LogAction, LogTitle: a.LogTitle,
		LogDescription: a.LogDescription, Username: a.Username, RoleCode: a.RoleCode, IPAddress: ip,
		Route: a.ActionQuery, RequestID: a.LogSessionCode}
}
