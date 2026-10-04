package api

import (
	"context"
	"fmt"
	"sort"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

type inventoryResources struct {
	CPU    int64 `json:"cpu_millicores"`
	Memory int64 `json:"memory_bytes"`
	Pods   int64 `json:"pods"`
}
type inventoryUsed struct {
	CPU    int64 `json:"cpu_millicores"`
	Memory int64 `json:"memory_bytes"`
	Pods   int   `json:"pod_count"`
}
type inventoryCondition struct {
	Type   string `json:"type"`
	Status string `json:"status"`
	Reason string `json:"reason,omitempty"`
}
type inventoryNode struct {
	Name        string               `json:"name"`
	Role        string               `json:"role"`
	Status      string               `json:"status"`
	Conditions  []inventoryCondition `json:"conditions"`
	Kubelet     string               `json:"kubelet_version,omitempty"`
	OS          string               `json:"os_image,omitempty"`
	Capacity    inventoryResources   `json:"capacity"`
	Allocatable inventoryResources   `json:"allocatable"`
	Used        inventoryUsed        `json:"used"`
	Taints      []corev1.Taint       `json:"taints"`
	Labels      map[string]string    `json:"labels"`
}
type inventoryContainer struct {
	Name  string `json:"name"`
	Image string `json:"image"`
	Ready bool   `json:"ready"`
}
type inventoryPod struct {
	Name       string               `json:"name"`
	Namespace  string               `json:"namespace"`
	Node       string               `json:"node"`
	Phase      string               `json:"phase"`
	Ready      string               `json:"ready"`
	Restarts   int32                `json:"restart_count"`
	Containers []inventoryContainer `json:"containers"`
	Age        int64                `json:"age_seconds"`
}
type inventoryNamespace struct {
	Name        string         `json:"name"`
	Pods        int            `json:"pod_count"`
	Deployments int            `json:"deployment_count"`
	Services    int            `json:"service_count"`
	Phases      map[string]int `json:"pod_phases"`
}
type inventoryPort struct {
	Port     int32  `json:"port"`
	Target   any    `json:"target_port"`
	Protocol string `json:"protocol"`
	Name     string `json:"name,omitempty"`
}
type inventoryService struct {
	Name      string            `json:"name"`
	Namespace string            `json:"namespace"`
	Type      string            `json:"type"`
	ClusterIP *string           `json:"cluster_ip"`
	Ports     []inventoryPort   `json:"ports"`
	Selector  map[string]string `json:"selector"`
}
type inventoryTotals struct {
	Nodes          int   `json:"nodes"`
	Namespaces     int   `json:"namespaces"`
	Pods           int   `json:"pods"`
	Services       int   `json:"services"`
	CPUCapacity    int64 `json:"cpu_capacity_millicores"`
	CPUUsed        int64 `json:"cpu_used_millicores"`
	MemoryCapacity int64 `json:"memory_capacity_bytes"`
	MemoryUsed     int64 `json:"memory_used_bytes"`
}
type inventoryDisplay struct {
	CPUCapacity    string `json:"cpu_capacity"`
	CPUUsed        string `json:"cpu_used"`
	MemoryCapacity string `json:"memory_capacity"`
	MemoryUsed     string `json:"memory_used"`
}
type inventoryTopology struct {
	Nodes      []inventoryNode      `json:"nodes"`
	Namespaces []inventoryNamespace `json:"namespaces"`
	Pods       []inventoryPod       `json:"pods"`
	Services   []inventoryService   `json:"services"`
	SyncedAt   string               `json:"synced_at"`
	Totals     inventoryTotals      `json:"totals"`
	Display    inventoryDisplay     `json:"display"`
}

func resourceInventory(resources corev1.ResourceList) inventoryResources {
	return inventoryResources{resources.Cpu().MilliValue(), resources.Memory().Value(), resources.Pods().Value()}
}

func (h *Handler) readInventoryTopology(ctx context.Context) (inventoryTopology, error) {
	out := inventoryTopology{Nodes: []inventoryNode{}, Namespaces: []inventoryNamespace{}, Pods: []inventoryPod{}, Services: []inventoryService{}}
	client := h.opsKubeClient()
	nodes, err := client.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return out, err
	}
	namespaces, err := client.CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
	if err != nil {
		return out, err
	}
	pods, err := client.CoreV1().Pods("").List(ctx, metav1.ListOptions{})
	if err != nil {
		return out, err
	}
	services, err := client.CoreV1().Services("").List(ctx, metav1.ListOptions{})
	if err != nil {
		return out, err
	}
	deployments, err := client.AppsV1().Deployments("").List(ctx, metav1.ListOptions{})
	if err != nil {
		return out, err
	}
	now := time.Now().UTC()
	nodeIndexes := map[string]int{}
	for _, node := range nodes.Items {
		role := "worker"
		if len(node.Labels) == 0 {
			role = "unknown"
		}
		if node.Labels["enclii.dev/role"] == "builder" || node.Labels["role"] == "builder" {
			role = "builder"
		}
		if _, ok := node.Labels["node-role.kubernetes.io/control-plane"]; ok {
			role = "control-plane"
		}
		if _, ok := node.Labels["node-role.kubernetes.io/master"]; ok {
			role = "control-plane"
		}
		conditions := []inventoryCondition{}
		status := "Unknown"
		for _, c := range node.Status.Conditions {
			conditions = append(conditions, inventoryCondition{string(c.Type), string(c.Status), c.Reason})
			if c.Type == corev1.NodeReady {
				if c.Status == corev1.ConditionTrue {
					status = "Ready"
				} else if c.Status == corev1.ConditionFalse {
					status = "NotReady"
				}
			}
		}
		taints := node.Spec.Taints
		if taints == nil {
			taints = []corev1.Taint{}
		}
		labels := node.Labels
		if labels == nil {
			labels = map[string]string{}
		}
		nodeIndexes[node.Name] = len(out.Nodes)
		out.Nodes = append(out.Nodes, inventoryNode{Name: node.Name, Role: role, Status: status, Conditions: conditions,
			Kubelet: node.Status.NodeInfo.KubeletVersion, OS: node.Status.NodeInfo.OSImage,
			Capacity: resourceInventory(node.Status.Capacity), Allocatable: resourceInventory(node.Status.Allocatable), Taints: taints, Labels: labels})
	}
	namespaceStats := map[string]*inventoryNamespace{}
	for _, ns := range namespaces.Items {
		namespaceStats[ns.Name] = &inventoryNamespace{Name: ns.Name, Phases: map[string]int{}}
	}
	for _, pod := range pods.Items {
		ready := 0
		var restarts int32
		states := map[string]corev1.ContainerStatus{}
		for _, state := range pod.Status.ContainerStatuses {
			states[state.Name] = state
			if state.Ready {
				ready++
			}
			restarts += state.RestartCount
		}
		total := len(pod.Status.ContainerStatuses)
		if total == 0 {
			total = len(pod.Spec.Containers)
		}
		age := int64(0)
		if pod.Status.StartTime != nil {
			age = max(0, int64(now.Sub(pod.Status.StartTime.Time).Seconds()))
		}
		phase := string(pod.Status.Phase)
		if phase == "" {
			phase = "Unknown"
		}
		item := inventoryPod{Name: pod.Name, Namespace: pod.Namespace, Node: pod.Spec.NodeName, Phase: phase, Ready: fmt.Sprintf("%d/%d", ready, total), Restarts: restarts, Age: age, Containers: []inventoryContainer{}}
		for _, container := range pod.Spec.Containers {
			item.Containers = append(item.Containers, inventoryContainer{container.Name, container.Image, states[container.Name].Ready})
		}
		out.Pods = append(out.Pods, item)
		if ns := namespaceStats[pod.Namespace]; ns != nil {
			ns.Pods++
			ns.Phases[phase]++
		}
		if index, ok := nodeIndexes[pod.Spec.NodeName]; ok && pod.Status.Phase == corev1.PodRunning {
			out.Nodes[index].Used.Pods++
			// Preserve the existing contract: these values are scheduled
			// requests of running containers, not metrics-server utilization.
			for _, c := range pod.Spec.Containers {
				out.Nodes[index].Used.CPU += c.Resources.Requests.Cpu().MilliValue()
				out.Nodes[index].Used.Memory += c.Resources.Requests.Memory().Value()
			}
		}
	}
	for _, deployment := range deployments.Items {
		if ns := namespaceStats[deployment.Namespace]; ns != nil {
			ns.Deployments++
		}
	}
	for _, service := range services.Items {
		item := inventoryService{Name: service.Name, Namespace: service.Namespace, Type: string(service.Spec.Type), Selector: service.Spec.Selector, Ports: []inventoryPort{}}
		if item.Type == "" {
			item.Type = "ClusterIP"
		}
		if service.Spec.ClusterIP != "" {
			ip := service.Spec.ClusterIP
			item.ClusterIP = &ip
		}
		for _, port := range service.Spec.Ports {
			var target any = port.TargetPort.IntVal
			if port.TargetPort.Type == intstr.String {
				target = port.TargetPort.StrVal
			} else if port.TargetPort.IntVal == 0 {
				target = port.Port
			}
			protocol := string(port.Protocol)
			if protocol == "" {
				protocol = "TCP"
			}
			item.Ports = append(item.Ports, inventoryPort{port.Port, target, protocol, port.Name})
		}
		out.Services = append(out.Services, item)
		if ns := namespaceStats[service.Namespace]; ns != nil {
			ns.Services++
		}
	}
	for _, ns := range namespaceStats {
		out.Namespaces = append(out.Namespaces, *ns)
	}
	sort.Slice(out.Nodes, func(i, j int) bool { return out.Nodes[i].Name < out.Nodes[j].Name })
	sort.Slice(out.Namespaces, func(i, j int) bool { return out.Namespaces[i].Name < out.Namespaces[j].Name })
	out.Totals = inventoryTotals{Nodes: len(out.Nodes), Namespaces: len(out.Namespaces), Pods: len(out.Pods), Services: len(out.Services)}
	for _, node := range out.Nodes {
		out.Totals.CPUCapacity += node.Capacity.CPU
		out.Totals.CPUUsed += node.Used.CPU
		out.Totals.MemoryCapacity += node.Capacity.Memory
		out.Totals.MemoryUsed += node.Used.Memory
	}
	out.Display = inventoryDisplay{inventoryCPU(out.Totals.CPUCapacity), inventoryCPU(out.Totals.CPUUsed), inventoryBytes(out.Totals.MemoryCapacity), inventoryBytes(out.Totals.MemoryUsed)}
	out.SyncedAt = now.Format(time.RFC3339)
	return out, nil
}
