package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/middleware"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

func (h *Handler) ListPropagationPolicies(c *gin.Context) {
	if h.placementService == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "placement service not configured"})
		return
	}
	policies, err := h.repos.PropagationPolicies.List(c.Request.Context())
	if err != nil {
		middleware.AbortInternal(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"policies": policies})
}

func (h *Handler) GetPropagationPolicy(c *gin.Context) {
	if h.placementService == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "placement service not configured"})
		return
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid policy ID"})
		return
	}
	policy, err := h.repos.PropagationPolicies.GetByID(c.Request.Context(), id)
	if err != nil {
		middleware.AbortInternal(c, err)
		return
	}
	if policy == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "policy not found"})
		return
	}
	c.JSON(http.StatusOK, policy)
}

func (h *Handler) CreatePropagationPolicy(c *gin.Context) {
	if h.placementService == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "placement service not configured"})
		return
	}
	var req types.PropagationPolicy
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := h.placementService.CreatePolicy(c.Request.Context(), &req); err != nil {
		middleware.AbortInternal(c, err)
		return
	}
	c.JSON(http.StatusCreated, req)
}

func (h *Handler) DeletePropagationPolicy(c *gin.Context) {
	if h.placementService == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "placement service not configured"})
		return
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid policy ID"})
		return
	}
	if err := h.repos.PropagationPolicies.Delete(c.Request.Context(), id); err != nil {
		middleware.AbortInternal(c, err)
		return
	}
	c.JSON(http.StatusNoContent, nil)
}
