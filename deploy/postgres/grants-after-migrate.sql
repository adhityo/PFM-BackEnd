-- Dijalankan oleh pfm_migrator setelah `migrate up` (idempoten).
-- Aplikasi memakai soft delete, jadi DELETE hanya untuk pembersihan token
-- dan file orphan; tabel log bersifat append-only.

GRANT DELETE ON auth_refresh_token, file_object TO pfm_app;
REVOKE UPDATE, DELETE ON activity_log, notification_download_counter, synchronized_data FROM pfm_app;

-- Tabel versi migrasi hanya boleh diubah oleh pfm_migrator.
REVOKE ALL ON goose_db_version FROM pfm_app;
