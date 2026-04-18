package reconciler

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/db"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/k8s"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// CanaryReconciler drives canary rollout state machines.
//
// # Design
//
// A canary rollout is realized as two K8s Deployments behind one Service:
//
//	<service>         — stable Deployment, runs N_stable pods at current digest
//	<service>-canary  — canary Deployment, runs N_canary pods at candidate digest
//
// Both Deployments select on the shared `app=<svc>` label so the existing
// Service routes traffic to BOTH pod sets. Traffic split is achieved by
// replica proportion: at 20% canary + 5 total replicas → 4 stable + 1 canary,
// K8s Service kube-proxy load-balances evenly across all ready endpoints, so
// 1/5 = 20% of requests land on canary pods.
//
// # Promotion
//
// On auto-promote, a new "stable" Deployment is created at the canary digest
// (name `<service>-stable-new`), scaled up to the full replica count, waited
// on for readiness, then the old stable Deployment is scaled to 0. After a
// 15-minute soak period (handled by a follow-up reconciler tick — not inside
// this call), the old stable is renamed back to the canonical service name
// and the temporary `-new` suffix is removed.
//
// For simplicity and safety of this initial implementation we stop one step
// short: we leave the promoted Deployment named `<service>-stable-new` and
// the `<service>-canary` Deployment is deleted. The actual rename to the
// canonical name is a manual step surfaced in the UI as "Finalize". This
// avoids a risky delete-and-recreate of the primary Deployment. A follow-up
// (P2.7.1) can automate that swap once we have observations from production.
//
// # Rollback
//
// Rollback simply scales the canary Deployment to 0 (or deletes it) — the
// stable Deployment was never touched, so traffic returns to 100% stable
// immediately. No Service mutation needed.
type CanaryReconciler struct {
	k8sClient *k8s.Client
	repos     *db.Repositories
	logger    *logrus.Logger
	// httpClient is injected for smoke-endpoint probing. Override in tests.
	httpClient *http.Client
}

// NewCanaryReconciler constructs a canary reconciler.
func NewCanaryReconciler(k8sClient *k8s.Client, repos *db.Repositories, logger *logrus.Logger) *CanaryReconciler {
	return &CanaryReconciler{
		k8sClient: k8sClient,
		repos:     repos,
		logger:    logger,
		httpClient: &http.Client{
			Timeout: 5 * time.Second,
		},
	}
}

// -------------------------------------------------------------------------
// Pure helpers — unit-tested independently of K8s + DB.
// -------------------------------------------------------------------------

// CanarySplit describes the outcome of splitting `total` replicas into a
// stable/canary proportion based on the requested percentage.
type CanarySplit struct {
	Total  int
	Canary int
	Stable int
	// ActualPercentage is the effective percentage after replica rounding.
	// For a 20% request at 4 total replicas = 1 canary + 3 stable = 25%.
	ActualPercentage float64
}

// ComputeCanarySplit allocates replicas between stable and canary sets based
// on the requested percentage.
//
// Invariants:
//   - Canary + Stable == Total (never consumes extra capacity)
//   - Canary >= 1 (you can't have a 0-replica canary)
//   - Stable >= 1 (the whole point is to preserve stable as fallback)
//   - Canary uses ceil(total * pct/100) to bias toward the user's intent
//     (asking for "at least" the requested percentage, not less)
func ComputeCanarySplit(total, percentage int) (CanarySplit, error) {
	if percentage < 5 || percentage > 50 {
		return CanarySplit{}, fmt.Errorf("canary percentage must be between 5 and 50 (got %d)", percentage)
	}
	if total < 2 {
		return CanarySplit{}, fmt.Errorf("canary requires at least 2 replicas (got %d) — single-replica services cannot split traffic", total)
	}

	// Ceiling division: (total * pct + 99) / 100.
	canary := (total*percentage + 99) / 100
	if canary < 1 {
		canary = 1
	}
	// Stable must be at least 1 replica — the whole safety property depends
	// on stable staying up. If ceil pushed canary to `total`, knock it down.
	if canary >= total {
		canary = total - 1
	}
	stable := total - canary

	return CanarySplit{
		Total:            total,
		Canary:           canary,
		Stable:           stable,
		ActualPercentage: float64(canary) * 100.0 / float64(total),
	}, nil
}

// isLegalCanaryTransition reports whether `from → to` is a permitted state
// change. The reconciler uses this to fail-safely if an out-of-date caller
// tries to push a stale transition.
func isLegalCanaryTransition(from, to types.CanaryRolloutState) bool {
	if from == to {
		return true // idempotent — re-applying same state is fine
	}
	switch from {
	case types.CanaryStatePending:
		return to == types.CanaryStateRunning || to == types.CanaryStateFailed
	case types.CanaryStateRunning:
		return to == types.CanaryStateValidating ||
			to == types.CanaryStateAutoRolledBack ||
			to == types.CanaryStateManualRolledBack ||
			to == types.CanaryStateFailed
	case types.CanaryStateValidating:
		return to == types.CanaryStatePromoting ||
			to == types.CanaryStateAutoRolledBack ||
			to == types.CanaryStateManualRolledBack ||
			to == types.CanaryStateFailed
	case types.CanaryStatePromoting:
		return to == types.CanaryStateSucceeded || to == types.CanaryStateFailed
	default:
		// Terminal states never transition.
		return false
	}
}

// canaryDeploymentName returns the K8s Deployment name for the canary side.
func canaryDeploymentName(serviceName string) string {
	return serviceName + "-canary"
}

// newStableDeploymentName returns the K8s Deployment name for the promoted
// new-stable side. The old stable remains at `<service>` until finalized.
func newStableDeploymentName(serviceName string) string {
	return serviceName + "-stable-new"
}

// -------------------------------------------------------------------------
// State machine — called by the controller's periodic tick.
// -------------------------------------------------------------------------

// TickInput bundles the data the reconciler needs for one advancement pass.
type TickInput struct {
	Rollout     *types.CanaryRollout
	Service     *types.Service
	Environment *types.Environment
	StableRel   *types.Release
	CanaryRel   *types.Release
}

// Tick advances the rollout state machine by one step. Safe to call
// repeatedly — each call is idempotent for its current state.
//
// Returns the new state (may be equal to current — means "still waiting").
func (c *CanaryReconciler) Tick(ctx context.Context, in TickInput) (types.CanaryRolloutState, error) {
	ro := in.Rollout
	logger := c.logger.WithFields(logrus.Fields{
		"rollout":    ro.ID,
		"service":    in.Service.Name,
		"state":      ro.State,
		"percentage": ro.CanaryPercentage,
	})

	switch ro.State {
	case types.CanaryStatePending:
		// Build the canary Deployment and scale the stable side down.
		if err := c.ensureCanaryReplicas(ctx, in); err != nil {
			_ = c.repos.CanaryRollouts.UpdateState(ctx, ro.ID, types.CanaryStateFailed, err.Error())
			return types.CanaryStateFailed, err
		}
		logger.Info("canary replicas ensured, advancing to running")
		return c.transition(ctx, ro.ID, types.CanaryStateRunning, "")

	case types.CanaryStateRunning:
		// Wait for canary pods to be Ready. Then enter validation window.
		ready, err := c.canaryReady(ctx, in)
		if err != nil {
			return ro.State, err
		}
		if !ready {
			return ro.State, nil // still coming up — no transition
		}
		logger.Info("canary pods ready, entering validation window")
		return c.transition(ctx, ro.ID, types.CanaryStateValidating, "")

	case types.CanaryStateValidating:
		// Check: validation window elapsed, error rate under threshold, optional smoke endpoint.
		if ro.ValidatingStartedAt == nil {
			// Defensive — should have been set on transition into validating.
			now := time.Now().UTC()
			ro.ValidatingStartedAt = &now
		}
		elapsed := time.Since(*ro.ValidatingStartedAt)
		window := time.Duration(ro.ValidationWindowSeconds) * time.Second

		// Fail-fast: error rate check every tick.
		healthy, healthErr := c.validationHealthCheck(ctx, in)
		if healthErr != nil {
			// Transient lookup error — log and try again next tick. Don't
			// auto-rollback on a metric scrape hiccup.
			logger.WithError(healthErr).Warn("validation health check errored; will retry")
			return ro.State, nil
		}
		if !healthy {
			logger.Warn("canary failed health check, initiating auto-rollback")
			if err := c.scaleDownCanary(ctx, in); err != nil {
				logger.WithError(err).Error("auto-rollback scale-down failed")
			}
			_ = c.repos.CanaryRollouts.SetRollbackReason(ctx, ro.ID, "auto: failed validation health check")
			return c.transition(ctx, ro.ID, types.CanaryStateAutoRolledBack, "canary failed validation")
		}

		if elapsed < window {
			// Still within validation window — keep observing.
			return ro.State, nil
		}
		logger.WithField("validation_elapsed", elapsed).Info("validation window complete, promoting")
		return c.transition(ctx, ro.ID, types.CanaryStatePromoting, "")

	case types.CanaryStatePromoting:
		// Build the new stable from canary digest, scale up, wait for ready,
		// then scale the old stable down.
		if err := c.promote(ctx, in); err != nil {
			_ = c.repos.CanaryRollouts.UpdateState(ctx, ro.ID, types.CanaryStateFailed, err.Error())
			return types.CanaryStateFailed, err
		}
		logger.Info("promotion complete")
		return c.transition(ctx, ro.ID, types.CanaryStateSucceeded, "")

	default:
		return ro.State, nil // terminal states: no work
	}
}

// transition updates state with legality enforcement.
func (c *CanaryReconciler) transition(ctx context.Context, id uuid.UUID, to types.CanaryRolloutState, errMsg string) (types.CanaryRolloutState, error) {
	// Re-read to guard against stale transitions from concurrent actors.
	ro, err := c.repos.CanaryRollouts.GetByID(ctx, id)
	if err != nil {
		return "", fmt.Errorf("re-read rollout: %w", err)
	}
	if !isLegalCanaryTransition(ro.State, to) {
		return ro.State, fmt.Errorf("illegal transition %s → %s", ro.State, to)
	}
	if err := c.repos.CanaryRollouts.UpdateState(ctx, id, to, errMsg); err != nil {
		return ro.State, fmt.Errorf("persist transition: %w", err)
	}
	return to, nil
}

// -------------------------------------------------------------------------
// K8s operations — build/scale/delete canary and new-stable Deployments.
// -------------------------------------------------------------------------

// ensureCanaryReplicas creates the canary Deployment if absent and sets the
// stable Deployment's replica count to the split's stable share.
func (c *CanaryReconciler) ensureCanaryReplicas(ctx context.Context, in TickInput) error {
	ns := in.Environment.KubeNamespace
	if ns == "" {
		return fmt.Errorf("environment has no kube_namespace")
	}
	split := CanarySplit{
		Total:  in.Rollout.TotalReplicas,
		Canary: in.Rollout.CanaryReplicas,
		Stable: in.Rollout.StableReplicas,
	}

	// 1. Scale down stable to StableReplicas (freeing capacity for canary).
	stable, err := c.k8sClient.Clientset.AppsV1().Deployments(ns).Get(ctx, in.Service.Name, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("get stable deployment %s/%s: %w", ns, in.Service.Name, err)
	}
	stableR := int32(split.Stable)
	stable.Spec.Replicas = &stableR
	if _, err := c.k8sClient.Clientset.AppsV1().Deployments(ns).Update(ctx, stable, metav1.UpdateOptions{}); err != nil {
		return fmt.Errorf("scale stable to %d: %w", split.Stable, err)
	}

	// 2. Build canary Deployment (copy of stable with canary digest + name).
	canary := buildCanaryDeployment(stable, canaryDeploymentName(in.Service.Name), in.CanaryRel.ImageURI, int32(split.Canary), in.Rollout.ID)

	existing, getErr := c.k8sClient.Clientset.AppsV1().Deployments(ns).Get(ctx, canary.Name, metav1.GetOptions{})
	if getErr != nil {
		if !k8serrors.IsNotFound(getErr) {
			return fmt.Errorf("check canary deployment: %w", getErr)
		}
		if _, err := c.k8sClient.Clientset.AppsV1().Deployments(ns).Create(ctx, canary, metav1.CreateOptions{}); err != nil {
			return fmt.Errorf("create canary deployment: %w", err)
		}
		return nil
	}
	// Update path: keep existing ResourceVersion + selector (selectors are
	// immutable post-create).
	canary.ResourceVersion = existing.ResourceVersion
	canary.Spec.Selector = existing.Spec.Selector
	for k, v := range existing.Spec.Selector.MatchLabels {
		canary.Spec.Template.Labels[k] = v
	}
	if _, err := c.k8sClient.Clientset.AppsV1().Deployments(ns).Update(ctx, canary, metav1.UpdateOptions{}); err != nil {
		return fmt.Errorf("update canary deployment: %w", err)
	}
	return nil
}

// buildCanaryDeployment clones a stable Deployment spec into a canary-named
// Deployment at the candidate image. Pure function — no K8s calls.
func buildCanaryDeployment(stable *appsv1.Deployment, canaryName, canaryImage string, replicas int32, rolloutID uuid.UUID) *appsv1.Deployment {
	out := stable.DeepCopy()
	// Strip fields that must not carry over.
	out.ResourceVersion = ""
	out.UID = ""
	out.Name = canaryName
	out.Spec.Replicas = &replicas

	// Add labels + annotations marking this as the canary side.
	if out.Labels == nil {
		out.Labels = map[string]string{}
	}
	out.Labels["enclii.dev/canary"] = "true"
	out.Labels["enclii.dev/canary-rollout"] = rolloutID.String()

	if out.Annotations == nil {
		out.Annotations = map[string]string{}
	}
	out.Annotations["enclii.dev/canary-rollout-id"] = rolloutID.String()

	// Template labels: KEEP the shared app=<svc> label so the Service picks
	// both pod sets up. Additionally add a `version=canary` label for
	// observability/Prometheus scraping.
	if out.Spec.Template.Labels == nil {
		out.Spec.Template.Labels = map[string]string{}
	}
	out.Spec.Template.Labels["version"] = "canary"
	out.Spec.Template.Labels["enclii.dev/canary"] = "true"
	out.Spec.Template.Labels["enclii.dev/canary-rollout"] = rolloutID.String()

	// Swap the container image in every container whose image matches the
	// stable's first container — services in this codebase are single-
	// container but we iterate for defensiveness.
	if len(out.Spec.Template.Spec.Containers) > 0 {
		stableImage := stable.Spec.Template.Spec.Containers[0].Image
		for i := range out.Spec.Template.Spec.Containers {
			if out.Spec.Template.Spec.Containers[i].Image == stableImage {
				out.Spec.Template.Spec.Containers[i].Image = canaryImage
			}
		}
	}

	// Give the canary its own Selector on the new label so K8s doesn't reject
	// the Deployment (two deployments can't share a MatchLabels selector).
	out.Spec.Selector = &metav1.LabelSelector{
		MatchLabels: map[string]string{
			"app":                       stable.Labels["app"],
			"enclii.dev/canary-rollout": rolloutID.String(),
		},
	}
	return out
}

// canaryReady returns true when the canary Deployment has ReadyReplicas >= desired.
func (c *CanaryReconciler) canaryReady(ctx context.Context, in TickInput) (bool, error) {
	ns := in.Environment.KubeNamespace
	dep, err := c.k8sClient.Clientset.AppsV1().Deployments(ns).Get(ctx, canaryDeploymentName(in.Service.Name), metav1.GetOptions{})
	if err != nil {
		return false, fmt.Errorf("get canary deployment: %w", err)
	}
	desired := int32(in.Rollout.CanaryReplicas)
	return dep.Status.ReadyReplicas >= desired && dep.Status.UpdatedReplicas >= desired, nil
}

// validationHealthCheck runs every tick during the validating window.
// Currently performs:
//  1. Readiness probe re-check (quick — kubectl-equivalent look at pod conditions).
//  2. Optional smoke endpoint GET (if configured).
//
// Error-rate check against Prometheus is stubbed out — this codebase does not
// yet depend on Prometheus queries in the reconciler. The metrics collector
// in internal/monitoring/ scrapes but doesn't expose a query API. A follow-up
// (P2.7.2) adds that path. For now, the canary's own readiness is the signal.
func (c *CanaryReconciler) validationHealthCheck(ctx context.Context, in TickInput) (bool, error) {
	ready, err := c.canaryReady(ctx, in)
	if err != nil {
		return false, err
	}
	if !ready {
		return false, nil
	}

	// Also check that NO canary pod has transitioned to CrashLoopBackOff
	// during validation — catches late-breaking runtime failures.
	ns := in.Environment.KubeNamespace
	rolloutID := in.Rollout.ID.String()
	pods, err := c.k8sClient.Clientset.CoreV1().Pods(ns).List(ctx, metav1.ListOptions{
		LabelSelector: fmt.Sprintf("enclii.dev/canary-rollout=%s", rolloutID),
	})
	if err != nil {
		return false, fmt.Errorf("list canary pods: %w", err)
	}
	for i := range pods.Items {
		if podHasCrashLoop(&pods.Items[i]) {
			return false, nil
		}
	}

	// Optional smoke endpoint.
	if in.Rollout.SmokeEndpoint != "" {
		if err := c.probeSmokeEndpoint(ctx, in.Rollout.SmokeEndpoint); err != nil {
			c.logger.WithError(err).WithField("endpoint", in.Rollout.SmokeEndpoint).Warn("canary smoke endpoint failed")
			return false, nil
		}
	}
	return true, nil
}

// probeSmokeEndpoint issues a GET and returns error if status != 200.
func (c *CanaryReconciler) probeSmokeEndpoint(ctx context.Context, endpoint string) error {
	// Basic URL sanity — the endpoint comes from user input at rollout-start.
	u, err := url.Parse(endpoint)
	if err != nil {
		return fmt.Errorf("parse smoke endpoint: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("smoke endpoint must be http(s), got %s", u.Scheme)
	}

	req, err := http.NewRequestWithContext(ctx, "GET", endpoint, nil)
	if err != nil {
		return err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer func() {
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("smoke endpoint %s returned %d", endpoint, resp.StatusCode)
	}
	return nil
}

// podHasCrashLoop returns true if any container on the pod is in CrashLoopBackOff.
func podHasCrashLoop(pod *corev1.Pod) bool {
	for _, cs := range pod.Status.ContainerStatuses {
		if cs.State.Waiting != nil && cs.State.Waiting.Reason == "CrashLoopBackOff" && cs.RestartCount >= 2 {
			return true
		}
	}
	return false
}

// promote builds a new stable Deployment from the canary digest, scales up,
// then scales down the old stable. See package docstring for rationale on
// why we stop short of renaming.
func (c *CanaryReconciler) promote(ctx context.Context, in TickInput) error {
	ns := in.Environment.KubeNamespace
	total := int32(in.Rollout.TotalReplicas)

	// 1. Build new-stable Deployment from canary digest, full replicas.
	canary, err := c.k8sClient.Clientset.AppsV1().Deployments(ns).Get(ctx, canaryDeploymentName(in.Service.Name), metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("get canary for promotion: %w", err)
	}
	newStable := canary.DeepCopy()
	newStable.ResourceVersion = ""
	newStable.UID = ""
	newStable.Name = newStableDeploymentName(in.Service.Name)
	newStable.Spec.Replicas = &total
	// New-stable carries the canary digest but is no longer labeled canary.
	delete(newStable.Labels, "enclii.dev/canary")
	if newStable.Spec.Template.Labels != nil {
		delete(newStable.Spec.Template.Labels, "version")
		delete(newStable.Spec.Template.Labels, "enclii.dev/canary")
	}
	newStable.Labels["enclii.dev/new-stable"] = "true"
	newStable.Labels["enclii.dev/canary-rollout"] = in.Rollout.ID.String()
	// Selector must be unique per Deployment; carry the rollout id as the
	// distinguishing key so kube-proxy still routes via the shared app=<svc>
	// label on the Service, but the Deployment's ReplicaSet owns pods only
	// under the rollout selector.
	newStable.Spec.Selector = &metav1.LabelSelector{
		MatchLabels: map[string]string{
			"app":                           canary.Labels["app"],
			"enclii.dev/new-stable-rollout": in.Rollout.ID.String(),
		},
	}
	if newStable.Spec.Template.Labels == nil {
		newStable.Spec.Template.Labels = map[string]string{}
	}
	newStable.Spec.Template.Labels["enclii.dev/new-stable-rollout"] = in.Rollout.ID.String()

	if _, err := c.k8sClient.Clientset.AppsV1().Deployments(ns).Create(ctx, newStable, metav1.CreateOptions{}); err != nil && !k8serrors.IsAlreadyExists(err) {
		return fmt.Errorf("create new-stable: %w", err)
	}

	// 2. Wait up to 5 minutes for new-stable pods to be Ready.
	if err := c.waitForDeploymentReady(ctx, ns, newStable.Name, total, 5*time.Minute); err != nil {
		return fmt.Errorf("new-stable readiness: %w", err)
	}

	// 3. Scale old stable and canary to 0.
	oldStable, err := c.k8sClient.Clientset.AppsV1().Deployments(ns).Get(ctx, in.Service.Name, metav1.GetOptions{})
	if err == nil {
		zero := int32(0)
		oldStable.Spec.Replicas = &zero
		if _, err := c.k8sClient.Clientset.AppsV1().Deployments(ns).Update(ctx, oldStable, metav1.UpdateOptions{}); err != nil {
			return fmt.Errorf("scale old stable to 0: %w", err)
		}
	}

	zero := int32(0)
	canary.Spec.Replicas = &zero
	if _, err := c.k8sClient.Clientset.AppsV1().Deployments(ns).Update(ctx, canary, metav1.UpdateOptions{}); err != nil {
		return fmt.Errorf("scale canary to 0: %w", err)
	}
	return nil
}

// scaleDownCanary takes the canary Deployment to 0 replicas (rollback path).
// The stable Deployment was never touched so traffic snaps back to 100% stable.
func (c *CanaryReconciler) scaleDownCanary(ctx context.Context, in TickInput) error {
	ns := in.Environment.KubeNamespace
	canary, err := c.k8sClient.Clientset.AppsV1().Deployments(ns).Get(ctx, canaryDeploymentName(in.Service.Name), metav1.GetOptions{})
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil // already gone — nothing to roll back
		}
		return fmt.Errorf("get canary for rollback: %w", err)
	}
	zero := int32(0)
	canary.Spec.Replicas = &zero
	if _, err := c.k8sClient.Clientset.AppsV1().Deployments(ns).Update(ctx, canary, metav1.UpdateOptions{}); err != nil {
		return fmt.Errorf("scale canary to 0: %w", err)
	}

	// Also restore the stable deployment's original replica count.
	stable, err := c.k8sClient.Clientset.AppsV1().Deployments(ns).Get(ctx, in.Service.Name, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("get stable for restore: %w", err)
	}
	total := int32(in.Rollout.TotalReplicas)
	stable.Spec.Replicas = &total
	if _, err := c.k8sClient.Clientset.AppsV1().Deployments(ns).Update(ctx, stable, metav1.UpdateOptions{}); err != nil {
		return fmt.Errorf("restore stable replicas: %w", err)
	}
	return nil
}

// ManualRollback is called by the API handler when an operator clicks "Rollback".
func (c *CanaryReconciler) ManualRollback(ctx context.Context, ro *types.CanaryRollout, svc *types.Service, env *types.Environment, reason string) error {
	if ro.State.IsTerminal() {
		return fmt.Errorf("rollout %s is already terminal (%s)", ro.ID, ro.State)
	}
	in := TickInput{Rollout: ro, Service: svc, Environment: env}
	if err := c.scaleDownCanary(ctx, in); err != nil {
		return err
	}
	if reason == "" {
		reason = "manual rollback"
	}
	_ = c.repos.CanaryRollouts.SetRollbackReason(ctx, ro.ID, reason)
	if err := c.repos.CanaryRollouts.UpdateState(ctx, ro.ID, types.CanaryStateManualRolledBack, reason); err != nil {
		return fmt.Errorf("persist manual rollback: %w", err)
	}
	return nil
}

// ManualPromote short-circuits the validation window, forcing an immediate
// promote. The rollout must currently be in `validating` (caller enforces).
func (c *CanaryReconciler) ManualPromote(ctx context.Context, ro *types.CanaryRollout, svc *types.Service, env *types.Environment, stableRel, canaryRel *types.Release) error {
	if ro.State != types.CanaryStateValidating && ro.State != types.CanaryStateRunning {
		return fmt.Errorf("rollout %s: manual promote only allowed from validating/running (got %s)", ro.ID, ro.State)
	}
	in := TickInput{Rollout: ro, Service: svc, Environment: env, StableRel: stableRel, CanaryRel: canaryRel}
	// Move to promoting immediately.
	if err := c.repos.CanaryRollouts.UpdateState(ctx, ro.ID, types.CanaryStatePromoting, ""); err != nil {
		return fmt.Errorf("persist promoting: %w", err)
	}
	ro.State = types.CanaryStatePromoting
	if err := c.promote(ctx, in); err != nil {
		_ = c.repos.CanaryRollouts.UpdateState(ctx, ro.ID, types.CanaryStateFailed, err.Error())
		return fmt.Errorf("promote: %w", err)
	}
	return c.repos.CanaryRollouts.UpdateState(ctx, ro.ID, types.CanaryStateSucceeded, "")
}

// waitForDeploymentReady polls until ReadyReplicas >= desired or timeout.
func (c *CanaryReconciler) waitForDeploymentReady(ctx context.Context, ns, name string, desired int32, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			dep, err := c.k8sClient.Clientset.AppsV1().Deployments(ns).Get(ctx, name, metav1.GetOptions{})
			if err != nil {
				if k8serrors.IsNotFound(err) {
					// Retry — the API server may be racing with our create.
					if time.Now().After(deadline) {
						return fmt.Errorf("deployment %s/%s never appeared", ns, name)
					}
					continue
				}
				return fmt.Errorf("get deployment %s/%s: %w", ns, name, err)
			}
			if dep.Status.ReadyReplicas >= desired && dep.Status.UpdatedReplicas >= desired {
				return nil
			}
			if time.Now().After(deadline) {
				return fmt.Errorf("timed out waiting for %s/%s: ready=%d/%d", ns, name, dep.Status.ReadyReplicas, desired)
			}
		}
	}
}

// -------------------------------------------------------------------------
// Validation (exposed as a package-level function for reuse in API handlers
// and the CLI without having to instantiate a reconciler).
// -------------------------------------------------------------------------

// ValidateRolloutSpec applies the canary feature's invariants to the caller's
// request. Returns a user-readable error on violation.
func ValidateRolloutSpec(spec types.CanaryRolloutSpec) error {
	if strings.TrimSpace(spec.ImageDigest) == "" {
		return fmt.Errorf("digest is required")
	}
	if spec.Percentage < 5 || spec.Percentage > 50 {
		return fmt.Errorf("percentage must be 5-50 (got %d)", spec.Percentage)
	}
	if spec.ValidationWindowMinutes < 1 || spec.ValidationWindowMinutes > 60 {
		return fmt.Errorf("validation_window_minutes must be 1-60 (got %d)", spec.ValidationWindowMinutes)
	}
	if spec.ErrorRateThreshold < 0 || spec.ErrorRateThreshold > 0.5 {
		return fmt.Errorf("error_rate_threshold must be 0.0-0.5 (got %v)", spec.ErrorRateThreshold)
	}
	if spec.SmokeEndpoint != "" {
		u, err := url.Parse(spec.SmokeEndpoint)
		if err != nil {
			return fmt.Errorf("invalid smoke_endpoint URL: %w", err)
		}
		if u.Scheme != "http" && u.Scheme != "https" {
			return fmt.Errorf("smoke_endpoint must be http(s)")
		}
	}
	return nil
}
