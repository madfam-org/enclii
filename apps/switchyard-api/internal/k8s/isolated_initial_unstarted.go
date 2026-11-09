package k8s

import (
	"context"
	"fmt"
	"reflect"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Only the bounded reader/close profiles may finish a create interrupted before
// immutable intent retention. Generation one is essential: an unsuspend and
// resuspend advances it even when Kubernetes clears StartTime. This is never
// recovery of an activated Job or of an initial migration/seed.
func (c *Client) checkUnstartedInitialRead(ctx context.Context, p IsolatedInitialJobPlan, job, expected *batchv1.Job, intent initialIntent) error {
	held := func(stage string) error {
		return &initialJobHoldError{stage: stage, cause: fmt.Errorf("existing Job without intent is not an exact unstarted bounded operation; held")}
	}
	if !boundedInitialProfile(p.Profile) {
		return held("unstarted_profile")
	}
	prepared, err := c.readInitialPreparation(ctx, p, intent)
	if err != nil {
		return held("unstarted_original_preparation")
	}
	bindInitialPreparation(expected, prepared)
	if !initialJobMatches(job, expected, intent) {
		return held("unstarted_spec")
	}
	if job.Generation != 1 || job.CreationTimestamp.IsZero() ||
		!initialUID.MatchString(string(job.UID)) || !initialUID.MatchString(job.ResourceVersion) ||
		job.DeletionTimestamp != nil || len(job.OwnerReferences) != 0 || len(job.Finalizers) != 0 ||
		!reflect.DeepEqual(job.Annotations, expected.Annotations) || job.Spec.Suspend == nil || !*job.Spec.Suspend {
		return held("unstarted_identity")
	}
	if prepared.CreationTimestamp.IsZero() || prepared.CreationTimestamp.After(job.CreationTimestamp.Time) {
		return held("unstarted_original_preparation")
	}
	status := job.Status.DeepCopy()
	for _, condition := range status.Conditions {
		if condition.Type != batchv1.JobSuspended || condition.Status != corev1.ConditionTrue {
			return held("unstarted_execution_status")
		}
	}
	if len(status.Conditions) > 1 {
		return held("unstarted_execution_status")
	}
	status.Conditions = nil
	if status.Ready != nil && *status.Ready == 0 {
		status.Ready = nil
	}
	if status.Terminating != nil && *status.Terminating == 0 {
		status.Terminating = nil
	}
	if status.UncountedTerminatedPods != nil && len(status.UncountedTerminatedPods.Succeeded) == 0 && len(status.UncountedTerminatedPods.Failed) == 0 {
		status.UncountedTerminatedPods = nil
	}
	if !reflect.DeepEqual(*status, batchv1.JobStatus{}) {
		return held("unstarted_execution_status")
	}
	// A full bounded namespace list also catches owned Pods whose generated
	// controller label is missing. An incomplete listing cannot prove no start.
	pods, err := c.kubeClient().CoreV1().Pods(p.Namespace).List(ctx, metav1.ListOptions{Limit: 1000})
	if err != nil || pods.Continue != "" || (pods.RemainingItemCount != nil && *pods.RemainingItemCount != 0) {
		return held("unstarted_pod_list")
	}
	for _, pod := range pods.Items {
		if pod.Labels["enclii.dev/bootstrap-job"] == job.Name || pod.Labels["batch.kubernetes.io/controller-uid"] == string(job.UID) || pod.Labels["job-name"] == job.Name || pod.Labels["batch.kubernetes.io/job-name"] == job.Name {
			return held("unstarted_pod_evidence")
		}
		for _, owner := range pod.OwnerReferences {
			if owner.UID == job.UID {
				return held("unstarted_pod_evidence")
			}
		}
	}
	// No custody or execution evidence may already exist. Unknown reads hold;
	// an interrupted operation after intent retention keeps its existing rules.
	for _, suffix := range []string{"-intent", "-receipt"} {
		if _, err = c.kubeClient().CoreV1().ConfigMaps(p.Namespace).Get(ctx, job.Name+suffix, metav1.GetOptions{}); !k8serrors.IsNotFound(err) {
			return held("unstarted_retained_record")
		}
	}
	if _, err = c.kubeClient().CoreV1().Secrets(p.Namespace).Get(ctx, job.Name+"-credentials", metav1.GetOptions{}); !k8serrors.IsNotFound(err) {
		return held("unstarted_transient_custody")
	}
	if _, err = c.kubeClient().NetworkingV1().NetworkPolicies(p.Namespace).Get(ctx, job.Name+"-database", metav1.GetOptions{}); !k8serrors.IsNotFound(err) {
		return held("unstarted_transient_network")
	}
	return nil
}
