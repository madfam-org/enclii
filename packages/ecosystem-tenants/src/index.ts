import registry from '../tenants.json'

export type EcosystemTenantId =
  | 'madfam'
  | 'janua'
  | 'enclii'
  | 'suluna'
  | 'primavera'
  | 'other'

export interface EcosystemTenantDefinition {
  id: EcosystemTenantId
  displayName: string
  domainSuffixes: string[]
  defaultSenderDomain: string
  defaultSenderAddress: string
  resendRegion: string
}

export const ECOSYSTEM_TENANTS: EcosystemTenantDefinition[] =
  registry.tenants as EcosystemTenantDefinition[]

export function tenantByID(id: string): EcosystemTenantDefinition | undefined {
  return ECOSYSTEM_TENANTS.find((t) => t.id === id)
}

/** Infer ecosystem tenant from a hostname or apex domain. */
export function tenantFromDomain(domain: string): EcosystemTenantId {
  const normalized = domain.toLowerCase().replace(/\.$/, '')
  for (const tenant of ECOSYSTEM_TENANTS) {
    if (tenant.id === 'other') continue
    for (const suffix of tenant.domainSuffixes) {
      if (normalized === suffix || normalized.endsWith('.' + suffix)) {
        return tenant.id
      }
    }
  }
  return 'other'
}

export function domainsForTenant(tenantId: EcosystemTenantId): string[] {
  return tenantByID(tenantId)?.domainSuffixes ?? []
}

export function defaultSenderForTenant(tenantId: EcosystemTenantId): string {
  return tenantByID(tenantId)?.defaultSenderAddress ?? ''
}

export const TENANT_CHIP_COLORS: Record<EcosystemTenantId, string> = {
  madfam: 'bg-purple-500/20 text-purple-400 border-purple-500/30',
  suluna: 'bg-blue-500/20 text-blue-400 border-blue-500/30',
  primavera: 'bg-amber-500/20 text-amber-400 border-amber-500/30',
  janua: 'bg-emerald-500/20 text-emerald-400 border-emerald-500/30',
  enclii: 'bg-cyan-500/20 text-cyan-400 border-cyan-500/30',
  other: 'bg-gray-500/20 text-gray-400 border-gray-500/30',
}
