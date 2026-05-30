'use client'

import { useCallback, useEffect, useState } from 'react'
import Link from 'next/link'
import {
  Github,
  Shield,
  Mail,
  Globe,
  ArrowUpRight,
  CheckCircle2,
  AlertTriangle,
} from 'lucide-react'
import { useAuth } from '@/contexts/AuthContext'
import { apiGet } from '@/lib/api'

type ProviderCatalog = {
  generated_at: string
  providers: Array<{
    name: string
    status: string
    readiness?: Record<string, unknown>
    tenant_bindings?: Array<{
      tenant: string
      default_sender: string
      resend_domain_status?: string
    }>
  }>
}

type IntegrationCardProps = {
  title: string
  description: string
  icon: React.ReactNode
  href: string
  status?: 'ready' | 'partial' | 'unknown'
  statusNote?: string
  adminOnly?: boolean
}

function IntegrationCard({
  title,
  description,
  icon,
  href,
  status = 'unknown',
  statusNote,
  adminOnly,
}: IntegrationCardProps) {
  const statusIcon =
    status === 'ready' ? (
      <CheckCircle2 className="size-4 text-emerald-500" />
    ) : status === 'partial' ? (
      <AlertTriangle className="size-4 text-amber-500" />
    ) : (
      <AlertTriangle className="size-4 text-muted-foreground" />
    )

  return (
    <Link
      href={href}
      className="block rounded-lg border border-border bg-card p-5 hover:border-enclii-blue/40 transition-colors"
    >
      <div className="flex items-start justify-between gap-3">
        <div className="flex items-center gap-3">
          <div className="p-2 rounded-md bg-enclii-blue/10 text-enclii-blue">{icon}</div>
          <div>
            <h3 className="font-semibold flex items-center gap-2">
              {title}
              {adminOnly && (
                <span className="text-[10px] uppercase tracking-wide text-muted-foreground border border-border rounded px-1">
                  Admin
                </span>
              )}
            </h3>
            <p className="text-sm text-muted-foreground mt-1">{description}</p>
          </div>
        </div>
        <ArrowUpRight className="size-4 text-muted-foreground shrink-0" />
      </div>
      <div className="mt-4 flex items-center gap-2 text-xs text-muted-foreground">
        {statusIcon}
        <span>{statusNote ?? status}</span>
      </div>
    </Link>
  )
}

export default function IntegrationsPage() {
  const { user } = useAuth()
  const isAdmin = !!user?.roles?.includes('admin')
  const [catalog, setCatalog] = useState<ProviderCatalog | null>(null)
  const [githubLinked, setGithubLinked] = useState<boolean | null>(null)

  useEffect(() => {
    if (!isAdmin) return
    apiGet<ProviderCatalog>('/v1/admin/providers/catalog')
      .then(setCatalog)
      .catch(() => setCatalog(null))
  }, [isAdmin])

  useEffect(() => {
    apiGet<{ linked?: boolean }>('/v1/integrations/github/status')
      .then((s) => setGithubLinked(!!s.linked))
      .catch(() => setGithubLinked(null))
  }, [])

  const resend = catalog?.providers.find((p) => p.name === 'resend')
  const cloudflare = catalog?.providers.find((p) => p.name === 'cloudflare')
  const resendReady = resend?.readiness?.configured === true
  const cfReady = cloudflare?.readiness?.configured === true

  return (
    <div className="max-w-4xl mx-auto space-y-8">
      <div>
        <h1 className="text-2xl font-semibold">Integrations</h1>
        <p className="text-muted-foreground mt-1">
          Connect GitHub, SSO, and platform email/DNS providers.
        </p>
      </div>

      {isAdmin && catalog && (
        <div className="rounded-lg border border-border bg-muted/20 px-4 py-3 text-sm flex flex-wrap gap-4">
          <span className="font-medium">Platform readiness</span>
          <span>Resend: {resendReady ? 'ready' : 'not ready'}</span>
          <span>Cloudflare: {cfReady ? 'ready' : 'partial'}</span>
          <Link
            href="https://admin.enclii.dev/providers"
            className="text-enclii-blue hover:underline ml-auto"
          >
            Open Dispatch Provider Hub
          </Link>
        </div>
      )}

      <div className="grid gap-4 sm:grid-cols-2">
        <IntegrationCard
          title="GitHub"
          description="Import repositories and deploy from your GitHub account."
          icon={<Github className="size-5" />}
          href="/projects"
          status={githubLinked ? 'ready' : githubLinked === false ? 'partial' : 'unknown'}
          statusNote={
            githubLinked === true
              ? 'Connected'
              : githubLinked === false
                ? 'Not linked — connect during project import'
                : 'Status unknown'
          }
        />
        <IntegrationCard
          title="Janua SSO"
          description="Operational status for the configured identity provider."
          icon={<Shield className="size-5" />}
          href="/integrations/janua"
          status="ready"
          statusNote="View reachability and session roles"
        />
        {isAdmin && (
          <>
            <IntegrationCard
              title="Email (Resend)"
              description="Transactional sender domains for signup and notifications."
              icon={<Mail className="size-5" />}
              href="https://admin.enclii.dev/providers/resend"
              status={resendReady ? 'ready' : 'partial'}
              statusNote={
                resendReady
                  ? String(resend?.readiness?.fromAddress ?? 'Sender configured')
                  : 'Configure in Dispatch Provider Hub'
              }
              adminOnly
            />
            <IntegrationCard
              title="Cloudflare"
              description="DNS and tunnel sync for custom domains."
              icon={<Globe className="size-5" />}
              href="/domains"
              status={cfReady ? 'ready' : 'partial'}
              statusNote={cfReady ? 'Domain sync configured' : 'Partial — see Domains page'}
              adminOnly
            />
          </>
        )}
      </div>

      {!isAdmin && (
        <p className="text-sm text-muted-foreground">
          Resend and Cloudflare platform status is visible to admins only. Mutations run in{' '}
          <a href="https://admin.enclii.dev/providers" className="text-enclii-blue hover:underline">
            Dispatch
          </a>
          .
        </p>
      )}
    </div>
  )
}
