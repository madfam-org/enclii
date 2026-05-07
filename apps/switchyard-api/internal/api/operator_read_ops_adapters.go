package api

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/gin-gonic/gin"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
)

func (h *Handler) readDynamicOperatorResources(ctx context.Context, operation, domain, action string, req operatorOperationRequest, gvr schema.GroupVersionResource, defaultNamespace string) operatorOperationResponse {
	if h.k8sClient == nil || h.k8sClient.DynamicClient == nil {
		return operatorReadUnavailable(operation, domain, action, "kubernetes dynamic client is not configured on switchyard-api")
	}

	namespace := operationNamespace(req, defaultNamespace)
	target := operationTarget(req)
	resources, err := h.readDynamicResources(ctx, namespace, target, gvr)
	if err != nil {
		return operatorReadFailed(operation, domain, action, err)
	}
	return operatorReadSuccess(operation, domain, action, gin.H{
		"namespace": namespace,
		"target":    target,
		"resources": resources,
		"count":     len(resources),
	})
}

func (h *Handler) readDynamicResources(ctx context.Context, namespace, target string, gvr schema.GroupVersionResource) ([]map[string]any, error) {
	namespaceable := h.k8sClient.DynamicClient.Resource(gvr)
	var resource dynamic.ResourceInterface
	if namespace != "" {
		resource = namespaceable.Namespace(namespace)
	} else {
		resource = namespaceable
	}

	var items []unstructured.Unstructured
	if target != "" {
		obj, err := resource.Get(ctx, target, metav1.GetOptions{})
		if err != nil {
			return nil, err
		}
		items = []unstructured.Unstructured{*obj}
	} else {
		list, err := resource.List(ctx, metav1.ListOptions{})
		if err != nil {
			return nil, err
		}
		items = list.Items
	}

	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		entry := map[string]any{
			"name":      item.GetName(),
			"namespace": item.GetNamespace(),
			"labels":    item.GetLabels(),
		}
		if status, ok, _ := unstructured.NestedMap(item.Object, "status"); ok {
			entry["status"] = status
		}
		out = append(out, entry)
	}
	sort.Slice(out, func(left, right int) bool {
		return fmt.Sprint(out[left]["name"]) < fmt.Sprint(out[right]["name"])
	})
	return out, nil
}

func (h *Handler) readArgoApplications(ctx context.Context, req operatorOperationRequest) (gin.H, error) {
	var dynamicErr error
	if h.k8sClient.DynamicClient != nil {
		gvr := schema.GroupVersionResource{Group: "argoproj.io", Version: "v1alpha1", Resource: "applications"}
		namespace := operationNamespace(req, "argocd")
		resources, err := h.readDynamicResources(ctx, namespace, operationTarget(req), gvr)
		if err == nil {
			return gin.H{"namespace": namespace, "applications": resources, "count": len(resources)}, nil
		}
		dynamicErr = err
	}
	if h.k8sClient.Clientset == nil {
		if dynamicErr != nil {
			return nil, dynamicErr
		}
		return nil, fmt.Errorf("kubernetes typed client is not configured on switchyard-api")
	}

	namespace := operationNamespace(req, "default")
	deployments, err := h.k8sClient.ListDeployments(ctx, namespace)
	if err != nil {
		return nil, err
	}
	out := make([]gin.H, 0, len(deployments))
	target := operationTarget(req)
	for _, deployment := range deployments {
		if target != "" && deployment.Name != target {
			continue
		}
		desired := int32(1)
		if deployment.Spec.Replicas != nil {
			desired = *deployment.Spec.Replicas
		}
		out = append(out, gin.H{
			"name":       deployment.Name,
			"namespace":  deployment.Namespace,
			"desired":    desired,
			"ready":      deployment.Status.ReadyReplicas,
			"available":  deployment.Status.AvailableReplicas,
			"updated":    deployment.Status.UpdatedReplicas,
			"generation": deployment.Generation,
			"observed":   deployment.Status.ObservedGeneration,
			"fallback":   "deployment-status",
		})
	}
	return gin.H{"namespace": namespace, "applications": out, "count": len(out)}, nil
}

func (h *Handler) readArgoApplicationDrift(ctx context.Context, req operatorOperationRequest) (gin.H, error) {
	gvr := schema.GroupVersionResource{Group: "argoproj.io", Version: "v1alpha1", Resource: "applications"}
	namespace := operationNamespace(req, "argocd")
	target := operationTarget(req)
	namespaceable := h.k8sClient.DynamicClient.Resource(gvr)
	var resource dynamic.ResourceInterface
	if namespace != "" {
		resource = namespaceable.Namespace(namespace)
	} else {
		resource = namespaceable
	}

	var items []unstructured.Unstructured
	if target != "" {
		app, err := resource.Get(ctx, target, metav1.GetOptions{})
		if err != nil {
			return nil, err
		}
		items = []unstructured.Unstructured{*app}
	} else {
		list, err := resource.List(ctx, metav1.ListOptions{})
		if err != nil {
			return nil, err
		}
		items = list.Items
	}

	apps := make([]gin.H, 0, len(items))
	driftedCount := 0
	driftedResources := 0
	for _, item := range items {
		summary, resourceDriftCount := argoApplicationDriftSummary(item)
		driftedResources += resourceDriftCount
		if drifted, _ := summary["drifted"].(bool); drifted {
			driftedCount++
		}
		apps = append(apps, summary)
	}
	sort.Slice(apps, func(left, right int) bool {
		return fmt.Sprint(apps[left]["name"]) < fmt.Sprint(apps[right]["name"])
	})

	return gin.H{
		"namespace":        namespace,
		"target":           target,
		"applications":     apps,
		"count":            len(apps),
		"driftedCount":     driftedCount,
		"driftedResources": driftedResources,
	}, nil
}

func argoApplicationDriftSummary(app unstructured.Unstructured) (gin.H, int) {
	syncStatus, _, _ := unstructured.NestedString(app.Object, "status", "sync", "status")
	if syncStatus == "" {
		syncStatus = "Unknown"
	}
	revision, _, _ := unstructured.NestedString(app.Object, "status", "sync", "revision")
	healthStatus, _, _ := unstructured.NestedString(app.Object, "status", "health", "status")
	comparedTo, _, _ := unstructured.NestedMap(app.Object, "status", "sync", "comparedTo")
	resources, driftedResources := argoApplicationResourceDrift(app)
	conditions := argoApplicationConditions(app)
	drifted := !strings.EqualFold(syncStatus, "Synced") || driftedResources > 0 || len(conditions) > 0

	summary := gin.H{
		"name":              app.GetName(),
		"namespace":         app.GetNamespace(),
		"syncStatus":        syncStatus,
		"healthStatus":      healthStatus,
		"revision":          revision,
		"comparedTo":        comparedTo,
		"drifted":           drifted,
		"driftedResources":  driftedResources,
		"resources":         resources,
		"conditions":        conditions,
		"conditionCount":    len(conditions),
		"resourceSyncCount": len(resources),
	}
	return summary, driftedResources
}

func argoApplicationResourceDrift(app unstructured.Unstructured) ([]gin.H, int) {
	rawResources, ok, _ := unstructured.NestedSlice(app.Object, "status", "resources")
	if !ok {
		return []gin.H{}, 0
	}
	resources := make([]gin.H, 0, len(rawResources))
	driftedCount := 0
	for _, raw := range rawResources {
		resource, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		syncStatus := mapStringValue(resource, "status")
		requiresPruning := mapBoolValue(resource, "requiresPruning")
		healthStatus := ""
		if health, ok := resource["health"].(map[string]any); ok {
			healthStatus = mapStringValue(health, "status")
		}
		drifted := requiresPruning || (syncStatus != "" && !strings.EqualFold(syncStatus, "Synced"))
		if drifted {
			driftedCount++
		}
		summary := gin.H{
			"group":           mapStringValue(resource, "group"),
			"kind":            mapStringValue(resource, "kind"),
			"namespace":       mapStringValue(resource, "namespace"),
			"name":            mapStringValue(resource, "name"),
			"syncStatus":      syncStatus,
			"healthStatus":    healthStatus,
			"requiresPruning": requiresPruning,
			"drifted":         drifted,
		}
		if hook := mapStringValue(resource, "hook"); hook != "" {
			summary["hook"] = hook
		}
		resources = append(resources, summary)
	}
	sort.Slice(resources, func(left, right int) bool {
		leftKey := fmt.Sprintf("%s/%s/%s", resources[left]["kind"], resources[left]["namespace"], resources[left]["name"])
		rightKey := fmt.Sprintf("%s/%s/%s", resources[right]["kind"], resources[right]["namespace"], resources[right]["name"])
		return leftKey < rightKey
	})
	return resources, driftedCount
}

func argoApplicationConditions(app unstructured.Unstructured) []gin.H {
	rawConditions, ok, _ := unstructured.NestedSlice(app.Object, "status", "conditions")
	if !ok {
		return []gin.H{}
	}
	conditions := make([]gin.H, 0, len(rawConditions))
	for _, raw := range rawConditions {
		condition, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		conditions = append(conditions, gin.H{
			"type":               mapStringValue(condition, "type"),
			"message":            mapStringValue(condition, "message"),
			"lastTransitionTime": mapStringValue(condition, "lastTransitionTime"),
		})
	}
	sort.Slice(conditions, func(left, right int) bool {
		return fmt.Sprint(conditions[left]["type"]) < fmt.Sprint(conditions[right]["type"])
	})
	return conditions
}

func (h *Handler) readPods(ctx context.Context, req operatorOperationRequest) (gin.H, error) {
	return h.readPodsInNamespace(ctx, operationNamespace(req, "default"), req, "")
}

func (h *Handler) readPodsInNamespace(ctx context.Context, namespace string, req operatorOperationRequest, labelSelector string) (gin.H, error) {
	pods, err := h.k8sClient.ListPods(ctx, namespace, labelSelector)
	if err != nil {
		return nil, err
	}
	target := operationTarget(req)
	out := make([]gin.H, 0, len(pods.Items))
	for _, pod := range pods.Items {
		if !podMatchesTarget(pod, target) {
			continue
		}
		restarts := int32(0)
		containers := make([]gin.H, 0, len(pod.Status.ContainerStatuses))
		for _, status := range pod.Status.ContainerStatuses {
			restarts += status.RestartCount
			containers = append(containers, gin.H{
				"name":         status.Name,
				"ready":        status.Ready,
				"restartCount": status.RestartCount,
				"image":        status.Image,
			})
		}
		out = append(out, gin.H{
			"name":       pod.Name,
			"namespace":  pod.Namespace,
			"phase":      pod.Status.Phase,
			"node":       pod.Spec.NodeName,
			"restarts":   restarts,
			"containers": containers,
			"conditions": pod.Status.Conditions,
		})
	}
	sort.Slice(out, func(left, right int) bool {
		return fmt.Sprint(out[left]["name"]) < fmt.Sprint(out[right]["name"])
	})
	return gin.H{"namespace": namespace, "target": target, "pods": out, "count": len(out)}, nil
}

func (h *Handler) readPodLogs(ctx context.Context, req operatorOperationRequest) (gin.H, error) {
	namespace := operationNamespace(req, "default")
	target := operationTarget(req)
	if target == "" {
		return nil, fmt.Errorf("target pod name is required for pods.logs")
	}
	logs, err := h.k8sClient.GetPodLogs(ctx, target, namespace)
	if err != nil {
		return nil, err
	}
	return gin.H{"namespace": namespace, "pod": target, "logs": logs}, nil
}

func (h *Handler) readPVCs(ctx context.Context, req operatorOperationRequest) (gin.H, error) {
	namespace := operationNamespace(req, "default")
	list, err := h.k8sClient.Clientset.CoreV1().PersistentVolumeClaims(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	target := operationTarget(req)
	out := make([]gin.H, 0, len(list.Items))
	for _, pvc := range list.Items {
		if target != "" && pvc.Name != target {
			continue
		}
		storageClass := ""
		if pvc.Spec.StorageClassName != nil {
			storageClass = *pvc.Spec.StorageClassName
		}
		out = append(out, gin.H{
			"name":         pvc.Name,
			"namespace":    pvc.Namespace,
			"phase":        pvc.Status.Phase,
			"volume":       pvc.Spec.VolumeName,
			"storageClass": storageClass,
			"capacity":     pvc.Status.Capacity.Storage().String(),
		})
	}
	return gin.H{"namespace": namespace, "target": target, "pvcs": out, "count": len(out)}, nil
}

func (h *Handler) readPVs(ctx context.Context) (gin.H, error) {
	list, err := h.k8sClient.Clientset.CoreV1().PersistentVolumes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	out := make([]gin.H, 0, len(list.Items))
	for _, pv := range list.Items {
		storageClass := pv.Spec.StorageClassName
		claim := ""
		if pv.Spec.ClaimRef != nil {
			claim = fmt.Sprintf("%s/%s", pv.Spec.ClaimRef.Namespace, pv.Spec.ClaimRef.Name)
		}
		out = append(out, gin.H{
			"name":         pv.Name,
			"phase":        pv.Status.Phase,
			"claim":        claim,
			"storageClass": storageClass,
			"capacity":     pv.Spec.Capacity.Storage().String(),
		})
	}
	return gin.H{"volumes": out, "count": len(out)}, nil
}

func podMatchesTarget(pod corev1.Pod, target string) bool {
	if target == "" {
		return true
	}
	if pod.Name == target || strings.Contains(pod.Name, target) {
		return true
	}
	return pod.Labels["app"] == target || pod.Labels["app.kubernetes.io/name"] == target
}
