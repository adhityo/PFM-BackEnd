-- Mail outbox, present in pfm_core, pfm_security and pfm_document (migrations/outbox).

-- name: InsertMailOutbox :exec
INSERT INTO mail_outbox (uuid, recipient, subject, body_text, body_html)
VALUES (sqlc.arg('uuid'), sqlc.arg('recipient'), sqlc.arg('subject'), sqlc.arg('body_text'), sqlc.narg('body_html'));

-- name: ClaimMailOutbox :many
-- Leases due messages by pushing next_attempt_at forward (backoff 1, 4, 16, 64 min, then 6 h),
-- so the SMTP call happens outside any transaction and other workers skip them.
UPDATE mail_outbox
SET attempts = attempts + 1,
    next_attempt_at = now() + make_interval(secs => LEAST(21600, 60 * power(4, attempts)))
WHERE id IN (SELECT m.id FROM mail_outbox m
             WHERE m.next_attempt_at <= now() AND m.attempts < sqlc.arg('max_attempts')::smallint
             ORDER BY m.id LIMIT sqlc.arg('batch') FOR UPDATE SKIP LOCKED)
RETURNING *;

-- name: DeleteMailOutbox :exec
DELETE FROM mail_outbox WHERE id = $1;

-- name: RecordMailFailure :exec
-- After the last attempt the body (which may hold a secret link) is scrubbed.
UPDATE mail_outbox
SET last_error = sqlc.arg('error'),
    body_text = CASE WHEN attempts >= sqlc.arg('max_attempts')::smallint THEN '[dihapus setelah gagal terkirim]' ELSE body_text END,
    body_html = CASE WHEN attempts >= sqlc.arg('max_attempts')::smallint THEN NULL ELSE body_html END
WHERE id = sqlc.arg('id');
