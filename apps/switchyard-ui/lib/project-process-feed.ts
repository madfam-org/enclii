export type ProjectProcessKind =
  | "git_push"
  | "ci"
  | "build"
  | "image"
  | "digest"
  | "gitops_sync"
  | "deploy"
  | "rollback"
  | "rollout"
  | "preview"
  | "domain"
  | "secret"
  | "addon"
  | "database"
  | "job"
  | "operator";

export type ProjectProcessStatus =
  | "queued"
  | "running"
  | "waiting"
  | "succeeded"
  | "failed"
  | "blocked"
  | "cancelled"
  | "unknown";

export type ProjectLiveState =
  | "idle"
  | "running"
  | "failed"
  | "blocked"
  | "unknown";

export interface ProjectProcess {
  id: string;
  correlation_id: string;
  project_id: string;
  project_slug: string;
  service_id?: string;
  service_name?: string;
  kind: ProjectProcessKind;
  status: ProjectProcessStatus;
  phase?: string;
  message?: string;
  branch?: string;
  commit_sha?: string;
  environment?: string;
  progress?: number;
  source: string;
  links?: {
    logs?: string;
    github_run?: string;
    deployment?: string;
    lifecycle?: string;
    remediation?: string;
  };
  started_at?: string;
  updated_at: string;
  completed_at?: string;
}

export interface ServiceProcessSummary {
  service_id: string;
  service_name?: string;
  active_count: number;
  failed_count: number;
  blocked_count: number;
  latest?: ProjectProcess;
}

export interface ProjectProcessSummary {
  project_id: string;
  project_slug: string;
  active_count: number;
  failed_count: number;
  blocked_count: number;
  latest?: ProjectProcess;
  processes: ProjectProcess[];
  services: ServiceProcessSummary[];
}

export interface ProjectProcessSummaryResponse {
  count: number;
  summaries: ProjectProcessSummary[];
}

export interface ProjectProcessTimelineResponse {
  count: number;
  project_id: string;
  slug: string;
  processes: ProjectProcess[];
  summary: ProjectProcessSummary;
}

export interface ProjectProcessGroup {
  key: string;
  service_id?: string;
  service_name: string;
  processes: ProjectProcess[];
}

const statusSeverity: Record<ProjectProcessStatus, number> = {
  blocked: 70,
  failed: 60,
  running: 50,
  waiting: 40,
  queued: 30,
  succeeded: 20,
  unknown: 10,
  cancelled: 0,
};

export function processSeverity(status: ProjectProcessStatus): number {
  return statusSeverity[status] ?? statusSeverity.unknown;
}

export function processLiveState(
  summary: ProjectProcessSummary | undefined,
): ProjectLiveState {
  if (!summary) return "idle";
  if (summary.blocked_count > 0) return "blocked";
  if (summary.failed_count > 0) return "failed";
  if (summary.active_count > 0) return "running";
  if (summary.latest) return "idle";
  return "idle";
}

export function highestSeverityProcess(
  processes: ProjectProcess[] | undefined,
): ProjectProcess | undefined {
  if (!processes || processes.length === 0) return undefined;
  return [...processes].sort((a, b) => {
    const severityDiff = processSeverity(b.status) - processSeverity(a.status);
    if (severityDiff !== 0) return severityDiff;
    return (
      new Date(b.updated_at).getTime() - new Date(a.updated_at).getTime()
    );
  })[0];
}

export function topProjectProcesses(
  summary: ProjectProcessSummary | undefined,
  limit = 3,
): ProjectProcess[] {
  if (!summary || !summary.processes) return [];
  return sortProcessesForDisplay(summary.processes).slice(0, limit);
}

export function serviceSummariesById(
  summary: ProjectProcessSummary | undefined,
): Record<string, ServiceProcessSummary> {
  const indexed: Record<string, ServiceProcessSummary> = {};
  for (const service of summary?.services || []) {
    indexed[service.service_id] = service;
  }
  return indexed;
}

export function processLabel(process: ProjectProcess): string {
  const kind = process.kind.replace(/_/g, " ");
  const phase = process.phase ? ` · ${process.phase.replace(/_/g, " ")}` : "";
  return `${kind}${phase}`;
}

export function processStatusLabel(status: ProjectProcessStatus): string {
  switch (status) {
    case "queued":
      return "Queued";
    case "running":
      return "Running";
    case "waiting":
      return "Waiting";
    case "succeeded":
      return "Succeeded";
    case "failed":
      return "Failed";
    case "blocked":
      return "Blocked";
    case "cancelled":
      return "Cancelled";
    default:
      return "Unknown";
  }
}

export function processSummaryTitle(process: ProjectProcess): string {
  const service = process.service_name ? `${process.service_name}: ` : "";
  const message = process.message ? ` — ${process.message}` : "";
  const branch = process.branch ? ` (${process.branch})` : "";
  return `${service}${processStatusLabel(process.status)} ${processLabel(process)}${branch}${message}`;
}

export function processHref(
  projectSlug: string,
  process: ProjectProcess,
): string {
  return (
    process.links?.github_run ||
    process.links?.deployment ||
    process.links?.logs ||
    process.links?.lifecycle ||
    `/projects/${projectSlug}/deployments`
  );
}

export function groupProcessesByService(
  processes: ProjectProcess[] | undefined,
  fallbackServiceName = "Project",
): ProjectProcessGroup[] {
  if (!processes || processes.length === 0) return [];

  const groups = new Map<string, ProjectProcessGroup>();
  for (const process of sortProcessesForDisplay(processes)) {
    const key = process.service_id || process.service_name || "__project__";
    let group = groups.get(key);
    if (!group) {
      group = {
        key,
        service_id: process.service_id,
        service_name: process.service_name || fallbackServiceName,
        processes: [],
      };
      groups.set(key, group);
    }
    group.processes.push(process);
  }

  return [...groups.values()];
}

function sortProcessesForDisplay(processes: ProjectProcess[]): ProjectProcess[] {
  return [...processes].sort((a, b) => {
    const severityDiff = processSeverity(b.status) - processSeverity(a.status);
    if (severityDiff !== 0) return severityDiff;
    return new Date(b.updated_at).getTime() - new Date(a.updated_at).getTime();
  });
}
