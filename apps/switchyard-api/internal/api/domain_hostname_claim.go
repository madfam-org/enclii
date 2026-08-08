package api

// Registering a hostname without a window another project can slip through.
//
// The three entry points that create a custom_domains row — AddCustomDomain,
// AddServiceDomain and the deploy path's provisionSingleDomain — each ran
// "does anything hold this hostname?" and "insert the row that holds it" as
// separate statements against the pool, with nothing in between. Two concurrent
// requests naming one hostname for two different projects both read free and
// both inserted. That is not a narrow race to be argued about: it was
// reproduced against a real Postgres, on an empty table, and it lands the
// database in exactly the two-projects-one-hostname state that makes the
// hostname permanently contested for its rightful owner.
//
// The unique constraint cannot catch it — it is scoped to one service — so the
// check and the claim are made atomic here instead, under the cross-project
// advisory lock in db.WithHostnameClaim.

import (
	"context"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/db"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// withRepos returns a shallow copy of the handler bound to different
// repositories, so an ownership check can be re-run inside a transaction.
//
// Every field of Handler is a pointer or an interface and the ownership path
// only reads them, so the copy shares all collaborators and differs solely in
// which database handle the repositories speak to.
func (h *Handler) withRepos(repos *db.Repositories) *Handler {
	if h == nil {
		return nil
	}
	scoped := *h
	scoped.repos = repos
	return &scoped
}

// claimHostname creates a custom_domains row only if, at the moment of the
// insert, no other project holds the hostname.
//
// The ownership check runs inside the same transaction as the insert and under
// the same advisory lock, so a concurrent claimant is serialised behind it and
// sees the committed row rather than an empty table.
//
// It fails closed twice over: the ownership check refuses when it cannot
// resolve who holds the hostname, and any error rolls the insert back, so a
// failed claim never leaves a half-made assertion of ownership behind.
func (h *Handler) claimHostname(ctx context.Context, record *types.CustomDomain, owner *domainOwner) error {
	return h.repos.WithHostnameClaim(ctx, record.Domain, func(txRepos *db.Repositories) error {
		if err := h.withRepos(txRepos).
			assertHostnameNotHeldByAnotherProject(ctx, record.Domain, owner); err != nil {
			return err
		}
		return txRepos.CustomDomains.Create(ctx, record)
	})
}
