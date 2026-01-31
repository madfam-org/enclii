package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

func (h *Handler) ListVirtualClusters(c *gin.Context) {
	if h.vclusterService == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "vcluster service not configured"})
		return
	}
	vcs, err := h.repos.VirtualClusters.List(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"vclusters": vcs})
}

func (h *Handler) GetVirtualCluster(c *gin.Context) {
	if h.vclusterService == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "vcluster service not configured"})
		return
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid vcluster ID"})
		return
	}
	vc, err := h.repos.VirtualClusters.GetByID(c.Request.Context(), id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if vc == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "virtual cluster not found"})
		return
	}
	c.JSON(http.StatusOK, vc)
}

func (h *Handler) ProvisionVirtualCluster(c *gin.Context) {
	if h.vclusterService == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "vcluster service not configured"})
		return
	}
	var req types.VirtualCluster
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := h.vclusterService.Provision(c.Request.Context(), &req); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, req)
}

func (h *Handler) TeardownVirtualCluster(c *gin.Context) {
	if h.vclusterService == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "vcluster service not configured"})
		return
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid vcluster ID"})
		return
	}
	if err := h.vclusterService.Teardown(c.Request.Context(), id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusNoContent, nil)
}

func (h *Handler) GetVClusterKubeconfig(c *gin.Context) {
	if h.vclusterService == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "vcluster service not configured"})
		return
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid vcluster ID"})
		return
	}
	kubeconfig, err := h.vclusterService.GetKubeconfig(c.Request.Context(), id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"kubeconfig": kubeconfig})
}
