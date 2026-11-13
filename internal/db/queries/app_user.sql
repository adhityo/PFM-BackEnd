-- name: GetUserAuthByLogin :one
SELECT u.id, u.uuid, u.username, u.email, u.status, u.password_hash, u.failed_login_count,
       u.locked_until, u.role_id, r.role_code, c.uuid AS client_uuid, u.client_id
FROM app_user u
LEFT JOIN role r ON r.id = u.role_id AND r.deleted_at IS NULL
LEFT JOIN client c ON c.id = u.client_id AND c.deleted_at IS NULL
WHERE (lower(u.username) = lower(sqlc.arg('login')::text) OR lower(u.email) = lower(sqlc.arg('login')::text))
  AND u.deleted_at IS NULL
ORDER BY u.id
LIMIT 1;

-- name: GetUserAuthByID :one
SELECT u.id, u.uuid, u.username, u.email, u.status, u.password_hash, u.failed_login_count,
       u.locked_until, u.role_id, r.role_code, c.uuid AS client_uuid, u.client_id
FROM app_user u
LEFT JOIN role r ON r.id = u.role_id AND r.deleted_at IS NULL
LEFT JOIN client c ON c.id = u.client_id AND c.deleted_at IS NULL
WHERE u.id = $1 AND u.deleted_at IS NULL;

-- name: RecordLoginFailure :one
UPDATE app_user
SET failed_login_count = CASE WHEN failed_login_count + 1 >= sqlc.arg('max_attempts')::int THEN 0
                              ELSE failed_login_count + 1 END,
    locked_until       = CASE WHEN failed_login_count + 1 >= sqlc.arg('max_attempts')::int
                              THEN now() + make_interval(secs => sqlc.arg('lock_seconds')::int)
                              ELSE locked_until END
WHERE id = sqlc.arg('id')
RETURNING failed_login_count, locked_until;

-- name: RecordLoginSuccess :exec
UPDATE app_user
SET failed_login_count = 0, locked_until = NULL, last_login_at = now()
WHERE id = $1;

-- name: GetUserByUUID :one
SELECT u.*, r.uuid AS role_uuid, r.role_code, c.uuid AS client_uuid
FROM app_user u
LEFT JOIN role r ON r.id = u.role_id
LEFT JOIN client c ON c.id = u.client_id
WHERE u.uuid = $1 AND u.deleted_at IS NULL
  AND (sqlc.narg('scope_client_id')::bigint IS NULL OR u.client_id = sqlc.narg('scope_client_id'));

-- name: ListUsers :many
SELECT u.*, r.uuid AS role_uuid, r.role_code, c.uuid AS client_uuid
FROM app_user u
LEFT JOIN role r ON r.id = u.role_id
LEFT JOIN client c ON c.id = u.client_id
WHERE u.deleted_at IS NULL
  AND (sqlc.narg('scope_client_id')::bigint IS NULL OR u.client_id = sqlc.narg('scope_client_id'))
  AND (sqlc.narg('q')::text IS NULL
       OR u.username ILIKE '%' || sqlc.narg('q') || '%' ESCAPE '\'
       OR u.email ILIKE '%' || sqlc.narg('q') || '%' ESCAPE '\'
       OR u.first_name ILIKE '%' || sqlc.narg('q') || '%' ESCAPE '\')
  AND (sqlc.narg('role_id')::bigint IS NULL OR u.role_id = sqlc.narg('role_id'))
  AND (sqlc.narg('status')::smallint IS NULL OR u.status = sqlc.narg('status'))
ORDER BY
  CASE WHEN sqlc.arg('sort')::text = 'username'    THEN u.username END ASC,
  CASE WHEN sqlc.arg('sort')::text = '-username'   THEN u.username END DESC,
  CASE WHEN sqlc.arg('sort')::text = 'created_at'  THEN u.created_at END ASC,
  CASE WHEN sqlc.arg('sort')::text = '-created_at' THEN u.created_at END DESC,
  u.id DESC
LIMIT sqlc.arg('limit') OFFSET sqlc.arg('offset');

-- name: CountUsers :one
SELECT count(*)
FROM app_user u
WHERE u.deleted_at IS NULL
  AND (sqlc.narg('scope_client_id')::bigint IS NULL OR u.client_id = sqlc.narg('scope_client_id'))
  AND (sqlc.narg('q')::text IS NULL
       OR u.username ILIKE '%' || sqlc.narg('q') || '%' ESCAPE '\'
       OR u.email ILIKE '%' || sqlc.narg('q') || '%' ESCAPE '\'
       OR u.first_name ILIKE '%' || sqlc.narg('q') || '%' ESCAPE '\')
  AND (sqlc.narg('role_id')::bigint IS NULL OR u.role_id = sqlc.narg('role_id'))
  AND (sqlc.narg('status')::smallint IS NULL OR u.status = sqlc.narg('status'));

-- name: CreateUser :one
INSERT INTO app_user (uuid, username, email, first_name, description, status, role_id, client_id,
                      is_supervisor, activation_key_hash, activation_key_expires_at, created_by, updated_by)
VALUES (sqlc.arg('uuid'), sqlc.arg('username'), sqlc.arg('email'), sqlc.narg('first_name'), sqlc.narg('description'),
        sqlc.arg('status'), sqlc.arg('role_id'), sqlc.narg('client_id'), sqlc.arg('is_supervisor'),
        sqlc.arg('activation_key_hash'), sqlc.arg('activation_key_expires_at'), sqlc.arg('actor'), sqlc.arg('actor'))
RETURNING id, uuid, created_at, updated_at;

-- name: UpdateUser :one
UPDATE app_user
SET email = sqlc.arg('email'), first_name = sqlc.narg('first_name'), description = sqlc.narg('description'),
    status = sqlc.arg('status'), role_id = sqlc.arg('role_id'), client_id = sqlc.narg('client_id'),
    is_supervisor = sqlc.arg('is_supervisor'), updated_by = sqlc.arg('actor')
WHERE uuid = sqlc.arg('uuid') AND updated_at = sqlc.arg('expected_updated_at') AND deleted_at IS NULL
RETURNING id, updated_at;

-- name: SoftDeleteUser :one
UPDATE app_user
SET deleted_at = now(), deleted_by = sqlc.arg('actor')
WHERE uuid = sqlc.arg('uuid') AND updated_at = sqlc.arg('expected_updated_at') AND deleted_at IS NULL
RETURNING id;

-- name: ResetUserCredential :one
UPDATE app_user
SET password_hash = NULL, activation_key_hash = sqlc.arg('activation_key_hash'),
    activation_key_expires_at = sqlc.arg('activation_key_expires_at'), password_changed = FALSE,
    failed_login_count = 0, locked_until = NULL, updated_by = sqlc.arg('actor')
WHERE uuid = sqlc.arg('uuid') AND updated_at = sqlc.arg('expected_updated_at') AND deleted_at IS NULL
RETURNING id, updated_at;

-- name: SetUserLockedUntil :one
UPDATE app_user
SET locked_until = sqlc.narg('locked_until'), failed_login_count = 0, updated_by = sqlc.arg('actor')
WHERE uuid = sqlc.arg('uuid') AND updated_at = sqlc.arg('expected_updated_at') AND deleted_at IS NULL
RETURNING id, updated_at;

-- name: GetUserByActivationKeyHashForUpdate :one
SELECT id, uuid, username, activation_key_expires_at
FROM app_user
WHERE activation_key_hash = $1 AND deleted_at IS NULL
FOR UPDATE;

-- name: SetUserPassword :exec
UPDATE app_user
SET password_hash = sqlc.arg('password_hash'), activation_key_hash = NULL, activation_key_expires_at = NULL,
    password_changed = TRUE, last_password_change = now(), failed_login_count = 0, locked_until = NULL,
    updated_by = sqlc.arg('actor')
WHERE id = sqlc.arg('id') AND deleted_at IS NULL;

-- name: CountActiveUsersByRole :one
SELECT count(*) FROM app_user WHERE role_id = $1 AND deleted_at IS NULL;

-- name: GetUserAuthByUUID :one
SELECT u.id, u.uuid, u.username, u.email, u.status, u.password_hash, u.failed_login_count,
       u.locked_until, u.role_id, r.role_code, c.uuid AS client_uuid, u.client_id
FROM app_user u
LEFT JOIN role r ON r.id = u.role_id AND r.deleted_at IS NULL
LEFT JOIN client c ON c.id = u.client_id AND c.deleted_at IS NULL
WHERE u.uuid = $1 AND u.deleted_at IS NULL;
