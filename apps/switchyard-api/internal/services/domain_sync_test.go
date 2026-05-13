package services

import "testing"

func TestTunnelCNAMEForTunnelID(t *testing.T) {
	tests := []struct {
		name     string
		tunnelID string
		want     string
	}{
		{
			name:     "configured tunnel id",
			tunnelID: "c9fac286-497b-4aac-9288-f784a1ea561c",
			want:     "c9fac286-497b-4aac-9288-f784a1ea561c.cfargotunnel.com",
		},
		{
			name:     "legacy fallback",
			tunnelID: "",
			want:     DefaultTunnelCNAME,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := TunnelCNAMEForTunnelID(tt.tunnelID); got != tt.want {
				t.Fatalf("TunnelCNAMEForTunnelID() = %q, want %q", got, tt.want)
			}
		})
	}
}
