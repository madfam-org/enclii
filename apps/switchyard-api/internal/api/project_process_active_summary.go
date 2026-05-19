package api

import (
	"strings"
	"time"

	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

func compactProjectProcessesForActiveSummary(processes []projectProcess, now time.Time) []projectProcess {
	return compactProjectProcessesForActiveSummaryWithStableServices(processes, nil, now)
}

func compactProjectProcessesForActiveSummaryWithStableServices(processes []projectProcess, stableServiceKeys map[string]struct{}, now time.Time) []projectProcess {
	if len(processes) == 0 {
		return processes
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}

	current := make([]projectProcess, 0, len(processes))
	liveServiceKeys := map[string]struct{}{}
	for _, process := range processes {
		if !isServiceStateProcess(process) {
			continue
		}
		if !isVisibleInActiveProcessSummary(process, now) {
			continue
		}
		current = append(current, process)
		liveServiceKeys[activeProcessStateKey(process)] = struct{}{}
	}

	sorted := append([]projectProcess(nil), processes...)
	sortProjectProcesses(sorted)
	seenLifecycleKeys := map[string]struct{}{}
	for _, process := range sorted {
		if isServiceStateProcess(process) {
			continue
		}
		if isProjectLevelNonProductionTerminalProcess(process) {
			continue
		}
		key := activeProcessStateKey(process)
		if _, ok := liveServiceKeys[key]; ok {
			continue
		}
		if _, ok := stableServiceKeys[key]; ok {
			continue
		}
		if _, ok := seenLifecycleKeys[key]; ok {
			continue
		}
		seenLifecycleKeys[key] = struct{}{}
		if isVisibleInActiveProcessSummary(process, now) {
			current = append(current, process)
		}
	}

	sortProjectProcesses(current)
	return current
}

func stableServiceActiveProcessKeys(project *types.Project, services []*types.Service) map[string]struct{} {
	keys := map[string]struct{}{}
	if project == nil {
		return keys
	}
	for _, service := range services {
		if !isStableServiceForActiveProcessSummary(service) {
			continue
		}
		for _, environment := range activeServiceEnvironments(service) {
			keys[activeProcessStateKey(projectProcess{
				ProjectID:   project.ID.String(),
				ProjectSlug: project.Slug,
				ServiceID:   service.ID.String(),
				ServiceName: service.Name,
				Kind:        "deploy",
				Environment: environment,
			})] = struct{}{}
		}
	}
	return keys
}

func isStableServiceForActiveProcessSummary(service *types.Service) bool {
	if service == nil {
		return false
	}
	if service.Status != "running" {
		return false
	}
	if service.Health != types.HealthStatusHealthy && service.Health != "stale" {
		return false
	}
	return service.RolloutState != "progressing" && service.RolloutState != "blocked"
}

func activeServiceEnvironments(service *types.Service) []string {
	if service == nil {
		return nil
	}
	env := strings.TrimSpace(service.AutoDeployEnv)
	if env == "" {
		return []string{"", "production"}
	}
	return []string{env}
}

func isProjectLevelNonProductionTerminalProcess(process projectProcess) bool {
	if process.ServiceID != "" || process.ServiceName != "" || isServiceStateProcess(process) {
		return false
	}
	if process.Status != "failed" && process.Status != "blocked" {
		return false
	}
	env := strings.ToLower(strings.TrimSpace(process.Environment))
	return env != "" && env != "production" && env != "prod"
}

func isVisibleInActiveProcessSummary(process projectProcess, now time.Time) bool {
	if !isVisibleWhenActiveOnly(process.Status) {
		return false
	}
	if isServiceStateProcess(process) {
		return true
	}
	if process.UpdatedAt.IsZero() {
		return false
	}
	age := now.Sub(process.UpdatedAt)
	if age < 0 {
		age = 0
	}
	switch process.Status {
	case "queued", "running", "waiting":
		return age <= lifecycleActiveProcessStaleAfter
	case "failed", "blocked":
		return age <= lifecycleFailedProcessStaleAfter
	default:
		return true
	}
}

func activeProcessStateKey(process projectProcess) string {
	project := process.ProjectID
	if project == "" {
		project = process.ProjectSlug
	}
	service := process.ServiceID
	if service == "" {
		service = process.ServiceName
	}
	if service == "" {
		service = "__project__"
	}
	environment := process.Environment
	if environment == "" {
		environment = "__default__"
	}
	return strings.Join([]string{
		project,
		service,
		environment,
		activeProcessFamily(process.Kind),
	}, ":")
}

func activeProcessFamily(kind string) string {
	switch kind {
	case "git_push", "ci", "build", "image", "digest":
		return "build"
	case "deploy", "gitops_sync", "rollout", "rollback":
		return "deploy"
	default:
		if kind == "" {
			return "operator"
		}
		return kind
	}
}

func isServiceStateProcess(process projectProcess) bool {
	return strings.HasPrefix(process.ID, "service-state:") || strings.HasPrefix(process.CorrelationID, "service:")
}

func isVisibleWhenActiveOnly(status string) bool {
	switch status {
	case "queued", "running", "waiting", "failed", "blocked":
		return true
	default:
		return false
	}
}
