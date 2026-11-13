-- name: GetRoleByUUID :one
SELECT * FROM role WHERE uuid = $1 AND deleted_at IS NULL;

-- name: GetRoleByID :one
SELECT * FROM role WHERE id = $1 AND deleted_at IS NULL;

-- name: GetRoleByCode :one
SELECT * FROM role WHERE role_code = $1 AND deleted_at IS NULL;

-- name: ListRoles :many
SELECT * FROM role
WHERE deleted_at IS NULL
  AND (sqlc.narg('q')::text IS NULL
       OR role_code ILIKE '%' || sqlc.narg('q') || '%' ESCAPE '\'
       OR role_name ILIKE '%' || sqlc.narg('q') || '%' ESCAPE '\')
ORDER BY
  CASE WHEN sqlc.arg('sort')::text = 'role_code'   THEN role_code END ASC,
  CASE WHEN sqlc.arg('sort')::text = '-role_code'  THEN role_code END DESC,
  CASE WHEN sqlc.arg('sort')::text = 'role_name'   THEN role_name END ASC,
  CASE WHEN sqlc.arg('sort')::text = '-role_name'  THEN role_name END DESC,
  CASE WHEN sqlc.arg('sort')::text = '-created_at' THEN created_at END DESC,
  id DESC
LIMIT sqlc.arg('limit') OFFSET sqlc.arg('offset');

-- name: CountRoles :one
SELECT count(*) FROM role
WHERE deleted_at IS NULL
  AND (sqlc.narg('q')::text IS NULL
       OR role_code ILIKE '%' || sqlc.narg('q') || '%' ESCAPE '\'
       OR role_name ILIKE '%' || sqlc.narg('q') || '%' ESCAPE '\');

-- name: CreateRole :one
INSERT INTO role (uuid, role_code, role_name, description, menu_url, status, created_by, updated_by)
VALUES (sqlc.arg('uuid'), sqlc.arg('role_code'), sqlc.arg('role_name'), sqlc.narg('description'),
        sqlc.narg('menu_url'), sqlc.arg('status'), sqlc.arg('actor'), sqlc.arg('actor'))
RETURNING *;

-- name: UpsertRoleByCode :one
INSERT INTO role (uuid, role_code, role_name, description, status, created_by, updated_by)
VALUES (sqlc.arg('uuid'), sqlc.arg('role_code'), sqlc.arg('role_name'), sqlc.narg('description'), 1,
        sqlc.arg('actor'), sqlc.arg('actor'))
ON CONFLICT (role_code) WHERE deleted_at IS NULL DO UPDATE SET role_code = EXCLUDED.role_code
RETURNING *;

-- name: UpdateRole :one
UPDATE role
SET role_code = sqlc.arg('role_code'), role_name = sqlc.arg('role_name'), description = sqlc.narg('description'),
    menu_url = sqlc.narg('menu_url'), status = sqlc.arg('status'), updated_by = sqlc.arg('actor')
WHERE uuid = sqlc.arg('uuid') AND updated_at = sqlc.arg('expected_updated_at') AND deleted_at IS NULL
RETURNING *;

-- name: SoftDeleteRole :one
UPDATE role
SET deleted_at = now(), deleted_by = sqlc.arg('actor')
WHERE uuid = sqlc.arg('uuid') AND updated_at = sqlc.arg('expected_updated_at') AND deleted_at IS NULL
RETURNING id;

-- name: GetRoleByUUIDForUpdate :one
SELECT * FROM role WHERE uuid = $1 AND deleted_at IS NULL FOR UPDATE;
