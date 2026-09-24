package api

// Split-out from deployment_handlers.go to keep that file under the
// repo-wide 800-line cap. This file holds the global cross-service deployment
// listing (`GET /v1/deployments`) with its XC-2 Round 5 tenant-filter
// dispatch, and the per-service list (`GET /v1/services/:id/deployments`).
// Per-deployment detail endpoints remain in deployment_handlers.go because
// they reuse the local helpers there.

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/logging"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/middleware"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// ListAllDeployments returns all deployments across services. XC-2 Round 5:
// when the caller is acting-as a tenant, results are filtered to deployments
// whose owning service's project belongs to that tenant.
func (h *Handler) ListAllDeployments(c *gin.Context) {
	ctx := c.Request.Context()

	var since *time.Time
	if sinceStr := c.Query("since"); sinceStr != "" {
		if d, err := time.ParseDuration(sinceStr); err == nil {
			t := time.Now().Add(-d)
			since = &t
		}
	}

	// limit 1..100 (default 50); anything else is a 400.
	limit, ok := queryLimitOr400(c, 50, 100)
	if !ok {
		return
	}

	var (
		deployments []*types.DeploymentEnriched
		err         error
	)
	// ADR-003: same rule as ListProjects — the unfiltered feed belongs to the
	// platform rank; a tenant admin gets its own tenants' deployments merged
	// with the ones it was explicitly granted.
	if teamID, ok := middleware.ActingTeamID(c); ok {
		deployments, err = h.repos.Deployments.ListAllEnrichedByTeam(ctx, teamID, since, limit)
	} else if h.callerIsPlatformAdmin(c) {
		deployments, err = h.repos.Deployments.ListAllEnriched(ctx, since, limit)
	} else if userID, ok := authenticatedUserID(c); ok {
		deployments, err = h.listDeploymentsForCaller(ctx, c, userID, since, limit)
	} else {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User not authenticated"})
		return
	}
	if err != nil {
		h.logger.Error(ctx, "Failed to list all deployments", logging.Error("db_error", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to retrieve deployments"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"deployments": deployments,
		"count":       len(deployments),
	})
}

// ListServiceDeployments returns every deployment of a service, newest release
// first. GET /v1/services/:id/deployments
//
// The list is assembled release by release. When reading a release's
// deployments fails, that release is skipped rather than failing the whole
// request, and the response says so: truncated is true and
// skipped_release_ids names the releases whose deployments are missing. A
// caller that needs the complete history (for example to choose a rollback
// target) must treat truncated=true as an error and retry. service_id,
// deployments and count are unchanged; truncated is always present and
// skipped_release_ids only when truncated.
func (h *Handler) ListServiceDeployments(c *gin.Context) {
	ctx := c.Request.Context()
	serviceID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid service ID"})
		return
	}

	if !h.enforceServiceAccess(c, serviceID) {
		return
	}

	releases, err := h.repos.Releases.ListByService(serviceID)
	if err != nil {
		h.logger.Error(ctx, "Failed to list releases", logging.Error("db_error", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to retrieve releases"})
		return
	}

	var allDeployments []*types.Deployment
	skipped := []string{}
	for _, release := range releases {
		deployments, err := h.repos.Deployments.ListByRelease(ctx, release.ID.String())
		if err != nil {
			h.logger.Error(ctx, "Failed to list deployments of a release; response marked truncated",
				logging.String("release_id", release.ID.String()),
				logging.Error("db_error", err))
			skipped = append(skipped, release.ID.String())
			continue
		}
		allDeployments = append(allDeployments, deployments...)
	}

	resp := gin.H{
		"service_id":  serviceID,
		"deployments": allDeployments,
		"count":       len(allDeployments),
		"truncated":   len(skipped) > 0,
	}
	if len(skipped) > 0 {
		resp["skipped_release_ids"] = skipped
	}
	c.JSON(http.StatusOK, resp)
}
