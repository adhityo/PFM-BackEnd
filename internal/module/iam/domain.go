// Package iam implements identity & access: users, roles, permissions,
// authentication and the activity log (PRD §10.5 IAM, §12).
package iam

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

func statusName(s *int16) string {
	if s != nil && *s == StatusActive {
		return "active"
	}
	return "inactive"
}

func statusCode(name string) int16 {
	if name == "active" {
		return StatusActive
	}
	return StatusInactive
}

// lockForever is used for administrative locks.
var lockForever = time.Date(9999, 12, 31, 0, 0, 0, 0, time.UTC)

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
	ClientID     *int64
	ClientUUID   *uuid.UUID
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
	ClientID     *int64
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
	ScopeClientID *int64
	Q             *string
	RoleID        *int64
	Status        *int16
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
	ClientID          *int64
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
	ClientID     *int64
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

// Paging is an offset/limit request for repositories.
type Paging struct {
	Sort   string
	Limit  int32
	Offset int32
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
