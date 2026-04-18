package signup

import (
	"context"
	"fmt"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/notifications"
)

// EmailAdapter wraps notifications.EmailService to satisfy the EmailSender
// interface the signup service uses. Keeps signup code free of the full
// notifications API surface and lets tests swap in a fake.
type EmailAdapter struct {
	svc *notifications.EmailService
}

// NewEmailAdapter wires a notifications.EmailService.
func NewEmailAdapter(svc *notifications.EmailService) *EmailAdapter {
	return &EmailAdapter{svc: svc}
}

// SendVerification sends the one-time verification email. We reuse the
// existing generic-send entrypoint rather than adding another templated
// method on notifications.EmailService — the content is Sprint-1-small.
func (a *EmailAdapter) SendVerification(ctx context.Context, to, verifyURL string) error {
	if a == nil || a.svc == nil {
		return fmt.Errorf("email service not configured")
	}
	subject := "Verify your Enclii email"
	body := fmt.Sprintf(`Welcome to Enclii!

Click the link below to verify your email and continue setting up your account:

%s

This link expires in 24 hours. If you didn't request this, you can safely ignore it.

— The Enclii team
`, verifyURL)
	return a.svc.SendGeneric(ctx, to, subject, body)
}

// SendWelcome sends the post-provision welcome email with a deep link to
// the user's new project.
func (a *EmailAdapter) SendWelcome(ctx context.Context, to, firstName, projectURL string) error {
	if a == nil || a.svc == nil {
		return fmt.Errorf("email service not configured")
	}
	name := firstName
	if name == "" {
		name = "there"
	}
	subject := "Welcome to Enclii — your first deploy awaits"
	body := fmt.Sprintf(`Hi %s,

Your Enclii account is ready. Jump into your new project to import a repo and ship your first deploy:

%s

If you get stuck, reply to this email — a human will answer.

— The Enclii team
`, name, projectURL)
	return a.svc.SendGeneric(ctx, to, subject, body)
}
