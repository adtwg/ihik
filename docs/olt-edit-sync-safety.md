# OLT Edit, Sync, and Scan Safety Checklist (AWGRevBILL)

## 1. Edit OLT: port SSH / CLI kredensial kembali ke default / tidak tersimpan

### Symptom
- Form Edit OLT menampilkan `cli_port = 22` dan username kosong setelah disimpan.
- Klik Tes OLT berhasil tapi Sync via CLI gagal karena port 22.

### Root causes
1. Public `OLT` struct tidak memiliki field `cli_protocol`, `cli_port`, `cli_username` → API tidak mengembalikannya → form fallback ke default.
2. Repository `Update()` menimpa kolom NOT NULL `v3_auth_protocol` / `v3_priv_protocol` dengan string kosong saat mode v2c → error `internal_error`.
3. `Create()` default `cli_port` didasarkan hanya pada `input.CLIPort == 0`, tidak memperhatikan protokol telnet -> port 23.

### Fixes
- Tambahkan field ke `internal/olt/service.go`:
  ```go
  CLIProtocol string `json:"cli_protocol,omitempty"`
  CLIPort     int    `json:"cli_port,omitempty"`
  CLIUsername string `json:"cli_username,omitempty"`
  ```
- Update `internal/postgres/olt_repository.go`:
  - `oltSelectColumns` harus mengembalikan `COALESCE(cli_protocol,'ssh'), cli_port, COALESCE(cli_username,'')`.
  - `scanOLT` harus scan ke field baru.
  - `Update()` gunakan `COALESCE(NULLIF($x,''), existing, default)` untuk v3 auth/priv dan `CASE WHEN cli_port=0 THEN cli_port ELSE $y END`.

### Verification
```bash
curl ... "$BASE/api/v1/olts/$OLT" | python3 -c "import sys,json; j=json.load(sys.stdin); assert j.get('cli_port') == 1279, j"
```

## 2. ONU data hilang setelah Sync

### Symptom
- Setelah Sync ONU, tabel ONU kosong (total 0).

### Root cause
`UpsertONUs()` melakukan `DELETE FROM olt_onus ...` sebelum walk SNMP berhasil. Jika walk error atau mengembalikan 0 baris, data lama terhapus.

### Fix
- Abort jika walk mengembalikan 0 ONU.
- Bungkus `DELETE` dan semua `INSERT` dalam satu transaction, rollback pada error.

## 3. Scan error setelah migration nullable columns

### Symptom
- `GET /onus` atau `/onus-paged` error `can't scan NULL into *string` (atau `*float64`).

### Root cause
Kolom baru (`ip_address`, `distance_m`, `in_bps`, `out_bps`) nullable, tapi query SELECT memasukkannya dan scan ke non-pointer type.

### Reliable fix
Gunakan `COALESCE(col, default)` di SELECT dan scan ke non-pointer field struct:
```sql
SELECT ..., COALESCE(ip_address,''), COALESCE(distance_m,0), COALESCE(in_bps,0), COALESCE(out_bps,0)
```
Hindari scan NULL ke pointer karena perilaku pgx bisa berbeda di image runtime vs local.

## 4. Sync via web error / timeout

### Symptom
- Klik Sync di web muncul "Request gagal." padahal API via curl berhasil.

### Root cause
- Browser `fetch` default timeout ~30 detik, sedangkan sync ONU bisa >60 detik.
- Backend `context.WithTimeout` handler sync hanya 60 detik.

### Fix
- Frontend: abort timeout 200 detik untuk path `/sync-onus` dan `/refresh-traffic`.
- Backend: naikkan handler timeout menjadi 180 detik.

## 5. SNMP community case-sensitive

### Symptom
- Test OLT timeout padahal community "sama".

### Root cause
SNMP v2c community bersifat case-sensitive.

### Fix
Mintalah user melihat community persis di OLT:
```
ZXAN(config)# show snmp configuration
```
pastikan huruf besar/kecil sama persis.

## 6. Deploy binary tidak tertimpa

### Symptom
- Source sudah fix tapi API perilakunya masih lama.

### Root cause
Docker compose/cache/image lama masih dipakai.

### Fix paksa
```bash
sg docker -c "docker compose rm -f -s -v api migrate web"
sg docker -c "docker rmi -f app-api app-migrate app-web 2>/dev/null || true"
sg docker -c "docker builder prune -af"
sg docker -c "docker compose build --no-cache api migrate web"
sg docker -c "docker compose up -d --wait"
```
Selalu verifikasi response shape endpoint setelah deploy, bukan hanya container status.
