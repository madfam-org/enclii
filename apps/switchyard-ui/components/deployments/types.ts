export interface Deployment {
  id: string;
  release_id: string;
  environment_id: string;
  replicas: number;
  status: 'pending' | 'deploying' | 'running' | 'failed' | 'stopped' | 'cancelled';
  health: 'healthy' | 'unhealthy' | 'unknown';
  created_at: string;
  updated_at: string;
  // P2.6: Heroku-style semantic version. Monotonic per service, never
  // reused even across rollbacks. Primary human-facing identifier.
  version_number?: number;
  // Git and PR information
  git_sha?: string;
  git_branch?: string;
  pr_number?: number;
  pr_title?: string;
  pr_url?: string;
  commit_message?: string;
  commit_author?: string;
  // Extended author information (GitOps Humanity)
  commit_author_username?: string;
  commit_author_email?: string;
  commit_author_avatar_url?: string;
  // Repository information for commit links
  repo_url?: string;
  // Enriched fields from API joins
  service_id?: string;
  service_name?: string;
}

/** Formats a deployment's semantic version as "v42", or returns null. */
export function deploymentVersionLabel(d: Pick<Deployment, 'version_number'>): string | null {
  if (d.version_number == null) return null;
  return `v${d.version_number}`;
}

export interface Release {
  id: string;
  service_id: string;
  version: string;
  image_uri: string;
  git_sha: string;
  status: 'building' | 'ready' | 'failed';
  error_message?: string;  // Error from build failure
  sbom?: string;
  sbom_format?: string;
  image_signature?: string;
  created_at: string;
  updated_at: string;
}

export interface DeploymentWithRelease extends Deployment {
  release?: Release;
}

export interface DeploymentsListResponse {
  service_id: string;
  deployments: Deployment[];
  count: number;
}

export interface RollbackResponse {
  message: string;
  rolled_back_to: Deployment;
  current_deployment: Deployment;
}

/**
 * Response from the instant (selector-flip) rollback endpoint.
 * POST /v1/services/{id}/rollback
 */
export interface InstantRollbackResponse {
  message: string;
  took_ms: number;
  scaled_up: boolean;
  from_deployment_id?: string;
  to_deployment_id: string;
  // P2.6: Heroku-style v-numbers for the from/to deployments. Optional
  // because historical rows may pre-date the backfill.
  from_version?: number;
  to_version?: number;
  ready_replicas: number;
  strategy: 'instant_selector_flip';
  namespace: string;
}

export interface InstantRollbackRequest {
  target_deployment_id: string;
  reason?: string;
  change_ticket_url?: string;
}

export interface ReleasesListResponse {
  releases: Release[];
}
