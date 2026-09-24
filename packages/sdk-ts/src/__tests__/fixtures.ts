/**
 * Response bodies shaped like the Switchyard API's Go structs, field for
 * field (JSON tags from packages/sdk-go/pkg/types and the gin.H literals in
 * apps/switchyard-api/internal/api). Contract tests feed these to the stub
 * fetch so a drift between SDK parsing and the handler output fails a test.
 */

/** types.Deployment */
export function goDeployment(id: string, overrides: Record<string, unknown> = {}) {
  return {
    id,
    release_id: 'rel-1',
    environment_id: 'env-1',
    deploy_order: 0,
    replicas: 2,
    status: 'running',
    health: 'healthy',
    service_id: 'svc-1',
    version_number: 7,
    created_at: '2026-09-20T10:00:00Z',
    updated_at: '2026-09-20T10:05:00Z',
    ...overrides,
  };
}

/** types.Release */
export function goRelease(id: string) {
  return {
    id,
    service_id: 'svc-1',
    version: 'v1.2.3',
    image_uri: 'ghcr.io/acme/api@sha256:abc',
    git_sha: 'abc123',
    status: 'ready',
    created_at: '2026-09-20T09:00:00Z',
    updated_at: '2026-09-20T09:10:00Z',
  };
}

/** types.EnvironmentVariableResponse (toEnvVarResponse masks secrets). */
export function goEnvVar(id: string, key: string, isSecret: boolean) {
  return {
    id,
    service_id: 'svc-1',
    key,
    value: isSecret ? '••••••••' : 'plain',
    is_secret: isSecret,
    created_at: '2026-09-20T09:00:00Z',
    updated_at: '2026-09-20T09:00:00Z',
  };
}

/** types.CronJob */
export function goCronJob(id: string) {
  return {
    id,
    project_id: 'proj-1',
    service_id: 'svc-1',
    name: 'nightly',
    schedule: '0 2 * * *',
    command: 'npm run sync',
    timeout: 3600,
    retries: 0,
    suspended: false,
    concurrency: 'forbid',
    created_at: '2026-09-20T09:00:00Z',
    updated_at: '2026-09-20T09:00:00Z',
  };
}

/** types.OneOffJob */
export function goOneOffJob(id: string, overrides: Record<string, unknown> = {}) {
  return {
    id,
    project_id: 'proj-1',
    service_id: 'svc-1',
    name: 'migrate',
    command: 'npm run migrate',
    timeout: 3600,
    status: 'pending',
    created_at: '2026-09-20T09:00:00Z',
    ...overrides,
  };
}

/** types.AuditLog */
export function goAuditLog(id: string) {
  return {
    id,
    timestamp: '2026-09-20T09:00:00Z',
    actor_email: 'dev@example.com',
    actor_role: 'developer',
    action: 'deploy',
    resource_type: 'service',
    resource_id: 'svc-1',
    resource_name: 'api',
    ip_address: '203.0.113.9',
    user_agent: 'enclii-cli',
    outcome: 'success',
    context: null,
  };
}
