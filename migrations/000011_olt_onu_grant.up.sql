-- Grant privileges untuk tabel runtime yang digunakan oleh aplikasi (isppay_app).
-- Dijalankan sebagai migrasi terpisah agar selalu teraplikasi setelah semua tabel dibuat.
GRANT SELECT, INSERT, UPDATE, DELETE ON olt_onus, olt_onu_daily, olts TO isppay_app;
ALTER DEFAULT PRIVILEGES FOR ROLE isppay_owner IN SCHEMA public GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO isppay_app;