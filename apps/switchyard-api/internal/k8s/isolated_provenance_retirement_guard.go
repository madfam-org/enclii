package k8s

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"

	"github.com/google/uuid"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func retirementHold(stage string) error {
	return &initialJobHoldError{stage: stage, cause: fmt.Errorf("provenance retirement held; inspect the same operation")}
}

func initialNeverStartedStatus(status batchv1.JobStatus) bool {
	s := status.DeepCopy()
	if len(s.Conditions) > 1 {
		return false
	}
	for _, condition := range s.Conditions {
		if condition.Type != batchv1.JobSuspended || condition.Status != corev1.ConditionTrue {
			return false
		}
	}
	s.Conditions = nil
	if s.Ready != nil && *s.Ready == 0 {
		s.Ready = nil
	}
	if s.Terminating != nil && *s.Terminating == 0 {
		s.Terminating = nil
	}
	if s.UncountedTerminatedPods != nil && len(s.UncountedTerminatedPods.Succeeded) == 0 && len(s.UncountedTerminatedPods.Failed) == 0 {
		s.UncountedTerminatedPods = nil
	}
	return reflect.DeepEqual(*s, batchv1.JobStatus{})
}

func (c *Client) noInitialExecutionPods(ctx context.Context, namespace string, job *batchv1.Job) bool {
	pods, err := c.kubeClient().CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{Limit: 1000})
	if err != nil || len(pods.Items) > 1000 || pods.Continue != "" || (pods.RemainingItemCount != nil && *pods.RemainingItemCount != 0) {
		return false
	}
	for _, pod := range pods.Items {
		if pod.Labels["enclii.dev/bootstrap-job"] == job.Name || pod.Labels["batch.kubernetes.io/controller-uid"] == string(job.UID) || pod.Labels["controller-uid"] == string(job.UID) || pod.Labels["job-name"] == job.Name || pod.Labels["batch.kubernetes.io/job-name"] == job.Name {
			return false
		}
		for _, owner := range pod.OwnerReferences {
			if owner.UID == job.UID {
				return false
			}
		}
	}
	return true
}

// Rebuild only the fixed READ workload shape. Dummy values are never persisted
// or executed: retirement does not recover an identity, credential or request.
func retirementExpectedJob(p IsolatedInitialJobPlan, job *batchv1.Job) (*batchv1.Job, error) {
	if job == nil || len(job.Spec.Template.Spec.Containers) != 1 || !IsProvenanceReadProfile(p.Profile) {
		return nil, retirementHold("retirement_profile")
	}
	p.Image = job.Spec.Template.Spec.Containers[0].Image
	provenance, known := initialProvenanceFor(p.Profile)
	if !known || !strings.HasPrefix(p.Image, provenance.ImagePrefix) || !initialSHA.MatchString(strings.TrimPrefix(p.Image, provenance.ImagePrefix)) {
		return nil, retirementHold("retirement_image")
	}
	p.Cluster = "unexecuted-reader-scope"
	if p.IdempotencyKey == "" {
		p.IdempotencyKey = "retained-stop-shape" // Shape only; retainedJobName supplies the native name.
	}
	p.Reason = job.Annotations["enclii.dev/operation-reason"]
	p.Data = map[string][]byte{"ca.crt": []byte("unused"), "CREA_SIM_PORTAL_DATABASE_URL": []byte("unused"), "CREA_SIM_DATABASE_NAMESPACE": []byte("unused")}
	if p.Profile == PortalFenceReadProfile {
		p.FenceRead = &PortalFenceReadInput{Version: "crea-sim-fence-kernel-read-v1", KernelID: "crea-sim-map01"}
		p.Data["request.json"], _ = json.Marshal(p.FenceRead)
	} else {
		p.GenerationRead = &PortalGenerationReadInput{Version: "crea-sim-generation-read-v1", ReservationID: uuid.MustParse("00000000-0000-4000-8000-000000000001").String(), KernelID: "crea-sim-map01"}
		p.Data["request.json"], _ = json.Marshal(p.GenerationRead)
	}
	expected, err := BuildIsolatedInitialJob(p)
	if err != nil {
		return nil, retirementHold("retirement_spec")
	}
	fingerprint := job.Annotations["enclii.dev/bootstrap-fingerprint"]
	if !initialSHA.MatchString(fingerprint) {
		return nil, retirementHold("retirement_fingerprint")
	}
	expected.Annotations["enclii.dev/bootstrap-fingerprint"] = fingerprint
	for _, key := range []string{initialPreparedUID, initialPreparedSHA} {
		if value, ok := job.Annotations[key]; ok {
			expected.Annotations[key] = value
		}
	}
	return expected, nil
}

func (c *Client) retirementStopState(ctx context.Context, p IsolatedInitialJobPlan, job *batchv1.Job, claim *corev1.ConfigMap, marked bool) error {
	expected, err := retirementExpectedJob(p, job)
	if err != nil {
		return err
	}
	if claim != nil && marked {
		expected.Annotations[retirementClaimUID] = string(claim.UID)
		expected.Annotations[retirementClaimSHA] = claim.Data["sha256"]
	}
	if !initialJobMatches(job, expected, initialIntent{PlanSHA256: expected.Annotations["enclii.dev/bootstrap-fingerprint"]}) || !reflect.DeepEqual(job.Annotations, expected.Annotations) {
		return retirementHold("retirement_spec")
	}
	if job.Generation != 1 || job.CreationTimestamp.IsZero() || !initialUID.MatchString(string(job.UID)) || !initialUID.MatchString(job.ResourceVersion) || job.DeletionTimestamp != nil || len(job.OwnerReferences) != 0 || len(job.Finalizers) != 0 || job.Spec.Suspend == nil || !*job.Spec.Suspend || !initialNeverStartedStatus(job.Status) {
		return retirementHold("retirement_execution_state")
	}
	if !c.noInitialExecutionPods(ctx, p.Namespace, job) {
		return retirementHold("retirement_pod_evidence")
	}
	cms := c.kubeClient().CoreV1().ConfigMaps(p.Namespace)
	retained, err := cms.Get(ctx, p.jobName()+"-intent", metav1.GetOptions{})
	if claim == nil {
		if !k8serrors.IsNotFound(err) {
			return retirementHold("retirement_intent_exists")
		}
	} else {
		if err != nil || retained.UID != claim.UID || !reflect.DeepEqual(retained.Data, claim.Data) {
			return retirementHold("retirement_claim_changed")
		}
		if _, err = validateRetirementClaim(retained, p); err != nil {
			return retirementHold("retirement_claim_changed")
		}
	}
	if _, err = cms.Get(ctx, p.jobName()+"-receipt", metav1.GetOptions{}); !k8serrors.IsNotFound(err) {
		return retirementHold("retirement_receipt_exists")
	}
	if _, err = c.kubeClient().CoreV1().Secrets(p.Namespace).Get(ctx, p.jobName()+"-credentials", metav1.GetOptions{}); !k8serrors.IsNotFound(err) {
		return retirementHold("retirement_custody_exists")
	}
	if _, err = c.kubeClient().NetworkingV1().NetworkPolicies(p.Namespace).Get(ctx, p.jobName()+"-database", metav1.GetOptions{}); !k8serrors.IsNotFound(err) {
		return retirementHold("retirement_network_exists")
	}
	return nil
}
