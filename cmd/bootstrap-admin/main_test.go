package main

import "testing"

func TestParseMode(t *testing.T) {
	tests := []struct {
		name      string
		arguments []string
		want      commandMode
		wantError bool
	}{
		{name: "create", want: modeCreate},
		{name: "check", arguments: []string{"--check"}, want: modeCheck},
		{name: "verify", arguments: []string{"--verify"}, want: modeVerify},
		{name: "unknown", arguments: []string{"--unknown"}, wantError: true},
		{name: "too many", arguments: []string{"--check", "extra"}, wantError: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := parseMode(test.arguments)
			if (err != nil) != test.wantError {
				t.Fatalf("parseMode() error = %v, wantError %v", err, test.wantError)
			}
			if got != test.want {
				t.Fatalf("parseMode() = %v, want %v", got, test.want)
			}
		})
	}
}
