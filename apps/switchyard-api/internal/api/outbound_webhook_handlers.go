package api

import (
	"database/sql"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/logging"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/webhooks"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// Outbound lifecycle webhooks (P2.3). Customer-configured HTTPS
// subscriptions that receive signed deploy/rollback/scale events.
//
// Routes:
//   POST   /v1/projects/:slug/lifecycle-webhooks
//   GET    /v1/projects/:slug/lifecycle-webhooks
//   GET    /v1/lifecycle-webhooks/:sub_id
//   PATCH  /v1/lifecycle-webhooks/:sub_id
//   DELETE /v1/lifecycle-webhooks/:sub_id
//   POST   /v1/lifecycle-webhooks/:sub_id/rotate-secret
//   POST   /v1/lifecycle-webhooks/:sub_id/test
//   GET    /v1/lifecycle-webhooks/:sub_id/deliveries
//   POST   /v1/lifecycle-webhooks/:sub_id/deliveries/:delivery_id/redeliver
//   GET    /v1/lifecycle-webhooks/event-types

const (
	outboundSecretDisplayNote = "Signing secret — save this now. It will never be shown again. " +
		"Use it to verify the X-Enclii-Signature header on incoming webhook POSTs."
)

// CreateOutboundWebhook POST /v1/projects/:slug/lifecycle-webhooks
func (h *Handler) CreateOutboundWebhook(c *gin.Context) {
	ctx := c.Request.Context()
	if h.webhookDispatcher == nil || h.webhookEncryptor == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "outbound webhooks not configured"})
		return
	}

	projectID, ok := h.lookupProjectID(c)
	if !ok {
		return
	}

	var req types.OutboundWebhookSubscriptionCreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if errMsg := validateOutboundCreate(&req); errMsg != "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": errMsg})
		return
	}

	plaintext, prefix, err := webhooks.GenerateSigningSecret()
	if err != nil {
		h.logger.Error(ctx, "generate signing secret failed", logging.Error("error", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "secret generation failed"})
		return
	}
	encrypted, err := h.webhookEncryptor.EncryptString(plaintext)
	if err != nil {
		h.logger.Error(ctx, "encrypt signing secret failed", logging.Error("error", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "secret encryption failed"})
		return
	}

	sub := &types.OutboundWebhookSubscription{
		ProjectID:          projectID,
		Name:               req.Name,
		URL:                req.URL,
		SecretSHA256Prefix: prefix,
		EventTypes:         req.EventTypes,
		Active:             true,
		CreatedBy:          c.GetString("user_email"),
	}
	if err := h.repos.OutboundWebhooks.CreateSubscription(ctx, sub, encrypted); err != nil {
		h.logger.Error(ctx, "create outbound subscription failed", logging.Error("error", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create subscription"})
		return
	}

	c.JSON(http.StatusCreated, &types.OutboundWebhookSubscriptionCreateResponse{
		Subscription:  *sub,
		SigningSecret: plaintext,
		Note:          outboundSecretDisplayNote,
	})
}

// ListOutboundWebhooks GET /v1/projects/:slug/lifecycle-webhooks
func (h *Handler) ListOutboundWebhooks(c *gin.Context) {
	ctx := c.Request.Context()
	if h.repos.OutboundWebhooks == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "outbound webhooks not configured"})
		return
	}
	projectID, ok := h.lookupProjectID(c)
	if !ok {
		return
	}
	subs, err := h.repos.OutboundWebhooks.ListSubscriptionsByProject(ctx, projectID)
	if err != nil {
		h.logger.Error(ctx, "list subscriptions failed", logging.Error("error", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list subscriptions"})
		return
	}
	if subs == nil {
		subs = []*types.OutboundWebhookSubscription{}
	}
	c.JSON(http.StatusOK, gin.H{"subscriptions": subs})
}

// GetOutboundWebhook GET /v1/lifecycle-webhooks/:sub_id
func (h *Handler) GetOutboundWebhook(c *gin.Context) {
	id, ok := h.parseSubID(c)
	if !ok {
		return
	}
	sub, ok := h.loadOutboundWebhookWithAccess(c, id)
	if !ok {
		return
	}
	c.JSON(http.StatusOK, sub)
}

// UpdateOutboundWebhook PATCH /v1/lifecycle-webhooks/:sub_id
func (h *Handler) UpdateOutboundWebhook(c *gin.Context) {
	ctx := c.Request.Context()
	id, ok := h.parseSubID(c)
	if !ok {
		return
	}
	if _, ok := h.loadOutboundWebhookWithAccess(c, id); !ok {
		return
	}

	var req types.OutboundWebhookSubscriptionUpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if req.URL != nil {
		if err := validateHTTPS(*req.URL); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
	}
	if req.EventTypes != nil {
		if errMsg := validateEventTypes(*req.EventTypes); errMsg != "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": errMsg})
			return
		}
	}

	if err := h.repos.OutboundWebhooks.UpdateSubscription(ctx, id, &req); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			c.JSON(http.StatusNotFound, gin.H{"error": "subscription not found"})
			return
		}
		h.logger.Error(ctx, "update subscription failed", logging.Error("error", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update subscription"})
		return
	}

	sub, err := h.repos.OutboundWebhooks.GetSubscription(ctx, id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to reread subscription"})
		return
	}
	c.JSON(http.StatusOK, sub)
}

// DeleteOutboundWebhook DELETE /v1/lifecycle-webhooks/:sub_id
func (h *Handler) DeleteOutboundWebhook(c *gin.Context) {
	ctx := c.Request.Context()
	id, ok := h.parseSubID(c)
	if !ok {
		return
	}
	if _, ok := h.loadOutboundWebhookWithAccess(c, id); !ok {
		return
	}
	if err := h.repos.OutboundWebhooks.DeleteSubscription(ctx, id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			c.JSON(http.StatusNotFound, gin.H{"error": "subscription not found"})
			return
		}
		h.logger.Error(ctx, "delete subscription failed", logging.Error("error", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to delete subscription"})
		return
	}
	c.JSON(http.StatusNoContent, nil)
}

// RotateOutboundWebhookSecret POST /v1/lifecycle-webhooks/:sub_id/rotate-secret
func (h *Handler) RotateOutboundWebhookSecret(c *gin.Context) {
	ctx := c.Request.Context()
	if h.webhookEncryptor == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "outbound webhooks not configured"})
		return
	}
	id, ok := h.parseSubID(c)
	if !ok {
		return
	}

	sub, ok := h.loadOutboundWebhookWithAccess(c, id)
	if !ok {
		return
	}

	plaintext, prefix, err := webhooks.GenerateSigningSecret()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "secret generation failed"})
		return
	}
	encrypted, err := h.webhookEncryptor.EncryptString(plaintext)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "secret encryption failed"})
		return
	}
	if err := h.repos.OutboundWebhooks.RotateSubscriptionSecret(ctx, id, prefix, encrypted); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "rotate failed"})
		return
	}
	sub.SecretSHA256Prefix = prefix
	sub.UpdatedAt = time.Now().UTC()

	c.JSON(http.StatusOK, &types.OutboundWebhookSubscriptionCreateResponse{
		Subscription:  *sub,
		SigningSecret: plaintext,
		Note:          outboundSecretDisplayNote,
	})
}

// TestOutboundWebhook POST /v1/lifecycle-webhooks/:sub_id/test
// Dispatches a synthetic test.ping event. Returns 202 with the
// delivery row so the caller can poll /deliveries to see the result.
func (h *Handler) TestOutboundWebhook(c *gin.Context) {
	ctx := c.Request.Context()
	if h.webhookDispatcher == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "outbound webhooks not configured"})
		return
	}
	id, ok := h.parseSubID(c)
	if !ok {
		return
	}
	sub, ok := h.loadOutboundWebhookWithAccess(c, id)
	if !ok {
		return
	}

	// test.ping is a control-plane event: build the envelope directly
	// and create a single delivery row so the worker picks it up on
	// its next tick.
	env, payloadBytes, sha, err := webhooks.BuildEnvelope(
		types.OutboundEventTestPing,
		map[string]any{
			"subscription_id": sub.ID.String(),
			"project_id":      sub.ProjectID.String(),
			"actor":           c.GetString("user_email"),
			"triggered_at":    time.Now().UTC().Format(time.RFC3339),
		},
		time.Now().UTC(),
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "envelope build failed"})
		return
	}
	if len(payloadBytes) > types.OutboundWebhookMaxPayloadBytes {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "test envelope too large"})
		return
	}
	now := time.Now().UTC()
	delivery := &types.OutboundWebhookDelivery{
		SubscriptionID: sub.ID,
		EventID:        env.ID,
		EventType:      types.OutboundEventTestPing,
		Payload:        env.Data,
		PayloadSHA256:  sha,
		AttemptNumber:  1,
		Status:         types.OutboundDeliveryPending,
		NextRetryAt:    &now,
	}
	if err := h.repos.OutboundWebhooks.CreateDelivery(ctx, delivery); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "enqueue failed"})
		return
	}
	c.JSON(http.StatusAccepted, gin.H{"delivery": delivery})
}

// ListOutboundWebhookDeliveries GET /v1/lifecycle-webhooks/:sub_id/deliveries
func (h *Handler) ListOutboundWebhookDeliveries(c *gin.Context) {
	ctx := c.Request.Context()
	id, ok := h.parseSubID(c)
	if !ok {
		return
	}
	if _, ok := h.loadOutboundWebhookWithAccess(c, id); !ok {
		return
	}
	limit := 50
	if q := c.Query("limit"); q != "" {
		if n, err := strconv.Atoi(q); err == nil && n > 0 && n <= 200 {
			limit = n
		}
	}
	offset := 0
	if q := c.Query("offset"); q != "" {
		if n, err := strconv.Atoi(q); err == nil && n >= 0 {
			offset = n
		}
	}
	deliveries, err := h.repos.OutboundWebhooks.ListDeliveriesBySubscription(ctx, id, limit, offset)
	if err != nil {
		h.logger.Error(ctx, "list deliveries failed", logging.Error("error", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list deliveries"})
		return
	}
	if deliveries == nil {
		deliveries = []*types.OutboundWebhookDelivery{}
	}
	c.JSON(http.StatusOK, gin.H{"deliveries": deliveries, "limit": limit, "offset": offset})
}

// RedeliverOutboundWebhook POST /v1/lifecycle-webhooks/:sub_id/deliveries/:delivery_id/redeliver
func (h *Handler) RedeliverOutboundWebhook(c *gin.Context) {
	ctx := c.Request.Context()
	subID, ok := h.parseSubID(c)
	if !ok {
		return
	}
	if _, ok := h.loadOutboundWebhookWithAccess(c, subID); !ok {
		return
	}
	deliveryID, err := uuid.Parse(c.Param("delivery_id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid delivery id"})
		return
	}
	clone, err := webhooks.Redeliver(ctx, h.repos, deliveryID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			c.JSON(http.StatusNotFound, gin.H{"error": "delivery not found"})
			return
		}
		h.logger.Error(ctx, "redeliver failed", logging.Error("error", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "redeliver failed"})
		return
	}
	c.JSON(http.StatusAccepted, gin.H{"delivery": clone})
}

// GetOutboundWebhookEventTypes GET /v1/lifecycle-webhooks/event-types
func (h *Handler) GetOutboundWebhookEventTypes(c *gin.Context) {
	out := make([]gin.H, 0, len(types.AllOutboundWebhookEventTypes()))
	for _, et := range types.AllOutboundWebhookEventTypes() {
		out = append(out, gin.H{
			"type":        string(et),
			"description": outboundEventDescription(et),
		})
	}
	c.JSON(http.StatusOK, gin.H{"event_types": out})
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// lookupProjectID resolves a :slug URL parameter to a project UUID and
// writes an appropriate error response if the project is missing.
func (h *Handler) lookupProjectID(c *gin.Context) (uuid.UUID, bool) {
	slug := c.Param("slug")
	p, err := h.repos.Projects.GetBySlug(slug)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			c.JSON(http.StatusNotFound, gin.H{"error": "project not found"})
			return uuid.Nil, false
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load project"})
		return uuid.Nil, false
	}
	return p.ID, true
}

func (h *Handler) parseSubID(c *gin.Context) (uuid.UUID, bool) {
	id, err := uuid.Parse(c.Param("sub_id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid subscription id"})
		return uuid.Nil, false
	}
	return id, true
}

func validateOutboundCreate(req *types.OutboundWebhookSubscriptionCreateRequest) string {
	if strings.TrimSpace(req.Name) == "" {
		return "name is required"
	}
	if err := validateHTTPS(req.URL); err != nil {
		return err.Error()
	}
	if msg := validateEventTypes(req.EventTypes); msg != "" {
		return msg
	}
	return ""
}

func validateHTTPS(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return errors.New("url is not parseable")
	}
	if u.Scheme != "https" {
		return errors.New("url must be https")
	}
	if u.Host == "" {
		return errors.New("url must include a host")
	}
	return nil
}

func validateEventTypes(xs []types.OutboundWebhookEventType) string {
	// An empty array is valid — it means "subscribe to every event".
	for _, e := range xs {
		if !types.IsValidOutboundEventType(string(e)) {
			return "unknown event type: " + string(e)
		}
	}
	return ""
}

func outboundEventDescription(et types.OutboundWebhookEventType) string {
	switch et {
	case types.OutboundEventDeployStarted:
		return "A deployment has begun for a service."
	case types.OutboundEventDeploySucceeded:
		return "A deployment has finished successfully and is healthy."
	case types.OutboundEventDeployFailed:
		return "A deployment has failed or is degraded."
	case types.OutboundEventRollbackSuccess:
		return "A service was rolled back to a previous release."
	case types.OutboundEventSecretRotated:
		return "A secret attached to a service was rotated."
	case types.OutboundEventServiceScaled:
		return "A service's replica count was changed."
	default:
		return ""
	}
}
