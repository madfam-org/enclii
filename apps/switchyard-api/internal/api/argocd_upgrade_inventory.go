package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

const argoInventoryObjectLimit = 2 << 20

type argoUpgradeInventory struct {
	Schema     string              `json:"schema"`
	Phase      string              `json:"phase"`
	StartedAt  string              `json:"started_at"`
	ObservedAt string              `json:"observed_at"`
	Topology   string              `json:"topology"`
	Objects    []argoUpgradeObject `json:"objects"`
	Holds      []string            `json:"holds"`
}

type argoUpgradeObject struct {
	Kind       string         `json:"kind"`
	Name       string         `json:"name"`
	Namespace  string         `json:"namespace"`
	State      string         `json:"state"`
	Metadata   map[string]any `json:"metadata,omitempty"`
	Projection map[string]any `json:"projection,omitempty"`
	Holds      []string       `json:"holds"`
}

func registerArgoUpgradeInventoryRoutes(platform *gin.RouterGroup, h *Handler) {
	// The handler repeats an unconditional active SQL platform-rank check.
	// Tenant-scope rollback and caller-supplied role claims cannot authorize it.
	platform.GET("/argocd/upgrade-preflight", h.ArgoUpgradeInventory)
}

func (h *Handler) ArgoUpgradeInventory(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	c.Header("Pragma", "no-cache")
	actor, err := uuid.Parse(c.GetString("user_id"))
	var allowed bool
	if err != nil || actor == uuid.Nil || h.repos == nil || h.repos.DB() == nil ||
		h.repos.DB().QueryRowContext(c.Request.Context(), `SELECT is_platform_admin AND active FROM users WHERE id=$1`, actor).Scan(&allowed) != nil || !allowed {
		c.JSON(http.StatusForbidden, gin.H{"error": "argocd_inventory_platform_rank_required"})
		return
	}
	// Reading one byte also refuses bodies on chunked or falsely zero-length
	// requests. There is no caller GVK, namespace, name, URL, or path selector.
	var raw []byte
	if c.Request.Body != nil {
		raw, err = io.ReadAll(io.LimitReader(c.Request.Body, 1))
	}
	if c.Request.Method != http.MethodGet || c.Request.URL.RawQuery != "" || c.Request.URL.ForceQuery ||
		c.Request.ContentLength != 0 || len(c.Request.TransferEncoding) != 0 || err != nil || len(raw) != 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "argocd_inventory_closed_request_required"})
		return
	}
	if h.k8sClient == nil || h.k8sClient.DynamicClient == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "argocd_inventory_reader_unavailable"})
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 25*time.Second)
	defer cancel()
	result := h.readArgoUpgradeInventory(ctx)
	encoded, err := json.Marshal(result)
	if err != nil || len(encoded) > 512<<10 {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "argocd_inventory_projection_limit"})
		return
	}
	c.Data(http.StatusOK, "application/json", encoded)
}

func (h *Handler) readArgoUpgradeInventory(ctx context.Context) argoUpgradeInventory {
	out := argoUpgradeInventory{
		Schema: "enclii-argocd-controller-upgrade-inventory-v1", Phase: "HELD_READ_ONLY_INVENTORY",
		StartedAt: time.Now().UTC().Format(time.RFC3339Nano), Objects: []argoUpgradeObject{},
		Holds: []string{"protected_config_and_credential_custody_unverified", "complete_backup_and_isolated_restore_unverified", "immutable_oci_signature_and_admission_unverified", "crd_roundtrip_compatibility_unverified", "ssa_conflict_dry_run_and_atomic_apply_unimplemented", "self_management_adoption_unverified", "runtime_and_fleet_acceptance_unverified"},
	}
	workloads, topology := h.readArgoUpgradeWorkloads(ctx)
	out.Topology = topology
	if topology != "NON_HA_LAYOUT_OBSERVED" && topology != "HA_LAYOUT_OBSERVED" {
		out.Holds = append(out.Holds, "unknown_or_mixed_workload_layout")
	}
	for _, target := range argoUpgradeTargets {
		item := argoUpgradeObject{Kind: target.Kind, Name: target.Name, Namespace: target.Namespace, Holds: []string{}}
		var object *unstructured.Unstructured
		var err error
		if target.Kind == "Deployment" || target.Kind == "StatefulSet" {
			object = workloads[target.Kind+"/"+target.Name]
			if object == nil {
				item.State = "not_observed"
				out.Objects = append(out.Objects, item)
				continue
			}
		} else {
			client := h.k8sClient.DynamicClient.Resource(target.GVR)
			if target.Namespace == "" {
				object, err = client.Get(ctx, target.Name, metav1.GetOptions{})
			} else {
				object, err = client.Namespace(target.Namespace).Get(ctx, target.Name, metav1.GetOptions{})
			}
		}
		if err != nil {
			item.State = "unavailable"
			switch {
			case apierrors.IsNotFound(err):
				item.State = "not_found"
			case apierrors.IsForbidden(err):
				item.State = "permission_denied"
			}
			// Transport errors may contain credentials, private hosts or bodies.
			item.Holds = append(item.Holds, "object_read_unverified")
			out.Objects = append(out.Objects, item)
			continue
		}
		item = projectArgoUpgradeObject(target, object)
		out.Objects = append(out.Objects, item)
	}
	out.ObservedAt = time.Now().UTC().Format(time.RFC3339Nano)
	return out
}

func (h *Handler) readArgoUpgradeWorkloads(ctx context.Context) (map[string]*unstructured.Unstructured, string) {
	objects := map[string]*unstructured.Unstructured{}
	unknown := false
	for _, kind := range []string{"Deployment", "StatefulSet"} {
		resource := "deployments"
		if kind == "StatefulSet" {
			resource = "statefulsets"
		}
		list, err := h.k8sClient.DynamicClient.Resource(schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: resource}).Namespace("argocd").List(ctx, metav1.ListOptions{Limit: 16})
		if err != nil || list == nil || list.GetContinue() != "" || (list.GetRemainingItemCount() != nil && *list.GetRemainingItemCount() != 0) || len(list.Items) > 16 {
			return objects, "UNKNOWN_INCOMPLETE_WORKLOAD_READ"
		}
		for i := range list.Items {
			object := &list.Items[i]
			if !argoUpgradeKnownIdentity(kind, object.GetName(), "argocd") || object.GetNamespace() != "argocd" || object.GetKind() != kind {
				unknown = true
				continue
			}
			key := kind + "/" + object.GetName()
			if objects[key] != nil {
				unknown = true
				continue
			}
			objects[key] = object
		}
	}
	if unknown {
		return objects, "UNKNOWN_EXTRA_WORKLOADS"
	}
	for _, key := range []string{"Deployment/argocd-server", "Deployment/argocd-repo-server", "Deployment/argocd-dex-server", "Deployment/argocd-applicationset-controller", "Deployment/argocd-notifications-controller", "StatefulSet/argocd-application-controller"} {
		if objects[key] == nil {
			return objects, "UNKNOWN_MISSING_COMPONENT"
		}
	}
	nonHA := objects["Deployment/argocd-redis"] != nil
	haProxy := objects["Deployment/argocd-redis-ha-haproxy"] != nil
	haServer := objects["StatefulSet/argocd-redis-ha-server"] != nil
	if nonHA && !haProxy && !haServer {
		return objects, "NON_HA_LAYOUT_OBSERVED"
	}
	if !nonHA && haProxy && haServer {
		return objects, "HA_LAYOUT_OBSERVED"
	}
	return objects, "UNKNOWN_MIXED_REDIS_LAYOUT"
}

func argoUpgradeKnownIdentity(kind, name, namespace string) bool {
	for _, target := range argoUpgradeTargets {
		if target.Kind == kind && target.Name == name && target.Namespace == namespace {
			return true
		}
	}
	return false
}
