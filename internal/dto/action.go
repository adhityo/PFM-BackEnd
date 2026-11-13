package dto

import (
	"strings"
	"time"

	"github.com/google/uuid"

	"pfm-backend/internal/model"
)

type ActionRequest struct {
	ActionCode  string `json:"action_code" validate:"required,perm_code"`
	ActionName  string `json:"action_name" validate:"required,max=255,safe_text"`
	Description string `json:"description" validate:"omitempty,max=255,safe_text"`
}

func (r *ActionRequest) Normalize() {
	r.ActionCode = strings.TrimSpace(r.ActionCode)
	r.ActionName = strings.TrimSpace(r.ActionName)
	r.Description = strings.TrimSpace(r.Description)
}

type ActionResponse struct {
	UUID        uuid.UUID `json:"uuid"`
	ActionCode  string    `json:"action_code"`
	ActionName  string    `json:"action_name"`
	Description *string   `json:"description"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

func ActionFromModel(a model.Action) ActionResponse {
	return ActionResponse{UUID: a.UUID, ActionCode: a.ActionCode, ActionName: a.ActionName,
		Description: a.Description, CreatedAt: a.CreatedAt, UpdatedAt: a.UpdatedAt}
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
