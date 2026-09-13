# ZTE C320/C300 — SNMP OID Reference
# Sumber: github.com/s4lfanet/nms-ztec320 (olt_adapters/snmp_oids.py)
#         + gponsolution.com + tembolok.id (CLI syntax)
# Semua OID relatif terhadap enterprise prefix .1.3.6.1.4.1.3902
# kecuali yang diawali .1.3.6.1.2.1 (standard MIB).
#
# Firmware v2.2+ memakai tree .3902.1082 ; v2.1 memakai .3902.1012.

## ── Card / Board Health (index: slot) ──
card_type       = .3902.1082.10.1.2.4.1.4.1.1
card_status     = .3902.1082.10.1.2.4.1.5.1.1
card_cpu        = .3902.1082.10.1.2.4.1.9.1.1
card_memory     = .3902.1082.10.1.2.4.1.11.1.1
card_role       = .3902.1082.10.1.2.4.1.13.1.1   # active/standby
card_temp       = .3902.1082.10.10.2.1.6.1.2.1.1  # suhu card
fan_speed       = .3902.1082.10.10.2.1.6.1.5.1.1
psu_voltage     = .3902.1082.10.10.2.3.11.1.2.1.1

## ── ONU Identity (index: ifIndex.onuId) — tree .3902.1082.500.10 ──
onu_sn          = .3902.1082.500.10.2.3.3.1.18    # serial string
onu_sn_hex      = .3902.1082.500.10.2.3.3.1.6     # serial hex
onu_desc        = .3902.1082.500.10.2.3.3.1.2
onu_name        = .3902.1082.500.10.2.3.3.1.3
onu_model       = .3902.1082.500.20.2.1.2.1.8
onu_status      = .3902.1082.500.10.2.3.8.1.4
onu_distance    = .3902.1082.500.10.2.3.10.1.2    # meter
pon_port_name   = .3902.1082.500.10.2.2.3.1.1

## ── Optical ONU-side (index: ifIndex.onuId.1) — tree .3902.1082.500.20 ──
onu_rx_down     = .3902.1082.500.20.2.2.2.1.10    # redaman downstream ONU
onu_tx_up       = .3902.1082.500.20.2.2.2.1.14    # transmit upstream ONU

## ── Optical OLT-side (index: ponIndex.onuId) — tree .3902.1015 ──
olt_rx          = .3902.1015.1010.11.2.1.2        # penerimaan di sisi OLT
onu_tx_olt_view = .3902.1015.1010.11.2.1.3

## ── SFP / PON Port Diagnostics (index: diagIndex) ──
sfp_olt_rx      = .3902.1015.3.1.13.1.1
sfp_tx_power    = .3902.1015.3.1.13.1.4
sfp_bias        = .3902.1015.3.1.13.1.9
sfp_voltage     = .3902.1015.3.1.13.1.10
sfp_wavelength  = .3902.1015.3.1.13.1.11
sfp_temp        = .3902.1015.3.1.13.1.12
sfp_model       = .3902.1015.3.1.13.1.13
sfp_vendor      = .3902.1015.3.1.13.1.14

## ── Standard MIBs ──
if_descr        = .1.3.6.1.2.1.2.2.1.2
if_oper_status  = .1.3.6.1.2.1.2.2.1.8
if_admin_status = .1.3.6.1.2.1.2.2.1.7
if_in_octets    = .1.3.6.1.2.1.31.1.1.1.6   # ifHCInOctets (64-bit)
if_out_octets   = .1.3.6.1.2.1.31.1.1.1.10  # ifHCOutOctets
sys_uptime      = .1.3.6.1.2.1.1.3.0
sys_descr       = .1.3.6.1.2.1.1.1.0

## ── CLI Templates (telnet/ssh) ──
show_card      = show card
show_fan       = show fan
onu_state      = show gpon onu state gpon-olt_{frame}/{slot}/{port}
onu_baseinfo   = show gpon onu baseinfo gpon-olt_{frame}/{slot}/{port}
onu_detail     = show gpon onu detail-info gpon-onu_{frame}/{slot}/{port}:{onu_id}
onu_equip      = show gpon remote-onu equip gpon-onu_{frame}/{slot}/{port}:{onu_id}
onu_optical    = show pon power attenuation gpon-onu_{frame}/{slot}/{port}:{onu_id}
onu_reboot_mng = pon-onu-mng gpon-onu_{frame}/{slot}/{port}:{onu_id}
optical_module = show interface optical-module-info {port_name}
running_config = show running-config

## ── Decoder penting dari snmp_core.py ──
# encode_pon_index(slot, port) -> index PON untuk tree .1015
# decode_rx_power(raw)         -> nilai dBm dari raw counter
# decode_distance(raw)         -> meter
# decode_c300_run_status(v)    -> status ONU (working/los/dying-gasp/logoff)
# parse_serial(val)            -> normalisasi SN (ZTEG...)


## ── Alert / Monitoring Patterns (dari alerts.py nms-ztec320) ──
# Pola alert yang terbukti berguna untuk NMS produksi:

# 1. RX Power rendah (absolute threshold)
#    - Ambil onu_rx_down per ONU
#    - Jika rx < threshold (default umum: -27 dBm) → alert 'rx_power_low'
#    - Dedupe: jangan kirim ulang alert sama dalam 2 jam (AlertHistory.last_alert_at)

# 2. RX drop mendadak (change threshold)
#    - Bandingkan rx sekarang vs sampling sebelumnya
#    - Perubahan >= rx_change_threshold (mis. 3 dB) → alert 'signal_drop'

# 3. ONU offline batch
#    - Group per PON port; jika banyak ONU LOS di satu PON → kemungkinan
#      masalah di port/fiber trunk, bukan per-pelanggan → alert berbeda

# 4. Recovery notification
#    - ONU yang sebelumnya alert lalu kembali working → notifikasi 'recovered'

# 5. Unregistered SN detection
#    - SN terdeteksi di PON tapi tidak ada di DB → alert 'unregistered'
#    - Berguna menangkap pelanggan colok sendiri / calon pelanggan baru

# 6. Alert routing
#    - Telegram bot & WhatsApp gateway untuk teknisi
#    - Batch message per PON agar tidak spam

## ── Fitur lain yang layak diadopsi ke AWGRevBILL ──
# auto_backup.py  -> backup config OLT otomatis terjadwal
# auto_sync.py    -> sinkronisasi ONU berkala tanpa manual sync
# sync_job.py     -> progress tracking (pct, msg) untuk UI sync besar
# metrics_service.py -> endpoint metrics (Prometheus-compatible?)
