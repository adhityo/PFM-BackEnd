package service

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/google/uuid"

	"pfm-backend/internal/apperr"
	"pfm-backend/internal/platform/auth"
	"pfm-backend/internal/repository"
)

// ActorLoader implements auth.ActorLoader using the current database state,
// so deactivated, locked, reset or deleted users lose access immediately.
// Users live in pfm_security and reference their client (pfm_core) by UUID;
// the core id used to scope business data is resolved here and cached.
type ActorLoader struct {
	users   repository.UserRepository
	clients repository.ClientLookup
	now     func() time.Time
	ttl     time.Duration

	mu    sync.Mutex
	cache map[uuid.UUID]cachedClient
}

type cachedClient struct {
	id      int64
	expires time.Time
}

func NewActorLoader(users repository.UserRepository, clients repository.ClientLookup) *ActorLoader {
	return &ActorLoader{users: users, clients: clients, now: time.Now, ttl: time.Minute, cache: map[uuid.UUID]cachedClient{}}
}

func (l *ActorLoader) LoadActor(ctx context.Context, userUUID uuid.UUID) (auth.Actor, error) {
	c, err := l.users.CredentialByUUID(ctx, userUUID)
	if err != nil {
		return auth.Actor{}, err
	}
	if !c.CanAuthenticate(l.now()) {
		return auth.Actor{}, apperr.Unauthenticated("akun tidak aktif")
	}
	a := auth.Actor{UserID: c.ID, UserUUID: c.UUID, Username: c.Username, RoleID: c.RoleID, RoleCode: c.RoleCode,
		ClientUUID: c.ClientUUID}
	if c.ClientUUID != nil {
		id, err := l.clientID(ctx, *c.ClientUUID)
		if err != nil {
			return auth.Actor{}, err
		}
		a.ClientID = &id
	}
	return a, nil
}

// clientID fails closed: a user whose client no longer exists cannot log in
// (otherwise a missing client would mean "no tenant filter").
func (l *ActorLoader) clientID(ctx context.Context, id uuid.UUID) (int64, error) {
	now := l.now()
	l.mu.Lock()
	hit, ok := l.cache[id]
	l.mu.Unlock()
	if ok && now.Before(hit.expires) {
		return hit.id, nil
	}
	cid, err := l.clients.ClientIDByUUID(ctx, id)
	if err != nil {
		var ae *apperr.Error
		if errors.As(err, &ae) && (ae.Kind == apperr.KindValidation || ae.Kind == apperr.KindNotFound) {
			return 0, apperr.Unauthenticated("client akun tidak aktif")
		}
		return 0, err
	}
	l.mu.Lock()
	l.cache[id] = cachedClient{id: cid, expires: now.Add(l.ttl)}
	l.mu.Unlock()
	return cid, nil
}
