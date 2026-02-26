// Package middleware provides HTTP middleware for the Switchyard API.
package middleware

import (
	"database/sql"
	stderrors "errors"
	"fmt"
	"net/http"
	"runtime/debug"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/sirupsen/logrus"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/errors"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/logging"
)

// ErrorHandlerMiddleware creates middleware that handles errors set via c.Error()
// and converts them to consistent JSON responses using the errors package.
//
// This middleware should be registered early in the chain to catch all errors.
//
// Usage in handlers:
//
//	if err != nil {
//	    c.Error(errors.ErrNotFound.WithError(err))
//	    c.Abort()
//	    return
//	}
//
// Or use the helper functions:
//
//	if err := AbortWithAppError(c, errors.ErrNotFound.WithError(err)); err != nil {
//	    return
//	}
func ErrorHandlerMiddleware(logger logging.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Next()

		// Check if any errors were added to the context
		if len(c.Errors) > 0 {
			// Get the last error (most specific)
			err := c.Errors.Last().Err

			// Check if response was already written
			if c.Writer.Written() {
				return
			}

			// Log the error
			if logger != nil {
				ctx := c.Request.Context()
				logger.Error(ctx, "Request error",
					logging.String("method", c.Request.Method),
					logging.String("path", c.Request.URL.Path),
					logging.Error("error", err))
			}

			// Get HTTP status and response from error
			status := errors.GetHTTPStatus(err)
			response := errors.GetErrorResponse(err)

			c.JSON(status, response)
		}
	}
}

// AbortWithAppError sets an application error on the context and aborts.
// This is the preferred way to return errors from handlers.
//
// Example:
//
//	if err := h.repos.Services.GetByID(id); err != nil {
//	    AbortWithAppError(c, errors.ErrServiceNotFound.WithError(err))
//	    return
//	}
func AbortWithAppError(c *gin.Context, appErr *errors.AppError) {
	_ = c.Error(appErr)
	c.Abort()
}

// AbortWithError wraps a standard error with an AppError and aborts.
// Use when you have a standard error that should be wrapped with app context.
//
// Example:
//
//	if err := db.Query(...); err != nil {
//	    AbortWithError(c, err, errors.ErrDatabaseError)
//	    return
//	}
func AbortWithError(c *gin.Context, err error, appErr *errors.AppError) {
	if err == nil {
		return
	}
	AbortWithAppError(c, appErr.WithError(err))
}

// AbortNotFound is a convenience function for 404 errors.
func AbortNotFound(c *gin.Context, resourceType string) {
	var appErr *errors.AppError
	switch resourceType {
	case "project":
		appErr = errors.ErrProjectNotFound
	case "service":
		appErr = errors.ErrServiceNotFound
	case "release":
		appErr = errors.ErrReleaseNotFound
	case "deployment":
		appErr = errors.ErrDeploymentNotFound
	default:
		appErr = errors.ErrNotFound.WithDetails(map[string]string{"resource": resourceType})
	}
	AbortWithAppError(c, appErr)
}

// AbortBadRequest is a convenience function for 400 errors with a message.
func AbortBadRequest(c *gin.Context, message string) {
	AbortWithAppError(c, errors.ErrInvalidInput.WithDetails(map[string]string{"message": message}))
}

// AbortValidation is a convenience function for validation errors.
func AbortValidation(c *gin.Context, details any) {
	AbortWithAppError(c, errors.ErrValidation.WithDetails(details))
}

// AbortInternal is a convenience function for 500 errors.
// The underlying error is logged but not exposed to the client.
func AbortInternal(c *gin.Context, err error) {
	AbortWithAppError(c, errors.ErrInternal.WithError(err))
}

// HandleDBError converts common database errors to appropriate HTTP responses.
// Returns true if an error was handled (and response sent), false otherwise.
//
// Example:
//
//	service, err := h.repos.Services.GetByID(id)
//	if HandleDBError(c, err, "service") {
//	    return
//	}
func HandleDBError(c *gin.Context, err error, resourceType string) bool {
	if err == nil {
		return false
	}

	if stderrors.Is(err, sql.ErrNoRows) {
		AbortNotFound(c, resourceType)
		return true
	}

	// For other database errors, return internal error
	AbortWithAppError(c, errors.ErrDatabaseError.WithError(err))
	return true
}

// ParseUUID parses a UUID from string and handles errors.
// Returns the parsed UUID and true if successful, or zero UUID and false if failed.
//
// Example:
//
//	serviceID, ok := ParseUUID(c, c.Param("id"), "service_id")
//	if !ok {
//	    return // error response already sent
//	}
func ParseUUID(c *gin.Context, s string, paramName string) (uuid.UUID, bool) {
	if s == "" {
		AbortBadRequest(c, paramName+" is required")
		return uuid.Nil, false
	}

	id, err := uuid.Parse(s)
	if err != nil {
		AbortWithAppError(c, errors.ErrInvalidUUID.WithDetails(map[string]string{
			"parameter": paramName,
			"value":     s,
		}))
		return uuid.Nil, false
	}

	return id, true
}

// BindJSON binds request body to a struct and handles errors.
// Returns true if binding was successful, false otherwise (error response sent).
//
// Example:
//
//	var req CreateServiceRequest
//	if !BindJSON(c, &req) {
//	    return // error response already sent
//	}
func BindJSON(c *gin.Context, obj any) bool {
	if err := c.ShouldBindJSON(obj); err != nil {
		AbortValidation(c, map[string]string{"message": err.Error()})
		return false
	}
	return true
}

// BindQuery binds query parameters to a struct and handles errors.
// Returns true if binding was successful, false otherwise.
func BindQuery(c *gin.Context, obj any) bool {
	if err := c.ShouldBindQuery(obj); err != nil {
		AbortValidation(c, map[string]string{"message": err.Error()})
		return false
	}
	return true
}

// RecoveryMiddleware creates a custom panic recovery middleware that:
// 1. Catches panics and prevents server crashes
// 2. Logs the full panic details and stack trace
// 3. Returns a proper JSON error response instead of empty body
//
// This replaces gin.Recovery() to provide better error visibility in production.
func RecoveryMiddleware(logger logging.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if err := recover(); err != nil {
				// Get stack trace
				stack := debug.Stack()

				// Log the panic with full details
				if logger != nil {
					ctx := c.Request.Context()
					logger.Error(ctx, "Panic recovered",
						logging.String("method", c.Request.Method),
						logging.String("path", c.Request.URL.Path),
						logging.String("panic", fmt.Sprintf("%v", err)),
						logging.String("stack", string(stack)))
				} else {
					// Fallback to structured logging via logrus if no context logger available
					logrus.WithFields(logrus.Fields{
						"method": c.Request.Method,
						"path":   c.Request.URL.Path,
						"panic":  fmt.Sprintf("%v", err),
						"stack":  string(stack),
					}).Error("Panic recovered in request handler")
				}

				// Return a proper JSON error response (panic details logged above, not exposed to client)
				c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{
					"error":   "internal_server_error",
					"message": "An unexpected error occurred",
				})
			}
		}()
		c.Next()
	}
}
