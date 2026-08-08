package api

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/cloudflare"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/config"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// stubZoneResolver answers the one question the mechanism decision asks.
type stubZoneResolver struct {
	zonesWeControl map[string]bool
	// failure, when set, is returned for every domain not in zonesWeControl.
	// It stands in for a transport/HTTP/pagination failure — the case that
	// must never be read as "this domain is client-owned".
	failure error
	calls   int
}

func (s *stubZoneResolver) FindZoneForDomain(ctx context.Context, domain string) (*cloudflare.Zone, error) {
	s.calls++
	if s.zonesWeControl[domain] {
		return &cloudflare.Zone{ID: "zone-" + domain, Name: domain}, nil
	}
	if s.failure != nil {
		return nil, s.failure
	}
	return nil, fmt.Errorf("%w: %s", cloudflare.ErrZoneNotFound, domain)
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
		wantErr    bool
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
		{
			// HIGH-1: the case that reroutes a MADFAM domain. A 500/429/
			// timeout/token blip is not "no zone", and must not be read as
			// one.
			name:    "a transport failure is undetermined, never client-owned",
			handler: saasConfiguredHandler(),
			zones: &stubZoneResolver{
				zonesWeControl: map[string]bool{"app.dhan.am": true},
				failure:        errors.New("cloudflare: HTTP error 500: internal server error"),
			},
			domain:     "app.dhan.am.example",
			want:       mechanismUndetermined,
			wantErr:    true,
			wantLookup: true,
		},
		{
			name:    "a rate-limited lookup is undetermined",
			handler: saasConfiguredHandler(),
			zones: &stubZoneResolver{
				failure: &cloudflare.APIError{Code: 10000, Message: "rate limited"},
			},
			domain:     "app.dhan.am",
			want:       mechanismUndetermined,
			wantErr:    true,
			wantLookup: true,
		},
		{
			// A zone we hold but Cloudflare is not serving is still ours, so
			// the domain must not be moved onto the custom-hostname path.
			name:    "a zone that exists but is not active is undetermined",
			handler: saasConfiguredHandler(),
			zones: &stubZoneResolver{
				failure: &cloudflare.ZoneNotActiveError{
					Domain: "app.dhan.am", ZoneName: "dhan.am", Status: "pending",
				},
			},
			domain:     "app.dhan.am",
			want:       mechanismUndetermined,
			wantErr:    true,
			wantLookup: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.handler.resolveDomainMechanism(context.Background(), tt.zones, tt.domain, tt.external)
			if got != tt.want {
				t.Errorf("mechanism = %q, want %q", got, tt.want)
			}
			if tt.wantErr && err == nil {
				t.Error("expected an error for an undecidable lookup, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("unexpected error: %v", err)
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

// TestApplyProvisioningResultUndeterminedLeavesRecordUntouched is the second
// half of the HIGH-1 regression: even once the undecidable lookup is
// classified, the live record must survive it. Before the fix a transient
// Cloudflare error rewrote a serving domain to
// tls_provider=cloudflare-for-saas / verified=false / status=pending.
func TestApplyProvisioningResultUndeterminedLeavesRecordUntouched(t *testing.T) {
	now := time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)
	verifiedAt := now.Add(-72 * time.Hour)

	record := &types.CustomDomain{
		Domain:      "app.dhan.am",
		TLSProvider: types.TLSProviderCertManager,
		Status:      types.DomainStatusActive,
		Verified:    true,
		VerifiedAt:  &verifiedAt,
		DNSCNAME:    "tunnel.enclii.dev",
	}

	result := domainProvisioningResult{Domain: "app.dhan.am", Mechanism: mechanismUndetermined}
	result.setErr(errors.New("cloudflare: HTTP error 500: internal server error"))

	applyProvisioningResult(record, result, now)

	if record.TLSProvider != types.TLSProviderCertManager {
		t.Errorf("TLSProvider = %q, want it untouched (%q)", record.TLSProvider, types.TLSProviderCertManager)
	}
	if record.Status != types.DomainStatusActive {
		t.Errorf("Status = %q, want it untouched (%q)", record.Status, types.DomainStatusActive)
	}
	if !record.Verified {
		t.Error("Verified = false; an undecidable lookup must not un-verify a live domain")
	}
	if record.VerifiedAt == nil || !record.VerifiedAt.Equal(verifiedAt) {
		t.Errorf("VerifiedAt = %v, want it untouched (%v)", record.VerifiedAt, verifiedAt)
	}
	if record.CustomHostnameID != "" {
		t.Errorf("CustomHostnameID = %q, want empty", record.CustomHostnameID)
	}
	if record.DNSCNAME != "tunnel.enclii.dev" {
		t.Errorf("DNSCNAME = %q, want it untouched", record.DNSCNAME)
	}

	// The diagnosis is still recorded, so the failure is not silent.
	if record.ProvisioningError == "" {
		t.Error("ProvisioningError is empty; the undecidable lookup must stay legible")
	}
	if record.ProvisioningCheckedAt == nil || !record.ProvisioningCheckedAt.Equal(now) {
		t.Errorf("ProvisioningCheckedAt = %v, want %v", record.ProvisioningCheckedAt, now)
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

	result := h.ensureCustomHostname(context.Background(), "cto.creatumundo.mx", &domainOwner{ProjectID: uuid.New()})
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
		owner := &domainOwner{ProjectID: uuid.New()}
		if err := h.releaseCustomHostnameForProject(context.Background(), "cto.creatumundo.mx", owner); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("by-domain teardown is a no-op without a cloudflare client", func(t *testing.T) {
		h := saasConfiguredHandler()
		owner := &domainOwner{ProjectID: uuid.New()}
		if err := h.releaseCustomHostnameForProject(context.Background(), "cto.creatumundo.mx", owner); err != nil {
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
		{
			// MEDIUM-1: the zone path writes to the record, so its writes have
			// to be reversible. A success after a failure must clear both the
			// error text and the error status, or one bad minute pins the
			// domain to status=error forever.
			name: "a successful zone pass clears the error a previous pass wrote",
			record: &types.CustomDomain{
				Domain:            "api.madfam.io",
				TLSProvider:       types.TLSProviderCertManager,
				Status:            types.DomainStatusError,
				Verified:          true,
				VerifiedAt:        &now,
				ProvisioningError: "failed to create DNS record for api.madfam.io: cloudflare: HTTP error 500",
			},
			result:       domainProvisioningResult{Domain: "api.madfam.io", Mechanism: mechanismZoneCNAME},
			wantStatus:   types.DomainStatusActive,
			wantVerified: true,
			wantProvider: types.TLSProviderCertManager,
		},
		{
			name: "a successful zone pass on an unverified domain returns it to pending, not error",
			record: &types.CustomDomain{
				Domain:            "api.madfam.io",
				TLSProvider:       types.TLSProviderCertManager,
				Status:            types.DomainStatusError,
				Verified:          false,
				ProvisioningError: "failed to create DNS record",
			},
			result:       domainProvisioningResult{Domain: "api.madfam.io", Mechanism: mechanismZoneCNAME},
			wantStatus:   types.DomainStatusPending,
			wantVerified: false,
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
