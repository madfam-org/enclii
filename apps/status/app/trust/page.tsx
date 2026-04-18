import type { Metadata } from 'next'
import Link from 'next/link'
import {
  Shield,
  Clock,
  Database,
  Lock,
  Download,
  Unlock,
  Mail,
  ExternalLink,
  CheckCircle2,
  CircleDot,
  Hourglass,
} from 'lucide-react'
import { getSiteConfig } from '@/lib/config'
import { cn } from '@/lib/utils'
import {
  TRUST_LAST_REVIEWED,
  uptime,
  rpo,
  rto,
  drDrill,
  dataStores,
  security,
  dataExport,
  lockInPosture,
  slaBreach,
  type CommitmentTone,
} from '@/lib/trust-commitments'

// Commitments rarely change; revalidate daily.
export const revalidate = 86400

export const metadata: Metadata = {
  title: 'Trust Center — Reliability and SLA Commitments',
  description:
    'Published uptime targets, RPO/RTO, data-protection posture, and security ' +
    'commitments for the Enclii platform. Numbers trace to runbooks; targets are dated.',
}

function ToneIcon({ tone }: { tone: CommitmentTone }) {
  if (tone === 'current') {
    return (
      <CheckCircle2
        className="size-4 text-status-operational shrink-0"
        aria-hidden="true"
      />
    )
  }
  if (tone === 'target') {
    return (
      <CircleDot
        className="size-4 text-status-maintenance shrink-0"
        aria-hidden="true"
      />
    )
  }
  return (
    <Hourglass
      className="size-4 text-status-degraded shrink-0"
      aria-hidden="true"
    />
  )
}

function ToneLabel({ tone }: { tone: CommitmentTone }) {
  const label =
    tone === 'current' ? 'Current' : tone === 'target' ? 'Target' : 'Pending drill'
  const classes =
    tone === 'current'
      ? 'bg-status-operational-muted text-status-operational'
      : tone === 'target'
        ? 'bg-status-maintenance-muted text-status-maintenance'
        : 'bg-status-degraded-muted text-status-degraded'
  return (
    <span
      className={cn(
        'inline-flex items-center gap-1 px-2 py-0.5 rounded-md text-xs font-medium',
        classes
      )}
    >
      <ToneIcon tone={tone} />
      {label}
    </span>
  )
}

function SectionHeading({
  id,
  icon: Icon,
  title,
  subtitle,
}: {
  id: string
  icon: React.ComponentType<{ className?: string }>
  title: string
  subtitle?: string
}) {
  return (
    <div className="mb-4">
      <h2
        id={id}
        className="flex items-center gap-2 text-xl font-semibold scroll-mt-20"
      >
        <Icon className="size-5 text-primary" aria-hidden="true" />
        {title}
      </h2>
      {subtitle && (
        <p className="text-sm text-muted-foreground mt-1">{subtitle}</p>
      )}
    </div>
  )
}

export default function TrustCenterPage() {
  const siteConfig = getSiteConfig()
  const lastReviewed = new Date(TRUST_LAST_REVIEWED).toLocaleDateString(
    'en-US',
    { year: 'numeric', month: 'long', day: 'numeric' }
  )

  return (
    <div className="max-w-5xl mx-auto px-4 sm:px-6 py-8 space-y-12">
      {/* Hero */}
      <header className="space-y-3">
        <div className="inline-flex items-center gap-2 px-3 py-1 rounded-full bg-primary/10 text-primary text-sm font-medium">
          <Shield className="size-4" aria-hidden="true" />
          Trust Center
        </div>
        <h1 className="text-3xl sm:text-4xl font-bold tracking-tight">
          Reliability and SLA commitments
        </h1>
        <p className="text-muted-foreground text-base max-w-3xl">
          This page lists the uptime, recovery, and data-protection commitments
          that apply to workloads running on Enclii. Every number here traces
          to an operational runbook or is clearly labeled as a target with a
          date. Last reviewed {lastReviewed}.
        </p>
        <div className="flex flex-wrap gap-3 text-sm pt-1">
          <Link
            href="/"
            className="inline-flex items-center gap-1.5 text-primary hover:underline"
          >
            Live status
            <ExternalLink className="size-3.5" aria-hidden="true" />
          </Link>
          <Link
            href="/trust/commitments.json"
            className="inline-flex items-center gap-1.5 text-primary hover:underline"
          >
            Commitments (JSON)
            <ExternalLink className="size-3.5" aria-hidden="true" />
          </Link>
          <Link
            href="/incidents"
            className="inline-flex items-center gap-1.5 text-primary hover:underline"
          >
            Incident history
            <ExternalLink className="size-3.5" aria-hidden="true" />
          </Link>
        </div>
      </header>

      {/* Reliability */}
      <section aria-labelledby="reliability">
        <SectionHeading
          id="reliability"
          icon={Clock}
          title="Reliability commitments"
          subtitle="Monthly uptime target, Recovery Point Objective (RPO), and Recovery Time Objective (RTO)."
        />
        <div className="overflow-hidden rounded-lg border border-border">
          <table className="w-full text-sm">
            <thead className="bg-muted/50 text-left">
              <tr>
                <th scope="col" className="px-4 py-3 font-medium">
                  Commitment
                </th>
                <th scope="col" className="px-4 py-3 font-medium">
                  Current
                </th>
                <th scope="col" className="px-4 py-3 font-medium">
                  Target
                </th>
                <th scope="col" className="px-4 py-3 font-medium">
                  State
                </th>
              </tr>
            </thead>
            <tbody className="divide-y divide-border">
              <tr>
                <th scope="row" className="px-4 py-3 font-medium align-top">
                  Monthly uptime
                </th>
                <td className="px-4 py-3 align-top">
                  <div className="font-semibold">
                    {uptime.monthlyTargetPercent.toFixed(1)}%
                  </div>
                  <div className="text-xs text-muted-foreground">
                    {uptime.currentPosture}
                  </div>
                </td>
                <td className="px-4 py-3 align-top">
                  <div className="font-semibold">99.9%</div>
                  <div className="text-xs text-muted-foreground">
                    By {uptime.targetDate} — {uptime.targetPosture}
                  </div>
                </td>
                <td className="px-4 py-3 align-top">
                  <ToneLabel tone="current" />
                </td>
              </tr>
              <tr>
                <th scope="row" className="px-4 py-3 font-medium align-top">
                  RPO — Postgres
                </th>
                <td className="px-4 py-3 align-top">
                  <div className="font-semibold">{rpo.postgres.current}</div>
                  <div className="text-xs text-muted-foreground">
                    {rpo.postgres.currentNotes}
                  </div>
                </td>
                <td className="px-4 py-3 align-top">
                  <div className="font-semibold">{rpo.postgres.target}</div>
                  <div className="text-xs text-muted-foreground">
                    By {rpo.postgres.targetDate} — WAL archiving to R2.
                  </div>
                </td>
                <td className="px-4 py-3 align-top">
                  <ToneLabel tone="current" />
                </td>
              </tr>
              <tr>
                <th scope="row" className="px-4 py-3 font-medium align-top">
                  RPO — Redis
                </th>
                <td className="px-4 py-3 align-top">
                  <div className="font-semibold">{rpo.redis.current}</div>
                  <div className="text-xs text-muted-foreground">
                    {rpo.redis.currentNotes}
                  </div>
                </td>
                <td className="px-4 py-3 align-top">
                  <div className="font-semibold">{rpo.redis.target}</div>
                  <div className="text-xs text-muted-foreground">
                    By {rpo.redis.targetDate} — AOF + Sentinel HA.
                  </div>
                </td>
                <td className="px-4 py-3 align-top">
                  <ToneLabel tone="current" />
                </td>
              </tr>
              <tr>
                <th scope="row" className="px-4 py-3 font-medium align-top">
                  RTO — single pod failure
                </th>
                <td className="px-4 py-3 align-top">
                  <div className="font-semibold">{rto.singlePod.current}</div>
                  <div className="text-xs text-muted-foreground">
                    {rto.singlePod.currentNotes}
                  </div>
                </td>
                <td className="px-4 py-3 align-top text-muted-foreground">
                  —
                </td>
                <td className="px-4 py-3 align-top">
                  <ToneLabel tone="current" />
                </td>
              </tr>
              <tr>
                <th scope="row" className="px-4 py-3 font-medium align-top">
                  RTO — control-plane failure
                </th>
                <td className="px-4 py-3 align-top">
                  <div className="font-semibold">{rto.controlPlane.current}</div>
                  <div className="text-xs text-muted-foreground">
                    {rto.controlPlane.currentNotes}
                  </div>
                </td>
                <td className="px-4 py-3 align-top">
                  <div className="font-semibold">{rto.controlPlane.target}</div>
                  <div className="text-xs text-muted-foreground">
                    By {rto.controlPlane.targetDate} — HA control plane with
                    embedded etcd.
                  </div>
                </td>
                <td className="px-4 py-3 align-top">
                  <ToneLabel tone="pending" />
                </td>
              </tr>
              <tr>
                <th scope="row" className="px-4 py-3 font-medium align-top">
                  RTO — full cluster rebuild
                </th>
                <td className="px-4 py-3 align-top">
                  <div className="font-semibold">{rto.fullRebuild.current}</div>
                  <div className="text-xs text-muted-foreground">
                    {rto.fullRebuild.currentNotes}
                  </div>
                </td>
                <td className="px-4 py-3 align-top text-muted-foreground">
                  —
                </td>
                <td className="px-4 py-3 align-top">
                  <ToneLabel tone="current" />
                </td>
              </tr>
            </tbody>
          </table>
        </div>
      </section>

      {/* DR Rehearsal */}
      <section aria-labelledby="dr-rehearsal">
        <SectionHeading
          id="dr-rehearsal"
          icon={Shield}
          title="Disaster-recovery rehearsals"
          subtitle="We rehearse recovery so the RTO numbers above stay honest."
        />
        <div className="rounded-lg border border-border p-5 space-y-3">
          <dl className="grid grid-cols-1 sm:grid-cols-3 gap-4 text-sm">
            <div>
              <dt className="text-muted-foreground">Cadence</dt>
              <dd className="font-medium">{drDrill.cadence}</dd>
            </div>
            <div>
              <dt className="text-muted-foreground">Last drill</dt>
              <dd className="font-medium">
                {drDrill.lastDrillDate ?? 'First drill pending'}
              </dd>
            </div>
            <div>
              <dt className="text-muted-foreground">Next drill</dt>
              <dd className="font-medium">
                Scheduled week of {drDrill.nextDrillDate}
              </dd>
            </div>
          </dl>
          <p className="text-sm text-muted-foreground">
            Every drill produces a dated entry in our internal DR log
            ({drDrill.logSource}). A summary of the most recent drill will be
            published on this page after it completes.
          </p>
        </div>
      </section>

      {/* Data protection */}
      <section aria-labelledby="data-protection">
        <SectionHeading
          id="data-protection"
          icon={Database}
          title="Data protection"
          subtitle="Backup method, retention, and current posture for each data store."
        />
        <div className="overflow-hidden rounded-lg border border-border">
          <table className="w-full text-sm">
            <thead className="bg-muted/50 text-left">
              <tr>
                <th scope="col" className="px-4 py-3 font-medium">
                  Data store
                </th>
                <th scope="col" className="px-4 py-3 font-medium">
                  Backup
                </th>
                <th scope="col" className="px-4 py-3 font-medium">
                  Retention
                </th>
                <th scope="col" className="px-4 py-3 font-medium">
                  Notes
                </th>
              </tr>
            </thead>
            <tbody className="divide-y divide-border">
              {dataStores.map((store) => (
                <tr key={store.store}>
                  <th scope="row" className="px-4 py-3 font-medium align-top">
                    {store.store}
                  </th>
                  <td className="px-4 py-3 align-top">{store.backup}</td>
                  <td className="px-4 py-3 align-top">{store.retention}</td>
                  <td className="px-4 py-3 align-top text-muted-foreground">
                    {store.statusNote}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </section>

      {/* Data export */}
      <section aria-labelledby="data-export">
        <SectionHeading
          id="data-export"
          icon={Download}
          title="Customer data export"
          subtitle="You own your data. If you ask, we hand it back."
        />
        <div className="rounded-lg border border-border p-5 space-y-3 text-sm">
          <p>{dataExport.policy}</p>
          <p className="text-muted-foreground">{dataExport.mechanism}</p>
          <p>
            Request channel:{' '}
            <a
              href={`mailto:${dataExport.contact}`}
              className="text-primary hover:underline"
            >
              {dataExport.contact}
            </a>
          </p>
          <p className="text-xs text-muted-foreground">
            A self-serve tenant-export API is planned for Phase 3.
          </p>
        </div>
      </section>

      {/* Lock-in posture */}
      <section aria-labelledby="lock-in">
        <SectionHeading
          id="lock-in"
          icon={Unlock}
          title="Vendor lock-in posture"
          subtitle="What it takes to move off Enclii if you ever need to."
        />
        <dl className="grid grid-cols-1 sm:grid-cols-2 gap-4 text-sm">
          <div className="rounded-lg border border-border p-4">
            <dt className="text-muted-foreground mb-1">License</dt>
            <dd className="font-medium">{lockInPosture.license}</dd>
          </div>
          <div className="rounded-lg border border-border p-4">
            <dt className="text-muted-foreground mb-1">Build system</dt>
            <dd className="font-medium">{lockInPosture.buildSystem}</dd>
          </div>
          <div className="rounded-lg border border-border p-4 sm:col-span-2">
            <dt className="text-muted-foreground mb-1">Portability</dt>
            <dd className="font-medium">{lockInPosture.portability}</dd>
          </div>
          <div className="rounded-lg border border-border p-4 sm:col-span-2">
            <dt className="text-muted-foreground mb-1">Source</dt>
            <dd className="font-medium">{lockInPosture.source}</dd>
          </div>
        </dl>
      </section>

      {/* Security & compliance */}
      <section aria-labelledby="security">
        <SectionHeading
          id="security"
          icon={Lock}
          title="Security and compliance"
          subtitle="Where each control stands today — including what is not yet in place."
        />
        <ul className="space-y-2">
          {security.map((item) => (
            <li
              key={item.label}
              className="flex items-start gap-3 rounded-lg border border-border p-4"
            >
              <ToneIcon tone={item.tone} />
              <div className="flex-1 min-w-0">
                <div className="flex items-center gap-2 flex-wrap">
                  <span className="font-medium text-sm">{item.label}</span>
                  <ToneLabel tone={item.tone} />
                </div>
                <p className="text-sm text-muted-foreground mt-0.5">
                  {item.status}
                </p>
              </div>
            </li>
          ))}
        </ul>
      </section>

      {/* SLA breach + contact */}
      <section aria-labelledby="sla-breach">
        <SectionHeading
          id="sla-breach"
          icon={Mail}
          title="Think we missed a commitment?"
        />
        <div className="rounded-lg border border-border p-5 space-y-3 text-sm">
          <p>{slaBreach.note}</p>
          <dl className="grid grid-cols-1 sm:grid-cols-2 gap-4">
            <div>
              <dt className="text-muted-foreground">Contact</dt>
              <dd className="font-medium">
                <a
                  href={`mailto:${slaBreach.contact}`}
                  className="text-primary hover:underline"
                >
                  {slaBreach.contact}
                </a>
              </dd>
            </div>
            <div>
              <dt className="text-muted-foreground">Acknowledgement</dt>
              <dd className="font-medium">{slaBreach.responseWindow}</dd>
            </div>
          </dl>
        </div>
      </section>

      {/* Footer note */}
      <footer className="text-xs text-muted-foreground border-t border-border pt-6">
        <p>
          {siteConfig.name} — this page is public. Commitments are reviewed at
          least quarterly. A machine-readable version is available at{' '}
          <Link
            href="/trust/commitments.json"
            className="text-primary hover:underline"
          >
            /trust/commitments.json
          </Link>
          .
        </p>
      </footer>
    </div>
  )
}
