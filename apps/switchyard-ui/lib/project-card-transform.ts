import { asRolloutState } from "@/components/dashboard/rollout-state";
import { resolveLatestDeployment } from "./project-deploy";
import type {
  CompactProject,
  CompactService,
} from "@/components/dashboard/project-card-compact";

export interface ApiProjectForCards {
  id: string;
  name: string;
  slug: string;
  description?: string;
  updated_at: string;
}

export interface ApiServiceForCards {
  id: string;
  name: string;
  git_repo: string;
  status: string;
  health: string;
  last_deployment: string;
  domain?: string;
  framework?: string;
  last_commit_message?: string;
  last_commit_branch?: string;
  desired_replicas?: number;
  ready_replicas?: number;
  auto_deploy_env?: string;
  current_image_uri?: string;
  rollout_state?: string;
  rollout_blocked_reason?: string;
}

export interface ApiProjectCardService {
  id: string;
  name: string;
  status: string;
  health: string;
  replicas?: string;
  environment?: string;
  domain?: string;
  current_image_uri?: string;
  rollout_state?: string;
  rollout_blocked_reason?: string;
  health_observed_at?: string;
  health_stale?: boolean;
}

export interface ApiProjectCardEvidence {
  service_rows: {
    status: "fresh" | "stale" | "empty" | string;
    count: number;
    healthy_count: number;
    stale_count: number;
    last_observed_at?: string;
    stale_after_seconds: number;
  };
  argo_application?: {
    name: string;
    sync_status: string;
    health_status: string;
    revision?: string;
    destination_namespace?: string;
    observed_at: string;
  };
  jobs?: {
    status: string;
    namespace_count: number;
    cron_job_count: number;
    failed_count: number;
    active_count: number;
    stuck_count: number;
    pending_count?: number;
    succeeded_count: number;
    last_observed_at: string;
    items?: {
      namespace: string;
      name: string;
      status: string;
      latest_job_name?: string;
      recent_failed_jobs?: number;
      active_jobs?: number;
      stuck_jobs?: number;
      succeeded_jobs?: number;
      last_schedule_time?: string;
      last_failure_time?: string;
    }[];
  };
}

export interface ApiProjectCardAggregate {
  id: string;
  name: string;
  slug: string;
  description?: string;
  updated_at: string;
  aggregate_status: CompactProject["aggregateStatus"];
  service_count: number;
  healthy_count: number;
  framework?: string;
  git_repo?: string;
  domain?: string;
  deploy_resolution: CompactProject["deployResolution"];
  last_deployment?: {
    timestamp: string;
    status: "success" | "failed" | "pending" | "building";
    branch: string;
    commit_message?: string;
  };
  evidence?: ApiProjectCardEvidence;
  services: ApiProjectCardService[];
}

const SERVICE_STATUSES = [
  "running",
  "pending",
  "failed",
  "deploying",
  "unknown",
] as const;

type ServiceStatus = (typeof SERVICE_STATUSES)[number];

const SERVICE_HEALTHS = ["healthy", "unhealthy", "unknown", "stale"] as const;
type ServiceHealth = (typeof SERVICE_HEALTHS)[number];

function normalizeServiceStatus(status: string): CompactService["status"] {
  return SERVICE_STATUSES.includes(status as ServiceStatus)
    ? (status as CompactService["status"])
    : "unknown";
}

function normalizeServiceHealth(health: string): CompactService["health"] {
  return SERVICE_HEALTHS.includes(health as ServiceHealth)
    ? (health as CompactService["health"])
    : "unknown";
}

export function apiServiceToCompactService(
  service: ApiServiceForCards,
): CompactService {
  return {
    id: service.id,
    name: service.name,
    status: normalizeServiceStatus(service.status),
    health: normalizeServiceHealth(service.health),
    replicas:
      service.ready_replicas !== undefined &&
      service.desired_replicas !== undefined
        ? `${service.ready_replicas}/${service.desired_replicas}`
        : undefined,
    environment: service.auto_deploy_env || undefined,
    currentImageUri: service.current_image_uri || undefined,
    domain: service.domain || undefined,
    rolloutState: asRolloutState(service.rollout_state),
    rolloutBlockedReason: service.rollout_blocked_reason || undefined,
  };
}

export function computeAggregateStatus(
  services: CompactService[],
): CompactProject["aggregateStatus"] {
  if (services.length === 0) return "unknown";

  const hasBlockedRollout = services.some((s) => s.rolloutState === "blocked");
  const hasFailedService = services.some((s) => s.status === "failed");
  if (hasBlockedRollout || hasFailedService) {
    return "failing";
  }

  const hasProgressingRollout = services.some(
    (s) => s.rolloutState === "progressing",
  );
  const hasInProgressStatus = services.some(
    (s) => s.status === "pending" || s.status === "deploying",
  );
  const hasUnhealthy = services.some((s) => s.health === "unhealthy");

  const allHealthyAndStable = services.every(
    (s) =>
      s.status === "running" &&
      s.health === "healthy" &&
      s.rolloutState !== "blocked" &&
      s.rolloutState !== "progressing",
  );

  if (
    allHealthyAndStable &&
    !hasInProgressStatus &&
    !hasProgressingRollout &&
    !hasUnhealthy
  ) {
    return "healthy";
  }

  return "degraded";
}

interface BuildCompactProjectParams {
  project: ApiProjectForCards;
  services: ApiServiceForCards[];
  servicesResolved: boolean;
}

export function buildCompactProject({
  project,
  services,
  servicesResolved,
}: BuildCompactProjectParams): CompactProject {
  const compactServices: CompactService[] = services.map(apiServiceToCompactService);
  const domain = compactServices.find((s) => s.domain)?.domain || undefined;
  const gitRepo = services.find((s) => s.git_repo)?.git_repo;
  const framework = services.find((s) => s.framework)?.framework;
  const healthyCount = compactServices.filter((s) => s.health === "healthy").length;
  const aggregateStatus = computeAggregateStatus(compactServices);

  const resolution = resolveLatestDeployment(
    services,
    servicesResolved,
  );

  return {
    id: project.id,
    name: project.name,
    slug: project.slug,
    description: project.description,
    framework,
    gitRepo,
    domain,
    lastDeployment: resolution.latest,
    deployResolution: resolution.status,
    serviceCount: services.length,
    healthyCount,
    services: compactServices,
    aggregateStatus,
    updatedAt: project.updated_at,
  };
}

export function projectCardAggregateToCompactProject(
  card: ApiProjectCardAggregate,
): CompactProject {
  return {
    id: card.id,
    name: card.name,
    slug: card.slug,
    description: card.description,
    framework: card.framework || undefined,
    gitRepo: card.git_repo || undefined,
    domain: card.domain || undefined,
    lastDeployment: card.last_deployment
      ? {
          timestamp: card.last_deployment.timestamp,
          status: card.last_deployment.status,
          branch: card.last_deployment.branch,
          commitMessage: card.last_deployment.commit_message || undefined,
        }
      : undefined,
    deployResolution: card.deploy_resolution,
    serviceCount: card.service_count,
    healthyCount: card.healthy_count,
    services: card.services.map((service) => ({
      id: service.id,
      name: service.name,
      status: normalizeServiceStatus(service.status),
      health: normalizeServiceHealth(service.health),
      replicas: service.replicas || undefined,
      environment: service.environment || undefined,
      currentImageUri: service.current_image_uri || undefined,
      domain: service.domain || undefined,
      rolloutState: asRolloutState(service.rollout_state),
      rolloutBlockedReason: service.rollout_blocked_reason || undefined,
      healthObservedAt: service.health_observed_at || undefined,
      healthStale: service.health_stale || service.health === "stale" || undefined,
    })),
    aggregateStatus: card.aggregate_status,
    evidence: card.evidence
      ? {
          serviceRows: {
            status: card.evidence.service_rows.status,
            count: card.evidence.service_rows.count,
            healthyCount: card.evidence.service_rows.healthy_count,
            staleCount: card.evidence.service_rows.stale_count,
            lastObservedAt: card.evidence.service_rows.last_observed_at,
            staleAfterSeconds: card.evidence.service_rows.stale_after_seconds,
          },
          argoApplication: card.evidence.argo_application
            ? {
                name: card.evidence.argo_application.name,
                syncStatus: card.evidence.argo_application.sync_status,
                healthStatus: card.evidence.argo_application.health_status,
                revision: card.evidence.argo_application.revision,
                destinationNamespace:
                  card.evidence.argo_application.destination_namespace,
                observedAt: card.evidence.argo_application.observed_at,
              }
            : undefined,
          jobs: card.evidence.jobs
            ? {
                status: card.evidence.jobs.status,
                namespaceCount: card.evidence.jobs.namespace_count,
                cronJobCount: card.evidence.jobs.cron_job_count,
                failedCount: card.evidence.jobs.failed_count,
                activeCount: card.evidence.jobs.active_count,
                stuckCount: card.evidence.jobs.stuck_count,
                pendingCount: card.evidence.jobs.pending_count,
                succeededCount: card.evidence.jobs.succeeded_count,
                lastObservedAt: card.evidence.jobs.last_observed_at,
                items: card.evidence.jobs.items?.map((item) => ({
                  namespace: item.namespace,
                  name: item.name,
                  status: item.status,
                  latestJobName: item.latest_job_name,
                  recentFailedJobs: item.recent_failed_jobs,
                  activeJobs: item.active_jobs,
                  stuckJobs: item.stuck_jobs,
                  succeededJobs: item.succeeded_jobs,
                  lastScheduleTime: item.last_schedule_time,
                  lastFailureTime: item.last_failure_time,
                })),
              }
            : undefined,
        }
      : undefined,
    updatedAt: card.updated_at,
  };
}
