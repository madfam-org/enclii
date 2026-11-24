package januastagedb

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"reflect"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/k8s"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	ktypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
)

var clusterGVR = schema.GroupVersionResource{Group: "postgresql.cnpg.io", Version: "v1", Resource: "clusters"}

// Journal must atomically bind the fixed held-service slot, operation ID and
// complete reviewed plan to immutable authority before ANY persistent native
// write. A replay or unknown claim result refuses; inspect that same operation.
type Journal interface {
	Check(context.Context, Plan) error
	Claim(context.Context, Plan) error
	Record(context.Context, Plan, string, string) error
	Receipt(context.Context, Plan) (NativeReceipt, error)
}

// Native callbacks are server-owned observations, never HTTP request values.
// NetworkGate must freshly validate exact bound policies, canonical API peers,
// CNPG operator selectors, namespace isolation and absence of broad additives.
// It must cover both application and addon namespaces. It returns no credentials.
// Apply does not create/repair policies: the separately journalled network phase
// must have installed and qualified them before a Cluster may be persisted.
type NetworkGate func(context.Context, Plan) error

// CapacityBudgetGate reobserves actual capacity and independently approved cost
// against CapacityBudgetSHA256; an available catalog row alone is insufficient.
type CapacityBudgetGate func(context.Context, Plan) error

// NativeReceipt comes from the retained journal, never labels alone.
type NativeReceipt struct{ ClusterUID, RuntimeSecretUID ktypes.UID }

type Kernel struct {
	kube     kubernetes.Interface
	dynamic  dynamic.Interface
	observe  Observer
	journal  Journal
	network  NetworkGate
	capacity CapacityBudgetGate
	verify   func(context.Context, string) error
}

func NewKernel(kube kubernetes.Interface, dynamic dynamic.Interface, observe Observer, journal Journal, network NetworkGate, capacity CapacityBudgetGate, verify func(context.Context, string) error) *Kernel {
	return &Kernel{kube: kube, dynamic: dynamic, observe: observe, journal: journal, network: network, capacity: capacity, verify: verify}
}

func (k *Kernel) prerequisites(ctx context.Context, p Plan) error {
	if k == nil || k.kube == nil || k.dynamic == nil || k.observe == nil || k.journal == nil || k.network == nil || k.capacity == nil || k.verify == nil || p.Revalidate(ctx, k.observe) != nil || k.journal.Check(ctx, p) != nil {
		return ErrRefused
	}
	ns, err := k.kube.CoreV1().Namespaces().Get(ctx, Namespace(p.Authority.ProjectID), metav1.GetOptions{})
	if err != nil || string(ns.UID) != p.AddonNamespaceUID || ns.ResourceVersion == "" || ns.DeletionTimestamp != nil || ns.Labels[k8s.LabelProjectNamespace] != p.Authority.ProjectID.String() || ns.Labels["enclii.dev/verify-signatures"] != "true" {
		return ErrRefused
	}
	for _, name := range []string{"enclii.dev/data-access", "enclii.dev/postgres-consumer", "enclii.dev/redis-consumer"} {
		if v := ns.Labels[name]; v != "" && v != "false" {
			return ErrRefused
		}
	}
	sc, err := k.kube.StorageV1().StorageClasses().Get(ctx, "longhorn-replicated", metav1.GetOptions{})
	if err != nil || string(sc.UID) != p.StorageClassUID || sc.ResourceVersion != p.StorageClassVersion || sc.DeletionTimestamp != nil || sc.ReclaimPolicy == nil || *sc.ReclaimPolicy != corev1.PersistentVolumeReclaimRetain {
		return ErrRefused
	}
	if k.network(ctx, p) != nil || k.capacity(ctx, p) != nil {
		return ErrRefused
	}
	return nil
}

// DryAdmit never reads or writes a Secret. Signature verification, current
// namespace/storage/network and fresh inert authority are mandatory even here.
func (k *Kernel) DryAdmit(ctx context.Context, p Plan) error {
	if k.prerequisites(ctx, p) != nil || k.verify(ctx, types.JanuaStagingPostgresImage) != nil {
		return ErrRefused
	}
	desired, err := p.ClusterManifest()
	if err != nil {
		return ErrRefused
	}
	api := k.dynamic.Resource(clusterGVR).Namespace(desired.GetNamespace())
	if _, err = api.Get(ctx, Cluster, metav1.GetOptions{}); !apierrors.IsNotFound(err) {
		return ErrRefused
	}
	admitted, err := api.Create(ctx, desired, metav1.CreateOptions{DryRun: []string{metav1.DryRunAll}, FieldValidation: metav1.FieldValidationStrict, FieldManager: Profile})
	if err != nil || !ClosedCluster(desired, admitted) {
		return ErrRefused
	}
	return k.prerequisites(ctx, p)
}

// Apply submits one dedicated Cluster only after a durable unique claim. No
// legacy addon Provision, services registration, owner read, migration, intake,
// backup or readiness assertion occurs. A failed/uncertain write is held and
// must not be replayed under another key or adopted from native leftovers.
func (k *Kernel) Apply(ctx context.Context, p Plan) error {
	if k == nil || k.journal == nil || k.DryAdmit(ctx, p) != nil {
		return ErrRefused
	}
	if k.prerequisites(ctx, p) != nil || k.journal.Claim(ctx, p) != nil {
		return ErrRefused
	}
	if k.prerequisites(ctx, p) != nil {
		return ErrRefused
	}
	secretAPI := k.kube.CoreV1().Secrets(Namespace(p.Authority.ProjectID))
	if _, err := secretAPI.Get(ctx, RuntimeSecret, metav1.GetOptions{}); !apierrors.IsNotFound(err) {
		return ErrRefused
	}
	password := make([]byte, 32)
	if _, err := rand.Read(password); err != nil {
		return ErrRefused
	}
	secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: RuntimeSecret, Namespace: Namespace(p.Authority.ProjectID), Labels: map[string]string{"enclii.dev/managed-by": Profile, "enclii.dev/project-id": p.Authority.ProjectID.String(), "enclii.dev/addon-id": p.OperationID.String()}, Annotations: map[string]string{"enclii.dev/staging-plan-sha256": p.SHA256}}, Type: corev1.SecretTypeBasicAuth, Data: map[string][]byte{"username": []byte(RuntimeRole), "password": []byte(hex.EncodeToString(password)), "dbname": []byte(Database), "host": []byte(Host(p.Authority.ProjectID)), "port": []byte("5432")}}
	createdSecret, err := secretAPI.Create(ctx, secret, metav1.CreateOptions{FieldValidation: metav1.FieldValidationStrict, FieldManager: Profile})
	if err != nil || createdSecret == nil || createdSecret.UID == "" || !reflect.DeepEqual(createdSecret.Data, secret.Data) || createdSecret.Type != secret.Type {
		return fmt.Errorf("%w: runtime custody create uncertain; inspect same operation", ErrRefused)
	}
	if k.journal.Record(ctx, p, "RUNTIME_SECRET_CREATED", string(createdSecret.UID)) != nil || k.prerequisites(ctx, p) != nil {
		return ErrRefused
	}
	desired, err := p.ClusterManifest()
	if err != nil {
		return ErrRefused
	}
	created, err := k.dynamic.Resource(clusterGVR).Namespace(desired.GetNamespace()).Create(ctx, desired, metav1.CreateOptions{FieldValidation: metav1.FieldValidationStrict, FieldManager: Profile})
	if err != nil || created == nil || created.GetUID() == "" || !ClosedCluster(desired, created) {
		return fmt.Errorf("%w: Cluster create uncertain; inspect same operation", ErrRefused)
	}
	if k.journal.Record(ctx, p, "CLUSTER_CREATED", string(created.GetUID())) != nil || k.prerequisites(ctx, p) != nil {
		return ErrRefused
	}
	// CAS update only the Secret this operation just created; no adoption/rotation.
	current, err := secretAPI.Get(ctx, RuntimeSecret, metav1.GetOptions{})
	if err != nil || current.UID != createdSecret.UID || current.ResourceVersion == "" || current.Type != createdSecret.Type || len(current.OwnerReferences) != 0 || !reflect.DeepEqual(current.Data, secret.Data) || !reflect.DeepEqual(current.Labels, secret.Labels) || !reflect.DeepEqual(current.Annotations, secret.Annotations) {
		return ErrRefused
	}
	current.OwnerReferences = []metav1.OwnerReference{{APIVersion: "postgresql.cnpg.io/v1", Kind: "Cluster", Name: Cluster, UID: created.GetUID()}}
	owned, err := secretAPI.Update(ctx, current, metav1.UpdateOptions{FieldValidation: metav1.FieldValidationStrict, FieldManager: Profile})
	if err != nil || owned == nil || owned.UID != createdSecret.UID || owned.ResourceVersion == "" || owned.Type != createdSecret.Type || !reflect.DeepEqual(owned.Data, createdSecret.Data) || !reflect.DeepEqual(owned.Labels, createdSecret.Labels) || !reflect.DeepEqual(owned.Annotations, createdSecret.Annotations) || !reflect.DeepEqual(owned.OwnerReferences, current.OwnerReferences) {
		return ErrRefused
	}
	return k.journal.Record(ctx, p, "CLUSTER_SUBMITTED_RUNTIME_UNQUALIFIED", string(created.GetUID()))
}

// ClosedCluster rejects unknown controller-defaulted privilege/import/auth paths.
// Ordinary harmless CNPG defaults may only add the explicitly listed scalars.
func ClosedCluster(expected, actual *unstructured.Unstructured) bool {
	if expected == nil || actual == nil || expected.GetName() != actual.GetName() || expected.GetNamespace() != actual.GetNamespace() {
		return false
	}
	for key, value := range expected.GetLabels() {
		if actual.GetLabels()[key] != value {
			return false
		}
	}
	for key, value := range expected.GetAnnotations() {
		if actual.GetAnnotations()[key] != value {
			return false
		}
	}
	wanted, ok := expected.Object["spec"].(map[string]any)
	if !ok {
		return false
	}
	got, ok := actual.Object["spec"].(map[string]any)
	if !ok {
		return false
	}
	for key, value := range wanted {
		if !reflect.DeepEqual(got[key], value) {
			return false
		}
	}
	for key, value := range got {
		if _, ok := wanted[key]; !ok {
			switch key {
			case "logLevel":
				if value != "info" {
					return false
				}
			case "enableAzurePVCUpdates":
				if value != false {
					return false
				}
			case "startDelay", "stopDelay", "switchoverDelay", "smartShutdownTimeout", "failoverDelay", "promotionTimeout", "minSyncReplicas", "maxSyncReplicas":
				n, ok := value.(int64)
				if !ok || n < 0 || n > 40000000 {
					return false
				}
			default:
				return false
			}
		}
	}
	return true
}

// RuntimeBundle and OwnerBundle stay server-side. Neither implements a wire
// response or accepts a supplied URI; callers must not log or export them.
type RuntimeBundle struct {
	DatabaseURL string     `json:"-"`
	PublicCA    []byte     `json:"-"`
	ClusterUID  ktypes.UID `json:"-"`
}
type OwnerBundle struct {
	DatabaseURL string     `json:"-"`
	PublicCA    []byte     `json:"-"`
	ClusterUID  ktypes.UID `json:"-"`
}
