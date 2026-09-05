package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/logging"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// GitHubActionsStatus represents the aggregated status of GitHub Actions workflows
type GitHubActionsStatus struct {
	Workflows     []*types.CIRun `json:"workflows"`
	OverallStatus string         `json:"overall_status"` // pending, in_progress, success, failure
	TotalRuns     int            `json:"total_runs"`
	SuccessCount  int            `json:"success_count"`
	FailureCount  int            `json:"failure_count"`
	InProgress    int            `json:"in_progress"`
}

// RoundhouseStatus represents the status of Roundhouse container builds
type RoundhouseStatus struct {
	Release      *types.Release `json:"release,omitempty"`
	Status       string         `json:"status"` // building, ready, failed
	ImageURI     string         `json:"image_uri,omitempty"`
	ErrorMessage string         `json:"error_message,omitempty"`
	HasSBOM      bool           `json:"has_sbom"`
	HasSignature bool           `json:"has_signature"`
}

// DeploymentProgressStatus represents the status of a deployment
type DeploymentProgressStatus struct {
	Deployment   *types.Deployment `json:"deployment,omitempty"`
	Status       string            `json:"status"` // pending, running, failed
	Health       string            `json:"health"`
	ErrorMessage string            `json:"error_message,omitempty"`
}

// UnifiedBuildStatus represents the complete build and deployment pipeline status
type UnifiedBuildStatus struct {
	CommitSHA     string                    `json:"commit_sha"`
	ServiceID     string                    `json:"service_id"`
	ServiceName   string                    `json:"service_name"`
	GitHubActions *GitHubActionsStatus      `json:"github_actions,omitempty"`
	Roundhouse    *RoundhouseStatus         `json:"roundhouse,omitempty"`
	Deployment    *DeploymentProgressStatus `json:"deployment,omitempty"`
	OverallStatus string                    `json:"overall_status"` // pending, building, ready, deploying, running, failed
	Stages        []BuildStage              `json:"stages"`
}

// BuildStage represents a stage in the build pipeline
type BuildStage struct {
	Name         string `json:"name"`
	Status       string `json:"status"` // pending, in_progress, success, failure, skipped
	StartedAt    string `json:"started_at,omitempty"`
	CompletedAt  string `json:"completed_at,omitempty"`
	ErrorMessage string `json:"error_message,omitempty"`
	URL          string `json:"url,omitempty"`
}

// GetUnifiedBuildStatus returns the unified build status for a commit
// GET /v1/services/:id/builds/:build_id/status
// Note: :build_id can be either a release UUID or commit SHA
func (h *Handler) GetUnifiedBuildStatus(c *gin.Context) {
	ctx := c.Request.Context()
	serviceID := c.Param("id")
	commitSHA := c.Param("build_id") // Can be commit SHA or release ID

	h.logger.Info(ctx, "Getting unified build status",
		logging.String("service_id", serviceID),
		logging.String("commit_sha", commitSHA))

	// Parse service ID
	serviceUUID, err := uuid.Parse(serviceID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid service ID"})
		return
	}

	// ADR-003: the tenant comparison at the target. Staged behind
	// ENCLII_TENANT_SCOPE_ENFORCE — see access_staged.go.
	if !h.enforceStagedServiceAccess(c, serviceUUID) {
		return
	}

	// Get service info
	service, err := h.repos.Services.GetByID(serviceUUID)
	if err != nil {
		h.logger.Error(ctx, "Service not found",
			logging.String("service_id", serviceID),
			logging.Error("error", err))
		c.JSON(http.StatusNotFound, gin.H{"error": "Service not found"})
		return
	}

	status := &UnifiedBuildStatus{
		CommitSHA:     commitSHA,
		ServiceID:     serviceID,
		ServiceName:   service.Name,
		OverallStatus: "pending",
		Stages:        []BuildStage{},
	}

	// Stage 1: Get GitHub Actions CI status
	ciRuns, err := h.repos.CIRuns.ListByServiceAndCommit(ctx, serviceUUID, commitSHA)
	if err == nil && len(ciRuns) > 0 {
		ghStatus := &GitHubActionsStatus{
			Workflows:     ciRuns,
			TotalRuns:     len(ciRuns),
			OverallStatus: "success",
		}

		// Calculate aggregated status
		for _, run := range ciRuns {
			switch run.Status {
			case types.CIRunStatusQueued, types.CIRunStatusInProgress:
				ghStatus.InProgress++
			case types.CIRunStatusCompleted:
				if run.Conclusion != nil {
					switch *run.Conclusion {
					case types.CIRunConclusionSuccess:
						ghStatus.SuccessCount++
					case types.CIRunConclusionFailure, types.CIRunConclusionTimedOut:
						ghStatus.FailureCount++
					}
				}
			}
		}

		// Determine overall GitHub Actions status
		if ghStatus.InProgress > 0 {
			ghStatus.OverallStatus = "in_progress"
		} else if ghStatus.FailureCount > 0 {
			ghStatus.OverallStatus = "failure"
		} else if ghStatus.SuccessCount == ghStatus.TotalRuns {
			ghStatus.OverallStatus = "success"
		} else {
			ghStatus.OverallStatus = "pending"
		}

		status.GitHubActions = ghStatus

		// Add CI stage to stages list
		ciStage := BuildStage{
			Name:   "CI Pipeline",
			Status: ghStatus.OverallStatus,
		}
		if len(ciRuns) > 0 && ciRuns[0].HTMLURL != "" {
			ciStage.URL = ciRuns[0].HTMLURL
		}
		if len(ciRuns) > 0 && ciRuns[0].StartedAt != nil {
			ciStage.StartedAt = ciRuns[0].StartedAt.Format("2006-01-02T15:04:05Z")
		}
		status.Stages = append(status.Stages, ciStage)
	}

	// Stage 2: Get Roundhouse build status (release by commit SHA)
	releases, err := h.repos.Releases.ListByService(serviceUUID)
	if err == nil {
		for _, release := range releases {
			if release.GitSHA == commitSHA {
				rhStatus := &RoundhouseStatus{
					Release:      release,
					ImageURI:     release.ImageURI,
					HasSBOM:      release.SBOM != "",
					HasSignature: release.ImageSignature != "",
				}

				switch release.Status {
				case types.ReleaseStatusBuilding:
					rhStatus.Status = "building"
				case types.ReleaseStatusReady:
					rhStatus.Status = "ready"
				case types.ReleaseStatusFailed:
					rhStatus.Status = "failed"
					if release.ErrorMessage != nil {
						rhStatus.ErrorMessage = *release.ErrorMessage
					}
				}

				status.Roundhouse = rhStatus

				// Add build stage
				buildStage := BuildStage{
					Name:      "Container Build",
					Status:    rhStatus.Status,
					StartedAt: release.CreatedAt.Format("2006-01-02T15:04:05Z"),
				}
				if rhStatus.Status == "failed" {
					buildStage.ErrorMessage = rhStatus.ErrorMessage
				}
				status.Stages = append(status.Stages, buildStage)

				// Stage 3: Get deployment status for this release
				deployments, err := h.repos.Deployments.ListByRelease(ctx, release.ID.String())
				if err == nil && len(deployments) > 0 {
					// Use the most recent deployment
					deployment := deployments[0]
					depStatus := &DeploymentProgressStatus{
						Deployment: deployment,
						Health:     string(deployment.Health),
					}

					switch deployment.Status {
					case types.DeploymentStatusPending:
						depStatus.Status = "pending"
					case types.DeploymentStatusRunning:
						depStatus.Status = "running"
					case types.DeploymentStatusFailed:
						depStatus.Status = "failed"
						if deployment.ErrorMessage != nil {
							depStatus.ErrorMessage = *deployment.ErrorMessage
						}
					}

					status.Deployment = depStatus

					// Add deployment stage
					deployStage := BuildStage{
						Name:      "Deployment",
						Status:    depStatus.Status,
						StartedAt: deployment.CreatedAt.Format("2006-01-02T15:04:05Z"),
					}
					if depStatus.Status == "failed" {
						deployStage.ErrorMessage = depStatus.ErrorMessage
					}
					status.Stages = append(status.Stages, deployStage)
				}

				break
			}
		}
	}

	// Calculate overall status
	status.OverallStatus = calculateOverallStatus(status)

	c.JSON(http.StatusOK, status)
}

// calculateOverallStatus determines the overall pipeline status
func calculateOverallStatus(status *UnifiedBuildStatus) string {
	// If any stage failed, overall is failed
	if status.GitHubActions != nil && status.GitHubActions.OverallStatus == "failure" {
		return "failed"
	}
	if status.Roundhouse != nil && status.Roundhouse.Status == "failed" {
		return "failed"
	}
	if status.Deployment != nil && status.Deployment.Status == "failed" {
		return "failed"
	}

	// If deployment is running, overall is running
	if status.Deployment != nil && status.Deployment.Status == "running" {
		return "running"
	}

	// If deploying (pending deployment), overall is deploying
	if status.Deployment != nil && status.Deployment.Status == "pending" {
		return "deploying"
	}

	// If build is ready but no deployment yet, overall is ready
	if status.Roundhouse != nil && status.Roundhouse.Status == "ready" && status.Deployment == nil {
		return "ready"
	}

	// If build is in progress
	if status.Roundhouse != nil && status.Roundhouse.Status == "building" {
		return "building"
	}

	// If CI is in progress
	if status.GitHubActions != nil && status.GitHubActions.OverallStatus == "in_progress" {
		return "building"
	}

	// Default to pending
	return "pending"
}

// GetBuildStatusByCommit returns the unified build status for a commit across all services
// GET /v1/builds/:commit_sha/status
func (h *Handler) GetBuildStatusByCommit(c *gin.Context) {
	ctx := c.Request.Context()
	commitSHA := c.Param("commit_sha")

	h.logger.Info(ctx, "Getting build status by commit",
		logging.String("commit_sha", commitSHA))

	// Get all CI runs for this commit
	ciRuns, err := h.repos.CIRuns.ListByCommitSHA(ctx, commitSHA)
	if err != nil {
		h.logger.Error(ctx, "Failed to get CI runs",
			logging.String("commit_sha", commitSHA),
			logging.Error("error", err))
	}

	// Group by service.
	//
	// ADR-003: this route is keyed by a git sha, not by a tenant-owned id, so
	// there is no single target to compare — every service that built the sha
	// is a target, and they can belong to different tenants. The guard's
	// question is therefore asked per service and answered by DROPPING the
	// row rather than refusing the request: the shape of the response is the
	// same 200 either way, it just stops carrying other tenants' service ids
	// and names. callerMayReachProject is the read-only sibling of the guard
	// (access_staged.go) and is kept in step with it by an agreement test.
	//
	// The route is registered outside the authenticated group, so an
	// anonymous caller resolves to no principal and reaches nothing: at stage
	// 3 this endpoint answers `services: []` to callers that present no
	// credential. That is a real break for any unauthenticated consumer and
	// it is listed in the stage-3 verification steps in the rollout runbook.
	//
	// Staged behind ENCLII_TENANT_SCOPE_ENFORCE — see access_staged.go.
	filterByReach := !h.stagedGateSkipped(c)
	reachable := make(map[uuid.UUID]bool)

	serviceStatuses := make(map[uuid.UUID]*UnifiedBuildStatus)

	for _, run := range ciRuns {
		if filterByReach {
			allowed, known := reachable[run.ServiceID]
			if !known {
				allowed = h.callerMayReachService(c, run.ServiceID)
				reachable[run.ServiceID] = allowed
			}
			if !allowed {
				continue
			}
		}
		if _, exists := serviceStatuses[run.ServiceID]; !exists {
			service, err := h.repos.Services.GetByID(run.ServiceID)
			serviceName := ""
			if err == nil {
				serviceName = service.Name
			}
			serviceStatuses[run.ServiceID] = &UnifiedBuildStatus{
				CommitSHA:   commitSHA,
				ServiceID:   run.ServiceID.String(),
				ServiceName: serviceName,
				GitHubActions: &GitHubActionsStatus{
					Workflows:     []*types.CIRun{},
					OverallStatus: "pending",
				},
				Stages: []BuildStage{},
			}
		}
		serviceStatuses[run.ServiceID].GitHubActions.Workflows = append(
			serviceStatuses[run.ServiceID].GitHubActions.Workflows,
			run,
		)
	}

	// Convert to list
	var statuses []*UnifiedBuildStatus
	for _, status := range serviceStatuses {
		// Calculate aggregated status for each service
		if status.GitHubActions != nil {
			for _, run := range status.GitHubActions.Workflows {
				status.GitHubActions.TotalRuns++
				switch run.Status {
				case types.CIRunStatusQueued, types.CIRunStatusInProgress:
					status.GitHubActions.InProgress++
				case types.CIRunStatusCompleted:
					if run.Conclusion != nil {
						switch *run.Conclusion {
						case types.CIRunConclusionSuccess:
							status.GitHubActions.SuccessCount++
						case types.CIRunConclusionFailure:
							status.GitHubActions.FailureCount++
						}
					}
				}
			}

			if status.GitHubActions.InProgress > 0 {
				status.GitHubActions.OverallStatus = "in_progress"
			} else if status.GitHubActions.FailureCount > 0 {
				status.GitHubActions.OverallStatus = "failure"
			} else if status.GitHubActions.SuccessCount == status.GitHubActions.TotalRuns {
				status.GitHubActions.OverallStatus = "success"
			}
		}

		status.OverallStatus = calculateOverallStatus(status)
		statuses = append(statuses, status)
	}

	c.JSON(http.StatusOK, gin.H{
		"commit_sha": commitSHA,
		"services":   statuses,
	})
}
