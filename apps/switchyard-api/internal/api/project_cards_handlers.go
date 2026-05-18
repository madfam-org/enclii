package api

import (
	"fmt"
	"net/http"
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

	Services []projectCardService `json:"services"`
}

type projectCardService struct {
	ID                   uuid.UUID `json:"id"`
	Name                 string    `json:"name"`
	Status               string    `json:"status"`
	Health               string    `json:"health"`
	Replicas             string    `json:"replicas,omitempty"`
	Environment          string    `json:"environment,omitempty"`
	Domain               string    `json:"domain,omitempty"`
	CurrentImageURI      string    `json:"current_image_uri,omitempty"`
	RolloutState         string    `json:"rollout_state,omitempty"`
	RolloutBlockedReason string    `json:"rollout_blocked_reason,omitempty"`
}

type projectCardLastDeployment struct {
	Timestamp     time.Time `json:"timestamp"`
	Status        string    `json:"status"`
	Branch        string    `json:"branch"`
	CommitMessage string    `json:"commit_message,omitempty"`
}

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
		cards = append(cards, buildProjectCardAggregate(project, services))
	}

	c.JSON(http.StatusOK, projectCardAggregateResponse{
		GeneratedAt: generatedAt,
		Projects:    cards,
		Count:       len(cards),
	})
}

func buildProjectCardAggregate(project *types.Project, services []*types.Service) projectCardAggregate {
	cardServices := make([]projectCardService, 0, len(services))
	healthyCount := 0
	var framework string
	var gitRepo string
	var domain string
	var latest *types.Service

	for _, service := range services {
		if service == nil {
			continue
		}
		if service.Health == types.HealthStatusHealthy {
			healthyCount++
		}
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
			Health:               normalizeCardServiceHealth(service.Health),
			Replicas:             formatCardReplicas(service.ReadyReplicas, service.DesiredReplicas),
			Environment:          service.AutoDeployEnv,
			Domain:               "",
			CurrentImageURI:      service.CurrentImageURI,
			RolloutState:         normalizeCardRolloutState(service.RolloutState),
			RolloutBlockedReason: service.RolloutBlockedReason,
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

	return projectCardAggregate{
		ID:               project.ID,
		Name:             project.Name,
		Slug:             project.Slug,
		UpdatedAt:        project.UpdatedAt,
		AggregateStatus:  aggregateStatusForCard(cardServices),
		ServiceCount:     len(cardServices),
		HealthyCount:     healthyCount,
		Framework:        framework,
		GitRepo:          gitRepo,
		Domain:           domain,
		DeployResolution: deployResolution,
		LastDeployment:   lastDeployment,
		Services:         cardServices,
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

func aggregateStatusForCard(services []projectCardService) string {
	if len(services) == 0 {
		return "unknown"
	}

	allHealthyAndStable := true
	for _, service := range services {
		if service.RolloutState == "blocked" || service.Status == "failed" {
			return "failing"
		}
		if service.Status != "running" || service.Health != "healthy" || service.RolloutState == "progressing" {
			allHealthyAndStable = false
		}
	}
	if allHealthyAndStable {
		return "healthy"
	}
	return "degraded"
}
