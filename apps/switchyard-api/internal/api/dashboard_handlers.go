package api

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/sync/errgroup"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/logging"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// DashboardStats represents the dashboard statistics
type DashboardStats struct {
	HealthyServices  int    `json:"healthy_services"`
	DeploymentsToday int    `json:"deployments_today"`
	ActiveProjects   int    `json:"active_projects"`
	AvgDeployTime    string `json:"avg_deploy_time"`
}

// RecentActivity represents a recent activity item
type RecentActivity struct {
	ID        string                 `json:"id"`
	Type      string                 `json:"type"`
	Message   string                 `json:"message"`
	Timestamp time.Time              `json:"timestamp"`
	Status    string                 `json:"status"`
	Metadata  map[string]interface{} `json:"metadata,omitempty"`
}

// ServiceOverview represents a service in the dashboard
type ServiceOverview struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	ProjectName string `json:"project_name"`
	Environment string `json:"environment"`
	Status      string `json:"status"`
	Version     string `json:"version"`
	Replicas    string `json:"replicas"`
}

// DashboardResponse contains all dashboard data
type DashboardResponse struct {
	Stats      DashboardStats    `json:"stats"`
	Activities []RecentActivity  `json:"activities"`
	Services   []ServiceOverview `json:"services"`
}

// dashboardCache provides in-memory caching for dashboard data
type dashboardCache struct {
	mu         sync.RWMutex
	data       *DashboardResponse
	expiry     time.Time
	ttl        time.Duration
	inProgress bool
}

var dashboardStatsCache = &dashboardCache{
	ttl: 5 * time.Second, // Cache for 5 seconds - balances freshness with performance
}

// GetDashboardStats returns public dashboard statistics
// This endpoint does not require authentication for local development
func (h *Handler) GetDashboardStats(c *gin.Context) {
	ctx := c.Request.Context()

	// Check cache first (fast path)
	dashboardStatsCache.mu.RLock()
	if dashboardStatsCache.data != nil && time.Now().Before(dashboardStatsCache.expiry) {
		data := dashboardStatsCache.data
		dashboardStatsCache.mu.RUnlock()
		c.JSON(http.StatusOK, data)
		return
	}
	dashboardStatsCache.mu.RUnlock()

	// Cache miss - fetch fresh data
	response, err := h.fetchDashboardData(ctx)
	if err != nil {
		h.logger.Error(ctx, "Failed to get dashboard data", logging.Error("error", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get dashboard stats"})
		return
	}

	// Update cache
	dashboardStatsCache.mu.Lock()
	dashboardStatsCache.data = response
	dashboardStatsCache.expiry = time.Now().Add(dashboardStatsCache.ttl)
	dashboardStatsCache.mu.Unlock()

	c.JSON(http.StatusOK, response)
}

// fetchDashboardData fetches all dashboard data with parallel queries
func (h *Handler) fetchDashboardData(ctx context.Context) (*DashboardResponse, error) {
	var (
		stats      DashboardStats
		activities []RecentActivity
		services   []ServiceOverview
	)

	// Fetch all three data sets in parallel using errgroup
	g, ctx := errgroup.WithContext(ctx)

	g.Go(func() error {
		var err error
		stats, err = h.getDashboardStatsOptimized(ctx)
		return err
	})

	g.Go(func() error {
		var err error
		activities, err = h.getRecentActivitiesOptimized(ctx)
		if err != nil {
			h.logger.Warn(ctx, "Failed to get activities, returning empty", logging.Error("error", err))
			activities = []RecentActivity{}
			return nil // Don't fail the entire request for activities
		}
		return nil
	})

	g.Go(func() error {
		var err error
		services, err = h.getServicesOverviewOptimized(ctx)
		if err != nil {
			h.logger.Warn(ctx, "Failed to get services overview, returning empty", logging.Error("error", err))
			services = []ServiceOverview{}
			return nil // Don't fail the entire request for services
		}
		return nil
	})

	if err := g.Wait(); err != nil {
		return nil, err
	}

	return &DashboardResponse{
		Stats:      stats,
		Activities: activities,
		Services:   services,
	}, nil
}

// getDashboardStatsOptimized retrieves dashboard statistics with optimized queries
func (h *Handler) getDashboardStatsOptimized(ctx context.Context) (DashboardStats, error) {
	stats := DashboardStats{
		AvgDeployTime: "N/A",
	}

	// Fetch projects once (single query)
	projects, err := h.projectService.ListProjects(ctx)
	if err != nil {
		return stats, err
	}
	stats.ActiveProjects = len(projects)

	if len(projects) == 0 {
		return stats, nil
	}

	// Use a simpler approach - collect service IDs first, then batch query
	var (
		allServiceIDs []string
		serviceMap    = make(map[string]*types.Service)
		mu            sync.Mutex
	)

	// Parallel service fetching per project
	g, gCtx := errgroup.WithContext(ctx)
	for _, project := range projects {
		project := project // capture loop variable
		g.Go(func() error {
			services, err := h.projectService.ListServices(gCtx, project.Slug)
			if err != nil {
				return nil // Skip errors for individual projects
			}
			mu.Lock()
			for _, svc := range services {
				svcCopy := svc
				allServiceIDs = append(allServiceIDs, svc.ID.String())
				serviceMap[svc.ID.String()] = svcCopy
			}
			mu.Unlock()
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		h.logger.Warn(ctx, "Some concurrent dashboard operations failed",
			logging.Error("error", err))
		// Continue with partial data
	}

	// Now batch query deployments for all services
	todayStart := time.Now().Truncate(24 * time.Hour)
	healthyCount := 0
	deploymentsToday := 0
	var totalDeployDuration time.Duration
	completedDeploys := 0

	// Process services in parallel batches
	g2, g2Ctx := errgroup.WithContext(ctx)
	var countMu sync.Mutex

	for _, svcID := range allServiceIDs {
		svcID := svcID
		g2.Go(func() error {
			svc := serviceMap[svcID]

			// Always check deployment table for today's deployments count
			// This must run regardless of which phase provides health status
			deployment, err := h.repos.Deployments.GetLatestByService(g2Ctx, svcID)
			if err == nil && deployment != nil && deployment.CreatedAt.After(todayStart) {
				countMu.Lock()
				deploymentsToday++
				// Track deploy duration for completed (running) deployments
				if deployment.Status == types.DeploymentStatusRunning && !deployment.UpdatedAt.IsZero() {
					duration := deployment.UpdatedAt.Sub(deployment.CreatedAt)
					if duration > 0 && duration < 30*time.Minute { // Sanity check: ignore unrealistic times
						totalDeployDuration += duration
						completedDeploys++
					}
				}
				countMu.Unlock()
			}

			// Phase 1: Check service-level health (from Cartographer)
			if svc != nil && svc.Health == types.HealthStatusHealthy {
				countMu.Lock()
				healthyCount++
				countMu.Unlock()
				return nil
			}

			// Phase 2: Fall back to deployment table for health (already fetched above)
			if deployment != nil {
				countMu.Lock()
				if deployment.Health == types.HealthStatusHealthy {
					healthyCount++
				}
				countMu.Unlock()
			}
			return nil
		})
	}
	if err := g2.Wait(); err != nil {
		h.logger.Warn(ctx, "Some concurrent K8s status checks failed",
			logging.Error("error", err))
		// Continue with partial data
	}

	stats.HealthyServices = healthyCount
	stats.DeploymentsToday = deploymentsToday

	// Calculate average deploy time from tracked deployments
	if completedDeploys > 0 {
		avgDuration := totalDeployDuration / time.Duration(completedDeploys)
		// Format as human-readable duration
		if avgDuration < time.Minute {
			stats.AvgDeployTime = fmt.Sprintf("%ds", int(avgDuration.Seconds()))
		} else if avgDuration < time.Hour {
			minutes := int(avgDuration.Minutes())
			seconds := int(avgDuration.Seconds()) % 60
			if seconds > 0 {
				stats.AvgDeployTime = fmt.Sprintf("%dm %ds", minutes, seconds)
			} else {
				stats.AvgDeployTime = fmt.Sprintf("%dm", minutes)
			}
		} else {
			stats.AvgDeployTime = fmt.Sprintf("%.1fh", avgDuration.Hours())
		}
	}

	return stats, nil
}

// getRecentActivitiesOptimized retrieves recent activities with optimized queries
func (h *Handler) getRecentActivitiesOptimized(ctx context.Context) ([]RecentActivity, error) {
	// Fetch projects once
	projects, err := h.projectService.ListProjects(ctx)
	if err != nil {
		return nil, err
	}

	// Collect activities from all projects in parallel
	var (
		allActivities []RecentActivity
		mu            sync.Mutex
	)

	g, ctx := errgroup.WithContext(ctx)

	for _, project := range projects {
		project := project
		g.Go(func() error {
			projectActivities, err := h.getProjectActivities(ctx, project)
			if err != nil {
				h.logger.Warn(ctx, "Failed to get activities for project",
					logging.String("project", project.Slug),
					logging.Error("error", err))
				return nil // Don't fail for individual project errors
			}
			mu.Lock()
			allActivities = append(allActivities, projectActivities...)
			mu.Unlock()
			return nil
		})
	}

	if err := g.Wait(); err != nil {
		h.logger.Warn(ctx, "Some concurrent activity fetches failed",
			logging.Error("error", err))
		// Continue with partial data
	}

	// Use efficient sort.Slice instead of bubble sort - O(n log n) vs O(n²)
	sort.Slice(allActivities, func(i, j int) bool {
		return allActivities[i].Timestamp.After(allActivities[j].Timestamp)
	})

	// Limit to 10 most recent
	if len(allActivities) > 10 {
		allActivities = allActivities[:10]
	}

	return allActivities, nil
}

// getProjectActivities fetches activities for a single project
func (h *Handler) getProjectActivities(ctx context.Context, project *types.Project) ([]RecentActivity, error) {
	var activities []RecentActivity

	services, err := h.projectService.ListServices(ctx, project.Slug)
	if err != nil {
		return activities, err
	}

	// Only fetch recent deployments (last 24 hours) to reduce data
	cutoff := time.Now().Add(-24 * time.Hour)

	for _, svc := range services {
		releases, err := h.repos.Releases.ListByService(svc.ID)
		if err != nil {
			continue
		}

		for _, release := range releases {
			deployments, err := h.repos.Deployments.ListByRelease(ctx, release.ID.String())
			if err != nil {
				continue
			}

			for _, d := range deployments {
				// Skip old deployments early
				if d.CreatedAt.Before(cutoff) {
					continue
				}

				status := mapDeploymentStatus(d.Status)

				activities = append(activities, RecentActivity{
					ID:        d.ID.String(),
					Type:      "deployment",
					Message:   "Deployed " + svc.Name + " to " + project.Name,
					Timestamp: d.CreatedAt,
					Status:    status,
					Metadata: map[string]interface{}{
						"version":     release.Version,
						"environment": "production",
					},
				})
			}
		}
	}

	return activities, nil
}

// mapDeploymentStatus converts deployment status to display status
func mapDeploymentStatus(status types.DeploymentStatus) string {
	switch status {
	case types.DeploymentStatusRunning:
		return "success"
	case types.DeploymentStatusFailed:
		return "failed"
	case types.DeploymentStatusPending:
		return "pending"
	default:
		return string(status)
	}
}

// getServicesOverviewOptimized retrieves service overview with parallel queries
func (h *Handler) getServicesOverviewOptimized(ctx context.Context) ([]ServiceOverview, error) {
	projects, err := h.projectService.ListProjects(ctx)
	if err != nil {
		return nil, err
	}

	var (
		overview []ServiceOverview
		mu       sync.Mutex
	)

	g, ctx := errgroup.WithContext(ctx)

	for _, project := range projects {
		project := project
		g.Go(func() error {
			projectServices, err := h.getProjectServicesOverview(ctx, project)
			if err != nil {
				return nil // Don't fail for individual project errors
			}
			mu.Lock()
			overview = append(overview, projectServices...)
			mu.Unlock()
			return nil
		})
	}

	if err := g.Wait(); err != nil {
		h.logger.Warn(ctx, "Some concurrent service overview fetches failed",
			logging.Error("error", err))
		// Continue with partial data
	}

	return overview, nil
}

// getProjectServicesOverview fetches service overview for a single project
func (h *Handler) getProjectServicesOverview(ctx context.Context, project *types.Project) ([]ServiceOverview, error) {
	var overview []ServiceOverview

	services, err := h.projectService.ListServices(ctx, project.Slug)
	if err != nil {
		return overview, err
	}

	// Process services in parallel within the project
	var (
		svcMu sync.Mutex
		wg    sync.WaitGroup
	)

	for _, svc := range services {
		svc := svc
		wg.Add(1)
		go func() {
			defer wg.Done()

			status := "unknown"
			version := "N/A"
			replicas := "0/0"

			// Phase 1: Check service-level health (from Cartographer)
			if svc.Health != "" && svc.Health != types.HealthStatusUnknown {
				switch svc.Health {
				case types.HealthStatusHealthy:
					status = "healthy"
				case types.HealthStatusUnhealthy:
					status = "unhealthy"
				default:
					status = "unknown"
				}
				replicas = fmt.Sprintf("%d/%d", svc.ReadyReplicas, svc.DesiredReplicas)
			} else {
				// Phase 2: Fall back to deployment table (Enclii-deployed services)
				deployment, err := h.repos.Deployments.GetLatestByService(ctx, svc.ID.String())
				if err == nil && deployment != nil {
					switch deployment.Health {
					case types.HealthStatusHealthy:
						status = "healthy"
					case types.HealthStatusUnhealthy:
						status = "unhealthy"
					default:
						status = "unknown"
					}
					replicas = "1/1"

					release, err := h.repos.Releases.GetByID(deployment.ReleaseID)
					if err == nil {
						version = release.Version
					}
				}
			}

			svcMu.Lock()
			overview = append(overview, ServiceOverview{
				ID:          svc.ID.String(),
				Name:        svc.Name,
				ProjectName: project.Name,
				Environment: "production",
				Status:      status,
				Version:     version,
				Replicas:    replicas,
			})
			svcMu.Unlock()
		}()
	}

	wg.Wait()
	return overview, nil
}
