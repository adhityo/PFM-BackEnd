// Package dto holds HTTP request/response bodies and their mappers.
package dto

import (
	"strings"
	"time"

	"github.com/google/uuid"
)

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

// MeResponse bootstraps a frontend session: who the user is and what they
// may do. Pages and buttons are rendered from Permissions (ADMIN receives the
// whole catalog). Cache it and reload when the X-Permissions-Version header
// of any API response differs from PermissionsVersion.
type MeResponse struct {
	User               UserResponse `json:"user"`
	Role               *MeRole      `json:"role"`
	IsAdmin            bool         `json:"is_admin"`
	Permissions        []string     `json:"permissions"`
	PermissionsVersion string       `json:"permissions_version"`
}

// MeRole is the user's role; an inactive role grants no permission.
type MeRole struct {
	UUID    uuid.UUID `json:"uuid"`
	Code    string    `json:"code"`
	Name    string    `json:"name"`
	MenuURL *string   `json:"menu_url"`
	Active  bool      `json:"active"`
}

// ForgotPasswordRequest asks for a password reset link by email.
type ForgotPasswordRequest struct {
	Email string `json:"email" validate:"required,email,max=254"`
}

func (r *ForgotPasswordRequest) Normalize() { r.Email = strings.ToLower(strings.TrimSpace(r.Email)) }
