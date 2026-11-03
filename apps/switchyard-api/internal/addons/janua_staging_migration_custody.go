package addons

import (
	"context"
	"fmt"
	"net/url"
	"reflect"
	"strings"

	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8stypes "k8s.io/apimachinery/pkg/types"
)

// JanuaStagingMigrationCustody is server-only, never a credential API/Vault
// projection. Reading it neither creates a role nor authorizes a Job.
type JanuaStagingMigrationCustody struct {
	OwnerURI, RuntimeURI, PublicCA string `json:"-"`
	OwnerUID, OwnerVersion         string
	Runtime                        *JanuaStagingCustody `json:"-"`
}

func (s *AddonService) ReadJanuaStagingMigrationCustody(ctx context.Context, a *types.DatabaseAddon, operation JanuaStagingOperation, journalUID k8stypes.UID, maxConnections int, peers []KarafielControlPeer, operator metav1.LabelSelector, revalidate func(context.Context) error) (*JanuaStagingMigrationCustody, error) {
	runtime, err := s.ReadJanuaStagingCustody(ctx, a, operation, journalUID, maxConnections, peers, operator, revalidate)
	if err != nil {
		return nil, err
	}
	p, ok := s.provisioners[types.DatabaseAddonTypePostgres].(*PostgresProvisioner)
	if !ok || p.k8sClient == nil || p.k8sClient.Kube() == nil {
		return nil, fmt.Errorf("Janua migration custody unavailable")
	}
	secrets := p.k8sClient.Kube().CoreV1().Secrets(a.K8sNamespace)
	owner, err := secrets.Get(ctx, JanuaStagingClusterName+"-app", metav1.GetOptions{})
	host := JanuaStagingClusterName + "-rw." + a.K8sNamespace + ".svc.cluster.local"
	if err != nil || owner == nil || owner.UID == "" || owner.ResourceVersion == "" || owner.DeletionTimestamp != nil || owner.Type != corev1.SecretTypeBasicAuth || len(owner.OwnerReferences) != 1 || !ownedByCNPG(owner, JanuaStagingClusterName, runtime.ClusterUID) || string(owner.Data["username"]) != runtimeMigrator || string(owner.Data["dbname"]) != JanuaStagingDatabase || !ownerHostMatches(string(owner.Data["host"]), host, a) || string(owner.Data["port"]) != "5432" || len(owner.Data["password"]) < 16 || len(owner.Data["password"]) > 1024 || strings.ContainsAny(string(owner.Data["password"]), "\x00\r\n") {
		return nil, fmt.Errorf("Janua migration owner custody refused")
	}
	raw, ok := runtime.Data["database_url"].(string)
	uri, parseErr := url.Parse(raw)
	if !ok || parseErr != nil || uri.User == nil || uri.User.Username() != JanuaStagingRuntime || uri.Hostname() != host || uri.Path != "/"+JanuaStagingDatabase || uri.Query().Get("sslmode") != "verify-full" || uri.Query().Get("sslrootcert") != JanuaStagingCAFile {
		return nil, fmt.Errorf("Janua migration runtime custody refused")
	}
	uri.User = url.UserPassword(runtimeMigrator, string(owner.Data["password"]))
	current, err := secrets.Get(ctx, owner.Name, metav1.GetOptions{})
	if err != nil || current == nil || current.UID != owner.UID || current.ResourceVersion != owner.ResourceVersion || !reflect.DeepEqual(current.Data, owner.Data) || !reflect.DeepEqual(current.OwnerReferences, owner.OwnerReferences) || revalidate(ctx) != nil {
		return nil, fmt.Errorf("Janua migration owner custody changed")
	}
	final, err := s.ReadJanuaStagingCustody(ctx, a, operation, journalUID, maxConnections, peers, operator, revalidate)
	if err != nil || !reflect.DeepEqual(runtime, final) {
		return nil, fmt.Errorf("Janua migration runtime custody changed")
	}
	return &JanuaStagingMigrationCustody{OwnerURI: uri.String(), RuntimeURI: raw, PublicCA: runtime.Data["database_ca"].(string), OwnerUID: string(owner.UID), OwnerVersion: owner.ResourceVersion, Runtime: runtime}, nil
}
