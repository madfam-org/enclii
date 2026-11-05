package api

import (
	"context"
	"fmt"
	"reflect"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/addons"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/secretsintake"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8stypes "k8s.io/apimachinery/pkg/types"
)

// Internal BatchA component only: no HTTP route or ApplySupported change.
// Credentials are read exclusively from the owned native birth receipt, never
// request values or a user/operator shell. CAS initializes both keys together.
func (h *Handler) projectJanuaStagingCustody(ctx context.Context, addon *types.DatabaseAddon, operation addons.JanuaStagingOperation, journalUID k8stypes.UID, maxConnections int, peers []addons.KarafielControlPeer, operator metav1.LabelSelector, revalidate func(context.Context) error) (int, error) {
	if h == nil || h.addonService == nil || h.vaultClient == nil || !h.vaultClient.IsEnabled() || h.k8sClient == nil || h.k8sClient.Kube() == nil || revalidate == nil {
		return 0, fmt.Errorf("Janua staging atomic custody adapter unavailable")
	}
	initializer, ok := h.vaultClient.(vaultSecretInitializer)
	if !ok {
		return 0, fmt.Errorf("Janua staging requires native atomic custody initialization")
	}
	if err := januaStageCustodyRegistry(); err != nil {
		return 0, err
	}
	custody, err := h.addonService.ReadJanuaStagingCustody(ctx, addon, operation, journalUID, maxConnections, peers, operator, revalidate)
	if err != nil {
		return 0, err
	}
	if err = revalidate(ctx); err != nil {
		return 0, fmt.Errorf("Janua staging custody persisted authority changed")
	}
	version, err := initializer.InitializeSecretData(ctx, "secret/janua-staging", custody.Data)
	if err != nil || version <= 0 {
		return 0, fmt.Errorf("Janua staging custody initialization uncertain; no overwrite or automatic retry")
	}
	if err = addons.WriteJanuaStagingCustodyReceipt(ctx, h.k8sClient.Kube(), operation, journalUID, custody, version); err != nil {
		return 0, err
	}
	return version, nil
}

func januaStageCustodyRegistry() error {
	registry, err := secretsintake.LoadRegistry()
	if err != nil {
		return fmt.Errorf("Janua staging custody registry unavailable")
	}
	for id, keys := range map[string][]string{"janua-staging/database": {"database_url"}, "janua-staging/database-ca": {"database_ca"}} {
		target, ok := registry[id]
		if !ok || target.VaultPath != "secret/janua-staging" || target.Namespace != addons.JanuaStagingNamespace || target.Generate == nil || !target.Generate.Disabled || !reflect.DeepEqual(target.Keys, keys) {
			return fmt.Errorf("Janua staging fixed custody target changed")
		}
	}
	return nil
}
