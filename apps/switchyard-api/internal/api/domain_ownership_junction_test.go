package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// BROKEN-3(1): junction-provisioned hostnames had no resolvable owner.
//
// assertHostnameClaimableBy keyed entirely off custom_domains, and a junction
// never writes one — persistDomainProvisioningResult is a documented no-op
// without a row. A hostname served through a junction therefore read as
// unowned, which is the one answer that lets another project adopt it: the
// ownership check "passed", and AddRoute then overwrote the victim's ingress
// rule to the attacker's service.
func TestCustomHostnameOwnershipSeesJunctionProvisionedHostnames(t *testing.T) {
	victimProject := uuid.New()

	t.Run("another project's junction blocks the claim", func(t *testing.T) {
		h, mock, cleanup := newStubbedCloudflareHandler(t, func(w http.ResponseWriter, r *http.Request) {
			t.Error("Cloudflare must not be reached for a hostname another project's junction serves")
		})
		defer cleanup()

		// No custom_domains row anywhere — the junction is the only record.
		mock.ExpectQuery(`SELECT\s+id, service_id, environment_id, domain`).
			WithArgs("app.client.com").
			WillReturnRows(sqlmock.NewRows(customDomainTestColumns))
		expectJunctionOwners(mock, "app.client.com", victimProject)

		result := h.ensureCustomHostname(context.Background(), "app.client.com",
			&domainOwner{ProjectID: uuid.New(), ServiceID: uuid.New()})

		if result.Err == nil {
			t.Fatal("expected the claim to be refused: the hostname is served by another project's junction")
		}
		if !contains(result.ErrorMessage, victimProject.String()) {
			t.Errorf("error = %q, want it to name the owning project %s", result.ErrorMessage, victimProject)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("unmet sql expectations: %v", err)
		}
	})

	t.Run("the project's own junction is its proof of ownership", func(t *testing.T) {
		var listed bool
		h, mock, cleanup := newStubbedCloudflareHandler(t, func(w http.ResponseWriter, r *http.Request) {
			listed = true
			writeStubJSON(t, w, http.StatusOK, map[string]interface{}{
				"success": true,
				"result": []map[string]interface{}{{
					"id": "ch-ours", "hostname": "app.client.com", "status": "active",
					"ssl": map[string]interface{}{"status": "active"},
				}},
				"result_info": map[string]interface{}{"total_pages": 1},
			})
		})
		defer cleanup()

		// Pre-check and adoption guard each perform the lookup.
		for i := 0; i < 2; i++ {
			mock.ExpectQuery(`SELECT\s+id, service_id, environment_id, domain`).
				WithArgs("app.client.com").
				WillReturnRows(sqlmock.NewRows(customDomainTestColumns))
			expectJunctionOwners(mock, "app.client.com", victimProject)
		}

		result := h.ensureCustomHostname(context.Background(), "app.client.com",
			&domainOwner{ProjectID: victimProject, ServiceID: uuid.New()})

		if result.Err != nil {
			t.Fatalf("the junction's own project was refused: %v", result.Err)
		}
		if !listed {
			t.Error("Cloudflare was never consulted")
		}
		if result.CustomHostnameID != "ch-ours" {
			t.Errorf("CustomHostnameID = %q, want ch-ours", result.CustomHostnameID)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("unmet sql expectations: %v", err)
		}
	})
}

// The same defect at the level that actually causes the damage: the victim's
// ACTIVE junction-served hostname must keep its ingress rule.
func TestProvisionDomainEdgeWithholdsTheRouteFromAJunctionServedHostname(t *testing.T) {
	h, mock, cleanup := newStubbedCloudflareHandler(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("Cloudflare must not be reached for a hostname another project's junction serves")
	})
	defer cleanup()

	tunnelRoutes := newMockTunnelRoutesManager()
	tunnelRoutes.routes["app.client.com"] = routeSpecFor("victim-web", "victim")
	h.tunnelRoutesService = tunnelRoutes

	victimProject := uuid.New()
	attackerService := uuid.New()
	// ensureCustomHostname's pre-check.
	mock.ExpectQuery(`SELECT\s+id, service_id, environment_id, domain`).
		WithArgs("app.client.com").
		WillReturnRows(sqlmock.NewRows(customDomainTestColumns))
	expectJunctionOwners(mock, "app.client.com", victimProject)
	// persistDomainProvisioningResult reloads only the ATTACKER's own rows for
	// this hostname. There are none, so it is a no-op and no UPDATE follows —
	// and, critically, the victim's junction-served hostname is never even
	// selected for.
	expectOwnRowLookup(mock, "app.client.com", attackerService, noCustomDomainRows())

	attackerNamespace := "attacker"
	result := h.provisionDomainEdge(context.Background(), "app.client.com", &types.Service{
		ID:           attackerService,
		ProjectID:    uuid.New(),
		Name:         "attacker-web",
		K8sNamespace: &attackerNamespace,
	}, "production", 80, boolPtr(true))

	if result.Err == nil {
		t.Fatal("expected the claim on a junction-served hostname to be refused")
	}
	spec := tunnelRoutes.routes["app.client.com"]
	if spec == nil {
		t.Fatal("the victim's ingress rule disappeared")
	}
	if spec.ServiceName != "victim-web" || spec.ServiceNamespace != "victim" {
		t.Errorf("ingress rule was overwritten to %s/%s; it must be untouched",
			spec.ServiceNamespace, spec.ServiceName)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sql expectations: %v", err)
	}
}

// BROKEN-3(2): mechanismUndetermined fell through to the route-first branch,
// and applyDomainRouting short-circuits on plan.err without ever checking
// ownership. An auto-detect domain whose zone lookup failed transiently
// therefore had the victim's ingress rule overwritten BEFORE, and INSTEAD OF,
// any ownership check. An undetermined mechanism must mutate nothing.
func TestProvisionDomainEdgeMutatesNothingWhenTheMechanismIsUndetermined(t *testing.T) {
	h, mock, cleanup := newStubbedCloudflareHandler(t, func(w http.ResponseWriter, r *http.Request) {
		// Every zone lookup fails transiently: not "no zone", just unknown.
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("internal server error"))
	})
	defer cleanup()

	tunnelRoutes := newMockTunnelRoutesManager()
	tunnelRoutes.routes["app.client.com"] = routeSpecFor("victim-web", "victim")
	h.tunnelRoutesService = tunnelRoutes

	// Only the persistence reload runs, scoped to the attacker's own service;
	// no ownership rewrite, no UPDATE.
	attackerService := uuid.New()
	expectOwnRowLookup(mock, "app.client.com", attackerService, noCustomDomainRows())

	attackerNamespace := "attacker"
	result := h.provisionDomainEdge(context.Background(), "app.client.com", &types.Service{
		ID:           attackerService,
		ProjectID:    uuid.New(),
		Name:         "attacker-web",
		K8sNamespace: &attackerNamespace,
	}, "production", 80, nil)

	if result.Mechanism != mechanismUndetermined {
		t.Fatalf("mechanism = %q, want %q", result.Mechanism, mechanismUndetermined)
	}
	if result.Err == nil {
		t.Error("an undetermined mechanism must carry its reason")
	}

	spec := tunnelRoutes.routes["app.client.com"]
	if spec == nil {
		t.Fatal("the pre-existing ingress rule disappeared")
	}
	if spec.ServiceName != "victim-web" || spec.ServiceNamespace != "victim" {
		t.Errorf("ingress rule was overwritten to %s/%s on an undecidable lookup; it must be untouched",
			spec.ServiceNamespace, spec.ServiceName)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sql expectations: %v", err)
	}
}

// The record for an undetermined pass keeps every lifecycle field and gains
// only the diagnosis — already covered for applyProvisioningResult, asserted
// here through the whole edge pass.
func TestUndeterminedMechanismRecordsOnlyTheDiagnosis(t *testing.T) {
	record := &types.CustomDomain{
		Domain:      "app.client.com",
		Status:      types.DomainStatusActive,
		Verified:    true,
		TLSProvider: types.TLSProviderCertManager,
	}
	result := domainProvisioningResult{Domain: "app.client.com", Mechanism: mechanismUndetermined}
	result.setErr(errors.New("cloudflare: HTTP error 500"))

	applyProvisioningResult(record, result, time.Now())

	if record.Status != types.DomainStatusActive || !record.Verified {
		t.Errorf("Status/Verified = (%q, %v), want them untouched", record.Status, record.Verified)
	}
	if record.TLSProvider != types.TLSProviderCertManager {
		t.Errorf("TLSProvider = %q, want it untouched", record.TLSProvider)
	}
	if record.ProvisioningError == "" {
		t.Error("the diagnosis was not recorded")
	}
}

// BROKEN-2 second half: releaseCustomHostnameForProject proceeded when
// held == false, so a hostname with no owning custom_domains row could be
// released by anyone naming it — and junction-provisioned hostnames never get
// a row. "Nobody owns it" is not permission on a zone shared by every tenant.
func TestReleaseCustomHostnameRefusesAnUnownedHostname(t *testing.T) {
	h, mock, cleanup := newStubbedCloudflareHandler(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("Cloudflare must not be called for a hostname no record entitles the caller to")
	})
	defer cleanup()

	requester := uuid.New()
	expectHostnameUnclaimed(mock, "app.victim.com")

	err := h.releaseCustomHostnameForProject(context.Background(), "app.victim.com",
		&domainOwner{ProjectID: requester, ServiceID: uuid.New()})

	if err == nil {
		t.Fatal("expected the release to be refused when no record establishes ownership")
	}
	if !contains(err.Error(), requester.String()) {
		t.Errorf("error = %q, want it to name the requesting project", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sql expectations: %v", err)
	}
}

// Failing closed on "no owner" must not break the legitimate junction
// teardown: the junction row is still present when the release runs, and it is
// what names the owner.
func TestReleaseCustomHostnameForJunctionStillReleasesTheOwnersHostname(t *testing.T) {
	var deleted bool
	h, mock, cleanup := newStubbedCloudflareHandler(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			deleted = true
			writeStubJSON(t, w, http.StatusOK, map[string]interface{}{"id": "ch-ours"})
			return
		}
		writeStubJSON(t, w, http.StatusOK, map[string]interface{}{
			"success": true,
			"result": []map[string]interface{}{{
				"id": "ch-ours", "hostname": "app.client.com", "status": "active",
			}},
			"result_info": map[string]interface{}{"total_pages": 1},
		})
	})
	defer cleanup()

	junction := &types.Junction{
		ID:        uuid.New(),
		ProjectID: uuid.New(),
		ServiceID: uuid.New(),
		Domain:    "app.client.com",
		Path:      "/",
	}

	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM junctions WHERE lower\(domain\) = lower\(\$1\) AND id <> \$2`).
		WithArgs(junction.Domain, junction.ID).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
	mock.ExpectQuery(`SELECT\s+id, service_id, environment_id, domain`).
		WithArgs(junction.Domain).
		WillReturnRows(sqlmock.NewRows(customDomainTestColumns))
	expectJunctionOwners(mock, junction.Domain, junction.ProjectID)

	if err := h.releaseCustomHostnameForJunction(context.Background(), junction); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !deleted {
		t.Error("the owning project's custom hostname was not released")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sql expectations: %v", err)
	}
}

// The declaration-time gate: AddCustomDomain's only ownership test was
// CustomDomains.Exists, which cannot see a junction. Claiming a
// junction-served hostname there does not merely overwrite the victim's
// routing — it mints a second, competing ownership record, after which the
// RIGHTFUL owner's own provisioning is refused too.
func TestAddCustomDomain_RefusesAJunctionServedHostname(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h, mock, cleanup := newSQLMockHandler(t)
	defer cleanup()

	attackerProject := uuid.New()
	attackerService := uuid.New()
	victimProject := uuid.New()
	envID := uuid.New()
	now := time.Now()

	mock.ExpectQuery(`SELECT id, project_id, name, git_repo`).
		WithArgs(attackerService).
		WillReturnRows(sqlmock.NewRows(serviceTestColumns).AddRow(
			attackerService, attackerProject, "attacker-web", "madfam-org/attacker", "", []byte(`{}`),
			[]byte(`[]`), false, "main", "production", now, now, []byte(`[]`),
			"web", "mx", []byte(`{}`),
		))
	mock.ExpectQuery(`FROM environments WHERE project_id = \$1 AND name = \$2`).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "project_id", "name", "kube_namespace", "created_at", "updated_at",
		}).AddRow(envID, attackerProject, "production", "attacker", now, now))
	// No custom_domains row exists for the hostname, which is exactly the state
	// a junction-provisioned hostname is in.
	mock.ExpectQuery(`SELECT EXISTS\(SELECT 1 FROM custom_domains WHERE lower\(domain\) = lower\(\$1\)\)`).
		WithArgs("app.victim.com").
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))

	// The check and the claim now run in one transaction under the
	// cross-project hostname lock, so the ownership lookup is staged INSIDE it.
	expectClaimTransaction(mock, "app.victim.com")
	expectHostnameUnclaimedExceptJunction(mock, "app.victim.com", victimProject)

	// Nothing may follow: no INSERT, and the transaction rolls back.
	mock.ExpectRollback()

	w := httptest.NewRecorder()
	_, engine := gin.CreateTestContext(w)
	engine.POST("/v1/services/:id/domains", h.AddCustomDomain)

	body := `{"domain":"App.Victim.com","environment":"production"}`
	req, _ := http.NewRequest(http.MethodPost, "/v1/services/"+attackerService.String()+"/domains",
		strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	engine.ServeHTTP(w, req)

	if w.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d; body = %s", w.Code, http.StatusConflict, w.Body.String())
	}
	if !contains(w.Body.String(), victimProject.String()) {
		t.Errorf("body = %s, want it to name the owning project", w.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sql expectations: %v", err)
	}
}

// expectHostnameUnclaimedExceptJunction stages the ownership lookup for a
// hostname whose ONLY record is another project's junction.
func expectHostnameUnclaimedExceptJunction(mock sqlmock.Sqlmock, domain string, projectID uuid.UUID) {
	mock.ExpectQuery(`SELECT\s+id, service_id, environment_id, domain`).
		WithArgs(domain).
		WillReturnRows(sqlmock.NewRows(customDomainTestColumns))
	expectJunctionOwners(mock, domain, projectID)
}

// BROKEN-2: the attacker's route in was a case variant. `App.Victim.com` was a
// different hostname to Postgres (plain btree, `WHERE domain = $1`) and the
// same hostname to Cloudflare (strings.EqualFold), so the attacker could hold a
// junction for it on their OWN project — passing enforceUserProjectAccess
// legitimately — and delete the victim's registration at the edge.
//
// Canonicalisation closes it at the front door: the variant is now the same
// hostname, and CreateJunction refuses it because another project holds it.
func TestCreateJunction_RefusesACaseVariantOfAnotherProjectsHostname(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h, mock, cleanup := newSQLMockHandler(t)
	defer cleanup()

	attackerProject := uuid.New()
	attackerService := uuid.New()
	victimProject := uuid.New()
	victimService := uuid.New()
	now := time.Now()

	mock.ExpectQuery(`FROM projects WHERE slug`).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "name", "slug", "ci_runner_mode", "created_at", "updated_at",
		}).AddRow(attackerProject, "attacker", "attacker", "shared", now, now))
	// ensureDefaultProductionEnvironment
	mock.ExpectQuery(`FROM environments`).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "project_id", "name", "kube_namespace", "created_at", "updated_at",
		}).AddRow(uuid.New(), attackerProject, "production", "attacker", now, now))
	mock.ExpectQuery(`SELECT id, project_id, name, git_repo`).
		WithArgs(attackerService).
		WillReturnRows(sqlmock.NewRows(serviceTestColumns).AddRow(
			attackerService, attackerProject, "attacker-web", "madfam-org/attacker", "", []byte(`{}`),
			[]byte(`[]`), false, "main", "production", now, now, []byte(`[]`),
			"web", "mx", []byte(`{}`),
		))
	// Uniqueness: the attacker picked a different path, so this is a clean miss.
	mock.ExpectQuery(`SELECT EXISTS\(SELECT 1 FROM junctions WHERE lower\(domain\) = lower\(\$1\) AND path = \$2\)`).
		WithArgs("app.victim.com", "/attacker").
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))
	// Ownership: the victim's custom_domains row, found because the lookup is
	// now case-insensitive AND the request was canonicalised.
	expectHostnameOwnedBy(mock, "app.victim.com", victimService, victimProject, "ch-victim")

	// Nothing may follow: no INSERT, no provisioning. sqlmock is strict.

	w := httptest.NewRecorder()
	_, engine := gin.CreateTestContext(w)
	engine.POST("/v1/projects/:slug/junctions", h.CreateJunction)

	body := `{"service_id":"` + attackerService.String() + `","domain":"App.Victim.com","path":"/attacker"}`
	req, _ := http.NewRequest(http.MethodPost, "/v1/projects/attacker/junctions",
		strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	engine.ServeHTTP(w, req)

	if w.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d; body = %s", w.Code, http.StatusConflict, w.Body.String())
	}
	if !contains(w.Body.String(), victimProject.String()) {
		t.Errorf("body = %s, want it to name the owning project", w.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sql expectations: %v", err)
	}
}
