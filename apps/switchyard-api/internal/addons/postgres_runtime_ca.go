package addons

import (
	"bytes"
	"context"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"time"

	sdktypes "github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
)

func ownedByCNPG(s *corev1.Secret, resource string, uid types.UID) bool {
	for _, owner := range s.OwnerReferences {
		if owner.APIVersion == CloudNativePGAPIVersion && owner.Kind == CloudNativePGKind && owner.Name == resource && owner.UID == uid {
			return true
		}
	}
	return false
}

func publicCABundle(raw []byte, now time.Time) (string, error) {
	if len(raw) == 0 || len(raw) > 32768 {
		return "", fmt.Errorf("isolated database CA is unavailable")
	}
	remaining := bytes.TrimSpace(raw)
	certificates := 0
	for len(remaining) > 0 {
		if !bytes.HasPrefix(remaining, []byte("-----BEGIN CERTIFICATE-----")) {
			return "", fmt.Errorf("isolated database CA is not a public certificate bundle")
		}
		block, rest := pem.Decode(remaining)
		if block == nil || block.Type != "CERTIFICATE" || len(block.Headers) != 0 {
			return "", fmt.Errorf("isolated database CA is not a public certificate bundle")
		}
		cert, err := x509.ParseCertificate(block.Bytes)
		if err != nil || !cert.IsCA || !cert.BasicConstraintsValid || now.Before(cert.NotBefore) || !now.Before(cert.NotAfter) || cert.KeyUsage&x509.KeyUsageCertSign == 0 {
			return "", fmt.Errorf("isolated database CA is invalid or expired")
		}
		certificates++
		remaining = bytes.TrimSpace(rest)
	}
	if certificates == 0 {
		return "", fmt.Errorf("isolated database CA is unavailable")
	}
	return string(raw), nil
}

// CNPG1.27 certificates status identifies the public server CA. Opt-in addons
// accept only their operator-generated default Secret, owned by this Cluster.
// No request controls a Secret selector. ca.key is never read or serialized.
func (p *PostgresProvisioner) runtimeServerCA(ctx context.Context, addon *sdktypes.DatabaseAddon, runtime *corev1.Secret) (string, error) {
	if p.k8sClient.DynamicClient == nil || addon.K8sResourceName == "" {
		return "", fmt.Errorf("isolated database Cluster is unavailable")
	}
	cluster, err := p.k8sClient.DynamicClient.Resource(cnpgGVR).Namespace(addon.K8sNamespace).Get(ctx, addon.K8sResourceName, metav1.GetOptions{})
	if err != nil {
		return "", fmt.Errorf("isolated database Cluster is unavailable")
	}
	uid := cluster.GetUID()
	if uid == "" || cluster.GetNamespace() != addon.K8sNamespace || cluster.GetName() != addon.K8sResourceName ||
		cluster.GetLabels()[LabelAddonID] != addon.ID.String() || cluster.GetLabels()[LabelProjectID] != addon.ProjectID.String() ||
		!ownedByCNPG(runtime, addon.K8sResourceName, uid) {
		return "", fmt.Errorf("isolated database credentials do not belong to the Cluster")
	}
	name, _, _ := unstructured.NestedString(cluster.Object, "status", "certificates", "serverCASecret")
	if name != addon.K8sResourceName+"-ca" {
		return "", fmt.Errorf("isolated database server CA is not reconciled")
	}
	ca, err := p.k8sClient.Kube().CoreV1().Secrets(addon.K8sNamespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil || ca.Namespace != addon.K8sNamespace || ca.Name != name || !ownedByCNPG(ca, addon.K8sResourceName, uid) {
		return "", fmt.Errorf("isolated database server CA does not belong to the Cluster")
	}
	return publicCABundle(ca.Data["ca.crt"], time.Now())
}
