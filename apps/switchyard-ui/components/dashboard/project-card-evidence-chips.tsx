import { AlertTriangle, Clock } from "lucide-react";
import type {
  CompactService,
  ProjectCardEvidence,
} from "./project-card-compact";

function argoEvidenceIsDegraded(
  argo: ProjectCardEvidence["argoApplication"],
): boolean {
  if (!argo) return false;
  return argo.syncStatus !== "Synced" || argo.healthStatus !== "Healthy";
}

interface ProjectCardEvidenceChipsProps {
  evidence?: ProjectCardEvidence;
  services: CompactService[];
}

export function ProjectCardEvidenceChips({
  evidence,
  services,
}: ProjectCardEvidenceChipsProps) {
  const argoEvidence = evidence?.argoApplication;
  const argoDegraded = argoEvidenceIsDegraded(argoEvidence);
  const staleHealthCount =
    evidence?.serviceRows.staleCount ??
    services.filter((service) => service.healthStale || service.health === "stale")
      .length;
  const argoHealthy = Boolean(argoEvidence) && !argoDegraded;
  const visibleStaleHealthCount = argoHealthy ? 0 : staleHealthCount;
  const jobFailureCount = evidence?.jobs?.failedCount ?? 0;
  const jobStuckCount = evidence?.jobs?.stuckCount ?? 0;
  const hasJobIssues = jobFailureCount > 0 || jobStuckCount > 0;

  if (!argoDegraded && visibleStaleHealthCount === 0 && !hasJobIssues) return null;

  return (
    <div className="relative z-10 mt-2 flex min-w-0 flex-wrap gap-1">
      {argoDegraded && argoEvidence && (
        <span
          className="border-status-error/35 bg-status-error/10 text-status-error inline-flex max-w-full items-center gap-1 rounded-full border px-1.5 py-0.5 text-[10px] font-medium"
          title={`Argo ${argoEvidence.syncStatus}/${argoEvidence.healthStatus} at ${new Date(argoEvidence.observedAt).toLocaleString()}`}
        >
          <AlertTriangle className="h-3 w-3 shrink-0" aria-hidden="true" />
          <span className="truncate">
            Argo {argoEvidence.healthStatus || argoEvidence.syncStatus}
          </span>
        </span>
      )}
      {visibleStaleHealthCount > 0 && (
        <span
          className="border-status-warning/35 bg-status-warning/10 text-status-warning inline-flex items-center gap-1 rounded-full border px-1.5 py-0.5 text-[10px] font-medium"
          title={
            evidence?.serviceRows.lastObservedAt
              ? `Last service health check ${new Date(evidence.serviceRows.lastObservedAt).toLocaleString()}`
              : "Service health evidence is stale"
          }
        >
          <Clock className="h-3 w-3 shrink-0" aria-hidden="true" />
          <span>{visibleStaleHealthCount} stale</span>
        </span>
      )}
      {hasJobIssues && evidence?.jobs && (
        <span
          className="border-status-error/35 bg-status-error/10 text-status-error inline-flex max-w-full items-center gap-1 rounded-full border px-1.5 py-0.5 text-[10px] font-medium"
          title={`CronJobs ${evidence.jobs.status}: ${evidence.jobs.failedCount} recent failed, ${evidence.jobs.stuckCount} stuck`}
        >
          <AlertTriangle className="h-3 w-3 shrink-0" aria-hidden="true" />
          <span className="truncate">
            {jobFailureCount > 0
              ? `${jobFailureCount} job failed`
              : `${jobStuckCount} job stuck`}
          </span>
        </span>
      )}
    </div>
  );
}
