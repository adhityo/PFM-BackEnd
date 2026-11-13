-- name: InsertFileObject :one
INSERT INTO file_object (uuid, storage, category, bucket, object_key, original_name, mime_type,
                         size_bytes, checksum_sha256, status, created_by, updated_by)
VALUES (sqlc.arg('uuid'), sqlc.arg('storage'), sqlc.arg('category'), sqlc.narg('bucket'), sqlc.arg('object_key'),
        sqlc.arg('original_name'), sqlc.arg('mime_type'), sqlc.arg('size_bytes'), sqlc.arg('checksum_sha256'),
        'pending', sqlc.arg('actor'), sqlc.arg('actor'))
RETURNING *;

-- name: GetFileObjectByUUID :one
SELECT * FROM file_object WHERE uuid = $1 AND deleted_at IS NULL;

-- name: GetFileObjectByUUIDForUpdate :one
SELECT * FROM file_object WHERE uuid = $1 AND deleted_at IS NULL FOR UPDATE;

-- name: ActivateFileObject :exec
UPDATE file_object SET status = 'active', updated_by = sqlc.arg('actor')
WHERE id = sqlc.arg('id') AND status = 'pending';

-- name: SoftDeleteFileObject :one
UPDATE file_object SET deleted_at = now(), deleted_by = sqlc.arg('actor')
WHERE uuid = sqlc.arg('uuid') AND deleted_at IS NULL
RETURNING *;

-- name: ListStalePendingFiles :many
SELECT * FROM file_object
WHERE status = 'pending' AND created_at < sqlc.arg('before') AND deleted_at IS NULL
ORDER BY id
LIMIT sqlc.arg('limit');

-- name: HardDeleteFileObject :exec
DELETE FROM file_object WHERE id = $1;
