import { render, screen } from "@testing-library/react";
import { HealthTab } from "./HealthTab";
import type { ServiceHealthResponse } from "../observability-types";

function health(status: string, observation_reason?: string): ServiceHealthResponse {
  return { healthy_count: 0, degraded_count: 1, unhealthy_count: 0, timestamp: "2026-09-23T00:00:00Z", services: [{ service_id: "fixture", service_name: "Example", project_slug: "example", status, observation_reason, uptime: 0, response_time_ms: 0, error_rate: 0, last_checked: "2026-09-23T00:00:00Z", pod_count: 0, ready_pods: 0 }] };
}

test("unknown runtime does not claim a zero-pod outage or zero uptime", () => {
  render(<HealthTab serviceHealth={health("unknown", "workload_not_found")} />);
  expect(screen.getByText("Runtime health unavailable: workload not found.")).toBeInTheDocument();
  expect(screen.getAllByText("Unavailable")).toHaveLength(2);
  expect(screen.queryByText("0/0")).not.toBeInTheDocument();
  expect(screen.queryByText("0.0%")).not.toBeInTheDocument();
});

test("an observed zero-pod deployment remains visibly unhealthy", () => {
  render(<HealthTab serviceHealth={health("unhealthy")} />);
  expect(screen.getByText("0/0")).toBeInTheDocument();
  expect(screen.queryByText("Unavailable")).not.toBeInTheDocument();
});
