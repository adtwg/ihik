package olt

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"strings"

	"isp-billing/internal/zte"

	"github.com/gosnmp/gosnmp"
)

// onuSNMPDetail menyimpan data ONU yang bisa diambil melalui SNMP secara cepat
// tanpa perlu sesi CLI serial. Config deep (tcont/gemport/service-port) hanya
// tersedia lewat CLI dan tidak diisi di sini.
type onuSNMPDetail struct {
	PON          string
	ONUID        int
	Status       string
	Name         string
	Description  string
	SerialNumber string
	ONUType      string
	DistanceM    float64
	RxDBM        float64
	TxDBM        float64
}

// fetchONUDetailSNMP membaca status/redaman/jarak per-ONU dari SNMP via GET
// pada index ASLI ONU (mis. "285278465.1") — bukan index sintetis. Index asli
// disimpan saat sync penuh (olt_onus.index) dan menjadi kunci semua tabel
// per-ONU ZTE. GET per-index cepat & tidak memicu timeout tabel optical.
// Profil firmware dipilih murah lewat coba-GET (v2.2 lalu v2.1), tanpa walk.
func (service *Service) fetchONUDetailSNMP(ctx context.Context, tenantID, id, pon string, onuID int, index string) (*onuSNMPDetail, error) {
	session, cleanup, err := service.connect(ctx, tenantID, id)
	if err != nil {
		return nil, err
	}
	defer cleanup()

	realIndex := service.resolveRealONUIndex(ctx, tenantID, id, index, pon, onuID)
	if realIndex == "" {
		return nil, fmt.Errorf("%w: index ONU %s:%d belum tersinkron", ErrUnreachable, sanitizePON(pon), onuID)
	}

	// Coba profil v2.2 (default modern) lalu v2.1. GET O(1) jadi mencoba dua
	// profil tetap murah dibanding walk penuh.
	var partial *onuSNMPDetail
	for _, key := range []string{"v2.2", "v2.1"} {
		profile := zte.FirmwareProfiles[key]
		out := service.getONUDetailForProfile(session, profile, realIndex, pon, onuID)
		if out.hasIdentity() {
			log.Printf("[fetchONUDetailSNMP] olt=%s pon=%s onu=%d idx=%s profile=%s status=%s rx=%.2f tx=%.2f dist=%.0f",
				id, out.PON, out.ONUID, realIndex, profile.Name, out.Status, out.RxDBM, out.TxDBM, out.DistanceM)
			return out, nil
		}
		if partial == nil {
			partial = out
		}
	}

	if partial != nil && partial.hasAny() {
		return partial, nil
	}
	return nil, fmt.Errorf("%w: SNMP tidak mengembalikan data ONU index=%s", ErrUnreachable, realIndex)
}

// hasIdentity: minimal ada status atau nama/serial (bukti profil cocok).
func (d *onuSNMPDetail) hasIdentity() bool {
	return d != nil && (d.Status != "" || d.Name != "" || d.SerialNumber != "")
}

// hasAny: ada data apa pun yang berguna.
func (d *onuSNMPDetail) hasAny() bool {
	return d != nil && (d.Status != "" || d.Name != "" || d.Description != "" ||
		d.SerialNumber != "" || d.DistanceM != 0 || d.RxDBM != 0 || d.TxDBM != 0)
}

// getONUDetailForProfile menjalankan seluruh GET per-ONU untuk satu profil OID.
func (service *Service) getONUDetailForProfile(session *gosnmp.GoSNMP, profile *zte.FirmwareProfile, idx, pon string, onuID int) *onuSNMPDetail {
	out := &onuSNMPDetail{PON: sanitizePON(pon), ONUID: onuID}

	// Status
	if profile.ONUStatus != "" {
		oid := profile.BaseOID + profile.ONUStatus + "." + idx
		if v, err := snmpUintValue(session, oid); err == nil {
			out.Status = StatusFromUint(v)
		}
	}

	// Name
	if profile.ONUName != "" {
		oid := profile.BaseOID + profile.ONUName + "." + idx
		if v, err := snmpStringValue(session, oid); err == nil && v != "" {
			out.Name = v
		}
	}

	// Description
	if profile.ONUDescr != "" {
		oid := profile.BaseOID + profile.ONUDescr + "." + idx
		if v, err := snmpStringValue(session, oid); err == nil && v != "" {
			out.Description = v
		}
	}

	// Serial number (string first; hex fallback)
	if profile.ONUSerial != "" {
		oid := profile.BaseOID + profile.ONUSerial + "." + idx
		if v, err := snmpStringValue(session, oid); err == nil && v != "" {
			out.SerialNumber = strings.ToUpper(strings.TrimSpace(v))
		}
	}
	if out.SerialNumber == "" && profile.ONUSerialHex != "" {
		oid := profile.BaseOID + profile.ONUSerialHex + "." + idx
		if v, err := snmpStringValue(session, oid); err == nil && v != "" {
			out.SerialNumber = strings.ToUpper(strings.TrimSpace(v))
		}
	}

	// ONU type
	if profile.ONUType != "" {
		oid := profile.BaseOID + profile.ONUType + "." + idx
		if v, err := snmpStringValue(session, oid); err == nil && v != "" {
			out.ONUType = v
		}
	}

	// Distance
	if profile.Distance != "" {
		oid := profile.BaseOID + profile.Distance + "." + idx
		if v, err := snmpUintValue(session, oid); err == nil && v > 0 {
			out.DistanceM = float64(v)
		}
	}

	// Optical Rx/Tx per-ONU
	rx, tx, rxErr, txErr := fetchONUOpticalByIndex(session, profile, idx)
	if rxErr == nil {
		out.RxDBM = rx
	}
	if txErr == nil {
		out.TxDBM = tx
	}

	return out
}

// fetchONUOpticalByIndex mencoba ambil Rx/Tx via GET pada OID per-ONU
// memakai index ASLI. GET tunggal aman meski walk tabel optical timeout.
func fetchONUOpticalByIndex(session *gosnmp.GoSNMP, profile *zte.FirmwareProfile, idx string) (rx, tx float64, rxErr, txErr error) {
	if profile.ONURxPower == "" && profile.ONUTxPower == "" {
		return 0, 0, fmt.Errorf("OID optical tidak tersedia"), fmt.Errorf("OID optical tidak tersedia")
	}

	rawRx := uint64(0)
	if profile.ONURxPower != "" {
		oid := profile.BaseOID + profile.ONURxPower + "." + idx
		v, err := snmpUintValue(session, oid)
		if err != nil {
			rxErr = err
		} else {
			rawRx = v
		}
	}

	rawTx := uint64(0)
	if profile.ONUTxPower != "" {
		oid := profile.BaseOID + profile.ONUTxPower + "." + idx
		v, err := snmpUintValue(session, oid)
		if err != nil {
			txErr = err
		} else {
			rawTx = v
		}
	}

	// Decode hasil; sentinel 0 / 65535 dianggap kosong.
	if rxErr == nil {
		rx = zte.OpticalFloat(rawRx, profile.OpticalEncoding)
	}
	if txErr == nil {
		tx = zte.OpticalFloat(rawTx, profile.OpticalEncoding)
	}
	return
}

// fetchONUTrafficSNMP membaca counter per-ONU dari tabel ifTable (ifName/ifHCInOctets/ifHCOutOctets).
// Jika ifName label ONU tidak ditemukan, fallback GET ifDescr.
func (service *Service) fetchONUTrafficSNMP(ctx context.Context, tenantID, id, pon string, onuID int) (*ONUTrafficSample, error) {
	session, cleanup, err := service.connect(ctx, tenantID, id)
	if err != nil {
		return nil, err
	}
	defer cleanup()

	// Build expected label, e.g. "1/1/5:7"
	labels := zte.WalkIfNameLabels(session)
	label := zte.ONULabel(fmt.Sprintf("%s:%d", sanitizePON(pon), onuID))
	ifIndex, ok := labels[label]
	if !ok {
		return nil, fmt.Errorf("%w: ifName %s tidak ditemukan", ErrUnreachable, label)
	}

	sample, err := zte.SampleTrafficONU(session, ifIndex)
	if err != nil {
		return nil, err
	}
	return &ONUTrafficSample{InOctets: sample.InOctets, OutOctets: sample.OutOctets, Method: "snmp_iftable"}, nil
}

// isRealSNMPIndex mengecek apakah string index sesuai format ZTE asli:
// segmen pertama > 0xFFFF (byte-encoded ifIndex/ponIndex), mis. "285278465.1".
func isRealSNMPIndex(index string) bool {
	index = strings.TrimSpace(index)
	if index == "" {
		return false
	}
	head := index
	if i := strings.IndexByte(index, '.'); i > 0 {
		head = index[:i]
	}
	v, err := strconv.ParseInt(head, 10, 64)
	return err == nil && v > 0xFFFF
}

// resolveRealONUIndex menentukan index SNMP asli untuk satu ONU. Urutan:
// 1) index yang diberikan bila sudah berformat asli; 2) lookup DB by pon:onuID;
// 3) fallback sintetis lama (best-effort, dipakai bila DB belum sync).
func (service *Service) resolveRealONUIndex(ctx context.Context, tenantID, id, index, pon string, onuID int) string {
	if isRealSNMPIndex(index) {
		return strings.TrimSpace(index)
	}
	if stored, err := service.repository.FindONUIndexByRef(ctx, tenantID, id, sanitizePON(pon), onuID); err == nil && strings.TrimSpace(stored) != "" {
		return strings.TrimSpace(stored)
	}
	if gponOnuIndex, err := BuildONUIndex(pon, onuID); err == nil {
		return fmt.Sprintf("%d.%d", gponOnuIndex, onuID)
	}
	return strings.TrimSpace(index)
}

func snmpUintValue(session *gosnmp.GoSNMP, oid string) (uint64, error) {
	res, err := session.Get([]string{oid})
	if err != nil {
		return 0, err
	}
	if len(res.Variables) == 0 {
		return 0, fmt.Errorf("OID %s kosong", oid)
	}
	v := res.Variables[0]
	if v.Type == gosnmp.NoSuchObject || v.Type == gosnmp.NoSuchInstance {
		return 0, fmt.Errorf("OID %s tidak tersedia", oid)
	}
	n := gosnmp.ToBigInt(v.Value)
	if n.Sign() >= 0 && n.IsUint64() {
		return n.Uint64(), nil
	}
	return 0, fmt.Errorf("OID %s bukan unsigned", oid)
}

func snmpStringValue(session *gosnmp.GoSNMP, oid string) (string, error) {
	res, err := session.Get([]string{oid})
	if err != nil {
		return "", err
	}
	if len(res.Variables) == 0 {
		return "", fmt.Errorf("OID %s kosong", oid)
	}
	v := res.Variables[0]
	if v.Type == gosnmp.NoSuchObject || v.Type == gosnmp.NoSuchInstance {
		return "", fmt.Errorf("OID %s tidak tersedia", oid)
	}
	if b, ok := v.Value.([]byte); ok {
		return strings.TrimSpace(string(b)), nil
	}
	return strings.TrimSpace(fmt.Sprintf("%v", v.Value)), nil
}

// DiscoverONUSNMP menjelajah OID kandidat untuk satu ONU pada index ASLI dan
// melaporkan nilai/error tiap OID di kedua profil firmware (v2.2 & v2.1) plus
// pembacaan BP MIB VLAN/service-port. Alat verifikasi Phase-0 (loopback-only).
func (service *Service) DiscoverONUSNMP(ctx context.Context, tenantID, id, pon string, onuID int) (map[string]any, error) {
	p, oid, ok := resolveONURef("", pon, onuID)
	if !ok {
		return nil, ErrInvalidInput
	}

	session, cleanup, err := service.connect(ctx, tenantID, id)
	if err != nil {
		return nil, err
	}
	defer cleanup()
	session.Context = ctx

	realIndex := service.resolveRealONUIndex(ctx, tenantID, id, "", p, oid)
	result := map[string]any{"pon": p, "onu_id": oid, "real_index": realIndex}
	if realIndex == "" {
		result["warning"] = "index asli belum tersinkron; jalankan sync penuh dulu"
		return result, nil
	}

	probe := func(col string) any {
		if col == "" {
			return nil
		}
		full := col + "." + realIndex
		res, gerr := session.Get([]string{full})
		if gerr != nil {
			return "ERR:" + gerr.Error()
		}
		if len(res.Variables) == 0 {
			return "NOVAR"
		}
		v := res.Variables[0]
		if v.Type == gosnmp.NoSuchObject || v.Type == gosnmp.NoSuchInstance || v.Type == gosnmp.EndOfMibView {
			return "NOSUCH"
		}
		if b, ok := v.Value.([]byte); ok {
			return strings.TrimSpace(string(b))
		}
		return gosnmp.ToBigInt(v.Value).String()
	}

	profilesReport := map[string]any{}
	for _, key := range []string{"v2.2", "v2.1"} {
		profile := zte.FirmwareProfiles[key]
		if profile == nil {
			continue
		}
		base := profile.BaseOID
		profilesReport[profile.Name] = map[string]any{
			"base_oid":   base,
			"status":     probe(base + profile.ONUStatus),
			"name":       probe(base + profile.ONUName),
			"descr":      probe(base + profile.ONUDescr),
			"serial":     probe(base + profile.ONUSerial),
			"serial_hex": probe(base + profile.ONUSerialHex),
			"type":       probe(base + profile.ONUType),
			"distance":   probe(base + profile.Distance),
			"rx_power":   probe(base + profile.ONURxPower),
			"tx_power":   probe(base + profile.ONUTxPower),
		}
	}
	result["profiles"] = profilesReport

	// BP MIB VLAN/service-port (reuse session).
	if vlans, ports, spErr := readONUServicePortsSNMP(session, p, oid); spErr == nil {
		result["vlans"] = vlans
		result["service_ports"] = ports
	} else {
		result["service_ports_error"] = spErr.Error()
	}

	return result, nil
}
