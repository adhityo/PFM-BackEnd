package dto

import (
	"strings"
	"time"

	"github.com/google/uuid"

	"pfm-backend/internal/model"
)

type RoleRequest struct {
	RoleCode    string `json:"role_code"   validate:"required,code"`
	RoleName    string `json:"role_name"   validate:"required,max=255,safe_text"`
	Description string `json:"description" validate:"omitempty,max=255,safe_text"`
	MenuURL     string `json:"menu_url"    validate:"omitempty,max=255,startswith=/,safe_text"`
	Status      string `json:"status"      validate:"required,oneof=active inactive"`
}

func (r *RoleRequest) Normalize() {
	r.RoleCode = strings.ToUpper(strings.TrimSpace(r.RoleCode))
	r.RoleName = strings.TrimSpace(r.RoleName)
	r.Description = strings.TrimSpace(r.Description)
	r.MenuURL = strings.TrimSpace(r.MenuURL)
}

type RoleResponse struct {
	UUID        uuid.UUID `json:"uuid"`
	RoleCode    string    `json:"role_code"`
	RoleName    string    `json:"role_name"`
	Description *string   `json:"description"`
	MenuURL     *string   `json:"menu_url"`
	Status      string    `json:"status"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

func RoleFromModel(r model.Role) RoleResponse {
	return RoleResponse{UUID: r.UUID, RoleCode: r.RoleCode, RoleName: r.RoleName, Description: r.Description,
		MenuURL: r.MenuURL, Status: model.StatusName(r.Status), CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt}
}

type RoleActionsRequest struct {
	ActionUUIDs []string `json:"action_uuids" validate:"required,max=1000,unique,dive,uuid"`
}

type RoleActionsResponse struct {
	RoleUUID uuid.UUID        `json:"role_uuid"`
	Actions  []ActionResponse `json:"actions"`
}

func RoleActionsFromModel(r model.Role, actions []model.Action) RoleActionsResponse {
	out := RoleActionsResponse{RoleUUID: r.UUID, Actions: make([]ActionResponse, 0, len(actions))}
	for _, a := range actions {
		out.Actions = append(out.Actions, ActionFromModel(a))
	}
	return out
}
