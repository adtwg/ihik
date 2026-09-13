package zte

import (
	"strconv"
	"strings"

	"github.com/gosnmp/gosnmp"
)

const onuPrivateCounterBase = "1.3.6.1.4.1.3902.1082.500.10.2.3.2.2.1"

// WalkAllPrivateCounters membaca seluruh subtree counter privat ZTE
// per-ONU (.2.2.1.*). Output: index ONU -> nomor kolom -> nilai counter.
// Kolom yang diamati produksi:
//   1 = byte DOWNSTREAM per-ONU (delta besar)
//   2 = byte UPSTREAM per-ONU (terbukti delta per-ONU, sebanding hc_in port)
//   3..N = paket/error/lookup counter (lebih kecil)
func WalkAllPrivateCounters(session *gosnmp.GoSNMP) (map[string]map[int]uint64, error) {
	out := map[string]map[int]uint64{}
	err := session.Walk(onuPrivateCounterBase, func(pdu gosnmp.SnmpPDU) error {
		suffix := strings.TrimPrefix(pdu.Name, "."+onuPrivateCounterBase+".")
		// suffix = <gponOnuIndex>.<onuId>.<column>
		parts := strings.Split(suffix, ".")
		if len(parts) < 2 {
			return nil
		}
		colStr := parts[len(parts)-1]
		col, err := strconv.Atoi(colStr)
		if err != nil {
			return nil
		}
		idx := strings.Join(parts[:len(parts)-1], ".")

		var v uint64
		switch pdu.Type {
		case gosnmp.Counter64, gosnmp.Counter32, gosnmp.Gauge32,
			gosnmp.Uinteger32, gosnmp.Integer:
			n := gosnmp.ToBigInt(pdu.Value)
			if n.Sign() >= 0 && n.IsUint64() {
				v = n.Uint64()
			} else {
				return nil
			}
		default:
			return nil
		}
		m, ok := out[idx]
		if !ok {
			m = map[int]uint64{}
			out[idx] = m
		}
		m[col] = v
		return nil
	})
	return out, err
}

// SampleAllPrivateONUCounters melakukan satu walk subtree privat ZTE
// dan mengembalikan TrafficSample (col 1=in, col 2=out) untuk semua ONU.
// Ini menggantikan SampleTrafficPrivateONU karena tabel privat tidak
// melayani GET per OID (hanya WALK).
func SampleAllPrivateONUCounters(session *gosnmp.GoSNMP, inCol, outCol int) (map[string]TrafficSample, error) {
	all, err := WalkAllPrivateCounters(session)
	if err != nil {
		return nil, err
	}
	out := make(map[string]TrafficSample, len(all))
	for idx, cols := range all {
		out[idx] = TrafficSample{
			InOctets:  cols[inCol],
			OutOctets: cols[outCol],
		}
	}
	return out, nil
}
