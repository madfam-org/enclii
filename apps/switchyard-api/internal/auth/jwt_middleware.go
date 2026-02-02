package auth

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
)

// AuthMiddleware supports both Authorization header and query parameter (for WebSocket connections)
func (j *JWTManager) AuthMiddleware() gin.HandlerFunc {
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
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Authorization required (header or token query param)"})
			c.Abort()
			return
		}

		// Check if this is an API token (starts with "enclii_")
		if strings.HasPrefix(tokenString, "enclii_") {
			j.handleAPITokenAuth(c, tokenString)
			return
		}

		// Try local token validation first
		claims, err := j.ValidateToken(tokenString)
		if err == nil {
			// Local token validated successfully
			c.Set("user_id", claims.UserID.String())
			c.Set("user_email", claims.Email)
			c.Set("user_role", claims.Role)
			c.Set("project_ids", claims.ProjectIDs)
			c.Set("claims", claims)
			c.Next()
			return
		}

		// Local token validation failed - try external JWKS validation if configured
		if j.HasExternalJWKS() {
			externalClaims, externalErr := j.ValidateExternalToken(tokenString)
			if externalErr == nil {
				// External token validated successfully
				logrus.WithFields(logrus.Fields{
					"email":  externalClaims.Email,
					"issuer": externalClaims.Issuer,
				}).Debug("User authenticated via external token")

				// Use subject as user_id string (handlers expect string, not uuid.UUID)
				userID := externalClaims.Subject

				// Determine role - default to developer, but check admin email mapping
				userRole := "developer"
				if j.adminEmails != nil && j.adminEmails[externalClaims.Email] {
					userRole = "admin"
					logrus.WithFields(logrus.Fields{
						"email":         externalClaims.Email,
						"original_role": "developer",
						"new_role":      "admin",
					}).Info("Applied admin role based on email mapping")
				}

				c.Set("user_id", userID)
				c.Set("user_email", externalClaims.Email)
				c.Set("user_role", userRole)
				c.Set("project_ids", []string{})
				c.Set("external_token", true)

				// Extract foundry_tier from external token claims
				if externalClaims.FoundryTier != "" {
					c.Set("foundry_tier", externalClaims.FoundryTier)
				}

				c.Next()
				return
			}
			logrus.WithError(externalErr).Debug("External token validation also failed")
		}

		// Both validations failed
		logrus.Warnf("Token validation failed: %v", err)
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid or expired token"})
		c.Abort()
	}
}

// handleAPITokenAuth handles authentication via API tokens (enclii_xxx format)
func (j *JWTManager) handleAPITokenAuth(c *gin.Context, tokenString string) {
	if j.apiTokenValidator == nil {
		logrus.WithFields(logrus.Fields{
			"path":   c.Request.URL.Path,
			"method": c.Request.Method,
			"ip":     c.ClientIP(),
		}).Warn("API token authentication not configured")

		c.JSON(http.StatusUnauthorized, gin.H{"error": "API token authentication not available"})
		c.Abort()
		return
	}

	// Validate the API token
	apiToken, err := j.apiTokenValidator.ValidateTokenForAuth(c.Request.Context(), tokenString)
	if err != nil {
		logrus.WithFields(logrus.Fields{
			"path":   c.Request.URL.Path,
			"method": c.Request.Method,
			"ip":     c.ClientIP(),
			"error":  err.Error(),
		}).Warn("Invalid API token")

		c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid or expired API token"})
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
		// Check if admin scope is present
		for _, scope := range apiToken.Scopes {
			if scope == "admin" {
				userRole = "admin"
				break
			}
		}
	}
	c.Set("user_role", userRole)

	// Update last used timestamp (async, don't block the request)
	go func() {
		if err := j.apiTokenValidator.UpdateLastUsed(context.Background(), apiToken.ID, c.ClientIP()); err != nil {
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
}

func (j *JWTManager) RequireRole(roles ...string) gin.HandlerFunc {
	return func(c *gin.Context) {
		userRole, exists := c.Get("user_role")
		if !exists {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "User role not found"})
			c.Abort()
			return
		}

		roleStr, ok := userRole.(string)
		if !ok {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Invalid role format"})
			c.Abort()
			return
		}

		// Check if user has required role with hierarchy support
		// Role hierarchy: admin > developer > viewer
		// admin can do anything developer or viewer can do
		// developer can do anything viewer can do
		hasRole := false
		for _, role := range roles {
			if roleStr == role {
				hasRole = true
				break
			}
			// Apply role hierarchy: superadmin and admin have all permissions
			if roleStr == "superadmin" || roleStr == "admin" {
				hasRole = true
				break
			}
			// developer can do viewer tasks
			if roleStr == "developer" && role == "viewer" {
				hasRole = true
				break
			}
		}

		if !hasRole {
			c.JSON(http.StatusForbidden, gin.H{
				"error": fmt.Sprintf("Required role: %v, current role: %s", roles, roleStr),
			})
			c.Abort()
			return
		}

		c.Next()
	}
}

func (j *JWTManager) RequireProjectAccess() gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := c.Request.Context()

		// Get project slug from URL params
		projectSlug := c.Param("slug")
		if projectSlug == "" {
			// No project in URL, skip check
			c.Next()
			return
		}

		// Get user ID from context (set by AuthMiddleware)
		userID, err := GetUserIDFromContext(c)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "User not authenticated"})
			c.Abort()
			return
		}

		// Get user role from context
		roleStr, exists := c.Get("role")
		if !exists {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "User role not found"})
			c.Abort()
			return
		}

		// Admin users have access to all projects
		if roleStr == "admin" {
			c.Next()
			return
		}

		// Check if repos are available
		if j.repos == nil {
			logrus.Warn("Project access repository not available, allowing request")
			c.Next()
			return
		}

		// Get project by slug
		project, err := j.repos.Projects.GetBySlug(projectSlug)
		if err != nil {
			if err == sql.ErrNoRows {
				c.JSON(http.StatusNotFound, gin.H{"error": "Project not found"})
			} else {
				logrus.WithError(err).Error("Failed to get project by slug")
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to retrieve project"})
			}
			c.Abort()
			return
		}

		// Check if user has access to this specific project
		hasAccess, err := j.repos.ProjectAccess.UserHasAccess(ctx, userID, project.ID)
		if err != nil {
			logrus.WithError(err).Error("Failed to check project access")
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to verify project access"})
			c.Abort()
			return
		}

		if !hasAccess {
			c.JSON(http.StatusForbidden, gin.H{
				"error": fmt.Sprintf("You don't have access to project '%s'", projectSlug),
			})
			c.Abort()
			return
		}

		// User has access, store project ID in context for later use
		c.Set("project_id", project.ID)
		c.Next()
	}
}
