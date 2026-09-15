package zte

import (
	"fmt"
	"strconv"
	"strings"
)

// OltHealth adalah snapshot kesehatan fisik OLT (card, CPU, suhu, SFP).
// Semua nilai opsional — firmware lama mungkin tidak expose semuanya.
type OltHealth struct {
	Cards []CardInfo `json:"cards"`
	Fans  []FanInfo  `json:"fans,omitempty"`
	Psus  []PsuInfo  `json:"psus,omitempty"`
	Sfps  []SfpInfo  `json:"sfps,omitempty"`
}

type CardInfo struct {
	Slot     int     `json:"slot"`
	Type     string  `json:"type"`            // GTGO, GTGH, SCXM, dll
	Status   string  `json:"status"`          // InService/Standby/Offline
	Role     string  `json:"role,omitempty"`  // active/standby
	CPUPerc  float64 `json:"cpu_percent"`
	MemPerc  float64 `json:"mem_percent"`
	TempC    float64 `json:"temp_c"`
}

type FanInfo struct {
	ID    int    `json:"id"`
	Speed int    `json:"speed_rpm"`
}

type PsuInfo struct {
	ID      int     `json:"id"`
	Voltage float64 `json:"voltage"`
}

type SfpInfo struct {
	Index      int     `json:"index"`      // diagIndex mentah
	Label      string  `json:"label,omitempty"` // decoded "shelf/slot/port"
	RxPowerDBM float64 `json:"rx_power_dbm"`
	TxPowerDBM float64 `json:"tx_power_dbm"`
	BiasMA     float64 `json:"bias_ma"`
	VoltageV   float64 `json:"voltage_v"`
	TempC      float64 `json:"temp_c"`
	Model      string  `json:"model,omitempty"`
	Vendor     string  `json:"vendor,omitempty"`
	Wavelength int     `json:"wavelength_nm,omitempty"`
	// Trafik agregat port (ifTable per-PON; TERBUKTI firmware V2.1.0:
	// ifIndex = 285278465+globalPort, counter 64-bit hidup).
	InBps  float64 `json:"in_bps,omitempty"`
	OutBps float64 `json:"out_bps,omitempty"`
	// Raw counter (untuk hitung delta antar-snapshot di service).
	InOctets  uint64 `json:"-"`
	OutOctets uint64 `json:"-"`
}

// OID health — tree .3902.1082.10 (firmware v2.2+). Untuk v2.1 sebagian
// OID tidak tersedia; collector mengembalikan nol dan UI menampilkan "—".
const (
	prefix1082 = ".1.3.6.1.4.1.3902.1082.10"
	prefix1015 = ".1.3.6.1.4.1.3902.1015"

	oidCardType   = prefix1082 + ".1.2.4.1.4" // + .slot.idx? ZTE memakai index ganda; kita walk lalu parse suffix
	oidCardStatus = prefix1082 + ".1.2.4.1.5"
	oidCardCPU    = prefix1082 + ".1.2.4.1.9"
	oidCardMem    = prefix1082 + ".1.2.4.1.11"
	oidCardRole   = prefix1082 + ".1.2.4.1.13"
	oidCardTemp   = prefix1082 + ".10.2.1.6.1.2"
	oidFanSpeed   = prefix1082 + ".10.2.1.6.1.5"
	oidPsuVolt    = prefix1082 + ".10.2.3.11.1.2"

	sfpBase = prefix1015 + ".3.1.13.1"
	oidSfpRx   = sfpBase + ".1"
	oidSfpTx   = sfpBase + ".4"
	oidSfpBias = sfpBase + ".9"
	oidSfpVolt = sfpBase + ".10"
	oidSfpTemp = sfpBase + ".12"
	oidSfpModel  = sfpBase + ".13"
	oidSfpVendor = sfpBase + ".14"
	oidSfpWave   = sfpBase + ".11"

	// ifTable counter agregat per PON port (64-bit).
	oidIfHCIn  = ".1.3.6.1.2.1.31.1.1.1.6"
	oidIfHCOut = ".1.3.6.1.2.1.31.1.1.1.10"
	// Basis ifIndex PON port pada C320 V2.1.0 (debug-walk 2026-08-30:
	// port global n => ifIndex = ponIfIndexBase + n).
	ponIfIndexBase = 285278465
	// sfpBaseIdx: base diagIndex ZTE untuk decode label PON.
	sfpBaseIdx = 268435456
)
// CollectHealthSFPOnly mengumpulkan hanya data SFP diagnostics.
// Dipakai untuk firmware V2.1.0 di mana OID card/temp/fan tidak tersedia.
func CollectHealthSFPOnly(session HealthWalker) (*OltHealth, error) {
	health := &OltHealth{Cards: make([]CardInfo, 0), Fans: make([]FanInfo, 0),
		Psus: make([]PsuInfo, 0), Sfps: make([]SfpInfo, 0)}

	sfpRx := walkMap(session, oidSfpRx)
	sfpTx := walkMap(session, oidSfpTx)
	sfpBias := walkMap(session, oidSfpBias)
	sfpVoltM := walkMap(session, oidSfpVolt)
	sfpTempM := walkMap(session, oidSfpTemp)
	sfpModel := walkMap(session, oidSfpModel)
	sfpVendor := walkMap(session, oidSfpVendor)
	sfpWave := walkMap(session, oidSfpWave)
	hcIn := walkMap(session, oidIfHCIn)
	hcOut := walkMap(session, oidIfHCOut)

	sfpIdx := unionKeys(sfpRx, sfpTx, sfpModel)
	for _, key := range sfpIdx {
		diag := atoiSafe(lastOIDSegment(key))
		info := SfpInfo{Index: diag}
		if rel := diag - sfpBaseIdx; rel >= 0 {
			slot := (rel >> 16) + 1
			port := (rel >> 8) & 0xFF
			if port == 0 {
				port = 1
			}
			info.Label = fmt.Sprintf("%d/%d/%d", 1, slot+1, port+1)
		}
		if v := centiToFloat(sfpRx[key]); v > -100 && v < 100 {
			info.RxPowerDBM = v
		}
		tx := centiToFloat(sfpTx[key])
		if tx > -100 && tx < 100 {
			info.TxPowerDBM = tx
		} else if tx >= 100 && tx < 2000 {
			info.TxPowerDBM = tx / 10
		}
		info.BiasMA = divide100(sfpBias[key])
		info.VoltageV = divide1000(sfpVoltM[key])
		info.TempC = celsiusValue(sfpTempM[key]) / 10
		info.Model = sfpModel[key]
		info.Vendor = sfpVendor[key]
		info.Wavelength = atoiSafe(sfpWave[key])
		if rel := diag - sfpBaseIdx; rel >= 0 {
			port := (rel >> 8) & 0xFF
			ifIdx := strconv.Itoa(ponIfIndexBase + port)
			info.InOctets = uint64Safe(hcIn[ifIdx])
			info.OutOctets = uint64Safe(hcOut[ifIdx])
		}
		health.Sfps = append(health.Sfps, info)
	}
	return health, nil
}

// CollectHealth mengumpulkan card/CPU/mem/temp/fan/PSU/SFP dalam satu panggilan.
// Walk paralel per subtree agar cepat; firmware yang tidak mendukung subtree
// tertentu akan menghasilkan walk kosong (bukan error).
func CollectHealth(session HealthWalker) (*OltHealth, error) {
	health := &OltHealth{Cards: make([]CardInfo, 0), Fans: make([]FanInfo, 0),
		Psus: make([]PsuInfo, 0), Sfps: make([]SfpInfo, 0)}

	cardType := walkMap(session, oidCardType)
	cardStatus := walkMap(session, oidCardStatus)
	cardCPU := walkMap(session, oidCardCPU)
	cardMem := walkMap(session, oidCardMem)
	cardRole := walkMap(session, oidCardRole)
	cardTemp := walkMap(session, oidCardTemp)

	// Gabungkan per slot dari suffix OID terakhir
	slots := unionKeys(cardType, cardCPU, cardMem, cardTemp, cardStatus)
	for _, key := range slots {
		slotNum := lastOIDSegment(key)
		info := CardInfo{Slot: atoiSafe(slotNum)}
		if v, ok := cardType[key]; ok {
			info.Type = v
		}
		if v, ok := cardStatus[key]; ok {
			info.Status = cardStatusText(v)
			if info.Status == "" {
				continue // slot kosong — jangan tampilkan
			}
		}
		if v, ok := cardRole[key]; ok && v != "" {
			info.Role = cardRoleText(v)
		}
		if v, ok := cardCPU[key]; ok {
			info.CPUPerc = percentValue(v)
		}
		if v, ok := cardMem[key]; ok {
			info.MemPerc = percentValue(v)
		}
		if v, ok := cardTemp[key]; ok {
			info.TempC = celsiusValue(v)
		}
		health.Cards = append(health.Cards, info)
	}

	fanSpeed := walkMap(session, oidFanSpeed)
	for _, key := range sortedKeys(fanSpeed) {
		health.Fans = append(health.Fans, FanInfo{
			ID:    atoiSafe(lastOIDSegment(key)),
			Speed: atoiSafe(fanSpeed[key]),
		})
	}

	psuVolt := walkMap(session, oidPsuVolt)
	for _, key := range sortedKeys(psuVolt) {
		v := divide100(psuVolt[key])
		if v < 1 { // tidak terpasang / nilai dummy
			continue
		}
		health.Psus = append(health.Psus, PsuInfo{
			ID:      atoiSafe(lastOIDSegment(key)),
			Voltage: v,
		})
	}

	sfpRx := walkMap(session, oidSfpRx)
	sfpTx := walkMap(session, oidSfpTx)
	sfpBias := walkMap(session, oidSfpBias)
	sfpVoltM := walkMap(session, oidSfpVolt)
	sfpTempM := walkMap(session, oidSfpTemp)
	sfpModel := walkMap(session, oidSfpModel)
	sfpVendor := walkMap(session, oidSfpVendor)
	sfpWave := walkMap(session, oidSfpWave)
	hcIn := walkMap(session, oidIfHCIn)
	hcOut := walkMap(session, oidIfHCOut)

	const sfpBaseIdx = 268435456 // 0x10000000 — base diagIndex ZTE
	sfpIdx := unionKeys(sfpRx, sfpTx, sfpModel)
	for _, key := range sfpIdx {
		diag := atoiSafe(lastOIDSegment(key))
		info := SfpInfo{Index: diag}
		// Decode composite index: rel>>16 = slot, (rel>>8)&0xFF = port.
		if rel := diag - sfpBaseIdx; rel >= 0 {
			slot := (rel >> 16) + 1
			port := (rel >> 8) & 0xFF
			if port == 0 {
				port = 1
			}
			info.Label = fmt.Sprintf("%d/%d/%d", 1, slot+1, port+1)
		}
		if v := centiToFloat(sfpRx[key]); v > -100 && v < 100 {
			info.RxPowerDBM = v
		}
		tx := centiToFloat(sfpTx[key])
		if tx > -100 && tx < 100 {
			info.TxPowerDBM = tx
		} else if tx >= 100 && tx < 2000 {
			info.TxPowerDBM = tx / 10 // varian firmware pakai 0.001 unit
		}
		info.BiasMA = divide100(sfpBias[key])
		info.VoltageV = divide1000(sfpVoltM[key])
		info.TempC = celsiusValue(sfpTempM[key]) / 10 // raw 446 -> 44.6C
		info.Model = sfpModel[key]
		info.Vendor = sfpVendor[key]
		info.Wavelength = atoiSafe(sfpWave[key])
		// ifIndex PON port = basis + nomor port global (0-based).
		if rel := diag - sfpBaseIdx; rel >= 0 {
			port := (rel >> 8) & 0xFF
			ifIdx := strconv.Itoa(ponIfIndexBase + port)
			info.InOctets = uint64Safe(hcIn[ifIdx])
			info.OutOctets = uint64Safe(hcOut[ifIdx])
		}
		health.Sfps = append(health.Sfps, info)
	}
	return health, nil
}

// sentinel menandai nilai tidak tersedia pada firmware ZTE (INT32_MAX/100).
func isSentinel(raw string) bool {
	return raw == "2147483647" || raw == "-2147483648" || raw == ""
}

// HealthWalker abstraksi sesi SNMP untuk testability.
type HealthWalker interface {
	WalkAll(rootOID string) map[string]string // key: suffix setelah rootOID
}

func walkMap(w HealthWalker, oid string) map[string]string {
	if oid == "" || w == nil {
		return nil
	}
	return w.WalkAll(oid)
}

func unionKeys(maps ...map[string]string) []string {
	set := map[string]struct{}{}
	for _, m := range maps {
		for k := range m {
			set[k] = struct{}{}
		}
	}
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sortStrings(out)
	return out
}

func sortStrings(list []string) {
	for i := 1; i < len(list); i++ {
		for j := i; j > 0 && list[j] < list[j-1]; j-- {
			list[j], list[j-1] = list[j-1], list[j]
		}
	}
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sortStrings(keys)
	return keys
}

func lastOIDSegment(key string) string {
	idx := strings.LastIndex(key, ".")
	if idx == -1 {
		return key
	}
	return key[idx+1:]
}

func atoiSafe(s string) int {
	v, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil {
		return 0
	}
	return v
}

// uint64Safe mem-parse counter SNMP (kosong/sentinel -> 0).
func uint64Safe(s string) uint64 {
	v, err := strconv.ParseUint(strings.TrimSpace(s), 10, 64)
	if err != nil || v == 4294967295 {
		return 0
	}
	return v
}

func percentValue(raw string) float64 {
	v, _ := strconv.ParseFloat(strings.TrimSpace(raw), 64)
	if v > 100 { // beberapa firmware melaporkan x100
		v /= 100
	}
	return v
}

func celsiusValue(raw string) float64 {
	v, _ := strconv.ParseFloat(strings.TrimSpace(raw), 64)
	if v > 200 { // x100 encoding
		v /= 100
	}
	return v
}

func divide100(raw string) float64 {
	v, _ := strconv.ParseFloat(strings.TrimSpace(raw), 64)
	return v / 100
}

func divide1000(raw string) float64 {
	v, _ := strconv.ParseFloat(strings.TrimSpace(raw), 64)
	return v / 1000
}

func cardStatusText(raw string) string {
	switch raw {
	case "1":
		return "InService"
	case "2":
		return "Standby"
	case "4", "5":
		return "" // slot kosong / tidak terpasang — sembunyikan
	case "0", "":
		return "Unknown"
	default:
		return "Offline"
	}
}

func cardRoleText(raw string) string {
	switch raw {
	case "1":
		return "active"
	case "2":
		return "standby"
	default:
		return ""
	}
}

