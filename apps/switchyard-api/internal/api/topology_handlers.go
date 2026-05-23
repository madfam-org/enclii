package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/logging"
)

// GetTopology returns the complete service topology graph
func (h *Handler) GetTopology(c *gin.Context) {
	ctx := c.Request.Context()

	// Optional environment filter
	environment := c.DefaultQuery("environment", "all")

	h.logger.Info(ctx, "Building topology graph", logging.String("environment", environment))

	graph, err := h.topologyBuilder.BuildTopology(ctx, environment)
	if err != nil {
		h.logger.Error(ctx, "Failed to build topology", logging.Error("error", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to build topology graph"})
		return
	}

	c.JSON(http.StatusOK, graph)
}

// GetServiceDependencies returns upstream and downstream dependencies for a service
func (h *Handler) GetServiceDependencies(c *gin.Context) {
	ctx := c.Request.Context()
	serviceID, ok := h.mustServiceAccess(c)
	if !ok {
		return
	}
	idStr := serviceID.String()

	h.logger.Info(ctx, "Getting service dependencies", logging.String("service_id", idStr))

	deps, err := h.topologyBuilder.GetServiceDependencies(ctx, idStr)
	if err != nil {
		h.logger.Error(ctx, "Failed to get dependencies", logging.Error("error", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get service dependencies"})
		return
	}

	c.JSON(http.StatusOK, deps)
}

// GetServiceImpact returns impact analysis for a service
func (h *Handler) GetServiceImpact(c *gin.Context) {
	ctx := c.Request.Context()
	serviceID, ok := h.mustServiceAccess(c)
	if !ok {
		return
	}
	idStr := serviceID.String()

	h.logger.Info(ctx, "Analyzing service impact", logging.String("service_id", idStr))

	impact, err := h.topologyBuilder.AnalyzeImpact(ctx, idStr)
	if err != nil {
		h.logger.Error(ctx, "Failed to analyze impact", logging.Error("error", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to analyze service impact"})
		return
	}

	c.JSON(http.StatusOK, impact)
}

// FindDependencyPath finds a path between two services
func (h *Handler) FindDependencyPath(c *gin.Context) {
	ctx := c.Request.Context()

	sourceID := c.Query("source")
	targetID := c.Query("target")

	if sourceID == "" || targetID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Both source and target query parameters are required"})
		return
	}

	sourceUUID, err := uuid.Parse(sourceID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid source service ID"})
		return
	}
	targetUUID, err := uuid.Parse(targetID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid target service ID"})
		return
	}
	if !h.enforceServiceAccess(c, sourceUUID) {
		return
	}
	if !h.enforceServiceAccess(c, targetUUID) {
		return
	}

	h.logger.Info(ctx, "Finding dependency path",
		logging.String("source", sourceID),
		logging.String("target", targetID))

	path, err := h.topologyBuilder.FindPath(ctx, sourceID, targetID)
	if err != nil {
		h.logger.Error(ctx, "Failed to find path", logging.Error("error", err))
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, path)
}
