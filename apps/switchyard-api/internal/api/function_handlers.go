package api

import (
	"database/sql"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/logging"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// CreateFunctionRequest defines the request body for creating a function
type CreateFunctionRequest struct {
	Name   string               `json:"name" binding:"required"`
	Config types.FunctionConfig `json:"config" binding:"required"`
}

// CreateFunctionResponse defines the response for function creation
type CreateFunctionResponse struct {
	Function *types.Function `json:"function"`
	Message  string          `json:"message"`
}

// ListFunctions lists all serverless functions for a project
// GET /v1/projects/:slug/functions
func (h *Handler) ListFunctions(c *gin.Context) {
	ctx := c.Request.Context()
	slug := c.Param("slug")

	// Get project by slug
	project, err := h.repos.Projects.GetBySlug(slug)
	if err != nil {
		if err == sql.ErrNoRows {
			c.JSON(http.StatusNotFound, gin.H{"error": "project not found"})
			return
		}
		h.logger.Error(ctx, "Failed to get project", logging.Error("error", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to get project"})
		return
	}

	// Get functions for project
	functions, err := h.repos.Functions.ListByProject(ctx, project.ID)
	if err != nil {
		h.logger.Error(ctx, "Failed to list functions", logging.Error("error", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list functions"})
		return
	}

	c.JSON(http.StatusOK, types.FunctionListResponse{
		Functions: convertFunctionPointers(functions),
		Total:     len(functions),
	})
}

// ListAllFunctions lists all serverless functions the user has access to
// GET /v1/functions
func (h *Handler) ListAllFunctions(c *gin.Context) {
	ctx := c.Request.Context()

	// Get user ID from context — middleware uses snake_case "user_id"
	userIDStr, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "user not authenticated"})
		return
	}

	userID, err := uuid.Parse(userIDStr.(string))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid user ID"})
		return
	}

	// Get projects the user has access to
	projectAccess, err := h.repos.ProjectAccess.ListByUser(ctx, userID)
	if err != nil {
		h.logger.Error(ctx, "Failed to list user projects", logging.Error("error", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list projects"})
		return
	}

	if len(projectAccess) == 0 {
		c.JSON(http.StatusOK, types.FunctionListResponse{
			Functions: []types.Function{},
			Total:     0,
		})
		return
	}

	// Extract project IDs
	projectIDs := make([]uuid.UUID, len(projectAccess))
	for i, pa := range projectAccess {
		projectIDs[i] = pa.ProjectID
	}

	// Get functions for all accessible projects
	functions, err := h.repos.Functions.ListByProjects(ctx, projectIDs)
	if err != nil {
		h.logger.Error(ctx, "Failed to list functions", logging.Error("error", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list functions"})
		return
	}

	c.JSON(http.StatusOK, types.FunctionListResponse{
		Functions: convertFunctionPointers(functions),
		Total:     len(functions),
	})
}

// CreateFunction creates a new serverless function
// POST /v1/projects/:slug/functions
func (h *Handler) CreateFunction(c *gin.Context) {
	ctx := c.Request.Context()
	slug := c.Param("slug")

	// Get project by slug
	project, err := h.repos.Projects.GetBySlug(slug)
	if err != nil {
		if err == sql.ErrNoRows {
			c.JSON(http.StatusNotFound, gin.H{"error": "project not found"})
			return
		}
		h.logger.Error(ctx, "Failed to get project", logging.Error("error", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to get project"})
		return
	}

	// Parse request body
	var req CreateFunctionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Validate runtime
	switch req.Config.Runtime {
	case types.FunctionRuntimeGo, types.FunctionRuntimePython, types.FunctionRuntimeNode, types.FunctionRuntimeRust:
		// Valid runtimes
	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid runtime, must be one of: go, python, node, rust"})
		return
	}

	// Set default handler if not provided
	if req.Config.Handler == "" {
		req.Config.Handler = types.FunctionRuntimeDefaults[req.Config.Runtime]
	}

	// Get user info from context (middleware uses snake_case keys)
	userID, _ := c.Get("user_id")
	userEmail, _ := c.Get("user_email")

	var userUUID *uuid.UUID
	if uid, ok := userID.(string); ok && uid != "" {
		if parsed, err := uuid.Parse(uid); err == nil {
			userUUID = &parsed
		}
	}

	var emailStr string
	if email, ok := userEmail.(string); ok {
		emailStr = email
	}

	// Create the function
	fn := &types.Function{
		ProjectID:      project.ID,
		Name:           req.Name,
		Config:         req.Config,
		CreatedBy:      userUUID,
		CreatedByEmail: emailStr,
	}

	if err := h.repos.Functions.Create(ctx, fn); err != nil {
		h.logger.Error(ctx, "Failed to create function",
			logging.String("project_slug", slug),
			logging.String("function_name", req.Name),
			logging.Error("error", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create function"})
		return
	}

	h.logger.Info(ctx, "Function created",
		logging.String("function_id", fn.ID.String()),
		logging.String("project_slug", slug),
		logging.String("runtime", string(fn.Config.Runtime)))

	c.JSON(http.StatusCreated, CreateFunctionResponse{
		Function: fn,
		Message:  "Function creation initiated. Build will start shortly.",
	})
}

// GetFunction retrieves a specific function
// GET /v1/functions/:id
func (h *Handler) GetFunction(c *gin.Context) {
	ctx := c.Request.Context()
	fnID := c.Param("id")

	// Parse function ID
	fnUUID, err := uuid.Parse(fnID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid function_id format"})
		return
	}

	fn, err := h.repos.Functions.GetByID(ctx, fnUUID)
	if err != nil {
		if err == sql.ErrNoRows {
			c.JSON(http.StatusNotFound, gin.H{"error": "function not found"})
			return
		}
		h.logger.Error(ctx, "Failed to get function", logging.Error("error", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to get function"})
		return
	}

	c.JSON(http.StatusOK, fn)
}

// UpdateFunctionRequest defines the request body for updating a function
type UpdateFunctionRequest struct {
	Config *types.FunctionConfig `json:"config,omitempty"`
}

// UpdateFunction updates a function configuration
// PATCH /v1/functions/:id
func (h *Handler) UpdateFunction(c *gin.Context) {
	ctx := c.Request.Context()
	fnID := c.Param("id")

	// Parse function ID
	fnUUID, err := uuid.Parse(fnID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid function_id format"})
		return
	}

	// Parse request body
	var req UpdateFunctionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Get existing function
	fn, err := h.repos.Functions.GetByID(ctx, fnUUID)
	if err != nil {
		if err == sql.ErrNoRows {
			c.JSON(http.StatusNotFound, gin.H{"error": "function not found"})
			return
		}
		h.logger.Error(ctx, "Failed to get function", logging.Error("error", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to get function"})
		return
	}

	// Update config if provided
	if req.Config != nil {
		// Validate runtime if changed
		if req.Config.Runtime != "" && req.Config.Runtime != fn.Config.Runtime {
			switch req.Config.Runtime {
			case types.FunctionRuntimeGo, types.FunctionRuntimePython, types.FunctionRuntimeNode, types.FunctionRuntimeRust:
				fn.Config.Runtime = req.Config.Runtime
			default:
				c.JSON(http.StatusBadRequest, gin.H{"error": "invalid runtime"})
				return
			}
		}

		// Update other config fields
		if req.Config.Handler != "" {
			fn.Config.Handler = req.Config.Handler
		}
		if req.Config.Memory != "" {
			fn.Config.Memory = req.Config.Memory
		}
		if req.Config.CPU != "" {
			fn.Config.CPU = req.Config.CPU
		}
		if req.Config.Timeout > 0 {
			fn.Config.Timeout = req.Config.Timeout
		}
		if req.Config.MinReplicas >= 0 {
			fn.Config.MinReplicas = req.Config.MinReplicas
		}
		if req.Config.MaxReplicas > 0 {
			fn.Config.MaxReplicas = req.Config.MaxReplicas
		}
		if req.Config.CooldownPeriod > 0 {
			fn.Config.CooldownPeriod = req.Config.CooldownPeriod
		}
		if req.Config.Concurrency > 0 {
			fn.Config.Concurrency = req.Config.Concurrency
		}
		if len(req.Config.Env) > 0 {
			fn.Config.Env = req.Config.Env
		}
	}

	// Mark function for redeployment
	fn.Status = types.FunctionStatusPending

	if err := h.repos.Functions.Update(ctx, fn); err != nil {
		h.logger.Error(ctx, "Failed to update function",
			logging.String("function_id", fnID),
			logging.Error("error", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update function"})
		return
	}

	h.logger.Info(ctx, "Function updated", logging.String("function_id", fnID))

	c.JSON(http.StatusOK, gin.H{
		"function": fn,
		"message":  "Function updated. Redeployment will start shortly.",
	})
}

// DeleteFunction deletes a serverless function
// DELETE /v1/functions/:id
func (h *Handler) DeleteFunction(c *gin.Context) {
	ctx := c.Request.Context()
	fnID := c.Param("id")

	// Parse function ID
	fnUUID, err := uuid.Parse(fnID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid function_id format"})
		return
	}

	// Soft delete the function
	if err := h.repos.Functions.SoftDelete(ctx, fnUUID); err != nil {
		if err == sql.ErrNoRows {
			c.JSON(http.StatusNotFound, gin.H{"error": "function not found"})
			return
		}
		h.logger.Error(ctx, "Failed to delete function",
			logging.String("function_id", fnID),
			logging.Error("error", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to delete function"})
		return
	}

	h.logger.Info(ctx, "Function deleted", logging.String("function_id", fnID))

	c.JSON(http.StatusOK, gin.H{"message": "Function deletion initiated"})
}

// InvokeFunctionRequest defines the request body for invoking a function
type InvokeFunctionRequest struct {
	Body    string            `json:"body,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
	Method  string            `json:"method,omitempty"`
}

// InvokeFunction invokes a function directly (for testing)
// POST /v1/functions/:id/invoke
func (h *Handler) InvokeFunction(c *gin.Context) {
	ctx := c.Request.Context()
	fnID := c.Param("id")

	// Parse function ID
	fnUUID, err := uuid.Parse(fnID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid function_id format"})
		return
	}

	// Parse request body
	var req InvokeFunctionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		// Allow empty body for simple invocations
		req = InvokeFunctionRequest{}
	}

	// Get function
	fn, err := h.repos.Functions.GetByID(ctx, fnUUID)
	if err != nil {
		if err == sql.ErrNoRows {
			c.JSON(http.StatusNotFound, gin.H{"error": "function not found"})
			return
		}
		h.logger.Error(ctx, "Failed to get function", logging.Error("error", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to get function"})
		return
	}

	// Check function status
	if fn.Status != types.FunctionStatusReady {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error":  "function not ready",
			"status": fn.Status,
		})
		return
	}

	// Check endpoint
	if fn.Endpoint == "" {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "function endpoint not available"})
		return
	}

	// Proxy the invocation to the function's KEDA HTTP add-on endpoint
	requestID := uuid.New().String()

	method := http.MethodPost
	if req.Method != "" {
		method = strings.ToUpper(req.Method)
	}

	timeout := 30 * time.Second
	if fn.Config.Timeout > 0 {
		timeout = time.Duration(fn.Config.Timeout) * time.Second
	}

	httpClient := &http.Client{Timeout: timeout}

	var body io.Reader
	if req.Body != "" {
		body = strings.NewReader(req.Body)
	}

	proxyReq, err := http.NewRequestWithContext(ctx, method, fn.Endpoint, body)
	if err != nil {
		h.logger.Error(ctx, "Failed to create proxy request", logging.Error("error", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create invocation request"})
		return
	}

	// Forward caller-specified headers
	for k, v := range req.Headers {
		proxyReq.Header.Set(k, v)
	}
	proxyReq.Header.Set("X-Request-ID", requestID)
	proxyReq.Header.Set("X-Enclii-Function-ID", fn.ID.String())

	// Default content type if body is present
	if body != nil && proxyReq.Header.Get("Content-Type") == "" {
		proxyReq.Header.Set("Content-Type", "application/json")
	}

	resp, err := httpClient.Do(proxyReq)
	if err != nil {
		h.logger.Error(ctx, "Function invocation failed",
			logging.String("function_id", fnID),
			logging.String("endpoint", fn.Endpoint),
			logging.Error("error", err))
		c.JSON(http.StatusBadGateway, gin.H{
			"error":      "function invocation failed",
			"request_id": requestID,
			"detail":     err.Error(),
		})
		return
	}
	defer func() { _ = resp.Body.Close() }()

	// Stream the function's response back to the caller
	for k, vals := range resp.Header {
		for _, v := range vals {
			c.Writer.Header().Add(k, v)
		}
	}
	c.Writer.Header().Set("X-Request-ID", requestID)
	c.Writer.WriteHeader(resp.StatusCode)
	io.Copy(c.Writer, resp.Body) //nolint:errcheck
}

// GetFunctionLogs retrieves logs for a function
// GET /v1/functions/:id/logs
func (h *Handler) GetFunctionLogs(c *gin.Context) {
	ctx := c.Request.Context()
	fnID := c.Param("id")

	// Parse function ID
	fnUUID, err := uuid.Parse(fnID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid function_id format"})
		return
	}

	// Parse query parameters
	limitStr := c.DefaultQuery("limit", "100")
	var limit int
	if _, err := c.GetQuery("limit"); err {
		limit = 100
	} else {
		if n, err := parseInt(limitStr); err == nil {
			limit = n
		} else {
			limit = 100
		}
	}

	var since *time.Time
	if sinceStr := c.Query("since"); sinceStr != "" {
		if t, err := time.Parse(time.RFC3339, sinceStr); err == nil {
			since = &t
		}
	}

	// Verify function exists
	fn, err := h.repos.Functions.GetByID(ctx, fnUUID)
	if err != nil {
		if err == sql.ErrNoRows {
			c.JSON(http.StatusNotFound, gin.H{"error": "function not found"})
			return
		}
		h.logger.Error(ctx, "Failed to get function", logging.Error("error", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to get function"})
		return
	}

	// Get invocations as a proxy for logs
	invocations, err := h.repos.Functions.GetInvocationsByFunction(ctx, fnUUID, limit, since)
	if err != nil {
		h.logger.Error(ctx, "Failed to get function invocations", logging.Error("error", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to get logs"})
		return
	}

	// For now, return invocations as log entries
	// Full log streaming would be implemented via K8s pod logs
	logs := make([]types.FunctionLogEntry, 0, len(invocations))
	for _, inv := range invocations {
		level := "info"
		message := "Function invoked"
		if inv.ErrorType != nil {
			level = "error"
			message = "Function error: " + *inv.ErrorType
		}
		if inv.ColdStart {
			message += " (cold start)"
		}
		logs = append(logs, types.FunctionLogEntry{
			Timestamp: inv.StartedAt,
			Level:     level,
			Message:   message,
			RequestID: inv.RequestID,
			Source:    "system",
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"function_id":   fn.ID,
		"function_name": fn.Name,
		"logs":          logs,
		"count":         len(logs),
	})
}

// GetFunctionMetrics retrieves metrics for a function
// GET /v1/functions/:id/metrics
func (h *Handler) GetFunctionMetrics(c *gin.Context) {
	ctx := c.Request.Context()
	fnID := c.Param("id")

	// Parse function ID
	fnUUID, err := uuid.Parse(fnID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid function_id format"})
		return
	}

	// Parse query parameters
	period := c.DefaultQuery("period", "hourly")
	if period != "hourly" && period != "daily" && period != "weekly" {
		period = "hourly"
	}

	since := time.Now().Add(-24 * time.Hour) // Default: last 24 hours
	if sinceStr := c.Query("since"); sinceStr != "" {
		if t, err := time.Parse(time.RFC3339, sinceStr); err == nil {
			since = t
		}
	}

	// Verify function exists
	fn, err := h.repos.Functions.GetByID(ctx, fnUUID)
	if err != nil {
		if err == sql.ErrNoRows {
			c.JSON(http.StatusNotFound, gin.H{"error": "function not found"})
			return
		}
		h.logger.Error(ctx, "Failed to get function", logging.Error("error", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to get function"})
		return
	}

	// Get aggregated metrics
	metrics, err := h.repos.Functions.GetMetrics(ctx, fnUUID, period, since)
	if err != nil {
		h.logger.Error(ctx, "Failed to get function metrics", logging.Error("error", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to get metrics"})
		return
	}

	// Also return current function stats
	c.JSON(http.StatusOK, gin.H{
		"function_id":   fn.ID,
		"function_name": fn.Name,
		"current": gin.H{
			"total_invocations":  fn.InvocationCount,
			"avg_duration_ms":    fn.AvgDurationMs,
			"available_replicas": fn.AvailableReplicas,
			"last_invoked_at":    fn.LastInvokedAt,
		},
		"period":  period,
		"since":   since,
		"metrics": metrics,
	})
}

// Helper function to convert []*types.Function to []types.Function
func convertFunctionPointers(fns []*types.Function) []types.Function {
	result := make([]types.Function, len(fns))
	for i, fn := range fns {
		result[i] = *fn
	}
	return result
}

// Helper function to parse int from string
func parseInt(s string) (int, error) {
	var n int
	_, err := fmt.Sscanf(s, "%d", &n)
	if err != nil {
		return 0, err
	}
	return n, nil
}
