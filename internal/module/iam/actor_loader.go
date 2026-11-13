package iam

import (
	"context"
	"time"

	"github.com/google/uuid"

	"pfm-backend/internal/apperr"
	"pfm-backend/internal/platform/auth"
)

// ActorLoader implements auth.ActorLoader using the current database state,
// so deactivated, locked, reset or deleted users lose access immediately.
type ActorLoader struct {
	users UserRepository
	now   func() time.Time
}

func NewActorLoader(users UserRepository) *ActorLoader {
	return &ActorLoader{users: users, now: time.Now}
}

func (l *ActorLoader) LoadActor(ctx context.Context, userUUID uuid.UUID) (auth.Actor, error) {
	c, err := l.users.CredentialByUUID(ctx, userUUID)
	if err != nil {
		return auth.Actor{}, err
	}
	if !c.CanAuthenticate(l.now()) {
		return auth.Actor{}, apperr.Unauthenticated("akun tidak aktif")
	}
	return auth.Actor{
		UserID: c.ID, UserUUID: c.UUID, Username: c.Username, RoleID: c.RoleID, RoleCode: c.RoleCode,
		ClientID: c.ClientID, ClientUUID: c.ClientUUID,
	}, nil
}
