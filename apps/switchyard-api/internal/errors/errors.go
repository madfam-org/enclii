package errors

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/lib/pq"
)

// AppError represents a structured application error
type AppError struct {
	Code       string `json:"code"`
	Message    string `json:"message"`
	HTTPStatus int    `json:"-"`
	Details    any    `json:"details,omitempty"`
	Err        error  `json:"-"`
}

// Error implements the error interface
func (e *AppError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("%s: %v", e.Message, e.Err)
	}
	return e.Message
}

// Unwrap implements error unwrapping
func (e *AppError) Unwrap() error {
	return e.Err
}

// WithDetails adds details to the error
func (e *AppError) WithDetails(details any) *AppError {
	return &AppError{
		Code:       e.Code,
		Message:    e.Message,
		HTTPStatus: e.HTTPStatus,
		Details:    details,
		Err:        e.Err,
	}
}

// WithError wraps an underlying error
func (e *AppError) WithError(err error) *AppError {
	return &AppError{
		Code:       e.Code,
		Message:    e.Message,
		HTTPStatus: e.HTTPStatus,
		Details:    e.Details,
		Err:        err,
	}
}

// Common error definitions
var (
	// Resource errors (404)
	ErrNotFound = &AppError{
		Code:       "NOT_FOUND",
		Message:    "Resource not found",
		HTTPStatus: http.StatusNotFound,
	}
	ErrProjectNotFound = &AppError{
		Code:       "PROJECT_NOT_FOUND",
		Message:    "Project not found",
		HTTPStatus: http.StatusNotFound,
	}
	ErrServiceNotFound = &AppError{
		Code:       "SERVICE_NOT_FOUND",
		Message:    "Service not found",
		HTTPStatus: http.StatusNotFound,
	}
	ErrReleaseNotFound = &AppError{
		Code:       "RELEASE_NOT_FOUND",
		Message:    "Release not found",
		HTTPStatus: http.StatusNotFound,
	}
	ErrReleaseNotReady = &AppError{
		Code:       "RELEASE_NOT_READY",
		Message:    "Release is not ready for deployment",
		HTTPStatus: http.StatusBadRequest,
	}
	ErrDeploymentNotFound = &AppError{
		Code:       "DEPLOYMENT_NOT_FOUND",
		Message:    "Deployment not found",
		HTTPStatus: http.StatusNotFound,
	}

	// Authentication errors (401)
	ErrUnauthorized = &AppError{
		Code:       "UNAUTHORIZED",
		Message:    "Authentication required",
		HTTPStatus: http.StatusUnauthorized,
	}
	ErrInvalidCredentials = &AppError{
		Code:       "INVALID_CREDENTIALS",
		Message:    "Invalid email or password",
		HTTPStatus: http.StatusUnauthorized,
	}
	ErrTokenExpired = &AppError{
		Code:       "TOKEN_EXPIRED",
		Message:    "Authentication token has expired",
		HTTPStatus: http.StatusUnauthorized,
	}
	ErrTokenInvalid = &AppError{
		Code:       "TOKEN_INVALID",
		Message:    "Invalid authentication token",
		HTTPStatus: http.StatusUnauthorized,
	}
	ErrSessionRevoked = &AppError{
		Code:       "SESSION_REVOKED",
		Message:    "Session has been revoked",
		HTTPStatus: http.StatusUnauthorized,
	}

	// Authorization errors (403)
	ErrForbidden = &AppError{
		Code:       "FORBIDDEN",
		Message:    "Access denied",
		HTTPStatus: http.StatusForbidden,
	}
	ErrInsufficientPermissions = &AppError{
		Code:       "INSUFFICIENT_PERMISSIONS",
		Message:    "Insufficient permissions for this operation",
		HTTPStatus: http.StatusForbidden,
	}
	ErrBudgetThrottled = &AppError{
		Code:       "BUDGET_THROTTLED",
		Message:    "Deploy blocked: project budget exceeded for this environment scope",
		HTTPStatus: http.StatusForbidden,
	}

	// Validation errors (400)
	ErrValidation = &AppError{
		Code:       "VALIDATION_ERROR",
		Message:    "Validation failed",
		HTTPStatus: http.StatusBadRequest,
	}
	ErrInvalidInput = &AppError{
		Code:       "INVALID_INPUT",
		Message:    "Invalid input data",
		HTTPStatus: http.StatusBadRequest,
	}
	ErrInvalidUUID = &AppError{
		Code:       "INVALID_UUID",
		Message:    "Invalid UUID format",
		HTTPStatus: http.StatusBadRequest,
	}
	ErrMissingParameter = &AppError{
		Code:       "MISSING_PARAMETER",
		Message:    "Required parameter is missing",
		HTTPStatus: http.StatusBadRequest,
	}

	// Conflict errors (409)
	ErrConflict = &AppError{
		Code:       "CONFLICT",
		Message:    "Resource conflict",
		HTTPStatus: http.StatusConflict,
	}
	ErrAlreadyExists = &AppError{
		Code:       "ALREADY_EXISTS",
		Message:    "Resource already exists",
		HTTPStatus: http.StatusConflict,
	}
	ErrEmailAlreadyExists = &AppError{
		Code:       "EMAIL_ALREADY_EXISTS",
		Message:    "Email address already registered",
		HTTPStatus: http.StatusConflict,
	}
	ErrSlugAlreadyExists = &AppError{
		Code:       "SLUG_ALREADY_EXISTS",
		Message:    "Slug already in use",
		HTTPStatus: http.StatusConflict,
	}

	// Build errors (422)
	ErrBuildFailed = &AppError{
		Code:       "BUILD_FAILED",
		Message:    "Build process failed",
		HTTPStatus: http.StatusUnprocessableEntity,
	}
	ErrBuildTimeout = &AppError{
		Code:       "BUILD_TIMEOUT",
		Message:    "Build process timed out",
		HTTPStatus: http.StatusUnprocessableEntity,
	}
	ErrInvalidBuildConfig = &AppError{
		Code:       "INVALID_BUILD_CONFIG",
		Message:    "Invalid build configuration",
		HTTPStatus: http.StatusUnprocessableEntity,
	}

	// Deployment errors (422)
	ErrDeploymentFailed = &AppError{
		Code:       "DEPLOYMENT_FAILED",
		Message:    "Deployment failed",
		HTTPStatus: http.StatusUnprocessableEntity,
	}
	ErrDeploymentTimeout = &AppError{
		Code:       "DEPLOYMENT_TIMEOUT",
		Message:    "Deployment timed out",
		HTTPStatus: http.StatusUnprocessableEntity,
	}
	ErrRollbackFailed = &AppError{
		Code:       "ROLLBACK_FAILED",
		Message:    "Rollback operation failed",
		HTTPStatus: http.StatusUnprocessableEntity,
	}

	// Infrastructure errors (503)
	ErrServiceUnavailable = &AppError{
		Code:       "SERVICE_UNAVAILABLE",
		Message:    "Service temporarily unavailable",
		HTTPStatus: http.StatusServiceUnavailable,
	}
	ErrDatabaseUnavailable = &AppError{
		Code:       "DATABASE_UNAVAILABLE",
		Message:    "Database connection unavailable",
		HTTPStatus: http.StatusServiceUnavailable,
	}
	ErrKubernetesUnavailable = &AppError{
		Code:       "KUBERNETES_UNAVAILABLE",
		Message:    "Kubernetes cluster unavailable",
		HTTPStatus: http.StatusServiceUnavailable,
	}

	// Internal errors (500)
	ErrInternal = &AppError{
		Code:       "INTERNAL_ERROR",
		Message:    "Internal server error",
		HTTPStatus: http.StatusInternalServerError,
	}
	ErrDatabaseError = &AppError{
		Code:       "DATABASE_ERROR",
		Message:    "Database operation failed",
		HTTPStatus: http.StatusInternalServerError,
	}
	ErrDatabaseTimeout = &AppError{
		Code:       "DATABASE_TIMEOUT",
		Message:    "Database operation timed out",
		HTTPStatus: http.StatusGatewayTimeout,
	}
	ErrDatabaseConnectionFailed = &AppError{
		Code:       "DATABASE_CONNECTION_FAILED",
		Message:    "Failed to connect to database",
		HTTPStatus: http.StatusServiceUnavailable,
	}
	ErrUnexpected = &AppError{
		Code:       "UNEXPECTED_ERROR",
		Message:    "An unexpected error occurred",
		HTTPStatus: http.StatusInternalServerError,
	}

	// Team/Organization errors
	ErrTeamNotFound = &AppError{
		Code:       "TEAM_NOT_FOUND",
		Message:    "Team not found",
		HTTPStatus: http.StatusNotFound,
	}
	ErrTeamMemberNotFound = &AppError{
		Code:       "TEAM_MEMBER_NOT_FOUND",
		Message:    "Team member not found",
		HTTPStatus: http.StatusNotFound,
	}
	ErrEnvironmentNotFound = &AppError{
		Code:       "ENVIRONMENT_NOT_FOUND",
		Message:    "Environment not found",
		HTTPStatus: http.StatusNotFound,
	}
	ErrPreviewNotFound = &AppError{
		Code:       "PREVIEW_NOT_FOUND",
		Message:    "Preview environment not found",
		HTTPStatus: http.StatusNotFound,
	}
	ErrTemplateNotFound = &AppError{
		Code:       "TEMPLATE_NOT_FOUND",
		Message:    "Template not found",
		HTTPStatus: http.StatusNotFound,
	}
	ErrDomainNotFound = &AppError{
		Code:       "DOMAIN_NOT_FOUND",
		Message:    "Domain not found",
		HTTPStatus: http.StatusNotFound,
	}
)

// New creates a new AppError
func New(code, message string, httpStatus int) *AppError {
	return &AppError{
		Code:       code,
		Message:    message,
		HTTPStatus: httpStatus,
	}
}

// Wrap wraps an error with application error information
func Wrap(err error, appErr *AppError) *AppError {
	if err == nil {
		return appErr
	}
	return appErr.WithError(err)
}

// Is checks if an error is a specific AppError
func Is(err error, target *AppError) bool {
	var appErr *AppError
	if errors.As(err, &appErr) {
		return appErr.Code == target.Code
	}
	return false
}

// GetHTTPStatus extracts HTTP status from error
func GetHTTPStatus(err error) int {
	var appErr *AppError
	if errors.As(err, &appErr) {
		return appErr.HTTPStatus
	}
	return http.StatusInternalServerError
}

// GetErrorResponse converts error to API response
func GetErrorResponse(err error) map[string]any {
	var appErr *AppError
	if errors.As(err, &appErr) {
		response := map[string]any{
			"error": map[string]any{
				"code":    appErr.Code,
				"message": appErr.Message,
			},
		}
		if appErr.Details != nil {
			response["error"].(map[string]any)["details"] = appErr.Details
		}
		return response
	}

	// Generic error response
	return map[string]any{
		"error": map[string]any{
			"code":    "INTERNAL_ERROR",
			"message": "An unexpected error occurred",
		},
	}
}

// =============================================================================
// DATABASE ERROR HELPERS
// =============================================================================

// WrapDBError wraps a database error with appropriate semantic error type
func WrapDBError(err error, notFoundErr *AppError) *AppError {
	if err == nil {
		return nil
	}

	// Check for sql.ErrNoRows
	if errors.Is(err, sql.ErrNoRows) {
		if notFoundErr != nil {
			return notFoundErr.WithError(err)
		}
		return ErrNotFound.WithError(err)
	}

	// Check for context deadline exceeded
	if errors.Is(err, context.DeadlineExceeded) {
		return ErrDatabaseTimeout.WithError(err)
	}

	// Check for context cancelled
	if errors.Is(err, context.Canceled) {
		return ErrDatabaseError.WithError(err)
	}

	// Check for PostgreSQL-specific errors
	var pqErr *pq.Error
	if errors.As(err, &pqErr) {
		return wrapPQError(pqErr)
	}

	// Default to generic database error
	return ErrDatabaseError.WithError(err)
}

// wrapPQError converts PostgreSQL errors to AppErrors
func wrapPQError(pqErr *pq.Error) *AppError {
	switch pqErr.Code {
	// Unique constraint violation
	case "23505":
		// Extract constraint name to provide more context
		if strings.Contains(pqErr.Constraint, "email") {
			return ErrEmailAlreadyExists.WithError(pqErr)
		}
		if strings.Contains(pqErr.Constraint, "slug") {
			return ErrSlugAlreadyExists.WithError(pqErr)
		}
		return ErrAlreadyExists.WithError(pqErr)

	// Foreign key violation
	case "23503":
		return ErrConflict.WithError(pqErr).WithDetails(map[string]string{
			"constraint": pqErr.Constraint,
		})

	// Not null violation
	case "23502":
		return ErrValidation.WithError(pqErr).WithDetails(map[string]string{
			"column": pqErr.Column,
		})

	// Connection errors
	case "08000", "08003", "08006", "08001", "08004":
		return ErrDatabaseConnectionFailed.WithError(pqErr)

	// Deadlock
	case "40P01":
		return ErrDatabaseError.WithError(pqErr).WithDetails(map[string]string{
			"reason": "deadlock_detected",
		})

	default:
		return ErrDatabaseError.WithError(pqErr)
	}
}

// IsNotFound checks if an error represents a "not found" condition
func IsNotFound(err error) bool {
	if errors.Is(err, sql.ErrNoRows) {
		return true
	}
	var appErr *AppError
	if errors.As(err, &appErr) {
		return appErr.HTTPStatus == http.StatusNotFound
	}
	return false
}

// IsUniqueViolation checks if an error is a unique constraint violation
func IsUniqueViolation(err error) bool {
	var pqErr *pq.Error
	if errors.As(err, &pqErr) {
		return pqErr.Code == "23505"
	}
	return false
}
