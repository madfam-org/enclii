package addons

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/k8s"
	sdktypes "github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Only a guarded server-side initial Job may consume this bundle. Every field
// is deliberately excluded from JSON; ordinary credentials remain runtime-only.
type InitialBootstrapCredentials struct {
	OwnerURI   string `json:"-"`
	RuntimeURI string `json:"-"`
	PublicCA   string `json:"-"`
	ClusterUID string `json:"-"`
}

func (s *AddonService) InitialBootstrapCredentials(ctx context.Context, addon *sdktypes.DatabaseAddon) (*InitialBootstrapCredentials, error) {
	p, ok := s.provisioners[sdktypes.DatabaseAddonTypePostgres].(*PostgresProvisioner)
	if !ok {
		return nil, fmt.Errorf("initial PostgreSQL adapter unavailable")
	}
	return p.initialBootstrapCredentials(ctx, addon)
}

func (p *PostgresProvisioner) initialBootstrapCredentials(ctx context.Context, addon *sdktypes.DatabaseAddon) (*InitialBootstrapCredentials, error) {
	if addon == nil || addon.ID == uuid.Nil || addon.ProjectID == uuid.Nil || addon.Status != sdktypes.DatabaseAddonStatusReady ||
		addon.Type != sdktypes.DatabaseAddonTypePostgres || !runtimeProfile(addon.Config) ||
		addon.K8sNamespace != "project-"+addon.ProjectID.String()[:8] || addon.ConnectionSecret != runtimeSecretName(addon.K8sResourceName) {
		return nil, fmt.Errorf("initial bootstrap requires its Ready isolated PostgreSQL addon")
	}
	if p.k8sClient == nil || p.k8sClient.Kube() == nil || p.k8sClient.DynamicClient == nil {
		return nil, fmt.Errorf("initial PostgreSQL adapter unavailable")
	}
	namespace, err := p.k8sClient.Kube().CoreV1().Namespaces().Get(ctx, addon.K8sNamespace, metav1.GetOptions{})
	if err != nil || namespace.UID == "" || namespace.Labels[k8s.LabelProjectNamespace] != addon.ProjectID.String() || namespace.Labels["enclii.dev/verify-signatures"] != "true" {
		return nil, fmt.Errorf("initial bootstrap namespace ownership/signature admission unavailable")
	}
	// Existing runtime helper verifies role/DB/host and public CA ownership. It
	// never accepts the owner Secret or a spoofed stored URI.
	runtime, err := p.GetCredentials(ctx, addon)
	if err != nil {
		return nil, fmt.Errorf("isolated runtime/CA custody unavailable")
	}
	cluster, err := p.k8sClient.DynamicClient.Resource(cnpgGVR).Namespace(addon.K8sNamespace).Get(ctx, addon.K8sResourceName, metav1.GetOptions{})
	if err != nil || cluster.GetUID() == "" || cluster.GetLabels()[LabelAddonID] != addon.ID.String() || cluster.GetLabels()[LabelProjectID] != addon.ProjectID.String() {
		return nil, fmt.Errorf("initial bootstrap Cluster ownership unavailable")
	}
	status, statusErr := p.parseClusterStatus(cluster, addon)
	if statusErr != nil || !status.Ready || status.Status != sdktypes.DatabaseAddonStatusReady {
		return nil, fmt.Errorf("actual initial bootstrap Cluster/runtime role is not Ready")
	}
	owner, err := p.k8sClient.Kube().CoreV1().Secrets(addon.K8sNamespace).Get(ctx, addon.K8sResourceName+"-app", metav1.GetOptions{})
	if err != nil || owner.Type != corev1.SecretTypeBasicAuth || owner.UID == "" || !ownedByCNPG(owner, addon.K8sResourceName, cluster.GetUID()) ||
		string(owner.Data["username"]) != runtimeMigrator || string(owner.Data["dbname"]) != addon.Config.DatabaseName ||
		!ownerHostMatches(string(owner.Data["host"]), runtime.Host, addon) || string(owner.Data["port"]) != "5432" || len(owner.Data["password"]) < 16 || strings.ContainsAny(string(owner.Data["password"]), "\x00\r\n") {
		return nil, fmt.Errorf("initial bootstrap owner custody unavailable")
	}
	return &InitialBootstrapCredentials{
		OwnerURI:   runtimeConnectionURI(runtimeMigrator, string(owner.Data["password"]), runtime.Host, runtime.DatabaseName),
		RuntimeURI: runtime.ConnectionURI, PublicCA: runtime.CACertificate, ClusterUID: string(cluster.GetUID()),
	}, nil
}

// CloudNativePG writes the read-write Service's short name ("<cluster>-rw")
// into the generated owner Secret's host key and keeps the FQDN in fqdn-uri,
// while the runtime Secret this adapter owns records the FQDN. Accept exactly
// those two spellings of the same Service; the owner URI is still built on
// the verified runtime FQDN, never on the stored value.
func ownerHostMatches(ownerHost, runtimeHost string, addon *sdktypes.DatabaseAddon) bool {
	service := addon.K8sResourceName + "-rw"
	if runtimeHost != service+"."+addon.K8sNamespace+".svc.cluster.local" {
		return false
	}
	return ownerHost == runtimeHost || ownerHost == service
}
