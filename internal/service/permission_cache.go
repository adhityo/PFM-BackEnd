package service

import (
	"context"
	"sort"
	"sync"
	"time"

	"pfm-backend/internal/platform/auth"
	"pfm-backend/internal/repository"
)

type permSet map[string]struct{}

func (s permSet) Has(code string) bool {
	_, ok := s[code]
	return ok
}

func (s permSet) list() []string {
	out := make([]string, 0, len(s))
	for k := range s {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

type cacheEntry struct {
	set permSet
	exp time.Time
}

// PermissionCache resolves role permissions with a short TTL and explicit
// invalidation when role_action changes (PRD §12.2).
type PermissionCache struct {
	roles repository.RoleRepository
	ttl   time.Duration

	mu      sync.RWMutex
	entries map[int64]cacheEntry
}

func NewPermissionCache(roles repository.RoleRepository, ttl time.Duration) *PermissionCache {
	return &PermissionCache{roles: roles, ttl: ttl, entries: map[int64]cacheEntry{}}
}

// PermissionsForRole implements auth.PermissionResolver.
func (c *PermissionCache) PermissionsForRole(ctx context.Context, roleID int64) (auth.PermissionSet, error) {
	return c.get(ctx, roleID)
}

func (c *PermissionCache) get(ctx context.Context, roleID int64) (permSet, error) {
	c.mu.RLock()
	e, ok := c.entries[roleID]
	c.mu.RUnlock()
	if ok && time.Now().Before(e.exp) {
		return e.set, nil
	}
	codes, err := c.roles.PermissionCodes(ctx, roleID)
	if err != nil {
		return nil, err
	}
	set := make(permSet, len(codes))
	for _, code := range codes {
		set[code] = struct{}{}
	}
	c.mu.Lock()
	c.entries[roleID] = cacheEntry{set: set, exp: time.Now().Add(c.ttl)}
	c.mu.Unlock()
	return set, nil
}

// Invalidate drops the cached permissions of a role.
func (c *PermissionCache) Invalidate(roleID int64) {
	c.mu.Lock()
	delete(c.entries, roleID)
	c.mu.Unlock()
}
