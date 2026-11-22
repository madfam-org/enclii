package addons

import (
	"context"
	"fmt"

	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/dynamic"
)

// HibernationState is what ops.addons.delete-retired reads from a managed
// Postgres addon's CNPG Cluster before it offers the addon for the retention
// hold. "Retired" means hibernated: an addon whose Cluster does not carry
// cnpg.io/hibernation=on is a running database and is never offered.
type HibernationState struct {
	Namespace  string `json:"namespace"`
	Cluster    string `json:"cluster"`
	ClusterUID string `json:"cluster_uid"`
	// Annotated: the Cluster carries cnpg.io/hibernation=on (the record that
	// ops.addons.hibernate left). Confirmed: CNPG's cnpg.io/hibernation
	// condition is True, i.e. CNPG has stopped the instances.
	Annotated bool `json:"hibernated"`
	Confirmed bool `json:"hibernation_confirmed"`
}

// ReadPostgresHibernationState reads the addon's CNPG Cluster. It writes
// nothing. An addon without Kubernetes resources (or not Postgres) is
// ErrHibernationUnsupported: the non-forced delete would hard-delete it at
// once instead of entering the retention hold, so it is never offered.
func ReadPostgresHibernationState(ctx context.Context, dyn dynamic.Interface, addon *types.DatabaseAddon) (*HibernationState, error) {
	if addon == nil || addon.Type != types.DatabaseAddonTypePostgres || addon.K8sNamespace == "" || addon.K8sResourceName == "" {
		return nil, ErrHibernationUnsupported
	}
	if dyn == nil {
		return nil, fmt.Errorf("kubernetes dynamic client is unavailable")
	}
	ns, name := addon.K8sNamespace, addon.K8sResourceName
	cluster, err := dyn.Resource(cnpgGVR).Namespace(ns).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("read CNPG cluster %s/%s: %w", ns, name, err)
	}
	return &HibernationState{
		Namespace:  ns,
		Cluster:    name,
		ClusterUID: string(cluster.GetUID()),
		Annotated:  cluster.GetAnnotations()[CNPGHibernationAnnotation] == "on",
		Confirmed:  cnpgConditionTrue(cluster, CNPGHibernationAnnotation),
	}, nil
}

// SupportsRetentionHold reports whether a non-forced delete of this addon
// enters the retention hold (pending_deletion) rather than an immediate hard
// delete. It is the exported view of addonSupportsRetention.
func SupportsRetentionHold(addon *types.DatabaseAddon) bool {
	return addonSupportsRetention(addon)
}
