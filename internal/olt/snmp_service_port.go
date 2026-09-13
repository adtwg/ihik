package olt

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gosnmp/gosnmp"
)

const (
	oidBPMapType      = "1.3.6.1.4.1.3902.1015.1010.5.10.1.1"
	oidBPMapStatus    = "1.3.6.1.4.1.3902.1015.1010.5.10.1.2"
	oidBPInfoCos      = "1.3.6.1.4.1.3902.1015.1010.5.11.1.3"
	oidBPInfoVLAN     = "1.3.6.1.4.1.3902.1015.1010.5.11.1.4"
	oidBPInfoRowState = "1.3.6.1.4.1.3902.1015.1010.5.11.1.5"
)

type snmpIntWrite struct {
	oid   string
	value int
}

type servicePortSNMPResult struct {
	executed []string
}

func snmpOID(base string, indexes ...int) string {
	parts := make([]string, 0, len(indexes)+1)
	parts = append(parts, base)
	for _, idx := range indexes {
		parts = append(parts, strconv.Itoa(idx))
	}
	return strings.Join(parts, ".")
}

func parsePONForSNMP(ponPort string) (slot, port int, err error) {
	parts := strings.Split(strings.TrimSpace(ponPort), "/")
	if len(parts) == 3 {
		slot, err = strconv.Atoi(parts[1])
		if err != nil {
			return 0, 0, fmt.Errorf("slot PON tidak valid")
		}
		port, err = strconv.Atoi(parts[2])
		if err != nil {
			return 0, 0, fmt.Errorf("port PON tidak valid")
		}
	} else if len(parts) == 2 {
		slot, err = strconv.Atoi(parts[0])
		if err != nil {
			return 0, 0, fmt.Errorf("slot PON tidak valid")
		}
		port, err = strconv.Atoi(parts[1])
		if err != nil {
			return 0, 0, fmt.Errorf("port PON tidak valid")
		}
	} else {
		return 0, 0, fmt.Errorf("format PON tidak didukung")
	}
	if slot < 1 || slot > 16 || port < 1 || port > 16 {
		return 0, 0, fmt.Errorf("slot/port PON di luar rentang")
	}
	return slot, port, nil
}

// ZTE .1012 mgmt OLT index: 0x10SSPP00 (S=slot, P=pon-port).
func zteMgmtOltID(slot, port int) int {
	return 0x10000000 | ((slot & 0xFF) << 16) | ((port & 0xFF) << 8)
}

func snmpSetInts(session *gosnmp.GoSNMP, vars []snmpIntWrite) error {
	pdus := make([]gosnmp.SnmpPDU, 0, len(vars))
	for _, v := range vars {
		pdus = append(pdus, gosnmp.SnmpPDU{Name: v.oid, Type: gosnmp.Integer, Value: v.value})
	}
	packet, err := session.Set(pdus)
	if err != nil {
		return err
	}
	if packet == nil {
		return fmt.Errorf("SNMP SET tidak mengembalikan packet")
	}
	if packet.Error != gosnmp.NoError {
		return fmt.Errorf("SNMP status %v (index=%d)", packet.Error, packet.ErrorIndex)
	}
	return nil
}

func snmpGetInt(session *gosnmp.GoSNMP, oid string) (int, error) {
	packet, err := session.Get([]string{oid})
	if err != nil {
		return 0, err
	}
	if packet == nil || len(packet.Variables) == 0 {
		return 0, fmt.Errorf("SNMP GET kosong")
	}
	v := packet.Variables[0]
	switch v.Type {
	case gosnmp.Integer, gosnmp.Counter32, gosnmp.Gauge32, gosnmp.Counter64, gosnmp.Uinteger32, gosnmp.TimeTicks:
		return int(gosnmp.ToBigInt(v.Value).Int64()), nil
	case gosnmp.OctetString:
		raw, _ := v.Value.([]byte)
		n, convErr := strconv.Atoi(strings.TrimSpace(string(raw)))
		if convErr != nil {
			return 0, fmt.Errorf("GET %s bukan integer", oid)
		}
		return n, nil
	default:
		return 0, fmt.Errorf("GET %s type %v tidak didukung", oid, v.Type)
	}
}

func appendSNMPAttempt(executed []string, label string, vars []snmpIntWrite) []string {
	executed = append(executed, label)
	for _, v := range vars {
		executed = append(executed, fmt.Sprintf("snmpset %s = %d", v.oid, v.value))
	}
	return executed
}

func (service *Service) applyServicePortViaSNMP(ctx context.Context, tenantID, id, ponPort string, in ONUConfigApplyInput, targetSVLAN int) (*servicePortSNMPResult, error) {
	if in.ServicePortID < 1 || in.ServicePortID > 256 {
		return nil, fmt.Errorf("service_port_id %d di luar rentang SNMP BP index (1-256)", in.ServicePortID)
	}
	if in.VPort < 1 || in.VPort > 4095 {
		return nil, fmt.Errorf("vport %d di luar rentang SNMP", in.VPort)
	}
	if in.UserVLAN != in.VLAN || targetSVLAN != in.VLAN {
		return nil, fmt.Errorf("SNMP hanya mendukung mode VLAN 1:1 (user-vlan=vlan=svlan)")
	}
	if in.UserSVLAN > 0 || in.CTagCOS > 0 || in.STagCOS > 0 || strings.TrimSpace(in.ServiceDescription) != "" {
		return nil, fmt.Errorf("SNMP belum mendukung field lanjutan (user-svid/cos/description)")
	}
	mode, okMode := normalizeServicePortMode(in.ServicePortMode)
	if !okMode {
		return nil, fmt.Errorf("service_port_mode tidak valid untuk SNMP")
	}
	if mode != "tagged" {
		return nil, fmt.Errorf("SNMP saat ini hanya mendukung mode tagged")
	}
	etype, okEtype := normalizeServiceEtherType(in.EtherType)
	if !okEtype {
		return nil, fmt.Errorf("ether_type tidak valid untuk SNMP")
	}
	if etype != "all" {
		return nil, fmt.Errorf("SNMP saat ini hanya mendukung ether_type=all")
	}

	slot, port, err := parsePONForSNMP(ponPort)
	if err != nil {
		return nil, err
	}
	oltID := zteMgmtOltID(slot, port)
	onuID := in.ONUID
	bpIndex := in.ServicePortID
	gemIndex := in.VPort

	mapTypeOID := snmpOID(oidBPMapType, oltID, onuID, bpIndex)
	bpStatusOID := snmpOID(oidBPMapStatus, oltID, onuID, bpIndex)
	cosOID := snmpOID(oidBPInfoCos, oltID, onuID, bpIndex, gemIndex)
	vlanOID := snmpOID(oidBPInfoVLAN, oltID, onuID, bpIndex, gemIndex)
	rowStateOID := snmpOID(oidBPInfoRowState, oltID, onuID, bpIndex, gemIndex)

	session, cleanup, err := service.connect(ctx, tenantID, id)
	if err != nil {
		return nil, err
	}
	defer cleanup()
	session.Context = ctx

	executed := make([]string, 0, 24)

	bpCreate := []snmpIntWrite{{oid: mapTypeOID, value: 2}, {oid: bpStatusOID, value: 4}}
	executed = appendSNMPAttempt(executed, "SNMP create BP entry", bpCreate)
	if err := snmpSetInts(session, bpCreate); err != nil {
		bpActive := []snmpIntWrite{{oid: mapTypeOID, value: 2}, {oid: bpStatusOID, value: 1}}
		executed = appendSNMPAttempt(executed, "SNMP update BP entry", bpActive)
		if err2 := snmpSetInts(session, bpActive); err2 != nil {
			return nil, fmt.Errorf("BP entry gagal: createAndGo=%v; active=%v", err, err2)
		}
	}

	bpInfoCreate := []snmpIntWrite{{oid: cosOID, value: 0}, {oid: vlanOID, value: in.VLAN}, {oid: rowStateOID, value: 4}}
	executed = appendSNMPAttempt(executed, "SNMP create BP map info", bpInfoCreate)
	if err := snmpSetInts(session, bpInfoCreate); err != nil {
		bpInfoUpdate := []snmpIntWrite{{oid: cosOID, value: 0}, {oid: vlanOID, value: in.VLAN}, {oid: rowStateOID, value: 1}}
		executed = appendSNMPAttempt(executed, "SNMP update BP map info", bpInfoUpdate)
		if err2 := snmpSetInts(session, bpInfoUpdate); err2 != nil {
			return nil, fmt.Errorf("BP map info gagal: createAndGo=%v; active=%v", err, err2)
		}
	}

	var lastErr error
	for attempt := 0; attempt < 4; attempt++ {
		mapType, errMap := snmpGetInt(session, mapTypeOID)
		vlanSet, errVLAN := snmpGetInt(session, vlanOID)
		rowState, errRow := snmpGetInt(session, rowStateOID)
		if errMap == nil && errVLAN == nil && errRow == nil {
			if mapType == 2 && vlanSet == in.VLAN && (rowState == 1 || rowState == 4 || rowState == 5) {
				return &servicePortSNMPResult{executed: executed}, nil
			}
			lastErr = fmt.Errorf("verifikasi mismatch: mapType=%d vlan=%d rowStatus=%d", mapType, vlanSet, rowState)
		} else {
			lastErr = fmt.Errorf("verifikasi SNMP gagal: mapType=%v, vlan=%v, rowStatus=%v", errMap, errVLAN, errRow)
		}
		if attempt < 3 {
			time.Sleep(500 * time.Millisecond)
		}
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, fmt.Errorf("verifikasi SNMP service-port gagal")
}

// fetchONUServicePortsSNMP membaca konfigurasi service-port/VLAN satu ONU via
// ZTE BP MIB (.1015.1010.5.11) — tabel yang sama dipakai jalur tulis. Best-effort:
// bila firmware tak expose, kembalikan kosong tanpa error fatal.
// Index BP info: <oltID>.<onuID>.<bpIndex>.<gemIndex>.
func (service *Service) fetchONUServicePortsSNMP(ctx context.Context, tenantID, id, ponPort string, onuID int) ([]int, []ONUServicePortConfig, error) {
	session, cleanup, err := service.connect(ctx, tenantID, id)
	if err != nil {
		return nil, nil, err
	}
	defer cleanup()
	session.Context = ctx
	return readONUServicePortsSNMP(session, ponPort, onuID)
}

// readONUServicePortsSNMP menjalankan pembacaan BP MIB pada session yang sudah
// terbuka (dipakai ulang oleh discovery agar tidak membuka koneksi baru).
func readONUServicePortsSNMP(session *gosnmp.GoSNMP, ponPort string, onuID int) ([]int, []ONUServicePortConfig, error) {
	slot, port, err := parsePONForSNMP(ponPort)
	if err != nil {
		return nil, nil, err
	}
	if onuID < 1 {
		return nil, nil, fmt.Errorf("onu_id tidak valid")
	}
	oltID := zteMgmtOltID(slot, port)

	vlanPrefix := snmpOID(oidBPInfoVLAN, oltID, onuID)
	cosPrefix := snmpOID(oidBPInfoCos, oltID, onuID)

	// key "<bpIndex>.<gemIndex>" -> nilai
	vlanBy := map[string]int{}
	cosBy := map[string]int{}

	trimSuffix := func(fullName, root string) string {
		name := strings.TrimPrefix(fullName, ".")
		root = strings.TrimPrefix(root, ".")
		return strings.TrimPrefix(strings.TrimPrefix(name, root), ".")
	}

	if werr := session.Walk(vlanPrefix, func(pdu gosnmp.SnmpPDU) error {
		sfx := trimSuffix(pdu.Name, vlanPrefix)
		v := int(gosnmp.ToBigInt(pdu.Value).Int64())
		if sfx != "" && v > 0 && v < 4096 {
			vlanBy[sfx] = v
		}
		return nil
	}); werr != nil {
		return nil, nil, werr
	}
	_ = session.Walk(cosPrefix, func(pdu gosnmp.SnmpPDU) error {
		sfx := trimSuffix(pdu.Name, cosPrefix)
		v := int(gosnmp.ToBigInt(pdu.Value).Int64())
		if sfx != "" && v >= 0 {
			cosBy[sfx] = v
		}
		return nil
	})

	ports := make([]ONUServicePortConfig, 0, len(vlanBy))
	vlanSet := map[int]struct{}{}
	for sfx, vlan := range vlanBy {
		parts := strings.Split(sfx, ".")
		bpIndex := 0
		gemIndex := 0
		if len(parts) >= 1 {
			bpIndex, _ = strconv.Atoi(parts[0])
		}
		if len(parts) >= 2 {
			gemIndex, _ = strconv.Atoi(parts[1])
		}
		cfg := ONUServicePortConfig{
			ID:    bpIndex,
			VPort: gemIndex,
			VLAN:  vlan,
			Mode:  "tagged",
		}
		if cos, ok := cosBy[sfx]; ok {
			cfg.CTagCOS = cos
		}
		ports = append(ports, cfg)
		vlanSet[vlan] = struct{}{}
	}
	sort.Slice(ports, func(i, j int) bool {
		if ports[i].ID != ports[j].ID {
			return ports[i].ID < ports[j].ID
		}
		return ports[i].VPort < ports[j].VPort
	})
	vlans := make([]int, 0, len(vlanSet))
	for v := range vlanSet {
		vlans = append(vlans, v)
	}
	sort.Ints(vlans)
	return vlans, ports, nil
}
