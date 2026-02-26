package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/middleware"
)

func (h *Handler) ListDriftEvents(c *gin.Context) {
	if h.driftService == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "drift service not configured"})
		return
	}
	var resolved *bool
	if r := c.Query("resolved"); r != "" {
		v := r == "true"
		resolved = &v
	}
	events, err := h.repos.DriftEvents.List(c.Request.Context(), resolved)
	if err != nil {
		middleware.AbortInternal(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"events": events})
}

func (h *Handler) GetDriftEvent(c *gin.Context) {
	if h.driftService == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "drift service not configured"})
		return
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid drift event ID"})
		return
	}
	event, err := h.repos.DriftEvents.GetByID(c.Request.Context(), id)
	if err != nil {
		middleware.AbortInternal(c, err)
		return
	}
	if event == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "drift event not found"})
		return
	}
	c.JSON(http.StatusOK, event)
}

func (h *Handler) ResolveDrift(c *gin.Context) {
	if h.driftService == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "drift service not configured"})
		return
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid drift event ID"})
		return
	}
	if err := h.driftService.ResolveDrift(c.Request.Context(), id); err != nil {
		middleware.AbortInternal(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "drift resolved"})
}
