package api

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/logging"
)

// verifyRoundhouseCallbackAuth enforces shared-secret auth for internal worker callbacks.
// Production fails closed when RoundhouseAPIKey is unset; non-production allows an empty
// key only when bootstrap mode explicitly permits it.
func (h *Handler) verifyRoundhouseCallbackAuth(c *gin.Context) bool {
	key := h.config.RoundhouseAPIKey
	authHeader := c.GetHeader("Authorization")
	expected := "Bearer " + key

	if key != "" {
		if authHeader != expected {
			h.logger.Warn(c.Request.Context(), "Internal callback unauthorized",
				logging.String("remote_addr", c.ClientIP()))
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
			return false
		}
		return true
	}

	if h.config.IsProduction() {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error": "Roundhouse API key is not configured",
			"code":  "internal_auth_misconfigured",
		})
		return false
	}

	if !h.config.AllowsUnauthenticatedInternalCallbacks() {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return false
	}

	return true
}

// verifyRoundhouseInternalReadAuth protects internal read endpoints (e.g. git_repo lookup).
func (h *Handler) verifyRoundhouseInternalReadAuth(c *gin.Context) bool {
	return h.verifyRoundhouseCallbackAuth(c)
}
