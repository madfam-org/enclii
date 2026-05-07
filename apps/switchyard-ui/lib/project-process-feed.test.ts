import {
  groupProcessesByService,
  highestSeverityProcess,
  processHref,
  processLiveState,
  processStatusLabel,
  serviceSummariesById,
  topProjectProcesses,
  type ProjectProcess,
  type ProjectProcessSummary,
} from "./project-process-feed";

const baseProcess = (overrides: Partial<ProjectProcess>): ProjectProcess => ({
  id: "p",
  correlation_id: "c",
  project_id: "project-1",
  project_slug: "project",
  kind: "deploy",
  status: "running",
  source: "switchyard",
  updated_at: "2026-05-06T12:00:00Z",
  ...overrides,
});

describe("processLiveState", () => {
  it("prioritizes blocked over failed and running", () => {
    expect(
      processLiveState({
        project_id: "p",
        project_slug: "project",
        active_count: 3,
        failed_count: 2,
        blocked_count: 1,
        processes: [],
        services: [],
      }),
    ).toBe("blocked");
  });

  it("returns running when active work exists", () => {
    expect(
      processLiveState({
        project_id: "p",
        project_slug: "project",
        active_count: 1,
        failed_count: 0,
        blocked_count: 0,
        processes: [],
        services: [],
      }),
    ).toBe("running");
  });
});

describe("highestSeverityProcess", () => {
  it("uses severity before recency", () => {
    const process = highestSeverityProcess([
      baseProcess({
        id: "running-new",
        status: "running",
        updated_at: "2026-05-06T12:10:00Z",
      }),
      baseProcess({
        id: "failed-old",
        status: "failed",
        updated_at: "2026-05-06T12:00:00Z",
      }),
    ]);

    expect(process?.id).toBe("failed-old");
  });
});

describe("topProjectProcesses", () => {
  it("returns highest severity processes first", () => {
    const summary: ProjectProcessSummary = {
      project_id: "p",
      project_slug: "project",
      active_count: 1,
      failed_count: 1,
      blocked_count: 1,
      processes: [
        baseProcess({ id: "running", status: "running" }),
        baseProcess({ id: "blocked", status: "blocked" }),
        baseProcess({ id: "failed", status: "failed" }),
      ],
      services: [],
    };

    expect(topProjectProcesses(summary, 2).map((p) => p.id)).toEqual([
      "blocked",
      "failed",
    ]);
  });
});

describe("serviceSummariesById", () => {
  it("indexes service summaries by service id", () => {
    const indexed = serviceSummariesById({
      project_id: "p",
      project_slug: "project",
      active_count: 0,
      failed_count: 0,
      blocked_count: 0,
      processes: [],
      services: [
        {
          service_id: "svc-1",
          service_name: "api",
          active_count: 1,
          failed_count: 0,
          blocked_count: 0,
        },
      ],
    });

    expect(indexed["svc-1"].service_name).toBe("api");
  });
});

describe("processHref", () => {
  it("prefers explicit external run links before internal fallbacks", () => {
    expect(
      processHref(
        "project",
        baseProcess({
          links: {
            github_run: "https://github.com/madfam-org/api/actions/runs/1",
            deployment: "/deployments/deploy-1",
            logs: "/projects/project/services/svc/logs",
          },
        }),
      ),
    ).toBe("https://github.com/madfam-org/api/actions/runs/1");
  });

  it("falls back to the project deployments view", () => {
    expect(processHref("project", baseProcess({ links: undefined }))).toBe(
      "/projects/project/deployments",
    );
  });
});

describe("groupProcessesByService", () => {
  it("groups drawer rows by service identity instead of domain", () => {
    const groups = groupProcessesByService(
      [
        baseProcess({
          id: "api-run",
          service_id: "svc-api",
          service_name: "api",
          status: "running",
        }),
        baseProcess({
          id: "api-failed",
          service_id: "svc-api",
          service_name: "api",
          status: "failed",
          updated_at: "2026-05-06T12:10:00Z",
        }),
        baseProcess({
          id: "web-run",
          service_id: "svc-web",
          service_name: "web",
          status: "running",
        }),
      ],
      "Project",
    );

    expect(groups).toHaveLength(2);
    expect(groups[0].service_name).toBe("api");
    expect(groups[0].processes.map((process) => process.id)).toEqual([
      "api-failed",
      "api-run",
    ]);
    expect(groups[1].service_name).toBe("web");
  });
});

describe("processStatusLabel", () => {
  it("labels statuses for compact UI chips", () => {
    expect(processStatusLabel("blocked")).toBe("Blocked");
    expect(processStatusLabel("running")).toBe("Running");
  });
});
