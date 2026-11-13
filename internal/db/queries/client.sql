-- name: GetClientIDByUUID :one
SELECT id FROM client WHERE uuid = $1 AND deleted_at IS NULL;
