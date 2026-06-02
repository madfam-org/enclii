package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

var opsCapabilities = []operatorCapability{
	{
		Name:        "apps",
		Status:      "partial",
		Description: "Argo application status/diff reads plus audited sync, retire, and rollback workflow contracts",
		Actions:     []string{"status", "sync", "diff", "retire", "rollback", "sync-sweep"},
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
		Name:        "jobs",
		Status:      "partial",
		Description: "Kubernetes CronJob reads plus audited one-off triggers that preserve the existing CronJob template",
		Actions:     []string{"list", "trigger"},
		Scopes:      []string{"namespace", "project", "service", "target"},
	},
	{
		Name:        "storage",
		Status:      "partial",
		Description: "PVC/PV/Longhorn reads plus attach-state and repair planning contracts",
		Actions:     []string{"volumes", "pvc", "longhorn", "repair-plan", "settings-apply", "prune-detached", "storageclass-apply"},
		Scopes:      []string{"namespace", "target"},
	},
	{
		Name:        "secrets",
		Status:      "partial",
		Description: "ExternalSecrets and Vault readiness reads plus audited sync, rotation cutover, and Vault backfill contracts",
		Actions:     []string{"external", "vault", "refresh", "sync", "sync-sweep", "rotate", "vault-backfill"},
		Scopes:      []string{"namespace", "project", "service", "target"},
	},
	{
		Name:        "policy",
		Status:      "partial",
		Description: "Kyverno report and exception reads plus waiver planning contracts",
		Actions:     []string{"violations", "exceptions", "waiver-plan", "cosign-enable"},
		Scopes:      []string{"namespace", "target"},
	},
	{
		Name:        "runners",
		Status:      "partial",
		Description: "ARC runner scale-set reads plus runner drain contracts",
		Actions:     []string{"arc", "drain"},
		Scopes:      []string{"namespace", "target"},
	},
	{
		Name:        "quote-flow",
		Status:      "partial",
		Description: "Enclii-first doctor for the Selva -> Yantra4D -> Cotiza -> ForgeSight quote path",
		Actions:     []string{"verify"},
		Scopes:      []string{"project", "target"},
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
		Description: "Tunnel/domain sync exists; DNS, Access, R2, SaaS hostname, and credential-readiness ops are contract-first",
		Actions:     []string{"zones", "zone-add-apply", "dns", "dns-apply", "tunnels", "tunnels-apply", "access", "r2", "hostnames", "credentials"},
		Scopes:      []string{"project", "service", "target"},
	},
	{
		Name:        "porkbun",
		Status:      "partial",
		Description: "Domain inventory, DNS fallback create, renewals, and nameserver ops",
		Actions:     []string{"domains", "dns", "dns-apply", "renewals", "nameservers", "nameservers-apply"},
		Scopes:      []string{"target"},
	},
	{
		Name:        "resend",
		Status:      "partial",
		Description: "Transactional email domains, DNS orchestration via Cloudflare, and send-test",
		Actions:     []string{"credentials", "domains", "domain", "emails", "domain-add-apply", "domain-verify-apply", "domain-dns-apply", "send-test-apply"},
		Scopes:      []string{"project", "service", "target", "tenant"},
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
