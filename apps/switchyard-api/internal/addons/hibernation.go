package addons

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	k8stypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
)

// Hibernation stops a retired managed-Postgres addon's compute and keeps its
// data. It is the reversible half of retirement: the CNPG Cluster object and
// its PVCs stay, so removing the annotation brings the instance back from the
// same volume. Deletion is a different, owner-approved operation (retention
// hold); hibernation never deletes a Cluster, a PVC or a Secret.
//
// CNPG does the stopping. The plan refuses a cluster the operator cannot
// manage: hibernation is a step of the operator's reconcile, and the operator
// stops before it when it cannot read an instance's status («Instance Status
// Extraction Error»). Deleting the pod by hand does not help there: on
// 2026-10-24 (crea-sim kernel01g) CNPG recreated it within seconds and the
// hibernation condition stayed False. The fix for that state is network
// (operator-ingress policy, postgres_control_plane.go), not this operation.
//
// Two writes, in order:
//  1. annotate the Cluster cnpg.io/hibernation=on (an Update carrying the
//     reviewed resourceVersion, so a Cluster changed since the plan is refused);
//     then wait, bounded, for CNPG's cnpg.io/hibernation condition to be True
//     with no instance pod left;
//  2. only then scale the per-addon exporter Deployment to zero: it scrapes a
//     database that no longer runs and still holds its own CPU request.
//
// Success is reported only once CNPG confirms. An unconfirmed apply returns
// ErrHibernationUnconfirmed with the annotation on; a later dry-run shows
// where CNPG got to, and the same apply finishes the job.
const (
	CNPGHibernationAnnotation = "cnpg.io/hibernation"
	cnpgClusterLabel          = "cnpg.io/cluster"
	cnpgInstanceRoleLabel     = "cnpg.io/instanceRole"
	// cnpgHealthyPhase is the only phase in which the operator is known to be
	// managing the cluster; a hibernated cluster reports it too.
	cnpgHealthyPhase = "Cluster in healthy state"
)

var (
	// hibernationConfirmTimeout bounds the apply's wait for CNPG. It stays
	// under the edge proxy's 100 s request limit; tests shorten it.
	hibernationConfirmTimeout = 75 * time.Second
	hibernationPollInterval   = 3 * time.Second
)

var (
	// ErrHibernationUnsupported: not a provisioned managed-Postgres addon.
	ErrHibernationUnsupported = errors.New("hibernation applies only to a provisioned managed-Postgres addon")
	// ErrHibernationPlanChanged: the live state no longer hashes to the
	// reviewed plan; the caller must take a new dry-run.
	ErrHibernationPlanChanged = errors.New("the cluster changed since the reviewed plan; obtain a new dry-run")
	// ErrHibernationBlocked: the operator is not managing the cluster, so it
	// would not act on the annotation. Nothing was changed.
	ErrHibernationBlocked = errors.New("CNPG is not managing this cluster, so it cannot hibernate it; nothing was changed")
	// ErrHibernationUnconfirmed: the annotation is on but CNPG had not
	// confirmed within the wait; re-run the dry-run to see its progress.
	ErrHibernationUnconfirmed = errors.New("annotation on, but CNPG has not confirmed hibernation yet; the exporter was left running")
)

// HibernationPod is one instance pod the plan will delete.
type HibernationPod struct {
	Name string `json:"name"`
	UID  string `json:"uid"`
}

// HibernationPlan is the deterministic, secret-free plan. PlanHash covers
// every field the apply acts on, so an apply carrying it acts on exactly the
// reviewed objects.
type HibernationPlan struct {
	AddonID         string `json:"addon_id"`
	Namespace       string `json:"namespace"`
	Cluster         string `json:"cluster"`
	ClusterUID      string `json:"cluster_uid"`
	ResourceVersion string `json:"resource_version"`
	// Phase is the CNPG cluster phase; Blocked explains why the operator would
	// not act (empty when it would).
	Phase   string `json:"phase"`
	Blocked string `json:"blocked,omitempty"`
	// AlreadyHibernated: the annotation is on. Hibernated: CNPG's
	// cnpg.io/hibernation condition is True, i.e. CNPG has acted on it.
	AlreadyHibernated bool             `json:"already_hibernated"`
	Hibernated        bool             `json:"hibernated"`
	InstancePods      []HibernationPod `json:"instance_pods"`
	Exporter          string           `json:"exporter,omitempty"`
	ExporterReplicas  int32            `json:"exporter_replicas"`
	PlanHash          string           `json:"plan_hash"`
}

// HibernationResult reports what the apply changed.
type HibernationResult struct {
	Plan        HibernationPlan `json:"plan"`
	Annotated   bool            `json:"annotated"`
	Confirmed   bool            `json:"hibernation_confirmed"`
	ExporterOff bool            `json:"exporter_scaled_to_zero"`
}

// PlanPostgresHibernation reads the Cluster, its instance pods and its
// exporter. It writes nothing.
func PlanPostgresHibernation(ctx context.Context, dyn dynamic.Interface, kube kubernetes.Interface, addon *types.DatabaseAddon) (*HibernationPlan, error) {
	if addon == nil || addon.Type != types.DatabaseAddonTypePostgres || addon.K8sNamespace == "" || addon.K8sResourceName == "" {
		return nil, ErrHibernationUnsupported
	}
	if dyn == nil || kube == nil {
		return nil, fmt.Errorf("kubernetes clients are unavailable")
	}
	ns, name := addon.K8sNamespace, addon.K8sResourceName
	cluster, err := dyn.Resource(cnpgGVR).Namespace(ns).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("read CNPG cluster %s/%s: %w", ns, name, err)
	}
	plan := &HibernationPlan{
		AddonID: addon.ID.String(), Namespace: ns, Cluster: name,
		ClusterUID: string(cluster.GetUID()), ResourceVersion: cluster.GetResourceVersion(),
		AlreadyHibernated: cluster.GetAnnotations()[CNPGHibernationAnnotation] == "on",
		Hibernated:        cnpgConditionTrue(cluster, CNPGHibernationAnnotation),
		InstancePods:      []HibernationPod{},
	}
	plan.Phase, _, _ = unstructured.NestedString(cluster.Object, "status", "phase")
	if !plan.Hibernated && plan.Phase != cnpgHealthyPhase {
		plan.Blocked = fmt.Sprintf("CNPG reports «%s», not «%s»: the operator would not act on the annotation. Check that it can reach the instance (operator-ingress NetworkPolicy, port %d) and plan again once the cluster is healthy", plan.Phase, cnpgHealthyPhase, cnpgInstanceStatusPort)
	}
	pods, err := kube.CoreV1().Pods(ns).List(ctx, metav1.ListOptions{LabelSelector: cnpgClusterLabel + "=" + name})
	if err != nil {
		return nil, fmt.Errorf("list instance pods of %s/%s: %w", ns, name, err)
	}
	for _, pod := range pods.Items {
		// Only instance pods (they carry the role label); a backup or join Job
		// pod of the same cluster is not part of hibernation.
		_, role := pod.Labels[cnpgInstanceRoleLabel]
		if pod.Labels["cnpg.io/podRole"] != "instance" && !role && pod.Labels["role"] == "" {
			continue
		}
		plan.InstancePods = append(plan.InstancePods, HibernationPod{Name: pod.Name, UID: string(pod.UID)})
	}
	sort.Slice(plan.InstancePods, func(i, j int) bool { return plan.InstancePods[i].Name < plan.InstancePods[j].Name })
	exporter, err := kube.AppsV1().Deployments(ns).Get(ctx, exporterResourceName(name), metav1.GetOptions{})
	switch {
	case err == nil:
		plan.Exporter = exporter.Name
		if exporter.Spec.Replicas != nil {
			plan.ExporterReplicas = *exporter.Spec.Replicas
		} else {
			plan.ExporterReplicas = 1
		}
	case k8serrors.IsNotFound(err):
	default:
		return nil, fmt.Errorf("read exporter of %s/%s: %w", ns, name, err)
	}
	plan.PlanHash = hibernationPlanHash(*plan)
	return plan, nil
}

// Done reports whether CNPG has hibernated the cluster and nothing is left to
// stop.
func (p HibernationPlan) Done() bool {
	return p.Hibernated && len(p.InstancePods) == 0 && (p.Exporter == "" || p.ExporterReplicas == 0)
}

// stopped reports whether CNPG has confirmed and stopped every instance.
func (p HibernationPlan) stopped() bool {
	return p.Hibernated && len(p.InstancePods) == 0
}

// cnpgConditionTrue reports whether the Cluster's status condition of that type
// is True.
func cnpgConditionTrue(cluster *unstructured.Unstructured, conditionType string) bool {
	conditions, _, _ := unstructured.NestedSlice(cluster.Object, "status", "conditions")
	for _, c := range conditions {
		m, ok := c.(map[string]interface{})
		if ok && m["type"] == conditionType {
			return m["status"] == "True"
		}
	}
	return false
}

func hibernationPlanHash(p HibernationPlan) string {
	p.PlanHash = ""
	raw, _ := json.Marshal(p)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// ApplyPostgresHibernation re-plans, refuses a blocked cluster or one whose
// live plan no longer hashes to expectedPlan, annotates it, waits for CNPG to
// confirm, then stops the exporter. A plan that is already Done returns without
// writing (an exact retry is safe); an annotated cluster CNPG has not finished
// is waited on again without a second write.
func ApplyPostgresHibernation(ctx context.Context, dyn dynamic.Interface, kube kubernetes.Interface, addon *types.DatabaseAddon, expectedPlan string) (*HibernationResult, error) {
	plan, err := PlanPostgresHibernation(ctx, dyn, kube, addon)
	if err != nil {
		return nil, err
	}
	result := &HibernationResult{Plan: *plan}
	if plan.Done() {
		result.Confirmed = true
		return result, nil
	}
	if plan.Blocked != "" {
		return nil, fmt.Errorf("%w: %s", ErrHibernationBlocked, plan.Blocked)
	}
	if expectedPlan == "" || expectedPlan != plan.PlanHash {
		return nil, ErrHibernationPlanChanged
	}
	if !plan.AlreadyHibernated {
		cluster, err := dyn.Resource(cnpgGVR).Namespace(plan.Namespace).Get(ctx, plan.Cluster, metav1.GetOptions{})
		if err != nil {
			return nil, fmt.Errorf("re-read CNPG cluster: %w", err)
		}
		if string(cluster.GetUID()) != plan.ClusterUID || cluster.GetResourceVersion() != plan.ResourceVersion {
			return nil, ErrHibernationPlanChanged
		}
		annotations := cluster.GetAnnotations()
		if annotations == nil {
			annotations = map[string]string{}
		}
		annotations[CNPGHibernationAnnotation] = "on"
		cluster.SetAnnotations(annotations)
		if _, err := dyn.Resource(cnpgGVR).Namespace(plan.Namespace).Update(ctx, cluster, metav1.UpdateOptions{FieldManager: "enclii-addon-hibernate"}); err != nil {
			return result, fmt.Errorf("annotate CNPG cluster (nothing else changed): %w", err)
		}
		result.Annotated = true
	}
	if !plan.stopped() {
		if err := waitForCNPGHibernation(ctx, dyn, kube, addon); err != nil {
			return result, err
		}
	}
	result.Confirmed = true
	if plan.Exporter != "" && plan.ExporterReplicas != 0 {
		patch := []byte(`{"spec":{"replicas":0}}`)
		if _, err := kube.AppsV1().Deployments(plan.Namespace).Patch(ctx, plan.Exporter, k8stypes.MergePatchType, patch, metav1.PatchOptions{FieldManager: "enclii-addon-hibernate"}); err != nil && !k8serrors.IsNotFound(err) {
			return result, fmt.Errorf("scale exporter %s to 0 (database already stopped): %w", plan.Exporter, err)
		}
		result.ExporterOff = true
	}
	return result, nil
}

// waitForCNPGHibernation polls until CNPG reports the hibernation condition
// True with no instance pod left, or the bounded wait ends.
func waitForCNPGHibernation(ctx context.Context, dyn dynamic.Interface, kube kubernetes.Interface, addon *types.DatabaseAddon) error {
	ctx, cancel := context.WithTimeout(ctx, hibernationConfirmTimeout)
	defer cancel()
	ticker := time.NewTicker(hibernationPollInterval)
	defer ticker.Stop()
	for {
		live, err := PlanPostgresHibernation(ctx, dyn, kube, addon)
		if err == nil && live.stopped() {
			return nil
		}
		select {
		case <-ctx.Done():
			return ErrHibernationUnconfirmed
		case <-ticker.C:
		}
	}
}
