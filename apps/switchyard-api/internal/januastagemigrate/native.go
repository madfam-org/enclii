package januastagemigrate

import (
	"context"
	"encoding/json"
	"reflect"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/januastagedb"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ktypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/utils/ptr"
)

type Database interface {
	ObserveReadiness(context.Context, januastagedb.Plan) (januastagedb.NativeReceipt, error)
	OwnerCustody(context.Context, januastagedb.Plan) (*januastagedb.OwnerBundle, error)
}
type Journal interface {
	Check(context.Context, Plan) error
	Claim(context.Context, Plan) error
	Record(context.Context, Plan, string, ktypes.UID, string) error
	Receipt(context.Context, Plan) (NativeReceipt, error)
	CheckCleanup(context.Context, Plan) error
	Outcome(context.Context, Plan, Outcome) error
	ReadOutcome(context.Context, Plan) (*Outcome, error)
}
type NativeReceipt struct {
	JobUID, OwnerSecretUID ktypes.UID
	Started                bool
	OwnerRemoved           bool
	OwnerSecretSHA256      string
}

// These mandatory gates must independently reobserve exact migrator egress/DB
// ingress and fixed same-namespace dedicated registry custody with retained
// UID/version and repo scope. JSON fields, labels or dry admission cannot do so.
type Gate func(context.Context, Plan) error

type Kernel struct {
	kube              kubernetes.Interface
	database          Database
	source            SourceObserver
	network, registry Gate
	journal           Journal
}

func NewKernel(kube kubernetes.Interface, database Database, source SourceObserver, network, registry Gate, journal Journal) *Kernel {
	return &Kernel{kube: kube, database: database, source: source, network: network, registry: registry, journal: journal}
}
func (k *Kernel) revalidate(ctx context.Context, p Plan) error {
	if k == nil || k.kube == nil || k.database == nil || k.source == nil || k.network == nil || k.registry == nil || k.journal == nil || p.Revalidate(ctx, k.source) != nil || k.journal.Check(ctx, p) != nil || k.network(ctx, p) != nil || k.registry(ctx, p) != nil {
		return ErrRefused
	}
	current, e := k.database.ObserveReadiness(ctx, p.Database)
	if e != nil || current.ClusterUID != p.ClusterUID || current.RuntimeSecretUID != p.RuntimeSecretUID { // pragma: allowlist secret -- retained native runtime Secret UID comparison; no credential value
		return ErrRefused
	}
	return nil
}

func ClosedJob(want, actual *batchv1.Job, suspended bool) bool {
	if want == nil || actual == nil || actual.Name != want.Name || actual.Namespace != want.Namespace || actual.DeletionTimestamp != nil || len(actual.OwnerReferences) != 0 || len(actual.Finalizers) != 0 || actual.GenerateName != "" || actual.Spec.Suspend == nil || *actual.Spec.Suspend != suspended {
		return false
	}
	for key, value := range want.Labels {
		if actual.Labels[key] != value {
			return false
		}
	}
	if !reflect.DeepEqual(actual.Annotations, want.Annotations) {
		return false
	}
	for key, value := range want.Annotations {
		if actual.Annotations[key] != value {
			return false
		}
	}
	copy := actual.DeepCopy()
	desired := want.DeepCopy()
	desired.Spec.Suspend = ptr.To(suspended)
	// Only known server-assigned controller identity labels/selectors and safe
	// typed defaults may differ. All Pod/container/volume/authority fields close.
	for _, key := range []string{"controller-uid", "batch.kubernetes.io/controller-uid"} {
		if value, ok := copy.Spec.Template.Labels[key]; ok {
			if actual.UID == "" || value != string(actual.UID) {
				return false
			}
			delete(copy.Spec.Template.Labels, key)
		}
	}
	for _, key := range []string{"job-name", "batch.kubernetes.io/job-name"} {
		if value, ok := copy.Spec.Template.Labels[key]; ok {
			if value != actual.Name {
				return false
			}
			delete(copy.Spec.Template.Labels, key)
		}
	}
	if copy.Spec.Selector != nil {
		if len(copy.Spec.Selector.MatchExpressions) != 0 || len(copy.Spec.Selector.MatchLabels) == 0 {
			return false
		}
		for key, value := range copy.Spec.Selector.MatchLabels {
			if (key != "controller-uid" && key != "batch.kubernetes.io/controller-uid") || value != string(actual.UID) || actual.UID == "" {
				return false
			}
		}
		copy.Spec.Selector = nil
	}
	if copy.Spec.ManualSelector != nil && !*copy.Spec.ManualSelector {
		copy.Spec.ManualSelector = nil
	}
	if copy.Spec.CompletionMode != nil && *copy.Spec.CompletionMode == batchv1.NonIndexedCompletion {
		copy.Spec.CompletionMode = nil
	}
	if copy.Spec.PodReplacementPolicy == nil || *copy.Spec.PodReplacementPolicy != batchv1.Failed {
		return false
	}
	copy.Spec.PodReplacementPolicy = nil
	desired.Spec.PodReplacementPolicy = nil
	pod := &copy.Spec.Template.Spec
	if pod.DNSPolicy == corev1.DNSClusterFirst {
		pod.DNSPolicy = ""
	}
	if pod.SchedulerName == corev1.DefaultSchedulerName {
		pod.SchedulerName = ""
	}
	if pod.TerminationGracePeriodSeconds != nil && *pod.TerminationGracePeriodSeconds == 30 {
		pod.TerminationGracePeriodSeconds = nil
	}
	if pod.ServiceAccountName == "default" {
		pod.ServiceAccountName = ""
	}
	if pod.DeprecatedServiceAccount == "default" {
		pod.DeprecatedServiceAccount = ""
	}
	for i := range pod.Containers {
		c := &pod.Containers[i]
		if c.TerminationMessagePath == corev1.TerminationMessagePathDefault {
			c.TerminationMessagePath = ""
		}
		if c.TerminationMessagePolicy == corev1.TerminationMessageReadFile {
			c.TerminationMessagePolicy = ""
		}
	}
	return reflect.DeepEqual(desired.Spec, copy.Spec)
}

// DryAdmit reads no owner custody or Secret and never claims/mutates resources.
func (k *Kernel) DryAdmit(ctx context.Context, p Plan) error {
	if k.revalidate(ctx, p) != nil {
		return ErrRefused
	}
	desired, e := p.Job()
	if e != nil {
		return ErrRefused
	}
	api := k.kube.BatchV1().Jobs(desired.Namespace)
	if _, e = api.Get(ctx, desired.Name, metav1.GetOptions{}); !apierrors.IsNotFound(e) {
		return ErrRefused
	}
	admitted, e := api.Create(ctx, desired, metav1.CreateOptions{DryRun: []string{metav1.DryRunAll}, FieldValidation: metav1.FieldValidationStrict, FieldManager: Profile})
	if e != nil || !ClosedJob(desired, admitted, true) {
		return ErrRefused
	}
	return k.revalidate(ctx, p)
}

// Start is one initial empty-only migration. The suspended Job and immutable
// claim precede owner read; the exact Job owns its short-lived dedicated Secret.
// Unknown effects stay held and cannot be replayed/adopted with another key.
func (k *Kernel) Start(ctx context.Context, p Plan) error {
	if k.DryAdmit(ctx, p) != nil || k.revalidate(ctx, p) != nil || k.journal.Claim(ctx, p) != nil || k.revalidate(ctx, p) != nil {
		return ErrRefused
	}
	desired, _ := p.Job()
	jobs := k.kube.BatchV1().Jobs(desired.Namespace)
	job, e := jobs.Create(ctx, desired, metav1.CreateOptions{FieldValidation: metav1.FieldValidationStrict, FieldManager: Profile})
	if e != nil || job == nil || job.UID == "" || job.ResourceVersion == "" || !ClosedJob(desired, job, true) {
		return receiptError()
	}
	if k.journal.Record(ctx, p, "SUSPENDED_JOB_CREATED", job.UID, p.SHA256) != nil || k.revalidate(ctx, p) != nil {
		return receiptError()
	}
	owner, e := k.database.OwnerCustody(ctx, p.Database)
	if e != nil || owner == nil || owner.ClusterUID != p.ClusterUID || len(owner.PublicCA) == 0 || owner.DatabaseURL == "" {
		return ErrRefused
	}
	secrets := k.kube.CoreV1().Secrets(desired.Namespace)
	if _, e = secrets.Get(ctx, p.SecretName(), metav1.GetOptions{}); !apierrors.IsNotFound(e) {
		return ErrRefused
	}
	custody, _ := json.Marshal(map[string]string{"database_url": owner.DatabaseURL, "namespace": desired.Namespace, "cluster_uid": string(p.ClusterUID)})
	descriptor, _ := json.Marshal(p.Source.Descriptor)
	secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: p.SecretName(), Namespace: desired.Namespace, Labels: p.labels(), Annotations: p.annotations(), OwnerReferences: []metav1.OwnerReference{{APIVersion: "batch/v1", Kind: "Job", Name: job.Name, UID: job.UID}}}, Type: corev1.SecretTypeOpaque, Immutable: ptr.To(true), Data: map[string][]byte{"owner.json": custody, "descriptor.json": descriptor, "ca.crt": owner.PublicCA}}
	if k.revalidate(ctx, p) != nil {
		return ErrRefused
	}
	created, e := secrets.Create(ctx, secret, metav1.CreateOptions{FieldValidation: metav1.FieldValidationStrict, FieldManager: Profile})
	if e != nil || !sameSecret(secret, created, false) {
		return receiptError()
	}
	if k.journal.Record(ctx, p, "OWNER_SECRET_CREATED", created.UID, secretHash(created)) != nil || k.revalidate(ctx, p) != nil {
		return receiptError()
	}
	// Read back both retained native coordinates immediately before unsuspending.
	current, e := jobs.Get(ctx, job.Name, metav1.GetOptions{})
	if e != nil || current.UID != job.UID || current.ResourceVersion == "" || !ClosedJob(desired, current, true) {
		return ErrRefused
	}
	fresh, e := secrets.Get(ctx, created.Name, metav1.GetOptions{})
	if e != nil || !sameSecret(created, fresh, true) {
		return ErrRefused
	}
	if k.revalidate(ctx, p) != nil {
		return ErrRefused
	}
	current.Spec.Suspend = ptr.To(false)
	running, e := jobs.Update(ctx, current, metav1.UpdateOptions{FieldValidation: metav1.FieldValidationStrict, FieldManager: Profile})
	if e != nil || running == nil || running.UID != job.UID || running.ResourceVersion == "" || !ClosedJob(desired, running, false) {
		return receiptError()
	}
	return k.journal.Record(ctx, p, "JOB_STARTED", job.UID, p.SHA256)
}

func sameSecret(want, actual *corev1.Secret, identity bool) bool {
	return want != nil && actual != nil && actual.Name == want.Name && actual.Namespace == want.Namespace && actual.UID != "" && actual.ResourceVersion != "" && (!identity || actual.UID == want.UID && actual.ResourceVersion == want.ResourceVersion) && actual.DeletionTimestamp == nil && actual.Immutable != nil && *actual.Immutable && actual.Type == want.Type && reflect.DeepEqual(actual.Labels, want.Labels) && reflect.DeepEqual(actual.Annotations, want.Annotations) && reflect.DeepEqual(actual.OwnerReferences, want.OwnerReferences) && reflect.DeepEqual(actual.Data, want.Data)
}

func secretHash(s *corev1.Secret) string { return hash(s.Data) }
