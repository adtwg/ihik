# OLT V2 Command Reference (diambil dari BILLING-FIX-PHP)

Sumber audit:
- `/media/aditjaya/New Volume/BILLING-FIX-PHP/controllers/olt_v2/*`
- `/media/aditjaya/New Volume/BILLING-FIX-PHP/helpers/olt_v2/OltV2Parser.php`
- `/media/aditjaya/New Volume/BILLING-FIX-PHP/docs/ANALISIS_OLT_MANAGER_V2.md`
- `/media/aditjaya/New Volume/BILLING-FIX-PHP/docs/PENYELESAIAN_REFACTORING_OLT_MANAGER_V2.md`

## Command inti yang terbukti dipakai OLT Manager V2

1. Status semua ONU
- `show gpon onu state`

2. Identitas per PON
- `show gpon onu baseinfo gpon-olt_<pon>`

3. Sinyal per PON / per ONU
- `show pon power onu-rx gpon-olt_<pon>`
- `show gpon onu detail-info gpon-onu_<pon>:<onu>`
- `show pon power attenuation gpon-onu_<pon>:<onu>`

4. Trafik per-ONU (referensi utama)
- `show interface gpon-onu_<pon>:<onu>`

## Catatan parser trafik (ZTE C320)
Output umum:
- `Input rate` = upstream (ONU -> OLT)
- `Output rate` = downstream (OLT -> ONU)
- `Total statistic` memuat counter kumulatif:
  - `Input: Bytes:<N>`
  - `Output: Bytes:<N>`

Untuk UI AWGRevBILL (label: Unduh/Unggah):
- `in_octets`  = **Output Bytes** (downstream / unduh)
- `out_octets` = **Input Bytes** (upstream / unggah)

## Command yang sering tidak tersedia
- `show gpon onu statistics interface ...` (di sebagian C320: unrecognized)
- Family `show pon onu traffic/statistics ...` (di sebagian C320: invalid/unrecognized)

## Strategi performa dari OLT V2 (untuk ribuan ONU)
- Jangan per-ONU dari awal.
- Fase cepat dulu:
  - `show gpon onu state`
  - loop per PON: `show gpon onu baseinfo gpon-olt_<pon>`
  - loop per PON: `show pon power onu-rx gpon-olt_<pon>`
- Enrichment detail (`detail-info` + `attenuation`) dibatasi/batch, bukan full serial tanpa batas.
