# Compare Provision ONU: BILLING-FIX-PHP vs AWGRevBILL (Fix v76)

## Masalah nyata yang muncul
Error di AWG saat provision manual:

`eksekusi "interface gpon-olt_1/1/1" ditolak OLT: %Error 20200 Invalid command`

## Akar masalah
Di AWG lama, flow `ProvisionONU` menjalankan command satu per satu via `session.Execute()`:
1. `interface gpon-olt_<pon>`
2. `onu <id> type <type> sn <sn>`

Untuk mode SSH, `Execute()` bersifat stateless per command. Akibatnya mode konfigurasi/interface tidak selalu terbawa; command `interface ...` bisa dieksekusi saat belum masuk mode config dan ditolak OLT.

## Cara PHP yang terbukti jalan
Di BILLING-FIX-PHP (`controllers/olt_v2/OltV2RegisterController.php`):
- selalu masuk `conf t` dulu,
- lalu `interface gpon-olt_<pon>`,
- lalu `onu ... type ... sn ...`,
- name/description di context `interface gpon-onu_<pon>:<onu>`.

## Perubahan di AWGRevBILL (v76)
File: `internal/olt/cli_service.go` (fungsi `ProvisionONU`)

### 1) Ganti eksekusi menjadi sequence mode-aware
- Dari `Execute()` per command
- Menjadi `ExecuteSequence()` dengan fallback preamble:
  - `(tanpa preamble)`
  - `configure terminal`
  - `configure`
  - `enable + configure terminal`
  - `enable + configure`

### 2) Register ONU dalam satu sequence
Body utama:
- `interface gpon-olt_<pon>`
- `onu <id> type <type> sn <serial>`
- `exit`
- `end`

### 3) Description di context ONU (parity PHP)
Kandidat 1 (utama):
- `interface gpon-onu_<pon>:<onu>`
- `description <text>`
- `exit`
- `end`

Kandidat 2 (fallback firmware):
- `interface gpon-olt_<pon>`
- `onu <id> description <text>`
- `exit`
- `end`

## Dampak
- Menghilangkan false-error `Invalid command` akibat mode CLI tidak konsisten.
- Menyamakan strategi kerja AWG dengan pola PHP yang sudah stabil.
- Tetap kompatibel SSH/Telnet dan firmware C320 yang butuh fallback mode.

## Verifikasi build
- `docker compose -p app build --no-cache api` ✅
- `docker compose -p app up -d --wait api` ✅

