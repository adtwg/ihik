package zte

import (
	"strings"

	"github.com/gosnmp/gosnmp"
)

// IfNameTraffic mencoba membaca ifName/ifDescr + ifHCInOctets/ifHCOutOctets
// untuk mendeteksi apakah OLT expose interface per-ONU.
// Return: map ifIndex -> {Name, InOctets, OutOctets} untuk entry yang namanya
// mengandung "onu" atau cocok filter.
func IfNameTraffic(session *gosnmp.GoSNMP) (map[string]TrafficSample, error) {
	names := map[string]string{}
	if err := session.Walk(IfName, func(pdu gosnmp.SnmpPDU) error {
		name := stringValue(pdu)
		if strings.Contains(strings.ToLower(name), "onu") {
			idx := strings.TrimPrefix(pdu.Name, "."+IfName+".")
			names[idx] = name
		}
		return nil
	}); err != nil {
		return nil, err
	}
	if len(names) == 0 {
		return map[string]TrafficSample{}, nil
	}
	out := make(map[string]TrafficSample, len(names))
	for idx := range names {
		out[idx] = TrafficSample{Name: names[idx]}
	}
	// baca counter 64-bit
	trafficSampleWalk(session, TrafficHCIn, func(idx string, v uint64) {
		s := out[idx]
		s.InOctets = v
		out[idx] = s
	})
	trafficSampleWalk(session, TrafficHCOut, func(idx string, v uint64) {
		s := out[idx]
		s.OutOctets = v
		out[idx] = s
	})
	return out, nil
}

func trafficSampleWalk(session *gosnmp.GoSNMP, oid string, cb func(idx string, v uint64)) {
	_ = session.Walk(oid, func(pdu gosnmp.SnmpPDU) error {
		idx := strings.TrimPrefix(pdu.Name, "."+oid+".")
		if idx == pdu.Name {
			idx = strings.TrimPrefix(pdu.Name, oid+".")
		}
		switch pdu.Type {
		case gosnmp.Counter64, gosnmp.Counter32:
			cb(idx, gosnmp.ToBigInt(pdu.Value).Uint64())
		}
		return nil
	})
}

func stringValue(pdu gosnmp.SnmpPDU) string {
	switch v := pdu.Value.(type) {
	case string:
		return v
	case []byte:
		return string(v)
	default:
		return ""
	}
}

// TrafficSampleName menambahkan nama untuk debugging.
type TrafficSampleNamed struct {
	TrafficSample
	Name string
}

func IfNameTrafficNamed(session *gosnmp.GoSNMP) (map[string]TrafficSampleNamed, error) {
	raw, err := IfNameTraffic(session)
	if err != nil {
		return nil, err
	}
	out := make(map[string]TrafficSampleNamed, len(raw))
	for idx, s := range raw {
		out[idx] = TrafficSampleNamed{TrafficSample: s, Name: s.Name}
	}
	return out, nil
}
