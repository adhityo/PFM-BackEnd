-- Asal event audit (core, security, document) setelah database dipisah.
-- +goose Up
-- +goose StatementBegin
ALTER TABLE activity_log ADD COLUMN source VARCHAR(20);
CREATE INDEX idx_activity_log_log_time ON activity_log (log_time);
CREATE INDEX idx_activity_log_log_action ON activity_log (log_action);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS idx_activity_log_log_action;
DROP INDEX IF EXISTS idx_activity_log_log_time;
ALTER TABLE activity_log DROP COLUMN IF EXISTS source;
-- +goose StatementEnd
