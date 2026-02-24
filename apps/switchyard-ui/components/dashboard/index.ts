/**
 * Dashboard Components
 *
 * Reusable components for the Enclii dashboard.
 */

// Statistics
export { StatCard, StatCardSkeleton } from "./stat-card";

// Deployment Progress
export {
  DeploymentProgress,
  DeploymentProgressSkeleton,
  type DeploymentStage,
} from "./deployment-progress";

// System Health
export { SystemHealth, SystemHealthBadge } from "./system-health";

// View Toggle
export { ViewToggle, useViewMode, ProjectsViewHeader } from "./view-toggle";
export type { ViewMode } from "./view-toggle";

// Usage Overview (CircularGauge integration)
export { UsageOverview, UsageGauges } from "./usage-overview";

// Sub-Nav Action Bar
export { SubNavActionBar } from "./sub-nav-action-bar";

// Sidebar Widgets
export { SidebarAlerts } from "./sidebar-alerts";
export { SidebarRecentPreviews } from "./sidebar-recent-previews";
