package api

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/k8s"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/logging"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// getWebSocketUpgrader returns an upgrader configured with allowed origins from config
func (h *Handler) getWebSocketUpgrader() *websocket.Upgrader {
	allowedOrigins := h.config.WebSocketAllowedOrigins
	return &websocket.Upgrader{
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
		CheckOrigin: func(r *http.Request) bool {
			// Allow connections from configured origins
			origin := r.Header.Get("Origin")
			if len(allowedOrigins) == 0 {
				// If no origins configured, deny all for security
				return false
			}
			for _, allowed := range allowedOrigins {
				if origin == allowed {
					return true
				}
			}
			return false
		},
	}
}

// LogStreamMessage represents a WebSocket message for log streaming
type LogStreamMessage struct {
	Type      string    `json:"type"` // "log", "error", "info", "connected", "disconnected"
	Pod       string    `json:"pod,omitempty"`
	Container string    `json:"container,omitempty"`
	Timestamp time.Time `json:"timestamp"`
	Message   string    `json:"message"`
}

// StreamLogsWS handles WebSocket connections for real-time log streaming
// GET /v1/deployments/:id/logs/stream
func (h *Handler) StreamLogsWS(c *gin.Context) {
	ctx := c.Request.Context()
	deploymentID := c.Param("id")

	// Get deployment
	deployment, err := h.repos.Deployments.GetByID(ctx, deploymentID)
	if err != nil {
		h.logger.Error(ctx, "Failed to get deployment for log streaming", logging.Error("error", err))
		c.JSON(http.StatusNotFound, gin.H{"error": "Deployment not found"})
		return
	}

	// Get release to find service ID
	release, err := h.repos.Releases.GetByID(deployment.ReleaseID)
	if err != nil {
		h.logger.Error(ctx, "Failed to get release for log streaming", logging.Error("error", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get release"})
		return
	}

	// Get service for namespace information
	service, err := h.repos.Services.GetByID(release.ServiceID)
	if err != nil {
		h.logger.Error(ctx, "Failed to get service for log streaming", logging.Error("error", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get service"})
		return
	}

	// Get project for namespace
	project, err := h.repos.Projects.GetByID(ctx, service.ProjectID)
	if err != nil {
		h.logger.Error(ctx, "Failed to get project for log streaming", logging.Error("error", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get project"})
		return
	}

	// Get environment
	env, err := h.repos.Environments.GetByID(ctx, deployment.EnvironmentID)
	if err != nil {
		h.logger.Error(ctx, "Failed to get environment for log streaming", logging.Error("error", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get environment"})
		return
	}

	// Upgrade HTTP connection to WebSocket
	conn, err := h.getWebSocketUpgrader().Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		h.logger.Error(ctx, "Failed to upgrade to WebSocket", logging.Error("error", err))
		return
	}
	defer func() { _ = conn.Close() }()

	// Parse query parameters
	tailLines := int64(100)
	if tl := c.Query("lines"); tl != "" {
		if parsed, err := strconv.ParseInt(tl, 10, 64); err == nil && parsed > 0 {
			tailLines = parsed
		}
	}

	timestamps := c.Query("timestamps") == "true"

	// Build namespace and label selector
	namespace := fmt.Sprintf("enclii-%s-%s", project.Slug, env.Name)
	labelSelector := fmt.Sprintf("app=%s", service.Name)

	// Send connected message
	connMsg := LogStreamMessage{
		Type:      "connected",
		Timestamp: time.Now(),
		Message:   fmt.Sprintf("Connected to logs for %s in %s", service.Name, namespace),
	}
	if err := conn.WriteJSON(connMsg); err != nil {
		h.logger.Error(ctx, "Failed to send connected message", logging.Error("error", err))
		return
	}

	// Create cancellable context for the stream
	streamCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Handle WebSocket close
	go func() {
		for {
			_, _, err := conn.ReadMessage()
			if err != nil {
				cancel()
				return
			}
		}
	}()

	// Create channels for log streaming
	logChan := make(chan k8s.LogLine, 100)
	errChan := make(chan error, 10)

	// Start streaming logs
	go h.k8sClient.StreamLogs(streamCtx, k8s.LogStreamOptions{
		Namespace:     namespace,
		LabelSelector: labelSelector,
		TailLines:     tailLines,
		Follow:        true,
		Timestamps:    timestamps,
	}, logChan, errChan)

	// Process logs and send to WebSocket
	for {
		select {
		case <-streamCtx.Done():
			// Send disconnected message
			disconnMsg := LogStreamMessage{
				Type:      "disconnected",
				Timestamp: time.Now(),
				Message:   "Log stream disconnected",
			}
			_ = conn.WriteJSON(disconnMsg)
			return

		case logLine, ok := <-logChan:
			if !ok {
				// Channel closed, stream ended
				return
			}
			msg := LogStreamMessage{
				Type:      "log",
				Pod:       logLine.Pod,
				Container: logLine.Container,
				Timestamp: logLine.Timestamp,
				Message:   logLine.Message,
			}
			if err := conn.WriteJSON(msg); err != nil {
				h.logger.Error(ctx, "Failed to write log message", logging.Error("error", err))
				return
			}

		case err, ok := <-errChan:
			if !ok {
				continue
			}
			errMsg := LogStreamMessage{
				Type:      "error",
				Timestamp: time.Now(),
				Message:   err.Error(),
			}
			if err := conn.WriteJSON(errMsg); err != nil {
				h.logger.Error(ctx, "Failed to write error message", logging.Error("error", err))
				return
			}
		}
	}
}

// StreamServiceLogsWS handles WebSocket connections for real-time log streaming by service ID
// GET /v1/services/:id/logs/stream
func (h *Handler) StreamServiceLogsWS(c *gin.Context) {
	ctx := c.Request.Context()
	serviceID := c.Param("id")
	envName := c.DefaultQuery("env", "development")

	// Parse service ID
	serviceUUID, err := uuid.Parse(serviceID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid service ID"})
		return
	}

	// Get service
	service, err := h.repos.Services.GetByID(serviceUUID)
	if err != nil {
		h.logger.Error(ctx, "Failed to get service for log streaming", logging.Error("error", err))
		c.JSON(http.StatusNotFound, gin.H{"error": "Service not found"})
		return
	}

	// Get project for namespace
	project, err := h.repos.Projects.GetByID(ctx, service.ProjectID)
	if err != nil {
		h.logger.Error(ctx, "Failed to get project for log streaming", logging.Error("error", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get project"})
		return
	}

	// Upgrade HTTP connection to WebSocket
	conn, err := h.getWebSocketUpgrader().Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		h.logger.Error(ctx, "Failed to upgrade to WebSocket", logging.Error("error", err))
		return
	}
	defer func() { _ = conn.Close() }()

	// Parse query parameters
	tailLines := int64(100)
	if tl := c.Query("lines"); tl != "" {
		if parsed, err := strconv.ParseInt(tl, 10, 64); err == nil && parsed > 0 {
			tailLines = parsed
		}
	}

	timestamps := c.Query("timestamps") == "true"

	// Build namespace and label selector
	namespace := fmt.Sprintf("enclii-%s-%s", project.Slug, envName)
	labelSelector := fmt.Sprintf("app=%s", service.Name)

	// Send connected message
	connMsg := LogStreamMessage{
		Type:      "connected",
		Timestamp: time.Now(),
		Message:   fmt.Sprintf("Connected to logs for %s in %s", service.Name, namespace),
	}
	if err := conn.WriteJSON(connMsg); err != nil {
		h.logger.Error(ctx, "Failed to send connected message", logging.Error("error", err))
		return
	}

	// Create cancellable context for the stream
	streamCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Handle WebSocket close
	go func() {
		for {
			_, _, err := conn.ReadMessage()
			if err != nil {
				cancel()
				return
			}
		}
	}()

	// Create channels for log streaming
	logChan := make(chan k8s.LogLine, 100)
	errChan := make(chan error, 10)

	// Start streaming logs
	go h.k8sClient.StreamLogs(streamCtx, k8s.LogStreamOptions{
		Namespace:     namespace,
		LabelSelector: labelSelector,
		TailLines:     tailLines,
		Follow:        true,
		Timestamps:    timestamps,
	}, logChan, errChan)

	// Process logs and send to WebSocket
	for {
		select {
		case <-streamCtx.Done():
			disconnMsg := LogStreamMessage{
				Type:      "disconnected",
				Timestamp: time.Now(),
				Message:   "Log stream disconnected",
			}
			_ = conn.WriteJSON(disconnMsg)
			return

		case logLine, ok := <-logChan:
			if !ok {
				return
			}
			msg := LogStreamMessage{
				Type:      "log",
				Pod:       logLine.Pod,
				Container: logLine.Container,
				Timestamp: logLine.Timestamp,
				Message:   logLine.Message,
			}
			if err := conn.WriteJSON(msg); err != nil {
				h.logger.Error(ctx, "Failed to write log message", logging.Error("error", err))
				return
			}

		case err, ok := <-errChan:
			if !ok {
				continue
			}
			errMsg := LogStreamMessage{
				Type:      "error",
				Timestamp: time.Now(),
				Message:   err.Error(),
			}
			if err := conn.WriteJSON(errMsg); err != nil {
				h.logger.Error(ctx, "Failed to write error message", logging.Error("error", err))
				return
			}
		}
	}
}

// GetLogsHistory returns historical logs (non-streaming)
// GET /v1/services/:id/logs/history
func (h *Handler) GetLogsHistory(c *gin.Context) {
	ctx := c.Request.Context()
	serviceID := c.Param("id")
	envName := c.DefaultQuery("env", "development")

	// Parse service ID
	serviceUUID, err := uuid.Parse(serviceID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid service ID"})
		return
	}

	// Get service
	service, err := h.repos.Services.GetByID(serviceUUID)
	if err != nil {
		h.logger.Error(ctx, "Failed to get service", logging.Error("error", err))
		c.JSON(http.StatusNotFound, gin.H{"error": "Service not found"})
		return
	}

	// Get project
	project, err := h.repos.Projects.GetByID(ctx, service.ProjectID)
	if err != nil {
		h.logger.Error(ctx, "Failed to get project", logging.Error("error", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get project"})
		return
	}

	// Parse query parameters
	lines := 100
	if l := c.Query("lines"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil && parsed > 0 && parsed <= 10000 {
			lines = parsed
		}
	}

	// Build namespace and label selector
	namespace := fmt.Sprintf("enclii-%s-%s", project.Slug, envName)
	labelSelector := fmt.Sprintf("app=%s", service.Name)

	// Get logs
	logs, err := h.k8sClient.GetLogs(ctx, namespace, labelSelector, lines, false)
	if err != nil {
		h.logger.Error(ctx, "Failed to get logs", logging.Error("error", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get logs"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"service_id":   serviceID,
		"service_name": service.Name,
		"environment":  envName,
		"namespace":    namespace,
		"logs":         logs,
		"lines":        lines,
	})
}

// GetBuildLogs returns build logs from R2 storage
// GET /v1/services/:id/builds/:build_id/logs
// build_id corresponds to release_id
func (h *Handler) GetBuildLogs(c *gin.Context) {
	ctx := c.Request.Context()
	serviceID := c.Param("id")
	buildID := c.Param("build_id")

	// Parse service ID
	serviceUUID, err := uuid.Parse(serviceID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid service ID"})
		return
	}

	// Parse build/release ID
	buildUUID, err := uuid.Parse(buildID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid build ID"})
		return
	}

	// Verify service exists
	service, err := h.repos.Services.GetByID(serviceUUID)
	if err != nil {
		h.logger.Error(ctx, "Failed to get service", logging.Error("error", err))
		c.JSON(http.StatusNotFound, gin.H{"error": "Service not found"})
		return
	}

	// Verify release exists and belongs to the service
	release, err := h.repos.Releases.GetByID(buildUUID)
	if err != nil {
		h.logger.Error(ctx, "Failed to get release", logging.Error("error", err))
		c.JSON(http.StatusNotFound, gin.H{"error": "Build not found"})
		return
	}

	if release.ServiceID != serviceUUID {
		c.JSON(http.StatusForbidden, gin.H{"error": "Build does not belong to this service"})
		return
	}

	// Return build metadata - logs are retrieved via builder namespace streaming
	// In production, build logs are also persisted to R2 storage after build completion
	c.JSON(http.StatusOK, gin.H{
		"service_id":   serviceID,
		"service_name": service.Name,
		"build_id":     buildID,
		"status":       string(release.Status),
		"git_sha":      release.GitSHA,
		"image_uri":    release.ImageURI,
		"created_at":   release.CreatedAt,
		"message":      "Use WebSocket endpoint /logs/stream for live build logs",
	})
}

// StreamBuildLogsWS handles WebSocket connections for real-time build log streaming
// GET /v1/services/:id/builds/:build_id/logs/stream
// This streams logs from the builder process in real-time
func (h *Handler) StreamBuildLogsWS(c *gin.Context) {
	ctx := c.Request.Context()
	serviceID := c.Param("id")
	buildID := c.Param("build_id")

	// Parse service ID
	serviceUUID, err := uuid.Parse(serviceID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid service ID"})
		return
	}

	// Parse build/release ID
	buildUUID, err := uuid.Parse(buildID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid build ID"})
		return
	}

	// Verify service exists
	service, err := h.repos.Services.GetByID(serviceUUID)
	if err != nil {
		h.logger.Error(ctx, "Failed to get service", logging.Error("error", err))
		c.JSON(http.StatusNotFound, gin.H{"error": "Service not found"})
		return
	}

	// Verify release exists and belongs to the service
	release, err := h.repos.Releases.GetByID(buildUUID)
	if err != nil {
		h.logger.Error(ctx, "Failed to get release", logging.Error("error", err))
		c.JSON(http.StatusNotFound, gin.H{"error": "Build not found"})
		return
	}

	if release.ServiceID != serviceUUID {
		c.JSON(http.StatusForbidden, gin.H{"error": "Build does not belong to this service"})
		return
	}

	// Upgrade HTTP connection to WebSocket
	conn, err := h.getWebSocketUpgrader().Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		h.logger.Error(ctx, "Failed to upgrade to WebSocket", logging.Error("error", err))
		return
	}
	defer func() { _ = conn.Close() }()

	// Send connected message
	connMsg := LogStreamMessage{
		Type:      "connected",
		Timestamp: time.Now(),
		Message:   fmt.Sprintf("Connected to build logs for %s (build %s)", service.Name, buildID[:8]),
	}
	if err := conn.WriteJSON(connMsg); err != nil {
		h.logger.Error(ctx, "Failed to send connected message", logging.Error("error", err))
		return
	}

	// Create cancellable context
	streamCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Handle WebSocket close
	go func() {
		for {
			_, _, err := conn.ReadMessage()
			if err != nil {
				cancel()
				return
			}
		}
	}()

	// For active builds, stream from builder namespace pods
	if release.Status == types.ReleaseStatusBuilding {
		// Stream from the builder pod
		namespace := "enclii-builds"
		labelSelector := fmt.Sprintf("build-id=%s", buildID)

		logChan := make(chan k8s.LogLine, 100)
		errChan := make(chan error, 10)

		go h.k8sClient.StreamLogs(streamCtx, k8s.LogStreamOptions{
			Namespace:     namespace,
			LabelSelector: labelSelector,
			TailLines:     100,
			Follow:        true,
			Timestamps:    true,
		}, logChan, errChan)

		for {
			select {
			case <-streamCtx.Done():
				disconnMsg := LogStreamMessage{
					Type:      "disconnected",
					Timestamp: time.Now(),
					Message:   "Build log stream disconnected",
				}
				_ = conn.WriteJSON(disconnMsg)
				return

			case logLine, ok := <-logChan:
				if !ok {
					// Channel closed, build may have completed
					statusMsg := LogStreamMessage{
						Type:      "info",
						Timestamp: time.Now(),
						Message:   "Build process ended",
					}
					_ = conn.WriteJSON(statusMsg)
					return
				}
				msg := LogStreamMessage{
					Type:      "log",
					Pod:       logLine.Pod,
					Container: logLine.Container,
					Timestamp: logLine.Timestamp,
					Message:   logLine.Message,
				}
				if err := conn.WriteJSON(msg); err != nil {
					h.logger.Error(ctx, "Failed to write log message", logging.Error("error", err))
					return
				}

			case err, ok := <-errChan:
				if !ok {
					continue
				}
				errMsg := LogStreamMessage{
					Type:      "error",
					Timestamp: time.Now(),
					Message:   err.Error(),
				}
				if err := conn.WriteJSON(errMsg); err != nil {
					h.logger.Error(ctx, "Failed to write error message", logging.Error("error", err))
					return
				}
			}
		}
	}

	// For completed builds, return info message
	statusMsg := LogStreamMessage{
		Type:      "info",
		Timestamp: time.Now(),
		Message:   fmt.Sprintf("Build completed with status: %s. Use GET endpoint for historical logs.", release.Status),
	}
	_ = conn.WriteJSON(statusMsg)
}

// LogSearchRequest represents a log search request
type LogSearchRequest struct {
	Query     string `json:"query" binding:"required"`
	StartTime string `json:"start_time"` // RFC3339 format
	EndTime   string `json:"end_time"`   // RFC3339 format
	Limit     int    `json:"limit"`
}

// SearchLogs searches through logs for a pattern
// POST /v1/services/:id/logs/search
func (h *Handler) SearchLogs(c *gin.Context) {
	ctx := c.Request.Context()
	serviceID := c.Param("id")
	envName := c.DefaultQuery("env", "development")

	var req LogSearchRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body"})
		return
	}

	// Parse service ID
	serviceUUID, err := uuid.Parse(serviceID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid service ID"})
		return
	}

	// Get service
	service, err := h.repos.Services.GetByID(serviceUUID)
	if err != nil {
		h.logger.Error(ctx, "Failed to get service", logging.Error("error", err))
		c.JSON(http.StatusNotFound, gin.H{"error": "Service not found"})
		return
	}

	// Get project
	project, err := h.repos.Projects.GetByID(ctx, service.ProjectID)
	if err != nil {
		h.logger.Error(ctx, "Failed to get project", logging.Error("error", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get project"})
		return
	}

	// Set default limit
	limit := 1000
	if req.Limit > 0 && req.Limit <= 10000 {
		limit = req.Limit
	}

	// Build namespace and label selector
	namespace := fmt.Sprintf("enclii-%s-%s", project.Slug, envName)
	labelSelector := fmt.Sprintf("app=%s", service.Name)

	// Get logs
	logs, err := h.k8sClient.GetLogs(ctx, namespace, labelSelector, limit, false)
	if err != nil {
		h.logger.Error(ctx, "Failed to get logs", logging.Error("error", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get logs"})
		return
	}

	// Simple grep-style search (in production, use a log aggregation service)
	var matchingLines []string
	for _, line := range splitLines(logs) {
		if containsIgnoreCase(line, req.Query) {
			matchingLines = append(matchingLines, line)
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"service_id":   serviceID,
		"service_name": service.Name,
		"environment":  envName,
		"query":        req.Query,
		"matches":      len(matchingLines),
		"logs":         matchingLines,
	})
}

// Helper functions
func splitLines(s string) []string {
	var lines []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			lines = append(lines, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}

func containsIgnoreCase(s, substr string) bool {
	s = strings.ToLower(s)
	substr = strings.ToLower(substr)
	return strings.Contains(s, substr)
}
