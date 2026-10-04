package api

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

type inventoryAppCondition struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}
type inventoryApplication struct {
	Name                 string                  `json:"name"`
	Namespace            string                  `json:"namespace"`
	Sync                 string                  `json:"sync_status"`
	Health               string                  `json:"health_status"`
	LastSync             *string                 `json:"last_sync_at"`
	TargetRevision       *string                 `json:"target_revision"`
	CurrentRevision      *string                 `json:"current_revision"`
	SourceRepo           *string                 `json:"source_repo"`
	SourcePath           *string                 `json:"source_path"`
	DestinationNamespace *string                 `json:"destination_namespace"`
	DestinationServer    *string                 `json:"destination_server"`
	Conditions           []inventoryAppCondition `json:"conditions"`
	Message              *string                 `json:"message"`
}
type inventoryApplications struct {
	Applications []inventoryApplication `json:"applications"`
	SyncedAt     string                 `json:"synced_at"`
}

func inventoryString(obj map[string]any, fields ...string) string {
	s, _, _ := unstructured.NestedString(obj, fields...)
	return s
}
func inventoryOptional(obj map[string]any, fields ...string) *string {
	s, found, _ := unstructured.NestedString(obj, fields...)
	if !found {
		return nil
	}
	return &s
}
func inventoryStringOr(obj map[string]any, fallback string, fields ...string) string {
	s := inventoryString(obj, fields...)
	if s == "" {
		return fallback
	}
	return s
}
func (h *Handler) readInventoryApplications(ctx context.Context, namespace string) (inventoryApplications, error) {
	out := inventoryApplications{Applications: []inventoryApplication{}}
	list, err := h.k8sClient.DynamicClient.Resource(argoApplicationGVR).Namespace(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return out, err
	}
	for _, item := range list.Items {
		obj := item.Object
		last := inventoryOptional(obj, "status", "operationState", "finishedAt")
		if last == nil {
			last = inventoryOptional(obj, "status", "operationState", "startedAt")
		}
		conditions := []inventoryAppCondition{}
		raw, _, _ := unstructured.NestedSlice(obj, "status", "conditions")
		for _, v := range raw {
			if c, ok := v.(map[string]any); ok {
				if t := inventoryString(c, "type"); t != "" {
					conditions = append(conditions, inventoryAppCondition{t, inventoryString(c, "message")})
				}
			}
		}
		out.Applications = append(out.Applications, inventoryApplication{
			Name: item.GetName(), Namespace: item.GetNamespace(), Sync: inventoryStringOr(obj, "Unknown", "status", "sync", "status"), Health: inventoryStringOr(obj, "Unknown", "status", "health", "status"), LastSync: last,
			TargetRevision: inventoryOptional(obj, "spec", "source", "targetRevision"), CurrentRevision: inventoryOptional(obj, "status", "sync", "revision"),
			SourceRepo: inventoryOptional(obj, "spec", "source", "repoURL"), SourcePath: inventoryOptional(obj, "spec", "source", "path"),
			DestinationNamespace: inventoryOptional(obj, "spec", "destination", "namespace"), DestinationServer: inventoryOptional(obj, "spec", "destination", "server"),
			Conditions: conditions, Message: inventoryOptional(obj, "status", "health", "message"),
		})
	}
	sort.Slice(out.Applications, func(i, j int) bool { return out.Applications[i].Name < out.Applications[j].Name })
	out.SyncedAt = time.Now().UTC().Format(time.RFC3339)
	return out, nil
}

type inventoryReplica struct {
	Name    string `json:"name"`
	Node    string `json:"node"`
	Running bool   `json:"running"`
	Mode    string `json:"mode"`
}
type inventoryVolume struct {
	Name          string             `json:"name"`
	Namespace     *string            `json:"namespace"`
	PVC           *string            `json:"pvc_name"`
	State         string             `json:"state"`
	Robustness    string             `json:"robustness"`
	Size          int64              `json:"size_bytes"`
	Display       string             `json:"size_display"`
	ReplicaTarget int64              `json:"replica_count_target"`
	Replicas      []inventoryReplica `json:"replicas"`
	Node          *string            `json:"attached_to_node"`
	Pod           *string            `json:"attached_to_pod"`
	Created       *string            `json:"created_at"`
	Engine        *string            `json:"data_engine"`
}
type inventoryVolumeSummary struct {
	Total    int    `json:"total"`
	Healthy  int    `json:"healthy"`
	Degraded int    `json:"degraded"`
	Faulted  int    `json:"faulted"`
	Bytes    int64  `json:"total_bytes"`
	Display  string `json:"total_size_display"`
}
type inventoryVolumes struct {
	Volumes  []inventoryVolume      `json:"volumes"`
	Summary  inventoryVolumeSummary `json:"summary"`
	SyncedAt string                 `json:"synced_at"`
}

func (h *Handler) readInventoryVolumes(ctx context.Context, namespace string) (inventoryVolumes, error) {
	out := inventoryVolumes{Volumes: []inventoryVolume{}}
	gvr := schema.GroupVersionResource{Group: "longhorn.io", Version: "v1beta2", Resource: "volumes"}
	volumes, err := h.k8sClient.DynamicClient.Resource(gvr).Namespace(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return out, err
	}
	gvr.Resource = "replicas"
	replicas, err := h.k8sClient.DynamicClient.Resource(gvr).Namespace(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return out, err
	}
	byVolume := map[string][]inventoryReplica{}
	for _, replica := range replicas.Items {
		obj := replica.Object
		volume := inventoryString(obj, "spec", "volumeName")
		byVolume[volume] = append(byVolume[volume], inventoryReplica{Name: replica.GetName(), Node: inventoryString(obj, "spec", "nodeID"), Running: inventoryString(obj, "status", "currentState") == "running", Mode: inventoryString(obj, "status", "mode")})
	}
	for _, volume := range volumes.Items {
		obj := volume.Object
		size := int64(0)
		if raw := inventoryString(obj, "spec", "size"); raw != "" {
			quantity, err := resource.ParseQuantity(raw)
			if err != nil {
				return out, fmt.Errorf("invalid volume size: %w", err)
			}
			size = quantity.Value()
		}
		target, _, _ := unstructured.NestedInt64(obj, "spec", "numberOfReplicas")
		reps := byVolume[volume.GetName()]
		if reps == nil {
			reps = []inventoryReplica{}
		}
		sort.Slice(reps, func(i, j int) bool { return reps[i].Name < reps[j].Name })
		var pod *string
		workloads, _, _ := unstructured.NestedSlice(obj, "status", "kubernetesStatus", "workloadsStatus")
		if len(workloads) > 0 {
			if first, ok := workloads[0].(map[string]any); ok {
				pod = inventoryOptional(first, "podName")
			}
		}
		v := inventoryVolume{Name: volume.GetName(), Namespace: inventoryOptional(obj, "status", "kubernetesStatus", "namespace"), PVC: inventoryOptional(obj, "status", "kubernetesStatus", "pvcName"),
			State: strings.ToLower(inventoryStringOr(obj, "unknown", "status", "state")), Robustness: strings.ToLower(inventoryStringOr(obj, "unknown", "status", "robustness")),
			Size: size, Display: inventoryBytes(size), ReplicaTarget: target, Replicas: reps, Node: inventoryOptional(obj, "status", "currentNodeID"), Pod: pod,
			Created: inventoryTimestamp(volume.GetCreationTimestamp().Time), Engine: inventoryOptional(obj, "spec", "dataEngine")}
		out.Volumes = append(out.Volumes, v)
		out.Summary.Bytes += size
		switch v.Robustness {
		case "healthy":
			out.Summary.Healthy++
		case "degraded":
			out.Summary.Degraded++
		case "faulted":
			out.Summary.Faulted++
		}
	}
	rank := func(s string) int {
		switch s {
		case "faulted":
			return 0
		case "degraded":
			return 1
		case "healthy":
			return 2
		default:
			return 3
		}
	}
	sort.Slice(out.Volumes, func(i, j int) bool {
		a, b := out.Volumes[i], out.Volumes[j]
		if rank(a.Robustness) != rank(b.Robustness) {
			return rank(a.Robustness) < rank(b.Robustness)
		}
		return a.Name < b.Name
	})
	out.Summary.Total = len(out.Volumes)
	out.Summary.Display = inventoryBytes(out.Summary.Bytes)
	out.SyncedAt = time.Now().UTC().Format(time.RFC3339)
	return out, nil
}
