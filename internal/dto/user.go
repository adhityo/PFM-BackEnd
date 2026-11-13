package dto

import (
	"strings"
	"time"

	"github.com/google/uuid"

	"pfm-backend/internal/model"
)

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

// UserFromModel maps a user; now decides whether a lock is still active.
func UserFromModel(u model.User, now time.Time) UserResponse {
	return UserResponse{UUID: u.UUID, Username: u.Username, Email: u.Email, FirstName: u.FirstName,
		Description: u.Description, Status: model.StatusName(u.Status), RoleUUID: u.RoleUUID, RoleCode: u.RoleCode,
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
