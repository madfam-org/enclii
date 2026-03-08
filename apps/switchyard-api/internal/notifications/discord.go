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

// DiscordSender handles sending notifications to Discord webhooks
type DiscordSender struct {
	client *http.Client
	logger *logrus.Logger
}

// NewDiscordSender creates a new Discord sender
func NewDiscordSender(logger *logrus.Logger) *DiscordSender {
	return &DiscordSender{
		client: &http.Client{Timeout: 10 * time.Second},
		logger: logger,
	}
}

// DiscordMessage represents the Discord webhook payload
type DiscordMessage struct {
	Content   string         `json:"content,omitempty"`
	Username  string         `json:"username,omitempty"`
	AvatarURL string         `json:"avatar_url,omitempty"`
	Embeds    []DiscordEmbed `json:"embeds,omitempty"`
}

// DiscordEmbed represents a Discord embed
type DiscordEmbed struct {
	Title       string              `json:"title,omitempty"`
	Description string              `json:"description,omitempty"`
	URL         string              `json:"url,omitempty"`
	Color       int                 `json:"color,omitempty"`
	Timestamp   string              `json:"timestamp,omitempty"`
	Footer      *DiscordEmbedFooter `json:"footer,omitempty"`
	Author      *DiscordEmbedAuthor `json:"author,omitempty"`
	Fields      []DiscordEmbedField `json:"fields,omitempty"`
}

// DiscordEmbedFooter represents the footer of a Discord embed
type DiscordEmbedFooter struct {
	Text    string `json:"text"`
	IconURL string `json:"icon_url,omitempty"`
}

// DiscordEmbedAuthor represents the author section of a Discord embed
type DiscordEmbedAuthor struct {
	Name    string `json:"name"`
	URL     string `json:"url,omitempty"`
	IconURL string `json:"icon_url,omitempty"`
}

// DiscordEmbedField represents a field in a Discord embed
type DiscordEmbedField struct {
	Name   string `json:"name"`
	Value  string `json:"value"`
	Inline bool   `json:"inline,omitempty"`
}

// Send sends an event to a Discord webhook
func (d *DiscordSender) Send(ctx context.Context, webhookURL string, event *types.WebhookEvent) (int, error) {
	message := d.buildMessage(event)

	payload, err := json.Marshal(message)
	if err != nil {
		return 0, fmt.Errorf("failed to marshal Discord message: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, webhookURL, bytes.NewReader(payload))
	if err != nil {
		return 0, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := d.client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("failed to send request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	// Discord returns 204 No Content on success
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		body, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, fmt.Errorf("Discord API returned status %d: %s", resp.StatusCode, string(body))
	}

	return resp.StatusCode, nil
}

// buildMessage creates a Discord message from an event
func (d *DiscordSender) buildMessage(event *types.WebhookEvent) *DiscordMessage {
	emoji, color, title := d.getEventMeta(event.Type)

	embed := DiscordEmbed{
		Title:     fmt.Sprintf("%s %s", emoji, title),
		Color:     color,
		Timestamp: event.Timestamp.Format(time.RFC3339),
		Footer: &DiscordEmbedFooter{
			Text: "Enclii",
		},
		Fields: []DiscordEmbedField{
			{
				Name:   "Project",
				Value:  event.Project.Name,
				Inline: true,
			},
		},
	}

	// Add event-specific fields
	if event.Deployment != nil {
		embed.Fields = append(embed.Fields, d.buildDeploymentFields(event.Deployment)...)
		if event.Deployment.URL != "" {
			embed.URL = event.Deployment.URL
		}
	}
	if event.Build != nil {
		embed.Fields = append(embed.Fields, d.buildBuildFields(event.Build)...)
	}
	if event.Service != nil {
		embed.Fields = append(embed.Fields, d.buildServiceFields(event.Service)...)
		if event.Service.URL != "" {
			embed.URL = event.Service.URL
		}
	}
	if event.Database != nil {
		embed.Fields = append(embed.Fields, d.buildDatabaseFields(event.Database)...)
	}

	return &DiscordMessage{
		Username:  "Enclii",
		AvatarURL: "https://enclii.dev/logo.png",
		Embeds:    []DiscordEmbed{embed},
	}
}

// getEventMeta returns emoji, color (as int), and title for an event type
func (d *DiscordSender) getEventMeta(eventType types.WebhookEventType) (emoji string, color int, title string) {
	switch eventType {
	case types.WebhookEventDeploymentStarted:
		return "🚀", 0x3AA3E3, "Deployment Started"
	case types.WebhookEventDeploymentSucceeded:
		return "✅", 0x36a64f, "Deployment Succeeded"
	case types.WebhookEventDeploymentFailed:
		return "❌", 0xdc3545, "Deployment Failed"
	case types.WebhookEventDeploymentCancelled:
		return "⏹️", 0x6c757d, "Deployment Cancelled"
	case types.WebhookEventBuildStarted:
		return "🔨", 0x3AA3E3, "Build Started"
	case types.WebhookEventBuildSucceeded:
		return "✅", 0x36a64f, "Build Succeeded"
	case types.WebhookEventBuildFailed:
		return "❌", 0xdc3545, "Build Failed"
	case types.WebhookEventServiceCreated:
		return "➕", 0x36a64f, "Service Created"
	case types.WebhookEventServiceDeleted:
		return "🗑️", 0x6c757d, "Service Deleted"
	case types.WebhookEventServiceStarted:
		return "▶️", 0x36a64f, "Service Started"
	case types.WebhookEventServiceStopped:
		return "⏸️", 0xffc107, "Service Stopped"
	case types.WebhookEventServiceUnhealthy:
		return "⚠️", 0xdc3545, "Service Unhealthy"
	case types.WebhookEventDatabaseReady:
		return "🗄️", 0x36a64f, "Database Ready"
	case types.WebhookEventDatabaseFailed:
		return "❌", 0xdc3545, "Database Failed"
	default:
		return "📢", 0x6c757d, string(eventType)
	}
}

func (d *DiscordSender) buildDeploymentFields(dep *types.WebhookDeploymentInfo) []DiscordEmbedField {
	fields := []DiscordEmbedField{
		{Name: "Service", Value: dep.ServiceName, Inline: true},
		{Name: "Environment", Value: dep.Environment, Inline: true},
	}

	if dep.Branch != "" {
		fields = append(fields, DiscordEmbedField{Name: "Branch", Value: dep.Branch, Inline: true})
	}
	if dep.CommitSHA != "" {
		fields = append(fields, DiscordEmbedField{Name: "Commit", Value: fmt.Sprintf("`%s`", dep.CommitSHA[:7]), Inline: true})
	}
	if dep.CommitMessage != "" {
		fields = append(fields, DiscordEmbedField{Name: "Commit Message", Value: dep.CommitMessage, Inline: false})
	}
	if dep.Error != "" {
		fields = append(fields, DiscordEmbedField{Name: "Error", Value: fmt.Sprintf("```%s```", dep.Error), Inline: false})
	}

	return fields
}

func (d *DiscordSender) buildBuildFields(b *types.WebhookBuildInfo) []DiscordEmbedField {
	fields := []DiscordEmbedField{
		{Name: "Service", Value: b.ServiceName, Inline: true},
		{Name: "Status", Value: b.Status, Inline: true},
	}

	if b.CommitSHA != "" {
		fields = append(fields, DiscordEmbedField{Name: "Commit", Value: fmt.Sprintf("`%s`", b.CommitSHA[:7]), Inline: true})
	}
	if b.ImageTag != "" {
		fields = append(fields, DiscordEmbedField{Name: "Image", Value: b.ImageTag, Inline: true})
	}
	if b.Error != "" {
		fields = append(fields, DiscordEmbedField{Name: "Error", Value: fmt.Sprintf("```%s```", b.Error), Inline: false})
	}

	return fields
}

func (d *DiscordSender) buildServiceFields(svc *types.WebhookServiceInfo) []DiscordEmbedField {
	fields := []DiscordEmbedField{
		{Name: "Service", Value: svc.Name, Inline: true},
		{Name: "Status", Value: svc.Status, Inline: true},
	}

	if svc.Error != "" {
		fields = append(fields, DiscordEmbedField{Name: "Error", Value: fmt.Sprintf("```%s```", svc.Error), Inline: false})
	}

	return fields
}

func (d *DiscordSender) buildDatabaseFields(db *types.WebhookDatabaseInfo) []DiscordEmbedField {
	fields := []DiscordEmbedField{
		{Name: "Database", Value: db.Name, Inline: true},
		{Name: "Type", Value: db.Type, Inline: true},
		{Name: "Status", Value: db.Status, Inline: true},
	}

	if db.Error != "" {
		fields = append(fields, DiscordEmbedField{Name: "Error", Value: fmt.Sprintf("```%s```", db.Error), Inline: false})
	}

	return fields
}
