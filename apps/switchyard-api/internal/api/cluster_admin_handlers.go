package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// ListAdminClusters returns all registered clusters
func (h *Handler) ListAdminClusters(c *gin.Context) {
	if h.clusterAdminService == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "cluster admin service not configured"})
		return
	}
	clusters, err := h.repos.Clusters.List(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"clusters": clusters})
}

// GetAdminCluster returns a single cluster
func (h *Handler) GetAdminCluster(c *gin.Context) {
	if h.clusterAdminService == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "cluster admin service not configured"})
		return
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid cluster ID"})
		return
	}
	cluster, err := h.repos.Clusters.GetByID(c.Request.Context(), id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if cluster == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "cluster not found"})
		return
	}
	c.JSON(http.StatusOK, cluster)
}

// RegisterCluster registers a new cluster
func (h *Handler) RegisterCluster(c *gin.Context) {
	if h.clusterAdminService == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "cluster admin service not configured"})
		return
	}
	var req types.Cluster
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := h.clusterAdminService.Register(c.Request.Context(), &req); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, req)
}

// UpdateCluster updates a cluster
func (h *Handler) UpdateCluster(c *gin.Context) {
	if h.clusterAdminService == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "cluster admin service not configured"})
		return
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid cluster ID"})
		return
	}
	var req types.Cluster
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	req.ID = id
	if err := h.clusterAdminService.Update(c.Request.Context(), &req); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, req)
}

// DeregisterCluster removes a cluster
func (h *Handler) DeregisterCluster(c *gin.Context) {
	if h.clusterAdminService == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "cluster admin service not configured"})
		return
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid cluster ID"})
		return
	}
	if err := h.clusterAdminService.Deregister(c.Request.Context(), id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusNoContent, nil)
}
