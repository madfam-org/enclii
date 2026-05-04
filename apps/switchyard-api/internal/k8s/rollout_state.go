package k8s

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// RolloutState describes whether a service's newest ReplicaSet has actually
// finished rolling. The status quo (just looking at Deployment.ReadyReplicas)
// reports `healthy` whenever ANY pod is Ready — even when a brand new RS has
// been failing readiness for days while the previous RS keeps serving. That's
// the dishonest case operators need surfaced; see RolloutStateBlocked.
type RolloutState string

const (
	// RolloutStateOK — newest ReplicaSet's status.readyReplicas == status.replicas
	// (all desired replicas of the latest revision are Ready). Truthful "all good".
	RolloutStateOK RolloutState = "ok"

	// RolloutStateProgressing — newest RS exists but isn't fully ready yet, and
	// the rollout is still inside the grace window (default 10 minutes). Normal
	// rolling-update behavior — don't alarm operators yet.
	RolloutStateProgressing RolloutState = "progressing"

	// RolloutStateBlocked — newest RS has ≥1 desired replicas but
	// readyReplicas < desiredReplicas for more than the grace window AND an
	// older RS is still serving traffic (i.e., the deploy hasn't actually
	// landed even though Deployment.ReadyReplicas looks fine). This is the
	// "stuck for 10 days" case the dashboard hides today.
	RolloutStateBlocked RolloutState = "blocked"
)

// RolloutBlockedReason is the most likely root cause when state == Blocked.
// Pulled from pod statuses on the newest RS.
type RolloutBlockedReason string

const (
	RolloutReasonNone               RolloutBlockedReason = ""
	RolloutReasonImagePullBackOff   RolloutBlockedReason = "image_pull_back_off"
	RolloutReasonCrashLoopBackOff   RolloutBlockedReason = "crash_loop_back_off"
	RolloutReasonReadinessProbeFail RolloutBlockedReason = "readiness_probe_failed"
	RolloutReasonPending            RolloutBlockedReason = "pending"
	RolloutReasonUnknown            RolloutBlockedReason = "unknown"
)

// DefaultRolloutGrace is the cutoff between "still rolling" and "blocked".
// Mirrors the Deployment progressDeadlineSeconds default (600s) and our SLO
// for fast deploys; configurable via the `grace` arg if a service legitimately
// needs longer (e.g., warm-up workloads). Kept package-level so tests can
// override deterministically.
const DefaultRolloutGrace = 10 * time.Minute

// RolloutEvaluation is the structured result returned to the API layer.
type RolloutEvaluation struct {
	State         RolloutState
	BlockedReason RolloutBlockedReason
}

// EvaluateRolloutState inspects a Deployment's ReplicaSets (and the newest
// RS's pods, when blocked) to decide whether the rollout is actually OK,
// still progressing, or stuck.
//
// Pure-ish: takes a kubernetes.Interface so unit tests can pass a fake
// clientset with synthetic ReplicaSets/Pods. Errors from the K8s API are
// returned to the caller; callers should treat errors as "unknown" and
// NOT downgrade the existing health field.
func EvaluateRolloutState(
	ctx context.Context,
	client kubernetes.Interface,
	namespace, deploymentName string,
	now time.Time,
	grace time.Duration,
) (RolloutEvaluation, error) {
	if grace <= 0 {
		grace = DefaultRolloutGrace
	}

	deploy, err := client.AppsV1().Deployments(namespace).Get(ctx, deploymentName, metav1.GetOptions{})
	if err != nil {
		return RolloutEvaluation{}, fmt.Errorf("get deployment %s/%s: %w", namespace, deploymentName, err)
	}

	rsList, err := client.AppsV1().ReplicaSets(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return RolloutEvaluation{}, fmt.Errorf("list replicasets in %s: %w", namespace, err)
	}

	owned := make([]appsv1.ReplicaSet, 0, len(rsList.Items))
	for i := range rsList.Items {
		if isOwnedBy(&rsList.Items[i], deploy.UID) {
			owned = append(owned, rsList.Items[i])
		}
	}
	if len(owned) == 0 {
		// No owned RS at all — the Deployment is fresh / has no template hash
		// pods yet. Don't lie either way; treat as OK so the legacy `health`
		// field still drives downstream signals.
		return RolloutEvaluation{State: RolloutStateOK}, nil
	}

	// Sort newest-first by creationTimestamp.
	sort.Slice(owned, func(i, j int) bool {
		return owned[i].CreationTimestamp.After(owned[j].CreationTimestamp.Time)
	})

	newest := owned[0]
	desired := int32(0)
	if newest.Spec.Replicas != nil {
		desired = *newest.Spec.Replicas
	}
	ready := newest.Status.ReadyReplicas

	// Case 1: newest RS has no desired replicas (e.g., scaled to 0) — that's
	// not a rollout blockage, just a paused / decommissioned RS.
	if desired == 0 {
		return RolloutEvaluation{State: RolloutStateOK}, nil
	}

	// Case 2: newest RS is fully Ready — the truthful happy path.
	if ready >= desired {
		return RolloutEvaluation{State: RolloutStateOK}, nil
	}

	// Newest RS is not fully Ready. Decide between Progressing vs Blocked
	// based on age of the RS and whether older RSes are still serving.
	age := now.Sub(newest.CreationTimestamp.Time)

	olderStillServing := false
	for _, rs := range owned[1:] {
		if rs.Status.ReadyReplicas > 0 {
			olderStillServing = true
			break
		}
	}

	if age <= grace {
		// Inside the grace window — even if older RS is still serving, this
		// is a normal rolling update.
		return RolloutEvaluation{State: RolloutStateProgressing}, nil
	}

	// Past the grace window AND older RS is still keeping the lights on →
	// the Deployment.ReadyReplicas signal is dishonest. Surface it.
	if olderStillServing {
		reason, _ := classifyBlockedReason(ctx, client, namespace, &newest)
		return RolloutEvaluation{State: RolloutStateBlocked, BlockedReason: reason}, nil
	}

	// No older RS still serving. The Deployment is genuinely down (no replicas
	// anywhere). The existing `health` field already covers that, so don't
	// duplicate the signal — just call it Progressing so we don't double-alarm.
	return RolloutEvaluation{State: RolloutStateProgressing}, nil
}

// classifyBlockedReason inspects pods owned by the newest RS to surface the
// most likely user-actionable cause: ImagePullBackOff, CrashLoopBackOff,
// Readiness, or Pending. Returns RolloutReasonUnknown if no pod statuses
// help (e.g., scheduler hasn't created any pods yet).
func classifyBlockedReason(
	ctx context.Context,
	client kubernetes.Interface,
	namespace string,
	rs *appsv1.ReplicaSet,
) (RolloutBlockedReason, error) {
	selector := buildPodSelector(rs)
	if selector == "" {
		return RolloutReasonUnknown, nil
	}

	pods, err := client.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: selector,
	})
	if err != nil {
		return RolloutReasonUnknown, fmt.Errorf("list pods for rs %s: %w", rs.Name, err)
	}
	if len(pods.Items) == 0 {
		// No pods scheduled at all → almost always a scheduling failure.
		return RolloutReasonPending, nil
	}

	// Tally pod statuses; pick the most common error class. Order matters:
	// ImagePullBackOff > CrashLoopBackOff > ReadinessProbeFail > Pending.
	var imgPull, crashLoop, readinessFail, pending int

	for _, pod := range pods.Items {
		// Pending phase with no containers running yet → scheduling/admission failure.
		if pod.Status.Phase == corev1.PodPending && len(pod.Status.ContainerStatuses) == 0 {
			pending++
			continue
		}

		hasRunningContainer := false
		for _, cs := range pod.Status.ContainerStatuses {
			if cs.State.Waiting != nil {
				switch cs.State.Waiting.Reason {
				case "ImagePullBackOff", "ErrImagePull":
					imgPull++
				case "CrashLoopBackOff":
					crashLoop++
				}
			}
			if cs.State.Running != nil {
				hasRunningContainer = true
				if !cs.Ready {
					// Container running but not Ready → readiness probe failing.
					readinessFail++
				}
			}
		}
		// A pod with no waiting reason and no running container is genuinely
		// stuck pending (e.g., init container looping but not yet recorded).
		if !hasRunningContainer && len(pod.Status.ContainerStatuses) > 0 &&
			imgPull == 0 && crashLoop == 0 && readinessFail == 0 {
			pending++
		}
	}

	switch {
	case imgPull > 0:
		return RolloutReasonImagePullBackOff, nil
	case crashLoop > 0:
		return RolloutReasonCrashLoopBackOff, nil
	case readinessFail > 0:
		return RolloutReasonReadinessProbeFail, nil
	case pending > 0:
		return RolloutReasonPending, nil
	default:
		return RolloutReasonUnknown, nil
	}
}

// buildPodSelector turns the RS's MatchLabels into a label selector string.
// Pods owned by the RS carry these labels (RS controller injects them via
// spec.template.metadata.labels), including the auto-generated
// `pod-template-hash` which uniquely identifies the RS.
func buildPodSelector(rs *appsv1.ReplicaSet) string {
	if rs.Spec.Selector == nil {
		return ""
	}
	parts := make([]string, 0, len(rs.Spec.Selector.MatchLabels))
	for k, v := range rs.Spec.Selector.MatchLabels {
		parts = append(parts, fmt.Sprintf("%s=%s", k, v))
	}
	sort.Strings(parts) // deterministic for tests
	return strings.Join(parts, ",")
}
