package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"slices"
	"strings"
	"sync"
	"time"

	"pfm-backend/internal/platform/auth"
	"pfm-backend/internal/repository"
)

// permSet is an immutable, sorted permission list. Its version is a hash of
// the codes, so it changes exactly when the effective permissions change.
type permSet struct {
	codes   []string
	index   map[string]struct{}
	version string
}

func newPermSet(codes []string) *permSet {
	s := &permSet{index: make(map[string]struct{}, len(codes))}
	for _, c := range codes {
		if _, dup := s.index[c]; !dup && c != "" {
			s.index[c] = struct{}{}
			s.codes = append(s.codes, c)
		}
	}
	slices.Sort(s.codes)
	sum := sha256.Sum256([]byte(strings.Join(s.codes, "\n")))
	s.version = hex.EncodeToString(sum[:8])
	return s
}

func (s *permSet) Has(code string) bool {
	_, ok := s.index[code]
	return ok
}

// Codes never returns nil: clients always receive a JSON array.
func (s *permSet) Codes() []string { return append(make([]string, 0, len(s.codes)), s.codes...) }

func (s *permSet) Version() string { return s.version }

type cacheEntry struct {
	set *permSet
	exp time.Time
}

// PermissionCache resolves role permissions with a short TTL and explicit
// invalidation when role_action or the catalog changes (PRD §12.2).
type PermissionCache struct {
	roles   repository.RoleRepository
	actions repository.ActionRepository
	ttl     time.Duration

	mu      sync.RWMutex
	entries map[int64]cacheEntry
	all     *cacheEntry
}

func NewPermissionCache(roles repository.RoleRepository, actions repository.ActionRepository, ttl time.Duration) *PermissionCache {
	return &PermissionCache{roles: roles, actions: actions, ttl: ttl, entries: map[int64]cacheEntry{}}
}

// PermissionsForRole implements auth.PermissionResolver.
func (c *PermissionCache) PermissionsForRole(ctx context.Context, roleID int64) (auth.PermissionSet, error) {
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
	set := newPermSet(codes)
	c.mu.Lock()
	c.entries[roleID] = cacheEntry{set: set, exp: time.Now().Add(c.ttl)}
	c.mu.Unlock()
	return set, nil
}

// AllPermissions implements auth.PermissionResolver: the whole catalog (ADMIN).
func (c *PermissionCache) AllPermissions(ctx context.Context) (auth.PermissionSet, error) {
	c.mu.RLock()
	e := c.all
	c.mu.RUnlock()
	if e != nil && time.Now().Before(e.exp) {
		return e.set, nil
	}
	actions, err := c.actions.Catalog(ctx)
	if err != nil {
		return nil, err
	}
	codes := make([]string, 0, len(actions))
	for _, a := range actions {
		codes = append(codes, a.ActionCode)
	}
	set := newPermSet(codes)
	c.mu.Lock()
	c.all = &cacheEntry{set: set, exp: time.Now().Add(c.ttl)}
	c.mu.Unlock()
	return set, nil
}

// Invalidate drops the cached permissions of a role.
func (c *PermissionCache) Invalidate(roleID int64) {
	c.mu.Lock()
	delete(c.entries, roleID)
	c.mu.Unlock()
}

// InvalidateCatalog drops every cached set after the catalog changed.
func (c *PermissionCache) InvalidateCatalog() {
	c.mu.Lock()
	c.entries = map[int64]cacheEntry{}
	c.all = nil
	c.mu.Unlock()
}
