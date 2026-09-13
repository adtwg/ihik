# OLT NMS — Riwayat Perbaikan & Fitur (2026-09)

Dokumen pelacakan (changelog teknis) untuk pekerjaan NMS OLT: perbaikan
Detail & Sync per-ONU berbasis SNMP, dan menu baru visualisasi perangkat
(chassis) OLT. Dibuat agar perubahan mudah dilacak & dilanjutkan.

Referensi OID: lihat [zte-oid-reference.md](zte-oid-reference.md).
Keamanan edit/sync: lihat [olt-edit-sync-safety.md](olt-edit-sync-safety.md).

---

## Bagian 1 — Detail & Sync per-ONU full-SNMP (fix "stuck")

### Symptom
- Ikon segitiga (detail per-ONU) dan tombol Sync per-ONU sering *stuck* /
  timeout. Panel detail lama muncul atau kosong.
- Sync per-ONU lambat (30–60 dtk) atau gagal saat konkuren (CLI locked / 409).

### Root cause (terverifikasi dari kode)
1. `fetchONUDetailSNMP` memakai **index sintetis** (`BuildONUIndex`, mis. `"4"`
   untuk 1/1/1:4) alih-alih **index SNMP asli** (mis. `"285278465.1"` =
   `ifIndex.onuId`). GET pada index salah → `NoSuchInstance` → error → jatuh ke
   CLI lambat → *stuck*. Index asli tersimpan di `olt_onus.index` saat sync penuh.
2. `fetchONUDetailSNMP` memanggil `detectBestProfile` yang **walk semua ONU 2×**
   (v2.2 + v2.1) hanya untuk memilih profil — mahal untuk lookup 1 ONU.
3. `SyncONULiveByRef` menjalankan **dua CLI berurutan** (ProbeONUConfigCLI +
   ProbeONUOpticalCLI) secara blocking saat SNMP gagal → backend 35s vs frontend
   abort 22s (mismatch).
4. Tabel counter privat ZTE hanya mendukung **WALK**, bukan GET; sedangkan
   identity/status/distance/optical mendukung **GET per-index**. Tabel optical
   `.500.20.*` **time-out bila di-WALK** → wajib GET tunggal.

### Fix
Backend (Go):
- `internal/postgres/olt_repository.go`: tambah `FindONUIndexByRef(ctx, tenantID,
  oltID, pon, onuID) (string, error)` — ambil `index` asli dari `olt_onus`.
  Ditambahkan juga ke interface `Repository` di `internal/olt/service.go`.
- `internal/olt/onu_snmp.go`:
  - `fetchONUDetailSNMP(ctx, tenantID, id, pon, onuID, index string)` — kini
    memakai `resolveRealONUIndex` (prioritas: index format asli → lookup DB →
    fallback sintetis). GET kolom di `BaseOID + col + "." + realIndex`.
  - Hilangkan `detectBestProfile`: deteksi profil **murah** dengan mencoba GET
    profil `v2.2` lalu `v2.1` (`getONUDetailForProfile`), O(1) tanpa walk.
  - `fetchONUOpticalByIndex` — Rx/Tx via **GET tunggal** per-index (bukan walk).
  - Helper: `isRealSNMPIndex`, `resolveRealONUIndex`, `hasIdentity`, `hasAny`.
- `internal/olt/service.go`:
  - `GetONUConfigDetail` — resolve real index, kembalikan snapshot SNMP instan +
    merge cache CLI bila fresh (TTL 5 mnt). CLI penuh hanya saat `forceCLI`.
  - `SyncONULiveByRef` — **tidak lagi blocking**: SNMP dulu; bila gagal →
    `scheduleCLIRefresh` (goroutine, timeout sendiri) mengisi cache di latar,
    request balikan cepat (method `cache`).
- `internal/olt/snmp_service_port.go`:
  - `fetchONUServicePortsSNMP` — baca **VLAN/service-port via ZTE BP MIB**
    (`.3902.1015.1010.5.11`), tabel yang sama dipakai jalur tulis. Best-effort;
    dipakai `GetONUConfigDetail` mengisi `ONUConfigDetail.VLANs`/`ServicePorts`.
  - `readONUServicePortsSNMP(session, pon, onuID)` — inti pembacaan agar bisa
    dipakai ulang (mis. discovery) tanpa membuka koneksi baru.

Timeout (selaras backend > frontend):
- `onuDetailCLI`: 15s (path SNMP) / 55s (forceCLI). Frontend 12s / 50s.
- `onuSyncOne`: 25s. Frontend 22s.

Traffic fallback (sudah ada, dikonfirmasi):
- `zte.SampleTrafficONU`: 64-bit `ifHCInOctets/ifHCOutOctets` → fallback 32-bit
  `ifInOctets (.1.3.6.1.2.1.2.2.1.10)` / `ifOutOctets (.16)` bila firmware menolak.

### Endpoint discovery (Phase 0, verifikasi firmware)
- `GET /internal/olts/{oltID}/onu-oid-scan?pon=&onu_id=` (loopback-only).
  Handler `internalONUOIDScan` → `Service.DiscoverONUSNMP` menjelajah OID kandidat
  pada index asli di kedua profil (v2.2 & v2.1) + baca BP MIB VLAN/service-port,
  melaporkan nilai/`NOSUCH`/`ERR` per OID. Pakai untuk mengunci OID sebelum andalkan.

### Verification
```bash
# 1. Detail per-ONU (SNMP cepat, <3 dtk, tanpa stuck)
curl "$BASE/api/v1/olts/$OLT/onu-detail-cli?pon=1/1/1&onu_id=4"

# 2. Sync per-ONU (<5 dtk, method=snmp, status/rx/tx/distance terupdate)
curl -X POST "$BASE/api/v1/olts/$OLT/onu-sync" \
  -d '{"pon":"1/1/1","onu_id":4}'

# 3. Discovery OID (loopback di server OLT)
curl "http://127.0.0.1:PORT/internal/olts/$OLT/onu-oid-scan?pon=1/1/1&onu_id=4"
```

### Catatan
- Format index asli: `ifIndex.onuId` byte-encoded (mis. `285278465.1`).
- ONU offline: balikan cepat, tidak hang (data parsial diterima).

---

## Bagian 2 — Menu baru: Visualisasi Perangkat OLT (chassis)

### Tujuan
Menu sidebar baru **"Perangkat OLT"** (`/olt/perangkat`) menampilkan skema fisik
chassis OLT (C320/C300 dst sesuai model) + card terpasang (SMXA/GTGH/GTGO) di
slot yang benar, dengan indikator status per-card (Running/Standby/Offline) dan
status per-port (online/LOS/idle/kosong). Skema **SVG/CSS data-driven**, bukan foto.

### Sumber data (sudah tersedia)
- `olts.model` → keluarga chassis (C320/C300/…).
- `GetHealth()` → `OltHealth.Cards[]` (slot, type, status, role, CPU/mem/temp) &
  `Sfps[]` (label `shelf/slot/port`, Rx/Tx). OID card: tree `.3902.1082.10`.
- `ListONUs()` → hitung ONU per PON (total/online) untuk occupancy port.
- Fallback: CLI `show card` bila daftar card SNMP kosong (umum di firmware v2.1
  yang time-out pada OID card).

### Backend (Go)
- `internal/olt/chassis.go` (BARU):
  - Tipe `ChassisView{Model, Family, Source, Cards[]}`,
    `ChassisCard{Slot, Type, Status, Role, CPU/Mem/Temp, IsControl, PortCount,
    Ports[]}`, `ChassisPort{Port, Label, HasSFP, Rx/Tx, ONUTotal, ONUOnline,
    Status}`.
  - `Service.Chassis(ctx, tenantID, id)` — gabung model + health (card/SFP) +
    occupancy ONU; fallback CLI `show card` (`parseShowCard`) bila card kosong.
  - Helper: `chassisFamily`, `isControlCard` (SCX/SMX/SXM/MCUD/…),
    `chassisPortStatus` (online|los|idle|empty), `slotPortFromPON`,
    `cardStatusFromCLI`. Online = `statusAllowsOptical(lower(status))`.
  - `Source`: `snmp` | `cli` | `snmp+cli`.
- `internal/httpapi/olt.go`: handler `oltChassis` (timeout 25s, karena bisa CLI).
- `internal/httpapi/server.go`: route
  `GET /api/v1/olts/{oltID}/chassis` (auth `PermissionRouterManage`, sama `/health`).

### Frontend (Next.js)
- `web/src/lib/olts/types.ts`: tipe `ChassisView`/`ChassisCard`/`ChassisPort`.
- `web/src/components/shell/app-sidebar.tsx`: item nav
  `{ href:"/olt/perangkat", label:"Perangkat OLT", icon: Cpu }`. Aturan *active*
  `/olt` diubah jadi exact-match agar tidak dobel sorot dengan `/olt/perangkat`.
- `web/src/app/(app)/olt/perangkat/page.tsx` (BARU): muat daftar OLT.
- `web/src/app/(app)/olt/perangkat/chassis-view.tsx` (BARU): pemilih OLT + skema:
  - `CardBlade` (control: bar CPU/MEM + suhu; line: grid port),
    `PortCell` (LED + ONU online/total, tooltip Rx dBm), `MiniBar`, `EmptyBay`,
    `Legend`, `ChassisSkeleton`.
  - `FAMILY_BASE_SLOTS`: C320=4, C300=19, C220=10, C600=16 (baseline slot kosong,
    hanya pengisi visual; card nyata tetap dari data).
  - Warna: online=emerald, standby=amber, LOS/offline=red, SFP-tanpa-ONU=sky,
    kosong=slate.

### `parseShowCard` (fallback CLI)
Toleran kolom kosong/`-`. Baris data diawali `Rack Shelf Slot` (tiga angka);
ambil `RealType` (fallback `CfgType`), perkiraan jumlah port (angka pertama
setelah kolom tipe), dan status (`INSERVICE`→InService, `STANDBY`→Standby,
`OFFLINE/FAULT/ABNORMAL`→Offline).

### Verification
```bash
# Endpoint chassis (cek model, slot, tipe card, ports, source)
curl "$BASE/api/v1/olts/$OLT/chassis"
```
- Buka `/olt/perangkat` → pilih OLT → skema tampil; LED status card & port benar.
- Uji firmware v2.1 (card SNMP kosong) → `source` = `cli`, slot/tipe terisi.
- Bandingkan C300 vs C320 → jumlah bay berbeda.

### Catatan / open items
- Geometri chassis & jumlah port per line-card masih **perkiraan**; sesuaikan
  `FAMILY_BASE_SLOTS` (frontend) & `parseShowCard` (backend) setelah melihat
  output `show card` asli.
- Scope: read-only (tampilan). Tidak termasuk aksi kontrol card, foto asli OLT,
  atau edit konfigurasi dari halaman ini.

---

## Ringkasan berkas yang disentuh
Backend:
- `internal/olt/chassis.go` (baru)
- `internal/olt/onu_snmp.go`
- `internal/olt/service.go`
- `internal/olt/snmp_service_port.go`
- `internal/postgres/olt_repository.go`
- `internal/httpapi/olt.go`
- `internal/httpapi/server.go`

Frontend:
- `web/src/lib/olts/types.ts`
- `web/src/components/shell/app-sidebar.tsx`
- `web/src/app/(app)/olt/perangkat/page.tsx` (baru)
- `web/src/app/(app)/olt/perangkat/chassis-view.tsx` (baru)

Status: semua berkas lulus diagnostik language server. Build penuh
(`go build` / `next build`) & uji live belum dijalankan (toolchain tidak
tersedia di lingkungan pengembangan saat ini).
