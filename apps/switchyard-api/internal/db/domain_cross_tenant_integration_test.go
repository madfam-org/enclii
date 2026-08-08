//go:build integration

package db

// Cross-tenant hostname regressions, against a real Postgres.
//
// These two defects are only visible with a live server: one is the behaviour
// of a migration under a constraint that does not fire, the other is two
// connections interleaving. Both were reproduced end to end before being fixed,
// and both fail against the code as it stood at c3f2a3e4.
//
// Run with:
//
//	TEST_DATABASE_URL=postgres://... go test -tags integration ./internal/db/...

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/golang-migrate/migrate/v4"
	migratepostgres "github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/google/uuid"
	_ "github.com/lib/pq"

	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// freshDatabase creates an empty database and returns a connection to it plus
// its URL. Each test gets its own so migrations can be stepped independently.
func freshDatabase(t *testing.T) (*sql.DB, string) {
	t.Helper()

	adminURL := os.Getenv("TEST_DATABASE_URL")
	if adminURL == "" {
		t.Skip("TEST_DATABASE_URL not set — skipping integration test")
	}

	admin, err := sql.Open("postgres", adminURL)
	if err != nil {
		t.Fatalf("connect to admin database: %v", err)
	}
	defer func() { _ = admin.Close() }()
	if err := admin.Ping(); err != nil {
		t.Fatalf("ping admin database: %v", err)
	}

	name := "enclii_xt_" + strings.ReplaceAll(uuid.New().String()[:8], "-", "")
	if _, err := admin.Exec("CREATE DATABASE " + name); err != nil {
		t.Fatalf("create database %s: %v", name, err)
	}
	t.Cleanup(func() {
		cleanup, err := sql.Open("postgres", adminURL)
		if err != nil {
			return
		}
		defer func() { _ = cleanup.Close() }()
		_, _ = cleanup.Exec("DROP DATABASE IF EXISTS " + name + " WITH (FORCE)")
	})

	url := replaceDatabaseName(adminURL, name)
	conn, err := sql.Open("postgres", url)
	if err != nil {
		t.Fatalf("connect to %s: %v", name, err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	if err := conn.Ping(); err != nil {
		t.Fatalf("ping %s: %v", name, err)
	}

	return conn, url
}

// replaceDatabaseName swaps the path component of a postgres URL.
func replaceDatabaseName(rawURL, name string) string {
	scheme := strings.SplitN(rawURL, "://", 2)
	if len(scheme) != 2 {
		return rawURL
	}
	rest := scheme[1]
	hostAndPath := rest
	query := ""
	if i := strings.Index(rest, "?"); i >= 0 {
		hostAndPath, query = rest[:i], rest[i:]
	}
	slash := strings.Index(hostAndPath, "/")
	if slash < 0 {
		return scheme[0] + "://" + hostAndPath + "/" + name + query
	}
	return scheme[0] + "://" + hostAndPath[:slash] + "/" + name + query
}

// migrateTo steps the schema to a specific version.
func migrateTo(t *testing.T, conn *sql.DB, version uint) error {
	t.Helper()

	driver, err := migratepostgres.WithInstance(conn, &migratepostgres.Config{})
	if err != nil {
		t.Fatalf("migration driver: %v", err)
	}
	source, err := iofs.New(migrationFS, "migrations")
	if err != nil {
		t.Fatalf("migration source: %v", err)
	}
	m, err := migrate.NewWithInstance("iofs", source, "postgres", driver)
	if err != nil {
		t.Fatalf("migration instance: %v", err)
	}
	if err := m.Migrate(version); err != nil && err != migrate.ErrNoChange {
		return err
	}
	return nil
}

// seedService creates a project, environment and service, returning the ids
// custom_domains needs.
func seedService(t *testing.T, conn *sql.DB, slug string) (projectID, envID, serviceID uuid.UUID) {
	t.Helper()

	projectID, envID, serviceID = uuid.New(), uuid.New(), uuid.New()
	mustExec(t, conn, `INSERT INTO projects (id, name, slug) VALUES ($1, $2, $3)`,
		projectID, slug, slug)
	mustExec(t, conn, `INSERT INTO environments (id, project_id, name, kube_namespace)
		VALUES ($1, $2, 'production', $3)`, envID, projectID, slug)
	mustExec(t, conn, `INSERT INTO services (id, project_id, name, git_repo)
		VALUES ($1, $2, $3, $4)`, serviceID, projectID, slug+"-web", "madfam-org/"+slug)
	return projectID, envID, serviceID
}

func mustExec(t *testing.T, conn *sql.DB, query string, args ...interface{}) {
	t.Helper()
	if _, err := conn.Exec(query, args...); err != nil {
		t.Fatalf("exec %q: %v", query, err)
	}
}

// BROKEN-2a, first half.
//
// Migration 034 grouped its collision check by (service_id, environment_id,
// lower(domain)) — the shape of the unique constraint — so a case pair held by
// two DIFFERENT services, in two different projects, raised no constraint and
// was never detected. The migration ran clean and silently merged a
// cross-project collision.
//
// It must refuse, and name both spellings, exactly as it already did for the
// narrow same-service case.
func TestMigration034RefusesACrossProjectCasePair(t *testing.T) {
	conn, _ := freshDatabase(t)

	if err := migrateTo(t, conn, 33); err != nil {
		t.Fatalf("migrate to 33: %v", err)
	}

	// The victim registered first, verified and live.
	_, victimEnv, victimService := seedService(t, conn, "victim")
	mustExec(t, conn, `INSERT INTO custom_domains (service_id, environment_id, domain, verified)
		VALUES ($1, $2, 'Release.Victim.com', true)`, victimService, victimEnv)

	// The attacker takes the same hostname in a different case, in a different
	// project. No unique constraint spans this pair, so the insert succeeds.
	_, attackerEnv, attackerService := seedService(t, conn, "attacker")
	mustExec(t, conn, `INSERT INTO custom_domains (service_id, environment_id, domain, verified)
		VALUES ($1, $2, 'release.victim.com', false)`, attackerService, attackerEnv)

	err := migrateTo(t, conn, 34)
	if err == nil {
		t.Fatal("migration 034 applied cleanly over a cross-project case pair. " +
			"It has just merged two projects' claims onto one hostname, and because " +
			"`SET domain = lower(domain)` rewrites the victim's row as a new heap version, " +
			"the attacker's row now answers an unordered lookup first.")
	}

	// And it has to say what it found, or an operator cannot resolve it.
	for _, want := range []string{"release.victim.com", "Release.Victim.com"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("refusal %q does not name the spelling %q", err, want)
		}
	}

	// Nothing may have been rewritten.
	var spellings int
	if err := conn.QueryRow(
		`SELECT count(DISTINCT domain) FROM custom_domains`).Scan(&spellings); err != nil {
		t.Fatalf("count spellings: %v", err)
	}
	if spellings != 2 {
		t.Errorf("distinct spellings = %d, want 2: a refused migration must not have rewritten a row", spellings)
	}
}

// The same for junctions, whose unique index is (domain, path): a case pair on
// two different paths raises no violation either, and every ownership lookup
// keys on lower(domain) alone.
func TestMigration034RefusesACrossProjectJunctionCasePair(t *testing.T) {
	conn, _ := freshDatabase(t)

	if err := migrateTo(t, conn, 33); err != nil {
		t.Fatalf("migrate to 33: %v", err)
	}

	victimProject, _, victimService := seedService(t, conn, "victim")
	mustExec(t, conn, `INSERT INTO junctions (project_id, service_id, domain, path)
		VALUES ($1, $2, 'App.Victim.com', '/')`, victimProject, victimService)

	attackerProject, _, attackerService := seedService(t, conn, "attacker")
	mustExec(t, conn, `INSERT INTO junctions (project_id, service_id, domain, path)
		VALUES ($1, $2, 'app.victim.com', '/api')`, attackerProject, attackerService)

	if err := migrateTo(t, conn, 34); err == nil {
		t.Fatal("migration 034 applied cleanly over a cross-project junction case pair")
	} else if !strings.Contains(err.Error(), "app.victim.com") {
		t.Errorf("refusal %q does not name the hostname", err)
	}
}

// A clean estate still migrates. The whole point is that this is a no-op on the
// real corpus (0 uppercase across 91 declared hostnames), so refusing has to be
// reserved for genuine collisions.
func TestMigration034AppliesCleanlyWithNoCasePairs(t *testing.T) {
	conn, _ := freshDatabase(t)

	if err := migrateTo(t, conn, 33); err != nil {
		t.Fatalf("migrate to 33: %v", err)
	}

	_, env, service := seedService(t, conn, "madfam")
	mustExec(t, conn, `INSERT INTO custom_domains (service_id, environment_id, domain)
		VALUES ($1, $2, 'api.enclii.dev')`, service, env)
	// The same hostname held by one service in two environments is legitimate
	// and must not be mistaken for a collision.
	otherEnv := uuid.New()
	mustExec(t, conn, `INSERT INTO environments (id, project_id, name, kube_namespace)
		SELECT $1, project_id, 'staging', 'madfam-staging' FROM services WHERE id = $2`,
		otherEnv, service)
	mustExec(t, conn, `INSERT INTO custom_domains (service_id, environment_id, domain)
		VALUES ($1, $2, 'api.enclii.dev')`, service, otherEnv)

	if err := migrateTo(t, conn, 34); err != nil {
		t.Fatalf("migration 034 refused a clean estate: %v", err)
	}

	var dirty bool
	if err := conn.QueryRow(`SELECT dirty FROM schema_migrations`).Scan(&dirty); err != nil {
		t.Fatalf("read migration state: %v", err)
	}
	if dirty {
		t.Error("schema_migrations is dirty after a clean 034")
	}
}

// readBarrier forces the interleaving the race needs, without deadlocking when
// the fix is present.
//
// The window is between "read who holds the hostname" and "insert the row that
// holds it". Two goroutines racing freely almost always serialise by luck — the
// first version of this test passed with the lock REMOVED, which made it worth
// nothing — so both are made to pause after their read until the other has read
// too.
//
// The wait is bounded, and that bound is what keeps the test honest in both
// directions. With the lock in place the second claimant is blocked before its
// read and can never arrive, so the first times out, proceeds, and commits;
// exactly one claim wins. Without the lock both arrive immediately, both saw an
// empty table, and both insert.
type readBarrier struct {
	arrived chan struct{}
	once    sync.Once
	count   int
	mu      sync.Mutex
}

func newReadBarrier() *readBarrier {
	return &readBarrier{arrived: make(chan struct{})}
}

func (b *readBarrier) waitForPeer() {
	b.mu.Lock()
	b.count++
	if b.count == 2 {
		b.once.Do(func() { close(b.arrived) })
	}
	b.mu.Unlock()

	select {
	case <-b.arrived:
	case <-time.After(2 * time.Second):
	}
}

// claimAttempt runs the full check-and-claim the handlers run: read who holds
// the hostname, pause at the barrier, then insert if nobody does.
func claimAttempt(
	repos *Repositories, barrier *readBarrier, hostname string, serviceID, envID uuid.UUID,
) error {
	return repos.WithHostnameClaim(context.Background(), hostname, func(tx *Repositories) error {
		existing, err := tx.CustomDomains.ListByDomain(context.Background(), hostname)
		if err != nil {
			return err
		}

		barrier.waitForPeer()

		for _, row := range existing {
			if row.ServiceID != serviceID {
				return fmt.Errorf("hostname %s already held by service %s", hostname, row.ServiceID)
			}
		}
		return tx.CustomDomains.Create(context.Background(), &types.CustomDomain{
			ServiceID:     serviceID,
			EnvironmentID: envID,
			Domain:        strings.ToLower(strings.TrimSpace(hostname)),
		})
	})
}

// runRacingClaims runs two claims concurrently and returns both outcomes.
func runRacingClaims(
	repos *Repositories,
	hostnameA string, serviceA, envA uuid.UUID,
	hostnameB string, serviceB, envB uuid.UUID,
) []error {
	barrier := newReadBarrier()
	errs := make([]error, 2)

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		errs[0] = claimAttempt(repos, barrier, hostnameA, serviceA, envA)
	}()
	go func() {
		defer wg.Done()
		errs[1] = claimAttempt(repos, barrier, hostnameB, serviceB, envB)
	}()
	wg.Wait()

	return errs
}

// BROKEN-2b.
//
// Exists -> assertHostnameNotHeldByAnotherProject -> Create ran as three
// statements against the pool with no transaction and no lock, and the unique
// constraint is service-scoped so the database could not backstop it. Two
// concurrent claims for one hostname in two different projects therefore both
// succeeded, reaching the corrupt two-projects-one-hostname state with no
// legacy data at all.
//
// With the claim taken under a transaction-scoped advisory lock, exactly one
// wins. The loser must lose because it SAW the winner's row, not because of an
// integrity error.
func TestConcurrentClaimsForOneHostnameLeaveExactlyOneHolder(t *testing.T) {
	conn, _ := freshDatabase(t)

	if err := migrateTo(t, conn, 34); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	_, envA, serviceA := seedService(t, conn, "project-a")
	_, envB, serviceB := seedService(t, conn, "project-b")

	const hostname = "contested.example.com"
	errs := runRacingClaims(NewRepositories(conn),
		hostname, serviceA, envA,
		hostname, serviceB, envB)

	succeeded := 0
	for _, err := range errs {
		if err == nil {
			succeeded++
		}
	}
	if succeeded != 1 {
		t.Fatalf("%d of 2 concurrent claims succeeded, want exactly 1 (errors: %v)", succeeded, errs)
	}

	var holders int
	if err := conn.QueryRow(
		`SELECT count(DISTINCT service_id) FROM custom_domains WHERE lower(domain) = $1`,
		hostname).Scan(&holders); err != nil {
		t.Fatalf("count holders: %v", err)
	}
	if holders != 1 {
		t.Errorf("distinct services holding %s = %d, want 1. Two projects holding one hostname "+
			"is the corrupt state that makes it permanently contested for its rightful owner.",
			hostname, holders)
	}
}

// The lock is keyed on the CANONICAL hostname, so a case variant contends for
// the same lock. Keying it on the raw string would serialise nothing for
// exactly the pair that motivated it.
func TestConcurrentClaimsContendAcrossCaseVariants(t *testing.T) {
	conn, _ := freshDatabase(t)

	if err := migrateTo(t, conn, 34); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	_, envA, serviceA := seedService(t, conn, "project-a")
	_, envB, serviceB := seedService(t, conn, "project-b")

	errs := runRacingClaims(NewRepositories(conn),
		"Contested.Example.com", serviceA, envA,
		"contested.example.com", serviceB, envB)

	succeeded := 0
	for _, err := range errs {
		if err == nil {
			succeeded++
		}
	}
	if succeeded != 1 {
		t.Fatalf("%d of 2 concurrent case-variant claims succeeded, want exactly 1 (errors: %v)",
			succeeded, errs)
	}
}

// BROKEN-2a, second half, against the real server: ListByDomain must return
// every row, in a stable order, even after 034's UPDATE has moved a tuple to
// the tail of the heap.
func TestListByDomainReturnsEveryHolderAfterTheLowercaseRewrite(t *testing.T) {
	conn, _ := freshDatabase(t)

	if err := migrateTo(t, conn, 33); err != nil {
		t.Fatalf("migrate to 33: %v", err)
	}

	_, victimEnv, victimService := seedService(t, conn, "victim")
	_, attackerEnv, attackerService := seedService(t, conn, "attacker")

	// The victim first, so it is physically first in the heap.
	mustExec(t, conn, `INSERT INTO custom_domains (service_id, environment_id, domain, verified)
		VALUES ($1, $2, 'release.victim.com', true)`, victimService, victimEnv)
	mustExec(t, conn, `INSERT INTO custom_domains (service_id, environment_id, domain, verified)
		VALUES ($1, $2, 'release.victim.com', false)`, attackerService, attackerEnv)

	// The rewrite 034 performs, which is what reordered the heap.
	mustExec(t, conn, `UPDATE custom_domains SET domain = lower(domain), updated_at = NOW()
		WHERE service_id = $1`, victimService)

	repo := NewCustomDomainRepository(conn)
	rows, err := repo.ListByDomain(context.Background(), "release.victim.com")
	if err != nil {
		t.Fatalf("ListByDomain: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("ListByDomain returned %d rows, want 2. Resolving this predicate with QueryRow "+
			"returns one row and silently discards the other holder.", len(rows))
	}

	holders := map[uuid.UUID]bool{}
	for _, row := range rows {
		holders[row.ServiceID] = true
	}
	if !holders[victimService] || !holders[attackerService] {
		t.Errorf("holders = %v, want both %s and %s", holders, victimService, attackerService)
	}
}
