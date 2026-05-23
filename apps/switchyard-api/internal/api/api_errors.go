package api

import (
	"github.com/gin-gonic/gin"

	apperrors "github.com/madfam-org/enclii/apps/switchyard-api/internal/errors"
)

// respondAppError writes a structured JSON error body consistent with
// middleware.ErrorHandlerMiddleware and OpenAPI error shapes.
func respondAppError(c *gin.Context, appErr *apperrors.AppError) {
	c.JSON(appErr.HTTPStatus, apperrors.GetErrorResponse(appErr))
}
