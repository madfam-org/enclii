package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/madfam-org/enclii/packages/sdk-go/pkg/types"
	corev1 "k8s.io/api/core/v1"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

type januaStageAdmissionReceipt = types.JanuaStagingAdmissionReceipt

func registerJanuaStagingAdmissionRoutes(platform *gin.RouterGroup, h *Handler) {
	platform.POST("/projects/:slug/environments/staging/addon-admission", h.RequirePlatformOperator(), h.JanuaStagingAddonAdmission)
}

// JanuaStagingAddonAdmission has one operation: server-side DryRunAll against
// a closed profile. It is never an apply, resource writer or credential reader.
func (h *Handler) JanuaStagingAddonAdmission(c *gin.Context) {
	h.januaStagingAddonAdmission(c, verifyJanuaStageDatabaseProducer)
}

func (h *Handler) januaStagingAddonAdmission(c *gin.Context, verify func(context.Context) error) {
	query := c.Request.URL.Query()
	for key, values := range query {
		if (key != "plan" && key != "service_id") || len(values) != 1 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "closed admission accepts only plan and a persisted staging service_id"})
			return
		}
	}
	serviceID, err := uuid.Parse(query.Get("service_id"))
	if c.Param("slug") != "janua" || query.Get("plan") == "" || err != nil || serviceID == uuid.Nil || c.Request.ContentLength != 0 || len(c.Request.TransferEncoding) != 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "explicit Janua staging plan and service_id required; request bodies and apply selectors are refused"})
		return
	}
	if h.repos == nil || h.repos.Projects == nil || h.repos.Environments == nil || h.repos.Onboardings == nil || h.repos.Services == nil || h.repos.ManagedDBPlans == nil || h.repos.DatabaseAddons == nil || h.k8sClient == nil || h.k8sClient.Kube() == nil || h.k8sClient.DynamicClient == nil || verify == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "closed staging admission adapter unavailable"})
		return
	}
	project, err := h.repos.Projects.GetBySlug("janua")
	if err != nil || project == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "project unavailable"})
		return
	}
	if !h.enforceUserProjectAccess(c, project.ID) {
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Minute)
	defer cancel()
	before, err := h.readJanuaStageAdmissionSource(ctx, serviceID, query.Get("plan"))
	if err != nil || before.ProjectID != project.ID {
		c.JSON(http.StatusConflict, gin.H{"error": errJanuaStageAdmission.Error()})
		return
	}
	objects, err := januaStageAdmissionManifests(before.ProjectID, before.Service, before.Plan, before.Peers, before.OperatorSelector)
	if err != nil {
		c.JSON(http.StatusConflict, gin.H{"error": errJanuaStageAdmission.Error()})
		return
	}
	if verify(ctx) != nil {
		c.JSON(http.StatusConflict, gin.H{"error": "fixed PostgreSQL producer signature could not be verified; no resources were persisted"})
		return
	}
	count, err := h.dryAdmitJanuaStage(ctx, objects)
	if err != nil {
		c.JSON(http.StatusConflict, gin.H{"error": "closed server-side dry admission refused or receipt incomplete; no resources were persisted"})
		return
	}
	after, err := h.readJanuaStageAdmissionSource(ctx, serviceID, query.Get("plan"))
	if err != nil || januaStageSourceFingerprint(before) != januaStageSourceFingerprint(after) {
		c.JSON(http.StatusConflict, gin.H{"error": "staging authority metadata changed during dry admission; discard this observation"})
		return
	}
	dry, apply := true, false
	receipt := januaStageAdmissionReceipt{
		Classification: types.JanuaStagingAdmissionClassification, DryRunOnly: &dry, ApplySupported: &apply, Profile: types.JanuaStagingAdmissionProfile, Image: januaStagePostgresImage,
		ProjectSlug: "janua", EnvironmentName: "staging", CatalogPlan: before.Plan.Code,
		ProjectID: before.ProjectID, EnvironmentID: before.EnvironmentID, ServiceID: serviceID, ObjectsAdmitted: count, APIPeers: len(before.Peers), Fingerprint: januaStageSourceFingerprint(before), ObservedAt: time.Now().UTC().Format(time.RFC3339Nano),
		Gates: types.JanuaStagingAdmissionGates(),
	}
	if types.ValidateJanuaStagingAdmissionReceipt(receipt, serviceID, query.Get("plan")) != nil {
		c.JSON(http.StatusConflict, gin.H{"error": "closed admission receipt could not be qualified"})
		return
	}
	c.JSON(http.StatusOK, receipt)
}

func (h *Handler) dryAdmitJanuaStage(ctx context.Context, objects januaStageAdmissionObjects) (int, error) {
	options := metav1.CreateOptions{DryRun: []string{metav1.DryRunAll}, FieldValidation: metav1.FieldValidationStrict, FieldManager: "janua-staging-admission"}
	if objects.Cluster == nil || objects.Pod == nil || len(objects.Policies) != 2 {
		return 0, errJanuaStageAdmission
	}
	cluster, err := h.k8sClient.DynamicClient.Resource(januaStageClusterGVR).Namespace(objects.Cluster.GetNamespace()).Create(ctx, objects.Cluster, options)
	if err != nil || cluster == nil || cluster.GetName() != objects.Cluster.GetName() || cluster.GetNamespace() != objects.Cluster.GetNamespace() || !januaStageAdmissionAuthorityClosed(objects.Cluster, cluster) {
		return 0, errJanuaStageAdmission
	}
	count := 1
	for _, policy := range objects.Policies {
		result, err := h.k8sClient.Kube().NetworkingV1().NetworkPolicies(policy.Namespace).Create(ctx, policy, options)
		if err != nil || result == nil || result.Name != policy.Name || result.Namespace != policy.Namespace || !januaStageAdmissionObjectUnchanged(policy.Spec, result.Spec) {
			return count, errJanuaStageAdmission
		}
		count++
	}
	pod, err := h.k8sClient.Kube().CoreV1().Pods(objects.Pod.Namespace).Create(ctx, objects.Pod, options)
	if err != nil || pod == nil || pod.Name != objects.Pod.Name || pod.Namespace != objects.Pod.Namespace || !januaStageAdmissionPodSafe(objects.Pod, pod) {
		return count, errJanuaStageAdmission
	}
	return count + 1, nil
}

func januaStageAdmissionAuthorityClosed(expected, actual *unstructured.Unstructured) bool {
	if !januaStageAdmissionContains(expected.Object["spec"], actual.Object["spec"]) {
		return false
	}
	bootstrap, ok, _ := unstructured.NestedMap(actual.Object, "spec", "bootstrap")
	if !ok || len(bootstrap) != 1 {
		return false
	} // no added recovery/clone source
	initdb, ok := bootstrap["initdb"].(map[string]any)
	if !ok {
		return false
	}
	for key, value := range initdb {
		if key == "postInitApplicationSQL" {
			continue
		} // exact static array already compared
		if (strings.Contains(strings.ToLower(key), "sql") || key == "secret" || key == "import") && !januaStageAdmissionEmpty(value) {
			return false
		}
	}
	roles, _, _ := unstructured.NestedSlice(actual.Object, "spec", "managed", "roles")
	if len(roles) != 1 {
		return false
	}
	role, ok := roles[0].(map[string]any)
	if !ok || !januaStageAdmissionEmpty(role["inRoles"]) {
		return false
	} // NOINHERIT alone does not forbid SET ROLE
	for _, key := range []string{"externalClusters", "replica", "superuserSecret", "serviceAccountTemplate"} {
		value, _, _ := unstructured.NestedFieldNoCopy(actual.Object, "spec", key)
		if !januaStageAdmissionEmpty(value) {
			return false
		}
	}
	return true
}

func januaStageAdmissionEmpty(value any) bool {
	if value == nil {
		return true
	}
	switch v := value.(type) {
	case []any:
		return len(v) == 0
	case map[string]any:
		return len(v) == 0
	case string:
		return v == ""
	}
	return false
}

// API defaults may add fields, but cannot change/remove the closed role flags,
// SQL, image, resource allocation, storage, or bootstrap owner. Returned objects
// are neither exported nor used in an apply path. Future apply must independently
// review all defaulted fields and repeat qualification against fresh authority.
func januaStageAdmissionContains(expected, actual any) bool {
	switch wanted := expected.(type) {
	case map[string]any:
		got, ok := actual.(map[string]any)
		if !ok {
			return false
		}
		for key, value := range wanted {
			if !januaStageAdmissionContains(value, got[key]) {
				return false
			}
		}
		return true
	case []any:
		got, ok := actual.([]any)
		if !ok || len(wanted) != len(got) {
			return false
		}
		for i, value := range wanted {
			if !januaStageAdmissionContains(value, got[i]) {
				return false
			}
		}
		return true
	default:
		// JSON numeric normalization accommodates CRD serializers without
		// allowing a changed value to pass as an admission receipt.
		a, _ := json.Marshal(expected)
		b, _ := json.Marshal(actual)
		return string(a) == string(b)
	}
}

func januaStageAdmissionPodSafe(expected, actual *corev1.Pod) bool {
	return len(actual.Spec.Containers) == 1 && len(actual.Spec.InitContainers) == 0 && len(actual.Spec.EphemeralContainers) == 0 && len(actual.Spec.Volumes) == 0 && len(actual.Spec.ImagePullSecrets) == 0 && !actual.Spec.HostNetwork && !actual.Spec.HostPID && !actual.Spec.HostIPC && actual.Spec.NodeName == "" &&
		actual.Spec.AutomountServiceAccountToken != nil && !*actual.Spec.AutomountServiceAccountToken && actual.Spec.RestartPolicy == expected.Spec.RestartPolicy &&
		januaStageAdmissionObjectUnchanged(expected.Spec.SecurityContext, actual.Spec.SecurityContext) &&
		januaStageAdmissionObjectUnchanged(expected.Spec.Containers[0].SecurityContext, actual.Spec.Containers[0].SecurityContext) &&
		apiequality.Semantic.DeepEqual(expected.Spec.Containers[0].Resources, actual.Spec.Containers[0].Resources) &&
		actual.Spec.Containers[0].Image == januaStagePostgresImage && januaStageAdmissionObjectUnchanged(expected.Spec.Containers[0].Command, actual.Spec.Containers[0].Command) && januaStageAdmissionObjectUnchanged(expected.Spec.Containers[0].Args, actual.Spec.Containers[0].Args) &&
		len(actual.Spec.Containers[0].Env) == 0 && len(actual.Spec.Containers[0].EnvFrom) == 0 && len(actual.Spec.Containers[0].VolumeMounts) == 0 && len(actual.Spec.Containers[0].VolumeDevices) == 0 && actual.Spec.Containers[0].Lifecycle == nil
}
