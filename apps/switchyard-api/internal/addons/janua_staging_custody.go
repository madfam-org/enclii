package addons

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net"
	"net/url"
	"reflect"

	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8stypes "k8s.io/apimachinery/pkg/types"
)

const JanuaStagingCAFile = "/var/run/janua-staging/ca/ca.crt"

// Sensitive custody data is deliberately absent from every JSON response.
// Only this closed reader can supply the fixed database/CA intake projection.
type JanuaStagingCustody struct {
	Data           map[string]interface{} `json:"-"`
	PublicCASHA256 string                 `json:"public_ca_sha256"`
	ClusterUID     k8stypes.UID           `json:"cluster_uid"`
	RuntimeUID     k8stypes.UID           `json:"runtime_uid"`
}

// ReadJanuaStagingCustody does not call generic GetCredentials or weaken its
// synthetic-only validation. It verifies the retained native create lineage,
// live namespace/Cluster/runtime UIDs, effective policies and CNPG readiness.
func (s *AddonService) ReadJanuaStagingCustody(ctx context.Context, a *types.DatabaseAddon, operation JanuaStagingOperation, journalUID k8stypes.UID, maxConnections int, peers []KarafielControlPeer, operator metav1.LabelSelector, revalidate func(context.Context) error) (*JanuaStagingCustody, error) {
	if s == nil || a == nil || a.EnvironmentID == nil || !JanuaStagingAddonOwned(a, operation.ProjectID, operation.EnvironmentID) || a.ID != operation.AddonID || !operation.valid() || revalidate == nil {
		return nil, fmt.Errorf("closed Janua custody authority refused")
	}
	p, ok := s.provisioners[types.DatabaseAddonTypePostgres].(*PostgresProvisioner)
	if !ok || p.k8sClient == nil || p.k8sClient.Kube() == nil || p.k8sClient.DynamicClient == nil {
		return nil, fmt.Errorf("closed Janua custody adapter unavailable")
	}
	if err := revalidate(ctx); err != nil {
		return nil, fmt.Errorf("Janua custody persisted authority changed")
	}
	receipt, err := ReadJanuaStagingBirthReceipt(ctx, p.k8sClient.Kube(), operation, journalUID)
	if err != nil {
		return nil, err
	}
	if err = p.verifyJanuaNamespaces(ctx, a, receipt.JanuaStagingNamespaceBinding); err != nil {
		return nil, err
	}
	expected, err := p.JanuaStagingCluster(a, maxConnections)
	if err != nil {
		return nil, err
	}
	namespace := expected.GetNamespace()
	if a.K8sNamespace != namespace || a.K8sResourceName != expected.GetName() || a.ConnectionSecret != JanuaStagingRuntimeSecretName {
		return nil, fmt.Errorf("Janua staging persisted native binding incomplete")
	}
	cluster, err := p.k8sClient.DynamicClient.Resource(cnpgGVR).Namespace(namespace).Get(ctx, expected.GetName(), metav1.GetOptions{})
	if err != nil || cluster.GetUID() != receipt.ClusterUID || cluster.GetDeletionTimestamp() != nil || !closedKarafielCluster(expected, cluster) || cluster.GetLabels()[LabelProjectID] != a.ProjectID.String() || cluster.GetLabels()[LabelAddonID] != a.ID.String() || cluster.GetAnnotations()["enclii.dev/staging-operation"] != operation.OperationID.String() || cluster.GetAnnotations()["enclii.dev/staging-plan-sha256"] != operation.PlanSHA256 {
		return nil, fmt.Errorf("Janua staging Cluster birth identity changed")
	}
	policies, err := JanuaStagingPolicies(a, peers, operator)
	if err != nil {
		return nil, err
	}
	if err = p.verifyJanuaPolicies(ctx, a, policies, receipt.PolicyUIDs); err != nil {
		return nil, err
	}
	secret, err := p.k8sClient.Kube().CoreV1().Secrets(namespace).Get(ctx, JanuaStagingRuntimeSecretName, metav1.GetOptions{})
	host := JanuaStagingClusterName + "-rw." + namespace + ".svc.cluster.local"
	if err != nil || secret == nil || secret.UID != receipt.RuntimeSecretUID || secret.ResourceVersion != receipt.RuntimeResourceVersion || secret.DeletionTimestamp != nil || secret.Immutable == nil || !*secret.Immutable || secret.Type != corev1.SecretTypeBasicAuth || len(secret.OwnerReferences) != 1 || !ownedByCNPG(secret, expected.GetName(), receipt.ClusterUID) || secret.Labels[LabelProjectID] != a.ProjectID.String() || secret.Labels[LabelAddonID] != a.ID.String() || secret.Annotations["enclii.dev/staging-operation"] != operation.OperationID.String() || string(secret.Data["username"]) != JanuaStagingRuntime || string(secret.Data["dbname"]) != JanuaStagingDatabase || string(secret.Data["host"]) != host || string(secret.Data["port"]) != "5432" || !runtimePassword.Match(secret.Data["password"]) {
		return nil, fmt.Errorf("Janua staging runtime birth identity changed")
	}
	ca, err := p.verifiedRuntimeServerCA(ctx, a, secret, parseValidatedPostgresClusterStatus)
	if err != nil {
		return nil, fmt.Errorf("Janua staging Ready runtime/public CA proof unavailable")
	}
	if err = revalidate(ctx); err != nil {
		return nil, fmt.Errorf("Janua custody authority changed during read")
	}
	// Recheck the exact objects immediately before returning secret material.
	current, err := p.k8sClient.Kube().CoreV1().Secrets(namespace).Get(ctx, secret.Name, metav1.GetOptions{})
	if err != nil || current.UID != secret.UID || current.ResourceVersion != secret.ResourceVersion || !reflect.DeepEqual(current.Data, secret.Data) {
		return nil, fmt.Errorf("Janua staging runtime changed during custody read")
	}
	uri := url.URL{Scheme: "postgresql", User: url.UserPassword(JanuaStagingRuntime, string(secret.Data["password"])), Host: net.JoinHostPort(host, "5432"), Path: "/" + JanuaStagingDatabase}
	uri.RawQuery = url.Values{"sslmode": []string{"verify-full"}, "sslrootcert": []string{JanuaStagingCAFile}}.Encode()
	sum := sha256.Sum256([]byte(ca))
	return &JanuaStagingCustody{Data: map[string]interface{}{"database_url": uri.String(), "database_ca": ca}, PublicCASHA256: hex.EncodeToString(sum[:]), ClusterUID: receipt.ClusterUID, RuntimeUID: receipt.RuntimeSecretUID}, nil
}
