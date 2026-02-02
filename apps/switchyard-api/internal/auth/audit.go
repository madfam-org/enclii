package auth

import (
	"encoding/json"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
)

// AuthEvent represents an authentication-related audit event
type AuthEvent string

const (
	// Login events
	EventLoginSuccess AuthEvent = "auth.login.success"
	EventLoginFailure AuthEvent = "auth.login.failure"

	// Token events
	EventTokenIssued      AuthEvent = "auth.token.issued"         // #nosec G101 -- event name, not a credential
	EventTokenValidated   AuthEvent = "auth.token.validated"      // #nosec G101 -- event name, not a credential
	EventTokenRefreshed   AuthEvent = "auth.token.refreshed"      // #nosec G101 -- event name, not a credential
	EventTokenRefreshFail AuthEvent = "auth.token.refresh_failed" // #nosec G101 -- event name, not a credential

	// Session events
	EventLogout         AuthEvent = "auth.logout"
	EventSessionRevoked AuthEvent = "auth.session.revoked"

	// External auth events
	EventExternalTokenValidated AuthEvent = "auth.external.validated" // #nosec G101 -- event name, not a credential
	EventExternalUserCreated    AuthEvent = "auth.external.user_created"
	EventExternalUserLinked     AuthEvent = "auth.external.user_linked"

	// OIDC events
	EventOIDCLoginInitiated AuthEvent = "auth.oidc.login_initiated"
	EventOIDCCallbackStart  AuthEvent = "auth.oidc.callback_start"
	EventOIDCCallbackFail   AuthEvent = "auth.oidc.callback_failed"
)

// AuthAuditLog represents a structured authentication audit log entry
type AuthAuditLog struct {
	Timestamp   time.Time              `json:"timestamp"`
	Event       AuthEvent              `json:"event"`
	UserID      string                 `json:"user_id,omitempty"`
	Email       string                 `json:"email,omitempty"`
	Method      string                 `json:"method,omitempty"`       // "local", "external", "oidc"
	TokenSource string                 `json:"token_source,omitempty"` // "local", "external"
	TokenType   string                 `json:"token_type,omitempty"`   // "access", "refresh"
	SessionID   string                 `json:"session_id,omitempty"`
	IP          string                 `json:"ip,omitempty"`
	UserAgent   string                 `json:"user_agent,omitempty"`
	Issuer      string                 `json:"issuer,omitempty"`
	ExpiresAt   *time.Time             `json:"expires_at,omitempty"`
	Reason      string                 `json:"reason,omitempty"` // For failures
	Extra       map[string]interface{} `json:"extra,omitempty"`
}

// AuthAuditor handles authentication event logging
type AuthAuditor struct {
	logger *logrus.Logger
}

// NewAuthAuditor creates a new authentication auditor
func NewAuthAuditor() *AuthAuditor {
	logger := logrus.New()
	logger.SetFormatter(&logrus.JSONFormatter{
		TimestampFormat: time.RFC3339Nano,
	})
	return &AuthAuditor{logger: logger}
}

// defaultAuditor is the global auth auditor instance
var defaultAuditor = NewAuthAuditor()

// Log logs an authentication event
func (a *AuthAuditor) Log(log *AuthAuditLog) {
	if log.Timestamp.IsZero() {
		log.Timestamp = time.Now().UTC()
	}

	// Convert to logrus fields for structured logging
	fields := logrus.Fields{
		"event": log.Event,
	}

	if log.UserID != "" {
		fields["user_id"] = log.UserID
	}
	if log.Email != "" {
		fields["email"] = log.Email
	}
	if log.Method != "" {
		fields["method"] = log.Method
	}
	if log.TokenSource != "" {
		fields["token_source"] = log.TokenSource
	}
	if log.TokenType != "" {
		fields["token_type"] = log.TokenType
	}
	if log.SessionID != "" {
		fields["session_id"] = log.SessionID
	}
	if log.IP != "" {
		fields["ip"] = log.IP
	}
	if log.UserAgent != "" {
		fields["user_agent"] = log.UserAgent
	}
	if log.Issuer != "" {
		fields["issuer"] = log.Issuer
	}
	if log.ExpiresAt != nil {
		fields["expires_at"] = log.ExpiresAt.Format(time.RFC3339)
	}
	if log.Reason != "" {
		fields["reason"] = log.Reason
	}
	if log.Extra != nil {
		for k, v := range log.Extra {
			fields[k] = v
		}
	}

	// Log based on event type
	entry := a.logger.WithFields(fields)
	switch {
	case isFailureEvent(log.Event):
		entry.Warn("Authentication event")
	default:
		entry.Info("Authentication event")
	}
}

// LogJSON logs an auth event as a single JSON line to stdout
// This is useful for log aggregators that expect structured JSON lines
func (a *AuthAuditor) LogJSON(log *AuthAuditLog) {
	if log.Timestamp.IsZero() {
		log.Timestamp = time.Now().UTC()
	}
	data, err := json.Marshal(log)
	if err != nil {
		a.logger.WithError(err).Error("Failed to marshal auth audit log")
		return
	}
	// Write raw JSON to stdout for log aggregators
	logrus.StandardLogger().Out.Write(append(data, '\n'))
}

// isFailureEvent checks if the event represents a failure
func isFailureEvent(event AuthEvent) bool {
	switch event {
	case EventLoginFailure, EventTokenRefreshFail, EventOIDCCallbackFail:
		return true
	default:
		return false
	}
}

// =============================================================================
// Convenience functions for common auth events
// =============================================================================

// LogLoginSuccess logs a successful login event
func LogLoginSuccess(userID uuid.UUID, email, method, ip, userAgent string) {
	defaultAuditor.Log(&AuthAuditLog{
		Event:     EventLoginSuccess,
		UserID:    userID.String(),
		Email:     email,
		Method:    method,
		IP:        ip,
		UserAgent: userAgent,
	})
}

// LogLoginFailure logs a failed login attempt
func LogLoginFailure(email, reason, ip, userAgent string) {
	defaultAuditor.Log(&AuthAuditLog{
		Event:     EventLoginFailure,
		Email:     email,
		Reason:    reason,
		IP:        ip,
		UserAgent: userAgent,
	})
}

// LogTokenIssued logs when a new token is issued
func LogTokenIssued(userID uuid.UUID, tokenType string, expiresAt time.Time, sessionID string) {
	defaultAuditor.Log(&AuthAuditLog{
		Event:     EventTokenIssued,
		UserID:    userID.String(),
		TokenType: tokenType,
		ExpiresAt: &expiresAt,
		SessionID: sessionID,
	})
}

// LogTokenValidated logs when a token is successfully validated
func LogTokenValidated(userID uuid.UUID, email, tokenSource string) {
	defaultAuditor.Log(&AuthAuditLog{
		Event:       EventTokenValidated,
		UserID:      userID.String(),
		Email:       email,
		TokenSource: tokenSource,
	})
}

// LogTokenRefreshed logs when a token is refreshed
func LogTokenRefreshed(userID uuid.UUID, sessionID string) {
	defaultAuditor.Log(&AuthAuditLog{
		Event:     EventTokenRefreshed,
		UserID:    userID.String(),
		SessionID: sessionID,
	})
}

// LogTokenRefreshFailed logs when token refresh fails
func LogTokenRefreshFailed(reason, ip string) {
	defaultAuditor.Log(&AuthAuditLog{
		Event:  EventTokenRefreshFail,
		Reason: reason,
		IP:     ip,
	})
}

// LogLogout logs a logout event
func LogLogout(userID uuid.UUID, sessionID, ip string) {
	defaultAuditor.Log(&AuthAuditLog{
		Event:     EventLogout,
		UserID:    userID.String(),
		SessionID: sessionID,
		IP:        ip,
	})
}

// LogSessionRevoked logs when a session is revoked
func LogSessionRevoked(userID uuid.UUID, sessionID, reason string) {
	defaultAuditor.Log(&AuthAuditLog{
		Event:     EventSessionRevoked,
		UserID:    userID.String(),
		SessionID: sessionID,
		Reason:    reason,
	})
}

// LogExternalTokenValidated logs when an external token (e.g., Janua) is validated
func LogExternalTokenValidated(userID uuid.UUID, email, issuer string) {
	defaultAuditor.Log(&AuthAuditLog{
		Event:       EventExternalTokenValidated,
		UserID:      userID.String(),
		Email:       email,
		TokenSource: "external",
		Issuer:      issuer,
	})
}

// LogExternalUserCreated logs when a user is created from external token
func LogExternalUserCreated(userID uuid.UUID, email, issuer string) {
	defaultAuditor.Log(&AuthAuditLog{
		Event:  EventExternalUserCreated,
		UserID: userID.String(),
		Email:  email,
		Issuer: issuer,
	})
}

// LogExternalUserLinked logs when an existing user is linked to external identity
func LogExternalUserLinked(userID uuid.UUID, email, issuer string) {
	defaultAuditor.Log(&AuthAuditLog{
		Event:  EventExternalUserLinked,
		UserID: userID.String(),
		Email:  email,
		Issuer: issuer,
	})
}

// LogOIDCLoginInitiated logs when OIDC login flow starts
func LogOIDCLoginInitiated(ip, userAgent string) {
	defaultAuditor.Log(&AuthAuditLog{
		Event:     EventOIDCLoginInitiated,
		Method:    "oidc",
		IP:        ip,
		UserAgent: userAgent,
	})
}

// LogOIDCCallbackStart logs when OIDC callback is received
func LogOIDCCallbackStart(ip string) {
	defaultAuditor.Log(&AuthAuditLog{
		Event:  EventOIDCCallbackStart,
		Method: "oidc",
		IP:     ip,
	})
}

// LogOIDCCallbackFailed logs when OIDC callback fails
func LogOIDCCallbackFailed(reason, ip string) {
	defaultAuditor.Log(&AuthAuditLog{
		Event:  EventOIDCCallbackFail,
		Method: "oidc",
		Reason: reason,
		IP:     ip,
	})
}

// =============================================================================
// Gin context helpers
// =============================================================================

// LogFromContext logs an auth event with IP and UserAgent from Gin context
func LogFromContext(c *gin.Context, event AuthEvent, userID uuid.UUID, email string, extra map[string]interface{}) {
	defaultAuditor.Log(&AuthAuditLog{
		Event:     event,
		UserID:    userID.String(),
		Email:     email,
		IP:        c.ClientIP(),
		UserAgent: c.Request.UserAgent(),
		Extra:     extra,
	})
}

// LogFailureFromContext logs a failure event with context information
func LogFailureFromContext(c *gin.Context, event AuthEvent, reason string, extra map[string]interface{}) {
	defaultAuditor.Log(&AuthAuditLog{
		Event:     event,
		Reason:    reason,
		IP:        c.ClientIP(),
		UserAgent: c.Request.UserAgent(),
		Extra:     extra,
	})
}
