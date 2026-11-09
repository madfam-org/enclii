package api

import (
	"bytes"
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"gopkg.in/yaml.v3"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/addons"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/workloadtargets"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

const karafielStageRepository = "madfam-org/karafiel"

var errKarafielStage = fmt.Errorf("Karafiel staging ownership, quarantine or native custody could not be qualified")

type karafielStagePolicyObservation struct {
	Name            string                         `json:"name"`
	UID             string                         `json:"uid"`
	ResourceVersion string                         `json:"resource_version"`
	Spec            networkingv1.NetworkPolicySpec `json:"spec"`
}

type karafielStageNativeBindingPolicy struct {
	karafielStagePolicyObservation
	BindingID         uuid.UUID `json:"binding_id"`
	AddonID           uuid.UUID `json:"addon_id"`
	ServiceID         uuid.UUID `json:"service_id"`
	ClusterUID        string    `json:"cluster_uid"`
	AppNamespaceUID   string    `json:"app_namespace_uid"`
	AddonNamespaceUID string    `json:"addon_namespace_uid"`
}

type karafielStageQuarantineObservation struct {
	Schema                string                             `json:"schema"`
	Phase                 string                             `json:"phase"`
	ObservedAt            string                             `json:"observed_at"`
	ProjectID             uuid.UUID                          `json:"project_id"`
	EnvironmentID         uuid.UUID                          `json:"environment_id"`
	ServiceID             uuid.UUID                          `json:"service_id"`
	AppNamespaceUID       string                             `json:"app_namespace_uid"`
	SourceRevision        string                             `json:"source_revision"`
	Policies              []karafielStagePolicyObservation   `json:"policies"`
	PolicySetSHA256       string                             `json:"policy_set_sha256"`
	ObservationSHA256     string                             `json:"observation_sha256"`
	NativeBindingPolicies []karafielStageNativeBindingPolicy `json:"native_binding_policies"`
}

type karafielStageSource struct {
	Project        *types.Project
	Environment    *types.Environment
	Service        *types.Service
	RegistrationID uuid.UUID
	Namespace      *corev1.Namespace
	Deployment     *appsv1.Deployment
	Observation    *karafielStageQuarantineObservation
}

func stageJSONHash(value any) string {
	data, _ := json.Marshal(value)
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var canonical interface{}
	if decoder.Decode(&canonical) != nil {
		return ""
	}
	normalized, _ := json.Marshal(canonical)
	sum := sha256.Sum256(normalized)
	return hex.EncodeToString(sum[:])
}

// Read-only internal kernel. Release-principal and human routes authorize
// separately; this function never changes an auth context or accepts a project,
// namespace, resource selector, approval Boolean, credential or policy body.
func (h *Handler) readKarafielStageQuarantine(ctx context.Context, serviceID uuid.UUID) (*karafielStageQuarantineObservation, error) {
	source, err := h.readKarafielStageSource(ctx, serviceID, false, nil)
	if err != nil {
		if h.repos == nil || h.repos.DatabaseAddons == nil {
			return nil, errKarafielStage
		}
		rows, e := h.repos.DatabaseAddons.ListByProject(ctx, mustStageProject(h))
		if e != nil {
			return nil, errKarafielStage
		}
		for _, addon := range rows {
			if addon != nil && addon.Name == addons.KarafielStagingAddon {
				source, _, _, err = h.karafielOwnedAddon(ctx, serviceID, addon.ID)
				break
			}
		}
		if err != nil || source == nil || source.Observation.Phase != "QUARANTINE_READY" {
			return nil, errKarafielStage
		}
	}
	return source.Observation, nil
}

func (h *Handler) readKarafielStageSource(ctx context.Context, serviceID uuid.UUID, allowLegacyOwnership bool, boundPolicies map[string]networkingv1.NetworkPolicySpec) (*karafielStageSource, error) {
	return h.readKarafielStageSourceMode(ctx, serviceID, allowLegacyOwnership, boundPolicies, false)
}

func (h *Handler) readKarafielStageSourceMode(ctx context.Context, serviceID uuid.UUID, allowLegacyOwnership bool, boundPolicies map[string]networkingv1.NetworkPolicySpec, allowServing bool) (*karafielStageSource, error) {
	if serviceID == uuid.Nil || h.repos == nil || h.repos.Projects == nil || h.repos.Environments == nil || h.repos.Services == nil || h.repos.Onboardings == nil || h.k8sClient == nil || h.opsKubeClient() == nil || h.k8sClient.DynamicClient == nil {
		return nil, errKarafielStage
	}
	p, err := h.repos.Projects.GetBySlug("karafiel")
	if err != nil || p == nil || p.ID == uuid.Nil {
		return nil, errKarafielStage
	}
	e, err := h.repos.Environments.GetByProjectAndName(p.ID, "staging")
	if err != nil || e == nil || e.ID == uuid.Nil || e.ProjectID != p.ID || e.KubeNamespace != addons.KarafielStagingNamespace {
		return nil, errKarafielStage
	}
	r, err := h.repos.Onboardings.GetByRepo(ctx, karafielStageRepository)
	if err != nil || r == nil || r.ID == uuid.Nil || r.ProjectID != p.ID || r.OnboardStatus != "completed" {
		return nil, errKarafielStage
	}
	s, err := h.repos.Services.GetByID(serviceID)
	if err != nil || s == nil || s.ProjectID != p.ID || s.Name != addons.KarafielStagingService || s.AutoDeploy || s.AutoDeployBranch != "release/karafiel-staging" || s.AutoDeployEnv != "staging" || s.Type != types.ServiceTypeWeb || s.K8sNamespace == nil || *s.K8sNamespace != e.KubeNamespace || s.AppPath != "apps/api" || strings.TrimSuffix(s.GitRepo, ".git") != "https://github.com/"+karafielStageRepository {
		return nil, errKarafielStage
	}
	enrollment, err := h.readKarafielHeldStageRegistration(ctx, serviceID)
	if err != nil || enrollment == nil {
		return nil, errKarafielStage
	}
	n, err := h.opsKubeClient().CoreV1().Namespaces().Get(ctx, e.KubeNamespace, metav1.GetOptions{})
	if err != nil || n.UID == "" || n.ResourceVersion == "" || n.DeletionTimestamp != nil {
		return nil, errKarafielStage
	}
	pendingNamespace, err := karafielNamespaceIsolationState(n, p.ID, allowLegacyOwnership, false)
	if err != nil {
		return nil, errKarafielStage
	}
	app, err := h.k8sClient.DynamicClient.Resource(argoApplicationGVR).Namespace("argocd").Get(ctx, "karafiel-staging", metav1.GetOptions{})
	if err != nil || app.GetUID() == "" || app.GetDeletionTimestamp() != nil {
		return nil, errKarafielStage
	}
	if validateKarafielSyncApplication(app, karafielSyncRequest{UID: string(app.GetUID()), Version: app.GetResourceVersion()}) != nil {
		return nil, errKarafielStage
	}
	field := func(parts ...string) string { v, _, _ := unstructured.NestedString(app.Object, parts...); return v }
	if field("spec", "source", "repoURL") != "https://github.com/madfam-org/karafiel.git" || field("spec", "source", "targetRevision") != "release/karafiel-staging" || field("spec", "source", "path") != "infra/k8s/overlays/staging" || field("spec", "destination", "namespace") != e.KubeNamespace || field("status", "sync", "status") != "Synced" || !bootstrapSHA.MatchString(field("status", "sync", "revision")) {
		return nil, errKarafielStage
	}
	if field("spec", "project") != "karafiel-staging" || field("spec", "destination", "server") != "https://kubernetes.default.svc" || argoApplicationDeploymentReplicasIgnoredOrUnknown(app.Object) {
		return nil, errKarafielStage
	}
	stageProject, err := h.readKarafielStageProject(ctx)
	if err != nil {
		return nil, errKarafielStage
	}
	automatic, found, _ := unstructured.NestedMap(app.Object, "spec", "syncPolicy", "automated")
	if found {
		enabled, ok := automatic["enabled"].(bool)
		if !ok || enabled {
			return nil, errKarafielStage
		}
	}
	for _, owner := range app.GetOwnerReferences() {
		if owner.Controller != nil && *owner.Controller {
			return nil, errKarafielStage
		}
	}
	root, err := h.k8sClient.DynamicClient.Resource(argoApplicationGVR).Namespace("argocd").Get(ctx, "enclii-infrastructure", metav1.GetOptions{})
	if err != nil || root.GetUID() == "" || string(root.GetUID()) != enrollment.Identity.SourceRootUID {
		return nil, errKarafielStage
	}
	enrolled := enrollment.Identity
	if enrolled.ProjectID != p.ID || enrolled.ServiceID != serviceID || enrolled.EnvironmentID != e.ID || enrolled.RegistrationID != r.ID || enrolled.Namespace != n.Name || enrolled.NamespaceUID != string(n.UID) || enrolled.Application != "karafiel-staging" || enrolled.ApplicationUID != string(app.GetUID()) || enrolled.Repo != "https://github.com/madfam-org/karafiel" || enrolled.Path != "infra/k8s/overlays/staging" || enrolled.Ref != "release/karafiel-staging" {
		return nil, errKarafielStage
	}
	argoConfig, err := h.opsKubeClient().CoreV1().ConfigMaps("argocd").Get(ctx, "argocd-cm", metav1.GetOptions{})
	if err != nil || string(argoConfig.UID) != enrolled.ArgoConfigUID {
		return nil, errKarafielStage
	}
	_, detached, detachErr := karafielOrphanStageNamespace(n, argoConfig)
	if detachErr != nil || !detached || !karafielPruneProtected(n.Annotations["argocd.argoproj.io/sync-options"]) {
		return nil, errKarafielStage
	}
	if !karafielArgoConfigMatchesEnrollment(argoConfig, enrolled) || workloadtargets.ValidateKarafielStagingRootAuthority(workloadtargets.Snapshot{Application: app, SourceRoot: root, ArgoConfig: argoConfig}) != nil {
		return nil, errKarafielStage
	}
	if _, found, e := unstructured.NestedFieldNoCopy(app.Object, "spec", "sources"); e != nil || found {
		return nil, errKarafielStage
	}
	for _, key := range []string{"repoURL", "path", "targetRevision"} {
		if field("status", "sync", "comparedTo", "source", key) != field("spec", "source", key) {
			return nil, errKarafielStage
		}
	}

	workloads, err := h.opsKubeClient().AppsV1().Deployments(e.KubeNamespace).List(ctx, metav1.ListOptions{})
	if err != nil || workloads.Continue != "" || len(workloads.Items) != 5 {
		return nil, errKarafielStage
	}
	sandbox, err := h.opsKubeClient().CoreV1().ConfigMaps(e.KubeNamespace).Get(ctx, "karafiel-cfdi-config", metav1.GetOptions{})
	descriptor, valid := karafielStageDescriptor()
	if err != nil || !valid || sandbox.UID == "" || sandbox.ResourceVersion == "" || sandbox.DeletionTimestamp != nil || len(sandbox.BinaryData) != 0 || !reflect.DeepEqual(sandbox.Data, descriptor.Config) {
		return nil, errKarafielStage
	}
	var api *appsv1.Deployment
	byName := map[string]*appsv1.Deployment{}
	serving := false
	for i := range workloads.Items {
		d := &workloads.Items[i]
		role := strings.TrimPrefix(d.Name, "karafiel-")
		if !map[string]bool{"api": true, "web": true, "admin": true, "worker": true, "beat": true}[role] || byName[d.Name] != nil || d.UID == "" || d.Spec.Replicas == nil || len(d.Spec.Template.Spec.Containers) != 1 || d.DeletionTimestamp != nil {
			return nil, errKarafielStage
		}
		byName[d.Name] = d
		if role == "api" {
			api = d
			serving = *d.Spec.Replicas == 1
		}
	}
	if api == nil || string(api.UID) != enrolled.DeploymentUID || enrolled.Deployment != "karafiel-api" || api.Spec.Template.Labels["enclii.dev/service"] != addons.KarafielStagingService {
		return nil, errKarafielStage
	}
	if !karafielDeploymentTrackingMatches(api, enrolled) {
		return nil, errKarafielStage
	}
	trackedAPI := 0
	resources, _, resourceErr := unstructured.NestedSlice(app.Object, "status", "resources")
	if resourceErr != nil {
		return nil, errKarafielStage
	}
	for _, raw := range resources {
		r, ok := raw.(map[string]interface{})
		if ok && r["group"] == "apps" && r["kind"] == "Deployment" && r["namespace"] == n.Name && r["name"] == "karafiel-api" {
			if r["status"] != "Synced" {
				return nil, errKarafielStage
			}
			trackedAPI++
		}
	}
	if trackedAPI != 1 {
		return nil, errKarafielStage
	}
	if serving && (pendingNamespace || !allowServing) {
		return nil, errKarafielStage
	}
	for name, d := range byName {
		desired := int32(0)
		if serving && name != "karafiel-worker" && name != "karafiel-beat" {
			desired = 1
		}
		if *d.Spec.Replicas != desired || d.Status.ObservedGeneration != d.Generation || d.Status.Replicas != desired || d.Status.ReadyReplicas != desired || d.Status.UpdatedReplicas != desired {
			return nil, errKarafielStage
		}
		if name == "karafiel-api" || name == "karafiel-worker" || name == "karafiel-beat" {
			if !karafielStageBackendRuntimeClosed(d) {
				return nil, errKarafielStage
			}
			values := map[string]string{}
			for _, entry := range d.Spec.Template.Spec.Containers[0].Env {
				if entry.ValueFrom == nil {
					values[entry.Name] = entry.Value
				}
			}
			for _, flag := range []string{"BANK_ALLOCATION_LEDGER_ENABLED", "DHANAM_SOURCE_CURRENTNESS_ENABLED", "FEATURE_CFDI_AUTO_ISSUE", "FEATURE_GRANT_COMPLIANCE"} {
				if values[flag] != "false" {
					return nil, errKarafielStage
				}
			}
			if d.Spec.Template.Spec.Containers[0].Image != api.Spec.Template.Spec.Containers[0].Image {
				return nil, errKarafielStage
			}
		}
	}
	pods, err := h.opsKubeClient().CoreV1().Pods(e.KubeNamespace).List(ctx, metav1.ListOptions{})
	if err != nil || pods.Continue != "" {
		return nil, errKarafielStage
	}
	live := map[string]int{}
	for _, pod := range pods.Items {
		if pod.Status.Phase == corev1.PodSucceeded || pod.Status.Phase == corev1.PodFailed {
			continue
		}
		if !serving || pod.Status.Phase != corev1.PodRunning || pod.DeletionTimestamp != nil || pod.UID == "" || len(pod.OwnerReferences) != 1 || len(pod.Spec.Containers) != 1 || len(pod.Status.ContainerStatuses) != 1 {
			return nil, errKarafielStage
		}
		owner := pod.OwnerReferences[0]
		if owner.Kind != "ReplicaSet" || owner.APIVersion != "apps/v1" || owner.Controller == nil || !*owner.Controller {
			return nil, errKarafielStage
		}
		rs, err := h.opsKubeClient().AppsV1().ReplicaSets(e.KubeNamespace).Get(ctx, owner.Name, metav1.GetOptions{})
		if err != nil || rs.UID != owner.UID || len(rs.OwnerReferences) != 1 {
			return nil, errKarafielStage
		}
		d := byName[rs.OwnerReferences[0].Name]
		if d == nil || d.UID != rs.OwnerReferences[0].UID || d.Name == "karafiel-worker" || d.Name == "karafiel-beat" {
			return nil, errKarafielStage
		}
		status := pod.Status.ContainerStatuses[0]
		image := d.Spec.Template.Spec.Containers[0].Image
		digest := strings.Split(image, "@sha256:")
		if len(digest) != 2 || !bootstrapDigest.MatchString("sha256:"+digest[1]) || pod.Spec.Containers[0].Image != image || !status.Ready || status.RestartCount != 0 || status.State.Running == nil || status.State.Running.StartedAt.IsZero() || (status.ImageID != image && status.ImageID != "docker-pullable://"+image && status.ImageID != "containerd://sha256:"+digest[1]) {
			return nil, errKarafielStage
		}
		live[d.Name]++
	}
	if serving && (live["karafiel-api"] != 1 || live["karafiel-web"] != 1 || live["karafiel-admin"] != 1) {
		return nil, errKarafielStage
	}
	jobs, err := h.opsKubeClient().BatchV1().Jobs(e.KubeNamespace).List(ctx, metav1.ListOptions{})
	if err != nil || jobs.Continue != "" {
		return nil, errKarafielStage
	}
	for _, job := range jobs.Items {
		terminal := false
		for _, condition := range job.Status.Conditions {
			if (condition.Type == batchv1.JobComplete || condition.Type == batchv1.JobFailed) && condition.Status == corev1.ConditionTrue {
				terminal = true
			}
		}
		if job.Status.Active != 0 || !terminal {
			return nil, errKarafielStage
		}
	}
	exceptions, exceptionErr := h.k8sClient.DynamicClient.Resource(karafielExceptionGVR).Namespace(e.KubeNamespace).List(ctx, metav1.ListOptions{})
	if exceptionErr != nil || exceptions.GetContinue() != "" || len(exceptions.Items) != 0 {
		return nil, errKarafielStage
	}
	crons, err := h.opsKubeClient().BatchV1().CronJobs(e.KubeNamespace).List(ctx, metav1.ListOptions{})
	if err != nil || crons.Continue != "" || len(crons.Items) != 0 {
		return nil, errKarafielStage
	}
	hpas, err := h.opsKubeClient().AutoscalingV2().HorizontalPodAutoscalers(e.KubeNamespace).List(ctx, metav1.ListOptions{})
	if err != nil || hpas.Continue != "" || (!serving && len(hpas.Items) != 0) || (serving && len(hpas.Items) != 1) {
		return nil, errKarafielStage
	}
	if serving {
		hpa := hpas.Items[0]
		if hpa.Name != "karafiel-api-hpa" || hpa.UID == "" || hpa.Spec.MinReplicas == nil || *hpa.Spec.MinReplicas != 1 || hpa.Spec.MaxReplicas != 1 || hpa.Spec.ScaleTargetRef.Kind != "Deployment" || hpa.Spec.ScaleTargetRef.APIVersion != "apps/v1" || hpa.Spec.ScaleTargetRef.Name != "karafiel-api" {
			return nil, errKarafielStage
		}
	}
	for _, kind := range []string{"statefulsets", "daemonsets"} {
		objects, err := h.k8sClient.DynamicClient.Resource(schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: kind}).Namespace(e.KubeNamespace).List(ctx, metav1.ListOptions{})
		if err != nil || objects.GetContinue() != "" || len(objects.Items) != 0 {
			return nil, errKarafielStage
		}
	}
	policies, err := h.opsKubeClient().NetworkingV1().NetworkPolicies(e.KubeNamespace).List(ctx, metav1.ListOptions{})
	if err != nil || policies.Continue != "" {
		return nil, errKarafielStage
	}
	observation := &karafielStageQuarantineObservation{Schema: "enclii-karafiel-staging-quarantine-v1", Phase: "QUARANTINE_READY", ObservedAt: time.Now().UTC().Format(time.RFC3339Nano), ProjectID: p.ID, EnvironmentID: e.ID, ServiceID: serviceID, AppNamespaceUID: string(n.UID), SourceRevision: field("status", "sync", "revision"), Policies: []karafielStagePolicyObservation{}, NativeBindingPolicies: []karafielStageNativeBindingPolicy{}}
	expected := map[string]bool{"staging-default-deny-all": true, "staging-dns-egress": true, "staging-cloudflared-ingress": true, "staging-frontend-api-egress": true, "staging-api-frontend-ingress": true, "staging-janua-egress": true, "staging-monitoring-ingress": true}
	for _, policy := range policies.Items {
		if policy.UID == "" || policy.ResourceVersion == "" || policy.DeletionTimestamp != nil {
			return nil, errKarafielStage
		}
		if !expected[policy.Name] {
			wanted, ok := boundPolicies[policy.Name]
			if !ok || !reflect.DeepEqual(wanted, policy.Spec) {
				return nil, errKarafielStage
			}
			observation.Policies = append(observation.Policies, karafielStagePolicyObservation{policy.Name, string(policy.UID), policy.ResourceVersion, policy.Spec})
			continue
		}
		if !karafielClosedStagePolicy(policy) {
			return nil, errKarafielStage
		}
		delete(expected, policy.Name)
		observation.Policies = append(observation.Policies, karafielStagePolicyObservation{policy.Name, string(policy.UID), policy.ResourceVersion, policy.Spec})
	}
	if len(expected) != 0 {
		return nil, errKarafielStage
	}
	sort.Slice(observation.Policies, func(i, j int) bool { return observation.Policies[i].Name < observation.Policies[j].Name })
	projection := []struct {
		Name string                         `json:"name"`
		Spec networkingv1.NetworkPolicySpec `json:"spec"`
	}{}
	for _, policy := range observation.Policies {
		projection = append(projection, struct {
			Name string                         `json:"name"`
			Spec networkingv1.NetworkPolicySpec `json:"spec"`
		}{policy.Name, policy.Spec})
	}
	observation.PolicySetSHA256 = stageJSONHash(projection)
	observation.ObservationSHA256 = stageJSONHash(struct {
		NamespaceUID, NamespaceRV, ApplicationUID, ApplicationRV, Source, PolicyHash string
		Workloads                                                                    []appsv1.Deployment
		Pods                                                                         []corev1.Pod
		Sandbox                                                                      *corev1.ConfigMap
		ProjectUID, ProjectRV                                                        string
	}{string(n.UID), n.ResourceVersion, string(app.GetUID()), app.GetResourceVersion(), observation.SourceRevision, observation.PolicySetSHA256, workloads.Items, pods.Items, sandbox, string(stageProject.GetUID()), stageProject.GetResourceVersion()})
	if serving {
		observation.Phase = "SERVING_COHORT_READY"
	}
	if pendingNamespace {
		observation.Phase = "OWNERSHIP_ADOPTION_REQUIRED"
	}
	return &karafielStageSource{p, e, s, r.ID, n, api, observation}, nil
}

//go:embed karafiel_staging_policy_contract.yaml
var karafielStagePolicyContract []byte

func karafielClosedStagePolicy(policy networkingv1.NetworkPolicy) bool {
	decoder := yaml.NewDecoder(bytes.NewReader(karafielStagePolicyContract))
	for {
		var object struct {
			Metadata struct {
				Name string `yaml:"name"`
			} `yaml:"metadata"`
			Spec map[string]interface{} `yaml:"spec"`
		}
		if decoder.Decode(&object) != nil {
			return false
		}
		if object.Metadata.Name == policy.Name {
			payload, err := json.Marshal(object.Spec)
			if err != nil {
				return false
			}
			var expected networkingv1.NetworkPolicySpec
			if json.Unmarshal(payload, &expected) != nil {
				return false
			}
			return reflect.DeepEqual(expected, policy.Spec)
		}
	}
}

func karafielArgoConfigMatchesEnrollment(config *corev1.ConfigMap, identity workloadtargets.Identity) bool {
	if config == nil || config.UID == "" || string(config.UID) != identity.ArgoConfigUID || config.ResourceVersion == "" || config.DeletionTimestamp != nil || config.Data["application.resourceTrackingMethod"] != identity.TrackingMethod || config.Data["installationID"] != identity.InstallationID {
		return false
	}
	switch identity.TrackingMethod {
	case "annotation", "annotation+label":
		return identity.InstanceLabelKey == ""
	case "label":
		key := config.Data["application.instanceLabelKey"]
		if key == "" {
			key = "app.kubernetes.io/instance"
		}
		return identity.InstallationID == "" && key == identity.InstanceLabelKey
	default:
		return false
	}
}
func karafielDeploymentTrackingMatches(d *appsv1.Deployment, identity workloadtargets.Identity) bool {
	if d == nil {
		return false
	}
	switch identity.TrackingMethod {
	case "annotation", "annotation+label":
		return d.Annotations["argocd.argoproj.io/tracking-id"] == "karafiel-staging:apps/Deployment:"+addons.KarafielStagingNamespace+"/karafiel-api" && d.Annotations["argocd.argoproj.io/installation-id"] == identity.InstallationID
	case "label":
		return identity.InstanceLabelKey != "" && d.Labels[identity.InstanceLabelKey] == "karafiel-staging"
	default:
		return false
	}
}
