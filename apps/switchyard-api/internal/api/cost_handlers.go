package api

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

func (h *Handler) GetCostAllocations(c *gin.Context) {
	if h.costTrackingService == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "cost tracking service not configured"})
		return
	}
	tenantID := c.Query("tenant_id")
	start, _ := time.Parse(time.RFC3339, c.DefaultQuery("start", time.Now().AddDate(0, -1, 0).Format(time.RFC3339)))
	end, _ := time.Parse(time.RFC3339, c.DefaultQuery("end", time.Now().Format(time.RFC3339)))
	allocations, err := h.repos.CostAllocations.ListByTenant(c.Request.Context(), tenantID, start, end)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"allocations": allocations})
}

func (h *Handler) GetCostSummary(c *gin.Context) {
	if h.costTrackingService == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "cost tracking service not configured"})
		return
	}
	start, _ := time.Parse(time.RFC3339, c.DefaultQuery("start", time.Now().AddDate(0, -1, 0).Format(time.RFC3339)))
	end, _ := time.Parse(time.RFC3339, c.DefaultQuery("end", time.Now().Format(time.RFC3339)))
	summary, err := h.repos.CostAllocations.GetSummary(c.Request.Context(), start, end)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"summary": summary})
}

func (h *Handler) AllocateCost(c *gin.Context) {
	if h.costTrackingService == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "cost tracking service not configured"})
		return
	}
	var req types.CostAllocation
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := h.repos.CostAllocations.Create(c.Request.Context(), &req); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, req)
}
