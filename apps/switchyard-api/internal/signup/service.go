// Package signup implements the self-serve signup state machine for P3.2.
//
// Sprint 1 scope:
//
//	email -> verification email sent (pending_verification)
//	      -> verification link clicked (verified)
//	      -> GitHub OAuth complete (github_linked)
//	      -> project auto-created (provisioning -> ready)
//
// Janua owns user identity + passwords + email verification-token storage.
// This service coordinates the workflow and holds enclii-side state (the
// signup_requests + signup_events tables). See internal/db/signup_repository.go.
//
// Deferred to Sprint 2: buildpack auto-detect, template one-click deploy,
// billing capture, magic-link signup.
// Deferred to Sprint 3: custom-domain claim, team invite at signup,
// trial-to-paid conversion tracking.
package signup

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/mail"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/sirupsen/logrus"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/db"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

const (
	// Verification tokens live for 24h. After that the user must re-initiate.
	verificationTokenTTL = 24 * time.Hour

	// OAuth state tokens expire with the signup row's updated_at + this
	// window. We don't enforce in SQL; the service checks on callback.
	oauthStateTTL = 30 * time.Minute
)

// Sentinel errors. Handlers map these to HTTP codes; tests assert on them.
var (
	ErrSignupDisabled          = errors.New("signup: feature disabled")
	ErrInvalidEmail            = errors.New("signup: invalid email address")
	ErrSignupNotFound          = errors.New("signup: not found")
	ErrInvalidToken            = errors.New("signup: invalid or expired verification token")
	ErrInvalidOAuthState       = errors.New("signup: invalid or expired oauth state")
	ErrEmailAlreadyRegistered  = errors.New("signup: email already has a completed account")
	ErrWrongStateForTransition = errors.New("signup: operation not valid in current state")
)

// JanuaClient abstracts the subset of Janua's public HTTP API that the
// signup flow uses. A real impl lives alongside; tests pass a fake.
type JanuaClient interface {
	// RegisterUser creates a new Janua user via POST /api/v1/auth/signup
	// with a random password (the user will set their own password later
	// via /password/forgot or a magic-link flow in Sprint 2). The returned
	// `sub` is the Janua user id used as the foreign key throughout enclii.
	//
	// Returns ErrEmailAlreadyRegistered if Janua reports the email is in
	// use with an already-verified account.
	RegisterUser(ctx context.Context, email, companyName string) (januaUserSub string, err error)

	// BuildGithubAuthorizeURL returns a URL to which we redirect the user
	// so they can authorize GitHub access via Janua's OAuth flow. The
	// `state` param is passed through Janua and back to our callback.
	BuildGithubAuthorizeURL(ctx context.Context, januaUserSub, state, callbackURL string) (string, error)

	// CompleteGithubOAuth exchanges the OAuth code that Janua handed back
	// to our callback for (github_username, access_token). The raw access
	// token is returned so we can immediately write it to a K8s Secret;
	// it is NEVER persisted in our DB.
	CompleteGithubOAuth(ctx context.Context, januaUserSub, code string) (githubUsername, accessToken string, err error)
}

// EmailSender is the subset of notifications.EmailService the signup flow
// needs. Tests pass a fake that captures messages.
type EmailSender interface {
	// SendVerification sends the initial verification email containing the
	// one-time-use link.
	SendVerification(ctx context.Context, to, verifyURL string) error
	// SendWelcome sends the final "you're in" email once a project is
	// provisioned.
	SendWelcome(ctx context.Context, to, firstName, projectURL string) error
}

// SecretWriter abstracts writing a K8s Secret. Concrete implementation
// uses the existing reconciler.SecretsProvisioner; tests use a fake.
type SecretWriter interface {
	// WriteGithubToken writes a Secret containing the GitHub access token
	// and returns a secret-ref string of the form
	//   "namespace/name#key"
	// following the RFC 0005 convention already used elsewhere. The raw
	// token MUST NOT be returned or logged — callers only need the ref.
	WriteGithubToken(ctx context.Context, signupID uuid.UUID, rawToken string) (secretRef string, err error)
}

// ProjectCreator abstracts the bit of the project service we call during
// final provisioning. Narrowing the surface avoids a big import graph in
// tests.
type ProjectCreator interface {
	CreateDefaultProjectForSignup(ctx context.Context, signupID uuid.UUID, email, companyName, januaUserSub string) (*types.Project, error)
}

// Service orchestrates the signup state machine.
type Service struct {
	repos         *db.Repositories
	janua         JanuaClient
	email         EmailSender
	secrets       SecretWriter
	projects      ProjectCreator
	logger        *logrus.Logger
	appBaseURL    string // e.g. https://app.enclii.dev
	apiBaseURL    string // e.g. https://api.enclii.dev (for OAuth callback)
	featureFlagOn bool
}

// Config is the struct of externally-provided deps + config.
type Config struct {
	Repos         *db.Repositories
	Janua         JanuaClient
	Email         EmailSender
	Secrets       SecretWriter
	Projects      ProjectCreator
	Logger        *logrus.Logger
	AppBaseURL    string
	APIBaseURL    string
	FeatureFlagOn bool
}

// NewService constructs the service. Any nil dep beyond Repos/Logger is
// allowed — the corresponding flow will return a 503-equivalent error.
func NewService(cfg Config) *Service {
	base := strings.TrimRight(cfg.AppBaseURL, "/")
	if base == "" {
		base = "https://app.enclii.dev"
	}
	api := strings.TrimRight(cfg.APIBaseURL, "/")
	if api == "" {
		api = "https://api.enclii.dev"
	}
	return &Service{
		repos:         cfg.Repos,
		janua:         cfg.Janua,
		email:         cfg.Email,
		secrets:       cfg.Secrets,
		projects:      cfg.Projects,
		logger:        cfg.Logger,
		appBaseURL:    base,
		apiBaseURL:    api,
		featureFlagOn: cfg.FeatureFlagOn,
	}
}

// IsEnabled reports whether ENCLII_SIGNUP_ENABLED is set. Handlers check
// this and return 404 (rather than 503) when disabled, so callers can't
// enumerate the surface until it's officially opened.
func (s *Service) IsEnabled() bool { return s.featureFlagOn }

// --- Public API ------------------------------------------------------------

// InitiateRequest is the input to POST /v1/signup.
type InitiateRequest struct {
	Email       string `json:"email" binding:"required"`
	CompanyName string `json:"company_name,omitempty"`
}

// InitiateResponse is the result of POST /v1/signup.
type InitiateResponse struct {
	SignupID uuid.UUID `json:"signup_id"`
	Email    string    `json:"email"`
	Status   string    `json:"status"`
	NextStep string    `json:"next_step"`
	// Only set in dev/test when email is not configured — lets the UI
	// skip the email round-trip. Never populated in production.
	DevVerifyURL string `json:"dev_verify_url,omitempty"`
}

// Initiate creates a signup_requests row, generates a verification token,
// and sends the verification email. Idempotent-ish: if an active signup
// already exists for this email, we re-send the verification email to
// that row instead of creating a new one.
func (s *Service) Initiate(ctx context.Context, req InitiateRequest) (*InitiateResponse, error) {
	if !s.featureFlagOn {
		return nil, ErrSignupDisabled
	}

	email := normalizeEmail(req.Email)
	if !validEmail(email) {
		return nil, ErrInvalidEmail
	}

	// Check for existing active signup — resume rather than create.
	existing, err := s.repos.Signups.GetActiveByEmail(ctx, email)
	if err == nil && existing != nil {
		// Re-mint a fresh token (invalidates any outstanding link).
		return s.reissueVerification(ctx, existing)
	}

	// Generate the one-time verification token. We store only the hash.
	rawToken, tokenHash, err := generateSecret(32)
	if err != nil {
		return nil, fmt.Errorf("generate verification token: %w", err)
	}
	expiresAt := time.Now().UTC().Add(verificationTokenTTL)

	sr := &db.SignupRequest{
		Email:                      email,
		VerificationTokenHash:      &tokenHash,
		VerificationTokenExpiresAt: &expiresAt,
		Status:                     db.SignupStatusPendingVerification,
	}
	if req.CompanyName != "" {
		cn := strings.TrimSpace(req.CompanyName)
		sr.CompanyName = &cn
	}

	if err := s.repos.Signups.Create(ctx, sr); err != nil {
		return nil, fmt.Errorf("persist signup request: %w", err)
	}
	_ = s.repos.Signups.AppendEvent(ctx, sr.ID, "initiated", map[string]any{
		"email":        email,
		"company_name": req.CompanyName,
	})

	verifyURL := s.buildVerifyURL(sr.ID, rawToken)

	devURL := ""
	if s.email != nil {
		if sendErr := s.email.SendVerification(ctx, email, verifyURL); sendErr != nil {
			s.logger.WithError(sendErr).
				WithField("signup_id", sr.ID).
				Warn("signup: verification email send failed; flow continues but user is stuck until retry")
		}
	} else {
		// No email configured — common in dev/test. Surface the URL so the
		// operator / integration tests can complete the flow manually.
		s.logger.WithField("verify_url", verifyURL).
			Info("signup: email sender not configured; returning dev verify URL inline")
		devURL = verifyURL
	}

	return &InitiateResponse{
		SignupID:     sr.ID,
		Email:        email,
		Status:       sr.Status,
		NextStep:     "verify_email",
		DevVerifyURL: devURL,
	}, nil
}

// reissueVerification is the resume path when a user hits POST /signup
// twice for the same email. We rotate the token so the old link stops
// working (preventing abuse) and re-send the email.
func (s *Service) reissueVerification(ctx context.Context, existing *db.SignupRequest) (*InitiateResponse, error) {
	// Only safe to reissue from pending_verification. If they're further
	// along, just tell them to resume the flow via their signup_id.
	if existing.Status != db.SignupStatusPendingVerification {
		return &InitiateResponse{
			SignupID: existing.ID,
			Email:    existing.Email,
			Status:   existing.Status,
			NextStep: nextStepFor(existing.Status),
		}, nil
	}

	rawToken, tokenHash, err := generateSecret(32)
	if err != nil {
		return nil, fmt.Errorf("generate verification token: %w", err)
	}
	expiresAt := time.Now().UTC().Add(verificationTokenTTL)
	if err := s.repos.Signups.UpdateVerificationToken(ctx, existing.ID, tokenHash, expiresAt); err != nil {
		return nil, err
	}
	_ = s.repos.Signups.AppendEvent(ctx, existing.ID, "verification_token_reissued", map[string]any{})

	verifyURL := s.buildVerifyURL(existing.ID, rawToken)
	devURL := ""
	if s.email != nil {
		if sendErr := s.email.SendVerification(ctx, existing.Email, verifyURL); sendErr != nil {
			s.logger.WithError(sendErr).Warn("signup: verification email resend failed")
		}
	} else {
		devURL = verifyURL
	}

	return &InitiateResponse{
		SignupID:     existing.ID,
		Email:        existing.Email,
		Status:       existing.Status,
		NextStep:     "verify_email",
		DevVerifyURL: devURL,
	}, nil
}

// GetStatus is the poll endpoint. Used by the UI wizard to re-render after
// the user clicks through the verification email.
func (s *Service) GetStatus(ctx context.Context, signupID uuid.UUID) (*db.SignupRequest, error) {
	sr, err := s.repos.Signups.GetByID(ctx, signupID)
	if err != nil {
		return nil, ErrSignupNotFound
	}
	return sr, nil
}

// VerifyEmail consumes a verification token from the email link, registers
// a Janua user, and transitions the signup to 'verified'.
func (s *Service) VerifyEmail(ctx context.Context, signupID uuid.UUID, rawToken string) (*db.SignupRequest, error) {
	if !s.featureFlagOn {
		return nil, ErrSignupDisabled
	}
	if rawToken == "" {
		return nil, ErrInvalidToken
	}

	sr, err := s.repos.Signups.GetByID(ctx, signupID)
	if err != nil {
		return nil, ErrSignupNotFound
	}

	// Idempotency: re-clicking the same link after success is fine —
	// just echo the current state back.
	if sr.Status != db.SignupStatusPendingVerification {
		return sr, nil
	}

	if sr.VerificationTokenHash == nil || sr.VerificationTokenExpiresAt == nil {
		return nil, ErrInvalidToken
	}
	if time.Now().UTC().After(*sr.VerificationTokenExpiresAt) {
		_ = s.repos.Signups.MarkFailed(ctx, signupID, "verification token expired")
		_ = s.repos.Signups.AppendEvent(ctx, signupID, "verification_token_expired", nil)
		return nil, ErrInvalidToken
	}
	if hashSecret(rawToken) != *sr.VerificationTokenHash {
		// Do not leak which token was wrong; just say invalid.
		return nil, ErrInvalidToken
	}

	// Register the user in Janua now that we have a real human on the
	// other end of the email.
	companyName := ""
	if sr.CompanyName != nil {
		companyName = *sr.CompanyName
	}

	var januaSub string
	if s.janua != nil {
		januaSub, err = s.janua.RegisterUser(ctx, sr.Email, companyName)
		if err != nil {
			if errors.Is(err, ErrEmailAlreadyRegistered) {
				_ = s.repos.Signups.MarkFailed(ctx, signupID, "email already registered in janua")
				_ = s.repos.Signups.AppendEvent(ctx, signupID, "janua_register_conflict", nil)
				return nil, ErrEmailAlreadyRegistered
			}
			return nil, fmt.Errorf("janua register: %w", err)
		}
	} else {
		// Dev mode: synthesize a sub so the flow can continue.
		januaSub = "dev-" + signupID.String()
	}

	if err := s.repos.Signups.MarkEmailVerified(ctx, signupID, januaSub); err != nil {
		return nil, err
	}
	_ = s.repos.Signups.AppendEvent(ctx, signupID, "email_verified", map[string]any{
		"janua_user_sub": januaSub,
	})

	return s.repos.Signups.GetByID(ctx, signupID)
}

// AuthorizeGithubResponse is returned by GET /v1/signup/:id/github/authorize.
type AuthorizeGithubResponse struct {
	AuthorizationURL string `json:"authorization_url"`
	State            string `json:"state"`
}

// AuthorizeGithub generates an OAuth state nonce, stores its hash, and
// returns the Janua-provided authorize URL for the UI to redirect to.
func (s *Service) AuthorizeGithub(ctx context.Context, signupID uuid.UUID) (*AuthorizeGithubResponse, error) {
	if !s.featureFlagOn {
		return nil, ErrSignupDisabled
	}
	if s.janua == nil {
		return nil, fmt.Errorf("janua client not configured")
	}

	sr, err := s.repos.Signups.GetByID(ctx, signupID)
	if err != nil {
		return nil, ErrSignupNotFound
	}
	if sr.Status != db.SignupStatusVerified {
		return nil, ErrWrongStateForTransition
	}
	if sr.JanuaUserSub == nil {
		return nil, fmt.Errorf("signup %s has no janua user", signupID)
	}

	rawState, stateHash, err := generateSecret(24)
	if err != nil {
		return nil, err
	}
	if err := s.repos.Signups.SetOAuthState(ctx, signupID, stateHash); err != nil {
		return nil, err
	}

	callback := fmt.Sprintf("%s/v1/signup/%s/github/callback", s.apiBaseURL, signupID)
	authURL, err := s.janua.BuildGithubAuthorizeURL(ctx, *sr.JanuaUserSub, rawState, callback)
	if err != nil {
		return nil, fmt.Errorf("build github authorize url: %w", err)
	}

	_ = s.repos.Signups.AppendEvent(ctx, signupID, "github_authorize_started", nil)

	return &AuthorizeGithubResponse{AuthorizationURL: authURL, State: rawState}, nil
}

// LinkGithub is the OAuth callback handler. Janua redirects here with
// (code, state). We verify state, complete the exchange, and transition
// the signup to github_linked.
func (s *Service) LinkGithub(ctx context.Context, signupID uuid.UUID, code, rawState string) (*db.SignupRequest, error) {
	if !s.featureFlagOn {
		return nil, ErrSignupDisabled
	}
	if code == "" || rawState == "" {
		return nil, ErrInvalidOAuthState
	}
	if s.janua == nil {
		return nil, fmt.Errorf("janua client not configured")
	}

	sr, err := s.repos.Signups.GetByID(ctx, signupID)
	if err != nil {
		return nil, ErrSignupNotFound
	}
	if sr.Status != db.SignupStatusVerified {
		return nil, ErrWrongStateForTransition
	}
	if sr.OAuthStateHash == nil || *sr.OAuthStateHash != hashSecret(rawState) {
		return nil, ErrInvalidOAuthState
	}
	// Bound the state's validity to the time since we set it.
	if time.Since(sr.UpdatedAt) > oauthStateTTL {
		return nil, ErrInvalidOAuthState
	}
	if sr.JanuaUserSub == nil {
		return nil, fmt.Errorf("signup has no janua user")
	}

	githubUsername, accessToken, err := s.janua.CompleteGithubOAuth(ctx, *sr.JanuaUserSub, code)
	if err != nil {
		return nil, fmt.Errorf("complete github oauth: %w", err)
	}

	secretRef := ""
	if s.secrets != nil {
		secretRef, err = s.secrets.WriteGithubToken(ctx, signupID, accessToken)
		if err != nil {
			return nil, fmt.Errorf("write github token secret: %w", err)
		}
	} else {
		// Dev mode: fake ref so the flow completes. The token itself is
		// dropped on the floor; in prod secrets is always wired.
		secretRef = fmt.Sprintf("dev/signup-tokens#ghat-%s", signupID)
	}

	if err := s.repos.Signups.MarkGithubLinked(ctx, signupID, githubUsername, secretRef); err != nil {
		return nil, err
	}
	_ = s.repos.Signups.AppendEvent(ctx, signupID, "github_linked", map[string]any{
		"github_username": githubUsername,
		"secret_ref":      secretRef,
	})

	return s.repos.Signups.GetByID(ctx, signupID)
}

// ProvisionResponse is returned by POST /v1/signup/:id/provision.
type ProvisionResponse struct {
	SignupID    uuid.UUID `json:"signup_id"`
	ProjectID   uuid.UUID `json:"project_id"`
	ProjectSlug string    `json:"project_slug"`
	RedirectURL string    `json:"redirect_url"`
	Status      string    `json:"status"`
}

// Provision runs the final step: create the default project, link it to
// the signup, send the welcome email, and transition to ready.
//
// This is safe to retry: if the caller hits provision twice, only one
// project gets created (state machine + DB check enforce this).
func (s *Service) Provision(ctx context.Context, signupID uuid.UUID) (*ProvisionResponse, error) {
	if !s.featureFlagOn {
		return nil, ErrSignupDisabled
	}

	sr, err := s.repos.Signups.GetByID(ctx, signupID)
	if err != nil {
		return nil, ErrSignupNotFound
	}

	// Idempotent fast path.
	if sr.Status == db.SignupStatusReady && sr.ProvisionedProjectID != nil {
		proj, _ := s.repos.Projects.GetByID(ctx, *sr.ProvisionedProjectID)
		return s.readyResponse(sr, proj), nil
	}

	if sr.Status != db.SignupStatusGithubLinked {
		return nil, ErrWrongStateForTransition
	}
	if sr.JanuaUserSub == nil {
		return nil, fmt.Errorf("signup has no janua user")
	}

	// Claim the provisioning slot — prevents two concurrent calls from
	// double-creating a project.
	if err := s.repos.Signups.MarkProvisioning(ctx, signupID); err != nil {
		return nil, err
	}
	_ = s.repos.Signups.AppendEvent(ctx, signupID, "provisioning_started", nil)

	companyName := ""
	if sr.CompanyName != nil {
		companyName = *sr.CompanyName
	}

	if s.projects == nil {
		_ = s.repos.Signups.MarkFailed(ctx, signupID, "project creator not configured")
		return nil, fmt.Errorf("project creator not configured")
	}

	proj, err := s.projects.CreateDefaultProjectForSignup(ctx, signupID, sr.Email, companyName, *sr.JanuaUserSub)
	if err != nil {
		errMsg := fmt.Sprintf("create project: %v", err)
		_ = s.repos.Signups.MarkFailed(ctx, signupID, errMsg)
		_ = s.repos.Signups.AppendEvent(ctx, signupID, "provisioning_failed", map[string]any{
			"error": errMsg,
		})
		return nil, fmt.Errorf("create project: %w", err)
	}

	if err := s.repos.Signups.MarkReady(ctx, signupID, proj.ID); err != nil {
		return nil, err
	}
	_ = s.repos.Signups.AppendEvent(ctx, signupID, "provisioned", map[string]any{
		"project_id":   proj.ID.String(),
		"project_slug": proj.Slug,
	})

	// Welcome email is best-effort; don't fail the signup if it bounces.
	if s.email != nil {
		projectURL := fmt.Sprintf("%s/projects/%s", s.appBaseURL, proj.Slug)
		if wErr := s.email.SendWelcome(ctx, sr.Email, firstNameFromEmail(sr.Email), projectURL); wErr != nil {
			s.logger.WithError(wErr).
				WithField("signup_id", signupID).
				Warn("signup: welcome email failed; signup is still ready")
		}
	}

	// Re-read so we return the committed row.
	sr, _ = s.repos.Signups.GetByID(ctx, signupID)
	return s.readyResponse(sr, proj), nil
}

// readyResponse builds the terminal response once a project exists.
func (s *Service) readyResponse(sr *db.SignupRequest, proj *types.Project) *ProvisionResponse {
	if proj == nil {
		return &ProvisionResponse{SignupID: sr.ID, Status: sr.Status}
	}
	return &ProvisionResponse{
		SignupID:    sr.ID,
		ProjectID:   proj.ID,
		ProjectSlug: proj.Slug,
		RedirectURL: fmt.Sprintf("%s/projects/%s", s.appBaseURL, proj.Slug),
		Status:      sr.Status,
	}
}

// NextStepFor returns the UI wizard's next-step hint for a given status.
// Exported so handlers can use it when rendering /status responses.
func NextStepFor(status string) string { return nextStepFor(status) }

func nextStepFor(status string) string {
	switch status {
	case db.SignupStatusPendingVerification:
		return "verify_email"
	case db.SignupStatusVerified:
		return "connect_github"
	case db.SignupStatusGithubLinked:
		return "provision"
	case db.SignupStatusProvisioning:
		return "wait_provisioning"
	case db.SignupStatusReady:
		return "done"
	case db.SignupStatusFailed:
		return "restart"
	default:
		return ""
	}
}

// --- Helpers ---------------------------------------------------------------

// generateSecret returns (raw, sha256-hex) of n random bytes.
func generateSecret(n int) (raw string, hash string, err error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", "", err
	}
	raw = hex.EncodeToString(b)
	hash = hashSecret(raw)
	return raw, hash, nil
}

func hashSecret(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

func normalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

// validEmail does a conservative parse. We require a domain with a dot so
// obvious garbage ("foo@bar") is caught even though Go's net/mail accepts it.
var domainDotRE = regexp.MustCompile(`@[^@]+\.[^@]+$`)

func validEmail(email string) bool {
	if len(email) == 0 || len(email) > 320 {
		return false
	}
	addr, err := mail.ParseAddress(email)
	if err != nil || addr.Address != email {
		return false
	}
	return domainDotRE.MatchString(email)
}

func firstNameFromEmail(email string) string {
	local := email
	if i := strings.IndexByte(email, '@'); i >= 0 {
		local = email[:i]
	}
	if i := strings.IndexAny(local, ".+-_"); i >= 0 {
		local = local[:i]
	}
	if local == "" {
		return "there"
	}
	return strings.ToUpper(local[:1]) + local[1:]
}

// buildVerifyURL assembles the URL we embed in the verification email.
// Points at the UI's /signup/verify page; UI extracts the token and posts
// it back to POST /v1/signup/:id/verify.
func (s *Service) buildVerifyURL(signupID uuid.UUID, rawToken string) string {
	return fmt.Sprintf("%s/signup/verify?signup_id=%s&token=%s", s.appBaseURL, signupID, rawToken)
}
