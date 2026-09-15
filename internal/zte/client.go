// Package zte mengimplementasikan klien SNMP untuk OLT ZTE C320/C300.
//
// OID utama yang dipakai (ZTE enterprise 3902, GPON):
//   - sysDescr       : 1.3.6.1.2.1.1.1.0
//   - sysUpTime      : 1.3.6.1.2.1.1.3.0
//   - ONU list       : 1.3.6.1.4.1.3902.1082.100.1.2.2.1.1 (zxGponOnuIndex)
//   - ONU name       : ...100.1.2.2.1.2   (zxGponOnuName)
//   - ONU serial     : ...100.1.2.2.1.4   (zxGponOnuSn)
//   - ONU status     : ...100.1.2.3.1.5   (zxGponOnuOltStatus: 1=logging,2=LOS,3=syncMib,4=working,5=dyingGasp,6=authFailed)
//   - ONU Rx power   : ...100.1.2.5.1.8   (zxGponOnuOpticalRxPower, 0.01 dBm)
//   - ONU Tx power   : ...100.1.2.5.1.9   (zxGponOnuOpticalTxPower)
//   - Reboot ONU     : set zxGponOnuReset (1.3.6.1.4.1.3902.1082.100.1.10.1.1.<idx>) = 1
//   - Disable ONU    : set zxGponOnuAdminStatus ... = 2 ; enable = 1
package zte

import (
	"context"
	"encoding/hex"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/gosnmp/gosnmp"
)

const (
	oidSysDescr  = "1.3.6.1.2.1.1.1.0"
	oidSysUpTime = "1.3.6.1.2.1.1.3.0"

	// Set operasi
	oidONUReboot = "1.3.6.1.4.1.3902.1082.100.1.10.1.1"  // + "." + index, Integer 1
	oidONUAdmin  = "1.3.6.1.4.1.3902.1082.100.1.2.2.1.7" // admin status per index (1=enable, 2=disable) — beberapa firmware memakai tabel ini

	timeout = 5 * time.Second
	retries = 3
)

// Credentials berisi parameter koneksi SNMP yang sudah didekripsi.
type Credentials struct {
	Host            string
	Port            uint16
	Mode            string // "v2c" atau "v3"
	Community       string // v2c
	V3User          string
	V3AuthProto     string // md5|sha|sha256|sha512
	V3AuthPass      string
	V3PrivProto     string // des|aes|aes192|aes256
	V3PrivPass      string
	V3ContextName   string
	V3ContextEngine string
}

// ONU adalah satu entri ONU hasil pembacaan OLT.
type ONU struct {
	Index        string  `json:"index"`
	ONUNumber    string  `json:"onu_number"`
	Name         string  `json:"name"`
	SerialNumber string  `json:"serial_number"`
	Model        string  `json:"model,omitempty"`
	Description  string  `json:"description"`
	Status       string  `json:"status"`
	RxPowerDBM   float64 `json:"rx_power_dbm"`
	TxPowerDBM   float64 `json:"tx_power_dbm"`
	DistanceM    float64 `json:"distance_m,omitempty"`
	IPAddress    string  `json:"ip_address,omitempty"`
	InBps        float64 `json:"in_bps,omitempty"`
	OutBps       float64 `json:"out_bps,omitempty"`
}

// SystemInfo adalah identitas dasar OLT.
type SystemInfo struct {
	SysDescr  string `json:"sys_descr"`
	SysUpTime string `json:"sys_uptime"`
	ModelHint string `json:"model_hint"`
}

func authProtocol(p string) gosnmp.SnmpV3AuthProtocol {
	switch strings.ToLower(p) {
	case "md5":
		return gosnmp.MD5
	case "sha256":
		return gosnmp.SHA256
	case "sha512":
		return gosnmp.SHA512
	default:
		return gosnmp.SHA
	}
}

func privProtocol(p string) gosnmp.SnmpV3PrivProtocol {
	switch strings.ToLower(p) {
	case "des":
		return gosnmp.DES
	case "aes192":
		return gosnmp.AES192
	case "aes256":
		return gosnmp.AES256
	default:
		return gosnmp.AES
	}
}

// Connect membuka sesi SNMP sesuai kredensial.
// snmpTuning mengembalikan timeout/retries/maxRepetitions; dapat dioverride via
// env (SNMP_TIMEOUT_SECONDS, SNMP_RETRIES, SNMP_MAX_REPETITIONS) untuk jaringan
// WAN yang rawan drop paket. Default 5s/3 retries/MaxRepetitions 10 (respons UDP
// lebih kecil => lebih tahan fragmentasi & packet loss lintas WAN).
func snmpTuning() (time.Duration, int, uint32) {
	t, r, mr := timeout, retries, uint32(10)
	if v := os.Getenv("SNMP_TIMEOUT_SECONDS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			t = time.Duration(n) * time.Second
		}
	}
	if v := os.Getenv("SNMP_RETRIES"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			r = n
		}
	}
	if v := os.Getenv("SNMP_MAX_REPETITIONS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			mr = uint32(n)
		}
	}
	return t, r, mr
}

func Connect(creds Credentials) (*gosnmp.GoSNMP, error) {
	tmo, rtr, maxRep := snmpTuning()
	session := &gosnmp.GoSNMP{
		Target:         creds.Host,
		Port:           creds.Port,
		Transport:      "udp",
		Timeout:        tmo,
		Retries:        rtr,
		MaxOids:        60,
		MaxRepetitions: maxRep,
		Context:        context.Background(),
	}
	if creds.Mode == "v3" {
		session.Version = gosnmp.Version3
		session.SecurityModel = gosnmp.UserSecurityModel
		session.SecurityParameters = &gosnmp.UsmSecurityParameters{
			UserName:                 creds.V3User,
			AuthenticationProtocol:   authProtocol(creds.V3AuthProto),
			AuthenticationPassphrase: creds.V3AuthPass,
			PrivacyProtocol:          privProtocol(creds.V3PrivProto),
			PrivacyPassphrase:        creds.V3PrivPass,
		}
		session.MsgFlags = gosnmp.AuthPriv
		if creds.V3AuthPass == "" && creds.V3PrivPass == "" {
			session.MsgFlags = gosnmp.NoAuthNoPriv
		} else if creds.V3PrivPass == "" {
			session.MsgFlags = gosnmp.AuthNoPriv
		}
		if creds.V3ContextEngine != "" || creds.V3ContextName != "" {
			session.ContextEngineID = creds.V3ContextEngine
			session.ContextName = creds.V3ContextName
		}
	} else {
		session.Version = gosnmp.Version2c
		session.Community = creds.Community
	}
	if err := session.Connect(); err != nil {
		return nil, fmt.Errorf("koneksi udp %s:%d: %w", creds.Host, creds.Port, err)
	}
	return session, nil
}

// GetSystem membaca sysDescr + sysUpTime dan menebak model OLT.
func GetSystem(session *gosnmp.GoSNMP) (SystemInfo, error) {
	result, err := session.Get([]string{oidSysDescr, oidSysUpTime})
	if err != nil {
		return SystemInfo{}, fmt.Errorf("snmp get: %w", err)
	}
	var info SystemInfo
	for _, v := range result.Variables {
		switch v.Type {
		case gosnmp.OctetString:
			info.SysDescr = string(v.Value.([]byte))
		case gosnmp.TimeTicks:
			info.SysUpTime = formatUptime(gosnmp.ToBigInt(v.Value).Uint64())
		}
	}
	upper := strings.ToUpper(info.SysDescr)
	switch {
	case strings.Contains(upper, "C600"):
		info.ModelHint = "ZTE-C600"
	case strings.Contains(upper, "C320"):
		info.ModelHint = "ZTE-C320"
	case strings.Contains(upper, "C300"):
		info.ModelHint = "ZTE-C300"
	default:
		info.ModelHint = ""
	}
	return info, nil
}

type walkResult struct {
	values map[string]string
	err    error
}

// WalkONUs membaca seluruh ONU beserta status dan redamannya memakai
// profil OID sesuai firmware OLT. Walk sekuensial per-OID.
// Fallback: jika primary OID gagal, coba alt OID dalam profil.
func WalkONUs(ctx context.Context, session *gosnmp.GoSNMP, profile *FirmwareProfile) ([]ONU, error) {
	addOID := func(suffix string) string {
		if suffix == "" {
			return ""
		}
		if strings.HasPrefix(suffix, "1.") {
			return suffix
		}
		return profile.BaseOID + suffix
	}

	oidName := addOID(profile.ONUName)
	oidSerial := addOID(profile.ONUSerial)
	oidSerialHex := addOID(profile.ONUSerialHex)
	oidDescr := addOID(profile.ONUDescr)
	oidStatus := addOID(profile.ONUStatus)
	oidRx := addOID(profile.ONURxPower)
	oidTx := addOID(profile.ONUTxPower)
	oidDist := addOID(profile.Distance)
	oidOLTRx := profile.OLTRxPower
	oidOLTTx := profile.OLTTxPower
	oidOLTRx2 := profile.OLTSFPRx2
	oidOLTTx2 := profile.OLTSFPTx2

	walkOne := func(oid string) (map[string]string, error) {
		values := map[string]string{}
		if oid == "" {
			return values, nil
		}
		err := session.Walk(oid, func(pdu gosnmp.SnmpPDU) error {
			// gosnmp mengembalikan nama OID dengan titik depan (".1.3.6..."),
			// sedangkan root tanpa titik — normalisasi dulu supaya suffix
			// (mis. "285278465.1") benar-benar terpotong. Tanpa ini key
			// tersimpan sebagai OID penuh dan merge antar-tabel gagal.
			name := strings.TrimPrefix(pdu.Name, ".")
			key := strings.TrimPrefix(name, oid+".")
			if key == name {
				key = strings.TrimPrefix(key, ".")
			}
			values[key] = pduValue(pdu)
			return nil
		})
		return values, err
	}

	// Core: name wajib berhasil. Retry karena SNMP UDP lintas WAN kadang drop
	// paket — satu timeout jangan menggagalkan seluruh sync.
	var name map[string]string
	var err error
	for attempt := 1; attempt <= 3; attempt++ {
		name, err = walkOne(oidName)
		if err == nil {
			break
		}
	}
	if err != nil {
		return nil, fmt.Errorf("snmp walk name: %w", err)
	}

	// Serial: non-fatal (beberapa firmware timeout/empty di OID utama),
	// fallback ke hex variant bila hasilnya kosong.
	serial, _ := walkOne(oidSerial)
	if oidSerialHex != "" {
		hexMap, _ := walkOne(oidSerialHex)
		for idx, val := range hexMap {
			if serial[idx] == "" && val != "" {
				serial[idx] = decodeHexSerial(val)
			}
		}
	}

	// Status: non-fatal — status OID sering kosong di beberapa firmware.
	status, _ := walkOne(oidStatus)

	// Optional fields — error non-fatal. Rx/Tx per-ONU di-walk terpisah dan
	// di-merge dengan lookup lokasi karena key tabel optical bisa format
	// diagIndex.onuID (contoh .1012/.1082), bukan gponOnuIndex.
	descr, _ := walkOne(oidDescr)
	rxMap, _ := walkOne(oidRx)
	txMap, _ := walkOne(oidTx)
	dist, _ := walkOne(oidDist)
	// Build lookup lokasi untuk optical tables.
	rxLoc := buildLocMap(rxMap)
	txLoc := buildLocMap(txMap)
	oltRx, _ := walkOne(oidOLTRx)
	oltTx, _ := walkOne(oidOLTTx)
	oltRx2, _ := walkOne(oidOLTRx2)
	oltTx2, _ := walkOne(oidOLTTx2)

	// Parse OLT-side SFP diagnostics per PON port.
	// Index format: diagIndex = 0x10000000 + (ponIndex << 8) + lane
	// suffix "1" → port 0, "256" → port 1, "512" → port 2.
	// Encoding (debug-walk produksi 2026-08-29): nilai mentah dBm×1000
	// (Tx 6450 → +6.45 dBm; dBm×100 menghasilkan 64.5 dBm mustahil).
	// Nilai sentinel INT32_MAX = kolom tidak diimplementasi firmware.
	const sfpBaseIdx = 268435456
	oltRxByPort := map[int]float64{}
	oltTxByPort := map[int]float64{}
	for suffix, raw := range oltRx {
		port := atoiPortIndex(suffix, sfpBaseIdx)
		if port >= 0 {
			oltRxByPort[port] = sfpMilliToFloat(raw)
		}
	}
	for suffix, raw := range oltTx {
		port := atoiPortIndex(suffix, sfpBaseIdx)
		if port >= 0 {
			oltTxByPort[port] = sfpMilliToFloat(raw)
		}
	}
	// Fallback ke alternatif factor dBm×100 jika primary tidak memenuhi.
	if len(oltRx2) > 0 {
		for suffix, raw := range oltRx2 {
			port := atoiPortIndex(suffix, sfpBaseIdx)
			if port >= 0 && oltRxByPort[port] == 0 {
				if v := sfpPowerCentiFloat(raw); v != 0 {
					oltRxByPort[port] = v
				}
			}
		}
	}
	if len(oltTx2) > 0 {
		for suffix, raw := range oltTx2 {
			port := atoiPortIndex(suffix, sfpBaseIdx)
			if port >= 0 && oltTxByPort[port] == 0 {
				if v := sfpPowerCentiFloat(raw); v != 0 {
					oltTxByPort[port] = v
				}
			}
		}
	}

	// Sumber daftar ONU = tabel konfigurasi (name) SAJA (hindari entri
	// hantu dari tabel status yang menyimpan baris ONU lama).
	// Pencocokan lintas tabel memakai lokasi logis (slot,port,onuId)
	// karena tiap tabel ZTE memakai format key berbeda (debug-walk
	// produksi 2026-08-29):
	//   .1082 name/serial/dist : "285278465.1"  (port = byte terakhir)
	//   .1012 status/distance  : "268501248.N" / "268501248" (port = byte ke-3)
	// lookup: exact key -> lokasi(slot,port,id).
	statusLoc := buildLocMap(status)
	distLoc := buildLocMap(dist)
	serialLoc := buildLocMap(serial)
	descrLoc := buildLocMap(descr)
	onus := make([]ONU, 0, len(name))
	for _, index := range sortedKeys(name) {
		slot, port, id, locOK := keyLoc(index)
		pick := func(exact, loc map[string]string) string {
			if v := exact[index]; v != "" {
				return v
			}
			if locOK {
				return loc[locKey(slot, port, id)]
			}
			return ""
		}
		descrVal := pick(descr, descrLoc)
		rxVal := pick(rxMap, rxLoc)
		txVal := pick(txMap, txLoc)
		onu := ONU{
			Index:        index,
			ONUNumber:    onuLabel(index),
			Name:         name[index],
			SerialNumber: cleanSerial(pick(serial, serialLoc)),
			Model:        descrVal,
			Description:  descrVal,
			Status:       onuStatusText(pick(status, statusLoc)),
			RxPowerDBM:   OpticalFloat(strToUint(rxVal), profile.OpticalEncoding),
			TxPowerDBM:   OpticalFloat(strToUint(txVal), profile.OpticalEncoding),
			DistanceM:    distanceMeters(pick(dist, distLoc)),
		}
		// Default: JANGAN isi Rx/Tx ONU dari SFP OLT-side per-port (tidak per-ONU).
		// Aktifkan hanya dengan env ZTE_ALLOW_OLT_SFP_POWER_FALLBACK=1.
		if strings.TrimSpace(os.Getenv("ZTE_ALLOW_OLT_SFP_POWER_FALLBACK")) == "1" {
			gport := portFromIndex(index)
			if onu.RxPowerDBM == 0 {
				if v, hit := oltRxByPort[gport]; hit && v != 0 {
					onu.RxPowerDBM = v
				}
			}
			if onu.TxPowerDBM == 0 {
				if v, hit := oltTxByPort[gport]; hit && v != 0 {
					onu.TxPowerDBM = v
				}
			}
		}
		onus = append(onus, onu)
	}
	return onus, nil
}

// keyLoc mem-parse suffix indeks ZTE ke lokasi logis (slot, port, onuId).
// Dua format key yang muncul di produksi:
//
//	0xSSLLPP01-ish ".1082": port = byte terakhir (mis. 0x11010106 → 6)
//	".1012"              : byte terakhir 0 → port = byte ke-3 (0x10010600 → 6)
//
// id ONU selalu komponen kedua (parts[1]) baik untuk:
//
//	v2.2 key "ifIndex.onuId" (2 parts)
//	v2.1 cfgTable "ponIndex.cfgId" (2 parts)
//	v2.1 regTable "ponIndex.onuSlot.onuId" (3 parts, onuId biasanya 1)
//
// parts terakhir regTable v2.1 adalah onuId=1 yang bukan identifier ONU.
func keyLoc(key string) (slot, port, id int, ok bool) {
	parts := strings.Split(key, ".")
	v, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || v <= 0xFFFF {
		return 0, 0, 0, false
	}
	// .1082: low byte = pon, slot di bits 8-15 (bits 16-23 = shelf).
	// .1012: low byte 0 -> port di bits 8-15, slot di bits 16-23.
	if v&0xFF != 0 {
		slot = int((v >> 8) & 0xFF)
		port = int(v & 0xFF)
	} else {
		slot = int((v >> 16) & 0xFF)
		port = int((v >> 8) & 0xFF)
	}
	if len(parts) >= 2 {
		if n, e := strconv.Atoi(parts[1]); e == nil && n > 0 {
			id = n
		}
	}
	if port <= 0 {
		return 0, 0, 0, false
	}
	return slot, port, id, true
}

func locKey(slot, port, id int) string {
	return strconv.Itoa(slot) + "/" + strconv.Itoa(port) + ":" + strconv.Itoa(id)
}

// buildLocMap membuat peta lookup keyed lokasi "slot/port:id".
// Entri tanpa ".id" (tabel per-port) disimpan dengan id 0.
func buildLocMap(source map[string]string) map[string]string {
	out := make(map[string]string, len(source))
	for k, v := range source {
		if v == "" {
			continue
		}
		if slot, port, id, ok := keyLoc(k); ok {
			out[locKey(slot, port, id)] = v
		}
	}
	return out
}

// distanceMeters menormalkan nilai jarak ZTE (meter mentah dari .1082;
// 0.001 km per-port dari .1012 sengaja tidak dipakai).
func distanceMeters(raw string) float64 {
	if raw == "" || isSentinel(raw) {
		return 0
	}
	v, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
	if err != nil || v < 0 || v > 100000 {
		return 0
	}
	return v
}

// (onuLoc/indexByLoc versi lama dihapus — diganti keyLoc/buildLocMap
// yang menangani dua format key hybrid .1082/.1012.)

// atoiPortIndex parses a ZTE diag index suffix and returns the port number.
// Returns -1 if the suffix cannot be parsed.
func strToUint(s string) uint64 {
	if s == "" || isSentinel(s) {
		return 0
	}
	v, err := strconv.ParseUint(strings.TrimSpace(s), 10, 64)
	if err != nil {
		return 0
	}
	return v
}

func atoiPortIndex(suffix string, baseIdx int) int {
	// suffix is like "1", "256", "513" after the root OID.
	rel, err := strconv.Atoi(strings.TrimPrefix(suffix, "."))
	if err != nil {
		// Try to extract last numeric segment
		parts := strings.Split(suffix, ".")
		if len(parts) > 0 {
			rel, err = strconv.Atoi(parts[len(parts)-1])
		}
		if err != nil {
			return -1
		}
	}
	port := (rel - baseIdx) >> 8
	if port < 0 {
		port = rel >> 8
	}
	return port
}

// portFromIndex menghitung nomor PON port GLOBAL (0-based) dari suffix
// indeks ONU ZTE, untuk mencocokkan dengan kunci tabel diagnosa SFP
// (.1015.3.1.13.1, suffix = 0x10000000 + globalPort<<8).
// gponOnuIndex contoh: 0x11010106 → shelf 1, slot 1, PON port 6 (1-based).
// Global = (slot-1)*PORTS_PER_CARD + (port-1); C320 GTGO = 8 port/kartu.
func portFromIndex(index string) int {
	parts := strings.Split(index, ".")
	if len(parts) == 0 {
		return -1
	}
	v, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || v <= 0xFFFF {
		return -1
	}
	slot := (v >> 8) & 0xFF // slot di bits 8-15 (bits 16-23 = shelf)
	port := v & 0xFF
	if slot == 0 || port == 0 {
		return -1
	}
	return int(slot-1)*8 + int(port-1)
}

// sfpMilliToFloat mengonversi nilai mentah diagnosa optical ZTE ke dBm.
// Encoding terverifikasi (debug-walk produksi 2026-08-29): dBm x1000 —
// Tx 6450 → +6.45 dBm. Sentinel INT32_MAX berarti kolom tidak didukung.
// Fallback dBm x100 jika hasil tidak masuk akal setelah /1000.
func sfpMilliToFloat(raw string) float64 {
	if raw == "" || isSentinel(raw) {
		return 0
	}
	v, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
	if err != nil {
		return 0
	}
	out := v / 1000
	if out <= 30 && out >= -60 {
		return out
	}
	out = v / 100
	if out <= 30 && out >= -60 {
		return out
	}
	return 0
}

// sfpPowerMilliFloat mengonversi nilai mentah factor dBm x1000.
func sfpPowerMilliFloat(raw string) float64 {
	if raw == "" || isSentinel(raw) {
		return 0
	}
	v, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
	if err != nil {
		return 0
	}
	return v / 1000
}

// sfpPowerCentiFloat mengonversi nilai mentah factor dBm x100.
func sfpPowerCentiFloat(raw string) float64 {
	if raw == "" || isSentinel(raw) {
		return 0
	}
	v, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
	if err != nil {
		return 0
	}
	return v / 100
}

// OpticalFloat mengonversi nilai mentah ZTE per-ONU ke dBm.
// Mode centi_minus_30 (.500.20.2.2 C300): signed 16-bit × 0.01 - 30 (sumber s4lfanet).
// Mode minus_10000_div_100 (.3.50.12 C320 v2.1 sumber kroto69): (raw-10000)/100.
// Mode raw_div_500_minus_30 (.3.50.12 alternatif sumber s4lfanet): raw/500 - 30.
// Mode dbuw_0_002_minus_30 (.500.20.2.2 C320 v2.2 sumber s4lfanet): raw*0.002 - 30.
func OpticalFloat(raw uint64, encoding string) float64 {
	if raw == 0 || raw == 65535 || raw == 2147483647 || raw == 4294967295 {
		return 0
	}
	v := float64(raw)
	switch encoding {
	case "centi_minus_30":
		if v == 65535 {
			return 0
		}
		if v >= 32768 {
			v = v - 65536
		}
		return v*0.01 - 30
	case "minus_10000_div_100":
		if v == 65535 || v == 0 {
			return 0
		}
		return (v - 10000) / 100
	case "raw_div_500_minus_30":
		if v == 65535 || v == 0 {
			return 0
		}
		return v/500.0 - 30
	case "dbuw_0_002_minus_30":
		if v == 65535 || v == 0 {
			return 0
		}
		if v >= 32768 {
			v = v - 65536
		}
		return v*0.002 - 30
	default:
		return sfpMilliToFloat(strconv.FormatUint(raw, 10))
	}
}

// atoiPortIndex parses a ZTE diag index suffix and returns the port number.
// Input: suffix string like "1", "256", "513" after the SFP diag root OID.
// Returns the port number extracted from the diagIndex encoding:
//   - diagIndex = 0x10000000 + (ponIndex << 8) + lane
//   - so suffix - baseIdx gives (ponIndex << 8) + lane, then >> 8 yields ponIndex.

// walkSafe menjalankan walk dan menelan error — kegagalan subtree opsional
// (rx/tx/distance/descr) tidak boleh menggagalkan sync keseluruhan.
func walkSafe(oid string) chan walkResult {
	ch := make(chan walkResult, 1)
	if oid == "" {
		ch <- walkResult{values: map[string]string{}}
		return ch
	}
	go func() {
		session := walkSession
		if session == nil {
			ch <- walkResult{values: map[string]string{}}
			return
		}
		values := map[string]string{}
		_ = session.Walk(oid, func(pdu gosnmp.SnmpPDU) error {
			values[strings.TrimPrefix(pdu.Name, oid+".")] = pduValue(pdu)
			return nil
		})
		ch <- walkResult{values: values} // err selalu nil (non-fatal)
	}()
	return ch
}

// walkSession diisi oleh WalkONUs sebelum menjalankan walkSafe.
var walkSession *gosnmp.GoSNMP

// pduValue menormalisasi nilai PDU menjadi string.
func pduValue(pdu gosnmp.SnmpPDU) string {
	switch pdu.Type {
	case gosnmp.OctetString:
		return sanitizeBytes(pdu.Value.([]byte))
	case gosnmp.Integer, gosnmp.Counter32, gosnmp.Gauge32, gosnmp.Counter64:
		return gosnmp.ToBigInt(pdu.Value).String()
	default:
		return fmt.Sprintf("%v", pdu.Value)
	}
}

// walkOptional menjalankan walk hanya jika OID tersedia untuk firmware ini.
func walkOptional(ctx context.Context, session *gosnmp.GoSNMP, oid string) chan walkResult {
	ch := make(chan walkResult, 1)
	go func() {
		if oid == "" {
			ch <- walkResult{values: map[string]string{}}
			return
		}
		values := map[string]string{}
		err := session.Walk(oid, func(pdu gosnmp.SnmpPDU) error {
			value := ""
			switch pdu.Type {
			case gosnmp.OctetString:
				value = sanitizeBytes(pdu.Value.([]byte))
			case gosnmp.Integer, gosnmp.Counter32, gosnmp.Gauge32:
				value = gosnmp.ToBigInt(pdu.Value).String()
			default:
				value = fmt.Sprintf("%v", pdu.Value)
			}
			values[strings.TrimPrefix(pdu.Name, oid+".")] = value
			return nil
		})
		ch <- walkResult{values: values, err: err}
	}()
	return ch
}

// ResetONU melakukan reboot ONU pada index tertentu.
func ResetONU(session *gosnmp.GoSNMP, index string) error {
	_, err := session.Set([]gosnmp.SnmpPDU{{
		Name:  oidONUReboot + "." + index,
		Type:  gosnmp.Integer,
		Value: 1,
	}})
	if err != nil {
		return fmt.Errorf("reboot onu %s: %w", index, err)
	}
	return nil
}

// SetONUAdmin men-disable (false) atau meng-enable (true) ONU.
func SetONUAdmin(session *gosnmp.GoSNMP, index string, enable bool) error {
	value := 2
	label := "disable"
	if enable {
		value = 1
		label = "enable"
	}
	_, err := session.Set([]gosnmp.SnmpPDU{{
		Name:  oidONUAdmin + "." + index,
		Type:  gosnmp.Integer,
		Value: value,
	}})
	if err != nil {
		return fmt.Errorf("%s onu %s: %w", label, index, err)
	}
	return nil
}

// StatusFromCode mengonversi kode status ONU numerik (OID .500.10.2.3.8.1.4)
// ke label kanonik. Sumber kebenaran tunggal — dipakai full sync & per-ONU.
func StatusFromCode(v uint64) string {
	switch v {
	case 1:
		return "logging"
	case 2:
		return "los"
	case 3:
		return "sync_mib"
	case 4:
		return "working"
	case 5:
		return "dying_gasp"
	case 6:
		return "offlined"
	case 7:
		return "auth_failed"
	default:
		return "unknown"
	}
}

func onuStatusText(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "unknown"
	}
	n, err := strconv.ParseUint(raw, 10, 64)
	if err != nil {
		return "unknown"
	}
	return StatusFromCode(n)
}

// onuLabel mengubah indeks internal ZTE (mis. 285278469.5) menjadi
// lokasi port yang mudah dibaca (1/1/5:5 = shelf/slot/port: onuId).
// gponOnuIndex = 0x11<<24 | shelf<<16 | slot<<8 | pon — slot di bits 8-15
// (terverifikasi hardware: slot2 -> 0x110102xx, C300 slot3 -> 0x110103xx).
// Decode lama salah baca byte shelf sebagai slot => ONU beda slot dengan
// pon:id sama mendapat label kembar dan detail/CLI menembak ONU lain.
func onuLabel(index string) string {
	parts := strings.Split(index, ".")
	if len(parts) >= 2 {
		if v, err := strconv.ParseInt(parts[0], 10, 64); err == nil && v > 0xFFFF {
			shelf := (v >> 16) & 0xFF
			slot := (v >> 8) & 0xFF
			port := v & 0xFF
			id := parts[len(parts)-1]
			if shelf > 0 && slot > 0 && port > 0 {
				return strconv.FormatInt(shelf, 10) + "/" +
					strconv.FormatInt(slot, 10) + "/" +
					strconv.FormatInt(port, 10) + ":" + id
			}
		}
	}
	return index
}

func centiToFloat(raw string) float64 {
	if raw == "" {
		return 0
	}
	value, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return 0
	}
	// Sentinel ZTE = INT32_MAX (2147483647) = "tidak ada data"
	if value >= 2147483000 {
		return 0
	}
	return value / 100
}

// deciToFloat: nilai dalam 0.1 satuan (jarak ZTE = decimeter).
func deciToFloat(raw string) float64 {
	if raw == "" {
		return 0
	}
	value, err := strconv.ParseFloat(raw, 64)
	if err != nil || value >= 2147483000 {
		return 0
	}
	return value / 10
}

// cleanSerial membuang prefix "N," (mis. "1,FHTTC14AC580" -> "FHTTC14AC580").
func cleanSerial(raw string) string {
	if raw == "" {
		return ""
	}
	if i := strings.IndexByte(raw, ','); i >= 0 && i < len(raw)-1 {
		prefix := raw[:i]
		if allDigit(prefix) {
			return raw[i+1:]
		}
	}
	return raw
}

// CleanSerial versi publik untuk jalur per-ONU (paket olt).
func CleanSerial(raw string) string { return cleanSerial(raw) }

// DecodeHexSerial versi publik untuk jalur per-ONU (paket olt).
func DecodeHexSerial(raw string) string { return decodeHexSerial(raw) }

func allDigit(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

// decodeHexSerial mengubah serial hex-encoded (mis. "464854544331340000"
// atau "46:48:54:54") menjadi ASCII ("FHTTC14"). Bila input bukan hex
// murni, kembalikan apa adanya (sudah berbentuk string serial).
func decodeHexSerial(raw string) string {
	s := strings.TrimSpace(raw)
	s = strings.ReplaceAll(s, ":", "")
	s = strings.ReplaceAll(s, " ", "")
	if s == "" || len(s)%2 != 0 || len(s) > 64 {
		return raw
	}
	isHex := true
	for _, c := range s {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			isHex = false
			break
		}
	}
	if !isHex {
		return raw
	}
	buf := make([]byte, len(s)/2)
	for i := range buf {
		lo := s[i*2]
		hi := s[i*2+1]
		decode := func(c byte) int {
			switch {
			case c >= '0' && c <= '9':
				return int(c - '0')
			case c >= 'a' && c <= 'f':
				return int(c-'a') + 10
			default:
				return int(c-'A') + 10
			}
		}
		v := decode(lo)<<4 | decode(hi)
		// hanya terima ASCII printable
		if v < 0x20 || v > 0x7e {
			return raw
		}
		buf[i] = byte(v)
	}
	out := strings.TrimRight(string(buf), "\x00")
	if out == "" {
		return raw
	}
	return out
}

func sanitizeBytes(raw []byte) string {
	printable := true
	for _, b := range raw {
		if b < 0x20 || b > 0x7e {
			printable = false
			break
		}
	}
	if printable && len(raw) > 0 {
		return string(raw)
	}
	if len(raw) > 0 && len(raw) <= 16 {
		return strings.ToUpper(hex.EncodeToString(raw))
	}
	return ""
}

func formatUptime(ticks uint64) string {
	const tickDuration = 10 * time.Millisecond
	total := time.Duration(ticks) * tickDuration
	days := int(total.Hours()) / 24
	hours := int(total.Hours()) % 24
	minutes := int(total.Minutes()) % 60
	return fmt.Sprintf("%dd %dh %dm", days, hours, minutes)
}

// TrafficSample adalah hasil pembacaan counter satu titik waktu.
type TrafficSample struct {
	InOctets  uint64
	OutOctets uint64
	Name      string `json:"-"` // opsional, dipakai debugging IfNameTraffic
}

// defaultPrivateCounterColumns menentukan pasangan in/out untuk tabel
// privat ZTE per-ONU (.500.10.2.3.2.2.1). Teridentifikasi dari delta-test
// produksi 2026-08-30:
//
//	col 2 = upstream bytes ONU (delta per-ONU sebanding hc_in port)
//	col 1 = downstream-ish bytes ONU (delta besar, muncul pertama dalam tabel)
//
// Pada ONT streaming, col 1 & 2 keduanya naik signifikan per-ONU,
// sedangkan col 3-18 adalah paket/error/lookup counter yang lebih kecil.
var privateInCol = 1
var privateOutCol = 2

// PrivateInCol / PrivateOutCol diekspos agar service.go tidak perlu
// duplikasi init environment variable.
func PrivateInCol() int  { return privateInCol }
func PrivateOutCol() int { return privateOutCol }

func init() {
	if v := os.Getenv("ZTE_ONU_TRAFFIC_IN_COL"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			privateInCol = n
		}
	}
	if v := os.Getenv("ZTE_ONU_TRAFFIC_OUT_COL"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			privateOutCol = n
		}
	}
}

// SampleTrafficPrivateONU membaca counter privat ZTE untuk satu ONU
// menggunakan gponOnuIndex.onuId. Dipakai saat ifTable tidak memiliki
// interface per-ONU (firmware C320 V2.1.0 hybrid tree .1082).
func SampleTrafficPrivateONU(session *gosnmp.GoSNMP, index string) (TrafficSample, error) {
	var sample TrafficSample
	base := onuPrivateCounterBase + "." + index + "."
	cols := map[string]*uint64{
		base + strconv.Itoa(privateInCol):  &sample.InOctets,
		base + strconv.Itoa(privateOutCol): &sample.OutOctets,
	}
	oids := make([]string, 0, len(cols))
	for oid := range cols {
		oids = append(oids, oid)
	}
	result, err := session.Get(oids)
	if err != nil {
		return TrafficSample{}, err
	}
	hits := 0
	for _, v := range result.Variables {
		if v.Type == gosnmp.NoSuchObject || v.Type == gosnmp.NoSuchInstance || v.Type == gosnmp.EndOfMibView {
			continue
		}
		target, ok := cols[v.Name]
		if !ok {
			// Normalize OID (gosnmp sometimes prepends '.').
			target, ok = cols[strings.TrimPrefix(v.Name, ".")]
		}
		if !ok {
			continue
		}
		*target = gosnmp.ToBigInt(v.Value).Uint64()
		hits++
	}
	if hits == 0 {
		return TrafficSample{}, fmt.Errorf("counter privat tidak tersedia untuk ONU %s", index)
	}
	return sample, nil
}

// SampleTrafficONU membaca counter oktet untuk satu ifIndex ONU.
// Firmware V2.1.0 produksi TIDAK melayani GET ifHCInOctets (64-bit) —
// fallback ke ifInOctets/ifOutOctets 32-bit (wrap ditangani SQL).
func SampleTrafficONU(session *gosnmp.GoSNMP, ifIndex string) (TrafficSample, error) {
	var sample TrafficSample
	result, err := session.Get([]string{
		TrafficHCIn + "." + ifIndex,
		TrafficHCOut + "." + ifIndex,
	})
	if err == nil {
		hits := 0
		for _, v := range result.Variables {
			name := strings.TrimPrefix(v.Name, ".")
			root := strings.TrimPrefix(TrafficHCIn, ".")
			if strings.HasPrefix(name, root+".") {
				sample.InOctets = gosnmp.ToBigInt(v.Value).Uint64()
			} else {
				sample.OutOctets = gosnmp.ToBigInt(v.Value).Uint64()
			}
			hits++
		}
		if hits == 2 {
			return sample, nil
		}
	}
	result, err = session.Get([]string{
		TrafficInOctets + "." + ifIndex,
		TrafficOutOctets + "." + ifIndex,
	})
	if err != nil {
		return TrafficSample{}, err
	}
	hits := 0
	for _, v := range result.Variables {
		name := strings.TrimPrefix(v.Name, ".")
		root := strings.TrimPrefix(TrafficInOctets, ".")
		if strings.HasPrefix(name, root+".") {
			sample.InOctets = gosnmp.ToBigInt(v.Value).Uint64()
		} else {
			sample.OutOctets = gosnmp.ToBigInt(v.Value).Uint64()
		}
		hits++
	}
	if hits == 0 {
		return TrafficSample{}, fmt.Errorf("counter tidak tersedia untuk ifIndex %s", ifIndex)
	}
	return sample, nil
}

// WalkIfDescr membaca seluruh ifDescr untuk memetakan ifIndex ONU.
func WalkIfDescr(session *gosnmp.GoSNMP) (map[string]string, error) {
	out := map[string]string{}
	err := session.Walk(IfDescr, func(pdu gosnmp.SnmpPDU) error {
		if pdu.Type == gosnmp.OctetString {
			out[strings.TrimPrefix(pdu.Name, IfDescr+".")] = string(pdu.Value.([]byte))
		}
		return nil
	})
	return out, err
}

// ONULabel adalah versi publik dari onuLabel: suffix indeks -> "1/1/6:1".
func ONULabel(index string) string { return onuLabel(index) }

// WalkIfNameLabels memindai ifName (1.3.6.1.2.1.2.2.1.2) dan ifDescr,
// mengembalikan peta "1/1/6:1" -> ifIndex. Interface ONU pada ZTE C320
// bernama berpola lokasi (mis. "gpon-onu_1/1/6:1" / "PON-ONU 1/1/6:1").
func WalkIfNameLabels(session *gosnmp.GoSNMP) map[string]string {
	out := map[string]string{}
	scan := func(root string, override bool) {
		_ = session.Walk(root, func(pdu gosnmp.SnmpPDU) error {
			if pdu.Type != gosnmp.OctetString {
				return nil
			}
			name := strings.TrimPrefix(pdu.Name, ".")
			idx := strings.TrimPrefix(name, root+".")
			if idx == name {
				idx = strings.TrimPrefix(idx, ".")
			}
			if lab := extractLocLabel(sanitizeBytes(pdu.Value.([]byte))); lab != "" {
				if override {
					out[lab] = idx
				} else if _, hit := out[lab]; !hit {
					out[lab] = idx
				}
			}
			return nil
		})
	}
	scan(IfName, true)   // ifName paling informatif
	scan(IfDescr, false) // fallback: fill yang belum ada
	return out
}

// extractLocLabel menemukan substring "S/S/P" atau "S/S/P:M" pertama.
func extractLocLabel(s string) string {
	digits := func(c byte) bool { return c >= '0' && c <= '9' }
	for i := 0; i < len(s); i++ {
		if !digits(s[i]) {
			continue
		}
		// butuh pola: digit+ '/' digit+ '/' digit+ (':' digit+)?
		j := i
		for sl := 0; sl < 3; sl++ {
			if sl > 0 {
				if j >= len(s) || s[j] != '/' {
					j = -1
					break
				}
				j++
			}
			start := j
			for j < len(s) && digits(s[j]) {
				j++
			}
			if j == start {
				j = -1
				break
			}
			if sl == 2 && j > i+1 && j < len(s) && s[j] == ':' {
				k := j + 1
				ds := k
				for k < len(s) && digits(s[k]) {
					k++
				}
				if k > ds {
					return s[i:k]
				}
			}
		}
		if j < 0 {
			continue
		}
		return s[i:j]
	}
	return ""
}

// GosnmpHealthWalker mengadaptasi gosnmp.GoSNMP ke antarmuka HealthWalker.
type GosnmpHealthWalker struct {
	Session *GoSNMPWrapper
}

// GoSNMPWrapper membungkus sesi SNMP (dibuat terpisah agar mudah dites).
type GoSNMPWrapper struct {
	WalkFunc func(rootOID string) (map[string]string, error)
}

// WalkAll menjalankan walk dan membuang prefix rootOID dari tiap key.
func (w *GosnmpHealthWalker) WalkAll(rootOID string) map[string]string {
	if w == nil || w.Session == nil || w.Session.WalkFunc == nil {
		return nil
	}
	values, err := w.Session.WalkFunc(rootOID)
	if err != nil {
		return nil
	}
	out := make(map[string]string, len(values))
	for k, v := range values {
		out[strings.TrimPrefix(k, rootOID+".")] = v
	}
	return out
}
