package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// Timetable handlers — NOT IMPLEMENTED.
// These stubs return 501 with an ETA and tracking link.
// Feature: Cron jobs and one-off scheduled tasks for user services.
// ETA: Q2 2026
// Tracking: https://github.com/madfam-org/enclii/issues

func notImplementedResponse(c *gin.Context, feature, eta string) {
	c.JSON(http.StatusNotImplemented, gin.H{
		"error":    "not_implemented",
		"message":  feature + " is not yet available",
		"eta":      eta,
		"tracking": "https://github.com/madfam-org/enclii/issues",
	})
}

// CreateCronJob returns 501 Not Implemented.
func (h *Handler) CreateCronJob(c *gin.Context) {
	notImplementedResponse(c, "Timetable (Cron Jobs)", "Q2 2026")
}

// ListCronJobs returns 501 Not Implemented.
func (h *Handler) ListCronJobs(c *gin.Context) {
	notImplementedResponse(c, "Timetable (Cron Jobs)", "Q2 2026")
}

// GetCronJob returns 501 Not Implemented.
func (h *Handler) GetCronJob(c *gin.Context) {
	notImplementedResponse(c, "Timetable (Cron Jobs)", "Q2 2026")
}

// UpdateCronJob returns 501 Not Implemented.
func (h *Handler) UpdateCronJob(c *gin.Context) {
	notImplementedResponse(c, "Timetable (Cron Jobs)", "Q2 2026")
}

// DeleteCronJob returns 501 Not Implemented.
func (h *Handler) DeleteCronJob(c *gin.Context) {
	notImplementedResponse(c, "Timetable (Cron Jobs)", "Q2 2026")
}

// CreateOneOffJob returns 501 Not Implemented.
func (h *Handler) CreateOneOffJob(c *gin.Context) {
	notImplementedResponse(c, "Timetable (One-Off Jobs)", "Q2 2026")
}

// ListCronJobRuns returns 501 Not Implemented.
func (h *Handler) ListCronJobRuns(c *gin.Context) {
	notImplementedResponse(c, "Timetable (Cron Job Runs)", "Q2 2026")
}
