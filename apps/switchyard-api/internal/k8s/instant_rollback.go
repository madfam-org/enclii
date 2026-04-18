package k8s

import (
	"context"
	"fmt"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8stypes "k8s.io/apimachinery/pkg/types"
)

// =============================================================================
// Instant Rollback via Service-selector flip
// =============================================================================
//
// Traditional rollback (RollbackDeployment above) updates the Deployment's
// container image — this triggers a rolling update which takes 30-90 seconds
// for pods to schedule, pull, start, and become Ready. ArgoCD may reconcile
// in the background and overwrite the change.
//
// InstantRollback takes a different path:
//
//  1. Find the ReplicaSet associated with the target Deployment record (via
//     the `enclii.dev/deployment=<uuid>` label applied in manifest.go).
//  2. If that ReplicaSet has no running pods, scale it to the Deployment's
//     desired replica count (waiting for Ready takes ~60s).
//  3. Add an extra selector key `enclii.dev/deployment=<uuid>` to the K8s
//     Service so traffic routes ONLY to pods owned by that ReplicaSet.
//  4. Audit trail is captured via the `enclii.dev/rollback-*` annotations.
//
// Result: traffic flips in <5s when pods are still running (common case when
// you rolled back within minutes of a bad deploy), <90s when they need to be
// scaled back up. ArgoCD reconciliation in the background naturally catches
// up without conflict because the selector key is additive, not destructive.
//
// Kubernetes default revisionHistoryLimit=10 already preserves old
// ReplicaSets, so no additional retention config is needed. Services that
// override that limit need to keep at least 1 previous revision for this to
// work — the API returns a clear error otherwise.
//
// Failure modes deliberately not handled here (out of scope per P0.5):
//   - StatefulSets (Service selector flip semantics differ)
//   - Services with type=LoadBalancer (works, but flip is at Service level,
//     not external LB — external LB sees it as normal endpoint churn)
//   - Cross-service coordinated rollback (use deployment-groups for that)

// InstantRollbackRequest describes the rollback target.
type InstantRollbackRequest struct {
	// Namespace the Deployment + Service live in.
	Namespace string
	// ServiceName is the K8s name shared by Deployment and Service.
	ServiceName string
	// TargetDeploymentID is the enclii Deployment UUID (written to pods as
	// the `enclii.dev/deployment` label by manifest.go).
	TargetDeploymentID string
	// Actor is the Janua subject performing the rollback (audit annotation).
	Actor string
}

// InstantRollbackResult reports the outcome and timing.
type InstantRollbackResult struct {
	// TookMS is the total wall-clock duration of the flip.
	TookMS int64
	// ScaledUp indicates whether the target ReplicaSet needed to be scaled
	// from 0 before the selector flip (slower path).
	ScaledUp bool
	// FromDeploymentID is the deployment UUID traffic was coming from.
	FromDeploymentID string
	// ReadyReplicas after the flip completed.
	ReadyReplicas int32
}

// InstantRollback flips the K8s Service selector to route traffic to the
// target enclii Deployment's ReplicaSet. Returns timing and diagnostics.
func (c *Client) InstantRollback(ctx context.Context, req InstantRollbackRequest) (*InstantRollbackResult, error) {
	start := time.Now()
	result := &InstantRollbackResult{}

	if req.Namespace == "" || req.ServiceName == "" || req.TargetDeploymentID == "" {
		return nil, fmt.Errorf("namespace, service_name, and target_deployment_id are required")
	}

	// Look up the K8s Deployment (umbrella controller managing ReplicaSets)
	deploy, err := c.Clientset.AppsV1().Deployments(req.Namespace).Get(ctx, req.ServiceName, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("get Deployment %s/%s: %w", req.Namespace, req.ServiceName, err)
	}

	// Look up the K8s Service (routing layer whose selector we'll flip)
	svc, err := c.Clientset.CoreV1().Services(req.Namespace).Get(ctx, req.ServiceName, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("get Service %s/%s: %w", req.Namespace, req.ServiceName, err)
	}

	// Record where traffic is coming from (for audit + response)
	result.FromDeploymentID = svc.Spec.Selector["enclii.dev/deployment"]

	// Find the target ReplicaSet by enclii.dev/deployment label
	targetRS, err := c.findReplicaSetByDeploymentID(ctx, req.Namespace, deploy, req.TargetDeploymentID)
	if err != nil {
		return nil, fmt.Errorf("locate target ReplicaSet: %w", err)
	}
	if targetRS == nil {
		return nil, fmt.Errorf("no ReplicaSet found with enclii.dev/deployment=%s (revisionHistoryLimit may have purged it)", req.TargetDeploymentID)
	}

	// Desired replica count: prefer the current Deployment spec; fall back to
	// 1 if unset (unusual — Deployments without a replica count default to 1).
	desired := int32(1)
	if deploy.Spec.Replicas != nil {
		desired = *deploy.Spec.Replicas
	}

	// If the target RS is scaled to 0, scale it back up and wait for Ready.
	if targetRS.Spec.Replicas == nil || *targetRS.Spec.Replicas < desired {
		result.ScaledUp = true
		targetRS.Spec.Replicas = &desired
		if _, err := c.Clientset.AppsV1().ReplicaSets(req.Namespace).Update(ctx, targetRS, metav1.UpdateOptions{}); err != nil {
			return nil, fmt.Errorf("scale up target ReplicaSet %s: %w", targetRS.Name, err)
		}
		// Bounded wait for target RS pods Ready. 90s aligns with the PR goal.
		ready, err := c.waitForReplicaSetReady(ctx, req.Namespace, targetRS.Name, desired, 90*time.Second)
		if err != nil {
			return nil, fmt.Errorf("wait for target ReplicaSet Ready: %w", err)
		}
		result.ReadyReplicas = ready
	} else {
		// Fast path: RS already running. Read current Ready count.
		current, err := c.Clientset.AppsV1().ReplicaSets(req.Namespace).Get(ctx, targetRS.Name, metav1.GetOptions{})
		if err == nil {
			result.ReadyReplicas = current.Status.ReadyReplicas
		}
	}

	// Flip the Service selector to pin traffic to the target deployment's
	// pods. Compute the new selector purely (see buildFlippedSelector) so
	// it's unit-testable.
	svc.Spec.Selector = buildFlippedSelector(svc.Spec.Selector, req.TargetDeploymentID)

	if svc.Annotations == nil {
		svc.Annotations = map[string]string{}
	}
	svc.Annotations["enclii.dev/rollback-at"] = time.Now().UTC().Format(time.RFC3339)
	svc.Annotations["enclii.dev/rollback-from"] = result.FromDeploymentID
	svc.Annotations["enclii.dev/rollback-to"] = req.TargetDeploymentID
	if req.Actor != "" {
		svc.Annotations["enclii.dev/rollback-actor"] = req.Actor
	}

	if _, err := c.Clientset.CoreV1().Services(req.Namespace).Update(ctx, svc, metav1.UpdateOptions{}); err != nil {
		return nil, fmt.Errorf("flip Service selector: %w", err)
	}

	result.TookMS = time.Since(start).Milliseconds()
	return result, nil
}

// buildFlippedSelector returns a new selector map pinning traffic to the
// target deployment UUID. Preserves all existing keys (non-destructive) so
// ArgoCD reconciliation of the Service won't conflict — ArgoCD will see the
// extra label as unmanaged drift but the Deployment controller continues to
// label pods with both keys, so traffic flow is unaffected.
//
// Pure function for unit testing.
func buildFlippedSelector(existing map[string]string, targetDeploymentID string) map[string]string {
	out := make(map[string]string, len(existing)+1)
	for k, v := range existing {
		out[k] = v
	}
	out["enclii.dev/deployment"] = targetDeploymentID
	return out
}

// findReplicaSetByDeploymentID locates the ReplicaSet owned by `deploy` that
// produced pods labeled with the given enclii deployment UUID. ReplicaSets
// inherit their pod template's labels on spec.template.metadata.labels.
func (c *Client) findReplicaSetByDeploymentID(
	ctx context.Context,
	namespace string,
	deploy *appsv1.Deployment,
	deploymentID string,
) (*appsv1.ReplicaSet, error) {
	rsList, err := c.Clientset.AppsV1().ReplicaSets(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: fmt.Sprintf("enclii.dev/deployment=%s", deploymentID),
	})
	if err != nil {
		return nil, fmt.Errorf("list ReplicaSets: %w", err)
	}

	for i := range rsList.Items {
		rs := &rsList.Items[i]
		if !isOwnedBy(rs, deploy.UID) {
			continue
		}
		return rs, nil
	}
	return nil, nil
}

// isOwnedBy returns true if the ReplicaSet has an OwnerReference to the given
// Deployment UID. Keeps rollback scoped to the intended Deployment — avoids
// accidentally flipping to an RS with the same label from a namespace-mate.
func isOwnedBy(rs *appsv1.ReplicaSet, deployUID k8stypes.UID) bool {
	for _, owner := range rs.OwnerReferences {
		if owner.UID == deployUID {
			return true
		}
	}
	return false
}

// waitForReplicaSetReady polls until ReadyReplicas >= desired or timeout.
func (c *Client) waitForReplicaSetReady(
	ctx context.Context,
	namespace, name string,
	desired int32,
	timeout time.Duration,
) (int32, error) {
	deadline := time.Now().Add(timeout)
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return 0, ctx.Err()
		case <-ticker.C:
			rs, err := c.Clientset.AppsV1().ReplicaSets(namespace).Get(ctx, name, metav1.GetOptions{})
			if err != nil {
				if k8serrors.IsNotFound(err) {
					return 0, fmt.Errorf("ReplicaSet %s/%s vanished mid-rollback", namespace, name)
				}
				// Transient error — keep polling until timeout
				continue
			}
			if rs.Status.ReadyReplicas >= desired {
				return rs.Status.ReadyReplicas, nil
			}
			if time.Now().After(deadline) {
				return rs.Status.ReadyReplicas, fmt.Errorf("timed out waiting for ReplicaSet Ready (got %d/%d after %s)", rs.Status.ReadyReplicas, desired, timeout)
			}
		}
	}
}
