'use client'

import { useCallback, useEffect, useMemo, useState } from 'react'
import { providerApi } from '@/lib/provider-api'
import { TenantFilter } from '@/components/providers/tenant-filter'
import { OperationPlanDialog } from '@/components/providers/operation-plan-dialog'
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@enclii/ui-components/dialog'
import { Input } from '@enclii/ui-components/input'
import { Label } from '@enclii/ui-components/label'
import { Button } from '@enclii/ui-components/button'
import { Badge } from '@enclii/ui-components/badge'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@enclii/ui-components/table'
import { RefreshCw, Mail, Plus, ShieldCheck, Send, Globe } from 'lucide-react'
import { tenantFromDomain, TENANT_CHIP_COLORS, type EcosystemTenantId } from '@enclii/ecosystem-tenants'
import type { ResendDomainRow } from '@/types/providers'

type PendingOp = {
  title: string
  description: string
  provider: string
  action: string
  body: Record<string, unknown>
} | null

export function ResendDomainsMatrix() {
  const [tenant, setTenant] = useState<EcosystemTenantId | 'all'>('all')
  const [domains, setDomains] = useState<ResendDomainRow[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [pendingOp, setPendingOp] = useState<PendingOp>(null)
  const [addInputOpen, setAddInputOpen] = useState(false)
  const [addDomainDraft, setAddDomainDraft] = useState('')
  const [addPlanOpen, setAddPlanOpen] = useState(false)
  const [addDomainTarget, setAddDomainTarget] = useState('')

  const load = useCallback(async () => {
    setLoading(true)
    setError(null)
    try {
      const resp = await providerApi.resendDomains(tenant === 'all' ? undefined : tenant)
      const raw = (resp.data as { domains?: ResendDomainRow[] })?.domains ?? []
      setDomains(raw)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load Resend domains')
    } finally {
      setLoading(false)
    }
  }, [tenant])

  useEffect(() => {
    load()
  }, [load])

  const filtered = useMemo(() => {
    if (tenant === 'all') return domains
    return domains.filter((d) => tenantFromDomain(d.name) === tenant)
  }, [domains, tenant])

  return (
    <div className="space-y-6">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div className="flex items-center gap-3">
          <div className="p-2 rounded-lg bg-primary/10 border border-primary/20">
            <Mail className="size-5 text-primary" />
          </div>
          <div>
            <h2 className="text-lg font-semibold">Email Domains (Resend)</h2>
            <p className="text-sm text-muted-foreground">Transactional sender domains by ecosystem tenant</p>
          </div>
        </div>
        <div className="flex items-center gap-2">
          <TenantFilter value={tenant} onChange={setTenant} />
          <Button variant="outline" size="sm" onClick={load} disabled={loading} className="gap-2">
            <RefreshCw className={`size-4 ${loading ? 'animate-spin' : ''}`} />
            Refresh
          </Button>
          <Button
            size="sm"
            onClick={() => {
              setAddDomainDraft('')
              setAddInputOpen(true)
            }}
            className="gap-2"
          >
            <Plus className="size-4" />
            Add domain
          </Button>
        </div>
      </div>

      {error && (
        <div className="rounded-lg border border-destructive/30 bg-destructive/10 p-4 text-sm text-destructive">
          {error}
        </div>
      )}

      <div className="rounded-lg border border-border overflow-hidden">
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Domain</TableHead>
              <TableHead>Tenant</TableHead>
              <TableHead>Resend status</TableHead>
              <TableHead className="text-right">Actions</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {filtered.map((row) => {
              const t = tenantFromDomain(row.name)
              return (
                <TableRow key={row.id || row.name}>
                  <TableCell className="font-mono text-sm">{row.name}</TableCell>
                  <TableCell>
                    <Badge className={TENANT_CHIP_COLORS[t]} variant="outline">
                      {t}
                    </Badge>
                  </TableCell>
                  <TableCell>{row.status}</TableCell>
                  <TableCell className="text-right space-x-1">
                    <Button
                      variant="ghost"
                      size="sm"
                      onClick={() =>
                        setPendingOp({
                          title: `Apply DNS for ${row.name}`,
                          description: 'Orchestrates Resend DNS requirements via Cloudflare',
                          provider: 'resend',
                          action: 'domain-dns-apply',
                          body: { args: { target: row.name } },
                        })
                      }
                    >
                      <Globe className="size-3 mr-1" />
                      DNS
                    </Button>
                    <Button
                      variant="ghost"
                      size="sm"
                      onClick={() =>
                        setPendingOp({
                          title: `Verify ${row.name}`,
                          provider: 'resend',
                          action: 'domain-verify-apply',
                          body: { args: { target: row.name } },
                        })
                      }
                    >
                      <ShieldCheck className="size-3 mr-1" />
                      Verify
                    </Button>
                    <Button
                      variant="ghost"
                      size="sm"
                      onClick={() =>
                        setPendingOp({
                          title: `Send test from ${row.name}`,
                          provider: 'resend',
                          action: 'send-test-apply',
                          body: { args: { target: row.name, to: 'ops@madfam.io' } },
                        })
                      }
                    >
                      <Send className="size-3 mr-1" />
                      Test
                    </Button>
                  </TableCell>
                </TableRow>
              )
            })}
            {!loading && filtered.length === 0 && (
              <TableRow>
                <TableCell colSpan={4} className="text-center text-muted-foreground py-8">
                  No Resend domains found for this filter
                </TableCell>
              </TableRow>
            )}
          </TableBody>
        </Table>
      </div>

      <Dialog open={addInputOpen} onOpenChange={setAddInputOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Add Resend domain</DialogTitle>
          </DialogHeader>
          <div className="space-y-2">
            <Label htmlFor="apex">Apex domain</Label>
            <Input
              id="apex"
              placeholder="enclii.dev"
              value={addDomainDraft}
              onChange={(e) => setAddDomainDraft(e.target.value)}
            />
          </div>
          <DialogFooter>
            <Button variant="outline" onClick={() => setAddInputOpen(false)}>
              Cancel
            </Button>
            <Button
              disabled={!addDomainDraft.trim()}
              onClick={() => {
                setAddDomainTarget(addDomainDraft.trim())
                setAddInputOpen(false)
                setAddPlanOpen(true)
              }}
            >
              Continue
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {addPlanOpen && addDomainTarget && (
        <OperationPlanDialog
          open={addPlanOpen}
          onOpenChange={(o) => {
            setAddPlanOpen(o)
            if (!o) setAddDomainTarget('')
          }}
          title={`Add Resend domain ${addDomainTarget}`}
          provider="resend"
          action="domain-add-apply"
          requestBody={{ args: { target: addDomainTarget } }}
          onSuccess={() => {
            setAddPlanOpen(false)
            setAddDomainTarget('')
            load()
          }}
        />
      )}

      {pendingOp && (
        <OperationPlanDialog
          open={!!pendingOp}
          onOpenChange={(o) => !o && setPendingOp(null)}
          title={pendingOp.title}
          description={pendingOp.description}
          provider={pendingOp.provider}
          action={pendingOp.action}
          requestBody={pendingOp.body}
          onSuccess={() => load()}
        />
      )}
    </div>
  )
}
