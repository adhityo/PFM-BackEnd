-- Audit outbox, present in pfm_core, pfm_security and pfm_document (migrations/outbox).

-- name: InsertAuditOutbox :exec
INSERT INTO audit_outbox (uuid, action_query, ip_address, log_action, log_description, log_session_code,
                          log_title, role_code, username, source)
VALUES (sqlc.arg('uuid'), sqlc.narg('action_query'), sqlc.narg('ip_address'), sqlc.arg('log_action'),
        sqlc.narg('log_description'), sqlc.narg('log_session_code'), sqlc.narg('log_title'),
        sqlc.narg('role_code'), sqlc.narg('username'), sqlc.arg('source'));

-- name: ClaimAuditOutbox :many
-- Oldest events first; SKIP LOCKED lets several shippers run safely.
SELECT * FROM audit_outbox ORDER BY id LIMIT sqlc.arg('batch') FOR UPDATE SKIP LOCKED;

-- name: DeleteAuditOutbox :exec
DELETE FROM audit_outbox WHERE id = ANY(sqlc.arg('ids')::bigint[]);
