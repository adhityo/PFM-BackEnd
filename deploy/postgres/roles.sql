-- Empat database terpisah dengan role hak minimum (PRD §7.1, §12.5). Dijalankan
-- sekali oleh superuser sebelum migrasi. Password di bawah hanya untuk
-- development; di production gunakan secret manager (ALTER ROLE ... PASSWORD)
-- dan, bila memungkinkan, server terpisah untuk pfm_log dan pfm_security.
--
--   role               database                     hak
--   pfm_migrator       semua (owner)                DDL saat migrasi
--   pfm_core_app       pfm_core                     API: SELECT/INSERT/UPDATE, outbox INSERT saja
--   pfm_security_app   pfm_security                 API: user, role, permission, refresh token
--   pfm_document_app   pfm_document                 API: metadata file & modul dokumen
--   pfm_log_reader     pfm_log                      API: SELECT activity_log (tanpa tulis)
--   pfm_log_writer     pfm_log                      worker: INSERT activity_log saja
--   pfm_worker         pfm_core/security/document   worker: kirim & hapus outbox, bersihkan file/token
--
-- Tidak ada role runtime yang boleh UPDATE/DELETE activity_log.

CREATE ROLE pfm_migrator     LOGIN PASSWORD 'pfm_migrator_dev';
CREATE ROLE pfm_core_app     LOGIN PASSWORD 'pfm_core_app_dev';
CREATE ROLE pfm_security_app LOGIN PASSWORD 'pfm_security_app_dev';
CREATE ROLE pfm_document_app LOGIN PASSWORD 'pfm_document_app_dev';
CREATE ROLE pfm_log_reader   LOGIN PASSWORD 'pfm_log_reader_dev';
CREATE ROLE pfm_log_writer   LOGIN PASSWORD 'pfm_log_writer_dev';
CREATE ROLE pfm_worker       LOGIN PASSWORD 'pfm_worker_dev';

CREATE DATABASE pfm_core     OWNER pfm_migrator;
CREATE DATABASE pfm_security OWNER pfm_migrator;
CREATE DATABASE pfm_log      OWNER pfm_migrator;
CREATE DATABASE pfm_document OWNER pfm_migrator;

-- Secara default PUBLIC boleh CONNECT ke database mana pun: dicabut.
REVOKE ALL ON DATABASE pfm_core, pfm_security, pfm_log, pfm_document FROM PUBLIC;
GRANT CONNECT ON DATABASE pfm_core     TO pfm_core_app, pfm_worker;
GRANT CONNECT ON DATABASE pfm_security TO pfm_security_app, pfm_worker;
GRANT CONNECT ON DATABASE pfm_document TO pfm_document_app, pfm_worker;
GRANT CONNECT ON DATABASE pfm_log      TO pfm_log_reader, pfm_log_writer;

-- Schema public: hanya pfm_migrator yang boleh membuat objek (PostgreSQL <= 14
-- masih memberi CREATE ke PUBLIC secara default).
\connect pfm_core
ALTER SCHEMA public OWNER TO pfm_migrator;
REVOKE CREATE ON SCHEMA public FROM PUBLIC;
\connect pfm_security
ALTER SCHEMA public OWNER TO pfm_migrator;
REVOKE CREATE ON SCHEMA public FROM PUBLIC;
\connect pfm_log
ALTER SCHEMA public OWNER TO pfm_migrator;
REVOKE CREATE ON SCHEMA public FROM PUBLIC;
\connect pfm_document
ALTER SCHEMA public OWNER TO pfm_migrator;
REVOKE CREATE ON SCHEMA public FROM PUBLIC;
