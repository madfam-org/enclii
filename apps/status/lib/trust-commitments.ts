/**
 * Trust-center source of truth.
 *
 * Every number here must trace to a runbook or be explicitly marked as a
 * target. When a commitment improves (e.g. WAL archiving lands, HA ships),
 * update this file and the machine-readable JSON at
 * /trust/commitments.json picks up the change automatically.
 *
 * Referenced docs (private, internal-devops repo):
 * - runbooks/disaster-recovery.md        — RPO/RTO numbers
 * - audits/2026-04-enclii-platform-audit.md — DR posture and Phase 1 targets
 *
 * DO NOT publish marketing claims here. Honest specifics only.
 */

/** ISO-8601 date this file was last reviewed. */
export const TRUST_LAST_REVIEWED = '2026-04-17'

export type CommitmentTone = 'current' | 'target' | 'pending'

export interface RangeCommitment {
  current: string
  currentNotes?: string
  target?: string
  targetDate?: string
  tone: CommitmentTone
}

export interface UptimeCommitment {
  monthlyTargetPercent: number
  currentPosture: string
  targetPosture: string
  targetDate: string
}

export interface DataStoreCommitment {
  store: string
  backup: string
  retention: string
  statusNote: string
}

export interface SecurityItem {
  label: string
  status: string
  tone: CommitmentTone
}

export interface DrDrill {
  /** First drill is scheduled; will populate from dr-log.md in future. */
  lastDrillDate: string | null
  nextDrillDate: string
  cadence: string
  logSource: string
}

export const uptime: UptimeCommitment = {
  monthlyTargetPercent: 99.5,
  currentPosture:
    'Single-replica Postgres and Redis; K8s reschedules pods on node failure.',
  targetPosture:
    '99.9% monthly uptime once Postgres streaming replication and Redis Sentinel ship.',
  targetDate: '2026-06-30',
}

export const rpo = {
  postgres: {
    current: '24 hours',
    currentNotes: 'Daily pg_dump → Cloudflare R2 (2 AM UTC CronJob).',
    target: '≤ 15 minutes',
    targetDate: '2026-06-30',
    tone: 'current' as const,
  } satisfies RangeCommitment,
  redis: {
    current: 'Hourly',
    currentNotes: 'RDB snapshots hourly; AOF pending HA deployment.',
    target: '≤ 1 minute',
    targetDate: '2026-06-30',
    tone: 'current' as const,
  } satisfies RangeCommitment,
}

export const rto = {
  singlePod: {
    current: '< 2 minutes',
    currentNotes: 'Automatic — K8s liveness/readiness reschedules pods.',
    tone: 'current' as const,
  } satisfies RangeCommitment,
  controlPlane: {
    current: '2 to 4 hours',
    currentNotes:
      'Documented in disaster-recovery runbook. Untested as of 2026-04-18; ' +
      'first drill scheduled the week of 2026-04-21.',
    target: '< 60 minutes',
    targetDate: '2026-09-30',
    tone: 'pending' as const,
  } satisfies RangeCommitment,
  fullRebuild: {
    current: '2 to 4 hours',
    currentNotes:
      'K8s manifests are GitOps-reconciled; Postgres restored from R2 backup.',
    tone: 'current' as const,
  } satisfies RangeCommitment,
}

export const drDrill: DrDrill = {
  lastDrillDate: null,
  nextDrillDate: '2026-04-21',
  cadence: 'Quarterly, beginning Q3 2026.',
  logSource: 'runbooks/disaster-recovery.md (internal)',
}

export const dataStores: DataStoreCommitment[] = [
  {
    store: 'PostgreSQL',
    backup: 'Daily pg_dump, encrypted, uploaded to Cloudflare R2.',
    retention: '30 days rolling.',
    statusNote: 'WAL archiving (≤ 15-min RPO) targeted Q2 2026.',
  },
  {
    store: 'Redis',
    backup: 'RDB snapshots every hour.',
    retention: '7 days rolling.',
    statusNote: 'AOF persistence pending HA deployment.',
  },
  {
    store: 'Cloudflare R2 (blob storage)',
    backup: 'Object versioning enabled; multi-region durability by Cloudflare.',
    retention: 'Version history 30 days; delete markers 90 days.',
    statusNote: 'Provider-managed durability (99.999999999% claim by Cloudflare).',
  },
  {
    store: 'Kubernetes manifests',
    backup: 'Git-versioned in madfam-org GitHub; ArgoCD-reconciled every 3 min.',
    retention: 'Full git history; ArgoCD keeps last 10 sync revisions.',
    statusNote: 'Rebuild from scratch via `kubectl apply -f infra/argocd/root-application.yaml`.',
  },
]

export const security: SecurityItem[] = [
  {
    label: 'Transport encryption',
    status: 'TLS 1.3 via Cloudflare Tunnel; no public node ports.',
    tone: 'current',
  },
  {
    label: 'Secrets at rest',
    status: 'Kubernetes Secrets with etcd encryption-at-rest.',
    tone: 'current',
  },
  {
    label: 'Secrets management',
    status: 'HashiCorp Vault integration landing Q2 2026 (RFC 0005).',
    tone: 'target',
  },
  {
    label: 'Container supply chain',
    status: 'Cosign signatures and CycloneDX SBOM published for every image.',
    tone: 'current',
  },
  {
    label: 'SOC 2 readiness',
    status: 'SOC 2 Type I target Q3 2026. Type II is not yet scoped.',
    tone: 'target',
  },
  {
    label: 'Audit log',
    status: 'Deploy-lifecycle and SSO session events logged; unified audit UI pending.',
    tone: 'pending',
  },
]

export const dataExport = {
  policy:
    'If you request an export of data we hold about you, we will deliver it within 72 hours of request acknowledgement.',
  mechanism:
    'Today: operator-assisted pg_dump plus tenant-scoped object export from R2. ' +
    'A self-serve tenant-export API is scoped for Phase 3.',
  contact: 'sla@madfam.io',
}

export const lockInPosture = {
  license: 'AGPL-3.0 (commercial license available for closed-source derivatives).',
  portability:
    'Everything runs on standard Kubernetes. No proprietary runtime or lock-in services. ' +
    'Your workloads, manifests, and data can move to any compliant K8s platform with ' +
    '`kubectl apply -f <your-manifests>`.',
  buildSystem: 'Paketo Buildpacks (CNB spec) or your own Dockerfile — no custom buildpack format.',
  source: 'Platform source is public at github.com/madfam-org/enclii.',
}

export const slaBreach = {
  contact: 'sla@madfam.io',
  responseWindow: 'Within 2 business days',
  note:
    'If you believe we have missed a commitment on this page, email the address above. ' +
    'We will confirm receipt, investigate, and share a post-mortem within 10 business days.',
}

/** Structured JSON payload served at /trust/commitments.json. */
export function buildCommitmentsJson() {
  return {
    last_reviewed: TRUST_LAST_REVIEWED,
    uptime: {
      monthly_target_percent_current: uptime.monthlyTargetPercent,
      monthly_target_percent_goal: 99.9,
      goal_target_date: uptime.targetDate,
    },
    rpo: {
      postgres: {
        current_minutes: 24 * 60,
        target_minutes: 15,
        target_date: '2026-06-30',
      },
      redis: {
        current_minutes: 60,
        target_minutes: 1,
        target_date: '2026-06-30',
      },
    },
    rto: {
      single_pod_failure_seconds: 120,
      control_plane_failure_minutes_current_max: 240,
      control_plane_failure_minutes_goal_max: 60,
      control_plane_target_date: '2026-09-30',
      full_rebuild_minutes_max: 240,
    },
    dr_drill: {
      cadence: drDrill.cadence,
      last_drill_date: drDrill.lastDrillDate,
      next_drill_date: drDrill.nextDrillDate,
    },
    data_stores: dataStores.map((d) => ({
      store: d.store,
      backup: d.backup,
      retention: d.retention,
      status_note: d.statusNote,
    })),
    security: security.map((s) => ({
      label: s.label,
      status: s.status,
      tone: s.tone,
    })),
    data_export: {
      window_hours_max: 72,
      contact: dataExport.contact,
    },
    lock_in_posture: {
      license: lockInPosture.license,
      source_url: 'https://github.com/madfam-org/enclii',
    },
    sla_breach: {
      contact: slaBreach.contact,
      response_window: slaBreach.responseWindow,
    },
  }
}
