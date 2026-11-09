package reconciler

import (
	"context"
	"errors"

	"github.com/sirupsen/logrus"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/addons"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// NetworkConverger converges the NetworkPolicies one managed-Postgres cluster
// needs. Satisfied by *addons.AddonService.ConvergeCNPGNetworkPolicies; a
// narrow interface for the same reason as RetentionFinalizer.
type NetworkConverger interface {
	ConvergeCNPGNetworkPolicies(ctx context.Context, addon *types.DatabaseAddon) error
}

// networkSweepEveryTicks spaces the sweep: on start, then every 20 ticks of
// the 30 s loop (10 minutes). Policies change rarely; a rewrite by another
// writer is healed within one interval.
const networkSweepEveryTicks = 20

// SetNetworkConverger wires the converger used by the network sweep. Safe to
// leave unset: the sweep no-ops then.
func (r *AddonReconciler) SetNetworkConverger(c NetworkConverger) {
	r.network = c
}

// sweepCNPGNetworkPolicies converges the policies of every live managed-Postgres
// addon. It is the backfill for clusters provisioned before a policy existed
// (the operator-ingress policy, 2026-10-24) and the repair for a policy that
// another writer replaced. Only allow rules are written; one addon's failure
// never stops the others.
func (r *AddonReconciler) sweepCNPGNetworkPolicies(ctx context.Context) {
	if r.network == nil || r.repos == nil || r.repos.DatabaseAddons == nil {
		return
	}
	live, err := r.repos.DatabaseAddons.ListLivePostgres(ctx)
	if err != nil {
		r.logger.WithError(err).Error("Failed to list live PostgreSQL addons for the NetworkPolicy sweep")
		return
	}
	failed := 0
	for _, addon := range live {
		if err := r.network.ConvergeCNPGNetworkPolicies(ctx, addon); err != nil && !errors.Is(err, addons.ErrNetworkConvergenceNotApplicable) {
			failed++
			r.logger.WithError(err).WithFields(logrus.Fields{"addon_id": addon.ID, "namespace": addon.K8sNamespace, "resource": addon.K8sResourceName}).Warn("CNPG NetworkPolicies not converged; next sweep retries")
		}
	}
	r.logger.WithFields(logrus.Fields{"count": len(live), "failed": failed}).Debug("CNPG NetworkPolicy sweep done")
}
