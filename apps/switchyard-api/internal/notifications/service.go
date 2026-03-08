package notifications

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/sirupsen/logrus"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/db"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// Service handles notification webhook delivery
type Service struct {
	repo   *db.WebhookRepository
	logger *logrus.Logger

	// Senders for each webhook type
	slack    *SlackSender
	discord  *DiscordSender
	telegram *TelegramSender
}

// NewService creates a new notification service
func NewService(repo *db.WebhookRepository, logger *logrus.Logger) *Service {
	return &Service{
		repo:     repo,
		logger:   logger,
		slack:    NewSlackSender(logger),
		discord:  NewDiscordSender(logger),
		telegram: NewTelegramSender(logger),
	}
}

// SendEvent sends a webhook event to all subscribed destinations for a project
func (s *Service) SendEvent(ctx context.Context, projectID uuid.UUID, event *types.WebhookEvent) error {
	// Get all enabled webhooks subscribed to this event type
	webhooks, err := s.repo.ListEnabledByEvent(ctx, projectID, event.Type)
	if err != nil {
		return fmt.Errorf("failed to list webhooks: %w", err)
	}

	if len(webhooks) == 0 {
		s.logger.WithFields(logrus.Fields{
			"project_id": projectID,
			"event_type": event.Type,
		}).Debug("No webhooks subscribed to event")
		return nil
	}

	// Send to each webhook asynchronously
	for _, webhook := range webhooks {
		go s.deliverToWebhook(context.Background(), webhook, event)
	}

	s.logger.WithFields(logrus.Fields{
		"project_id":    projectID,
		"event_type":    event.Type,
		"webhook_count": len(webhooks),
	}).Info("Queued webhook deliveries")

	return nil
}

// deliverToWebhook sends an event to a single webhook destination
func (s *Service) deliverToWebhook(ctx context.Context, webhook *types.WebhookDestination, event *types.WebhookEvent) {
	logger := s.logger.WithFields(logrus.Fields{
		"webhook_id":   webhook.ID,
		"webhook_name": webhook.Name,
		"webhook_type": webhook.Type,
		"event_type":   event.Type,
	})

	// Create delivery record
	delivery := &types.WebhookDelivery{
		WebhookID:     webhook.ID,
		EventType:     event.Type,
		EventID:       &event.ID,
		Payload:       eventToPayload(event),
		Status:        types.WebhookDeliveryStatusPending,
		AttemptNumber: 1,
	}

	if err := s.repo.CreateDelivery(ctx, delivery); err != nil {
		logger.WithError(err).Error("Failed to create delivery record")
		return
	}

	// Send the webhook
	startTime := time.Now()
	var sendErr error
	var statusCode int

	switch webhook.Type {
	case types.WebhookTypeSlack:
		statusCode, sendErr = s.slack.Send(ctx, webhook.WebhookURL, event)
	case types.WebhookTypeDiscord:
		statusCode, sendErr = s.discord.Send(ctx, webhook.WebhookURL, event)
	case types.WebhookTypeTelegram:
		statusCode, sendErr = s.telegram.Send(ctx, webhook.TelegramBotToken, webhook.TelegramChatID, event)
	case types.WebhookTypeCustom:
		statusCode, sendErr = s.sendCustomWebhook(ctx, webhook, event)
	default:
		sendErr = fmt.Errorf("unsupported webhook type: %s", webhook.Type)
	}

	duration := int(time.Since(startTime).Milliseconds())
	completedAt := time.Now()

	// Update delivery record
	delivery.DurationMs = &duration
	delivery.CompletedAt = &completedAt
	delivery.StatusCode = &statusCode

	if sendErr != nil {
		delivery.Status = types.WebhookDeliveryStatusFailed
		delivery.ErrorMessage = sendErr.Error()
		logger.WithError(sendErr).Error("Webhook delivery failed")

		// Update webhook failure tracking
		_ = s.repo.UpdateDeliveryStatus(ctx, webhook.ID, "failed", sendErr.Error(), true)
	} else {
		delivery.Status = types.WebhookDeliveryStatusSuccess
		logger.Info("Webhook delivery succeeded")

		// Reset failure count on success
		_ = s.repo.UpdateDeliveryStatus(ctx, webhook.ID, "success", "", false)
	}

	if err := s.repo.UpdateDelivery(ctx, delivery); err != nil {
		logger.WithError(err).Error("Failed to update delivery record")
	}
}

// sendCustomWebhook sends to a custom webhook URL with optional headers
func (s *Service) sendCustomWebhook(ctx context.Context, webhook *types.WebhookDestination, event *types.WebhookEvent) (int, error) {
	// Custom webhooks use the same format as our internal events
	// Could add signature verification here using webhook.SigningSecret
	sender := NewCustomSender(s.logger, webhook.CustomHeaders, webhook.SigningSecret)
	return sender.Send(ctx, webhook.WebhookURL, event)
}

// TestWebhook sends a test event to a webhook
func (s *Service) TestWebhook(ctx context.Context, webhook *types.WebhookDestination, eventType types.WebhookEventType) error {
	testEvent := &types.WebhookEvent{
		ID:        uuid.New(),
		Type:      eventType,
		Timestamp: time.Now(),
		ProjectID: webhook.ProjectID,
		Project: types.WebhookProjectInfo{
			ID:   webhook.ProjectID,
			Name: "Test Project",
			Slug: "test-project",
		},
	}

	// Add sample event data based on event type
	switch {
	case eventType == types.WebhookEventDeploymentSucceeded || eventType == types.WebhookEventDeploymentFailed:
		testEvent.Deployment = &types.WebhookDeploymentInfo{
			ID:            uuid.New(),
			ServiceName:   "test-service",
			Environment:   "production",
			Status:        "succeeded",
			CommitSHA:     "abc123def",
			CommitMessage: "Test deployment",
			Branch:        "main",
			URL:           "https://test.example.com",
		}
	case eventType == types.WebhookEventBuildSucceeded || eventType == types.WebhookEventBuildFailed:
		testEvent.Build = &types.WebhookBuildInfo{
			ID:          uuid.New(),
			ServiceName: "test-service",
			Status:      "succeeded",
			CommitSHA:   "abc123def",
			ImageTag:    "v1.0.0",
		}
	}

	var err error
	switch webhook.Type {
	case types.WebhookTypeSlack:
		_, err = s.slack.Send(ctx, webhook.WebhookURL, testEvent)
	case types.WebhookTypeDiscord:
		_, err = s.discord.Send(ctx, webhook.WebhookURL, testEvent)
	case types.WebhookTypeTelegram:
		_, err = s.telegram.Send(ctx, webhook.TelegramBotToken, webhook.TelegramChatID, testEvent)
	case types.WebhookTypeCustom:
		_, err = s.sendCustomWebhook(ctx, webhook, testEvent)
	default:
		err = fmt.Errorf("unsupported webhook type: %s", webhook.Type)
	}

	return err
}

// RetryDelivery retries a failed webhook delivery
func (s *Service) RetryDelivery(ctx context.Context, webhook *types.WebhookDestination, delivery *types.WebhookDelivery) error {
	// Reconstruct the event from the delivery payload
	event := &types.WebhookEvent{
		Type:      delivery.EventType,
		Timestamp: delivery.AttemptedAt,
		Project: types.WebhookProjectInfo{
			ID: webhook.ProjectID,
		},
	}

	// Increment attempt number
	delivery.AttemptNumber++

	var sendErr error
	var statusCode int
	startTime := time.Now()

	switch webhook.Type {
	case types.WebhookTypeSlack:
		statusCode, sendErr = s.slack.Send(ctx, webhook.WebhookURL, event)
	case types.WebhookTypeDiscord:
		statusCode, sendErr = s.discord.Send(ctx, webhook.WebhookURL, event)
	case types.WebhookTypeTelegram:
		statusCode, sendErr = s.telegram.Send(ctx, webhook.TelegramBotToken, webhook.TelegramChatID, event)
	case types.WebhookTypeCustom:
		statusCode, sendErr = s.sendCustomWebhook(ctx, webhook, event)
	default:
		return fmt.Errorf("unsupported webhook type: %s", webhook.Type)
	}

	duration := int(time.Since(startTime).Milliseconds())
	completedAt := time.Now()

	delivery.DurationMs = &duration
	delivery.CompletedAt = &completedAt
	delivery.StatusCode = &statusCode

	if sendErr != nil {
		delivery.Status = types.WebhookDeliveryStatusFailed
		delivery.ErrorMessage = sendErr.Error()
		_ = s.repo.UpdateDeliveryStatus(ctx, webhook.ID, "failed", sendErr.Error(), true)
	} else {
		delivery.Status = types.WebhookDeliveryStatusSuccess
		delivery.ErrorMessage = ""
		_ = s.repo.UpdateDeliveryStatus(ctx, webhook.ID, "success", "", false)
	}

	if err := s.repo.UpdateDelivery(ctx, delivery); err != nil {
		s.logger.WithError(err).Error("Failed to update delivery record after retry")
	}

	return sendErr
}

// Helper to convert event to generic payload map
func eventToPayload(event *types.WebhookEvent) map[string]any {
	return map[string]any{
		"id":         event.ID,
		"type":       event.Type,
		"timestamp":  event.Timestamp,
		"project":    event.Project,
		"deployment": event.Deployment,
		"build":      event.Build,
		"service":    event.Service,
		"database":   event.Database,
	}
}
