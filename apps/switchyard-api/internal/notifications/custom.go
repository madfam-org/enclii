package notifications

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// CustomSender handles sending notifications to custom webhook URLs
type CustomSender struct {
	client        *http.Client
	logger        *logrus.Logger
	customHeaders map[string]string
	signingSecret string
}

// NewCustomSender creates a new custom webhook sender
func NewCustomSender(logger *logrus.Logger, customHeaders map[string]string, signingSecret string) *CustomSender {
	return &CustomSender{
		client:        &http.Client{Timeout: 30 * time.Second},
		logger:        logger,
		customHeaders: customHeaders,
		signingSecret: signingSecret,
	}
}

// CustomWebhookPayload represents the payload sent to custom webhooks
type CustomWebhookPayload struct {
	ID         string                       `json:"id"`
	Type       types.WebhookEventType       `json:"type"`
	Timestamp  time.Time                    `json:"timestamp"`
	Project    types.WebhookProjectInfo     `json:"project"`
	Deployment *types.WebhookDeploymentInfo `json:"deployment,omitempty"`
	Build      *types.WebhookBuildInfo      `json:"build,omitempty"`
	Service    *types.WebhookServiceInfo    `json:"service,omitempty"`
	Database   *types.WebhookDatabaseInfo   `json:"database,omitempty"`
}

// Send sends an event to a custom webhook URL
func (c *CustomSender) Send(ctx context.Context, webhookURL string, event *types.WebhookEvent) (int, error) {
	payload := c.buildPayload(event)

	body, err := json.Marshal(payload)
	if err != nil {
		return 0, fmt.Errorf("failed to marshal custom webhook payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, webhookURL, bytes.NewReader(body))
	if err != nil {
		return 0, fmt.Errorf("failed to create request: %w", err)
	}

	// Set default headers
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Enclii-Webhook/1.0")
	req.Header.Set("X-Enclii-Event", string(event.Type))
	req.Header.Set("X-Enclii-Delivery", event.ID.String())
	req.Header.Set("X-Enclii-Timestamp", fmt.Sprintf("%d", event.Timestamp.Unix()))

	// Add HMAC signature if signing secret is configured
	if c.signingSecret != "" {
		signature := c.computeSignature(body)
		req.Header.Set("X-Enclii-Signature", signature)
		req.Header.Set("X-Enclii-Signature-256", "sha256="+signature)
	}

	// Add custom headers
	for key, value := range c.customHeaders {
		req.Header.Set(key, value)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("failed to send request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	// Accept any 2xx status as success
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, fmt.Errorf("webhook returned status %d: %s", resp.StatusCode, string(respBody))
	}

	return resp.StatusCode, nil
}

// buildPayload creates the webhook payload from an event
func (c *CustomSender) buildPayload(event *types.WebhookEvent) *CustomWebhookPayload {
	return &CustomWebhookPayload{
		ID:         event.ID.String(),
		Type:       event.Type,
		Timestamp:  event.Timestamp,
		Project:    event.Project,
		Deployment: event.Deployment,
		Build:      event.Build,
		Service:    event.Service,
		Database:   event.Database,
	}
}

// computeSignature computes the HMAC-SHA256 signature of the payload
func (c *CustomSender) computeSignature(payload []byte) string {
	mac := hmac.New(sha256.New, []byte(c.signingSecret))
	mac.Write(payload)
	return hex.EncodeToString(mac.Sum(nil))
}

// VerifySignature verifies a webhook signature (for use by receivers)
// This is a helper function that can be used by services receiving Enclii webhooks
func VerifySignature(payload []byte, signature, secret string) bool {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	expected := hex.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(expected), []byte(signature))
}
