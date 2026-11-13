-- name: InsertActivityLog :exec
INSERT INTO activity_log (uuid, action_query, ip_address, log_action, log_description, log_session_code,
                          log_time, log_title, role_code, username)
VALUES (sqlc.arg('uuid'), sqlc.narg('action_query'), sqlc.narg('ip_address'), sqlc.arg('log_action'),
        sqlc.narg('log_description'), sqlc.narg('log_session_code'), now(), sqlc.narg('log_title'),
        sqlc.narg('role_code'), sqlc.narg('username'));

-- name: GetActivityLogByUUID :one
SELECT * FROM activity_log WHERE uuid = $1;

-- name: ListActivityLogs :many
SELECT * FROM activity_log
WHERE (sqlc.narg('username')::text IS NULL OR username = sqlc.narg('username'))
  AND (sqlc.narg('log_action')::text IS NULL OR log_action = sqlc.narg('log_action'))
  AND (sqlc.narg('from')::timestamptz IS NULL OR log_time >= sqlc.narg('from'))
  AND (sqlc.narg('to')::timestamptz IS NULL OR log_time < sqlc.narg('to'))
ORDER BY
  CASE WHEN sqlc.arg('sort')::text = 'log_time' THEN log_time END ASC,
  log_time DESC, id DESC
LIMIT sqlc.arg('limit') OFFSET sqlc.arg('offset');

-- name: CountActivityLogs :one
SELECT count(*) FROM activity_log
WHERE (sqlc.narg('username')::text IS NULL OR username = sqlc.narg('username'))
  AND (sqlc.narg('log_action')::text IS NULL OR log_action = sqlc.narg('log_action'))
  AND (sqlc.narg('from')::timestamptz IS NULL OR log_time >= sqlc.narg('from'))
  AND (sqlc.narg('to')::timestamptz IS NULL OR log_time < sqlc.narg('to'));
