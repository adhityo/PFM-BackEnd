package iam

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	"pfm-backend/internal/apperr"
	"pfm-backend/internal/platform/audit"
	"pfm-backend/internal/platform/auth"
	"pfm-backend/internal/platform/database"
	"pfm-backend/internal/platform/security"
)

// UserService manages app_user (PRD §10.5: app_user is MASTER + actions).
type UserService struct {
	tx            *database.TxManager
	users         UserRepository
	roles         RoleRepository
	tokens        RefreshTokenRepository
	clients       ClientLookup
	audit         *audit.Recorder
	activationTTL time.Duration
	now           func() time.Time
}

func NewUserService(tx *database.TxManager, users UserRepository, roles RoleRepository, tokens RefreshTokenRepository,
	clients ClientLookup, rec *audit.Recorder, activationTTL time.Duration) *UserService {
	return &UserService{tx: tx, users: users, roles: roles, tokens: tokens, clients: clients, audit: rec,
		activationTTL: activationTTL, now: time.Now}
}

// UserSorts is the sort whitelist for user lists.
var UserSorts = []string{"username", "-username", "created_at", "-created_at"}

func (s *UserService) List(ctx context.Context, f UserFilter, p Paging) ([]UserResponse, int64, error) {
	actor, err := auth.MustActor(ctx)
	if err != nil {
		return nil, 0, err
	}
	f.ScopeClientID = actor.ScopeClientID()
	users, total, err := s.users.List(ctx, f, p)
	if err != nil {
		return nil, 0, err
	}
	now := s.now()
	out := make([]UserResponse, 0, len(users))
	for _, u := range users {
		out = append(out, toUserResponse(u, now))
	}
	return out, total, nil
}

// Get returns a user within the actor's tenant scope (404 otherwise).
func (s *UserService) Get(ctx context.Context, id uuid.UUID) (User, error) {
	actor, err := auth.MustActor(ctx)
	if err != nil {
		return User{}, err
	}
	return s.users.GetByUUID(ctx, id, actor.ScopeClientID())
}

// Response converts a user for output.
func (s *UserService) Response(u User) UserResponse { return toUserResponse(u, s.now()) }

// resolveRole checks that the role exists and the actor may assign it.
func (s *UserService) resolveRole(ctx context.Context, actor auth.Actor, roleUUID string) (Role, error) {
	id, _ := uuid.Parse(roleUUID)
	role, err := s.roles.GetByUUID(ctx, id)
	if err != nil {
		if errors.Is(err, apperr.ErrNotFound) {
			return Role{}, apperr.Invalid("role_uuid", "exists", "role tidak ditemukan")
		}
		return Role{}, err
	}
	if role.RoleCode == auth.RoleAdmin && !actor.IsAdmin() {
		return Role{}, apperr.Forbidden("hanya ADMIN yang dapat memberikan role ADMIN")
	}
	return role, nil
}

// resolveClient enforces tenant scope: scoped actors can only manage users of
// their own client.
func (s *UserService) resolveClient(ctx context.Context, actor auth.Actor, clientUUID string) (*int64, error) {
	scope := actor.ScopeClientID()
	if clientUUID == "" {
		return scope, nil
	}
	id, _ := uuid.Parse(clientUUID)
	cid, err := s.clients.ClientIDByUUID(ctx, id)
	if err != nil {
		return nil, err
	}
	if scope != nil && *scope != cid {
		return nil, apperr.Forbidden("tidak dapat mengelola pengguna client lain")
	}
	return &cid, nil
}

func (s *UserService) newActivationKey() (raw, hash string, exp time.Time, err error) {
	raw, hash, err = security.NewOpaqueToken()
	return raw, hash, s.now().Add(s.activationTTL), err
}

// Create inserts a user without a password and returns a one-time activation key.
func (s *UserService) Create(ctx context.Context, in CreateUserRequest) (UserCredentialResponse, error) {
	actor, err := auth.MustActor(ctx)
	if err != nil {
		return UserCredentialResponse{}, err
	}
	var out UserCredentialResponse
	err = s.tx.WithinTx(ctx, func(ctx context.Context) error {
		role, err := s.resolveRole(ctx, actor, in.RoleUUID)
		if err != nil {
			return err
		}
		clientID, err := s.resolveClient(ctx, actor, in.ClientUUID)
		if err != nil {
			return err
		}
		raw, hash, exp, err := s.newActivationKey()
		if err != nil {
			return err
		}
		id := newUUID()
		if _, err := s.users.Create(ctx, NewUser{UUID: id, Username: in.Username, Email: in.Email,
			FirstName: optional(in.FirstName), Description: optional(in.Description), Status: statusCode(in.Status),
			RoleID: role.ID, ClientID: clientID, IsSupervisor: in.IsSupervisor, ActivationKeyHash: hash,
			ActivationExpires: exp, Actor: actor.AuditName()}); err != nil {
			return err
		}
		u, err := s.users.GetByUUID(ctx, id, nil)
		if err != nil {
			return err
		}
		out = UserCredentialResponse{User: s.Response(u), ActivationKey: raw, ActivationExpiresAt: exp}
		return s.audit.Record(ctx, audit.Entry{Action: "app_user.create", Entity: "app_user", EntityUUID: id,
			Description: "role=" + role.RoleCode, Route: "POST /users"})
	})
	return out, err
}

// loadManaged loads a user the actor is allowed to modify.
func (s *UserService) loadManaged(ctx context.Context, actor auth.Actor, id uuid.UUID) (User, error) {
	u, err := s.users.GetByUUID(ctx, id, actor.ScopeClientID())
	if err != nil {
		return User{}, err
	}
	if u.RoleCode != nil && *u.RoleCode == auth.RoleAdmin && !actor.IsAdmin() {
		return User{}, apperr.Forbidden("hanya ADMIN yang dapat mengubah pengguna ADMIN")
	}
	return u, nil
}

func (s *UserService) Update(ctx context.Context, id uuid.UUID, in UpdateUserRequest, expected time.Time) (User, error) {
	actor, err := auth.MustActor(ctx)
	if err != nil {
		return User{}, err
	}
	var out User
	err = s.tx.WithinTx(ctx, func(ctx context.Context) error {
		cur, err := s.loadManaged(ctx, actor, id)
		if err != nil {
			return err
		}
		role, err := s.resolveRole(ctx, actor, in.RoleUUID)
		if err != nil {
			return err
		}
		clientID, err := s.resolveClient(ctx, actor, in.ClientUUID)
		if err != nil {
			return err
		}
		status := statusCode(in.Status)
		if cur.UUID == actor.UserUUID && (cur.RoleID == nil || *cur.RoleID != role.ID || status != StatusActive) {
			return apperr.Invalid("role_uuid", "self", "tidak dapat mengubah role atau menonaktifkan akun sendiri")
		}
		if _, err := s.users.Update(ctx, UserChange{UUID: id, Email: in.Email, FirstName: optional(in.FirstName),
			Description: optional(in.Description), Status: status, RoleID: role.ID, ClientID: clientID,
			IsSupervisor: in.IsSupervisor, Actor: actor.AuditName(), Expected: expected}); err != nil {
			return err
		}
		if status != StatusActive {
			if err := s.tokens.RevokeAllForUser(ctx, cur.ID); err != nil {
				return err
			}
		}
		out, err = s.users.GetByUUID(ctx, id, nil)
		if err != nil {
			return err
		}
		return s.audit.Record(ctx, audit.Entry{Action: "app_user.update", Entity: "app_user", EntityUUID: id,
			Description: "role=" + role.RoleCode + " status=" + in.Status, Route: "PUT /users/{uuid}"})
	})
	return out, err
}

func (s *UserService) Delete(ctx context.Context, id uuid.UUID, expected time.Time) error {
	actor, err := auth.MustActor(ctx)
	if err != nil {
		return err
	}
	if id == actor.UserUUID {
		return apperr.Conflict("tidak dapat menghapus akun sendiri")
	}
	return s.tx.WithinTx(ctx, func(ctx context.Context) error {
		if _, err := s.loadManaged(ctx, actor, id); err != nil {
			return err
		}
		uid, err := s.users.SoftDelete(ctx, id, actor.AuditName(), expected)
		if err != nil {
			return err
		}
		if err := s.tokens.RevokeAllForUser(ctx, uid); err != nil {
			return err
		}
		return s.audit.Record(ctx, audit.Entry{Action: "app_user.delete", Entity: "app_user", EntityUUID: id,
			Route: "DELETE /users/{uuid}"})
	})
}

// ResetCredential clears the password, ends all sessions and issues a new activation key.
func (s *UserService) ResetCredential(ctx context.Context, id uuid.UUID, expected time.Time) (UserCredentialResponse, error) {
	actor, err := auth.MustActor(ctx)
	if err != nil {
		return UserCredentialResponse{}, err
	}
	var out UserCredentialResponse
	err = s.tx.WithinTx(ctx, func(ctx context.Context) error {
		if _, err := s.loadManaged(ctx, actor, id); err != nil {
			return err
		}
		raw, hash, exp, err := s.newActivationKey()
		if err != nil {
			return err
		}
		uid, err := s.users.ResetCredential(ctx, id, hash, exp, actor.AuditName(), expected)
		if err != nil {
			return err
		}
		if err := s.tokens.RevokeAllForUser(ctx, uid); err != nil {
			return err
		}
		u, err := s.users.GetByUUID(ctx, id, nil)
		if err != nil {
			return err
		}
		out = UserCredentialResponse{User: s.Response(u), ActivationKey: raw, ActivationExpiresAt: exp}
		return s.audit.Record(ctx, audit.Entry{Action: "app_user.reset_password", Entity: "app_user", EntityUUID: id,
			Route: "POST /users/{uuid}/reset-password"})
	})
	return out, err
}

// SetLock locks (indefinitely) or unlocks a user.
func (s *UserService) SetLock(ctx context.Context, id uuid.UUID, lock bool, expected time.Time) (User, error) {
	actor, err := auth.MustActor(ctx)
	if err != nil {
		return User{}, err
	}
	if lock && id == actor.UserUUID {
		return User{}, apperr.Conflict("tidak dapat mengunci akun sendiri")
	}
	var out User
	err = s.tx.WithinTx(ctx, func(ctx context.Context) error {
		if _, err := s.loadManaged(ctx, actor, id); err != nil {
			return err
		}
		var until *time.Time
		action := "app_user.unlock"
		if lock {
			until, action = &lockForever, "app_user.lock"
		}
		uid, err := s.users.SetLockedUntil(ctx, id, until, actor.AuditName(), expected)
		if err != nil {
			return err
		}
		if lock {
			if err := s.tokens.RevokeAllForUser(ctx, uid); err != nil {
				return err
			}
		}
		out, err = s.users.GetByUUID(ctx, id, nil)
		if err != nil {
			return err
		}
		return s.audit.Record(ctx, audit.Entry{Action: action, Entity: "app_user", EntityUUID: id,
			Route: "POST /users/{uuid}/" + map[bool]string{true: "lock", false: "unlock"}[lock]})
	})
	return out, err
}
