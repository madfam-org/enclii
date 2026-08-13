package api

import (
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/logging"
)

// API Token Management Handlers
// Provides endpoints for creating, listing, and revoking API tokens

// ============================================================================
// REQUEST/RESPONSE TYPES
// ============================================================================

// Token lifecycle policy.
//
// These were implicit before: the cap was a bare `10` at the comparison site,
// and there was no lifetime policy at all — an absent expiry meant forever.
const (
	// maxActiveTokensPerUser bounds LIVE credentials, not rows. Expired tokens
	// are excluded by APITokenRepository.CountByUser.
	maxActiveTokensPerUser = 10

	// defaultTokenLifetimeDays applies when a caller specifies no lifetime.
	// It is deliberately not "forever": the default must be the safe case.
	defaultTokenLifetimeDays = 90

	// maxTokenLifetimeDays is the ceiling for an explicit lifetime. A year is
	// long enough for CI credentials to be practical and short enough that
	// every token is re-authorised within a normal audit cycle.
	maxTokenLifetimeDays = 365
)

// CreateAPITokenRequest represents the request to create a new API token.
type CreateAPITokenRequest struct {
	Name   string   `json:"name" binding:"required,min=1,max=100"`
	Scopes []string `json:"scopes,omitempty"` // Roles, NOT restrictions — see below
	// ExpiresIn is in days. Absent or non-positive means
	// defaultTokenLifetimeDays; values above maxTokenLifetimeDays are rejected.
	// It can no longer mean "never expires".
	ExpiresIn *int `json:"expires_in_days,omitempty"`
}

// NOTE ON SCOPES — they are ROLES, and the two auth paths disagree about them.
// middleware/auth.go sets user_roles to this list VERBATIM, replacing the
// default ["developer"]; auth/jwt_middleware.go ignores everything except the
// literal "admin", which ESCALATES the token to the admin role. So a
// well-intentioned `--scopes deploy` restricts nothing and renames the role to
// one nothing grants, while `--scopes admin` quietly issues an admin
// credential. Omitting scopes yields "developer" in both paths. The comment
// this replaces said "empty = full access", which is the opposite of what the
// middleware does.

// APITokenResponse represents a token in list responses (without the actual token)
type APITokenResponse struct {
	ID         uuid.UUID  `json:"id"`
	Name       string     `json:"name"`
	Prefix     string     `json:"prefix"`
	Scopes     []string   `json:"scopes,omitempty"`
	ExpiresAt  *time.Time `json:"expires_at,omitempty"`
	LastUsedAt *time.Time `json:"last_used_at,omitempty"`
	// LastUsedIP is where the token was last presented from. The database has
	// recorded it all along (UpdateLastUsed passes c.ClientIP()) but it was
	// absent from this struct, so it was never returned by the API.
	//
	// That gap has a cost. Deciding whether a token is safe to revoke means
	// answering "what is still using this?", and with the name being a free-text
	// label and token authentications not appearing in the audit log, the
	// originating IP was the only evidence available — and it was hidden. On
	// 2026-08-13 an account with 23 tokens had to attribute them by correlating
	// token names against workflow filenames and cron schedules by hand.
	LastUsedIP string    `json:"last_used_ip,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
	Revoked    bool      `json:"revoked"`
}

// ============================================================================
// TOKEN CRUD HANDLERS
// ============================================================================

// CreateAPIToken creates a new API token for the authenticated user
// @Summary Create API token
// @Description Create a new API token for programmatic access
// @Tags tokens
// @Accept json
// @Produce json
// @Param request body CreateAPITokenRequest true "Token creation request"
// @Success 201 {object} types.APITokenCreateResponse
// @Failure 400 {object} map[string]string
// @Failure 401 {object} map[string]string
// @Router /v1/user/tokens [post]
func (h *Handler) CreateAPIToken(c *gin.Context) {
	ctx := c.Request.Context()

	// Get user ID from context (set by auth middleware)
	userID, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User not authenticated"})
		return
	}

	uid, ok := userID.(uuid.UUID)
	if !ok {
		// Try parsing as string
		uidStr, ok := userID.(string)
		if !ok {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Invalid user ID format"})
			return
		}
		var err error
		uid, err = uuid.Parse(uidStr)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Invalid user ID"})
			return
		}
	}

	var req CreateAPITokenRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request: " + err.Error()})
		return
	}

	// Check token limit. CountByUser excludes expired tokens as well as revoked
	// ones, so the cap limits LIVE credentials rather than rows.
	count, err := h.repos.APITokens.CountByUser(ctx, uid)
	if err != nil {
		h.logger.Error(ctx, "Failed to count user tokens", logging.Error("error", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create token"})
		return
	}
	if count >= maxActiveTokensPerUser {
		// The old message said "revoke unused tokens" and stopped there, which
		// is not actionable: there is no way to tell which are unused without
		// listing them, and a user at the cap is by definition stuck. Name the
		// commands.
		c.JSON(http.StatusBadRequest, gin.H{
			"error": fmt.Sprintf(
				"Maximum of %d active tokens allowed (you have %d). "+
					"List them with `enclii tokens list` and revoke the ones you no longer "+
					"need with `enclii tokens revoke <id>`. Expired tokens no longer count "+
					"toward this limit, so anything blocking you here is still usable and "+
					"should be revoked deliberately.",
				maxActiveTokensPerUser, count),
			"active_tokens": count,
			"limit":         maxActiveTokensPerUser,
		})
		return
	}

	// Calculate expiration.
	//
	// A nil or non-positive value used to mean "never expires", which is how a
	// single account accumulated 23 immortal credentials — every one still able
	// to authenticate months after the work that needed it had finished. An
	// unbounded default is the wrong default for a credential: the safe case
	// should be the one you get by saying nothing.
	//
	// So an unspecified lifetime now means defaultTokenLifetimeDays, and any
	// explicit lifetime is capped at maxTokenLifetimeDays. Callers that relied
	// on permanence get a year of runway and a rotation deadline instead of a
	// credential that outlives the project.
	days := defaultTokenLifetimeDays
	if req.ExpiresIn != nil && *req.ExpiresIn > 0 {
		days = *req.ExpiresIn
	}
	if days > maxTokenLifetimeDays {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": fmt.Sprintf(
				"Token lifetime of %d days exceeds the maximum of %d. "+
					"A credential that outlives the context it was granted in cannot be "+
					"audited; mint a shorter one and rotate it.",
				days, maxTokenLifetimeDays),
			"max_expires_in_days": maxTokenLifetimeDays,
		})
		return
	}
	exp := time.Now().AddDate(0, 0, days)
	expiresAt := &exp

	// Create the token
	tokenResp, err := h.repos.APITokens.Create(ctx, uid, req.Name, req.Scopes, expiresAt)
	if err != nil {
		h.logger.Error(ctx, "Failed to create API token", logging.Error("error", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create token"})
		return
	}

	h.logger.Info(ctx, "API token created",
		logging.Field{Key: "token_id", Value: tokenResp.ID},
		logging.Field{Key: "name", Value: req.Name})

	c.JSON(http.StatusCreated, tokenResp)
}

// ListAPITokens lists all API tokens for the authenticated user
// @Summary List API tokens
// @Description Get all API tokens for the authenticated user
// @Tags tokens
// @Produce json
// @Param include_revoked query bool false "Include revoked tokens"
// @Success 200 {array} APITokenResponse
// @Failure 401 {object} map[string]string
// @Router /v1/user/tokens [get]
func (h *Handler) ListAPITokens(c *gin.Context) {
	ctx := c.Request.Context()

	// Get user ID from context
	userID, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User not authenticated"})
		return
	}

	uid, ok := userID.(uuid.UUID)
	if !ok {
		uidStr, ok := userID.(string)
		if !ok {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Invalid user ID format"})
			return
		}
		var err error
		uid, err = uuid.Parse(uidStr)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Invalid user ID"})
			return
		}
	}

	// Check if we should include revoked tokens
	includeRevoked := c.Query("include_revoked") == "true"

	var tokens []*APITokenResponse

	if includeRevoked {
		dbTokens, err := h.repos.APITokens.ListByUser(ctx, uid)
		if err != nil {
			h.logger.Error(ctx, "Failed to list tokens", logging.Error("error", err))
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to list tokens"})
			return
		}
		for _, t := range dbTokens {
			tokens = append(tokens, &APITokenResponse{
				ID:         t.ID,
				Name:       t.Name,
				Prefix:     t.Prefix,
				Scopes:     t.Scopes,
				ExpiresAt:  t.ExpiresAt,
				LastUsedAt: t.LastUsedAt,
				LastUsedIP: t.LastUsedIP,
				CreatedAt:  t.CreatedAt,
				Revoked:    t.Revoked,
			})
		}
	} else {
		dbTokens, err := h.repos.APITokens.ListActiveByUser(ctx, uid)
		if err != nil {
			h.logger.Error(ctx, "Failed to list tokens", logging.Error("error", err))
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to list tokens"})
			return
		}
		for _, t := range dbTokens {
			tokens = append(tokens, &APITokenResponse{
				ID:         t.ID,
				Name:       t.Name,
				Prefix:     t.Prefix,
				Scopes:     t.Scopes,
				ExpiresAt:  t.ExpiresAt,
				LastUsedAt: t.LastUsedAt,
				LastUsedIP: t.LastUsedIP,
				CreatedAt:  t.CreatedAt,
				Revoked:    t.Revoked,
			})
		}
	}

	// Return empty array instead of null
	if tokens == nil {
		tokens = []*APITokenResponse{}
	}

	c.JSON(http.StatusOK, tokens)
}

// GetAPIToken gets details for a specific token
// @Summary Get API token
// @Description Get details for a specific API token
// @Tags tokens
// @Produce json
// @Param token_id path string true "Token ID"
// @Success 200 {object} APITokenResponse
// @Failure 401 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Router /v1/user/tokens/{token_id} [get]
func (h *Handler) GetAPIToken(c *gin.Context) {
	ctx := c.Request.Context()

	// Get user ID from context
	userID, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User not authenticated"})
		return
	}

	uid, ok := userID.(uuid.UUID)
	if !ok {
		uidStr, ok := userID.(string)
		if !ok {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Invalid user ID format"})
			return
		}
		var err error
		uid, err = uuid.Parse(uidStr)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Invalid user ID"})
			return
		}
	}

	// Parse token ID from path
	tokenIDStr := c.Param("token_id")
	tokenID, err := uuid.Parse(tokenIDStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid token ID format"})
		return
	}

	// Get the token
	token, err := h.repos.APITokens.GetByID(ctx, tokenID)
	if err != nil {
		h.logger.Error(ctx, "Failed to get token", logging.Error("error", err))
		c.JSON(http.StatusNotFound, gin.H{"error": "Token not found"})
		return
	}

	// Verify ownership
	if token.UserID != uid {
		c.JSON(http.StatusNotFound, gin.H{"error": "Token not found"})
		return
	}

	c.JSON(http.StatusOK, APITokenResponse{
		ID:         token.ID,
		Name:       token.Name,
		Prefix:     token.Prefix,
		Scopes:     token.Scopes,
		ExpiresAt:  token.ExpiresAt,
		LastUsedAt: token.LastUsedAt,
		CreatedAt:  token.CreatedAt,
		Revoked:    token.Revoked,
	})
}

// RevokeAPIToken revokes (soft deletes) an API token
// @Summary Revoke API token
// @Description Revoke an API token, preventing future use
// @Tags tokens
// @Param token_id path string true "Token ID"
// @Success 204 "Token revoked"
// @Failure 401 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Router /v1/user/tokens/{token_id} [delete]
func (h *Handler) RevokeAPIToken(c *gin.Context) {
	ctx := c.Request.Context()

	// Get user ID from context
	userID, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User not authenticated"})
		return
	}

	uid, ok := userID.(uuid.UUID)
	if !ok {
		uidStr, ok := userID.(string)
		if !ok {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Invalid user ID format"})
			return
		}
		var err error
		uid, err = uuid.Parse(uidStr)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Invalid user ID"})
			return
		}
	}

	// Parse token ID from path
	tokenIDStr := c.Param("token_id")
	tokenID, err := uuid.Parse(tokenIDStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid token ID format"})
		return
	}

	// Revoke the token
	err = h.repos.APITokens.Revoke(ctx, tokenID, uid)
	if err != nil {
		h.logger.Error(ctx, "Failed to revoke token", logging.Error("error", err))
		c.JSON(http.StatusNotFound, gin.H{"error": "Token not found or already revoked"})
		return
	}

	h.logger.Info(ctx, "API token revoked", logging.Field{Key: "token_id", Value: tokenID})

	c.Status(http.StatusNoContent)
}
