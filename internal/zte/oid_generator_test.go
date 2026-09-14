package zte

import (
	"fmt"
	"testing"
)

func TestGenerateBoardPonOIDSuffixes(t *testing.T) {
	// Konstanta terverifikasi vs hardware (referensi go-api-c320 / snmp-olt-zte).
	cases := []struct {
		board, pon         int
		wantID, wantType   int
	}{
		{1, 1, 285278465, 268501248},
		{2, 1, 285278721, 268566784},
		{3, 1, 285278977, 268632320},
	}
	for _, c := range cases {
		cfg, err := GenerateBoardPonOID(c.board, c.pon)
		if err != nil {
			t.Fatalf("board %d pon %d: error tak terduga: %v", c.board, c.pon, err)
		}
		if cfg.OnuIDSuffix != c.wantID {
			t.Errorf("board %d pon %d OnuIDSuffix = %d, mau %d", c.board, c.pon, cfg.OnuIDSuffix, c.wantID)
		}
		if cfg.OnuTypeSuffix != c.wantType {
			t.Errorf("board %d pon %d OnuTypeSuffix = %d, mau %d", c.board, c.pon, cfg.OnuTypeSuffix, c.wantType)
		}
	}
}

func TestGenerateBoardPonOIDStrings(t *testing.T) {
	cfg, err := GenerateBoardPonOID(1, 1)
	if err != nil {
		t.Fatalf("error tak terduga: %v", err)
	}
	wantName := fmt.Sprintf("%s%s.%d", BaseOID1, OnuIDNamePrefix, 285278465)
	if cfg.NameOID != wantName {
		t.Errorf("NameOID = %s, mau %s", cfg.NameOID, wantName)
	}
	wantTx := fmt.Sprintf("%s%s.%d", BaseOID2, OnuTxPowerPrefix, 268501248)
	if cfg.TxPowerOID != wantTx {
		t.Errorf("TxPowerOID = %s, mau %s", cfg.TxPowerOID, wantTx)
	}
}

func TestGenerateBoardPonOIDValidation(t *testing.T) {
	if _, err := GenerateBoardPonOID(0, 1); err == nil {
		t.Error("board 0 seharusnya error")
	}
	if _, err := GenerateBoardPonOID(1, 0); err == nil {
		t.Error("pon 0 seharusnya error")
	}
	if _, err := GenerateBoardPonOID(MaxBoardID+1, 1); err == nil {
		t.Error("board di atas batas seharusnya error")
	}
}
