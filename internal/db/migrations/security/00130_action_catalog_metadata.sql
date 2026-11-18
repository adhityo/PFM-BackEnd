-- Metadata katalog permission untuk editor role dan menu frontend:
--  * module: kelompok (system, masterdata, market, ..., custom);
--  * resource_label: label resource, mis. "obligasi";
--  * is_system: permission bawaan kode, disinkronkan oleh `migrate seed`. Tidak dapat diubah atau
--    dihapus lewat API, karena route backend memerlukannya.
-- +goose Up
-- +goose StatementBegin
ALTER TABLE action ADD COLUMN module VARCHAR(50);
ALTER TABLE action ADD COLUMN resource_label VARCHAR(255);
ALTER TABLE action ADD COLUMN is_system BOOLEAN NOT NULL DEFAULT FALSE;
CREATE INDEX idx_action_module ON action (module);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS idx_action_module;
ALTER TABLE action DROP COLUMN IF EXISTS is_system;
ALTER TABLE action DROP COLUMN IF EXISTS resource_label;
ALTER TABLE action DROP COLUMN IF EXISTS module;
-- +goose StatementEnd
