// Package dto holds HTTP request/response bodies and their mappers.
package dto

import (
	"strings"
	"time"
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

type MeResponse struct {
	User        UserResponse `json:"user"`
	Permissions []string     `json:"permissions"`
}
