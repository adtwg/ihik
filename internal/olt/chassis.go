package olt

import (
	"context"
	"sort"
	"strconv"
	"strings"

	"isp-billing/internal/zte"
)

// ChassisPort adalah satu port PON pada line card, dengan status ringkas.
type ChassisPort struct {
	Port      int     `json:"port"`
	Label     string  `json:"label,omitempty"`
	HasSFP    bool    `json:"has_sfp"`
	RxDBM     float64 `json:"rx_dbm,omitempty"`
	TxDBM     float64 `json:"tx_dbm,omitempty"`
	ONUTotal  int     `json:"onu_total"`
	ONUOnline int     `json:"onu_online"`
	Status    string  `json:"status"` // online|los|idle|empty
}

// ChassisCard adalah satu card pada slot chassis OLT.
type ChassisCard struct {
	Slot      int           `json:"slot"`
	Type      string        `json:"type,omitempty"`
	Status    string        `json:"status,omitempty"` // InService|Standby|Offline
	Role      string        `json:"role,omitempty"`
	CPUPerc   float64       `json:"cpu_percent"`
	MemPerc   float64       `json:"mem_percent"`
	TempC     float64       `json:"temp_c"`
	IsControl bool          `json:"is_control"`
	PortCount int           `json:"port_count"`
	Ports     []ChassisPort `json:"ports,omitempty"`
}

// ChassisView adalah snapshot layout fisik OLT untuk visualisasi.
type ChassisView struct {
	Model  string        `json:"model"`
	Family string        `json:"family"` // C320|C300|unknown
	Source string        `json:"source"` // snmp|cli|snmp+cli
	Cards  []ChassisCard `json:"cards"`
}

// Chassis menyusun layout fisik OLT (card + port) dari data health SNMP,
// dengan fallback CLI "show card" bila daftar card SNMP kosong (umum di
// firmware v2.1 yang time-out pada OID card). Occupancy port dihitung dari
// cache ONU (jumlah & online per PON).
func (service *Service) Chassis(ctx context.Context, tenantID, id string) (*ChassisView, error) {
	oltMeta, err := service.Get(ctx, tenantID, id)
	if err != nil {
		return nil, err
	}
	view := &ChassisView{Model: oltMeta.Model, Family: chassisFamily(oltMeta.Model)}

	health, herr := service.GetHealth(ctx, tenantID, id)
	var cards []zte.CardInfo
	var sfps []zte.SfpInfo
	if herr == nil && health != nil {
		cards = health.Cards
		sfps = health.Sfps
	}

	source := "snmp"
	portHint := map[int]int{}
	if len(cards) == 0 {
		if raw, cerr := service.ExecuteCLIShow(ctx, tenantID, id, "show card"); cerr == nil {
			parsed, hint := parseShowCard(raw)
			if len(parsed) > 0 {
				cards = parsed
				portHint = hint
				if len(sfps) > 0 {
					source = "snmp+cli"
				} else {
					source = "cli"
				}
			}
		}
	}
	if len(cards) == 0 && herr != nil {
		return nil, herr
	}
	view.Source = source

	onus, _ := service.ListONUs(ctx, tenantID, id)

	type portKey struct{ slot, port int }
	type portAgg struct{ total, online int }
	onuAgg := map[portKey]*portAgg{}
	for _, o := range onus {
		slot, port, ok := slotPortFromPON(o.ONUNumber)
		if !ok {
			continue
		}
		key := portKey{slot, port}
		agg := onuAgg[key]
		if agg == nil {
			agg = &portAgg{}
			onuAgg[key] = agg
		}
		agg.total++
		if statusAllowsOptical(strings.ToLower(strings.TrimSpace(o.Status))) {
			agg.online++
		}
	}
	sfpMap := map[portKey]zte.SfpInfo{}
	for _, s := range sfps {
		slot, port, ok := slotPortFromPON(s.Label)
		if ok {
			sfpMap[portKey{slot, port}] = s
		}
	}

	out := make([]ChassisCard, 0, len(cards))
	for _, c := range cards {
		card := ChassisCard{
			Slot:      c.Slot,
			Type:      c.Type,
			Status:    c.Status,
			Role:      c.Role,
			CPUPerc:   c.CPUPerc,
			MemPerc:   c.MemPerc,
			TempC:     c.TempC,
			IsControl: isControlCard(c.Type),
		}
		if !card.IsControl {
			maxPort := portHint[c.Slot]
			for k := range sfpMap {
				if k.slot == c.Slot && k.port > maxPort {
					maxPort = k.port
				}
			}
			for k := range onuAgg {
				if k.slot == c.Slot && k.port > maxPort {
					maxPort = k.port
				}
			}
			for p := 1; p <= maxPort; p++ {
				port := ChassisPort{Port: p}
				if s, ok := sfpMap[portKey{c.Slot, p}]; ok {
					port.HasSFP = true
					port.Label = s.Label
					port.RxDBM = s.RxPowerDBM
					port.TxDBM = s.TxPowerDBM
				}
				if a, ok := onuAgg[portKey{c.Slot, p}]; ok {
					port.ONUTotal = a.total
					port.ONUOnline = a.online
				}
				port.Status = chassisPortStatus(port)
				card.Ports = append(card.Ports, port)
			}
			card.PortCount = len(card.Ports)
		}
		out = append(out, card)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Slot < out[j].Slot })
	view.Cards = out
	return view, nil
}

// chassisFamily menormalkan model ke keluarga chassis untuk pemilihan layout.
func chassisFamily(model string) string {
	m := strings.ToUpper(model)
	switch {
	case strings.Contains(m, "C320"):
		return "C320"
	case strings.Contains(m, "C300"):
		return "C300"
	case strings.Contains(m, "C220"):
		return "C220"
	case strings.Contains(m, "C600"):
		return "C600"
	default:
		return "unknown"
	}
}

// isControlCard menandai card kontrol/switch (SCXL/SCXM/SMXA/dll) vs line card.
func isControlCard(cardType string) bool {
	t := strings.ToUpper(strings.TrimSpace(cardType))
	if t == "" {
		return false
	}
	for _, p := range []string{"SCX", "SMX", "SXM", "MCUD", "SUXA", "PRWG", "PRWF"} {
		if strings.HasPrefix(t, p) {
			return true
		}
	}
	return false
}

// chassisPortStatus meringkas status satu port dari occupancy ONU + SFP.
func chassisPortStatus(p ChassisPort) string {
	switch {
	case p.ONUTotal > 0 && p.ONUOnline > 0:
		return "online"
	case p.ONUTotal > 0:
		return "los"
	case p.HasSFP:
		return "idle"
	default:
		return "empty"
	}
}

// slotPortFromPON mengambil (slot, port) dari label PON "shelf/slot/port"
// atau "shelf/slot/port:onuID" atau "slot/port".
func slotPortFromPON(ref string) (int, int, bool) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return 0, 0, false
	}
	if i := strings.IndexByte(ref, ':'); i >= 0 {
		ref = ref[:i]
	}
	parts := strings.Split(ref, "/")
	nums := make([]int, 0, len(parts))
	for _, part := range parts {
		n, err := strconv.Atoi(strings.TrimSpace(part))
		if err != nil {
			return 0, 0, false
		}
		nums = append(nums, n)
	}
	switch len(nums) {
	case 3:
		return nums[1], nums[2], true
	case 2:
		return nums[0], nums[1], true
	default:
		return 0, 0, false
	}
}

// parseShowCard mengurai output CLI "show card" ZTE menjadi daftar card +
// perkiraan jumlah port per slot. Toleran terhadap kolom kosong/"-".
func parseShowCard(raw string) ([]zte.CardInfo, map[int]int) {
	cards := make([]zte.CardInfo, 0, 8)
	portHint := map[int]int{}
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "-") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 4 {
			continue
		}
		// Baris data diawali Rack Shelf Slot (tiga angka).
		rack, err1 := strconv.Atoi(fields[0])
		shelf, err2 := strconv.Atoi(fields[1])
		slot, err3 := strconv.Atoi(fields[2])
		if err1 != nil || err2 != nil || err3 != nil || rack < 0 || shelf < 0 || slot < 1 {
			continue
		}
		cfgType := fields[3]
		realType := ""
		if len(fields) >= 5 {
			realType = fields[4]
		}
		cardType := realType
		if cardType == "" || cardType == "-" {
			cardType = cfgType
		}
		if cardType == "-" {
			cardType = ""
		}
		// Port count: angka pertama setelah kolom tipe.
		for _, f := range fields[4:] {
			if n, err := strconv.Atoi(f); err == nil {
				if n > 0 && n <= 64 {
					portHint[slot] = n
				}
				break
			}
		}
		status := cardStatusFromCLI(fields[len(fields)-1])
		cards = append(cards, zte.CardInfo{Slot: slot, Type: cardType, Status: status})
	}
	return cards, portHint
}

// cardStatusFromCLI memetakan token status CLI ke label yang dipakai SNMP.
func cardStatusFromCLI(token string) string {
	switch strings.ToUpper(strings.TrimSpace(token)) {
	case "INSERVICE", "ONLINE", "READY":
		return "InService"
	case "STANDBY":
		return "Standby"
	case "OFFLINE", "FAULT", "ABNORMAL":
		return "Offline"
	default:
		return ""
	}
}
