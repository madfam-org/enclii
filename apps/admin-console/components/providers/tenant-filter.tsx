'use client'

import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@enclii/ui-components/select'
import { ECOSYSTEM_TENANTS, type EcosystemTenantId } from '@enclii/ecosystem-tenants'

type TenantFilterProps = {
  value: EcosystemTenantId | 'all'
  onChange: (tenant: EcosystemTenantId | 'all') => void
}

export function TenantFilter({ value, onChange }: TenantFilterProps) {
  return (
    <Select value={value} onValueChange={(v) => onChange(v as EcosystemTenantId | 'all')}>
      <SelectTrigger className="w-[180px]">
        <SelectValue placeholder="All tenants" />
      </SelectTrigger>
      <SelectContent>
        <SelectItem value="all">All tenants</SelectItem>
        {ECOSYSTEM_TENANTS.filter((t) => t.id !== 'other').map((t) => (
          <SelectItem key={t.id} value={t.id}>
            {t.displayName}
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  )
}
