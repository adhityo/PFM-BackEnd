-- pfm_log: dijalankan oleh pfm_migrator setelah `migrate up` (idempoten).
-- Append-only: API hanya membaca, worker hanya menambah; tidak ada role
-- runtime yang dapat mengubah atau menghapus jejak audit.
REVOKE ALL ON ALL TABLES IN SCHEMA public FROM PUBLIC;
GRANT USAGE ON SCHEMA public TO pfm_log_reader, pfm_log_writer;
GRANT SELECT ON activity_log TO pfm_log_reader;
GRANT INSERT ON activity_log TO pfm_log_writer;
-- Writer tidak punya SELECT: ShipActivityLog memakai ON CONFLICT DO NOTHING tanpa target kolom.
REVOKE ALL ON goose_db_version FROM pfm_log_reader, pfm_log_writer;
