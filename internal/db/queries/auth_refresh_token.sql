-- name: InsertRefreshToken :one
INSERT INTO auth_refresh_token (uuid, user_id, family_id, token_hash, user_agent, ip_address, expires_at)
VALUES (sqlc.arg('uuid'), sqlc.arg('user_id'), sqlc.arg('family_id'), sqlc.arg('token_hash'),
        sqlc.narg('user_agent'), sqlc.narg('ip_address'), sqlc.arg('expires_at'))
RETURNING id;

-- name: GetRefreshTokenByHashForUpdate :one
SELECT * FROM auth_refresh_token WHERE token_hash = $1 FOR UPDATE;

-- name: RevokeRefreshToken :exec
UPDATE auth_refresh_token
SET revoked_at = now(), replaced_by = sqlc.narg('replaced_by')
WHERE id = sqlc.arg('id') AND revoked_at IS NULL;

-- name: RevokeRefreshTokenFamily :exec
UPDATE auth_refresh_token SET revoked_at = now()
WHERE family_id = $1 AND revoked_at IS NULL;

-- name: RevokeUserRefreshTokens :exec
UPDATE auth_refresh_token SET revoked_at = now()
WHERE user_id = $1 AND revoked_at IS NULL;

-- name: DeleteExpiredRefreshTokens :execrows
DELETE FROM auth_refresh_token WHERE expires_at < now() - interval '1 day';
