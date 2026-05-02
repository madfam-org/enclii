/**
 * Unit tests for lib/project-deploy.ts — the shared helper that
 * reconciles "latest deployment" between the dashboard and /projects.
 *
 * The bug this helper fixes (PR-1 in app-fidelity-audit.md): two pages
 * had two duplicated reducers, which drifted; specifically /projects
 * was rendering "No recent deployments" for projects that the dashboard
 * showed a real timestamp for, because a transient fetch rejection was
 * being treated as "no services" instead of "unknown".
 */

import {
  resolveLatestDeployment,
  emptyDeploymentLabel,
} from "./project-deploy";

describe("resolveLatestDeployment", () => {
  it('returns "unknown" when the services fetch did not resolve', () => {
    const r = resolveLatestDeployment([], false);
    expect(r.status).toBe("unknown");
    expect(r.latest).toBeUndefined();
  });

  it('returns "no-deploys" when services resolved but list is empty', () => {
    const r = resolveLatestDeployment([], true);
    expect(r.status).toBe("no-deploys");
    expect(r.latest).toBeUndefined();
  });

  it('returns "no-deploys" when services exist but none have a last_deployment', () => {
    const r = resolveLatestDeployment(
      [{ status: "pending", last_deployment: "" }, { status: "running" }],
      true,
    );
    expect(r.status).toBe("no-deploys");
  });

  it('returns "deployed" with the most recent timestamp wins', () => {
    const r = resolveLatestDeployment(
      [
        {
          status: "running",
          last_deployment: "2026-05-01T08:00:00Z",
          last_commit_branch: "main",
          last_commit_message: "old commit",
        },
        {
          status: "running",
          last_deployment: "2026-05-02T08:00:00Z",
          last_commit_branch: "release",
          last_commit_message: "new commit",
        },
      ],
      true,
    );
    expect(r.status).toBe("deployed");
    expect(r.latest?.timestamp).toBe("2026-05-02T08:00:00Z");
    expect(r.latest?.branch).toBe("release");
    expect(r.latest?.commitMessage).toBe("new commit");
    expect(r.latest?.status).toBe("success");
  });

  it("maps service status to deployment status correctly", () => {
    const cases: Array<[string, string]> = [
      ["running", "success"],
      ["failed", "failed"],
      ["deploying", "building"],
      ["pending", "pending"],
      ["unknown", "pending"],
    ];
    for (const [svcStatus, expected] of cases) {
      const r = resolveLatestDeployment(
        [{ status: svcStatus, last_deployment: "2026-05-02T08:00:00Z" }],
        true,
      );
      expect(r.latest?.status).toBe(expected);
    }
  });

  it('falls back to branch "main" when last_commit_branch is missing', () => {
    const r = resolveLatestDeployment(
      [{ status: "running", last_deployment: "2026-05-02T08:00:00Z" }],
      true,
    );
    expect(r.latest?.branch).toBe("main");
  });
});

describe("emptyDeploymentLabel", () => {
  it('returns "No deployments yet" for the no-deploys state', () => {
    expect(emptyDeploymentLabel("no-deploys")).toBe("No deployments yet");
  });

  it('returns em-dash for the unknown state', () => {
    // The em-dash is intentional — see PR-1: do not claim a project has
    // zero deployments when we never successfully fetched services.
    expect(emptyDeploymentLabel("unknown")).toBe("—");
  });

  it('returns empty string for the deployed state (caller renders timestamp)', () => {
    expect(emptyDeploymentLabel("deployed")).toBe("");
  });
});
