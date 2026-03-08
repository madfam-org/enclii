package notifications

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

const telegramAPIBase = "https://api.telegram.org/bot"

// TelegramSender handles sending notifications to Telegram via Bot API
type TelegramSender struct {
	client *http.Client
	logger *logrus.Logger
}

// NewTelegramSender creates a new Telegram sender
func NewTelegramSender(logger *logrus.Logger) *TelegramSender {
	return &TelegramSender{
		client: &http.Client{Timeout: 10 * time.Second},
		logger: logger,
	}
}

// TelegramMessage represents the Telegram sendMessage payload
type TelegramMessage struct {
	ChatID                string                  `json:"chat_id"`
	Text                  string                  `json:"text"`
	ParseMode             string                  `json:"parse_mode,omitempty"`
	DisableWebPagePreview bool                    `json:"disable_web_page_preview,omitempty"`
	DisableNotification   bool                    `json:"disable_notification,omitempty"`
	ReplyMarkup           *TelegramInlineKeyboard `json:"reply_markup,omitempty"`
}

// TelegramInlineKeyboard represents an inline keyboard
type TelegramInlineKeyboard struct {
	InlineKeyboard [][]TelegramInlineButton `json:"inline_keyboard"`
}

// TelegramInlineButton represents a button in an inline keyboard
type TelegramInlineButton struct {
	Text string `json:"text"`
	URL  string `json:"url,omitempty"`
}

// TelegramResponse represents the Telegram API response
type TelegramResponse struct {
	OK          bool   `json:"ok"`
	ErrorCode   int    `json:"error_code,omitempty"`
	Description string `json:"description,omitempty"`
}

// Send sends an event to a Telegram chat via Bot API
func (t *TelegramSender) Send(ctx context.Context, botToken, chatID string, event *types.WebhookEvent) (int, error) {
	if botToken == "" {
		return 0, fmt.Errorf("telegram bot token is required")
	}
	if chatID == "" {
		return 0, fmt.Errorf("telegram chat ID is required")
	}

	message := t.buildMessage(chatID, event)

	payload, err := json.Marshal(message)
	if err != nil {
		return 0, fmt.Errorf("failed to marshal Telegram message: %w", err)
	}

	url := fmt.Sprintf("%s%s/sendMessage", telegramAPIBase, botToken)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return 0, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := t.client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("failed to send request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, _ := io.ReadAll(resp.Body)

	var telegramResp TelegramResponse
	if err := json.Unmarshal(body, &telegramResp); err != nil {
		return resp.StatusCode, fmt.Errorf("failed to parse Telegram response: %w", err)
	}

	if !telegramResp.OK {
		return telegramResp.ErrorCode, fmt.Errorf("Telegram API error: %s", telegramResp.Description)
	}

	return resp.StatusCode, nil
}

// buildMessage creates a Telegram message from an event
func (t *TelegramSender) buildMessage(chatID string, event *types.WebhookEvent) *TelegramMessage {
	emoji, title := t.getEventMeta(event.Type)

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("%s *%s*\n\n", emoji, escapeMarkdown(title)))
	sb.WriteString(fmt.Sprintf("📁 *Project:* %s\n", escapeMarkdown(event.Project.Name)))

	// Add event-specific details
	if event.Deployment != nil {
		t.appendDeploymentDetails(&sb, event.Deployment)
	}
	if event.Build != nil {
		t.appendBuildDetails(&sb, event.Build)
	}
	if event.Service != nil {
		t.appendServiceDetails(&sb, event.Service)
	}
	if event.Database != nil {
		t.appendDatabaseDetails(&sb, event.Database)
	}

	sb.WriteString(fmt.Sprintf("\n⏱ %s", event.Timestamp.Format("Jan 2, 2006 15:04 MST")))

	message := &TelegramMessage{
		ChatID:                chatID,
		Text:                  sb.String(),
		ParseMode:             "MarkdownV2",
		DisableWebPagePreview: true,
	}

	// Add inline buttons for URLs
	var buttons []TelegramInlineButton
	if event.Deployment != nil && event.Deployment.URL != "" {
		buttons = append(buttons, TelegramInlineButton{
			Text: "🔗 View Deployment",
			URL:  event.Deployment.URL,
		})
	}
	if event.Service != nil && event.Service.URL != "" {
		buttons = append(buttons, TelegramInlineButton{
			Text: "🔗 View Service",
			URL:  event.Service.URL,
		})
	}

	if len(buttons) > 0 {
		message.ReplyMarkup = &TelegramInlineKeyboard{
			InlineKeyboard: [][]TelegramInlineButton{buttons},
		}
	}

	return message
}

// getEventMeta returns emoji and title for an event type
func (t *TelegramSender) getEventMeta(eventType types.WebhookEventType) (emoji, title string) {
	switch eventType {
	case types.WebhookEventDeploymentStarted:
		return "🚀", "Deployment Started"
	case types.WebhookEventDeploymentSucceeded:
		return "✅", "Deployment Succeeded"
	case types.WebhookEventDeploymentFailed:
		return "❌", "Deployment Failed"
	case types.WebhookEventDeploymentCancelled:
		return "⏹", "Deployment Cancelled"
	case types.WebhookEventBuildStarted:
		return "🔨", "Build Started"
	case types.WebhookEventBuildSucceeded:
		return "✅", "Build Succeeded"
	case types.WebhookEventBuildFailed:
		return "❌", "Build Failed"
	case types.WebhookEventServiceCreated:
		return "➕", "Service Created"
	case types.WebhookEventServiceDeleted:
		return "🗑", "Service Deleted"
	case types.WebhookEventServiceStarted:
		return "▶️", "Service Started"
	case types.WebhookEventServiceStopped:
		return "⏸", "Service Stopped"
	case types.WebhookEventServiceUnhealthy:
		return "⚠️", "Service Unhealthy"
	case types.WebhookEventDatabaseReady:
		return "🗄", "Database Ready"
	case types.WebhookEventDatabaseFailed:
		return "❌", "Database Failed"
	default:
		return "📢", string(eventType)
	}
}

func (t *TelegramSender) appendDeploymentDetails(sb *strings.Builder, d *types.WebhookDeploymentInfo) {
	sb.WriteString(fmt.Sprintf("🔧 *Service:* %s\n", escapeMarkdown(d.ServiceName)))
	sb.WriteString(fmt.Sprintf("🌍 *Environment:* %s\n", escapeMarkdown(d.Environment)))

	if d.Branch != "" {
		sb.WriteString(fmt.Sprintf("🌿 *Branch:* %s\n", escapeMarkdown(d.Branch)))
	}
	if d.CommitSHA != "" {
		sb.WriteString(fmt.Sprintf("📝 *Commit:* `%s`\n", escapeMarkdown(d.CommitSHA[:7])))
	}
	if d.CommitMessage != "" {
		sb.WriteString(fmt.Sprintf("💬 %s\n", escapeMarkdown(d.CommitMessage)))
	}
	if d.Error != "" {
		sb.WriteString(fmt.Sprintf("\n⚠️ *Error:*\n```\n%s\n```\n", escapeMarkdown(d.Error)))
	}
}

func (t *TelegramSender) appendBuildDetails(sb *strings.Builder, b *types.WebhookBuildInfo) {
	sb.WriteString(fmt.Sprintf("🔧 *Service:* %s\n", escapeMarkdown(b.ServiceName)))
	sb.WriteString(fmt.Sprintf("📊 *Status:* %s\n", escapeMarkdown(b.Status)))

	if b.CommitSHA != "" {
		sb.WriteString(fmt.Sprintf("📝 *Commit:* `%s`\n", escapeMarkdown(b.CommitSHA[:7])))
	}
	if b.ImageTag != "" {
		sb.WriteString(fmt.Sprintf("🏷 *Image:* %s\n", escapeMarkdown(b.ImageTag)))
	}
	if b.Error != "" {
		sb.WriteString(fmt.Sprintf("\n⚠️ *Error:*\n```\n%s\n```\n", escapeMarkdown(b.Error)))
	}
}

func (t *TelegramSender) appendServiceDetails(sb *strings.Builder, svc *types.WebhookServiceInfo) {
	sb.WriteString(fmt.Sprintf("🔧 *Service:* %s\n", escapeMarkdown(svc.Name)))
	sb.WriteString(fmt.Sprintf("📊 *Status:* %s\n", escapeMarkdown(svc.Status)))

	if svc.Error != "" {
		sb.WriteString(fmt.Sprintf("\n⚠️ *Error:*\n```\n%s\n```\n", escapeMarkdown(svc.Error)))
	}
}

func (t *TelegramSender) appendDatabaseDetails(sb *strings.Builder, db *types.WebhookDatabaseInfo) {
	sb.WriteString(fmt.Sprintf("🗄 *Database:* %s\n", escapeMarkdown(db.Name)))
	sb.WriteString(fmt.Sprintf("📦 *Type:* %s\n", escapeMarkdown(db.Type)))
	sb.WriteString(fmt.Sprintf("📊 *Status:* %s\n", escapeMarkdown(db.Status)))

	if db.Error != "" {
		sb.WriteString(fmt.Sprintf("\n⚠️ *Error:*\n```\n%s\n```\n", escapeMarkdown(db.Error)))
	}
}

// escapeMarkdown escapes special characters for Telegram MarkdownV2
func escapeMarkdown(s string) string {
	// MarkdownV2 requires escaping these characters: _ * [ ] ( ) ~ ` > # + - = | { } . !
	replacer := strings.NewReplacer(
		"_", "\\_",
		"*", "\\*",
		"[", "\\[",
		"]", "\\]",
		"(", "\\(",
		")", "\\)",
		"~", "\\~",
		"`", "\\`",
		">", "\\>",
		"#", "\\#",
		"+", "\\+",
		"-", "\\-",
		"=", "\\=",
		"|", "\\|",
		"{", "\\{",
		"}", "\\}",
		".", "\\.",
		"!", "\\!",
	)
	return replacer.Replace(s)
}
