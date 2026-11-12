package januastagemigrate

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/januastagedb"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ktypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
)

//go:embed command.py
var Command string

// #nosec G101 -- the NAME of the fixed dedicated registry-pull Secret object, not a credential value.
const PullSecret = "janua-staging-registry" // pragma: allowlist secret -- fixed dedicated registry-pull Secret object name; not a credential
const Container = "migrate"

// SourceObservation must come from a server reader that resolves the exact held
// image, verifies its keyless producer/build SHA/main ref/event and required CI,
// and computes the complete reviewed descriptor at that certificate-bound SHA.
// This type's shape is never signature, CI, native or execution authority.
type SourceObservation struct {
	AuthorityID            uuid.UUID  `json:"authority_id"`
	ManifestSourceRevision string     `json:"manifest_source_revision"`
	ManifestSourceSHA256   string     `json:"manifest_source_sha256"`
	Descriptor             Descriptor `json:"descriptor"`
	ObservedAt             string     `json:"observed_at"`
}
type SourceObserver func(context.Context, uuid.UUID) (SourceObservation, error)

type Plan struct {
	Database              januastagedb.Plan `json:"database"`
	OperationID           uuid.UUID         `json:"operation_id"`
	Source                SourceObservation `json:"source"`
	ClusterUID            ktypes.UID        `json:"cluster_uid"`
	RuntimeSecretUID      ktypes.UID        `json:"runtime_secret_uid"`
	RegistrySecretUID     ktypes.UID        `json:"registry_secret_uid"`
	RegistrySecretVersion string            `json:"registry_secret_resource_version"`
	NetworkSHA256         string            `json:"migration_network_sha256"`
	Reason                string            `json:"-"`
	ReasonSHA256          string            `json:"reason_sha256"`
	SHA256                string            `json:"plan_sha256"`
}

func BuildPlan(database januastagedb.Plan, operation uuid.UUID, source SourceObservation, native januastagedb.NativeReceipt, registryUID ktypes.UID, registryVersion, network, reason string) (Plan, error) {
	if database.Validate() != nil || operation == uuid.Nil || source.AuthorityID != database.Authority.ID || source.ManifestSourceRevision != database.Authority.SourceRevision || source.ManifestSourceSHA256 != database.Authority.SourceSHA256 || source.Descriptor.Validate() != nil || native.ClusterUID == "" || native.RuntimeSecretUID == "" || registryUID == "" || registryVersion == "" || !digest.MatchString(network) || strings.TrimSpace(reason) == "" || len(reason) > 512 {
		return Plan{}, ErrRefused
	}
	stamp, e := time.Parse(time.RFC3339Nano, source.ObservedAt)
	if e != nil || stamp.IsZero() {
		return Plan{}, ErrRefused
	}
	source.ObservedAt = ""
	r := sha256.Sum256([]byte(reason))
	p := Plan{Database: database, OperationID: operation, Source: source, ClusterUID: native.ClusterUID, RuntimeSecretUID: native.RuntimeSecretUID, RegistrySecretUID: registryUID, RegistrySecretVersion: registryVersion, NetworkSHA256: network, Reason: reason, ReasonSHA256: hex.EncodeToString(r[:])}
	p.SHA256 = hash(p)
	return p, nil
}
func (p Plan) Validate() error {
	source := p.Source
	source.ObservedAt = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC).Format(time.RFC3339Nano)
	want, e := BuildPlan(p.Database, p.OperationID, source, januastagedb.NativeReceipt{ClusterUID: p.ClusterUID, RuntimeSecretUID: p.RuntimeSecretUID}, p.RegistrySecretUID, p.RegistrySecretVersion, p.NetworkSHA256, p.Reason)
	if e != nil || p.Source.ObservedAt != "" || p.SHA256 != want.SHA256 || p.ReasonSHA256 != want.ReasonSHA256 {
		return ErrRefused
	}
	return nil
}
func (p Plan) Revalidate(ctx context.Context, observer SourceObserver) error {
	if p.Validate() != nil || observer == nil {
		return ErrRefused
	}
	current, e := observer(ctx, p.Source.AuthorityID)
	if e != nil {
		return ErrRefused
	}
	stamp, e := time.Parse(time.RFC3339Nano, current.ObservedAt)
	if e != nil || time.Since(stamp) < 0 || time.Since(stamp) > 30*time.Second {
		return ErrRefused
	}
	rebuilt, e := BuildPlan(p.Database, p.OperationID, current, januastagedb.NativeReceipt{ClusterUID: p.ClusterUID, RuntimeSecretUID: p.RuntimeSecretUID}, p.RegistrySecretUID, p.RegistrySecretVersion, p.NetworkSHA256, p.Reason)
	if e != nil || rebuilt.SHA256 != p.SHA256 {
		return ErrRefused
	}
	return nil
}
func (p Plan) JobName() string {
	return "janua-migrate-" + strings.ReplaceAll(p.OperationID.String(), "-", "")
}
func (p Plan) SecretName() string { return p.JobName() + "-owner" }
func (p Plan) labels() map[string]string {
	return map[string]string{"enclii.dev/managed-by": Profile, "enclii.dev/project": p.Database.Authority.ProjectID.String(), "enclii.dev/migration-operation": p.OperationID.String(), "enclii.dev/janua-migrator": Profile}
}
func (p Plan) annotations() map[string]string {
	return map[string]string{"enclii.dev/migration-plan-sha256": p.SHA256, "enclii.dev/source-graph-sha256": p.Source.Descriptor.GraphSHA256, "enclii.dev/cluster-uid": string(p.ClusterUID)}
}

// Job is always initially suspended. It contains fixed source-selected code and
// no owner/runtime/JWT/provider credentials, envFrom, PVC, service account token,
// shell, reset, stamp, retry, TTL or production image/namespace selector.
func (p Plan) Job() (*batchv1.Job, error) {
	if p.Validate() != nil {
		return nil, ErrRefused
	}
	namespace := januastagedb.Namespace(p.Database.Authority.ProjectID)
	env := []corev1.EnvVar{
		{Name: "EXPECTED_NAMESPACE", Value: namespace},
		{Name: "POD_NAMESPACE", ValueFrom: &corev1.EnvVarSource{FieldRef: &corev1.ObjectFieldSelector{APIVersion: "v1", FieldPath: "metadata.namespace"}}},
		{Name: "EXPECTED_CLUSTER_UID", Value: string(p.ClusterUID)},
		{Name: "EXPECTED_GRAPH_SHA256", Value: p.Source.Descriptor.GraphSHA256},
		{Name: "EXPECTED_HEAD", Value: p.Source.Descriptor.Head},
		{Name: "EXPECTED_IMAGE", Value: p.Source.Descriptor.Image},
		{Name: "EXPECTED_IMAGE_SOURCE_REVISION", Value: p.Source.Descriptor.ImageSourceRevision},
		{Name: "PYTHONDONTWRITEBYTECODE", Value: "1"}, {Name: "PYTHONUNBUFFERED", Value: "1"},
	}
	container := corev1.Container{
		Name: Container, Image: p.Source.Descriptor.Image, ImagePullPolicy: corev1.PullAlways,
		WorkingDir: "/app", Command: []string{"python", "-c", Command}, Env: env,
		SecurityContext: &corev1.SecurityContext{
			Privileged: ptr.To(false), AllowPrivilegeEscalation: ptr.To(false), ReadOnlyRootFilesystem: ptr.To(true),
			Capabilities: &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}},
		},
		Resources: corev1.ResourceRequirements{
			Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("100m"), corev1.ResourceMemory: resource.MustParse("256Mi")},
			Limits:   corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("500m"), corev1.ResourceMemory: resource.MustParse("512Mi")},
		},
		VolumeMounts: []corev1.VolumeMount{{Name: "owner", MountPath: "/var/run/janua-migration", ReadOnly: true}, {Name: "tmp", MountPath: "/tmp"}},
	}
	pod := corev1.PodSpec{
		RestartPolicy: corev1.RestartPolicyNever, AutomountServiceAccountToken: ptr.To(false), EnableServiceLinks: ptr.To(false),
		SecurityContext: &corev1.PodSecurityContext{
			RunAsNonRoot: ptr.To(true), RunAsUser: ptr.To(int64(1001)), RunAsGroup: ptr.To(int64(1001)), FSGroup: ptr.To(int64(1001)),
			SeccompProfile: &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault},
		},
		ImagePullSecrets: []corev1.LocalObjectReference{{Name: PullSecret}},
		Containers:       []corev1.Container{container},
		Volumes: []corev1.Volume{
			{Name: "owner", VolumeSource: corev1.VolumeSource{Secret: &corev1.SecretVolumeSource{
				SecretName: p.SecretName(), DefaultMode: ptr.To(int32(0440)),
				Items: []corev1.KeyToPath{{Key: "owner.json", Path: "owner.json"}, {Key: "descriptor.json", Path: "descriptor.json"}, {Key: "ca.crt", Path: "ca.crt"}},
			}}},
			{Name: "tmp", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{SizeLimit: ptr.To(resource.MustParse("128Mi"))}}},
		},
	}
	return &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{Name: p.JobName(), Namespace: namespace, Labels: p.labels(), Annotations: p.annotations()},
		Spec: batchv1.JobSpec{
			Suspend: ptr.To(true), PodReplacementPolicy: ptr.To(batchv1.Failed), BackoffLimit: ptr.To(int32(0)),
			Completions: ptr.To(int32(1)), Parallelism: ptr.To(int32(1)), ActiveDeadlineSeconds: ptr.To(int64(360)),
			Template: corev1.PodTemplateSpec{ObjectMeta: metav1.ObjectMeta{Labels: p.labels(), Annotations: p.annotations()}, Spec: pod},
		},
	}, nil
}

func receiptError() error { return fmt.Errorf("%w: inspect the same retained operation", ErrRefused) }
