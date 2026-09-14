-- Grant runtime untuk tabel OLT kini ditangani dinamis oleh binary migrate
-- (fungsi grantRuntimePrivileges) memakai nama role runtime dari environment,
-- sehingga tidak lagi hardcode nama role di sini (dulu: isppay_app/isppay_owner
-- yang hanya ada di deployment lama, sehingga install baru gagal).
-- Migrasi dipertahankan sebagai no-op agar urutan versi tetap konsisten.
DO $$ BEGIN END $$;