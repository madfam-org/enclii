package api

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

type operatorOperationRequest struct {
	Operation      string            `json:"operation"`
	DryRun         bool              `json:"dry_run"`
	Reason         string            `json:"reason,omitempty"`
	IdempotencyKey string            `json:"idempotency_key,omitempty"`
	Scope          map[string]string `json:"scope,omitempty"`
	Args           map[string]string `json:"args,omitempty"`
}

type operatorOperationStep struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Detail string `json:"detail,omitempty"`
}

type operatorOperationResponse struct {
	OperationID string                  `json:"operation_id,omitempty"`
	AuditID     string                  `json:"audit_id,omitempty"`
	Operation   string                  `json:"operation"`
	Status      string                  `json:"status"`
	DryRun      bool                    `json:"dry_run"`
	Summary     string                  `json:"summary,omitempty"`
	Data        any                     `json:"data,omitempty"`
	Steps       []operatorOperationStep `json:"steps,omitempty"`
	Warnings    []string                `json:"warnings,omitempty"`
	Next        []string                `json:"next,omitempty"`
}

type operatorCapability struct {
	Name        string   `json:"name"`
	Status      string   `json:"status"`
	Description string   `json:"description,omitempty"`
	Actions     []string `json:"actions,omitempty"`
	Scopes      []string `json:"scopes,omitempty"`
}

type operatorCapabilitiesResponse struct {
	Capabilities []operatorCapability `json:"capabilities"`
}

var opsCapabilities = []operatorCapability{
	{
		Name:        "apps",
		Status:      "partial",
		Description: "Argo application status reads plus sync, diff, and rollback workflow contracts",
		Actions:     []string{"status", "sync", "diff", "rollback"},
		Scopes:      []string{"namespace", "project", "service", "target"},
	},
	{
		Name:        "pods",
		Status:      "partial",
		Description: "Pod diagnosis and logs reads plus restart workflow contracts",
		Actions:     []string{"diagnose", "logs", "restart"},
		Scopes:      []string{"namespace", "service", "target"},
	},
	{
		Name:        "storage",
		Status:      "partial",
		Description: "PVC/PV/Longhorn reads plus attach-state and repair planning contracts",
		Actions:     []string{"volumes", "pvc", "longhorn", "repair-plan"},
		Scopes:      []string{"namespace", "target"},
	},
	{
		Name:        "secrets",
		Status:      "partial",
		Description: "ExternalSecrets and Vault readiness reads plus refresh contracts",
		Actions:     []string{"external", "vault", "refresh"},
		Scopes:      []string{"namespace", "project", "service", "target"},
	},
	{
		Name:        "policy",
		Status:      "partial",
		Description: "Kyverno report and exception reads plus waiver planning contracts",
		Actions:     []string{"violations", "exceptions", "waiver-plan"},
		Scopes:      []string{"namespace", "target"},
	},
	{
		Name:        "runners",
		Status:      "partial",
		Description: "ARC runner scale-set reads plus runner drain contracts",
		Actions:     []string{"arc", "drain"},
		Scopes:      []string{"namespace", "target"},
	},
}

var providerCapabilities = []operatorCapability{
	{
		Name:        "github",
		Status:      "partial",
		Description: "GitHub App is live; Actions, secrets, GHCR, and protection ops are contract-first",
		Actions:     []string{"runs", "rerun", "cancel", "secrets", "packages", "protection"},
		Scopes:      []string{"project", "service", "target"},
	},
	{
		Name:        "cloudflare",
		Status:      "partial",
		Description: "Tunnel/domain sync exists; DNS, Access, R2, and SaaS hostname ops are contract-first",
		Actions:     []string{"dns", "dns-apply", "tunnels", "access", "r2", "hostnames"},
		Scopes:      []string{"project", "service", "target"},
	},
	{
		Name:        "porkbun",
		Status:      "contract",
		Description: "Domain inventory, DNS fallback, renewals, and nameserver ops",
		Actions:     []string{"domains", "dns", "renewals", "nameservers"},
		Scopes:      []string{"target"},
	},
	{
		Name:        "hetzner",
		Status:      "contract",
		Description: "Robot/Cloud nodes, load balancers, vSwitch, Storage Boxes, and firewall ops",
		Actions:     []string{"nodes", "lb", "vswitch", "storage", "firewall"},
		Scopes:      []string{"target"},
	},
}

// GetOpsCapabilities returns the operator workflow contract supported by this
// Switchyard API build. Capability status is explicit so Selva can distinguish
// live adapters from contract-only surfaces.
func (h *Handler) GetOpsCapabilities(c *gin.Context) {
	c.JSON(http.StatusOK, operatorCapabilitiesResponse{Capabilities: opsCapabilities})
}

// GetProviderCapabilities returns the external-provider workflow contract.
func (h *Handler) GetProviderCapabilities(c *gin.Context) {
	c.JSON(http.StatusOK, operatorCapabilitiesResponse{Capabilities: providerCapabilities})
}

// HandleOpsOperation is the P0 operation contract endpoint for kubectl/Argo
// replacement workflows. Dry-run requests return a deterministic plan; apply
// requests return 501 until the concrete adapter is wired.
func (h *Handler) HandleOpsOperation(c *gin.Context) {
	domain := c.Param("domain")
	action := c.Param("action")
	h.handleOperatorOperation(c, "ops", domain, action, opsCapabilities)
}

// HandleProviderOperation is the P0 operation contract endpoint for
// gh/Cloudflare/Porkbun/Hetzner replacement workflows. Dry-run requests return
// a deterministic plan; apply requests return 501 until the provider adapter is
// wired.
func (h *Handler) HandleProviderOperation(c *gin.Context) {
	provider := c.Param("provider")
	action := c.Param("action")
	h.handleOperatorOperation(c, "providers", provider, action, providerCapabilities)
}

func (h *Handler) handleOperatorOperation(c *gin.Context, prefix, domain, action string, capabilities []operatorCapability) {
	var req operatorOperationRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("invalid operation request: %s", err.Error())})
		return
	}

	operation := strings.TrimSpace(req.Operation)
	if operation == "" {
		operation = fmt.Sprintf("%s.%s.%s", prefix, domain, action)
	}
	if !operationSupported(domain, action, capabilities) {
		c.JSON(http.StatusNotFound, gin.H{"error": fmt.Sprintf("unsupported operation %s.%s", domain, action)})
		return
	}
	if !req.DryRun && strings.TrimSpace(req.Reason) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "reason is required when dry_run=false"})
		return
	}

	if req.DryRun {
		if resp, handled := h.handleReadOnlyOperatorOperation(c, prefix, domain, action, operation, req); handled {
			c.JSON(http.StatusOK, resp)
			return
		}
	}

	resp := operatorOperationResponse{
		OperationID: fmt.Sprintf("op_%d", time.Now().UTC().UnixNano()),
		Operation:   operation,
		Status:      "planned",
		DryRun:      req.DryRun,
		Summary:     fmt.Sprintf("%s.%s is covered by the Enclii operation contract", domain, action),
		Steps: []operatorOperationStep{
			{Name: "authorize", Status: "planned", Detail: "check caller RBAC and provider scope"},
			{Name: "load-state", Status: "planned", Detail: "read current live/provider state"},
			{Name: "diff", Status: "planned", Detail: "compare requested intent with current state"},
			{Name: "audit", Status: "planned", Detail: "write audit event before mutation"},
		},
		Warnings: []string{
			"adapter execution is not wired in this build; dry-run is safe, apply is blocked",
		},
		Next: []string{
			"wire a concrete adapter for this capability",
			"add policy gates and idempotent apply semantics",
			"bind Selva agents to this endpoint instead of raw shell tools",
		},
	}

	if req.DryRun {
		c.JSON(http.StatusOK, resp)
		return
	}

	resp.Status = "adapter_required"
	resp.DryRun = false
	c.JSON(http.StatusNotImplemented, resp)
}

func operationSupported(domain, action string, capabilities []operatorCapability) bool {
	for _, capability := range capabilities {
		if capability.Name != domain {
			continue
		}
		for _, supportedAction := range capability.Actions {
			if supportedAction == action {
				return true
			}
		}
	}
	return false
}
