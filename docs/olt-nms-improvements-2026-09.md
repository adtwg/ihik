# OLT NMS — Riwayat Perbaikan & Fitur (2026-09)

Dokumen pelacakan (changelog teknis) untuk pekerjaan NMS OLT: perbaikan
Detail & Sync per-ONU berbasis SNMP, dan menu baru visualisasi perangkat
(chassis) OLT. Dibuat agar perubahan mudah dilacak & dilanjutkan.

Referensi OID: lihat [zte-oid-reference.md](zte-oid-reference.md).
Keamanan edit/sync: lihat [olt-edit-sync-safety.md](olt-edit-sync-safety.md).

---

## Pembaruan 2026-09-16: validasi Tes SNMP system

- GetSystem sebelumnya menerima paket kosong/NoSuchObject sebagai sukses,
  sehingga UI bisa menulis tersambung tanpa model maupun uptime.
- Tes sekarang memeriksa packet error, OID sysDescr/sysUpTime yang tepat,
  tipe nilai, deskripsi tidak kosong, dan uptime TimeTicks valid. Uptime nol
  tetap valid. Model yang tidak dikenal tetap boleh lolos bila kedua OID valid.
- Respons parsial/kosong menjadi error berisi OID yang belum terbaca dan
  arahan pemeriksaan IP/port, versi SNMP, akses view/community dan ACL VPS.
- Tes mencakup C300/C320, model unknown, uptime nol, packet nil/kosong,
  AuthorizationError, NoSuchObject, OID/tipe salah, dan data parsial.

Ini memperbaiki sukses palsu pada Tes SNMP, bukan membuktikan penyebab sync
C300 produksi. Diagnosis berikutnya memerlukan hasil baca dari VPS terhadap
sysDescr 1.3.6.1.2.1.1.1.0 dan sysUpTime 1.3.6.1.2.1.1.3.0, versi firmware,
serta konfirmasi ONU terdaftar. Jangan kirim community/password atau membuka
SNMP ke internet; periksa izin baca dari alamat sumber VPS yang benar.

## Pembaruan 2026-09-15: sync C300 dengan tabel nama kosong

- Enumerasi SNMP sebelumnya hanya memakai tabel nama. Bila tabel nama kosong
  tetapi serial konfigurasi tersedia, sync sekarang memakai indeks serial
  sebagai fallback. Tabel status tetap tidak menjadi sumber inventori karena
  bisa menyimpan ONU lama. Perilaku tabel nama yang tersedia tetap dipertahankan.
- Respons NoSuchInstance/NoSuchObject/EndOfMibView dan OID di luar subtree
  tidak menjadi baris ONU. Jika nama/serial kosong, walk optik opsional dilewati
  agar profil alternatif dapat segera dicoba.
- Error serial dan error profil v2.1 tidak lagi disamarkan sebagai dua profil
  kosong. Inventori kosong tanpa error jaringan tidak dilabeli OLT unreachable;
  pesan mengarahkan pemeriksaan registrasi ONU, SNMP view/community, dan OID.
- Tes fixture kedua profil mencakup serial-only, serial hex ASCII, tabel nama
  normal, status-only, respons unsupported, dan kegagalan serial/profil.

Batas verifikasi: belum ada hasil walk dari C300 pengguna. Fallback ini hanya
menangani serial yang tersedia di OID profil existing, bukan menjamin semua
firmware C300 kompatibel. Jika tetap kosong, periksa hasil Tes SNMP/sysDescr,
versi firmware, dan jumlah ONU terdaftar sebelum menambah profil OID baru.
Jangan kirim community, password SNMPv3, atau kredensial CLI dalam laporan.

## Pembaruan 2026-09-15: pisahkan detail dan popup trafik ONU

- Expand detail hanya memuat detail/config ONU, tanpa polling, grafik, atau
  angka trafik upstream/downstream. Profil konfigurasi tetap tersedia.
- Tombol ikon `Cek trafik ONU` pada kolom Aksi membuka popup tersendiri.
  Popup memuat riwayat intraday dan probe live, dengan rentang 5/15/60/180
  menit, jeda/lanjutkan, refresh, serta pemilih sumber SNMP atau fallback CLI.
- Live membutuhkan dua counter valid. Counter reset, pergantian metode,
  selang lebih dari 60 detik, dan integer di luar presisi JavaScript tidak
  dijadikan sampel nol. Unduh menggunakan OLT output/Tx; unggah input/Rx.
- Polling dijadwalkan setelah request sebelumnya selesai: 5 detik SNMP atau
  15 detik fallback CLI, dengan backoff error sampai 30 detik. Tab tersembunyi
  melewati probe. Jeda/close/pergantian OLT membatalkan request; refresh saat
  dijeda hanya memuat riwayat. Sumber aktual mengikuti `sample.method`.
- Error live (termasuk HTTP 502) hanya tampil dalam popup. Riwayat/sampel
  sebelumnya tetap terlihat dan tidak dianggap bacaan live terbaru.
- SNMP `SampleTrafficONU` sekarang mensyaratkan dua OID counter yang tepat
  dan tipe Counter32/Counter64. Respons NoSuchInstance, parsial, packet error,
  atau nil memicu fallback 32-bit; hasil yang tetap tidak lengkap menjadi
  error, bukan trafik nol palsu.

Validasi: tes Go `./internal/olt ./internal/zte ./internal/ztecli
./internal/httpapi`, lima tes Node API/traffic, dan typecheck frontend lulus.
Browser dengan fixture loopback: detail tidak memanggil endpoint trafik;
popup menampilkan dua garis dan live setelah dua sampel; jeda menghentikan
probe; 502 mempertahankan riwayat; Escape/tombol tutup menutup dialog;
counter request tetap setelah close; buka ulang setelah reload berhasil.
Screenshot desktop 1440px dan mobile 390px diperiksa; grafik mobile dapat
digulir horizontal tanpa membuat dialog melampaui layar.

Belum diverifikasi pada OLT/VPS produksi. Penyebab spesifik HTTP 502 di VPS
tidak dapat dipastikan dari fixture; firmware yang tidak menyediakan counter
masih dapat menghasilkan error live. Build produksi tidak dijalankan untuk
perubahan ini. Catatan di atas menggantikan alur trafik dalam detail di bawah.

## Pembaruan 2026-09-15: blocking dan tampak depan chassis

Bagian ini menggantikan catatan timeout/geometri/status di riwayat lama bawah.

### Penyebab yang ditemukan pada kode

- `clientAPI` mengganti signal milik komponen dengan controller internal. Abort
  panel/pindah OLT tidak membatalkan fetch; GET bisa diulang hingga tiga kali.
- `zte.Connect` memakai context background. Batas waktu HTTP tidak diteruskan
  ke dial/GET SNMP; socket yang sedang membaca tetap bisa menunggu retry.
- Cache `ifName` memegang mutex global selama WALK jaringan, menahan OLT lain.
- Timer enrichment tidak dibersihkan saat panel ditutup. Guard initial-fetch
  ditandai sebelum timer dijalankan, rentan cleanup/effect replay React.
- Polling trafik otomatis bisa membuka fallback CLI serial; background detail
  dan sync tidak berbagi guard per OLT. Polling statistik bisa tumpang tindih.
- Updater counter menjalankan updater series lain di dalamnya; replay React
  dapat menghasilkan sampel/timestamp grafik ganda.
- Chassis tidak membatalkan request OLT sebelumnya; port GTGH/GTGO yang tidak
  memiliki data ONU/SFP bisa tidak digambar. Semua ONU offline dilabeli LOS,
  padahal bukan bukti alarm LOS atau status link fisik port.

### Alur yang diperbaiki

1. Komponen -> `clientAPI`: signal pemanggil diteruskan ke controller fetch,
   listener dibersihkan, request dengan signal tidak di-retry otomatis.
   Timeout default tetap ada; `timeoutMs` eksplisit dipakai untuk operasi panjang.
2. Handler -> service -> `zte.ConnectContext`: context diteruskan sejak dial;
   pembatalan context menutup socket agar GET yang menunggu segera berhenti.
   Cleanup koneksi tidak lagi menghapus error perangkat seolah probe sukses.
3. Detail biasa: GET SNMP maksimal 4 detik, service-port maksimal 1 detik,
   lalu snapshot/cache; tidak menunggu CLI. Force CLI langsung menjalankan
   probe eksplisit meskipun cache masih fresh. Anggaran ini termasuk jaringan,
   bukan jaminan waktu DB atau proxy.
4. Satu pekerjaan background detail/sync per tenant/OLT. Refresh yang sedang
   sibuk tidak membangun antrean goroutine baru. Auto-refresh panel tetap
   dibatasi tiga percobaan; deep-config bisa tetap parsial jika CLI gagal/sibuk.
5. Polling trafik memakai `snmp_only=1`, anggaran SNMP 6 detik; refresh manual
   tetap mendukung CLI. Handler trafik 12 detik, browser 15 detik. Cache ifName
   tidak menahan mutex selama WALK dan menolak refresh duplikat per OLT.
6. Detail: handler 15/55 detik, browser 18/60 detik (normal/force CLI).
   Chassis: health 8 detik, handler total 25 detik, browser 28 detik.
7. Semua timer detail dibersihkan saat unmount/ganti ONU; statistik dan trafik
   hanya satu request aktif per panel. Sampel grafik tidak memiliki efek samping
   dalam updater state. Sumber trafik mengikuti `sample.method`.
8. Cache health terpisah per tenant/OLT dan mencegah probe health paralel.

### Dashboard chassis

- Tampak depan tambahan berada di atas inventori existing; tidak mengubah
  konfigurasi OLT. Blade/card dan konektor dapat dipilih untuk membaca slot,
  tipe, peran, status, PON, jumlah ONU serta optical yang tersedia.
- C320: dua blade layanan horizontal (1/2), dua modul kontrol bawah (3/4),
  panel fan samping. C300: template 16 posisi layanan, kontrol 9/10 dan
  posisi daya 19/20, blade vertikal dengan proporsi mengacu chassis 10U.
- Port GTGH=16 dan GTGO=8 tetap muncul walaupun ONU/SFP belum terbaca.
- Data tidak tersedia dilabeli belum terbaca/terdeteksi, bukan otomatis kosong.
  ONU offline tidak otomatis dilabeli LOS. Gagal membaca inventori mengembalikan
  error, bukan chassis kosong palsu. Refresh gagal mempertahankan snapshot lama
  beserta pesan error; perpindahan OLT membuang snapshot OLT sebelumnya.
- Collector SNMP menyertakan slot yang hanya memiliki card type. Parser CLI
  membaca status/peran dan mengabaikan baris terpotong.

Referensi bentuk/spesifikasi yang diperiksa (foto tidak disalin ke aplikasi):
- https://www.thunder-link.com/c320-2dc-1gtgh.html/
- https://www.thunder-link.com/c300-1gtgh_p2042.html/

### Batas Akurasi dan Uji Lapangan

Ini template tampak depan berdasarkan referensi, **bukan klaim replika persis
setiap revisi hardware**. C300 memiliki varian 14/16 slot layanan, subrack
19/21 inci, board uplink/common-interface dan susunan daya yang dapat berbeda.
Slot di luar template tetap terlihat di inventori existing. Penomoran mengacu
inventori API; multi-shelf belum dimodelkan karena `CardInfo` hanya membawa slot.

Status port masih berasal dari ONU/SFP snapshot, bukan `ifOperStatus`/admin
state terverifikasi. Indikator online berarti ada ONU online pada port tersebut.
Fan, konektor kontrol dan panel daya adalah bentuk referensi, bukan pembacaan
sensor/link mereka. Status card/role hanya ditampilkan jika data API tersedia.

Sebelum menyatakan cocok persis dengan lapangan:
1. Ambil foto tampak depan dan `show card` read-only untuk tiap varian C320/C300.
2. Bandingkan slot SMXA/SCX, GTGH/GTGO, uplink/daya dan urutan port pada chassis.
3. Cocokkan snapshot API `/chassis` dengan inventori dan status nyata perangkat.
4. Uji ONU online/offline, SNMP tidak merespons, CLI sibuk, buka/tutup detail,
   reload browser, perpindahan OLT dan refresh bersamaan beberapa pengguna.
5. Verifikasi OID oper/admin port serta multi-shelf sebelum menampilkan indikator
   sebagai status fisik link atau mendukung chassis yang bukan template ini.

### Verifikasi Lokal

```sh
go test ./internal/olt ./internal/zte ./internal/ztecli ./internal/httpapi
node --test web/src/lib/api/client.test.mjs
npm --prefix web run typecheck
npm --prefix web run build
```

Tes regresi meliputi pembatalan GET UDP tanpa balasan, isolasi WALK antar-OLT,
guard/cache health per tenant, parsing show card dan kapasitas/status port.
Tes helper API meliputi abort tanpa retry, deadline eksplisit dan timeout default.
Browser memakai fixture read-only lokal, bukan OLT produksi: reload detail,
cleanup timer (0 fetch detail setelah tutup + maju 65 detik), pergantian OLT
lambat, inspector port, serta screenshot desktop 1440px/mobile 390px. Chassis
lebar digeser dalam area sendiri; tidak membuat halaman mobile melebar.

Hasil sesi lokal: tes paket di atas, tes helper API dan typecheck lulus.
Production build mencapai compile/typecheck/collecting page data, tetapi
terminal tidak mengembalikan status akhir yang dapat diverifikasi. Race-test
juga belum terverifikasi karena masalah eksekusi terminal. Jalankan ulang
kedua gate tersebut di CI/deployment sebelum rilis. Tidak ada uji OLT live
atau perubahan konfigurasi perangkat yang dilakukan pada sesi ini.

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
