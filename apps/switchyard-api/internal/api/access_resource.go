package api

import (
	"database/sql"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/db"
	apperrors "github.com/madfam-org/enclii/apps/switchyard-api/internal/errors"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/logging"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// mustServiceAccess parses :id as a service UUID and enforces project membership.
// Use on routes under /v1/services/:id/...
func (h *Handler) mustServiceAccess(c *gin.Context) (uuid.UUID, bool) {
	serviceID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		respondAppError(c, apperrors.ErrInvalidUUID.WithDetails(map[string]string{
			"parameter": "id",
			"value":     c.Param("id"),
		}))
		return uuid.Nil, false
	}
	if !h.enforceServiceAccess(c, serviceID) {
		return uuid.Nil, false
	}
	return serviceID, true
}

// enforceDeploymentAccess resolves a deployment to its service and enforces access.
func (h *Handler) enforceDeploymentAccess(c *gin.Context, deploymentID uuid.UUID) bool {
	ctx := c.Request.Context()
	deployment, err := h.repos.Deployments.GetByID(ctx, deploymentID.String())
	if err != nil {
		if err == sql.ErrNoRows {
			respondAppError(c, apperrors.ErrDeploymentNotFound)
		} else {
			h.logger.Error(ctx, "Failed to get deployment for access check", logging.Error("error", err))
			respondAppError(c, apperrors.ErrInternal.WithError(err))
		}
		return false
	}

	serviceID := uuid.Nil
	if deployment.ServiceID != nil {
		serviceID = *deployment.ServiceID
	} else {
		release, rErr := h.repos.Releases.GetByID(deployment.ReleaseID)
		if rErr != nil || release == nil {
			respondAppError(c, apperrors.ErrDeploymentNotFound)
			return false
		}
		serviceID = release.ServiceID
	}
	return h.enforceServiceAccess(c, serviceID)
}

// loadPreviewWithAccess returns a preview after enforcing project access.
func (h *Handler) loadPreviewWithAccess(c *gin.Context, previewID uuid.UUID) (*types.PreviewEnvironment, bool) {
	ctx := c.Request.Context()
	preview, err := h.repos.PreviewEnvironments.GetByID(ctx, previewID)
	if err != nil {
		if err == sql.ErrNoRows {
			respondAppError(c, apperrors.ErrNotFound)
		} else {
			h.logger.Error(ctx, "Failed to get preview for access check", logging.Error("error", err))
			respondAppError(c, apperrors.ErrInternal.WithError(err))
		}
		return nil, false
	}
	if !h.enforceUserProjectAccess(c, preview.ProjectID) {
		return nil, false
	}
	return preview, true
}

// loadFunctionWithAccess returns a function after enforcing project access.
func (h *Handler) loadFunctionWithAccess(c *gin.Context, functionID uuid.UUID) (*types.Function, bool) {
	ctx := c.Request.Context()
	fn, err := h.repos.Functions.GetByID(ctx, functionID)
	if err != nil {
		if err == sql.ErrNoRows {
			respondAppError(c, apperrors.ErrNotFound)
		} else {
			h.logger.Error(ctx, "Failed to get function for access check", logging.Error("error", err))
			respondAppError(c, apperrors.ErrInternal.WithError(err))
		}
		return nil, false
	}
	if !h.enforceUserProjectAccess(c, fn.ProjectID) {
		return nil, false
	}
	return fn, true
}

// loadEnvVarWithAccess loads an env var scoped to :id on the route and enforces access.
func (h *Handler) loadEnvVarWithAccess(c *gin.Context, evID uuid.UUID) (*types.EnvironmentVariable, bool) {
	svcID, ok := h.mustServiceAccess(c)
	if !ok {
		return nil, false
	}

	ctx := c.Request.Context()
	ev, err := h.repos.EnvVars.GetByID(ctx, evID)
	if err != nil {
		if err == sql.ErrNoRows {
			respondAppError(c, apperrors.ErrNotFound)
		} else {
			h.logger.Error(ctx, "Failed to get env var for access check", logging.Error("error", err))
			respondAppError(c, apperrors.ErrInternal.WithError(err))
		}
		return nil, false
	}
	if ev.ServiceID != svcID {
		respondAppError(c, apperrors.ErrNotFound)
		return nil, false
	}
	return ev, true
}

// loadAddonWithAccess returns an addon after enforcing project access.
func (h *Handler) loadAddonWithAccess(c *gin.Context, addonID uuid.UUID) (*types.DatabaseAddon, bool) {
	ctx := c.Request.Context()
	addon, err := h.addonService.GetAddon(ctx, addonID)
	if err != nil {
		if err == sql.ErrNoRows {
			respondAppError(c, apperrors.ErrNotFound)
		} else {
			h.logger.Error(ctx, "Failed to get addon for access check", logging.Error("error", err))
			respondAppError(c, apperrors.ErrInternal.WithError(err))
		}
		return nil, false
	}
	if !h.enforceUserProjectAccess(c, addon.ProjectID) {
		return nil, false
	}
	return addon, true
}

// loadWebhookWithAccess returns a notification webhook after project access check.
func (h *Handler) loadWebhookWithAccess(c *gin.Context, webhookID uuid.UUID) (*types.WebhookDestination, bool) {
	ctx := c.Request.Context()
	webhook, err := h.repos.Webhooks.GetByID(ctx, webhookID)
	if err != nil {
		if err == sql.ErrNoRows {
			respondAppError(c, apperrors.ErrNotFound)
		} else {
			h.logger.Error(ctx, "Failed to get webhook for access check", logging.Error("error", err))
			respondAppError(c, apperrors.ErrInternal.WithError(err))
		}
		return nil, false
	}
	if !h.enforceUserProjectAccess(c, webhook.ProjectID) {
		return nil, false
	}
	return webhook, true
}

// loadOutboundWebhookWithAccess returns a lifecycle webhook subscription after project access check.
func (h *Handler) loadOutboundWebhookWithAccess(c *gin.Context, subID uuid.UUID) (*types.OutboundWebhookSubscription, bool) {
	ctx := c.Request.Context()
	if h.repos.OutboundWebhooks == nil {
		respondAppError(c, apperrors.ErrInternal.WithDetails(map[string]string{"reason": "outbound webhooks not configured"}))
		return nil, false
	}
	sub, err := h.repos.OutboundWebhooks.GetSubscription(ctx, subID)
	if err != nil {
		if err == sql.ErrNoRows {
			respondAppError(c, apperrors.ErrNotFound)
		} else {
			h.logger.Error(ctx, "Failed to get outbound webhook for access check", logging.Error("error", err))
			respondAppError(c, apperrors.ErrInternal.WithError(err))
		}
		return nil, false
	}
	if !h.enforceUserProjectAccess(c, sub.ProjectID) {
		return nil, false
	}
	return sub, true
}

// loadDeploymentGroupWithSlugAccess loads a deployment group and ensures it belongs to :slug project.
func (h *Handler) loadDeploymentGroupWithSlugAccess(c *gin.Context, groupID uuid.UUID) (*db.DeploymentGroup, bool) {
	ctx := c.Request.Context()
	if h.repos.DeploymentGroups == nil {
		respondAppError(c, apperrors.ErrInternal.WithDetails(map[string]string{"reason": "deployment groups not configured"}))
		return nil, false
	}
	group, err := h.repos.DeploymentGroups.GetByID(ctx, groupID)
	if err != nil {
		if err == sql.ErrNoRows {
			respondAppError(c, apperrors.ErrNotFound)
		} else {
			h.logger.Error(ctx, "Failed to get deployment group for access check", logging.Error("error", err))
			respondAppError(c, apperrors.ErrInternal.WithError(err))
		}
		return nil, false
	}
	if !h.enforceUserProjectAccess(c, group.ProjectID) {
		return nil, false
	}
	if slug := c.Param("slug"); slug != "" {
		if raw, ok := c.Get("project_id"); ok {
			if projectID, ok := raw.(uuid.UUID); ok && group.ProjectID != projectID {
				respondAppError(c, apperrors.ErrNotFound)
				return nil, false
			}
		}
	}
	return group, true
}
