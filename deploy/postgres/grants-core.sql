-- pfm_core: dijalankan oleh pfm_migrator setelah `migrate up` (idempoten).
-- Aplikasi memakai soft delete: tidak ada DELETE untuk API.
REVOKE ALL ON ALL TABLES IN SCHEMA public FROM PUBLIC;
GRANT USAGE ON SCHEMA public TO pfm_core_app, pfm_worker;
GRANT SELECT, INSERT, UPDATE ON ALL TABLES IN SCHEMA public TO pfm_core_app;
GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA public TO pfm_core_app;
-- Outbox audit: API hanya menulis; membaca/menghapus hanya worker.
REVOKE SELECT, UPDATE, DELETE ON audit_outbox FROM pfm_core_app;
-- FOR UPDATE SKIP LOCKED (klaim batch) membutuhkan hak UPDATE.
GRANT SELECT, UPDATE, DELETE ON audit_outbox TO pfm_worker;
REVOKE ALL ON goose_db_version FROM pfm_core_app, pfm_worker;
