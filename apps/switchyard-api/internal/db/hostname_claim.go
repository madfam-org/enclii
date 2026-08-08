package db

// Serialising the claim of a hostname across projects.
//
// Registering a hostname is a three-statement dance — does a row exist, does
// another project hold it, insert — and it used to run with no transaction and
// no lock. Two concurrent requests naming the same hostname for two different
// projects therefore both read "free" and both inserted, which is not a
// theoretical interleaving: it was reproduced against a real Postgres with no
// legacy data at all.
//
// The database cannot backstop this on its own. The only uniqueness on
// custom_domains.domain is
// custom_domains_service_id_environment_id_domain_key, and widening it to a
// global unique index on lower(domain) is not available: one service is
// legitimately allowed to hold the same hostname in more than one environment,
// and junctions.domain is unique only per (domain, path). So the mutual
// exclusion has to be taken explicitly, and every claimant has to take it.
//
// A transaction-scoped advisory lock keyed on the canonical hostname is what
// that costs. It is released automatically at COMMIT or ROLLBACK — including
// when the connection dies, which a row-level guard could not promise — and it
// is keyed on the hostname rather than on any row, so it serialises claimants
// that have no row in common yet.

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

// hostnameClaimLockClass namespaces every advisory lock this helper takes, so
// they cannot collide with an advisory lock some other subsystem keys on the
// same hash. Postgres advisory locks are a single global space; the two-int
// form exists precisely so callers can carve it up.
//
// The value is arbitrary and only has to be stable and unique within this
// codebase.
const hostnameClaimLockClass = 0x454e4348 // "ENCH" — Enclii, hostname claim

// canonicalHostnameKey renders the hostname in the form the lock is keyed on.
//
// It MUST agree with how the rows are stored and compared (lower(domain)),
// otherwise "app.victim.com" and "App.Victim.com" take two different locks and
// the mutual exclusion silently does nothing for exactly the case-variant pair
// that motivated it.
func canonicalHostnameKey(hostname string) string {
	return strings.ToLower(strings.TrimSpace(hostname))
}

// WithHostnameClaim runs fn inside a transaction that holds the cross-project
// claim lock for one hostname.
//
// Everything that decides whether a hostname is free AND then records the claim
// must run inside fn. Splitting the check from the insert across two calls
// re-opens the window this exists to close, so callers pass the whole
// check-and-claim, not just the write.
//
// fn receives transaction-scoped repositories; reads through them see a
// consistent snapshot and writes through them commit atomically with the
// lock's release.
func (r *Repositories) WithHostnameClaim(
	ctx context.Context, hostname string, fn func(txRepos *Repositories) error,
) error {
	if r == nil || r.db == nil {
		return fmt.Errorf("cannot claim hostname %s: database connection not initialized", hostname)
	}

	key := canonicalHostnameKey(hostname)
	if key == "" {
		return fmt.Errorf("cannot claim an empty hostname")
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin hostname claim transaction for %s: %w", hostname, err)
	}

	// hashtext() collapses the hostname to an int4, so two distinct hostnames
	// can share a lock. That is safe in the only direction that matters: a
	// collision serialises two unrelated claims (slower), it never lets two
	// claimants of the SAME hostname run concurrently.
	if _, err := tx.ExecContext(ctx,
		"SELECT pg_advisory_xact_lock($1, hashtext($2))", hostnameClaimLockClass, key); err != nil {
		if rbErr := tx.Rollback(); rbErr != nil && rbErr != sql.ErrTxDone {
			return fmt.Errorf("failed to take the claim lock for %s: %v, rollback failed: %w", hostname, err, rbErr)
		}
		return fmt.Errorf("failed to take the claim lock for %s: %w", hostname, err)
	}

	txRepos := newTxRepositories(r.db, tx)

	if err := fn(txRepos); err != nil {
		if rbErr := tx.Rollback(); rbErr != nil && rbErr != sql.ErrTxDone {
			return fmt.Errorf("hostname claim for %s failed: %v, rollback failed: %w", hostname, err, rbErr)
		}
		return err
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit the hostname claim for %s: %w", hostname, err)
	}

	return nil
}
