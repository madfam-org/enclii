package db

// Serialising writes to a Cloudflare tunnel's ingress configuration across
// every switchyard-api replica.
//
// The tunnel's ingress is one document. Every route change is a read of the
// whole document, an edit of one rule, and a PUT of the whole document back,
// and the Cloudflare configurations endpoint offers no precondition to make
// that PUT conditional on what was read. An in-process mutex serialises the
// writers of ONE replica; switchyard-api runs at least two, and a push webhook,
// a delayed junction reconcile and an operator reconcile can land on different
// replicas at the same moment. Two replicas that both read before either
// writes lose one of the two writes — and when the lost write is a canary
// revert, the hostname stays on the backend the revert was undoing.
//
// A transaction-scoped advisory lock keyed on the tunnel id closes that. It is
// released at COMMIT or ROLLBACK, including when the connection dies, so a
// replica that crashes mid-write cannot wedge every other replica.

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

// tunnelConfigLockClass namespaces the advisory locks taken here, so they
// cannot collide with the hostname-claim locks (hostnameClaimLockClass) that
// share Postgres's single advisory-lock space.
const tunnelConfigLockClass = 0x454e5443 // "ENTC" — Enclii, tunnel config

// tunnelConfigLockTimeout bounds how long a writer waits for another replica's
// write to finish. A whole-config write is two Cloudflare API calls; waiting
// far longer than that means the holder is wedged, and failing the write
// visibly beats queueing behind it forever.
const tunnelConfigLockTimeout = "30s"

// WithTunnelConfigLock runs fn while holding the cross-replica write lock for
// one tunnel's ingress configuration.
//
// fn must contain the WHOLE read-modify-write — the read of the current
// configuration as well as the write — because a read taken outside the lock
// is exactly the stale read this exists to prevent. fn does not use the
// transaction; it exists only to scope the lock.
func (r *Repositories) WithTunnelConfigLock(
	ctx context.Context, tunnelID string, fn func(ctx context.Context) error,
) error {
	key := strings.TrimSpace(tunnelID)
	if key == "" {
		return fmt.Errorf("cannot lock tunnel configuration: no tunnel id")
	}
	if r == nil || r.db == nil {
		return fmt.Errorf("cannot lock tunnel configuration: database connection not initialized")
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin tunnel configuration lock transaction: %w", err)
	}
	rollback := func(cause error) error {
		if rbErr := tx.Rollback(); rbErr != nil && rbErr != sql.ErrTxDone {
			return fmt.Errorf("%w (rollback also failed: %v)", cause, rbErr)
		}
		return cause
	}

	if _, err := tx.ExecContext(ctx, "SET LOCAL lock_timeout = '"+tunnelConfigLockTimeout+"'"); err != nil {
		return rollback(fmt.Errorf("failed to bound the tunnel configuration lock wait: %w", err))
	}
	if _, err := tx.ExecContext(ctx,
		"SELECT pg_advisory_xact_lock($1, hashtext($2))", tunnelConfigLockClass, key); err != nil {
		return rollback(fmt.Errorf("failed to take the tunnel configuration lock: %w", err))
	}

	if err := fn(ctx); err != nil {
		return rollback(err)
	}

	if err := tx.Commit(); err != nil {
		// fn has already run; the write it made stands. Report the release
		// failure without pretending the write failed.
		return fmt.Errorf("tunnel configuration written, but releasing its lock failed: %w", err)
	}
	return nil
}
