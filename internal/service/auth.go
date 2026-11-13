package service

import (
	"context"
	"errors"
	"net/netip"
	"strings"
	"time"

	"github.com/google/uuid"

	"pfm-backend/internal/apperr"
	"pfm-backend/internal/dto"
	"pfm-backend/internal/model"
	"pfm-backend/internal/platform/audit"
	"pfm-backend/internal/platform/auth"
	"pfm-backend/internal/platform/database"
	"pfm-backend/internal/platform/security"
	"pfm-backend/internal/repository"
)

// AuthConfig holds authentication policy.
type AuthConfig struct {
	RefreshTTL       time.Duration
	ActivationTTL    time.Duration
	LoginMaxAttempts int
	LoginLockFor     time.Duration
}

// ClientInfo describes the caller of an unauthenticated endpoint.
type ClientInfo struct {
	IP        *netip.Addr
	UserAgent string
	RequestID string
}

// AuthService implements login, token refresh, logout, activation and password change.
type AuthService struct {
	tx     *database.TxManager
	users  repository.UserRepository
	tokens repository.RefreshTokenRepository
	roles  repository.RoleRepository
	hasher *security.PasswordHasher
	signer *security.JWTSigner
	audit  *audit.Recorder
	cfg    AuthConfig
	now    func() time.Time
}

func NewAuthService(tx *database.TxManager, users repository.UserRepository, tokens repository.RefreshTokenRepository, roles repository.RoleRepository,
	hasher *security.PasswordHasher, signer *security.JWTSigner, rec *audit.Recorder, cfg AuthConfig) *AuthService {
	return &AuthService{tx: tx, users: users, tokens: tokens, roles: roles, hasher: hasher, signer: signer,
		audit: rec, cfg: cfg, now: time.Now}
}

var errInvalidCredentials = apperr.Unauthenticated("username atau password salah")

func (s *AuthService) clientActor(ci ClientInfo, c *model.Credential) auth.Actor {
	a := auth.Actor{IP: ci.IP, UserAgent: ci.UserAgent, RequestID: ci.RequestID, Username: "anonymous"}
	if c != nil {
		a.UserID, a.UserUUID, a.Username, a.RoleCode = c.ID, c.UUID, c.Username, c.RoleCode
	}
	return a
}

// Login authenticates with username or email. Every failure returns the same
// error and costs the same password-hash time (PRD §12.1).
func (s *AuthService) Login(ctx context.Context, in dto.LoginRequest, ci ClientInfo) (dto.TokenResponse, error) {
	cred, err := s.users.CredentialByLogin(ctx, in.Login)
	if err != nil {
		if errors.Is(err, apperr.ErrNotFound) {
			s.hasher.VerifyDummy(in.Password)
			_ = s.audit.RecordAs(ctx, s.clientActor(ci, nil), audit.Entry{Action: "auth.login_failed", Entity: "app_user",
				Description: "username tidak dikenal", Route: "POST /auth/login"})
			return dto.TokenResponse{}, errInvalidCredentials
		}
		return dto.TokenResponse{}, err
	}
	now := s.now()
	if cred.PasswordHash == nil || cred.Status != model.StatusActive || (cred.LockedUntil != nil && cred.LockedUntil.After(now)) {
		s.hasher.VerifyDummy(in.Password)
		_ = s.audit.RecordAs(ctx, s.clientActor(ci, &cred), audit.Entry{Action: "auth.login_rejected", Entity: "app_user",
			EntityUUID: cred.UUID, Description: "akun tidak aktif, belum aktivasi, atau terkunci", Route: "POST /auth/login"})
		return dto.TokenResponse{}, errInvalidCredentials
	}
	match, rehash, err := s.hasher.Verify(in.Password, *cred.PasswordHash)
	if err != nil || !match {
		if ferr := s.users.RecordLoginFailure(ctx, cred.ID, s.cfg.LoginMaxAttempts, s.cfg.LoginLockFor); ferr != nil {
			return dto.TokenResponse{}, ferr
		}
		_ = s.audit.RecordAs(ctx, s.clientActor(ci, &cred), audit.Entry{Action: "auth.login_failed", Entity: "app_user",
			EntityUUID: cred.UUID, Description: "password salah", Route: "POST /auth/login"})
		return dto.TokenResponse{}, errInvalidCredentials
	}

	var out dto.TokenResponse
	err = s.tx.WithinTx(ctx, func(ctx context.Context) error {
		if rehash {
			h, err := s.hasher.Hash(in.Password)
			if err != nil {
				return err
			}
			if err := s.users.SetPassword(ctx, cred.ID, h, cred.UUID.String()); err != nil {
				return err
			}
		}
		if err := s.users.RecordLoginSuccess(ctx, cred.ID); err != nil {
			return err
		}
		out, _, err = s.issue(ctx, cred, uuid.New(), ci)
		if err != nil {
			return err
		}
		return s.audit.RecordAs(ctx, s.clientActor(ci, &cred), audit.Entry{Action: "auth.login", Entity: "app_user",
			EntityUUID: cred.UUID, Route: "POST /auth/login"})
	})
	return out, err
}

// issue creates an access token and a new refresh token in the given family.
// It returns the id of the stored refresh token.
func (s *AuthService) issue(ctx context.Context, cred model.Credential, family uuid.UUID, ci ClientInfo) (dto.TokenResponse, int64, error) {
	raw, hash, err := security.NewOpaqueToken()
	if err != nil {
		return dto.TokenResponse{}, 0, err
	}
	exp := s.now().Add(s.cfg.RefreshTTL)
	id, err := s.tokens.Insert(ctx, model.NewRefreshToken{UserID: cred.ID, FamilyID: family, TokenHash: hash,
		UserAgent: ci.UserAgent, IP: ci.IP, ExpiresAt: exp})
	if err != nil {
		return dto.TokenResponse{}, 0, err
	}
	access, _, err := s.signer.Sign(cred.UUID, cred.RoleCode, cred.ClientUUID)
	if err != nil {
		return dto.TokenResponse{}, 0, err
	}
	return dto.TokenResponse{AccessToken: access, TokenType: "Bearer", ExpiresIn: int64(s.signer.TTL().Seconds()),
		RefreshToken: raw, RefreshExpiresAt: exp}, id, nil
}

var errInvalidRefresh = apperr.Unauthenticated("refresh token tidak valid atau kedaluwarsa")

// Refresh rotates a refresh token. Presenting an already-rotated token is
// treated as theft: the whole token family is revoked.
func (s *AuthService) Refresh(ctx context.Context, raw string, ci ClientInfo) (dto.TokenResponse, error) {
	var out dto.TokenResponse
	reused := false
	err := s.tx.WithinTx(ctx, func(ctx context.Context) error {
		tok, err := s.tokens.GetByHashForUpdate(ctx, security.HashToken(raw))
		if err != nil {
			if errors.Is(err, apperr.ErrNotFound) {
				return errInvalidRefresh
			}
			return err
		}
		if tok.RevokedAt != nil {
			reused = true
			if err := s.tokens.RevokeFamily(ctx, tok.FamilyID); err != nil {
				return err
			}
			cred, cerr := s.users.CredentialByID(ctx, tok.UserID)
			var c *model.Credential
			if cerr == nil {
				c = &cred
			}
			return s.audit.RecordAs(ctx, s.clientActor(ci, c), audit.Entry{Action: "auth.refresh_reuse_detected",
				Entity: "auth_refresh_token", Description: "token lama dipakai ulang; seluruh sesi dicabut",
				Route: "POST /auth/refresh"})
		}
		if !tok.ExpiresAt.After(s.now()) {
			return errInvalidRefresh
		}
		cred, err := s.users.CredentialByID(ctx, tok.UserID)
		if err != nil || !cred.CanAuthenticate(s.now()) {
			if rerr := s.tokens.RevokeFamily(ctx, tok.FamilyID); rerr != nil {
				return rerr
			}
			reused = true // commit the revocation, then reject
			return nil
		}
		var newID int64
		out, newID, err = s.issue(ctx, cred, tok.FamilyID, ci)
		if err != nil {
			return err
		}
		return s.tokens.Revoke(ctx, tok.ID, &newID)
	})
	if err != nil {
		return dto.TokenResponse{}, err
	}
	if reused {
		return dto.TokenResponse{}, errInvalidRefresh
	}
	return out, nil
}

// Logout revokes the session (token family) of the given refresh token.
// It never reveals whether the token existed.
func (s *AuthService) Logout(ctx context.Context, raw string) error {
	return s.tx.WithinTx(ctx, func(ctx context.Context) error {
		tok, err := s.tokens.GetByHashForUpdate(ctx, security.HashToken(raw))
		if err != nil {
			if errors.Is(err, apperr.ErrNotFound) {
				return nil
			}
			return err
		}
		return s.tokens.RevokeFamily(ctx, tok.FamilyID)
	})
}

// LogoutAll revokes every session of the current user.
func (s *AuthService) LogoutAll(ctx context.Context) error {
	actor, err := auth.MustActor(ctx)
	if err != nil {
		return err
	}
	return s.tx.WithinTx(ctx, func(ctx context.Context) error {
		if err := s.tokens.RevokeAllForUser(ctx, actor.UserID); err != nil {
			return err
		}
		return s.audit.Record(ctx, audit.Entry{Action: "auth.logout_all", Entity: "app_user", EntityUUID: actor.UserUUID,
			Route: "POST /auth/logout-all"})
	})
}

var errInvalidActivation = apperr.Invalid("activation_key", "valid", "kunci aktivasi tidak valid atau kedaluwarsa")

// Activate sets the first password (or a reset password) using a one-time key.
func (s *AuthService) Activate(ctx context.Context, in dto.ActivateRequest, ci ClientInfo) error {
	return s.tx.WithinTx(ctx, func(ctx context.Context) error {
		target, err := s.users.ActivationTargetForUpdate(ctx, security.HashToken(in.ActivationKey))
		if err != nil {
			if errors.Is(err, apperr.ErrNotFound) {
				return errInvalidActivation
			}
			return err
		}
		if target.ExpiresAt == nil || !target.ExpiresAt.After(s.now()) {
			return errInvalidActivation
		}
		if err := checkPasswordPolicy(in.NewPassword, target.Username); err != nil {
			return err
		}
		hash, err := s.hasher.Hash(in.NewPassword)
		if err != nil {
			return err
		}
		if err := s.users.SetPassword(ctx, target.ID, hash, target.UUID.String()); err != nil {
			return err
		}
		if err := s.tokens.RevokeAllForUser(ctx, target.ID); err != nil {
			return err
		}
		a := s.clientActor(ci, &model.Credential{ID: target.ID, UUID: target.UUID, Username: target.Username})
		return s.audit.RecordAs(ctx, a, audit.Entry{Action: "auth.activate", Entity: "app_user", EntityUUID: target.UUID,
			Route: "POST /auth/activate"})
	})
}

// ChangePassword requires the current password and ends every session.
func (s *AuthService) ChangePassword(ctx context.Context, in dto.ChangePasswordRequest) error {
	actor, err := auth.MustActor(ctx)
	if err != nil {
		return err
	}
	cred, err := s.users.CredentialByID(ctx, actor.UserID)
	if err != nil {
		return err
	}
	if cred.PasswordHash == nil {
		return apperr.Forbidden("akun belum diaktivasi")
	}
	match, _, err := s.hasher.Verify(in.OldPassword, *cred.PasswordHash)
	if err != nil || !match {
		return apperr.Invalid("old_password", "match", "password lama salah")
	}
	if in.OldPassword == in.NewPassword {
		return apperr.Invalid("new_password", "different", "password baru harus berbeda dari password lama")
	}
	if err := checkPasswordPolicy(in.NewPassword, cred.Username); err != nil {
		return err
	}
	hash, err := s.hasher.Hash(in.NewPassword)
	if err != nil {
		return err
	}
	return s.tx.WithinTx(ctx, func(ctx context.Context) error {
		if err := s.users.SetPassword(ctx, cred.ID, hash, actor.AuditName()); err != nil {
			return err
		}
		if err := s.tokens.RevokeAllForUser(ctx, cred.ID); err != nil {
			return err
		}
		return s.audit.Record(ctx, audit.Entry{Action: "auth.change_password", Entity: "app_user", EntityUUID: cred.UUID,
			Route: "POST /auth/change-password"})
	})
}

// Me returns the current user and effective permissions.
func (s *AuthService) Me(ctx context.Context) (dto.MeResponse, error) {
	actor, err := auth.MustActor(ctx)
	if err != nil {
		return dto.MeResponse{}, err
	}
	u, err := s.users.GetByUUID(ctx, actor.UserUUID, nil)
	if err != nil {
		return dto.MeResponse{}, err
	}
	perms := []string{}
	switch {
	case actor.IsAdmin():
		perms = []string{"*"}
	case actor.RoleID != nil:
		codes, err := s.roles.PermissionCodes(ctx, *actor.RoleID)
		if err != nil {
			return dto.MeResponse{}, err
		}
		perms = permSetFrom(codes).list()
	}
	return dto.MeResponse{User: dto.UserFromModel(u, s.now()), Permissions: perms}, nil
}

func permSetFrom(codes []string) permSet {
	s := make(permSet, len(codes))
	for _, c := range codes {
		s[c] = struct{}{}
	}
	return s
}

// checkPasswordPolicy enforces rules beyond length (validated on the DTO).
func checkPasswordPolicy(password, username string) error {
	if username != "" && len(username) >= 3 && strings.Contains(strings.ToLower(password), strings.ToLower(username)) {
		return apperr.Invalid("new_password", "password", "password tidak boleh mengandung username")
	}
	classes := 0
	var lower, upper, digit, other bool
	for _, r := range password {
		switch {
		case r >= 'a' && r <= 'z':
			lower = true
		case r >= 'A' && r <= 'Z':
			upper = true
		case r >= '0' && r <= '9':
			digit = true
		default:
			other = true
		}
	}
	for _, b := range []bool{lower, upper, digit, other} {
		if b {
			classes++
		}
	}
	if classes < 3 {
		return apperr.Invalid("new_password", "password", "password harus memuat minimal 3 dari: huruf kecil, huruf besar, angka, simbol")
	}
	return nil
}
