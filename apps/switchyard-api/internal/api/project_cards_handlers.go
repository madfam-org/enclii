package api

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/errors"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/logging"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/middleware"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

type projectCardAggregateResponse struct {
	GeneratedAt time.Time              `json:"generated_at"`
	Projects    []projectCardAggregate `json:"projects"`
	Count       int                    `json:"count"`
}

type projectCardAggregate struct {
	ID          uuid.UUID `json:"id"`
	Name        string    `json:"name"`
	Slug        string    `json:"slug"`
	Description string    `json:"description,omitempty"`
	UpdatedAt   time.Time `json:"updated_at"`

	AggregateStatus string `json:"aggregate_status"`
	ServiceCount    int    `json:"service_count"`
	HealthyCount    int    `json:"healthy_count"`

	Framework string `json:"framework,omitempty"`
	GitRepo   string `json:"git_repo,omitempty"`
	Domain    string `json:"domain,omitempty"`

	DeployResolution string                     `json:"deploy_resolution"`
	LastDeployment   *projectCardLastDeployment `json:"last_deployment,omitempty"`

	Evidence projectCardEvidence  `json:"evidence"`
	Services []projectCardService `json:"services"`
}

type projectCardService struct {
	ID                   uuid.UUID  `json:"id"`
	Name                 string     `json:"name"`
	Status               string     `json:"status"`
	Health               string     `json:"health"`
	Replicas             string     `json:"replicas,omitempty"`
	Environment          string     `json:"environment,omitempty"`
	Domain               string     `json:"domain,omitempty"`
	CurrentImageURI      string     `json:"current_image_uri,omitempty"`
	RolloutState         string     `json:"rollout_state,omitempty"`
	RolloutBlockedReason string     `json:"rollout_blocked_reason,omitempty"`
	HealthObservedAt     *time.Time `json:"health_observed_at,omitempty"`
	HealthStale          bool       `json:"health_stale,omitempty"`
}

type projectCardLastDeployment struct {
	Timestamp     time.Time `json:"timestamp"`
	Status        string    `json:"status"`
	Branch        string    `json:"branch"`
	CommitMessage string    `json:"commit_message,omitempty"`
}

type projectCardEvidence struct {
	ServiceRows     projectCardServiceRowsEvidence      `json:"service_rows"`
	ArgoApplication *projectCardArgoApplicationEvidence `json:"argo_application,omitempty"`
	Jobs            *projectCardJobsEvidence            `json:"jobs,omitempty"`
}

type projectCardServiceRowsEvidence struct {
	Status            string     `json:"status"`
	Count             int        `json:"count"`
	HealthyCount      int        `json:"healthy_count"`
	StaleCount        int        `json:"stale_count"`
	LastObservedAt    *time.Time `json:"last_observed_at,omitempty"`
	StaleAfterSeconds int        `json:"stale_after_seconds"`
}

type projectCardArgoApplicationEvidence struct {
	Name                 string     `json:"name"`
	SyncStatus           string     `json:"sync_status"`
	HealthStatus         string     `json:"health_status"`
	Revision             string     `json:"revision,omitempty"`
	DestinationNamespace string     `json:"destination_namespace,omitempty"`
	ObservedAt           time.Time  `json:"observed_at"`
	DeployedAt           *time.Time `json:"deployed_at,omitempty"`
	SourceRepo           string     `json:"-"`
	PartOf               string     `json:"-"`
}

type projectCardJobsEvidence struct {
	Status         string                   `json:"status"`
	NamespaceCount int                      `json:"namespace_count"`
	CronJobCount   int                      `json:"cron_job_count"`
	FailedCount    int                      `json:"failed_count"`
	ActiveCount    int                      `json:"active_count"`
	StuckCount     int                      `json:"stuck_count"`
	PendingCount   int                      `json:"pending_count,omitempty"`
	SucceededCount int                      `json:"succeeded_count"`
	LastObservedAt time.Time                `json:"last_observed_at"`
	Items          []projectCardJobEvidence `json:"items,omitempty"`
}

type projectCardJobEvidence struct {
	Namespace        string     `json:"namespace"`
	Name             string     `json:"name"`
	Status           string     `json:"status"`
	LatestJobName    string     `json:"latest_job_name,omitempty"`
	RecentFailedJobs int        `json:"recent_failed_jobs,omitempty"`
	ActiveJobs       int        `json:"active_jobs,omitempty"`
	StuckJobs        int        `json:"stuck_jobs,omitempty"`
	SucceededJobs    int        `json:"succeeded_jobs,omitempty"`
	LastScheduleTime *time.Time `json:"last_schedule_time,omitempty"`
	LastFailureTime  *time.Time `json:"last_failure_time,omitempty"`
}

const projectCardServiceHealthStaleAfter = 10 * time.Minute

// ListProjectCards returns the dashboard/project-card aggregate used by both
// the main dashboard and /projects. It preserves /v1/projects as the raw
// project list for SDK compatibility while giving the UI one authoritative
// card projection with service facts, release metadata, and rollout state.
func (h *Handler) ListProjectCards(c *gin.Context) {
	ctx := c.Request.Context()
	if h == nil || h.projectService == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Project service not configured"})
		return
	}

	var teamID *uuid.UUID
	if id, ok := middleware.ActingTeamID(c); ok {
		teamID = &id
	}

	projects, err := h.projectService.ListProjectsScoped(ctx, teamID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to list projects"})
		return
	}

	generatedAt := time.Now().UTC()
	cards := make([]projectCardAggregate, 0, len(projects))
	argoEvidenceByName := h.listProjectCardArgoEvidence(ctx, generatedAt)
	onboardingArgoAppsByProject := h.projectCardOnboardingArgoApps(ctx)
	jobEvidenceByNamespace := h.listProjectCardJobEvidence(ctx, generatedAt)

	for _, project := range projects {
		services, err := h.projectService.ListServices(ctx, project.Slug)
		if err != nil {
			if errors.Is(err, errors.ErrProjectNotFound) {
				continue
			}
			h.logger.Error(ctx, "Failed to list services for project card",
				logging.Error("error", err),
				logging.String("project_slug", project.Slug))
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to list project cards"})
			return
		}

		h.enrichServicesWithRolloutState(ctx, services)
		argoEvidence := matchProjectCardArgoEvidence(project, services, onboardingArgoAppsByProject, argoEvidenceByName)
		jobEvidence := matchProjectCardJobEvidence(project, services, argoEvidence, jobEvidenceByNamespace)
		cardServices := projectCardVisibleServices(project, services)
		cards = append(cards, buildProjectCardAggregateAt(project, cardServices, argoEvidence, jobEvidence, generatedAt))
	}

	c.JSON(http.StatusOK, projectCardAggregateResponse{
		GeneratedAt: generatedAt,
		Projects:    cards,
		Count:       len(cards),
	})
}

func buildProjectCardAggregate(project *types.Project, services []*types.Service) projectCardAggregate {
	return buildProjectCardAggregateAt(project, services, nil, nil, time.Now().UTC())
}

func projectCardVisibleServices(project *types.Project, services []*types.Service) []*types.Service {
	if len(services) == 0 {
		return services
	}
	visible := make([]*types.Service, 0, len(services))
	for _, service := range services {
		if projectCardPlaceholderService(project, service) {
			continue
		}
		visible = append(visible, service)
	}
	return visible
}

func projectCardPlaceholderService(project *types.Project, service *types.Service) bool {
	if project == nil || service == nil {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(service.Name), strings.TrimSpace(project.Slug)) &&
		service.K8sNamespace == nil &&
		service.DesiredReplicas == 0 &&
		service.ReadyReplicas == 0 &&
		service.LastDeployment == nil
}

func buildProjectCardAggregateAt(project *types.Project, services []*types.Service, argoEvidence *projectCardArgoApplicationEvidence, jobEvidence *projectCardJobsEvidence, now time.Time) projectCardAggregate {
	cardServices := make([]projectCardService, 0, len(services))
	healthyCount := 0
	staleCount := 0
	var framework string
	var gitRepo string
	var domain string
	var latest *types.Service
	var lastObservedAt *time.Time

	for _, service := range services {
		if service == nil {
			continue
		}
		health, healthStale := cardServiceHealth(service, now)
		if health == string(types.HealthStatusHealthy) {
			healthyCount++
		}
		if healthStale {
			staleCount++
		}
		lastObservedAt = laterTime(lastObservedAt, service.LastHealthCheck)
		if framework == "" {
			framework = service.Framework
		}
		if gitRepo == "" {
			gitRepo = service.GitRepo
		}
		// Domain is not yet persisted on Service. Keep the aggregate field for
		// forward-compatible API shape; existing UI still preserves per-service
		// domain if/when the service response grows that field.
		if service.LastDeployment != nil && (latest == nil || service.LastDeployment.After(*latest.LastDeployment)) {
			latest = service
		}
		cardServices = append(cardServices, projectCardService{
			ID:                   service.ID,
			Name:                 service.Name,
			Status:               normalizeCardServiceStatus(service.Status),
			Health:               health,
			Replicas:             formatCardReplicas(service.ReadyReplicas, service.DesiredReplicas),
			Environment:          service.AutoDeployEnv,
			Domain:               "",
			CurrentImageURI:      service.CurrentImageURI,
			RolloutState:         normalizeCardRolloutState(service.RolloutState),
			RolloutBlockedReason: service.RolloutBlockedReason,
			HealthObservedAt:     service.LastHealthCheck,
			HealthStale:          healthStale,
		})
	}

	deployResolution := "no-deploys"
	var lastDeployment *projectCardLastDeployment
	if latest != nil && latest.LastDeployment != nil {
		deployResolution = "deployed"
		branch := latest.LastCommitBranch
		if branch == "" {
			branch = "main"
		}
		lastDeployment = &projectCardLastDeployment{
			Timestamp:     *latest.LastDeployment,
			Status:        deploymentStatusForCard(latest.Status),
			Branch:        branch,
			CommitMessage: latest.LastCommitMsg,
		}
	}
	if lastDeployment == nil && argoEvidence != nil && argoEvidence.DeployedAt != nil {
		deployResolution = "deployed"
		lastDeployment = &projectCardLastDeployment{
			Timestamp: *argoEvidence.DeployedAt,
			Status:    deploymentStatusForArgoEvidence(argoEvidence),
			Branch:    "main",
		}
	}

	serviceRowsStatus := "fresh"
	if len(cardServices) == 0 {
		serviceRowsStatus = "empty"
	} else if staleCount > 0 {
		serviceRowsStatus = "stale"
	}

	return projectCardAggregate{
		ID:               project.ID,
		Name:             project.Name,
		Slug:             project.Slug,
		UpdatedAt:        project.UpdatedAt,
		AggregateStatus:  aggregateStatusForCard(cardServices, argoEvidence, jobEvidence),
		ServiceCount:     len(cardServices),
		HealthyCount:     healthyCount,
		Framework:        framework,
		GitRepo:          gitRepo,
		Domain:           domain,
		DeployResolution: deployResolution,
		LastDeployment:   lastDeployment,
		Evidence: projectCardEvidence{
			ServiceRows: projectCardServiceRowsEvidence{
				Status:            serviceRowsStatus,
				Count:             len(cardServices),
				HealthyCount:      healthyCount,
				StaleCount:        staleCount,
				LastObservedAt:    lastObservedAt,
				StaleAfterSeconds: int(projectCardServiceHealthStaleAfter.Seconds()),
			},
			ArgoApplication: argoEvidence,
			Jobs:            jobEvidence,
		},
		Services: cardServices,
	}
}

func deploymentStatusForArgoEvidence(argoEvidence *projectCardArgoApplicationEvidence) string {
	switch aggregateStatusFromArgoEvidence(argoEvidence) {
	case "healthy":
		return "success"
	case "failing":
		return "failed"
	default:
		return "pending"
	}
}

func normalizeCardServiceStatus(status string) string {
	switch status {
	case "running", "pending", "failed", "deploying", "unknown":
		return status
	default:
		return "unknown"
	}
}

func normalizeCardServiceHealth(health types.HealthStatus) string {
	switch health {
	case types.HealthStatusHealthy, types.HealthStatusUnhealthy, types.HealthStatusUnknown:
		return string(health)
	default:
		return string(types.HealthStatusUnknown)
	}
}

func normalizeCardRolloutState(state string) string {
	switch state {
	case "ok", "progressing", "blocked":
		return state
	default:
		return ""
	}
}

func cardServiceHealth(service *types.Service, now time.Time) (string, bool) {
	health := normalizeCardServiceHealth(service.Health)
	if health != string(types.HealthStatusHealthy) || service.LastHealthCheck == nil {
		return health, false
	}
	observedAt := service.LastHealthCheck.UTC()
	if now.Sub(observedAt) > projectCardServiceHealthStaleAfter {
		return "stale", true
	}
	return health, false
}

func laterTime(current, candidate *time.Time) *time.Time {
	if candidate == nil {
		return current
	}
	candidateUTC := candidate.UTC()
	if current == nil || candidateUTC.After(*current) {
		return &candidateUTC
	}
	return current
}

func formatCardReplicas(ready, desired int) string {
	if desired <= 0 && ready <= 0 {
		return ""
	}
	return fmt.Sprintf("%d/%d", ready, desired)
}

func deploymentStatusForCard(serviceStatus string) string {
	switch serviceStatus {
	case "running":
		return "success"
	case "failed":
		return "failed"
	case "deploying":
		return "building"
	default:
		return "pending"
	}
}

func aggregateStatusForCard(services []projectCardService, argoEvidence *projectCardArgoApplicationEvidence, jobEvidence *projectCardJobsEvidence) string {
	argoStatus := aggregateStatusFromArgoEvidence(argoEvidence)
	if argoStatus == "failing" {
		return "failing"
	}
	jobStatus := aggregateStatusFromJobEvidence(jobEvidence)
	if jobStatus == "failing" {
		return "failing"
	}
	if len(services) == 0 {
		if jobStatus != "" {
			return jobStatus
		}
		if argoStatus != "" {
			return argoStatus
		}
		return "unknown"
	}

	allHealthyAndStable := true
	allStableWithOnlyStaleHealth := len(services) > 0
	for _, service := range services {
		if service.RolloutState == "blocked" || service.Status == "failed" {
			return "failing"
		}
		if service.Status != "running" || service.Health != "healthy" || service.RolloutState == "progressing" {
			allHealthyAndStable = false
		}
		if service.Status != "running" || service.RolloutState == "progressing" || (service.Health != "healthy" && service.Health != "stale") {
			allStableWithOnlyStaleHealth = false
		}
	}
	if allHealthyAndStable && argoStatus != "degraded" && jobStatus != "degraded" {
		return "healthy"
	}
	if allStableWithOnlyStaleHealth && argoStatus == "healthy" && jobStatus != "degraded" {
		return "healthy"
	}
	return "degraded"
}

func aggregateStatusFromJobEvidence(jobEvidence *projectCardJobsEvidence) string {
	if jobEvidence == nil {
		return ""
	}
	switch jobEvidence.Status {
	case "failing":
		return "failing"
	case "degraded", "unknown":
		return "degraded"
	case "healthy", "active", "pending", "empty":
		return "healthy"
	default:
		return ""
	}
}

func aggregateStatusFromArgoEvidence(argoEvidence *projectCardArgoApplicationEvidence) string {
	if argoEvidence == nil {
		return ""
	}
	switch argoEvidence.HealthStatus {
	case "Degraded", "Missing":
		return "failing"
	case "Progressing", "Suspended", "Unknown":
		return "degraded"
	}
	switch argoEvidence.SyncStatus {
	case "Failed", "Error":
		return "failing"
	case "OutOfSync", "Unknown":
		return "degraded"
	}
	if argoEvidence.SyncStatus != "" && argoEvidence.SyncStatus != "Synced" {
		return "degraded"
	}
	if argoEvidence.HealthStatus != "" && argoEvidence.HealthStatus != "Healthy" {
		return "degraded"
	}
	if argoEvidence.SyncStatus == "Synced" && argoEvidence.HealthStatus == "Healthy" {
		return "healthy"
	}
	return ""
}
