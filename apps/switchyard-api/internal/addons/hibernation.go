package addons

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"

	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
// Three writes, in order:
//  1. annotate the Cluster cnpg.io/hibernation=on (an Update carrying the
//     reviewed resourceVersion, so a Cluster changed since the plan is refused);
//  2. delete each instance pod with a UID precondition. CNPG normally stops the
//     instances itself once the annotation lands, but when it cannot reach an
//     instance ("Instance Status Extraction Error") the pod keeps running and
//     keeps its CPU request; with the annotation on, CNPG does not recreate a
//     deleted pod;
//  3. scale the per-addon exporter Deployment to zero: it scrapes a database
//     that no longer runs and still holds its own CPU request.
const (
	CNPGHibernationAnnotation = "cnpg.io/hibernation"
	cnpgClusterLabel          = "cnpg.io/cluster"
	cnpgInstanceRoleLabel     = "cnpg.io/instanceRole"
)

var (
	// ErrHibernationUnsupported: not a provisioned managed-Postgres addon.
	ErrHibernationUnsupported = errors.New("hibernation applies only to a provisioned managed-Postgres addon")
	// ErrHibernationPlanChanged: the live state no longer hashes to the
	// reviewed plan; the caller must take a new dry-run.
	ErrHibernationPlanChanged = errors.New("the cluster changed since the reviewed plan; obtain a new dry-run")
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
	AddonID           string           `json:"addon_id"`
	Namespace         string           `json:"namespace"`
	Cluster           string           `json:"cluster"`
	ClusterUID        string           `json:"cluster_uid"`
	ResourceVersion   string           `json:"resource_version"`
	AlreadyHibernated bool             `json:"already_hibernated"`
	InstancePods      []HibernationPod `json:"instance_pods"`
	Exporter          string           `json:"exporter,omitempty"`
	ExporterReplicas  int32            `json:"exporter_replicas"`
	PlanHash          string           `json:"plan_hash"`
}

// HibernationResult reports what the apply changed.
type HibernationResult struct {
	Plan        HibernationPlan `json:"plan"`
	Annotated   bool            `json:"annotated"`
	PodsDeleted []string        `json:"pods_deleted"`
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
		InstancePods:      []HibernationPod{},
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

// Done reports whether nothing is left to stop.
func (p HibernationPlan) Done() bool {
	return p.AlreadyHibernated && len(p.InstancePods) == 0 && (p.Exporter == "" || p.ExporterReplicas == 0)
}

func hibernationPlanHash(p HibernationPlan) string {
	p.PlanHash = ""
	raw, _ := json.Marshal(p)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// ApplyPostgresHibernation re-plans, refuses unless the live plan hashes to
// expectedPlan, then performs the three writes. A plan that is already Done
// returns without writing (an exact retry is safe).
func ApplyPostgresHibernation(ctx context.Context, dyn dynamic.Interface, kube kubernetes.Interface, addon *types.DatabaseAddon, expectedPlan string) (*HibernationResult, error) {
	plan, err := PlanPostgresHibernation(ctx, dyn, kube, addon)
	if err != nil {
		return nil, err
	}
	result := &HibernationResult{Plan: *plan, PodsDeleted: []string{}}
	if plan.Done() {
		return result, nil
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
	for _, pod := range plan.InstancePods {
		uid := k8stypes.UID(pod.UID)
		err := kube.CoreV1().Pods(plan.Namespace).Delete(ctx, pod.Name, metav1.DeleteOptions{Preconditions: &metav1.Preconditions{UID: &uid}})
		if err != nil && !k8serrors.IsNotFound(err) {
			return result, fmt.Errorf("delete instance pod %s (cluster already annotated): %w", pod.Name, err)
		}
		result.PodsDeleted = append(result.PodsDeleted, pod.Name)
	}
	if plan.Exporter != "" && plan.ExporterReplicas != 0 {
		patch := []byte(`{"spec":{"replicas":0}}`)
		if _, err := kube.AppsV1().Deployments(plan.Namespace).Patch(ctx, plan.Exporter, k8stypes.MergePatchType, patch, metav1.PatchOptions{FieldManager: "enclii-addon-hibernate"}); err != nil && !k8serrors.IsNotFound(err) {
			return result, fmt.Errorf("scale exporter %s to 0 (database already stopped): %w", plan.Exporter, err)
		}
		result.ExporterOff = true
	}
	return result, nil
}
