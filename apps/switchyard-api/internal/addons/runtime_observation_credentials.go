package addons

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/k8s"
	sdktypes "github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Values are server-only transport custody. An observer has no owner URI.
type RuntimeObservationCredentials struct {
	RuntimeURI string `json:"-"`
	PublicCA   string `json:"-"`
	ClusterUID string `json:"-"`
}

func (s *AddonService) RuntimeObservationCredentials(ctx context.Context, addon *sdktypes.DatabaseAddon) (*RuntimeObservationCredentials, error) {
	p, ok := s.provisioners[sdktypes.DatabaseAddonTypePostgres].(*PostgresProvisioner)
	if !ok {
		return nil, fmt.Errorf("runtime observation adapter unavailable")
	}
	return p.runtimeObservationCredentials(ctx, addon)
}

func (p *PostgresProvisioner) runtimeObservationCredentials(ctx context.Context, addon *sdktypes.DatabaseAddon) (*RuntimeObservationCredentials, error) {
	if addon == nil || addon.ID == uuid.Nil || addon.ProjectID == uuid.Nil || addon.Status != sdktypes.DatabaseAddonStatusReady || addon.Type != sdktypes.DatabaseAddonTypePostgres || !runtimeProfile(addon.Config) || addon.K8sNamespace != "project-"+addon.ProjectID.String()[:8] || addon.ConnectionSecret != runtimeSecretName(addon.K8sResourceName) || p.k8sClient == nil || p.k8sClient.Kube() == nil || p.k8sClient.DynamicClient == nil {
		return nil, fmt.Errorf("Ready isolated observation transport unavailable")
	}
	ns, err := p.k8sClient.Kube().CoreV1().Namespaces().Get(ctx, addon.K8sNamespace, metav1.GetOptions{})
	if err != nil || ns.UID == "" || ns.Labels[k8s.LabelProjectNamespace] != addon.ProjectID.String() || ns.Labels["enclii.dev/verify-signatures"] != "true" {
		return nil, fmt.Errorf("runtime observation namespace refused")
	}
	runtime, err := p.GetCredentials(ctx, addon)
	if err != nil {
		return nil, fmt.Errorf("runtime observation custody refused")
	}
	cluster, err := p.k8sClient.DynamicClient.Resource(cnpgGVR).Namespace(addon.K8sNamespace).Get(ctx, addon.K8sResourceName, metav1.GetOptions{})
	if err != nil || cluster.GetUID() == "" || cluster.GetLabels()[LabelAddonID] != addon.ID.String() || cluster.GetLabels()[LabelProjectID] != addon.ProjectID.String() {
		return nil, fmt.Errorf("runtime observation Cluster refused")
	}
	status, err := p.parseClusterStatus(cluster, addon)
	if err != nil || !status.Ready || status.Status != sdktypes.DatabaseAddonStatusReady {
		return nil, fmt.Errorf("actual runtime observation Cluster not Ready")
	}
	return &RuntimeObservationCredentials{RuntimeURI: runtime.ConnectionURI, PublicCA: runtime.CACertificate, ClusterUID: string(cluster.GetUID())}, nil
}
