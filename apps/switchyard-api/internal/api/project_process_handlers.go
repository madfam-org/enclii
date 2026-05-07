package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/middleware"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

type projectProcess struct {
	ID            string            `json:"id"`
	CorrelationID string            `json:"correlation_id"`
	ProjectID     string            `json:"project_id"`
	ProjectSlug   string            `json:"project_slug"`
	ServiceID     string            `json:"service_id,omitempty"`
	ServiceName   string            `json:"service_name,omitempty"`
	Kind          string            `json:"kind"`
	Status        string            `json:"status"`
	Phase         string            `json:"phase,omitempty"`
	Message       string            `json:"message,omitempty"`
	Branch        string            `json:"branch,omitempty"`
	CommitSHA     string            `json:"commit_sha,omitempty"`
	Environment   string            `json:"environment,omitempty"`
	Progress      *int              `json:"progress,omitempty"`
	Source        string            `json:"source"`
	Links         map[string]string `json:"links,omitempty"`
	StartedAt     *time.Time        `json:"started_at,omitempty"`
	UpdatedAt     time.Time         `json:"updated_at"`
	CompletedAt   *time.Time        `json:"completed_at,omitempty"`
}

type serviceProcessSummary struct {
	ServiceID    string          `json:"service_id"`
	ServiceName  string          `json:"service_name,omitempty"`
	ActiveCount  int             `json:"active_count"`
	FailedCount  int             `json:"failed_count"`
	BlockedCount int             `json:"blocked_count"`
	Latest       *projectProcess `json:"latest,omitempty"`
}

type projectProcessSummary struct {
	ProjectID    string                  `json:"project_id"`
	ProjectSlug  string                  `json:"project_slug"`
	ActiveCount  int                     `json:"active_count"`
	FailedCount  int                     `json:"failed_count"`
	BlockedCount int                     `json:"blocked_count"`
	Latest       *projectProcess         `json:"latest,omitempty"`
	Processes    []projectProcess        `json:"processes"`
	Services     []serviceProcessSummary `json:"services"`
}

type projectProcessSummaryResponse struct {
	Count     int                     `json:"count"`
	Summaries []projectProcessSummary `json:"summaries"`
}

func (h *Handler) GetProjectProcessSummaries(c *gin.Context) {
	ctx := c.Request.Context()
	if h == nil || h.repos == nil || h.repos.Projects == nil || h.repos.Services == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "process summary dependencies unavailable"})
		return
	}

	projectIDs, err := parseProjectProcessIDs(c.Query("project_ids"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if len(projectIDs) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "project_ids is required"})
		return
	}

	limitPerProject := projectProcessLimit(c.Query("limit_per_project"), 5, 20)
	activeOnly := strings.EqualFold(c.Query("active_only"), "true")

	summaries := make([]projectProcessSummary, 0, len(projectIDs))
	for _, projectID := range projectIDs {
		visible, err := h.projectVisibleInActingScope(ctx, c, projectID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to verify project scope"})
			return
		}
		if !visible {
			continue
		}

		summary, err := h.buildProjectProcessSummary(ctx, projectID, limitPerProject, activeOnly)
		if err != nil {
			if err == sql.ErrNoRows {
				continue
			}
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to build process summary"})
			return
		}
		summaries = append(summaries, *summary)
	}

	c.JSON(http.StatusOK, gin.H{
		"count":     len(summaries),
		"summaries": summaries,
	})
}

func (h *Handler) StreamProjectProcessSummaries(c *gin.Context) {
	projectIDs, err := parseProjectProcessIDs(c.Query("project_ids"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if len(projectIDs) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "project_ids is required"})
		return
	}
	h.streamProjectProcessSummaries(c, projectIDs)
}

func (h *Handler) GetProjectProcesses(c *gin.Context) {
	ctx := c.Request.Context()
	if h == nil || h.repos == nil || h.repos.Projects == nil || h.repos.Services == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "process summary dependencies unavailable"})
		return
	}

	project, err := h.repos.Projects.GetBySlug(c.Param("slug"))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Project not found"})
		return
	}
	if !h.enforceActingTeamForProject(c, project.ID) {
		return
	}

	limit := projectProcessLimit(c.Query("limit"), 50, 100)
	activeOnly := strings.EqualFold(c.Query("active_only"), "true")
	summary, err := h.buildProjectProcessSummary(ctx, project.ID, limit, activeOnly)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to build process timeline"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"count":      len(summary.Processes),
		"project_id": summary.ProjectID,
		"slug":       summary.ProjectSlug,
		"processes":  summary.Processes,
		"summary":    summary,
	})
}

func (h *Handler) StreamProjectProcesses(c *gin.Context) {
	if h == nil || h.repos == nil || h.repos.Projects == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "process stream dependencies unavailable"})
		return
	}
	project, err := h.repos.Projects.GetBySlug(c.Param("slug"))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Project not found"})
		return
	}
	if !h.enforceActingTeamForProject(c, project.ID) {
		return
	}
	h.streamProjectProcessSummaries(c, []uuid.UUID{project.ID})
}

func (h *Handler) streamProjectProcessSummaries(c *gin.Context, projectIDs []uuid.UUID) {
	ctx := c.Request.Context()
	if h == nil || h.repos == nil || h.repos.Projects == nil || h.repos.Services == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "process stream dependencies unavailable"})
		return
	}

	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "streaming unsupported"})
		return
	}

	limitPerProject := projectProcessLimit(c.Query("limit_per_project"), 5, 20)
	activeOnly := !strings.EqualFold(c.Query("active_only"), "false")
	once := strings.EqualFold(c.Query("once"), "true")
	interval := projectProcessStreamInterval(c.Query("interval_ms"))

	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")
	c.Status(http.StatusOK)

	emit := func(event string, data any) error {
		payload, err := json.Marshal(data)
		if err != nil {
			return err
		}
		eventID := strconv.FormatInt(time.Now().UnixNano(), 10)
		if _, err := fmt.Fprintf(c.Writer, "id: %s\nevent: %s\ndata: %s\n\n", eventID, event, payload); err != nil {
			return err
		}
		flusher.Flush()
		return nil
	}

	build := func() (projectProcessSummaryResponse, error) {
		summaries := make([]projectProcessSummary, 0, len(projectIDs))
		for _, projectID := range projectIDs {
			visible, err := h.projectVisibleInActingScope(ctx, c, projectID)
			if err != nil {
				return projectProcessSummaryResponse{}, err
			}
			if !visible {
				continue
			}
			summary, err := h.buildProjectProcessSummary(ctx, projectID, limitPerProject, activeOnly)
			if err != nil {
				if err == sql.ErrNoRows {
					continue
				}
				return projectProcessSummaryResponse{}, err
			}
			summaries = append(summaries, *summary)
		}
		return projectProcessSummaryResponse{Count: len(summaries), Summaries: summaries}, nil
	}

	initial, err := build()
	if err != nil {
		_ = emit("error", gin.H{"error": "failed to build process summary"})
		return
	}
	if err := emit("summary", initial); err != nil || once {
		return
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	heartbeat := time.NewTicker(25 * time.Second)
	defer heartbeat.Stop()

	lastPayload, _ := json.Marshal(initial)
	for {
		select {
		case <-ctx.Done():
			return
		case <-heartbeat.C:
			if _, err := fmt.Fprintf(c.Writer, ": heartbeat %s\n\n", time.Now().UTC().Format(time.RFC3339)); err != nil {
				return
			}
			flusher.Flush()
		case <-ticker.C:
			next, err := build()
			if err != nil {
				if err := emit("error", gin.H{"error": "failed to refresh process summary"}); err != nil {
					return
				}
				continue
			}
			payload, _ := json.Marshal(next)
			if string(payload) == string(lastPayload) {
				continue
			}
			lastPayload = payload
			if err := emit("summary", next); err != nil {
				return
			}
		}
	}
}

func (h *Handler) buildProjectProcessSummary(ctx context.Context, projectID uuid.UUID, limit int, activeOnly bool) (*projectProcessSummary, error) {
	project, err := h.repos.Projects.GetByID(ctx, projectID)
	if err != nil {
		return nil, err
	}

	services, err := h.repos.Services.ListByProject(project.ID)
	if err != nil {
		return nil, err
	}
	h.enrichServicesWithRolloutState(ctx, services)

	serviceNames := map[string]string{}
	for _, service := range services {
		serviceNames[service.ID.String()] = service.Name
	}

	processes := make([]projectProcess, 0)
	if h.repos.LifecycleEvents != nil {
		eventLimit := limit * 5
		if eventLimit < 25 {
			eventLimit = 25
		}
		if eventLimit > 200 {
			eventLimit = 200
		}
		events, err := h.repos.LifecycleEvents.GetTimeline(ctx, types.LifecycleTimelineQuery{
			ProjectID: &project.ID,
			Limit:     eventLimit,
		})
		if err != nil {
			return nil, err
		}
		for _, event := range events {
			process := processFromLifecycleEvent(project, serviceNames, event)
			if !activeOnly || isVisibleWhenActiveOnly(process.Status) {
				processes = append(processes, process)
			}
		}
	}

	for _, service := range services {
		process, ok := processFromServiceState(project, service)
		if ok && (!activeOnly || isVisibleWhenActiveOnly(process.Status)) {
			processes = append(processes, process)
		}
	}

	return summarizeProjectProcesses(project, processes, limit), nil
}

func summarizeProjectProcesses(project *types.Project, processes []projectProcess, limit int) *projectProcessSummary {
	sortProjectProcesses(processes)

	summary := &projectProcessSummary{
		ProjectID:   project.ID.String(),
		ProjectSlug: project.Slug,
		Processes:   []projectProcess{},
		Services:    []serviceProcessSummary{},
	}

	serviceSummaries := map[string]*serviceProcessSummary{}
	for i := range processes {
		process := &processes[i]
		if summary.Latest == nil {
			summary.Latest = process
		}
		switch process.Status {
		case "queued", "running", "waiting":
			summary.ActiveCount++
		case "failed":
			summary.FailedCount++
		case "blocked":
			summary.BlockedCount++
		}

		if process.ServiceID != "" {
			serviceSummary := serviceSummaries[process.ServiceID]
			if serviceSummary == nil {
				serviceSummary = &serviceProcessSummary{
					ServiceID:   process.ServiceID,
					ServiceName: process.ServiceName,
				}
				serviceSummaries[process.ServiceID] = serviceSummary
			}
			if serviceSummary.Latest == nil {
				serviceSummary.Latest = process
			}
			switch process.Status {
			case "queued", "running", "waiting":
				serviceSummary.ActiveCount++
			case "failed":
				serviceSummary.FailedCount++
			case "blocked":
				serviceSummary.BlockedCount++
			}
		}
	}

	if limit <= 0 || limit > len(processes) {
		limit = len(processes)
	}
	summary.Processes = append(summary.Processes, processes[:limit]...)

	for _, serviceSummary := range serviceSummaries {
		summary.Services = append(summary.Services, *serviceSummary)
	}
	sortServiceProcessSummaries(summary.Services)

	return summary
}

func processFromLifecycleEvent(project *types.Project, serviceNames map[string]string, event types.DeploymentLifecycleEvent) projectProcess {
	kind, status := lifecycleProcessKindAndStatus(event.EventType)
	serviceID := ""
	serviceName := ""
	if event.ServiceID != nil {
		serviceID = event.ServiceID.String()
		serviceName = serviceNames[serviceID]
	}

	environment := ""
	if event.TargetEnv != nil {
		environment = *event.TargetEnv
	}

	message := ""
	if event.Message != nil {
		message = *event.Message
	}

	process := projectProcess{
		ID:            event.ID.String(),
		CorrelationID: lifecycleCorrelationID(event),
		ProjectID:     project.ID.String(),
		ProjectSlug:   project.Slug,
		ServiceID:     serviceID,
		ServiceName:   serviceName,
		Kind:          kind,
		Status:        status,
		Phase:         event.EventType,
		Message:       message,
		Branch:        event.Branch,
		CommitSHA:     event.CommitSHA,
		Environment:   environment,
		Source:        lifecycleSource(event.Source),
		UpdatedAt:     event.CreatedAt,
		Links:         map[string]string{},
	}
	if status == "queued" || status == "running" || status == "waiting" {
		process.StartedAt = &event.CreatedAt
	} else {
		process.CompletedAt = &event.CreatedAt
	}
	if event.DeploymentID != nil {
		process.Links["deployment"] = "/deployments/" + event.DeploymentID.String()
	}
	if event.CommitSHA != "" {
		process.Links["lifecycle"] = fmt.Sprintf("/projects/%s/deployments?commit=%s", project.Slug, event.CommitSHA)
	}
	if len(process.Links) == 0 {
		process.Links = nil
	}
	return process
}

func processFromServiceState(project *types.Project, service *types.Service) (projectProcess, bool) {
	if service == nil {
		return projectProcess{}, false
	}

	kind := "deploy"
	status := ""
	phase := service.Status
	message := ""

	switch service.RolloutState {
	case "blocked":
		kind = "rollout"
		status = "blocked"
		phase = "rollout_blocked"
		if service.RolloutBlockedReason != "" {
			message = "Rollout blocked: " + service.RolloutBlockedReason
		} else {
			message = "Rollout blocked"
		}
	case "progressing":
		kind = "rollout"
		status = "running"
		phase = "rollout_progressing"
		message = "Rollout progressing"
	}

	if status == "" {
		switch service.Status {
		case "deploying":
			status = "running"
			message = "Deployment in progress"
		case "pending":
			status = "waiting"
			message = "Service pending"
		case "failed":
			status = "failed"
			message = "Service failed"
		default:
			return projectProcess{}, false
		}
	}

	updatedAt := service.UpdatedAt
	if updatedAt.IsZero() {
		updatedAt = time.Now().UTC()
	}

	process := projectProcess{
		ID:            "service-state:" + service.ID.String() + ":" + phase,
		CorrelationID: "service:" + service.ID.String() + ":" + phase,
		ProjectID:     project.ID.String(),
		ProjectSlug:   project.Slug,
		ServiceID:     service.ID.String(),
		ServiceName:   service.Name,
		Kind:          kind,
		Status:        status,
		Phase:         phase,
		Message:       message,
		Branch:        service.LastCommitBranch,
		Environment:   service.AutoDeployEnv,
		Source:        "switchyard",
		UpdatedAt:     updatedAt,
		Links: map[string]string{
			"logs": fmt.Sprintf("/projects/%s/services/%s/logs", project.Slug, service.ID.String()),
		},
	}
	if service.LastDeployment != nil {
		process.StartedAt = service.LastDeployment
	}
	return process, true
}

func lifecycleProcessKindAndStatus(eventType string) (string, string) {
	switch eventType {
	case types.LifecyclePushReceived:
		return "git_push", "succeeded"
	case types.LifecycleBuildStarted:
		return "build", "running"
	case types.LifecycleBuildSucceeded:
		return "build", "succeeded"
	case types.LifecycleBuildFailed:
		return "build", "failed"
	case types.LifecycleImagePushed:
		return "image", "succeeded"
	case types.LifecycleDigestCommitted:
		return "digest", "succeeded"
	case types.LifecycleDeployStarted:
		return "deploy", "running"
	case types.LifecycleDeploySynced:
		return "gitops_sync", "running"
	case types.LifecycleDeployHealthy:
		return "deploy", "succeeded"
	case types.LifecycleDeployDegraded:
		return "rollout", "blocked"
	case types.LifecycleDeployFailed:
		return "deploy", "failed"
	case types.LifecyclePreviewCreated:
		return "preview", "succeeded"
	case types.LifecyclePreviewDestroyed:
		return "preview", "succeeded"
	default:
		return "operator", "unknown"
	}
}

func lifecycleSource(source string) string {
	switch source {
	case types.SourceGitHubWebhook:
		return "github"
	case types.SourceCICallback:
		return "ci_callback"
	case types.SourceArgocdCallback:
		return "argocd"
	case types.SourcePlatform:
		return "switchyard"
	case types.SourceManual:
		return "enclii_ops"
	default:
		return "switchyard"
	}
}

func lifecycleCorrelationID(event types.DeploymentLifecycleEvent) string {
	if explicit, ok := event.Metadata["correlation_id"].(string); ok && explicit != "" {
		return explicit
	}
	if event.ServiceID != nil && event.CommitSHA != "" {
		env := ""
		if event.TargetEnv != nil {
			env = *event.TargetEnv
		}
		return strings.Join([]string{event.ServiceID.String(), event.CommitSHA, env}, ":")
	}
	if event.ProjectID != nil && event.CommitSHA != "" {
		return strings.Join([]string{event.ProjectID.String(), event.CommitSHA}, ":")
	}
	return event.ID.String()
}

func parseProjectProcessIDs(raw string) ([]uuid.UUID, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	parts := strings.Split(raw, ",")
	ids := make([]uuid.UUID, 0, len(parts))
	seen := map[uuid.UUID]bool{}
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		id, err := uuid.Parse(part)
		if err != nil {
			return nil, fmt.Errorf("invalid project_id %q", part)
		}
		if seen[id] {
			continue
		}
		seen[id] = true
		ids = append(ids, id)
	}
	return ids, nil
}

func projectProcessLimit(raw string, fallback, max int) int {
	if fallback <= 0 {
		fallback = 5
	}
	limit := fallback
	if strings.TrimSpace(raw) != "" {
		if parsed, err := strconv.Atoi(raw); err == nil {
			limit = parsed
		}
	}
	if limit <= 0 {
		return fallback
	}
	if limit > max {
		return max
	}
	return limit
}

func projectProcessStreamInterval(raw string) time.Duration {
	const fallback = 10 * time.Second
	if strings.TrimSpace(raw) == "" {
		return fallback
	}
	ms, err := strconv.Atoi(raw)
	if err != nil {
		return fallback
	}
	if ms < 5000 {
		ms = 5000
	}
	if ms > 60000 {
		ms = 60000
	}
	return time.Duration(ms) * time.Millisecond
}

func (h *Handler) projectVisibleInActingScope(ctx context.Context, c *gin.Context, projectID uuid.UUID) (bool, error) {
	actingTeamID, ok := middleware.ActingTeamID(c)
	if !ok {
		return true, nil
	}
	teamID, err := h.repos.Projects.GetTeamID(ctx, projectID)
	if err != nil {
		if err == sql.ErrNoRows {
			return false, nil
		}
		return false, err
	}
	return teamID == actingTeamID, nil
}

func isVisibleWhenActiveOnly(status string) bool {
	switch status {
	case "queued", "running", "waiting", "failed", "blocked":
		return true
	default:
		return false
	}
}

func sortProjectProcesses(processes []projectProcess) {
	for i := 1; i < len(processes); i++ {
		current := processes[i]
		j := i - 1
		for j >= 0 && processes[j].UpdatedAt.Before(current.UpdatedAt) {
			processes[j+1] = processes[j]
			j--
		}
		processes[j+1] = current
	}
}

func sortServiceProcessSummaries(summaries []serviceProcessSummary) {
	for i := 1; i < len(summaries); i++ {
		current := summaries[i]
		j := i - 1
		for j >= 0 && serviceSummarySortKey(summaries[j]) < serviceSummarySortKey(current) {
			summaries[j+1] = summaries[j]
			j--
		}
		summaries[j+1] = current
	}
}

func serviceSummarySortKey(summary serviceProcessSummary) int {
	return summary.BlockedCount*100 + summary.FailedCount*10 + summary.ActiveCount
}
