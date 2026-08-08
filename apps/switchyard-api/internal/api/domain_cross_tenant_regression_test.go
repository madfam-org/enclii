package api

// Round-4 cross-tenant regressions.
//
// Each test here fails against the code as it stood at c3f2a3e4. They are
// grouped by the defect they pin rather than by the function they call, because
// the defects were only reachable as chains: a multi-row predicate resolved with
// QueryRow, an ownership check ordered after a configuration guard, and a
// persistence step keyed on a hostname string instead of an owner.

import (
	"context"
	"database/sql/driver"
	"net/http"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/config"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// customDomainRow renders one custom_domains row in customDomainTestColumns
// order.
func customDomainRow(serviceID uuid.UUID, domain, customHostnameID string, verified bool) []driver.Value {
	now := time.Now()
	status := "pending"
	if verified {
		status = "active"
	}
	return append([]driver.Value{
		uuid.New(), serviceID, uuid.New(), domain, verified, true,
		"letsencrypt-prod", now, now, &now, nil, false, false, nil,
		"cloudflare-for-saas", status, nil,
	}, customHostnameID, status, status, nil, "", now)
}

// expectServiceLookup stages the services row hostnameOwners reads to turn a
// custom_domains row into an owning project.
func expectServiceLookup(mock sqlmock.Sqlmock, serviceID, projectID uuid.UUID, name string) {
	now := time.Now()
	mock.ExpectQuery(`SELECT id, project_id, name, git_repo`).
		WithArgs(serviceID).
		WillReturnRows(sqlmock.NewRows(serviceTestColumns).AddRow(
			serviceID, projectID, name, "madfam-org/"+name, "", []byte(`{}`),
			[]byte(`[]`), false, "main", "production", now, now, []byte(`[]`),
			"web", "mx", []byte(`{}`),
		))
}

// BROKEN-2a, second half.
//
// `WHERE lower(domain) = lower($1)` is a multi-row predicate: custom_domains is
// unique only per (service, environment, domain), so two projects can hold rows
// for one hostname. Resolving it with QueryRowContext returned whichever tuple
// the heap yielded first and discarded the rest — and migration 034's
// `SET domain = lower(domain)` rewrites a row as a new heap version, which is
// precisely what moved the victim's tuple behind the attacker's.
//
// The consequence measured against the real handler was not a wrong log line:
// hostnameOwners reported ONLY the attacker, so releaseCustomHostnameForProject
// found no foreign owner and deleted the victim's live custom hostname.
func TestHostnameOwnersSeesEveryProjectHoldingTheHostname(t *testing.T) {
	h, mock, cleanup := newSQLMockHandler(t)
	defer cleanup()

	const hostname = "release.victim.com"
	attackerService, attackerProject := uuid.New(), uuid.New()
	victimService, victimProject := uuid.New(), uuid.New()

	// The attacker's row comes back FIRST, exactly as it did after 034
	// rewrote the victim's tuple to the tail of the heap.
	mock.ExpectQuery(`SELECT\s+id, service_id, environment_id, domain`).
		WithArgs(hostname).
		WillReturnRows(sqlmock.NewRows(customDomainTestColumns).
			AddRow(customDomainRow(attackerService, hostname, "", false)...).
			AddRow(customDomainRow(victimService, hostname, "ch-victim", true)...))
	expectServiceLookup(mock, attackerService, attackerProject, "attacker-web")
	expectServiceLookup(mock, victimService, victimProject, "victim-web")
	expectJunctionOwners(mock, hostname)

	owners, err := h.hostnameOwners(context.Background(), hostname)
	if err != nil {
		t.Fatalf("hostnameOwners() error = %v", err)
	}

	if !containsProjectID(owners, victimProject) {
		t.Errorf("owners = %v, want it to include the victim project %s. "+
			"A hostname held by two projects that reports only one has not tied a break, "+
			"it has made the other project invisible to every ownership gate.",
			owners, victimProject)
	}
	if !containsProjectID(owners, attackerProject) {
		t.Errorf("owners = %v, want it to include %s too", owners, attackerProject)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sql expectations: %v", err)
	}
}

// The same shape, asserted where it actually bit: the attacker asks to release
// the hostname and must be refused because the victim is now visible.
func TestReleaseRefusedWhenTheHostnameIsAlsoHeldByAnotherProject(t *testing.T) {
	h, mock, cleanup := newStubbedCloudflareHandler(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("Cloudflare must not be reached: the hostname is held by another project too")
	})
	defer cleanup()

	const hostname = "release.victim.com"
	attackerService, attackerProject := uuid.New(), uuid.New()
	victimService, victimProject := uuid.New(), uuid.New()

	mock.ExpectQuery(`SELECT\s+id, service_id, environment_id, domain`).
		WithArgs(hostname).
		WillReturnRows(sqlmock.NewRows(customDomainTestColumns).
			AddRow(customDomainRow(attackerService, hostname, "", false)...).
			AddRow(customDomainRow(victimService, hostname, "ch-victim", true)...))
	expectServiceLookup(mock, attackerService, attackerProject, "attacker-web")
	expectServiceLookup(mock, victimService, victimProject, "victim-web")
	expectJunctionOwners(mock, hostname)

	err := h.releaseCustomHostnameForProject(context.Background(), hostname,
		&domainOwner{ProjectID: attackerProject, ServiceID: attackerService})

	if err == nil {
		t.Fatal("the release was allowed; it deletes the victim's live custom hostname at the edge")
	}
	if !contains(err.Error(), victimProject.String()) {
		t.Errorf("error = %q, want it to name the other holder %s", err, victimProject)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sql expectations: %v", err)
	}
}

// BROKEN-3a.
//
// ensureCustomHostname returned at the customHostnameZone() guard BEFORE
// assertHostnameClaimableBy ran. On the day-one configuration — no
// fallback-origin vars, the state the PR called "inert" — an attacker's
// enclii.yaml naming the victim's hostname with `external: true` therefore
// reached the persist step with ownership never checked.
//
// Ownership is the cheaper question and does not depend on Cloudflare being
// configured, so it must be asked first.
func TestEnsureCustomHostnameChecksOwnershipBeforeTheUnconfiguredZoneGuard(t *testing.T) {
	h, mock, cleanup := newSQLMockHandler(t)
	defer cleanup()
	// Cloudflare for SaaS is NOT configured: this is the day-one state.
	h.config = &config.Config{}

	const hostname = "app.victim.com"
	victimProject := uuid.New()
	expectHostnameOwnedBy(mock, hostname, uuid.New(), victimProject, "ch-victim")

	result := h.ensureCustomHostname(context.Background(), hostname,
		&domainOwner{ProjectID: uuid.New(), ServiceID: uuid.New()})

	if result.Err == nil {
		t.Fatal("expected a refusal naming the owning project")
	}
	if !contains(result.ErrorMessage, victimProject.String()) {
		t.Errorf("error = %q, want it to name the owning project %s. "+
			"Reporting only 'cloudflare for saas is not configured' means the ownership "+
			"check never ran, which is what let the outcome be written to the victim's row.",
			result.ErrorMessage, victimProject)
	}
	if !isHostnameHeld(result.Err) {
		t.Errorf("error = %v, want it to be recognised as a positive ownership conflict", result.Err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sql expectations: %v", err)
	}
}

// BROKEN-3a, end to end: the whole deploy-path pass on the day-one config must
// leave the victim's record untouched.
//
// Reachable after every build through webhook_push -> provisionDomainsFromYAML
// -> provisionSingleDomain -> provisionDomainEdge.
func TestDeployPathOnDayOneConfigNeverWritesAnotherProjectsRecord(t *testing.T) {
	const hostname = "app.victim.com"

	// The exact configuration BROKEN-3a describes: a Cloudflare client exists,
	// but the fallback-origin vars are absent, so ensureCustomHostname reaches
	// its unconfigured-zone guard. Ownership must already have been checked by
	// then, and the outcome must land on the attacker's own row or nowhere.
	t.Run("fallback origin unset", func(t *testing.T) {
		h, mock, cleanup := newStubbedCloudflareHandler(t, func(w http.ResponseWriter, r *http.Request) {
			t.Error("Cloudflare must not be reached for a hostname another project holds")
		})
		defer cleanup()
		h.config = &config.Config{}
		h.tunnelRoutesService = newMockTunnelRoutesManager()

		victimProject := uuid.New()
		attackerService := uuid.New()

		expectHostnameOwnedBy(mock, hostname, uuid.New(), victimProject, "ch-victim")
		// The ONLY row the persist step may look for is one of the attacker's
		// own. Staging it with the attacker's service id is the assertion: if
		// the code reverts to resolving by hostname, the argument list no
		// longer matches and ExpectationsWereMet fails.
		expectOwnRowLookup(mock, hostname, attackerService, noCustomDomainRows())

		attackerNamespace := "attacker"
		result := h.provisionDomainEdge(context.Background(), hostname, &types.Service{
			ID:           attackerService,
			ProjectID:    uuid.New(),
			Name:         "attacker-web",
			K8sNamespace: &attackerNamespace,
		}, "production", 80, boolPtr(true))

		if result.Err == nil {
			t.Fatal("expected the pass to fail; it names a hostname another project holds")
		}
		if !contains(result.ErrorMessage, victimProject.String()) {
			t.Errorf("error = %q, want the ownership refusal, not the unconfigured-zone message",
				result.ErrorMessage)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("unmet sql expectations: %v", err)
		}
	})

	// The other day-one shape: no Cloudflare client at all. planDomainRouting
	// short-circuits with an error before any mechanism runs, so ownership is
	// never consulted on this branch — which is exactly why the persist step
	// has to be owner-keyed rather than trusting its callers to have checked.
	t.Run("no cloudflare client", func(t *testing.T) {
		h, mock, cleanup := newSQLMockHandler(t)
		defer cleanup()
		h.config = &config.Config{}
		h.tunnelRoutesService = newMockTunnelRoutesManager()

		attackerService := uuid.New()
		expectOwnRowLookup(mock, hostname, attackerService, noCustomDomainRows())

		attackerNamespace := "attacker"
		result := h.provisionDomainEdge(context.Background(), hostname, &types.Service{
			ID:           attackerService,
			ProjectID:    uuid.New(),
			Name:         "attacker-web",
			K8sNamespace: &attackerNamespace,
		}, "production", 80, boolPtr(true))

		if result.Err == nil {
			t.Fatal("expected the pass to fail; the requested mechanism cannot be run")
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("unmet sql expectations: %v", err)
		}
	})
}

// BROKEN-3b / 3c, at the unit that caused them.
//
// persistDomainProvisioningResult carried no owner and resolved its row by
// hostname, so the refusal branches — which had just established that the
// hostname belongs to somebody else — wrote their outcome onto that somebody
// else's record.
func TestPersistProvisioningResultOnlyEverWritesTheOwnersRow(t *testing.T) {
	t.Run("a hostname the service does not hold is never written", func(t *testing.T) {
		h, mock, cleanup := newSQLMockHandler(t)
		defer cleanup()

		attackerService := uuid.New()
		result := domainProvisioningResult{
			Domain:    "app.victim.com",
			Mechanism: mechanismCustomHostname,
		}
		result.setErr(heldByAnotherProject("refusing to route app.victim.com"))

		// Scoped to the attacker's service, which holds no such row.
		expectOwnRowLookup(mock, "app.victim.com", attackerService, noCustomDomainRows())
		// No UPDATE may follow.

		h.persistDomainProvisioningResult(context.Background(), result,
			&domainOwner{ProjectID: uuid.New(), ServiceID: attackerService})

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("unmet sql expectations: %v", err)
		}
	})

	t.Run("an outcome with no owner writes nothing at all", func(t *testing.T) {
		h, mock, cleanup := newSQLMockHandler(t)
		defer cleanup()

		// Not a single query may be issued: with no owner there is no row this
		// outcome is entitled to, and falling back to the hostname is the bug.
		h.persistDomainProvisioningResult(context.Background(), domainProvisioningResult{
			Domain:    "app.victim.com",
			Mechanism: mechanismCustomHostname,
		}, nil)

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("unmet sql expectations: %v", err)
		}
	})

	t.Run("the owner's own row is still written", func(t *testing.T) {
		h, mock, cleanup := newSQLMockHandler(t)
		defer cleanup()

		ownerService := uuid.New()
		expectOwnRowLookup(mock, "app.client.com", ownerService,
			sqlmock.NewRows(customDomainTestColumns).
				AddRow(customDomainRow(ownerService, "app.client.com", "ch-own", false)...))
		mock.ExpectQuery(`UPDATE custom_domains`).
			WillReturnRows(sqlmock.NewRows([]string{"updated_at"}).AddRow(time.Now()))

		h.persistDomainProvisioningResult(context.Background(), domainProvisioningResult{
			Domain:           "app.client.com",
			Mechanism:        mechanismCustomHostname,
			CustomHostnameID: "ch-own",
			HostnameStatus:   "active",
			SSLStatus:        "active",
		}, &domainOwner{ProjectID: uuid.New(), ServiceID: ownerService})

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("unmet sql expectations: %v", err)
		}
	})
}

// BROKEN-2b, at the seam.
//
// The claim is only atomic if the ownership check runs against the SAME
// transaction that inserts. This asserts the shape: BEGIN, the advisory lock
// keyed on the canonical hostname, the ownership reads, the INSERT, COMMIT —
// in that order, on one connection.
func TestClaimHostnameChecksAndInsertsInsideOneLockedTransaction(t *testing.T) {
	h, mock, cleanup := newSQLMockHandler(t)
	defer cleanup()

	const hostname = "fresh.client.com"
	serviceID := uuid.New()

	expectClaimTransaction(mock, hostname)
	expectHostnameUnclaimed(mock, hostname)
	mock.ExpectQuery(`INSERT INTO custom_domains`).
		WillReturnRows(sqlmock.NewRows([]string{"id", "created_at", "updated_at"}).
			AddRow(uuid.New(), time.Now(), time.Now()))
	mock.ExpectCommit()

	err := h.claimHostname(context.Background(), &types.CustomDomain{
		ServiceID:     serviceID,
		EnvironmentID: uuid.New(),
		Domain:        hostname,
	}, &domainOwner{ProjectID: uuid.New(), ServiceID: serviceID})
	if err != nil {
		t.Fatalf("claimHostname() error = %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sql expectations: %v", err)
	}
}

// And the refusal rolls back rather than leaving a half-made claim.
func TestClaimHostnameRollsBackWhenAnotherProjectHoldsIt(t *testing.T) {
	h, mock, cleanup := newSQLMockHandler(t)
	defer cleanup()

	const hostname = "app.victim.com"
	victimProject := uuid.New()

	expectClaimTransaction(mock, hostname)
	expectHostnameOwnedBy(mock, hostname, uuid.New(), victimProject, "ch-victim")
	mock.ExpectRollback()

	err := h.claimHostname(context.Background(), &types.CustomDomain{
		ServiceID:     uuid.New(),
		EnvironmentID: uuid.New(),
		Domain:        hostname,
	}, &domainOwner{ProjectID: uuid.New(), ServiceID: uuid.New()})

	if err == nil {
		t.Fatal("expected the claim to be refused")
	}
	if !isHostnameHeld(err) {
		t.Errorf("error = %v, want a positive ownership conflict so the handler answers 409, not 500", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sql expectations: %v", err)
	}
}
