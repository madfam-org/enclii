package notifications

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func testLogger() *logrus.Logger {
	logger := logrus.New()
	logger.SetLevel(logrus.FatalLevel) // suppress log noise during tests
	return logger
}

func sampleDeploymentEvent() *types.WebhookEvent {
	return &types.WebhookEvent{
		ID:        uuid.New(),
		Type:      types.WebhookEventDeploymentSucceeded,
		Timestamp: time.Now().UTC().Truncate(time.Second),
		ProjectID: uuid.New(),
		Project: types.WebhookProjectInfo{
			ID:   uuid.New(),
			Name: "my-project",
			Slug: "my-project",
		},
		Deployment: &types.WebhookDeploymentInfo{
			ID:            uuid.New(),
			ServiceName:   "api-service",
			Environment:   "production",
			Status:        "succeeded",
			CommitSHA:     "abc1234def5678",
			CommitMessage: "fix: resolve auth bug",
			Branch:        "main",
			URL:           "https://api.example.com",
		},
	}
}

func sampleBuildEvent() *types.WebhookEvent {
	return &types.WebhookEvent{
		ID:        uuid.New(),
		Type:      types.WebhookEventBuildSucceeded,
		Timestamp: time.Now().UTC().Truncate(time.Second),
		ProjectID: uuid.New(),
		Project: types.WebhookProjectInfo{
			ID:   uuid.New(),
			Name: "build-project",
			Slug: "build-project",
		},
		Build: &types.WebhookBuildInfo{
			ID:          uuid.New(),
			ServiceName: "worker",
			Status:      "succeeded",
			CommitSHA:   "deadbeef1234567",
			ImageTag:    "v2.1.0",
		},
	}
}

// roundTripFunc is an adapter to allow the use of ordinary functions as http.RoundTripper.
type roundTripFunc func(req *http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

// redirectTransport returns an http.Client that redirects all requests to the given test server.
func redirectTransport(server *httptest.Server) *http.Client {
	return &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			req.URL.Scheme = "http"
			req.URL.Host = server.Listener.Addr().String()
			return http.DefaultTransport.RoundTrip(req)
		}),
	}
}

// ---------------------------------------------------------------------------
// Slack Tests
// ---------------------------------------------------------------------------

func TestSlackSender_Send_Success(t *testing.T) {
	var receivedBody []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
		var err error
		receivedBody, err = io.ReadAll(r.Body)
		require.NoError(t, err)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer server.Close()

	sender := NewSlackSender(testLogger())
	event := sampleDeploymentEvent()

	statusCode, err := sender.Send(context.Background(), server.URL, event)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, statusCode)

	// Verify the payload is valid Slack message with blocks
	var msg SlackMessage
	require.NoError(t, json.Unmarshal(receivedBody, &msg))
	assert.NotEmpty(t, msg.Blocks, "Slack message should contain blocks")

	// Header block should contain event title
	require.NotNil(t, msg.Blocks[0].Text)
	assert.Equal(t, "header", msg.Blocks[0].Type)
	assert.Contains(t, msg.Blocks[0].Text.Text, "Deployment Succeeded")

	// Attachment should contain success color
	require.Len(t, msg.Attachments, 1)
	assert.Equal(t, "#36a64f", msg.Attachments[0].Color)
}

func TestSlackSender_Send_NonOKStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte("invalid_token"))
	}))
	defer server.Close()

	sender := NewSlackSender(testLogger())
	event := sampleDeploymentEvent()

	statusCode, err := sender.Send(context.Background(), server.URL, event)
	assert.Error(t, err)
	assert.Equal(t, http.StatusForbidden, statusCode)
	assert.Contains(t, err.Error(), "Slack API returned status 403")
	assert.Contains(t, err.Error(), "invalid_token")
}

func TestSlackSender_BuildMessage_DeploymentFields(t *testing.T) {
	sender := NewSlackSender(testLogger())
	event := sampleDeploymentEvent()

	msg := sender.buildMessage(event)

	// Flatten all block text for easy searching
	var allText strings.Builder
	for _, block := range msg.Blocks {
		if block.Text != nil {
			allText.WriteString(block.Text.Text)
			allText.WriteString(" ")
		}
		for _, f := range block.Fields {
			allText.WriteString(f.Text)
			allText.WriteString(" ")
		}
	}
	combined := allText.String()

	assert.Contains(t, combined, "my-project", "should contain project name")
	assert.Contains(t, combined, "api-service", "should contain service name")
	assert.Contains(t, combined, "production", "should contain environment")
	assert.Contains(t, combined, "main", "should contain branch")
	assert.Contains(t, combined, "abc1234", "should contain truncated commit SHA")
	assert.Contains(t, combined, "View Deployment", "should contain deployment URL link")
}

// ---------------------------------------------------------------------------
// Discord Tests
// ---------------------------------------------------------------------------

func TestDiscordSender_Send_Success(t *testing.T) {
	var receivedBody []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
		var err error
		receivedBody, err = io.ReadAll(r.Body)
		require.NoError(t, err)
		w.WriteHeader(http.StatusNoContent) // Discord returns 204 on success
	}))
	defer server.Close()

	sender := NewDiscordSender(testLogger())
	event := sampleBuildEvent()

	statusCode, err := sender.Send(context.Background(), server.URL, event)
	require.NoError(t, err)
	assert.Equal(t, http.StatusNoContent, statusCode)

	// Verify the payload is valid Discord message with embeds
	var msg DiscordMessage
	require.NoError(t, json.Unmarshal(receivedBody, &msg))
	assert.Equal(t, "Enclii", msg.Username)
	require.Len(t, msg.Embeds, 1)

	embed := msg.Embeds[0]
	assert.Contains(t, embed.Title, "Build Succeeded")
	assert.Equal(t, 0x36a64f, embed.Color)
	assert.NotNil(t, embed.Footer)
	assert.Equal(t, "Enclii", embed.Footer.Text)

	// Verify embed fields contain build info
	fieldNames := make([]string, 0, len(embed.Fields))
	for _, f := range embed.Fields {
		fieldNames = append(fieldNames, f.Name)
	}
	assert.Contains(t, fieldNames, "Project")
	assert.Contains(t, fieldNames, "Service")
	assert.Contains(t, fieldNames, "Status")
}

func TestDiscordSender_Send_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"message":"internal error"}`))
	}))
	defer server.Close()

	sender := NewDiscordSender(testLogger())
	event := sampleDeploymentEvent()

	statusCode, err := sender.Send(context.Background(), server.URL, event)
	assert.Error(t, err)
	assert.Equal(t, http.StatusInternalServerError, statusCode)
	assert.Contains(t, err.Error(), "Discord API returned status 500")
}

// ---------------------------------------------------------------------------
// Telegram Tests
// ---------------------------------------------------------------------------

func TestTelegramSender_Send_Success(t *testing.T) {
	var receivedBody []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Contains(t, r.URL.Path, "/bot")
		assert.Contains(t, r.URL.Path, "/sendMessage")
		var err error
		receivedBody, err = io.ReadAll(r.Body)
		require.NoError(t, err)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true,"result":{}}`))
	}))
	defer server.Close()

	sender := NewTelegramSender(testLogger())
	// Since telegramAPIBase is a package-level const, we redirect all requests
	// to the test server via a custom transport.
	sender.client = redirectTransport(server)

	event := sampleDeploymentEvent()
	statusCode, err := sender.Send(context.Background(), "test-bot-token", "-12345", event)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, statusCode)

	// Verify the Telegram message payload
	var msg TelegramMessage
	require.NoError(t, json.Unmarshal(receivedBody, &msg))
	assert.Equal(t, "-12345", msg.ChatID)
	assert.Equal(t, "MarkdownV2", msg.ParseMode)
	assert.True(t, msg.DisableWebPagePreview)
	assert.NotEmpty(t, msg.Text)

	// Should include inline keyboard with deployment URL
	require.NotNil(t, msg.ReplyMarkup)
	require.NotEmpty(t, msg.ReplyMarkup.InlineKeyboard)
	assert.Contains(t, msg.ReplyMarkup.InlineKeyboard[0][0].URL, "https://api.example.com")
}

func TestTelegramSender_Send_MissingBotToken(t *testing.T) {
	sender := NewTelegramSender(testLogger())
	event := sampleDeploymentEvent()

	_, err := sender.Send(context.Background(), "", "-12345", event)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "bot token is required")
}

func TestTelegramSender_Send_MissingChatID(t *testing.T) {
	sender := NewTelegramSender(testLogger())
	event := sampleDeploymentEvent()

	_, err := sender.Send(context.Background(), "test-token", "", event)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "chat ID is required")
}

func TestTelegramSender_Send_APIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK) // Telegram returns 200 even on bot errors
		_, _ = w.Write([]byte(`{"ok":false,"error_code":401,"description":"Unauthorized"}`))
	}))
	defer server.Close()

	sender := NewTelegramSender(testLogger())
	sender.client = redirectTransport(server)

	event := sampleDeploymentEvent()
	statusCode, err := sender.Send(context.Background(), "bad-token", "-12345", event)
	assert.Error(t, err)
	assert.Equal(t, 401, statusCode)
	assert.Contains(t, err.Error(), "Unauthorized")
}

// ---------------------------------------------------------------------------
// Custom Webhook Tests
// ---------------------------------------------------------------------------

func TestCustomSender_Send_Success(t *testing.T) {
	var receivedHeaders http.Header
	var receivedBody []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedHeaders = r.Header.Clone()
		var err error
		receivedBody, err = io.ReadAll(r.Body)
		require.NoError(t, err)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	customHeaders := map[string]string{
		"X-Custom-Auth": "Bearer token123",
	}
	sender := NewCustomSender(testLogger(), customHeaders, "")

	event := sampleDeploymentEvent()
	statusCode, err := sender.Send(context.Background(), server.URL, event)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, statusCode)

	// Verify standard headers
	assert.Equal(t, "application/json", receivedHeaders.Get("Content-Type"))
	assert.Equal(t, "Enclii-Webhook/1.0", receivedHeaders.Get("User-Agent"))
	assert.Equal(t, string(event.Type), receivedHeaders.Get("X-Enclii-Event"))
	assert.Equal(t, event.ID.String(), receivedHeaders.Get("X-Enclii-Delivery"))
	assert.NotEmpty(t, receivedHeaders.Get("X-Enclii-Timestamp"))

	// Custom headers should be present
	assert.Equal(t, "Bearer token123", receivedHeaders.Get("X-Custom-Auth"))

	// No signature header when signing secret is empty
	assert.Empty(t, receivedHeaders.Get("X-Enclii-Signature"))
	assert.Empty(t, receivedHeaders.Get("X-Enclii-Signature-256"))

	// Verify the payload is a valid CustomWebhookPayload
	var payload CustomWebhookPayload
	require.NoError(t, json.Unmarshal(receivedBody, &payload))
	assert.Equal(t, event.ID.String(), payload.ID)
	assert.Equal(t, event.Type, payload.Type)
	assert.Equal(t, event.Project.Name, payload.Project.Name)
	assert.NotNil(t, payload.Deployment)
	assert.Equal(t, "api-service", payload.Deployment.ServiceName)
}

func TestCustomSender_Send_WithHMACSignature(t *testing.T) {
	signingSecret := "my-super-secret-key"
	var receivedHeaders http.Header
	var receivedBody []byte

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedHeaders = r.Header.Clone()
		var err error
		receivedBody, err = io.ReadAll(r.Body)
		require.NoError(t, err)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	sender := NewCustomSender(testLogger(), nil, signingSecret)
	event := sampleDeploymentEvent()

	statusCode, err := sender.Send(context.Background(), server.URL, event)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, statusCode)

	// Verify signature headers are present
	signature := receivedHeaders.Get("X-Enclii-Signature")
	assert.NotEmpty(t, signature)
	signature256 := receivedHeaders.Get("X-Enclii-Signature-256")
	assert.True(t, strings.HasPrefix(signature256, "sha256="))

	// Independently compute the expected HMAC-SHA256 signature
	mac := hmac.New(sha256.New, []byte(signingSecret))
	mac.Write(receivedBody)
	expectedSignature := hex.EncodeToString(mac.Sum(nil))

	assert.Equal(t, expectedSignature, signature)
	assert.Equal(t, "sha256="+expectedSignature, signature256)
}

func TestCustomSender_Send_Non2xxStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte("upstream timeout"))
	}))
	defer server.Close()

	sender := NewCustomSender(testLogger(), nil, "")
	event := sampleDeploymentEvent()

	statusCode, err := sender.Send(context.Background(), server.URL, event)
	assert.Error(t, err)
	assert.Equal(t, http.StatusBadGateway, statusCode)
	assert.Contains(t, err.Error(), "webhook returned status 502")
	assert.Contains(t, err.Error(), "upstream timeout")
}

// ---------------------------------------------------------------------------
// VerifySignature (standalone helper)
// ---------------------------------------------------------------------------

func TestVerifySignature(t *testing.T) {
	secret := "webhook-secret-2026"
	payload := []byte(`{"type":"deployment.succeeded","project":"my-app"}`)

	// Compute correct signature
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	correctSig := hex.EncodeToString(mac.Sum(nil))

	t.Run("valid signature", func(t *testing.T) {
		assert.True(t, VerifySignature(payload, correctSig, secret))
	})

	t.Run("invalid signature", func(t *testing.T) {
		assert.False(t, VerifySignature(payload, "deadbeef0000", secret))
	})

	t.Run("wrong secret", func(t *testing.T) {
		assert.False(t, VerifySignature(payload, correctSig, "wrong-secret"))
	})

	t.Run("tampered payload", func(t *testing.T) {
		tampered := []byte(`{"type":"deployment.failed","project":"my-app"}`)
		assert.False(t, VerifySignature(tampered, correctSig, secret))
	})
}

// ---------------------------------------------------------------------------
// Telegram escapeMarkdown
// ---------------------------------------------------------------------------

func TestEscapeMarkdown(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "no special characters",
			input:    "hello world",
			expected: "hello world",
		},
		{
			name:     "underscores and asterisks",
			input:    "hello_world *bold*",
			expected: `hello\_world \*bold\*`,
		},
		{
			name:     "brackets and parens",
			input:    "[link](url)",
			expected: `\[link\]\(url\)`,
		},
		{
			name:     "dots and exclamation",
			input:    "v1.2.3!",
			expected: `v1\.2\.3\!`,
		},
		{
			name:     "all special characters",
			input:    "_*[]()~`>#+-=|{}.!",
			expected: `\_\*\[\]\(\)\~\` + "`" + `\>\#\+\-\=\|\{\}\.\!`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := escapeMarkdown(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// ---------------------------------------------------------------------------
// Event Metadata Tests
// ---------------------------------------------------------------------------

func TestSlackSender_GetEventMeta_AllEventTypes(t *testing.T) {
	sender := NewSlackSender(testLogger())

	eventTypes := []struct {
		eventType   types.WebhookEventType
		expectTitle string
		expectColor string
	}{
		{types.WebhookEventDeploymentStarted, "Deployment Started", "#3AA3E3"},
		{types.WebhookEventDeploymentSucceeded, "Deployment Succeeded", "#36a64f"},
		{types.WebhookEventDeploymentFailed, "Deployment Failed", "#dc3545"},
		{types.WebhookEventDeploymentCancelled, "Deployment Cancelled", "#6c757d"},
		{types.WebhookEventBuildStarted, "Build Started", "#3AA3E3"},
		{types.WebhookEventBuildSucceeded, "Build Succeeded", "#36a64f"},
		{types.WebhookEventBuildFailed, "Build Failed", "#dc3545"},
		{types.WebhookEventServiceCreated, "Service Created", "#36a64f"},
		{types.WebhookEventServiceDeleted, "Service Deleted", "#6c757d"},
		{types.WebhookEventServiceStarted, "Service Started", "#36a64f"},
		{types.WebhookEventServiceStopped, "Service Stopped", "#ffc107"},
		{types.WebhookEventServiceUnhealthy, "Service Unhealthy", "#dc3545"},
		{types.WebhookEventDatabaseReady, "Database Ready", "#36a64f"},
		{types.WebhookEventDatabaseFailed, "Database Failed", "#dc3545"},
	}

	for _, tt := range eventTypes {
		t.Run(string(tt.eventType), func(t *testing.T) {
			_, color, title := sender.getEventMeta(tt.eventType)
			assert.Equal(t, tt.expectTitle, title)
			assert.Equal(t, tt.expectColor, color)
		})
	}

	// Unknown event type falls back to raw string
	t.Run("unknown event type", func(t *testing.T) {
		_, color, title := sender.getEventMeta("custom.unknown")
		assert.Equal(t, "custom.unknown", title)
		assert.Equal(t, "#6c757d", color)
	})
}

func TestDiscordSender_GetEventMeta_ColorIsHexInt(t *testing.T) {
	sender := NewDiscordSender(testLogger())

	// Discord uses integer colors; verify key colors match expected hex values
	_, color, _ := sender.getEventMeta(types.WebhookEventDeploymentSucceeded)
	assert.Equal(t, 0x36a64f, color)

	_, color, _ = sender.getEventMeta(types.WebhookEventDeploymentFailed)
	assert.Equal(t, 0xdc3545, color)
}

// ---------------------------------------------------------------------------
// Email Service Tests
// ---------------------------------------------------------------------------

func TestEmailService_Disabled_LogsOnly(t *testing.T) {
	svc := NewEmailService(EmailConfig{
		APIKey: "", // empty = disabled
	}, testLogger())

	assert.False(t, svc.IsEnabled())

	// Should succeed without sending anything
	err := svc.SendTeamInvitation(context.Background(), TeamInvitationData{
		InviteeEmail:    "test@example.com",
		TeamName:        "My Team",
		InviterName:     "Admin",
		InviterEmail:    "admin@example.com",
		Role:            "developer",
		InvitationToken: "tok_abc123",
		ExpiresAt:       time.Now().Add(7 * 24 * time.Hour),
	})
	assert.NoError(t, err)
}

func TestEmailService_Enabled_SendsToResend(t *testing.T) {
	var receivedBody []byte
	var receivedAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedAuth = r.Header.Get("Authorization")
		var err error
		receivedBody, err = io.ReadAll(r.Body)
		require.NoError(t, err)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"msg_123"}`))
	}))
	defer server.Close()

	svc := NewEmailService(EmailConfig{
		APIKey:    "re_test_key",
		FromEmail: "noreply@test.dev",
		FromName:  "TestApp",
		BaseURL:   "https://test.dev",
	}, testLogger())

	assert.True(t, svc.IsEnabled())

	// Override the httpClient transport to redirect to our test server
	svc.client.SetHTTPClient(redirectTransport(server))

	err := svc.SendTeamInvitation(context.Background(), TeamInvitationData{
		InviteeEmail:    "newuser@example.com",
		TeamName:        "Acme Corp",
		TeamSlug:        "acme-corp",
		InviterName:     "Jane Doe",
		InviterEmail:    "jane@acme.com",
		Role:            "admin",
		InvitationToken: "tok_invite_xyz",
		ExpiresAt:       time.Date(2026, 4, 1, 12, 0, 0, 0, time.UTC),
	})
	require.NoError(t, err)

	assert.Equal(t, "Bearer re_test_key", receivedAuth)

	var email resendEmail
	require.NoError(t, json.Unmarshal(receivedBody, &email))
	assert.Equal(t, "TestApp <noreply@test.dev>", email.From)
	assert.Equal(t, []string{"newuser@example.com"}, email.To)
	assert.Contains(t, email.Subject, "Acme Corp")
	assert.Contains(t, email.HTML, "tok_invite_xyz")
	assert.Contains(t, email.Text, "Jane Doe")
}

func TestEmailService_ResendError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(`{"statusCode":422,"name":"validation_error","message":"Invalid email"}`))
	}))
	defer server.Close()

	svc := NewEmailService(EmailConfig{
		APIKey: "re_test_key",
	}, testLogger())

	svc.client.SetHTTPClient(redirectTransport(server))

	err := svc.SendTeamInvitation(context.Background(), TeamInvitationData{
		InviteeEmail:    "bad-email",
		TeamName:        "Test",
		InviterName:     "Admin",
		InviterEmail:    "admin@test.com",
		Role:            "viewer",
		InvitationToken: "tok_abc",
		ExpiresAt:       time.Now().Add(time.Hour),
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "status 422")
}

// ---------------------------------------------------------------------------
// eventToPayload Tests
// ---------------------------------------------------------------------------

func TestEventToPayload(t *testing.T) {
	event := sampleDeploymentEvent()

	payload := eventToPayload(event)

	assert.Equal(t, event.ID, payload["id"])
	assert.Equal(t, event.Type, payload["type"])
	assert.Equal(t, event.Timestamp, payload["timestamp"])
	assert.Equal(t, event.Project, payload["project"])
	assert.NotNil(t, payload["deployment"])
	assert.Nil(t, payload["build"])
	assert.Nil(t, payload["service"])
	assert.Nil(t, payload["database"])
}

// ---------------------------------------------------------------------------
// withDefault helper
// ---------------------------------------------------------------------------

func TestWithDefault(t *testing.T) {
	assert.Equal(t, "hello", withDefault("hello", "fallback"))
	assert.Equal(t, "fallback", withDefault("", "fallback"))
}

// ---------------------------------------------------------------------------
// Custom Webhook buildPayload
// ---------------------------------------------------------------------------

func TestCustomSender_BuildPayload(t *testing.T) {
	sender := NewCustomSender(testLogger(), nil, "")
	event := sampleDeploymentEvent()

	payload := sender.buildPayload(event)

	assert.Equal(t, event.ID.String(), payload.ID)
	assert.Equal(t, event.Type, payload.Type)
	assert.Equal(t, event.Timestamp, payload.Timestamp)
	assert.Equal(t, event.Project, payload.Project)
	assert.NotNil(t, payload.Deployment)
	assert.Nil(t, payload.Build)
	assert.Nil(t, payload.Service)
	assert.Nil(t, payload.Database)
}

// ---------------------------------------------------------------------------
// Custom Webhook computeSignature determinism
// ---------------------------------------------------------------------------

func TestCustomSender_ComputeSignature_Deterministic(t *testing.T) {
	sender := NewCustomSender(testLogger(), nil, "test-secret")
	payload := []byte(`{"event":"test"}`)

	sig1 := sender.computeSignature(payload)
	sig2 := sender.computeSignature(payload)

	assert.Equal(t, sig1, sig2, "same payload and secret must produce identical signatures")
	assert.Len(t, sig1, 64, "HMAC-SHA256 hex digest should be 64 characters")

	// Different payload produces different signature
	sigDiff := sender.computeSignature([]byte(`{"event":"other"}`))
	assert.NotEqual(t, sig1, sigDiff)
}

// ---------------------------------------------------------------------------
// Slack buildMessage with service event
// ---------------------------------------------------------------------------

func TestSlackSender_BuildMessage_ServiceEvent(t *testing.T) {
	sender := NewSlackSender(testLogger())
	event := &types.WebhookEvent{
		ID:        uuid.New(),
		Type:      types.WebhookEventServiceUnhealthy,
		Timestamp: time.Now().UTC(),
		Project:   types.WebhookProjectInfo{Name: "infra-project"},
		Service: &types.WebhookServiceInfo{
			ID:     uuid.New(),
			Name:   "payment-worker",
			Status: "unhealthy",
			URL:    "https://payments.example.com",
			Error:  "health check timeout after 30s",
		},
	}

	msg := sender.buildMessage(event)

	// Header should reflect unhealthy event
	require.NotEmpty(t, msg.Blocks)
	assert.Contains(t, msg.Blocks[0].Text.Text, "Service Unhealthy")

	// Error color
	require.Len(t, msg.Attachments, 1)
	assert.Equal(t, "#dc3545", msg.Attachments[0].Color)

	// Check that service details and error are present
	var allText strings.Builder
	for _, block := range msg.Blocks {
		if block.Text != nil {
			allText.WriteString(block.Text.Text)
			allText.WriteString(" ")
		}
		for _, f := range block.Fields {
			allText.WriteString(f.Text)
			allText.WriteString(" ")
		}
	}
	combined := allText.String()
	assert.Contains(t, combined, "payment-worker")
	assert.Contains(t, combined, "health check timeout")
}

// ---------------------------------------------------------------------------
// Discord buildMessage with database event
// ---------------------------------------------------------------------------

func TestDiscordSender_BuildMessage_DatabaseEvent(t *testing.T) {
	sender := NewDiscordSender(testLogger())
	event := &types.WebhookEvent{
		ID:        uuid.New(),
		Type:      types.WebhookEventDatabaseReady,
		Timestamp: time.Now().UTC(),
		Project:   types.WebhookProjectInfo{Name: "data-project"},
		Database: &types.WebhookDatabaseInfo{
			ID:     uuid.New(),
			Name:   "app-postgres",
			Type:   "postgresql",
			Status: "ready",
		},
	}

	msg := sender.buildMessage(event)

	require.Len(t, msg.Embeds, 1)
	embed := msg.Embeds[0]
	assert.Contains(t, embed.Title, "Database Ready")
	assert.Equal(t, 0x36a64f, embed.Color)

	fieldMap := make(map[string]string)
	for _, f := range embed.Fields {
		fieldMap[f.Name] = f.Value
	}
	assert.Equal(t, "app-postgres", fieldMap["Database"])
	assert.Equal(t, "postgresql", fieldMap["Type"])
	assert.Equal(t, "ready", fieldMap["Status"])
}

// ---------------------------------------------------------------------------
// Custom Webhook unreachable server
// ---------------------------------------------------------------------------

func TestCustomSender_Send_ConnectionRefused(t *testing.T) {
	sender := NewCustomSender(testLogger(), nil, "")
	event := sampleDeploymentEvent()

	// Use a URL that will definitely refuse connection
	_, err := sender.Send(context.Background(), "http://127.0.0.1:1", event)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to send request")
}

// ---------------------------------------------------------------------------
// Slack test event with deployment payload
// ---------------------------------------------------------------------------

func TestSlackSender_Send_TestEventPayload(t *testing.T) {
	var receivedBody []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var err error
		receivedBody, err = io.ReadAll(r.Body)
		require.NoError(t, err)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer server.Close()

	sender := NewSlackSender(testLogger())
	testEvent := &types.WebhookEvent{
		ID:        uuid.New(),
		Type:      types.WebhookEventDeploymentSucceeded,
		Timestamp: time.Now(),
		Project: types.WebhookProjectInfo{
			ID:   uuid.New(),
			Name: "Test Project",
			Slug: "test-project",
		},
		Deployment: &types.WebhookDeploymentInfo{
			ID:            uuid.New(),
			ServiceName:   "test-service",
			Environment:   "production",
			Status:        "succeeded",
			CommitSHA:     "abc123def",
			CommitMessage: "Test deployment",
			Branch:        "main",
			URL:           "https://test.example.com",
		},
	}

	statusCode, err := sender.Send(context.Background(), server.URL, testEvent)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, statusCode)

	var msg SlackMessage
	require.NoError(t, json.Unmarshal(receivedBody, &msg))

	// Verify the test event payload is correctly formatted
	assert.NotEmpty(t, msg.Blocks)
	assert.Contains(t, msg.Blocks[0].Text.Text, "Deployment Succeeded")

	// Verify deployment data is rendered
	var allText strings.Builder
	for _, block := range msg.Blocks {
		if block.Text != nil {
			allText.WriteString(block.Text.Text)
		}
		for _, f := range block.Fields {
			allText.WriteString(f.Text)
		}
	}
	combined := allText.String()
	assert.Contains(t, combined, "Test Project")
	assert.Contains(t, combined, "test-service")
	assert.Contains(t, combined, "production")
}

// ---------------------------------------------------------------------------
// Telegram message with no URL buttons
// ---------------------------------------------------------------------------

func TestTelegramSender_BuildMessage_NoURLButtons(t *testing.T) {
	sender := NewTelegramSender(testLogger())
	event := &types.WebhookEvent{
		ID:        uuid.New(),
		Type:      types.WebhookEventBuildFailed,
		Timestamp: time.Now().UTC(),
		Project:   types.WebhookProjectInfo{Name: "ci-project"},
		Build: &types.WebhookBuildInfo{
			ID:          uuid.New(),
			ServiceName: "frontend",
			Status:      "failed",
			CommitSHA:   "1234567890abcdef",
			Error:       "npm install failed",
		},
	}

	msg := sender.buildMessage("-999", event)

	assert.Equal(t, "-999", msg.ChatID)
	assert.Contains(t, msg.Text, "Build Failed")
	assert.Contains(t, msg.Text, "frontend")
	assert.Contains(t, msg.Text, "npm install failed")
	// No deployment or service URL, so no inline keyboard
	assert.Nil(t, msg.ReplyMarkup, "should not have inline keyboard when no URLs are present")
}

// ---------------------------------------------------------------------------
// Email withDefault edge cases
// ---------------------------------------------------------------------------

func TestNewEmailService_Defaults(t *testing.T) {
	svc := NewEmailService(EmailConfig{
		APIKey: "re_key",
		// All other fields left empty to test defaults
	}, testLogger())

	assert.True(t, svc.IsEnabled())
	assert.Equal(t, "noreply@enclii.dev", svc.fromEmail)
	assert.Equal(t, "Enclii", svc.fromName)
	assert.Equal(t, "https://app.enclii.dev", svc.baseURL)
}

// ---------------------------------------------------------------------------
// Slack message with error details in deployment
// ---------------------------------------------------------------------------

func TestSlackSender_BuildMessage_DeploymentWithError(t *testing.T) {
	sender := NewSlackSender(testLogger())
	event := &types.WebhookEvent{
		ID:        uuid.New(),
		Type:      types.WebhookEventDeploymentFailed,
		Timestamp: time.Now().UTC(),
		Project:   types.WebhookProjectInfo{Name: "error-project"},
		Deployment: &types.WebhookDeploymentInfo{
			ID:          uuid.New(),
			ServiceName: "api",
			Environment: "staging",
			Status:      "failed",
			CommitSHA:   "aaaa1234bbb5678",
			Error:       "CrashLoopBackOff: container exited with code 1",
		},
	}

	msg := sender.buildMessage(event)

	// Check for error block
	found := false
	for _, block := range msg.Blocks {
		if block.Text != nil && strings.Contains(block.Text.Text, "CrashLoopBackOff") {
			found = true
			break
		}
	}
	assert.True(t, found, "error details should appear in Slack message blocks")

	// Failed events should have red color
	require.Len(t, msg.Attachments, 1)
	assert.Equal(t, "#dc3545", msg.Attachments[0].Color)
}

// ---------------------------------------------------------------------------
// Verify sender HTTP timeout is configured
// ---------------------------------------------------------------------------

func TestSenderTimeouts(t *testing.T) {
	t.Run("slack sender has 10s timeout", func(t *testing.T) {
		sender := NewSlackSender(testLogger())
		assert.Equal(t, 10*time.Second, sender.client.Timeout)
	})

	t.Run("discord sender has 10s timeout", func(t *testing.T) {
		sender := NewDiscordSender(testLogger())
		assert.Equal(t, 10*time.Second, sender.client.Timeout)
	})

	t.Run("telegram sender has 10s timeout", func(t *testing.T) {
		sender := NewTelegramSender(testLogger())
		assert.Equal(t, 10*time.Second, sender.client.Timeout)
	})

	t.Run("custom sender has 30s timeout", func(t *testing.T) {
		sender := NewCustomSender(testLogger(), nil, "")
		assert.Equal(t, 30*time.Second, sender.client.Timeout)
	})

	t.Run("email service has 30s timeout", func(t *testing.T) {
		svc := NewEmailService(EmailConfig{APIKey: "key"}, testLogger())
		assert.Equal(t, 30*time.Second, svc.ResendClient().HTTPClient().Timeout)
	})
}

// ---------------------------------------------------------------------------
// Custom Webhook buildPayload with build event
// ---------------------------------------------------------------------------

func TestCustomSender_BuildPayload_BuildEvent(t *testing.T) {
	sender := NewCustomSender(testLogger(), nil, "")
	event := sampleBuildEvent()

	payload := sender.buildPayload(event)

	assert.Equal(t, event.Type, payload.Type)
	assert.Nil(t, payload.Deployment)
	assert.NotNil(t, payload.Build)
	assert.Equal(t, "worker", payload.Build.ServiceName)
	assert.Equal(t, "v2.1.0", payload.Build.ImageTag)
}

// ---------------------------------------------------------------------------
// Custom Webhook 2xx range acceptance
// ---------------------------------------------------------------------------

func TestCustomSender_Send_Accepts2xxRange(t *testing.T) {
	for _, code := range []int{200, 201, 202, 204} {
		t.Run(fmt.Sprintf("status_%d", code), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(code)
			}))
			defer server.Close()

			sender := NewCustomSender(testLogger(), nil, "")
			event := sampleDeploymentEvent()

			statusCode, err := sender.Send(context.Background(), server.URL, event)
			assert.NoError(t, err)
			assert.Equal(t, code, statusCode)
		})
	}
}
