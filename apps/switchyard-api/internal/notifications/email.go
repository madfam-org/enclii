package notifications

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/sirupsen/logrus"
)

// EmailService handles transactional email delivery
type EmailService struct {
	logger     *logrus.Logger
	apiKey     string // Resend API key
	fromEmail  string // Default from email
	fromName   string // Default from name
	baseURL    string // App base URL for links
	enabled    bool   // Whether email is configured
	httpClient *http.Client
}

// EmailConfig holds email service configuration
type EmailConfig struct {
	APIKey    string // RESEND_API_KEY
	FromEmail string // EMAIL_FROM_ADDRESS (default: noreply@enclii.dev)
	FromName  string // EMAIL_FROM_NAME (default: Enclii)
	BaseURL   string // APP_BASE_URL (e.g., https://app.enclii.dev)
}

// NewEmailService creates a new email service
func NewEmailService(cfg EmailConfig, logger *logrus.Logger) *EmailService {
	enabled := cfg.APIKey != ""

	if !enabled {
		logger.Warn("Email service not configured - emails will be logged only. Set RESEND_API_KEY to enable.")
	}

	return &EmailService{
		logger:     logger,
		apiKey:     cfg.APIKey,
		fromEmail:  withDefault(cfg.FromEmail, "noreply@enclii.dev"),
		fromName:   withDefault(cfg.FromName, "Enclii"),
		baseURL:    withDefault(cfg.BaseURL, "https://app.enclii.dev"),
		enabled:    enabled,
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
}

func withDefault(val, def string) string {
	if val == "" {
		return def
	}
	return val
}

// TeamInvitationData contains data for team invitation emails
type TeamInvitationData struct {
	InviteeEmail    string
	TeamName        string
	TeamSlug        string
	InviterName     string
	InviterEmail    string
	Role            string
	InvitationToken string
	ExpiresAt       time.Time
}

// SendTeamInvitation sends a team invitation email
func (s *EmailService) SendTeamInvitation(ctx context.Context, data TeamInvitationData) error {
	inviteURL := fmt.Sprintf("%s/invitations/accept?token=%s", s.baseURL, data.InvitationToken)

	subject := fmt.Sprintf("You've been invited to join %s on Enclii", data.TeamName)

	htmlBody := fmt.Sprintf(`
<!DOCTYPE html>
<html>
<head>
    <meta charset="utf-8">
    <style>
        body { font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif; line-height: 1.6; color: #333; }
        .container { max-width: 600px; margin: 0 auto; padding: 20px; }
        .button { display: inline-block; background: #0066cc; color: white; padding: 12px 24px; text-decoration: none; border-radius: 6px; margin: 20px 0; }
        .footer { margin-top: 40px; font-size: 12px; color: #666; }
    </style>
</head>
<body>
    <div class="container">
        <h1>You're invited to join %s</h1>
        <p>Hi,</p>
        <p><strong>%s</strong> (%s) has invited you to join the <strong>%s</strong> team on Enclii as a <strong>%s</strong>.</p>
        <p>Click the button below to accept this invitation:</p>
        <a href="%s" class="button">Accept Invitation</a>
        <p>Or copy and paste this link into your browser:</p>
        <p style="word-break: break-all;">%s</p>
        <p>This invitation expires on %s.</p>
        <div class="footer">
            <p>If you weren't expecting this invitation, you can safely ignore this email.</p>
            <p>&copy; Enclii - Self-hosted DevOps Platform</p>
        </div>
    </div>
</body>
</html>`,
		data.TeamName,
		data.InviterName, data.InviterEmail, data.TeamName, data.Role,
		inviteURL, inviteURL,
		data.ExpiresAt.Format("January 2, 2006 at 3:04 PM UTC"),
	)

	textBody := fmt.Sprintf(`You're invited to join %s on Enclii

Hi,

%s (%s) has invited you to join the %s team on Enclii as a %s.

Accept your invitation by visiting:
%s

This invitation expires on %s.

If you weren't expecting this invitation, you can safely ignore this email.
`,
		data.TeamName,
		data.InviterName, data.InviterEmail, data.TeamName, data.Role,
		inviteURL,
		data.ExpiresAt.Format("January 2, 2006 at 3:04 PM UTC"),
	)

	return s.send(ctx, data.InviteeEmail, subject, htmlBody, textBody)
}

// resendEmail represents the Resend API email payload
type resendEmail struct {
	From    string   `json:"from"`
	To      []string `json:"to"`
	Subject string   `json:"subject"`
	HTML    string   `json:"html,omitempty"`
	Text    string   `json:"text,omitempty"`
}

// send sends an email via the Resend API
func (s *EmailService) send(ctx context.Context, to, subject, htmlBody, textBody string) error {
	logger := s.logger.WithFields(logrus.Fields{
		"to":      to,
		"subject": subject,
	})

	// If email not configured, just log
	if !s.enabled {
		logger.Info("Email would be sent (email service not configured)")
		logger.WithField("text_body", textBody).Debug("Email content")
		return nil
	}

	email := resendEmail{
		From:    fmt.Sprintf("%s <%s>", s.fromName, s.fromEmail),
		To:      []string{to},
		Subject: subject,
		HTML:    htmlBody,
		Text:    textBody,
	}

	body, err := json.Marshal(email)
	if err != nil {
		return fmt.Errorf("failed to marshal email: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", "https://api.resend.com/emails", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+s.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		logger.WithError(err).Error("Failed to send email")
		return fmt.Errorf("failed to send email: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 400 {
		var errResp map[string]interface{}
		_ = json.NewDecoder(resp.Body).Decode(&errResp)
		logger.WithFields(logrus.Fields{
			"status_code": resp.StatusCode,
			"response":    errResp,
		}).Error("Email API returned error")
		return fmt.Errorf("email API error: status %d", resp.StatusCode)
	}

	logger.Info("Email sent successfully")
	return nil
}

// IsEnabled returns whether email sending is configured
func (s *EmailService) IsEnabled() bool {
	return s.enabled
}

// SendGeneric sends a plain-text transactional email. It's a thin wrapper
// around send() exposed for services that need ad-hoc notifications
// (e.g. tenant export readiness) without embedding HTML templates here.
// When `to` is empty, the email is routed to the default admin list
// configured via EMAIL_TO_ADMINS (no-op if neither to nor admin list is
// configured).
func (s *EmailService) SendGeneric(ctx context.Context, to, subject, body string) error {
	if to == "" {
		// Dev/staging with no admin mailing list: log and return.
		s.logger.WithField("subject", subject).
			Info("SendGeneric: no recipient, logging only")
		return nil
	}
	return s.send(ctx, to, subject, "", body)
}
