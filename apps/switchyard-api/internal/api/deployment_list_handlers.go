package api

// Split-out from deployment_handlers.go to keep that file under the
// repo-wide 800-line cap. This file holds the global cross-service deployment
// listing (`GET /v1/deployments`) and its XC-2 Round 5 tenant-filter dispatch.
// Per-deployment detail endpoints + per-service deployment lists remain in
// deployment_handlers.go because they reuse the local helpers there.

import (
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

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

	limit := 50
	if limitStr := c.Query("limit"); limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 && l <= 100 {
			limit = l
		}
	}

	var (
		deployments []*types.DeploymentEnriched
		err         error
	)
	if teamID, ok := middleware.ActingTeamID(c); ok {
		deployments, err = h.repos.Deployments.ListAllEnrichedByTeam(ctx, teamID, since, limit)
	} else {
		deployments, err = h.repos.Deployments.ListAllEnriched(ctx, since, limit)
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
