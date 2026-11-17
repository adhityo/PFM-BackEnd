-- +goose Up
-- +goose StatementBegin
-- Tidak butuh extension: gen_random_uuid() sudah bawaan PostgreSQL >= 13,
-- sehingga role migrasi tidak perlu hak CREATE EXTENSION.
CREATE OR REPLACE FUNCTION set_updated_at() RETURNS trigger AS $$
BEGIN
    NEW.updated_at = now();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP FUNCTION IF EXISTS set_updated_at();
-- +goose StatementEnd
