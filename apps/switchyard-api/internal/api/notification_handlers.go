package api

import (
	"database/sql"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/logging"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// CreateWebhookRequest defines the request body for creating a webhook destination
type CreateWebhookRequest struct {
	Name             string                   `json:"name" binding:"required"`
	Type             types.WebhookType        `json:"type" binding:"required"`
	WebhookURL       string                   `json:"webhook_url,omitempty"`
	TelegramBotToken string                   `json:"telegram_bot_token,omitempty"`
	TelegramChatID   string                   `json:"telegram_chat_id,omitempty"`
	Events           []types.WebhookEventType `json:"events" binding:"required,min=1"`
	CustomHeaders    map[string]string        `json:"custom_headers,omitempty"`
	SigningSecret    string                   `json:"signing_secret,omitempty"`
}

// UpdateWebhookRequest defines the request body for updating a webhook destination
type UpdateWebhookRequest struct {
	Name             *string                  `json:"name,omitempty"`
	WebhookURL       *string                  `json:"webhook_url,omitempty"`
	TelegramBotToken *string                  `json:"telegram_bot_token,omitempty"`
	TelegramChatID   *string                  `json:"telegram_chat_id,omitempty"`
	Events           []types.WebhookEventType `json:"events,omitempty"`
	Enabled          *bool                    `json:"enabled,omitempty"`
	CustomHeaders    map[string]string        `json:"custom_headers,omitempty"`
	SigningSecret    *string                  `json:"signing_secret,omitempty"`
}

// CreateWebhook creates a new webhook destination for a project
// POST /v1/projects/:slug/webhooks
func (h *Handler) CreateWebhook(c *gin.Context) {
	ctx := c.Request.Context()
	slug := c.Param("slug")

	// Get project by slug
	project, err := h.repos.Projects.GetBySlug(slug)
	if err != nil {
		if err == sql.ErrNoRows {
			c.JSON(http.StatusNotFound, gin.H{"error": "project not found"})
			return
		}
		h.logger.Error(ctx, "Failed to get project", logging.Error("error", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to get project"})
		return
	}

	// Parse request body
	var req CreateWebhookRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Validate webhook type
	switch req.Type {
	case types.WebhookTypeSlack, types.WebhookTypeDiscord:
		if req.WebhookURL == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "webhook_url is required for Slack/Discord webhooks"})
			return
		}
	case types.WebhookTypeTelegram:
		if req.TelegramBotToken == "" || req.TelegramChatID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "telegram_bot_token and telegram_chat_id are required for Telegram webhooks"})
			return
		}
	case types.WebhookTypeCustom:
		if req.WebhookURL == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "webhook_url is required for custom webhooks"})
			return
		}
	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid webhook type, must be one of: slack, discord, telegram, custom"})
		return
	}

	// Create the webhook destination
	webhook := &types.WebhookDestination{
		ProjectID:        project.ID,
		Name:             req.Name,
		Type:             req.Type,
		WebhookURL:       req.WebhookURL,
		TelegramBotToken: req.TelegramBotToken,
		TelegramChatID:   req.TelegramChatID,
		Events:           req.Events,
		Enabled:          true,
		CustomHeaders:    req.CustomHeaders,
		SigningSecret:    req.SigningSecret,
	}

	if err := h.repos.Webhooks.Create(ctx, webhook); err != nil {
		h.logger.Error(ctx, "Failed to create webhook", logging.Error("error", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create webhook"})
		return
	}

	// Clear sensitive fields before returning
	webhook.TelegramBotToken = ""
	webhook.SigningSecret = ""

	c.JSON(http.StatusCreated, gin.H{
		"webhook": webhook,
		"message": "Webhook created successfully",
	})
}

// ListWebhooks lists all webhook destinations for a project
// GET /v1/projects/:slug/webhooks
func (h *Handler) ListWebhooks(c *gin.Context) {
	ctx := c.Request.Context()
	slug := c.Param("slug")

	// Get project by slug
	project, err := h.repos.Projects.GetBySlug(slug)
	if err != nil {
		if err == sql.ErrNoRows {
			c.JSON(http.StatusNotFound, gin.H{"error": "project not found"})
			return
		}
		h.logger.Error(ctx, "Failed to get project", logging.Error("error", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to get project"})
		return
	}

	webhooks, err := h.repos.Webhooks.ListByProject(ctx, project.ID)
	if err != nil {
		h.logger.Error(ctx, "Failed to list webhooks", logging.Error("error", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list webhooks"})
		return
	}

	// Clear sensitive fields
	for i := range webhooks {
		webhooks[i].TelegramBotToken = ""
		webhooks[i].SigningSecret = ""
	}

	c.JSON(http.StatusOK, gin.H{"webhooks": webhooks})
}

// GetWebhook gets a specific webhook destination
// GET /v1/webhooks/:id
func (h *Handler) GetWebhook(c *gin.Context) {
	idStr := c.Param("id")

	id, err := uuid.Parse(idStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid webhook ID"})
		return
	}

	webhook, ok := h.loadWebhookWithAccess(c, id)
	if !ok {
		return
	}

	// Clear sensitive fields
	webhook.TelegramBotToken = ""
	webhook.SigningSecret = ""

	c.JSON(http.StatusOK, gin.H{"webhook": webhook})
}

// UpdateWebhook updates a webhook destination
// PATCH /v1/webhooks/:id
func (h *Handler) UpdateWebhook(c *gin.Context) {
	ctx := c.Request.Context()
	idStr := c.Param("id")

	id, err := uuid.Parse(idStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid webhook ID"})
		return
	}

	webhook, ok := h.loadWebhookWithAccess(c, id)
	if !ok {
		return
	}

	// Parse request body
	var req UpdateWebhookRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Apply updates
	if req.Name != nil {
		webhook.Name = *req.Name
	}
	if req.WebhookURL != nil {
		webhook.WebhookURL = *req.WebhookURL
	}
	if req.TelegramBotToken != nil {
		webhook.TelegramBotToken = *req.TelegramBotToken
	}
	if req.TelegramChatID != nil {
		webhook.TelegramChatID = *req.TelegramChatID
	}
	if len(req.Events) > 0 {
		webhook.Events = req.Events
	}
	if req.Enabled != nil {
		webhook.Enabled = *req.Enabled
		// Re-enable if explicitly enabled
		if *req.Enabled {
			webhook.AutoDisabledAt = nil
			webhook.ConsecutiveFailures = 0
		}
	}
	if req.CustomHeaders != nil {
		webhook.CustomHeaders = req.CustomHeaders
	}
	if req.SigningSecret != nil {
		webhook.SigningSecret = *req.SigningSecret
	}

	if err := h.repos.Webhooks.Update(ctx, webhook); err != nil {
		h.logger.Error(ctx, "Failed to update webhook", logging.Error("error", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update webhook"})
		return
	}

	// Clear sensitive fields
	webhook.TelegramBotToken = ""
	webhook.SigningSecret = ""

	c.JSON(http.StatusOK, gin.H{
		"webhook": webhook,
		"message": "Webhook updated successfully",
	})
}

// DeleteWebhook deletes a webhook destination
// DELETE /v1/webhooks/:id
func (h *Handler) DeleteWebhook(c *gin.Context) {
	ctx := c.Request.Context()
	idStr := c.Param("id")

	id, err := uuid.Parse(idStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid webhook ID"})
		return
	}

	if _, ok := h.loadWebhookWithAccess(c, id); !ok {
		return
	}

	if err := h.repos.Webhooks.Delete(ctx, id); err != nil {
		h.logger.Error(ctx, "Failed to delete webhook", logging.Error("error", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to delete webhook"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Webhook deleted successfully"})
}

// TestWebhook sends a test event to a webhook
// POST /v1/webhooks/:id/test
func (h *Handler) TestWebhook(c *gin.Context) {
	ctx := c.Request.Context()
	idStr := c.Param("id")

	id, err := uuid.Parse(idStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid webhook ID"})
		return
	}

	webhook, ok := h.loadWebhookWithAccess(c, id)
	if !ok {
		return
	}

	// Check if notification service is configured
	if h.notificationService == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "notification service not configured"})
		return
	}

	// Determine event type to test with
	eventType := types.WebhookEventDeploymentSucceeded
	if len(webhook.Events) > 0 {
		eventType = webhook.Events[0]
	}

	// Send test webhook
	if err := h.notificationService.TestWebhook(ctx, webhook, eventType); err != nil {
		h.logger.Error(ctx, "Failed to send test webhook", logging.Error("error", err))
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "test webhook failed",
			"details": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message":    "Test webhook sent successfully",
		"event_type": eventType,
	})
}

// ListWebhookDeliveries lists delivery history for a webhook
// GET /v1/webhooks/:id/deliveries
func (h *Handler) ListWebhookDeliveries(c *gin.Context) {
	ctx := c.Request.Context()
	idStr := c.Param("id")

	id, err := uuid.Parse(idStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid webhook ID"})
		return
	}

	if _, ok := h.loadWebhookWithAccess(c, id); !ok {
		return
	}

	// Get deliveries (limited to last 50)
	deliveries, err := h.repos.Webhooks.ListDeliveries(ctx, id, 50)
	if err != nil {
		h.logger.Error(ctx, "Failed to list deliveries", logging.Error("error", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list deliveries"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"deliveries": deliveries})
}

// RetryWebhookDelivery retries a failed webhook delivery
// POST /v1/webhooks/:id/deliveries/:delivery_id/retry
func (h *Handler) RetryWebhookDelivery(c *gin.Context) {
	ctx := c.Request.Context()
	webhookIDStr := c.Param("id")
	deliveryIDStr := c.Param("delivery_id")

	webhookID, err := uuid.Parse(webhookIDStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid webhook ID"})
		return
	}

	deliveryID, err := uuid.Parse(deliveryIDStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid delivery ID"})
		return
	}

	webhook, ok := h.loadWebhookWithAccess(c, webhookID)
	if !ok {
		return
	}

	// Get delivery
	delivery, err := h.repos.Webhooks.GetDelivery(ctx, deliveryID)
	if err != nil {
		if err == sql.ErrNoRows {
			c.JSON(http.StatusNotFound, gin.H{"error": "delivery not found"})
			return
		}
		h.logger.Error(ctx, "Failed to get delivery", logging.Error("error", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to get delivery"})
		return
	}

	// Verify delivery belongs to webhook
	if delivery.WebhookID != webhook.ID {
		c.JSON(http.StatusNotFound, gin.H{"error": "delivery not found for this webhook"})
		return
	}

	// Check if notification service is configured
	if h.notificationService == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "notification service not configured"})
		return
	}

	// Retry the delivery
	if err := h.notificationService.RetryDelivery(ctx, webhook, delivery); err != nil {
		h.logger.Error(ctx, "Failed to retry delivery", logging.Error("error", err))
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "retry failed",
			"details": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Delivery retry initiated"})
}

// GetWebhookEventTypes returns available webhook event types
// GET /v1/webhooks/event-types
func (h *Handler) GetWebhookEventTypes(c *gin.Context) {
	eventTypes := []struct {
		Type        types.WebhookEventType `json:"type"`
		Category    string                 `json:"category"`
		Description string                 `json:"description"`
	}{
		// Deployment events
		{types.WebhookEventDeploymentStarted, "deployment", "Deployment has started"},
		{types.WebhookEventDeploymentSucceeded, "deployment", "Deployment completed successfully"},
		{types.WebhookEventDeploymentFailed, "deployment", "Deployment failed"},
		{types.WebhookEventDeploymentCancelled, "deployment", "Deployment was cancelled"},
		// Build events
		{types.WebhookEventBuildStarted, "build", "Build has started"},
		{types.WebhookEventBuildSucceeded, "build", "Build completed successfully"},
		{types.WebhookEventBuildFailed, "build", "Build failed"},
		// Service events
		{types.WebhookEventServiceCreated, "service", "New service was created"},
		{types.WebhookEventServiceDeleted, "service", "Service was deleted"},
		{types.WebhookEventServiceStarted, "service", "Service started running"},
		{types.WebhookEventServiceStopped, "service", "Service was stopped"},
		{types.WebhookEventServiceUnhealthy, "service", "Service health check failed"},
		// Database events
		{types.WebhookEventDatabaseReady, "database", "Database is ready"},
		{types.WebhookEventDatabaseFailed, "database", "Database provisioning failed"},
	}

	c.JSON(http.StatusOK, gin.H{"event_types": eventTypes})
}
