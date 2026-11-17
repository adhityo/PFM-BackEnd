// Package model holds domain types shared by repositories and services.
package model

import (
	"net/netip"
	"time"

	"github.com/google/uuid"
)

// Status values shared by app_user and role (legacy data: 1 = active).
const (
	StatusInactive int16 = 0
	StatusActive   int16 = 1
)

// StatusName maps a status column to its API value.
func StatusName(s *int16) string {
	if s != nil && *s == StatusActive {
		return "active"
	}
	return "inactive"
}

// StatusCode maps an API status value to the column value.
func StatusCode(name string) int16 {
	if name == "active" {
		return StatusActive
	}
	return StatusInactive
}

// Paging is an offset/limit request for repositories.
type Paging struct {
	Sort   string
	Limit  int32
	Offset int32
}

// Credential is the subset of app_user needed for authentication.
type Credential struct {
	ID           int64
	UUID         uuid.UUID
	Username     string
	Status       int16
	PasswordHash *string
	LockedUntil  *time.Time
	RoleID       *int64
	RoleCode     string
	ClientUUID   *uuid.UUID // client in pfm_core
}

// CanAuthenticate reports whether the account may hold a session at time now.
func (c Credential) CanAuthenticate(now time.Time) bool {
	if c.Status != StatusActive || c.PasswordHash == nil {
		return false
	}
	return c.LockedUntil == nil || !c.LockedUntil.After(now)
}

// User is an app_user as exposed by the API.
type User struct {
	ID           int64
	UUID         uuid.UUID
	Username     string
	Email        string
	FirstName    *string
	Description  *string
	Status       *int16
	RoleID       *int64
	RoleUUID     *uuid.UUID
	RoleCode     *string
	ClientUUID   *uuid.UUID
	IsSupervisor bool
	PasswordSet  bool
	LockedUntil  *time.Time
	LastLoginAt  *time.Time
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// UserFilter holds list filters for users.
type UserFilter struct {
	ScopeClientUUID *uuid.UUID
	Q               *string
	RoleID          *int64
	Status          *int16
}

// NewUser is the data needed to insert a user.
type NewUser struct {
	UUID              uuid.UUID
	Username          string
	Email             string
	FirstName         *string
	Description       *string
	Status            int16
	RoleID            int64
	ClientUUID        *uuid.UUID
	IsSupervisor      bool
	ActivationKeyHash string
	ActivationExpires time.Time
	Actor             string
}

// UserChange is the data needed to update a user.
type UserChange struct {
	UUID         uuid.UUID
	Email        string
	FirstName    *string
	Description  *string
	Status       int16
	RoleID       int64
	ClientUUID   *uuid.UUID
	IsSupervisor bool
	Actor        string
	Expected     time.Time
}

// ActivationTarget is a user found by activation key.
type ActivationTarget struct {
	ID        int64
	UUID      uuid.UUID
	Username  string
	ExpiresAt *time.Time
}

// Role is a row of `role`.
type Role struct {
	ID          int64
	UUID        uuid.UUID
	RoleCode    string
	RoleName    string
	Description *string
	MenuURL     *string
	Status      *int16
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// RoleInput is used for create and update.
type RoleInput struct {
	RoleCode    string
	RoleName    string
	Description *string
	MenuURL     *string
	Status      int16
	Actor       string
}

// Action is a permission catalog entry (`action`).
type Action struct {
	ID          int64
	UUID        uuid.UUID
	ActionCode  string
	ActionName  string
	Description *string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// ActionInput is used for create and update.
type ActionInput struct {
	ActionCode  string
	ActionName  string
	Description *string
	Actor       string
}

// RefreshToken is a stored (hashed) refresh token.
type RefreshToken struct {
	ID        int64
	UserID    int64
	FamilyID  uuid.UUID
	ExpiresAt time.Time
	RevokedAt *time.Time
}

// NewRefreshToken is the data needed to store a refresh token.
type NewRefreshToken struct {
	UserID    int64
	FamilyID  uuid.UUID
	TokenHash string
	UserAgent string
	IP        *netip.Addr
	ExpiresAt time.Time
}

// ActivityLog is an audit entry.
type ActivityLog struct {
	UUID           uuid.UUID
	ActionQuery    *string
	IPAddress      *netip.Addr
	LogAction      *string
	LogDescription *string
	LogSessionCode *string
	LogTime        *time.Time
	LogTitle       *string
	RoleCode       *string
	Username       *string
}

// ActivityLogFilter holds list filters for the activity log.
type ActivityLogFilter struct {
	Username  *string
	LogAction *string
	From      *time.Time
	To        *time.Time
}
