package api

import (
	"context"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/k8s"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/workloadtargets"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func (h *Handler) readKarafielProductionTargetSnapshots(ctx context.Context) ([]workloadtargets.Snapshot, error) {
	return h.readKarafielProductionTargetSnapshotsWithDiagnostic(ctx, nil)
}

func (h *Handler) readKarafielProductionTargetSnapshotsWithDiagnostic(ctx context.Context, diagnostic *workloadtargets.ProductionFenceDiagnostic) ([]workloadtargets.Snapshot, error) {
	if h.repos == nil || h.repos.DB() == nil || h.repos.Projects == nil || h.repos.Services == nil || h.repos.Environments == nil || h.repos.Onboardings == nil ||
		h.opsKubeClient() == nil || h.k8sClient == nil || h.k8sClient.DynamicClient == nil || h.config == nil || h.config.GitHubToken == "" {
		return nil, diagnostic.SnapshotFailure("prerequisites", workloadtargets.ErrUnavailable)
	}
	base := workloadtargets.Snapshot{}
	var err error
	base.Project, err = h.repos.Projects.GetBySlug("karafiel")
	if err != nil || base.Project == nil {
		return nil, diagnostic.SnapshotFailure("project_intent", workloadtargets.ErrIntent)
	}
	base.Environment, err = h.repos.Environments.GetByProjectAndName(base.Project.ID, "production")
	if err != nil || base.Environment == nil || base.Environment.KubeNamespace != "karafiel" {
		return nil, diagnostic.SnapshotFailure("environment_intent", workloadtargets.ErrIntent)
	}
	base.Registration, err = h.repos.Onboardings.GetByRepo(ctx, "madfam-org/karafiel")
	if err != nil || base.Registration == nil || base.Registration.ProjectID != base.Project.ID || base.Registration.OnboardStatus != "completed" {
		return nil, diagnostic.SnapshotFailure("registration_intent", workloadtargets.ErrIntent)
	}
	services, err := h.repos.Services.ListByProject(base.Project.ID)
	if err != nil {
		return nil, diagnostic.SnapshotFailure("fleet_registry", workloadtargets.ErrUnavailable)
	}
	byName := map[string]*types.Service{}
	for _, s := range services {
		if s.K8sNamespace == nil || *s.K8sNamespace != "karafiel" {
			continue
		}
		if !workloadtargets.KarafielProductionDeployment(s.Name) || byName[s.Name] != nil {
			return nil, diagnostic.SnapshotFailure("fleet_registry", workloadtargets.ErrConflict)
		}
		byName[s.Name] = s
	}
	if len(byName) != 5 {
		return nil, diagnostic.SnapshotFailure("fleet_registry", workloadtargets.ErrIntent)
	}
	base.NamespaceIntentExclusive, err = h.workloadTargetStore().NamespaceIntentExclusive(ctx, base.Project.ID, "karafiel")
	if err != nil || !base.NamespaceIntentExclusive {
		return nil, diagnostic.SnapshotFailure("namespace_claim", workloadtargets.ErrIntent)
	}
	kube := h.opsKubeClient()
	base.Namespace, err = kube.CoreV1().Namespaces().Get(ctx, "karafiel", metav1.GetOptions{})
	if err != nil {
		return nil, diagnostic.SnapshotFailure("namespace_read", workloadtargets.ErrOwnership)
	}
	base.Application, err = h.k8sClient.DynamicClient.Resource(argoApplicationGVR).Namespace("argocd").Get(ctx, "karafiel-services", metav1.GetOptions{})
	if err != nil {
		return nil, diagnostic.SnapshotFailure("application_read", workloadtargets.ErrOwnership)
	}
	base.SourceRoot, err = h.k8sClient.DynamicClient.Resource(argoApplicationGVR).Namespace("argocd").Get(ctx, "enclii-infrastructure", metav1.GetOptions{})
	if err != nil {
		return nil, diagnostic.SnapshotFailure("root_read", workloadtargets.ErrOwnership)
	}
	appsets := argoApplicationGVR
	appsets.Resource = "applicationsets"
	base.SourceParent, err = h.k8sClient.DynamicClient.Resource(appsets).Namespace("argocd").Get(ctx, "project-applications", metav1.GetOptions{})
	if err != nil {
		return nil, diagnostic.SnapshotFailure("parent_read", workloadtargets.ErrOwnership)
	}
	rootRevision, _, err := unstructured.NestedString(base.SourceRoot.Object, "status", "sync", "revision")
	if err != nil || !workloadtargets.ValidRevision(rootRevision) {
		return nil, diagnostic.SnapshotFailure("root_revision", workloadtargets.ErrOwnership)
	}
	content, _, err := getGitHubFileContent(ctx, h.config.GitHubToken, "madfam-org", "enclii", "infra/argocd/projects/karafiel/config.json", rootRevision)
	if err != nil || len(content) > 8192 || k8s.DecodeInitialBootstrapJSON([]byte(content), &base.SourceConfiguration) != nil {
		return nil, diagnostic.SnapshotFailure("source_configuration", workloadtargets.ErrOwnership)
	}
	base.SourceConfigurationRevision = rootRevision
	base.ArgoConfig, err = kube.CoreV1().ConfigMaps("argocd").Get(ctx, "argocd-cm", metav1.GetOptions{})
	if err != nil {
		return nil, diagnostic.SnapshotFailure("tracking_configuration", workloadtargets.ErrOwnership)
	}
	revision, _, err := unstructured.NestedString(base.Application.Object, "status", "sync", "revision")
	if err != nil || !workloadtargets.ValidRevision(revision) {
		return nil, diagnostic.SnapshotFailure("application_revision", workloadtargets.ErrOwnership)
	}
	files, err := h.fetchManifestFiles(ctx, "madfam-org", "karafiel", "infra/k8s/production", revision)
	if err != nil {
		return nil, diagnostic.SnapshotFailure("manifest_fetch", workloadtargets.ErrOwnership)
	}
	snapshots := make([]workloadtargets.Snapshot, 0, 5)
	for _, name := range []string{"karafiel-api", "karafiel-web", "karafiel-admin", "karafiel-worker", "karafiel-beat"} {
		s := base
		s.Service = byName[name]
		s.Deployment, err = kube.AppsV1().Deployments("karafiel").Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return nil, diagnostic.SnapshotFailure("deployment_read", workloadtargets.ErrOwnership)
		}
		s.Declared, err = declaredTarget(files, "karafiel", name)
		if err != nil {
			return nil, diagnostic.SnapshotFailure("declared_target", err)
		}
		s.Incumbent, err = h.workloadTargetStore().Get(ctx, s.Service.ID, base.Environment.ID)
		if err != nil {
			return nil, diagnostic.SnapshotFailure("incumbent_read", err)
		}
		snapshots = append(snapshots, s)
	}
	return snapshots, nil
}
