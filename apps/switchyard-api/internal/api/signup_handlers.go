package api

import (
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/logging"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/middleware"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/signup"
)

// registerSignupRoutes mounts the P3.2 Sprint 1 self-serve signup surface
// under v1. Extracted from handlers.go to keep that file under the
// 800-line budget enforced by pre-commit. All routes are public (no auth)
// and gate internally on the ENCLII_SIGNUP_ENABLED flag; rate limits are
// tuned per-endpoint to balance wizard UX (frequent status polls) with
// brute-force resistance on token/oauth endpoints.
func registerSignupRoutes(
	v1 *gin.RouterGroup,
	h *Handler,
	authRateLimiter *middleware.RateLimiter,
	strictAuthRateLimiter *middleware.RateLimiter,
) {
	initiate := middleware.RateLimitByIP(5, time.Hour)  // 5/hour/IP per spec
	status := middleware.RateLimitByIP(30, time.Minute) // UI polls every ~2s

	v1.POST("/signup", initiate, h.InitiateSignup)
	v1.GET("/signup/:id/status", status, h.GetSignupStatus)
	v1.POST("/signup/:id/verify", strictAuthRateLimiter.Middleware(), h.VerifySignupEmail)
	v1.POST("/signup/:id/resend", initiate, h.ResendSignupVerification)
	v1.GET("/signup/:id/github/authorize", authRateLimiter.Middleware(), h.AuthorizeGithubForSignup)
	v1.GET("/signup/:id/github/callback", h.GithubCallbackForSignup)
	v1.POST("/signup/:id/provision", authRateLimiter.Middleware(), h.ProvisionSignup)
}

// P3.2 Sprint 1 — self-serve signup HTTP surface.
//
// All endpoints under this file are intentionally public (no auth
// required). The flow is rate-limited per-IP by the route registration in
// handlers.go. When ENCLII_SIGNUP_ENABLED=false the handlers return
// 404 so the surface is not discoverable.

// InitiateSignup handles POST /v1/signup.
func (h *Handler) InitiateSignup(c *gin.Context) {
	if h.signupService == nil || !h.signupService.IsEnabled() {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	ctx := c.Request.Context()

	var req signup.InitiateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	resp, err := h.signupService.Initiate(ctx, req)
	if err != nil {
		h.logger.Warn(ctx, "signup: initiate failed",
			logging.String("email", req.Email),
			logging.Error("error", err))
		switch {
		case errors.Is(err, signup.ErrSignupDisabled):
			c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		case errors.Is(err, signup.ErrInvalidEmail):
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid email"})
		default:
			c.JSON(http.StatusInternalServerError, gin.H{"error": "signup initiate failed"})
		}
		return
	}
	c.JSON(http.StatusCreated, resp)
}

// GetSignupStatus handles GET /v1/signup/:id/status.
func (h *Handler) GetSignupStatus(c *gin.Context) {
	if h.signupService == nil || !h.signupService.IsEnabled() {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	ctx := c.Request.Context()

	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid signup id"})
		return
	}
	sr, err := h.signupService.GetStatus(ctx, id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "signup not found"})
		return
	}

	resp := gin.H{
		"signup_id":  sr.ID,
		"email":      sr.Email,
		"status":     sr.Status,
		"next_step":  signup.NextStepFor(sr.Status),
		"created_at": sr.CreatedAt,
		"updated_at": sr.UpdatedAt,
	}
	if sr.ProvisionedProjectID != nil {
		resp["project_id"] = sr.ProvisionedProjectID
	}
	if sr.GithubUsername != nil {
		resp["github_username"] = *sr.GithubUsername
	}
	if sr.ErrorMessage != nil {
		resp["error_message"] = *sr.ErrorMessage
	}
	c.JSON(http.StatusOK, resp)
}

// ResendSignupVerification handles POST /v1/signup/:id/resend — re-sends verification email.
func (h *Handler) ResendSignupVerification(c *gin.Context) {
	if h.signupService == nil || !h.signupService.IsEnabled() {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	ctx := c.Request.Context()

	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid signup id"})
		return
	}

	resp, err := h.signupService.ResendVerification(ctx, id)
	if err != nil {
		switch {
		case errors.Is(err, signup.ErrSignupNotFound):
			c.JSON(http.StatusNotFound, gin.H{"error": "signup not found"})
		case errors.Is(err, signup.ErrWrongStateForTransition):
			c.JSON(http.StatusConflict, gin.H{"error": "signup not awaiting verification"})
		default:
			h.logger.Error(ctx, "signup: resend verification failed", logging.Error("error", err))
			c.JSON(http.StatusInternalServerError, gin.H{"error": "resend failed"})
		}
		return
	}
	c.JSON(http.StatusOK, resp)
}

// VerifySignupEmailRequest is the body of POST /v1/signup/:id/verify.
type VerifySignupEmailRequest struct {
	Token string `json:"token" binding:"required"`
}

// VerifySignupEmail handles POST /v1/signup/:id/verify.
func (h *Handler) VerifySignupEmail(c *gin.Context) {
	if h.signupService == nil || !h.signupService.IsEnabled() {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	ctx := c.Request.Context()

	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid signup id"})
		return
	}

	var body VerifySignupEmailRequest
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	sr, err := h.signupService.VerifyEmail(ctx, id, body.Token)
	if err != nil {
		switch {
		case errors.Is(err, signup.ErrInvalidToken):
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid or expired token"})
		case errors.Is(err, signup.ErrSignupNotFound):
			c.JSON(http.StatusNotFound, gin.H{"error": "signup not found"})
		case errors.Is(err, signup.ErrEmailAlreadyRegistered):
			c.JSON(http.StatusConflict, gin.H{
				"error":     "email already registered",
				"next_step": "login",
			})
		default:
			h.logger.Error(ctx, "signup: verify failed", logging.Error("error", err))
			c.JSON(http.StatusInternalServerError, gin.H{"error": "verify failed"})
		}
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"signup_id": sr.ID,
		"status":    sr.Status,
		"next_step": signup.NextStepFor(sr.Status),
	})
}

// AuthorizeGithubForSignup handles GET /v1/signup/:id/github/authorize.
// Returns the URL the UI should redirect the browser to.
func (h *Handler) AuthorizeGithubForSignup(c *gin.Context) {
	if h.signupService == nil || !h.signupService.IsEnabled() {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	ctx := c.Request.Context()

	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid signup id"})
		return
	}

	resp, err := h.signupService.AuthorizeGithub(ctx, id)
	if err != nil {
		switch {
		case errors.Is(err, signup.ErrSignupNotFound):
			c.JSON(http.StatusNotFound, gin.H{"error": "signup not found"})
		case errors.Is(err, signup.ErrWrongStateForTransition):
			c.JSON(http.StatusConflict, gin.H{"error": "signup not in verified state"})
		default:
			h.logger.Error(ctx, "signup: authorize github failed", logging.Error("error", err))
			c.JSON(http.StatusInternalServerError, gin.H{"error": "authorize failed"})
		}
		return
	}
	c.JSON(http.StatusOK, resp)
}

// GithubCallbackForSignup handles GET /v1/signup/:id/github/callback.
// Called by Janua after the user authorizes GitHub. On success we 302
// the browser to the UI so the wizard can poll /status.
func (h *Handler) GithubCallbackForSignup(c *gin.Context) {
	if h.signupService == nil || !h.signupService.IsEnabled() {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	ctx := c.Request.Context()

	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid signup id"})
		return
	}

	code := c.Query("code")
	state := c.Query("state")
	if oauthErr := c.Query("error"); oauthErr != "" {
		// User denied or upstream rejected. Redirect back to UI with the
		// error so it can render a "try again" CTA.
		ui := h.redirectBackToWizard(id, "oauth_denied")
		c.Redirect(http.StatusFound, ui)
		return
	}

	if _, err := h.signupService.LinkGithub(ctx, id, code, state); err != nil {
		h.logger.Warn(ctx, "signup: link github failed",
			logging.String("signup_id", id.String()),
			logging.Error("error", err))
		ui := h.redirectBackToWizard(id, "oauth_failed")
		c.Redirect(http.StatusFound, ui)
		return
	}
	// Success — send the browser back to the wizard's "finishing" step.
	// The UI then calls POST /v1/signup/:id/provision.
	ui := h.redirectBackToWizard(id, "")
	c.Redirect(http.StatusFound, ui)
}

// redirectBackToWizard constructs the UI URL the user should land on
// after OAuth completes (or fails). Kept as a method so it uses the
// handler's configured AppBaseURL.
func (h *Handler) redirectBackToWizard(signupID uuid.UUID, errCode string) string {
	base := h.config.AppBaseURL
	if base == "" {
		base = "https://app.enclii.dev"
	}
	url := base + "/signup?signup_id=" + signupID.String()
	if errCode != "" {
		url += "&error=" + errCode
	}
	return url
}

// ProvisionSignup handles POST /v1/signup/:id/provision.
func (h *Handler) ProvisionSignup(c *gin.Context) {
	if h.signupService == nil || !h.signupService.IsEnabled() {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	ctx := c.Request.Context()

	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid signup id"})
		return
	}

	resp, err := h.signupService.Provision(ctx, id)
	if err != nil {
		switch {
		case errors.Is(err, signup.ErrSignupNotFound):
			c.JSON(http.StatusNotFound, gin.H{"error": "signup not found"})
		case errors.Is(err, signup.ErrWrongStateForTransition):
			c.JSON(http.StatusConflict, gin.H{"error": "signup not ready to provision"})
		default:
			h.logger.Error(ctx, "signup: provision failed", logging.Error("error", err))
			c.JSON(http.StatusInternalServerError, gin.H{"error": "provision failed"})
		}
		return
	}
	c.JSON(http.StatusOK, resp)
}
