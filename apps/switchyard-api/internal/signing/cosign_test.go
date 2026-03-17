package signing

import (
	"testing"
	"time"
)

func TestNewSigner(t *testing.T) {
	tests := []struct {
		name        string
		keyless     bool
		timeout     time.Duration
		wantMethod  string
		wantTimeout time.Duration
	}{
		{"keyless with default timeout", true, 0, "keyless", 2 * time.Minute},
		{"key-based with default timeout", false, 0, "key-based", 2 * time.Minute},
		{"keyless with custom timeout", true, 5 * time.Minute, "keyless", 5 * time.Minute},
		{"key-based with custom timeout", false, 30 * time.Second, "key-based", 30 * time.Second},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := NewSigner(tt.keyless, tt.timeout)
			if s == nil {
				t.Fatal("expected non-nil signer")
			}
			if s.keyless != tt.keyless {
				t.Errorf("keyless = %v, want %v", s.keyless, tt.keyless)
			}
			if s.timeout != tt.wantTimeout {
				t.Errorf("timeout = %v, want %v", s.timeout, tt.wantTimeout)
			}
			if got := s.getSigningMethod(); got != tt.wantMethod {
				t.Errorf("getSigningMethod() = %q, want %q", got, tt.wantMethod)
			}
		})
	}
}

func TestExtractSignature(t *testing.T) {
	s := NewSigner(true, 0)

	tests := []struct {
		name   string
		output string
		want   string
	}{
		{
			"cosign push output",
			"Pushing signature to: ghcr.io/madfam/my-service:sha256-abc123.sig",
			"ghcr.io/madfam/my-service:sha256-abc123.sig",
		},
		{
			"sha256 in output",
			"tlog entry created with index: 12345\nPushing signature to: registry.io/img:sha256-deadbeef.sig\n",
			"registry.io/img:sha256-deadbeef.sig",
		},
		{
			"no signature in output",
			"some random output without any sig info",
			"some random output without any sig info",
		},
		{
			"empty output",
			"",
			"",
		},
		{
			"long output gets truncated",
			string(make([]byte, 300)),
			string(make([]byte, 200)) + "...",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := s.extractSignature(tt.output)
			if got != tt.want {
				t.Errorf("extractSignature() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSignResult_Fields(t *testing.T) {
	now := time.Now().UTC()
	result := &SignResult{
		Success:       true,
		Signature:     "sha256:abc123",
		SignedAt:      now,
		SigningMethod: "keyless",
	}

	if !result.Success {
		t.Error("expected Success=true")
	}
	if result.Signature != "sha256:abc123" {
		t.Errorf("Signature = %q, want %q", result.Signature, "sha256:abc123")
	}
	if result.SigningMethod != "keyless" {
		t.Errorf("SigningMethod = %q, want %q", result.SigningMethod, "keyless")
	}
}

func TestSignatureInfo_Fields(t *testing.T) {
	info := &SignatureInfo{
		ImageURI:      "ghcr.io/test/img:v1",
		Signature:     "sig-data",
		SignedAt:      time.Now(),
		SigningMethod: "key-based",
		Verified:      true,
	}

	if info.ImageURI != "ghcr.io/test/img:v1" {
		t.Errorf("ImageURI = %q", info.ImageURI)
	}
	if !info.Verified {
		t.Error("expected Verified=true")
	}
}
