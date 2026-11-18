package service

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	"pfm-backend/internal/apperr"
	"pfm-backend/internal/dto"
	"pfm-backend/internal/model"
	"pfm-backend/internal/platform/audit"
	"pfm-backend/internal/platform/auth"
	"pfm-backend/internal/platform/crud"
	"pfm-backend/internal/platform/database"
	"pfm-backend/internal/platform/security"
	"pfm-backend/internal/repository"
)

// lockForever is used for administrative locks.
var lockForever = time.Date(9999, 12, 31, 0, 0, 0, 0, time.UTC)

// UserService manages app_user (PRD §10.5: app_user is MASTER + actions).
type UserService struct {
	tx            *database.TxManager
	users         repository.UserRepository
	roles         repository.RoleRepository
	tokens        repository.RefreshTokenRepository
	clients       repository.ClientLookup
	audit         *audit.Recorder
	activationTTL time.Duration
	now           func() time.Time
}

func NewUserService(tx *database.TxManager, users repository.UserRepository, roles repository.RoleRepository, tokens repository.RefreshTokenRepository,
	clients repository.ClientLookup, rec *audit.Recorder, activationTTL time.Duration) *UserService {
	return &UserService{tx: tx, users: users, roles: roles, tokens: tokens, clients: clients, audit: rec,
		activationTTL: activationTTL, now: time.Now}
}

// UserSorts is the sort whitelist for user lists.
var UserSorts = []string{"username", "-username", "created_at", "-created_at"}

func (s *UserService) List(ctx context.Context, f model.UserFilter, p model.Paging) ([]dto.UserResponse, int64, error) {
	actor, err := auth.MustActor(ctx)
	if err != nil {
		return nil, 0, err
	}
	f.ScopeClientUUID = actor.ScopeClientUUID()
	users, total, err := s.users.List(ctx, f, p)
	if err != nil {
		return nil, 0, err
	}
	now := s.now()
	out := make([]dto.UserResponse, 0, len(users))
	for _, u := range users {
		out = append(out, dto.UserFromModel(u, now))
	}
	return out, total, nil
}

// Get returns a user within the actor's tenant scope (404 otherwise).
func (s *UserService) Get(ctx context.Context, id uuid.UUID) (model.User, error) {
	actor, err := auth.MustActor(ctx)
	if err != nil {
		return model.User{}, err
	}
	return s.users.GetByUUID(ctx, id, actor.ScopeClientUUID())
}

// Response converts a user for output.
func (s *UserService) Response(u model.User) dto.UserResponse { return dto.UserFromModel(u, s.now()) }

// resolveRole checks that the role exists and the actor may assign it.
func (s *UserService) resolveRole(ctx context.Context, actor auth.Actor, roleUUID string) (model.Role, error) {
	id, _ := uuid.Parse(roleUUID)
	role, err := s.roles.GetByUUID(ctx, id)
	if err != nil {
		if errors.Is(err, apperr.ErrNotFound) {
			return model.Role{}, apperr.Invalid("role_uuid", "exists", "role tidak ditemukan")
		}
		return model.Role{}, err
	}
	if role.RoleCode == auth.RoleAdmin && !actor.IsAdmin() {
		return model.Role{}, apperr.Forbidden("hanya ADMIN yang dapat memberikan role ADMIN")
	}
	if err := requireCovers(ctx, s.roles, actor, role.ID,
		"tidak dapat memberikan role yang memiliki permission di luar milik Anda"); err != nil {
		return model.Role{}, err
	}
	return role, nil
}

// resolveClient enforces tenant scope: scoped actors can only manage users of
// their own client. The client lives in pfm_core and is checked there (no FK
// across databases).
func (s *UserService) resolveClient(ctx context.Context, actor auth.Actor, clientUUID string) (*uuid.UUID, error) {
	scope := actor.ScopeClientUUID()
	if clientUUID == "" {
		return scope, nil
	}
	id, _ := uuid.Parse(clientUUID)
	if _, err := s.clients.ClientIDByUUID(ctx, id); err != nil {
		return nil, err
	}
	if scope != nil && *scope != id {
		return nil, apperr.Forbidden("tidak dapat mengelola pengguna client lain")
	}
	return &id, nil
}

func (s *UserService) newActivationKey() (raw, hash string, exp time.Time, err error) {
	raw, hash, err = security.NewOpaqueToken()
	return raw, hash, s.now().Add(s.activationTTL), err
}

// Create inserts a user without a password and returns a one-time activation key.
func (s *UserService) Create(ctx context.Context, in dto.CreateUserRequest) (dto.UserCredentialResponse, error) {
	actor, err := auth.MustActor(ctx)
	if err != nil {
		return dto.UserCredentialResponse{}, err
	}
	var out dto.UserCredentialResponse
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
		id := crud.NewID()
		if _, err := s.users.Create(ctx, model.NewUser{UUID: id, Username: in.Username, Email: in.Email,
			FirstName: crud.Text(in.FirstName), Description: crud.Text(in.Description), Status: model.StatusCode(in.Status),
			RoleID: role.ID, ClientUUID: clientID, IsSupervisor: in.IsSupervisor, ActivationKeyHash: hash,
			ActivationExpires: exp, Actor: actor.AuditName()}); err != nil {
			return err
		}
		u, err := s.users.GetByUUID(ctx, id, nil)
		if err != nil {
			return err
		}
		out = dto.UserCredentialResponse{User: s.Response(u), ActivationKey: raw, ActivationExpiresAt: exp}
		return s.audit.Record(ctx, audit.Entry{Action: "app_user.create", Entity: "app_user", EntityUUID: id,
			Description: "role=" + role.RoleCode, Route: "POST /users"})
	})
	return out, err
}

// loadManaged loads a user the actor is allowed to modify.
func (s *UserService) loadManaged(ctx context.Context, actor auth.Actor, id uuid.UUID) (model.User, error) {
	u, err := s.users.GetByUUID(ctx, id, actor.ScopeClientUUID())
	if err != nil {
		return model.User{}, err
	}
	if u.RoleCode != nil && *u.RoleCode == auth.RoleAdmin && !actor.IsAdmin() {
		return model.User{}, apperr.Forbidden("hanya ADMIN yang dapat mengubah pengguna ADMIN")
	}
	if u.RoleID != nil {
		if err := requireCovers(ctx, s.roles, actor, *u.RoleID,
			"tidak dapat mengelola pengguna yang memiliki permission di luar milik Anda"); err != nil {
			return model.User{}, err
		}
	}
	return u, nil
}

func (s *UserService) Update(ctx context.Context, id uuid.UUID, in dto.UpdateUserRequest, expected time.Time) (model.User, error) {
	actor, err := auth.MustActor(ctx)
	if err != nil {
		return model.User{}, err
	}
	var out model.User
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
		status := model.StatusCode(in.Status)
		if cur.UUID == actor.UserUUID && (cur.RoleID == nil || *cur.RoleID != role.ID || status != model.StatusActive) {
			return apperr.Invalid("role_uuid", "self", "tidak dapat mengubah role atau menonaktifkan akun sendiri")
		}
		if _, err := s.users.Update(ctx, model.UserChange{UUID: id, Email: in.Email, FirstName: crud.Text(in.FirstName),
			Description: crud.Text(in.Description), Status: status, RoleID: role.ID, ClientUUID: clientID,
			IsSupervisor: in.IsSupervisor, Actor: actor.AuditName(), Expected: expected}); err != nil {
			return err
		}
		if status != model.StatusActive {
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
func (s *UserService) ResetCredential(ctx context.Context, id uuid.UUID, expected time.Time) (dto.UserCredentialResponse, error) {
	actor, err := auth.MustActor(ctx)
	if err != nil {
		return dto.UserCredentialResponse{}, err
	}
	var out dto.UserCredentialResponse
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
		out = dto.UserCredentialResponse{User: s.Response(u), ActivationKey: raw, ActivationExpiresAt: exp}
		return s.audit.Record(ctx, audit.Entry{Action: "app_user.reset_password", Entity: "app_user", EntityUUID: id,
			Route: "POST /users/{uuid}/reset-password"})
	})
	return out, err
}

// SetLock locks (indefinitely) or unlocks a user.
func (s *UserService) SetLock(ctx context.Context, id uuid.UUID, lock bool, expected time.Time) (model.User, error) {
	actor, err := auth.MustActor(ctx)
	if err != nil {
		return model.User{}, err
	}
	if lock && id == actor.UserUUID {
		return model.User{}, apperr.Conflict("tidak dapat mengunci akun sendiri")
	}
	var out model.User
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
