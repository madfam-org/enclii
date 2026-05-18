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
  description: string;
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

const SERVICE_STATUSES = [
  "running",
  "pending",
  "failed",
  "deploying",
  "unknown",
] as const;

type ServiceStatus = (typeof SERVICE_STATUSES)[number];

const SERVICE_HEALTHS = ["healthy", "unhealthy", "unknown"] as const;
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
