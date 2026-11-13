package iam

import (
	"strings"
	"time"

	"github.com/google/uuid"
)

// ---------------------------------------------------------------- auth

type LoginRequest struct {
	Login    string `json:"login"    validate:"required,max=255"`
	Password string `json:"password" validate:"required,max=128"`
}

func (r *LoginRequest) Normalize() { r.Login = strings.TrimSpace(r.Login) }

type RefreshRequest struct {
	RefreshToken string `json:"refresh_token" validate:"required,max=64"`
}

type ActivateRequest struct {
	ActivationKey string `json:"activation_key" validate:"required,max=64"`
	NewPassword   string `json:"new_password"   validate:"required,min=12,max=128"`
}

type ChangePasswordRequest struct {
	OldPassword string `json:"old_password" validate:"required,max=128"`
	NewPassword string `json:"new_password" validate:"required,min=12,max=128"`
}

type TokenResponse struct {
	AccessToken      string    `json:"access_token"`
	TokenType        string    `json:"token_type"`
	ExpiresIn        int64     `json:"expires_in"`
	RefreshToken     string    `json:"refresh_token"`
	RefreshExpiresAt time.Time `json:"refresh_expires_at"`
}

type MeResponse struct {
	User        UserResponse `json:"user"`
	Permissions []string     `json:"permissions"`
}

// ---------------------------------------------------------------- users

type CreateUserRequest struct {
	Username     string `json:"username"      validate:"required,username"`
	Email        string `json:"email"         validate:"required,email,max=254"`
	FirstName    string `json:"first_name"    validate:"omitempty,max=255,safe_text"`
	Description  string `json:"description"   validate:"omitempty,max=255,safe_text"`
	RoleUUID     string `json:"role_uuid"     validate:"required,uuid"`
	ClientUUID   string `json:"client_uuid"   validate:"omitempty,uuid"`
	IsSupervisor bool   `json:"is_supervisor"`
	Status       string `json:"status"        validate:"required,oneof=active inactive"`
}

func (r *CreateUserRequest) Normalize() {
	r.Username = strings.ToLower(strings.TrimSpace(r.Username))
	r.Email = strings.ToLower(strings.TrimSpace(r.Email))
	r.FirstName = strings.TrimSpace(r.FirstName)
	r.Description = strings.TrimSpace(r.Description)
}

type UpdateUserRequest struct {
	Email        string `json:"email"         validate:"required,email,max=254"`
	FirstName    string `json:"first_name"    validate:"omitempty,max=255,safe_text"`
	Description  string `json:"description"   validate:"omitempty,max=255,safe_text"`
	RoleUUID     string `json:"role_uuid"     validate:"required,uuid"`
	ClientUUID   string `json:"client_uuid"   validate:"omitempty,uuid"`
	IsSupervisor bool   `json:"is_supervisor"`
	Status       string `json:"status"        validate:"required,oneof=active inactive"`
}

func (r *UpdateUserRequest) Normalize() {
	r.Email = strings.ToLower(strings.TrimSpace(r.Email))
	r.FirstName = strings.TrimSpace(r.FirstName)
	r.Description = strings.TrimSpace(r.Description)
}

type UserResponse struct {
	UUID         uuid.UUID  `json:"uuid"`
	Username     string     `json:"username"`
	Email        string     `json:"email"`
	FirstName    *string    `json:"first_name"`
	Description  *string    `json:"description"`
	Status       string     `json:"status"`
	RoleUUID     *uuid.UUID `json:"role_uuid"`
	RoleCode     *string    `json:"role_code"`
	ClientUUID   *uuid.UUID `json:"client_uuid"`
	IsSupervisor bool       `json:"is_supervisor"`
	PasswordSet  bool       `json:"password_set"`
	Locked       bool       `json:"locked"`
	LockedUntil  *time.Time `json:"locked_until"`
	LastLoginAt  *time.Time `json:"last_login_at"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
}

func toUserResponse(u User, now time.Time) UserResponse {
	return UserResponse{UUID: u.UUID, Username: u.Username, Email: u.Email, FirstName: u.FirstName,
		Description: u.Description, Status: statusName(u.Status), RoleUUID: u.RoleUUID, RoleCode: u.RoleCode,
		ClientUUID: u.ClientUUID, IsSupervisor: u.IsSupervisor, PasswordSet: u.PasswordSet,
		Locked: u.LockedUntil != nil && u.LockedUntil.After(now), LockedUntil: u.LockedUntil,
		LastLoginAt: u.LastLoginAt, CreatedAt: u.CreatedAt, UpdatedAt: u.UpdatedAt}
}

// UserCredentialResponse returns a one-time activation key (create / reset).
type UserCredentialResponse struct {
	User                UserResponse `json:"user"`
	ActivationKey       string       `json:"activation_key"`
	ActivationExpiresAt time.Time    `json:"activation_expires_at"`
}

// ---------------------------------------------------------------- roles & actions

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

func toRoleResponse(r Role) RoleResponse {
	return RoleResponse{UUID: r.UUID, RoleCode: r.RoleCode, RoleName: r.RoleName, Description: r.Description,
		MenuURL: r.MenuURL, Status: statusName(r.Status), CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt}
}

type RoleActionsRequest struct {
	ActionUUIDs []string `json:"action_uuids" validate:"required,max=1000,unique,dive,uuid"`
}

type RoleActionsResponse struct {
	RoleUUID uuid.UUID        `json:"role_uuid"`
	Actions  []ActionResponse `json:"actions"`
}

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

func toActionResponse(a Action) ActionResponse {
	return ActionResponse{UUID: a.UUID, ActionCode: a.ActionCode, ActionName: a.ActionName,
		Description: a.Description, CreatedAt: a.CreatedAt, UpdatedAt: a.UpdatedAt}
}

// ---------------------------------------------------------------- activity log

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

func toActivityLogResponse(a ActivityLog) ActivityLogResponse {
	var ip *string
	if a.IPAddress != nil {
		s := a.IPAddress.String()
		ip = &s
	}
	return ActivityLogResponse{UUID: a.UUID, LogTime: a.LogTime, LogAction: a.LogAction, LogTitle: a.LogTitle,
		LogDescription: a.LogDescription, Username: a.Username, RoleCode: a.RoleCode, IPAddress: ip,
		Route: a.ActionQuery, RequestID: a.LogSessionCode}
}

func optional(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
