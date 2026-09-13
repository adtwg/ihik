# AWGRevBILL — Alur Deploy, Topologi, dan Playbook Anti-Error

> Dokumen ini adalah **pegangan tetap**. Setiap kali menyentuh production,
> baca ini dulu. Ditulis 2026-08-29 setelah rangkaian incident OLT NMS.

## 1. Topologi Production

```
Browser
  │  http://barubill.dasnet.biz.id
  ▼
nginx aaPanel :80  (punya port 80 — JANGAN direbut)
  │  proxy_pass 127.0.0.1:8090
  ▼
Caddy gateway (container app-gateway-1, bind 127.0.0.1:8090:80)
  │   Caddyfile WAJIB: auto_https off, :80, TANPA header HSTS
  ├── /api/* /health /ready → app-api-1 :8080   (Go 1.24)
  └── sisanya               → app-web-1 :3000   (Next.js)
app-api-1
  ├── postgres → app-postgres-1 (PG17, user runtime: isppay_app)
  └── SNMP/SSH → OLT ZTE C320 103.163.80.142:7298 (community terenkripsi AES-GCM
      dengan APP_ENCRYPTION_KEY dari .env)
```

Dependency chain compose: `postgres → migrate → api → web → gateway`.
**Merestart `api` tanpa `--wait` membuat web/gateway ikut mati** — ini
penyebab nomor satu "tiba-tiba web error setelah deploy".

## 2. File Kunci Server

| Path | Fungsi | Aturan |
|---|---|---|
| `/opt/isp-billing/app/.env` | kredensial DB + APP_ENCRYPTION_KEY | **chattr +i** (terkunci). Jangan pernah kirim .env lewat tarball |
| `/opt/isp-billing/deploy.sh` | sumber konfigurasi historis + **key asli** | jangan dijalankan untuk "fix config" (password DB-nya lama) |
| `/opt/isp-billing/app/Caddyfile` | reverse proxy | auto_https off; `-Strict-Transport-Security` |
| `/opt/isp-billing/app/deploy-safe.sh` | **deployer resmi** | snapshot → build → up --wait → verify → auto-rollback |
| `/opt/isp-billing/snap-*/` | snapshot tiap deploy | rollback manual: `bash /opt/isp-billing/rollback.sh <dir>` |

## 3. Alur Deploy Standar (satu-satunya yang boleh dipakai)

```bash
# di mesin lokal (Hermes):
cd <repo>
go build ./... && go vet ./...            # via docker golang:1.24-alpine
# + smoke-test binary api (cek panic rute dll)
tar czf src.tar.gz --exclude='.env' --exclude='web/node_modules' \
    --exclude='.git' --exclude='web/.next' .
base64 src.tar.gz > src.b64
curl -F reqtype=fileupload -F fileToUpload=@src.b64 https://catbox.moe/user/api.php

# di terminal aaPanel (paste blok):
cd /opt/isp-billing
curl -L -o src.tar.gz <url>
tar xzf src.tar.gz -C app/ --exclude=.env
cd app && bash deploy-safe.sh
```

`deploy-safe.sh` memverifikasi sebelum menyatakan sukses:
1. `/ready` 200 via gateway → **grep regex `"status":"(ok|ready)"`**, JANGAN
   string-persis (pernah me-rollback build sehat karena API balas `"ready"`)
   → kalau gagal: auto-rollback
2. grants `olt_onus` punya DELETE (user isppay_app)
3. api, web, gateway, postgres semua Up/healthy
4. login probe API merespon

## 4. Aturan Anti-Ulang (bekas insiden, jangan diulang)

1. **JANGAN pernah masukkan `.env` ke tarball.** Incident 26 Agu: .env lokal
   menimpa server → DB auth hancur + key enkripsi berubah → semua kredensial
   OLT/Mikrotik tak bisa didekripsi ("ciphertext is invalid", 500 semua).
2. **`--no-cache` wajib** pada `docker compose build`. Layer `COPY . .` caches
   binary basi. Gejala: pesan error masih menyebut "1 retries" padahal kode 3.
3. **Selalu `docker compose up -d` penuh / `--wait`.** `up -d api` saja →
   web+gateway tumbang diam-diam → "Request gagal" di login.
4. **`docker compose run --rm migrate` setiap deploy** — grants & skema.
5. **Test-connection OLT sukses tapi sync timeout = OID mati di kode**, bukan
   jaringan. Jangan pernah minta user ping/nc. OLT C320 V2.1.0 ini TIDAK
   punya subtree optical per-ONU — walk ke `.500.20.*` = timeout beruntun.
6. **Satu perubahan OID terverifikasi sekali waktu**, valid via debug-walk,
   bukan spekulasi dari repo orang lain.
7. **Perintah untuk terminal aaPanel: baris pendek, tanpa `$(...)`, `{{}}`,
   `&& \`, dan TANPA nilai secret literal** (sistem redaksi chat mengubah
   key/password jadi `***` dan merusak file — ini yang mengulang crash-loop
   key 64-hex). Nilai rahasia selalu diambil dari file di server via grep.
8. **Compile + smoke check sebelum upload.** Binary harus bisa start tanpa
   panic (route duplikat `mux.Handle` = panic saat boot, semua mati).
9. **Revert-first**: sinkron yang sebelumnya jalan lalu rusak setelah deploy
   → restore snapshot/catbox URL terakhir yang diketahui baik, baru debugging.
10. **Setelah deploy, verifikasi data di DB**, bukan hanya HTTP 200:
    `SELECT index,onu_number,name,serial_number,status,rx_power_dbm,tx_power_dbm,
     distance_m FROM olt_onus WHERE olt_id='6802682a-b8c8-415d-910d-36a278dde0f4' LIMIT 5;`
    `index` harus suffix numerik (`285278465.1`), BUKAN OID penuh `.1.3.6...`
    (kalau OID penuh = bug TrimPrefix leading-dot gosnmp, lihat docs zte).

## 5. Kenyataan Firmware OLT Produksi (ZTE C320 V2.1.0 hybrid)

Terverifikasi 2026-08-29 via `GET /api/v1/olts/{id}/debug-walk` (lihat
skill olt-sync-troubleshooting → references/verified-production-oids.md).

| Data | Status | Sumber |
|---|---|---|
| Daftar ONU + nama | ✅ | .1082.500.10.2.3.3.1.2 (54 riil) |
| Serial (SN) | ✅ | hex .3.3.1.6 → decode ASCII (string .3.3.1.18 kosong) |
| Status online | ✅ | .3.8.1.4 (1=working) |
| Jarak | ✅ | .500.10.2.3.10.1.2 meter (key sama dgn name) |
| Tx dBm | ✅ | SFP per-port .1015.3.1.13.1.4, encoding **dBm×1000** |
| Rx dBm | ❌ firmware tidak expose (.13.1.1 sentinel; .13.1.2 = threshold) | tampilkan "—" |
| Trafik bps | ⚠️ | GET ifHC 64-bit ditolak firmware → fallback 32-bit .2.2.1.10/.16 |

## 6. Endpoint Debug yang Tersedia (jangan bongkar-pasang kode untuk diagnosis)

- `GET  /api/v1/olts/{id}/debug-walk` → counts + sampel mentah semua tabel OID
- `POST /api/v1/olts/{id}/test`       → single SNMP GET
- `GET  /api/v1/olts/{id}/health`     → card/CPU/SFP (read-only, tak butuh DB)
- `POST /api/v1/olts/{id}/sync-onus`  → baca + simpan cache
- `POST /api/v1/olts/{id}/refresh-traffic` → sampling bps (klik 2x, jarak ±30 dtk)

Semua butuh cookie `isp_session` + header `X-On-Behalf-Tenant`.
