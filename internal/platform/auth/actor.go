// Package auth carries the authenticated actor, authentication/authorization
// middleware and a permission-aware router.
package auth

import (
	"context"
	"net/netip"

	"github.com/google/uuid"
)

// RoleAdmin is the super administrator role code; it holds every permission.
const RoleAdmin = "ADMIN"

// PermPIIRead allows reading unmasked PII (PRD §12.2).
const PermPIIRead = "pii:read"

// Actor is the authenticated user performing a request.
type Actor struct {
	UserID     int64
	UserUUID   uuid.UUID
	Username   string
	RoleID     *int64
	RoleCode   string
	ClientID   *int64
	ClientUUID *uuid.UUID
	IP         *netip.Addr
	UserAgent  string
	RequestID  string

	perms PermissionSet
}

// PermissionSet answers permission questions for an actor.
type PermissionSet interface {
	Has(code string) bool
}

// AuditName is stored in created_by/updated_by/deleted_by columns.
func (a Actor) AuditName() string { return a.UserUUID.String() }

// IsAdmin reports whether the actor is a super administrator.
func (a Actor) IsAdmin() bool { return a.RoleCode == RoleAdmin }

// Can reports whether the actor holds a permission.
func (a Actor) Can(code string) bool {
	if a.IsAdmin() {
		return true
	}
	return a.perms != nil && a.perms.Has(code)
}

// ScopeClientID returns the tenant filter for queries: nil means global access.
func (a Actor) ScopeClientID() *int64 {
	if a.IsAdmin() {
		return nil
	}
	return a.ClientID
}

// WithPermissions returns a copy of the actor bound to a permission set.
func (a Actor) WithPermissions(p PermissionSet) Actor {
	a.perms = p
	return a
}

type actorKey struct{}

// WithActor stores the actor in ctx.
func WithActor(ctx context.Context, a Actor) context.Context {
	return context.WithValue(ctx, actorKey{}, a)
}

// ActorFrom returns the actor stored in ctx.
func ActorFrom(ctx context.Context) (Actor, bool) {
	a, ok := ctx.Value(actorKey{}).(Actor)
	return a, ok
}

// SystemActor is used by background jobs and seeding.
func SystemActor() Actor {
	return Actor{Username: "system", RoleCode: RoleAdmin}
}
