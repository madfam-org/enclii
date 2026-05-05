/**
 * Tests for `components/dashboard/sidebar-alerts.tsx`.
 *
 * Validates the Phase 1 contract: every alert renders inside an anchor
 * pointing at its routed href, the chevron affordance is present, and
 * a locally-muted alert is dimmed with a "muted until …" badge.
 *
 * Network + scope context are stubbed so the test focuses on the row
 * structure, not the polling lifecycle.
 */

import { act, render, screen, waitFor, within } from "@testing-library/react";
import { SidebarAlerts } from "./sidebar-alerts";
import { MUTED_ALERTS_STORAGE_KEY } from "@/lib/muted-alerts";

// Stub the API layer so the component renders deterministically.
const mockApiGet = jest.fn();
jest.mock("@/lib/api", () => ({
  apiGet: (...args: unknown[]) => mockApiGet(...args),
}));

// Don't actually start the 60s poller during tests.
jest.mock("@/hooks/use-polling", () => ({
  usePolling: jest.fn(),
}));

// Master-admin-scope predicate isn't important for these assertions —
// we only need a stable boolean and to avoid mounting AuthContext.
jest.mock("@/contexts/ScopeContext", () => ({
  useIsAdminScope: () => false,
}));

const SVC = "550e8400-e29b-41d4-a716-446655440000";

const SAMPLE_ALERTS = {
  alerts: [
    {
      id: "alert-error-rate-high",
      name: "High Error Rate",
      severity: "critical" as const,
      fired_at: new Date().toISOString(),
    },
    {
      id: `alert-service-unhealthy-${SVC}`,
      name: "Service Unhealthy",
      severity: "warning" as const,
      fired_at: new Date().toISOString(),
      service_id: SVC,
    },
  ],
  total: 2,
};

beforeEach(() => {
  window.localStorage.clear();
  mockApiGet.mockReset();
  mockApiGet.mockResolvedValue(SAMPLE_ALERTS);
});

async function renderAndWait() {
  const utils = render(<SidebarAlerts />);
  await waitFor(() => expect(mockApiGet).toHaveBeenCalled());
  // Wait for the "loading" state to clear.
  await waitFor(() =>
    expect(screen.queryAllByTestId("sidebar-alert-link").length).toBeGreaterThan(0),
  );
  return utils;
}

describe("SidebarAlerts — clickability", () => {
  it("wraps each alert in a Link with the correct href", async () => {
    await renderAndWait();
    const rows = screen.getAllByTestId("sidebar-alert-link");
    expect(rows).toHaveLength(2);
    // Global error rate → /observability
    expect(rows[0]).toHaveAttribute("href", "/observability");
    // Service-scoped → /services/<uuid>
    expect(rows[1]).toHaveAttribute("href", `/services/${SVC}`);
  });

  it("renders an accessible label that names the action and the alert", async () => {
    await renderAndWait();
    expect(
      screen.getByRole("link", { name: /Open observability: High Error Rate/i }),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("link", { name: /Open service: Service Unhealthy/i }),
    ).toBeInTheDocument();
  });

  it("renders a chevron affordance per row", async () => {
    await renderAndWait();
    const chevrons = screen.getAllByTestId("alert-chevron");
    expect(chevrons).toHaveLength(2);
  });
});

describe("SidebarAlerts — muted alerts", () => {
  it("dims a row whose alert ID is in the local mute store + shows the badge", async () => {
    // Pre-populate localStorage so the mute is hydrated on mount.
    const mutedUntil = Date.now() + 60_000;
    window.localStorage.setItem(
      MUTED_ALERTS_STORAGE_KEY,
      JSON.stringify({
        "alert-error-rate-high": { mutedUntil },
      }),
    );

    await renderAndWait();
    // Wait for the hook's hydrating useEffect to flush.
    await waitFor(() => {
      const rows = screen.getAllByTestId("sidebar-alert-link");
      expect(rows[0]).toHaveAttribute("data-muted", "true");
    });

    const mutedRow = screen.getAllByTestId("sidebar-alert-link")[0];
    expect(mutedRow.className).toMatch(/opacity-60/);
    // Badge should appear for the muted row.
    const badge = within(mutedRow).getByTestId("muted-badge");
    expect(badge.textContent).toMatch(/muted until/i);
  });

  it("non-muted rows have data-muted=false", async () => {
    await renderAndWait();
    const rows = screen.getAllByTestId("sidebar-alert-link");
    rows.forEach((row) => {
      expect(row).toHaveAttribute("data-muted", "false");
    });
  });
});

describe("SidebarAlerts — empty + loading states", () => {
  it('renders "No active alerts" when the API returns an empty list', async () => {
    mockApiGet.mockResolvedValueOnce({ alerts: [], total: 0 });
    render(<SidebarAlerts />);
    await waitFor(() => expect(mockApiGet).toHaveBeenCalled());
    await act(async () => {});
    expect(await screen.findByText(/No active alerts/i)).toBeInTheDocument();
  });
});
