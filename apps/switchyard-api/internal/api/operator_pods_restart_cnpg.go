package api

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/google/uuid"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/addons"
	k8sclient "github.com/madfam-org/enclii/apps/switchyard-api/internal/k8s"
)

// ops.pods.restart, CNPG instance pods in Enclii project namespaces.
//
// The namespace allowlist (ENCLII_OPS_RESTART_NAMESPACES) names platform
// namespaces; project namespaces are created per project and are never listed.
// So when a managed-Postgres instance kept a broken volume mount after a node
// fault (2026-10-24: `global/pg_filenode.map: Input/output error`), the only
// fix was a break-glass `kubectl delete pod`. This path is the audited
// replacement, and it is deliberately narrower than the allowlisted one:
//
//   - only `pod/<name>`, never a workload rollout;
//   - only in a namespace Enclii created for a project: named
//     `project-<first 8 of the project id>` and labelled `managed-by=enclii`
//     plus `enclii.dev/project=<that id>`;
//   - only a CloudNativePG INSTANCE pod: controller owner a postgresql.cnpg.io
//     Cluster, labels `cnpg.io/cluster` = that Cluster, `cnpg.io/podRole` =
//     instance and `cnpg.io/instanceName` = the pod (poolers, Jobs and
//     anything else stay refused);
//   - that Cluster is the live owner (UID), is not being deleted, is not
//     hibernating, and is an Enclii addon of the same project (`managed-by` and
//     `enclii.dev/project-id` labels);
//   - apply requires --expect-uid from a reviewed dry run, not just --reason.
//
// CNPG recreates a deleted instance pod with the same name on the same PVC, so
// this is a restart, not data loss: the PVC is never touched. The dry run warns
// when that interrupts a database that still answers (the pod is Ready) and
// when the pod is the primary of a multi-instance cluster (a failover).
// Platform rank, the audit row before the mutation and the UID-preconditioned
// delete are the ones every pod restart uses (operator_pods_restart.go).
const (
	restartModeCNPGInstance = "cnpg-instance-recreate"
	cnpgPodRoleLabel        = "cnpg.io/podRole"
	cnpgInstanceNameLabel   = "cnpg.io/instanceName"
	cnpgInstanceRoleLabel   = "cnpg.io/instanceRole"
	cnpgClusterPodLabel     = "cnpg.io/cluster"
	cnpgAPIGroupPrefix      = "postgresql.cnpg.io/"
	projectNamespacePrefix  = "project-"
)

// restartCNPGClusterGVR is postgresql.cnpg.io/v1 clusters.
var restartCNPGClusterGVR = januaStageClusterGVR

// projectNamespaceCandidate is the cheap name test that routes a refused
// namespace to the CNPG path; encliiProjectNamespaceRefusal is the real check.
func projectNamespaceCandidate(namespace string) bool {
	return strings.HasPrefix(namespace, projectNamespacePrefix)
}

// encliiProjectNamespaceRefusal returns why ns is not a namespace Enclii
// created for a project, or "" and the project id when it is.
func encliiProjectNamespaceRefusal(ns *corev1.Namespace) (string, string) {
	project := ns.Labels[k8sclient.LabelProjectNamespace]
	id, err := uuid.Parse(project)
	switch {
	case ns.Labels[addons.LabelManagedBy] != addons.LabelManagedValue:
		return fmt.Sprintf("namespace %q is not managed by Enclii (label %s=%s missing)", ns.Name, addons.LabelManagedBy, addons.LabelManagedValue), ""
	case err != nil:
		return fmt.Sprintf("namespace %q carries no project id (label %s)", ns.Name, k8sclient.LabelProjectNamespace), ""
	case ns.Name != projectNamespacePrefix+id.String()[:8]:
		return fmt.Sprintf("namespace %q does not match its project label %s", ns.Name, project), ""
	case ns.DeletionTimestamp != nil:
		return fmt.Sprintf("namespace %q is being deleted", ns.Name), ""
	}
	return "", id.String()
}

// cnpgInstancePodRefusal returns why pod is not a CNPG instance pod, or "" and
// its controlling Cluster reference.
func cnpgInstancePodRefusal(pod *corev1.Pod) (string, *metav1.OwnerReference) {
	owner := metav1.GetControllerOf(pod)
	switch {
	case owner == nil || owner.Kind != "Cluster" || !strings.HasPrefix(owner.APIVersion, cnpgAPIGroupPrefix):
		return "it is not controlled by a CloudNativePG Cluster", nil
	case pod.Labels[cnpgClusterPodLabel] != owner.Name:
		return fmt.Sprintf("its %s label does not name its owning Cluster %s", cnpgClusterPodLabel, owner.Name), nil
	case pod.Labels[cnpgPodRoleLabel] != "instance":
		return fmt.Sprintf("it is not a database instance (%s=%q)", cnpgPodRoleLabel, pod.Labels[cnpgPodRoleLabel]), nil
	case pod.Labels[cnpgInstanceNameLabel] != pod.Name:
		return fmt.Sprintf("its %s label does not name the pod", cnpgInstanceNameLabel), nil
	}
	return "", owner
}

// cnpgClusterRefusal checks the owning Cluster object itself.
func cnpgClusterRefusal(cluster *unstructured.Unstructured, owner *metav1.OwnerReference, projectID string) string {
	labels := cluster.GetLabels()
	switch {
	case string(cluster.GetUID()) != string(owner.UID):
		return fmt.Sprintf("Cluster %s was replaced (UID %s, the pod's owner is %s)", owner.Name, cluster.GetUID(), owner.UID)
	case cluster.GetDeletionTimestamp() != nil:
		return fmt.Sprintf("Cluster %s is being deleted", owner.Name)
	case labels[addons.LabelManagedBy] != addons.LabelManagedValue || labels[addons.LabelProjectID] != projectID:
		return fmt.Sprintf("Cluster %s is not an Enclii addon of project %s", owner.Name, projectID)
	case cluster.GetAnnotations()[addons.CNPGHibernationAnnotation] == "on":
		return fmt.Sprintf("Cluster %s is hibernating (%s=on); CNPG is stopping its instances, so recreating one would fight it", owner.Name, addons.CNPGHibernationAnnotation)
	}
	return ""
}

// restartCNPGInstancePod is the CNPG path of pod/<name>. The caller has
// validated the request, the actor, the clients and the audit store.
func (h *Handler) restartCNPGInstancePod(ctx context.Context, req operatorOperationRequest, actor opsActor, resp operatorOperationResponse, namespace, name, allowlistRefusal string) (operatorOperationResponse, int) {
	what := fmt.Sprintf("pod %s/%s", namespace, name)
	refuse := func(why string) (operatorOperationResponse, int) {
		resp.Status = "refused"
		resp.Summary = fmt.Sprintf("%s; in an Enclii project namespace only a CloudNativePG instance pod may be recreated, and %s; nothing was changed", allowlistRefusal, why)
		return resp, http.StatusConflict
	}
	if h.k8sClient == nil || h.k8sClient.DynamicClient == nil {
		resp.Status = "adapter_unconfigured"
		resp.Summary = "the Kubernetes dynamic client is not configured in this Switchyard API instance, so the CNPG Cluster cannot be checked"
		return resp, http.StatusServiceUnavailable
	}
	client := h.opsKubeClient()
	ns, err := client.CoreV1().Namespaces().Get(ctx, namespace, metav1.GetOptions{})
	if err != nil {
		return restartReadFailure(resp, err, "namespace "+namespace)
	}
	nsRefusal, projectID := encliiProjectNamespaceRefusal(ns)
	if nsRefusal != "" {
		return refuse(nsRefusal)
	}
	pod, err := client.CoreV1().Pods(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return restartReadFailure(resp, err, what)
	}
	podRefusal, owner := cnpgInstancePodRefusal(pod)
	if podRefusal != "" {
		return refuse(what + " " + podRefusal)
	}
	cluster, err := h.k8sClient.DynamicClient.Resource(restartCNPGClusterGVR).Namespace(namespace).Get(ctx, owner.Name, metav1.GetOptions{})
	if err != nil {
		return restartReadFailure(resp, err, fmt.Sprintf("CNPG Cluster %s/%s", namespace, owner.Name))
	}
	view := viewRestartPod(pod, "")
	phase, _, _ := unstructured.NestedString(cluster.Object, "status", "phase")
	instances, _, _ := unstructured.NestedInt64(cluster.Object, "spec", "instances")
	readyInstances, _, _ := unstructured.NestedInt64(cluster.Object, "status", "readyInstances")
	role := pod.Labels[cnpgInstanceRoleLabel]
	data := map[string]any{"namespace": namespace, "kind": restartKindPod, "name": name, "mode": restartModeCNPGInstance,
		"uid": view.UID, "phase": view.Phase, "ready": view.Ready, "restarts": view.Restarts,
		"ownerKind": owner.Kind, "ownerName": owner.Name, "project": projectID,
		"cluster": owner.Name, "clusterUID": string(cluster.GetUID()), "clusterPhase": phase,
		"instances": instances, "readyInstances": readyInstances, "instanceRole": role,
		"pvcsKept": restartPodClaims(pod)}
	if pod.Spec.TerminationGracePeriodSeconds != nil {
		data["gracePeriodSeconds"] = *pod.Spec.TerminationGracePeriodSeconds
	}
	resp.Data = data
	if why := cnpgClusterRefusal(cluster, owner, projectID); why != "" {
		return refuse(why)
	}
	expectUID := operationArg(req, "expect_uid", "expect-uid")
	switch {
	case view.Terminating:
		resp.Status, resp.Summary = "refused", what+" is already terminating; deleting it again would not help"
		return resp, http.StatusConflict
	case !req.DryRun && expectUID == "":
		resp.Status = "invalid_request"
		resp.Summary = "recreating a database instance requires --expect-uid with the UID from a reviewed dry run; nothing was changed"
		return resp, http.StatusBadRequest
	case expectUID != "" && expectUID != view.UID:
		resp.Status = "refused"
		resp.Summary = fmt.Sprintf("%s was already replaced since review (UID %s, reviewed %s); nothing was deleted", what, view.UID, expectUID)
		return resp, http.StatusConflict
	}
	warnings := []string{}
	if view.Ready {
		warnings = append(warnings, what+" is Ready: recreating it interrupts a database that still passes its readiness check. Read its log first (enclii ops pods diagnose)")
	}
	if instances <= 1 {
		warnings = append(warnings, fmt.Sprintf("Cluster %s runs one instance: the database is unavailable until CNPG's replacement pod is Ready", owner.Name))
	} else if role == "primary" {
		warnings = append(warnings, fmt.Sprintf("%s is the primary of a %d-instance cluster: CNPG fails over to a standby", what, instances))
	}
	resp.Warnings = warnings

	if req.DryRun {
		resp.Status = "ready_to_apply"
		resp.Summary = fmt.Sprintf("recreating CNPG instance %s is ready to apply; Cluster %s recreates it on the same PVCs %s (CNPG phase «%s»)", what, owner.Name, strings.Join(restartPodClaims(pod), ", "), phase)
		resp.Steps = cnpgRestartSteps("planned", restartAuditPlanned)
		resp.Next = []string{stuckPodNext(namespace, view)}
		return resp, http.StatusOK
	}

	uid := pod.UID // equal to expectUID (required and checked above)
	details := restartAuditDetails(req, resp.OperationID, namespace, restartKindPod, name, restartModeCNPGInstance)
	details["uid"] = string(uid)
	details["owner_kind"], details["owner_name"] = owner.Kind, owner.Name
	details["cluster_uid"] = string(cluster.GetUID())
	details["cluster_phase"] = phase
	details["instance_role"] = role
	details["project"] = projectID
	auditID, err := h.writeRestartAudit(ctx, actor, "requested", "pod", string(uid), name, details)
	if err != nil {
		resp.Status, resp.Summary = "failed", "the audit record could not be written, so nothing was changed"
		resp.Warnings = append(resp.Warnings, err.Error())
		return resp, http.StatusServiceUnavailable
	}
	resp.AuditID = auditID
	err = client.CoreV1().Pods(namespace).Delete(ctx, name, metav1.DeleteOptions{Preconditions: &metav1.Preconditions{UID: &uid}})
	if err != nil {
		h.recordRestartResult(ctx, actor, "pod", string(uid), name, details, auditID, err, "")
		out, status := restartMutationFailure(resp, err, what)
		out.Steps = cnpgRestartSteps("failed", restartAuditRecorded)
		return out, status
	}
	if warning := h.recordRestartResult(ctx, actor, "pod", string(uid), name, details, auditID, nil, ""); warning != "" {
		resp.Warnings = append(resp.Warnings, warning)
	}
	resp.Status = "submitted"
	resp.Summary = fmt.Sprintf("deleted CNPG instance %s through Enclii; Cluster %s recreates it on the same PVCs; none was touched", what, owner.Name)
	resp.Steps = cnpgRestartSteps("completed", restartAuditRecorded)
	resp.Next = []string{fmt.Sprintf("enclii ops pods diagnose %s -n %s", name, namespace)}
	h.logRestart(ctx, actor, req, namespace, restartKindPod, name, auditID)
	return resp, http.StatusAccepted
}

// restartPodClaims lists the PVCs the pod mounts, for the plan: the delete
// never touches them, and CNPG mounts the same ones in the replacement.
func restartPodClaims(pod *corev1.Pod) []string {
	claims := []string{}
	for _, volume := range pod.Spec.Volumes {
		if volume.PersistentVolumeClaim != nil {
			claims = append(claims, volume.PersistentVolumeClaim.ClaimName)
		}
	}
	return claims
}

func cnpgRestartSteps(status, audit string) []operatorOperationStep {
	steps := restartSteps(status, "delete the instance pod with a UID precondition and its own grace period; CNPG recreates it on the same PVC", audit)
	steps[0].Detail = "platform rank checked; Enclii project namespace, CloudNativePG instance pod of an Enclii addon"
	return steps
}
