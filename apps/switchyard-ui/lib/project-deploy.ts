/**
 * Single source of truth for resolving a project's "latest deployment"
 * displayed on the dashboard (`/`) and the projects index (`/projects`).
 *
 * Before this helper, the two pages each carried a duplicated 30-line
 * inline reduction of `services[]` -> `lastDeployment`. They diverged
 * subtly (audit finding PR-1, app-fidelity-audit.md): the dashboard was
 * showing "1d ago" for projects whose /projects card said "No recent
 * deployments" — same fetch, same component, but transient differences
 * in fetch state (one Promise.allSettled rejection vs one fulfillment)
 * made the empty-state copy wrong.
 *
 * Tri-state result (`status` field):
 *   - "deployed":   We have a real `last_deployment` timestamp. Render it.
 *   - "no-deploys": Services loaded successfully but none have ever been
 *                   deployed. Render "No deployments yet".
 *   - "unknown":    We couldn't resolve services for this project (fetch
 *                   rejected). Render "—" — *don't* claim it has zero
 *                   deployments when we never asked the API.
 *
 * Callers are responsible for translating `status` to UI copy. The empty
 * states on both /projects and /dashboard route through this helper.
 */

export interface DeployServiceLike {
  status?: string;
  health?: string;
  last_deployment?: string;
  last_commit_branch?: string;
  last_commit_message?: string;
}

export interface LatestDeployment {
  timestamp: string;
  status: "success" | "failed" | "pending" | "building";
  branch: string;
  commitMessage?: string;
}

export type ResolveStatus = "deployed" | "no-deploys" | "unknown";

export interface DeployResolution {
  status: ResolveStatus;
  /** Populated only when `status === "deployed"`. */
  latest?: LatestDeployment;
}

/**
 * Resolve the most recent deployment from a service list.
 *
 * @param services  Services for the project, in any order.
 * @param resolved  True when the upstream `/v1/projects/:slug/services`
 *                  call fulfilled (even if it returned an empty array).
 *                  False when it rejected — used to disambiguate
 *                  "no deployments yet" from "we don't know".
 */
export function resolveLatestDeployment(
  services: DeployServiceLike[] | undefined | null,
  resolved: boolean,
): DeployResolution {
  if (!resolved) {
    return { status: "unknown" };
  }

  const safe = services ?? [];
  const withDeploy = safe.filter((s) => !!s.last_deployment);
  if (withDeploy.length === 0) {
    return { status: "no-deploys" };
  }

  const latestService = [...withDeploy].sort(
    (a, b) =>
      new Date(b.last_deployment as string).getTime() -
      new Date(a.last_deployment as string).getTime(),
  )[0];

  return {
    status: "deployed",
    latest: {
      timestamp: latestService.last_deployment as string,
      status:
        latestService.status === "running"
          ? "success"
          : latestService.status === "failed"
            ? "failed"
            : latestService.status === "deploying"
              ? "building"
              : "pending",
      branch: latestService.last_commit_branch || "main",
      commitMessage: latestService.last_commit_message || undefined,
    },
  };
}

/**
 * Empty-state copy for the project card's deployment line. Kept in sync
 * with the resolver so card consumers (ProjectCardCompact) don't drift.
 */
export function emptyDeploymentLabel(status: ResolveStatus): string {
  switch (status) {
    case "no-deploys":
      return "No deployments yet";
    case "unknown":
      return "—"; // em dash — "we don't know yet"
    case "deployed":
      // Should never appear: caller should render the timestamp instead.
      return "";
  }
}
