package ztecli

import (
	"errors"
	"strings"
	"testing"
)

func TestValidateCommand_AllowsInformationCommands(t *testing.T) {
	cases := []string{
		"show pon onu information ?",
		"show pon onu information gpon-olt_1/1/5 1",
		"show pon onu information gpon-onu_1/1/5:1",
		"service-port 5 description Internet",
		"switchport mode hybrid vport 1",
		"switch vlan 100 untag vport 1",
		"wan-ip 2 mode pppoe vlan-profile PPPoE username user1 password pass1 auth-mode auto host 1",
		"no wan-ip 2",
	}

	for _, cmd := range cases {
		if err := ValidateCommand(cmd); err != nil {
			t.Fatalf("command %q harus diizinkan, dapat error: %v", cmd, err)
		}
	}
}

func TestValidateCommand_BlocksDangerousToken(t *testing.T) {
	err := ValidateCommand("show running-config format")
	if err == nil {
		t.Fatal("expected error for dangerous token 'format'")
	}
	if !errors.Is(err, ErrForbiddenCmd) {
		t.Fatalf("expected ErrForbiddenCmd, got: %v", err)
	}
	if !strings.Contains(strings.ToLower(err.Error()), "format") {
		t.Fatalf("expected error mention 'format', got: %v", err)
	}
}

func TestValidateCommand_BlocksMultiCommandInjection(t *testing.T) {
	err := ValidateCommand("show version; reload")
	if err == nil {
		t.Fatal("expected multi-command injection to be blocked")
	}
	if !errors.Is(err, ErrForbiddenCmd) {
		t.Fatalf("expected ErrForbiddenCmd, got: %v", err)
	}
}

func TestFindCLICommandErrorLine_DetectsZTEError(t *testing.T) {
	out := `interface gpon-onu_1/1/5:2
%Error 20202: Invalid parameter.`
	line := findCLICommandErrorLine(out)
	if line == "" {
		t.Fatal("expected CLI error line to be detected")
	}
	if !strings.Contains(strings.ToLower(line), "error") {
		t.Fatalf("expected detected line contains error, got: %q", line)
	}
}

func TestFindCLICommandErrorLine_IgnoresNormalOutput(t *testing.T) {
	out := `show version
ZXAN C320 Software, Version V2.1.0`
	line := findCLICommandErrorLine(out)
	if line != "" {
		t.Fatalf("expected no error line, got: %q", line)
	}
}
