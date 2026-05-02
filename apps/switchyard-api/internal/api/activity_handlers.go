package api

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/logging"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/middleware"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// ActivityListResponse represents the paginated activity response
type ActivityListResponse struct {
	Activities []*types.AuditLog `json:"activities"`
	Count      int               `json:"count"`
	Limit      int               `json:"limit"`
	Offset     int               `json:"offset"`
}

// GetActivity returns paginated audit logs for the activity feed
// GET /v1/activity
func (h *Handler) GetActivity(c *gin.Context) {
	ctx := c.Request.Context()

	// Parse query parameters
	limit := 50
	if l := c.Query("limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil && parsed > 0 && parsed <= 100 {
			limit = parsed
		}
	}

	offset := 0
	if o := c.Query("offset"); o != "" {
		if parsed, err := strconv.Atoi(o); err == nil && parsed >= 0 {
			offset = parsed
		}
	}

	// Build filters from query parameters
	filters := make(map[string]interface{})

	if action := c.Query("action"); action != "" {
		filters["action"] = action
	}

	if resourceType := c.Query("resource_type"); resourceType != "" {
		filters["resource_type"] = resourceType
	}

	if projectID := c.Query("project_id"); projectID != "" {
		if id, err := uuid.Parse(projectID); err == nil {
			filters["project_id"] = id
		}
	}

	if actorID := c.Query("actor_id"); actorID != "" {
		if id, err := uuid.Parse(actorID); err == nil {
			filters["actor_id"] = id
		}
	}

	// XC-2 Round 5: when the master admin is acting-as a tenant, scope the
	// activity feed to rows owned by that tenant — either rows whose
	// project_id resolves to the team, or rows emitted while a master
	// admin was acting-as that team (acting_on_behalf_of_team_id).
	var (
		logs []*types.AuditLog
		err  error
	)
	if teamID, ok := middleware.ActingTeamID(c); ok {
		logs, err = h.repos.AuditLogs.QueryByTeam(ctx, teamID, filters, limit, offset)
	} else {
		logs, err = h.repos.AuditLogs.Query(ctx, filters, limit, offset)
	}
	if err != nil {
		h.logger.Error(ctx, "Failed to query activity logs", logging.Error("error", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch activity"})
		return
	}

	c.JSON(http.StatusOK, ActivityListResponse{
		Activities: logs,
		Count:      len(logs),
		Limit:      limit,
		Offset:     offset,
	})
}

// GetActivityActions returns available action types for filtering
// GET /v1/activity/actions
func (h *Handler) GetActivityActions(c *gin.Context) {
	// Return common action types
	actions := []string{
		"create",
		"update",
		"delete",
		"deploy",
		"rollback",
		"build",
		"login",
		"logout",
		"invite",
		"join",
		"leave",
	}

	c.JSON(http.StatusOK, gin.H{"actions": actions})
}

// GetActivityResourceTypes returns available resource types for filtering
// GET /v1/activity/resource-types
func (h *Handler) GetActivityResourceTypes(c *gin.Context) {
	// Return common resource types
	resourceTypes := []string{
		"project",
		"service",
		"environment",
		"deployment",
		"release",
		"domain",
		"team",
		"user",
		"env_var",
		"preview",
	}

	c.JSON(http.StatusOK, gin.H{"resource_types": resourceTypes})
}
