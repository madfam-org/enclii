package januastagemigrate

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"reflect"
	"strings"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ktypes "k8s.io/apimachinery/pkg/types"
)

type SchemaReceipt struct {
	Profile             string     `json:"profile"`
	ImageSourceRevision string     `json:"image_source_revision"`
	GraphSHA256         string     `json:"graph_sha256"`
	Head                string     `json:"head"`
	Applied             int        `json:"applied"`
	Pending             int        `json:"pending"`
	Lineage             string     `json:"lineage"`
	Database            string     `json:"database"`
	OwnerRole           string     `json:"owner_role"`
	ClusterUID          ktypes.UID `json:"cluster_uid"`
	CatalogSHA256       string     `json:"catalog_sha256"`
	PrincipalRows       int        `json:"principal_rows"`
}
type Outcome struct {
	State          string         `json:"state"`
	JobUID         ktypes.UID     `json:"job_uid"`
	OwnerSecretUID ktypes.UID     `json:"owner_secret_uid"`
	PodUID         ktypes.UID     `json:"pod_uid,omitempty"`
	ImageID        string         `json:"image_id,omitempty"`
	StartedAt      string         `json:"started_at,omitempty"`
	FinishedAt     string         `json:"finished_at,omitempty"`
	ExitCode       int32          `json:"exit_code"`
	Schema         *SchemaReceipt `json:"schema,omitempty"`
}

func (o Outcome) Validate(p Plan) error {
	if p.Validate() != nil || o.JobUID == "" || o.OwnerSecretUID == "" || o.JobUID == o.OwnerSecretUID {
		return ErrRefused
	}
	if o.State == "FAILED_NO_RUNTIME" || o.State == "ABORTED_BEFORE_START" {
		if o.PodUID != "" || o.ImageID != "" || o.Schema != nil || o.StartedAt != "" || o.FinishedAt != "" || o.ExitCode != -1 {
			return ErrRefused
		}
		return nil
	}
	if o.PodUID == "" || !imageIDMatches(p, o.ImageID) {
		return ErrRefused
	}
	start, e := time.Parse(time.RFC3339Nano, o.StartedAt)
	finish, f := time.Parse(time.RFC3339Nano, o.FinishedAt)
	if e != nil || f != nil || start.IsZero() || finish.Before(start) {
		return ErrRefused
	}
	if o.State == "FAILED" {
		if o.ExitCode == 0 || o.Schema != nil {
			return ErrRefused
		}
		return nil
	}
	if o.State != "SUCCEEDED" || o.ExitCode != 0 || o.Schema == nil {
		return ErrRefused
	}
	s := o.Schema
	d := p.Source.Descriptor
	if s.Profile != Profile || s.ImageSourceRevision != d.ImageSourceRevision || s.GraphSHA256 != d.GraphSHA256 || s.Head != d.Head || s.Applied != d.Revisions || s.Pending != 0 || s.Lineage != "FRESH_EMPTY" || s.Database != "janua_staging" || s.OwnerRole != "enclii_migrator" || s.ClusterUID != p.ClusterUID || !digest.MatchString(s.CatalogSHA256) || s.PrincipalRows != 0 {
		return ErrRefused
	}
	return nil
}
func imageIDMatches(p Plan, id string) bool {
	return id == p.Source.Descriptor.Image || id == "docker-pullable://"+p.Source.Descriptor.Image || id == "containerd://"+strings.TrimPrefix(p.Source.Descriptor.Image, ImageRepository+"@")
}

type LogReader func(context.Context, string, string, ktypes.UID) ([]byte, error)

func DecodeSchema(raw []byte) (*SchemaReceipt, error) {
	if len(raw) == 0 || len(raw) > 16384 {
		return nil, ErrRefused
	}
	keys := json.NewDecoder(bytes.NewReader(raw))
	token, e := keys.Token()
	if e != nil || token != json.Delim('{') {
		return nil, ErrRefused
	}
	seen := map[string]bool{}
	for keys.More() {
		token, e := keys.Token()
		key, ok := token.(string)
		if e != nil || !ok || seen[key] {
			return nil, ErrRefused
		}
		seen[key] = true
		var value json.RawMessage
		if keys.Decode(&value) != nil {
			return nil, ErrRefused
		}
	}
	if token, e = keys.Token(); e != nil || token != json.Delim('}') {
		return nil, ErrRefused
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var receipt SchemaReceipt
	if decoder.Decode(&receipt) != nil || decoder.Decode(new(any)) != io.EOF {
		return nil, ErrRefused
	}
	return &receipt, nil
}
func condition(job *batchv1.Job, kind batchv1.JobConditionType) bool {
	for _, c := range job.Status.Conditions {
		if c.Type == kind && c.Status == corev1.ConditionTrue {
			return true
		}
	}
	return false
}

// Inspect retains only exact native/schema metadata; raw logs never escape.
// It runs no SQL and can read a terminal historical Job even if current CI or
// held-source activation has moved. Current operator rank remains mandatory.
func (k *Kernel) Inspect(ctx context.Context, p Plan, logs LogReader) (Outcome, error) {
	if k == nil || k.kube == nil || k.journal == nil || p.Validate() != nil || k.journal.CheckCleanup(ctx, p) != nil {
		return Outcome{}, ErrRefused
	}
	native, e := k.journal.Receipt(ctx, p)
	if e != nil {
		return Outcome{}, ErrRefused
	}
	stored, e := k.journal.ReadOutcome(ctx, p)
	if e != nil {
		return Outcome{}, ErrRefused
	}
	desired, _ := p.Job()
	job, e := k.kube.BatchV1().Jobs(desired.Namespace).Get(ctx, desired.Name, metav1.GetOptions{})
	if e != nil || job == nil || job.UID != native.JobUID || job.ResourceVersion == "" || !ClosedJob(desired, job, false) || job.Status.Active != 0 || (!condition(job, batchv1.JobComplete) && !condition(job, batchv1.JobFailed)) {
		return Outcome{}, ErrRefused
	}
	pods, e := k.jobPods(ctx, p, native)
	if e != nil {
		return Outcome{}, ErrRefused
	}
	if _, e := k.retainedOwner(ctx, p, native); e != nil {
		if stored == nil {
			return Outcome{}, ErrRefused
		}
		if _, readErr := k.kube.CoreV1().Secrets(desired.Namespace).Get(ctx, p.SecretName(), metav1.GetOptions{}); !apierrors.IsNotFound(readErr) {
			return Outcome{}, ErrRefused
		}
	}
	o := Outcome{JobUID: native.JobUID, OwnerSecretUID: native.OwnerSecretUID} // pragma: allowlist secret -- retained native owner Secret UID in metadata-only receipt
	if len(pods.Items) == 0 {
		if !condition(job, batchv1.JobFailed) || job.Status.Succeeded != 0 {
			return Outcome{}, ErrRefused
		}
		o.State = "FAILED_NO_RUNTIME"
		o.ExitCode = -1
	} else {
		if len(pods.Items) != 1 {
			return Outcome{}, ErrRefused
		}
		pod := &pods.Items[0]
		if !closedPod(p, job, pod) || len(pod.Status.ContainerStatuses) != 1 {
			return Outcome{}, ErrRefused
		}
		status := pod.Status.ContainerStatuses[0]
		term := status.State.Terminated
		if status.Name != Container || status.RestartCount != 0 || term == nil || term.ContainerID == "" || !imageIDMatches(p, status.ImageID) || term.StartedAt.IsZero() || term.FinishedAt.IsZero() {
			return Outcome{}, ErrRefused
		}
		o.PodUID = pod.UID
		o.ImageID = status.ImageID
		o.ExitCode = term.ExitCode
		o.StartedAt = term.StartedAt.Time.UTC().Format(time.RFC3339Nano)
		o.FinishedAt = term.FinishedAt.Time.UTC().Format(time.RFC3339Nano)
		if term.ExitCode == 0 {
			if !condition(job, batchv1.JobComplete) || job.Status.Succeeded != 1 || job.Status.Failed != 0 || pod.Status.Phase != corev1.PodSucceeded {
				return Outcome{}, ErrRefused
			}
			if stored != nil {
				if stored.State != "SUCCEEDED" || stored.PodUID != pod.UID {
					return Outcome{}, ErrRefused
				}
				o.Schema = stored.Schema
			} else {
				if logs == nil {
					return Outcome{}, ErrRefused
				}
				raw, e := logs(ctx, desired.Namespace, pod.Name, pod.UID)
				if e != nil {
					return Outcome{}, ErrRefused
				}
				o.Schema, e = DecodeSchema(raw)
				if e != nil {
					return Outcome{}, ErrRefused
				}
			}
			o.State = "SUCCEEDED"
			fresh, e := k.kube.CoreV1().Pods(desired.Namespace).Get(ctx, pod.Name, metav1.GetOptions{})
			if e != nil || !reflect.DeepEqual(fresh, pod) {
				return Outcome{}, ErrRefused
			}
		} else {
			if !condition(job, batchv1.JobFailed) || pod.Status.Phase != corev1.PodFailed {
				return Outcome{}, ErrRefused
			}
			o.State = "FAILED"
		}
	}
	if o.Validate(p) != nil || k.journal.Outcome(ctx, p, o) != nil {
		return Outcome{}, ErrRefused
	}
	return o, nil
}

func closedPod(p Plan, job *batchv1.Job, pod *corev1.Pod) bool {
	if pod == nil || pod.UID == "" || pod.ResourceVersion == "" || pod.DeletionTimestamp != nil || pod.Namespace != job.Namespace || len(pod.OwnerReferences) != 1 {
		return false
	}
	owner := pod.OwnerReferences[0]
	if owner.APIVersion != "batch/v1" || owner.Kind != "Job" || owner.Name != job.Name || owner.UID != job.UID || owner.Controller == nil || !*owner.Controller {
		return false
	}
	if pod.Status.Phase != corev1.PodSucceeded && pod.Status.Phase != corev1.PodFailed {
		return false
	}
	candidate := job.DeepCopy()
	candidate.Spec.Template = corev1.PodTemplateSpec{ObjectMeta: metav1.ObjectMeta{Labels: pod.Labels, Annotations: pod.Annotations}, Spec: *pod.Spec.DeepCopy()}
	// Native scheduler binding is not a caller-selected source placement. Do not
	// return/store node identity. All execution and authority fields remain closed.
	candidate.Spec.Template.Spec.NodeName = ""
	if v := candidate.Spec.Template.Spec.Priority; v != nil && *v != 0 {
		return false
	}
	candidate.Spec.Template.Spec.Priority = nil
	if v := candidate.Spec.Template.Spec.PreemptionPolicy; v != nil && *v != corev1.PreemptLowerPriority {
		return false
	}
	candidate.Spec.Template.Spec.PreemptionPolicy = nil
	var retained []corev1.Toleration
	for _, v := range candidate.Spec.Template.Spec.Tolerations {
		standard := (v.Key == "node.kubernetes.io/not-ready" || v.Key == "node.kubernetes.io/unreachable") && v.Operator == corev1.TolerationOpExists && v.Effect == corev1.TaintEffectNoExecute && v.Value == "" && v.TolerationSeconds != nil && *v.TolerationSeconds == 300
		if !standard {
			retained = append(retained, v)
		}
	}
	candidate.Spec.Template.Spec.Tolerations = retained
	desired, _ := p.Job()
	return ClosedJob(desired, candidate, false)
}

// Cleanup never starts/retries a Job or needs current owner/source credentials.
// It deletes only the exact retained short-lived Secret after a confirmed
// terminal outcome and a second no-active-or-terminating-Pod inspection.
func (k *Kernel) Cleanup(ctx context.Context, p Plan, logs LogReader) error {
	if _, e := k.Inspect(ctx, p, logs); e != nil {
		return ErrRefused
	}
	native, e := k.journal.Receipt(ctx, p)
	if e != nil {
		return ErrRefused
	}
	desired, _ := p.Job()
	secrets := k.kube.CoreV1().Secrets(desired.Namespace)
	current, e := secrets.Get(ctx, p.SecretName(), metav1.GetOptions{})
	if apierrors.IsNotFound(e) {
		if native.OwnerRemoved {
			return nil
		}
		return k.journal.Record(ctx, p, "OWNER_SECRET_REMOVED", native.OwnerSecretUID, native.OwnerSecretSHA256)
	}
	if e != nil || current.UID != native.OwnerSecretUID || current.ResourceVersion == "" || current.DeletionTimestamp != nil || len(current.OwnerReferences) != 1 {
		return ErrRefused
	}
	owner := current.OwnerReferences[0]
	if owner.UID != native.JobUID || owner.Kind != "Job" || owner.APIVersion != "batch/v1" || owner.Name != p.JobName() {
		return ErrRefused
	}
	if _, e = k.Inspect(ctx, p, logs); e != nil || k.journal.CheckCleanup(ctx, p) != nil {
		return ErrRefused
	}
	uid, version := current.UID, current.ResourceVersion
	if secrets.Delete(ctx, current.Name, metav1.DeleteOptions{Preconditions: &metav1.Preconditions{UID: &uid, ResourceVersion: &version}}) != nil {
		return receiptError()
	}
	if _, e = secrets.Get(ctx, current.Name, metav1.GetOptions{}); !apierrors.IsNotFound(e) {
		return receiptError()
	}
	return k.journal.Record(ctx, p, "OWNER_SECRET_REMOVED", native.OwnerSecretUID, native.OwnerSecretSHA256)
}
