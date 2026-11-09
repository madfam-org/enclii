package api

import (
	"context"
	"reflect"

	"github.com/google/uuid"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/checks"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/workloadtargets"
	yamlv3 "gopkg.in/yaml.v3"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
)

func (h *Handler) readKarafielStageRegistrationSnapshot(ctx context.Context, operation uuid.UUID) (workloadtargets.Snapshot, error) {
	s := workloadtargets.Snapshot{}
	if operation == uuid.Nil || h.repos == nil || h.repos.DB() == nil || h.repos.Projects == nil || h.repos.Environments == nil || h.repos.Onboardings == nil || h.opsKubeClient() == nil || h.k8sClient == nil || h.k8sClient.DynamicClient == nil || h.config == nil || h.config.GitHubToken == "" {
		return s, workloadtargets.ErrUnavailable
	}
	var err error
	s.Project, err = h.repos.Projects.GetBySlug("karafiel")
	if err != nil || s.Project == nil {
		return s, workloadtargets.ErrIntent
	}
	s.Environment, err = h.repos.Environments.GetByProjectAndName(s.Project.ID, "staging")
	if err != nil || s.Environment == nil || s.Environment.KubeNamespace != workloadtargets.KarafielStagingNamespace {
		return s, workloadtargets.ErrIntent
	}
	s.Registration, err = h.repos.Onboardings.GetByRepo(ctx, "madfam-org/karafiel")
	if err != nil || s.Registration == nil || s.Registration.ProjectID != s.Project.ID || s.Registration.OnboardStatus != "completed" {
		return s, workloadtargets.ErrIntent
	}
	s.Service = stageServiceCandidate(s.Project.ID, operation)
	var query stageRegistrationQuery = h.repos.DB()
	if h.workloadTargetSession != nil {
		query = h.workloadTargetSession.Tx()
	}
	var conflict bool
	if query.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM services WHERE id=$1 OR (project_id=$2 AND name=$3) OR k8s_namespace=$4)`, s.Service.ID, s.Project.ID, workloadtargets.KarafielStagingServiceName, workloadtargets.KarafielStagingNamespace).Scan(&conflict) != nil {
		return s, workloadtargets.ErrUnavailable
	}
	if conflict {
		return s, workloadtargets.ErrConflict
	}
	if query.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM environments WHERE kube_namespace=$1 AND id<>$2)`, workloadtargets.KarafielStagingNamespace, s.Environment.ID).Scan(&conflict) != nil {
		return s, workloadtargets.ErrUnavailable
	}
	if conflict {
		return s, workloadtargets.ErrConflict
	}
	s.NamespaceIntentExclusive, err = h.workloadTargetStore().NamespaceIntentExclusive(ctx, s.Project.ID, workloadtargets.KarafielStagingNamespace)
	if err != nil || !s.NamespaceIntentExclusive {
		return s, workloadtargets.ErrIntent
	}
	kube := h.opsKubeClient()
	s.Namespace, err = kube.CoreV1().Namespaces().Get(ctx, workloadtargets.KarafielStagingNamespace, metav1.GetOptions{})
	if err != nil {
		return s, workloadtargets.ErrOwnership
	}
	s.Deployment, err = kube.AppsV1().Deployments(workloadtargets.KarafielStagingNamespace).Get(ctx, "karafiel-api", metav1.GetOptions{})
	if err != nil {
		return s, workloadtargets.ErrOwnership
	}
	s.Application, err = h.k8sClient.DynamicClient.Resource(argoApplicationGVR).Namespace("argocd").Get(ctx, "karafiel-staging", metav1.GetOptions{})
	if err != nil {
		return s, workloadtargets.ErrOwnership
	}
	if argoApplicationDeploymentReplicasIgnoredOrUnknown(s.Application.Object) {
		return s, workloadtargets.ErrOwnership
	}
	s.SourceRoot, err = h.k8sClient.DynamicClient.Resource(argoApplicationGVR).Namespace("argocd").Get(ctx, "enclii-infrastructure", metav1.GetOptions{})
	if err != nil {
		return s, workloadtargets.ErrOwnership
	}
	s.ArgoConfig, err = kube.CoreV1().ConfigMaps("argocd").Get(ctx, "argocd-cm", metav1.GetOptions{})
	if err != nil {
		return s, workloadtargets.ErrOwnership
	}
	revision, _, err := unstructured.NestedString(s.Application.Object, "status", "sync", "revision")
	if err != nil || !workloadtargets.ValidRevision(revision) {
		return s, workloadtargets.ErrOwnership
	}
	files, err := h.fetchManifestFiles(ctx, "madfam-org", "karafiel", "infra/k8s/overlays/staging", revision)
	if err != nil {
		return s, workloadtargets.ErrOwnership
	}
	if validateKarafielRegistrationQuarantine(files) != nil {
		return s, workloadtargets.ErrOwnership
	}
	s.Declared, err = declaredTarget(files, workloadtargets.KarafielStagingNamespace, "karafiel-api")
	return s, err
}

// Metadata enrollment requires the actual source to be the supported flat,
// held quarantine tree. The native database adapter separately proves actual
// all-workload/pod termination; this source check is not runtime readiness.
func validateKarafielRegistrationQuarantine(files []checks.ManifestFile) error {
	// The canonical quarantine projection intentionally retains source patches
	// that its flat Kustomization does not reference. They are not applied. Reuse
	// the native target selector's strict path/duplicate/reference validation,
	// then inspect only the exact resources the Kustomization declares.
	if _, err := declaredTarget(files, workloadtargets.KarafielStagingNamespace, "karafiel-api"); err != nil {
		return workloadtargets.ErrOwnership
	}
	byName := map[string]checks.ManifestFile{}
	selected := []checks.ManifestFile{}
	var resources []any
	for _, file := range files {
		byName[file.Path] = file
		if checks.IsKustomizationFile(file.Path) {
			var doc map[string]any
			if yamlv3.Unmarshal([]byte(file.Content), &doc) != nil {
				return workloadtargets.ErrOwnership
			}
			var ok bool
			resources, ok = doc["resources"].([]any)
			if !ok {
				return workloadtargets.ErrOwnership
			}
			selected = append(selected, file)
		}
	}
	for _, raw := range resources {
		name, ok := raw.(string)
		if !ok {
			return workloadtargets.ErrOwnership
		}
		file, exists := byName[name]
		if !exists {
			return workloadtargets.ErrOwnership
		}
		selected = append(selected, file)
	}
	objects, err := resolveAdmissionObjects(selected, workloadtargets.KarafielStagingNamespace)
	if err != nil {
		return workloadtargets.ErrOwnership
	}
	seen := map[string]bool{}
	for _, item := range objects {
		o := item.Object
		kind, _ := o["kind"].(string)
		object := &unstructured.Unstructured{Object: o}
		allowed := map[string]bool{
			"apps/v1/Deployment": true, "v1/Service": true, "v1/ConfigMap": true, "v1/ResourceQuota": true, "v1/LimitRange": true,
			"networking.k8s.io/v1/NetworkPolicy": true, "external-secrets.io/v1beta1/ExternalSecret": true, "external-secrets.io/v1/ExternalSecret": true, "monitoring.coreos.com/v1/ServiceMonitor": true, // pragma: allowlist secret -- public Kubernetes API kind names; not credentials
		}
		if !allowed[object.GetAPIVersion()+"/"+kind] || object.GetNamespace() != workloadtargets.KarafielStagingNamespace || object.GetName() == "" {
			return workloadtargets.ErrOwnership
		}
		switch kind {
		case "Job", "CronJob", "HorizontalPodAutoscaler", "PodDisruptionBudget", "Pod", "StatefulSet", "DaemonSet", "ReplicaSet", "ReplicationController":
			return workloadtargets.ErrOwnership
		case "Deployment":
			u := &unstructured.Unstructured{Object: o}
			name := u.GetName()
			if o["apiVersion"] != "apps/v1" || u.GetNamespace() != workloadtargets.KarafielStagingNamespace || seen[name] ||
				!map[string]bool{"karafiel-api": true, "karafiel-web": true, "karafiel-admin": true, "karafiel-worker": true, "karafiel-beat": true}[name] {
				return workloadtargets.ErrOwnership
			}
			deployment := &appsv1.Deployment{}
			if runtime.DefaultUnstructuredConverter.FromUnstructured(o, deployment) != nil || deployment.Spec.Replicas == nil || *deployment.Spec.Replicas != 0 {
				return workloadtargets.ErrOwnership
			}
			seen[name] = true
		}
	}
	if len(seen) != 5 {
		return workloadtargets.ErrOwnership
	}
	return nil
}

// This proves immutable metadata enrollment only, never namespace adoption,
// runtime readiness, source convergence, database custody or migration success.
// Native consumers must additionally compare the binding's UIDs/source against
// their independently observed live namespace/Deployment/Application evidence.
func (h *Handler) readKarafielHeldStageRegistration(ctx context.Context, serviceID uuid.UUID) (*workloadtargets.Binding, error) {
	if serviceID == uuid.Nil || h.repos == nil || h.repos.DB() == nil || h.repos.Projects == nil || h.repos.Services == nil || h.repos.Environments == nil || h.repos.Onboardings == nil {
		return nil, workloadtargets.ErrUnavailable
	}
	p, err := h.repos.Projects.GetBySlug("karafiel")
	if err != nil || p == nil {
		return nil, workloadtargets.ErrIntent
	}
	s, err := h.repos.Services.GetByID(serviceID)
	if err != nil || workloadtargets.ValidateKarafielStagingService(s, p.ID) != nil {
		return nil, workloadtargets.ErrIntent
	}
	e, err := h.repos.Environments.GetByProjectAndName(p.ID, "staging")
	if err != nil || e == nil || e.KubeNamespace != workloadtargets.KarafielStagingNamespace {
		return nil, workloadtargets.ErrIntent
	}
	r, err := h.repos.Onboardings.GetByRepo(ctx, "madfam-org/karafiel")
	if err != nil || r == nil || r.ProjectID != p.ID || r.OnboardStatus != "completed" {
		return nil, workloadtargets.ErrIntent
	}
	store := h.workloadTargetStore()
	binding, err := store.Get(ctx, serviceID, e.ID)
	if err != nil || binding == nil || binding.Identity.SourceAuthority != workloadtargets.KarafielStagingHeldAuthority || binding.Identity.ProjectID != p.ID || binding.Identity.RegistrationID != r.ID {
		return nil, workloadtargets.ErrIntent
	}
	receipt, err := store.Receipt(ctx, binding.ID)
	if err != nil || receipt == nil || !reflect.DeepEqual(receipt.Binding, *binding) || serviceID != workloadtargets.KarafielStagingServiceID(p.ID, binding.ID) {
		return nil, workloadtargets.ErrIntent
	}
	request := workloadtargets.Request{Project: "karafiel", ServiceID: serviceID, Environment: "staging", Namespace: workloadtargets.KarafielStagingNamespace, Deployment: "karafiel-api", ExpectedPlan: binding.PlanSHA, Reason: receipt.Reason, IdempotencyID: binding.ID}
	sha, err := workloadtargets.RequestHash(request, receipt.ActorID)
	if err != nil || sha != receipt.RequestSHA {
		return nil, workloadtargets.ErrIntent
	}
	return binding, nil
}
