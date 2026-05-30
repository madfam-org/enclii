package notifications

import (
	"context"
	"fmt"
	"time"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/resend"
	"github.com/sirupsen/logrus"
)

// EmailService handles transactional email delivery via Resend.
type EmailService struct {
	logger    *logrus.Logger
	client    *resend.Client
	fromEmail string
	fromName  string
	baseURL   string
	enabled   bool
}

// EmailConfig holds email service configuration.
type EmailConfig struct {
	APIKey    string // RESEND_API_KEY
	FromEmail string // EMAIL_FROM_ADDRESS (default: noreply@enclii.dev)
	FromName  string // EMAIL_FROM_NAME (default: Enclii)
	BaseURL   string // APP_BASE_URL (e.g., https://app.enclii.dev)
}

// NewEmailService creates a new email service.
func NewEmailService(cfg EmailConfig, logger *logrus.Logger) *EmailService {
	client := resend.NewClient(resend.Config{APIKey: cfg.APIKey})
	enabled := client.Configured()

	if !enabled {
		logger.Warn("Email service not configured - emails will be logged only. Set RESEND_API_KEY to enable.")
	}

	return &EmailService{
		logger:    logger,
		client:    client,
		fromEmail: withDefault(cfg.FromEmail, "noreply@enclii.dev"),
		fromName:  withDefault(cfg.FromName, "Enclii"),
		baseURL:   withDefault(cfg.BaseURL, "https://app.enclii.dev"),
		enabled:   enabled,
	}
}

func withDefault(val, def string) string {
	if val == "" {
		return def
	}
	return val
}

// ResendClient exposes the underlying Resend API client for provider adapters.
func (s *EmailService) ResendClient() *resend.Client {
	if s == nil {
		return nil
	}
	return s.client
}

// TeamInvitationData contains data for team invitation emails.
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

// SendTeamInvitation sends a team invitation email.
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

// resendEmail represents the Resend API email payload (used in tests).
type resendEmail struct {
	From    string   `json:"from"`
	To      []string `json:"to"`
	Subject string   `json:"subject"`
	HTML    string   `json:"html,omitempty"`
	Text    string   `json:"text,omitempty"`
}

func (s *EmailService) send(ctx context.Context, to, subject, htmlBody, textBody string) error {
	logger := s.logger.WithFields(logrus.Fields{
		"to":      to,
		"subject": subject,
	})

	if !s.enabled {
		logger.Info("Email would be sent (email service not configured)")
		logger.WithField("text_body", textBody).Debug("Email content")
		return nil
	}

	_, err := s.client.SendEmail(ctx, resend.SendEmailRequest{
		From:    fmt.Sprintf("%s <%s>", s.fromName, s.fromEmail),
		To:      []string{to},
		Subject: subject,
		HTML:    htmlBody,
		Text:    textBody,
	})
	if err != nil {
		logger.WithError(err).Error("Failed to send email")
		return fmt.Errorf("failed to send email: %w", err)
	}

	logger.Info("Email sent successfully")
	return nil
}

// IsEnabled returns whether email sending is configured.
func (s *EmailService) IsEnabled() bool {
	return s.enabled
}

// FromEmail returns the configured sender address.
func (s *EmailService) FromEmail() string {
	if s == nil {
		return ""
	}
	return s.fromEmail
}

// FromName returns the configured sender display name.
func (s *EmailService) FromName() string {
	if s == nil {
		return ""
	}
	return s.fromName
}

// SendGeneric sends a plain-text transactional email.
func (s *EmailService) SendGeneric(ctx context.Context, to, subject, body string) error {
	if to == "" {
		s.logger.WithField("subject", subject).
			Info("SendGeneric: no recipient, logging only")
		return nil
	}
	return s.send(ctx, to, subject, "", body)
}
