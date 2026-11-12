package januastagemigrate

import (
	"context"
	"reflect"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const abortMarker = "enclii.dev/initial-migration-aborted"

func closedAbortedJob(p Plan, job *batchv1.Job) bool {
	if job == nil || job.Annotations[abortMarker] != p.SHA256 {
		return false
	}
	copy := job.DeepCopy()
	delete(copy.Annotations, abortMarker)
	desired, _ := p.Job()
	return ClosedJob(desired, copy, true)
}
func (k *Kernel) noRuntime(ctx context.Context, p Plan, native NativeReceipt) (*batchv1.Job, error) {
	namespace := p.DatabaseNamespace()
	job, e := k.kube.BatchV1().Jobs(namespace).Get(ctx, p.JobName(), metav1.GetOptions{})
	desired, _ := p.Job()
	if e != nil || job == nil || job.UID != native.JobUID || job.ResourceVersion == "" || (!ClosedJob(desired, job, true) && !closedAbortedJob(p, job)) || job.Status.Active != 0 || job.Status.Succeeded != 0 || job.Status.Failed != 0 || condition(job, batchv1.JobComplete) || condition(job, batchv1.JobFailed) {
		return nil, ErrRefused
	}
	pods, e := k.jobPods(ctx, p, native)
	if e != nil || len(pods.Items) != 0 {
		return nil, ErrRefused
	}
	return job, nil
}
func (p Plan) DatabaseNamespace() string {
	return "project-" + p.Database.Authority.ProjectID.String()[:8]
}
func (k *Kernel) retainedOwner(ctx context.Context, p Plan, native NativeReceipt) (*corev1.Secret, error) {
	secret, e := k.kube.CoreV1().Secrets(p.DatabaseNamespace()).Get(ctx, p.SecretName(), metav1.GetOptions{})
	if e != nil || secret == nil || secret.UID != native.OwnerSecretUID || secret.ResourceVersion == "" || secret.DeletionTimestamp != nil || secret.Immutable == nil || !*secret.Immutable || secret.Type != corev1.SecretTypeOpaque || secretHash(secret) != native.OwnerSecretSHA256 || !reflect.DeepEqual(secret.Labels, p.labels()) || !reflect.DeepEqual(secret.Annotations, p.annotations()) || len(secret.OwnerReferences) != 1 {
		return nil, ErrRefused
	}
	owner := secret.OwnerReferences[0]
	if owner.APIVersion != "batch/v1" || owner.Kind != "Job" || owner.Name != p.JobName() || owner.UID != native.JobUID {
		return nil, ErrRefused
	}
	return secret, nil
}

// AbortBeforeStart removes only known retained pre-start owner custody. A CAS
// abort marker races the exact Job resourceVersion against Start's unsuspend;
// the loser cannot delete credentials from a Job that may be executing. The
// marker is rejected by Start and the durable unique DB slot prohibits restart.
func (k *Kernel) AbortBeforeStart(ctx context.Context, p Plan) error {
	if k == nil || k.kube == nil || k.journal == nil || p.Validate() != nil || k.journal.CheckCleanup(ctx, p) != nil {
		return ErrRefused
	}
	native, e := k.journal.Receipt(ctx, p)
	if e != nil || native.Started || !digest.MatchString(native.OwnerSecretSHA256) {
		return ErrRefused
	}
	job, e := k.noRuntime(ctx, p, native)
	if e != nil {
		return ErrRefused
	}
	api := k.kube.CoreV1().Secrets(p.DatabaseNamespace())
	secret, e := k.retainedOwner(ctx, p, native)
	if e != nil {
		if _, missing := api.Get(ctx, p.SecretName(), metav1.GetOptions{}); !apierrors.IsNotFound(missing) {
			return ErrRefused
		}
		prior, readErr := k.journal.ReadOutcome(ctx, p)
		if readErr != nil || prior == nil || prior.State != "ABORTED_BEFORE_START" || !closedAbortedJob(p, job) {
			return ErrRefused
		}
		if native.OwnerRemoved {
			return nil
		}
		return k.journal.Record(ctx, p, "OWNER_SECRET_REMOVED", native.OwnerSecretUID, native.OwnerSecretSHA256)
	}
	if !closedAbortedJob(p, job) {
		update := job.DeepCopy()
		update.Annotations[abortMarker] = p.SHA256
		updated, e := k.kube.BatchV1().Jobs(job.Namespace).Update(ctx, update, metav1.UpdateOptions{FieldValidation: metav1.FieldValidationStrict, FieldManager: Profile})
		if e != nil || updated == nil || updated.UID != native.JobUID || updated.ResourceVersion == "" || !closedAbortedJob(p, updated) {
			return receiptError()
		}
	}
	if job, e = k.noRuntime(ctx, p, native); e != nil || !closedAbortedJob(p, job) || k.journal.CheckCleanup(ctx, p) != nil {
		return ErrRefused
	}
	outcome := Outcome{State: "ABORTED_BEFORE_START", JobUID: native.JobUID, OwnerSecretUID: native.OwnerSecretUID, ExitCode: -1} // pragma: allowlist secret -- retained native owner Secret UID in metadata-only abort receipt
	if k.journal.Outcome(ctx, p, outcome) != nil {
		return ErrRefused
	}
	if job, e = k.noRuntime(ctx, p, native); e != nil || !closedAbortedJob(p, job) {
		return ErrRefused
	}
	fresh, e := k.retainedOwner(ctx, p, native)
	if e != nil || !reflect.DeepEqual(secret, fresh) {
		return ErrRefused
	}
	uid, version := fresh.UID, fresh.ResourceVersion
	if api.Delete(ctx, fresh.Name, metav1.DeleteOptions{Preconditions: &metav1.Preconditions{UID: &uid, ResourceVersion: &version}}) != nil {
		return receiptError()
	}
	if _, e = api.Get(ctx, fresh.Name, metav1.GetOptions{}); !apierrors.IsNotFound(e) {
		return receiptError()
	}
	return k.journal.Record(ctx, p, "OWNER_SECRET_REMOVED", native.OwnerSecretUID, native.OwnerSecretSHA256)
}

// A mutable label is not a complete no-runtime proof. Read every namespace
// Pod, then bind Job ownership and detect any other owner-secret consumer.
func (k *Kernel) jobPods(ctx context.Context, p Plan, native NativeReceipt) (*corev1.PodList, error) {
	all, e := k.kube.CoreV1().Pods(p.DatabaseNamespace()).List(ctx, metav1.ListOptions{})
	if e != nil || all.Continue != "" || (all.RemainingItemCount != nil && *all.RemainingItemCount > 0) {
		return nil, ErrRefused
	}
	selected := &corev1.PodList{}
	for _, pod := range all.Items {
		owned := false
		for _, owner := range pod.OwnerReferences {
			if owner.UID == native.JobUID {
				owned = true
			}
		}
		labelled := pod.Labels["enclii.dev/migration-operation"] == p.OperationID.String()
		consumes := false
		for _, volume := range pod.Spec.Volumes {
			if volume.Secret != nil && volume.Secret.SecretName == p.SecretName() {
				consumes = true
			}
			if volume.Projected != nil {
				for _, source := range volume.Projected.Sources {
					if source.Secret != nil && source.Secret.Name == p.SecretName() {
						consumes = true
					}
				}
			}
		}
		containers := append(append([]corev1.Container{}, pod.Spec.Containers...), pod.Spec.InitContainers...)
		for _, ephemeral := range pod.Spec.EphemeralContainers {
			containers = append(containers, corev1.Container{Env: ephemeral.Env, EnvFrom: ephemeral.EnvFrom})
		}
		for _, container := range containers {
			for _, env := range container.Env {
				if env.ValueFrom != nil && env.ValueFrom.SecretKeyRef != nil && env.ValueFrom.SecretKeyRef.Name == p.SecretName() {
					consumes = true
				}
			}
			for _, env := range container.EnvFrom {
				if env.SecretRef != nil && env.SecretRef.Name == p.SecretName() {
					consumes = true
				}
			}
		}
		for _, pull := range pod.Spec.ImagePullSecrets {
			if pull.Name == p.SecretName() {
				consumes = true
			}
		}
		if owned || labelled || consumes {
			if !owned || !labelled {
				return nil, ErrRefused
			}
			selected.Items = append(selected.Items, pod)
		}
	}
	return selected, nil
}
