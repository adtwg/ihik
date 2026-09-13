# AWGRevBILL

AWGRevBILL — NMS & billing ISP multi-tenant dengan backend Go/PostgreSQL dan frontend Next.js. Slice saat ini mencakup RBAC Super Admin/Mitra, login aman, dashboard operasional, server-side customer table, model MikroTik PPPoE, sinkronisasi, provisioning, invoice, dan pembayaran kasir.

## Prasyarat

- Production: Ubuntu Server 24.04 LTS, domain, dan Docker Engine
- Development native: Go 1.24, Node.js 22, dan PostgreSQL 17

## Deployment Production

### Instalasi Otomatis

Siapkan VPS Ubuntu Server 24.04 LTS kosong, arahkan record `A` domain langsung
ke IPv4 VPS, lalu buka port SSH aktif serta TCP `80/443` pada firewall provider.
Jalankan sebagai akses awal `root`:

```bash
apt-get update && apt-get install -y git && \
git clone https://github.com/adtwg/ihik.git /opt/isp-billing && \
cd /opt/isp-billing && bash install.sh
```

Wizard meminta domain, email
ACME, username, dan password Super Admin. Tekan Enter pada prompt password agar
password admin digenerate otomatis. Dua password PostgreSQL selalu dibuat acak
dan tidak ditampilkan.

Mode non-interaktif dengan password admin generated:

```bash
cd /opt/isp-billing
sudo bash install.sh \
	--non-interactive \
	--domain billing.example.com \
	--acme-email admin@example.com \
	--admin-user admin \
	--generate-admin-password
```

Untuk password yang ditentukan sendiri, gunakan `--admin-password-file` dengan
file milik `root` mode `600`; password literal sengaja tidak diterima sebagai
argumen command. Jalankan `sudo bash install.sh --help` untuk daftar opsi.

Installer memasang Docker Engine resmi, UFW, Fail2ban, user `deploy`, migration,
HTTPS Caddy, dan backup harian. Progress tampil di terminal dan log lengkap ada
di `/var/log/isp-billing/installer.log`. Password database disimpan dalam
`/opt/isp-billing/.env` mode `600`. Password admin generated disimpan sekali di
`/root/isp-billing-initial-admin.txt`; pindahkan ke password manager lalu hapus
file tersebut.

Installer aman dijalankan ulang: `.env`, password database, volume PostgreSQL,
data, dan password Super Admin existing dipertahankan. Jika volume PostgreSQL
ada tetapi `.env` hilang, installer berhenti dan meminta recovery, bukan membuat
password baru. Installer mengunci Docker ke socket lokal, memakai `compose.yaml`
secara eksplisit, dan memverifikasi marker volume sebelum update. Volume yang
hilang atau terganti menghentikan rerun untuk recovery data. Installer tidak
mengubah port atau metode autentikasi SSH.

Migration yang sudah applied bersifat append-only. Runner menyimpan checksum
SHA-256 dan menolak file applied yang berubah atau hilang dari release.

Repository private tetap harus di-clone menggunakan autentikasi Git yang sudah
disiapkan. Detail prasyarat, recovery, instalasi manual, restore, dan hardening
lanjutan tersedia di [INSTALL_VPS_UBUNTU.md](INSTALL_VPS_UBUNTU.md).

Arsitektur production hanya memublikasikan Caddy pada port standar `80/443`. PostgreSQL, Go API, dan Next.js tidak memiliki host port. Aplikasi diakses langsung melalui `https://subdomain` tanpa suffix port.

## Development Native

1. Siapkan PostgreSQL 17 dan `DATABASE_URL` development.
2. Jalankan `go mod tidy`, `go run ./cmd/migrate`, lalu `go run ./cmd/api`.
3. Dari folder `web/`, jalankan `npm ci` dan `npm run dev`.
4. Set `APP_ENV=development` untuk API dan `API_INTERNAL_URL=http://localhost:8080` untuk Next.js.

File [compose.yaml](compose.yaml) ditujukan untuk deployment production dan memerlukan `.env` berdasarkan [.env.example](.env.example).

## Endpoint Awal

- `POST /api/v1/auth/login`
- `GET /api/v1/me`
- `POST /api/v1/auth/logout`
- `GET /api/v1/customers?page=1&page_size=25&archived=active`
- `POST /api/v1/customers`
- `POST /api/v1/customers/{customerID}/archive`
- `GET /health`
- `GET /ready` (memeriksa koneksi PostgreSQL)

Daftar pelanggan sekarang memakai server-side processing:

```text
GET /api/v1/customers?page=1&page_size=25&search=andi&sort=name&order=asc&archived=include
GET /api/v1/dashboard
```

Request mutasi yang sudah login wajib mengirim cookie `isp_session`, cookie `isp_csrf`, dan header `X-CSRF-Token` dengan nilai dari cookie CSRF.

Contoh body tambah pelanggan:

```json
{
	"name": "Pelanggan Satu",
	"phone": "081234567890",
	"email": "pelanggan@example.com",
	"address": "Alamat pemasangan"
}
```

Nomor pelanggan dibuat atomik per Mitra dengan format `CUST-000001`. Endpoint tidak menerima `tenant_id`; tenant selalu berasal dari session agar ID tenant tidak dapat dipalsukan oleh client.

## Status Implementasi

Sudah tersedia:

- Skema identity, RBAC, session, audit log, pelanggan, router, PPP profile, paket, PPPoE, sinkronisasi, provisioning, invoice, dan kasir.
- Login Argon2id, opaque server-side session, CSRF, throttling, dan security headers.
- Vertical slice pelanggan: tambah, daftar, dan archive dengan tenant scoping.
- Next.js responsive shell, sidebar penuh/ikon dengan cookie, drawer mobile, dashboard Mitra, dashboard Super Admin, dan tabel pelanggan TanStack.
- Pagination, pencarian, sorting, dan filter arsip dilakukan oleh PostgreSQL melalui API, bukan di browser.
- Compose menjalankan migration, API, Next.js, dan Caddy sebagai satu origin.
- Migration runner dan bootstrap Super Admin.

Berikutnya:

- API Super Admin untuk membuat Mitra dan user Mitra.
- Envelope encryption dan API tambah/test koneksi MikroTik.
- Load PPP profile/secret, preview diff, dan resolusi konflik.
- Paket dan mapping profile normal/isolir.
- Tambah pelanggan sekaligus enqueue pembuatan PPPoE.

## Aturan Keamanan

- Jangan commit `.env` atau kredensial MikroTik.
- Produksi wajib memakai HTTPS dan akun database dengan privilege minimum.
- Port MikroTik API tidak boleh dibuka ke internet umum; gunakan API-SSL atau jaringan manajemen/VPN.
- Pelanggan dan layanan diarsipkan, bukan dihapus. Dokumen keuangan ditolak oleh database jika dicoba dihapus.