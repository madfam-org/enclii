package notifications

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// SlackSender handles sending notifications to Slack webhooks
type SlackSender struct {
	client *http.Client
	logger *logrus.Logger
}

// NewSlackSender creates a new Slack sender
func NewSlackSender(logger *logrus.Logger) *SlackSender {
	return &SlackSender{
		client: &http.Client{Timeout: 10 * time.Second},
		logger: logger,
	}
}

// SlackMessage represents the Slack webhook payload
type SlackMessage struct {
	Text        string            `json:"text,omitempty"`
	Blocks      []SlackBlock      `json:"blocks,omitempty"`
	Attachments []SlackAttachment `json:"attachments,omitempty"`
}

// SlackBlock represents a Slack block element
type SlackBlock struct {
	Type     string           `json:"type"`
	Text     *SlackTextBlock  `json:"text,omitempty"`
	Fields   []SlackTextBlock `json:"fields,omitempty"`
	Elements []SlackElement   `json:"elements,omitempty"`
}

// SlackTextBlock represents a text block
type SlackTextBlock struct {
	Type  string `json:"type"`
	Text  string `json:"text"`
	Emoji bool   `json:"emoji,omitempty"`
}

// SlackElement represents a block element
type SlackElement struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
	URL  string `json:"url,omitempty"`
}

// SlackAttachment represents a message attachment
type SlackAttachment struct {
	Color  string `json:"color,omitempty"`
	Text   string `json:"text,omitempty"`
	Footer string `json:"footer,omitempty"`
}

// Send sends an event to a Slack webhook
func (s *SlackSender) Send(ctx context.Context, webhookURL string, event *types.WebhookEvent) (int, error) {
	message := s.buildMessage(event)

	payload, err := json.Marshal(message)
	if err != nil {
		return 0, fmt.Errorf("failed to marshal Slack message: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, webhookURL, bytes.NewReader(payload))
	if err != nil {
		return 0, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("failed to send request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, fmt.Errorf("Slack API returned status %d: %s", resp.StatusCode, string(body))
	}

	return resp.StatusCode, nil
}

// buildMessage creates a Slack message from an event
func (s *SlackSender) buildMessage(event *types.WebhookEvent) *SlackMessage {
	// Determine emoji and color based on event type
	emoji, color, title := s.getEventMeta(event.Type)

	blocks := []SlackBlock{
		{
			Type: "header",
			Text: &SlackTextBlock{
				Type:  "plain_text",
				Text:  fmt.Sprintf("%s %s", emoji, title),
				Emoji: true,
			},
		},
		{
			Type: "section",
			Text: &SlackTextBlock{
				Type: "mrkdwn",
				Text: fmt.Sprintf("*Project:* %s", event.Project.Name),
			},
		},
	}

	// Add event-specific details
	if event.Deployment != nil {
		blocks = append(blocks, s.buildDeploymentBlocks(event.Deployment)...)
	}
	if event.Build != nil {
		blocks = append(blocks, s.buildBuildBlocks(event.Build)...)
	}
	if event.Service != nil {
		blocks = append(blocks, s.buildServiceBlocks(event.Service)...)
	}
	if event.Database != nil {
		blocks = append(blocks, s.buildDatabaseBlocks(event.Database)...)
	}

	// Add timestamp context
	blocks = append(blocks, SlackBlock{
		Type: "context",
		Elements: []SlackElement{
			{
				Type: "plain_text",
				Text: fmt.Sprintf("Enclii • %s", event.Timestamp.Format(time.RFC822)),
			},
		},
	})

	return &SlackMessage{
		Blocks: blocks,
		Attachments: []SlackAttachment{
			{Color: color},
		},
	}
}

func (s *SlackSender) getEventMeta(eventType types.WebhookEventType) (emoji, color, title string) {
	switch eventType {
	case types.WebhookEventDeploymentStarted:
		return "🚀", "#3AA3E3", "Deployment Started"
	case types.WebhookEventDeploymentSucceeded:
		return "✅", "#36a64f", "Deployment Succeeded"
	case types.WebhookEventDeploymentFailed:
		return "❌", "#dc3545", "Deployment Failed"
	case types.WebhookEventDeploymentCancelled:
		return "⏹️", "#6c757d", "Deployment Cancelled"
	case types.WebhookEventBuildStarted:
		return "🔨", "#3AA3E3", "Build Started"
	case types.WebhookEventBuildSucceeded:
		return "✅", "#36a64f", "Build Succeeded"
	case types.WebhookEventBuildFailed:
		return "❌", "#dc3545", "Build Failed"
	case types.WebhookEventServiceCreated:
		return "➕", "#36a64f", "Service Created"
	case types.WebhookEventServiceDeleted:
		return "🗑️", "#6c757d", "Service Deleted"
	case types.WebhookEventServiceStarted:
		return "▶️", "#36a64f", "Service Started"
	case types.WebhookEventServiceStopped:
		return "⏸️", "#ffc107", "Service Stopped"
	case types.WebhookEventServiceUnhealthy:
		return "⚠️", "#dc3545", "Service Unhealthy"
	case types.WebhookEventDatabaseReady:
		return "🗄️", "#36a64f", "Database Ready"
	case types.WebhookEventDatabaseFailed:
		return "❌", "#dc3545", "Database Failed"
	default:
		return "📢", "#6c757d", string(eventType)
	}
}

func (s *SlackSender) buildDeploymentBlocks(d *types.WebhookDeploymentInfo) []SlackBlock {
	fields := []SlackTextBlock{
		{Type: "mrkdwn", Text: fmt.Sprintf("*Service:*\n%s", d.ServiceName)},
		{Type: "mrkdwn", Text: fmt.Sprintf("*Environment:*\n%s", d.Environment)},
	}

	if d.Branch != "" {
		fields = append(fields, SlackTextBlock{Type: "mrkdwn", Text: fmt.Sprintf("*Branch:*\n%s", d.Branch)})
	}
	if d.CommitSHA != "" {
		fields = append(fields, SlackTextBlock{Type: "mrkdwn", Text: fmt.Sprintf("*Commit:*\n`%s`", d.CommitSHA[:7])})
	}

	blocks := []SlackBlock{
		{Type: "section", Fields: fields},
	}

	if d.CommitMessage != "" {
		blocks = append(blocks, SlackBlock{
			Type: "section",
			Text: &SlackTextBlock{Type: "mrkdwn", Text: fmt.Sprintf("*Commit Message:*\n%s", d.CommitMessage)},
		})
	}

	if d.URL != "" {
		blocks = append(blocks, SlackBlock{
			Type: "section",
			Text: &SlackTextBlock{Type: "mrkdwn", Text: fmt.Sprintf("*URL:*\n<%s|View Deployment>", d.URL)},
		})
	}

	if d.Error != "" {
		blocks = append(blocks, SlackBlock{
			Type: "section",
			Text: &SlackTextBlock{Type: "mrkdwn", Text: fmt.Sprintf("*Error:*\n```%s```", d.Error)},
		})
	}

	return blocks
}

func (s *SlackSender) buildBuildBlocks(b *types.WebhookBuildInfo) []SlackBlock {
	fields := []SlackTextBlock{
		{Type: "mrkdwn", Text: fmt.Sprintf("*Service:*\n%s", b.ServiceName)},
		{Type: "mrkdwn", Text: fmt.Sprintf("*Status:*\n%s", b.Status)},
	}

	if b.CommitSHA != "" {
		fields = append(fields, SlackTextBlock{Type: "mrkdwn", Text: fmt.Sprintf("*Commit:*\n`%s`", b.CommitSHA[:7])})
	}
	if b.ImageTag != "" {
		fields = append(fields, SlackTextBlock{Type: "mrkdwn", Text: fmt.Sprintf("*Image:*\n%s", b.ImageTag)})
	}

	blocks := []SlackBlock{
		{Type: "section", Fields: fields},
	}

	if b.Error != "" {
		blocks = append(blocks, SlackBlock{
			Type: "section",
			Text: &SlackTextBlock{Type: "mrkdwn", Text: fmt.Sprintf("*Error:*\n```%s```", b.Error)},
		})
	}

	return blocks
}

func (s *SlackSender) buildServiceBlocks(svc *types.WebhookServiceInfo) []SlackBlock {
	fields := []SlackTextBlock{
		{Type: "mrkdwn", Text: fmt.Sprintf("*Service:*\n%s", svc.Name)},
		{Type: "mrkdwn", Text: fmt.Sprintf("*Status:*\n%s", svc.Status)},
	}

	blocks := []SlackBlock{
		{Type: "section", Fields: fields},
	}

	if svc.URL != "" {
		blocks = append(blocks, SlackBlock{
			Type: "section",
			Text: &SlackTextBlock{Type: "mrkdwn", Text: fmt.Sprintf("*URL:*\n<%s|View Service>", svc.URL)},
		})
	}

	if svc.Error != "" {
		blocks = append(blocks, SlackBlock{
			Type: "section",
			Text: &SlackTextBlock{Type: "mrkdwn", Text: fmt.Sprintf("*Error:*\n```%s```", svc.Error)},
		})
	}

	return blocks
}

func (s *SlackSender) buildDatabaseBlocks(db *types.WebhookDatabaseInfo) []SlackBlock {
	fields := []SlackTextBlock{
		{Type: "mrkdwn", Text: fmt.Sprintf("*Database:*\n%s", db.Name)},
		{Type: "mrkdwn", Text: fmt.Sprintf("*Type:*\n%s", db.Type)},
		{Type: "mrkdwn", Text: fmt.Sprintf("*Status:*\n%s", db.Status)},
	}

	blocks := []SlackBlock{
		{Type: "section", Fields: fields},
	}

	if db.Error != "" {
		blocks = append(blocks, SlackBlock{
			Type: "section",
			Text: &SlackTextBlock{Type: "mrkdwn", Text: fmt.Sprintf("*Error:*\n```%s```", db.Error)},
		})
	}

	return blocks
}
