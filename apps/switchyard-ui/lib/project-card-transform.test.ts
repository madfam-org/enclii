import {
  apiServiceToCompactService,
  buildCompactProject,
  computeAggregateStatus,
} from "./project-card-transform";

const baseProject = {
  id: "project-1",
  name: "Orchard",
  slug: "orchard",
  description: "Fresh cards for every rollout",
  updated_at: "2026-05-10T12:00:00Z",
};

describe("apiServiceToCompactService", () => {
  it("normalizes unknown status and health values and carries rollout metadata", () => {
    const compact = apiServiceToCompactService({
      id: "svc-1",
      name: "api",
      git_repo: "org/repo",
      status: "not-real",
      health: "also-not-real",
      last_deployment: "2026-05-10T11:00:00Z",
      rollout_state: "blocked",
      rollout_blocked_reason: "crash_loop_back_off",
      desired_replicas: 2,
      ready_replicas: 1,
      auto_deploy_env: "production",
      current_image_uri: "ghcr.io/acme/api:1.2.3",
    });

    expect(compact.status).toBe("unknown");
    expect(compact.health).toBe("unknown");
    expect(compact.rolloutState).toBe("blocked");
    expect(compact.rolloutBlockedReason).toBe("crash_loop_back_off");
    expect(compact.replicas).toBe("1/2");
    expect(compact.environment).toBe("production");
    expect(compact.currentImageUri).toBe("ghcr.io/acme/api:1.2.3");
  });
});

describe("computeAggregateStatus", () => {
  it("returns healthy when every service is stable and rollout-ok", () => {
    const status = computeAggregateStatus([
      {
        id: "svc-1",
        name: "api",
        status: "running",
        health: "healthy",
        rolloutState: "ok",
      },
      {
        id: "svc-2",
        name: "web",
        status: "running",
        health: "healthy",
      },
    ]);
    expect(status).toBe("healthy");
  });

  it("returns degraded when a rollout is still progressing", () => {
    const status = computeAggregateStatus([
      {
        id: "svc-1",
        name: "api",
        status: "running",
        health: "healthy",
        rolloutState: "progressing",
      },
    ]);
    expect(status).toBe("degraded");
  });

  it("returns failing when a rollout is blocked even if service status is running", () => {
    const status = computeAggregateStatus([
      {
        id: "svc-1",
        name: "api",
        status: "running",
        health: "healthy",
        rolloutState: "blocked",
      },
    ]);
    expect(status).toBe("failing");
  });

  it("returns failing when any service is failed", () => {
    const status = computeAggregateStatus([
      {
        id: "svc-1",
        name: "api",
        status: "failed",
        health: "unhealthy",
      },
    ]);
    expect(status).toBe("failing");
  });

  it("returns unknown when no services exist", () => {
    expect(computeAggregateStatus([])).toBe("unknown");
  });
});

describe("buildCompactProject", () => {
  it("maps api project + services into dashboard-ready compact data", () => {
    const compact = buildCompactProject({
      project: baseProject,
      services: [
        {
          id: "svc-1",
          name: "api",
          git_repo: "org/repo",
          status: "running",
          health: "healthy",
          last_deployment: "2026-05-12T12:00:00Z",
          last_commit_message: "feat: cutover to rollout-safe image",
          last_commit_branch: "main",
          domain: "api.example.com",
          framework: "nextjs",
          rollout_state: "ok",
          auto_deploy_env: "production",
          desired_replicas: 2,
          ready_replicas: 2,
        },
      ],
      servicesResolved: true,
    });

    expect(compact.gitRepo).toBe("org/repo");
    expect(compact.domain).toBe("api.example.com");
    expect(compact.framework).toBe("nextjs");
    expect(compact.serviceCount).toBe(1);
    expect(compact.healthyCount).toBe(1);
    expect(compact.aggregateStatus).toBe("healthy");
    expect(compact.deployResolution).toBe("deployed");
    expect(compact.lastDeployment?.status).toBe("success");
    expect(compact.lastDeployment?.timestamp).toBe(
      "2026-05-12T12:00:00Z",
    );
  });

  it("returns unknown deploy resolution when services were not fetched", () => {
    const compact = buildCompactProject({
      project: baseProject,
      services: [],
      servicesResolved: false,
    });

    expect(compact.deployResolution).toBe("unknown");
    expect(compact.lastDeployment).toBeUndefined();
    expect(compact.aggregateStatus).toBe("unknown");
    expect(compact.services).toHaveLength(0);
  });

  it("leaves framework empty when backend service facts do not include one", () => {
    const compact = buildCompactProject({
      project: baseProject,
      services: [
        {
          id: "svc-1",
          name: "product-web",
          git_repo: "https://github.com/example/plain-product",
          status: "running",
          health: "healthy",
          last_deployment: "",
        },
      ],
      servicesResolved: true,
    });

    expect(compact.framework).toBeUndefined();
  });
});
