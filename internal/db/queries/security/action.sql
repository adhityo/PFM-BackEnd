-- name: GetActionByUUID :one
SELECT * FROM action WHERE uuid = $1 AND deleted_at IS NULL;

-- name: ListActions :many
SELECT * FROM action
WHERE deleted_at IS NULL
  AND (sqlc.narg('q')::text IS NULL
       OR action_code ILIKE '%' || sqlc.narg('q') || '%' ESCAPE '\'
       OR action_name ILIKE '%' || sqlc.narg('q') || '%' ESCAPE '\')
  AND (sqlc.narg('module')::text IS NULL OR module = sqlc.narg('module'))
ORDER BY
  CASE WHEN sqlc.arg('sort')::text = 'action_code'  THEN action_code END ASC,
  CASE WHEN sqlc.arg('sort')::text = '-action_code' THEN action_code END DESC,
  CASE WHEN sqlc.arg('sort')::text = '-created_at'  THEN created_at END DESC,
  id
LIMIT sqlc.arg('limit') OFFSET sqlc.arg('offset');

-- name: CountActions :one
SELECT count(*) FROM action
WHERE deleted_at IS NULL
  AND (sqlc.narg('q')::text IS NULL
       OR action_code ILIKE '%' || sqlc.narg('q') || '%' ESCAPE '\'
       OR action_name ILIKE '%' || sqlc.narg('q') || '%' ESCAPE '\')
  AND (sqlc.narg('module')::text IS NULL OR module = sqlc.narg('module'));

-- name: ListCatalogActions :many
-- Whole catalog for role editors and the ADMIN permission set.
SELECT * FROM action WHERE deleted_at IS NULL AND action_code IS NOT NULL ORDER BY action_code;

-- name: GetActionsByUUIDs :many
SELECT * FROM action WHERE uuid = ANY(sqlc.arg('uuids')::uuid[]) AND deleted_at IS NULL;

-- name: CreateAction :one
-- Custom (non-system) actions only; system actions come from SyncSystemAction.
INSERT INTO action (uuid, action_code, action_name, description, module, resource_label, is_system, created_by, updated_by)
VALUES (sqlc.arg('uuid'), sqlc.arg('action_code'), sqlc.arg('action_name'), sqlc.narg('description'), 'custom',
        sqlc.narg('resource_label'), FALSE, sqlc.arg('actor'), sqlc.arg('actor'))
RETURNING *;

-- name: SyncSystemAction :exec
-- Inserts or refreshes a permission of the code catalog (`migrate seed`).
INSERT INTO action (uuid, action_code, action_name, module, resource_label, is_system, created_by, updated_by)
VALUES (sqlc.arg('uuid'), sqlc.arg('action_code'), sqlc.arg('action_name'), sqlc.arg('module'),
        sqlc.arg('resource_label'), TRUE, sqlc.arg('actor'), sqlc.arg('actor'))
ON CONFLICT (action_code) WHERE deleted_at IS NULL DO UPDATE
SET action_name = EXCLUDED.action_name, module = EXCLUDED.module, resource_label = EXCLUDED.resource_label,
    is_system = TRUE, updated_by = EXCLUDED.updated_by
WHERE (action.action_name, action.module, action.resource_label, action.is_system)
      IS DISTINCT FROM (EXCLUDED.action_name, EXCLUDED.module, EXCLUDED.resource_label, TRUE);

-- name: UpdateAction :one
UPDATE action
SET action_code = sqlc.arg('action_code'), action_name = sqlc.arg('action_name'),
    description = sqlc.narg('description'), resource_label = sqlc.narg('resource_label'), updated_by = sqlc.arg('actor')
WHERE uuid = sqlc.arg('uuid') AND updated_at = sqlc.arg('expected_updated_at') AND deleted_at IS NULL
RETURNING *;

-- name: SoftDeleteAction :one
UPDATE action
SET deleted_at = now(), deleted_by = sqlc.arg('actor')
WHERE uuid = sqlc.arg('uuid') AND updated_at = sqlc.arg('expected_updated_at') AND deleted_at IS NULL
RETURNING id;
