-- name: GetActionByUUID :one
SELECT * FROM action WHERE uuid = $1 AND deleted_at IS NULL;

-- name: ListActions :many
SELECT * FROM action
WHERE deleted_at IS NULL
  AND (sqlc.narg('q')::text IS NULL
       OR action_code ILIKE '%' || sqlc.narg('q') || '%' ESCAPE '\'
       OR action_name ILIKE '%' || sqlc.narg('q') || '%' ESCAPE '\')
ORDER BY
  CASE WHEN sqlc.arg('sort')::text = 'action_code'  THEN action_code END ASC,
  CASE WHEN sqlc.arg('sort')::text = '-action_code' THEN action_code END DESC,
  CASE WHEN sqlc.arg('sort')::text = '-created_at'  THEN created_at END DESC,
  id DESC
LIMIT sqlc.arg('limit') OFFSET sqlc.arg('offset');

-- name: CountActions :one
SELECT count(*) FROM action
WHERE deleted_at IS NULL
  AND (sqlc.narg('q')::text IS NULL
       OR action_code ILIKE '%' || sqlc.narg('q') || '%' ESCAPE '\'
       OR action_name ILIKE '%' || sqlc.narg('q') || '%' ESCAPE '\');

-- name: GetActionsByUUIDs :many
SELECT * FROM action WHERE uuid = ANY(sqlc.arg('uuids')::uuid[]) AND deleted_at IS NULL;

-- name: CreateAction :one
INSERT INTO action (uuid, action_code, action_name, description, created_by, updated_by)
VALUES (sqlc.arg('uuid'), sqlc.arg('action_code'), sqlc.arg('action_name'), sqlc.narg('description'),
        sqlc.arg('actor'), sqlc.arg('actor'))
RETURNING *;

-- name: UpsertActionByCode :exec
INSERT INTO action (uuid, action_code, action_name, created_by, updated_by)
VALUES (sqlc.arg('uuid'), sqlc.arg('action_code'), sqlc.arg('action_name'), sqlc.arg('actor'), sqlc.arg('actor'))
ON CONFLICT (action_code) WHERE deleted_at IS NULL DO NOTHING;

-- name: UpdateAction :one
UPDATE action
SET action_code = sqlc.arg('action_code'), action_name = sqlc.arg('action_name'),
    description = sqlc.narg('description'), updated_by = sqlc.arg('actor')
WHERE uuid = sqlc.arg('uuid') AND updated_at = sqlc.arg('expected_updated_at') AND deleted_at IS NULL
RETURNING *;

-- name: SoftDeleteAction :one
UPDATE action
SET deleted_at = now(), deleted_by = sqlc.arg('actor')
WHERE uuid = sqlc.arg('uuid') AND updated_at = sqlc.arg('expected_updated_at') AND deleted_at IS NULL
RETURNING id;
