package auth

import (
	"context"
	"net/netip"
	"strings"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"

	"pfm-backend/internal/apperr"
	"pfm-backend/internal/platform/httpx"
	"pfm-backend/internal/platform/security"
)

// ActorLoader resolves the current state of a user from the database so that
// deleted, locked or re-assigned users lose access immediately.
type ActorLoader interface {
	LoadActor(ctx context.Context, userUUID uuid.UUID) (Actor, error)
}

// PermissionResolver returns the permission set of a role.
type PermissionResolver interface {
	PermissionsForRole(ctx context.Context, roleID int64) (PermissionSet, error)
}

// Authenticator verifies bearer tokens and attaches the Actor to the request.
type Authenticator struct {
	signer *security.JWTSigner
	users  ActorLoader
	perms  PermissionResolver
}

func NewAuthenticator(signer *security.JWTSigner, users ActorLoader, perms PermissionResolver) *Authenticator {
	return &Authenticator{signer: signer, users: users, perms: perms}
}

// Middleware rejects requests without a valid access token.
func (a *Authenticator) Middleware() fiber.Handler {
	return func(c fiber.Ctx) error {
		raw := c.Get(fiber.HeaderAuthorization)
		token, ok := strings.CutPrefix(raw, "Bearer ")
		if !ok || token == "" {
			return apperr.Unauthenticated("token akses tidak ada")
		}
		claims, err := a.signer.Verify(strings.TrimSpace(token))
		if err != nil {
			return apperr.Unauthenticated("token akses tidak valid atau kedaluwarsa")
		}
		userUUID, _ := uuid.Parse(claims.Subject)
		actor, err := a.users.LoadActor(c.Context(), userUUID)
		if err != nil {
			if e, ok := apperr.As(err); ok && (e.Kind == apperr.KindNotFound || e.Kind == apperr.KindUnauthenticated) {
				return apperr.Unauthenticated("akun tidak aktif")
			}
			return err
		}
		if actor.RoleID != nil && !actor.IsAdmin() {
			set, err := a.perms.PermissionsForRole(c.Context(), *actor.RoleID)
			if err != nil {
				return err
			}
			actor = actor.WithPermissions(set)
		}
		if ip, err := netip.ParseAddr(c.IP()); err == nil {
			actor.IP = &ip
		}
		actor.UserAgent = truncate(c.Get(fiber.HeaderUserAgent), 255)
		actor.RequestID = httpx.RequestID(c)

		c.SetContext(WithActor(c.Context(), actor))
		return c.Next()
	}
}

// RequirePermission rejects actors lacking the permission with 403.
func RequirePermission(code string) fiber.Handler {
	return func(c fiber.Ctx) error {
		actor, ok := ActorFrom(c.Context())
		if !ok {
			return apperr.ErrUnauthenticated
		}
		if !actor.Can(code) {
			return apperr.Forbidden("izin " + code + " diperlukan")
		}
		return c.Next()
	}
}

// MustActor returns the authenticated actor or an Unauthenticated error.
func MustActor(ctx context.Context) (Actor, error) {
	a, ok := ActorFrom(ctx)
	if !ok {
		return Actor{}, apperr.ErrUnauthenticated
	}
	return a, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
