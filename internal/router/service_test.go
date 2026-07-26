package router

import "testing"

func TestParseProfilePrice(t *testing.T) {
	cases := []struct {
		name     string
		profile  string
		want     float64
		hasPrice bool
	}{
		{name: "plain 150K", profile: "150K", want: 150_000, hasPrice: true},
		{name: "lowercase k", profile: "250k", want: 250_000, hasPrice: true},
		{name: "with prefix", profile: "PAKET-150K", want: 150_000, hasPrice: true},
		{name: "with words", profile: "Home 300K Fiber", want: 300_000, hasPrice: true},
		{name: "space before K", profile: "Paket 175 K", want: 175_000, hasPrice: true},
		{name: "no price", profile: "default-vip", want: 0, hasPrice: false},
		{name: "number without K", profile: "profile-100", want: 0, hasPrice: false},
		{name: "K inside word", profile: "PAKET-KILAT", want: 0, hasPrice: false},
		{name: "unreasonably large", profile: "9999999K", want: 0, hasPrice: false},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			got, hasPrice := ParseProfilePrice(testCase.profile)
			if hasPrice != testCase.hasPrice || got != testCase.want {
				t.Fatalf("ParseProfilePrice(%q) = %v, %v; want %v, %v", testCase.profile, got, hasPrice, testCase.want, testCase.hasPrice)
			}
		})
	}
}
