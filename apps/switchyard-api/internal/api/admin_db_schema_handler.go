package api

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/db"
)

// GetAdminDBSchema reports golang-migrate version/dirty state and GA-critical
// column presence. Replaces break-glass psql checks for migration 030 verify.
//
// GET /v1/admin/db/schema
func (h *Handler) GetAdminDBSchema(c *gin.Context) {
	if h.repos == nil || h.repos.DB() == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "database unavailable"})
		return
	}
	report, err := db.BuildSchemaReport(c.Request.Context(), h.repos.DB())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, report)
}
