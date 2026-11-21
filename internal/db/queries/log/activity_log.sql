-- name: ShipActivityLog :exec
-- Written only by the outbox shipper; replays are ignored (uuid is the only
-- unique key besides id). No conflict target, so the INSERT-only log user
-- needs no SELECT privilege at all.
INSERT INTO activity_log (uuid, action_query, ip_address, log_action, log_description, log_session_code,
                          log_time, log_title, role_code, username, source, client_uuid)
VALUES (sqlc.arg('uuid'), sqlc.narg('action_query'), sqlc.narg('ip_address'), sqlc.narg('log_action'),
        sqlc.narg('log_description'), sqlc.narg('log_session_code'), sqlc.arg('log_time'), sqlc.narg('log_title'),
        sqlc.narg('role_code'), sqlc.narg('username'), sqlc.arg('source'), sqlc.narg('client_uuid'))
ON CONFLICT DO NOTHING;

-- name: GetActivityLogByUUID :one
-- Users bound to a client see only events of their client's users.
SELECT * FROM activity_log WHERE uuid = sqlc.arg('uuid')
  AND (sqlc.narg('scope_client_uuid')::uuid IS NULL OR client_uuid = sqlc.narg('scope_client_uuid'));

-- name: ListActivityLogs :many
SELECT * FROM activity_log
WHERE (sqlc.narg('username')::text IS NULL OR username = sqlc.narg('username'))
  AND (sqlc.narg('log_action')::text IS NULL OR log_action = sqlc.narg('log_action'))
  AND (sqlc.narg('from')::timestamptz IS NULL OR log_time >= sqlc.narg('from'))
  AND (sqlc.narg('to')::timestamptz IS NULL OR log_time < sqlc.narg('to'))
  AND (sqlc.narg('scope_client_uuid')::uuid IS NULL OR client_uuid = sqlc.narg('scope_client_uuid'))
ORDER BY
  CASE WHEN sqlc.arg('sort')::text = 'log_time' THEN log_time END ASC,
  log_time DESC, id DESC
LIMIT sqlc.arg('limit') OFFSET sqlc.arg('offset');

-- name: CountActivityLogs :one
SELECT count(*) FROM activity_log
WHERE (sqlc.narg('username')::text IS NULL OR username = sqlc.narg('username'))
  AND (sqlc.narg('log_action')::text IS NULL OR log_action = sqlc.narg('log_action'))
  AND (sqlc.narg('from')::timestamptz IS NULL OR log_time >= sqlc.narg('from'))
  AND (sqlc.narg('to')::timestamptz IS NULL OR log_time < sqlc.narg('to'))
  AND (sqlc.narg('scope_client_uuid')::uuid IS NULL OR client_uuid = sqlc.narg('scope_client_uuid'));
