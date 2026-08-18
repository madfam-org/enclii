package api

import (
	"database/sql"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/addons"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/logging"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/middleware"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// GetDataAPI returns the data-API (PostgREST) state for an addon.
// GET /v1/addons/:id/data-api
func (h *Handler) GetDataAPI(c *gin.Context) {
	ctx := c.Request.Context()
	if h.dataAPIService == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "data-API service not available"})
		return
	}

	addonUUID, ok := h.parseAddonID(c)
	if !ok {
		return
	}
	if _, ok := h.loadAddonWithAccess(c, addonUUID); !ok {
		return
	}

	api, err := h.dataAPIService.GetDataAPI(ctx, addonUUID)
	if err != nil {
		if err == sql.ErrNoRows {
			// Not an error — the addon simply has no data-API. Report disabled.
			c.JSON(http.StatusOK, gin.H{"enabled": false})
			return
		}
		h.logger.Error(ctx, "Failed to get data-API", logging.Error("error", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to get data-API"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"enabled":  api.Status != types.DataAPIStatusDisabled,
		"data_api": api,
	})
}

// EnableDataAPIRequest is the request body for enabling an addon's data-API.
type EnableDataAPIRequest struct {
	Schemas  string `json:"schemas,omitempty"`
	AnonRole string `json:"anon_role,omitempty"`
}

// EnableDataAPI enables the auto-generated REST API for a Postgres addon.
// POST /v1/addons/:id/data-api
func (h *Handler) EnableDataAPI(c *gin.Context) {
	ctx := c.Request.Context()
	if h.dataAPIService == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "data-API service not available"})
		return
	}

	addonUUID, ok := h.parseAddonID(c)
	if !ok {
		return
	}
	if _, ok := h.loadAddonWithAccess(c, addonUUID); !ok {
		return
	}

	var req EnableDataAPIRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		// Body is optional (defaults apply); only reject malformed JSON.
		if err.Error() != "EOF" {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
	}

	api, err := h.dataAPIService.EnableDataAPI(ctx, addons.EnableDataAPIRequest{
		AddonID:  addonUUID,
		Schemas:  req.Schemas,
		AnonRole: req.AnonRole,
		Actor:    addonActorFromContext(c),
	})
	if err != nil {
		msg := err.Error()
		switch {
		case strings.Contains(msg, "only supported for postgres"),
			strings.Contains(msg, "not ready"),
			strings.Contains(msg, "invalid schema"),
			strings.Contains(msg, "invalid anon role"):
			c.JSON(http.StatusBadRequest, gin.H{"error": msg})
			return
		case strings.Contains(msg, "addon not found"):
			c.JSON(http.StatusNotFound, gin.H{"error": msg})
			return
		}
		h.logger.Error(ctx, "Failed to enable data-API",
			logging.String("addon_id", addonUUID.String()),
			logging.Error("error", err))
		middleware.AbortInternal(c, err)
		return
	}

	h.logger.Info(ctx, "Data-API enable requested",
		logging.String("addon_id", addonUUID.String()),
		logging.String("host", api.Host))

	c.JSON(http.StatusAccepted, gin.H{
		"data_api": api,
		"message":  "Data-API enable requested; provisioning PostgREST",
	})
}

// DisableDataAPI disables an addon's data-API.
// DELETE /v1/addons/:id/data-api
func (h *Handler) DisableDataAPI(c *gin.Context) {
	ctx := c.Request.Context()
	if h.dataAPIService == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "data-API service not available"})
		return
	}

	addonUUID, ok := h.parseAddonID(c)
	if !ok {
		return
	}
	if _, ok := h.loadAddonWithAccess(c, addonUUID); !ok {
		return
	}

	if err := h.dataAPIService.DisableDataAPI(ctx, addonUUID, addonActorFromContext(c)); err != nil {
		if err == sql.ErrNoRows || strings.Contains(err.Error(), "data-API not found") {
			c.JSON(http.StatusNotFound, gin.H{"error": "data-API not found"})
			return
		}
		h.logger.Error(ctx, "Failed to disable data-API",
			logging.String("addon_id", addonUUID.String()),
			logging.Error("error", err))
		middleware.AbortInternal(c, err)
		return
	}

	h.logger.Info(ctx, "Data-API disable requested", logging.String("addon_id", addonUUID.String()))
	c.JSON(http.StatusOK, gin.H{"message": "Data-API disable requested"})
}

// MintDataAPITokenRequest is the request body to mint a JWT for the data-API.
type MintDataAPITokenRequest struct {
	Role       string            `json:"role,omitempty"`
	TTLSeconds int               `json:"ttl_seconds,omitempty"`
	Claims     map[string]string `json:"claims,omitempty"`
}

// MintDataAPIToken mints a short-lived HS256 JWT signed with the addon's
// data-API secret, for the tenant's app / testing.
// POST /v1/addons/:id/data-api/token
func (h *Handler) MintDataAPIToken(c *gin.Context) {
	ctx := c.Request.Context()
	if h.dataAPIService == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "data-API service not available"})
		return
	}

	addonUUID, ok := h.parseAddonID(c)
	if !ok {
		return
	}
	if _, ok := h.loadAddonWithAccess(c, addonUUID); !ok {
		return
	}

	var req MintDataAPITokenRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		if err.Error() != "EOF" {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
	}

	resp, err := h.dataAPIService.MintToken(ctx, addonUUID, types.DataAPITokenRequest{
		Role:       req.Role,
		TTLSeconds: req.TTLSeconds,
		Claims:     req.Claims,
	})
	if err != nil {
		msg := err.Error()
		switch {
		case strings.Contains(msg, "data-API not found"),
			strings.Contains(msg, "disabled"),
			strings.Contains(msg, "not provisioned"):
			c.JSON(http.StatusConflict, gin.H{"error": msg})
			return
		case strings.Contains(msg, "invalid role"):
			c.JSON(http.StatusBadRequest, gin.H{"error": msg})
			return
		}
		h.logger.Error(ctx, "Failed to mint data-API token",
			logging.String("addon_id", addonUUID.String()),
			logging.Error("error", err))
		middleware.AbortInternal(c, err)
		return
	}

	c.JSON(http.StatusOK, resp)
}

// parseAddonID parses and validates the :id path param, writing a 400 on
// failure. Local to the data-API handlers to avoid churning addon_handlers.go.
func (h *Handler) parseAddonID(c *gin.Context) (uuid.UUID, bool) {
	id := c.Param("id")
	parsed, err := uuid.Parse(id)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid addon_id format"})
		return uuid.Nil, false
	}
	return parsed, true
}
