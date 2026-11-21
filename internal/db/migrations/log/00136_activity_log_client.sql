-- Client pelaku event (disalin worker dari audit_outbox): pengguna milik client hanya melihat log
-- client-nya (temuan keamanan A5).
-- +goose Up
-- +goose StatementBegin
ALTER TABLE activity_log ADD COLUMN client_uuid UUID;  -- client @ pfm_core (tanpa FK lintas database)
CREATE INDEX idx_activity_log_client_uuid ON activity_log (client_uuid);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS idx_activity_log_client_uuid;
ALTER TABLE activity_log DROP COLUMN IF EXISTS client_uuid;
-- +goose StatementEnd
