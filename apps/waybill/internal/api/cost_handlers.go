package api

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/madfam-org/enclii/apps/waybill/internal/budgets"
)

// CostHandlers exposes the project/service cost + budget CRUD endpoints.
type CostHandlers struct {
	store  *budgets.Store
	cost   *budgets.CostReader
	logger *zap.Logger
}

// NewCostHandlers constructs the cost + budget handler group.
func NewCostHandlers(store *budgets.Store, cost *budgets.CostReader, logger *zap.Logger) *CostHandlers {
	return &CostHandlers{store: store, cost: cost, logger: logger}
}

// Register mounts the cost + budgets routes under /api/v1.
func (h *CostHandlers) Register(g *gin.RouterGroup) {
	g.GET("/projects/:project_id/cost", h.getProjectCost)
	g.GET("/services/:service_id/cost", h.getServiceCost)
	g.GET("/projects/:project_id/budgets", h.listBudgets)
	g.POST("/projects/:project_id/budgets", h.createBudget)
	g.PATCH("/projects/:project_id/budgets/:budget_id", h.updateBudget)
	g.DELETE("/projects/:project_id/budgets/:budget_id", h.deleteBudget)
	g.GET("/projects/:project_id/budgets/alerts", h.listAlertEvents)
	g.GET("/projects/:project_id/throttles", h.listThrottles)
}

// GET /projects/:project_id/cost?period=30d&group_by=service|env|metric
func (h *CostHandlers) getProjectCost(c *gin.Context) {
	projectID, err := uuid.Parse(c.Param("project_id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid project_id"})
		return
	}
	start, end, ok := parsePeriod(c)
	if !ok {
		return
	}
	groupBy := strings.ToLower(c.DefaultQuery("group_by", "metric"))

	total, err := h.cost.ProjectCost(c.Request.Context(), projectID, start, end)
	if err != nil {
		h.logger.Error("project cost failed", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to query cost"})
		return
	}

	var series []budgets.TimeSeriesPoint
	var breakdown []budgets.BreakdownEntry

	if groupBy == "day" {
		series, err = h.cost.ProjectSeries(c.Request.Context(), projectID, start, end)
	} else {
		breakdown, err = h.cost.ProjectBreakdown(c.Request.Context(), projectID, start, end, groupBy)
	}
	if err != nil {
		h.logger.Error("project breakdown failed", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to query breakdown"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"project_id":   projectID,
		"period_start": start,
		"period_end":   end,
		"total_cents":  total,
		"group_by":     groupBy,
		"series":       series,
		"breakdown":    breakdown,
	})
}

// GET /services/:service_id/cost?period=30d
//
// We join through usage_events to restrict to events where resource_id matches.
// Only rough cost info is exposed here — precise per-service billing is
// deliberately out of scope for P2.2 (see RFC in roadmap).
func (h *CostHandlers) getServiceCost(c *gin.Context) {
	serviceID, err := uuid.Parse(c.Param("service_id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid service_id"})
		return
	}
	start, end, ok := parsePeriod(c)
	if !ok {
		return
	}
	// Delegate to a method that scopes by resource_id.
	cents, err := h.cost.ServiceCost(c.Request.Context(), serviceID, start, end)
	if err != nil {
		h.logger.Error("service cost failed", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to query service cost"})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"service_id":   serviceID,
		"period_start": start,
		"period_end":   end,
		"total_cents":  cents,
	})
}

func (h *CostHandlers) listBudgets(c *gin.Context) {
	projectID, err := uuid.Parse(c.Param("project_id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid project_id"})
		return
	}
	items, err := h.store.ListByProject(c.Request.Context(), projectID)
	if err != nil {
		h.logger.Error("list budgets", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list budgets"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"budgets": items})
}

func (h *CostHandlers) createBudget(c *gin.Context) {
	projectID, err := uuid.Parse(c.Param("project_id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid project_id"})
		return
	}
	var req budgets.CreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if !req.Period.IsValid() {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid period"})
		return
	}
	b, err := h.store.Create(c.Request.Context(), projectID, req)
	if err != nil {
		if errors.Is(err, budgets.ErrAlreadyExists) {
			c.JSON(http.StatusConflict, gin.H{"error": "budget already exists for this period"})
			return
		}
		h.logger.Error("create budget", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create budget"})
		return
	}
	c.JSON(http.StatusCreated, b)
}

func (h *CostHandlers) updateBudget(c *gin.Context) {
	budgetID, err := uuid.Parse(c.Param("budget_id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid budget_id"})
		return
	}
	var req budgets.UpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	b, err := h.store.Update(c.Request.Context(), budgetID, req)
	if err != nil {
		if errors.Is(err, budgets.ErrNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "budget not found"})
			return
		}
		h.logger.Error("update budget", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update budget"})
		return
	}
	c.JSON(http.StatusOK, b)
}

func (h *CostHandlers) deleteBudget(c *gin.Context) {
	budgetID, err := uuid.Parse(c.Param("budget_id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid budget_id"})
		return
	}
	if err := h.store.Delete(c.Request.Context(), budgetID); err != nil {
		if errors.Is(err, budgets.ErrNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "budget not found"})
			return
		}
		h.logger.Error("delete budget", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to delete budget"})
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *CostHandlers) listAlertEvents(c *gin.Context) {
	projectID, err := uuid.Parse(c.Param("project_id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid project_id"})
		return
	}
	limit := 30
	items, err := h.store.ListRecentAlerts(c.Request.Context(), projectID, limit)
	if err != nil {
		h.logger.Error("list alerts", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list alerts"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"alerts": items})
}

func (h *CostHandlers) listThrottles(c *gin.Context) {
	projectID, err := uuid.Parse(c.Param("project_id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid project_id"})
		return
	}
	items, err := h.store.ListActiveThrottles(c.Request.Context(), projectID)
	if err != nil {
		h.logger.Error("list throttles", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list throttles"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"throttles": items})
}

// parsePeriod accepts the `period` query parameter (30d, 90d, 1y, 14d) and
// returns a [start, end) time range. On error it writes a 400 and returns ok=false.
func parsePeriod(c *gin.Context) (time.Time, time.Time, bool) {
	period := strings.ToLower(c.DefaultQuery("period", "30d"))
	now := time.Now().UTC()
	var start time.Time
	switch period {
	case "7d":
		start = now.AddDate(0, 0, -7)
	case "14d":
		start = now.AddDate(0, 0, -14)
	case "30d":
		start = now.AddDate(0, 0, -30)
	case "90d":
		start = now.AddDate(0, 0, -90)
	case "1y":
		start = now.AddDate(-1, 0, 0)
	case "mtd":
		start = time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid period; use 7d|14d|30d|90d|1y|mtd"})
		return time.Time{}, time.Time{}, false
	}
	return start, now, true
}
