package api

import (
	"context"
	"fmt"
	"time"

	"github.com/gin-gonic/gin"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

var operatorReadActions = map[string]map[string]map[string]bool{
	"ops": {
		"apps":    {"status": true, "diff": true},
		"pods":    {"diagnose": true, "logs": true},
		"storage": {"volumes": true, "pvc": true, "longhorn": true},
		"secrets": {"external": true, "vault": true},
		"policy":  {"violations": true, "exceptions": true},
		"runners": {"arc": true},
	},
	"providers": {
		"github":     {"runs": true, "secrets": true, "packages": true, "protection": true},
		"cloudflare": {"dns": true, "tunnels": true, "access": true, "r2": true, "hostnames": true},
		"porkbun":    {"domains": true, "dns": true, "renewals": true, "nameservers": true},
		"hetzner":    {"nodes": true, "lb": true, "vswitch": true, "storage": true, "firewall": true},
	},
}

func (h *Handler) handleReadOnlyOperatorOperation(c *gin.Context, prefix, domain, action, operation string, req operatorOperationRequest) (operatorOperationResponse, bool) {
	if !isReadOnlyOperatorAction(prefix, domain, action) {
		return operatorOperationResponse{}, false
	}

	ctx := c.Request.Context()
	switch prefix {
	case "ops":
		return h.handleOpsReadOperation(ctx, domain, action, operation, req), true
	case "providers":
		return h.handleProviderReadOperation(ctx, domain, action, operation, req), true
	default:
		return operatorOperationResponse{}, false
	}
}

func isReadOnlyOperatorAction(prefix, domain, action string) bool {
	domains, ok := operatorReadActions[prefix]
	if !ok {
		return false
	}
	actions, ok := domains[domain]
	return ok && actions[action]
}

func operatorReadSuccess(operation, domain, action string, data any) operatorOperationResponse {
	return operatorOperationResponse{
		OperationID: fmt.Sprintf("op_%d", time.Now().UTC().UnixNano()),
		Operation:   operation,
		Status:      "succeeded",
		DryRun:      true,
		Summary:     fmt.Sprintf("%s.%s read completed through Enclii", domain, action),
		Data:        data,
	}
}

func operatorReadUnavailable(operation, domain, action, detail string) operatorOperationResponse {
	return operatorOperationResponse{
		OperationID: fmt.Sprintf("op_%d", time.Now().UTC().UnixNano()),
		Operation:   operation,
		Status:      "adapter_unconfigured",
		DryRun:      true,
		Summary:     fmt.Sprintf("%s.%s is part of the Enclii contract but needs adapter configuration", domain, action),
		Warnings:    []string{detail},
		Next:        []string{"configure or wire the Enclii read adapter", "avoid raw tooling except break-glass diagnostics"},
	}
}

func operatorReadFailed(operation, domain, action string, err error) operatorOperationResponse {
	return operatorOperationResponse{
		OperationID: fmt.Sprintf("op_%d", time.Now().UTC().UnixNano()),
		Operation:   operation,
		Status:      "failed",
		DryRun:      true,
		Summary:     fmt.Sprintf("%s.%s read failed through Enclii", domain, action),
		Warnings:    []string{err.Error()},
	}
}

func (h *Handler) handleOpsReadOperation(ctx context.Context, domain, action, operation string, req operatorOperationRequest) operatorOperationResponse {
	if h.k8sClient == nil {
		return operatorReadUnavailable(operation, domain, action, "kubernetes client is not configured on switchyard-api")
	}

	switch domain + "." + action {
	case "apps.status":
		data, err := h.readArgoApplications(ctx, req)
		if err != nil {
			return operatorReadFailed(operation, domain, action, err)
		}
		return operatorReadSuccess(operation, domain, action, data)
	case "apps.diff":
		if h.k8sClient.DynamicClient == nil {
			return operatorReadUnavailable(operation, domain, action, "kubernetes dynamic client is not configured; Argo Application drift cannot be read")
		}
		data, err := h.readArgoApplicationDrift(ctx, req)
		if err != nil {
			return operatorReadFailed(operation, domain, action, err)
		}
		return operatorReadSuccess(operation, domain, action, data)
	case "pods.diagnose":
		if h.k8sClient.Clientset == nil {
			return operatorReadUnavailable(operation, domain, action, "kubernetes typed client is not configured on switchyard-api")
		}
		data, err := h.readPods(ctx, req)
		if err != nil {
			return operatorReadFailed(operation, domain, action, err)
		}
		return operatorReadSuccess(operation, domain, action, data)
	case "pods.logs":
		if h.k8sClient.Clientset == nil {
			return operatorReadUnavailable(operation, domain, action, "kubernetes typed client is not configured on switchyard-api")
		}
		data, err := h.readPodLogs(ctx, req)
		if err != nil {
			return operatorReadFailed(operation, domain, action, err)
		}
		return operatorReadSuccess(operation, domain, action, data)
	case "storage.pvc":
		if h.k8sClient.Clientset == nil {
			return operatorReadUnavailable(operation, domain, action, "kubernetes typed client is not configured on switchyard-api")
		}
		data, err := h.readPVCs(ctx, req)
		if err != nil {
			return operatorReadFailed(operation, domain, action, err)
		}
		return operatorReadSuccess(operation, domain, action, data)
	case "storage.volumes":
		if h.k8sClient.Clientset == nil {
			return operatorReadUnavailable(operation, domain, action, "kubernetes typed client is not configured on switchyard-api")
		}
		data, err := h.readPVs(ctx)
		if err != nil {
			return operatorReadFailed(operation, domain, action, err)
		}
		return operatorReadSuccess(operation, domain, action, data)
	case "storage.longhorn":
		return h.readDynamicOperatorResources(ctx, operation, domain, action, req, schema.GroupVersionResource{Group: "longhorn.io", Version: "v1beta2", Resource: "volumes"}, "longhorn-system")
	case "secrets.external":
		return h.readDynamicOperatorResources(ctx, operation, domain, action, req, schema.GroupVersionResource{Group: "external-secrets.io", Version: "v1beta1", Resource: "externalsecrets"}, operationNamespace(req, "default"))
	case "secrets.vault":
		if h.k8sClient.Clientset == nil {
			return operatorReadUnavailable(operation, domain, action, "kubernetes typed client is not configured on switchyard-api")
		}
		data, err := h.readPodsInNamespace(ctx, operationNamespace(req, "vault"), req, "app.kubernetes.io/name=vault")
		if err != nil {
			return operatorReadFailed(operation, domain, action, err)
		}
		return operatorReadSuccess(operation, domain, action, data)
	case "policy.violations":
		return h.readDynamicOperatorResources(ctx, operation, domain, action, req, schema.GroupVersionResource{Group: "wgpolicyk8s.io", Version: "v1alpha2", Resource: "policyreports"}, operationNamespace(req, "default"))
	case "policy.exceptions":
		return h.readDynamicOperatorResources(ctx, operation, domain, action, req, schema.GroupVersionResource{Group: "kyverno.io", Version: "v2", Resource: "policyexceptions"}, operationNamespace(req, "default"))
	case "runners.arc":
		return h.readDynamicOperatorResources(ctx, operation, domain, action, req, schema.GroupVersionResource{Group: "actions.github.com", Version: "v1alpha1", Resource: "autoscalingrunnersets"}, operationNamespace(req, "arc-runners"))
	default:
		return operatorReadUnavailable(operation, domain, action, "read adapter is not wired for this operation")
	}
}
