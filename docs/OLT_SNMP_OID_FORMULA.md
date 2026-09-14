# Formula OID SNMP OLT ZTE (C320/C300) — Referensi Aplikasi

Dokumen ini adalah sumber kebenaran (single source of truth) formula OID SNMP untuk
sinkronisasi ONU di aplikasi ini. Tujuan: pengambilan data **cepat & anti-timeout**
dengan query **terarah per-(board,pon)**, bukan walk seluruh tree.

Kredit referensi:
- `s4lfanet/go-api-c320` — pola OID C320 awal.
- `Cepat-Kilat-Teknologi/snmp-olt-zte` — formula ifIndex slot-parametrik (C320/C300),
  GETBULK/pooling, terverifikasi vs hardware.
- `alijayanet/billing-rtrw` — referensi tambahan OID billing RTRW.
- Validasi lapangan internal: `internal/zte/firmware.go` (C320 V2.1.0 produksi).

Implementasi generator: [internal/zte/oid_generator.go](../internal/zte/oid_generator.go)
(uji: [oid_generator_test.go](../internal/zte/oid_generator_test.go)).

---

## 1. Prinsip: jangan walk seluruh tree

Firmware ZTE (tree `.1082`) sering **timeout** bila subtree Rx/Tx per-ONU
(`.500.20.*`) di-walk global. Solusi: hitung **ifIndex** tiap (board,pon) lalu
lakukan **GETBULK ter-scope per PON** (maks ~128 ONU/PON) + **GET batch** untuk field.
Ini yang membuat pendekatan referensi mencapai ribuan req/s tanpa timeout.

---

## 2. Dua ruang index (shelf diasumsikan = 1)

### ONU-ID space — BaseOID1 `.1.3.6.1.4.1.3902.1082`
Dipakai untuk: name, serial, description, status, rx power, last online/offline,
offline reason, optical distance.

```
onuIDSuffix = OnuIDIfIndexBase + slot*OnuIDSlotStride + pon*OnuIDIncrement
            = 285278208 (0x11010000) + slot*256 (0x100) + pon*1
```

### TYPE space — BaseOID2 `.1.3.6.1.4.1.3902.1012`
Dipakai untuk: onu type, tx power, ip address.

```
onuTypeSuffix = OnuTypeIfIndexBase + slot*OnuTypeSlotStride + pon*OnuTypeIncrement
              = 268435456 (0x10000000) + slot*65536 (0x10000) + pon*256 (0x100)
```

### Nilai terverifikasi (vs hardware)

| slot | pon | onuIDSuffix (.1082) | onuTypeSuffix (.1012) |
|------|-----|---------------------|-----------------------|
| 1 | 1 | 285278465 | 268501248 |
| 2 | 1 | 285278721 | 268566784 |
| 3 (C300) | 1 | 285278977 | 268632320 |

> `board_id` di API = nomor slot fisik. C320 umumnya slot 1–2 (16 PON/slot).
> C300 bisa di slot lebih tinggi (mis. 3 dan 5), kartu GTGO=8 PON / GTGH=16 PON.

---

## 3. Prefix per-field

### ONU-ID space (relatif `BaseOID1`, lalu tambahkan `.onuIDSuffix` lalu `.onuId`)

| Field | Prefix | Catatan |
|-------|--------|---------|
| Name | `.500.10.2.3.3.1.2` | nama ONU |
| Serial (string) | `.500.10.2.3.3.1.18` | kosong di sebagian firmware |
| Serial (hex) | `.500.10.2.3.3.1.6` | fallback, decode hex → ASCII |
| Description | `.500.10.2.3.3.1.3` | |
| Status | `.500.10.2.3.8.1.4` | 1=working; enum lihat §5 |
| Rx power (ONU) | `.500.20.2.2.2.1.10` | GET terarah, JANGAN walk global |
| Last online | `.500.10.2.3.8.1.5` | |
| Last offline | `.500.10.2.3.8.1.6` | |
| Offline reason | `.500.10.2.3.8.1.7` | |
| Optical distance | `.500.10.2.3.10.1.2` | meter |

### TYPE space (relatif `BaseOID2`, lalu tambahkan `.onuTypeSuffix` lalu `.onuId`)

| Field | Prefix |
|-------|--------|
| ONU type/model | `.3.50.11.2.1.17` |
| Tx power (ONU) | `.3.50.12.1.1.14` |
| IP address | `.3.50.16.1.1.10` |

### Standar (IF-MIB / ENTITY-MIB) — untuk traffic & uplink/card auto-detect

| Field | OID |
|-------|-----|
| ifName | `1.3.6.1.2.1.31.1.1.1.1` (mis. `xgei_1/19/1`) |
| ifHCInOctets | `1.3.6.1.2.1.31.1.1.1.6` (64-bit) |
| ifHCOutOctets | `1.3.6.1.2.1.31.1.1.1.10` (64-bit) |
| ifHighSpeed | `1.3.6.1.2.1.31.1.1.1.15` (Mbps) |
| ifAdminStatus | `1.3.6.1.2.1.2.2.1.7` |
| ifOperStatus | `1.3.6.1.2.1.2.2.1.8` |
| entPhysicalDescr | `1.3.6.1.2.1.47.1.1.1.1.2` |
| entPhysicalClass | `1.3.6.1.2.1.47.1.1.1.1.5` (3=module/card) |

---

## 4. Encoding daya optik (raw → dBm)

Nilai mentah SNMP perlu dikonversi. Pilih sesuai firmware (lihat `firmware.go`):

| Kode encoding | Formula | Dipakai |
|---------------|---------|---------|
| `dbuw_0_002_minus_30` | signed16 * 0.002 − 30 | tree .1082 (v2.2) |
| `raw_div_500_minus_30` | raw/500 − 30 | tree .1012 (v2.1) |
| `centi_minus_30` | raw≥32768 → (raw−65536)*0.01−30; else raw*0.01−30 | alternatif |
| `minus_10000_div_100` | (raw−10000)/100 | alternatif |
| default | raw/1000 (sfp mili→float) | SFP diag OLT-side |

---

## 5. Enum status ONU (`.500.10.2.3.8.1.4`)

| Nilai | Arti (normalisasi app) |
|-------|------------------------|
| 1 | working (online) |
| 2 | los |
| 3 | sync_mib |
| 4 | auth_failed |
| 5 | dying_gasp |
| 6 | offlined |

---

## 6. Strategi query cepat (rekomendasi implementasi)

1. **Enumerasi per PON**: untuk tiap (board,pon) hasil `GenerateBoardPonOID`,
   GETBULK ter-scope pada `StatusOID`/`NameOID` (bounded ≤ MaxPonID*128) → daftar onuId aktif.
2. **Ambil field**: GET batch (`MaxOids` per request, mis. 60) untuk
   serial/status/descr/type/ip/rx/tx/distance per onuId terarah.
3. **Paralel**: worker pool antar-PON + semaphore (mis. 5) agar OLT tak kebanjiran.
4. **Sesi gosnmp**: `Version` sesuai `snmp_mode`, `MaxRepetitions` (mis. 25),
   `MaxOids` (mis. 60), `Timeout`/`Retries` wajar, dan set `session.Context` ke
   context request agar timeout dihormati.
5. **Non-fatal per-PON**: kegagalan satu PON tidak menggagalkan seluruh sync
   (kumpulkan partial + catat).

Index ONU aplikasi: `Index = "<onuIDSuffix>.<onuId>"`, `ONUNumber = "<board>/<pon>:<onuId>"`.

---

## 7. Catatan firmware

- **v2.2 (tree .1082)**: default mayoritas C320/C300 modern. Serial string
  `.3.3.1.18` bisa kosong → pakai hex `.3.3.1.6`. Status `.3.8.1.4` hidup (1=working).
  JANGAN walk global `.500.20.*` (timeout) — pakai GET terarah.
- **v2.1 (tree .1012)**: sebagian tabel name/serial tidak ter-expose; enumerasi
  jatuh ke hybrid `.1082`. Distance per-PORT `.13.1.1.20` konstan (bukan per-ONU).
- Deteksi: `sysDescr` mengandung `v2.1` → profil .1012; selain itu → .1082.
