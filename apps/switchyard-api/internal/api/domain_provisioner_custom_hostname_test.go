package api

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/cloudflare"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/config"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// stubZoneResolver answers the one question the mechanism decision asks.
type stubZoneResolver struct {
	zonesWeControl map[string]bool
	calls          int
}

func (s *stubZoneResolver) FindZoneForDomain(ctx context.Context, domain string) (*cloudflare.Zone, error) {
	s.calls++
	if s.zonesWeControl[domain] {
		return &cloudflare.Zone{ID: "zone-" + domain, Name: domain}, nil
	}
	return nil, errors.New("no Cloudflare zone found for domain " + domain)
}

func saasConfiguredHandler() *Handler {
	return &Handler{
		logger: newNopLogger(),
		config: &config.Config{
			CloudflareFallbackOriginZoneID:   "fallback-zone",
			CloudflareFallbackOriginHostname: "proxy.enclii.dev",
		},
	}
}

func TestResolveDomainMechanism(t *testing.T) {
	tests := []struct {
		name       string
		handler    *Handler
		zones      zoneResolver
		domain     string
		external   *bool
		want       domainProvisioningMechanism
		wantLookup bool
	}{
		{
			name:       "domain in a zone we control keeps the zone path",
			handler:    saasConfiguredHandler(),
			zones:      &stubZoneResolver{zonesWeControl: map[string]bool{"api.madfam.io": true}},
			domain:     "api.madfam.io",
			want:       mechanismZoneCNAME,
			wantLookup: true,
		},
		{
			name:       "client-owned domain routes to the custom hostname path",
			handler:    saasConfiguredHandler(),
			zones:      &stubZoneResolver{},
			domain:     "cto.creatumundo.mx",
			want:       mechanismCustomHostname,
			wantLookup: true,
		},
		{
			name:     "external true forces the custom hostname path without a lookup",
			handler:  saasConfiguredHandler(),
			zones:    &stubZoneResolver{zonesWeControl: map[string]bool{"api.madfam.io": true}},
			domain:   "api.madfam.io",
			external: boolPtr(true),
			want:     mechanismCustomHostname,
		},
		{
			name:     "external false forces the zone path without a lookup",
			handler:  saasConfiguredHandler(),
			zones:    &stubZoneResolver{},
			domain:   "cto.creatumundo.mx",
			external: boolPtr(false),
			want:     mechanismZoneCNAME,
		},
		{
			name: "unconfigured fallback origin keeps today's zone behaviour",
			handler: &Handler{
				logger: newNopLogger(),
				config: &config.Config{},
			},
			zones:  &stubZoneResolver{},
			domain: "cto.creatumundo.mx",
			want:   mechanismZoneCNAME,
		},
		{
			name: "partially configured fallback origin is treated as unconfigured",
			handler: &Handler{
				logger: newNopLogger(),
				config: &config.Config{CloudflareFallbackOriginZoneID: "fallback-zone"},
			},
			zones:  &stubZoneResolver{},
			domain: "cto.creatumundo.mx",
			want:   mechanismZoneCNAME,
		},
		{
			name:    "nil zone resolver falls back to the zone path",
			handler: saasConfiguredHandler(),
			zones:   nil,
			domain:  "cto.creatumundo.mx",
			want:    mechanismZoneCNAME,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.handler.resolveDomainMechanism(context.Background(), tt.zones, tt.domain, tt.external)
			if got != tt.want {
				t.Errorf("mechanism = %q, want %q", got, tt.want)
			}
			if stub, ok := tt.zones.(*stubZoneResolver); ok && stub != nil {
				if tt.wantLookup && stub.calls == 0 {
					t.Error("expected a zone lookup, none happened")
				}
				if !tt.wantLookup && stub.calls != 0 {
					t.Errorf("zone lookups = %d, want 0", stub.calls)
				}
			}
		})
	}
}

func TestCustomHostnameZone(t *testing.T) {
	tests := []struct {
		name    string
		handler *Handler
		wantOK  bool
	}{
		{
			name:    "both values configured",
			handler: saasConfiguredHandler(),
			wantOK:  true,
		},
		{
			name:    "nil config",
			handler: &Handler{logger: newNopLogger()},
			wantOK:  false,
		},
		{
			name: "whitespace-only values are not configuration",
			handler: &Handler{
				logger: newNopLogger(),
				config: &config.Config{
					CloudflareFallbackOriginZoneID:   "   ",
					CloudflareFallbackOriginHostname: "proxy.enclii.dev",
				},
			},
			wantOK: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, ok := tt.handler.customHostnameZone()
			if ok != tt.wantOK {
				t.Errorf("ok = %v, want %v", ok, tt.wantOK)
			}
		})
	}
}

func TestEnsureCustomHostnameFailsClosedWhenUnconfigured(t *testing.T) {
	h := &Handler{logger: newNopLogger(), config: &config.Config{}}

	result := h.ensureCustomHostname(context.Background(), "cto.creatumundo.mx")
	if result.Err == nil {
		t.Fatal("expected a typed provisioning error, got nil")
	}
	if result.ErrorMessage == "" {
		t.Error("ErrorMessage is empty; the failure must be legible on the record")
	}
	if result.CustomHostnameID != "" {
		t.Errorf("CustomHostnameID = %q, want empty on failure", result.CustomHostnameID)
	}
	if result.HostnameStatus != "" {
		t.Errorf("HostnameStatus = %q, want empty on failure", result.HostnameStatus)
	}
}

func TestDeleteCustomHostnameGuards(t *testing.T) {
	t.Run("no hostname id is a no-op", func(t *testing.T) {
		h := saasConfiguredHandler()
		if err := h.deleteCustomHostname(context.Background(), "cto.creatumundo.mx", ""); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("unconfigured fails closed rather than silently succeeding", func(t *testing.T) {
		h := &Handler{logger: newNopLogger(), config: &config.Config{}}
		if err := h.deleteCustomHostname(context.Background(), "cto.creatumundo.mx", "ch-1"); err == nil {
			t.Fatal("expected an error when Cloudflare for SaaS is not configured")
		}
	})

	t.Run("by-domain teardown is a no-op when the feature is off", func(t *testing.T) {
		// Nothing could have been provisioned, so there is nothing to delete
		// and nothing to complain about.
		h := &Handler{logger: newNopLogger(), config: &config.Config{}}
		if err := h.deleteCustomHostnameByDomain(context.Background(), "cto.creatumundo.mx"); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("by-domain teardown is a no-op without a cloudflare client", func(t *testing.T) {
		h := saasConfiguredHandler()
		if err := h.deleteCustomHostnameByDomain(context.Background(), "cto.creatumundo.mx"); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

func TestApplyProvisioningResult(t *testing.T) {
	now := time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name          string
		record        *types.CustomDomain
		result        domainProvisioningResult
		wantStatus    string
		wantVerified  bool
		wantProvider  string
		wantErrStored bool
	}{
		{
			name:   "cloudflare reports active hostname and active certificate",
			record: &types.CustomDomain{Domain: "cto.creatumundo.mx"},
			result: domainProvisioningResult{
				Domain:           "cto.creatumundo.mx",
				Mechanism:        mechanismCustomHostname,
				CustomHostnameID: "ch-1",
				HostnameStatus:   string(cloudflare.CustomHostnameStatusActive),
				SSLStatus:        string(cloudflare.CustomHostnameSSLActive),
			},
			wantStatus:   types.DomainStatusActive,
			wantVerified: true,
			wantProvider: types.TLSProviderCloudflareForSaaS,
		},
		{
			// A 200 from Cloudflare is not "active". Only Cloudflare saying
			// active makes it active.
			name:   "pending hostname is never marked verified",
			record: &types.CustomDomain{Domain: "cto.creatumundo.mx"},
			result: domainProvisioningResult{
				Domain:           "cto.creatumundo.mx",
				Mechanism:        mechanismCustomHostname,
				CustomHostnameID: "ch-1",
				HostnameStatus:   string(cloudflare.CustomHostnameStatusPending),
				SSLStatus:        string(cloudflare.CustomHostnameSSLPendingValidation),
				PendingDNSRecords: []types.PendingDNSRecord{
					{Purpose: "routing", Type: "CNAME", Name: "cto.creatumundo.mx", Value: "proxy.enclii.dev"},
				},
			},
			wantStatus:   types.DomainStatusPending,
			wantVerified: false,
			wantProvider: types.TLSProviderCloudflareForSaaS,
		},
		{
			name:   "hostname active but certificate pending stays pending",
			record: &types.CustomDomain{Domain: "cto.creatumundo.mx"},
			result: domainProvisioningResult{
				Mechanism:      mechanismCustomHostname,
				HostnameStatus: string(cloudflare.CustomHostnameStatusActive),
				SSLStatus:      string(cloudflare.CustomHostnameSSLPendingIssuance),
			},
			wantStatus:   types.DomainStatusPending,
			wantVerified: false,
			wantProvider: types.TLSProviderCloudflareForSaaS,
		},
		{
			name:   "a previously verified domain is un-verified when it moves away",
			record: &types.CustomDomain{Domain: "cto.creatumundo.mx", Verified: true, Status: types.DomainStatusActive},
			result: domainProvisioningResult{
				Mechanism:      mechanismCustomHostname,
				HostnameStatus: string(cloudflare.CustomHostnameStatusMoved),
				SSLStatus:      string(cloudflare.CustomHostnameSSLActive),
			},
			wantStatus:   types.DomainStatusError,
			wantVerified: false,
			wantProvider: types.TLSProviderCloudflareForSaaS,
		},
		{
			name:   "blocked hostname is an error",
			record: &types.CustomDomain{Domain: "cto.creatumundo.mx"},
			result: domainProvisioningResult{
				Mechanism:      mechanismCustomHostname,
				HostnameStatus: string(cloudflare.CustomHostnameStatusBlocked),
			},
			wantStatus:   types.DomainStatusError,
			wantVerified: false,
			wantProvider: types.TLSProviderCloudflareForSaaS,
		},
		{
			name:   "provisioning failure is stored, not swallowed",
			record: &types.CustomDomain{Domain: "cto.creatumundo.mx", Verified: true},
			result: func() domainProvisioningResult {
				r := domainProvisioningResult{Mechanism: mechanismCustomHostname}
				r.setErr(errors.New("cloudflare for saas is not configured"))
				return r
			}(),
			wantStatus:    types.DomainStatusError,
			wantVerified:  false,
			wantProvider:  types.TLSProviderCloudflareForSaaS,
			wantErrStored: true,
		},
		{
			name:   "zone path failure is recorded without touching the TLS provider",
			record: &types.CustomDomain{Domain: "api.madfam.io", TLSProvider: types.TLSProviderCertManager},
			result: func() domainProvisioningResult {
				r := domainProvisioningResult{Domain: "api.madfam.io", Mechanism: mechanismZoneCNAME}
				r.setErr(errors.New("failed to create DNS record"))
				return r
			}(),
			wantStatus:    types.DomainStatusError,
			wantVerified:  false,
			wantProvider:  types.TLSProviderCertManager,
			wantErrStored: true,
		},
		{
			name: "successful zone path leaves the record alone",
			record: &types.CustomDomain{
				Domain:      "api.madfam.io",
				TLSProvider: types.TLSProviderCertManager,
				Status:      types.DomainStatusActive,
				Verified:    true,
				VerifiedAt:  &now,
			},
			result:       domainProvisioningResult{Domain: "api.madfam.io", Mechanism: mechanismZoneCNAME},
			wantStatus:   types.DomainStatusActive,
			wantVerified: true,
			wantProvider: types.TLSProviderCertManager,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			applyProvisioningResult(tt.record, tt.result, now)

			if tt.record.Status != tt.wantStatus {
				t.Errorf("Status = %q, want %q", tt.record.Status, tt.wantStatus)
			}
			if tt.record.Verified != tt.wantVerified {
				t.Errorf("Verified = %v, want %v", tt.record.Verified, tt.wantVerified)
			}
			if tt.record.TLSProvider != tt.wantProvider {
				t.Errorf("TLSProvider = %q, want %q", tt.record.TLSProvider, tt.wantProvider)
			}
			if tt.wantErrStored && tt.record.ProvisioningError == "" {
				t.Error("ProvisioningError is empty; a failure must stay legible on the record")
			}
			if !tt.wantErrStored && tt.record.ProvisioningError != "" {
				t.Errorf("ProvisioningError = %q, want empty", tt.record.ProvisioningError)
			}
			if tt.record.ProvisioningCheckedAt == nil || !tt.record.ProvisioningCheckedAt.Equal(now) {
				t.Errorf("ProvisioningCheckedAt = %v, want %v", tt.record.ProvisioningCheckedAt, now)
			}
			if tt.wantVerified && tt.record.VerifiedAt == nil {
				t.Error("VerifiedAt is nil for a verified domain")
			}
		})
	}
}

func TestApplyProvisioningResultNilRecordIsSafe(t *testing.T) {
	applyProvisioningResult(nil, domainProvisioningResult{}, time.Now())
}

func TestDescribePendingClientAction(t *testing.T) {
	tests := []struct {
		name     string
		result   domainProvisioningResult
		contains []string
	}{
		{
			name: "waiting on the client lists every record",
			result: domainProvisioningResult{
				Domain:    "cto.creatumundo.mx",
				Mechanism: mechanismCustomHostname,
				PendingDNSRecords: []types.PendingDNSRecord{
					{Purpose: "routing", Type: "CNAME", Name: "cto.creatumundo.mx", Value: "proxy.enclii.dev"},
					{Purpose: "ownership", Type: "TXT", Name: "_cf-custom-hostname.cto.creatumundo.mx", Value: "token"},
				},
			},
			contains: []string{
				"2 DNS record(s)",
				"CNAME cto.creatumundo.mx -> proxy.enclii.dev",
				"TXT _cf-custom-hostname.cto.creatumundo.mx -> token",
			},
		},
		{
			name: "an error is reported verbatim",
			result: func() domainProvisioningResult {
				r := domainProvisioningResult{Domain: "cto.creatumundo.mx", Mechanism: mechanismCustomHostname}
				r.setErr(errors.New("cloudflare for saas is not configured"))
				return r
			}(),
			contains: []string{"Provisioning failed", "not configured"},
		},
		{
			name: "nothing outstanding reports the Cloudflare state",
			result: domainProvisioningResult{
				Domain:         "cto.creatumundo.mx",
				Mechanism:      mechanismCustomHostname,
				HostnameStatus: "active",
				SSLStatus:      "active",
			},
			contains: []string{"active"},
		},
		{
			name:     "zone path with nothing outstanding says nothing",
			result:   domainProvisioningResult{Domain: "api.madfam.io", Mechanism: mechanismZoneCNAME},
			contains: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := describePendingClientAction(tt.result)
			if len(tt.contains) == 0 {
				if got != "" {
					t.Errorf("describePendingClientAction() = %q, want empty", got)
				}
				return
			}
			for _, want := range tt.contains {
				if !contains(got, want) {
					t.Errorf("describePendingClientAction() = %q, want it to contain %q", got, want)
				}
			}
		})
	}
}

func TestToPendingDNSRecords(t *testing.T) {
	got := toPendingDNSRecords([]cloudflare.ClientDNSRecord{
		{Purpose: cloudflare.DNSRecordPurposeRouting, Type: "CNAME", Name: "a.client.com", Value: "proxy.enclii.dev"},
	})
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1", len(got))
	}
	want := types.PendingDNSRecord{Purpose: "routing", Type: "CNAME", Name: "a.client.com", Value: "proxy.enclii.dev"}
	if got[0] != want {
		t.Errorf("record = %+v, want %+v", got[0], want)
	}

	if toPendingDNSRecords(nil) != nil {
		t.Error("toPendingDNSRecords(nil) should be nil")
	}
}
