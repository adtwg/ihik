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
