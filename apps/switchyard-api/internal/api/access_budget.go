package api

import (
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	apperrors "github.com/madfam-org/enclii/apps/switchyard-api/internal/errors"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/logging"
)

// budgetEnvScope maps a deployment environment name to a waybill_throttles.env_scope value.
func budgetEnvScope(environmentName string) string {
	switch strings.ToLower(strings.TrimSpace(environmentName)) {
	case "production", "prod":
		return "production"
	default:
		return "non-production"
	}
}

// enforceBudgetNotThrottled blocks deploy/build when Waybill has written an active throttle row.
func (h *Handler) enforceBudgetNotThrottled(c *gin.Context, projectID uuid.UUID, environmentName string) bool {
	if h.repos == nil || h.repos.WaybillThrottles == nil {
		return true
	}
	ctx := c.Request.Context()
	scope := budgetEnvScope(environmentName)
	blocked, err := h.repos.WaybillThrottles.HasActive(ctx, projectID, scope)
	if err != nil {
		h.logger.Error(ctx, "Failed to check budget throttle", logging.Error("error", err))
		respondAppError(c, apperrors.ErrInternal.WithError(err))
		return false
	}
	if blocked {
		respondAppError(c, apperrors.ErrBudgetThrottled.WithDetails(map[string]string{
			"env_scope": scope,
		}))
		return false
	}
	return true
}
