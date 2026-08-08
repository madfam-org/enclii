package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
	"github.com/sirupsen/logrus"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/cloudflare"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/config"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/db"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/services"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// Cross-project custom hostname ownership (HIGH-2, HIGH-3) plus the state
// hygiene the ownership checks depend on (LOW-2, LOW-4).
//
// The fallback-origin zone is shared by every tenant. Before these tests the
// only key anywhere in the provisioning and teardown paths was the hostname
// string, which any project could name.

// customDomainTestColumns mirrors db.customDomainSelectColumns; the db
// package's copy is unexported.
var customDomainTestColumns = []string{
	"id", "service_id", "environment_id", "domain", "verified",
	"tls_enabled", "tls_issuer", "created_at", "updated_at", "verified_at",
	"cloudflare_tunnel_id", "is_platform_domain", "zero_trust_enabled",
	"access_policy_id", "tls_provider", "status", "dns_cname",
	"custom_hostname_id", "custom_hostname_status", "custom_hostname_ssl_status",
	"pending_dns_records", "provisioning_error", "provisioning_checked_at",
}

var serviceTestColumns = []string{
	"id", "project_id", "name", "git_repo", "app_path", "build_config",
	"volumes", "auto_deploy", "auto_deploy_branch", "auto_deploy_env",
	"created_at", "updated_at", "jobs", "type", "region", "health_check",
}

// expectHostnameOwnedBy stages the two reads hostnameOwner performs: the
// custom_domains row for the hostname, and the service it belongs to.
func expectHostnameOwnedBy(mock sqlmock.Sqlmock, domain string, serviceID, projectID uuid.UUID, customHostnameID string) {
	now := time.Now()
	mock.ExpectQuery(`SELECT\s+id, service_id, environment_id, domain`).
		WithArgs(domain).
		WillReturnRows(sqlmock.NewRows(customDomainTestColumns).AddRow(
			uuid.New(), serviceID, uuid.New(), domain, true, true,
			"letsencrypt-prod", now, now, &now, nil, false, false, nil,
			"cloudflare-for-saas", "active", nil,
			customHostnameID, "active", "active", nil, "", now,
		))
	mock.ExpectQuery(`SELECT id, project_id, name, git_repo`).
		WithArgs(serviceID).
		WillReturnRows(sqlmock.NewRows(serviceTestColumns).AddRow(
			serviceID, projectID, "owner-svc", "madfam-org/owner", "", []byte(`{}`),
			[]byte(`[]`), false, "main", "production", now, now, []byte(`[]`),
			"web", "mx", []byte(`{}`),
		))
}

// expectHostnameUnclaimed stages a custom_domains lookup that finds nothing.
func expectHostnameUnclaimed(mock sqlmock.Sqlmock, domain string) {
	mock.ExpectQuery(`SELECT\s+id, service_id, environment_id, domain`).
		WithArgs(domain).
		WillReturnRows(sqlmock.NewRows(customDomainTestColumns))
}

func newSQLMockHandler(t *testing.T) (*Handler, sqlmock.Sqlmock, func()) {
	t.Helper()
	database, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	h := &Handler{
		logger: newNopLogger(),
		repos:  db.NewRepositories(database),
		config: &config.Config{
			CloudflareFallbackOriginZoneID:   "fallback-zone",
			CloudflareFallbackOriginHostname: "proxy.enclii.dev",
		},
	}
	return h, mock, func() { _ = database.Close() }
}

// newStubbedCloudflareHandler wires a Handler to a Cloudflare stub so the
// provisioning path runs end to end without touching the real API.
func newStubbedCloudflareHandler(t *testing.T, stub http.HandlerFunc) (*Handler, sqlmock.Sqlmock, func()) {
	t.Helper()

	server := httptest.NewServer(stub)
	cfClient, err := cloudflare.NewClient(&cloudflare.Config{
		APIToken:  "test-token",
		AccountID: "test-account",
		ZoneID:    "test-zone",
		TunnelID:  "test-tunnel",
		BaseURL:   server.URL,
	})
	if err != nil {
		server.Close()
		t.Fatalf("cloudflare.NewClient() error = %v", err)
	}

	h, mock, closeDB := newSQLMockHandler(t)
	logger := logrus.New()
	logger.SetOutput(io.Discard)
	h.domainSyncService = services.NewDomainSyncService(cfClient, h.repos, logger)

	return h, mock, func() {
		closeDB()
		server.Close()
	}
}

// HIGH-3(a): an existing registration for a hostname on the shared zone proves
// nothing about who owns it. Provisioning must refuse when another project
// holds the custom_domains record, instead of adopting the hostname and
// recording verified=true off the back of another project's certificate.
func TestEnsureCustomHostnameRefusesAnotherProjectsHostname(t *testing.T) {
	h, mock, cleanup := newSQLMockHandler(t)
	defer cleanup()

	ownerProject := uuid.New()
	expectHostnameOwnedBy(mock, "app.client.com", uuid.New(), ownerProject, "ch-owned")

	adopter := &domainOwner{ProjectID: uuid.New(), ServiceID: uuid.New()}
	result := h.ensureCustomHostname(context.Background(), "app.client.com", adopter)

	if result.Err == nil {
		t.Fatal("expected the claim to be refused, got nil error")
	}
	if !contains(result.ErrorMessage, ownerProject.String()) {
		t.Errorf("error = %q, want it to name the owning project %s", result.ErrorMessage, ownerProject)
	}
	if result.CustomHostnameID != "" {
		t.Errorf("CustomHostnameID = %q, want empty: nothing may be adopted", result.CustomHostnameID)
	}
	if result.HostnameStatus != "" || result.SSLStatus != "" {
		t.Errorf("status = (%q, %q), want empty: the other project's state must not be copied",
			result.HostnameStatus, result.SSLStatus)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sql expectations: %v", err)
	}
}

// The same hostname, claimed by the project that already owns it, goes through.
func TestEnsureCustomHostnameAllowsTheOwningProject(t *testing.T) {
	var listed bool
	h, mock, cleanup := newStubbedCloudflareHandler(t, func(w http.ResponseWriter, r *http.Request) {
		listed = true
		writeStubJSON(t, w, http.StatusOK, map[string]interface{}{
			"success": true,
			"result": []map[string]interface{}{{
				"id":       "ch-owned",
				"hostname": "app.client.com",
				"status":   "active",
				"ssl":      map[string]interface{}{"status": "active"},
			}},
			"result_info": map[string]interface{}{"total_pages": 1},
		})
	})
	defer cleanup()

	ownerProject := uuid.New()
	ownerService := uuid.New()
	// Once for the pre-check, once for the adoption guard.
	expectHostnameOwnedBy(mock, "app.client.com", ownerService, ownerProject, "ch-owned")
	expectHostnameOwnedBy(mock, "app.client.com", ownerService, ownerProject, "ch-owned")

	result := h.ensureCustomHostname(context.Background(), "app.client.com",
		&domainOwner{ProjectID: ownerProject, ServiceID: ownerService})

	if result.Err != nil {
		t.Fatalf("unexpected error for the owning project: %v", result.Err)
	}
	if !listed {
		t.Error("Cloudflare was never consulted")
	}
	if result.CustomHostnameID != "ch-owned" {
		t.Errorf("CustomHostnameID = %q, want ch-owned", result.CustomHostnameID)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sql expectations: %v", err)
	}
}

// An ownership lookup that fails must refuse, not assume the hostname is free.
func TestEnsureCustomHostnameFailsClosedWhenOwnershipIsUnknown(t *testing.T) {
	h, mock, cleanup := newSQLMockHandler(t)
	defer cleanup()

	mock.ExpectQuery(`SELECT\s+id, service_id, environment_id, domain`).
		WithArgs("app.client.com").
		WillReturnError(errors.New("connection reset by peer"))

	result := h.ensureCustomHostname(context.Background(), "app.client.com",
		&domainOwner{ProjectID: uuid.New(), ServiceID: uuid.New()})

	if result.Err == nil {
		t.Fatal("expected a refusal when ownership could not be established")
	}
	if result.CustomHostnameID != "" {
		t.Errorf("CustomHostnameID = %q, want empty", result.CustomHostnameID)
	}
}

// A hostname nobody holds may be claimed.
func TestEnsureCustomHostnameAllowsAnUnclaimedHostname(t *testing.T) {
	var created bool
	h, mock, cleanup := newStubbedCloudflareHandler(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			created = true
			writeStubJSON(t, w, http.StatusCreated, map[string]interface{}{
				"success": true,
				"result": map[string]interface{}{
					"id":       "ch-new",
					"hostname": "fresh.client.com",
					"status":   "pending",
					"ssl":      map[string]interface{}{"status": "pending_validation"},
				},
			})
			return
		}
		writeStubJSON(t, w, http.StatusOK, map[string]interface{}{
			"success":     true,
			"result":      []map[string]interface{}{},
			"result_info": map[string]interface{}{"total_pages": 1},
		})
	})
	defer cleanup()

	expectHostnameUnclaimed(mock, "fresh.client.com")

	result := h.ensureCustomHostname(context.Background(), "fresh.client.com",
		&domainOwner{ProjectID: uuid.New(), ServiceID: uuid.New()})

	if result.Err != nil {
		t.Fatalf("unexpected error: %v", result.Err)
	}
	if !created {
		t.Error("expected the hostname to be created")
	}
	if result.CustomHostnameID != "ch-new" {
		t.Errorf("CustomHostnameID = %q, want ch-new", result.CustomHostnameID)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sql expectations: %v", err)
	}
}

// HIGH-2(a): teardown resolved the hostname from the shared zone with no
// tenant identifier anywhere in the path, so deleting one project's junction
// could take another project's client domain offline.
func TestReleaseCustomHostnameRefusesAnotherProjectsHostname(t *testing.T) {
	var cloudflareCalled bool
	h, mock, cleanup := newStubbedCloudflareHandler(t, func(w http.ResponseWriter, r *http.Request) {
		cloudflareCalled = true
		writeStubJSON(t, w, http.StatusOK, map[string]interface{}{"success": true})
	})
	defer cleanup()

	ownerProject := uuid.New()
	expectHostnameOwnedBy(mock, "app.client.com", uuid.New(), ownerProject, "ch-owned")

	err := h.releaseCustomHostnameForProject(context.Background(), "app.client.com",
		&domainOwner{ProjectID: uuid.New(), ServiceID: uuid.New()})

	if err == nil {
		t.Fatal("expected the release to be refused")
	}
	if !contains(err.Error(), ownerProject.String()) {
		t.Errorf("error = %q, want it to name the owning project %s", err, ownerProject)
	}
	if cloudflareCalled {
		t.Error("Cloudflare was called; a refused release must not reach the API at all")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sql expectations: %v", err)
	}
}

func TestReleaseCustomHostnameDeletesForTheOwningProject(t *testing.T) {
	var deleted bool
	h, mock, cleanup := newStubbedCloudflareHandler(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			deleted = true
			writeStubJSON(t, w, http.StatusOK, map[string]interface{}{"id": "ch-owned"})
			return
		}
		writeStubJSON(t, w, http.StatusOK, map[string]interface{}{
			"success": true,
			"result": []map[string]interface{}{{
				"id": "ch-owned", "hostname": "app.client.com", "status": "active",
			}},
			"result_info": map[string]interface{}{"total_pages": 1},
		})
	})
	defer cleanup()

	ownerProject := uuid.New()
	ownerService := uuid.New()
	expectHostnameOwnedBy(mock, "app.client.com", ownerService, ownerProject, "ch-owned")

	err := h.releaseCustomHostnameForProject(context.Background(), "app.client.com",
		&domainOwner{ProjectID: ownerProject, ServiceID: ownerService})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !deleted {
		t.Error("the owning project's custom hostname was not deleted")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sql expectations: %v", err)
	}
}

func TestReleaseCustomHostnameRefusesWithoutAnOwner(t *testing.T) {
	h, _, cleanup := newStubbedCloudflareHandler(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("Cloudflare must not be called without an owning project")
	})
	defer cleanup()

	if err := h.releaseCustomHostnameForProject(context.Background(), "app.client.com", nil); err == nil {
		t.Fatal("expected a refusal when the requesting project is unknown")
	}
}

// HIGH-2(a), junction path: a hostname still served by another junction — on a
// different path, possibly in another project, because the uniqueness index is
// domain+path only — must not have its custom hostname torn down.
func TestReleaseCustomHostnameForJunctionKeepsHostnameStillInUse(t *testing.T) {
	h, mock, cleanup := newStubbedCloudflareHandler(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("Cloudflare must not be called while other junctions still serve this hostname")
	})
	defer cleanup()

	junction := &types.Junction{
		ID:        uuid.New(),
		ProjectID: uuid.New(),
		ServiceID: uuid.New(),
		Domain:    "app.client.com",
		Path:      "/api",
	}

	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM junctions WHERE domain = \$1 AND id <> \$2`).
		WithArgs(junction.Domain, junction.ID).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))

	if err := h.releaseCustomHostnameForJunction(context.Background(), junction); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sql expectations: %v", err)
	}
}

// HIGH-3(b): AddRoute overwrites an existing ingress rule for the same
// hostname, so a client-owned domain must clear its ownership check before any
// route is touched. Non-external domains keep the pre-existing order.
func TestProvisionDomainEdgeWithholdsTheRouteUntilOwnershipIsProven(t *testing.T) {
	t.Run("refused custom hostname leaves the ingress rule alone", func(t *testing.T) {
		h, mock, cleanup := newStubbedCloudflareHandler(t, func(w http.ResponseWriter, r *http.Request) {
			t.Error("Cloudflare must not be reached for a hostname another project owns")
		})
		defer cleanup()

		tunnelRoutes := newMockTunnelRoutesManager()
		existingNamespace := "victim"
		tunnelRoutes.routes["app.client.com"] = routeSpecFor("victim-web", existingNamespace)
		h.tunnelRoutesService = tunnelRoutes

		ownerProject := uuid.New()
		expectHostnameOwnedBy(mock, "app.client.com", uuid.New(), ownerProject, "ch-owned")
		// persistDomainProvisioningResult reloads the row to record the failure.
		expectHostnameOwnedBy(mock, "app.client.com", uuid.New(), ownerProject, "ch-owned")
		mock.ExpectQuery(`UPDATE custom_domains`).WillReturnRows(
			sqlmock.NewRows([]string{"updated_at"}).AddRow(time.Now()))

		attackerNamespace := "attacker"
		result := h.provisionDomainEdge(context.Background(), "app.client.com", &types.Service{
			ID:           uuid.New(),
			ProjectID:    uuid.New(),
			Name:         "attacker-web",
			K8sNamespace: &attackerNamespace,
		}, "production", 80, boolPtr(true))

		if result.Err == nil {
			t.Fatal("expected the custom hostname claim to be refused")
		}
		spec := tunnelRoutes.routes["app.client.com"]
		if spec == nil {
			t.Fatal("the pre-existing ingress rule disappeared")
		}
		if spec.ServiceName != "victim-web" || spec.ServiceNamespace != existingNamespace {
			t.Errorf("ingress rule was overwritten to %s/%s; it must be untouched",
				spec.ServiceNamespace, spec.ServiceName)
		}
	})

	t.Run("a non-external domain keeps the route-first order", func(t *testing.T) {
		h := &Handler{logger: newNopLogger(), config: &config.Config{}}
		tunnelRoutes := newMockTunnelRoutesManager()
		h.tunnelRoutesService = tunnelRoutes

		namespace := "tulana"
		h.provisionDomainEdge(context.Background(), "tulana-app.madfam.io", &types.Service{
			ID:           uuid.New(),
			ProjectID:    uuid.New(),
			Name:         "tulana-web",
			K8sNamespace: &namespace,
		}, "production", 80, nil)

		if tunnelRoutes.routes["tulana-app.madfam.io"] == nil {
			t.Error("the tunnel route for a non-external domain must still be added")
		}
	})
}

// MEDIUM-4: `external: true` before the operator configures the fallback
// origin reaches no Cloudflare call at all, so the domain gets no DNS record
// of any kind. It must not pass silently, and it must not fall back to the
// zone path either — that would point a client-owned domain at DNS we do not
// control.
func TestExternalDomainWithoutCloudflareFailsLoudlyAndDoesNotFallBack(t *testing.T) {
	h := &Handler{logger: newNopLogger(), config: &config.Config{}}

	plan := h.planDomainRouting(context.Background(), "app.client.com", boolPtr(true))
	if plan.mechanism != mechanismCustomHostname {
		t.Errorf("mechanism = %q, want %q: an external domain must never silently take the zone path",
			plan.mechanism, mechanismCustomHostname)
	}
	if plan.err == nil {
		t.Fatal("expected a legible error, got nil")
	}
	if plan.skip {
		t.Error("skip = true; an external domain that cannot be provisioned must not pass silently")
	}

	record := &types.CustomDomain{Domain: "app.client.com", Status: types.DomainStatusPending}
	result := domainProvisioningResult{Domain: "app.client.com", Mechanism: plan.mechanism}
	result.setErr(plan.err)
	applyProvisioningResult(record, result, time.Now())

	if record.ProvisioningError == "" {
		t.Error("ProvisioningError is empty; the record must carry the reason")
	}
	if record.Status != types.DomainStatusError {
		t.Errorf("Status = %q, want %q", record.Status, types.DomainStatusError)
	}

	// And the operator-facing read path must render it.
	info := domainInfoFor(record)
	if info.ProvisioningError == "" {
		t.Error("DomainInfo.ProvisioningError is empty; the failure would be invisible on the read path")
	}
}

// domainInfoFor mirrors the DomainInfo assembly in networking_handlers.go for
// the two fields this test cares about.
func domainInfoFor(domain *types.CustomDomain) types.DomainInfo {
	return types.DomainInfo{
		Domain:                domain.Domain,
		Status:                domain.Status,
		ProvisioningError:     domain.ProvisioningError,
		ProvisioningCheckedAt: domain.ProvisioningCheckedAt,
		PendingDNSRecords:     domain.PendingDNSRecords,
	}
}

// LOW-2: a domain that flips from `external: true` back to the zone path keeps
// a non-empty CustomHostnameID, on which cleanupDomainsForService branches and
// skips the zone DNS deletion. The id may only be cleared once Cloudflare has
// confirmed the registration was released.
func TestZonePathClearsCustomHostnameStateOnlyAfterRelease(t *testing.T) {
	now := time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)

	t.Run("release confirmed clears the state", func(t *testing.T) {
		record := &types.CustomDomain{
			Domain:                  "app.example.com",
			TLSProvider:             types.TLSProviderCloudflareForSaaS,
			Status:                  types.DomainStatusActive,
			Verified:                true,
			CustomHostnameID:        "ch-stale",
			CustomHostnameStatus:    "active",
			CustomHostnameSSLStatus: "active",
			PendingDNSRecords: []types.PendingDNSRecord{
				{Purpose: "ownership", Type: "TXT", Name: "_cf-custom-hostname.app.example.com", Value: "token"},
			},
		}

		applyProvisioningResult(record, domainProvisioningResult{
			Domain:                 "app.example.com",
			Mechanism:              mechanismZoneCNAME,
			CustomHostnameReleased: true,
		}, now)

		if record.CustomHostnameID != "" {
			t.Errorf("CustomHostnameID = %q, want empty", record.CustomHostnameID)
		}
		if record.CustomHostnameStatus != "" || record.CustomHostnameSSLStatus != "" {
			t.Errorf("hostname statuses = (%q, %q), want empty",
				record.CustomHostnameStatus, record.CustomHostnameSSLStatus)
		}
		if record.PendingDNSRecords != nil {
			t.Errorf("PendingDNSRecords = %+v, want nil", record.PendingDNSRecords)
		}
		if record.TLSProvider != types.TLSProviderCertManager {
			t.Errorf("TLSProvider = %q, want %q", record.TLSProvider, types.TLSProviderCertManager)
		}
	})

	t.Run("release not confirmed keeps the id so teardown can retry", func(t *testing.T) {
		record := &types.CustomDomain{
			Domain:           "app.example.com",
			TLSProvider:      types.TLSProviderCloudflareForSaaS,
			Status:           types.DomainStatusActive,
			CustomHostnameID: "ch-stale",
		}

		applyProvisioningResult(record, domainProvisioningResult{
			Domain:    "app.example.com",
			Mechanism: mechanismZoneCNAME,
		}, now)

		if record.CustomHostnameID != "ch-stale" {
			t.Errorf("CustomHostnameID = %q, want it kept: the registration was never released",
				record.CustomHostnameID)
		}
	})
}

// LOW-2, second half: cleanupDomainsForService used to `continue` after
// releasing a custom hostname, so a domain carrying both a hostname id and a
// proxied CNAME in one of our zones kept the CNAME pointing at the tunnel
// forever.
func TestCleanupDomainsForServiceDeletesTheZoneRecordToo(t *testing.T) {
	var deletedHostname, deletedDNSRecord bool

	h, mock, cleanup := newStubbedCloudflareHandler(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodDelete && contains(r.URL.Path, "/custom_hostnames/"):
			deletedHostname = true
			writeStubJSON(t, w, http.StatusOK, map[string]interface{}{"id": "ch-stale"})
		case r.Method == http.MethodDelete && contains(r.URL.Path, "/dns_records/"):
			deletedDNSRecord = true
			writeStubJSON(t, w, http.StatusOK, map[string]interface{}{
				"success": true,
				"result":  map[string]interface{}{"id": "rec-1"},
			})
		case r.URL.Path == "/zones":
			writeStubJSON(t, w, http.StatusOK, map[string]interface{}{
				"success": true,
				"result": []map[string]interface{}{
					{"id": "zone-1", "name": "example.com", "status": "active"},
				},
				"result_info": map[string]interface{}{"total_pages": 1},
			})
		default:
			// DNS record listing for the zone.
			writeStubJSON(t, w, http.StatusOK, map[string]interface{}{
				"success": true,
				"result": []map[string]interface{}{
					{"id": "rec-1", "type": "CNAME", "name": "app.example.com", "content": "tunnel.enclii.dev"},
				},
				"result_info": map[string]interface{}{"total_pages": 1},
			})
		}
	})
	defer cleanup()

	h.tunnelRoutesService = newMockTunnelRoutesManager()

	serviceID := uuid.New()
	now := time.Now()
	mock.ExpectQuery(`SELECT\s+id, service_id, environment_id, domain`).
		WithArgs(serviceID.String()).
		WillReturnRows(sqlmock.NewRows(customDomainTestColumns).AddRow(
			uuid.New(), serviceID, uuid.New(), "app.example.com", true, true,
			"letsencrypt-prod", now, now, &now, nil, false, false, nil,
			"cloudflare-for-saas", "active", nil,
			"ch-stale", "active", "active", nil, "", now,
		))

	h.cleanupDomainsForService(context.Background(), serviceID)

	if !deletedHostname {
		t.Error("the custom hostname was not released")
	}
	if !deletedDNSRecord {
		t.Error("the zone DNS record was not deleted; a proxied CNAME to the tunnel would be left dangling")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sql expectations: %v", err)
	}
}

// LOW-4: the ownership token and the DCV challenge value are what prove
// control of a hostname. They belong in the authenticated API response, never
// in a log line.
func TestPendingClientRecordNamesOmitValues(t *testing.T) {
	result := domainProvisioningResult{
		Domain:    "app.client.com",
		Mechanism: mechanismCustomHostname,
		PendingDNSRecords: []types.PendingDNSRecord{
			{Purpose: "routing", Type: "CNAME", Name: "app.client.com", Value: "proxy.enclii.dev"},
			{Purpose: "ownership", Type: "TXT", Name: "_cf-custom-hostname.app.client.com", Value: "ownership-secret"},
			{Purpose: "ssl_validation", Type: "TXT", Name: "_acme-challenge.app.client.com", Value: "dcv-secret"},
		},
	}

	logged := describePendingClientRecordNames(result)
	for _, secret := range []string{"ownership-secret", "dcv-secret", "proxy.enclii.dev"} {
		if contains(logged, secret) {
			t.Errorf("log rendering %q leaks the record value %q", logged, secret)
		}
	}
	for _, name := range []string{"_cf-custom-hostname.app.client.com", "_acme-challenge.app.client.com"} {
		if !contains(logged, name) {
			t.Errorf("log rendering %q should still name the record %q", logged, name)
		}
	}
	if !contains(logged, "3 DNS record(s)") {
		t.Errorf("log rendering %q should carry the count", logged)
	}

	// The authenticated response still carries the values the client needs.
	if !contains(describePendingClientAction(result), "ownership-secret") {
		t.Error("the client-facing message must still carry the record values")
	}
}

func routeSpecFor(serviceName, namespace string) *services.RouteSpec {
	return &services.RouteSpec{
		Hostname:         "app.client.com",
		ServiceName:      serviceName,
		ServiceNamespace: namespace,
		ServicePort:      80,
	}
}

func writeStubJSON(t *testing.T, w http.ResponseWriter, status int, body interface{}) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(body); err != nil {
		t.Fatalf("failed to encode stub response: %v", err)
	}
}
