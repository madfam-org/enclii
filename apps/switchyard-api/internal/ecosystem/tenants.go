package ecosystem

import (
	_ "embed"
	"encoding/json"
	"strings"
)

// TenantID identifies a MADFAM ecosystem slice.
type TenantID string

const (
	TenantMADFAM    TenantID = "madfam"
	TenantJanua     TenantID = "janua"
	TenantEnclii    TenantID = "enclii"
	TenantSuluna    TenantID = "suluna"
	TenantPrimavera TenantID = "primavera"
	TenantOther     TenantID = "other"
)

// TenantDefinition describes one ecosystem tenant slice.
type TenantDefinition struct {
	ID                   TenantID `json:"id"`
	DisplayName          string   `json:"displayName"`
	DomainSuffixes       []string `json:"domainSuffixes"`
	DefaultSenderDomain  string   `json:"defaultSenderDomain"`
	DefaultSenderAddress string   `json:"defaultSenderAddress"`
	ResendRegion         string   `json:"resendRegion"`
}

type tenantRegistry struct {
	Tenants []TenantDefinition `json:"tenants"`
}

//go:embed tenants.json
var tenantsJSON []byte

var registry tenantRegistry

func init() {
	if err := json.Unmarshal(tenantsJSON, &registry); err != nil {
		panic("ecosystem: invalid tenants.json: " + err.Error())
	}
}

// AllTenants returns every tenant definition including "other".
func AllTenants() []TenantDefinition {
	out := make([]TenantDefinition, len(registry.Tenants))
	copy(out, registry.Tenants)
	return out
}

// TenantByID returns a tenant definition or nil.
func TenantByID(id TenantID) *TenantDefinition {
	for i := range registry.Tenants {
		if registry.Tenants[i].ID == id {
			t := registry.Tenants[i]
			return &t
		}
	}
	return nil
}

// TenantFromDomain infers the ecosystem tenant from a hostname or apex domain.
func TenantFromDomain(domain string) TenantID {
	normalized := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(domain), "."))
	for _, tenant := range registry.Tenants {
		if tenant.ID == TenantOther {
			continue
		}
		for _, suffix := range tenant.DomainSuffixes {
			if normalized == suffix || strings.HasSuffix(normalized, "."+suffix) {
				return tenant.ID
			}
		}
	}
	return TenantOther
}

// DomainsForTenant returns known apex domains for a tenant.
func DomainsForTenant(id TenantID) []string {
	if t := TenantByID(id); t != nil {
		return append([]string(nil), t.DomainSuffixes...)
	}
	return nil
}

// DefaultSenderForTenant returns noreply@ style address for a tenant.
func DefaultSenderForTenant(id TenantID) string {
	if t := TenantByID(id); t != nil {
		return t.DefaultSenderAddress
	}
	return ""
}

// ResendRegionForDomain returns the Resend region for a domain (defaults us-east-1).
func ResendRegionForDomain(domain string) string {
	if t := TenantByID(TenantFromDomain(domain)); t != nil && t.ResendRegion != "" {
		return t.ResendRegion
	}
	return "us-east-1"
}
