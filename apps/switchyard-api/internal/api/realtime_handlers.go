package api

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/logging"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/realtime"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// This file exposes the C2 realtime-subscriptions surface:
//
//	GET    /v1/projects/:slug/addons/:id/realtime            (WebSocket stream)
//	POST   /v1/addons/:id/realtime/tables                    (enable a table)
//	GET    /v1/addons/:id/realtime/tables                    (list enabled)
//	DELETE /v1/addons/:id/realtime/tables/:schema/:table     (disable a table)
//
// All routes are project-access gated (RequireProjectAccessBySlug for the WS
// route; loadAddonWithAccess for the addon-scoped routes). The heavy lifting
// lives in internal/realtime: the hub (LISTEN fan-out) and the manager (trigger
// install). See ADR-002.

// realtimeAddonResolver adapts the addon service into realtime.AddonResolver.
// It resolves an addon id to its live connection URI (the DSN the hub dials for
// LISTEN). Project-access authorization is enforced by the route BEFORE the WS
// handler calls Resolve, so this only checks existence + readiness.
type realtimeAddonResolver struct {
	handler *Handler
}

// Resolve implements realtime.AddonResolver.
func (r *realtimeAddonResolver) Resolve(ctx context.Context, addonID string) (*realtime.AddonConnInfo, error) {
	id, err := uuid.Parse(addonID)
	if err != nil {
		return nil, err
	}
	addon, err := r.handler.addonService.GetAddon(ctx, id)
	if err != nil {
		return nil, err
	}
	if addon.Status != types.DatabaseAddonStatusReady {
		return nil, realtime.ErrNotReady
	}
	creds, err := r.handler.addonService.GetCredentials(ctx, id)
	if err != nil {
		return nil, err
	}
	return &realtime.AddonConnInfo{
		Key: addon.ID.String(),
		DSN: creds.ConnectionURI,
	}, nil
}

// StreamAddonRealtime is the WebSocket entry point. The :slug project access
// and :id addon access are both enforced by middleware/helpers before we get
// here; we only guard the feature-enabled case and delegate.
//
// GET /v1/projects/:slug/addons/:id/realtime
func (h *Handler) StreamAddonRealtime(c *gin.Context) {
	addonUUID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid addon_id format"})
		return
	}
	// Re-check addon → project access (defense in depth alongside the slug
	// middleware; mirrors every other /addons/:id route).
	if _, ok := h.loadAddonWithAccess(c, addonUUID); !ok {
		return
	}
	if h.realtimeHandler == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error":  "realtime_not_configured",
			"detail": "Realtime subscriptions are not enabled for this deployment.",
		})
		return
	}
	h.realtimeHandler.Stream(c)
}

// realtimeTableRequest is the body for enabling a table.
type realtimeTableRequest struct {
	Schema string `json:"schema,omitempty"`
	Table  string `json:"table" binding:"required"`
}

// EnableAddonRealtimeTable installs the realtime trigger on a table.
//
// POST /v1/addons/:id/realtime/tables   { "schema": "public", "table": "orders" }
func (h *Handler) EnableAddonRealtimeTable(c *gin.Context) {
	ctx := c.Request.Context()
	addonUUID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid addon_id format"})
		return
	}
	if h.realtimeManager == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "realtime_not_configured"})
		return
	}
	if _, ok := h.loadAddonWithAccess(c, addonUUID); !ok {
		return
	}

	var req realtimeTableRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	ref := realtime.TableRef{Schema: req.Schema, Table: req.Table}.Normalize()
	if err := realtime.ValidateTableRef(ref); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	dsn, ok := h.resolveAddonDSN(c, addonUUID)
	if !ok {
		return
	}
	if err := h.realtimeManager.EnableTable(ctx, dsn, ref); err != nil {
		h.logger.Error(ctx, "Failed to enable realtime table",
			logging.String("addon_id", addonUUID.String()),
			logging.String("table", ref.Key()),
			logging.Error("error", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to enable realtime on table"})
		return
	}

	h.logger.Info(ctx, "Realtime enabled on table",
		logging.String("addon_id", addonUUID.String()),
		logging.String("table", ref.Key()))
	c.JSON(http.StatusCreated, gin.H{
		"message": "realtime enabled",
		"schema":  ref.Schema,
		"table":   ref.Table,
	})
}

// DisableAddonRealtimeTable removes the realtime trigger from a table.
//
// DELETE /v1/addons/:id/realtime/tables/:schema/:table
func (h *Handler) DisableAddonRealtimeTable(c *gin.Context) {
	ctx := c.Request.Context()
	addonUUID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid addon_id format"})
		return
	}
	if h.realtimeManager == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "realtime_not_configured"})
		return
	}
	if _, ok := h.loadAddonWithAccess(c, addonUUID); !ok {
		return
	}

	ref := realtime.TableRef{Schema: c.Param("schema"), Table: c.Param("table")}.Normalize()
	if err := realtime.ValidateTableRef(ref); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	dsn, ok := h.resolveAddonDSN(c, addonUUID)
	if !ok {
		return
	}
	if err := h.realtimeManager.DisableTable(ctx, dsn, ref); err != nil {
		h.logger.Error(ctx, "Failed to disable realtime table",
			logging.String("addon_id", addonUUID.String()),
			logging.String("table", ref.Key()),
			logging.Error("error", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to disable realtime on table"})
		return
	}

	h.logger.Info(ctx, "Realtime disabled on table",
		logging.String("addon_id", addonUUID.String()),
		logging.String("table", ref.Key()))
	c.JSON(http.StatusOK, gin.H{
		"message": "realtime disabled",
		"schema":  ref.Schema,
		"table":   ref.Table,
	})
}

// ListAddonRealtimeTables lists tables with realtime enabled.
//
// GET /v1/addons/:id/realtime/tables
func (h *Handler) ListAddonRealtimeTables(c *gin.Context) {
	ctx := c.Request.Context()
	addonUUID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid addon_id format"})
		return
	}
	if h.realtimeManager == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "realtime_not_configured"})
		return
	}
	if _, ok := h.loadAddonWithAccess(c, addonUUID); !ok {
		return
	}

	dsn, ok := h.resolveAddonDSN(c, addonUUID)
	if !ok {
		return
	}
	tables, err := h.realtimeManager.ListTables(ctx, dsn)
	if err != nil {
		h.logger.Error(ctx, "Failed to list realtime tables",
			logging.String("addon_id", addonUUID.String()),
			logging.Error("error", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list realtime tables"})
		return
	}

	// Normalize to a stable JSON shape ([] not null when empty).
	out := make([]gin.H, 0, len(tables))
	for _, t := range tables {
		out = append(out, gin.H{"schema": t.Schema, "table": t.Table})
	}
	c.JSON(http.StatusOK, gin.H{"tables": out, "count": len(out)})
}

// resolveAddonDSN loads an addon's connection URI for a trigger operation,
// writing the appropriate error response on failure. The addon must be ready.
func (h *Handler) resolveAddonDSN(c *gin.Context, addonID uuid.UUID) (string, bool) {
	ctx := c.Request.Context()
	addon, err := h.addonService.GetAddon(ctx, addonID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "addon not found"})
		return "", false
	}
	if addon.Status != types.DatabaseAddonStatusReady {
		c.JSON(http.StatusConflict, gin.H{"error": "addon_not_ready", "detail": "The addon is not ready."})
		return "", false
	}
	creds, err := h.addonService.GetCredentials(ctx, addonID)
	if err != nil {
		h.logger.Error(ctx, "Failed to get addon credentials for realtime",
			logging.String("addon_id", addonID.String()),
			logging.Error("error", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to resolve addon connection"})
		return "", false
	}
	if creds.ConnectionURI == "" {
		c.JSON(http.StatusConflict, gin.H{"error": "addon_no_connection", "detail": "The addon has no usable connection URI (discovered addons are unsupported for realtime)."})
		return "", false
	}
	return creds.ConnectionURI, true
}
