import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import {
  ProjectCardCompact,
  type CompactProject,
} from "./project-card-compact";
import type { ProjectProcess } from "@/lib/project-process-feed";

const mockApiRequest = jest.fn();

jest.mock("@/lib/api", () => ({
  apiRequest: (...args: unknown[]) => mockApiRequest(...args),
}));

const baseProcess = (overrides: Partial<ProjectProcess>): ProjectProcess => ({
  id: "process-1",
  correlation_id: "correlation-1",
  project_id: "project-1",
  project_slug: "orchard",
  kind: "deploy",
  status: "running",
  source: "switchyard",
  updated_at: "2026-05-06T12:00:00Z",
  ...overrides,
});

function projectWithDomains(): CompactProject {
  const process = baseProcess({
    id: "api-deploy",
    service_id: "svc-api",
    service_name: "api",
    message: "Deploying api",
  });

  return {
    id: "project-1",
    name: "Orchard",
    slug: "orchard",
    framework: "nextjs",
    serviceCount: 2,
    healthyCount: 2,
    aggregateStatus: "healthy",
    processSummary: {
      project_id: "project-1",
      project_slug: "orchard",
      active_count: 1,
      failed_count: 0,
      blocked_count: 0,
      latest: process,
      processes: [process],
      services: [
        {
          service_id: "svc-api",
          service_name: "api",
          active_count: 1,
          failed_count: 0,
          blocked_count: 0,
          latest: process,
        },
      ],
    },
    liveState: "running",
    services: [
      {
        id: "svc-api",
        name: "api",
        status: "running",
        health: "healthy",
        environment: "production",
        domain: "api.example.test",
        lastProcess: process,
      },
      {
        id: "svc-web",
        name: "web",
        status: "running",
        health: "healthy",
        environment: "staging",
        domain: "web.example.test",
      },
    ],
  };
}

describe("ProjectCardCompact process drawer", () => {
  beforeEach(() => {
    mockApiRequest.mockReset();
    mockApiRequest.mockResolvedValue({
      count: 2,
      project_id: "project-1",
      slug: "orchard",
      processes: [
        baseProcess({
          id: "api-deploy",
          service_id: "svc-api",
          service_name: "api",
          message: "Deploying api",
        }),
        baseProcess({
          id: "web-deploy",
          service_id: "svc-web",
          service_name: "web",
          status: "waiting",
          message: "Waiting for staging rollout",
        }),
      ],
      summary: {
        project_id: "project-1",
        project_slug: "orchard",
        active_count: 2,
        failed_count: 0,
        blocked_count: 0,
        processes: [],
        services: [],
      },
    });
  });

  it("opens the process sheet without removing per-service domain rows", async () => {
    render(<ProjectCardCompact project={projectWithDomains()} />);

    expect(screen.getByText("api.example.test")).toBeInTheDocument();
    expect(screen.getByText("web.example.test")).toBeInTheDocument();

    fireEvent.click(
      screen.getByRole("button", {
        name: /Open process feed for Orchard/i,
      }),
    );

    const dialog = await screen.findByRole("dialog", {
      name: /Orchard process feed/i,
    });

    await waitFor(() =>
      expect(mockApiRequest).toHaveBeenCalledWith(
        "/v1/projects/orchard/processes?limit=50&active_only=false",
        expect.objectContaining({ method: "GET" }),
      ),
    );
    expect(within(dialog).getByText("api")).toBeInTheDocument();
    expect(within(dialog).getByText("web")).toBeInTheDocument();
    expect(screen.getByText("api.example.test")).toBeInTheDocument();
    expect(screen.getByText("web.example.test")).toBeInTheDocument();
  });

  it("renders CronJob failure evidence as a card chip", () => {
    const project = {
      ...projectWithDomains(),
      aggregateStatus: "failing" as const,
      evidence: {
        serviceRows: {
          status: "fresh",
          count: 2,
          healthyCount: 2,
          staleCount: 0,
          staleAfterSeconds: 600,
        },
        jobs: {
          status: "failing",
          namespaceCount: 1,
          cronJobCount: 1,
          failedCount: 2,
          activeCount: 0,
          stuckCount: 0,
          succeededCount: 0,
          lastObservedAt: "2026-05-18T20:00:00Z",
        },
      },
    };

    render(<ProjectCardCompact project={project} />);

    expect(screen.getByText("2 job failed")).toBeInTheDocument();
  });
});
