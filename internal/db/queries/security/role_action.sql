-- name: ListActionsByRole :many
SELECT a.*
FROM role_action ra
JOIN action a ON a.id = ra.action_id AND a.deleted_at IS NULL
WHERE ra.role_id = $1 AND ra.deleted_at IS NULL
ORDER BY a.action_code;

-- name: ListPermissionCodesByRole :many
SELECT a.action_code::text
FROM role_action ra
JOIN action a ON a.id = ra.action_id AND a.deleted_at IS NULL
WHERE ra.role_id = $1 AND ra.deleted_at IS NULL AND a.action_code IS NOT NULL;

-- name: SoftDeleteRoleActionsByRole :exec
UPDATE role_action
SET deleted_at = now(), deleted_by = sqlc.arg('actor')
WHERE role_id = sqlc.arg('role_id') AND deleted_at IS NULL;

-- name: InsertRoleAction :exec
INSERT INTO role_action (uuid, role_id, action_id, role_name, action_name, status, created_by, updated_by)
VALUES (sqlc.arg('uuid'), sqlc.arg('role_id'), sqlc.arg('action_id'), sqlc.narg('role_name'),
        sqlc.narg('action_name'), 1, sqlc.arg('actor'), sqlc.arg('actor'));

-- name: CountActiveRoleActionsByAction :one
SELECT count(*) FROM role_action WHERE action_id = $1 AND deleted_at IS NULL;

-- name: TouchRole :exec
UPDATE role SET updated_by = sqlc.arg('actor') WHERE id = sqlc.arg('id');
