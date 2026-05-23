package api

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/logging"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// FunctionBuildCallbackRequest represents the callback payload from Roundhouse after a function build completes
type FunctionBuildCallbackRequest struct {
	JobID          uuid.UUID `json:"job_id" binding:"required"`
	FunctionID     uuid.UUID `json:"function_id" binding:"required"`
	Success        bool      `json:"success"`
	ImageURI       string    `json:"image_uri"`
	ImageDigest    string    `json:"image_digest"`
	ImageSizeMB    float64   `json:"image_size_mb"`
	SBOM           string    `json:"sbom"`
	SBOMFormat     string    `json:"sbom_format"`
	ImageSignature string    `json:"image_signature"`
	DurationSecs   float64   `json:"duration_secs"`
	ErrorMessage   string    `json:"error_message"`
	LogsURL        string    `json:"logs_url"`
	Runtime        string    `json:"runtime"` // go, python, node, rust
}

// FunctionBuildCompleteCallback handles the callback from Roundhouse when a function build finishes
// POST /v1/callbacks/function-build-complete
func (h *Handler) FunctionBuildCompleteCallback(c *gin.Context) {
	ctx := c.Request.Context()

	if !h.verifyRoundhouseCallbackAuth(c) {
		return
	}

	var req FunctionBuildCallbackRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.logger.Error(ctx, "Invalid function build callback request",
			logging.Error("parse_error", err))
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	h.logger.Info(ctx, "Received function build callback from Roundhouse",
		logging.String("job_id", req.JobID.String()),
		logging.String("function_id", req.FunctionID.String()),
		logging.Bool("success", req.Success),
		logging.String("runtime", req.Runtime))

	// Process the callback
	if err := h.processFunctionBuildCallback(ctx, &req); err != nil {
		h.logger.Error(ctx, "Failed to process function build callback",
			logging.String("function_id", req.FunctionID.String()),
			logging.Error("process_error", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to process callback"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status":      "processed",
		"function_id": req.FunctionID,
	})
}

// processFunctionBuildCallback updates the function record after build completes
func (h *Handler) processFunctionBuildCallback(ctx context.Context, req *FunctionBuildCallbackRequest) error {
	// Get the function
	fn, err := h.repos.Functions.GetByID(ctx, req.FunctionID)
	if err != nil {
		h.logger.Error(ctx, "Failed to get function for callback",
			logging.String("function_id", req.FunctionID.String()),
			logging.Error("db_error", err))
		return err
	}

	if req.Success {
		// Update function with build results (ImageURI)
		if req.ImageURI != "" {
			if err := h.repos.Functions.UpdateImageURI(ctx, req.FunctionID, req.ImageURI); err != nil {
				h.logger.Error(ctx, "Failed to update function image URI",
					logging.String("function_id", req.FunctionID.String()),
					logging.Error("db_error", err))
				return err
			}
			h.logger.Info(ctx, "Function image URI updated",
				logging.String("function_id", req.FunctionID.String()),
				logging.String("image_uri", req.ImageURI))
		}

		h.logger.Info(ctx, "Function build completed successfully",
			logging.String("function_id", req.FunctionID.String()),
			logging.String("function_name", fn.Name),
			logging.String("job_id", req.JobID.String()),
			logging.Float64("duration_secs", req.DurationSecs),
			logging.String("image_uri", req.ImageURI),
			logging.String("runtime", req.Runtime))

		// The FunctionReconciler will pick up the change on next reconciliation
		// and transition from 'building' to 'deploying' state

	} else {
		// Build failed - update status to failed
		if err := h.repos.Functions.UpdateStatus(ctx, req.FunctionID, types.FunctionStatusFailed, req.ErrorMessage); err != nil {
			h.logger.Error(ctx, "Failed to update function status to failed",
				logging.String("function_id", req.FunctionID.String()),
				logging.Error("db_error", err))
			return err
		}

		h.logger.Error(ctx, "Function build failed",
			logging.String("function_id", req.FunctionID.String()),
			logging.String("function_name", fn.Name),
			logging.String("job_id", req.JobID.String()),
			logging.String("error", req.ErrorMessage),
			logging.String("logs_url", req.LogsURL))
	}

	return nil
}
