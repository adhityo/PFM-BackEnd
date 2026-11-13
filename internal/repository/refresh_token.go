package repository

import (
	"context"

	"github.com/google/uuid"

	"pfm-backend/internal/db/sqlc"
	"pfm-backend/internal/model"
	"pfm-backend/internal/platform/database"
)

// RefreshTokenRepository persists auth_refresh_token.
type RefreshTokenRepository interface {
	Insert(ctx context.Context, t model.NewRefreshToken) (int64, error)
	GetByHashForUpdate(ctx context.Context, hash string) (model.RefreshToken, error)
	Revoke(ctx context.Context, id int64, replacedBy *int64) error
	RevokeFamily(ctx context.Context, family uuid.UUID) error
	RevokeAllForUser(ctx context.Context, userID int64) error
}

type refreshTokenRepository struct{ tx *database.TxManager }

func NewRefreshTokenRepository(tx *database.TxManager) RefreshTokenRepository {
	return &refreshTokenRepository{tx: tx}
}

func (r *refreshTokenRepository) Insert(ctx context.Context, t model.NewRefreshToken) (int64, error) {
	var ua *string
	if t.UserAgent != "" {
		ua = &t.UserAgent
	}
	id, err := r.tx.Queries(ctx).InsertRefreshToken(ctx, sqlc.InsertRefreshTokenParams{
		Uuid: newUUID(), UserID: t.UserID, FamilyID: t.FamilyID, TokenHash: t.TokenHash,
		UserAgent: ua, IpAddress: t.IP, ExpiresAt: t.ExpiresAt,
	})
	return id, database.MapError("insert refresh token", err)
}

func (r *refreshTokenRepository) GetByHashForUpdate(ctx context.Context, hash string) (model.RefreshToken, error) {
	row, err := r.tx.Queries(ctx).GetRefreshTokenByHashForUpdate(ctx, hash)
	if err != nil {
		return model.RefreshToken{}, database.MapError("get refresh token", err)
	}
	return model.RefreshToken{ID: row.ID, UserID: row.UserID, FamilyID: row.FamilyID, ExpiresAt: row.ExpiresAt, RevokedAt: row.RevokedAt}, nil
}

func (r *refreshTokenRepository) Revoke(ctx context.Context, id int64, replacedBy *int64) error {
	err := r.tx.Queries(ctx).RevokeRefreshToken(ctx, sqlc.RevokeRefreshTokenParams{ID: id, ReplacedBy: replacedBy})
	return database.MapError("revoke refresh token", err)
}

func (r *refreshTokenRepository) RevokeFamily(ctx context.Context, family uuid.UUID) error {
	return database.MapError("revoke token family", r.tx.Queries(ctx).RevokeRefreshTokenFamily(ctx, family))
}

func (r *refreshTokenRepository) RevokeAllForUser(ctx context.Context, userID int64) error {
	return database.MapError("revoke user tokens", r.tx.Queries(ctx).RevokeUserRefreshTokens(ctx, userID))
}
