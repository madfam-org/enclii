package audit

import (
	"bytes"
	"encoding/json"
	"io"
	"net/url"
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

		// Capture request body for context — except on routes whose bodies
		// carry credentials, where the body is never read or stored at all.
		// Redaction is a second line of defence, not the first.
		omitBody := isCredentialRoute(c.FullPath(), c.Request.URL.Path)
		var requestBody map[string]interface{}
		if !omitBody && c.Request.Body != nil {
			bodyBytes, err := io.ReadAll(c.Request.Body)
			if err == nil {
				// Restore body for handler
				c.Request.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))

				// Parse JSON body (ignore errors for non-JSON)
				_ = json.Unmarshal(bodyBytes, &requestBody)
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
		auditContext := map[string]interface{}{
			"method":      c.Request.Method,
			"path":        c.Request.URL.Path,
			"query":       sanitizeQuery(c.Request.URL.RawQuery),
			"status_code": statusCode,
			"duration_ms": time.Since(startTime).Milliseconds(),
		}
		if omitBody {
			auditContext["request_body_omitted"] = true
		} else {
			auditContext["request_body"] = sanitizeRequestBody(requestBody)
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
			Context:      auditContext,
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

// sanitizeRequestBody returns a copy of body with sensitive values redacted
// at every depth, through nested objects and arrays.
func sanitizeRequestBody(body map[string]interface{}) map[string]interface{} {
	if body == nil {
		return nil
	}
	sanitized, _ := sanitizeValue(body, 0).(map[string]interface{})
	return sanitized
}

const (
	redactedValue = "[REDACTED]"
	// maxSanitizeDepth bounds recursion; anything deeper is dropped rather
	// than stored unexamined.
	maxSanitizeDepth = 32
)

func sanitizeValue(v interface{}, depth int) interface{} {
	if depth > maxSanitizeDepth {
		return "[TRUNCATED]"
	}
	switch t := v.(type) {
	case map[string]interface{}:
		out := make(map[string]interface{}, len(t))
		for k, child := range t {
			if isSensitiveField(k) {
				out[k] = redactedValue
				continue
			}
			out[k] = sanitizeValue(child, depth+1)
		}
		return out
	case []interface{}:
		out := make([]interface{}, len(t))
		for i, child := range t {
			out[i] = sanitizeValue(child, depth+1)
		}
		return out
	default:
		return v
	}
}

// sensitiveFieldFragments redact any key that CONTAINS them (after
// lower-casing and removing '_' and '-').
var sensitiveFieldFragments = []string{
	"password",
	"passwd",
	"passphrase",
	"secret",
	"token",
	"apikey",
	"privatekey",
	"credential",
	"auth",
	"plaintext",
	"ciphertext",
	"connectionstring",
	"dsn",
	"cookie",
	"signature",
	"certificate",
}

// sensitiveFieldNames redact keys that EQUAL them (same normalization). They
// are too generic to match as fragments ("env" would hit "environment") but
// are where key/value and env-map payloads carry their values. The NAME half
// of a key/value pair ("key", "name") is kept for the audit trail.
var sensitiveFieldNames = map[string]bool{
	"value":      true,
	"values":     true,
	"data":       true,
	"stringdata": true,
	"binarydata": true,
	"env":        true,
	"envs":       true,
	"envvars":    true,
	"vars":       true,
	"variables":  true,
	"pem":        true,
	"cert":       true,
}

func normalizeFieldName(fieldName string) string {
	lower := strings.ToLower(fieldName)
	return strings.NewReplacer("_", "", "-", "", " ", "").Replace(lower)
}

// isSensitiveField checks if a field contains sensitive data
func isSensitiveField(fieldName string) bool {
	name := normalizeFieldName(fieldName)
	if sensitiveFieldNames[name] {
		return true
	}
	for _, s := range sensitiveFieldFragments {
		if strings.Contains(name, s) {
			return true
		}
	}
	return false
}

// credentialRouteSegments mark routes whose request bodies carry credentials
// or secret material. A route matches when any literal path segment contains
// one of them; path parameters (":id") are ignored.
var credentialRouteSegments = []string{
	"secret",
	"env-var",
	"envvar",
	"intake",
	"token",
	"auth",
	"login",
	"register",
	"signup",
	"password",
	"credential",
	"webhook",
	"provision",
	"vault",
	"integration",
	"invitation",
	"key",
}

// isCredentialRoute reports whether the request body must not be stored. It
// checks the matched route template and, when no route matched, the raw path
// (failing toward omission).
func isCredentialRoute(routeTemplate, rawPath string) bool {
	path := routeTemplate
	if path == "" {
		path = rawPath
	}
	for _, segment := range strings.Split(strings.ToLower(path), "/") {
		if segment == "" || strings.HasPrefix(segment, ":") || strings.HasPrefix(segment, "*") {
			continue
		}
		for _, marker := range credentialRouteSegments {
			if strings.Contains(segment, marker) {
				return true
			}
		}
	}
	return false
}

// sanitizeQuery redacts the values of sensitive query parameters (for
// example a credential passed as ?token=). Unparseable queries are dropped.
func sanitizeQuery(rawQuery string) string {
	if rawQuery == "" {
		return ""
	}
	values, err := url.ParseQuery(rawQuery)
	if err != nil {
		return "[UNPARSEABLE]"
	}
	for k := range values {
		if isSensitiveField(k) {
			values[k] = []string{redactedValue}
		}
	}
	return values.Encode()
}

// Close gracefully shuts down the async logger
func (m *Middleware) Close() error {
	return m.asyncLogger.Close()
}
