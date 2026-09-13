# Compare & Implementasi ONU Config AWGRevBILL (Go)

Tanggal: 2026-09-06
Scope: AWGRevBILL (bukan BILLING-FIX-PHP)

## 1) Compare singkat (praktis)

| Aspek | PPPoE | IPoE (DHCP) | Static IP |
|---|---|---|---|
| WAN mode | `pppoe` | `ipoe` (fallback `dhcp` di CLI) | `static` |
| Credential | Wajib username/password | Tidak perlu PPP username/password | Tidak perlu PPP username/password |
| Parameter wajib | VLAN profile, WAN ID, PPP user/pass | VLAN profile, WAN ID | VLAN profile, WAN ID, `wan_static_ip` |
| Risiko salah input | Tinggi di credential | Rendah | Sedang (harus valid IP) |
| Use case | Internet PPP account | DHCP/IPoE area | Pelanggan fixed IP |

## 2) Keputusan implementasi AWG

- Tetap pakai flow existing AWG: `uncfg -> provision-onu -> auto_config_onu`.
- Tambahkan dukungan penuh mode IP pada flow yang sama (bukan raw CLI manual):
  - `auto_config_onu` kini menerima `wan_mode: pppoe | ipoe | static`.
  - Validasi static mewajibkan `wan_static_ip`.
  - UI WAN IP dan Auto Config menampilkan opsi `Static IP` + field input static IP.

## 3) File yang diubah

1. `internal/olt/cli_service.go`
   - Auto config tidak lagi menolak `static`.
   - Error message mode diperluas jadi `pppoe/ipoe/static`.
   - Tambah validasi: jika `wan_mode=static` maka `wan_static_ip` wajib.

2. `web/src/app/(app)/olt/onu-traffic-detail.tsx`
   - Mode WAN frontend diperluas: `pppoe | ipoe | static`.
   - Form `set_wan_ip`:
     - tambah opsi `Static IP`.
     - tampilkan field `Static IP *` saat mode static.
     - kirim payload `wan_static_ip` saat static.
   - Form `auto_config_onu`:
     - tambah opsi `Static IP`.
     - tampilkan field static IP saat mode static.
     - validasi mode static wajib `wan_static_ip`.

## 4) Dampak kompatibilitas

- PPPoE lama tetap jalan (tidak breaking).
- IPoE lama tetap jalan.
- Tambahan baru: mode Static IP pada jalur yang sama.
- Tidak ada perubahan endpoint publik (kontrak tetap di endpoint existing).

## 5) Payload contoh (Auto Config - Static)

```json
{
  "operation": "auto_config_onu",
  "pon": "1/1/1",
  "onu_id": 12,
  "tcont_id": 1,
  "tcont_profile": "UPLINE",
  "gemport_id": 1,
  "service_port_id": 12,
  "vport": 1,
  "user_vlan": 1200,
  "vlan": 1200,
  "wan_ip_id": 1,
  "wan_mode": "static",
  "wan_auth_mode": "auto",
  "wan_vlan_profile": "INTERNET",
  "wan_static_ip": "10.10.10.2"
}
```
