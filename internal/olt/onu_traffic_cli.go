package olt

import (
	"context"
	"fmt"
	"log"
	"regexp"
	"strconv"
	"strings"
	"time"

	"isp-billing/internal/ztecli"
)

// ONUTrafficSample hasil parsing CLI trafik per ONU (oktet).
type ONUTrafficSample struct {
	PONPort   string `json:"pon_port"`
	ONUID     int    `json:"onu_id"`
	InOctets  uint64 `json:"in_octets"`
	OutOctets uint64 `json:"out_octets"`
	Raw       string `json:"raw,omitempty"`
	Command   string `json:"command,omitempty"`
	Method    string `json:"method,omitempty"`
}

var (
	trafficByteRegex      = regexp.MustCompile(`(?:^|[^A-Za-z0-9])([0-9][0-9,._]*)(?:$|[^A-Za-z0-9])`)
	rxTxPairRegex         = regexp.MustCompile(`(?i)(?:rx|receive|incoming|downstream|ingress|in\b)[^0-9]{0,24}([0-9][0-9,._]*)[^0-9]{1,24}(?:tx|transmit|outgoing|upstream|egress|out\b)[^0-9]{0,24}([0-9][0-9,._]*)`)
	txRxPairRegex         = regexp.MustCompile(`(?i)(?:tx|transmit|outgoing|upstream|egress|out\b)[^0-9]{0,24}([0-9][0-9,._]*)[^0-9]{1,24}(?:rx|receive|incoming|downstream|ingress|in\b)[^0-9]{0,24}([0-9][0-9,._]*)`)
	ifaceInputBytesRegex  = regexp.MustCompile(`(?i)input\s*:\s*bytes\s*:?\s*([0-9][0-9,._]*)`)
	ifaceOutputBytesRegex = regexp.MustCompile(`(?i)output\s*:\s*bytes\s*:?\s*([0-9][0-9,._]*)`)
	spacesRegex           = regexp.MustCompile(`\s+`)
)

// ProbeONUTrafficCLI menjalankan beberapa command kandidat untuk membaca
// trafik per-ONU lewat SSH/Telnet CLI. Mengembalikan hasil pertama yang
// berhasil diparse, plus map raw output semua command untuk diagnosis.
func (service *Service) ProbeONUTrafficCLI(ctx context.Context, tenantID, id, ponPort string, onuID int) (*ONUTrafficSample, map[string]string, error) {
	return service.probeONUTraffic(ctx, tenantID, id, ponPort, onuID, true)
}

func (service *Service) ProbeONUTrafficSNMP(ctx context.Context, tenantID, id, ponPort string, onuID int) (*ONUTrafficSample, map[string]string, error) {
	return service.probeONUTraffic(ctx, tenantID, id, ponPort, onuID, false)
}

func (service *Service) probeONUTraffic(ctx context.Context, tenantID, id, ponPort string, onuID int, allowCLI bool) (*ONUTrafficSample, map[string]string, error) {
	ponPort = sanitizePON(ponPort)
	if ponPort == "" || onuID < 1 || onuID > 128 {
		return nil, nil, ErrInvalidInput
	}

	raws := map[string]string{"method": "snmp"}

	// SNMP fast path: baca counter per-ONU tanpa sesi CLI serial.
	snmpCtx, cancelSNMP := context.WithTimeout(ctx, 6*time.Second)
	snmpSample, snmpErr := service.fetchONUTrafficSNMP(snmpCtx, tenantID, id, ponPort, onuID)
	cancelSNMP()
	if snmpErr == nil && snmpSample != nil {
		snmpSample.PONPort = ponPort
		snmpSample.ONUID = onuID
		snmpSample.Method = "snmp"
		return snmpSample, raws, nil
	}
	if snmpErr == nil {
		snmpErr = fmt.Errorf("%w: counter SNMP kosong", ErrUnreachable)
	}
	raws["snmp_error"] = snmpErr.Error()
	if !allowCLI || ctx.Err() != nil {
		return nil, raws, snmpErr
	}

	log.Printf("ProbeONUTrafficCLI SNMP failed for %s:%d, fallback CLI: %v", ponPort, onuID, snmpErr)

	// Fallback CLI legacy.
	candidates := []string{
		fmt.Sprintf("show interface gpon-onu_%s:%d", ponPort, onuID),
		fmt.Sprintf("show gpon onu statistics interface gpon-onu_%s:%d", ponPort, onuID),
		fmt.Sprintf("show pon onu information gpon-onu_%s:%d", ponPort, onuID),
		fmt.Sprintf("show pon onu information gpon-olt_%s %d", ponPort, onuID),
		fmt.Sprintf("show gpon onu detail-info gpon-onu_%s:%d", ponPort, onuID),
	}

	var firstErr error

	output, err := service.withCLISession(ctx, tenantID, id, func(session *ztecli.Session) (string, error) {
		for _, cmd := range candidates {
			out, execErr := session.Execute(ctx, cmd)
			if execErr != nil {
				if firstErr == nil {
					firstErr = execErr
				}
				continue
			}
			raws[cmd] = out
			sample := parseONUTrafficCLI(out)
			if sample != nil && (sample.InOctets > 0 || sample.OutOctets > 0) {
				sample.PONPort = ponPort
				sample.ONUID = onuID
				sample.Raw = out
				sample.Command = cmd
				return out, nil
			}
		}
		return "", fmt.Errorf("tidak ada command yang menghasilkan counter byte")
	})
	if err != nil {
		if firstErr != nil {
			return nil, raws, firstErr
		}
		return nil, raws, err
	}

	// re-parse output yang berhasil
	sample := parseONUTrafficCLI(output)
	if sample == nil {
		sample = &ONUTrafficSample{}
	}
	sample.PONPort = ponPort
	sample.ONUID = onuID
	sample.Raw = output
	sample.Method = "cli"
	return sample, raws, nil
}

// parseONUTrafficCLI parsing output trafik CLI per-ONU.
// Referensi utama BILLING-FIX-PHP OLT V2:
// `show interface gpon-onu_X/X/X:N` -> section `Total statistic` berisi
// Input/Output Bytes kumulatif yang aman untuk delta-rate di frontend.
func parseONUTrafficCLI(output string) *ONUTrafficSample {
	flat := spacesRegex.ReplaceAllString(strings.ReplaceAll(output, "\r", " "), " ")
	flat = strings.TrimSpace(flat)
	if flat == "" {
		return nil
	}

	// 1) Prioritas: parse counter kumulatif dari show interface.
	// ZTE C320 tipikal:
	//   Input:  Bytes:113304670082546  Packets:...
	//   Output: Bytes:1221845814614955 Packets:...
	// Mapping AWGRevBILL UI:
	//   in_octets  = downstream (Output bytes)
	//   out_octets = upstream   (Input bytes)
	inUp := uint64(0)
	outDown := uint64(0)
	if m := ifaceInputBytesRegex.FindStringSubmatch(flat); len(m) == 2 {
		inUp = parseTrafficBytes(m[1])
	}
	if m := ifaceOutputBytesRegex.FindStringSubmatch(flat); len(m) == 2 {
		outDown = parseTrafficBytes(m[1])
	}
	if outDown > 0 || inUp > 0 {
		return &ONUTrafficSample{InOctets: outDown, OutOctets: inUp}
	}

	// 2) Fallback generik untuk firmware lain.
	var inOctets, outOctets uint64
	lowAll := strings.ToLower(flat)
	pairHit := false
	if m := rxTxPairRegex.FindStringSubmatch(lowAll); len(m) == 3 {
		inOctets = parseTrafficBytes(m[1])
		outOctets = parseTrafficBytes(m[2])
		pairHit = inOctets > 0 || outOctets > 0
	}
	if !pairHit {
		if m := txRxPairRegex.FindStringSubmatch(lowAll); len(m) == 3 {
			outOctets = parseTrafficBytes(m[1])
			inOctets = parseTrafficBytes(m[2])
			pairHit = inOctets > 0 || outOctets > 0
		}
	}
	for _, line := range strings.Split(output, "\n") {
		if pairHit {
			break
		}
		line = strings.ToLower(strings.TrimSpace(line))
		if line == "" {
			continue
		}
		if strings.Contains(line, "invalid command") || strings.Contains(line, "invalid parameter") || strings.Contains(line, "%error") {
			continue
		}
		if !strings.Contains(line, "byte") && !strings.Contains(line, "octet") && !strings.Contains(line, "bps") && !strings.Contains(line, "bit/s") && !strings.Contains(line, "rate") {
			continue
		}
		if strings.Contains(line, "profile") || strings.Contains(line, "gem port") || strings.Contains(line, "t-cont") || strings.Contains(line, "port id") || strings.Contains(line, "queue") || strings.Contains(line, "status") {
			continue
		}
		m := trafficByteRegex.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		v := parseTrafficBytes(m[1])
		if v == 0 {
			continue
		}
		// in_octets=unduh(downstream/output), out_octets=unggah(upstream/input)
		if strings.Contains(line, "downstream") || strings.Contains(line, "output") || strings.Contains(line, "rx") || strings.Contains(line, "receive") || strings.Contains(line, "incoming") || strings.Contains(line, "ingress") {
			inOctets += v
			continue
		}
		if strings.Contains(line, "upstream") || strings.Contains(line, "input") || strings.Contains(line, "tx") || strings.Contains(line, "transmit") || strings.Contains(line, "outgoing") || strings.Contains(line, "egress") || strings.Contains(line, "sent") {
			outOctets += v
		}
	}
	if inOctets == 0 && outOctets == 0 {
		return nil
	}
	return &ONUTrafficSample{InOctets: inOctets, OutOctets: outOctets}
}

func parseTrafficBytes(s string) uint64 {
	s = strings.ReplaceAll(s, ",", "")
	s = strings.ReplaceAll(s, ".", "")
	s = strings.ReplaceAll(s, "_", "")
	s = strings.ReplaceAll(s, " ", "")
	n, _ := strconv.ParseUint(s, 10, 64)
	return n
}
