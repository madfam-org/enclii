package api

import (
	"context"
	"strings"

	"github.com/madfam-org/enclii/apps/switchyard-api/internal/workloadtargets"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
)

// Only fixed metadata/source reads occur here. No fallback list selector,
// namespace ownership stamp, secret read, preflight apply or status write.
func (h *Handler) readWorkloadTargetSnapshot(ctx context.Context, r workloadtargets.Request) (workloadtargets.Snapshot, error) {
	s := workloadtargets.Snapshot{}
	if h.repos == nil || h.repos.Services == nil || h.repos.Projects == nil || h.repos.Environments == nil || h.repos.Onboardings == nil ||
		h.repos.DB() == nil || h.opsKubeClient() == nil || h.k8sClient == nil || h.k8sClient.DynamicClient == nil || h.config == nil || h.config.GitHubToken == "" {
		return s, workloadtargets.ErrUnavailable
	}
	var err error
	s.Project, err = h.repos.Projects.GetBySlug(r.Project)
	if err != nil || s.Project == nil {
		return s, workloadtargets.ErrIntent
	}
	s.Service, err = h.repos.Services.GetByID(r.ServiceID)
	if err != nil || s.Service == nil || s.Service.ProjectID != s.Project.ID {
		return s, workloadtargets.ErrIntent
	}
	s.Environment, err = h.repos.Environments.GetByProjectAndName(s.Project.ID, r.Environment)
	if err != nil || s.Environment == nil || s.Environment.KubeNamespace != r.Namespace {
		return s, workloadtargets.ErrIntent
	}
	_, repo, err := workloadtargets.CanonicalRepo(s.Service.GitRepo)
	if err != nil {
		return s, err
	}
	s.Registration, err = h.repos.Onboardings.GetByRepo(ctx, repo)
	if err != nil || s.Registration == nil || s.Registration.ProjectID != s.Project.ID || s.Registration.ArgocdAppName == nil {
		return s, workloadtargets.ErrIntent
	}
	s.Incumbent, err = h.workloadTargetStore().Get(ctx, s.Service.ID, s.Environment.ID)
	if err != nil {
		return s, err
	}
	s.NamespaceIntentExclusive, err = h.workloadTargetStore().NamespaceIntentExclusive(ctx, s.Project.ID, r.Namespace)
	if err != nil || !s.NamespaceIntentExclusive {
		return s, workloadtargets.ErrIntent
	}
	revision, err := readTargetNativeObjects(ctx, h.opsKubeClient(), h.k8sClient.DynamicClient, *s.Registration.ArgocdAppName, r, &s)
	if err != nil {
		return s, err
	}
	path, ok := s.Registration.ConfigSnapshot["manifest_path"].(string)
	if !ok || !workloadtargets.ValidPath(path) {
		return s, workloadtargets.ErrIntent
	}
	parts := strings.Split(repo, "/")
	manifests, err := h.fetchManifestFiles(ctx, parts[0], parts[1], path, revision)
	if err != nil {
		return s, workloadtargets.RefuseOwnershipCause(workloadtargets.ReasonManifestFetch, err)
	}
	s.Declared, err = declaredTarget(manifests, r.Namespace, r.Deployment)
	return s, err
}

// readTargetNativeObjects performs the four fixed native reads (Namespace,
// Deployment, the registered Argo Application, argocd/argocd-cm) and returns
// the Application's synced revision. Each failure names its read; the API
// server's error is kept for the server log only.
func readTargetNativeObjects(ctx context.Context, kube kubernetes.Interface, dyn dynamic.Interface, application string, r workloadtargets.Request, s *workloadtargets.Snapshot) (string, error) {
	var err error
	if s.Namespace, err = kube.CoreV1().Namespaces().Get(ctx, r.Namespace, metav1.GetOptions{}); err != nil {
		return "", workloadtargets.RefuseOwnershipCause(workloadtargets.ReasonNamespaceRead, err)
	}
	if s.Deployment, err = kube.AppsV1().Deployments(r.Namespace).Get(ctx, r.Deployment, metav1.GetOptions{}); err != nil {
		return "", workloadtargets.RefuseOwnershipCause(workloadtargets.ReasonDeploymentRead, err)
	}
	if s.Application, err = dyn.Resource(argoApplicationGVR).Namespace("argocd").Get(ctx, application, metav1.GetOptions{}); err != nil {
		return "", workloadtargets.RefuseOwnershipCause(workloadtargets.ReasonApplicationRead, err)
	}
	if s.ArgoConfig, err = kube.CoreV1().ConfigMaps("argocd").Get(ctx, "argocd-cm", metav1.GetOptions{}); err != nil {
		return "", workloadtargets.RefuseOwnershipCause(workloadtargets.ReasonArgoConfigRead, err)
	}
	revision, _, err := unstructured.NestedString(s.Application.Object, "status", "sync", "revision")
	if err != nil || !workloadtargets.ValidRevision(revision) {
		return "", workloadtargets.RefuseOwnership(workloadtargets.ReasonSyncRevision)
	}
	return revision, nil
}
