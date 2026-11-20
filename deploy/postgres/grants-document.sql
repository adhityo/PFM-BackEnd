-- pfm_document: dijalankan oleh pfm_migrator setelah `migrate up` (idempoten).
REVOKE ALL ON ALL TABLES IN SCHEMA public FROM PUBLIC;
GRANT USAGE ON SCHEMA public TO pfm_document_app, pfm_worker;
GRANT SELECT, INSERT, UPDATE ON ALL TABLES IN SCHEMA public TO pfm_document_app;
GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA public TO pfm_document_app;
REVOKE SELECT, UPDATE, DELETE ON audit_outbox, mail_outbox FROM pfm_document_app;
-- FOR UPDATE SKIP LOCKED (klaim batch) membutuhkan hak UPDATE.
GRANT SELECT, UPDATE, DELETE ON audit_outbox, mail_outbox TO pfm_worker;
-- Upload pending yang kedaluwarsa dihapus oleh worker.
GRANT SELECT, DELETE ON file_object TO pfm_worker;
REVOKE ALL ON goose_db_version FROM pfm_document_app, pfm_worker;
