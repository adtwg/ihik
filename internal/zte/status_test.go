package zte

import "testing"

func TestStatusFromCode(t *testing.T) {
	cases := map[uint64]string{
		1: "logging",
		2: "los",
		3: "sync_mib",
		4: "working",
		5: "dying_gasp",
		6: "offlined",
		7: "auth_failed",
		0: "unknown",
		9: "unknown",
	}
	for code, want := range cases {
		if got := StatusFromCode(code); got != want {
			t.Errorf("StatusFromCode(%d) = %q, mau %q", code, got, want)
		}
	}
}

func TestOnuStatusTextDelegates(t *testing.T) {
	if got := onuStatusText("4"); got != "working" {
		t.Errorf("onuStatusText(\"4\") = %q, mau working", got)
	}
	if got := onuStatusText(""); got != "unknown" {
		t.Errorf("onuStatusText(\"\") = %q, mau unknown", got)
	}
	if got := onuStatusText("x"); got != "unknown" {
		t.Errorf("onuStatusText(\"x\") = %q, mau unknown", got)
	}
}

func TestOnuLabelSlotDecoding(t *testing.T) {
	// Encoding 0x11|shelf|slot|pon: slot di bits 8-15. Regression: decode lama
	// membaca shelf sebagai slot sehingga slot>=2 kolaps ke label slot 1.
	cases := map[string]string{
		"285278465.5": "1/1/1:5", // 0x11010101 slot1 pon1
		"285278471.9": "1/1/7:9", // 0x11010107 slot1 pon7
		"285278727.9": "1/2/7:9", // 0x11010207 slot2 pon7 (dulu salah: 1/1/7)
		"285278977.1": "1/3/1:1", // 0x11010301 C300 slot3 (verifikasi referensi)
	}
	for index, want := range cases {
		if got := onuLabel(index); got != want {
			t.Errorf("onuLabel(%q) = %q, mau %q", index, got, want)
		}
	}
	// Kolisi lama: slot1/pon7 vs slot2/pon7 kini WAJIB beda label.
	if onuLabel("285278471.5") == onuLabel("285278727.5") {
		t.Error("label slot1 dan slot2 masih kolaps (collision)")
	}
}
