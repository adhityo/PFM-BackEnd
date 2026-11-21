-- Client pelaku event audit (UUID client @ pfm_core; kosong = pengguna global/sistem), supaya log
-- aktivitas bisa dibatasi per client (temuan keamanan A5).
-- +goose Up
-- +goose StatementBegin
ALTER TABLE audit_outbox ADD COLUMN client_uuid UUID;  -- client @ pfm_core (tanpa FK lintas database)
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE audit_outbox DROP COLUMN IF EXISTS client_uuid;
-- +goose StatementEnd
