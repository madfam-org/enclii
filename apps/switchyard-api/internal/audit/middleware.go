package audit

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/db"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// Middleware provides audit logging for API requests
type Middleware struct {
	repos       *db.Repositories
	asyncLogger *AsyncLogger
}

// NewMiddleware creates a new audit middleware
func NewMiddleware(repos *db.Repositories) *Middleware {
	return &Middleware{
		repos:       repos,
		asyncLogger: NewAsyncLogger(repos, 100, ""), // Buffer size 100, default fallback path
	}
}

// AuditMiddleware is a Gin middleware that logs all API mutations
func (m *Middleware) AuditMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		// Only audit mutations (POST, PUT, PATCH, DELETE)
		if !shouldAudit(c.Request.Method) {
			c.Next()
			return
		}

		// Extract user context from JWT (set by auth middleware)
		actorID, actorEmail, actorRole := extractActorInfo(c)
		if actorID == uuid.Nil {
			// No authenticated user - skip audit (public endpoints)
			c.Next()
			return
		}

		// Capture request body for context
		var requestBody map[string]interface{}
		if c.Request.Body != nil {
			bodyBytes, err := io.ReadAll(c.Request.Body)
			if err == nil {
				// Restore body for handler
				c.Request.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))

				// Parse JSON body (ignore errors for non-JSON)
				json.Unmarshal(bodyBytes, &requestBody)
			}
		}

		// Extract resource info from path
		resourceType, resourceID, resourceName := extractResourceInfo(c)

		// Record start time
		startTime := time.Now()

		// Process request
		c.Next()

		// Determine outcome based on status code
		statusCode := c.Writer.Status()
		outcome := determineOutcome(statusCode)

		// Build audit log - use nil for OIDC users (no local user row)
		var actorIDPtr *uuid.UUID
		if actorID != uuid.Nil {
			actorIDPtr = &actorID
		}
		auditLog := &types.AuditLog{
			ActorID:      actorIDPtr,
			ActorEmail:   actorEmail,
			ActorRole:    actorRole,
			Action:       buildAction(c.Request.Method, c.FullPath()),
			ResourceType: resourceType,
			ResourceID:   resourceID,
			ResourceName: resourceName,
			IPAddress:    c.ClientIP(),
			UserAgent:    c.Request.UserAgent(),
			Outcome:      outcome,
			Context: map[string]interface{}{
				"method":       c.Request.Method,
				"path":         c.Request.URL.Path,
				"query":        c.Request.URL.RawQuery,
				"status_code":  statusCode,
				"duration_ms":  time.Since(startTime).Milliseconds(),
				"request_body": sanitizeRequestBody(requestBody),
			},
			Metadata: map[string]interface{}{
				"route":  c.FullPath(),
				"params": c.Params,
			},
		}

		// Extract project/environment IDs if present
		if projectID := c.Param("project_id"); projectID != "" {
			if pid, err := uuid.Parse(projectID); err == nil {
				auditLog.ProjectID = &pid
			}
		}
		if envID := c.Param("environment_id"); envID != "" {
			if eid, err := uuid.Parse(envID); err == nil {
				auditLog.EnvironmentID = &eid
			}
		}

		// Log asynchronously (non-blocking)
		m.asyncLogger.Log(auditLog)
	}
}

// shouldAudit determines if a request should be audited
func shouldAudit(method string) bool {
	// Only audit mutations
	mutations := []string{"POST", "PUT", "PATCH", "DELETE"}
	for _, m := range mutations {
		if method == m {
			return true
		}
	}
	return false
}

// extractActorInfo extracts user information from Gin context
func extractActorInfo(c *gin.Context) (uuid.UUID, string, types.Role) {
	// Extract from context (set by auth middleware)
	userIDStr, exists := c.Get("user_id")
	if !exists {
		return uuid.Nil, "", ""
	}

	var userID uuid.UUID
	switch v := userIDStr.(type) {
	case string:
		userID, _ = uuid.Parse(v)
	case uuid.UUID:
		userID = v
	}

	email, _ := c.Get("email")
	emailStr, _ := email.(string)

	role, _ := c.Get("role")
	roleStr, _ := role.(string)

	return userID, emailStr, types.Role(roleStr)
}

// extractResourceInfo extracts resource type and ID from the request
func extractResourceInfo(c *gin.Context) (resourceType, resourceID, resourceName string) {
	path := c.Request.URL.Path

	// Determine resource type from path
	if strings.Contains(path, "/projects") {
		resourceType = "project"
		resourceID = c.Param("slug")
		if resourceID == "" {
			resourceID = c.Param("project_id")
		}
	} else if strings.Contains(path, "/services") {
		resourceType = "service"
		resourceID = c.Param("id")
		if resourceID == "" {
			resourceID = c.Param("service_id")
		}
	} else if strings.Contains(path, "/deployments") {
		resourceType = "deployment"
		resourceID = c.Param("id")
		if resourceID == "" {
			resourceID = c.Param("deployment_id")
		}
	} else if strings.Contains(path, "/releases") {
		resourceType = "release"
		resourceID = c.Param("id")
		if resourceID == "" {
			resourceID = c.Param("release_id")
		}
	} else if strings.Contains(path, "/environments") {
		resourceType = "environment"
		resourceID = c.Param("id")
		if resourceID == "" {
			resourceID = c.Param("environment_id")
		}
	} else {
		// Generic resource
		resourceType = "api"
		resourceID = path
	}

	return resourceType, resourceID, resourceName
}

// buildAction builds a human-readable action string
func buildAction(method, path string) string {
	// Convert HTTP method to action verb
	var action string
	switch method {
	case "POST":
		if strings.Contains(path, "/build") {
			action = "build"
		} else if strings.Contains(path, "/deploy") {
			action = "deploy"
		} else if strings.Contains(path, "/rollback") {
			action = "rollback"
		} else {
			action = "create"
		}
	case "PUT", "PATCH":
		action = "update"
	case "DELETE":
		action = "delete"
	default:
		action = strings.ToLower(method)
	}

	// Add resource context
	if strings.Contains(path, "/projects") {
		action = action + "_project"
	} else if strings.Contains(path, "/services") {
		action = action + "_service"
	} else if strings.Contains(path, "/deployments") {
		action = action + "_deployment"
	}

	return action
}

// determineOutcome determines if the request was successful
func determineOutcome(statusCode int) string {
	if statusCode >= 200 && statusCode < 300 {
		return "success"
	} else if statusCode >= 400 && statusCode < 500 {
		if statusCode == 401 || statusCode == 403 {
			return "denied"
		}
		return "failure"
	} else if statusCode >= 500 {
		return "failure"
	}
	return "unknown"
}

// sanitizeRequestBody removes sensitive fields from request body
func sanitizeRequestBody(body map[string]interface{}) map[string]interface{} {
	if body == nil {
		return nil
	}

	// Create a copy to avoid modifying original
	sanitized := make(map[string]interface{})
	for k, v := range body {
		// Redact sensitive fields
		if isSensitiveField(k) {
			sanitized[k] = "[REDACTED]"
		} else {
			sanitized[k] = v
		}
	}

	return sanitized
}

// isSensitiveField checks if a field contains sensitive data
func isSensitiveField(fieldName string) bool {
	sensitive := []string{
		"password",
		"secret",
		"token",
		"api_key",
		"apikey",
		"private_key",
		"privatekey",
		"credential",
		"auth",
	}

	lowerField := strings.ToLower(fieldName)
	for _, s := range sensitive {
		if strings.Contains(lowerField, s) {
			return true
		}
	}

	return false
}

// Close gracefully shuts down the async logger
func (m *Middleware) Close() error {
	return m.asyncLogger.Close()
}
