package api

import "testing"

func TestResendRecordFQDN(t *testing.T) {
	cases := map[string]string{
		"resend._domainkey": "resend._domainkey.enclii.dev",
		"send":              "send.enclii.dev",
		"@":                 "enclii.dev",
		"":                  "enclii.dev",
	}
	for name, want := range cases {
		if got := resendRecordFQDN("enclii.dev", name); got != want {
			t.Fatalf("resendRecordFQDN(%q) = %q, want %q", name, got, want)
		}
	}
}

func TestResendDomainTarget(t *testing.T) {
	req := operatorOperationRequest{Args: map[string]string{"target": "Enclii.Dev"}}
	if got := resendDomainTarget(req); got != "enclii.dev" {
		t.Fatalf("got %q", got)
	}
}
