package api

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/logging"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

// ── Request / Response types ────────────────────────────────────────

// ExecRequest represents a command to execute in a service pod.
type ExecRequest struct {
	Command []string `json:"command" binding:"required"`
	Timeout int      `json:"timeout"` // seconds, default 60, max 1800
	Env     string   `json:"env"`     // environment, default "production"
}

// ExecResponse contains the result of a pod exec operation.
type ExecResponse struct {
	Stdout     string `json:"stdout"`
	Stderr     string `json:"stderr"`
	ExitCode   int    `json:"exit_code"`
	Pod        string `json:"pod"`
	DurationMs int64  `json:"duration_ms"`
}

// RestartRequest triggers a rolling restart of a service.
type RestartRequest struct {
	Env    string `json:"env"`    // default "production"
	Reason string `json:"reason"` // audit reason
}

// ScaleRequest sets the replica count for a service.
type ScaleRequest struct {
	Replicas int32  `json:"replicas" binding:"required"`
	Env      string `json:"env"` // default "production"
}

// MigrateRequest runs a database migration command.
type MigrateRequest struct {
	Command []string `json:"command" binding:"required"`
	Env     string   `json:"env"`
	DryRun  bool     `json:"dry_run"`
}

// DetailedHealthResponse aggregates pod, probe, and resource status.
type DetailedHealthResponse struct {
	Status       string      `json:"status"` // healthy, degraded, unhealthy
	ServiceName  string      `json:"service_name"`
	Environment  string      `json:"environment"`
	Pods         []PodHealth `json:"pods"`
	TotalPods    int         `json:"total_pods"`
	ReadyPods    int         `json:"ready_pods"`
	RestartCount int         `json:"restart_count"`
	CheckedAt    string      `json:"checked_at"`
}

// PodHealth describes the health of a single pod.
type PodHealth struct {
	Name     string `json:"name"`
	Status   string `json:"status"`
	Ready    bool   `json:"ready"`
	Restarts int    `json:"restarts"`
	Age      string `json:"age"`
}

// ── SecOps: Command allowlists ──────────────────────────────────────

// execAllowedPrefixes defines commands that can be executed via the exec endpoint.
// Defense in depth: the Selva tool also validates against its own allowlist.
var execAllowedPrefixes = []string{
	"python manage.py migrate",
	"python manage.py showmigrations",
	"python manage.py check",
	"python manage.py shell -c",
	"npx prisma migrate deploy",
	"npx prisma migrate status",
	"npx prisma db push",
	"npx tsx scripts/",
	"alembic upgrade",
	"alembic current",
	"node -e",
	"cat ",
	"ls ",
	"echo ",
}

// migrateAllowedPrefixes is a stricter subset for migration commands.
var migrateAllowedPrefixes = []string{
	"python manage.py migrate",
	"npx prisma migrate deploy",
	"npx prisma db push",
	"alembic upgrade",
	"rake db:migrate",
	"flyway migrate",
}

func isCommandAllowed(cmd []string, allowlist []string) bool {
	if len(cmd) == 0 {
		return false
	}
	full := strings.Join(cmd, " ")
	for _, prefix := range allowlist {
		if strings.HasPrefix(full, prefix) {
			return true
		}
	}
	return false
}

// ── Handlers ────────────────────────────────────────────────────────

// ExecService executes a command in a running service pod.
// POST /v1/services/:id/exec
// SecOps: admin-only, command allowlist, timeout cap, audit logged.
func (h *Handler) ExecService(c *gin.Context) {
	ctx := c.Request.Context()
	serviceID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid service ID"})
		return
	}

	var req ExecRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// SecOps: command allowlist
	if !isCommandAllowed(req.Command, execAllowedPrefixes) {
		h.logger.Warn(ctx, "Exec command blocked by allowlist",
			logging.String("command", strings.Join(req.Command, " ")),
		)
		c.JSON(http.StatusForbidden, gin.H{
			"error":   "command not in allowlist",
			"allowed": execAllowedPrefixes,
		})
		return
	}

	// SecOps: timeout cap at 30 minutes
	timeout := req.Timeout
	if timeout <= 0 {
		timeout = 60
	}
	if timeout > 1800 {
		timeout = 1800
	}

	env := req.Env
	if env == "" {
		env = "production"
	}

	// Resolve service -> namespace -> pod
	svc, err := h.repos.Services.GetByID(serviceID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "service not found"})
		return
	}

	project, err := h.repos.Projects.GetByID(ctx, svc.ProjectID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "project not found"})
		return
	}

	namespace := project.Slug
	start := time.Now()

	// Execute command via K8s API
	stdout, stderr, exitCode, execErr := h.k8sClient.ExecCommand(
		ctx,
		namespace,
		svc.Name,
		req.Command,
		time.Duration(timeout)*time.Second,
	)

	duration := time.Since(start).Milliseconds()

	if execErr != nil {
		h.logger.Error(ctx, "Exec failed",
			logging.String("service", svc.Name),
			logging.Error("error", execErr),
		)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":       fmt.Sprintf("exec failed: %v", execErr),
			"duration_ms": duration,
		})
		return
	}

	h.logger.Info(ctx, "Exec completed",
		logging.String("service", svc.Name),
		logging.String("command", strings.Join(req.Command, " ")),
		logging.Field{Key: "exit_code", Value: exitCode},
		logging.Field{Key: "duration_ms", Value: duration},
	)

	c.JSON(http.StatusOK, ExecResponse{
		Stdout:     stdout,
		Stderr:     stderr,
		ExitCode:   exitCode,
		Pod:        svc.Name,
		DurationMs: duration,
	})
}

// RestartService triggers a rolling restart of a service.
// POST /v1/services/:id/restart
func (h *Handler) RestartService(c *gin.Context) {
	ctx := c.Request.Context()
	serviceID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid service ID"})
		return
	}

	var req RestartRequest
	_ = c.ShouldBindJSON(&req) // optional body

	env := req.Env
	if env == "" {
		env = "production"
	}
	reason := req.Reason
	if reason == "" {
		reason = "manual-restart"
	}

	svc, err := h.repos.Services.GetByID(serviceID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "service not found"})
		return
	}

	project, err := h.repos.Projects.GetByID(ctx, svc.ProjectID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "project not found"})
		return
	}

	if err := h.k8sClient.RollingRestart(ctx, project.Slug, svc.Name); err != nil {
		h.logger.Error(ctx, "Restart failed",
			logging.String("service", svc.Name),
			logging.Error("error", err),
		)
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("restart failed: %v", err)})
		return
	}

	h.logger.Info(ctx, "Service restarted",
		logging.String("service", svc.Name),
		logging.String("reason", reason),
	)

	c.JSON(http.StatusOK, gin.H{
		"message":      fmt.Sprintf("Rolling restart initiated for %s", svc.Name),
		"service_name": svc.Name,
		"environment":  env,
		"reason":       reason,
		"restarted_at": time.Now().UTC().Format(time.RFC3339),
	})
}

// ScaleService sets the replica count for a service.
// POST /v1/services/:id/scale
func (h *Handler) ScaleService(c *gin.Context) {
	ctx := c.Request.Context()
	serviceID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid service ID"})
		return
	}

	var req ScaleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// SecOps: cap replicas
	if req.Replicas > 10 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "replicas capped at 10"})
		return
	}
	if req.Replicas < 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "replicas must be >= 0"})
		return
	}

	env := req.Env
	if env == "" {
		env = "production"
	}

	svc, err := h.repos.Services.GetByID(serviceID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "service not found"})
		return
	}

	project, err := h.repos.Projects.GetByID(ctx, svc.ProjectID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "project not found"})
		return
	}

	if err := h.k8sClient.ScaleDeployment(ctx, project.Slug, svc.Name, req.Replicas); err != nil {
		h.logger.Error(ctx, "Scale failed",
			logging.String("service", svc.Name),
			logging.Error("error", err),
		)
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("scale failed: %v", err)})
		return
	}

	h.logger.Info(ctx, "Service scaled",
		logging.String("service", svc.Name),
		logging.Field{Key: "replicas", Value: req.Replicas},
	)

	// P2.3: fan out service.scaled to outbound webhook subscribers.
	// Non-blocking — the HTTP response to the scale call is unaffected
	// by webhook delivery success/failure. We snapshot primitives here
	// before launching the goroutine so we never touch c after return.
	if h.webhookDispatcher != nil {
		data := map[string]any{
			"service_id":   svc.ID.String(),
			"service_name": svc.Name,
			"env":          env,
			"to_replicas":  req.Replicas,
			"actor":        c.GetString("user_email"),
		}
		projectID := svc.ProjectID
		svcName := svc.Name
		go func() {
			bg := context.Background()
			if err := h.webhookDispatcher.Dispatch(
				bg, projectID, nil,
				types.OutboundEventServiceScaled, data,
			); err != nil {
				h.logger.Warn(bg, "service.scaled webhook dispatch failed",
					logging.String("service", svcName),
					logging.Error("dispatch_error", err))
			}
		}()
	}

	c.JSON(http.StatusOK, gin.H{
		"message":      fmt.Sprintf("Scaled %s to %d replicas", svc.Name, req.Replicas),
		"service_name": svc.Name,
		"environment":  env,
		"replicas":     req.Replicas,
	})
}

// MigrateService runs a database migration command in a service pod.
// POST /v1/services/:id/migrate
// SecOps: admin-only, migration command allowlist, confirmation header required.
func (h *Handler) MigrateService(c *gin.Context) {
	ctx := c.Request.Context()
	serviceID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid service ID"})
		return
	}

	var req MigrateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// SecOps: migration command allowlist (stricter than exec)
	if !isCommandAllowed(req.Command, migrateAllowedPrefixes) {
		c.JSON(http.StatusForbidden, gin.H{
			"error":   "command not in migration allowlist",
			"allowed": migrateAllowedPrefixes,
		})
		return
	}

	// SecOps: require confirmation header
	if c.GetHeader("X-Confirm-Migration") != "true" {
		c.JSON(http.StatusPreconditionRequired, gin.H{
			"error": "migration requires X-Confirm-Migration: true header",
		})
		return
	}

	// Delegate to ExecService with migration-specific context
	env := req.Env
	if env == "" {
		env = "production"
	}

	svc, err := h.repos.Services.GetByID(serviceID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "service not found"})
		return
	}

	project, err := h.repos.Projects.GetByID(ctx, svc.ProjectID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "project not found"})
		return
	}

	start := time.Now()
	stdout, stderr, exitCode, execErr := h.k8sClient.ExecCommand(
		ctx,
		project.Slug,
		svc.Name,
		req.Command,
		300*time.Second, // 5 minute timeout for migrations
	)
	duration := time.Since(start).Milliseconds()

	if execErr != nil {
		h.logger.Error(ctx, "Migration failed",
			logging.String("service", svc.Name),
			logging.Error("error", execErr),
		)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":       fmt.Sprintf("migration failed: %v", execErr),
			"stdout":      stdout,
			"stderr":      stderr,
			"duration_ms": duration,
		})
		return
	}

	h.logger.Info(ctx, "Migration completed",
		logging.String("service", svc.Name),
		logging.String("command", strings.Join(req.Command, " ")),
		logging.Field{Key: "exit_code", Value: exitCode},
		logging.Field{Key: "duration_ms", Value: duration},
	)

	c.JSON(http.StatusOK, gin.H{
		"message":      fmt.Sprintf("Migration completed for %s (exit code: %d)", svc.Name, exitCode),
		"stdout":       stdout,
		"stderr":       stderr,
		"exit_code":    exitCode,
		"duration_ms":  duration,
		"service_name": svc.Name,
	})
}

// GetDetailedHealth returns aggregated health status for a service.
// GET /v1/services/:id/health/detailed
func (h *Handler) GetDetailedHealth(c *gin.Context) {
	ctx := c.Request.Context()
	serviceID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid service ID"})
		return
	}

	svc, err := h.repos.Services.GetByID(serviceID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "service not found"})
		return
	}

	project, err := h.repos.Projects.GetByID(ctx, svc.ProjectID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "project not found"})
		return
	}

	// Get deployment status info from K8s
	statusInfo, err := h.k8sClient.GetDeploymentStatusInfo(ctx, project.Slug, svc.Name)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":  fmt.Sprintf("health check failed: %v", err),
			"status": "unknown",
		})
		return
	}

	// Fetch pods to calculate aggregate restart counts and populate pod details
	podList, err := h.k8sClient.ListPods(ctx, project.Slug, fmt.Sprintf("enclii.dev/service=%s", svc.Name))
	totalRestarts := 0
	var pods []PodHealth
	if err == nil && podList != nil {
		for _, pod := range podList.Items {
			podRestarts := 0
			for _, containerStatus := range pod.Status.ContainerStatuses {
				podRestarts += int(containerStatus.RestartCount)
			}
			totalRestarts += podRestarts

			// Determine simple pod status
			podPhase := string(pod.Status.Phase)
			isReady := false
			for _, cond := range pod.Status.Conditions {
				if cond.Type == "Ready" && cond.Status == "True" {
					isReady = true
					break
				}
			}

			pods = append(pods, PodHealth{
				Name:     pod.Name,
				Status:   podPhase,
				Ready:    isReady,
				Restarts: podRestarts,
				Age:      time.Since(pod.CreationTimestamp.Time).Truncate(time.Second).String(),
			})
		}
	} else {
		h.logger.Warn(ctx, "Failed to list pods for detailed health", logging.Error("error", err))
	}

	// Determine overall health
	status := "healthy"
	if statusInfo.ReadyReplicas < statusInfo.DesiredReplicas {
		status = "degraded"
	}
	if statusInfo.ReadyReplicas == 0 {
		status = "unhealthy"
	}

	c.JSON(http.StatusOK, DetailedHealthResponse{
		Status:       status,
		ServiceName:  svc.Name,
		Environment:  c.DefaultQuery("env", "production"),
		Pods:         pods,
		TotalPods:    int(statusInfo.DesiredReplicas),
		ReadyPods:    int(statusInfo.ReadyReplicas),
		RestartCount: totalRestarts,
		CheckedAt:    time.Now().UTC().Format(time.RFC3339),
	})
}
