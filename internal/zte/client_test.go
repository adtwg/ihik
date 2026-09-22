package zte

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/gosnmp/gosnmp"
)

func TestParseSystemResponse(t *testing.T) {
	valid := func() *gosnmp.SnmpPacket {
		return &gosnmp.SnmpPacket{Variables: []gosnmp.SnmpPDU{
			{Name: "." + oidSysUpTime, Type: gosnmp.TimeTicks, Value: uint32(0)},
			{Name: oidSysDescr, Type: gosnmp.OctetString, Value: []byte("ZTE ZXA10 C300 V2.1.0")},
		}}
	}
	for _, scenario := range []string{"C300", "C320", "unknown model", "empty", "nil", "denied", "unsupported", "partial", "wrong OIDs", "wrong types", "blank description", "invalid value"} {
		t.Run(scenario, func(t *testing.T) {
			packet := valid()
			switch scenario {
			case "C320":
				packet.Variables[1].Value = []byte("ZTE C320")
			case "unknown model":
				packet.Variables[1].Value = []byte("SNMP agent")
			case "empty":
				packet.Variables = nil
			case "nil":
				packet = nil
			case "denied":
				packet.Error = gosnmp.AuthorizationError
			case "unsupported":
				for index := range packet.Variables {
					packet.Variables[index].Type = gosnmp.NoSuchObject
					packet.Variables[index].Value = nil
				}
			case "partial":
				packet.Variables = packet.Variables[:1]
			case "wrong OIDs":
				for index := range packet.Variables {
					packet.Variables[index].Name += ".99"
				}
			case "wrong types":
				packet.Variables[0].Type = gosnmp.Counter32
			case "blank description":
				packet.Variables[1].Value = []byte("  ")
			case "invalid value":
				packet.Variables[1].Value = 123
			}
			info, err := parseSystemResponse(packet)
			wantSuccess := scenario == "C300" || scenario == "C320" || scenario == "unknown model"
			if (err == nil) != wantSuccess {
				t.Fatalf("info=%+v error=%v, wantSuccess=%v", info, err, wantSuccess)
			}
			if wantSuccess {
				if info.SysUpTime == "" || (scenario != "unknown model" && !strings.Contains(info.ModelHint, scenario)) {
					t.Fatalf("invalid system info: %+v", info)
				}
				if scenario == "unknown model" && info.ModelHint != "" {
					t.Fatalf("invented model: %s", info.ModelHint)
				}
			}
		})
	}
}

func TestWalkONUsConfigurationInventory(t *testing.T) {
	for _, profileName := range []string{"v2.1", "v2.2"} {
		for _, scenario := range []string{"serial only", "hex serial only", "status only", "named ONU", "unsupported tables"} {
			t.Run(profileName+"/"+scenario, func(t *testing.T) {
				profile := FirmwareProfiles[profileName]
				if scenario == "hex serial only" && profile.ONUSerialHex == "" {
					t.Skip("profile has no hex serial column")
				}
				walk := func(oid string, visit gosnmp.WalkFunc) error {
					value := ""
					switch oid {
					case profile.BaseOID + profile.ONUName:
						if scenario == "named ONU" {
							value = "Customer"
						}
					case profile.BaseOID + profile.ONUSerial:
						if scenario == "serial only" {
							value = "ZTEG12345678"
						}
					case profile.BaseOID + profile.ONUSerialHex:
						if scenario == "hex serial only" {
							value = "5A5445473132333435363738"
						}
					case profile.BaseOID + profile.ONUStatus:
						return visit(gosnmp.SnmpPDU{Name: oid + ".285278465.99", Type: gosnmp.Integer, Value: 4})
					}
					if scenario == "unsupported tables" {
						return visit(gosnmp.SnmpPDU{Name: oid + ".285278465.1", Type: gosnmp.NoSuchInstance})
					}
					if value == "" {
						return nil
					}
					return visit(gosnmp.SnmpPDU{Name: "." + oid + ".285278465.1", Type: gosnmp.OctetString, Value: []byte(value)})
				}
				onus, err := walkONUs(context.Background(), walk, profile)
				if err != nil {
					t.Fatal(err)
				}
				if scenario == "status only" || scenario == "unsupported tables" {
					if len(onus) != 0 {
						t.Fatalf("invented ONU from non-configuration table: %+v", onus)
					}
					return
				}
				if len(onus) != 1 || onus[0].Index != "285278465.1" {
					t.Fatalf("expected configured ONU, got %+v", onus)
				}
				if scenario != "named ONU" && onus[0].SerialNumber != "ZTEG12345678" {
					t.Fatalf("unexpected serial: %q", onus[0].SerialNumber)
				}
			})
		}
	}
}

func TestWalkONUsEmptyInventoryStopsBeforeOptical(t *testing.T) {
	profile := FirmwareProfiles["v2.2"]
	for _, failSerial := range []bool{false, true} {
		calls := 0
		_, err := walkONUs(context.Background(), func(oid string, visit gosnmp.WalkFunc) error {
			calls++
			if failSerial && oid == profile.BaseOID+profile.ONUSerial {
				return errors.New("serial timeout")
			}
			return nil
		}, profile)
		if calls != 3 {
			t.Fatalf("empty inventory walked optional tables: %d calls", calls)
		}
		if (err != nil) != failSerial {
			t.Fatalf("serial error lost: failSerial=%v err=%v", failSerial, err)
		}
	}
}

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
