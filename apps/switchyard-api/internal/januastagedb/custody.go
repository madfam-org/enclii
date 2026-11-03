package januastagedb

import (
	"context"
	"crypto/x509"
	"encoding/pem"
	"net"
	"net/url"
	"reflect"
	"strings"
	"time"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/addons"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	ktypes "k8s.io/apimachinery/pkg/types"
)

func clusterOwned(secret *corev1.Secret, uid ktypes.UID) bool {
	if secret == nil || secret.UID == "" || secret.ResourceVersion == "" || secret.DeletionTimestamp != nil {
		return false
	}
	for _, owner := range secret.OwnerReferences {
		if owner.APIVersion == "postgresql.cnpg.io/v1" && owner.Kind == "Cluster" && owner.Name == Cluster && owner.UID == uid {
			return true
		}
	}
	return false
}

func (k *Kernel) readyCluster(ctx context.Context, p Plan) (*unstructured.Unstructured, error) {
	if k.prerequisites(ctx, p) != nil {
		return nil, ErrRefused
	}
	receipt, err := k.journal.Receipt(ctx, p)
	if err != nil || receipt.ClusterUID == "" || receipt.RuntimeSecretUID == "" {
		return nil, ErrRefused
	}
	current, err := k.dynamic.Resource(clusterGVR).Namespace(Namespace(p.Authority.ProjectID)).Get(ctx, Cluster, metav1.GetOptions{})
	expected, e := p.ClusterManifest()
	if err != nil || e != nil || current.GetUID() != receipt.ClusterUID || current.GetResourceVersion() == "" || current.GetDeletionTimestamp() != nil || !ClosedCluster(expected, current) {
		return nil, ErrRefused
	}
	status, ok, err := unstructured.NestedMap(current.Object, "status")
	if err != nil || !ok {
		return nil, ErrRefused
	}
	ready, _, _ := unstructured.NestedInt64(status, "readyInstances")
	instances, _, _ := unstructured.NestedInt64(status, "instances")
	phase, _, _ := unstructured.NestedString(status, "phase")
	write, _, _ := unstructured.NestedString(status, "writeService")
	if ready != int64(p.Allocation.Replicas) || instances != ready || phase != "Cluster in healthy state" || write != Cluster+"-rw" || !addons.RuntimeRoleReconciled(status, RuntimeRole) {
		return nil, ErrRefused
	}
	return current, nil
}

func (k *Kernel) publicCA(ctx context.Context, p Plan, cluster *unstructured.Unstructured) ([]byte, error) {
	caName, _, _ := unstructured.NestedString(cluster.Object, "status", "certificates", "serverCASecret")
	tlsName, _, _ := unstructured.NestedString(cluster.Object, "status", "certificates", "serverTLSSecret")
	if caName != Cluster+"-ca" || tlsName != Cluster+"-server" {
		return nil, ErrRefused
	}
	api := k.kube.CoreV1().Secrets(Namespace(p.Authority.ProjectID))
	ca, err := api.Get(ctx, caName, metav1.GetOptions{})
	if err != nil || !clusterOwned(ca, cluster.GetUID()) {
		return nil, ErrRefused
	}
	public, err := addons.ValidatePublicCABundle(ca.Data["ca.crt"], time.Now())
	if err != nil {
		return nil, ErrRefused
	}
	server, err := api.Get(ctx, tlsName, metav1.GetOptions{})
	if err != nil || !clusterOwned(server, cluster.GetUID()) {
		return nil, ErrRefused
	}
	block, rest := pem.Decode(server.Data["tls.crt"])
	if block == nil || block.Type != "CERTIFICATE" || len(strings.TrimSpace(string(rest))) != 0 {
		return nil, ErrRefused
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, ErrRefused
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM([]byte(public)) {
		return nil, ErrRefused
	}
	if _, err = cert.Verify(x509.VerifyOptions{Roots: roots, DNSName: Host(p.Authority.ProjectID), CurrentTime: time.Now()}); err != nil {
		return nil, ErrRefused
	}
	return []byte(public), nil
}

func connectionURI(p Plan, role, password string) string {
	value := url.URL{Scheme: "postgresql", User: url.UserPassword(role, password), Host: net.JoinHostPort(Host(p.Authority.ProjectID), "5432"), Path: "/" + Database}
	// The downstream fixed profile additionally supplies the verified public CA.
	query := url.Values{"sslmode": {"verify-full"}}
	value.RawQuery = query.Encode()
	return value.String()
}

// RuntimeCustody reads only the fixed nonowner group and verified public CA.
// It never reads the CNPG owner secret or accepts a supplied stored URI.
func (k *Kernel) RuntimeCustody(ctx context.Context, p Plan) (*RuntimeBundle, error) {
	cluster, err := k.readyCluster(ctx, p)
	if err != nil {
		return nil, err
	}
	secret, err := k.kube.CoreV1().Secrets(Namespace(p.Authority.ProjectID)).Get(ctx, RuntimeSecret, metav1.GetOptions{})
	receipt, e := k.journal.Receipt(ctx, p)
	if err != nil || e != nil || secret == nil || secret.UID != receipt.RuntimeSecretUID || !clusterOwned(secret, cluster.GetUID()) || secret.Type != corev1.SecretTypeBasicAuth || secret.Labels["enclii.dev/project-id"] != p.Authority.ProjectID.String() || secret.Labels["enclii.dev/addon-id"] != p.OperationID.String() || secret.Annotations["enclii.dev/staging-plan-sha256"] != p.SHA256 || string(secret.Data["username"]) != RuntimeRole || string(secret.Data["dbname"]) != Database || string(secret.Data["host"]) != Host(p.Authority.ProjectID) || string(secret.Data["port"]) != "5432" || !digest.Match(secret.Data["password"]) {
		return nil, ErrRefused
	}
	ca, err := k.publicCA(ctx, p, cluster)
	if err != nil {
		return nil, err
	}
	if !k.unchangedSecret(ctx, p, secret) || !k.sameReadyCluster(ctx, p, cluster.GetUID()) {
		return nil, ErrRefused
	}
	return &RuntimeBundle{DatabaseURL: connectionURI(p, RuntimeRole, string(secret.Data["password"])), PublicCA: ca, ClusterUID: cluster.GetUID()}, nil
}

// OwnerCustody is separate and can feed only the fixed migration executor.
// Its fields have no JSON representation; no HTTP credentials endpoint exists.
func (k *Kernel) OwnerCustody(ctx context.Context, p Plan) (*OwnerBundle, error) {
	runtime, err := k.RuntimeCustody(ctx, p)
	if err != nil {
		return nil, err
	}
	owner, err := k.kube.CoreV1().Secrets(Namespace(p.Authority.ProjectID)).Get(ctx, Cluster+"-app", metav1.GetOptions{})
	host := string(ownerData(owner, "host"))
	if err != nil || !clusterOwned(owner, runtime.ClusterUID) || owner.Type != corev1.SecretTypeBasicAuth || string(owner.Data["username"]) != OwnerRole || string(owner.Data["dbname"]) != Database || (host != Host(p.Authority.ProjectID) && host != Cluster+"-rw") || string(owner.Data["port"]) != "5432" || len(owner.Data["password"]) < 16 || strings.ContainsAny(string(owner.Data["password"]), "\x00\r\n") {
		return nil, ErrRefused
	}
	if !k.unchangedSecret(ctx, p, owner) || !k.sameReadyCluster(ctx, p, runtime.ClusterUID) {
		return nil, ErrRefused
	}
	return &OwnerBundle{DatabaseURL: connectionURI(p, OwnerRole, string(owner.Data["password"])), PublicCA: runtime.PublicCA, ClusterUID: runtime.ClusterUID}, nil
}

func ownerData(s *corev1.Secret, name string) []byte {
	if s == nil {
		return nil
	}
	return s.Data[name]
}

// Kubernetes reads are not transactional. Compare a second authenticated read
// before releasing custody; replacement or password/owner drift remains held.
func (k *Kernel) unchangedSecret(ctx context.Context, p Plan, before *corev1.Secret) bool {
	if before == nil || before.ResourceVersion == "" {
		return false
	}
	after, err := k.kube.CoreV1().Secrets(Namespace(p.Authority.ProjectID)).Get(ctx, before.Name, metav1.GetOptions{})
	return err == nil && reflect.DeepEqual(before, after)
}

func (k *Kernel) sameReadyCluster(ctx context.Context, p Plan, uid ktypes.UID) bool {
	current, err := k.readyCluster(ctx, p)
	return err == nil && current.GetUID() == uid
}
