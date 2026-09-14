# Troubleshooting Instalasi VPS — Catatan Insiden & Pencegahan

Dokumen ini merangkum dua masalah yang muncul saat instalasi pertama di VPS Ubuntu
(Tencent Cloud, `revbill.awgnet.biz.id`) beserta akar penyebab, solusi, dan langkah
pencegahan agar tidak terulang. Keduanya sudah diperbaiki di kode.

---

## Ringkasan cepat

| # | Gejala | Akar penyebab | Status |
|---|--------|---------------|--------|
| 1 | `migrate` gagal: `role "isppay_app" does not exist (SQLSTATE 42704)` | Migrasi `000011` hardcode nama role deployment lama | ✅ Fixed (`000011` jadi no-op) |
| 2 | `GAGAL [Konfigurasi UFW dan Fail2ban] pada baris 596 (exit 255)` padahal install lanjut | Deteksi port SSH gagal pada SSH socket-activation + pipeline memicu ERR-trap di subshell | ✅ Fixed (fallback socket + guard `|| true`) |

---

## Masalah 1 — Migrasi 000011 hardcode nama role

### Gejala
```
migrate-1 | apply migration 000011_olt_onu_grant.up.sql: ERROR: role "isppay_app" does not exist (SQLSTATE 42704)
```
Migrasi 000001–000010 sukses, lalu berhenti di 000011. Container `migrate` Exited(1),
sehingga `api`/`web`/`gateway` tidak pernah start.

### Akar penyebab
File `migrations/000011_olt_onu_grant.up.sql` versi lama berisi:
```sql
GRANT SELECT, INSERT, UPDATE, DELETE ON olt_onus, olt_onu_daily, olts TO isppay_app;
ALTER DEFAULT PRIVILEGES FOR ROLE isppay_owner IN SCHEMA public GRANT ... TO isppay_app;
```
Nama role `isppay_app` / `isppay_owner` **hanya ada di server aaPanel lama**. Pada
instalasi VPS baru, `install.sh` membuat role `isp_billing_owner` / `isp_billing_app`,
sehingga `GRANT ... TO isppay_app` gagal karena role-nya tidak ada.

### Kenapa migrasi itu memang tidak perlu
Binary `migrate` (`cmd/migrate/main.go`, fungsi `grantRuntimePrivileges`) sudah
memberikan grant yang sama **secara dinamis** ke role runtime yang benar (dibaca dari
`DATABASE_RUNTIME_USER`), dan dijalankan **setelah** semua migrasi selesai. Jadi grant
di migrasi 000011 murni redundan.

### Solusi (sudah diterapkan)
`000011_olt_onu_grant.up.sql` diubah menjadi no-op agar nomor versi tetap konsisten:
```sql
DO $$ BEGIN END $$;
```

### Pencegahan
- **Migrasi SQL tidak boleh hardcode nama role/user yang bergantung environment.**
  Semua grant runtime diserahkan ke `grantRuntimePrivileges` di `cmd/migrate`.
- Migrasi bersifat append-only dan divalidasi checksum; jangan mengubah isi migrasi
  yang sudah diterapkan di DB produksi — buat migrasi baru bila perlu perubahan.

---

## Masalah 2 — Pesan GAGAL palsu di step UFW/Fail2ban

### Gejala
```
[05/16] Konfigurasi UFW dan Fail2ban
GAGAL [Konfigurasi UFW dan Fail2ban] pada baris 596 (exit 255).
Periksa log: /var/log/isp-billing/installer.log
[06/16] Periksa konflik port host
...
[16/16] Aktifkan dan verifikasi HTTPS publik
```
Muncul "GAGAL" tetapi installer **tetap jalan sampai [16/16]** dan aplikasi berhasil
terpasang serta bisa diakses. Jadi ini **alarm palsu (false positive)**, bukan kegagalan
yang menghentikan proses.

### Akar penyebab (dua lapis)
1. **Deteksi port SSH gagal.** Fungsi `detect_ssh_ports` mencari port SSH lewat
   `$SSH_CONNECTION` dan proses `sshd`. Namun:
   - Login dilakukan lewat **konsol VNC** provider, bukan SSH → `$SSH_CONNECTION` kosong.
   - Ubuntu 24.04 memakai **systemd socket-activation** untuk SSH, sehingga listener
     port 22 dimiliki oleh proses `systemd` (bukan `sshd`), dan `sshd -T` mengembalikan
     exit `255` pada kondisi tertentu.
2. **Pipeline memicu ERR-trap di dalam subshell.** Karena `set -o pipefail` + ERR-trap
   yang diwariskan ke subshell (`set -E`), pipeline seperti
   `sshd -T | awk ...` di dalam proses-substitusi `< <(...)` mengembalikan status gagal.
   Subshell tersebut menjalankan ERR-trap → mencetak "GAGAL" lalu keluar, **tetapi
   proses utama tetap lanjut** (karena hanya subshell yang mati). Itu sebabnya "GAGAL"
   muncul di step 5 tapi install tetap sampai step 16.

### Solusi (sudah diterapkan di `install.sh`)
1. **Fallback socket-activation** di `detect_ssh_ports`: bila tidak ada port terdeteksi,
   ambil port dari `systemctl show ssh.socket -p Listen` (dan `sshd.socket`), lalu
   verifikasi benar-benar listen.
2. **Guard `|| true`** pada semua pipeline di dalam proses-substitusi agar kegagalan
   pipeline (mis. `sshd -T` exit 255, atau `grep` tanpa match) tidak lagi memicu
   ERR-trap dan mencetak "GAGAL" palsu.

### Cara aman menjalankan installer
Bila login lewat konsol (bukan SSH) atau SSH pakai port non-standar, sebutkan port
secara eksplisit — ini paling pasti:
```bash
sudo bash install.sh --ssh-port 22
```

### Verifikasi setelah install
```bash
# UFW aktif dan mengizinkan SSH + HTTP/HTTPS
sudo ufw status verbose

# Fail2ban jalan dan jail sshd memakai port yang benar
sudo systemctl status fail2ban --no-pager
sudo cat /etc/fail2ban/jail.d/sshd.local   # baris "port = 22"

# Semua container sehat
cd /opt/isp-billing && sudo docker compose ps
```

### Pencegahan
- Pada Ubuntu 24.04+, SSH default socket-activation — jangan mengandalkan nama proses
  `sshd` untuk deteksi port. Gunakan `ssh.socket`/`--ssh-port`.
- Untuk skrip `set -Eeuo pipefail`, selalu beri `|| true` pada pipeline "best-effort"
  di dalam `< <(...)` agar tidak memicu ERR-trap palsu.

---

## Status kode saat ini
- `migrations/000011_olt_onu_grant.up.sql` → no-op (`DO $$ BEGIN END $$;`).
- `install.sh` → fallback deteksi SSH socket-activation + guard `|| true` pada pipeline
  deteksi port. Installer idempotent dan aman dijalankan ulang.

## Catatan pemulihan (jika mengulang dari nol)
1. Tarik kode terbaru: `sudo git config --global --add safe.directory /opt/isp-billing && sudo git pull origin main`.
2. Jalankan installer: `sudo bash install.sh --ssh-port 22`.
3. `migrate` akan skip migrasi yang sudah diterapkan, menjalankan 000011 (no-op), lalu
   `grantRuntimePrivileges` memberi grant ke role runtime yang benar.
