package reconciler

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/sirupsen/logrus"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"

	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// ---------------------------------------------------------------------------
// Service network identity for one-off jobs (opt-in)
//
// A one-off Job pod normally carries only the reconciler's bookkeeping labels.
// In a namespace whose NetworkPolicies deny egress by default and admit
// database egress by pod label (`app: dhanam-api`,
// `app.kubernetes.io/part-of: madfam-platform`, `app.kubernetes.io/name: ...`),
// that pod cannot reach its own service's database, so operator scripts that
// need it had to run break-glass with `kubectl exec` into a live pod.
//
// With OneOffJob.NetworkIdentity == "service" the pod is given the service's
// network identity, under three rules:
//
//  1. Image. Only the service's deployed image (job image empty or equal to
//     the Deployment's first container image), or the same repository pinned
//     by digest. Any other image is refused: the job is marked failed with a
//     stored reason and no Job is created. A foreign image never runs with
//     the service's egress.
//  2. Only selected labels. The copied labels are the Deployment pod
//     template's values for exactly the label keys the namespace's
//     NetworkPolicies select on (podSelector matchLabels and matchExpressions
//     keys). Because the pod then agrees with the service's pods on every key
//     any of those policies reads, each policy selects the job pod if and only
//     if it selects the service's pods. That is re-checked policy by policy
//     before the Job is built; any mismatch is a refusal.
//  3. Visible and inert. The pod and Job carry enclii.dev/run-once and
//     enclii.dev/network-identity, plus annotations naming the source
//     Deployment and the copied keys. The pod has a readiness gate nothing ever
//     sets, so it is never Ready and a Service that selects the same labels
//     never routes traffic to it.
// ---------------------------------------------------------------------------

const (
	// LabelRunOnce marks a one-off Job and its pod that carry a service's
	// network identity, so a pod listed under the service's labels is
	// recognisable as a one-off run rather than a replica.
	LabelRunOnce = "enclii.dev/run-once"

	// LabelNetworkIdentity records which network identity the pod runs with.
	LabelNetworkIdentity = "enclii.dev/network-identity"

	// AnnotationNetworkIdentitySource names the Deployment the identity was
	// copied from ("deployment/<name>").
	AnnotationNetworkIdentitySource = "enclii.dev/network-identity-source"

	// AnnotationNetworkIdentityLabels lists the copied label keys, sorted and
	// comma-separated (empty when no policy selects on any label).
	AnnotationNetworkIdentityLabels = "enclii.dev/network-identity-labels"

	// ReadinessGateServesNoTraffic is a pod readiness gate that nothing ever
	// sets to True. A pod with an unmet readiness gate is never Ready, so the
	// EndpointSlice controller lists it as not ready and Services do not route
	// to it, even though it carries the service's selector labels.
	ReadinessGateServesNoTraffic corev1.PodConditionType = "enclii.dev/one-off-job-serves-no-traffic"
)

// reservedIdentityLabelKeys are label keys the reconciler or the Job
// controller set on a one-off pod themselves. Copying a service value over one
// of them would break status sync or the controller's own bookkeeping, and a
// policy keyed on one of them cannot be matched faithfully, so such a policy
// makes the request a refusal.
var reservedIdentityLabelKeys = map[string]bool{
	labelManagedBy:       true,
	LabelOneOffJobID:     true,
	LabelRunOnce:         true,
	LabelNetworkIdentity: true,
	"job-name":           true,
	"controller-uid":     true,
}

// reservedIdentityLabelPrefixes covers the Job controller's
// batch.kubernetes.io/* pod labels.
var reservedIdentityLabelPrefixes = []string{"batch.kubernetes.io/"}

func isReservedIdentityLabelKey(key string) bool {
	if reservedIdentityLabelKeys[key] {
		return true
	}
	for _, prefix := range reservedIdentityLabelPrefixes {
		if strings.HasPrefix(key, prefix) {
			return true
		}
	}
	return false
}

// jobRuntimeSource records which live Deployment a runtime context was
// resolved from. It is nil for every fallback context (service or Deployment
// not resolvable), which is how the network identity step knows there is no
// service identity to copy.
type jobRuntimeSource struct {
	DeploymentName string
	// Image is the Deployment's first container image: the service's
	// currently deployed image.
	Image string
	// PodLabels are the Deployment's pod template labels (a copy).
	PodLabels map[string]string
}

func sourceFromDeployment(deploymentName string, podLabels map[string]string, containers []corev1.Container) *jobRuntimeSource {
	src := &jobRuntimeSource{DeploymentName: deploymentName, PodLabels: map[string]string{}}
	for k, v := range podLabels {
		src.PodLabels[k] = v
	}
	if len(containers) > 0 {
		src.Image = containers[0].Image
	}
	return src
}

// serviceNetworkIdentity is the resolved identity applied to a one-off Job.
type serviceNetworkIdentity struct {
	Source string            // "deployment/<name>"
	Labels map[string]string // copied pod labels (may be empty)
}

// networkIdentityRefusal is a deterministic refusal: retrying cannot change
// the outcome, so the job is marked failed with this reason.
type networkIdentityRefusal struct{ reason string }

func (e *networkIdentityRefusal) Error() string { return e.reason }

func refuseIdentity(format string, args ...interface{}) error {
	return &networkIdentityRefusal{reason: "service network identity refused: " + fmt.Sprintf(format, args...)}
}

var digestPinnedImage = regexp.MustCompile(`@sha256:[a-f0-9]{64}$`)

// imageRepository strips a digest and a tag from an image reference:
// ghcr.io/org/api:v3@sha256:... -> ghcr.io/org/api. A registry port
// (host:5000/org/api) is not mistaken for a tag because only a colon after
// the last slash is a tag separator.
func imageRepository(image string) string {
	if at := strings.Index(image, "@"); at >= 0 {
		image = image[:at]
	}
	if colon := strings.LastIndex(image, ":"); colon > strings.LastIndex(image, "/") {
		image = image[:colon]
	}
	return image
}

// checkServiceIdentityImage applies rule 1. jobImage is the image the job
// asked for (empty = the service's deployed image).
func checkServiceIdentityImage(deployedImage, jobImage string) error {
	if deployedImage == "" {
		return refuseIdentity("the service Deployment has no container image to compare against")
	}
	if jobImage == "" || jobImage == deployedImage {
		return nil
	}
	if digestPinnedImage.MatchString(jobImage) && imageRepository(jobImage) == imageRepository(deployedImage) {
		return nil
	}
	return refuseIdentity("image %q is not the service's deployed image %q (or the same repository pinned by @sha256 digest)", jobImage, deployedImage)
}

// computeServiceNetworkIdentity applies rule 2: it returns the service pod's
// values for exactly the label keys the namespace's policies select on, after
// checking that every policy selects the resulting job pod if and only if it
// selects the service's pods. jobPodLabels are the labels the job pod carries
// besides the identity (bookkeeping plus the Job controller's own).
func computeServiceNetworkIdentity(servicePodLabels map[string]string, policies []networkingv1.NetworkPolicy, jobPodLabels map[string]string) (map[string]string, error) {
	referenced := map[string]bool{}
	selectors := make([]labels.Selector, 0, len(policies))
	for i := range policies {
		np := &policies[i]
		selector, err := metav1.LabelSelectorAsSelector(&np.Spec.PodSelector)
		if err != nil {
			return nil, refuseIdentity("NetworkPolicy %s has a pod selector that cannot be evaluated: %v", np.Name, err)
		}
		selectors = append(selectors, selector)

		keys := make([]string, 0, len(np.Spec.PodSelector.MatchLabels)+len(np.Spec.PodSelector.MatchExpressions))
		for key := range np.Spec.PodSelector.MatchLabels {
			keys = append(keys, key)
		}
		for _, expr := range np.Spec.PodSelector.MatchExpressions {
			keys = append(keys, expr.Key)
		}
		for _, key := range keys {
			if isReservedIdentityLabelKey(key) {
				return nil, refuseIdentity("NetworkPolicy %s selects on %q, a label the reconciler sets on one-off pods itself", np.Name, key)
			}
			referenced[key] = true
		}
	}

	identity := map[string]string{}
	for key := range referenced {
		if value, ok := servicePodLabels[key]; ok {
			identity[key] = value
		}
	}

	finalPodLabels := labels.Set{}
	for k, v := range jobPodLabels {
		finalPodLabels[k] = v
	}
	for k, v := range identity {
		finalPodLabels[k] = v
	}
	servicePod := labels.Set(servicePodLabels)
	for i, selector := range selectors {
		if selector.Matches(servicePod) != selector.Matches(finalPodLabels) {
			return nil, refuseIdentity("NetworkPolicy %s would select the job pod differently from the service's pods", policies[i].Name)
		}
	}

	return identity, nil
}

// oneOffJobNetworkIdentity resolves the identity a pending one-off job asked
// for. It returns (nil, false, nil) when the job asked for none. A refusal is
// recorded on the job (MarkFailed) and reported as refused=true; a transient
// error (the NetworkPolicy list failed for a reason other than RBAC) is
// returned so the job stays pending and the next pass retries.
func (r *TimetableReconciler) oneOffJobNetworkIdentity(ctx context.Context, job *types.OneOffJob, namespace string, rc jobRuntimeContext) (*serviceNetworkIdentity, bool, error) {
	if job.NetworkIdentity == "" {
		return nil, false, nil
	}

	identity, err := r.resolveServiceNetworkIdentity(ctx, job, namespace, rc)
	if err == nil {
		return identity, false, nil
	}

	refusal, ok := err.(*networkIdentityRefusal)
	if !ok {
		return nil, false, err
	}

	r.logger.WithFields(logrus.Fields{
		"one_off_job_id": job.ID,
		"name":           job.Name,
		"namespace":      namespace,
		"image":          rc.Image,
	}).Warn("Timetable: one-off job network identity refused, marking failed")

	if markErr := r.repos.OneOffJobs.MarkFailed(ctx, job.ID, refusal.reason); markErr != nil {
		return nil, true, fmt.Errorf("%s (and recording the refusal failed: %v)", refusal.reason, markErr)
	}
	return nil, true, nil
}

func (r *TimetableReconciler) resolveServiceNetworkIdentity(ctx context.Context, job *types.OneOffJob, namespace string, rc jobRuntimeContext) (*serviceNetworkIdentity, error) {
	if job.NetworkIdentity != types.OneOffJobNetworkIdentityService {
		return nil, refuseIdentity("unknown network identity %q", job.NetworkIdentity)
	}
	if rc.Source == nil {
		return nil, refuseIdentity("the service's Deployment could not be resolved in namespace %s", namespace)
	}
	if err := checkServiceIdentityImage(rc.Source.Image, job.Image); err != nil {
		return nil, err
	}

	list, err := r.k8sClient.Kube().NetworkingV1().NetworkPolicies(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		if apierrors.IsForbidden(err) {
			return nil, refuseIdentity("cannot list NetworkPolicies in namespace %s: %v", namespace, err)
		}
		return nil, fmt.Errorf("list NetworkPolicies in %s: %w", namespace, err)
	}

	jobName := OneOffJobK8sName(job)
	jobPodLabels := oneOffJobBaseLabels(job)
	jobPodLabels[LabelRunOnce] = "true"
	jobPodLabels[LabelNetworkIdentity] = job.NetworkIdentity
	// The Job controller adds these to every pod it creates.
	jobPodLabels["job-name"] = jobName
	jobPodLabels["batch.kubernetes.io/job-name"] = jobName

	copied, err := computeServiceNetworkIdentity(rc.Source.PodLabels, list.Items, jobPodLabels)
	if err != nil {
		return nil, err
	}

	return &serviceNetworkIdentity{
		Source: "deployment/" + rc.Source.DeploymentName,
		Labels: copied,
	}, nil
}

// oneOffJobBaseLabels are the bookkeeping labels every one-off Job and pod
// carries.
func oneOffJobBaseLabels(job *types.OneOffJob) map[string]string {
	return map[string]string{
		labelManagedBy:   labelManagedByValue,
		LabelOneOffJobID: job.ID.String(),
	}
}

// applyTo stamps the identity onto a built one-off Job: identity labels on the
// pod template only, the audit markers and annotations on both the Job and the
// pod, and the never-satisfied readiness gate on the pod. A nil identity
// leaves the Job untouched.
func (id *serviceNetworkIdentity) applyTo(k8sJob *batchv1.Job) {
	if id == nil {
		return
	}

	keys := make([]string, 0, len(id.Labels))
	for key := range id.Labels {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	annotations := map[string]string{
		AnnotationNetworkIdentitySource: id.Source,
		AnnotationNetworkIdentityLabels: strings.Join(keys, ","),
	}
	markers := map[string]string{
		LabelRunOnce:         "true",
		LabelNetworkIdentity: types.OneOffJobNetworkIdentityService,
	}

	for _, meta := range []*metav1.ObjectMeta{&k8sJob.ObjectMeta, &k8sJob.Spec.Template.ObjectMeta} {
		if meta.Labels == nil {
			meta.Labels = map[string]string{}
		}
		if meta.Annotations == nil {
			meta.Annotations = map[string]string{}
		}
		for k, v := range markers {
			meta.Labels[k] = v
		}
		for k, v := range annotations {
			meta.Annotations[k] = v
		}
	}

	for k, v := range id.Labels {
		k8sJob.Spec.Template.Labels[k] = v
	}

	k8sJob.Spec.Template.Spec.ReadinessGates = append(k8sJob.Spec.Template.Spec.ReadinessGates,
		corev1.PodReadinessGate{ConditionType: ReadinessGateServesNoTraffic})
}
