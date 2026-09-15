package zte

import (
	"errors"
	"testing"

	"github.com/gosnmp/gosnmp"
)

func TestSampleTrafficONU(t *testing.T) {
	for _, scenario := range []string{"64-bit zero", "unsupported", "partial", "wrong OID", "packet error", "nil packet", "transport error", "no counters"} {
		t.Run(scenario, func(t *testing.T) {
			calls := 0
			get := func(oids []string) (*gosnmp.SnmpPacket, error) {
				calls++
				packet := &gosnmp.SnmpPacket{Variables: []gosnmp.SnmpPDU{
					{Name: "." + oids[1], Type: gosnmp.Counter64, Value: uint64(0)},
					{Name: oids[0], Type: gosnmp.Counter64, Value: uint64(0)},
				}}
				if calls == 2 {
					for index := range packet.Variables {
						packet.Variables[index].Type = gosnmp.Counter32
						packet.Variables[index].Value = uint32(42)
					}
					if scenario != "no counters" {
						return packet, nil
					}
				}
				switch scenario {
				case "unsupported", "no counters":
					for index := range packet.Variables {
						packet.Variables[index].Type = gosnmp.NoSuchInstance
						packet.Variables[index].Value = nil
					}
				case "partial":
					packet.Variables = packet.Variables[:1]
				case "wrong OID":
					packet.Variables[0].Name += ".1"
				case "packet error":
					packet.Error = gosnmp.NoSuchName
				case "nil packet":
					return nil, nil
				case "transport error":
					return nil, errors.New("timeout")
				}
				return packet, nil
			}
			sample, err := sampleTrafficONU(get, "123")
			if scenario == "no counters" {
				if err == nil {
					t.Fatal("missing counters must not be accepted as zero")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			wantCalls, wantValue := 2, uint64(42)
			if scenario == "64-bit zero" {
				wantCalls, wantValue = 1, 0
			}
			if calls != wantCalls || sample.InOctets != wantValue || sample.OutOctets != wantValue {
				t.Fatalf("calls=%d sample=%+v, want calls=%d values=%d", calls, sample, wantCalls, wantValue)
			}
		})
	}
}