package api

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/argocd"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
)

func (h *Handler) listProjectCardArgoEvidence(ctx context.Context, observedAt time.Time) map[string]projectCardArgoApplicationEvidence {
	evidence := map[string]projectCardArgoApplicationEvidence{}
	if h == nil || h.k8sClient == nil || h.k8sClient.DynamicClient == nil {
		return evidence
	}

	namespace := argocd.DefaultNamespace
	if h.config != nil && strings.TrimSpace(h.config.ArgocdNamespace) != "" {
		namespace = h.config.ArgocdNamespace
	}
	apps, err := h.k8sClient.DynamicClient.Resource(argoApplicationGVR).Namespace(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return evidence
	}

	for _, app := range apps.Items {
		appEvidence := projectCardArgoEvidenceFromApplication(app, observedAt)
		if appEvidence.Name != "" {
			evidence[appEvidence.Name] = appEvidence
		}
	}
	return evidence
}

func projectCardArgoEvidenceFromApplication(app unstructured.Unstructured, observedAt time.Time) projectCardArgoApplicationEvidence {
	syncStatus, _, _ := unstructured.NestedString(app.Object, "status", "sync", "status")
	if syncStatus == "" {
		syncStatus = "Unknown"
	}
	healthStatus, _, _ := unstructured.NestedString(app.Object, "status", "health", "status")
	if healthStatus == "" {
		healthStatus = "Unknown"
	}
	revision, _, _ := unstructured.NestedString(app.Object, "status", "sync", "revision")
	destinationNamespace, _, _ := unstructured.NestedString(app.Object, "spec", "destination", "namespace")

	labels := app.GetLabels()
	annotations := app.GetAnnotations()
	return projectCardArgoApplicationEvidence{
		Name:                 app.GetName(),
		SyncStatus:           syncStatus,
		HealthStatus:         healthStatus,
		Revision:             revision,
		DestinationNamespace: destinationNamespace,
		ObservedAt:           observedAt,
		SourceRepo:           annotations["enclii.dev/source-repo"],
		PartOf:               labels["app.kubernetes.io/part-of"],
	}
}

func (h *Handler) projectCardOnboardingArgoApps(ctx context.Context) map[uuid.UUID]string {
	apps := map[uuid.UUID]string{}
	if h == nil || h.repos == nil || h.repos.Onboardings == nil {
		return apps
	}
	registrations, err := h.repos.Onboardings.List(ctx)
	if err != nil {
		return apps
	}
	for _, reg := range registrations {
		if reg.ArgocdAppName != nil && strings.TrimSpace(*reg.ArgocdAppName) != "" {
			apps[reg.ProjectID] = strings.TrimSpace(*reg.ArgocdAppName)
		}
	}
	return apps
}

func matchProjectCardArgoEvidence(
	project *types.Project,
	services []*types.Service,
	onboardingArgoAppsByProject map[uuid.UUID]string,
	argoEvidenceByName map[string]projectCardArgoApplicationEvidence,
) *projectCardArgoApplicationEvidence {
	if project == nil || len(argoEvidenceByName) == 0 {
		return nil
	}

	candidateNames := map[string]bool{}
	for _, candidate := range projectCardArgoNameCandidates(project, onboardingArgoAppsByProject) {
		candidateNames[candidate] = true
	}

	projectKeys := map[string]bool{
		projectCardSanitizeArgoName(project.Slug): true,
		projectCardSanitizeArgoName(project.Name): true,
	}
	serviceRepos := map[string]bool{}
	for _, service := range services {
		if service == nil {
			continue
		}
		if repo := normalizeProjectCardGitRepo(service.GitRepo); repo != "" {
			serviceRepos[repo] = true
		}
	}

	var matched *projectCardArgoApplicationEvidence
	matchedRank := 0
	for _, evidence := range argoEvidenceByName {
		rank := projectCardArgoEvidenceMatchRank(evidence, candidateNames, projectKeys, serviceRepos)
		if rank == 0 {
			continue
		}
		evidence := evidence
		if matched == nil || rank > matchedRank || (rank == matchedRank && projectCardArgoEvidenceLessSevere(*matched, evidence)) {
			matched = &evidence
			matchedRank = rank
		}
	}
	return matched
}

func projectCardArgoEvidenceMatchRank(
	evidence projectCardArgoApplicationEvidence,
	candidateNames map[string]bool,
	projectKeys map[string]bool,
	serviceRepos map[string]bool,
) int {
	name := projectCardSanitizeArgoName(evidence.Name)
	if candidateNames[name] {
		return 2
	}
	for projectKey := range projectKeys {
		if projectKey == "" {
			continue
		}
		if name == projectKey || strings.HasPrefix(name, projectKey+"-") {
			return 2
		}
	}
	if projectKeys[projectCardSanitizeArgoName(evidence.PartOf)] {
		return 1
	}
	if repo := normalizeProjectCardGitRepo(evidence.SourceRepo); repo != "" && serviceRepos[repo] {
		return 1
	}
	return 0
}

func projectCardArgoEvidenceLessSevere(current, candidate projectCardArgoApplicationEvidence) bool {
	currentSeverity := projectCardArgoEvidenceSeverity(current)
	candidateSeverity := projectCardArgoEvidenceSeverity(candidate)
	if currentSeverity != candidateSeverity {
		return currentSeverity < candidateSeverity
	}
	return projectCardSanitizeArgoName(candidate.Name) < projectCardSanitizeArgoName(current.Name)
}

func projectCardArgoEvidenceSeverity(evidence projectCardArgoApplicationEvidence) int {
	switch aggregateStatusFromArgoEvidence(&evidence) {
	case "failing":
		return 3
	case "degraded":
		return 2
	default:
		return 1
	}
}

func projectCardArgoNameCandidates(project *types.Project, onboardingArgoAppsByProject map[uuid.UUID]string) []string {
	seen := map[string]bool{}
	candidates := []string{}
	add := func(value string) {
		value = projectCardSanitizeArgoName(value)
		if value == "" || seen[value] {
			return
		}
		seen[value] = true
		candidates = append(candidates, value)
	}

	if onboardingArgoAppsByProject != nil {
		add(onboardingArgoAppsByProject[project.ID])
	}
	if projectCardSanitizeArgoName(project.Slug) == "enclii" {
		add("core-services")
	}
	add(project.Slug)
	add(project.Slug + "-services")
	add(project.Name)
	add(project.Name + "-services")
	return candidates
}

func projectCardSanitizeArgoName(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.ReplaceAll(value, "_", "-")
	value = strings.ReplaceAll(value, " ", "-")
	return value
}

func normalizeProjectCardGitRepo(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.TrimPrefix(value, "https://github.com/")
	value = strings.TrimPrefix(value, "http://github.com/")
	value = strings.TrimPrefix(value, "git@github.com:")
	value = strings.TrimSuffix(value, ".git")
	return value
}
