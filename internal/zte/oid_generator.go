package zte

import "fmt"

// OID slot-parametrik untuk ZTE C320/C300 (shelf=1). Sumber: pola go-api-c320
// (s4lfanet) + snmp-olt-zte (Cepat-Kilat), tervalidasi vs hardware. Tujuan:
// hitung ifIndex per (board,pon) lalu GET/GETBULK terarah — hindari walk
// seluruh tree yang lambat/timeout pada firmware .1082.
//
// Dua ruang index:
//   ONU-ID space (BaseOID1 .3902.1082): name/serial/status/descr/rx/lastonline/offline/reason/distance
//     onuIDSuffix   = OnuIDIfIndexBase   + slot*OnuIDSlotStride   + pon*OnuIDIncrement
//   TYPE space  (BaseOID2 .3902.1012): onu type / tx power / ip
//     onuTypeSuffix = OnuTypeIfIndexBase + slot*OnuTypeSlotStride + pon*OnuTypeIncrement
//
// Verifikasi: slot1->285278465/268501248, slot2->285278721/268566784,
// slot3(C300)->285278977/268632320.
const (
	BaseOID1 = ".1.3.6.1.4.1.3902.1082"
	BaseOID2 = ".1.3.6.1.4.1.3902.1012"

	// Prefix per-field pada ONU-ID space (relatif BaseOID1).
	OnuIDNamePrefix              = ".500.10.2.3.3.1.2"
	OnuSerialNumberPrefix        = ".500.10.2.3.3.1.18"
	OnuDescriptionPrefix         = ".500.10.2.3.3.1.3"
	OnuStatusIDPrefix            = ".500.10.2.3.8.1.4"
	OnuRxPowerPrefix             = ".500.20.2.2.2.1.10"
	OnuLastOnlineTimePrefix      = ".500.10.2.3.8.1.5"
	OnuLastOfflineTimePrefix     = ".500.10.2.3.8.1.6"
	OnuLastOfflineReasonPrefix   = ".500.10.2.3.8.1.7"
	OnuGponOpticalDistancePrefix = ".500.10.2.3.10.1.2"

	// Prefix per-field pada TYPE space (relatif BaseOID2).
	OnuTypePrefix      = ".3.50.11.2.1.17"
	OnuTxPowerPrefix   = ".3.50.12.1.1.14"
	OnuIPAddressPrefix = ".3.50.16.1.1.10"

	// Basis ifIndex & stride (lihat doc paket).
	OnuIDIfIndexBase   = 285278208 // 0x11010000
	OnuIDSlotStride    = 256       // 0x100
	OnuIDIncrement     = 1
	OnuTypeIfIndexBase = 268435456 // 0x10000000
	OnuTypeSlotStride  = 65536     // 0x10000
	OnuTypeIncrement   = 256

	MaxBoardID = 30
	MaxPonID   = 16
)

// BoardPonConfig menampung OID root per-(board,pon). Walk root status/name
// menghasilkan leaf per-onuId; GET field spesifik = root + "." + onuID.
type BoardPonConfig struct {
	BoardID int
	PonID   int

	OnuIDSuffix   int
	OnuTypeSuffix int

	NameOID            string
	SerialNumberOID    string
	DescriptionOID     string
	StatusOID          string
	RxPowerOID         string
	LastOnlineOID      string
	LastOfflineOID     string
	OfflineReasonOID   string
	OpticalDistanceOID string
	TypeOID            string
	TxPowerOID         string
	IPAddressOID       string
}

// GenerateBoardPonOID menghitung seluruh OID root untuk satu (board,pon) fisik.
func GenerateBoardPonOID(boardID, ponID int) (*BoardPonConfig, error) {
	if boardID < 1 || boardID > MaxBoardID {
		return nil, fmt.Errorf("boardID tidak valid: %d (harus 1-%d)", boardID, MaxBoardID)
	}
	if ponID < 1 || ponID > MaxPonID {
		return nil, fmt.Errorf("ponID tidak valid: %d (harus 1-%d)", ponID, MaxPonID)
	}

	onuIDSuffix := OnuIDIfIndexBase + boardID*OnuIDSlotStride + ponID*OnuIDIncrement
	onuTypeSuffix := OnuTypeIfIndexBase + boardID*OnuTypeSlotStride + ponID*OnuTypeIncrement

	id := func(prefix string) string { return fmt.Sprintf("%s%s.%d", BaseOID1, prefix, onuIDSuffix) }
	typ := func(prefix string) string { return fmt.Sprintf("%s%s.%d", BaseOID2, prefix, onuTypeSuffix) }

	return &BoardPonConfig{
		BoardID:            boardID,
		PonID:              ponID,
		OnuIDSuffix:        onuIDSuffix,
		OnuTypeSuffix:      onuTypeSuffix,
		NameOID:            id(OnuIDNamePrefix),
		SerialNumberOID:    id(OnuSerialNumberPrefix),
		DescriptionOID:     id(OnuDescriptionPrefix),
		StatusOID:          id(OnuStatusIDPrefix),
		RxPowerOID:         id(OnuRxPowerPrefix),
		LastOnlineOID:      id(OnuLastOnlineTimePrefix),
		LastOfflineOID:     id(OnuLastOfflineTimePrefix),
		OfflineReasonOID:   id(OnuLastOfflineReasonPrefix),
		OpticalDistanceOID: id(OnuGponOpticalDistancePrefix),
		TypeOID:            typ(OnuTypePrefix),
		TxPowerOID:         typ(OnuTxPowerPrefix),
		IPAddressOID:       typ(OnuIPAddressPrefix),
	}, nil
}
