package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

func (h *Handler) ListManagedResources(c *gin.Context) {
	if h.infrastructureService == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "infrastructure service not configured"})
		return
	}
	provider := c.Query("provider")
	kind := c.Query("kind")
	status := types.SyncStatus(c.Query("status"))
	resources, err := h.repos.ManagedResources.List(c.Request.Context(), provider, kind, status)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"resources": resources})
}

func (h *Handler) GetManagedResource(c *gin.Context) {
	if h.infrastructureService == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "infrastructure service not configured"})
		return
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid resource ID"})
		return
	}
	resource, err := h.repos.ManagedResources.GetByID(c.Request.Context(), id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if resource == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "resource not found"})
		return
	}
	c.JSON(http.StatusOK, resource)
}

func (h *Handler) CreateManagedResource(c *gin.Context) {
	if h.infrastructureService == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "infrastructure service not configured"})
		return
	}
	var req types.ManagedResource
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := h.infrastructureService.CreateComposition(c.Request.Context(), &req); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, req)
}

func (h *Handler) UpdateResourcePolicy(c *gin.Context) {
	if h.infrastructureService == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "infrastructure service not configured"})
		return
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid resource ID"})
		return
	}
	var req struct {
		Policy types.ManagementPolicy `json:"management_policy" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := h.infrastructureService.SwitchPolicy(c.Request.Context(), id, req.Policy); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "policy updated"})
}

func (h *Handler) DeleteManagedResource(c *gin.Context) {
	if h.infrastructureService == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "infrastructure service not configured"})
		return
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid resource ID"})
		return
	}
	if err := h.repos.ManagedResources.Delete(c.Request.Context(), id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusNoContent, nil)
}
