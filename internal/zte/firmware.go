package zte

// FirmwareProfile menampung seluruh OID untuk satu keluarga firmware ZTE.
// Sumber: pola OID go-api-c320 (s4lfanet) + validasi lapangan C320 produksi.
type FirmwareProfile struct {
	Name          string
	BaseOID       string
	ONUName       string // nama ONU
	ONUType       string // model tipe ONU (mis. F670L)
	ONUSerial     string // serial number
	ONUSerialHex  string // serial hex fallback (beberapa firmware)
	ONUDescr      string // deskripsi / firmware versi
	ONUStatus     string // status online (1=online; lainnya per tabel)
	ONURxPower    string // redaman terima ONU per-ONU (kosongkan bila firmware tak support)
	ONUTxPower    string
	OLTRxPower    string // OLT-side PON port Rx power (.3902.1015 SFP diag)
	OLTTxPower    string // OLT-side PON port Tx power (.3902.1015 SFP diag)
	OLTSFPRx2     string // alternatif factor dBm×100
	OLTSFPTx2     string
	ONUIP         string
	LastOnline    string
	LastOffline   string
	OfflineReason string
	Distance      string // jarak optik per-ONU (meter)
	// OpticalEncoding menjelaskan cara decode nilai mentah ke dBm.
	// "centi_minus_30" => raw>=32768 => (raw-65536)*0.01-30, raw<32768 => raw*0.01-30
	// "minus_10000_div_100" => (raw-10000)/100
	// "raw_div_500_minus_30" => raw/500 - 30
	// "dbuw_0_002_minus_30" => signed 16-bit *0.002 - 30
	// "" => default sfpMilliToFloat (dBm×1000)
	OpticalEncoding string
}

// Profiles dua generasi firmware C320/C300.
var FirmwareProfiles = map[string]*FirmwareProfile{
	"v2.1": {
		Name:    "ZTE C320/C300 v2.1",
		BaseOID: "1.3.6.1.4.1.3902.1012",
		// Debug-walk produksi 2026-08-29 (C320 V2.1.0): tree .1012 tidak
		// expose tabel name/serial (count 0). Enumerasi ONU tetap jalan via
		// fallback service.go ke profil v2.2 (tree .1082 hybrid).
		// Status .3.31.4.1.100 hidup (256 entri, key diagIndex.onuId, 1=online).
		// Distance .3.13.1.1.20 = max range per PORT (selalu 5000) — bukan
		// jarak riil per-ONU, sengaja dikosongkan.
		ONUName:         ".3.13.3.1.5",
		ONUType:         ".3.13.3.1.10",
		ONUSerial:       ".3.13.3.1.2",
		ONUSerialHex:    "",
		ONUDescr:        ".3.13.3.1.11",
		ONUStatus:       ".3.31.4.1.100",
		ONURxPower:      ".3.50.12.1.1.10",
		ONUTxPower:      ".3.50.12.1.1.11",
		OpticalEncoding: "raw_div_500_minus_30",
		// .13.1.1 = sentinel INT32_MAX di firmware ini; .13.1.2 konstan
		// -340 (threshold, bukan daya) → Rx OLT-side memang tak tersedia.
		OLTRxPower:    "1.3.6.1.4.1.3902.1015.3.1.13.1.1",
		OLTTxPower:    "1.3.6.1.4.1.3902.1015.3.1.13.1.4",
		OLTSFPRx2:     "1.3.6.1.4.1.3902.1015.3.1.13.1.2",
		OLTSFPTx2:     "1.3.6.1.4.1.3902.1015.3.1.13.1.5",
		ONUIP:         "",
		LastOnline:    ".3.31.4.1.2",
		LastOffline:   ".3.31.4.1.2",
		OfflineReason: ".3.13.3.1.4",
		Distance:      "",
	},
	"v2.2": {
		Name:    "ZTE C320/C300 v2.2+",
		BaseOID: "1.3.6.1.4.1.3902.1082",
		// OID tervalidasi di produksi (C320 V2.1.0 hybrid tree .1082).
		// PENTING: tree .500.20.* dan .1015.1010.* Rx/Tx per-ONU bikin
		// TIMEOUT walk di firmware ini — JANGAN di-walk.
		// Serial string .3.3.1.18 KOSONG pada V2.1.0 → pakai hex .3.3.1.6.
		// Status: .3.3.1.9 kosong → .3.8.1.4 hidup (1=working).
		// Distance: .500.10.2.3.10.1.2 (meter) — masih divalidasi via
		// debug-walk; kalau kosong berarti firmware tak expose.
		ONUName:         ".500.10.2.3.3.1.2",
		ONUType:         ".500.10.2.3.3.1.7",
		ONUSerial:       ".500.10.2.3.3.1.18",
		ONUSerialHex:    ".500.10.2.3.3.1.6",
		ONUDescr:        ".500.10.2.3.3.1.3",
		ONUStatus:       ".500.10.2.3.8.1.4",
		ONURxPower:      ".500.20.2.2.2.1.10",
		ONUTxPower:      ".500.20.2.2.2.1.14",
		OpticalEncoding: "dbuw_0_002_minus_30",
		OLTRxPower:      "1.3.6.1.4.1.3902.1015.3.1.13.1.1", // SFP per PON port
		OLTTxPower:      "1.3.6.1.4.1.3902.1015.3.1.13.1.4",
		OLTSFPRx2:       "1.3.6.1.4.1.3902.1015.3.1.13.1.2",
		OLTSFPTx2:       "1.3.6.1.4.1.3902.1015.3.1.13.1.5",
		ONUIP:           "",
		LastOnline:      ".500.10.2.3.8.1.5",
		LastOffline:     ".500.10.2.3.8.1.6",
		OfflineReason:   ".500.10.2.3.8.1.7",
		Distance:        ".500.10.2.3.10.1.2",
	},
}

// DetectFirmware menebak profil firmware dari sysDescr.
// Default = v2.2 (tree 1082) — mayoritas C320/C300 modern & terbukti
// berjalan pada OLT produksi user (barubill.dasnet.biz.id, Aug 2026).
func DetectFirmware(sysDescr string) *FirmwareProfile {
	d := lower(sysDescr)
	// Hanya v2.1 eksplisit yang memakai tree lama .1012
	if contains(d, "v2.1") || contains(d, "v2.1.0") || contains(d, "2.1.0p") {
		return FirmwareProfiles["v2.1"]
	}
	return FirmwareProfiles["v2.2"]
}

func lower(s string) string {
	out := []byte(s)
	for i := range out {
		if out[i] >= 'A' && out[i] <= 'Z' {
			out[i] += 32
		}
	}
	return string(out)
}

func contains(haystack, needle string) bool {
	return len(needle) > 0 && len(haystack) >= len(needle) && indexOf(haystack, needle) >= 0
}

func indexOf(haystack, needle string) int {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return i
		}
	}
	return -1
}

// Trafik counter (ifTable) per interface ONU.
// Di ZTE C320/C300, tiap ONU punya ifIndex sendiri pada ifTable standar,
// dengan nama interface berpola lokasi (mis. "gpon-onu_1/1/6:1").
var (
	TrafficInOctets  = "1.3.6.1.2.1.2.2.1.10"    // ifInOctets  (32-bit)
	TrafficOutOctets = "1.3.6.1.2.1.2.2.1.16"    // ifOutOctets (32-bit)
	TrafficHCIn      = "1.3.6.1.2.1.31.1.1.1.6"  // ifHCInOctets  (64-bit)
	TrafficHCOut     = "1.3.6.1.2.1.31.1.1.1.10" // ifHCOutOctets (64-bit)
	IfName           = "1.3.6.1.2.1.2.2.1.2"     // ifName (pola lokasi ONU)
	IfDescr          = "1.3.6.1.2.1.31.1.1.1.1"  // ifAlias/descr
)

// AlternateProfile mengembalikan profil pasangan (v2.1 <-> v2.2).
// Dipakai sebagai fallback saat walk dengan profil terdeteksi kosong.
func AlternateProfile(current *FirmwareProfile) *FirmwareProfile {
	if current == nil || current.Name == FirmwareProfiles["v2.1"].Name {
		return FirmwareProfiles["v2.2"]
	}
	return FirmwareProfiles["v2.1"]
}
