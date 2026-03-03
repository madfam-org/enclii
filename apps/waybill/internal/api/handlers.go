package api

import (
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/madfam-org/enclii/apps/waybill/internal/billing"
	"github.com/madfam-org/enclii/apps/waybill/internal/events"
	"go.uber.org/zap"
)

// Handlers contains all API handlers
type Handlers struct {
	collector  *events.Collector
	calculator *billing.Calculator
	stripe     *billing.StripeClient
	logger     *zap.Logger
}

// NewHandlers creates new API handlers
func NewHandlers(
	collector *events.Collector,
	calculator *billing.Calculator,
	stripe *billing.StripeClient,
	logger *zap.Logger,
) *Handlers {
	return &Handlers{
		collector:  collector,
		calculator: calculator,
		stripe:     stripe,
		logger:     logger,
	}
}

// RecordEvent handles internal event recording
func (h *Handlers) RecordEvent(c *gin.Context) {
	var req events.EventRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	event, err := h.collector.Record(c.Request.Context(), &req)
	if err != nil {
		h.logger.Error("failed to record event", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to record event"})
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"event_id": event.ID,
		"recorded": true,
	})
}

// RecordEventBatch handles batch event recording
func (h *Handlers) RecordEventBatch(c *gin.Context) {
	var req struct {
		Events []*events.EventRequest `json:"events" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if err := h.collector.RecordBatch(c.Request.Context(), req.Events); err != nil {
		h.logger.Error("failed to record events", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to record events"})
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"recorded": len(req.Events),
	})
}

// GetCurrentUsage returns current period usage for a project
func (h *Handlers) GetCurrentUsage(c *gin.Context) {
	projectID, err := uuid.Parse(c.Param("project_id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid project ID"})
		return
	}

	summary, err := h.calculator.GetCurrentPeriodUsage(c.Request.Context(), projectID)
	if err != nil {
		h.logger.Error("failed to get usage", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to get usage"})
		return
	}

	c.JSON(http.StatusOK, summary)
}

// GetUsageHistory returns historical usage for a project
func (h *Handlers) GetUsageHistory(c *gin.Context) {
	projectID, err := uuid.Parse(c.Param("project_id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid project ID"})
		return
	}

	months := 6 // Default
	if m := c.Query("months"); m != "" {
		if parsed, err := strconv.Atoi(m); err == nil && parsed > 0 && parsed <= 12 {
			months = parsed
		}
	}

	summaries, err := h.calculator.GetHistoricalUsage(c.Request.Context(), projectID, months)
	if err != nil {
		h.logger.Error("failed to get usage history", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to get usage history"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"project_id": projectID,
		"periods":    summaries,
	})
}

// EstimateCost returns a cost estimate for given specs
func (h *Handlers) EstimateCost(c *gin.Context) {
	var specs billing.ResourceSpecs
	if err := c.ShouldBindJSON(&specs); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	estimate := h.calculator.EstimateCost(&specs)
	c.JSON(http.StatusOK, estimate)
}

// GetInvoices lists invoices for a project
func (h *Handlers) GetInvoices(c *gin.Context) {
	projectID, err := uuid.Parse(c.Param("project_id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid project ID"})
		return
	}

	// Try to retrieve invoices from Stripe if configured
	if h.stripe != nil && h.stripe.IsEnabled() {
		customerID, err := h.stripe.GetCustomerIDForProject(c.Request.Context(), projectID.String())
		if err == nil && customerID != "" {
			invoices, err := h.stripe.ListInvoices(c.Request.Context(), customerID, 20)
			if err != nil {
				h.logger.Error("failed to list invoices from Stripe", zap.Error(err))
				c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list invoices"})
				return
			}
			c.JSON(http.StatusOK, gin.H{
				"project_id": projectID,
				"invoices":   invoices,
			})
			return
		}
	}

	// No Stripe customer for this project — return empty list
	c.JSON(http.StatusOK, gin.H{
		"project_id": projectID,
		"invoices":   []interface{}{},
	})
}

// GetPlans returns available pricing plans
func (h *Handlers) GetPlans(c *gin.Context) {
	plans := []gin.H{
		{
			"id":            "hobby",
			"name":          "Hobby",
			"description":   "For personal projects and experiments",
			"price_monthly": 5.0,
			"includes": gin.H{
				"compute_gb_hours": 500,
				"build_minutes":    500,
				"storage_gb":       1,
				"bandwidth_gb":     100,
				"custom_domains":   1,
			},
			"overage": gin.H{
				"compute_per_gb_hour": 0.000463,
				"build_per_minute":    0.01,
				"storage_per_gb":      0.25,
				"bandwidth_per_gb":    0.10,
			},
		},
		{
			"id":            "pro",
			"name":          "Pro",
			"description":   "For production applications",
			"price_monthly": 20.0,
			"includes": gin.H{
				"compute_gb_hours": 2000,
				"build_minutes":    2000,
				"storage_gb":       10,
				"bandwidth_gb":     500,
				"custom_domains":   "unlimited",
			},
			"overage": gin.H{
				"compute_per_gb_hour": 0.000463,
				"build_per_minute":    0.01,
				"storage_per_gb":      0.20,
				"bandwidth_per_gb":    0.08,
			},
			"features": []string{
				"Priority support",
				"Team collaboration",
				"Advanced metrics",
				"Custom health checks",
			},
		},
		{
			"id":            "team",
			"name":          "Team",
			"description":   "For teams and organizations",
			"price_monthly": 50.0,
			"includes": gin.H{
				"compute_gb_hours": 5000,
				"build_minutes":    5000,
				"storage_gb":       50,
				"bandwidth_gb":     1000,
				"custom_domains":   "unlimited",
				"team_members":     10,
			},
			"overage": gin.H{
				"compute_per_gb_hour": 0.0004,
				"build_per_minute":    0.008,
				"storage_per_gb":      0.15,
				"bandwidth_per_gb":    0.05,
			},
			"features": []string{
				"Everything in Pro",
				"SSO/SAML",
				"Audit logs",
				"SLA guarantee",
				"Dedicated support",
			},
		},
	}

	c.JSON(http.StatusOK, gin.H{"plans": plans})
}

// HealthCheck returns service health
func (h *Handlers) HealthCheck(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"status":  "healthy",
		"service": "waybill",
		"time":    time.Now().UTC(),
	})
}
