-- Role database terpisah (PRD §12.5). Dijalankan sekali oleh superuser
-- sebelum migrasi. Password di bawah hanya untuk development; di production
-- gunakan secret manager dan ganti dengan ALTER ROLE ... PASSWORD.

CREATE ROLE pfm_migrator LOGIN PASSWORD 'pfm_migrator_dev';
CREATE ROLE pfm_app      LOGIN PASSWORD 'pfm_app_dev';

GRANT CONNECT ON DATABASE pfm TO pfm_app, pfm_migrator;
GRANT USAGE, CREATE ON SCHEMA public TO pfm_migrator;
GRANT USAGE ON SCHEMA public TO pfm_app;

-- Semua tabel yang dibuat pfm_migrator otomatis memberi DML (tanpa DELETE) ke pfm_app.
ALTER DEFAULT PRIVILEGES FOR ROLE pfm_migrator IN SCHEMA public
    GRANT SELECT, INSERT, UPDATE ON TABLES TO pfm_app;
ALTER DEFAULT PRIVILEGES FOR ROLE pfm_migrator IN SCHEMA public
    GRANT USAGE, SELECT ON SEQUENCES TO pfm_app;
