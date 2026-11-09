package addons

import (
	"context"
	"errors"

	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// ErrNetworkConvergenceNotApplicable: the addon is not a live, provisioned
// managed-Postgres cluster, so there is no policy to converge.
var ErrNetworkConvergenceNotApplicable = errors.New("not a live provisioned managed-Postgres addon")

// networkConvergible reports whether the addon's CNPG cluster should carry the
// control-plane and operator policies: provisioned, and not on its way out.
func networkConvergible(addon *types.DatabaseAddon) bool {
	if addon == nil || addon.Type != types.DatabaseAddonTypePostgres || addon.K8sNamespace == "" || addon.K8sResourceName == "" {
		return false
	}
	switch addon.Status {
	case types.DatabaseAddonStatusDeleted, types.DatabaseAddonStatusFailed, types.DatabaseAddonStatusDeleting, types.DatabaseAddonStatusPendingDeletion:
		return false
	}
	return true
}

// ConvergeCNPGNetworkPolicies converges the NetworkPolicies one managed-Postgres
// cluster needs (postgres_control_plane.go). Idempotent. RefreshStatus calls it,
// and the addon reconciler's network sweep calls it for every live cluster, so
// clusters provisioned before a policy existed, or whose policy was rewritten,
// heal without an operator.
func (s *AddonService) ConvergeCNPGNetworkPolicies(ctx context.Context, addon *types.DatabaseAddon) error {
	if isKarafielStagingAddon(addon) {
		return ErrNetworkConvergenceNotApplicable
	}
	if !networkConvergible(addon) {
		return ErrNetworkConvergenceNotApplicable
	}
	pg, ok := s.provisioners[types.DatabaseAddonTypePostgres].(*PostgresProvisioner)
	if !ok || pg.k8sClient == nil {
		return errors.New("postgres provisioner has no cluster client")
	}
	return pg.ensureCNPGNetworkPolicies(ctx, addon.K8sNamespace, addon.K8sResourceName)
}
