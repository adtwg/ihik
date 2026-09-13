package olt

import (
	"testing"

	"isp-billing/internal/zte"
)

func TestParseONUConfigDetail(t *testing.T) {
	detail := &ONUConfigDetail{}

	detailInfo := `
Name: Pelanggan-A
Description: ONU Rumah A
Serial number: ZTEGABC12345
ONU type reported: F660
ONU Distance: 3245m
Phase state: working
`
	runningCfg := `
interface gpon-onu_1/1/5:2
 switchport mode hybrid vport 2
 tcont 1 profile DBA-100M
 gemport 1 traffic-limit upstream UP-100M downstream DW-100M
 service-port 1 description INTERNET
 service-port 1 vport 1 user-vlan 102 vlan 102 etype pppoe
 service-port 2 vport 2 user-vlan 201 user-svlan 301 vlan 3101 svlan 3201 cos 4 scos 6
 wan-ip 1 mode pppoe vlan-profile PPPoE ip-profile IPOE auth-mode chap username priyatmo_pucung password priyatmo_pucung host 1
 ZXAN(gpon-onu-mng)#wan-ip 1 ping-response enable traceroute-response disable
 wan-ip 2 mode static vlan-profile STATIC-VLAN ip-profile STATIC-IP ip-address 10.20.30.2/24
!`
	tcontOut := `
T-CONT 1
Profile name: DBA-100M
`
	gemportOut := `
Gemport 1
Down traffic profile name: DW-100M
Up traffic profile name: UP-100M
`
	attenOut := `
OLT                  ONU              Attenuation
--------------------------------------------------------------------------
 up      Rx :-25.100(dbm)      Tx:2.079(dbm)        26.802(dB)
 down    Tx :6.207(dbm)        Rx:-19.320(dbm)      25.527(dB)
`
	ifaceOut := `
Input rate : 1250000 Bps
Output rate : 2500000 Bps
`

	parseDetailInfoCLI(detail, detailInfo)
	parseRunningConfigCLI(detail, runningCfg)
	parseTcontProfilesCLI(detail, tcontOut)
	parseGemportProfilesCLI(detail, gemportOut)
	parseAttenuationSidesCLI(detail, attenOut)
	parseInterfaceRatesCLI(detail, ifaceOut)

	detail.VLANs = uniqueSortedInts(detail.VLANs)
	detail.DBAProfiles = uniqueSortedStrings(detail.DBAProfiles)
	detail.UpstreamProfiles = uniqueSortedStrings(detail.UpstreamProfiles)
	detail.DownstreamProfiles = uniqueSortedStrings(detail.DownstreamProfiles)

	if detail.Name != "Pelanggan-A" {
		t.Fatalf("unexpected name: %q", detail.Name)
	}
	if detail.Status != "working" {
		t.Fatalf("unexpected status: %q", detail.Status)
	}
	if detail.SerialNumber != "ZTEGABC12345" {
		t.Fatalf("unexpected serial: %q", detail.SerialNumber)
	}
	if detail.DistanceM != 3245 {
		t.Fatalf("unexpected distance: %v", detail.DistanceM)
	}
	if len(detail.VLANs) != 5 {
		t.Fatalf("expected 5 vlan values, got %v", detail.VLANs)
	}
	if detail.VLANs[0] != 102 || detail.VLANs[4] != 3201 {
		t.Fatalf("unexpected vlan list: %v", detail.VLANs)
	}
	if len(detail.DBAProfiles) != 1 || detail.DBAProfiles[0] != "DBA-100M" {
		t.Fatalf("unexpected dba profiles: %v", detail.DBAProfiles)
	}
	if len(detail.UpstreamProfiles) != 1 || detail.UpstreamProfiles[0] != "UP-100M" {
		t.Fatalf("unexpected upstream profiles: %v", detail.UpstreamProfiles)
	}
	if len(detail.DownstreamProfiles) != 1 || detail.DownstreamProfiles[0] != "DW-100M" {
		t.Fatalf("unexpected downstream profiles: %v", detail.DownstreamProfiles)
	}
	if detail.UpstreamBps != 1250000 {
		t.Fatalf("unexpected upstream bps: %v", detail.UpstreamBps)
	}
	if detail.DownstreamBps != 2500000 {
		t.Fatalf("unexpected downstream bps: %v", detail.DownstreamBps)
	}
	if detail.RxOLTSideDBM != -25.1 {
		t.Fatalf("unexpected rx olt side: %v", detail.RxOLTSideDBM)
	}
	if detail.RxONUSideDBM != -19.32 {
		t.Fatalf("unexpected rx onu side: %v", detail.RxONUSideDBM)
	}
	if len(detail.ServicePorts) != 2 {
		t.Fatalf("expected 2 service ports, got %d", len(detail.ServicePorts))
	}
	if detail.ServicePorts[0].VPort != 1 || detail.ServicePorts[1].VPort != 2 {
		t.Fatalf("unexpected vport values: %+v", detail.ServicePorts)
	}
	if detail.ServicePorts[0].Description != "INTERNET" {
		t.Fatalf("unexpected service-port 1 description: %+v", detail.ServicePorts[0])
	}
	if detail.ServicePorts[0].EtherType != "PPPoE" {
		t.Fatalf("unexpected service-port 1 ether type: %+v", detail.ServicePorts[0])
	}
	if detail.ServicePorts[1].Mode != "Hybrid" {
		t.Fatalf("unexpected service-port 2 mode: %+v", detail.ServicePorts[1])
	}
	if detail.ServicePorts[1].UserSVLAN != 301 || detail.ServicePorts[1].SVLAN != 3201 {
		t.Fatalf("unexpected service-port 2 s-vid mapping: %+v", detail.ServicePorts[1])
	}
	if detail.ServicePorts[1].CTagCOS != 4 || detail.ServicePorts[1].STagCOS != 6 {
		t.Fatalf("unexpected service-port 2 cos mapping: %+v", detail.ServicePorts[1])
	}
	if len(detail.WANIPs) != 2 {
		t.Fatalf("expected 2 wan-ip rows, got %d", len(detail.WANIPs))
	}
	if detail.WANIPs[0].Mode != "PPPoE" || detail.WANIPs[0].VLANProfile != "PPPoE" {
		t.Fatalf("unexpected wan-ip 1 parsing: %+v", detail.WANIPs[0])
	}
	if detail.WANIPs[0].AuthMode != "CHAP" {
		t.Fatalf("unexpected wan-ip 1 auth mode: %+v", detail.WANIPs[0])
	}
	if detail.WANIPs[0].PPPoEUsername != "priyatmo_pucung" || detail.WANIPs[0].PPPoEPassword != "priyatmo_pucung" {
		t.Fatalf("unexpected wan-ip 1 PPPoE parsing: %+v", detail.WANIPs[0])
	}
	if detail.WANIPs[0].RespondPing == nil || !*detail.WANIPs[0].RespondPing {
		t.Fatalf("unexpected wan-ip 1 ping-response parsing: %+v", detail.WANIPs[0])
	}
	if detail.WANIPs[0].RespondTraceroute == nil || *detail.WANIPs[0].RespondTraceroute {
		t.Fatalf("unexpected wan-ip 1 traceroute-response parsing: %+v", detail.WANIPs[0])
	}
	if detail.WANIPs[1].Mode != "Static" || detail.WANIPs[1].StaticIP != "10.20.30.2/24" {
		t.Fatalf("unexpected wan-ip 2 static parsing: %+v", detail.WANIPs[1])
	}
}

func TestParseRunningConfigCLI_WANIPsWithoutServicePort(t *testing.T) {
	detail := &ONUConfigDetail{}
	runningCfg := `
pon-onu-mng gpon-onu_1/1/1:3
 service INTERNET gemport 1 vlan 100
 ZXAN(gpon-onu-mng)#wan-ip 1 mode pppoe vlan-profile PPPoE auth-mode auto username user1 password pass1 host 1
 ZXAN(gpon-onu-mng)#wan-ip 1 ping-response enable traceroute-response enable
!`

	parseRunningConfigCLI(detail, runningCfg)

	if len(detail.ServicePorts) != 0 {
		t.Fatalf("expected 0 service ports, got %d", len(detail.ServicePorts))
	}
	if len(detail.WANIPs) != 1 {
		t.Fatalf("expected 1 wan-ip row, got %d", len(detail.WANIPs))
	}
	row := detail.WANIPs[0]
	if row.ID != 1 || row.Mode != "PPPoE" || row.VLANProfile != "PPPoE" {
		t.Fatalf("unexpected wan row core mapping: %+v", row)
	}
	if row.PPPoEUsername != "user1" || row.PPPoEPassword != "pass1" {
		t.Fatalf("unexpected wan row credentials parsing: %+v", row)
	}
	if row.AuthMode != "Auto" {
		t.Fatalf("unexpected auth mode: %+v", row)
	}
	if row.RespondPing == nil || !*row.RespondPing {
		t.Fatalf("unexpected ping-response parsing: %+v", row)
	}
	if row.RespondTraceroute == nil || !*row.RespondTraceroute {
		t.Fatalf("unexpected traceroute-response parsing: %+v", row)
	}
}

func TestDeriveONUProvisionState(t *testing.T) {
	t.Run("unconfigured", func(t *testing.T) {
		state := deriveONUProvisionState(&ONUConfigDetail{})
		if state.Status != "unconfigured" {
			t.Fatalf("expected unconfigured, got %q", state.Status)
		}
		if state.Access != "unknown" {
			t.Fatalf("expected unknown access, got %q", state.Access)
		}
	})

	t.Run("bridge configured", func(t *testing.T) {
		state := deriveONUProvisionState(&ONUConfigDetail{
			Tconts:       []ONUTcontConfig{{ID: 1}},
			Gemports:     []ONUGemportConfig{{ID: 1}},
			ServicePorts: []ONUServicePortConfig{{ID: 1, VPort: 1, UserVLAN: 100, VLAN: 100}},
		})
		if state.Status != "configured" {
			t.Fatalf("expected configured, got %q", state.Status)
		}
		if state.Access != "bridge" {
			t.Fatalf("expected bridge access, got %q", state.Access)
		}
	})

	t.Run("pppoe partial", func(t *testing.T) {
		state := deriveONUProvisionState(&ONUConfigDetail{
			ServicePorts: []ONUServicePortConfig{{ID: 1}},
			WANIPs:       []ONUWANIPConfig{{ID: 1, Mode: "PPPoE"}},
		})
		if state.Status != "partial" {
			t.Fatalf("expected partial, got %q", state.Status)
		}
		if state.Access != "pppoe" {
			t.Fatalf("expected pppoe access, got %q", state.Access)
		}
	})
}

func TestParseUncfgONUs_BulkHintsAndPort(t *testing.T) {
	output := `
1   ZTEGCABCDEF1234   online
onu-id 7 sn ZTEGC0000AAAABBBB state online gpon-olt_1/1/3
2   ZTEGCABCDEF1234   online
`
	rows := parseUncfgONUs(output, "1/1/1")
	if len(rows) != 3 {
		t.Fatalf("expected 3 rows before dedupe, got %d", len(rows))
	}
	if rows[0].SerialNumber != "ZTEGCABCDEF1234" || rows[0].PONPort != "1/1/1" || rows[0].ONUIDHint != 1 {
		t.Fatalf("unexpected row0: %+v", rows[0])
	}
	if rows[1].PONPort != "1/1/3" || rows[1].ONUIDHint != 7 {
		t.Fatalf("unexpected row1: %+v", rows[1])
	}

	deduped := dedupeUnconfiguredONUs(rows)
	if len(deduped) != 2 {
		t.Fatalf("expected 2 rows after dedupe, got %d", len(deduped))
	}
}

func TestBuildONUPortOccupancyAndEmptySlots(t *testing.T) {
	onus := []zte.ONU{
		{ONUNumber: "1/1/1:1"},
		{ONUNumber: "1/1/1:3"},
		{Index: "285278467.2"},
	}
	occupancy := buildONUPortOccupancy(onus)
	if len(occupancy) != 2 {
		t.Fatalf("expected occupancy for 2 ports, got %d", len(occupancy))
	}
	used111 := sortedONUIDs(occupancy["1/1/1"])
	if len(used111) != 2 || used111[0] != 1 || used111[1] != 3 {
		t.Fatalf("unexpected used IDs for 1/1/1: %v", used111)
	}
	empty111 := findEmptyONUIDs(occupancy["1/1/1"], 5)
	if len(empty111) != 3 || empty111[0] != 2 || empty111[1] != 4 || empty111[2] != 5 {
		t.Fatalf("unexpected empty IDs for 1/1/1: %v", empty111)
	}

	suggested, rest := pickSuggestedONUID(4, empty111)
	if suggested != 4 {
		t.Fatalf("expected hint 4 to be selected, got %d", suggested)
	}
	if len(rest) != 2 || rest[0] != 2 || rest[1] != 5 {
		t.Fatalf("unexpected queue after hint selection: %v", rest)
	}
}
