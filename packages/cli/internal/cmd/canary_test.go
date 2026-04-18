package cmd

import (
	"testing"
)

func TestParseCanaryPercentage(t *testing.T) {
	cases := []struct {
		input   string
		want    int
		wantErr bool
	}{
		{"20%", 20, false},
		{"20", 20, false},
		{" 20 % ", 20, false},
		{"5%", 5, false},
		{"50%", 50, false},
		{"4%", 0, true},  // below min
		{"51%", 0, true}, // above max
		{"abc", 0, true},
		{"", 0, true},
		{"100", 0, true},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.input, func(t *testing.T) {
			got, err := parseCanaryPercentage(tc.input)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got %d", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Errorf("got %d, want %d", got, tc.want)
			}
		})
	}
}

func TestLooksLikeUUID(t *testing.T) {
	cases := map[string]bool{
		"11111111-2222-3333-4444-555555555555":  true,
		"not-a-uuid":                            false,
		"fortuna-api":                           false,
		"":                                      false,
		"11111111-2222-3333-4444-5555555555555": false, // too long
	}
	for in, want := range cases {
		if got := looksLikeUUID(in); got != want {
			t.Errorf("looksLikeUUID(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestTruncate(t *testing.T) {
	if got := truncate("abc", 10); got != "abc" {
		t.Errorf("truncate short: %q", got)
	}
	if got := truncate("abcdefghij", 5); got != "abcde..." {
		t.Errorf("truncate long: %q", got)
	}
}
