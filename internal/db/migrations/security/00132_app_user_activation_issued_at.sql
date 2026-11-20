-- Waktu penerbitan kunci aktivasi/reset terakhir: membatasi frekuensi email lupa password.
-- +goose Up
-- +goose StatementBegin
ALTER TABLE app_user ADD COLUMN activation_key_issued_at TIMESTAMPTZ;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE app_user DROP COLUMN IF EXISTS activation_key_issued_at;
-- +goose StatementEnd
