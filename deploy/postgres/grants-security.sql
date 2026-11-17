-- pfm_security: dijalankan oleh pfm_migrator setelah `migrate up` (idempoten).
REVOKE ALL ON ALL TABLES IN SCHEMA public FROM PUBLIC;
GRANT USAGE ON SCHEMA public TO pfm_security_app, pfm_worker;
GRANT SELECT, INSERT, UPDATE ON ALL TABLES IN SCHEMA public TO pfm_security_app;
GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA public TO pfm_security_app;
REVOKE SELECT, UPDATE, DELETE ON audit_outbox FROM pfm_security_app;
-- FOR UPDATE SKIP LOCKED (klaim batch) membutuhkan hak UPDATE.
GRANT SELECT, UPDATE, DELETE ON audit_outbox TO pfm_worker;
-- Token kedaluwarsa dihapus oleh worker.
GRANT SELECT, DELETE ON auth_refresh_token TO pfm_worker;
REVOKE ALL ON goose_db_version FROM pfm_security_app, pfm_worker;
