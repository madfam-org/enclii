package api

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/ecosystem"
)

type providerCatalogEntry struct {
	Name           string          `json:"name"`
	Status         string          `json:"status"`
	Description    string          `json:"description,omitempty"`
	Actions        []string        `json:"actions,omitempty"`
	Scopes         []string        `json:"scopes,omitempty"`
	Readiness      any             `json:"readiness,omitempty"`
	TenantBindings []tenantBinding `json:"tenant_bindings,omitempty"`
}

type tenantBinding struct {
	Tenant             ecosystem.TenantID `json:"tenant"`
	DisplayName        string             `json:"display_name"`
	DomainSuffixes     []string           `json:"domain_suffixes"`
	DefaultSender      string             `json:"default_sender"`
	ResendRegion       string             `json:"resend_region"`
	ResendDomainStatus string             `json:"resend_domain_status,omitempty"`
}

type providerCatalogResponse struct {
	GeneratedAt string                       `json:"generated_at"`
	Providers   []providerCatalogEntry       `json:"providers"`
	Ops         []operatorCapability         `json:"ops"`
	Ecosystem   []ecosystem.TenantDefinition `json:"ecosystem_tenants"`
}

// GetAdminProvidersCatalog aggregates provider capabilities and readiness for Dispatch / app admin UIs.
func (h *Handler) GetAdminProvidersCatalog(c *gin.Context) {
	ctx := c.Request.Context()
	providers := make([]providerCatalogEntry, 0, len(providerCapabilities))
	for _, cap := range providerCapabilities {
		entry := providerCatalogEntry{
			Name:        cap.Name,
			Status:      cap.Status,
			Description: cap.Description,
			Actions:     cap.Actions,
			Scopes:      cap.Scopes,
		}
		entry.Readiness = h.providerReadinessSnapshot(ctx, cap.Name)
		if cap.Name == "resend" {
			entry.TenantBindings = h.resendTenantBindings(ctx)
		}
		providers = append(providers, entry)
	}
	c.JSON(http.StatusOK, providerCatalogResponse{
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
		Providers:   providers,
		Ops:         opsCapabilities,
		Ecosystem:   ecosystem.AllTenants(),
	})
}

func (h *Handler) providerReadinessSnapshot(ctx context.Context, provider string) any {
	op := "providers." + provider + ".credentials"
	switch provider {
	case "cloudflare":
		return h.handleCloudflareCredentialsReadOperation(provider, "credentials", op).Data
	case "resend":
		return h.handleResendCredentialsReadOperation(provider, "credentials", op).Data
	case "github":
		if strings.TrimSpace(h.config.GitHubToken) == "" {
			return gin.H{"configured": false, "apiKeyPresent": false}
		}
		return gin.H{"configured": true, "tokenPresent": true, "secretValuesExposed": false}
	case "porkbun":
		return gin.H{"configured": h.porkbunConfigured()}
	default:
		return gin.H{"configured": false}
	}
}

func (h *Handler) resendTenantBindings(ctx context.Context) []tenantBinding {
	bindings := make([]tenantBinding, 0)
	domainStatus := map[string]string{}
	if client := h.resendClient(); client != nil && client.Configured() {
		if domains, err := client.ListDomains(ctx); err == nil {
			for _, d := range domains {
				domainStatus[strings.ToLower(d.Name)] = d.Status
			}
		}
	}
	for _, t := range ecosystem.AllTenants() {
		if t.ID == ecosystem.TenantOther {
			continue
		}
		status := ""
		for _, suffix := range t.DomainSuffixes {
			if s, ok := domainStatus[strings.ToLower(suffix)]; ok {
				status = s
				break
			}
		}
		bindings = append(bindings, tenantBinding{
			Tenant:             t.ID,
			DisplayName:        t.DisplayName,
			DomainSuffixes:     t.DomainSuffixes,
			DefaultSender:      t.DefaultSenderAddress,
			ResendRegion:       t.ResendRegion,
			ResendDomainStatus: status,
		})
	}
	return bindings
}

func (h *Handler) porkbunConfigured() bool {
	return h != nil && h.config != nil && strings.TrimSpace(h.config.PorkbunAPIKey) != ""
}
