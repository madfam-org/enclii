package auth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/db"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
	"github.com/sirupsen/logrus"
	"golang.org/x/oauth2"
)

// OIDCManager handles authentication via external OIDC providers (like Janua)
type OIDCManager struct {
	provider     *oidc.Provider
	verifier     *oidc.IDTokenVerifier
	oauth2Config *oauth2.Config
	repos        *db.Repositories
	jwtManager   *JWTManager     // Used for issuing local session tokens AND validating external tokens
	adminEmails  map[string]bool // email -> is admin (for OIDC fallback when tokens don't include roles)
}

// NewOIDCManager creates a new OIDC authentication manager
func NewOIDCManager(
	ctx context.Context,
	issuer string,
	clientID string,
	clientSecret string,
	redirectURL string,
	repos *db.Repositories,
	cache SessionRevoker,
	accessTokenDuration time.Duration,
	refreshTokenDuration time.Duration,
) (*OIDCManager, error) {
	return NewOIDCManagerWithExternalJWKS(ctx, issuer, clientID, clientSecret, redirectURL, repos, cache, "", "", 0, accessTokenDuration, refreshTokenDuration)
}

// NewOIDCManagerWithExternalJWKS creates an OIDC manager with external JWKS validation support
// This allows CLI/API clients to authenticate directly with external tokens (e.g., Janua)
func NewOIDCManagerWithExternalJWKS(
	ctx context.Context,
	issuer string,
	clientID string,
	clientSecret string,
	redirectURL string,
	repos *db.Repositories,
	cache SessionRevoker,
	externalJWKSURL string,
	externalIssuer string,
	jwksCacheTTL time.Duration,
	accessTokenDuration time.Duration,
	refreshTokenDuration time.Duration,
) (*OIDCManager, error) {
	// Apply defaults if not specified
	if accessTokenDuration == 0 {
		accessTokenDuration = 15 * time.Minute
	}
	if refreshTokenDuration == 0 {
		refreshTokenDuration = 7 * 24 * time.Hour
	}

	// Discover OIDC provider configuration
	provider, err := oidc.NewProvider(ctx, issuer)
	if err != nil {
		return nil, fmt.Errorf("failed to create OIDC provider: %w", err)
	}

	// Create ID token verifier
	verifier := provider.Verifier(&oidc.Config{
		ClientID: clientID,
	})

	// Configure OAuth2 client
	oauth2Config := &oauth2.Config{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		RedirectURL:  redirectURL,
		Endpoint:     provider.Endpoint(),
		Scopes:       []string{oidc.ScopeOpenID, "profile", "email"},
	}

	// Create JWT manager with external JWKS support
	var jwtManager *JWTManager
	if externalJWKSURL != "" {
		jwtManager, err = NewJWTManagerWithExternalJWKS(
			accessTokenDuration,
			refreshTokenDuration,
			repos,
			cache,
			externalJWKSURL,
			externalIssuer,
			jwksCacheTTL,
		)
	} else {
		jwtManager, err = NewJWTManager(
			accessTokenDuration,
			refreshTokenDuration,
			repos,
			cache,
		)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to create JWT manager: %w", err)
	}

	// Load admin emails from environment variable (comma-separated)
	// Example: ENCLII_ADMIN_EMAILS=admin@madfam.io,superuser@example.com
	adminEmails := make(map[string]bool)
	if adminEmailsEnv := os.Getenv("ENCLII_ADMIN_EMAILS"); adminEmailsEnv != "" {
		for _, email := range strings.Split(adminEmailsEnv, ",") {
			email = strings.TrimSpace(email)
			if email != "" {
				adminEmails[email] = true
				logrus.WithField("email", email).Info("Registered OIDC admin email")
			}
		}
	}

	return &OIDCManager{
		provider:     provider,
		verifier:     verifier,
		oauth2Config: oauth2Config,
		repos:        repos,
		jwtManager:   jwtManager,
		adminEmails:  adminEmails,
	}, nil
}

// GetAuthURL returns the OAuth authorization URL for redirecting users
func (o *OIDCManager) GetAuthURL(state string) string {
	return o.oauth2Config.AuthCodeURL(state, oauth2.AccessTypeOffline)
}

// GetSilentAuthURL returns the OAuth authorization URL with prompt=none for silent authentication
// This is used to check if the user has an active SSO session without user interaction
func (o *OIDCManager) GetSilentAuthURL(state string, redirectURL string) string {
	// Create a copy of oauth2Config with the silent callback URL
	silentConfig := *o.oauth2Config
	if redirectURL != "" {
		silentConfig.RedirectURL = redirectURL
	}
	return silentConfig.AuthCodeURL(state, oauth2.AccessTypeOffline, oauth2.SetAuthURLParam("prompt", "none"))
}

// GetRedirectURL returns the configured redirect URL
func (o *OIDCManager) GetRedirectURL() string {
	return o.oauth2Config.RedirectURL
}

// loadUserProjectIDs loads the project IDs that a user has access to
func (o *OIDCManager) loadUserProjectIDs(ctx context.Context, userID uuid.UUID) []string {
	access, err := o.repos.ProjectAccess.ListByUser(ctx, userID)
	if err != nil {
		logrus.WithError(err).WithField("user_id", userID).Warn("Failed to load user project access")
		return []string{}
	}

	// Extract unique project IDs
	projectIDSet := make(map[string]bool)
	for _, a := range access {
		projectIDSet[a.ProjectID.String()] = true
	}

	projectIDs := make([]string, 0, len(projectIDSet))
	for id := range projectIDSet {
		projectIDs = append(projectIDs, id)
	}
	return projectIDs
}

// HandleCallback processes the OAuth callback and creates/updates user
func (o *OIDCManager) HandleCallback(ctx context.Context, code string) (*TokenPair, error) {
	// Exchange authorization code for token
	oauth2Token, err := o.oauth2Config.Exchange(ctx, code)
	if err != nil {
		return nil, fmt.Errorf("failed to exchange token: %w", err)
	}

	// Extract ID token from OAuth2 token
	rawIDToken, ok := oauth2Token.Extra("id_token").(string)
	if !ok {
		return nil, fmt.Errorf("no id_token in token response")
	}

	// Verify ID token signature and claims
	idToken, err := o.verifier.Verify(ctx, rawIDToken)
	if err != nil {
		return nil, fmt.Errorf("failed to verify ID token: %w", err)
	}

	// Extract standard claims
	var claims struct {
		Email         string `json:"email"`
		EmailVerified bool   `json:"email_verified"`
		Name          string `json:"name"`
		Sub           string `json:"sub"`
	}
	if err := idToken.Claims(&claims); err != nil {
		return nil, fmt.Errorf("failed to parse claims: %w", err)
	}

	// Require verified email
	if !claims.EmailVerified {
		return nil, fmt.Errorf("email not verified by OIDC provider")
	}

	// Get or create user
	user, err := o.getOrCreateUser(ctx, &claims, idToken.Issuer)
	if err != nil {
		return nil, fmt.Errorf("failed to get/create user: %w", err)
	}

	// Generate local JWT session tokens
	tokens, err := o.jwtManager.GenerateTokenPair(user)
	if err != nil {
		return nil, fmt.Errorf("failed to generate session tokens: %w", err)
	}

	// Preserve the IDP access token for calling IDP-specific APIs
	// (e.g., Janua's OAuth account linking endpoint)
	if oauth2Token.AccessToken != "" {
		tokens.IDPToken = oauth2Token.AccessToken
		if !oauth2Token.Expiry.IsZero() {
			tokens.IDPTokenExpiresAt = &oauth2Token.Expiry
		}
	}

	logrus.WithFields(logrus.Fields{
		"user_id":       user.ID,
		"email":         user.Email,
		"oidc_subject":  claims.Sub,
		"oidc_issuer":   idToken.Issuer,
		"has_idp_token": tokens.IDPToken != "",
	}).Info("User authenticated via OIDC")

	return tokens, nil
}

// getOrCreateUser finds an existing user or creates a new one from OIDC claims
func (o *OIDCManager) getOrCreateUser(ctx context.Context, claims *struct {
	Email         string `json:"email"`
	EmailVerified bool   `json:"email_verified"`
	Name          string `json:"name"`
	Sub           string `json:"sub"`
}, issuer string) (*User, error) {
	// Try to find user by OIDC identity (issuer + subject)
	user, err := o.repos.Users.GetByOIDCIdentity(ctx, issuer, claims.Sub)
	if err == nil {
		// User found by OIDC identity
		logrus.WithFields(logrus.Fields{
			"user_id":      user.ID,
			"oidc_subject": claims.Sub,
		}).Debug("Found user by OIDC identity")
		return &User{
			ID:         user.ID,
			Email:      user.Email,
			Name:       user.Name,
			Role:       user.Role,
			ProjectIDs: o.loadUserProjectIDs(ctx, user.ID),
			Active:     user.Active,
		}, nil
	}

	// Try to find user by email (migration from local auth)
	user, err = o.repos.Users.GetByEmail(ctx, claims.Email)
	if err == nil {
		// User exists from local auth - link to OIDC identity
		logrus.WithFields(logrus.Fields{
			"user_id":      user.ID,
			"email":        claims.Email,
			"oidc_subject": claims.Sub,
		}).Info("Linking existing user to OIDC identity")

		// Update user with OIDC identity
		user.OIDCSubject = &claims.Sub
		user.OIDCIssuer = &issuer
		if err := o.repos.Users.Update(ctx, user); err != nil {
			return nil, fmt.Errorf("failed to link OIDC identity: %w", err)
		}

		return &User{
			ID:         user.ID,
			Email:      user.Email,
			Name:       user.Name,
			Role:       user.Role,
			ProjectIDs: o.loadUserProjectIDs(ctx, user.ID),
			Active:     user.Active,
		}, nil
	}

	// User doesn't exist - create new user from OIDC claims
	logrus.WithFields(logrus.Fields{
		"email":        claims.Email,
		"oidc_subject": claims.Sub,
	}).Info("Creating new user from OIDC")

	newUser := &types.User{
		ID:           uuid.New(),
		Email:        claims.Email,
		Name:         claims.Name,
		Role:         "developer", // Default role for new OIDC users
		Active:       true,
		OIDCSubject:  &claims.Sub,
		OIDCIssuer:   &issuer,
		PasswordHash: "", // No password for OIDC-only users
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}

	if err := o.repos.Users.Create(ctx, newUser); err != nil {
		return nil, fmt.Errorf("failed to create user: %w", err)
	}

	return &User{
		ID:         newUser.ID,
		Email:      newUser.Email,
		Name:       newUser.Name,
		Role:       newUser.Role,
		ProjectIDs: o.loadUserProjectIDs(ctx, newUser.ID),
		Active:     newUser.Active,
	}, nil
}

// AuthMiddleware returns a Gin middleware for OIDC authentication
// Implements dual-mode validation: local tokens first, then external tokens
// Supports both Authorization header and query parameter (for WebSocket connections)
func (o *OIDCManager) AuthMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		var tokenString string

		// Try Authorization header first (standard method)
		authHeader := c.GetHeader("Authorization")
		if authHeader != "" {
			bearerToken := strings.Split(authHeader, " ")
			if len(bearerToken) == 2 && bearerToken[0] == "Bearer" {
				tokenString = bearerToken[1]
			}
		}

		// Fall back to query parameter (for WebSocket connections)
		// WebSocket API doesn't support custom headers, so token is passed via query param
		if tokenString == "" {
			tokenString = c.Query("token")
		}

		if tokenString == "" {
			c.JSON(401, gin.H{"error": "Authorization required (header or token query param)"})
			c.Abort()
			return
		}

		// Check if this is an API token (starts with "enclii_")
		if strings.HasPrefix(tokenString, "enclii_") {
			if !o.jwtManager.HasAPITokenValidator() {
				logrus.WithFields(logrus.Fields{
					"path":   c.Request.URL.Path,
					"method": c.Request.Method,
					"ip":     c.ClientIP(),
				}).Warn("API token authentication not configured")

				c.JSON(401, gin.H{"error": "API token authentication not available"})
				c.Abort()
				return
			}

			// Validate the API token via jwtManager
			apiToken, err := o.jwtManager.apiTokenValidator.ValidateTokenForAuth(c.Request.Context(), tokenString)
			if err != nil {
				logrus.WithFields(logrus.Fields{
					"path":   c.Request.URL.Path,
					"method": c.Request.Method,
					"ip":     c.ClientIP(),
					"error":  err.Error(),
				}).Warn("Invalid API token")

				c.JSON(401, gin.H{"error": "Invalid or expired API token"})
				c.Abort()
				return
			}

			// Set user context from API token
			c.Set("user_id", apiToken.UserID.String())
			c.Set("auth_type", "api_token")
			c.Set("api_token_id", apiToken.ID)
			c.Set("api_token_name", apiToken.Name)

			// API tokens get developer role by default (scoped by token scopes if needed)
			userRole := "developer"
			if len(apiToken.Scopes) > 0 {
				for _, scope := range apiToken.Scopes {
					if scope == "admin" {
						userRole = "admin"
						break
					}
				}
			}
			c.Set("user_role", userRole)

			// Update last used timestamp (async)
			go func() {
				if err := o.jwtManager.apiTokenValidator.UpdateLastUsed(context.Background(), apiToken.ID, c.ClientIP()); err != nil {
					logrus.WithFields(logrus.Fields{
						"token_id": apiToken.ID,
						"error":    err.Error(),
					}).Warn("Failed to update API token last used")
				}
			}()

			logrus.WithFields(logrus.Fields{
				"path":       c.Request.URL.Path,
				"method":     c.Request.Method,
				"user_id":    apiToken.UserID,
				"token_id":   apiToken.ID,
				"token_name": apiToken.Name,
			}).Debug("API token authentication successful")

			c.Next()
			return
		}

		// Try local token validation first
		localClaims, localErr := o.jwtManager.ValidateToken(tokenString)
		if localErr == nil {
			// Local token valid - set context and continue
			c.Set("user_id", localClaims.UserID.String())
			c.Set("user_email", localClaims.Email)
			c.Set("user_role", localClaims.Role)
			c.Set("project_ids", localClaims.ProjectIDs)
			c.Set("claims", localClaims)
			c.Set("token_source", "local")

			// Audit: Log successful local token validation
			LogTokenValidated(localClaims.UserID, localClaims.Email, "local")

			c.Next()
			return
		}

		// Local validation failed - try external JWKS if configured
		if o.jwtManager.HasExternalJWKS() {
			externalClaims, externalErr := o.jwtManager.ValidateExternalToken(tokenString)
			if externalErr == nil {
				// External token valid - get or create user
				user, isNew, err := o.getOrCreateUserFromExternalTokenWithStatus(c.Request.Context(), externalClaims)
				if err != nil {
					logrus.WithError(err).Error("Failed to get/create user from external token")
					c.JSON(401, gin.H{"error": "Failed to process external token"})
					c.Abort()
					return
				}

				// Set context with user info from external token
				c.Set("user_id", user.ID.String())
				c.Set("user_email", user.Email)

				// Apply admin email override if user's email is in adminEmails map
				// This enables OIDC providers (like Janua) that don't include roles in tokens
				userRole := user.Role
				if o.adminEmails[user.Email] {
					userRole = "admin"
					logrus.WithFields(logrus.Fields{
						"email":         user.Email,
						"original_role": user.Role,
						"new_role":      "admin",
					}).Info("Applied admin role based on email mapping")
				}
				c.Set("user_role", userRole)
				c.Set("project_ids", user.ProjectIDs)
				c.Set("token_source", "external")
				c.Set("external_issuer", externalClaims.Issuer)

				// Extract foundry_tier from external token claims (set by Janua after Dhanam purchase)
				if externalClaims.FoundryTier != "" {
					c.Set("foundry_tier", externalClaims.FoundryTier)
				}

				// Audit: Log external token validation and user creation/linking
				LogExternalTokenValidated(user.ID, user.Email, externalClaims.Issuer)
				if isNew {
					LogExternalUserCreated(user.ID, user.Email, externalClaims.Issuer)
				}

				logrus.WithFields(logrus.Fields{
					"user_id": user.ID,
					"email":   user.Email,
					"issuer":  externalClaims.Issuer,
				}).Info("User authenticated via external token")

				c.Next()
				return
			}

			logrus.WithFields(logrus.Fields{
				"local_error":    localErr.Error(),
				"external_error": externalErr.Error(),
			}).Debug("Both local and external token validation failed")
		}

		// Both validations failed
		logrus.Warnf("Token validation failed: %v", localErr)
		c.JSON(401, gin.H{"error": "Invalid or expired token"})
		c.Abort()
	}
}

// getOrCreateUserFromExternalToken creates or updates a user from external token claims
func (o *OIDCManager) getOrCreateUserFromExternalToken(ctx context.Context, claims *ExternalClaims) (*User, error) {
	user, _, err := o.getOrCreateUserFromExternalTokenWithStatus(ctx, claims)
	return user, err
}

// getOrCreateUserFromExternalTokenWithStatus creates or updates a user from external token claims
// Returns the user, whether a new user was created, and any error
func (o *OIDCManager) getOrCreateUserFromExternalTokenWithStatus(ctx context.Context, claims *ExternalClaims) (*User, bool, error) {
	issuer := claims.Issuer

	// Try to find user by OIDC identity
	user, err := o.repos.Users.GetByOIDCIdentity(ctx, issuer, claims.Subject)
	if err == nil {
		return &User{
			ID:         user.ID,
			Email:      user.Email,
			Name:       user.Name,
			Role:       user.Role,
			ProjectIDs: o.loadUserProjectIDs(ctx, user.ID),
			Active:     user.Active,
		}, false, nil
	}

	// Try to find by email
	user, err = o.repos.Users.GetByEmail(ctx, claims.Email)
	if err == nil {
		// Link existing user to external identity
		user.OIDCSubject = &claims.Subject
		user.OIDCIssuer = &issuer
		if err := o.repos.Users.Update(ctx, user); err != nil {
			return nil, false, fmt.Errorf("failed to link external identity: %w", err)
		}

		// Audit: Log user linking
		LogExternalUserLinked(user.ID, claims.Email, issuer)

		logrus.WithFields(logrus.Fields{
			"user_id": user.ID,
			"email":   claims.Email,
			"issuer":  issuer,
		}).Info("Linked existing user to external identity")

		return &User{
			ID:         user.ID,
			Email:      user.Email,
			Name:       user.Name,
			Role:       user.Role,
			ProjectIDs: o.loadUserProjectIDs(ctx, user.ID),
			Active:     user.Active,
		}, false, nil
	}

	// Create new user
	newUser := &types.User{
		ID:           uuid.New(),
		Email:        claims.Email,
		Name:         claims.Name,
		Role:         "developer",
		Active:       true,
		OIDCSubject:  &claims.Subject,
		OIDCIssuer:   &issuer,
		PasswordHash: "",
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}

	if err := o.repos.Users.Create(ctx, newUser); err != nil {
		return nil, false, fmt.Errorf("failed to create user: %w", err)
	}

	logrus.WithFields(logrus.Fields{
		"user_id": newUser.ID,
		"email":   claims.Email,
		"issuer":  issuer,
	}).Info("Created user from external token")

	return &User{
		ID:         newUser.ID,
		Email:      newUser.Email,
		Name:       newUser.Name,
		Role:       newUser.Role,
		ProjectIDs: o.loadUserProjectIDs(ctx, newUser.ID),
		Active:     newUser.Active,
	}, true, nil
}

// RequireRole returns a middleware that requires specific roles
func (o *OIDCManager) RequireRole(roles ...string) gin.HandlerFunc {
	return o.jwtManager.RequireRole(roles...)
}

// SetAPITokenValidator sets the API token validator for API token authentication
// This enables authentication via API tokens (enclii_xxx format) in addition to OIDC
func (o *OIDCManager) SetAPITokenValidator(validator APITokenValidator) {
	o.jwtManager.SetAPITokenValidator(validator)
}

// GenerateState generates a cryptographically random state parameter for CSRF protection
func GenerateState() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.URLEncoding.EncodeToString(b), nil
}

// Logout revokes the session (same as JWT manager)
func (o *OIDCManager) Logout(ctx context.Context, sessionID string) error {
	return o.jwtManager.RevokeSession(ctx, sessionID)
}

// ValidateToken validates a JWT token (for API access)
func (o *OIDCManager) ValidateToken(tokenString string) (*Claims, error) {
	return o.jwtManager.ValidateToken(tokenString)
}

// RefreshToken refreshes an access token using a refresh token
func (o *OIDCManager) RefreshToken(ctx context.Context, refreshToken string) (*TokenPair, error) {
	return o.jwtManager.RefreshToken(refreshToken)
}
